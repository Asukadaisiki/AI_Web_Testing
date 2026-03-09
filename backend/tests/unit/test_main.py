"""Tests for application startup behavior."""

from __future__ import annotations

from fastapi import FastAPI
from fastapi.testclient import TestClient

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


def test_create_app_serves_artifacts_directory(monkeypatch, tmp_path) -> None:
    monkeypatch.setattr(main_module, "verify_database_connection", lambda: None)
    monkeypatch.setattr(main_module, "ARTIFACTS_DIR", tmp_path)
    tmp_path.mkdir(parents=True, exist_ok=True)
    artifact_file = tmp_path / "executions" / "sample.txt"
    artifact_file.parent.mkdir(parents=True, exist_ok=True)
    artifact_file.write_text("artifact-ok", encoding="utf-8")

    app = main_module.create_app()
    assert isinstance(app, FastAPI)

    with TestClient(app) as client:
        response = client.get("/artifacts/executions/sample.txt")

    assert response.status_code == 200
    assert response.text == "artifact-ok"
