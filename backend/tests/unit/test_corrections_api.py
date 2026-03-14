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


def test_create_list_and_patch_correction_state(client, db_session) -> None:
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
    assert response.json()["page_url_pattern"] == "https://app.example.com/users/*/orders/*?tab=detail"
    assert response.json()["verified_count"] == 0
    assert response.json()["consecutive_failures"] == 0
    assert response.json()["is_active"] is True

    list_response = client.get("/api/v1/corrections", params={"target_description": "提交按钮", "is_active": True})
    assert list_response.status_code == 200
    assert len(list_response.json()) == 1
    assert list_response.json()[0]["correction_value"] == "#submit-btn"

    deactivate_response = client.patch(
        f"/api/v1/corrections/{response.json()['id']}",
        json={"is_active": False},
    )
    assert deactivate_response.status_code == 200
    assert deactivate_response.json()["is_active"] is False

    legacy_response = client.put(f"/api/v1/corrections/{response.json()['id']}/deactivate")
    assert legacy_response.status_code in {404, 405}


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


def test_create_correction_requires_created_by(client, db_session) -> None:
    execution_id = _create_source_execution(client, db_session)

    response = client.post(
        "/api/v1/corrections",
        json={
            "page_url": "https://app.example.com/login",
            "target_description": "login button",
            "correction_type": "css",
            "correction_value": "#login-btn",
            "source_execution_id": execution_id,
        },
    )

    assert response.status_code == 422


def test_list_corrections_supports_case_insensitive_filter_and_pagination(client, db_session) -> None:
    first_execution_id = _create_source_execution(client, db_session)
    second_execution_id = _create_source_execution(client, db_session)

    first_response = client.post(
        "/api/v1/corrections",
        json={
            "page_url": "https://app.example.com/login?session=abc12345xyz67890",
            "target_description": "Login Button",
            "correction_type": "css",
            "correction_value": "#login-btn",
            "source_execution_id": first_execution_id,
            "created_by": 1,
        },
    )
    assert first_response.status_code == 201

    second_response = client.post(
        "/api/v1/corrections",
        json={
            "page_url": "https://app.example.com/login?session=def67890xyz12345",
            "target_description": "login button",
            "correction_type": "xpath",
            "correction_value": "//button[@id='secondary-login']",
            "source_execution_id": second_execution_id,
            "created_by": 1,
        },
    )
    assert second_response.status_code == 201

    filtered = client.get(
        "/api/v1/corrections",
        params={"target_description": "LOGIN BUTTON", "limit": 1, "offset": 0},
    )
    assert filtered.status_code == 200
    assert len(filtered.json()) == 1
    assert filtered.json()[0]["id"] == second_response.json()["id"]

    paged = client.get(
        "/api/v1/corrections",
        params={"target_description": "login button", "limit": 1, "offset": 1},
    )
    assert paged.status_code == 200
    assert len(paged.json()) == 1
    assert paged.json()[0]["id"] == first_response.json()["id"]

    page_filtered = client.get(
        "/api/v1/corrections",
        params={"page_url": "https://app.example.com/login?session=xyz98765abc43210"},
    )
    assert page_filtered.status_code == 200
    assert [item["id"] for item in page_filtered.json()] == [second_response.json()["id"], first_response.json()["id"]]


def test_create_correction_deactivates_existing_active_duplicate(client, db_session) -> None:
    first_execution_id = _create_source_execution(client, db_session)
    second_execution_id = _create_source_execution(client, db_session)

    first_response = client.post(
        "/api/v1/corrections",
        json={
            "page_url": "https://app.example.com/orders/123",
            "target_description": "Order CTA",
            "correction_type": "css",
            "correction_value": "#order-cta",
            "source_execution_id": first_execution_id,
            "created_by": 1,
        },
    )
    assert first_response.status_code == 201

    second_response = client.post(
        "/api/v1/corrections",
        json={
            "page_url": "https://app.example.com/orders/456",
            "target_description": "order cta",
            "correction_type": "test_id",
            "correction_value": "order-cta",
            "source_execution_id": second_execution_id,
            "created_by": 1,
        },
    )
    assert second_response.status_code == 201

    active_records = client.get("/api/v1/corrections", params={"target_description": "ORDER CTA", "is_active": True}).json()
    inactive_records = client.get("/api/v1/corrections", params={"target_description": "Order CTA", "is_active": False}).json()

    assert [record["id"] for record in active_records] == [second_response.json()["id"]]
    assert [record["id"] for record in inactive_records] == [first_response.json()["id"]]
