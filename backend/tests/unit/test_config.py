"""Tests for application configuration parsing."""

from __future__ import annotations

import pytest

from app.core.config import get_settings


def test_get_settings_requires_auth_session_secret(monkeypatch) -> None:
    monkeypatch.delenv("AUTH_SESSION_SECRET", raising=False)
    get_settings.cache_clear()

    try:
        with pytest.raises(RuntimeError, match="AUTH_SESSION_SECRET"):
            get_settings()
    finally:
        get_settings.cache_clear()


def test_get_settings_uses_secure_cookie_by_default(monkeypatch) -> None:
    monkeypatch.setenv("AUTH_SESSION_SECRET", "test-session-secret")
    monkeypatch.delenv("AUTH_SESSION_HTTPS_ONLY", raising=False)
    get_settings.cache_clear()

    try:
        settings = get_settings()
    finally:
        get_settings.cache_clear()

    assert settings.auth_session_https_only is True


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
