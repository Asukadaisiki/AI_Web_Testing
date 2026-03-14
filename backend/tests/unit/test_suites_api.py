"""Tests for suite endpoints."""

from __future__ import annotations

import app.services.executions as execution_service
import app.services.suites as suite_service
from app.models import SuiteRun, SuiteRunItem, TestSuite
from app.schemas.executions import StepExecutionEvidence
from app.runners import RunnerExecutionError


def _create_case(
    client,
    *,
    name: str,
    project_id: int = 1,
    base_url: str = "https://example.com",
    input_contract: list[dict] | None = None,
    output_contract: list[dict] | None = None,
    steps: list[dict] | None = None,
) -> int:
    response = client.post(
        "/api/v1/cases",
        json={
            "project_id": project_id,
            "actor_user_id": 1,
            "name": name,
            "description": f"{name} description",
            "base_url": base_url,
            "input_contract": input_contract or [],
            "output_contract": output_contract or [],
            "steps": steps or [{"action": "goto", "value": "/"}],
        },
    )
    assert response.status_code == 201
    return response.json()["id"]


def test_create_get_and_update_suite_success(client) -> None:
    case_id_1 = _create_case(client, name="Login Smoke")
    case_id_2 = _create_case(client, name="Logout Smoke")

    create_response = client.post(
        "/api/v1/suites",
        json={
            "project_id": 1,
            "actor_user_id": 1,
            "name": "Basic Smoke Suite",
            "description": "Login and logout",
            "cases": [{"case_id": case_id_1}, {"case_id": case_id_2}],
        },
    )

    assert create_response.status_code == 201
    assert create_response.headers["Location"] == "/api/v1/suites/1"
    assert create_response.json()["case_count"] == 2
    assert create_response.json()["latest_run"] is None
    assert create_response.json()["cases"] == [
        {"case_id": case_id_1, "case_name": "Login Smoke", "order_index": 1},
        {"case_id": case_id_2, "case_name": "Logout Smoke", "order_index": 2},
    ]

    list_response = client.get("/api/v1/suites")
    assert list_response.status_code == 200
    assert list_response.json()[0]["latest_run"] is None

    get_response = client.get("/api/v1/suites/1")
    assert get_response.status_code == 200
    assert get_response.json()["cases"][0]["case_name"] == "Login Smoke"

    update_response = client.put(
        "/api/v1/suites/1",
        json={
            "project_id": 1,
            "actor_user_id": 1,
            "name": "Regression Suite",
            "description": "Reverse order",
            "cases": [{"case_id": case_id_2}, {"case_id": case_id_1}],
        },
    )

    assert update_response.status_code == 200
    assert update_response.json()["name"] == "Regression Suite"
    assert update_response.json()["cases"] == [
        {"case_id": case_id_2, "case_name": "Logout Smoke", "order_index": 1},
        {"case_id": case_id_1, "case_name": "Login Smoke", "order_index": 2},
    ]


def test_create_suite_rejects_duplicate_case_ids_and_cross_project_cases(client, db_session) -> None:
    case_id = _create_case(client, name="Project One Case")
    from app.models import Project

    db_session.add(Project(id=2, name="Other Project", description="Other"))
    db_session.commit()
    other_case_id = _create_case(client, name="Other Project Case", project_id=2)

    duplicate_response = client.post(
        "/api/v1/suites",
        json={
            "project_id": 1,
            "actor_user_id": 1,
            "name": "Duplicate Suite",
            "cases": [{"case_id": case_id}, {"case_id": case_id}],
        },
    )
    assert duplicate_response.status_code == 422
    assert duplicate_response.json() == {"detail": "Suite cannot contain duplicate case_id values."}

    cross_project_response = client.post(
        "/api/v1/suites",
        json={
            "project_id": 1,
            "actor_user_id": 1,
            "name": "Cross Project Suite",
            "cases": [{"case_id": case_id}, {"case_id": other_case_id}],
        },
    )
    assert cross_project_response.status_code == 422
    assert cross_project_response.json() == {"detail": "Suite can only contain cases from the same project."}


