"""Platform API chain whitebox tests.

Simulates a real user flow from session login through case creation,
execution, and result verification.
"""

from __future__ import annotations

from fastapi.testclient import TestClient


LOGIN_URL = "/api/v1/auth/login"
ME_URL = "/api/v1/auth/me"
CASES_URL = "/api/v1/cases"

SEED_EMAIL = "seed-owner@example.com"
SEED_PASSWORD = "password123"


def test_unauthenticated_access_returns_401(anonymous_client: TestClient) -> None:
    """未登录访问 /auth/me 返回 401。"""
    resp = anonymous_client.get(ME_URL)
    assert resp.status_code == 401
    assert "未登录" in resp.json()["detail"]


def test_login_sets_session_and_returns_user(anonymous_client: TestClient) -> None:
    """登录后返回用户信息，session cookie 被设置。"""
    resp = anonymous_client.post(
        LOGIN_URL,
        json={"email": SEED_EMAIL, "password": SEED_PASSWORD},
    )
    assert resp.status_code == 200

    body = resp.json()
    assert body["email"] == SEED_EMAIL
    assert "id" in body
    assert "display_name" in body

    # 验证 session cookie 存在
    cookies = anonymous_client.cookies
    assert len(cookies) > 0


def test_session_persists_across_requests(client: TestClient) -> None:
    """已登录用户连续调用 /auth/me，返回一致的用户信息。"""
    resp1 = client.get(ME_URL)
    assert resp1.status_code == 200

    resp2 = client.get(ME_URL)
    assert resp2.status_code == 200

    assert resp1.json() == resp2.json()
    assert resp1.json()["email"] == SEED_EMAIL


LOGIN_CASE_DSL = {
    "name": "The Internet - 正向登录验证",
    "description": "验证用户使用正确账号密码可以成功登录并进入安全页面",
    "steps": [
        {"action": "goto", "value": "https://the-internet.herokuapp.com/login"},
        {"action": "input", "target": "username", "value": "tomsmith"},
        {"action": "input", "target": "password", "value": "SuperSecretPassword!"},
        {"action": "click", "target": "Login"},
        {"action": "assert_url_contains", "value": "/secure"},
        {"action": "assert_text", "target": "flash", "value": "You logged into a secure area!"},
    ],
}


def test_create_case_with_valid_dsl(client: TestClient) -> None:
    """创建包含有效 DSL 的测试用例，返回 201 和 Location header。"""
    payload = {**LOGIN_CASE_DSL, "project_id": 1}
    resp = client.post(CASES_URL, json=payload)
    assert resp.status_code == 201

    body = resp.json()
    assert body["name"] == LOGIN_CASE_DSL["name"]
    assert body["project_id"] == 1
    assert len(body["steps"]) == 6
    assert "Location" in resp.headers
    assert str(body["id"]) in resp.headers["Location"]


def test_execute_login_case_and_verify_results(client: TestClient) -> None:
    """执行登录测试用例，验证结果状态为 passed。"""
    # 创建用例
    payload = {**LOGIN_CASE_DSL, "project_id": 1}
    create_resp = client.post(CASES_URL, json=payload)
    assert create_resp.status_code == 201
    case_id = create_resp.json()["id"]

    # 执行用例
    execute_resp = client.post(
        f"/api/v1/cases/{case_id}/execute",
        json={},
    )
    assert execute_resp.status_code == 200

    body = execute_resp.json()
    assert body["status"] == "passed"
    assert body["case_id"] == case_id
    assert body["total_steps"] == 6
    assert body["failed_step_index"] is None

    # 验证步骤证据
    report = body.get("report")
    assert report is not None
    assert len(report["steps"]) == 6
    for step in report["steps"]:
        assert step["status"] == "passed"


def test_full_api_chain_e2e(anonymous_client: TestClient) -> None:
    """完整链路端到端：登录 → 创建用例 → 执行 → 查看结果。"""
    # Step 1: 未登录应返回 401
    resp = anonymous_client.get(ME_URL)
    assert resp.status_code == 401

    # Step 2: 登录
    login_resp = anonymous_client.post(
        LOGIN_URL,
        json={"email": SEED_EMAIL, "password": SEED_PASSWORD},
    )
    assert login_resp.status_code == 200
    assert login_resp.json()["email"] == SEED_EMAIL

    # Step 3: 验证会话
    me_resp = anonymous_client.get(ME_URL)
    assert me_resp.status_code == 200
    assert me_resp.json()["email"] == SEED_EMAIL

    # Step 4: 创建测试用例
    payload = {**LOGIN_CASE_DSL, "project_id": 1}
    create_resp = anonymous_client.post(CASES_URL, json=payload)
    assert create_resp.status_code == 201
    case_id = create_resp.json()["id"]

    # Step 5: 执行用例
    execute_resp = anonymous_client.post(
        f"/api/v1/cases/{case_id}/execute",
        json={},
    )
    assert execute_resp.status_code == 200
    execution_id = execute_resp.json()["id"]
    assert execute_resp.json()["status"] == "passed"

    # Step 6: 查看执行结果
    detail_resp = anonymous_client.get(f"/api/v1/executions/{execution_id}")
    assert detail_resp.status_code == 200
    detail = detail_resp.json()
    assert detail["status"] == "passed"
    assert detail["case_id"] == case_id
    assert detail["report"] is not None
    assert len(detail["report"]["steps"]) == 6
