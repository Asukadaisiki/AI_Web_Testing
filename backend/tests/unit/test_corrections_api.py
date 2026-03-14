"""Tests for locator correction endpoints."""

from __future__ import annotations

from app.models import TestCaseRun


def _create_source_execution(client, db_session) -> int:
    case_response = client.post(
        "/api/v1/cases",
        json={
            "project_id": 1,
            "actor_user_id": 1,
            "name": "修正来源用例",
            "base_url": "https://example.com",
            "steps": [{"action": "goto", "value": "/login"}],
        },
    )
    case_id = case_response.json()["id"]
    execution = TestCaseRun(
        case_id=case_id,
        project_id=1,
        triggered_by=1,
        status="failed",
        error_message="boom",
    )
    db_session.add(execution)
    db_session.commit()
    db_session.refresh(execution)
    return execution.id


def test_create_list_and_deactivate_correction(client, db_session) -> None:
    execution_id = _create_source_execution(client, db_session)

    response = client.post(
        "/api/v1/corrections",
        json={
            "page_url": "https://app.example.com/users/123/orders/456?tab=detail",
            "target_description": "提交按钮",
            "correction_type": "css",
            "correction_value": "#submit-btn",
            "source_execution_id": execution_id,
            "created_by": 1,
        },
    )

    assert response.status_code == 201
    assert response.json()["page_url_pattern"] == "https://app.example.com/users/*/orders/*"
    assert response.json()["verified_count"] == 0
    assert response.json()["consecutive_failures"] == 0
    assert response.json()["is_active"] is True

    list_response = client.get("/api/v1/corrections", params={"target_description": "提交按钮", "is_active": True})
    assert list_response.status_code == 200
    assert len(list_response.json()) == 1
    assert list_response.json()[0]["correction_value"] == "#submit-btn"

    deactivate_response = client.put(f"/api/v1/corrections/{response.json()['id']}/deactivate")
    assert deactivate_response.status_code == 200
    assert deactivate_response.json()["is_active"] is False


def test_create_correction_requires_existing_execution(client) -> None:
    response = client.post(
        "/api/v1/corrections",
        json={
            "page_url": "https://app.example.com/login",
            "target_description": "登录按钮",
            "correction_type": "xpath",
            "correction_value": "//button[@id='login']",
            "source_execution_id": 999,
            "created_by": 1,
        },
    )

    assert response.status_code == 404
    assert response.json() == {"detail": "Execution 999 not found."}
