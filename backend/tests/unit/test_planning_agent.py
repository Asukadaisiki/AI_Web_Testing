"""Unit tests for test_planning_agent module."""

from __future__ import annotations

import pytest

from app.ai.test_planning_agent import (
    _auto_explore_entry_url,
    _build_draft_prompt,
    _build_link_selection_message,
    _count_explored_elements,
    _extract_internal_links,
    _extract_page_elements,
    _extract_undefined_variables,
    _has_explored_pages,
    _has_internal_links_in_tool_calls,
    _extract_links_from_tool_calls,
    _was_link_list_presented,
    _get_presented_links,
    _clear_link_tracking,
    _track_link_presentation,
    _looks_like_login,
    _looks_like_login_requirements,
    _is_login_url,
    _rank_links_by_flow_relevance,
    _is_asking_about_explorable_elements,
    _find_unexplored_login_url,
    _auto_explore_entry_and_find_login,
    _tool_call_signature,
)
from app.schemas.ai_planning import AIPlanningRequirements, AIPlanningToolCall


def test_draft_prompt_includes_a11y_targeting_hint() -> None:
    """_build_draft_prompt should require explored a11y role/name targets."""
    requirements = AIPlanningRequirements(
        app_under_test="Login Page",
        business_goal="Test login",
        entry_url_or_page="https://example.com/login",
    )
    prompt = _build_draft_prompt(requirements, scenario_title="登录成功", negative_case=False)
    assert 'role="name"' in prompt
    assert "不要构造 CSS 选择器" in prompt


def _planning_settings(**overrides):
    from types import SimpleNamespace

    values = {
        "enable_ai_planning": True,
        "ai_planning_model": "gpt-4.1-mini",
        "ai_planning_base_url": "https://api.openai.com/v1",
        "ai_planning_api_key": "planning-key",
        "ai_planning_timeout_ms": 30000,
        "ai_planning_max_react_safety_cap": 30,
        "ai_planning_context_compress_threshold": 10,
        "ai_planning_context_keep_recent": 4,
    }
    values.update(overrides)
    return SimpleNamespace(**values)


def test_stream_planning_llm_yields_text_chunks_and_full_response(monkeypatch) -> None:
    from app.ai import test_planning_agent as planning_agent

    class FakeStreamResponse:
        def __enter__(self):
            return self

        def __exit__(self, exc_type, exc, tb):
            return False

        def raise_for_status(self):
            pass

        def iter_lines(self):
            yield 'data: {"choices":[{"delta":{"content":"你好"}}]}'
            yield 'data: {"choices":[{"delta":{"content":"，世界"}}]}'
            yield "data: [DONE]"

    class FakeClient:
        def __enter__(self):
            return self

        def __exit__(self, exc_type, exc, tb):
            return False

        def stream(self, *args, **kwargs):
            return FakeStreamResponse()

    monkeypatch.setattr(planning_agent.httpx, "Client", lambda timeout: FakeClient())

    events = list(
        planning_agent._stream_planning_llm(
            messages=[{"role": "user", "content": "帮我规划登录测试"}],
            api_key="k",
            model="glm-4.7",
            base_url="https://example.com/v1",
            timeout_seconds=30,
        )
    )

    assert events == [
        {"type": "text_chunk", "text": "你好"},
        {"type": "text_chunk", "text": "，世界"},
        {"type": "raw_response", "text": "你好，世界"},
    ]


def test_stream_planning_turn_emits_status_then_turn_complete(monkeypatch) -> None:
    from app.ai import test_planning_agent as planning_agent

    monkeypatch.setattr(planning_agent, "get_settings", lambda: _planning_settings())
    monkeypatch.setattr(
        planning_agent,
        "_stream_planning_llm",
        lambda **_: iter(
            [
                {"type": "text_chunk", "text": '{"action":"generate_plan","action_input":{"summary":"登录测试方案"}}'},
                {"type": "raw_response", "text": '{"action":"generate_plan","action_input":{"summary":"登录测试方案"}}'},
            ]
        ),
    )

    stream = planning_agent.stream_planning_turn(
        transcript=[{"role": "user", "content": "帮我规划登录测试"}],
        existing_requirements=None,
        db_session=object(),
        project_id=1,
    )
    events: list[dict] = []
    with pytest.raises(StopIteration) as stop:
        while True:
            events.append(next(stream))

    assert events[0] == {"type": "status", "phase": "thinking", "message": "正在分析需求..."}
    assert events[-1]["type"] == "turn_complete"
    assert stop.value.value.session_status == "plan_ready"


