"""Tests for cookie-session authentication."""

from __future__ import annotations

import importlib.util
from pathlib import Path

from sqlalchemy import text

from app.core.auth import verify_password


def _load_legacy_password_hash() -> str:
    migration_path = Path(__file__).resolve().parents[2] / "alembic" / "versions" / "20260324_0015_user_auth_baseline.py"
    spec = importlib.util.spec_from_file_location("user_auth_baseline_migration", migration_path)
    assert spec is not None and spec.loader is not None
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module.LEGACY_PASSWORD_HASH


LEGACY_PASSWORD_HASH = _load_legacy_password_hash()


def test_legacy_seed_password_hash_matches_default_password() -> None:
    assert verify_password("password123", LEGACY_PASSWORD_HASH)


def test_login_returns_current_user_and_sets_session_cookie(anonymous_client) -> None:
    response = anonymous_client.post(
        "/api/v1/auth/login",
        json={
            "email": "seed-owner@example.com",
            "password": "password123",
        },
    )

    assert response.status_code == 200
    assert response.json() == {
        "id": 1,
        "email": "seed-owner@example.com",
        "display_name": "Seed Owner",
    }
    assert "session=" in response.headers["set-cookie"]


def test_login_rejects_wrong_password(anonymous_client) -> None:
    response = anonymous_client.post(
        "/api/v1/auth/login",
        json={
            "email": "seed-owner@example.com",
            "password": "wrong-password",
        },
    )

    assert response.status_code == 401
    assert response.json() == {"detail": "邮箱或密码错误。"}


def test_login_rejects_inactive_user(anonymous_client, db_session) -> None:
    db_session.execute(text("UPDATE users SET is_active = 0 WHERE id = 1"))
    db_session.commit()

    response = anonymous_client.post(
        "/api/v1/auth/login",
        json={
            "email": "seed-owner@example.com",
            "password": "password123",
        },
    )

    assert response.status_code == 403
    assert response.json() == {"detail": "当前账号已停用。"}


def test_me_returns_current_user_after_login(anonymous_client) -> None:
    login_response = anonymous_client.post(
        "/api/v1/auth/login",
        json={
            "email": "seed-owner@example.com",
            "password": "password123",
        },
    )
    assert login_response.status_code == 200

    response = anonymous_client.get("/api/v1/auth/me")

    assert response.status_code == 200
    assert response.json() == {
        "id": 1,
        "email": "seed-owner@example.com",
        "display_name": "Seed Owner",
    }


def test_me_returns_401_when_not_logged_in(anonymous_client) -> None:
    response = anonymous_client.get("/api/v1/auth/me")

    assert response.status_code == 401
    assert response.json() == {"detail": "未登录或登录态已失效。"}


def test_logout_clears_session_cookie(anonymous_client) -> None:
    login_response = anonymous_client.post(
        "/api/v1/auth/login",
        json={
            "email": "seed-owner@example.com",
            "password": "password123",
        },
    )
    assert login_response.status_code == 200

    response = anonymous_client.post("/api/v1/auth/logout")

    assert response.status_code == 200
    assert response.json() == {"success": True}
    assert "session=null" in response.headers["set-cookie"]
    assert "expires=thu, 01 jan 1970 00:00:00 gmt" in response.headers["set-cookie"].lower()

    me_response = anonymous_client.get("/api/v1/auth/me")
    assert me_response.status_code == 401


def test_business_routes_require_login(anonymous_client) -> None:
    cases_response = anonymous_client.get("/api/v1/cases")
    executions_response = anonymous_client.get("/api/v1/executions")
    settings_response = anonymous_client.get("/api/v1/settings/ai")

    assert cases_response.status_code == 401
    assert executions_response.status_code == 401
    assert settings_response.status_code == 401
