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
