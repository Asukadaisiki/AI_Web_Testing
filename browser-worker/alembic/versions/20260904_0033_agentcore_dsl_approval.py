"""Bind AgentCore runs to generated and approved DSL versions."""

from __future__ import annotations

from alembic import op
import sqlalchemy as sa


revision = "20260904_0033"
down_revision = "20260904_0032"
branch_labels = None
depends_on = None


def upgrade() -> None:
    with op.batch_alter_table("agent_runs") as batch_op:
        batch_op.add_column(sa.Column("latest_generation_id", sa.Integer(), nullable=True))
        batch_op.add_column(sa.Column("approved_generation_id", sa.Integer(), nullable=True))
        batch_op.create_foreign_key(
            "fk_agent_runs_latest_generation_id",
            "dsl_generation_runs",
            ["latest_generation_id"],
            ["id"],
            ondelete="SET NULL",
        )
        batch_op.create_foreign_key(
            "fk_agent_runs_approved_generation_id",
            "dsl_generation_runs",
            ["approved_generation_id"],
            ["id"],
            ondelete="SET NULL",
        )


def downgrade() -> None:
    with op.batch_alter_table("agent_runs") as batch_op:
        batch_op.drop_constraint(
            "fk_agent_runs_approved_generation_id",
            type_="foreignkey",
        )
        batch_op.drop_constraint(
            "fk_agent_runs_latest_generation_id",
            type_="foreignkey",
        )
        batch_op.drop_column("approved_generation_id")
        batch_op.drop_column("latest_generation_id")
