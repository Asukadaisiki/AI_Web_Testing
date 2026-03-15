"""Tests for locator correction endpoints."""

from __future__ import annotations

import pytest
from sqlalchemy.exc import IntegrityError

import app.api.routes.corrections as corrections_route
from app.locators.corrections import SQLAlchemyCorrectionStore
from app.models import TestCaseRun
from app.schemas.corrections import CreateCorrectionRequest
from app.services.corrections import CorrectionConflictError, create_correction


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


def test_create_correction_returns_conflict_when_service_reports_duplicate(client, monkeypatch) -> None:
    def fake_create_correction(*_args, **_kwargs):
        raise CorrectionConflictError("Another active correction already exists for the same page URL pattern and target description.")

    monkeypatch.setattr(corrections_route, "create_correction", fake_create_correction)

    response = client.post(
        "/api/v1/corrections",
        json={
            "page_url": "https://app.example.com/login",
            "target_description": "登录按钮",
            "correction_type": "css",
            "correction_value": "#login-btn",
            "source_execution_id": 1,
            "created_by": 1,
        },
    )

    assert response.status_code == 409
    assert response.json() == {
        "detail": "Another active correction already exists for the same page URL pattern and target description."
    }


def test_create_correction_translates_integrity_error_to_conflict(client, db_session, monkeypatch) -> None:
    execution_id = _create_source_execution(client, db_session)
    payload = CreateCorrectionRequest(
        page_url="https://app.example.com/login",
        target_description="登录按钮",
        correction_type="css",
        correction_value="#login-btn",
        source_execution_id=execution_id,
        created_by=1,
    )

    def fake_flush() -> None:
        raise IntegrityError("INSERT", {}, Exception("duplicate active correction"))

    monkeypatch.setattr(db_session, "flush", fake_flush)

    with pytest.raises(CorrectionConflictError) as exc_info:
        create_correction(db_session, payload)

    assert str(exc_info.value) == "Another active correction already exists for the same page URL pattern and target description."


def test_patch_activate_correction_returns_conflict_when_other_active_record_exists(client, db_session) -> None:
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

    reactivate_response = client.patch(
        f"/api/v1/corrections/{first_response.json()['id']}",
        json={"is_active": True},
    )

    assert reactivate_response.status_code == 409
    assert reactivate_response.json() == {
        "detail": "Another active correction already exists for the same page URL pattern and target description."
    }

    active_records = client.get("/api/v1/corrections", params={"target_description": "order cta", "is_active": True})
    assert active_records.status_code == 200
    assert [record["id"] for record in active_records.json()] == [second_response.json()["id"]]


def test_list_correction_events_returns_created_and_runtime_events(client, db_session) -> None:
    execution_id = _create_source_execution(client, db_session)
    response = client.post(
        "/api/v1/corrections",
        json={
            "page_url": "https://app.example.com/login",
            "target_description": "登录按钮",
            "correction_type": "css",
            "correction_value": "#login-btn",
            "source_execution_id": execution_id,
            "created_by": 1,
        },
    )
    assert response.status_code == 201

    correction_id = response.json()["id"]
    store = SQLAlchemyCorrectionStore(db_session)
    store.record_success(correction_id, execution_id=execution_id)
    db_session.commit()

    events_response = client.get(f"/api/v1/corrections/{correction_id}/events")
    assert events_response.status_code == 200
    payload = events_response.json()
    assert [item["event_type"] for item in payload] == ["tier0_hit", "created"]
    assert payload[0]["execution_id"] == execution_id
    assert payload[0]["verified_count_after"] == 1
    assert payload[1]["execution_id"] == execution_id


def test_get_corrections_overview_returns_window_metrics_and_trend(client, db_session) -> None:
    execution_id = _create_source_execution(client, db_session)
    response = client.post(
        "/api/v1/corrections",
        json={
            "page_url": "https://app.example.com/login",
            "target_description": "登录按钮",
            "correction_type": "css",
            "correction_value": "#login-btn",
            "source_execution_id": execution_id,
            "created_by": 1,
        },
    )
    assert response.status_code == 201

    correction_id = response.json()["id"]
    store = SQLAlchemyCorrectionStore(db_session)
    store.record_success(correction_id, execution_id=execution_id)
    for _ in range(3):
        store.record_failure(correction_id, execution_id=execution_id)
    db_session.commit()

    overview_response = client.get("/api/v1/corrections/overview", params={"window_days": 7})
    assert overview_response.status_code == 200
    payload = overview_response.json()
    assert payload["total_count"] == 1
    assert payload["active_count"] == 0
    assert payload["inactive_count"] == 1
    assert payload["hit_count"] == 1
    assert payload["miss_count"] == 3
    assert payload["auto_deactivated_count"] == 1
    assert len(payload["trend_points"]) == 7
    assert payload["trend_points"][-1]["hit_count"] == 1
    assert payload["trend_points"][-1]["miss_count"] == 3


