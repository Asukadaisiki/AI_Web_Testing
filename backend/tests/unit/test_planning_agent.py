"""Unit tests for test_planning_agent module."""

from __future__ import annotations

import pytest

from app.ai.test_planning_agent import _build_draft_prompt
from app.schemas.ai_planning import AIPlanningRequirements


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
