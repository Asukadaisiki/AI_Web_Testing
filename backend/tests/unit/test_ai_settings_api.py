"""Tests for runtime AI settings API."""

from __future__ import annotations

from pathlib import Path

from fastapi.testclient import TestClient
import pytest

from app.ai.dsl_generator import AI_DSL_PROMPT_VERSION
from app.core.auth import hash_password
import app.core.config as config_module
import app.main as main_module
from app.db import Base
from app.db.session import get_engine
from app.locators.ai_visual import reset_ai_visual_runtime_state
from app.models import User
from app.services.dsl import reset_dsl_generation_runtime_stats


@pytest.fixture
def ai_settings_client(monkeypatch: pytest.MonkeyPatch, tmp_path: Path) -> TestClient:
    env_file = tmp_path / ".env"
    database_path = tmp_path / "ai-settings-test.db"
    env_file.write_text(
        "\n".join(
            [
                f"DATABASE_URL=sqlite:///{database_path.as_posix()}",
                "ENABLE_AI_DSL_GENERATE=false",
                "AI_DSL_TIMEOUT_MS=15000",
                "AI_DSL_BASE_URL=https://api.openai.com/v1",
                "AI_DSL_MODEL=",
                "AI_DSL_STRICT_MODE=false",
                "AI_DSL_ALLOW_AUTO_REPAIR=true",
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
                "ENABLE_AI_PLANNING=false",
                "AI_PLANNING_MODEL=",
                "AI_PLANNING_BASE_URL=https://api.openai.com/v1",
                "AI_PLANNING_API_KEY=",
                "AI_PLANNING_TIMEOUT_MS=30000",
                "AI_PLANNING_MAX_REACT_ROUNDS=5",
            ]
        )
        + "\n",
        encoding="utf-8",
    )
    monkeypatch.setattr(config_module, "ENV_FILE_PATH", env_file)
    monkeypatch.setattr(main_module, "verify_database_connection", lambda: None)
    config_module.get_settings.cache_clear()
    get_engine.cache_clear()
    reset_dsl_generation_runtime_stats()
    reset_ai_visual_runtime_state()

    engine = get_engine()
    Base.metadata.create_all(bind=engine)
    with engine.begin() as connection:
        connection.execute(
            User.__table__.insert().values(
                id=1,
                email="seed-owner@example.com",
                display_name="Seed",
                password_hash=hash_password("password123"),
                is_active=True,
            )
        )

    app = main_module.create_app()
    with TestClient(app) as client:
        login_response = client.post(
            "/api/v1/auth/login",
            json={"email": "seed-owner@example.com", "password": "password123"},
        )
        assert login_response.status_code == 200
        yield client


def test_get_ai_settings_masks_secret_values(ai_settings_client: TestClient, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("AI_DSL_API_KEY", "dsl-secret")
    monkeypatch.setenv("VLM_API_KEY", "vlm-secret")
    config_module.get_settings.cache_clear()

    response = ai_settings_client.get("/api/v1/settings/ai")

    assert response.status_code == 200
    assert response.json()["has_ai_dsl_api_key"] is True
    assert response.json()["has_vlm_api_key"] is True
    assert response.json()["ai_dsl_strict_mode"] is False
    assert response.json()["ai_dsl_allow_auto_repair"] is True
    assert "ai_dsl_api_key" not in response.json()
    assert "vlm_api_key" not in response.json()


def test_update_ai_settings_persists_to_env_file_and_allows_clearing_keys(
    ai_settings_client: TestClient,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setenv("AI_DSL_API_KEY", "old-dsl-secret")
    monkeypatch.setenv("VLM_API_KEY", "old-vlm-secret")
    monkeypatch.delenv("AI_PLANNING_API_KEY", raising=False)
    config_module.get_settings.cache_clear()

    response = ai_settings_client.put(
        "/api/v1/settings/ai",
        json={
            "enable_ai_dsl_generate": True,
            "ai_dsl_timeout_ms": 20000,
            "ai_dsl_base_url": "https://llm.example.com/v1",
            "ai_dsl_model": "gpt-dsl",
            "ai_dsl_strict_mode": True,
            "ai_dsl_allow_auto_repair": False,
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
            "enable_ai_planning": False,
            "ai_planning_model": None,
            "ai_planning_base_url": "https://api.openai.com/v1",
            "ai_planning_timeout_ms": 30000,
            "ai_planning_max_react_rounds": 5,
            "ai_planning_api_key": None,
            "clear_ai_planning_api_key": False,
        },
    )

    assert response.status_code == 200
    assert response.json() == {
        "enable_ai_dsl_generate": True,
        "ai_dsl_timeout_ms": 20000,
        "ai_dsl_base_url": "https://llm.example.com/v1",
        "ai_dsl_model": "gpt-dsl",
        "ai_dsl_strict_mode": True,
        "ai_dsl_allow_auto_repair": False,
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
        "enable_ai_planning": False,
        "ai_planning_model": None,
        "ai_planning_base_url": "https://api.openai.com/v1",
        "ai_planning_timeout_ms": 30000,
        "ai_planning_max_react_rounds": 5,
        "has_ai_planning_api_key": False,
    }

    env_text = config_module.ENV_FILE_PATH.read_text(encoding="utf-8")
    assert "ENABLE_AI_DSL_GENERATE=true" in env_text
    assert "AI_DSL_TIMEOUT_MS=20000" in env_text
    assert "AI_DSL_BASE_URL=https://llm.example.com/v1" in env_text
    assert "AI_DSL_MODEL=gpt-dsl" in env_text
    assert "AI_DSL_STRICT_MODE=true" in env_text
    assert "AI_DSL_ALLOW_AUTO_REPAIR=false" in env_text
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


def test_update_ai_settings_accepts_glm_model_family(ai_settings_client: TestClient) -> None:
    response = ai_settings_client.put(
        "/api/v1/settings/ai",
        json={
            "enable_ai_dsl_generate": False,
            "ai_dsl_timeout_ms": 15000,
            "ai_dsl_base_url": "https://api.openai.com/v1",
            "ai_dsl_model": None,
            "ai_dsl_strict_mode": False,
            "ai_dsl_allow_auto_repair": True,
            "ai_dsl_api_key": None,
            "clear_ai_dsl_api_key": False,
            "enable_ai_visual_locate": True,
            "ai_visual_timeout_ms": 12000,
            "ai_visual_failure_threshold": 3,
            "ai_visual_cooldown_seconds": 60,
            "ai_visual_rate_limit_per_minute": 10,
            "vlm_base_url": "https://open.bigmodel.cn/api/paas/v4",
            "vlm_model": "glm-4.6v-flash",
            "vlm_model_family": "glm",
            "vlm_api_key": "glm-secret",
            "clear_vlm_api_key": False,
            "enable_ai_planning": False,
            "ai_planning_model": None,
            "ai_planning_base_url": "https://api.openai.com/v1",
            "ai_planning_timeout_ms": 30000,
            "ai_planning_max_react_rounds": 5,
            "ai_planning_api_key": None,
            "clear_ai_planning_api_key": False,
        },
    )

    assert response.status_code == 200
    assert response.json()["vlm_model_family"] == "glm"


