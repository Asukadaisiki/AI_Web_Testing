"""Change test_cases.project_id FK from RESTRICT to CASCADE.

Allows project deletion to cascade-remove test cases automatically.
The cascade chain: projects → test_cases (CASCADE) → test_case_runs (CASCADE).
Downstream tables (dsl_generation_runs, report_preferences, ai_planning_sessions)
use SET NULL on case_id and are unaffected.
"""

from __future__ import annotations

from alembic import op
import sqlalchemy as sa


revision = "20260425_0020"
down_revision = "20260425_0019"
branch_labels = None
depends_on = None

FK_NAME = "fk_test_cases_project_id_projects"


def upgrade() -> None:
    op.drop_constraint(FK_NAME, "test_cases", type_="foreignkey")
    op.create_foreign_key(FK_NAME, "test_cases", "projects", ["project_id"], ["id"], ondelete="CASCADE")


def downgrade() -> None:
    op.drop_constraint(FK_NAME, "test_cases", type_="foreignkey")
    op.create_foreign_key(FK_NAME, "test_cases", "projects", ["project_id"], ["id"], ondelete="RESTRICT")
