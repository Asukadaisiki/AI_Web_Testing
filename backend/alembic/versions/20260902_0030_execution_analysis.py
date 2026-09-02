"""Persist unified execution failure signals and analysis."""

from __future__ import annotations

from alembic import op
import sqlalchemy as sa


revision = "20260902_0030"
down_revision = "20260902_0029"
branch_labels = None
depends_on = None


def upgrade() -> None:
    with op.batch_alter_table("test_case_runs") as batch_op:
        batch_op.add_column(sa.Column("failure_signal_json", sa.JSON(), nullable=True))
        batch_op.add_column(
            sa.Column("analysis_status", sa.String(length=20), nullable=False, server_default="pending")
        )
        batch_op.add_column(sa.Column("analysis_json", sa.JSON(), nullable=True))
        batch_op.alter_column("analysis_status", server_default=None)

    with op.batch_alter_table("execution_batches") as batch_op:
        batch_op.add_column(
            sa.Column("analysis_status", sa.String(length=20), nullable=False, server_default="pending")
        )
        batch_op.add_column(sa.Column("analysis_json", sa.JSON(), nullable=True))
        batch_op.alter_column("analysis_status", server_default=None)

    with op.batch_alter_table("dsl_anti_patterns") as batch_op:
        batch_op.add_column(sa.Column("failure_category", sa.String(length=32), nullable=True))
        batch_op.create_index(op.f("ix_dsl_anti_patterns_failure_category"), ["failure_category"])


def downgrade() -> None:
    with op.batch_alter_table("dsl_anti_patterns") as batch_op:
        batch_op.drop_index(op.f("ix_dsl_anti_patterns_failure_category"))
        batch_op.drop_column("failure_category")

    with op.batch_alter_table("execution_batches") as batch_op:
        batch_op.drop_column("analysis_json")
        batch_op.drop_column("analysis_status")

    with op.batch_alter_table("test_case_runs") as batch_op:
        batch_op.drop_column("analysis_json")
        batch_op.drop_column("analysis_status")
        batch_op.drop_column("failure_signal_json")
