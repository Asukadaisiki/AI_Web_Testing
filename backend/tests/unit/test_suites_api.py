"""Tests for suite endpoints."""

from __future__ import annotations

import app.services.executions as execution_service
from app.models import Project, TestSuite as SuiteModel
from app.schemas.executions import StepExecutionEvidence
from app.runners import RunnerExecutionError


def _create_case(
    client,
    *,
    name: str,
    project_id: int = 1,
    base_url: str = "https://example.com",
    steps: list[dict] | None = None,
) -> int:
    response = client.post(
        "/api/v1/cases",
        json={
            "project_id": project_id,
            "actor_user_id": 1,
            "name": name,
            "description": f"{name} 描述",
            "base_url": base_url,
            "steps": steps or [{"action": "goto", "value": "/"}],
        },
    )
    assert response.status_code == 201
    return response.json()["id"]


def test_create_get_and_update_suite_success(client) -> None:
    case_id_1 = _create_case(client, name="登录冒烟")
    case_id_2 = _create_case(client, name="退出冒烟")

    create_response = client.post(
        "/api/v1/suites",
        json={
            "project_id": 1,
            "actor_user_id": 1,
            "name": "基础冒烟套件",
            "description": "登录与退出",
            "cases": [{"case_id": case_id_1}, {"case_id": case_id_2}],
        },
    )

    assert create_response.status_code == 201
    assert create_response.headers["Location"] == "/api/v1/suites/1"
    assert create_response.json()["case_count"] == 2
    assert create_response.json()["cases"] == [
        {"case_id": case_id_1, "case_name": "登录冒烟", "order_index": 1},
        {"case_id": case_id_2, "case_name": "退出冒烟", "order_index": 2},
    ]

    list_response = client.get("/api/v1/suites")
    assert list_response.status_code == 200
    assert list_response.json() == [
        {
            "id": 1,
            "project_id": 1,
            "name": "基础冒烟套件",
            "description": "登录与退出",
            "case_count": 2,
            "created_by": 1,
            "updated_by": 1,
            "created_at": create_response.json()["created_at"],
            "updated_at": create_response.json()["updated_at"],
        }
    ]

    get_response = client.get("/api/v1/suites/1")
    assert get_response.status_code == 200
    assert get_response.json()["cases"][0]["case_name"] == "登录冒烟"

    update_response = client.put(
        "/api/v1/suites/1",
        json={
            "project_id": 1,
            "actor_user_id": 1,
            "name": "回归套件",
            "description": "顺序反转",
            "cases": [{"case_id": case_id_2}, {"case_id": case_id_1}],
        },
    )

    assert update_response.status_code == 200
    assert update_response.json()["name"] == "回归套件"
    assert update_response.json()["cases"] == [
        {"case_id": case_id_2, "case_name": "退出冒烟", "order_index": 1},
        {"case_id": case_id_1, "case_name": "登录冒烟", "order_index": 2},
    ]


def test_create_suite_rejects_duplicate_case_ids_and_cross_project_cases(client, db_session) -> None:
    case_id = _create_case(client, name="主项目用例")
    db_session.add(Project(id=2, name="Other Project", description="跨项目"))
    db_session.commit()
    other_case_id = _create_case(client, name="跨项目用例", project_id=2)

    duplicate_response = client.post(
        "/api/v1/suites",
        json={
            "project_id": 1,
            "actor_user_id": 1,
            "name": "重复套件",
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
            "name": "跨项目套件",
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
            "name": "空套件",
            "cases": [],
        },
    )

    assert response.status_code == 422


def test_execute_suite_runs_all_cases_and_returns_aggregate_result(client, monkeypatch) -> None:
    case_id_1 = _create_case(client, name="步骤一")
    case_id_2 = _create_case(client, name="步骤二")
    suite_response = client.post(
        "/api/v1/suites",
        json={
            "project_id": 1,
            "actor_user_id": 1,
            "name": "顺序执行套件",
            "cases": [{"case_id": case_id_1}, {"case_id": case_id_2}],
        },
    )
    assert suite_response.status_code == 201

    call_sequence: list[str] = []

    def fake_execute_case_with_playwright(*, case, execution_id: int, base_url: str | None):
        call_sequence.append(case.name)
        assert base_url == "https://override.example.com"
        return [
            StepExecutionEvidence(
                step_index=0,
                action="goto",
                value="/",
                status="passed",
            )
        ]

    monkeypatch.setattr(execution_service, "execute_case_with_playwright", fake_execute_case_with_playwright)

    response = client.post(
        "/api/v1/suites/1/execute",
        json={"actor_user_id": 1, "base_url": "https://override.example.com"},
    )

    assert response.status_code == 200
    assert call_sequence == ["步骤一", "步骤二"]
    assert response.json()["suite_id"] == 1
    assert response.json()["suite_name"] == "顺序执行套件"
    assert response.json()["total_cases"] == 2
    assert response.json()["passed_cases"] == 2
    assert response.json()["failed_cases"] == 0
    assert response.json()["status"] == "passed"
    assert response.json()["executions"] == [
        {"execution_id": 1, "case_id": case_id_1, "case_name": "步骤一", "status": "passed"},
        {"execution_id": 2, "case_id": case_id_2, "case_name": "步骤二", "status": "passed"},
    ]


def test_execute_suite_continues_after_failure_and_rejects_empty_seeded_suite(client, db_session, monkeypatch) -> None:
    case_id_1 = _create_case(client, name="先失败")
    case_id_2 = _create_case(client, name="后通过")
    suite_response = client.post(
        "/api/v1/suites",
        json={
            "project_id": 1,
            "actor_user_id": 1,
            "name": "继续执行套件",
            "cases": [{"case_id": case_id_1}, {"case_id": case_id_2}],
        },
    )
    assert suite_response.status_code == 201

    call_sequence: list[str] = []

    def fake_execute_case_with_playwright(*, case, execution_id: int, base_url: str | None):
        call_sequence.append(case.name)
        if case.name == "先失败":
            raise RunnerExecutionError(
                "runner boom",
                step_results=[
                    StepExecutionEvidence(
                        step_index=0,
                        action="click",
                        target="提交按钮",
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

    execute_response = client.post("/api/v1/suites/1/execute", json={"actor_user_id": 1})

    assert execute_response.status_code == 200
    assert call_sequence == ["先失败", "后通过"]
    assert execute_response.json()["passed_cases"] == 1
    assert execute_response.json()["failed_cases"] == 1
    assert execute_response.json()["status"] == "failed"
    assert execute_response.json()["executions"] == [
        {"execution_id": 1, "case_id": case_id_1, "case_name": "先失败", "status": "failed"},
        {"execution_id": 2, "case_id": case_id_2, "case_name": "后通过", "status": "passed"},
    ]

    db_session.add(SuiteModel(id=2, project_id=1, created_by=1, updated_by=1, name="空白套件"))
    db_session.commit()
    empty_response = client.post("/api/v1/suites/2/execute", json={"actor_user_id": 1})
    assert empty_response.status_code == 422
    assert empty_response.json() == {"detail": "Suite must contain at least one case before execution."}
