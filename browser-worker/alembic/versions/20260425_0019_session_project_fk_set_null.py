"""Change ai_planning_sessions.project_id FK to SET NULL and allow nullable.

Projects and sessions are loosely coupled: deleting a project should not
block if sessions reference it. The FK is changed from RESTRICT to SET NULL
so that project deletion nullifies the column instead of raising an error.
"""

from __future__ import annotations

from alembic import op
import sqlalchemy as sa


revision = "20260425_0019"
down_revision = "2348081d0e8a"
branch_labels = None
depends_on = None

FK_NAME = "fk_ai_planning_sessions_project_id_projects"


def upgrade() -> None:
    op.drop_constraint(FK_NAME, "ai_planning_sessions", type_="foreignkey")
    op.alter_column("ai_planning_sessions", "project_id", nullable=True)
    op.create_foreign_key(FK_NAME, "ai_planning_sessions", "projects", ["project_id"], ["id"], ondelete="SET NULL")


def downgrade() -> None:
    op.drop_constraint(FK_NAME, "ai_planning_sessions", type_="foreignkey")
    op.alter_column("ai_planning_sessions", "project_id", nullable=False)
    op.create_foreign_key(FK_NAME, "ai_planning_sessions", "projects", ["project_id"], ["id"], ondelete="RESTRICT")