def test_batch_patch_correction_state_updates_multiple_records(client, db_session) -> None:
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
    second_response = client.post(
        "/api/v1/corrections",
        json={
            "page_url": "https://app.example.com/profile/123",
            "target_description": "Profile CTA",
            "correction_type": "test_id",
            "correction_value": "profile-cta",
            "source_execution_id": second_execution_id,
            "created_by": 1,
        },
    )

    bulk_response = client.patch(
        "/api/v1/corrections/bulk",
        json={
            "correction_ids": [first_response.json()["id"], second_response.json()["id"]],
            "is_active": False,
        },
    )

    assert bulk_response.status_code == 200
    assert [item["is_active"] for item in bulk_response.json()] == [False, False]

    first_events = client.get(f"/api/v1/corrections/{first_response.json()['id']}/events")
    assert first_events.status_code == 200
    assert [item["event_type"] for item in first_events.json()] == ["deactivated", "created"]


# --- correction_value format validation ---


def test_create_correction_rejects_xpath_value_for_css_type(client, db_session) -> None:
    execution_id = _create_source_execution(client, db_session)
    response = client.post(
        "/api/v1/corrections",
        json={
            "page_url": "https://app.example.com/login",
            "target_description": "login button",
            "correction_type": "css",
            "correction_value": "//button[@id='login']",
            "source_execution_id": execution_id,
            "created_by": 1,
        },
    )
    assert response.status_code == 422


def test_create_correction_accepts_valid_css_value(client, db_session) -> None:
    execution_id = _create_source_execution(client, db_session)
    response = client.post(
        "/api/v1/corrections",
        json={
            "page_url": "https://app.example.com/login",
            "target_description": "login button",
            "correction_type": "css",
            "correction_value": "#login-btn",
            "source_execution_id": execution_id,
            "created_by": 1,
        },
    )
    assert response.status_code == 201


def test_create_correction_rejects_css_value_for_xpath_type(client, db_session) -> None:
    execution_id = _create_source_execution(client, db_session)
    response = client.post(
        "/api/v1/corrections",
        json={
            "page_url": "https://app.example.com/login",
            "target_description": "login button",
            "correction_type": "xpath",
            "correction_value": "#login-btn",
            "source_execution_id": execution_id,
            "created_by": 1,
        },
    )
    assert response.status_code == 422


def test_create_correction_accepts_valid_xpath_value(client, db_session) -> None:
    execution_id = _create_source_execution(client, db_session)
    response = client.post(
        "/api/v1/corrections",
        json={
            "page_url": "https://app.example.com/login",
            "target_description": "login button xpath",
            "correction_type": "xpath",
            "correction_value": "//button[@id='login']",
            "source_execution_id": execution_id,
            "created_by": 1,
        },
    )
    assert response.status_code == 201


def test_create_correction_rejects_selector_syntax_for_test_id_type(client, db_session) -> None:
    execution_id = _create_source_execution(client, db_session)
    response = client.post(
        "/api/v1/corrections",
        json={
            "page_url": "https://app.example.com/login",
            "target_description": "login button test_id",
            "correction_type": "test_id",
            "correction_value": "#login-btn",
            "source_execution_id": execution_id,
            "created_by": 1,
        },
    )
    assert response.status_code == 422


def test_create_correction_accepts_valid_test_id_value(client, db_session) -> None:
    execution_id = _create_source_execution(client, db_session)
    response = client.post(
        "/api/v1/corrections",
        json={
            "page_url": "https://app.example.com/login",
            "target_description": "login button test_id ok",
            "correction_type": "test_id",
            "correction_value": "login-button",
            "source_execution_id": execution_id,
            "created_by": 1,
        },
    )
    assert response.status_code == 201


# --- batch activation conflict ---


def test_batch_activate_corrections_returns_conflict_when_same_key_active(client, db_session) -> None:
    first_execution_id = _create_source_execution(client, db_session)
    second_execution_id = _create_source_execution(client, db_session)

    first_response = client.post(
        "/api/v1/corrections",
        json={
            "page_url": "https://app.example.com/orders/123",
            "target_description": "Batch Conflict CTA",
            "correction_type": "css",
            "correction_value": "#batch-cta",
            "source_execution_id": first_execution_id,
            "created_by": 1,
        },
    )
    assert first_response.status_code == 201

    second_response = client.post(
        "/api/v1/corrections",
        json={
            "page_url": "https://app.example.com/orders/456",
            "target_description": "batch conflict cta",
            "correction_type": "test_id",
            "correction_value": "batch-cta",
            "source_execution_id": second_execution_id,
            "created_by": 1,
        },
    )
    assert second_response.status_code == 201

    first_id = first_response.json()["id"]
    client.patch(f"/api/v1/corrections/{first_id}", json={"is_active": False})

    bulk_response = client.patch(
        "/api/v1/corrections/bulk",
        json={
            "correction_ids": [first_id],
            "is_active": True,
        },
    )

    assert bulk_response.status_code == 409
