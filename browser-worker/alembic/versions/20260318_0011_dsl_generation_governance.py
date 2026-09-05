"""Add governance fields to AI DSL generation runs."""

from __future__ import annotations

from alembic import op
import sqlalchemy as sa


revision = "20260318_0011"
down_revision = "20260317_0010"
branch_labels = None
depends_on = None


PROMPT_VERSION = "2026-03-18.governance-v1"


def upgrade() -> None:
    with op.batch_alter_table("dsl_generation_runs") as batch_op:
        batch_op.add_column(sa.Column("project_id", sa.Integer(), nullable=True))
        batch_op.add_column(sa.Column("case_id", sa.Integer(), nullable=True))
        batch_op.add_column(
            sa.Column(
                "prompt_version",
                sa.String(length=100),
                nullable=False,
                server_default=sa.text(f"'{PROMPT_VERSION}'"),
            )
        )
        batch_op.add_column(
            sa.Column(
                "preserve_contracts_requested",
                sa.Boolean(),
                nullable=False,
                server_default=sa.false(),
            )
        )
        batch_op.add_column(
            sa.Column(
                "preserve_contracts_applied",
                sa.Boolean(),
                nullable=False,
                server_default=sa.false(),
            )
        )
        batch_op.add_column(
            sa.Column(
                "warnings_json",
                sa.JSON(),
                nullable=False,
                server_default=sa.text("'[]'"),
            )
        )
        batch_op.add_column(
            sa.Column(
                "normalization_notes_json",
                sa.JSON(),
                nullable=False,
                server_default=sa.text("'[]'"),
            )
        )
        batch_op.add_column(sa.Column("rejection_reason_code", sa.String(length=64), nullable=True))
        batch_op.add_column(sa.Column("feedback_note", sa.String(length=1000), nullable=True))
        batch_op.create_foreign_key(
            "fk_dsl_generation_runs_project_id_projects",
            "projects",
            ["project_id"],
            ["id"],
            ondelete="SET NULL",
        )
        batch_op.create_foreign_key(
            "fk_dsl_generation_runs_case_id_test_cases",
            "test_cases",
            ["case_id"],
            ["id"],
            ondelete="SET NULL",
        )
        batch_op.create_index("ix_dsl_generation_runs_project_id", ["project_id"], unique=False)
        batch_op.create_index("ix_dsl_generation_runs_case_id", ["case_id"], unique=False)
        batch_op.create_check_constraint(
            "ck_dsl_generation_runs_rejection_reason_code",
            "("
            "feedback_status = 'rejected' AND rejection_reason_code IN ("
            "'wrong_actions', 'invalid_structure', 'context_mismatch', 'bad_contracts', 'other'"
            ")"
            ") OR ("
            "feedback_status IN ('pending', 'accepted') AND rejection_reason_code IS NULL"
            ")",
        )
        batch_op.alter_column("prompt_version", server_default=None)
        batch_op.alter_column("preserve_contracts_requested", server_default=None)
        batch_op.alter_column("preserve_contracts_applied", server_default=None)
        batch_op.alter_column("warnings_json", server_default=None)
        batch_op.alter_column("normalization_notes_json", server_default=None)


def downgrade() -> None:
    with op.batch_alter_table("dsl_generation_runs") as batch_op:
        batch_op.drop_constraint("ck_dsl_generation_runs_rejection_reason_code", type_="check")
        batch_op.drop_index("ix_dsl_generation_runs_case_id")
        batch_op.drop_index("ix_dsl_generation_runs_project_id")
        batch_op.drop_constraint("fk_dsl_generation_runs_case_id_test_cases", type_="foreignkey")
        batch_op.drop_constraint("fk_dsl_generation_runs_project_id_projects", type_="foreignkey")
        batch_op.drop_column("feedback_note")
        batch_op.drop_column("rejection_reason_code")
        batch_op.drop_column("normalization_notes_json")
        batch_op.drop_column("warnings_json")
        batch_op.drop_column("preserve_contracts_applied")
        batch_op.drop_column("preserve_contracts_requested")
        batch_op.drop_column("prompt_version")
        batch_op.drop_column("case_id")
        batch_op.drop_column("project_id")
