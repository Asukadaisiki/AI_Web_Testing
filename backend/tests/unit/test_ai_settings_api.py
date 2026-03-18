"""Tests for runtime AI settings API."""

from __future__ import annotations

from pathlib import Path

from fastapi.testclient import TestClient
import pytest

import app.core.config as config_module
import app.main as main_module
from app.db import Base
from app.db.session import get_engine
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

    engine = get_engine()
    Base.metadata.create_all(bind=engine)
    with engine.begin() as connection:
        connection.execute(User.__table__.insert().values(id=1, email="seed-owner@example.com", display_name="Seed"))

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


def test_get_ai_settings_overview_returns_runtime_generation_stats(
    ai_settings_client: TestClient,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setenv("ENABLE_AI_DSL_GENERATE", "true")
    monkeypatch.setenv("AI_DSL_MODEL", "gpt-dsl")
    monkeypatch.setenv("AI_DSL_STRICT_MODE", "true")
    monkeypatch.setenv("AI_DSL_ALLOW_AUTO_REPAIR", "false")
    config_module.get_settings.cache_clear()

    ai_settings_client.post(
        "/api/v1/dsl/generate",
        json={
            "prompt": "打开 example.com",
            "actor_user_id": 1,
        },
    )

    response = ai_settings_client.get("/api/v1/settings/ai/overview")

    assert response.status_code == 200
    assert response.json() == {
        "ai_dsl_enabled": True,
        "ai_dsl_model": "gpt-dsl",
        "ai_dsl_strict_mode": True,
        "ai_dsl_allow_auto_repair": False,
        "generation_stats": {
            "total_requests": 1,
            "success_count": 0,
            "failure_count": 1,
            "accepted_count": 0,
            "rejected_count": 0,
            "pending_count": 1,
            "decision_coverage_rate": 0.0,
            "last_model": "gpt-dsl",
            "last_error_type": "DslGenerationConfigError",
            "last_error_message": "AI DSL 生成配置不完整。请提供 AI_DSL_API_KEY 与 AI_DSL_MODEL。",
            "last_24h_requests": 1,
            "last_24h_success_count": 0,
            "last_24h_failure_count": 1,
            "last_24h_auto_repair_rate": 0.0,
            "top_error_types": [
                {
                    "error_type": "DslGenerationConfigError",
                    "count": 1,
                }
            ],
            "accepted_import_mode_breakdown": [],
            "top_rejection_reasons": [],
            "model_outcome_breakdown": [
                {
                    "model_name": "gpt-dsl",
                    "total_requests": 1,
                    "success_count": 0,
                    "accepted_count": 0,
                    "rejected_count": 0,
                }
            ],
            "generation_mode_breakdown": [
                {
                    "generation_mode": "strict_steps_only",
                    "total_requests": 1,
                    "success_count": 0,
                    "accepted_count": 0,
                    "rejected_count": 0,
                }
            ],
        },
    }


def test_get_ai_settings_overview_includes_feedback_governance_stats(
    ai_settings_client: TestClient,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setenv("ENABLE_AI_DSL_GENERATE", "true")
    monkeypatch.setenv("AI_DSL_API_KEY", "test-key")
    monkeypatch.setenv("AI_DSL_MODEL", "gpt-dsl")
    config_module.get_settings.cache_clear()

    responses = iter(
        [
            """
{
  "name": "accepted replace",
  "steps": [{"action": "goto", "value": "/replace"}]
}""",
            """
{
  "name": "accepted steps",
  "steps": [{"action": "goto", "value": "/steps"}]
}""",
            """
{
  "name": "rejected",
  "steps": [{"action": "goto", "value": "/rejected"}]
}""",
        ]
    )
    monkeypatch.setattr("app.ai.dsl_generator._call_llm", lambda **_: next(responses))

    accepted_replace = ai_settings_client.post(
        "/api/v1/dsl/generate",
        json={"prompt": "accepted replace", "actor_user_id": 1},
    ).json()["generation_id"]
    accepted_steps = ai_settings_client.post(
        "/api/v1/dsl/generate",
        json={"prompt": "accepted steps", "actor_user_id": 1},
    ).json()["generation_id"]
    rejected = ai_settings_client.post(
        "/api/v1/dsl/generate",
        json={"prompt": "rejected", "actor_user_id": 1},
    ).json()["generation_id"]

    accepted_replace_response = ai_settings_client.patch(
        f"/api/v1/dsl/generations/{accepted_replace}/feedback",
        json={
            "actor_user_id": 1,
            "feedback_status": "accepted",
            "feedback_import_mode": "replace",
        },
    )
    accepted_steps_response = ai_settings_client.patch(
        f"/api/v1/dsl/generations/{accepted_steps}/feedback",
        json={
            "actor_user_id": 1,
            "feedback_status": "accepted",
            "feedback_import_mode": "steps_only",
        },
    )
    rejected_response = ai_settings_client.patch(
        f"/api/v1/dsl/generations/{rejected}/feedback",
        json={
            "actor_user_id": 1,
            "feedback_status": "rejected",
            "rejection_reason_code": "bad_contracts",
        },
    )

    assert accepted_replace_response.status_code == 200
    assert accepted_steps_response.status_code == 200
    assert rejected_response.status_code == 200

    overview_response = ai_settings_client.get("/api/v1/settings/ai/overview")

    assert overview_response.status_code == 200
    assert overview_response.json()["generation_stats"]["accepted_count"] == 2
    assert overview_response.json()["generation_stats"]["rejected_count"] == 1
    assert overview_response.json()["generation_stats"]["pending_count"] == 0
    assert overview_response.json()["generation_stats"]["decision_coverage_rate"] == 1.0
    assert overview_response.json()["generation_stats"]["accepted_import_mode_breakdown"] == [
        {"import_mode": "replace", "count": 1},
        {"import_mode": "steps_only", "count": 1},
    ]
    assert overview_response.json()["generation_stats"]["top_rejection_reasons"] == [
        {"rejection_reason_code": "bad_contracts", "count": 1}
    ]
    assert overview_response.json()["generation_stats"]["model_outcome_breakdown"] == [
        {
            "model_name": "gpt-dsl",
            "total_requests": 3,
            "success_count": 3,
            "accepted_count": 2,
            "rejected_count": 1,
        }
    ]
    assert overview_response.json()["generation_stats"]["generation_mode_breakdown"] == [
        {
            "generation_mode": "draft",
            "total_requests": 3,
            "success_count": 3,
            "accepted_count": 2,
            "rejected_count": 1,
        }
    ]
