"""Tests for case execution endpoints."""

from __future__ import annotations

from datetime import datetime, timedelta

import app.services.executions as execution_service
from app.models import TestCaseRun
from app.schemas.executions import (
    ConsoleEvent,
    DOMSummary,
    LocatorCandidateAttributes,
    LocatorCandidateEvidence,
    LocatorTrace,
    NetworkEvent,
    StepExecutionEvidence,
    ViewportSnapshot,
)
from app.runners import RunnerExecutionError


def test_execute_case_success(client, monkeypatch) -> None:
    create_response = client.post(
        "/api/v1/cases",
        json={
            "project_id": 1,
            "actor_user_id": 1,
            "name": "执行用例",
            "base_url": "https://case.example.com",
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
                duration_ms=42,
                url="http://example.com/login",
                page_title="登录页",
                viewport=ViewportSnapshot(width=1280, height=720),
                dom_summary=DOMSummary(
                    text_preview="登录页面 请输入账号密码",
                    button_count=1,
                    input_count=2,
                    link_count=1,
                ),
                console_events=[
                    ConsoleEvent(level="warning", text="Deprecated warning", source_url="http://example.com/app.js")
                ],
                network_events=[
                    NetworkEvent(
                        url="http://example.com/api/login",
                        method="POST",
                        status=500,
                        resource_type="xhr",
                    )
                ],
                locator_trace=LocatorTrace(
                    target="登录按钮",
                    match_strategy="button_role",
                    selection_reason="Selected highest-scoring candidate (108) with rules: exact-button-role-match, visible, enabled, has-preview-text.",
                    candidates=[
                        LocatorCandidateEvidence(
                            strategy="button_role",
                            preview_text="登录",
                            role="button",
                            attributes=LocatorCandidateAttributes(aria_label="登录按钮"),
                            score=108,
                            matched_rules=["exact-button-role-match", "visible", "enabled", "has-preview-text"],
                            rejected_reasons=[],
                            visible=True,
                            enabled=True,
                        )
                    ],
                    selected_candidate=LocatorCandidateEvidence(
                        strategy="button_role",
                        preview_text="登录",
                        role="button",
                        attributes=LocatorCandidateAttributes(aria_label="登录按钮"),
                        score=108,
                        matched_rules=["exact-button-role-match", "visible", "enabled", "has-preview-text"],
                        rejected_reasons=[],
                        visible=True,
                        enabled=True,
                    ),
                ),
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
    assert response.json()["report"]["steps"][0]["duration_ms"] == 42
    assert response.json()["report"]["steps"][0]["page_title"] == "登录页"
    assert response.json()["report"]["steps"][0]["locator_trace"]["match_strategy"] == "button_role"
    assert response.json()["report"]["steps"][0]["locator_trace"]["selection_reason"] is not None
    assert response.json()["report"]["steps"][0]["console_events"][0]["level"] == "warning"
    assert response.json()["report"]["steps"][0]["network_events"][0]["status"] == 500
    assert response.json()["report"]["steps"][0]["screenshot_url"] == "/artifacts/executions/1/step-01.png"
    assert response.json()["duration_ms"] is not None
    assert response.json()["total_steps"] == 1
    assert response.json()["failed_step_index"] is None
    assert response.json()["failure_category"] is None
    assert response.json()["failure_step_action"] is None
    assert response.json()["latest_url"] == "http://example.com/login"
    assert response.json()["latest_screenshot_url"] == "/artifacts/executions/1/step-01.png"

    detail = client.get("/api/v1/executions/1")
    assert detail.status_code == 200
    assert detail.json()["case_name"] == "执行用例"
    assert detail.json()["status"] == "passed"

    case_runs = client.get(f"/api/v1/cases/{create_response.json()['id']}/executions")
    assert case_runs.status_code == 200
    assert case_runs.json()[0]["id"] == 1
    assert case_runs.json()[0]["case_name"] == "执行用例"


def test_execute_case_uses_case_base_url_when_request_does_not_override(client, monkeypatch) -> None:
    create_response = client.post(
        "/api/v1/cases",
        json={
            "project_id": 1,
            "actor_user_id": 1,
            "name": "默认地址用例",
            "base_url": "https://case.example.com",
            "steps": [{"action": "goto", "value": "/from-case"}],
        },
    )

    def fake_execute_case_with_playwright(*, case, execution_id: int, base_url: str | None):
        assert case.base_url == "https://case.example.com"
        assert execution_id == 1
        assert base_url == "https://case.example.com"
        return [
            StepExecutionEvidence(
                step_index=0,
                action="goto",
                value="/from-case",
                status="passed",
            )
        ]

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
    assert response.json()["status"] == "passed"


def test_execute_case_fails_early_when_relative_goto_has_no_case_base_url(client, monkeypatch) -> None:
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
        raise AssertionError("runner should not be called when case base_url is missing")

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
    assert response.json()["error_message"] == "Relative goto step requires case.base_url or execution request base_url."
    assert response.json()["report"]["steps"][0]["status"] == "failed"
    assert response.json()["report"]["steps"][0]["locator_trace"] is None
    assert response.json()["failed_step_index"] == 0
    assert response.json()["failure_category"] == "configuration"
    assert response.json()["failure_step_action"] == "goto"


def test_execute_case_returns_not_found_for_unknown_case(client) -> None:
    response = client.post("/api/v1/cases/999/execute", json={"actor_user_id": 1})

    assert response.status_code == 404
    assert response.json() == {"detail": "Case 999 not found."}


def test_get_execution_returns_not_found_for_unknown_id(client) -> None:
    response = client.get("/api/v1/executions/999")

    assert response.status_code == 404
    assert response.json() == {"detail": "Execution not found."}


def test_list_executions_supports_filters_limit_offset_and_case_id(client, monkeypatch) -> None:
    created_cases: list[int] = []
    for name in ["成功用例", "失败用例", "第二个成功用例"]:
        response = client.post(
            "/api/v1/cases",
            json={
                "project_id": 1,
                "actor_user_id": 1,
                "name": name,
                "base_url": "https://example.com",
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
    assert all_runs.json()[0]["total_steps"] == 1
    assert all_runs.json()[0]["latest_url"] is None
    assert all_runs.json()[0]["latest_screenshot_url"] == "/artifacts/executions/3/step-01.png"

    failed_runs = client.get("/api/v1/executions", params={"project_id": 1, "status": "failed"})
    assert failed_runs.status_code == 200
    assert len(failed_runs.json()) == 1
    assert failed_runs.json()[0]["case_name"] == "失败用例"
    assert failed_runs.json()[0]["failed_step_index"] == 0
    assert failed_runs.json()[0]["failure_category"] == "navigation"
    assert failed_runs.json()[0]["failure_step_action"] == "goto"

    case_runs = client.get("/api/v1/executions", params={"project_id": 1, "case_id": created_cases[0]})
    assert case_runs.status_code == 200
    assert len(case_runs.json()) == 1
    assert case_runs.json()[0]["case_id"] == created_cases[0]

    failure_category_runs = client.get(
        "/api/v1/executions",
        params={"project_id": 1, "failure_category": "navigation"},
    )
    assert failure_category_runs.status_code == 200
    assert len(failure_category_runs.json()) == 1
    assert failure_category_runs.json()[0]["case_name"] == "失败用例"

    limited_runs = client.get("/api/v1/executions", params={"project_id": 1, "limit": 2})
    assert limited_runs.status_code == 200
    assert len(limited_runs.json()) == 2

    offset_runs = client.get("/api/v1/executions", params={"project_id": 1, "limit": 1, "offset": 1})
    assert offset_runs.status_code == 200
    assert [item["case_name"] for item in offset_runs.json()] == ["失败用例"]


def test_get_execution_detail_compatible_with_legacy_report_payload(client, db_session) -> None:
    create_response = client.post(
        "/api/v1/cases",
        json={
            "project_id": 1,
            "actor_user_id": 1,
            "name": "旧报告用例",
            "base_url": "https://legacy.example.com",
            "steps": [{"action": "goto", "value": "/legacy"}],
        },
    )
    db_session.add(
        TestCaseRun(
            id=1,
            case_id=create_response.json()["id"],
            project_id=1,
            triggered_by=1,
            status="failed",
            error_message="legacy boom",
            report={
                "status": "failed",
                "steps": [
                    {
                        "step_index": 0,
                        "action": "goto",
                        "value": "/legacy",
                        "status": "failed",
                        "screenshot_path": "artifacts/executions/1/step-01.png",
                        "error_message": "legacy boom",
                    }
                ],
            },
        )
    )
    db_session.commit()

    response = client.get("/api/v1/executions/1")

    assert response.status_code == 200
    assert response.json()["total_steps"] == 1
    assert response.json()["failed_step_index"] == 0
    assert response.json()["latest_screenshot_url"] == "/artifacts/executions/1/step-01.png"
    assert response.json()["failure_category"] == "navigation"
    assert response.json()["failure_step_action"] == "goto"
    assert response.json()["report"]["steps"][0]["locator_trace"] is None
    assert response.json()["report"]["steps"][0]["console_events"] == []


def test_get_executions_overview_returns_zero_counts_when_no_runs_exist(client) -> None:
    response = client.get("/api/v1/executions/overview", params={"project_id": 1})

    assert response.status_code == 200
    assert response.json() == {
        "total_count": 0,
        "passed_count": 0,
        "failed_count": 0,
        "running_count": 0,
        "pass_rate": 0.0,
        "avg_duration_ms": 0,
        "latest_failed_runs": [],
        "failure_categories": [
            {"category": "configuration", "count": 0},
            {"category": "locator", "count": 0},
            {"category": "assertion", "count": 0},
            {"category": "navigation", "count": 0},
            {"category": "network", "count": 0},
            {"category": "runner", "count": 0},
        ],
    }


def test_get_executions_overview_aggregates_counts_categories_and_recent_failures(client, db_session) -> None:
    create_response = client.post(
        "/api/v1/cases",
        json={
            "project_id": 1,
            "actor_user_id": 1,
            "name": "聚合用例",
            "base_url": "https://example.com",
            "steps": [{"action": "goto", "value": "/"}],
        },
    )
    case_id = create_response.json()["id"]
    started_at = datetime(2026, 3, 10, 12, 0, 0)
    reports = [
        (
            1,
            "failed",
            "Relative goto step requires case.base_url or execution request base_url.",
            {
                "status": "failed",
                "steps": [
                    {
                        "step_index": 0,
                        "action": "goto",
                        "value": "/missing",
                        "status": "failed",
                        "error_message": "Relative goto step requires case.base_url or execution request base_url.",
                    }
                ],
            },
            started_at,
            started_at + timedelta(milliseconds=100),
        ),
        (
            2,
            "failed",
            "locator boom",
            {
                "status": "failed",
                "steps": [
                    {
                        "step_index": 0,
                        "action": "click",
                        "target": "登录按钮",
                        "status": "failed",
                        "url": "https://example.com/login",
                        "locator_trace": {
                            "target": "登录按钮",
                            "failure_reason": "No visible candidate matched target.",
                            "candidates": [],
                        },
                        "error_message": "locator boom",
                    }
                ],
            },
            started_at + timedelta(minutes=1),
            started_at + timedelta(minutes=1, milliseconds=200),
        ),
        (
            3,
            "failed",
            "assert boom",
            {
                "status": "failed",
                "steps": [
                    {
                        "step_index": 0,
                        "action": "assert_text",
                        "target": "欢迎语",
                        "value": "欢迎回来",
                        "status": "failed",
                        "url": "https://example.com/dashboard",
                        "error_message": "assert boom",
                    }
                ],
            },
            started_at + timedelta(minutes=2),
            started_at + timedelta(minutes=2, milliseconds=300),
        ),
        (
            4,
            "failed",
            "navigation boom",
            {
                "status": "failed",
                "steps": [
                    {
                        "step_index": 0,
                        "action": "goto",
                        "value": "/dashboard",
                        "status": "failed",
                        "url": "https://example.com/dashboard",
                        "error_message": "navigation boom",
                    }
                ],
            },
            started_at + timedelta(minutes=3),
            started_at + timedelta(minutes=3, milliseconds=400),
        ),
        (
            5,
            "failed",
            "network boom",
            {
                "status": "failed",
                "steps": [
                    {
                        "step_index": 0,
                        "action": "click",
                        "target": "提交",
                        "status": "failed",
                        "url": "https://example.com/submit",
                        "network_events": [
                            {
                                "url": "https://example.com/api/submit",
                                "method": "POST",
                                "status": 500,
                                "resource_type": "xhr",
                            }
                        ],
                        "error_message": "network boom",
                    }
                ],
            },
            started_at + timedelta(minutes=4),
            started_at + timedelta(minutes=4, milliseconds=500),
        ),
        (
            6,
            "failed",
            "runner boom",
            {
                "status": "failed",
                "steps": [
                    {
                        "step_index": 0,
                        "action": "click",
                        "target": "保存按钮",
                        "status": "failed",
                        "url": "https://example.com/save",
                        "error_message": "runner boom",
                    }
                ],
            },
            started_at + timedelta(minutes=5),
            started_at + timedelta(minutes=5, milliseconds=600),
        ),
        (
            7,
            "passed",
            None,
            {
                "status": "passed",
                "steps": [
                    {
                        "step_index": 0,
                        "action": "goto",
                        "value": "/",
                        "status": "passed",
                        "url": "https://example.com/",
                    }
                ],
            },
            started_at + timedelta(minutes=6),
            started_at + timedelta(minutes=6, milliseconds=700),
        ),
        (
            8,
            "running",
            None,
            None,
            started_at + timedelta(minutes=7),
            None,
        ),
    ]

    db_session.add_all(
        [
            TestCaseRun(
                id=run_id,
                case_id=case_id,
                project_id=1,
                triggered_by=1,
                status=status,
                error_message=error_message,
                report=report,
                started_at=run_started_at,
                finished_at=finished_at,
            )
            for run_id, status, error_message, report, run_started_at, finished_at in reports
        ]
    )
    db_session.commit()

    response = client.get("/api/v1/executions/overview", params={"project_id": 1, "case_id": case_id})

    assert response.status_code == 200
    payload = response.json()
    assert payload["total_count"] == 8
    assert payload["passed_count"] == 1
    assert payload["failed_count"] == 6
    assert payload["running_count"] == 1
    assert payload["pass_rate"] == 0.1429
    assert payload["avg_duration_ms"] == 400
    assert [item["category"] for item in payload["failure_categories"]] == [
        "configuration",
        "locator",
        "assertion",
        "navigation",
        "network",
        "runner",
    ]
    assert [item["count"] for item in payload["failure_categories"]] == [1, 1, 1, 1, 1, 1]
    assert [item["id"] for item in payload["latest_failed_runs"]] == [6, 5, 4, 3, 2]
    assert payload["latest_failed_runs"][0]["failure_category"] == "runner"
    assert payload["latest_failed_runs"][1]["failure_category"] == "network"
    assert payload["latest_failed_runs"][2]["failure_category"] == "navigation"
    assert payload["latest_failed_runs"][3]["failure_category"] == "assertion"
    assert payload["latest_failed_runs"][4]["failure_category"] == "locator"
    assert payload["latest_failed_runs"][0]["failure_step_action"] == "click"
    assert payload["latest_failed_runs"][0]["latest_url"] == "https://example.com/save"
