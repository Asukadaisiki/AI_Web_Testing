"""Drop the legacy Python Planning persistence tables.

Revision ID: 20260905_0037
Revises: 20260905_0036
"""

from __future__ import annotations

from alembic import op


revision = "20260905_0037"
down_revision = "20260905_0036"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.drop_table("ai_planning_event_logs")
    op.drop_table("ai_planning_tool_results")
    op.drop_table("ai_planning_drafts")
    op.drop_table("ai_planning_messages")
    op.drop_table("test_point_insights")


def downgrade() -> None:
    raise RuntimeError(
        "Revision 20260905_0037 permanently removes unused legacy Planning data. "
        "Restore from backup before downgrading."
    )
