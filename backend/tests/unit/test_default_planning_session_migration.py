"""Regression tests for the default planning session seed migration."""

from __future__ import annotations

import importlib.util
from pathlib import Path

from alembic.migration import MigrationContext
from alembic.operations import Operations
from sqlalchemy import create_engine, text


def test_default_planning_session_migration_seeds_and_links_default_project(tmp_path) -> None:
    engine = create_engine(f"sqlite:///{(tmp_path / 'default-planning-session.db').as_posix()}", future=True)

    try:
        with engine.begin() as connection:
            _create_schema(connection)
            connection.execute(
                text("INSERT INTO users (id, email, display_name, is_active) VALUES (1, 'seed@example.com', 'Seed', 1)")
            )
            connection.execute(
                text("INSERT INTO projects (id, name, description, is_default) VALUES (1, 'Default Project', 'seed', 1)")
            )
            connection.execute(
                text("INSERT INTO project_members (id, project_id, user_id, role) VALUES (1, 1, 1, 'owner')")
            )

            context = MigrationContext.configure(connection)
            migration = _load_migration_module()
            assert migration.revision == "20260831_0027"
            assert migration.down_revision == "20260829_0026"

            with Operations.context(context):
                migration.upgrade()

            session_row = connection.execute(
                text(
                    """
                    SELECT id, actor_user_id, active_project_id, title, status
                    FROM ai_planning_sessions
                    """
                )
            ).one()
            assert session_row.actor_user_id == 1
            assert session_row.active_project_id == 1
            assert session_row.title == "默认规划会话"
            assert session_row.status == "collecting"

            assert connection.execute(
                text(
                    """
                    SELECT COUNT(*)
                    FROM session_projects
                    WHERE session_id = :session_id AND project_id = 1
                    """
                ),
                {"session_id": session_row.id},
            ).scalar_one() == 1

            with Operations.context(context):
                migration.upgrade()

            assert connection.execute(
                text("SELECT COUNT(*) FROM ai_planning_sessions")
            ).scalar_one() == 1

            with Operations.context(context):
                migration.downgrade()

            assert connection.execute(
                text("SELECT COUNT(*) FROM ai_planning_sessions")
            ).scalar_one() == 0
    finally:
        engine.dispose()


def _create_schema(connection) -> None:
    connection.execute(
        text(
            """
            CREATE TABLE users (
                id INTEGER PRIMARY KEY,
                email VARCHAR(255) NOT NULL,
                display_name VARCHAR(255) NOT NULL,
                is_active BOOLEAN NOT NULL DEFAULT 1
            )
            """
        )
    )
    connection.execute(
        text(
            """
            CREATE TABLE projects (
                id INTEGER PRIMARY KEY,
                name VARCHAR(200) NOT NULL,
                description VARCHAR(1000),
                is_default BOOLEAN NOT NULL DEFAULT 0
            )
            """
        )
    )
    connection.execute(
        text(
            """
            CREATE TABLE project_members (
                id INTEGER PRIMARY KEY,
                project_id INTEGER NOT NULL,
                user_id INTEGER NOT NULL,
                role VARCHAR(50) NOT NULL
            )
            """
        )
    )
    connection.execute(
        text(
            """
            CREATE TABLE ai_planning_sessions (
                id INTEGER PRIMARY KEY,
                actor_user_id INTEGER NOT NULL,
                active_project_id INTEGER,
                case_id INTEGER,
                title VARCHAR(200),
                status VARCHAR(32) NOT NULL,
                requirements_json JSON NOT NULL,
                plan_json JSON,
                missing_slots_json JSON NOT NULL,
                last_error_message TEXT,
                created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
                updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
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
                project_id INTEGER NOT NULL,
                created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
            )
            """
        )
    )
    connection.execute(
        text("CREATE TABLE ai_planning_messages (id INTEGER PRIMARY KEY, session_id INTEGER NOT NULL)")
    )
    connection.execute(
        text("CREATE TABLE ai_planning_drafts (id INTEGER PRIMARY KEY, session_id INTEGER NOT NULL)")
    )


def _load_migration_module():
    migration_path = (
        Path(__file__).resolve().parents[2]
        / "alembic"
        / "versions"
        / "20260831_0027_seed_default_planning_session.py"
    )
    spec = importlib.util.spec_from_file_location("migration_20260831_0027", migration_path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"Unable to load migration module from {migration_path}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module
