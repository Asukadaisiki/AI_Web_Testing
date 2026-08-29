"""Tests for case execution endpoints."""

from __future__ import annotations

from datetime import UTC, datetime, timedelta

import app.services.executions as execution_service
from app.models import Project, ProjectMember, TestCaseRun
from app.schemas.executions import (
    AILocateCandidate,
    ConsoleEvent,
    DOMElementSnapshot,
    DOMSummary,
    InterventionRequest,
    LocatorCandidateAttributes,
    LocatorCandidateEvidence,
    LocatorTrace,
    NetworkEvent,
    StepExecutionEvidence,
    ViewportSnapshot,
)
from app.runners import RunnerExecutionError, RunnerInterventionError
from app.runners.playwright_runner import StepStreamEvent
from app.schemas.executions import CaseExecutionRequest


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

    def fake_execute_case_with_playwright_streaming(*, case, execution_id: int, base_url: str | None, cancel_event=None, correction_store=None, input_values=None):
        yield from ()
        assert case.name == "执行用例"
        assert execution_id == 1
        assert base_url == "http://example.com"
        assert correction_store is not None
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
        "execute_case_with_playwright_streaming",
        fake_execute_case_with_playwright_streaming,
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

    def fake_execute_case_with_playwright_streaming(*, case, execution_id: int, base_url: str | None, cancel_event=None, correction_store=None, input_values=None):
        yield from ()
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
        "execute_case_with_playwright_streaming",
        fake_execute_case_with_playwright_streaming,
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

    def fake_execute_case_with_playwright_streaming(*, case, execution_id: int, base_url: str | None, cancel_event=None, correction_store=None, input_values=None):
        yield from ()
        raise AssertionError("runner should not be called when case base_url is missing")

    monkeypatch.setattr(
        execution_service,
        "execute_case_with_playwright_streaming",
        fake_execute_case_with_playwright_streaming,
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


def test_execute_case_marks_needs_intervention_when_all_locator_tiers_fail(client, monkeypatch) -> None:
    create_response = client.post(
        "/api/v1/cases",
        json={
            "project_id": 1,
            "actor_user_id": 1,
            "name": "需要人工干预用例",
            "base_url": "https://case.example.com",
            "steps": [{"action": "click", "target": "登录按钮"}],
        },
    )

    def fake_execute_case_with_playwright_streaming(*, case, execution_id: int, base_url: str | None, cancel_event=None, correction_store=None, input_values=None):
        yield from ()
        raise RunnerInterventionError(
            "All locate tiers failed for target: 登录按钮",
            step_results=[
                StepExecutionEvidence(
                    step_index=0,
                    action="click",
                    target="登录按钮",
                    status="failed",
                    error_message="All locate tiers failed for target: 登录按钮",
                    locator_trace=LocatorTrace(
                        target="登录按钮",
                        candidates=[],
                        failure_reason="No locator candidates matched target.",
                    ),
                    intervention_request=InterventionRequest(
                        screenshot_url="/artifacts/executions/1/step-01.png",
                        page_url="https://case.example.com/login",
                        target_description="登录按钮",
                        dom_snapshot=[
                            DOMElementSnapshot(
                                tag="button",
                                text="登录",
                                role="button",
                                css_selector="#login-btn",
                                xpath="/html/body/button[1]",
                                visible=True,
                                enabled=True,
                            )
                        ],
                        ai_candidate=AILocateCandidate(
                            center=[320, 160],
                            bbox=[280, 120, 360, 200],
                            confidence=0.7,
                            raw_response='{"bbox":[280,120,360,200]}',
                        ),
                    ),
                    screenshot_path="artifacts/executions/1/step-01.png",
                )
            ],
        )

    monkeypatch.setattr(
        execution_service,
        "execute_case_with_playwright_streaming",
        fake_execute_case_with_playwright_streaming,
    )

    response = client.post(
        f"/api/v1/cases/{create_response.json()['id']}/execute",
        json={"actor_user_id": 1},
    )

    assert response.status_code == 200
    assert response.json()["status"] == "needs_intervention"
    assert response.json()["failure_category"] == "locator"
    assert response.json()["report"]["status"] == "failed"
    assert response.json()["report"]["steps"][0]["intervention_request"]["page_url"] == "https://case.example.com/login"
    assert response.json()["report"]["steps"][0]["intervention_request"]["dom_snapshot"][0]["css_selector"] == "#login-btn"


def test_get_execution_returns_not_found_for_unknown_id(client) -> None:
    response = client.get("/api/v1/executions/999")

    assert response.status_code == 404
    assert response.json() == {"detail": "Execution not found."}


def test_delete_execution_removes_record_and_returns_204(client, db_session) -> None:
    create_response = client.post(
        "/api/v1/cases",
        json={
            "project_id": 1,
            "actor_user_id": 1,
            "name": "待删除用例",
            "base_url": "https://example.com",
            "steps": [{"action": "goto", "value": "/"}],
        },
    )
    db_session.add(
        TestCaseRun(
            id=100,
            case_id=create_response.json()["id"],
            project_id=1,
            triggered_by=1,
            status="failed",
            error_message="to be deleted",
            report={"status": "failed", "steps": []},
        )
    )
    db_session.commit()

    detail_before = client.get("/api/v1/executions/100")
    assert detail_before.status_code == 200

    delete_response = client.delete("/api/v1/executions/100")
    assert delete_response.status_code == 204

    detail_after = client.get("/api/v1/executions/100")
    assert detail_after.status_code == 404


def test_delete_execution_returns_404_for_unknown_id(client) -> None:
    response = client.delete("/api/v1/executions/999")
    assert response.status_code == 404
    assert response.json() == {"detail": "Execution 999 not found."}


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

    def fake_execute_case_with_playwright_streaming(*, case, execution_id: int, base_url: str | None, cancel_event=None, correction_store=None, input_values=None):
        yield from ()
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
        "execute_case_with_playwright_streaming",
        fake_execute_case_with_playwright_streaming,
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
        "scope_type": "project",
        "scope_project_id": 1,
        "scope_case_id": None,
        "total_count": 0,
        "passed_count": 0,
        "failed_count": 0,
        "running_count": 0,
        "auto_completed_count": 0,
        "intervention_count": 0,
        "pass_rate": 0.0,
        "automation_rate": 0.0,
        "intervention_rate": 0.0,
        "avg_duration_ms": 0,
        "current_window_range": None,
        "previous_window_range": None,
        "previous_window_stats": {
            "total_count": 0,
            "passed_count": 0,
            "failed_count": 0,
            "running_count": 0,
            "pass_rate": 0.0,
            "avg_duration_ms": 0,
        },
        "window_comparison": {
            "total_count_delta": 0,
            "passed_count_delta": 0,
            "failed_count_delta": 0,
            "running_count_delta": 0,
            "pass_rate_delta": 0.0,
            "avg_duration_ms_delta": 0,
        },
        "latest_failed_runs": [],
        "latest_intervention_runs": [],
        "failure_categories": [
            {"category": "configuration", "count": 0},
            {"category": "locator", "count": 0},
            {"category": "assertion", "count": 0},
            {"category": "navigation", "count": 0},
            {"category": "network", "count": 0},
            {"category": "runner", "count": 0},
        ],
        "trend_points": [],
        "failure_step_actions": [],
        "top_failed_cases": [],
        "failure_root_causes": [],
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
    assert payload["current_window_range"] is None
    assert payload["previous_window_range"] is None
    assert payload["previous_window_stats"] == {
        "total_count": 0,
        "passed_count": 0,
        "failed_count": 0,
        "running_count": 0,
        "pass_rate": 0.0,
        "avg_duration_ms": 0,
    }
    assert payload["window_comparison"] == {
        "total_count_delta": 0,
        "passed_count_delta": 0,
        "failed_count_delta": 0,
        "running_count_delta": 0,
        "pass_rate_delta": 0.0,
        "avg_duration_ms_delta": 0,
    }
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
    assert payload["trend_points"] == [
        {
            "date": "2026-03-10",
            "total_count": 8,
            "passed_count": 1,
            "failed_count": 6,
            "auto_completed_count": 7,
            "intervention_count": 0,
            "pass_rate": 0.1429,
            "avg_duration_ms": 400,
        }
    ]
    assert payload["failure_step_actions"] == [
        {"action": "click", "count": 3},
        {"action": "goto", "count": 2},
        {"action": "assert_text", "count": 1},
    ]
    assert payload["top_failed_cases"] == [
        {
            "case_id": case_id,
            "case_name": "聚合用例",
            "failure_count": 6,
            "latest_execution_id": 6,
            "latest_failure_category": "runner",
        }
    ]
    assert [item["latest_execution_id"] for item in payload["failure_root_causes"]] == [6, 5, 4, 3, 2, 1]
    assert [item["title"] for item in payload["failure_root_causes"]] == [
        "runner boom",
        "network boom",
        "navigation boom",
        "assert boom",
        "locator boom",
        "Relative goto step requires case.base_url or execution request base_url.",
    ]
    assert all(len(item["fingerprint"]) == 16 for item in payload["failure_root_causes"])
    assert all(item["count"] == 1 for item in payload["failure_root_causes"])
    assert all(item["affected_case_count"] == 1 for item in payload["failure_root_causes"])


def test_get_executions_overview_supports_failure_fingerprint_filter(client, db_session) -> None:
    case_ids: list[int] = []
    for name in ["共享根因 A", "共享根因 B", "不同根因"]:
        response = client.post(
            "/api/v1/cases",
            json={
                "project_id": 1,
                "actor_user_id": 1,
                "name": name,
                "base_url": "https://example.com",
                "steps": [{"action": "click", "target": "提交"}],
            },
        )
        case_ids.append(response.json()["id"])

    now = datetime.now(UTC).replace(tzinfo=None, hour=9, minute=0, second=0, microsecond=0)
    db_session.add_all(
        [
            TestCaseRun(
                id=1,
                case_id=case_ids[0],
                project_id=1,
                triggered_by=1,
                status="failed",
                error_message="shared runner boom",
                report={
                    "status": "failed",
                    "steps": [
                        {
                            "step_index": 0,
                            "action": "click",
                            "target": "提交",
                            "status": "failed",
                            "error_message": "shared runner boom",
                        }
                    ],
                },
                started_at=now - timedelta(hours=1),
                finished_at=now - timedelta(hours=1) + timedelta(milliseconds=200),
            ),
            TestCaseRun(
                id=2,
                case_id=case_ids[1],
                project_id=1,
                triggered_by=1,
                status="failed",
                error_message="shared runner boom",
                report={
                    "status": "failed",
                    "steps": [
                        {
                            "step_index": 0,
                            "action": "click",
                            "target": "提交",
                            "status": "failed",
                            "error_message": "shared runner boom",
                        }
                    ],
                },
                started_at=now,
                finished_at=now + timedelta(milliseconds=220),
            ),
            TestCaseRun(
                id=3,
                case_id=case_ids[2],
                project_id=1,
                triggered_by=1,
                status="failed",
                error_message="other runner boom",
                report={
                    "status": "failed",
                    "steps": [
                        {
                            "step_index": 0,
                            "action": "click",
                            "target": "提交",
                            "status": "failed",
                            "error_message": "other runner boom",
                        }
                    ],
                },
                started_at=now + timedelta(minutes=1),
                finished_at=now + timedelta(minutes=1, milliseconds=240),
            ),
        ]
    )
    db_session.commit()

    overview_response = client.get("/api/v1/executions/overview", params={"project_id": 1, "window_days": 7})

    assert overview_response.status_code == 200
    overview_payload = overview_response.json()
    assert overview_payload["failure_root_causes"][0]["title"] == "shared runner boom"
    assert overview_payload["failure_root_causes"][0]["count"] == 2
    assert overview_payload["failure_root_causes"][0]["affected_case_count"] == 2
    assert overview_payload["failure_root_causes"][0]["latest_execution_id"] == 2
    assert overview_payload["failure_root_causes"][1]["title"] == "other runner boom"
    assert overview_payload["failure_root_causes"][1]["count"] == 1

    shared_fingerprint = overview_payload["failure_root_causes"][0]["fingerprint"]
    filtered_response = client.get(
        "/api/v1/executions/overview",
        params={"project_id": 1, "window_days": 7, "failure_fingerprint": shared_fingerprint},
    )

    assert filtered_response.status_code == 200
    filtered_payload = filtered_response.json()
    assert filtered_payload["total_count"] == 2
    assert filtered_payload["failed_count"] == 2
    assert filtered_payload["failure_root_causes"] == [overview_payload["failure_root_causes"][0]]

    list_response = client.get(
        "/api/v1/executions",
        params={"project_id": 1, "failure_fingerprint": shared_fingerprint},
    )
    assert list_response.status_code == 200
    assert [item["id"] for item in list_response.json()] == [2, 1]


def test_get_executions_overview_supports_window_days_trends_and_top_failed_cases(client, db_session) -> None:
    case_ids: list[int] = []
    for name in ["近七天高频失败", "近七天较新失败", "过期失败"]:
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
        case_ids.append(response.json()["id"])

    now = datetime.now(UTC).replace(tzinfo=None, hour=10, minute=0, second=0, microsecond=0)
    runs = [
        TestCaseRun(
            id=1,
            case_id=case_ids[0],
            project_id=1,
            triggered_by=1,
            status="failed",
            error_message="click boom",
            report={
                "status": "failed",
                "steps": [
                    {
                        "step_index": 0,
                        "action": "click",
                        "target": "登录按钮",
                        "status": "failed",
                        "error_message": "click boom",
                    }
                ],
            },
            started_at=now - timedelta(days=1),
            finished_at=now - timedelta(days=1) + timedelta(milliseconds=100),
        ),
        TestCaseRun(
            id=2,
            case_id=case_ids[0],
            project_id=1,
            triggered_by=1,
            status="failed",
            error_message="goto boom",
            report={
                "status": "failed",
                "steps": [
                    {
                        "step_index": 0,
                        "action": "goto",
                        "value": "/dashboard",
                        "status": "failed",
                        "error_message": "goto boom",
                    }
                ],
            },
            started_at=now - timedelta(days=3),
            finished_at=now - timedelta(days=3) + timedelta(milliseconds=200),
        ),
        TestCaseRun(
            id=3,
            case_id=case_ids[1],
            project_id=1,
            triggered_by=1,
            status="failed",
            error_message="assert boom",
            report={
                "status": "failed",
                "steps": [
                    {
                        "step_index": 0,
                        "action": "assert_text",
                        "target": "欢迎语",
                        "value": "欢迎回来",
                        "status": "failed",
                        "error_message": "assert boom",
                    }
                ],
            },
            started_at=now - timedelta(days=1) + timedelta(hours=1),
            finished_at=now - timedelta(days=1) + timedelta(hours=1, milliseconds=300),
        ),
        TestCaseRun(
            id=4,
            case_id=case_ids[1],
            project_id=1,
            triggered_by=1,
            status="passed",
            error_message=None,
            report={
                "status": "passed",
                "steps": [
                    {
                        "step_index": 0,
                        "action": "goto",
                        "value": "/",
                        "status": "passed",
                    }
                ],
            },
            started_at=now,
            finished_at=now + timedelta(milliseconds=400),
        ),
        TestCaseRun(
            id=5,
            case_id=case_ids[2],
            project_id=1,
            triggered_by=1,
            status="failed",
            error_message="expired boom",
            report={
                "status": "failed",
                "steps": [
                    {
                        "step_index": 0,
                        "action": "click",
                        "target": "旧按钮",
                        "status": "failed",
                        "error_message": "expired boom",
                    }
                ],
            },
            started_at=now - timedelta(days=10),
            finished_at=now - timedelta(days=10) + timedelta(milliseconds=500),
        ),
    ]
    db_session.add_all(runs)
    db_session.commit()

    response = client.get("/api/v1/executions/overview", params={"project_id": 1, "window_days": 7})

    assert response.status_code == 200
    payload = response.json()
    assert payload["total_count"] == 4
    assert payload["passed_count"] == 1
    assert payload["failed_count"] == 3
    assert payload["running_count"] == 0
    assert payload["current_window_range"] == {
        "start_date": (now.date() - timedelta(days=6)).isoformat(),
        "end_date": now.date().isoformat(),
    }
    assert payload["previous_window_range"] == {
        "start_date": (now.date() - timedelta(days=13)).isoformat(),
        "end_date": (now.date() - timedelta(days=7)).isoformat(),
    }
    assert payload["previous_window_stats"] == {
        "total_count": 1,
        "passed_count": 0,
        "failed_count": 1,
        "running_count": 0,
        "pass_rate": 0.0,
        "avg_duration_ms": 500,
    }
    assert payload["window_comparison"] == {
        "total_count_delta": 3,
        "passed_count_delta": 1,
        "failed_count_delta": 2,
        "running_count_delta": 0,
        "pass_rate_delta": 0.25,
        "avg_duration_ms_delta": -250,
    }
    assert len(payload["trend_points"]) == 7
    assert payload["trend_points"][-1]["date"] == now.date().isoformat()
    assert payload["trend_points"][-1]["passed_count"] == 1
    assert payload["trend_points"][-2]["failed_count"] == 2
    assert payload["trend_points"][-4]["failed_count"] == 1
    assert payload["failure_step_actions"] == [
        {"action": "assert_text", "count": 1},
        {"action": "click", "count": 1},
        {"action": "goto", "count": 1},
    ]
    assert payload["top_failed_cases"] == [
        {
            "case_id": case_ids[0],
            "case_name": "近七天高频失败",
            "failure_count": 2,
            "latest_execution_id": 1,
            "latest_failure_category": "runner",
        },
        {
            "case_id": case_ids[1],
            "case_name": "近七天较新失败",
            "failure_count": 1,
            "latest_execution_id": 3,
            "latest_failure_category": "assertion",
        },
    ]
    assert [item["title"] for item in payload["failure_root_causes"]] == [
        "assert boom",
        "goto boom",
        "click boom",
    ]
    assert [item["count"] for item in payload["failure_root_causes"]] == [1, 1, 1]

    for days in [14, 30]:
        extra_response = client.get("/api/v1/executions/overview", params={"project_id": 1, "window_days": days})
        assert extra_response.status_code == 200
        assert len(extra_response.json()["trend_points"]) == days


def test_list_executions_supports_window_days_and_matches_overview_filters(client, db_session) -> None:
    case_ids: list[int] = []
    for name in ["窗口内共享根因", "窗口外共享根因", "不同根因通过用例"]:
        response = client.post(
            "/api/v1/cases",
            json={
                "project_id": 1,
                "actor_user_id": 1,
                "name": name,
                "base_url": "https://example.com",
                "steps": [{"action": "click", "target": "提交按钮"}],
            },
        )
        case_ids.append(response.json()["id"])

    now = datetime.now(UTC).replace(tzinfo=None, hour=14, minute=0, second=0, microsecond=0)
    db_session.add_all(
        [
            TestCaseRun(
                id=1,
                case_id=case_ids[0],
                project_id=1,
                triggered_by=1,
                status="failed",
                error_message="shared runner boom",
                report={
                    "status": "failed",
                    "steps": [
                        {
                            "step_index": 0,
                            "action": "click",
                            "target": "提交按钮",
                            "status": "failed",
                            "error_message": "shared runner boom",
                        }
                    ],
                },
                started_at=now - timedelta(days=1),
                finished_at=now - timedelta(days=1) + timedelta(milliseconds=180),
            ),
            TestCaseRun(
                id=2,
                case_id=case_ids[1],
                project_id=1,
                triggered_by=1,
                status="failed",
                error_message="shared runner boom",
                report={
                    "status": "failed",
                    "steps": [
                        {
                            "step_index": 0,
                            "action": "click",
                            "target": "提交按钮",
                            "status": "failed",
                            "error_message": "shared runner boom",
                        }
                    ],
                },
                started_at=now - timedelta(days=10),
                finished_at=now - timedelta(days=10) + timedelta(milliseconds=220),
            ),
            TestCaseRun(
                id=3,
                case_id=case_ids[0],
                project_id=1,
                triggered_by=1,
                status="passed",
                error_message=None,
                report={
                    "status": "passed",
                    "steps": [
                        {
                            "step_index": 0,
                            "action": "click",
                            "target": "提交按钮",
                            "status": "passed",
                        }
                    ],
                },
                started_at=now - timedelta(days=2),
                finished_at=now - timedelta(days=2) + timedelta(milliseconds=160),
            ),
            TestCaseRun(
                id=4,
                case_id=case_ids[2],
                project_id=1,
                triggered_by=1,
                status="failed",
                error_message="assert boom",
                report={
                    "status": "failed",
                    "steps": [
                        {
                            "step_index": 0,
                            "action": "assert_text",
                            "target": "结果文案",
                            "value": "成功",
                            "status": "failed",
                            "error_message": "assert boom",
                        }
                    ],
                },
                started_at=now,
                finished_at=now + timedelta(milliseconds=200),
            ),
        ]
    )
    db_session.commit()

    overview_response = client.get("/api/v1/executions/overview", params={"project_id": 1, "window_days": 7})

    assert overview_response.status_code == 200
    overview_payload = overview_response.json()
    assert overview_payload["total_count"] == 3
    assert overview_payload["failed_count"] == 2
    assert [item["title"] for item in overview_payload["failure_root_causes"]] == [
        "assert boom",
        "shared runner boom",
    ]

    shared_fingerprint = next(
        item["fingerprint"]
        for item in overview_payload["failure_root_causes"]
        if item["title"] == "shared runner boom"
    )

    filtered_response = client.get(
        "/api/v1/executions",
        params={
            "project_id": 1,
            "window_days": 7,
            "status": "failed",
            "failure_fingerprint": shared_fingerprint,
        },
    )

    assert filtered_response.status_code == 200
    assert [item["id"] for item in filtered_response.json()] == [1]

    case_response = client.get(
        "/api/v1/executions",
        params={
            "project_id": 1,
            "window_days": 7,
            "case_id": case_ids[0],
        },
    )
    assert case_response.status_code == 200
    assert [item["id"] for item in case_response.json()] == [1, 3]

    invalid_response = client.get("/api/v1/executions", params={"project_id": 1, "window_days": 5})
    assert invalid_response.status_code == 422
    assert invalid_response.json() == {"detail": "window_days must be one of: 7, 14, 30."}


def test_get_executions_overview_supports_scope_filters_and_automation_metrics(client, db_session) -> None:
    db_session.add(Project(id=2, name="Cross Project", description="secondary project"))
    db_session.add(ProjectMember(project_id=2, user_id=1, role="owner"))
    db_session.commit()

    case_response_one = client.post(
        "/api/v1/cases",
        json={
            "project_id": 1,
            "actor_user_id": 1,
            "name": "项目一通过用例",
            "base_url": "https://example.com",
            "steps": [{"action": "goto", "value": "/pass"}],
        },
    )
    case_response_two = client.post(
        "/api/v1/cases",
        json={
            "project_id": 1,
            "actor_user_id": 1,
            "name": "项目一失败用例",
            "base_url": "https://example.com",
            "steps": [{"action": "click", "target": "提交"}],
        },
    )
    case_response_three = client.post(
        "/api/v1/cases",
        json={
            "project_id": 2,
            "actor_user_id": 1,
            "name": "项目二人工介入用例",
            "base_url": "https://example.com",
            "steps": [{"action": "click", "target": "登录按钮"}],
        },
    )

    now = datetime.now(UTC).replace(tzinfo=None, hour=10, minute=0, second=0, microsecond=0)
    db_session.add_all(
        [
            TestCaseRun(
                id=1,
                case_id=case_response_one.json()["id"],
                project_id=1,
                triggered_by=1,
                status="passed",
                error_message=None,
                report={
                    "status": "passed",
                    "steps": [
                        {
                            "step_index": 0,
                            "action": "goto",
                            "value": "/pass",
                            "status": "passed",
                        }
                    ],
                },
                started_at=now - timedelta(hours=2),
                finished_at=now - timedelta(hours=2) + timedelta(milliseconds=120),
            ),
            TestCaseRun(
                id=2,
                case_id=case_response_two.json()["id"],
                project_id=1,
                triggered_by=1,
                status="failed",
                error_message="runner boom",
                report={
                    "status": "failed",
                    "steps": [
                        {
                            "step_index": 0,
                            "action": "click",
                            "target": "提交",
                            "status": "failed",
                            "error_message": "runner boom",
                        }
                    ],
                },
                started_at=now - timedelta(hours=1),
                finished_at=now - timedelta(hours=1) + timedelta(milliseconds=160),
            ),
            TestCaseRun(
                id=3,
                case_id=case_response_three.json()["id"],
                project_id=2,
                triggered_by=1,
                status="needs_intervention",
                error_message="need manual help",
                report={
                    "status": "failed",
                    "steps": [
                        {
                            "step_index": 0,
                            "action": "click",
                            "target": "登录按钮",
                            "status": "failed",
                            "error_message": "need manual help",
                            "intervention_request": {
                                "page_url": "https://example.com/login",
                                "target_description": "登录按钮",
                                "dom_snapshot": [],
                            },
                        }
                    ],
                },
                started_at=now,
                finished_at=now + timedelta(milliseconds=200),
            ),
        ]
    )
    db_session.commit()

    global_response = client.get(
        "/api/v1/executions/overview",
        params={"scope_type": "global", "window_days": 7},
    )
    assert global_response.status_code == 200
    global_payload = global_response.json()
    assert global_payload["scope_type"] == "global"
    assert global_payload["scope_project_id"] is None
    assert global_payload["scope_case_id"] is None
    assert global_payload["total_count"] == 3
    assert global_payload["auto_completed_count"] == 2
    assert global_payload["automation_rate"] == 0.6667
    assert global_payload["intervention_count"] == 1
    assert global_payload["intervention_rate"] == 0.3333
    assert global_payload["pass_rate"] == 0.3333

    project_response = client.get(
        "/api/v1/executions/overview",
        params={"scope_type": "project", "project_id": 1, "window_days": 7},
    )
    assert project_response.status_code == 200
    project_payload = project_response.json()
    assert project_payload["scope_type"] == "project"
    assert project_payload["scope_project_id"] == 1
    assert project_payload["scope_case_id"] is None
    assert project_payload["total_count"] == 2
    assert project_payload["auto_completed_count"] == 2
    assert project_payload["automation_rate"] == 1.0
    assert project_payload["intervention_count"] == 0
    assert project_payload["intervention_rate"] == 0.0

    case_response = client.get(
        "/api/v1/executions/overview",
        params={
            "scope_type": "case",
            "project_id": 1,
            "case_id": case_response_two.json()["id"],
            "window_days": 7,
        },
    )
    assert case_response.status_code == 200
    case_payload = case_response.json()
    assert case_payload["scope_type"] == "case"
    assert case_payload["scope_project_id"] == 1
    assert case_payload["scope_case_id"] == case_response_two.json()["id"]
    assert case_payload["total_count"] == 1
    assert case_payload["failed_count"] == 1
    assert case_payload["auto_completed_count"] == 1
    assert case_payload["automation_rate"] == 1.0
    assert case_payload["intervention_count"] == 0
    assert case_payload["intervention_rate"] == 0.0


# ---------------------------------------------------------------------------
# Streaming execution tests
# ---------------------------------------------------------------------------


def test_sync_and_streaming_execution_share_final_result(
    client,
    monkeypatch,
    db_session,
) -> None:
    create_response = client.post(
        "/api/v1/cases",
        json={
            "project_id": 1,
            "actor_user_id": 1,
            "name": "统一事件源用例",
            "base_url": "https://stream.example.com",
            "steps": [{"action": "goto", "value": "/home"}],
        },
    )
    case_id = create_response.json()["id"]

    def fake_streaming(**_kwargs):
        yield StepStreamEvent(
            type="step_start",
            step_index=0,
            action="goto",
            value="/home",
        )
        yield StepStreamEvent(
            type="step_complete",
            step_index=0,
            action="goto",
            status="passed",
            duration_ms=30,
        )
        return [
            StepExecutionEvidence(
                step_index=0,
                action="goto",
                value="/home",
                status="passed",
                duration_ms=30,
            )
        ]

    monkeypatch.setattr(
        execution_service,
        "execute_case_with_playwright_streaming",
        fake_streaming,
    )

    sync_detail = execution_service.execute_case(
        db_session,
        case_id,
        CaseExecutionRequest(actor_user_id=1),
    )
    stream = execution_service.execute_case_streaming(
        db_session,
        case_id,
        CaseExecutionRequest(actor_user_id=1),
    )
    stream_events = []
    try:
        while True:
            stream_events.append(next(stream))
    except StopIteration as stop:
        stream_detail = stop.value

    assert [event.type for event in stream_events] == [
        "step_start",
        "step_complete",
    ]
    assert sync_detail.status == stream_detail.status == "passed"
    assert [
        step.model_dump(mode="json")
        for step in sync_detail.report.steps
    ] == [
        step.model_dump(mode="json")
        for step in stream_detail.report.steps
    ]


def test_execute_case_streaming_yields_step_events_and_returns_detail(client, monkeypatch, db_session) -> None:
    """execute_case_streaming should yield StepStreamEvent per step, return detail via StopIteration."""
    create_response = client.post(
        "/api/v1/cases",
        json={
            "project_id": 1,
            "actor_user_id": 1,
            "name": "流式用例",
            "base_url": "https://stream.example.com",
            "steps": [
                {"action": "goto", "value": "/home"},
                {"action": "click", "target": "提交按钮"},
            ],
        },
    )
    case_id = create_response.json()["id"]

    fake_evidence = [
        StepExecutionEvidence(
            step_index=0, action="goto", value="/home",
            status="passed", duration_ms=30,
        ),
        StepExecutionEvidence(
            step_index=1, action="click", target="提交按钮",
            status="passed", duration_ms=55,
        ),
    ]

    def fake_streaming(*, case, execution_id: int, base_url: str | None, cancel_event=None, correction_store=None, input_values=None):
        for index, step in enumerate(case.steps):
            yield StepStreamEvent(
                type="step_start",
                step_index=index,
                action=step.action,
                target=getattr(step, "target", None),
                value=getattr(step, "value", None),
            )
            yield StepStreamEvent(
                type="step_complete",
                step_index=index,
                action=step.action,
                status="passed",
                duration_ms=fake_evidence[index].duration_ms,
            )
        return fake_evidence

    monkeypatch.setattr(
        execution_service,
        "execute_case_with_playwright_streaming",
        fake_streaming,
    )

    stream = execution_service.execute_case_streaming(
        db_session, case_id, CaseExecutionRequest(actor_user_id=1),
    )
    events = []
    detail = None
    try:
        while True:
            events.append(next(stream))
    except StopIteration as stop:
        detail = stop.value

    assert [event.type for event in events] == [
        "step_start", "step_complete",
        "step_start", "step_complete",
    ]
    assert events[0].action == "goto"
    assert events[1].status == "passed"
    assert events[2].action == "click"
    assert detail is not None
    assert detail.status == "passed"
    assert detail.case_name == "流式用例"


def test_execute_case_streaming_cancellation_raises_error(client, monkeypatch, db_session) -> None:
    """When cancel_event is set mid-stream, a RunnerCancelledError should surface."""
    from threading import Event
    from app.runners.playwright_runner import RunnerCancelledError

    create_response = client.post(
        "/api/v1/cases",
        json={
            "project_id": 1,
            "actor_user_id": 1,
            "name": "取消用例",
            "base_url": "https://cancel.example.com",
            "steps": [{"action": "goto", "value": "/page"}],
        },
    )
    case_id = create_response.json()["id"]

    cancel_event = Event()

    def fake_streaming_cancel(*, case, execution_id, base_url, cancel_event=None, correction_store=None, input_values=None):
        yield StepStreamEvent(type="step_start", step_index=0, action="goto", value="/page")
        cancel_event.set()
        raise RunnerCancelledError("Execution cancelled by user.", step_results=[])

    monkeypatch.setattr(
        execution_service,
        "execute_case_with_playwright_streaming",
        fake_streaming_cancel,
    )

    stream = execution_service.execute_case_streaming(
        db_session, case_id, CaseExecutionRequest(actor_user_id=1),
        cancel_event=cancel_event,
    )
    events = [next(stream)]
    try:
        while True:
            events.append(next(stream))
    except RunnerCancelledError:
        pass

    assert len(events) == 1
    assert events[0].type == "step_start"
    execution = (
        db_session.query(TestCaseRun)
        .filter(TestCaseRun.case_id == case_id)
        .one()
    )
    assert execution.status == "cancelled"
    assert execution.finished_at is not None
    assert execution.report["steps"] == []
