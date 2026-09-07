"""Add independent research oracle results."""

from __future__ import annotations

from alembic import op
import sqlalchemy as sa


revision = "20260907_0042"
down_revision = "20260906_0041"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.create_table(
        "research_oracle_results",
        sa.Column("research_run_id", sa.String(length=64), nullable=False),
        sa.Column("execution_id", sa.Integer(), nullable=False),
        sa.Column("schema_version", sa.String(length=64), nullable=False),
        sa.Column("evaluator", sa.String(length=200), nullable=False),
        sa.Column("passed", sa.Boolean(), nullable=False),
        sa.Column("reason_code", sa.String(length=100), nullable=False),
        sa.Column("decision_json", sa.JSON(), nullable=False),
        sa.Column("content_sha256", sa.String(length=64), nullable=False),
        sa.Column("created_at", sa.DateTime(), server_default=sa.func.now(), nullable=False),
        sa.Column("updated_at", sa.DateTime(), server_default=sa.func.now(), nullable=False),
        sa.CheckConstraint(
            "schema_version = 'research.oracle.v1'",
            name=op.f("ck_research_oracle_results_schema_version"),
        ),
        sa.CheckConstraint(
            "length(content_sha256) = 64 AND "
            "lower(content_sha256) = content_sha256",
            name=op.f("ck_research_oracle_results_content_sha256"),
        ),
        sa.ForeignKeyConstraint(
            ["research_run_id"],
            ["research_runs.id"],
            name=op.f(
                "fk_research_oracle_results_research_run_id_research_runs"
            ),
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["execution_id"],
            ["test_case_runs.id"],
            name=op.f(
                "fk_research_oracle_results_execution_id_test_case_runs"
            ),
            ondelete="RESTRICT",
        ),
        sa.PrimaryKeyConstraint(
            "research_run_id",
            name=op.f("pk_research_oracle_results"),
        ),
    )
    op.create_index(
        op.f("ix_research_oracle_results_execution_id"),
        "research_oracle_results",
        ["execution_id"],
    )


def downgrade() -> None:
    op.drop_index(
        op.f("ix_research_oracle_results_execution_id"),
        table_name="research_oracle_results",
    )
    op.drop_table("research_oracle_results")
