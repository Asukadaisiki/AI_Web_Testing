"""Unit tests for test_planning_agent module."""

from __future__ import annotations

import pytest

from app.ai.test_planning_agent import (
    _auto_explore_entry_url,
    _build_draft_prompt,
    _extract_page_elements,
    _has_explored_pages,
)
from app.schemas.ai_planning import AIPlanningRequirements, AIPlanningToolCall


def test_draft_prompt_includes_dom_aware_hint() -> None:
    """_build_draft_prompt should include DOM-aware targeting hint."""
    requirements = AIPlanningRequirements(
        app_under_test="Login Page",
        business_goal="Test login",
        entry_url_or_page="https://example.com/login",
    )
    prompt = _build_draft_prompt(requirements, scenario_title="登录成功", negative_case=False)
    assert "label" in prompt
    assert "placeholder" in prompt or "实际" in prompt


def _planning_settings(**overrides):
    from types import SimpleNamespace

    values = {
        "enable_ai_planning": True,
        "ai_planning_model": "gpt-4.1-mini",
        "ai_planning_base_url": "https://api.openai.com/v1",
        "ai_planning_api_key": "planning-key",
        "ai_planning_timeout_ms": 30000,
        "ai_planning_max_react_rounds": 3,
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
        explored, calls = _auto_explore_entry_url(requirements, [], object(), 1)
        assert explored is False
        assert calls == []

    def test_skips_when_entry_url_not_a_url(self) -> None:
        requirements = AIPlanningRequirements(entry_url_or_page="登录页面")
        explored, calls = _auto_explore_entry_url(requirements, [], object(), 1)
        assert explored is False

    def test_auto_explores_valid_url(self) -> None:
        from unittest.mock import patch

        requirements = AIPlanningRequirements(entry_url_or_page="https://example.com/login")
        mock_result = '{"url":"https://example.com/login","formatted":"input [placeholder=Email]","element_count":1}'

        with patch("app.ai.test_planning_agent.execute_tool", return_value=mock_result):
            explored, calls = _auto_explore_entry_url(requirements, [], object(), 1)

        assert explored is True
        assert len(calls) == 1
        assert calls[0].tool == "explore_page"
