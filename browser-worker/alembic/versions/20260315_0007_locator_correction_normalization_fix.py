"""Repair locator correction normalized target values after 0006."""

from __future__ import annotations

from collections import defaultdict

from alembic import op
import sqlalchemy as sa


revision = "20260315_0007"
down_revision = "20260314_0006"
branch_labels = None
depends_on = None


def upgrade() -> None:
    connection = op.get_bind()
    rows = connection.execute(
        sa.text(
            """
            SELECT id, page_url_pattern, target_description, is_active, updated_at
            FROM locator_corrections
            ORDER BY updated_at DESC, id DESC
            """
        )
    ).mappings().all()

    normalized_by_id: dict[int, str] = {}
    desired_is_active_by_id: dict[int, bool] = {}
    grouped_rows: dict[tuple[str, str], list[dict[str, object]]] = defaultdict(list)

    for row in rows:
        normalized_target = _normalize_target_description(str(row["target_description"]))
        normalized_by_id[int(row["id"])] = normalized_target
        grouped_rows[(str(row["page_url_pattern"]), normalized_target)].append(dict(row))

    for grouped in grouped_rows.values():
        active_rows = [row for row in grouped if bool(row["is_active"])]
        active_rows.sort(key=lambda row: (row["updated_at"], row["id"]), reverse=True)
        winning_active_id = int(active_rows[0]["id"]) if active_rows else None
        for row in grouped:
            row_id = int(row["id"])
            desired_is_active_by_id[row_id] = bool(row["is_active"]) and row_id == winning_active_id

    locator_corrections = sa.table(
        "locator_corrections",
        sa.column("id", sa.Integer()),
        sa.column("normalized_target_description", sa.String(length=200)),
        sa.column("is_active", sa.Boolean()),
    )

    for row_id, desired_is_active in desired_is_active_by_id.items():
        if not desired_is_active:
            connection.execute(
                sa.update(locator_corrections)
                .where(locator_corrections.c.id == row_id)
                .values(is_active=False)
            )

    for row_id, normalized_target in normalized_by_id.items():
        connection.execute(
            sa.update(locator_corrections)
            .where(locator_corrections.c.id == row_id)
            .values(normalized_target_description=normalized_target)
        )


def downgrade() -> None:
    connection = op.get_bind()
    connection.execute(
        sa.text(
            """
            UPDATE locator_corrections
            SET normalized_target_description = lower(trim(target_description))
            """
        )
    )


def _normalize_target_description(target_description: str) -> str:
    return " ".join(target_description.strip().lower().split())
