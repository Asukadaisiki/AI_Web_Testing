"""Add explicit active project to planning sessions."""

from alembic import op
import sqlalchemy as sa


revision = "20260829_0026"
down_revision = "20260608_0025"
branch_labels = None
depends_on = None


def upgrade() -> None:
    with op.batch_alter_table("ai_planning_sessions") as batch_op:
        batch_op.add_column(
            sa.Column("active_project_id", sa.Integer(), nullable=True),
        )
        batch_op.create_foreign_key(
            "fk_ai_planning_sessions_active_project_id_projects",
            "projects",
            ["active_project_id"],
            ["id"],
            ondelete="SET NULL",
        )
    op.create_index(
        "ix_ai_planning_sessions_active_project_id",
        "ai_planning_sessions",
        ["active_project_id"],
    )
    op.execute(
        """
        UPDATE ai_planning_sessions
        SET active_project_id = (
            SELECT sp.project_id
            FROM session_projects AS sp
            WHERE sp.session_id = ai_planning_sessions.id
            ORDER BY sp.id ASC
            LIMIT 1
        )
        """
    )


def downgrade() -> None:
    op.drop_index(
        "ix_ai_planning_sessions_active_project_id",
        table_name="ai_planning_sessions",
    )
    with op.batch_alter_table("ai_planning_sessions") as batch_op:
        batch_op.drop_constraint(
            "fk_ai_planning_sessions_active_project_id_projects",
            type_="foreignkey",
        )
        batch_op.drop_column("active_project_id")
