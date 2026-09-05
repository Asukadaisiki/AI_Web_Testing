"""Keep analysis status compatible with rolling workers."""

from __future__ import annotations

from alembic import op
import sqlalchemy as sa


revision = "20260904_0034"
down_revision = "20260904_0033"
branch_labels = None
depends_on = None


def upgrade() -> None:
    with op.batch_alter_table("test_case_runs") as batch_op:
        batch_op.alter_column(
            "analysis_status",
            existing_type=sa.String(length=20),
            server_default="pending",
            existing_nullable=False,
        )
    with op.batch_alter_table("execution_batches") as batch_op:
        batch_op.alter_column(
            "analysis_status",
            existing_type=sa.String(length=20),
            server_default="pending",
            existing_nullable=False,
        )


def downgrade() -> None:
    with op.batch_alter_table("execution_batches") as batch_op:
        batch_op.alter_column(
            "analysis_status",
            existing_type=sa.String(length=20),
            server_default=None,
            existing_nullable=False,
        )
    with op.batch_alter_table("test_case_runs") as batch_op:
        batch_op.alter_column(
            "analysis_status",
            existing_type=sa.String(length=20),
            server_default=None,
            existing_nullable=False,
        )
