"""Tests for application configuration parsing."""

from __future__ import annotations

from app.core.config import get_settings


def test_get_settings_falls_back_when_ai_visual_int_env_is_invalid(monkeypatch) -> None:
    monkeypatch.setenv("AI_VISUAL_TIMEOUT_MS", "abc")
    monkeypatch.setenv("AI_VISUAL_FAILURE_THRESHOLD", "oops")
    monkeypatch.setenv("AI_VISUAL_COOLDOWN_SECONDS", "NaN")
    monkeypatch.setenv("AI_VISUAL_RATE_LIMIT_PER_MINUTE", "bad")
    get_settings.cache_clear()

    try:
        settings = get_settings()
    finally:
        get_settings.cache_clear()

    assert settings.ai_visual_timeout_ms == 10000
    assert settings.ai_visual_failure_threshold == 3
    assert settings.ai_visual_cooldown_seconds == 60
    assert settings.ai_visual_rate_limit_per_minute == 10
