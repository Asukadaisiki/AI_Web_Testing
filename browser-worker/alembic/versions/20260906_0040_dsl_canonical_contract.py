"""Bind approved canonical DSL bytes to formal execution jobs."""

from __future__ import annotations

from alembic import op
import sqlalchemy as sa


revision = "20260906_0040"
down_revision = "20260905_0039"
branch_labels = None
depends_on = None


def upgrade() -> None:
    with op.batch_alter_table("dsl_generation_runs") as batch_op:
        batch_op.add_column(sa.Column("dsl_sha256", sa.String(length=64), nullable=True))
        batch_op.add_column(sa.Column("dsl_canonical_version", sa.String(length=32), nullable=True))
        batch_op.create_index("ix_dsl_generation_runs_dsl_sha256", ["dsl_sha256"])

    with op.batch_alter_table("execution_jobs") as batch_op:
        batch_op.add_column(sa.Column("dsl_snapshot", sa.JSON(), nullable=True))
        batch_op.add_column(sa.Column("dsl_canonical_json", sa.Text(), nullable=True))
        batch_op.add_column(sa.Column("dsl_sha256", sa.String(length=64), nullable=True))
        batch_op.add_column(sa.Column("dsl_canonical_version", sa.String(length=32), nullable=True))


def downgrade() -> None:
    with op.batch_alter_table("execution_jobs") as batch_op:
        batch_op.drop_column("dsl_canonical_version")
        batch_op.drop_column("dsl_sha256")
        batch_op.drop_column("dsl_canonical_json")
        batch_op.drop_column("dsl_snapshot")

    with op.batch_alter_table("dsl_generation_runs") as batch_op:
        batch_op.drop_index("ix_dsl_generation_runs_dsl_sha256")
        batch_op.drop_column("dsl_canonical_version")
        batch_op.drop_column("dsl_sha256")
