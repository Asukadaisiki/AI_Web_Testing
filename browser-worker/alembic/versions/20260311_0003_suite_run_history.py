"""Add suite run history tables."""

from __future__ import annotations

from alembic import op
import sqlalchemy as sa


revision = "20260311_0003"
down_revision = "20260309_0002"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.create_table(
        "suite_runs",
        sa.Column("id", sa.Integer(), nullable=False),
        sa.Column("suite_id", sa.Integer(), nullable=False),
        sa.Column("triggered_by", sa.Integer(), nullable=False),
        sa.Column("source", sa.String(length=50), nullable=False),
        sa.Column("source_suite_run_id", sa.Integer(), nullable=True),
        sa.Column("status", sa.String(length=20), nullable=False),
        sa.Column("total_cases", sa.Integer(), nullable=False),
        sa.Column("passed_cases", sa.Integer(), nullable=False),
        sa.Column("failed_cases", sa.Integer(), nullable=False),
        sa.Column("base_url_override", sa.String(length=500), nullable=True),
        sa.Column("started_at", sa.DateTime(), server_default=sa.func.now(), nullable=False),
        sa.Column("finished_at", sa.DateTime(), nullable=True),
        sa.ForeignKeyConstraint(
            ["source_suite_run_id"],
            ["suite_runs.id"],
            name=op.f("fk_suite_runs_source_suite_run_id_suite_runs"),
            ondelete="SET NULL",
        ),
        sa.ForeignKeyConstraint(
            ["suite_id"],
            ["test_suites.id"],
            name=op.f("fk_suite_runs_suite_id_test_suites"),
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["triggered_by"],
            ["users.id"],
            name=op.f("fk_suite_runs_triggered_by_users"),
            ondelete="RESTRICT",
        ),
        sa.PrimaryKeyConstraint("id", name=op.f("pk_suite_runs")),
    )
    op.create_index(op.f("ix_suite_runs_source_suite_run_id"), "suite_runs", ["source_suite_run_id"], unique=False)
    op.create_index(op.f("ix_suite_runs_status"), "suite_runs", ["status"], unique=False)
    op.create_index(op.f("ix_suite_runs_suite_id"), "suite_runs", ["suite_id"], unique=False)
    op.create_index(op.f("ix_suite_runs_triggered_by"), "suite_runs", ["triggered_by"], unique=False)

    op.create_table(
        "suite_run_items",
        sa.Column("id", sa.Integer(), nullable=False),
        sa.Column("suite_run_id", sa.Integer(), nullable=False),
        sa.Column("case_id", sa.Integer(), nullable=False),
        sa.Column("case_name_snapshot", sa.String(length=200), nullable=False),
        sa.Column("order_index", sa.Integer(), nullable=False),
        sa.Column("execution_id", sa.Integer(), nullable=False),
        sa.Column("status", sa.String(length=20), nullable=False),
        sa.ForeignKeyConstraint(
            ["case_id"],
            ["test_cases.id"],
            name=op.f("fk_suite_run_items_case_id_test_cases"),
            ondelete="RESTRICT",
        ),
        sa.ForeignKeyConstraint(
            ["execution_id"],
            ["test_case_runs.id"],
            name=op.f("fk_suite_run_items_execution_id_test_case_runs"),
            ondelete="RESTRICT",
        ),
        sa.ForeignKeyConstraint(
            ["suite_run_id"],
            ["suite_runs.id"],
            name=op.f("fk_suite_run_items_suite_run_id_suite_runs"),
            ondelete="CASCADE",
        ),
        sa.PrimaryKeyConstraint("id", name=op.f("pk_suite_run_items")),
        sa.UniqueConstraint("execution_id"),
        sa.UniqueConstraint("suite_run_id", "order_index"),
    )
    op.create_index(op.f("ix_suite_run_items_case_id"), "suite_run_items", ["case_id"], unique=False)
    op.create_index(op.f("ix_suite_run_items_status"), "suite_run_items", ["status"], unique=False)
    op.create_index(op.f("ix_suite_run_items_suite_run_id"), "suite_run_items", ["suite_run_id"], unique=False)


def downgrade() -> None:
    op.drop_index(op.f("ix_suite_run_items_suite_run_id"), table_name="suite_run_items")
    op.drop_index(op.f("ix_suite_run_items_status"), table_name="suite_run_items")
    op.drop_index(op.f("ix_suite_run_items_case_id"), table_name="suite_run_items")
    op.drop_table("suite_run_items")

    op.drop_index(op.f("ix_suite_runs_triggered_by"), table_name="suite_runs")
    op.drop_index(op.f("ix_suite_runs_suite_id"), table_name="suite_runs")
    op.drop_index(op.f("ix_suite_runs_status"), table_name="suite_runs")
    op.drop_index(op.f("ix_suite_runs_source_suite_run_id"), table_name="suite_runs")
    op.drop_table("suite_runs")
