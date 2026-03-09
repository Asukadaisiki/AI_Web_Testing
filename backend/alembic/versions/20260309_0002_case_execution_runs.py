"""Add case execution run table."""

from __future__ import annotations

from alembic import op
import sqlalchemy as sa


revision = "20260309_0002"
down_revision = "20260309_0001"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.create_table(
        "test_case_runs",
        sa.Column("id", sa.Integer(), nullable=False),
        sa.Column("case_id", sa.Integer(), nullable=False),
        sa.Column("project_id", sa.Integer(), nullable=False),
        sa.Column("triggered_by", sa.Integer(), nullable=False),
        sa.Column("status", sa.String(length=20), nullable=False),
        sa.Column("error_message", sa.String(length=2000), nullable=True),
        sa.Column("report", sa.JSON(), nullable=True),
        sa.Column("started_at", sa.DateTime(), server_default=sa.func.now(), nullable=False),
        sa.Column("finished_at", sa.DateTime(), nullable=True),
        sa.ForeignKeyConstraint(
            ["case_id"],
            ["test_cases.id"],
            name=op.f("fk_test_case_runs_case_id_test_cases"),
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["project_id"],
            ["projects.id"],
            name=op.f("fk_test_case_runs_project_id_projects"),
            ondelete="RESTRICT",
        ),
        sa.ForeignKeyConstraint(
            ["triggered_by"],
            ["users.id"],
            name=op.f("fk_test_case_runs_triggered_by_users"),
            ondelete="RESTRICT",
        ),
        sa.PrimaryKeyConstraint("id", name=op.f("pk_test_case_runs")),
    )
    op.create_index(op.f("ix_test_case_runs_case_id"), "test_case_runs", ["case_id"], unique=False)
    op.create_index(op.f("ix_test_case_runs_project_id"), "test_case_runs", ["project_id"], unique=False)
    op.create_index(op.f("ix_test_case_runs_status"), "test_case_runs", ["status"], unique=False)
    op.create_index(op.f("ix_test_case_runs_triggered_by"), "test_case_runs", ["triggered_by"], unique=False)


def downgrade() -> None:
    op.drop_index(op.f("ix_test_case_runs_triggered_by"), table_name="test_case_runs")
    op.drop_index(op.f("ix_test_case_runs_status"), table_name="test_case_runs")
    op.drop_index(op.f("ix_test_case_runs_project_id"), table_name="test_case_runs")
    op.drop_index(op.f("ix_test_case_runs_case_id"), table_name="test_case_runs")
    op.drop_table("test_case_runs")
