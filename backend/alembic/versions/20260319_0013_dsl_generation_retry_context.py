"""Add retry context fields to AI DSL generation runs."""

from __future__ import annotations

from alembic import op
import sqlalchemy as sa


revision = "20260319_0013"
down_revision = "20260318_0012"
branch_labels = None
depends_on = None


def upgrade() -> None:
    with op.batch_alter_table("dsl_generation_runs") as batch_op:
        batch_op.add_column(sa.Column("retry_from_generation_id", sa.Integer(), nullable=True))
        batch_op.add_column(sa.Column("retry_reason_code", sa.String(length=64), nullable=True))
        batch_op.add_column(sa.Column("retry_note", sa.String(length=1000), nullable=True))
        batch_op.create_foreign_key(
            "fk_dsl_generation_runs_retry_from_generation_id",
            "dsl_generation_runs",
            ["retry_from_generation_id"],
            ["id"],
            ondelete="SET NULL",
        )
        batch_op.create_index("ix_dsl_generation_runs_retry_from_generation_id", ["retry_from_generation_id"], unique=False)
        batch_op.create_check_constraint(
            "ck_dsl_generation_runs_retry_reason_code",
            "retry_reason_code IS NULL OR retry_reason_code IN ("
            "'wrong_actions', 'invalid_structure', 'context_mismatch', 'bad_contracts', 'other'"
            ")",
        )
        batch_op.create_check_constraint(
            "ck_dsl_generation_runs_retry_context",
            "("
            "retry_from_generation_id IS NULL AND retry_reason_code IS NULL AND retry_note IS NULL"
            ") OR ("
            "retry_from_generation_id IS NOT NULL AND retry_reason_code IS NOT NULL"
            ")",
        )


def downgrade() -> None:
    with op.batch_alter_table("dsl_generation_runs") as batch_op:
        batch_op.drop_constraint("ck_dsl_generation_runs_retry_context", type_="check")
        batch_op.drop_constraint("ck_dsl_generation_runs_retry_reason_code", type_="check")
        batch_op.drop_index("ix_dsl_generation_runs_retry_from_generation_id")
        batch_op.drop_constraint("fk_dsl_generation_runs_retry_from_generation_id", type_="foreignkey")
        batch_op.drop_column("retry_note")
        batch_op.drop_column("retry_reason_code")
        batch_op.drop_column("retry_from_generation_id")