def test_run_planning_turn_wraps_stream_planning_turn(monkeypatch) -> None:
    from app.ai import test_planning_agent as planning_agent

    monkeypatch.setattr(planning_agent, "get_settings", lambda: _planning_settings())
    monkeypatch.setattr(
        planning_agent,
        "_stream_planning_llm",
        lambda **_: iter(
            [
                {"type": "text_chunk", "text": '{"action":"ask_user","action_input":{"message":"请补充入口页面"},"collected_info":{"app_under_test":"商城"}}'},
                {"type": "raw_response", "text": '{"action":"ask_user","action_input":{"message":"请补充入口页面"},"collected_info":{"app_under_test":"商城"}}'},
            ]
        ),
    )

    result = planning_agent.run_planning_turn(
        transcript=[{"role": "user", "content": "帮我规划测试"}],
        existing_requirements=None,
        db_session=object(),
        project_id=1,
    )

    assert result.session_status == "collecting"
    assert result.assistant_message == "请补充入口页面"
    assert result.requirements.app_under_test == "商城"


class TestHasExploredPages:
    def test_returns_false_when_empty(self) -> None:
        assert _has_explored_pages([]) is False

    def test_returns_true_for_explore_page(self) -> None:
        calls = [AIPlanningToolCall(tool="explore_page", params={}, result={})]
        assert _has_explored_pages(calls) is True

    def test_returns_true_for_explore_flow(self) -> None:
        calls = [AIPlanningToolCall(tool="explore_flow", params={}, result={})]
        assert _has_explored_pages(calls) is True

    def test_returns_false_for_other_tools(self) -> None:
        calls = [AIPlanningToolCall(tool="get_project_info", params={}, result={})]
        assert _has_explored_pages(calls) is False


class TestExtractPageElements:
    def test_extracts_from_explore_page(self) -> None:
        calls = [AIPlanningToolCall(
            tool="explore_page",
            params={"url": "https://example.com"},
            result={"formatted": "input [placeholder='Email']"},
        )]
        assert _extract_page_elements(calls) == "input [placeholder='Email']"

    def test_extracts_from_explore_flow(self) -> None:
        calls = [AIPlanningToolCall(
            tool="explore_flow",
            params={"urls": ["https://example.com"]},
            result={"formatted": "=== 页面: https://example.com ===\nbutton [text='Login']"},
        )]
        assert "button [text='Login']" in _extract_page_elements(calls)

    def test_returns_none_when_no_explore_calls(self) -> None:
        calls = [AIPlanningToolCall(tool="get_project_info", params={}, result={})]
        assert _extract_page_elements(calls) is None

    def test_returns_none_when_formatted_empty(self) -> None:
        calls = [AIPlanningToolCall(tool="explore_page", params={}, result={"formatted": ""})]
        assert _extract_page_elements(calls) is None


class TestAutoExploreEntryUrl:
    def test_skips_when_no_entry_url(self) -> None:
        requirements = AIPlanningRequirements()
        explored, calls, links = _auto_explore_entry_url(requirements, [], object(), 1)
        assert explored is False
        assert calls == []
        assert links == []

    def test_skips_when_entry_url_not_a_url(self) -> None:
        requirements = AIPlanningRequirements(entry_url_or_page="登录页面")
        explored, calls, links = _auto_explore_entry_url(requirements, [], object(), 1)
        assert explored is False
        assert links == []

    def test_auto_explores_valid_url(self) -> None:
        from unittest.mock import patch

        requirements = AIPlanningRequirements(entry_url_or_page="https://example.com/login")
        mock_result = '{"url":"https://example.com/login","formatted":"input [placeholder=Email]","element_count":1}'

        with patch("app.ai.test_planning_agent.execute_tool", return_value=mock_result):
            explored, calls, links = _auto_explore_entry_url(requirements, [], object(), 1)

        assert explored is True
        assert len(calls) == 1
        assert calls[0].tool == "explore_page"
        assert isinstance(links, list)

    def test_extracts_internal_links_from_result(self) -> None:
        from unittest.mock import patch

        requirements = AIPlanningRequirements(entry_url_or_page="https://example.com")
        mock_result = (
            '{"url":"https://example.com","formatted":"a [href=/login]","element_count":2,'
            '"elements":[{"tag":"a","href":"/login"},{"tag":"a","href":"/products"}]}'
        )

        with patch("app.ai.test_planning_agent.execute_tool", return_value=mock_result):
            explored, calls, links = _auto_explore_entry_url(requirements, [], object(), 1)

        assert explored is True
        assert len(links) == 2
        assert any("/login" in u for u in links)