def test_create_suite_requires_at_least_one_case(client) -> None:
    response = client.post(
        "/api/v1/suites",
        json={
            "project_id": 1,
            "actor_user_id": 1,
            "name": "Empty Suite",
            "cases": [],
        },
    )

    assert response.status_code == 422


def test_execute_suite_persists_suite_run_and_exposes_history_and_origin(client, db_session, monkeypatch) -> None:
    case_id_1 = _create_case(
        client,
        name="Step One",
        output_contract=[
            {
                "name": "sessionToken",
                "context_key": "session_token",
                "value_type": "string",
                "source": "latest_url",
            }
        ],
        steps=[{"action": "goto", "value": "/session"}],
    )
    case_id_2 = _create_case(
        client,
        name="Step Two",
        input_contract=[
            {
                "name": "sessionToken",
                "context_key": "session_token",
                "value_type": "string",
                "required": True,
            }
        ],
        steps=[{"action": "assert_url_contains", "value": "${session_token}"}],
    )
    suite_response = client.post(
        "/api/v1/suites",
        json={
            "project_id": 1,
            "actor_user_id": 1,
            "name": "Ordered Suite",
            "cases": [{"case_id": case_id_1}, {"case_id": case_id_2}],
        },
    )
    assert suite_response.status_code == 201

    call_sequence: list[str] = []

    def fake_execute_case_with_playwright(*, case, execution_id: int, base_url: str | None, correction_store=None):
        call_sequence.append(case.name)
        assert base_url == "https://override.example.com"
        if case.name == "Step Two":
            assert case.steps[0].value == "https://override.example.com/sessions/token-001"
        return [
            StepExecutionEvidence(
                step_index=0,
                action="goto",
                value="/",
                status="passed",
                url="https://override.example.com/sessions/token-001",
            )
        ]

    monkeypatch.setattr(execution_service, "execute_case_with_playwright", fake_execute_case_with_playwright)

    response = client.post(
        "/api/v1/suites/1/execute",
        json={"actor_user_id": 1, "base_url": "https://override.example.com"},
    )

    assert response.status_code == 200
    payload = response.json()
    assert call_sequence == ["Step One", "Step Two"]
    assert payload["id"] == 1
    assert payload["suite_id"] == 1
    assert payload["suite_name"] == "Ordered Suite"
    assert payload["source"] == "manual"
    assert payload["source_suite_run_id"] is None
    assert payload["context_source"] == "empty"
    assert payload["context_source_suite_run_id"] is None
    assert payload["rerun_context_mode"] == "not_applicable"
    assert payload["context_snapshot"] == {"session_token": "https://override.example.com/sessions/token-001"}
    assert payload["total_cases"] == 2
    assert payload["passed_cases"] == 2
    assert payload["failed_cases"] == 0
    assert payload["status"] == "passed"
    assert payload["base_url_override"] == "https://override.example.com"
    assert payload["items"] == [
        {
            "id": 1,
            "case_id": case_id_1,
            "case_name_snapshot": "Step One",
            "order_index": 1,
            "execution_id": 1,
            "status": "passed",
            "context_reads": [],
            "context_writes": [
                {
                    "name": "sessionToken",
                    "context_key": "session_token",
                    "value_type": "string",
                    "source": "latest_url",
                    "status": "written",
                    "error_message": None,
                }
            ],
            "context_resolution_error": None,
        },
        {
            "id": 2,
            "case_id": case_id_2,
            "case_name_snapshot": "Step Two",
            "order_index": 2,
            "execution_id": 2,
            "status": "passed",
            "context_reads": [
                {
                    "name": "sessionToken",
                    "context_key": "session_token",
                    "value_type": "string",
                    "resolved": True,
                    "source_suite_run_id": 1,
                    "error_message": None,
                }
            ],
            "context_writes": [],
            "context_resolution_error": None,
        },
    ]
    assert payload["executions"] == [
        {"execution_id": 1, "case_id": case_id_1, "case_name": "Step One", "status": "passed"},
        {"execution_id": 2, "case_id": case_id_2, "case_name": "Step Two", "status": "passed"},
    ]

    stored_runs = db_session.query(SuiteRun).order_by(SuiteRun.id.asc()).all()
    assert len(stored_runs) == 1
    assert stored_runs[0].status == "passed"
    assert stored_runs[0].base_url_override == "https://override.example.com"
    assert stored_runs[0].context_source == "empty"
    assert stored_runs[0].rerun_context_mode == "not_applicable"
    assert stored_runs[0].context_snapshot == {"session_token": "https://override.example.com/sessions/token-001"}

    stored_items = db_session.query(SuiteRunItem).order_by(SuiteRunItem.id.asc()).all()
    assert len(stored_items) == 2
    assert [item.execution_id for item in stored_items] == [1, 2]
    assert stored_items[0].context_writes == [
        {
            "name": "sessionToken",
            "context_key": "session_token",
            "value_type": "string",
            "source": "latest_url",
            "status": "written",
            "error_message": None,
        }
    ]
    assert stored_items[1].context_reads == [
        {
            "name": "sessionToken",
            "context_key": "session_token",
            "value_type": "string",
            "resolved": True,
            "source_suite_run_id": 1,
            "error_message": None,
        }
    ]

    list_runs_response = client.get("/api/v1/suites/1/runs")
    assert list_runs_response.status_code == 200
    assert list_runs_response.json()[0]["id"] == 1
    assert list_runs_response.json()[0]["status"] == "passed"
    assert list_runs_response.json()[0]["context_source"] == "empty"

    get_run_response = client.get("/api/v1/suites/1/runs/1")
    assert get_run_response.status_code == 200
    assert get_run_response.json()["items"][0]["execution_id"] == 1
    assert get_run_response.json()["items"][1]["context_reads"][0]["context_key"] == "session_token"

    suites_response = client.get("/api/v1/suites")
    assert suites_response.status_code == 200
    assert suites_response.json()[0]["latest_run"]["id"] == 1
    assert suites_response.json()[0]["latest_run"]["context_snapshot"] == {
        "session_token": "https://override.example.com/sessions/token-001"
    }

    execution_response = client.get("/api/v1/executions/1")
    assert execution_response.status_code == 200
    assert execution_response.json()["origin_suite_run"] == {
        "suite_id": 1,
        "suite_name": "Ordered Suite",
        "suite_run_id": 1,
    }
    assert execution_response.json()["suite_context"] == {
        "reads": [],
        "writes": [
            {
                "name": "sessionToken",
                "context_key": "session_token",
                "value_type": "string",
                "source": "latest_url",
                "status": "written",
                "error_message": None,
            }
        ],
        "resolution_error": None,
    }


