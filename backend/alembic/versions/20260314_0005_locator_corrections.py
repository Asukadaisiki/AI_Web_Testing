"""Add locator corrections table."""

from __future__ import annotations

from alembic import op
import sqlalchemy as sa


revision = "20260314_0005"
down_revision = "20260313_0004"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.create_table(
        "locator_corrections",
        sa.Column("id", sa.Integer(), nullable=False),
        sa.Column("page_url_pattern", sa.String(length=500), nullable=False),
        sa.Column("target_description", sa.String(length=200), nullable=False),
        sa.Column("correction_type", sa.String(length=20), nullable=False),
        sa.Column("correction_value", sa.Text(), nullable=False),
        sa.Column("verified_count", sa.Integer(), nullable=False, server_default="0"),
        sa.Column("consecutive_failures", sa.Integer(), nullable=False, server_default="0"),
        sa.Column("is_active", sa.Boolean(), nullable=False, server_default=sa.true()),
        sa.Column("source_execution_id", sa.Integer(), nullable=False),
        sa.Column("created_by", sa.Integer(), nullable=False),
        sa.Column("created_at", sa.DateTime(), nullable=False, server_default=sa.func.now()),
        sa.Column("updated_at", sa.DateTime(), nullable=False, server_default=sa.func.now()),
        sa.ForeignKeyConstraint(["created_by"], ["users.id"], ondelete="RESTRICT"),
        sa.ForeignKeyConstraint(["source_execution_id"], ["test_case_runs.id"], ondelete="CASCADE"),
        sa.PrimaryKeyConstraint("id", name=op.f("pk_locator_corrections")),
    )
    op.create_index(op.f("ix_locator_corrections_page_url_pattern"), "locator_corrections", ["page_url_pattern"], unique=False)
    op.create_index(
        op.f("ix_locator_corrections_target_description"),
        "locator_corrections",
        ["target_description"],
        unique=False,
    )
    op.create_index(
        op.f("ix_locator_corrections_source_execution_id"),
        "locator_corrections",
        ["source_execution_id"],
        unique=False,
    )
    op.create_index(op.f("ix_locator_corrections_created_by"), "locator_corrections", ["created_by"], unique=False)

    op.alter_column("locator_corrections", "verified_count", server_default=None)
    op.alter_column("locator_corrections", "consecutive_failures", server_default=None)
    op.alter_column("locator_corrections", "is_active", server_default=None)
    op.alter_column("locator_corrections", "created_at", server_default=None)
    op.alter_column("locator_corrections", "updated_at", server_default=None)


def downgrade() -> None:
    op.drop_index(op.f("ix_locator_corrections_created_by"), table_name="locator_corrections")
    op.drop_index(op.f("ix_locator_corrections_source_execution_id"), table_name="locator_corrections")
    op.drop_index(op.f("ix_locator_corrections_target_description"), table_name="locator_corrections")
    op.drop_index(op.f("ix_locator_corrections_page_url_pattern"), table_name="locator_corrections")
    op.drop_table("locator_corrections")