class TestExtractInternalLinks:
    def test_returns_empty_for_none(self) -> None:
        assert _extract_internal_links(None, "https://example.com") == []

    def test_returns_empty_for_no_elements(self) -> None:
        result = {"elements": []}
        assert _extract_internal_links(result, "https://example.com") == []

    def test_extracts_same_domain_links(self) -> None:
        result = {
            "elements": [
                {"tag": "a", "href": "/login"},
                {"tag": "a", "href": "/products"},
                {"tag": "a", "href": "https://other.com/else"},
                {"tag": "span", "href": "/ignored"},
                {"tag": "a", "href": "#anchor"},
                {"tag": "a", "href": "javascript:void(0)"},
            ]
        }
        links = _extract_internal_links(result, "https://example.com")
        assert len(links) == 2
        assert "https://example.com/login" in links
        assert "https://example.com/products" in links

    def test_respects_max_links(self) -> None:
        result = {
            "elements": [{"tag": "a", "href": f"/page{i}"} for i in range(30)]
        }
        links = _extract_internal_links(result, "https://example.com", max_links=5)
        assert len(links) == 5


class TestLinkSelectionMessage:
    def test_includes_links_and_sentinel(self) -> None:
        msg = _build_link_selection_message(
            ["https://example.com/login", "https://example.com/cart"],
            "登录并加入购物车",
        )
        assert "https://example.com/login" in msg
        assert "登录并加入购物车" in msg
        assert "⟨LINK_LIST⟩" in msg
        assert "explore_flow" in msg

    def test_empty_links_returns_hint(self) -> None:
        msg = _build_link_selection_message([], None)
        assert "未发现" in msg

    def test_no_core_user_flow(self) -> None:
        msg = _build_link_selection_message(["https://example.com/login"], None)
        assert "https://example.com/login" in msg
        assert "⟨LINK_LIST⟩" in msg


class TestLinkTracking:
    def test_was_presented_detects_sentinel(self) -> None:
        conv: list[dict[str, str]] = []
        assert _was_link_list_presented(conv) is False

        msg = _build_link_selection_message(["https://x.com/a"], None)
        _track_link_presentation(conv, ["https://x.com/a"])
        conv.append({"role": "system", "content": msg})
        assert _was_link_list_presented(conv) is True

    def test_get_presented_links(self) -> None:
        conv: list[dict[str, str]] = []
        msg = _build_link_selection_message(
            ["https://x.com/a", "https://x.com/b"], None,
        )
        conv.append({"role": "system", "content": msg})
        links = _get_presented_links(conv)
        assert "https://x.com/a" in links
        assert "https://x.com/b" in links

    def test_clear_link_tracking_removes_sentinel(self) -> None:
        conv: list[dict[str, str]] = []
        msg = _build_link_selection_message(["https://x.com/a"], None)
        conv.append({"role": "system", "content": msg})
        _clear_link_tracking(conv)
        assert "⟨LINK_LIST⟩" not in conv[0]["content"]
        assert "https://x.com/a" in conv[0]["content"]


class TestLinkExtractionFromToolCalls:
    def test_has_links_when_explore_page_has_elements(self) -> None:
        calls = [
            AIPlanningToolCall(
                tool="explore_page",
                params={"url": "https://example.com"},
                result={
                    "url": "https://example.com",
                    "elements": [
                        {"tag": "a", "href": "/login"},
                        {"tag": "a", "href": "/cart"},
                    ],
                },
            )
        ]
        assert _has_internal_links_in_tool_calls(calls) is True

    def test_no_links_when_no_explore_calls(self) -> None:
        calls = [AIPlanningToolCall(tool="get_project_info", params={}, result={})]
        assert _has_internal_links_in_tool_calls(calls) is False

    def test_extract_links_returns_urls(self) -> None:
        calls = [
            AIPlanningToolCall(
                tool="explore_page",
                params={"url": "https://example.com"},
                result={
                    "url": "https://example.com",
                    "elements": [
                        {"tag": "a", "href": "/login"},
                        {"tag": "a", "href": "/cart"},
                    ],
                },
            )
        ]
        links = _extract_links_from_tool_calls(calls, None)
        assert len(links) == 2


