"""Tests for application startup behavior."""

from __future__ import annotations

from fastapi import FastAPI
from fastapi.testclient import TestClient
import pytest

from app.db import get_db_session
import app.main as main_module


def test_create_app_verifies_database_connection(monkeypatch) -> None:
    called = False

    def fake_verify_database_connection() -> None:
        nonlocal called
        called = True

    monkeypatch.setattr(main_module, "verify_database_connection", fake_verify_database_connection)

    app = main_module.create_app()

    assert isinstance(app, FastAPI)
    assert called is True


def test_create_app_requires_auth_session_secret(monkeypatch) -> None:
    monkeypatch.setattr(main_module, "verify_database_connection", lambda: None)
    monkeypatch.delenv("AUTH_SESSION_SECRET", raising=False)

    with pytest.raises(RuntimeError, match="AUTH_SESSION_SECRET"):
        main_module.create_app()


def test_create_app_creates_storage_states_dir(monkeypatch, reset_cached_state, tmp_path) -> None:
    """create_app should ensure storage_states directory exists."""
    monkeypatch.setattr(main_module, "verify_database_connection", lambda: None)
    monkeypatch.setenv("AUTH_SESSION_SECRET", "test-secret")
    monkeypatch.setenv("STORAGE_STATE_DIR", str(tmp_path / "test_states"))
    from app.core.config import get_settings
    get_settings.cache_clear()
    app = main_module.create_app()
    assert (tmp_path / "test_states").exists()
    assert hasattr(app.state, "storage_states_dir")


def test_create_app_protects_artifacts_directory(monkeypatch, tmp_path, db_session) -> None:
    monkeypatch.setattr(main_module, "verify_database_connection", lambda: None)
    monkeypatch.setattr(main_module, "ARTIFACTS_DIR", tmp_path)
    monkeypatch.setenv("AUTH_SESSION_SECRET", "test-session-secret")
    monkeypatch.setenv("AUTH_SESSION_HTTPS_ONLY", "false")
    tmp_path.mkdir(parents=True, exist_ok=True)
    artifact_file = tmp_path / "executions" / "sample.txt"
    artifact_file.parent.mkdir(parents=True, exist_ok=True)
    artifact_file.write_text("artifact-ok", encoding="utf-8")

    app = main_module.create_app()
    assert isinstance(app, FastAPI)

    def override_get_db_session():
        yield db_session

    app.dependency_overrides[get_db_session] = override_get_db_session

    with TestClient(app) as client:
        anonymous_response = client.get("/artifacts/executions/sample.txt")
        login_response = client.post(
            "/api/v1/auth/login",
            json={"email": "seed-owner@example.com", "password": "password123"},
        )
        assert login_response.status_code == 200
        authenticated_response = client.get("/artifacts/executions/sample.txt")

    assert anonymous_response.status_code == 200
    assert authenticated_response.status_code == 200
    assert anonymous_response.text == "artifact-ok"
