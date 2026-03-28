"""Regression tests for the locator correction normalization repair migration."""

from __future__ import annotations

import importlib.util
from pathlib import Path

from alembic.migration import MigrationContext
from alembic.operations import Operations
from sqlalchemy import create_engine, text
from sqlalchemy.orm import Session

from app.core.auth import hash_password
from app.db import Base
import app.models  # noqa: F401
from app.locators.corrections import find_active_correction


def test_upgrade_0007_repairs_normalized_targets_and_deduplicates_active_records(tmp_path) -> None:
    database_path = tmp_path / "migration.db"
    database_url = f"sqlite:///{database_path.as_posix()}"
    engine = create_engine(database_url, future=True, connect_args={"check_same_thread": False})
    Base.metadata.create_all(bind=engine)

    try:
        with engine.begin() as connection:
            connection.execute(
                text(
                    """
                    INSERT INTO users (id, email, display_name, password_hash, is_active)
                    VALUES (1, 'seed-owner@example.com', 'Seed Owner', :password_hash, 1)
                    """
                ),
                {"password_hash": hash_password("password123")},
            )
            connection.execute(
                text(
                    """
                    INSERT INTO projects (id, name, description)
                    VALUES (1, 'Default Project', 'Seed project for tests.')
                    """
                )
            )
            connection.execute(
                text(
                    """
                    INSERT INTO test_cases (id, project_id, created_by, updated_by, name, description, dsl)
                    VALUES (1, 1, 1, 1, 'Migration Case', NULL, :dsl)
                    """
                ),
                {"dsl": '{"name":"Migration Case","steps":[{"action":"click","target":"Login Button"}]}'},
            )
            connection.execute(
                text(
                    """
                    INSERT INTO test_case_runs (id, case_id, project_id, triggered_by, status, error_message)
                    VALUES (1, 1, 1, 1, 'failed', 'boom')
                    """
                )
            )
            connection.execute(
                text(
                    """
                    INSERT INTO locator_corrections (
                        id,
                        page_url_pattern,
                        target_description,
                        normalized_target_description,
                        correction_type,
                        correction_value,
                        verified_count,
                        consecutive_failures,
                        is_active,
                        source_execution_id,
                        created_by,
                        created_at,
                        updated_at
                    ) VALUES
                        (
                            1,
                            'https://app.example.com/orders/*',
                            'Login   Button',
                            'login   button',
                            'css',
                            '#login-primary',
                            0,
                            0,
                            1,
                            1,
                            1,
                            '2026-03-15 10:00:00',
                            '2026-03-15 10:00:00'
                        ),
                        (
                            2,
                            'https://app.example.com/orders/*',
                            'Login Button',
                            'login button',
                            'xpath',
                            '//button[@id="login-secondary"]',
                            0,
                            0,
                            1,
                            1,
                            1,
                            '2026-03-15 11:00:00',
                            '2026-03-15 11:00:00'
                        )
                    """
                )
            )

            context = MigrationContext.configure(connection)
            with Operations.context(context):
                _load_migration_module().upgrade()

        with engine.connect() as connection:
            rows = connection.execute(
                text(
                    """
                    SELECT id, normalized_target_description, is_active
                    FROM locator_corrections
                    ORDER BY id
                    """
                )
            ).mappings().all()

        assert rows == [
            {"id": 1, "normalized_target_description": "login button", "is_active": 0},
            {"id": 2, "normalized_target_description": "login button", "is_active": 1},
        ]

        with Session(engine) as session:
            active = find_active_correction(
                session,
                page_url="https://app.example.com/orders/456",
                target_description=" Login   Button ",
            )

        assert active is not None
        assert active.id == 2
        assert active.normalized_target_description == "login button"
    finally:
        engine.dispose()


def _load_migration_module():
    migration_path = (
        Path(__file__).resolve().parents[2]
        / "alembic"
        / "versions"
        / "20260315_0007_locator_correction_normalization_fix.py"
    )
    spec = importlib.util.spec_from_file_location("migration_20260315_0007", migration_path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"Unable to load migration module from {migration_path}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module