class TestLooksLikeLoginRequirements:
    def test_flow_mentions_login(self) -> None:
        req = AIPlanningRequirements(
            app_under_test="Test",
            core_user_flow="1. 点击login进入登录页面",
        )
        assert _looks_like_login_requirements(req) is True

    def test_test_data_has_email_and_password(self) -> None:
        req = AIPlanningRequirements(
            app_under_test="Test",
            test_data_or_account="账号: test@example.com, 密码: 123456",
        )
        assert _looks_like_login_requirements(req) is True

    def test_test_data_email_only_returns_false(self) -> None:
        req = AIPlanningRequirements(
            app_under_test="Test",
            test_data_or_account="邮箱: test@example.com",
        )
        assert _looks_like_login_requirements(req) is False

    def test_no_login_indicators(self) -> None:
        req = AIPlanningRequirements(
            app_under_test="Test",
            business_goal="验证购物车功能",
            test_data_or_account="商品名称: Test Item",
        )
        assert _looks_like_login_requirements(req) is False

    def test_business_goal_mentions_login(self) -> None:
        req = AIPlanningRequirements(
            app_under_test="Test",
            business_goal="验证用户登录后购物车",
        )
        assert _looks_like_login_requirements(req) is True


class TestIsLoginUrl:
    def test_login_path(self) -> None:
        assert _is_login_url("https://example.com/login") is True

    def test_signin_path(self) -> None:
        assert _is_login_url("https://example.com/signin") is True

    def test_sign_in_path(self) -> None:
        assert _is_login_url("https://example.com/sign-in") is True

    def test_auth_path(self) -> None:
        assert _is_login_url("https://example.com/auth") is True

    def test_products_path(self) -> None:
        assert _is_login_url("https://example.com/products") is False

    def test_case_insensitive(self) -> None:
        assert _is_login_url("https://example.com/LOGIN") is True

    def test_login_in_subpath(self) -> None:
        assert _is_login_url("https://example.com/user/login") is True


class TestRankLinksByFlowRelevance:
    def test_login_ranked_first_when_flow_mentions_login(self) -> None:
        links = [
            "https://example.com/products",
            "https://example.com/login",
            "https://example.com/contact",
        ]
        ranked = _rank_links_by_flow_relevance(links, "用户需要登录后购物")
        assert ranked[0] == "https://example.com/login"

    def test_product_ranked_first_when_flow_mentions_product(self) -> None:
        links = [
            "https://example.com/contact",
            "https://example.com/products",
            "https://example.com/login",
        ]
        ranked = _rank_links_by_flow_relevance(links, "browse products filter brands add to cart")
        assert "products" in ranked[0]

    def test_cart_ranked_first_when_flow_mentions_cart(self) -> None:
        links = [
            "https://example.com/products",
            "https://example.com/view_cart",
        ]
        ranked = _rank_links_by_flow_relevance(links, "view shopping cart add items")
        assert "cart" in ranked[0]

    def test_no_flow_returns_original_order(self) -> None:
        links = ["https://example.com/z", "https://example.com/a"]
        ranked = _rank_links_by_flow_relevance(links, None)
        assert ranked == links

    def test_empty_links(self) -> None:
        assert _rank_links_by_flow_relevance([], "login flow") == []


class TestIsAskingAboutExplorableElements:
    def test_login_question(self) -> None:
        assert _is_asking_about_explorable_elements("请问登录页面的邮箱输入框定位是什么？") is True

    def test_email_question(self) -> None:
        assert _is_asking_about_explorable_elements("请提供email输入框的定位器") is True

    def test_password_question(self) -> None:
        assert _is_asking_about_explorable_elements("密码输入框的locator是什么？") is True

    def test_normal_question(self) -> None:
        assert _is_asking_about_explorable_elements("你希望我生成一个测试方案吗？") is False

    def test_empty_message(self) -> None:
        assert _is_asking_about_explorable_elements("") is False

    def test_selector_keyword(self) -> None:
        assert _is_asking_about_explorable_elements("请告诉我要用什么selector") is True


