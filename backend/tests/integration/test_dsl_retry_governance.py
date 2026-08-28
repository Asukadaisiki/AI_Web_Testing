"""Integration coverage for retry governance audit behavior."""

from __future__ import annotations

from app.models import DslGenerationRun
from app.core.config import get_settings


def test_invalid_retry_request_does_not_persist_extra_audit_row(client, db_session, monkeypatch) -> None:
    monkeypatch.setenv("ENABLE_AI_DSL_GENERATE", "true")
    monkeypatch.setenv("AI_DSL_API_KEY", "test-key")
    monkeypatch.setenv("AI_DSL_MODEL", "gpt-test")
    get_settings.cache_clear()
    monkeypatch.setattr(
        "app.ai.dsl_generator._call_llm",
        lambda **_: """
{
  "name": "Rejected Draft",
  "steps": [{"action": "goto", "value": "/retry"}]
}""",
    )

    first_response = client.post(
        "/api/v1/dsl/generate",
        json={
            "prompt": "生成一个可重试草案",
            "actor_user_id": 1,
            "base_url": "https://example.com",
        },
    )
    assert first_response.status_code == 200
    previous_generation_id = first_response.json()["generation_id"]

    reject_response = client.patch(
        f"/api/v1/dsl/generations/{previous_generation_id}/feedback",
        json={
            "actor_user_id": 1,
            "feedback_status": "rejected",
            "rejection_reason_code": "bad_contracts",
            "feedback_note": "契约命名不稳定",
        },
    )
    assert reject_response.status_code == 200

    def should_not_call_llm(**_):
        raise AssertionError("LLM should not be called for invalid retry requests.")

    monkeypatch.setattr("app.ai.dsl_generator._call_llm", should_not_call_llm)

    retry_response = client.post(
        "/api/v1/dsl/generate",
        json={
            "prompt": "伪造 retry_reason_code",
            "actor_user_id": 1,
            "retry_from_generation_id": previous_generation_id,
            "retry_reason_code": "context_mismatch",
        },
    )

    assert retry_response.status_code == 409
    assert "retry_reason_code" in retry_response.json()["detail"]
    runs = db_session.query(DslGenerationRun).order_by(DslGenerationRun.id.asc()).all()
    assert len(runs) == 1
    assert runs[0].id == previous_generation_id
    assert runs[0].feedback_status == "rejected"
