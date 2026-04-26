"""add exploration_runs and failure_records tables

Revision ID: 20260426_0022
Revises: 20260426_0021
"""

from __future__ import annotations

from alembic import op
import sqlalchemy as sa

revision = "20260426_0022"
down_revision = "20260426_0021"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.create_table(
        "exploration_runs",
        sa.Column("id", sa.Integer(), primary_key=True),
        sa.Column("session_id", sa.Integer(), sa.ForeignKey("ai_planning_sessions.id", ondelete="CASCADE"), nullable=False),
        sa.Column("case_id", sa.Integer(), sa.ForeignKey("test_cases.id", ondelete="CASCADE"), nullable=False),
        sa.Column("case_run_id", sa.Integer(), sa.ForeignKey("test_case_runs.id", ondelete="SET NULL"), nullable=True),
        sa.Column("role", sa.String(20), nullable=False, server_default="explorer"),
        sa.Column("status", sa.String(20), nullable=False, server_default="running"),
        sa.Column("failure_records_json", sa.JSON(), nullable=True),
        sa.Column("judge_conclusions_json", sa.JSON(), nullable=True),
        sa.Column("router_decision_json", sa.JSON(), nullable=True),
        sa.Column("auto_fix_attempted", sa.Boolean(), nullable=False, server_default=sa.text("false")),
        sa.Column("created_at", sa.DateTime(), server_default=sa.func.now(), nullable=False),
    )
    op.create_index("ix_exploration_runs_session_id", "exploration_runs", ["session_id"])
    op.create_index("ix_exploration_runs_case_id", "exploration_runs", ["case_id"])

    op.create_table(
        "failure_records",
        sa.Column("id", sa.Integer(), primary_key=True),
        sa.Column("exploration_run_id", sa.Integer(), sa.ForeignKey("exploration_runs.id", ondelete="CASCADE"), nullable=False),
        sa.Column("step_index", sa.Integer(), nullable=False),
        sa.Column("action", sa.String(50), nullable=False),
        sa.Column("target", sa.String(500), nullable=True),
        sa.Column("value", sa.String(500), nullable=True),
        sa.Column("error_message", sa.Text(), nullable=False),
        sa.Column("evidence_json", sa.JSON(), nullable=True),
        sa.Column("classification", sa.String(50), nullable=True),
        sa.Column("retry_count", sa.Integer(), nullable=False, server_default=sa.text("0")),
    )
    op.create_index("ix_failure_records_exploration_run_id", "failure_records", ["exploration_run_id"])


def downgrade() -> None:
    op.drop_index("ix_failure_records_exploration_run_id", table_name="failure_records")
    op.drop_table("failure_records")
    op.drop_index("ix_exploration_runs_case_id", table_name="exploration_runs")
    op.drop_index("ix_exploration_runs_session_id", table_name="exploration_runs")
    op.drop_table("exploration_runs")
