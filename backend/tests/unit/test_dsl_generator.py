"""Focused tests for DSL generation HTTP response handling."""

from __future__ import annotations

import socket
from urllib.error import URLError

import pytest

from app.ai.dsl_generator import (
    DslGenerationError,
    DslGenerationNetworkError,
    _call_llm,
    _is_transient_network_error,
    _urlopen_with_retry,
)


class _FakeResponse:
    def __init__(self, body: bytes, content_type: str = "text/html; charset=utf-8") -> None:
        self._body = body
        self.status = 200
        self.headers = {"Content-Type": content_type}

    def read(self) -> bytes:
        return self._body

    def __enter__(self) -> _FakeResponse:
        return self

    def __exit__(self, exc_type, exc, tb) -> None:
        return None


def test_call_llm_raises_dsl_generation_error_for_non_json_response(monkeypatch: pytest.MonkeyPatch) -> None:
    from app.ai import dsl_generator

    monkeypatch.setattr(
        dsl_generator.request,
        "urlopen",
        lambda *args, **kwargs: _FakeResponse(b"<html><body>gateway home</body></html>"),
    )

    with pytest.raises(DslGenerationError, match="JSON"):
        _call_llm(
            messages=[{"role": "user", "content": "generate a DSL case"}],
            api_key="test-key",
            model="gpt-4o-mini",
            base_url="https://api.example.com",
            timeout_seconds=1,
        )


# --- Bug B tests ----------------------------------------------------------------

class TestIsTransientNetworkError:
    def test_socket_timeout_is_transient(self) -> None:
        assert _is_transient_network_error(socket.timeout("timed out")) is True

    def test_url_error_with_winerror_10060_is_transient(self) -> None:
        exc = URLError(OSError(10060, "connection timeout"))
        assert _is_transient_network_error(exc) is True

    def test_connection_error_is_transient(self) -> None:
        assert _is_transient_network_error(ConnectionError("refused")) is True

    def test_value_error_is_not_transient(self) -> None:
        assert _is_transient_network_error(ValueError("bad input")) is False


class TestUrlopenWithRetry:
    def test_retries_then_succeeds(self, monkeypatch: pytest.MonkeyPatch) -> None:
        from app.ai import dsl_generator

        calls = {"n": 0}

        def fake_urlopen(req, timeout):
            calls["n"] += 1
            if calls["n"] < 3:
                raise URLError(socket.timeout("timed out"))
            return _FakeResponse(b'{"ok": true}', content_type="application/json")

        monkeypatch.setattr(dsl_generator.request, "urlopen", fake_urlopen)
        monkeypatch.setattr(dsl_generator._time, "sleep", lambda _s: None)

        result = _urlopen_with_retry(
            object(), timeout_seconds=1, max_retries=2, initial_backoff=0.01,
        )
        assert calls["n"] == 3
        assert result.read() == b'{"ok": true}'

    def test_exhausts_retries_and_raises(self, monkeypatch: pytest.MonkeyPatch) -> None:
        from app.ai import dsl_generator

        attempts = {"n": 0}

        def fake_urlopen(req, timeout):
            attempts["n"] += 1
            raise URLError(socket.timeout("persistent timeout"))

        monkeypatch.setattr(dsl_generator.request, "urlopen", fake_urlopen)
        monkeypatch.setattr(dsl_generator._time, "sleep", lambda _s: None)

        with pytest.raises(URLError):
            _urlopen_with_retry(
                object(), timeout_seconds=1, max_retries=2, initial_backoff=0.01,
            )
        # 1 initial attempt + 2 retries = 3 calls
        assert attempts["n"] == 3

    def test_non_transient_error_raises_without_retry(self, monkeypatch: pytest.MonkeyPatch) -> None:
        from app.ai import dsl_generator

        attempts = {"n": 0}

        def fake_urlopen(req, timeout):
            attempts["n"] += 1
            raise ValueError("non-retriable")

        monkeypatch.setattr(dsl_generator.request, "urlopen", fake_urlopen)

        with pytest.raises(ValueError):
            _urlopen_with_retry(
                object(), timeout_seconds=1, max_retries=2, initial_backoff=0.01,
            )
        assert attempts["n"] == 1


def test_dsl_flash_llm_wraps_network_error_with_chinese_message(monkeypatch: pytest.MonkeyPatch) -> None:
    """_call_dsl_flash_llm should raise DslGenerationNetworkError with actionable message on TCP timeout."""
    from app.ai import dsl_generator

    class _FakeSettings:
        ai_dsl_api_key = "test-key"
        ai_dsl_model = "deepseek-v4-pro"
        ai_dsl_flash_model = None
        ai_dsl_base_url = "https://api.deepseek.com"

    def fake_urlopen(req, timeout):
        raise URLError(OSError(10060, "WinError 10060"))

    monkeypatch.setattr(dsl_generator.request, "urlopen", fake_urlopen)
    monkeypatch.setattr(dsl_generator._time, "sleep", lambda _s: None)

    with pytest.raises(DslGenerationNetworkError) as exc_info:
        dsl_generator._call_dsl_flash_llm(
            messages=[{"role": "user", "content": "test"}],
            settings=_FakeSettings(),
            timeout_seconds=1,
        )
    assert "无法连接到 LLM API" in str(exc_info.value)
    assert "WinError 10060" in str(exc_info.value) or "10060" in str(exc_info.value)


