"""Add control-plane ownership metadata.

Revision ID: 20260905_0035
Revises: 20260904_0034
"""

from __future__ import annotations

from alembic import op
import sqlalchemy as sa


revision = "20260905_0035"
down_revision = "20260904_0034"
branch_labels = None
depends_on = None


def upgrade() -> None:
    with op.batch_alter_table("agent_runs") as batch_op:
        batch_op.add_column(sa.Column("actor_user_id", sa.Integer(), nullable=True))
        batch_op.create_foreign_key(
            "fk_agent_runs_actor_user_id_users",
            "users",
            ["actor_user_id"],
            ["id"],
            ondelete="RESTRICT",
        )
    op.create_index(
        "ix_agent_runs_actor_user_id",
        "agent_runs",
        ["actor_user_id"],
    )

    with op.batch_alter_table("ai_planning_sessions") as batch_op:
        batch_op.add_column(
            sa.Column(
                "runtime_owner",
                sa.String(length=16),
                server_default="python",
                nullable=False,
            )
        )
    op.create_index(
        "ix_ai_planning_sessions_runtime_owner",
        "ai_planning_sessions",
        ["runtime_owner"],
    )


def downgrade() -> None:
    op.drop_index(
        "ix_ai_planning_sessions_runtime_owner",
        table_name="ai_planning_sessions",
    )
    with op.batch_alter_table("ai_planning_sessions") as batch_op:
        batch_op.drop_column("runtime_owner")

    op.drop_index("ix_agent_runs_actor_user_id", table_name="agent_runs")
    with op.batch_alter_table("agent_runs") as batch_op:
        batch_op.drop_constraint(
            "fk_agent_runs_actor_user_id_users",
            type_="foreignkey",
        )
        batch_op.drop_column("actor_user_id")
