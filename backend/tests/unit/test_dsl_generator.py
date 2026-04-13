"""Focused tests for DSL generation HTTP response handling."""

from __future__ import annotations

import pytest

from app.ai.dsl_generator import DslGenerationError, _call_llm


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