class TestFindUnexploredLoginUrl:
    def test_finds_login_link_in_explore_result(self) -> None:
        req = AIPlanningRequirements(
            entry_url_or_page="https://example.com",
        )
        calls = [
            AIPlanningToolCall(
                tool="explore_page",
                params={"url": "https://example.com"},
                result={
                    "url": "https://example.com",
                    "elements": [
                        {"tag": "a", "href": "/login"},
                        {"tag": "a", "href": "/products"},
                    ],
                },
            )
        ]
        result = _find_unexplored_login_url(calls, req)
        assert result == "https://example.com/login"

    def test_skips_already_explored_login(self) -> None:
        req = AIPlanningRequirements(
            entry_url_or_page="https://example.com",
        )
        calls = [
            AIPlanningToolCall(
                tool="explore_page",
                params={"url": "https://example.com"},
                result={
                    "url": "https://example.com",
                    "elements": [{"tag": "a", "href": "/login"}],
                },
            ),
            AIPlanningToolCall(
                tool="explore_page",
                params={"url": "https://example.com/login"},
                result={"url": "https://example.com/login", "elements": []},
            ),
        ]
        result = _find_unexplored_login_url(calls, req)
        assert result is None

    def test_no_login_link_returns_none(self) -> None:
        req = AIPlanningRequirements(
            entry_url_or_page="https://example.com",
        )
        calls = [
            AIPlanningToolCall(
                tool="explore_page",
                params={"url": "https://example.com"},
                result={
                    "url": "https://example.com",
                    "elements": [{"tag": "a", "href": "/products"}],
                },
            )
        ]
        result = _find_unexplored_login_url(calls, req)
        assert result is None

    def test_no_explore_calls_returns_none(self) -> None:
        req = AIPlanningRequirements(
            entry_url_or_page="https://example.com",
        )
        result = _find_unexplored_login_url([], req)
        assert result is None


class TestAutoExploreEntryAndFindLogin:
    def test_explores_entry_url_and_returns_login_link(self) -> None:
        from unittest.mock import patch

        req = AIPlanningRequirements(
            entry_url_or_page="https://example.com",
            test_data_or_account="email: test@example.com, password: 123456",
        )
        mock_result = (
            '{"url":"https://example.com","formatted":"a [text=Login]","element_count":2,'
            '"elements":[{"tag":"a","href":"/login"},{"tag":"a","href":"/products"}]}'
        )

        with patch("app.ai.test_planning_agent.execute_tool", return_value=mock_result):
            result = _auto_explore_entry_and_find_login(req, [], object(), 1)

        assert result == "https://example.com/login"

    def test_returns_none_when_no_entry_url(self) -> None:
        req = AIPlanningRequirements()
        assert _auto_explore_entry_and_find_login(req, [], object(), 1) is None

    def test_returns_none_when_no_login_links(self) -> None:
        from unittest.mock import patch

        req = AIPlanningRequirements(entry_url_or_page="https://example.com")
        mock_result = (
            '{"url":"https://example.com","formatted":"a [text=Home]","element_count":1,'
            '"elements":[{"tag":"a","href":"/products"}]}'
        )

        with patch("app.ai.test_planning_agent.execute_tool", return_value=mock_result):
            result = _auto_explore_entry_and_find_login(req, [], object(), 1)

        assert result is None

    def test_adds_explore_page_to_tool_calls(self) -> None:
        from unittest.mock import patch

        req = AIPlanningRequirements(
            entry_url_or_page="https://example.com",
            test_data_or_account="email: test@example.com, password: 123456",
        )
        mock_result = (
            '{"url":"https://example.com","formatted":"a [text=Login]","element_count":2,'
            '"elements":[{"tag":"a","href":"/login"}]}'
        )
        calls: list = []

        with patch("app.ai.test_planning_agent.execute_tool", return_value=mock_result):
            _auto_explore_entry_and_find_login(req, calls, object(), 1)

        assert len(calls) == 1
        assert calls[0].tool == "explore_page"
        assert calls[0].params == {"url": "https://example.com"}


class TestCountExploredElements:
    def test_count_explored_elements_empty(self) -> None:
        assert _count_explored_elements([]) == 0

    def test_count_explored_elements_with_explore_page(self) -> None:
        calls = [
            AIPlanningToolCall(tool="explore_page", params={}, result={"element_count": 250}),
            AIPlanningToolCall(
                tool="explore_flow", params={}, result={"pages": [
                    {"element_count": 100}, {"element_count": 50}
                ]}
            ),
        ]
        assert _count_explored_elements(calls) == 400


