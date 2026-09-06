"""Add versioned research experiments, runs, and transitions."""

from __future__ import annotations

from alembic import op
import sqlalchemy as sa


revision = "20260906_0041"
down_revision = "20260906_0040"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.create_table(
        "research_experiments",
        sa.Column("id", sa.String(length=64), nullable=False),
        sa.Column("project_id", sa.Integer(), nullable=False),
        sa.Column("name", sa.String(length=200), nullable=False),
        sa.Column("goal", sa.Text(), nullable=False),
        sa.Column("dataset_version", sa.String(length=200), nullable=False),
        sa.Column("model_provider", sa.String(length=200), nullable=False),
        sa.Column("model_name", sa.String(length=200), nullable=False),
        sa.Column("model_version", sa.String(length=200), nullable=False),
        sa.Column("prompt_version", sa.String(length=200), nullable=False),
        sa.Column("browser_name", sa.String(length=200), nullable=False),
        sa.Column("browser_version", sa.String(length=200), nullable=False),
        sa.Column("viewport_json", sa.JSON(), nullable=False),
        sa.Column("code_sha256", sa.String(length=64), nullable=False),
        sa.Column("policy_version", sa.String(length=200), nullable=False),
        sa.Column("observation_profile", sa.String(length=200), nullable=False),
        sa.Column("dsl_profile", sa.String(length=200), nullable=False),
        sa.Column("seed", sa.BigInteger(), nullable=False),
        sa.Column("variant", sa.String(length=200), nullable=False),
        sa.Column("repetitions", sa.Integer(), nullable=False),
        sa.Column("status", sa.String(length=32), nullable=False),
        sa.Column("config_json", sa.JSON(), nullable=False),
        sa.Column("created_at", sa.DateTime(), server_default=sa.func.now(), nullable=False),
        sa.Column("updated_at", sa.DateTime(), server_default=sa.func.now(), nullable=False),
        sa.CheckConstraint(
            "status IN ('draft', 'active', 'completed', 'cancelled')",
            name=op.f("ck_research_experiments_status"),
        ),
        sa.CheckConstraint(
            "repetitions >= 1",
            name=op.f("ck_research_experiments_repetitions_positive"),
        ),
        sa.CheckConstraint(
            "length(code_sha256) = 64 AND lower(code_sha256) = code_sha256",
            name=op.f("ck_research_experiments_code_sha256"),
        ),
        sa.CheckConstraint(
            "policy_version = 'research.policy.v1'",
            name=op.f("ck_research_experiments_policy_version"),
        ),
        sa.ForeignKeyConstraint(
            ["project_id"],
            ["projects.id"],
            name=op.f("fk_research_experiments_project_id_projects"),
            ondelete="RESTRICT",
        ),
        sa.PrimaryKeyConstraint("id", name=op.f("pk_research_experiments")),
    )
    op.create_index(
        op.f("ix_research_experiments_project_id"),
        "research_experiments",
        ["project_id"],
    )
    op.create_index(
        op.f("ix_research_experiments_status"),
        "research_experiments",
        ["status"],
    )
    op.create_index(
        "ix_research_experiments_project_created",
        "research_experiments",
        ["project_id", "created_at"],
    )

    op.create_table(
        "research_runs",
        sa.Column("id", sa.String(length=64), nullable=False),
        sa.Column("experiment_id", sa.String(length=64), nullable=False),
        sa.Column("project_id", sa.Integer(), nullable=False),
        sa.Column("idempotency_key", sa.String(length=200), nullable=False),
        sa.Column("repetition_index", sa.Integer(), nullable=False),
        sa.Column("warmup", sa.Boolean(), nullable=False),
        sa.Column("status", sa.String(length=32), nullable=False),
        sa.Column("schema_version", sa.String(length=64), nullable=False),
        sa.Column("projector_version", sa.String(length=64), nullable=False),
        sa.Column("metric_version", sa.String(length=64), nullable=False),
        sa.Column("policy_version", sa.String(length=64), nullable=False),
        sa.Column("agent_run_id", sa.String(length=64), nullable=True),
        sa.Column("generation_id", sa.Integer(), nullable=True),
        sa.Column("batch_id", sa.Integer(), nullable=True),
        sa.Column("execution_id", sa.Integer(), nullable=True),
        sa.Column("dsl_sha256", sa.String(length=64), nullable=True),
        sa.Column("metrics_json", sa.JSON(), nullable=True),
        sa.Column("started_at", sa.DateTime(), nullable=True),
        sa.Column("finished_at", sa.DateTime(), nullable=True),
        sa.Column("created_at", sa.DateTime(), server_default=sa.func.now(), nullable=False),
        sa.Column("updated_at", sa.DateTime(), server_default=sa.func.now(), nullable=False),
        sa.CheckConstraint(
            "status IN ('pending', 'running', 'completed', 'failed', 'cancelled')",
            name=op.f("ck_research_runs_status"),
        ),
        sa.CheckConstraint(
            "repetition_index >= 0",
            name=op.f("ck_research_runs_repetition_index_non_negative"),
        ),
        sa.CheckConstraint(
            "schema_version = 'research.persistence.v1' AND "
            "projector_version = 'research.projector.v1' AND "
            "metric_version = 'research.metrics.v1' AND "
            "policy_version = 'research.policy.v1'",
            name=op.f("ck_research_runs_versions"),
        ),
        sa.CheckConstraint(
            "(status = 'pending' AND started_at IS NULL AND finished_at IS NULL) OR "
            "(status = 'running' AND started_at IS NOT NULL AND finished_at IS NULL) OR "
            "(status IN ('completed', 'failed') AND "
            "started_at IS NOT NULL AND finished_at IS NOT NULL) OR "
            "(status = 'cancelled' AND finished_at IS NOT NULL)",
            name=op.f("ck_research_runs_status_timestamps"),
        ),
        sa.CheckConstraint(
            "started_at IS NULL OR finished_at IS NULL OR finished_at >= started_at",
            name=op.f("ck_research_runs_timestamp_order"),
        ),
        sa.CheckConstraint(
            "dsl_sha256 IS NULL OR "
            "(length(dsl_sha256) = 64 AND lower(dsl_sha256) = dsl_sha256)",
            name=op.f("ck_research_runs_dsl_sha256"),
        ),
        sa.CheckConstraint(
            "(generation_id IS NULL OR agent_run_id IS NOT NULL) AND "
            "(batch_id IS NULL OR generation_id IS NOT NULL) AND "
            "(execution_id IS NULL OR batch_id IS NOT NULL) AND "
            "(dsl_sha256 IS NULL OR generation_id IS NOT NULL)",
            name=op.f("ck_research_runs_link_prefix"),
        ),
        sa.ForeignKeyConstraint(
            ["experiment_id"],
            ["research_experiments.id"],
            name=op.f("fk_research_runs_experiment_id_research_experiments"),
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["project_id"],
            ["projects.id"],
            name=op.f("fk_research_runs_project_id_projects"),
            ondelete="RESTRICT",
        ),
        sa.ForeignKeyConstraint(
            ["agent_run_id"],
            ["agent_runs.id"],
            name=op.f("fk_research_runs_agent_run_id_agent_runs"),
            ondelete="RESTRICT",
        ),
        sa.ForeignKeyConstraint(
            ["generation_id"],
            ["dsl_generation_runs.id"],
            name=op.f("fk_research_runs_generation_id_dsl_generation_runs"),
            ondelete="RESTRICT",
        ),
        sa.ForeignKeyConstraint(
            ["batch_id"],
            ["execution_batches.id"],
            name=op.f("fk_research_runs_batch_id_execution_batches"),
            ondelete="RESTRICT",
        ),
        sa.ForeignKeyConstraint(
            ["execution_id"],
            ["test_case_runs.id"],
            name=op.f("fk_research_runs_execution_id_test_case_runs"),
            ondelete="RESTRICT",
        ),
        sa.PrimaryKeyConstraint("id", name=op.f("pk_research_runs")),
        sa.UniqueConstraint(
            "experiment_id",
            "idempotency_key",
            name="uq_research_runs_experiment_idempotency",
        ),
        sa.UniqueConstraint(
            "experiment_id",
            "repetition_index",
            "warmup",
            name="uq_research_runs_experiment_repetition_warmup",
        ),
    )
    op.create_index(op.f("ix_research_runs_experiment_id"), "research_runs", ["experiment_id"])
    op.create_index(op.f("ix_research_runs_project_id"), "research_runs", ["project_id"])
    op.create_index(op.f("ix_research_runs_status"), "research_runs", ["status"])
    op.create_index(op.f("ix_research_runs_agent_run_id"), "research_runs", ["agent_run_id"])
    op.create_index(op.f("ix_research_runs_generation_id"), "research_runs", ["generation_id"])
    op.create_index(op.f("ix_research_runs_batch_id"), "research_runs", ["batch_id"])
    op.create_index(op.f("ix_research_runs_execution_id"), "research_runs", ["execution_id"])
    op.create_index(
        "ix_research_runs_experiment_created",
        "research_runs",
        ["experiment_id", "created_at"],
    )

    op.create_table(
        "research_transitions",
        sa.Column("id", sa.BigInteger(), autoincrement=True, nullable=False),
        sa.Column("research_run_id", sa.String(length=64), nullable=False),
        sa.Column("ordinal", sa.BigInteger(), nullable=False),
        sa.Column("append_key", sa.String(length=200), nullable=False),
        sa.Column("content_sha256", sa.String(length=64), nullable=False),
        sa.Column("schema_version", sa.String(length=64), nullable=False),
        sa.Column("transition_json", sa.JSON(), nullable=False),
        sa.Column("artifact_refs_json", sa.JSON(), nullable=False),
        sa.Column("created_at", sa.DateTime(), server_default=sa.func.now(), nullable=False),
        sa.CheckConstraint(
            "ordinal >= 0",
            name=op.f("ck_research_transitions_ordinal_non_negative"),
        ),
        sa.CheckConstraint(
            "length(content_sha256) = 64 AND "
            "lower(content_sha256) = content_sha256",
            name=op.f("ck_research_transitions_content_sha256"),
        ),
        sa.CheckConstraint(
            "schema_version = 'research.persistence.v1'",
            name=op.f("ck_research_transitions_schema_version"),
        ),
        sa.ForeignKeyConstraint(
            ["research_run_id"],
            ["research_runs.id"],
            name=op.f("fk_research_transitions_research_run_id_research_runs"),
            ondelete="CASCADE",
        ),
        sa.PrimaryKeyConstraint("id", name=op.f("pk_research_transitions")),
        sa.UniqueConstraint(
            "research_run_id",
            "ordinal",
            name="uq_research_transitions_run_ordinal",
        ),
        sa.UniqueConstraint(
            "research_run_id",
            "append_key",
            name="uq_research_transitions_run_append_key",
        ),
    )
    op.create_index(
        op.f("ix_research_transitions_research_run_id"),
        "research_transitions",
        ["research_run_id"],
    )
    op.create_index(
        "ix_research_transitions_run_created",
        "research_transitions",
        ["research_run_id", "created_at"],
    )


