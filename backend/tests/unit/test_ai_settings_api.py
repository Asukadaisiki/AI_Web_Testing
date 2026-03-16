"""Tests for runtime AI settings API."""

from __future__ import annotations

from pathlib import Path

from fastapi.testclient import TestClient
import pytest

import app.core.config as config_module
import app.main as main_module


@pytest.fixture
def ai_settings_client(monkeypatch: pytest.MonkeyPatch, tmp_path: Path) -> TestClient:
    env_file = tmp_path / ".env"
    env_file.write_text(
        "\n".join(
            [
                "ENABLE_AI_DSL_GENERATE=false",
                "AI_DSL_TIMEOUT_MS=15000",
                "AI_DSL_BASE_URL=https://api.openai.com/v1",
                "AI_DSL_MODEL=",
                "AI_DSL_API_KEY=",
                "ENABLE_AI_VISUAL_LOCATE=false",
                "AI_VISUAL_TIMEOUT_MS=10000",
                "AI_VISUAL_FAILURE_THRESHOLD=3",
                "AI_VISUAL_COOLDOWN_SECONDS=60",
                "AI_VISUAL_RATE_LIMIT_PER_MINUTE=10",
                "VLM_BASE_URL=https://api.openai.com/v1",
                "VLM_MODEL=",
                "VLM_MODEL_FAMILY=gpt-4o",
                "VLM_API_KEY=",
            ]
        )
        + "\n",
        encoding="utf-8",
    )
    monkeypatch.setattr(config_module, "ENV_FILE_PATH", env_file)
    monkeypatch.setattr(main_module, "verify_database_connection", lambda: None)
    config_module.get_settings.cache_clear()

    app = main_module.create_app()
    with TestClient(app) as client:
        yield client


def test_get_ai_settings_masks_secret_values(ai_settings_client: TestClient, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("AI_DSL_API_KEY", "dsl-secret")
    monkeypatch.setenv("VLM_API_KEY", "vlm-secret")
    config_module.get_settings.cache_clear()

    response = ai_settings_client.get("/api/v1/settings/ai")

    assert response.status_code == 200
    assert response.json()["has_ai_dsl_api_key"] is True
    assert response.json()["has_vlm_api_key"] is True
    assert "ai_dsl_api_key" not in response.json()
    assert "vlm_api_key" not in response.json()


def test_update_ai_settings_persists_to_env_file_and_allows_clearing_keys(
    ai_settings_client: TestClient,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setenv("AI_DSL_API_KEY", "old-dsl-secret")
    monkeypatch.setenv("VLM_API_KEY", "old-vlm-secret")
    config_module.get_settings.cache_clear()

    response = ai_settings_client.put(
        "/api/v1/settings/ai",
        json={
            "enable_ai_dsl_generate": True,
            "ai_dsl_timeout_ms": 20000,
            "ai_dsl_base_url": "https://llm.example.com/v1",
            "ai_dsl_model": "gpt-dsl",
            "ai_dsl_api_key": "new-dsl-secret",
            "clear_ai_dsl_api_key": False,
            "enable_ai_visual_locate": True,
            "ai_visual_timeout_ms": 12000,
            "ai_visual_failure_threshold": 4,
            "ai_visual_cooldown_seconds": 90,
            "ai_visual_rate_limit_per_minute": 12,
            "vlm_base_url": "https://vlm.example.com/v1",
            "vlm_model": "gpt-4o-mini",
            "vlm_model_family": "gpt-4o",
            "vlm_api_key": None,
            "clear_vlm_api_key": True,
        },
    )

    assert response.status_code == 200
    assert response.json() == {
        "enable_ai_dsl_generate": True,
        "ai_dsl_timeout_ms": 20000,
        "ai_dsl_base_url": "https://llm.example.com/v1",
        "ai_dsl_model": "gpt-dsl",
        "has_ai_dsl_api_key": True,
        "enable_ai_visual_locate": True,
        "ai_visual_timeout_ms": 12000,
        "ai_visual_failure_threshold": 4,
        "ai_visual_cooldown_seconds": 90,
        "ai_visual_rate_limit_per_minute": 12,
        "vlm_base_url": "https://vlm.example.com/v1",
        "vlm_model": "gpt-4o-mini",
        "vlm_model_family": "gpt-4o",
        "has_vlm_api_key": False,
    }

    env_text = config_module.ENV_FILE_PATH.read_text(encoding="utf-8")
    assert "ENABLE_AI_DSL_GENERATE=true" in env_text
    assert "AI_DSL_TIMEOUT_MS=20000" in env_text
    assert "AI_DSL_BASE_URL=https://llm.example.com/v1" in env_text
    assert "AI_DSL_MODEL=gpt-dsl" in env_text
    assert "AI_DSL_API_KEY=new-dsl-secret" in env_text
    assert "ENABLE_AI_VISUAL_LOCATE=true" in env_text
    assert "AI_VISUAL_TIMEOUT_MS=12000" in env_text
    assert "AI_VISUAL_FAILURE_THRESHOLD=4" in env_text
    assert "AI_VISUAL_COOLDOWN_SECONDS=90" in env_text
    assert "AI_VISUAL_RATE_LIMIT_PER_MINUTE=12" in env_text
    assert "VLM_BASE_URL=https://vlm.example.com/v1" in env_text
    assert "VLM_MODEL=gpt-4o-mini" in env_text
    assert "VLM_MODEL_FAMILY=gpt-4o" in env_text
    assert "VLM_API_KEY=" in env_text
