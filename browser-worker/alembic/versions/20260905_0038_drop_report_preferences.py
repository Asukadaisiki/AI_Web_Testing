"""Drop unused report preferences.

Revision ID: 20260905_0038
Revises: 20260905_0037
"""

from __future__ import annotations

from alembic import op
import sqlalchemy as sa


revision = "20260905_0038"
down_revision = "20260905_0037"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.drop_table("report_preferences")


def downgrade() -> None:
    op.create_table(
        "report_preferences",
        sa.Column("id", sa.Integer(), primary_key=True),
        sa.Column("user_id", sa.Integer(), nullable=False),
        sa.Column("scope_type", sa.String(length=20), nullable=False),
        sa.Column("project_id", sa.Integer(), nullable=True),
        sa.Column("case_id", sa.Integer(), nullable=True),
        sa.Column("window_days", sa.Integer(), nullable=False),
        sa.Column("created_at", sa.DateTime(), server_default=sa.func.now(), nullable=False),
        sa.Column("updated_at", sa.DateTime(), server_default=sa.func.now(), nullable=False),
        sa.ForeignKeyConstraint(["user_id"], ["users.id"], ondelete="CASCADE"),
        sa.ForeignKeyConstraint(["project_id"], ["projects.id"], ondelete="SET NULL"),
        sa.ForeignKeyConstraint(["case_id"], ["test_cases.id"], ondelete="SET NULL"),
        sa.UniqueConstraint("user_id"),
    )