def downgrade() -> None:
    op.drop_index(
        "ix_research_transitions_run_created",
        table_name="research_transitions",
    )
    op.drop_index(
        op.f("ix_research_transitions_research_run_id"),
        table_name="research_transitions",
    )
    op.drop_table("research_transitions")

    op.drop_index("ix_research_runs_experiment_created", table_name="research_runs")
    op.drop_index(op.f("ix_research_runs_execution_id"), table_name="research_runs")
    op.drop_index(op.f("ix_research_runs_batch_id"), table_name="research_runs")
    op.drop_index(op.f("ix_research_runs_generation_id"), table_name="research_runs")
    op.drop_index(op.f("ix_research_runs_agent_run_id"), table_name="research_runs")
    op.drop_index(op.f("ix_research_runs_status"), table_name="research_runs")
    op.drop_index(op.f("ix_research_runs_project_id"), table_name="research_runs")
    op.drop_index(op.f("ix_research_runs_experiment_id"), table_name="research_runs")
    op.drop_table("research_runs")

    op.drop_index(
        "ix_research_experiments_project_created",
        table_name="research_experiments",
    )
    op.drop_index(
        op.f("ix_research_experiments_status"),
        table_name="research_experiments",
    )
    op.drop_index(
        op.f("ix_research_experiments_project_id"),
        table_name="research_experiments",
    )
    op.drop_table("research_experiments")