def test_rerun_failed_suite_run_only_reruns_failed_items(client, monkeypatch) -> None:
    case_id_1 = _create_case(client, name="First Fails")
    case_id_2 = _create_case(client, name="Second Passes")
    suite_response = client.post(
        "/api/v1/suites",
        json={
            "project_id": 1,
            "actor_user_id": 1,
            "name": "Rerun Suite",
            "cases": [{"case_id": case_id_1}, {"case_id": case_id_2}],
        },
    )
    assert suite_response.status_code == 201

    call_sequence: list[str] = []
    case_one_attempt = {"count": 0}

    def fake_execute_case_with_playwright(*, case, execution_id: int, base_url: str | None, correction_store=None):
        call_sequence.append(case.name)
        if case.name == "First Fails":
            case_one_attempt["count"] += 1
            if case_one_attempt["count"] == 1:
                raise RunnerExecutionError(
                    "runner boom",
                    step_results=[
                        StepExecutionEvidence(
                            step_index=0,
                            action="click",
                            target="Submit",
                            status="failed",
                            error_message="runner boom",
                        )
                    ],
                )
        return [
            StepExecutionEvidence(
                step_index=0,
                action="goto",
                value="/",
                status="passed",
            )
        ]

    monkeypatch.setattr(execution_service, "execute_case_with_playwright", fake_execute_case_with_playwright)

    initial_response = client.post("/api/v1/suites/1/execute", json={"actor_user_id": 1})
    assert initial_response.status_code == 200
    assert initial_response.json()["status"] == "failed"
    assert initial_response.json()["failed_cases"] == 1

    rerun_response = client.post("/api/v1/suites/1/runs/1/rerun-failed", json={"actor_user_id": 1})
    assert rerun_response.status_code == 200
    rerun_payload = rerun_response.json()
    assert call_sequence == ["First Fails", "Second Passes", "First Fails"]
    assert rerun_payload["id"] == 2
    assert rerun_payload["source"] == "rerun_failed"
    assert rerun_payload["source_suite_run_id"] == 1
    assert rerun_payload["context_source"] == "suite_run_snapshot"
    assert rerun_payload["context_source_suite_run_id"] == 1
    assert rerun_payload["rerun_context_mode"] == "reuse_source_context"
    assert rerun_payload["context_snapshot"] == {}
    assert rerun_payload["total_cases"] == 1
    assert rerun_payload["passed_cases"] == 1
    assert rerun_payload["failed_cases"] == 0
    assert rerun_payload["items"] == [
        {
            "id": 3,
            "case_id": case_id_1,
            "case_name_snapshot": "First Fails",
            "order_index": 1,
            "execution_id": 3,
            "status": "passed",
            "context_reads": [],
            "context_writes": [],
            "context_resolution_error": None,
        }
    ]
    assert rerun_payload["executions"] == [
        {"execution_id": 3, "case_id": case_id_1, "case_name": "First Fails", "status": "passed"}
    ]

    list_runs_response = client.get("/api/v1/suites/1/runs")
    assert list_runs_response.status_code == 200
    assert [item["id"] for item in list_runs_response.json()] == [2, 1]

    rerun_detail_response = client.get("/api/v1/suites/1/runs/2")
    assert rerun_detail_response.status_code == 200
    assert rerun_detail_response.json()["source_suite_run_id"] == 1
    assert rerun_detail_response.json()["context_source_suite_run_id"] == 1


