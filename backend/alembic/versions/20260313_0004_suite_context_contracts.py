"""Add suite context contract fields."""

from __future__ import annotations

from alembic import op
import sqlalchemy as sa


revision = "20260313_0004"
down_revision = "20260311_0003"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.add_column("suite_runs", sa.Column("context_source", sa.String(length=50), nullable=False, server_default="empty"))
    op.add_column("suite_runs", sa.Column("context_source_suite_run_id", sa.Integer(), nullable=True))
    op.add_column(
        "suite_runs",
        sa.Column("rerun_context_mode", sa.String(length=50), nullable=False, server_default="not_applicable"),
    )
    op.add_column("suite_runs", sa.Column("context_snapshot", sa.JSON(), nullable=False, server_default=sa.text("'{}'")))
    op.create_foreign_key(
        op.f("fk_suite_runs_context_source_suite_run_id_suite_runs"),
        "suite_runs",
        "suite_runs",
        ["context_source_suite_run_id"],
        ["id"],
        ondelete="SET NULL",
    )
    op.create_index(
        op.f("ix_suite_runs_context_source_suite_run_id"),
        "suite_runs",
        ["context_source_suite_run_id"],
        unique=False,
    )

    op.add_column("suite_run_items", sa.Column("context_reads", sa.JSON(), nullable=False, server_default="[]"))
    op.add_column("suite_run_items", sa.Column("context_writes", sa.JSON(), nullable=False, server_default="[]"))
    op.add_column("suite_run_items", sa.Column("context_resolution_error", sa.String(length=2000), nullable=True))

    op.alter_column("suite_runs", "context_source", server_default=None)
    op.alter_column("suite_runs", "rerun_context_mode", server_default=None)
    op.alter_column("suite_runs", "context_snapshot", server_default=None)
    op.alter_column("suite_run_items", "context_reads", server_default=None)
    op.alter_column("suite_run_items", "context_writes", server_default=None)


def downgrade() -> None:
    op.drop_column("suite_run_items", "context_resolution_error")
    op.drop_column("suite_run_items", "context_writes")
    op.drop_column("suite_run_items", "context_reads")

    op.drop_index(op.f("ix_suite_runs_context_source_suite_run_id"), table_name="suite_runs")
    op.drop_constraint(op.f("fk_suite_runs_context_source_suite_run_id_suite_runs"), "suite_runs", type_="foreignkey")
    op.drop_column("suite_runs", "context_snapshot")
    op.drop_column("suite_runs", "rerun_context_mode")
    op.drop_column("suite_runs", "context_source_suite_run_id")
    op.drop_column("suite_runs", "context_source")
