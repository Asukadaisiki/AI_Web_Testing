"""Add AI planning session, message, and draft tables."""

from __future__ import annotations

from alembic import op
import sqlalchemy as sa


revision = "20260330_0018"
down_revision = "20260329_0017"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.create_table(
        "ai_planning_sessions",
        sa.Column("id", sa.Integer(), nullable=False),
        sa.Column("actor_user_id", sa.Integer(), nullable=False),
        sa.Column("project_id", sa.Integer(), nullable=False),
        sa.Column("case_id", sa.Integer(), nullable=True),
        sa.Column("title", sa.String(length=200), nullable=True),
        sa.Column("status", sa.String(length=32), nullable=False),
        sa.Column("requirements_json", sa.JSON(), nullable=False),
        sa.Column("plan_json", sa.JSON(), nullable=True),
        sa.Column("missing_slots_json", sa.JSON(), nullable=False),
        sa.Column("last_error_message", sa.Text(), nullable=True),
        sa.Column("created_at", sa.DateTime(), server_default=sa.func.now(), nullable=False),
        sa.Column("updated_at", sa.DateTime(), server_default=sa.func.now(), nullable=False),
        sa.ForeignKeyConstraint(["actor_user_id"], ["users.id"], ondelete="RESTRICT"),
        sa.ForeignKeyConstraint(["project_id"], ["projects.id"], ondelete="RESTRICT"),
        sa.ForeignKeyConstraint(["case_id"], ["test_cases.id"], ondelete="SET NULL"),
        sa.PrimaryKeyConstraint("id"),
    )
    op.create_index(op.f("ix_ai_planning_sessions_actor_user_id"), "ai_planning_sessions", ["actor_user_id"], unique=False)
    op.create_index(op.f("ix_ai_planning_sessions_project_id"), "ai_planning_sessions", ["project_id"], unique=False)
    op.create_index(op.f("ix_ai_planning_sessions_case_id"), "ai_planning_sessions", ["case_id"], unique=False)
    op.create_index(op.f("ix_ai_planning_sessions_created_at"), "ai_planning_sessions", ["created_at"], unique=False)

    op.create_table(
        "ai_planning_messages",
        sa.Column("id", sa.Integer(), nullable=False),
        sa.Column("session_id", sa.Integer(), nullable=False),
        sa.Column("role", sa.String(length=16), nullable=False),
        sa.Column("turn_type", sa.String(length=32), nullable=False),
        sa.Column("content", sa.Text(), nullable=False),
        sa.Column("structured_payload_json", sa.JSON(), nullable=True),
        sa.Column("created_at", sa.DateTime(), server_default=sa.func.now(), nullable=False),
        sa.ForeignKeyConstraint(["session_id"], ["ai_planning_sessions.id"], ondelete="CASCADE"),
        sa.PrimaryKeyConstraint("id"),
    )
    op.create_index(op.f("ix_ai_planning_messages_session_id"), "ai_planning_messages", ["session_id"], unique=False)
    op.create_index(op.f("ix_ai_planning_messages_created_at"), "ai_planning_messages", ["created_at"], unique=False)

    op.create_table(
        "ai_planning_drafts",
        sa.Column("id", sa.Integer(), nullable=False),
        sa.Column("session_id", sa.Integer(), nullable=False),
        sa.Column("scenario_key", sa.String(length=100), nullable=False),
        sa.Column("title", sa.String(length=200), nullable=False),
        sa.Column("status", sa.String(length=32), nullable=False),
        sa.Column("dsl_generation_id", sa.Integer(), nullable=True),
        sa.Column("dsl_case_json", sa.JSON(), nullable=True),
        sa.Column("warnings_json", sa.JSON(), nullable=False),
        sa.Column("normalization_notes_json", sa.JSON(), nullable=False),
        sa.Column("error_message", sa.Text(), nullable=True),
        sa.Column("created_at", sa.DateTime(), server_default=sa.func.now(), nullable=False),
        sa.Column("updated_at", sa.DateTime(), server_default=sa.func.now(), nullable=False),
        sa.ForeignKeyConstraint(["session_id"], ["ai_planning_sessions.id"], ondelete="CASCADE"),
        sa.ForeignKeyConstraint(["dsl_generation_id"], ["dsl_generation_runs.id"], ondelete="SET NULL"),
        sa.PrimaryKeyConstraint("id"),
    )
    op.create_index(op.f("ix_ai_planning_drafts_session_id"), "ai_planning_drafts", ["session_id"], unique=False)
    op.create_index(op.f("ix_ai_planning_drafts_scenario_key"), "ai_planning_drafts", ["scenario_key"], unique=False)
    op.create_index(op.f("ix_ai_planning_drafts_dsl_generation_id"), "ai_planning_drafts", ["dsl_generation_id"], unique=False)
    op.create_index(op.f("ix_ai_planning_drafts_created_at"), "ai_planning_drafts", ["created_at"], unique=False)


def downgrade() -> None:
    op.drop_index(op.f("ix_ai_planning_drafts_created_at"), table_name="ai_planning_drafts")
    op.drop_index(op.f("ix_ai_planning_drafts_dsl_generation_id"), table_name="ai_planning_drafts")
    op.drop_index(op.f("ix_ai_planning_drafts_scenario_key"), table_name="ai_planning_drafts")
    op.drop_index(op.f("ix_ai_planning_drafts_session_id"), table_name="ai_planning_drafts")
    op.drop_table("ai_planning_drafts")

    op.drop_index(op.f("ix_ai_planning_messages_created_at"), table_name="ai_planning_messages")
    op.drop_index(op.f("ix_ai_planning_messages_session_id"), table_name="ai_planning_messages")
    op.drop_table("ai_planning_messages")

    op.drop_index(op.f("ix_ai_planning_sessions_created_at"), table_name="ai_planning_sessions")
    op.drop_index(op.f("ix_ai_planning_sessions_case_id"), table_name="ai_planning_sessions")
    op.drop_index(op.f("ix_ai_planning_sessions_project_id"), table_name="ai_planning_sessions")
    op.drop_index(op.f("ix_ai_planning_sessions_actor_user_id"), table_name="ai_planning_sessions")
    op.drop_table("ai_planning_sessions")