def test_rerun_failed_suite_run_rejects_run_without_failures(client, monkeypatch) -> None:
    case_id = _create_case(client, name="Always Passes")
    suite_response = client.post(
        "/api/v1/suites",
        json={
            "project_id": 1,
            "actor_user_id": 1,
            "name": "Passing Suite",
            "cases": [{"case_id": case_id}],
        },
    )
    assert suite_response.status_code == 201

    def fake_execute_case_with_playwright(*, case, execution_id: int, base_url: str | None, correction_store=None):
        return [
            StepExecutionEvidence(
                step_index=0,
                action="goto",
                value="/",
                status="passed",
            )
        ]

    monkeypatch.setattr(execution_service, "execute_case_with_playwright", fake_execute_case_with_playwright)

    execute_response = client.post("/api/v1/suites/1/execute", json={"actor_user_id": 1})
    assert execute_response.status_code == 200

    rerun_response = client.post("/api/v1/suites/1/runs/1/rerun-failed", json={"actor_user_id": 1})
    assert rerun_response.status_code == 422
    assert rerun_response.json() == {"detail": "Suite run does not contain failed cases to rerun."}


def test_execute_suite_fails_fast_when_required_context_is_missing(client, monkeypatch) -> None:
    case_id = _create_case(
        client,
        name="Needs Context",
        input_contract=[
            {
                "name": "sessionToken",
                "context_key": "session_token",
                "value_type": "string",
                "required": True,
            }
        ],
        steps=[{"action": "assert_url_contains", "value": "${session_token}"}],
    )
    suite_response = client.post(
        "/api/v1/suites",
        json={
            "project_id": 1,
            "actor_user_id": 1,
            "name": "Missing Context Suite",
            "cases": [{"case_id": case_id}],
        },
    )
    assert suite_response.status_code == 201

    runner_calls: list[str] = []

    def fake_execute_case_with_playwright(*, case, execution_id: int, base_url: str | None, correction_store=None):
        runner_calls.append(case.name)
        return []

    monkeypatch.setattr(execution_service, "execute_case_with_playwright", fake_execute_case_with_playwright)

    response = client.post("/api/v1/suites/1/execute", json={"actor_user_id": 1})

    assert response.status_code == 200
    payload = response.json()
    assert runner_calls == []
    assert payload["status"] == "failed"
    assert payload["failed_cases"] == 1
    assert payload["context_snapshot"] == {}
    assert payload["items"][0]["context_reads"] == [
        {
            "name": "sessionToken",
            "context_key": "session_token",
            "value_type": "string",
            "resolved": False,
            "source_suite_run_id": None,
            "error_message": "Required context key 'session_token' is missing.",
        }
    ]
    assert payload["items"][0]["context_resolution_error"] == "Required context key 'session_token' is missing."

    execution_response = client.get(f"/api/v1/executions/{payload['items'][0]['execution_id']}")
    assert execution_response.status_code == 200
    assert execution_response.json()["status"] == "failed"
    assert execution_response.json()["report"]["steps"][0]["action"] == "context_resolve"
    assert execution_response.json()["suite_context"]["resolution_error"] == "Required context key 'session_token' is missing."


