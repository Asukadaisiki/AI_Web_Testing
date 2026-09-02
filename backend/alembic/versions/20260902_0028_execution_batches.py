"""Add persistent execution batches and queued case jobs."""

from __future__ import annotations

from alembic import op
import sqlalchemy as sa


revision = "20260902_0028"
down_revision = "20260831_0027"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.create_table(
        "execution_batches",
        sa.Column("id", sa.Integer(), nullable=False),
        sa.Column("project_id", sa.Integer(), nullable=False),
        sa.Column("planning_session_id", sa.Integer(), nullable=True),
        sa.Column("triggered_by", sa.Integer(), nullable=False),
        sa.Column("status", sa.String(length=32), nullable=False),
        sa.Column("idempotency_key", sa.String(length=100), nullable=True),
        sa.Column("concurrency_limit", sa.Integer(), nullable=False),
        sa.Column("input_values_json", sa.JSON(), nullable=False),
        sa.Column("created_at", sa.DateTime(), server_default=sa.func.now(), nullable=False),
        sa.Column("started_at", sa.DateTime(), nullable=True),
        sa.Column("finished_at", sa.DateTime(), nullable=True),
        sa.CheckConstraint(
            "status IN ('pending', 'running', 'passed', 'failed', 'needs_intervention', 'cancelled')",
            name=op.f("ck_execution_batches_status"),
        ),
        sa.CheckConstraint(
            "concurrency_limit >= 1",
            name=op.f("ck_execution_batches_concurrency_limit_positive"),
        ),
        sa.ForeignKeyConstraint(
            ["planning_session_id"],
            ["ai_planning_sessions.id"],
            name=op.f("fk_execution_batches_planning_session_id_ai_planning_sessions"),
            ondelete="SET NULL",
        ),
        sa.ForeignKeyConstraint(
            ["project_id"],
            ["projects.id"],
            name=op.f("fk_execution_batches_project_id_projects"),
            ondelete="RESTRICT",
        ),
        sa.ForeignKeyConstraint(
            ["triggered_by"],
            ["users.id"],
            name=op.f("fk_execution_batches_triggered_by_users"),
            ondelete="RESTRICT",
        ),
        sa.PrimaryKeyConstraint("id", name=op.f("pk_execution_batches")),
        sa.UniqueConstraint(
            "triggered_by",
            "idempotency_key",
            name="uq_execution_batches_actor_idempotency",
        ),
    )
    op.create_index(op.f("ix_execution_batches_created_at"), "execution_batches", ["created_at"])
    op.create_index(op.f("ix_execution_batches_planning_session_id"), "execution_batches", ["planning_session_id"])
    op.create_index(op.f("ix_execution_batches_project_id"), "execution_batches", ["project_id"])
    op.create_index(op.f("ix_execution_batches_status"), "execution_batches", ["status"])
    op.create_index(op.f("ix_execution_batches_triggered_by"), "execution_batches", ["triggered_by"])

    op.create_table(
        "execution_jobs",
        sa.Column("id", sa.Integer(), nullable=False),
        sa.Column("batch_id", sa.Integer(), nullable=False),
        sa.Column("project_id", sa.Integer(), nullable=False),
        sa.Column("case_id", sa.Integer(), nullable=False),
        sa.Column("order_index", sa.Integer(), nullable=False),
        sa.Column("status", sa.String(length=32), nullable=False),
        sa.Column("attempt_count", sa.Integer(), nullable=False),
        sa.Column("max_attempts", sa.Integer(), nullable=False),
        sa.Column("lease_owner", sa.String(length=200), nullable=True),
        sa.Column("lease_expires_at", sa.DateTime(), nullable=True),
        sa.Column("cancel_requested", sa.Boolean(), nullable=False),
        sa.Column("last_error_message", sa.Text(), nullable=True),
        sa.Column("created_at", sa.DateTime(), server_default=sa.func.now(), nullable=False),
        sa.Column("started_at", sa.DateTime(), nullable=True),
        sa.Column("finished_at", sa.DateTime(), nullable=True),
        sa.CheckConstraint(
            "status IN ('pending', 'running', 'passed', 'failed', 'needs_intervention', 'cancelled')",
            name=op.f("ck_execution_jobs_status"),
        ),
        sa.CheckConstraint("attempt_count >= 0", name=op.f("ck_execution_jobs_attempt_count_non_negative")),
        sa.CheckConstraint("max_attempts >= 1", name=op.f("ck_execution_jobs_max_attempts_positive")),
        sa.ForeignKeyConstraint(
            ["batch_id"],
            ["execution_batches.id"],
            name=op.f("fk_execution_jobs_batch_id_execution_batches"),
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["case_id"],
            ["test_cases.id"],
            name=op.f("fk_execution_jobs_case_id_test_cases"),
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["project_id"],
            ["projects.id"],
            name=op.f("fk_execution_jobs_project_id_projects"),
            ondelete="RESTRICT",
        ),
        sa.PrimaryKeyConstraint("id", name=op.f("pk_execution_jobs")),
        sa.UniqueConstraint("batch_id", "case_id", name="uq_execution_jobs_batch_case"),
        sa.UniqueConstraint("batch_id", "order_index", name="uq_execution_jobs_batch_order"),
    )
    op.create_index(op.f("ix_execution_jobs_batch_id"), "execution_jobs", ["batch_id"])
    op.create_index(op.f("ix_execution_jobs_case_id"), "execution_jobs", ["case_id"])
    op.create_index(op.f("ix_execution_jobs_created_at"), "execution_jobs", ["created_at"])
    op.create_index(op.f("ix_execution_jobs_lease_expires_at"), "execution_jobs", ["lease_expires_at"])
    op.create_index(op.f("ix_execution_jobs_lease_owner"), "execution_jobs", ["lease_owner"])
    op.create_index(op.f("ix_execution_jobs_project_id"), "execution_jobs", ["project_id"])
    op.create_index(op.f("ix_execution_jobs_status"), "execution_jobs", ["status"])

    with op.batch_alter_table("test_case_runs") as batch_op:
        batch_op.add_column(sa.Column("batch_id", sa.Integer(), nullable=True))
        batch_op.add_column(sa.Column("job_id", sa.Integer(), nullable=True))
        batch_op.add_column(sa.Column("attempt_number", sa.Integer(), nullable=False, server_default="1"))
        batch_op.add_column(sa.Column("dsl_snapshot", sa.JSON(), nullable=True))
        batch_op.add_column(sa.Column("dsl_sha256", sa.String(length=64), nullable=True))
        batch_op.add_column(
            sa.Column(
                "report_schema_version",
                sa.String(length=32),
                nullable=False,
                server_default="execution.report.v1",
            )
        )
        batch_op.create_foreign_key(
            op.f("fk_test_case_runs_batch_id_execution_batches"),
            "execution_batches",
            ["batch_id"],
            ["id"],
            ondelete="SET NULL",
        )
        batch_op.create_foreign_key(
            op.f("fk_test_case_runs_job_id_execution_jobs"),
            "execution_jobs",
            ["job_id"],
            ["id"],
            ondelete="SET NULL",
        )
        batch_op.create_index(op.f("ix_test_case_runs_batch_id"), ["batch_id"])
        batch_op.create_index(op.f("ix_test_case_runs_job_id"), ["job_id"])
        batch_op.create_index(op.f("ix_test_case_runs_dsl_sha256"), ["dsl_sha256"])
        batch_op.alter_column("attempt_number", server_default=None)
        batch_op.alter_column("report_schema_version", server_default=None)


