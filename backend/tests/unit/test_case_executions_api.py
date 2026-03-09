"""Tests for case execution endpoints."""

from __future__ import annotations

import app.services.executions as execution_service
from app.schemas.executions import StepExecutionEvidence
from app.runners import RunnerExecutionError


def test_execute_case_success(client, monkeypatch) -> None:
    create_response = client.post(
        "/api/v1/cases",
        json={
            "project_id": 1,
            "actor_user_id": 1,
            "name": "执行用例",
            "steps": [{"action": "goto", "value": "/login"}],
        },
    )

    def fake_execute_case_with_playwright(*, case, execution_id: int, base_url: str | None):
        assert case.name == "执行用例"
        assert execution_id == 1
        assert base_url == "http://example.com"
        return [
            StepExecutionEvidence(
                step_index=0,
                action="goto",
                value="/login",
                status="passed",
                url="http://example.com/login",
                screenshot_path="artifacts/executions/1/step-01.png",
            )
        ]

    monkeypatch.setattr(
        execution_service,
        "execute_case_with_playwright",
        fake_execute_case_with_playwright,
    )

    response = client.post(
        f"/api/v1/cases/{create_response.json()['id']}/execute",
        json={"actor_user_id": 1, "base_url": "http://example.com"},
    )

    assert response.status_code == 200
    assert response.json()["status"] == "passed"
    assert response.json()["report"]["steps"][0]["status"] == "passed"

    detail = client.get("/api/v1/executions/1")
    assert detail.status_code == 200
    assert detail.json()["status"] == "passed"

    case_runs = client.get(f"/api/v1/cases/{create_response.json()['id']}/executions")
    assert case_runs.status_code == 200
    assert case_runs.json()[0]["id"] == 1


def test_execute_case_returns_failed_execution_when_runner_fails(client, monkeypatch) -> None:
    create_response = client.post(
        "/api/v1/cases",
        json={
            "project_id": 1,
            "actor_user_id": 1,
            "name": "失败用例",
            "steps": [{"action": "goto", "value": "/missing"}],
        },
    )

    def fake_execute_case_with_playwright(*, case, execution_id: int, base_url: str | None):
        raise RunnerExecutionError(
            "Relative goto step requires base_url or EXECUTION_BASE_URL.",
            step_results=[
                StepExecutionEvidence(
                    step_index=0,
                    action="goto",
                    value="/missing",
                    status="failed",
                    error_message="Relative goto step requires base_url or EXECUTION_BASE_URL.",
                )
            ],
        )

    monkeypatch.setattr(
        execution_service,
        "execute_case_with_playwright",
        fake_execute_case_with_playwright,
    )

    response = client.post(
        f"/api/v1/cases/{create_response.json()['id']}/execute",
        json={"actor_user_id": 1},
    )

    assert response.status_code == 200
    assert response.json()["status"] == "failed"
    assert response.json()["error_message"] == "Relative goto step requires base_url or EXECUTION_BASE_URL."
    assert response.json()["report"]["steps"][0]["status"] == "failed"


def test_execute_case_returns_not_found_for_unknown_case(client) -> None:
    response = client.post("/api/v1/cases/999/execute", json={"actor_user_id": 1})

    assert response.status_code == 404
    assert response.json() == {"detail": "Case 999 not found."}


def test_get_execution_returns_not_found_for_unknown_id(client) -> None:
    response = client.get("/api/v1/executions/999")

    assert response.status_code == 404
    assert response.json() == {"detail": "Execution not found."}