def test_execute_suite_skips_missing_optional_context_without_failing(client, monkeypatch) -> None:
    case_id = _create_case(
        client,
        name="Optional Context",
        input_contract=[
            {
                "name": "optionalToken",
                "context_key": "optional_token",
                "value_type": "string",
                "required": False,
            }
        ],
        steps=[{"action": "assert_url_contains", "value": "example.com"}],
    )
    suite_response = client.post(
        "/api/v1/suites",
        json={
            "project_id": 1,
            "actor_user_id": 1,
            "name": "Optional Context Suite",
            "cases": [{"case_id": case_id}],
        },
    )
    assert suite_response.status_code == 201

    def fake_execute_case_with_playwright(*, case, execution_id: int, base_url: str | None, correction_store=None):
        return [
            StepExecutionEvidence(
                step_index=0,
                action="assert_url_contains",
                value="example.com",
                status="passed",
                url="https://example.com/",
            )
        ]

    monkeypatch.setattr(execution_service, "execute_case_with_playwright", fake_execute_case_with_playwright)

    response = client.post("/api/v1/suites/1/execute", json={"actor_user_id": 1})

    assert response.status_code == 200
    payload = response.json()
    assert payload["status"] == "passed"
    assert payload["items"][0]["context_reads"] == [
        {
            "name": "optionalToken",
            "context_key": "optional_token",
            "value_type": "string",
            "resolved": False,
            "source_suite_run_id": None,
            "error_message": None,
        }
    ]
    assert payload["items"][0]["context_resolution_error"] is None


def test_execute_suite_marks_output_write_failure_when_source_value_is_missing(client, monkeypatch) -> None:
    case_id = _create_case(
        client,
        name="Writes Missing Output",
        output_contract=[
            {
                "name": "pageTitle",
                "context_key": "page_title",
                "value_type": "string",
                "source": "last_step_page_title",
            }
        ],
    )
    suite_response = client.post(
        "/api/v1/suites",
        json={
            "project_id": 1,
            "actor_user_id": 1,
            "name": "Output Failure Suite",
            "cases": [{"case_id": case_id}],
        },
    )
    assert suite_response.status_code == 201

    def fake_execute_case_with_playwright(*, case, execution_id: int, base_url: str | None, correction_store=None):
        return [
            StepExecutionEvidence(
                step_index=0,
                action="goto",
                value="/",
                status="passed",
                url="https://example.com/final",
            )
        ]

    monkeypatch.setattr(execution_service, "execute_case_with_playwright", fake_execute_case_with_playwright)

    response = client.post("/api/v1/suites/1/execute", json={"actor_user_id": 1})

    assert response.status_code == 200
    payload = response.json()
    assert payload["status"] == "failed"
    assert payload["context_snapshot"] == {}
    assert payload["items"][0]["context_writes"] == [
        {
            "name": "pageTitle",
            "context_key": "page_title",
            "value_type": "string",
            "source": "last_step_page_title",
            "status": "skipped",
            "error_message": "Output contract source 'last_step_page_title' did not produce a value.",
        }
    ]
    assert payload["items"][0]["context_resolution_error"] == (
        "Output contract source 'last_step_page_title' did not produce a value."
    )