class TestExtractUndefinedVariables:
    def test_extract_undefined_variables_detects_missing(self) -> None:
        steps = [
            {"action": "assert_text", "target": "td > h4", "value": "${product_a_name}"},
            {"action": "assert_text", "target": "td > p", "value": "${product_a_price}"},
        ]
        input_contract = [
            {"name": "登录邮箱", "context_key": "login_email", "value_type": "string", "required": True},
        ]
        undefined = _extract_undefined_variables(steps, input_contract)
        assert "product_a_name" in undefined
        assert "product_a_price" in undefined
        assert "login_email" not in undefined

    def test_extract_undefined_variables_handles_capture_text(self) -> None:
        steps = [
            {"action": "capture_text", "target": "Product Name", "context_key": "product_a_name"},
            {"action": "assert_text", "target": "cart td", "value": "${product_a_name}"},
        ]
        undefined = _extract_undefined_variables(steps, [])
        assert len(undefined) == 0

    def test_extract_undefined_variables_all_vars_defined(self) -> None:
        steps = [
            {"action": "input", "target": "Email", "value": "${login_email}"},
            {"action": "input", "target": "Password", "value": "${login_password}"},
        ]
        input_contract = [
            {"context_key": "login_email", "value_type": "string", "name": "邮箱", "required": True},
            {"context_key": "login_password", "value_type": "string", "name": "密码", "required": True},
        ]
        assert _extract_undefined_variables(steps, input_contract) == []


class TestToolCallSignature:
    """Bug C: tool call dedup signature behavior."""

    def test_create_project_dedup_by_name_case_insensitive(self) -> None:
        a = _tool_call_signature("create_project", {"name": "Automation Exercise", "description": "x"})
        b = _tool_call_signature("create_project", {"name": "automation exercise", "description": "different"})
        assert a == b
        assert a is not None

    def test_create_project_no_name_returns_none(self) -> None:
        assert _tool_call_signature("create_project", {}) is None
        assert _tool_call_signature("create_project", {"name": "  "}) is None

    def test_explore_flow_dedup_by_base_url_and_steps(self) -> None:
        params1 = {
            "base_url": "https://example.com",
            "flow_description": "Login flow",
            "steps": [{"url": "/login", "actions": [{"action": "click", "target": "Login"}]}],
        }
        params2 = {
            "base_url": "https://example.com/",  # trailing slash should be canonicalized
            "flow_description": "login flow",  # case-insensitive
            "steps": [{"url": "/login", "actions": [{"action": "click", "target": "Login"}]}],
        }
        assert _tool_call_signature("explore_flow", params1) == _tool_call_signature("explore_flow", params2)

    def test_explore_flow_different_steps_have_different_signatures(self) -> None:
        params1 = {"base_url": "https://x.com", "steps": [{"url": "/a"}]}
        params2 = {"base_url": "https://x.com", "steps": [{"url": "/b"}]}
        assert _tool_call_signature("explore_flow", params1) != _tool_call_signature("explore_flow", params2)

    def test_explore_flow_input_value_ignored_in_signature(self) -> None:
        """input values (e.g., passwords) shouldn't affect dedup signature."""
        params1 = {
            "base_url": "https://x.com",
            "steps": [{"url": "/", "actions": [{"action": "input", "target": "Email", "value": "a@x.com"}]}],
        }
        params2 = {
            "base_url": "https://x.com",
            "steps": [{"url": "/", "actions": [{"action": "input", "target": "Email", "value": "b@x.com"}]}],
        }
        assert _tool_call_signature("explore_flow", params1) == _tool_call_signature("explore_flow", params2)

    def test_explore_page_dedup_by_url(self) -> None:
        a = _tool_call_signature("explore_page", {"url": "https://x.com/login"})
        b = _tool_call_signature("explore_page", {"url": "https://x.com/login/"})  # trailing slash
        assert a == b

    def test_unsupported_tool_returns_none(self) -> None:
        """Tools not eligible for dedup return None so they always execute."""
        assert _tool_call_signature("get_project_info", {}) is None
        assert _tool_call_signature("list_test_cases", {"search": "login"}) is None
