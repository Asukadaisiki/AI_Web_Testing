"""Drop unused planning flow-step and locator-attempt tables.

Revision ID: 20260905_0036
Revises: 20260905_0035
"""

from __future__ import annotations

from alembic import op
import sqlalchemy as sa


revision = "20260905_0036"
down_revision = "20260905_0035"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.drop_table("ai_planning_flow_steps")
    op.drop_table("locator_attempt_logs")


def downgrade() -> None:
    op.create_table(
        "locator_attempt_logs",
        sa.Column("id", sa.Integer(), nullable=False),
        sa.Column("run_id", sa.Integer(), nullable=False),
        sa.Column("project_id", sa.Integer(), nullable=False),
        sa.Column("step_index", sa.Integer(), nullable=False),
        sa.Column("step_action", sa.String(length=20), nullable=False),
        sa.Column("target_description", sa.String(length=200), nullable=False),
        sa.Column("page_url", sa.String(length=2000), nullable=False),
        sa.Column("page_url_pattern", sa.String(length=500), nullable=False),
        sa.Column("candidates_json", sa.Text(), nullable=False),
        sa.Column("selected_candidate", sa.Text(), nullable=False),
        sa.Column("strategy_used", sa.String(length=50), nullable=False),
        sa.Column("fallback_tier_reached", sa.Integer(), nullable=False),
        sa.Column("pre_features", sa.Text(), nullable=True),
        sa.Column("runtime_features", sa.Text(), nullable=True),
        sa.Column("final_score", sa.Float(), nullable=False),
        sa.Column("action_success", sa.Boolean(), nullable=False),
        sa.Column("postcondition_result", sa.Text(), nullable=True),
        sa.Column("postcondition_passed", sa.Boolean(), nullable=False),
        sa.Column("click_recovery_used", sa.String(length=50), nullable=True),
        sa.Column("overall_success", sa.Boolean(), nullable=False),
        sa.Column("element_type", sa.String(length=50), nullable=True),
        sa.Column("selector_type", sa.String(length=50), nullable=True),
        sa.Column("domain", sa.String(length=200), nullable=True),
        sa.Column("route", sa.String(length=500), nullable=True),
        sa.Column(
            "created_at",
            sa.DateTime(),
            server_default=sa.text("now()"),
            nullable=False,
        ),
        sa.ForeignKeyConstraint(
            ["project_id"],
            ["projects.id"],
            name=op.f("fk_locator_attempt_logs_project_id_projects"),
            ondelete="RESTRICT",
        ),
        sa.ForeignKeyConstraint(
            ["run_id"],
            ["test_case_runs.id"],
            name=op.f("fk_locator_attempt_logs_run_id_test_case_runs"),
            ondelete="CASCADE",
        ),
        sa.PrimaryKeyConstraint("id", name=op.f("pk_locator_attempt_logs")),
    )
    op.create_index(
        "ix_lal_domain_strategy",
        "locator_attempt_logs",
        ["domain", "selector_type"],
        unique=False,
    )
    op.create_index(
        "ix_lal_project_success",
        "locator_attempt_logs",
        ["project_id", "overall_success"],
        unique=False,
    )
    op.create_index(
        "ix_lal_run_step",
        "locator_attempt_logs",
        ["run_id", "step_index"],
        unique=False,
    )

    op.create_table(
        "ai_planning_flow_steps",
        sa.Column("id", sa.Integer(), nullable=False),
        sa.Column("session_id", sa.Integer(), nullable=False),
        sa.Column("scenario_key", sa.String(length=100), nullable=False),
        sa.Column("step_index", sa.Integer(), nullable=False),
        sa.Column("action", sa.String(length=50), nullable=False),
        sa.Column("target", sa.String(length=500), nullable=True),
        sa.Column("value", sa.String(length=1000), nullable=True),
        sa.Column("trigger", sa.String(length=50), nullable=True),
        sa.Column("expected_result", sa.String(length=1000), nullable=True),
        sa.Column("page_url", sa.String(length=1000), nullable=True),
        sa.Column("page_state", sa.String(length=10), nullable=True),
        sa.Column("element_indices", sa.JSON(), nullable=True),
        sa.Column("element_target_keywords", sa.JSON(), nullable=True),
        sa.Column(
            "created_at",
            sa.DateTime(),
            server_default=sa.text("now()"),
            nullable=False,
        ),
        sa.Column(
            "updated_at",
            sa.DateTime(),
            server_default=sa.text("now()"),
            nullable=False,
        ),
        sa.ForeignKeyConstraint(
            ["session_id"],
            ["ai_planning_sessions.id"],
            name=op.f("fk_ai_planning_flow_steps_session_id_ai_planning_sessions"),
            ondelete="CASCADE",
        ),
        sa.PrimaryKeyConstraint("id", name=op.f("pk_ai_planning_flow_steps")),
    )
    op.create_index(
        op.f("ix_ai_planning_flow_steps_scenario_key"),
        "ai_planning_flow_steps",
        ["scenario_key"],
        unique=False,
    )
    op.create_index(
        op.f("ix_ai_planning_flow_steps_session_id"),
        "ai_planning_flow_steps",
        ["session_id"],
        unique=False,
    )