def test_execute_suite_aborts_remaining_output_writes_after_first_failure(client, monkeypatch) -> None:
    case_id = _create_case(
        client,
        name="Multiple Outputs",
        output_contract=[
            {
                "name": "pageTitle",
                "context_key": "page_title",
                "value_type": "string",
                "source": "last_step_page_title",
            },
            {
                "name": "latestPage",
                "context_key": "latest_page",
                "value_type": "string",
                "source": "latest_url",
            },
        ],
    )
    suite_response = client.post(
        "/api/v1/suites",
        json={
            "project_id": 1,
            "actor_user_id": 1,
            "name": "Multiple Outputs Suite",
            "cases": [{"case_id": case_id}],
        },
    )
    assert suite_response.status_code == 201

    def fake_execute_case_with_playwright(*, case, execution_id: int, base_url: str | None, correction_store=None):
        return [
            StepExecutionEvidence(
                step_index=0,
                action="goto",
                value="/",
                status="passed",
                url="https://example.com/final",
            )
        ]

    monkeypatch.setattr(execution_service, "execute_case_with_playwright", fake_execute_case_with_playwright)

    response = client.post("/api/v1/suites/1/execute", json={"actor_user_id": 1})

    assert response.status_code == 200
    payload = response.json()
    assert payload["status"] == "failed"
    assert payload["items"][0]["context_writes"] == [
        {
            "name": "pageTitle",
            "context_key": "page_title",
            "value_type": "string",
            "source": "last_step_page_title",
            "status": "skipped",
            "error_message": "Output contract source 'last_step_page_title' did not produce a value.",
        },
        {
            "name": "latestPage",
            "context_key": "latest_page",
            "value_type": "string",
            "source": "latest_url",
            "status": "skipped",
            "error_message": "Context write aborted because another output contract failed.",
        },
    ]
    assert payload["context_snapshot"] == {}


def test_execute_suite_rejects_context_type_mismatch(client, monkeypatch) -> None:
    case_id_1 = _create_case(
        client,
        name="Writes String",
        output_contract=[
            {
                "name": "sessionToken",
                "context_key": "session_token",
                "value_type": "string",
                "source": "latest_url",
            }
        ],
    )
    case_id_2 = _create_case(
        client,
        name="Reads Number",
        input_contract=[
            {
                "name": "sessionToken",
                "context_key": "session_token",
                "value_type": "number",
                "required": True,
            }
        ],
        steps=[{"action": "assert_url_contains", "value": "${session_token}"}],
    )
    suite_response = client.post(
        "/api/v1/suites",
        json={
            "project_id": 1,
            "actor_user_id": 1,
            "name": "Type Mismatch Suite",
            "cases": [{"case_id": case_id_1}, {"case_id": case_id_2}],
        },
    )
    assert suite_response.status_code == 201

    def fake_execute_case_with_playwright(*, case, execution_id: int, base_url: str | None, correction_store=None):
        return [
            StepExecutionEvidence(
                step_index=0,
                action="goto",
                value="/",
                status="passed",
                url="https://example.com/string-value",
            )
        ]

    monkeypatch.setattr(execution_service, "execute_case_with_playwright", fake_execute_case_with_playwright)

    response = client.post("/api/v1/suites/1/execute", json={"actor_user_id": 1})

    assert response.status_code == 200
    payload = response.json()
    assert payload["status"] == "failed"
    assert payload["items"][1]["context_reads"] == [
        {
            "name": "sessionToken",
            "context_key": "session_token",
            "value_type": "number",
            "resolved": False,
            "source_suite_run_id": None,
            "error_message": "Context key 'session_token' expected number but received string.",
        }
    ]
    assert payload["items"][1]["context_resolution_error"] == (
        "Context key 'session_token' expected number but received string."
    )


