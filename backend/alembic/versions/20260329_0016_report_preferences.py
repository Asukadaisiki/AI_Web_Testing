"""Add per-user report center preferences."""

from __future__ import annotations

from alembic import op
import sqlalchemy as sa


revision = "20260329_0016"
down_revision = "20260324_0015"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.create_table(
        "report_preferences",
        sa.Column("id", sa.Integer(), nullable=False),
        sa.Column("user_id", sa.Integer(), nullable=False),
        sa.Column("scope_type", sa.String(length=20), nullable=False),
        sa.Column("project_id", sa.Integer(), nullable=True),
        sa.Column("case_id", sa.Integer(), nullable=True),
        sa.Column("window_days", sa.Integer(), nullable=False),
        sa.Column("created_at", sa.DateTime(), nullable=False, server_default=sa.func.now()),
        sa.Column("updated_at", sa.DateTime(), nullable=False, server_default=sa.func.now()),
        sa.ForeignKeyConstraint(["case_id"], ["test_cases.id"], ondelete="SET NULL"),
        sa.ForeignKeyConstraint(["project_id"], ["projects.id"], ondelete="SET NULL"),
        sa.ForeignKeyConstraint(["user_id"], ["users.id"], ondelete="CASCADE"),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint("user_id"),
    )
    op.create_index(op.f("ix_report_preferences_case_id"), "report_preferences", ["case_id"], unique=False)
    op.create_index(op.f("ix_report_preferences_project_id"), "report_preferences", ["project_id"], unique=False)
    op.create_index(op.f("ix_report_preferences_user_id"), "report_preferences", ["user_id"], unique=False)


def downgrade() -> None:
    op.drop_index(op.f("ix_report_preferences_user_id"), table_name="report_preferences")
    op.drop_index(op.f("ix_report_preferences_project_id"), table_name="report_preferences")
    op.drop_index(op.f("ix_report_preferences_case_id"), table_name="report_preferences")
    op.drop_table("report_preferences")
