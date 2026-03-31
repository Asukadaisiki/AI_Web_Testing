"""Regression tests for the suite table removal migration."""

from __future__ import annotations

import importlib.util
from pathlib import Path

from alembic.migration import MigrationContext
from alembic.operations import Operations
from sqlalchemy import create_engine, inspect, text

from app.db import Base
import app.models  # noqa: F401


def test_upgrade_0017_drops_suite_tables_and_downgrade_restores_them(tmp_path) -> None:
    database_path = tmp_path / "suite-drop-migration.db"
    engine = create_engine(f"sqlite:///{database_path.as_posix()}", future=True, connect_args={"check_same_thread": False})
    Base.metadata.create_all(bind=engine)

    try:
        with engine.begin() as connection:
            connection.execute(text("PRAGMA foreign_keys=OFF"))
            connection.execute(
                text(
                    """
                    CREATE TABLE test_suites (
                        id INTEGER NOT NULL PRIMARY KEY,
                        project_id INTEGER NOT NULL,
                        created_by INTEGER NOT NULL,
                        updated_by INTEGER NOT NULL,
                        name VARCHAR(200) NOT NULL,
                        description VARCHAR(1000),
                        created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
                        updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
                        FOREIGN KEY(project_id) REFERENCES projects (id) ON DELETE RESTRICT,
                        FOREIGN KEY(created_by) REFERENCES users (id) ON DELETE RESTRICT,
                        FOREIGN KEY(updated_by) REFERENCES users (id) ON DELETE RESTRICT
                    )
                    """
                )
            )
            connection.execute(text("CREATE INDEX ix_test_suites_project_id ON test_suites (project_id)"))
            connection.execute(text("CREATE INDEX ix_test_suites_created_by ON test_suites (created_by)"))
            connection.execute(text("CREATE INDEX ix_test_suites_updated_by ON test_suites (updated_by)"))
            connection.execute(text("CREATE INDEX ix_test_suites_name ON test_suites (name)"))

            connection.execute(
                text(
                    """
                    CREATE TABLE suite_cases (
                        suite_id INTEGER NOT NULL,
                        case_id INTEGER NOT NULL,
                        order_index INTEGER NOT NULL,
                        PRIMARY KEY (suite_id, case_id),
                        UNIQUE (suite_id, order_index),
                        FOREIGN KEY(suite_id) REFERENCES test_suites (id) ON DELETE CASCADE,
                        FOREIGN KEY(case_id) REFERENCES test_cases (id) ON DELETE CASCADE
                    )
                    """
                )
            )

            connection.execute(
                text(
                    """
                    CREATE TABLE suite_runs (
                        id INTEGER NOT NULL PRIMARY KEY,
                        suite_id INTEGER NOT NULL,
                        triggered_by INTEGER NOT NULL,
                        source VARCHAR(50) NOT NULL,
                        source_suite_run_id INTEGER,
                        status VARCHAR(20) NOT NULL,
                        total_cases INTEGER NOT NULL,
                        passed_cases INTEGER NOT NULL,
                        failed_cases INTEGER NOT NULL,
                        base_url_override VARCHAR(500),
                        started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
                        finished_at DATETIME,
                        context_source VARCHAR(50) NOT NULL,
                        context_source_suite_run_id INTEGER,
                        rerun_context_mode VARCHAR(50) NOT NULL,
                        context_snapshot JSON NOT NULL DEFAULT '{}',
                        FOREIGN KEY(suite_id) REFERENCES test_suites (id) ON DELETE CASCADE,
                        FOREIGN KEY(triggered_by) REFERENCES users (id) ON DELETE RESTRICT,
                        FOREIGN KEY(source_suite_run_id) REFERENCES suite_runs (id) ON DELETE SET NULL,
                        FOREIGN KEY(context_source_suite_run_id) REFERENCES suite_runs (id) ON DELETE SET NULL
                    )
                    """
                )
            )
            connection.execute(text("CREATE INDEX ix_suite_runs_suite_id ON suite_runs (suite_id)"))
            connection.execute(text("CREATE INDEX ix_suite_runs_triggered_by ON suite_runs (triggered_by)"))
            connection.execute(text("CREATE INDEX ix_suite_runs_status ON suite_runs (status)"))
            connection.execute(text("CREATE INDEX ix_suite_runs_source_suite_run_id ON suite_runs (source_suite_run_id)"))
            connection.execute(
                text("CREATE INDEX ix_suite_runs_context_source_suite_run_id ON suite_runs (context_source_suite_run_id)")
            )

            connection.execute(
                text(
                    """
                    CREATE TABLE suite_run_items (
                        id INTEGER NOT NULL PRIMARY KEY,
                        suite_run_id INTEGER NOT NULL,
                        case_id INTEGER NOT NULL,
                        case_name_snapshot VARCHAR(200) NOT NULL,
                        order_index INTEGER NOT NULL,
                        execution_id INTEGER NOT NULL UNIQUE,
                        status VARCHAR(20) NOT NULL,
                        context_reads JSON NOT NULL DEFAULT '[]',
                        context_writes JSON NOT NULL DEFAULT '[]',
                        context_resolution_error VARCHAR(2000),
                        UNIQUE (suite_run_id, order_index),
                        FOREIGN KEY(suite_run_id) REFERENCES suite_runs (id) ON DELETE CASCADE,
                        FOREIGN KEY(case_id) REFERENCES test_cases (id) ON DELETE RESTRICT,
                        FOREIGN KEY(execution_id) REFERENCES test_case_runs (id) ON DELETE RESTRICT
                    )
                    """
                )
            )
            connection.execute(text("CREATE INDEX ix_suite_run_items_suite_run_id ON suite_run_items (suite_run_id)"))
            connection.execute(text("CREATE INDEX ix_suite_run_items_case_id ON suite_run_items (case_id)"))
            connection.execute(text("CREATE INDEX ix_suite_run_items_status ON suite_run_items (status)"))

            before_upgrade = set(inspect(connection).get_table_names())
            assert {"test_suites", "suite_cases", "suite_runs", "suite_run_items"}.issubset(before_upgrade)

            context = MigrationContext.configure(connection)
            with Operations.context(context):
                migration = _load_migration_module()
                migration.upgrade()

            after_upgrade = set(inspect(connection).get_table_names())
            assert "test_suites" not in after_upgrade
            assert "suite_cases" not in after_upgrade
            assert "suite_runs" not in after_upgrade
            assert "suite_run_items" not in after_upgrade

            with Operations.context(context):
                migration.downgrade()

            after_downgrade = set(inspect(connection).get_table_names())
            assert {"test_suites", "suite_cases", "suite_runs", "suite_run_items"}.issubset(after_downgrade)

            suite_run_columns = {column["name"] for column in inspect(connection).get_columns("suite_runs")}
            assert {"context_source", "context_source_suite_run_id", "rerun_context_mode", "context_snapshot"}.issubset(
                suite_run_columns
            )

            suite_run_item_columns = {column["name"] for column in inspect(connection).get_columns("suite_run_items")}
            assert {"context_reads", "context_writes", "context_resolution_error"}.issubset(suite_run_item_columns)
    finally:
        engine.dispose()


def _load_migration_module():
    migration_path = (
        Path(__file__).resolve().parents[2]
        / "alembic"
        / "versions"
        / "20260329_0017_drop_suite_tables.py"
    )
    spec = importlib.util.spec_from_file_location("migration_20260329_0017", migration_path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"Unable to load migration module from {migration_path}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module