def test_rerun_failed_suite_run_supports_reuse_source_context_and_empty_context(client, monkeypatch) -> None:
    case_id_1 = _create_case(
        client,
        name="Write Token",
        output_contract=[
            {
                "name": "sessionToken",
                "context_key": "session_token",
                "value_type": "string",
                "source": "latest_url",
            }
        ],
    )
    case_id_2 = _create_case(
        client,
        name="Flaky Consumer",
        input_contract=[
            {
                "name": "sessionToken",
                "context_key": "session_token",
                "value_type": "string",
                "required": True,
            }
        ],
        steps=[{"action": "assert_url_contains", "value": "${session_token}"}],
    )
    suite_response = client.post(
        "/api/v1/suites",
        json={
            "project_id": 1,
            "actor_user_id": 1,
            "name": "Rerun Context Suite",
            "cases": [{"case_id": case_id_1}, {"case_id": case_id_2}],
        },
    )
    assert suite_response.status_code == 201

    case_two_attempt = {"count": 0}

    def fake_execute_case_with_playwright(*, case, execution_id: int, base_url: str | None, correction_store=None):
        if case.name == "Write Token":
            return [
                StepExecutionEvidence(
                    step_index=0,
                    action="goto",
                    value="/",
                    status="passed",
                    url="https://example.com/session-rerun",
                )
            ]

        assert case.steps[0].value == "https://example.com/session-rerun"
        case_two_attempt["count"] += 1
        if case_two_attempt["count"] == 1:
            raise RunnerExecutionError(
                "runner boom",
                step_results=[
                    StepExecutionEvidence(
                        step_index=0,
                        action="assert_url_contains",
                        value="https://example.com/session-rerun",
                        status="failed",
                        error_message="runner boom",
                    )
                ],
            )
        return [
            StepExecutionEvidence(
                step_index=0,
                action="assert_url_contains",
                value="https://example.com/session-rerun",
                status="passed",
                url="https://example.com/session-rerun",
            )
        ]

    monkeypatch.setattr(execution_service, "execute_case_with_playwright", fake_execute_case_with_playwright)

    initial_response = client.post("/api/v1/suites/1/execute", json={"actor_user_id": 1})
    assert initial_response.status_code == 200
    assert initial_response.json()["status"] == "failed"
    assert initial_response.json()["context_snapshot"] == {"session_token": "https://example.com/session-rerun"}

    reuse_response = client.post("/api/v1/suites/1/runs/1/rerun-failed", json={"actor_user_id": 1})
    assert reuse_response.status_code == 200
    assert reuse_response.json()["status"] == "passed"
    assert reuse_response.json()["context_source"] == "suite_run_snapshot"
    assert reuse_response.json()["context_snapshot"] == {"session_token": "https://example.com/session-rerun"}

    empty_response = client.post(
        "/api/v1/suites/1/runs/1/rerun-failed",
        json={"actor_user_id": 1, "rerun_context_mode": "empty_context"},
    )
    assert empty_response.status_code == 200
    assert empty_response.json()["status"] == "failed"
    assert empty_response.json()["context_source"] == "empty"
    assert empty_response.json()["context_snapshot"] == {}
    assert empty_response.json()["items"][0]["context_resolution_error"] == (
        "Required context key 'session_token' is missing."
    )


def test_render_value_placeholders_recurses_into_nested_lists_and_dicts() -> None:
    rendered = suite_service._render_value_placeholders(
        {
            "steps": [{"action": "assert_text", "value": "token=${session_token}"}],
            "metadata": {
                "items": [
                    "prefix-${session_token}",
                    {"deep": "${session_token}"},
                ]
            },
        },
        allowed_keys={"session_token"},
        context_values={"session_token": "abc-123"},
    )

    assert rendered == {
        "steps": [{"action": "assert_text", "value": "token=abc-123"}],
        "metadata": {
            "items": [
                "prefix-abc-123",
                {"deep": "abc-123"},
            ]
        },
    }


def test_execute_suite_rejects_empty_seeded_suite(client, db_session) -> None:
    db_session.add(TestSuite(id=2, project_id=1, created_by=1, updated_by=1, name="Empty Suite"))
    db_session.commit()

    response = client.post("/api/v1/suites/2/execute", json={"actor_user_id": 1})
    assert response.status_code == 422
    assert response.json() == {"detail": "Suite must contain at least one case before execution."}
