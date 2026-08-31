"""Seed default planning session for the default project."""

from __future__ import annotations

import json

from alembic import op
import sqlalchemy as sa


revision = "20260831_0027"
down_revision = "20260829_0026"
branch_labels = None
depends_on = None


DEFAULT_USER_ID = 1
DEFAULT_PROJECT_ID = 1
DEFAULT_SESSION_TITLE = "默认规划会话"
REQUIRED_REQUIREMENT_SLOTS = [
    "app_under_test",
    "business_goal",
    "entry_url_or_page",
    "core_user_flow",
    "main_assertions",
    "test_data_or_account",
    "scope_limits",
]


def upgrade() -> None:
    bind = op.get_bind()
    bind.execute(
        sa.text(
            """
            UPDATE projects
            SET is_default = TRUE
            WHERE id = :project_id
            """
        ),
        {"project_id": DEFAULT_PROJECT_ID},
    )
    default_project_id = bind.execute(
        sa.text(
            """
            SELECT p.id
            FROM projects AS p
            JOIN project_members AS pm ON pm.project_id = p.id
            WHERE p.id = :project_id AND pm.user_id = :user_id
            LIMIT 1
            """
        ),
        {"project_id": DEFAULT_PROJECT_ID, "user_id": DEFAULT_USER_ID},
    ).scalar()

    if default_project_id is not None:
        default_session_id = bind.execute(
            sa.text(
                """
                SELECT s.id
                FROM ai_planning_sessions AS s
                JOIN session_projects AS sp ON sp.session_id = s.id
                WHERE s.actor_user_id = :user_id AND sp.project_id = :project_id
                ORDER BY s.id ASC
                LIMIT 1
                """
            ),
            {"project_id": default_project_id, "user_id": DEFAULT_USER_ID},
        ).scalar()

        if default_session_id is None:
            default_session_id = _insert_default_session(bind, default_project_id)
            bind.execute(
                sa.text(
                    """
                    INSERT INTO session_projects (session_id, project_id)
                    VALUES (:session_id, :project_id)
                    """
                ),
                {"session_id": default_session_id, "project_id": default_project_id},
            )

        bind.execute(
            sa.text(
                """
                UPDATE ai_planning_sessions
                SET active_project_id = :project_id
                WHERE id = :session_id
                """
            ),
            {"session_id": default_session_id, "project_id": default_project_id},
        )

    if bind.dialect.name == "postgresql":
        _sync_postgresql_sequences()


def downgrade() -> None:
    bind = op.get_bind()
    default_session_id = bind.execute(
        sa.text(
            """
            SELECT s.id
            FROM ai_planning_sessions AS s
            JOIN session_projects AS sp ON sp.session_id = s.id
            WHERE s.actor_user_id = :user_id
              AND sp.project_id = :project_id
              AND s.title = :title
              AND NOT EXISTS (
                  SELECT 1 FROM ai_planning_messages AS m WHERE m.session_id = s.id
              )
              AND NOT EXISTS (
                  SELECT 1 FROM ai_planning_drafts AS d WHERE d.session_id = s.id
              )
            ORDER BY s.id ASC
            LIMIT 1
            """
        ),
        {
            "project_id": DEFAULT_PROJECT_ID,
            "title": DEFAULT_SESSION_TITLE,
            "user_id": DEFAULT_USER_ID,
        },
    ).scalar()

    if default_session_id is not None:
        bind.execute(
            sa.text("DELETE FROM ai_planning_sessions WHERE id = :session_id"),
            {"session_id": default_session_id},
        )


def _insert_default_session(bind, default_project_id: int) -> int:
    requirements_json = json.dumps(
        {
            "app_under_test": None,
            "business_goal": None,
            "entry_url_or_page": None,
            "core_user_flow": None,
            "main_assertions": [],
            "test_data_or_account": None,
            "scope_limits": None,
            "test_context": None,
        }
    )
    missing_slots_json = json.dumps(REQUIRED_REQUIREMENT_SLOTS)

    if bind.dialect.name == "postgresql":
        return bind.execute(
            sa.text(
                """
                INSERT INTO ai_planning_sessions (
                    actor_user_id,
                    active_project_id,
                    title,
                    status,
                    requirements_json,
                    missing_slots_json
                )
                VALUES (
                    :user_id,
                    :project_id,
                    :title,
                    'collecting',
                    CAST(:requirements_json AS JSON),
                    CAST(:missing_slots_json AS JSON)
                )
                RETURNING id
                """
            ),
            {
                "missing_slots_json": missing_slots_json,
                "project_id": default_project_id,
                "requirements_json": requirements_json,
                "title": DEFAULT_SESSION_TITLE,
                "user_id": DEFAULT_USER_ID,
            },
        ).scalar_one()

    return bind.execute(
        sa.text(
            """
            INSERT INTO ai_planning_sessions (
                actor_user_id,
                active_project_id,
                title,
                status,
                requirements_json,
                missing_slots_json
            )
            VALUES (
                :user_id,
                :project_id,
                :title,
                'collecting',
                :requirements_json,
                :missing_slots_json
            )
            RETURNING id
            """
        ),
        {
            "missing_slots_json": missing_slots_json,
            "project_id": default_project_id,
            "requirements_json": requirements_json,
            "title": DEFAULT_SESSION_TITLE,
            "user_id": DEFAULT_USER_ID,
        },
    ).scalar_one()


def _sync_postgresql_sequences() -> None:
    for table_name in (
        "users",
        "projects",
        "project_members",
        "ai_planning_sessions",
        "session_projects",
    ):
        op.execute(
            f"""
            SELECT setval(
                pg_get_serial_sequence('{table_name}', 'id'),
                COALESCE((SELECT MAX(id) FROM {table_name}), 1),
                (SELECT MAX(id) IS NOT NULL FROM {table_name})
            )
            """
        )