def test_generate_dsl_case_propagates_a11y_nodes_by_state(monkeypatch: pytest.MonkeyPatch) -> None:
    """Bug A: generate_dsl_case must pass payload.a11y_nodes_by_state to segmented gen.

    Previously the single-segment path hardcoded page_elements_by_state={}, dropping
    all explored a11y nodes when scenario flow_steps was empty.
    """
    from app.schemas.dsl import GenerateDslRequest, DSLCase, GenerateDslMeta
    from app.services import dsl as dsl_service

    captured: dict[str, object] = {}

    def fake_generate_segmented_case_draft(*, payload, flow_steps, page_elements_by_state):
        captured["page_elements_by_state"] = page_elements_by_state
        captured["flow_steps"] = flow_steps
        return (
            DSLCase.model_validate({
                "name": "x", "description": "x", "base_url": "https://x.com",
                "input_contract": [], "output_contract": [],
                "steps": [{"step_index": 1, "action": "goto", "value": "/"}],
            }),
            [],
            [],
            GenerateDslMeta.model_validate({
                "model": "fake", "generation_mode": "draft", "import_mode": "replace",
                "prompt_variant": "baseline_draft", "context_profile": "blank_request",
                "active_governance_focus_reasons": [], "risk_flags": [],
                "base_url_source": "request", "base_url_backfilled": False,
                "repaired_invalid_actions": 0, "removed_invalid_steps": 0,
                "removed_invalid_contracts": 0, "preserve_contracts_applied": False,
                "used_current_case_context": False, "used_current_steps_context": False,
            }),
        )

    monkeypatch.setattr(dsl_service, "generate_segmented_case_draft", fake_generate_segmented_case_draft)
    monkeypatch.setattr(dsl_service, "_ensure_user_exists", lambda s, uid: None)
    monkeypatch.setattr(dsl_service, "_ensure_project_exists", lambda s, pid: None)
    monkeypatch.setattr(dsl_service, "_ensure_case_exists", lambda s, cid: None)
    monkeypatch.setattr(dsl_service, "_select_governance_focus_reasons", lambda s: [])
    monkeypatch.setattr(dsl_service, "_persist_generation_run", lambda *a, **kw: type("R", (), {"id": 1})())
    monkeypatch.setattr(dsl_service, "_capture_anti_patterns_from_warnings", lambda *a, **kw: None)
    monkeypatch.setattr(dsl_service, "_record_generation_success", lambda *a, **kw: None)

    payload = GenerateDslRequest(
        prompt="test scenario",
        base_url="https://x.com",
        actor_user_id=1,
        project_id=1,
        a11y_nodes_by_state={
            "S0": [{"node_id": "n1", "role": "button", "name": "Login"}],
            "S1": [{"node_id": "n2", "role": "textbox", "name": "Email"}],
        },
        # flow_steps is None (empty) — this used to drop a11y data
    )

    dsl_service.generate_dsl_case(session=None, payload=payload)

    assert captured["page_elements_by_state"] == {
        "S0": [{"node_id": "n1", "role": "button", "name": "Login"}],
        "S1": [{"node_id": "n2", "role": "textbox", "name": "Email"}],
    }


# --- Bug F: LLM step field misplacement (target↔value) ------------------------


class TestNormalizeLlmStep:
    def test_goto_with_target_moved_to_value(self) -> None:
        from app.ai.dsl_generator import _normalize_llm_step
        step = {"action": "goto", "target": "https://example.com/login", "step_index": 1}
        normalized = _normalize_llm_step(step)
        assert normalized is not None
        assert normalized["value"] == "https://example.com/login"
        assert "target" not in normalized

    def test_assert_url_contains_target_moved_to_value(self) -> None:
        from app.ai.dsl_generator import _normalize_llm_step
        step = {"action": "assert_url_contains", "target": "/dashboard"}
        normalized = _normalize_llm_step(step)
        assert normalized is not None
        assert normalized["value"] == "/dashboard"
        assert "target" not in normalized

    def test_goto_with_value_already_set_unchanged(self) -> None:
        from app.ai.dsl_generator import _normalize_llm_step
        step = {"action": "goto", "value": "/login"}
        normalized = _normalize_llm_step(step)
        assert normalized is not None
        assert normalized["value"] == "/login"

    def test_click_target_unchanged(self) -> None:
        """For click, target is correct — must NOT be moved to value."""
        from app.ai.dsl_generator import _normalize_llm_step
        step = {"action": "click", "target": "Login"}
        normalized = _normalize_llm_step(step)
        assert normalized is not None
        assert normalized["target"] == "Login"
        assert "value" not in normalized

    def test_action_alias_navigate_becomes_goto(self) -> None:
        from app.ai.dsl_generator import _normalize_llm_step
        step = {"action": "navigate", "target": "/home"}
        normalized = _normalize_llm_step(step)
        assert normalized is not None
        assert normalized["action"] == "goto"
        assert normalized["value"] == "/home"

    def test_action_alias_open_becomes_goto(self) -> None:
        from app.ai.dsl_generator import _normalize_llm_step
        step = {"action": "open", "target": "https://example.com"}
        normalized = _normalize_llm_step(step)
        assert normalized is not None
        assert normalized["action"] == "goto"
        assert normalized["value"] == "https://example.com"

    def test_invalid_step_returns_none(self) -> None:
        from app.ai.dsl_generator import _normalize_llm_step
        assert _normalize_llm_step(None) is None
        assert _normalize_llm_step("not a dict") is None
        assert _normalize_llm_step({}) is None  # no action
        assert _normalize_llm_step({"action": ""}) is None



