"""Add locator correction events table."""

from __future__ import annotations

from alembic import op
import sqlalchemy as sa


revision = "20260315_0008"
down_revision = "20260315_0007"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.create_table(
        "locator_correction_events",
        sa.Column("id", sa.Integer(), nullable=False),
        sa.Column("correction_id", sa.Integer(), nullable=False),
        sa.Column("event_type", sa.String(length=32), nullable=False),
        sa.Column("page_url_pattern", sa.String(length=500), nullable=False),
        sa.Column("target_description", sa.String(length=200), nullable=False),
        sa.Column("execution_id", sa.Integer(), nullable=True),
        sa.Column("verified_count_after", sa.Integer(), nullable=False),
        sa.Column("consecutive_failures_after", sa.Integer(), nullable=False),
        sa.Column("is_active_after", sa.Boolean(), nullable=False),
        sa.Column("created_at", sa.DateTime(), server_default=sa.func.now(), nullable=False),
        sa.CheckConstraint(
            "event_type IN ('created', 'activated', 'deactivated', 'tier0_hit', 'tier0_miss', 'auto_deactivated')",
            name="ck_locator_correction_events_event_type",
        ),
        sa.ForeignKeyConstraint(
            ["correction_id"],
            ["locator_corrections.id"],
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["execution_id"],
            ["test_case_runs.id"],
            ondelete="SET NULL",
        ),
        sa.PrimaryKeyConstraint("id"),
    )
    op.create_index(
        "ix_locator_correction_events_correction_id",
        "locator_correction_events",
        ["correction_id"],
        unique=False,
    )
    op.create_index(
        "ix_locator_correction_events_event_type",
        "locator_correction_events",
        ["event_type"],
        unique=False,
    )
    op.create_index(
        "ix_locator_correction_events_execution_id",
        "locator_correction_events",
        ["execution_id"],
        unique=False,
    )


def downgrade() -> None:
    op.drop_index("ix_locator_correction_events_execution_id", table_name="locator_correction_events")
    op.drop_index("ix_locator_correction_events_event_type", table_name="locator_correction_events")
    op.drop_index("ix_locator_correction_events_correction_id", table_name="locator_correction_events")
    op.drop_table("locator_correction_events")
