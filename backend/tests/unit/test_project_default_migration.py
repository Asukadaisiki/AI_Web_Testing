"""Regression tests for the project default flag migration."""

from __future__ import annotations

import importlib.util
from pathlib import Path

from alembic.migration import MigrationContext
from alembic.operations import Operations
from sqlalchemy import create_engine, inspect, text


def test_project_default_migration_upgrades_existing_rows_and_downgrades(tmp_path) -> None:
    engine = create_engine(f"sqlite:///{(tmp_path / 'project-default.db').as_posix()}", future=True)

    try:
        with engine.begin() as connection:
            connection.execute(
                text(
                    """
                    CREATE TABLE projects (
                        id INTEGER NOT NULL PRIMARY KEY,
                        name VARCHAR(200) NOT NULL,
                        description VARCHAR(1000),
                        created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
                        updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
                    )
                    """
                )
            )
            connection.execute(
                text("INSERT INTO projects (id, name) VALUES (1, 'Existing Project')")
            )

            context = MigrationContext.configure(connection)
            migration = _load_migration_module()
            assert migration.revision == "45061d8892d7"
            assert migration.down_revision == "1c65d6ff37db"

            with Operations.context(context):
                migration.upgrade()

            columns = {column["name"]: column for column in inspect(connection).get_columns("projects")}
            assert columns["is_default"]["nullable"] is False
            assert connection.execute(
                text("SELECT is_default FROM projects WHERE id = 1")
            ).scalar_one() == 0

            connection.execute(text("INSERT INTO projects (id, name) VALUES (2, 'New Project')"))
            assert connection.execute(
                text("SELECT is_default FROM projects WHERE id = 2")
            ).scalar_one() == 0

            with Operations.context(context):
                migration.downgrade()

            assert "is_default" not in {
                column["name"] for column in inspect(connection).get_columns("projects")
            }
    finally:
        engine.dispose()


def _load_migration_module():
    migration_path = (
        Path(__file__).resolve().parents[2]
        / "alembic"
        / "versions"
        / "45061d8892d7_add_is_default_to_projects.py"
    )
    spec = importlib.util.spec_from_file_location("migration_45061d8892d7", migration_path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"Unable to load migration module from {migration_path}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module
