"""Tests for application configuration parsing."""

from __future__ import annotations

from pathlib import Path

import pytest

import app.core.config as config_module
from app.core.config import get_settings


def test_get_settings_requires_auth_session_secret(monkeypatch, tmp_path: Path) -> None:
    monkeypatch.delenv("AUTH_SESSION_SECRET", raising=False)
    monkeypatch.setattr(config_module, "ENV_FILE_PATH", tmp_path / ".missing.env")
    get_settings.cache_clear()

    try:
        with pytest.raises(RuntimeError, match="AUTH_SESSION_SECRET"):
            get_settings()
    finally:
        get_settings.cache_clear()


def test_get_settings_uses_secure_cookie_by_default(monkeypatch, tmp_path: Path) -> None:
    monkeypatch.setenv("AUTH_SESSION_SECRET", "test-session-secret")
    monkeypatch.delenv("AUTH_SESSION_HTTPS_ONLY", raising=False)
    monkeypatch.setattr(config_module, "ENV_FILE_PATH", tmp_path / ".missing.env")
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

    assert settings.ai_visual_timeout_ms == 600000
    assert settings.ai_visual_failure_threshold == 3
    assert settings.ai_visual_cooldown_seconds == 60
    assert settings.ai_visual_rate_limit_per_minute == 10


def test_env_example_includes_ai_dsl_and_vlm_settings() -> None:
    env_example = Path(__file__).resolve().parents[2] / ".env.example"
    env_text = env_example.read_text(encoding="utf-8")

    required_lines = [
        "ENABLE_AI_DSL_GENERATE=false",
        "AI_DSL_BASE_URL=https://open.bigmodel.cn/api/paas/v4",
        "AI_DSL_MODEL=glm-4.7-flash",
        "ENABLE_AI_VISUAL_LOCATE=false",
        "VLM_BASE_URL=https://api.openai.com/v1",
        "VLM_MODEL=",
        "VLM_MODEL_FAMILY=gpt-4o",
        "VLM_API_KEY=",
    ]

    for line in required_lines:
        assert line in env_text


def test_storage_state_dir_defaults_to_storage_states(monkeypatch, reset_cached_state) -> None:
    """Should default to 'storage_states' directory."""
    monkeypatch.setenv("AUTH_SESSION_SECRET", "test-secret")
    from app.core.config import get_settings
    settings = get_settings()
    assert settings.storage_state_dir == "storage_states"


def test_ai_visual_locate_default_is_disabled(monkeypatch, reset_cached_state) -> None:
    """VLM should be disabled unless an environment explicitly enables it."""
    monkeypatch.setenv("AUTH_SESSION_SECRET", "test-secret")
    monkeypatch.delenv("ENABLE_AI_VISUAL_LOCATE", raising=False)
    monkeypatch.setattr(config_module, "ENV_FILE_PATH", Path("/nonexistent/.env"))
    from app.core.config import get_settings
    get_settings.cache_clear()
    settings = get_settings()
    assert settings.enable_ai_visual_locate is False


def test_enable_ai_visual_locate_can_be_enabled(monkeypatch, reset_cached_state) -> None:
    """Should respect ENABLE_AI_VISUAL_LOCATE=true."""
    monkeypatch.setenv("AUTH_SESSION_SECRET", "test-secret")
    monkeypatch.setenv("ENABLE_AI_VISUAL_LOCATE", "true")
    monkeypatch.setattr(config_module, "ENV_FILE_PATH", Path("/nonexistent/.env"))
    from app.core.config import get_settings
    settings = get_settings()
    assert settings.enable_ai_visual_locate is True
