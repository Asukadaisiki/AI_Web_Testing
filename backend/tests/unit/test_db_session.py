"""Tests for database configuration helpers."""

from __future__ import annotations

from sqlalchemy import text

from app.db.session import get_session_factory


def test_session_factory_uses_database_url_from_env(monkeypatch) -> None:
    monkeypatch.setenv("DATABASE_URL", "sqlite:///:memory:")

    session_factory = get_session_factory()

    with session_factory() as session:
        result = session.execute(text("SELECT 1")).scalar_one()

    assert result == 1
