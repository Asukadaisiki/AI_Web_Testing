"""Add feedback fields to AI DSL generation runs."""

from __future__ import annotations

from alembic import op
import sqlalchemy as sa


revision = "20260317_0010"
down_revision = "20260316_0009"
branch_labels = None
depends_on = None


def upgrade() -> None:
    with op.batch_alter_table("dsl_generation_runs") as batch_op:
        batch_op.add_column(
            sa.Column(
                "feedback_status",
                sa.String(length=32),
                nullable=False,
                server_default=sa.text("'pending'"),
            )
        )
        batch_op.add_column(sa.Column("feedback_import_mode", sa.String(length=32), nullable=True))
        batch_op.add_column(sa.Column("feedback_recorded_at", sa.DateTime(), nullable=True))
        batch_op.create_check_constraint(
            "ck_dsl_generation_runs_feedback_status",
            "feedback_status IN ('pending', 'accepted', 'rejected')",
        )
        batch_op.create_check_constraint(
            "ck_dsl_generation_runs_feedback_import_mode",
            "("
            "feedback_status = 'accepted' AND feedback_import_mode IN ('replace', 'steps_only', 'contracts_only')"
            ") OR ("
            "feedback_status IN ('pending', 'rejected') AND feedback_import_mode IS NULL"
            ")",
        )
        batch_op.alter_column("feedback_status", server_default=None)


def downgrade() -> None:
    with op.batch_alter_table("dsl_generation_runs") as batch_op:
        batch_op.drop_constraint("ck_dsl_generation_runs_feedback_import_mode", type_="check")
        batch_op.drop_constraint("ck_dsl_generation_runs_feedback_status", type_="check")
        batch_op.drop_column("feedback_recorded_at")
        batch_op.drop_column("feedback_import_mode")
        batch_op.drop_column("feedback_status")
