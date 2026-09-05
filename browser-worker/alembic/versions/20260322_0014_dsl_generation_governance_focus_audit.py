"""Persist governance focus reasons on AI DSL generation runs."""

from __future__ import annotations

from alembic import op
import sqlalchemy as sa


revision = "20260322_0014"
down_revision = "20260319_0013"
branch_labels = None
depends_on = None


def upgrade() -> None:
    with op.batch_alter_table("dsl_generation_runs") as batch_op:
        batch_op.add_column(
            sa.Column(
                "governance_focus_reasons_json",
                sa.JSON(),
                nullable=False,
                server_default=sa.text("'[]'"),
            )
        )
    with op.batch_alter_table("dsl_generation_runs") as batch_op:
        batch_op.alter_column("governance_focus_reasons_json", server_default=None)


def downgrade() -> None:
    with op.batch_alter_table("dsl_generation_runs") as batch_op:
        batch_op.drop_column("governance_focus_reasons_json")
