"""Add AgentCore runs and event stream."""

from __future__ import annotations

from alembic import op
import sqlalchemy as sa


revision = "20260904_0032"
down_revision = "20260902_0031"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.create_table(
        "agent_runs",
        sa.Column("id", sa.String(length=64), nullable=False),
        sa.Column("conversation_id", sa.String(length=100), nullable=False),
        sa.Column("project_id", sa.Integer(), nullable=True),
        sa.Column("status", sa.String(length=32), nullable=False),
        sa.Column("input", sa.Text(), nullable=False),
        sa.Column("pending_tool_call_id", sa.String(length=100), nullable=True),
        sa.Column("pending_step_id", sa.String(length=100), nullable=True),
        sa.Column("transcript_json", sa.JSON(), nullable=False),
        sa.Column("last_event_seq", sa.BigInteger(), server_default="0", nullable=False),
        sa.Column("created_at", sa.DateTime(), server_default=sa.text("now()"), nullable=False),
        sa.Column("updated_at", sa.DateTime(), server_default=sa.text("now()"), nullable=False),
        sa.CheckConstraint(
            "status IN ('running', 'waiting_user', 'completed', 'failed', 'cancelled')",
            name=op.f("ck_agent_runs_status"),
        ),
        sa.ForeignKeyConstraint(["project_id"], ["projects.id"], ondelete="SET NULL"),
        sa.PrimaryKeyConstraint("id"),
    )
    op.create_index(op.f("ix_agent_runs_conversation_id"), "agent_runs", ["conversation_id"])
    op.create_index(op.f("ix_agent_runs_project_id"), "agent_runs", ["project_id"])
    op.create_index(op.f("ix_agent_runs_status"), "agent_runs", ["status"])

    op.create_table(
        "agent_events",
        sa.Column("id", sa.BigInteger(), autoincrement=True, nullable=False),
        sa.Column("run_id", sa.String(length=64), nullable=False),
        sa.Column("seq", sa.BigInteger(), nullable=False),
        sa.Column("event_type", sa.String(length=40), nullable=False),
        sa.Column("conversation_id", sa.String(length=100), nullable=False),
        sa.Column("step_id", sa.String(length=100), nullable=True),
        sa.Column("tool_call_id", sa.String(length=100), nullable=True),
        sa.Column("parent_id", sa.String(length=100), nullable=True),
        sa.Column("checkpoint_id", sa.String(length=100), nullable=True),
        sa.Column("payload_json", sa.JSON(), nullable=False),
        sa.Column("created_at", sa.DateTime(), nullable=False),
        sa.ForeignKeyConstraint(["run_id"], ["agent_runs.id"], ondelete="CASCADE"),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint("run_id", "seq", name="uq_agent_events_run_seq"),
    )
    op.create_index(op.f("ix_agent_events_run_id"), "agent_events", ["run_id"])
    op.create_index("ix_agent_events_run_created", "agent_events", ["run_id", "created_at"])


def downgrade() -> None:
    op.drop_index("ix_agent_events_run_created", table_name="agent_events")
    op.drop_index(op.f("ix_agent_events_run_id"), table_name="agent_events")
    op.drop_table("agent_events")
    op.drop_index(op.f("ix_agent_runs_status"), table_name="agent_runs")
    op.drop_index(op.f("ix_agent_runs_project_id"), table_name="agent_runs")
    op.drop_index(op.f("ix_agent_runs_conversation_id"), table_name="agent_runs")
    op.drop_table("agent_runs")
