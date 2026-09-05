"""Backfill unified failure categories for existing anti-patterns."""

from __future__ import annotations

from alembic import op
import sqlalchemy as sa


revision = "20260902_0031"
down_revision = "20260902_0030"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.execute(
        sa.text(
            """
            UPDATE dsl_anti_patterns
            SET failure_category = CASE error_category
                WHEN 'missing_navigation' THEN 'navigation'
                WHEN 'target_not_found' THEN 'locator'
                WHEN 'wrong_page_state' THEN 'assertion'
                WHEN 'missing_input_before_assert' THEN 'assertion'
                WHEN 'missing_capture_text' THEN 'assertion'
                ELSE 'runner'
            END
            WHERE failure_category IS NULL
            """
        )
    )


def downgrade() -> None:
    pass
