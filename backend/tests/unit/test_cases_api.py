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
            "base_url": "https://example.com",
            "input_contract": [
                {
                    "name": "username",
                    "context_key": "login_username",
                    "value_type": "string",
                    "required": True,
                }
            ],
            "output_contract": [
                {
                    "name": "sessionToken",
                    "context_key": "session_token",
                    "value_type": "string",
                }
            ],
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
    assert response.json()["base_url"] == "https://example.com"
    assert response.json()["input_contract"] == [
        {
            "name": "username",
            "context_key": "login_username",
            "value_type": "string",
            "required": True,
            "description": None,
        }
    ]
    assert response.json()["output_contract"] == [
        {
            "name": "sessionToken",
            "context_key": "session_token",
            "value_type": "string",
            "description": None,
        }
    ]
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
        "base_url": "https://first.example.com",
        "steps": [{"action": "goto", "value": "/first"}],
    }
    second = {
        "project_id": 1,
        "actor_user_id": 1,
        "name": "第二个用例",
        "base_url": "https://second.example.com",
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
            "base_url": "https://detail.example.com",
            "steps": [{"action": "goto", "value": "/detail"}],
        },
    )

    response = client.get(f"/api/v1/cases/{create_response.json()['id']}")

    assert response.status_code == 200
    assert response.json()["name"] == "详情用例"
    assert response.json()["base_url"] == "https://detail.example.com"
    assert response.json()["input_contract"] == []
    assert response.json()["output_contract"] == []


def test_get_case_detail_returns_not_found_for_unknown_case(client) -> None:
    response = client.get("/api/v1/cases/999")

    assert response.status_code == 404
    assert response.json() == {"detail": "Case not found."}


def test_update_case_success(client) -> None:
    create_response = client.post(
        "/api/v1/cases",
        json={
            "project_id": 1,
            "actor_user_id": 1,
            "name": "待更新用例",
            "base_url": "https://before.example.com",
            "steps": [{"action": "goto", "value": "/before"}],
        },
    )

    response = client.put(
        f"/api/v1/cases/{create_response.json()['id']}",
        json={
            "project_id": 1,
            "actor_user_id": 1,
            "name": "已更新用例",
            "description": "更新后的描述",
            "base_url": "https://after.example.com",
            "input_contract": [
                {
                    "name": "orderId",
                    "context_key": "order_id",
                    "value_type": "string",
                    "required": False,
                }
            ],
            "output_contract": [
                {
                    "name": "confirmationCode",
                    "context_key": "confirmation_code",
                    "value_type": "string",
                }
            ],
            "steps": [
                {"action": "goto", "value": "/after"},
                {"action": "assert_url_contains", "value": "/after"},
            ],
        },
    )

    assert response.status_code == 200
    assert response.json()["name"] == "已更新用例"
    assert response.json()["description"] == "更新后的描述"
    assert response.json()["base_url"] == "https://after.example.com"
    assert response.json()["input_contract"] == [
        {
            "name": "orderId",
            "context_key": "order_id",
            "value_type": "string",
            "required": False,
            "description": None,
        }
    ]
    assert response.json()["output_contract"] == [
        {
            "name": "confirmationCode",
            "context_key": "confirmation_code",
            "value_type": "string",
            "description": None,
        }
    ]
    assert response.json()["steps"] == [
        {"action": "goto", "value": "/after"},
        {"action": "assert_url_contains", "value": "/after"},
    ]


def test_update_case_returns_not_found_for_unknown_case(client) -> None:
    response = client.put(
        "/api/v1/cases/999",
        json={
            "project_id": 1,
            "actor_user_id": 1,
            "name": "未知用例",
            "steps": [{"action": "goto", "value": "/unknown"}],
        },
    )

    assert response.status_code == 404
    assert response.json() == {"detail": "Case 999 not found."}


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
