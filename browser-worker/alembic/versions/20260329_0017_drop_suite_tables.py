"""Drop legacy suite tables."""

from __future__ import annotations

from alembic import op
import sqlalchemy as sa


revision = "20260329_0017"
down_revision = "20260329_0016"
branch_labels = None
depends_on = None


def upgrade() -> None:
    existing_tables = set(sa.inspect(op.get_bind()).get_table_names())

    if "suite_run_items" in existing_tables:
        op.drop_table("suite_run_items")
    if "suite_runs" in existing_tables:
        op.drop_table("suite_runs")
    if "suite_cases" in existing_tables:
        op.drop_table("suite_cases")
    if "test_suites" in existing_tables:
        op.drop_table("test_suites")


def downgrade() -> None:
    existing_tables = set(sa.inspect(op.get_bind()).get_table_names())

    if "test_suites" not in existing_tables:
        op.create_table(
            "test_suites",
            sa.Column("id", sa.Integer(), nullable=False),
            sa.Column("project_id", sa.Integer(), nullable=False),
            sa.Column("created_by", sa.Integer(), nullable=False),
            sa.Column("updated_by", sa.Integer(), nullable=False),
            sa.Column("name", sa.String(length=200), nullable=False),
            sa.Column("description", sa.String(length=1000), nullable=True),
            sa.Column("created_at", sa.DateTime(), server_default=sa.func.now(), nullable=False),
            sa.Column("updated_at", sa.DateTime(), server_default=sa.func.now(), nullable=False),
            sa.ForeignKeyConstraint(
                ["created_by"],
                ["users.id"],
                name=op.f("fk_test_suites_created_by_users"),
                ondelete="RESTRICT",
            ),
            sa.ForeignKeyConstraint(
                ["project_id"],
                ["projects.id"],
                name=op.f("fk_test_suites_project_id_projects"),
                ondelete="RESTRICT",
            ),
            sa.ForeignKeyConstraint(
                ["updated_by"],
                ["users.id"],
                name=op.f("fk_test_suites_updated_by_users"),
                ondelete="RESTRICT",
            ),
            sa.PrimaryKeyConstraint("id", name=op.f("pk_test_suites")),
        )
        op.create_index(op.f("ix_test_suites_created_by"), "test_suites", ["created_by"], unique=False)
        op.create_index(op.f("ix_test_suites_name"), "test_suites", ["name"], unique=False)
        op.create_index(op.f("ix_test_suites_project_id"), "test_suites", ["project_id"], unique=False)
        op.create_index(op.f("ix_test_suites_updated_by"), "test_suites", ["updated_by"], unique=False)

    if "suite_cases" not in existing_tables:
        op.create_table(
            "suite_cases",
            sa.Column("suite_id", sa.Integer(), nullable=False),
            sa.Column("case_id", sa.Integer(), nullable=False),
            sa.Column("order_index", sa.Integer(), nullable=False),
            sa.ForeignKeyConstraint(
                ["case_id"],
                ["test_cases.id"],
                name=op.f("fk_suite_cases_case_id_test_cases"),
                ondelete="CASCADE",
            ),
            sa.ForeignKeyConstraint(
                ["suite_id"],
                ["test_suites.id"],
                name=op.f("fk_suite_cases_suite_id_test_suites"),
                ondelete="CASCADE",
            ),
            sa.PrimaryKeyConstraint("suite_id", "case_id", name=op.f("pk_suite_cases")),
            sa.UniqueConstraint("suite_id", "order_index", name=op.f("uq_suite_cases_suite_id")),
        )

    if "suite_runs" not in existing_tables:
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
            sa.Column("context_source", sa.String(length=50), nullable=False),
            sa.Column("context_source_suite_run_id", sa.Integer(), nullable=True),
            sa.Column("rerun_context_mode", sa.String(length=50), nullable=False),
            sa.Column("context_snapshot", sa.JSON(), nullable=False),
            sa.ForeignKeyConstraint(
                ["context_source_suite_run_id"],
                ["suite_runs.id"],
                name=op.f("fk_suite_runs_context_source_suite_run_id_suite_runs"),
                ondelete="SET NULL",
            ),
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
        op.create_index(op.f("ix_suite_runs_context_source_suite_run_id"), "suite_runs", ["context_source_suite_run_id"], unique=False)
        op.create_index(op.f("ix_suite_runs_source_suite_run_id"), "suite_runs", ["source_suite_run_id"], unique=False)
        op.create_index(op.f("ix_suite_runs_status"), "suite_runs", ["status"], unique=False)
        op.create_index(op.f("ix_suite_runs_suite_id"), "suite_runs", ["suite_id"], unique=False)
        op.create_index(op.f("ix_suite_runs_triggered_by"), "suite_runs", ["triggered_by"], unique=False)

    if "suite_run_items" not in existing_tables:
        op.create_table(
            "suite_run_items",
            sa.Column("id", sa.Integer(), nullable=False),
            sa.Column("suite_run_id", sa.Integer(), nullable=False),
            sa.Column("case_id", sa.Integer(), nullable=False),
            sa.Column("case_name_snapshot", sa.String(length=200), nullable=False),
            sa.Column("order_index", sa.Integer(), nullable=False),
            sa.Column("execution_id", sa.Integer(), nullable=False),
            sa.Column("status", sa.String(length=20), nullable=False),
            sa.Column("context_reads", sa.JSON(), nullable=False),
            sa.Column("context_writes", sa.JSON(), nullable=False),
            sa.Column("context_resolution_error", sa.String(length=2000), nullable=True),
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
