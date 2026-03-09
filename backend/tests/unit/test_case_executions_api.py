"""Tests for case execution endpoints."""

from __future__ import annotations

from pathlib import Path

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
    assert response.json()["case_name"] == "执行用例"
    assert response.json()["status"] == "passed"
    assert response.json()["report"]["steps"][0]["status"] == "passed"
    assert response.json()["report"]["steps"][0]["screenshot_url"] == "/artifacts/executions/1/step-01.png"

    detail = client.get("/api/v1/executions/1")
    assert detail.status_code == 200
    assert detail.json()["case_name"] == "执行用例"
    assert detail.json()["status"] == "passed"

    case_runs = client.get(f"/api/v1/cases/{create_response.json()['id']}/executions")
    assert case_runs.status_code == 200
    assert case_runs.json()[0]["id"] == 1
    assert case_runs.json()[0]["case_name"] == "执行用例"


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
    assert response.json()["case_name"] == "失败用例"
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


def test_list_executions_supports_filters_and_limit(client, monkeypatch) -> None:
    created_cases: list[int] = []
    for name in ["成功用例", "失败用例", "第二个成功用例"]:
        response = client.post(
            "/api/v1/cases",
            json={
                "project_id": 1,
                "actor_user_id": 1,
                "name": name,
                "steps": [{"action": "goto", "value": "/"}],
            },
        )
        created_cases.append(response.json()["id"])

    def fake_execute_case_with_playwright(*, case, execution_id: int, base_url: str | None):
        if case.name == "失败用例":
            raise RunnerExecutionError(
                "boom",
                step_results=[
                    StepExecutionEvidence(
                        step_index=0,
                        action="goto",
                        value="/",
                        status="failed",
                        error_message="boom",
                    )
                ],
            )
        return [
            StepExecutionEvidence(
                step_index=0,
                action="goto",
                value="/",
                status="passed",
                screenshot_path=f"artifacts/executions/{execution_id}/step-01.png",
            )
        ]

    monkeypatch.setattr(
        execution_service,
        "execute_case_with_playwright",
        fake_execute_case_with_playwright,
    )

    for case_id in created_cases:
        client.post(
            f"/api/v1/cases/{case_id}/execute",
            json={"actor_user_id": 1, "base_url": "http://example.com"},
        )

    all_runs = client.get("/api/v1/executions", params={"project_id": 1})
    assert all_runs.status_code == 200
    assert [item["case_name"] for item in all_runs.json()] == [
        "第二个成功用例",
        "失败用例",
        "成功用例",
    ]

    failed_runs = client.get("/api/v1/executions", params={"project_id": 1, "status": "failed"})
    assert failed_runs.status_code == 200
    assert len(failed_runs.json()) == 1
    assert failed_runs.json()[0]["case_name"] == "失败用例"

    limited_runs = client.get("/api/v1/executions", params={"project_id": 1, "limit": 2})
    assert limited_runs.status_code == 200
    assert len(limited_runs.json()) == 2
