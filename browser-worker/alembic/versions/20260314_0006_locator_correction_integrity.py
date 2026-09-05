"""Harden locator correction integrity and lookup semantics."""

from __future__ import annotations

from alembic import op
import sqlalchemy as sa


revision = "20260314_0006"
down_revision = "20260314_0005"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.create_table(
        "locator_corrections_v2",
        sa.Column("id", sa.Integer(), nullable=False),
        sa.Column("page_url_pattern", sa.String(length=500), nullable=False),
        sa.Column("target_description", sa.String(length=200), nullable=False),
        sa.Column("normalized_target_description", sa.String(length=200), nullable=False),
        sa.Column("correction_type", sa.String(length=20), nullable=False),
        sa.Column("correction_value", sa.Text(), nullable=False),
        sa.Column("verified_count", sa.Integer(), nullable=False),
        sa.Column("consecutive_failures", sa.Integer(), nullable=False),
        sa.Column("is_active", sa.Boolean(), nullable=False),
        sa.Column("source_execution_id", sa.Integer(), nullable=True),
        sa.Column("created_by", sa.Integer(), nullable=False),
        sa.Column("created_at", sa.DateTime(), nullable=False),
        sa.Column("updated_at", sa.DateTime(), nullable=False),
        sa.CheckConstraint(
            "correction_type IN ('css', 'xpath', 'test_id')",
            name="ck_locator_corrections_correction_type",
        ),
        sa.ForeignKeyConstraint(["created_by"], ["users.id"], ondelete="RESTRICT"),
        sa.ForeignKeyConstraint(["source_execution_id"], ["test_case_runs.id"], ondelete="SET NULL"),
        sa.PrimaryKeyConstraint("id", name=op.f("pk_locator_corrections_v2")),
    )

    op.execute(
        """
        WITH ranked AS (
            SELECT
                id,
                page_url_pattern,
                target_description,
                lower(trim(target_description)) AS normalized_target_description,
                correction_type,
                correction_value,
                verified_count,
                consecutive_failures,
                is_active,
                source_execution_id,
                created_by,
                created_at,
                updated_at,
                CASE
                    WHEN is_active THEN ROW_NUMBER() OVER (
                        PARTITION BY page_url_pattern, lower(trim(target_description))
                        ORDER BY updated_at DESC, id DESC
                    )
                    ELSE 1
                END AS active_rank
            FROM locator_corrections
        )
        INSERT INTO locator_corrections_v2 (
            id,
            page_url_pattern,
            target_description,
            normalized_target_description,
            correction_type,
            correction_value,
            verified_count,
            consecutive_failures,
            is_active,
            source_execution_id,
            created_by,
            created_at,
            updated_at
        )
        SELECT
            id,
            page_url_pattern,
            target_description,
            normalized_target_description,
            correction_type,
            correction_value,
            verified_count,
            consecutive_failures,
            CASE WHEN is_active AND active_rank > 1 THEN FALSE ELSE is_active END,
            source_execution_id,
            created_by,
            created_at,
            updated_at
        FROM ranked
        """
    )

    op.drop_table("locator_corrections")
    op.rename_table("locator_corrections_v2", "locator_corrections")

    op.create_index(
        "ix_locator_corrections_source_execution_id",
        "locator_corrections",
        ["source_execution_id"],
        unique=False,
    )
    op.create_index("ix_locator_corrections_created_by", "locator_corrections", ["created_by"], unique=False)
    op.create_index(
        "ix_locator_corrections_lookup",
        "locator_corrections",
        ["page_url_pattern", "normalized_target_description"],
        unique=False,
    )
    op.create_index(
        "uq_locator_corrections_active_lookup",
        "locator_corrections",
        ["page_url_pattern", "normalized_target_description"],
        unique=True,
        sqlite_where=sa.text("is_active = 1"),
        postgresql_where=sa.text("is_active"),
    )


def downgrade() -> None:
    connection = op.get_bind()
    null_count = connection.execute(
        sa.text("SELECT COUNT(*) FROM locator_corrections WHERE source_execution_id IS NULL")
    ).scalar_one()
    if null_count:
        raise RuntimeError("Cannot downgrade with locator corrections that have null source_execution_id.")

    op.drop_index("uq_locator_corrections_active_lookup", table_name="locator_corrections")
    op.drop_index("ix_locator_corrections_lookup", table_name="locator_corrections")
    op.drop_index("ix_locator_corrections_created_by", table_name="locator_corrections")
    op.drop_index("ix_locator_corrections_source_execution_id", table_name="locator_corrections")

    op.create_table(
        "locator_corrections_v1",
        sa.Column("id", sa.Integer(), nullable=False),
        sa.Column("page_url_pattern", sa.String(length=500), nullable=False),
        sa.Column("target_description", sa.String(length=200), nullable=False),
        sa.Column("correction_type", sa.String(length=20), nullable=False),
        sa.Column("correction_value", sa.Text(), nullable=False),
        sa.Column("verified_count", sa.Integer(), nullable=False),
        sa.Column("consecutive_failures", sa.Integer(), nullable=False),
        sa.Column("is_active", sa.Boolean(), nullable=False),
        sa.Column("source_execution_id", sa.Integer(), nullable=False),
        sa.Column("created_by", sa.Integer(), nullable=False),
        sa.Column("created_at", sa.DateTime(), nullable=False),
        sa.Column("updated_at", sa.DateTime(), nullable=False),
        sa.ForeignKeyConstraint(["created_by"], ["users.id"], ondelete="RESTRICT"),
        sa.ForeignKeyConstraint(["source_execution_id"], ["test_case_runs.id"], ondelete="CASCADE"),
        sa.PrimaryKeyConstraint("id", name=op.f("pk_locator_corrections_v1")),
    )

    op.execute(
        """
        INSERT INTO locator_corrections_v1 (
            id,
            page_url_pattern,
            target_description,
            correction_type,
            correction_value,
            verified_count,
            consecutive_failures,
            is_active,
            source_execution_id,
            created_by,
            created_at,
            updated_at
        )
        SELECT
            id,
            page_url_pattern,
            target_description,
            correction_type,
            correction_value,
            verified_count,
            consecutive_failures,
            is_active,
            source_execution_id,
            created_by,
            created_at,
            updated_at
        FROM locator_corrections
        """
    )

    op.drop_table("locator_corrections")
    op.rename_table("locator_corrections_v1", "locator_corrections")
    op.create_index("ix_locator_corrections_page_url_pattern", "locator_corrections", ["page_url_pattern"], unique=False)
    op.create_index(
        "ix_locator_corrections_target_description",
        "locator_corrections",
        ["target_description"],
        unique=False,
    )
    op.create_index(
        "ix_locator_corrections_source_execution_id",
        "locator_corrections",
        ["source_execution_id"],
        unique=False,
    )
    op.create_index("ix_locator_corrections_created_by", "locator_corrections", ["created_by"], unique=False)
