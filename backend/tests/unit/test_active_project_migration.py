"""Regression tests for the planning active-project migration."""

from __future__ import annotations

import importlib.util
from pathlib import Path

from alembic.migration import MigrationContext
from alembic.operations import Operations
from sqlalchemy import create_engine, inspect, text


def test_active_project_migration_backfills_first_session_link(tmp_path) -> None:
    engine = create_engine(f"sqlite:///{(tmp_path / 'active-project.db').as_posix()}", future=True)

    try:
        with engine.begin() as connection:
            connection.execute(text("CREATE TABLE projects (id INTEGER PRIMARY KEY)"))
            connection.execute(
                text(
                    """
                    CREATE TABLE ai_planning_sessions (
                        id INTEGER PRIMARY KEY,
                        actor_user_id INTEGER NOT NULL
                    )
                    """
                )
            )
            connection.execute(
                text(
                    """
                    CREATE TABLE session_projects (
                        id INTEGER PRIMARY KEY,
                        session_id INTEGER NOT NULL,
                        project_id INTEGER NOT NULL
                    )
                    """
                )
            )
            connection.execute(text("INSERT INTO projects (id) VALUES (10), (20)"))
            connection.execute(
                text("INSERT INTO ai_planning_sessions (id, actor_user_id) VALUES (1, 1)")
            )
            connection.execute(
                text(
                    """
                    INSERT INTO session_projects (id, session_id, project_id)
                    VALUES (2, 1, 20), (1, 1, 10)
                    """
                )
            )

            context = MigrationContext.configure(connection)
            migration = _load_migration_module()
            with Operations.context(context):
                migration.upgrade()

            assert connection.execute(
                text("SELECT active_project_id FROM ai_planning_sessions WHERE id = 1")
            ).scalar_one() == 10
            assert "active_project_id" in {
                column["name"]
                for column in inspect(connection).get_columns("ai_planning_sessions")
            }

            with Operations.context(context):
                migration.downgrade()

            assert "active_project_id" not in {
                column["name"]
                for column in inspect(connection).get_columns("ai_planning_sessions")
            }
    finally:
        engine.dispose()


def _load_migration_module():
    migration_path = (
        Path(__file__).resolve().parents[2]
        / "alembic"
        / "versions"
        / "20260829_0026_active_planning_project.py"
    )
    spec = importlib.util.spec_from_file_location("migration_20260829_0026", migration_path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"Unable to load migration module from {migration_path}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module
