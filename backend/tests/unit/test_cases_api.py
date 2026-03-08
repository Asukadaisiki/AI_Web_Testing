"""Tests for case persistence endpoints."""

from __future__ import annotations


def test_create_case_success(client) -> None:
    response = client.post(
        "/api/v1/cases",
        json={
            "project_id": 1,
            "actor_user_id": 1,
            "name": "登录冒烟",
            "description": "验证登录成功后跳转仪表盘",
            "steps": [
                {"action": "goto", "value": "/login"},
                {"action": "input", "target": "用户名输入框", "value": "admin"},
                {"action": "click", "target": "登录按钮"},
                {"action": "assert_url_contains", "value": "/dashboard"},
            ],
        },
    )

    assert response.status_code == 201
    assert response.headers["Location"] == "/api/v1/cases/1"
    assert response.json()["project_id"] == 1
    assert response.json()["created_by"] == 1
    assert response.json()["updated_by"] == 1
    assert response.json()["steps"] == [
        {"action": "goto", "value": "/login"},
        {"action": "input", "target": "用户名输入框", "value": "admin"},
        {"action": "click", "target": "登录按钮"},
        {"action": "assert_url_contains", "value": "/dashboard"},
    ]


def test_list_cases_returns_latest_first(client) -> None:
    first = {
        "project_id": 1,
        "actor_user_id": 1,
        "name": "第一个用例",
        "steps": [{"action": "goto", "value": "/first"}],
    }
    second = {
        "project_id": 1,
        "actor_user_id": 1,
        "name": "第二个用例",
        "steps": [{"action": "goto", "value": "/second"}],
    }

    assert client.post("/api/v1/cases", json=first).status_code == 201
    assert client.post("/api/v1/cases", json=second).status_code == 201

    response = client.get("/api/v1/cases")

    assert response.status_code == 200
    assert [case["name"] for case in response.json()] == ["第二个用例", "第一个用例"]


def test_get_case_detail_returns_case(client) -> None:
    create_response = client.post(
        "/api/v1/cases",
        json={
            "project_id": 1,
            "actor_user_id": 1,
            "name": "详情用例",
            "steps": [{"action": "goto", "value": "/detail"}],
        },
    )

    response = client.get(f"/api/v1/cases/{create_response.json()['id']}")

    assert response.status_code == 200
    assert response.json()["name"] == "详情用例"


def test_get_case_detail_returns_not_found_for_unknown_case(client) -> None:
    response = client.get("/api/v1/cases/999")

    assert response.status_code == 404
    assert response.json() == {"detail": "Case not found."}


def test_create_case_rejects_invalid_dsl(client) -> None:
    response = client.post(
        "/api/v1/cases",
        json={
            "project_id": 1,
            "actor_user_id": 1,
            "name": "非法 DSL",
            "steps": [
                {"action": "click", "value": "缺少 target"},
            ],
        },
    )

    assert response.status_code == 422


def test_create_case_returns_not_found_when_project_missing(client) -> None:
    response = client.post(
        "/api/v1/cases",
        json={
            "project_id": 999,
            "actor_user_id": 1,
            "name": "孤立用例",
            "steps": [{"action": "goto", "value": "/demo"}],
        },
    )

    assert response.status_code == 404
    assert response.json() == {"detail": "Project 999 not found."}