def downgrade() -> None:
    with op.batch_alter_table("test_case_runs") as batch_op:
        batch_op.drop_index(op.f("ix_test_case_runs_dsl_sha256"))
        batch_op.drop_index(op.f("ix_test_case_runs_job_id"))
        batch_op.drop_index(op.f("ix_test_case_runs_batch_id"))
        batch_op.drop_constraint(op.f("fk_test_case_runs_job_id_execution_jobs"), type_="foreignkey")
        batch_op.drop_constraint(op.f("fk_test_case_runs_batch_id_execution_batches"), type_="foreignkey")
        batch_op.drop_column("report_schema_version")
        batch_op.drop_column("dsl_sha256")
        batch_op.drop_column("dsl_snapshot")
        batch_op.drop_column("attempt_number")
        batch_op.drop_column("job_id")
        batch_op.drop_column("batch_id")

    op.drop_index(op.f("ix_execution_jobs_status"), table_name="execution_jobs")
    op.drop_index(op.f("ix_execution_jobs_project_id"), table_name="execution_jobs")
    op.drop_index(op.f("ix_execution_jobs_lease_owner"), table_name="execution_jobs")
    op.drop_index(op.f("ix_execution_jobs_lease_expires_at"), table_name="execution_jobs")
    op.drop_index(op.f("ix_execution_jobs_created_at"), table_name="execution_jobs")
    op.drop_index(op.f("ix_execution_jobs_case_id"), table_name="execution_jobs")
    op.drop_index(op.f("ix_execution_jobs_batch_id"), table_name="execution_jobs")
    op.drop_table("execution_jobs")

    op.drop_index(op.f("ix_execution_batches_triggered_by"), table_name="execution_batches")
    op.drop_index(op.f("ix_execution_batches_status"), table_name="execution_batches")
    op.drop_index(op.f("ix_execution_batches_project_id"), table_name="execution_batches")
    op.drop_index(op.f("ix_execution_batches_planning_session_id"), table_name="execution_batches")
    op.drop_index(op.f("ix_execution_batches_created_at"), table_name="execution_batches")
    op.drop_table("execution_batches")
