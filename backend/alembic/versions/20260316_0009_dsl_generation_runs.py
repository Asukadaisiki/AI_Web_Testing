"""Add durable AI DSL generation run history."""

from __future__ import annotations

from alembic import op
import sqlalchemy as sa


revision = "20260316_0009"
down_revision = "20260315_0008"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.create_table(
        "dsl_generation_runs",
        sa.Column("id", sa.Integer(), nullable=False),
        sa.Column("actor_user_id", sa.Integer(), nullable=False),
        sa.Column("prompt_preview", sa.String(length=200), nullable=False),
        sa.Column("prompt_sha256", sa.String(length=64), nullable=False),
        sa.Column("request_base_url", sa.String(length=500), nullable=True),
        sa.Column("generation_mode", sa.String(length=32), nullable=False),
        sa.Column("import_mode", sa.String(length=32), nullable=False),
        sa.Column("model_name", sa.String(length=200), nullable=True),
        sa.Column("success", sa.Boolean(), nullable=False),
        sa.Column("error_type", sa.String(length=200), nullable=True),
        sa.Column("error_message", sa.String(length=2000), nullable=True),
        sa.Column("used_current_case_context", sa.Boolean(), nullable=False),
        sa.Column("used_current_steps_context", sa.Boolean(), nullable=False),
        sa.Column("base_url_source", sa.String(length=32), nullable=False),
        sa.Column("base_url_backfilled", sa.Boolean(), nullable=False),
        sa.Column("repaired_invalid_actions", sa.Integer(), nullable=False),
        sa.Column("removed_invalid_steps", sa.Integer(), nullable=False),
        sa.Column("removed_invalid_contracts", sa.Integer(), nullable=False),
        sa.Column("warnings_count", sa.Integer(), nullable=False),
        sa.Column("normalization_notes_count", sa.Integer(), nullable=False),
        sa.Column("generated_case_json", sa.JSON(), nullable=True),
        sa.Column("created_at", sa.DateTime(), server_default=sa.func.now(), nullable=False),
        sa.CheckConstraint(
            "generation_mode IN ('draft', 'strict_steps_only')",
            name="ck_dsl_generation_runs_generation_mode",
        ),
        sa.CheckConstraint(
            "import_mode IN ('replace', 'steps_only', 'contracts_only')",
            name="ck_dsl_generation_runs_import_mode",
        ),
        sa.CheckConstraint(
            "base_url_source IN ('ai_output', 'request', 'current_case', 'none')",
            name="ck_dsl_generation_runs_base_url_source",
        ),
        sa.ForeignKeyConstraint(["actor_user_id"], ["users.id"], ondelete="RESTRICT"),
        sa.PrimaryKeyConstraint("id"),
    )
    op.create_index("ix_dsl_generation_runs_actor_user_id", "dsl_generation_runs", ["actor_user_id"], unique=False)
    op.create_index("ix_dsl_generation_runs_created_at", "dsl_generation_runs", ["created_at"], unique=False)
    op.create_index("ix_dsl_generation_runs_prompt_sha256", "dsl_generation_runs", ["prompt_sha256"], unique=False)
    op.create_index("ix_dsl_generation_runs_success", "dsl_generation_runs", ["success"], unique=False)


def downgrade() -> None:
    op.drop_index("ix_dsl_generation_runs_success", table_name="dsl_generation_runs")
    op.drop_index("ix_dsl_generation_runs_prompt_sha256", table_name="dsl_generation_runs")
    op.drop_index("ix_dsl_generation_runs_created_at", table_name="dsl_generation_runs")
    op.drop_index("ix_dsl_generation_runs_actor_user_id", table_name="dsl_generation_runs")
    op.drop_table("dsl_generation_runs")
