"""Tests for application startup behavior."""

from __future__ import annotations

from fastapi import FastAPI

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
