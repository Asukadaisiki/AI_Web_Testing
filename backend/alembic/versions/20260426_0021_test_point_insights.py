"""add test_point_insights table

Revision ID: 20260426_0021
Revises: 20260425_0020
"""

from __future__ import annotations

from alembic import op
import sqlalchemy as sa

revision = "20260426_0021"
down_revision = "20260425_0020"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.create_table(
        "test_point_insights",
        sa.Column("id", sa.Integer(), primary_key=True),
        sa.Column("project_id", sa.Integer(), sa.ForeignKey("projects.id", ondelete="CASCADE"), unique=True, nullable=False),
        sa.Column("flaky_case_ids", sa.JSON(), nullable=True),
        sa.Column("failure_patterns", sa.JSON(), nullable=True),
        sa.Column("regression_risk", sa.String(20), nullable=True),
        sa.Column("last_analysis_summary", sa.String(2000), nullable=True),
        sa.Column("updated_at", sa.DateTime(), server_default=sa.func.now(), onupdate=sa.func.now(), nullable=False),
    )
    op.create_index("ix_test_point_insights_project_id", "test_point_insights", ["project_id"], unique=True)


def downgrade() -> None:
    op.drop_index("ix_test_point_insights_project_id", table_name="test_point_insights")
    op.drop_table("test_point_insights")
