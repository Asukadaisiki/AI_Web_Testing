"""Drop authentication-only user columns."""

from __future__ import annotations

from alembic import op
import sqlalchemy as sa


revision = "20260905_0039"
down_revision = "20260905_0038"
branch_labels = None
depends_on = None


def upgrade() -> None:
    with op.batch_alter_table("users") as batch_op:
        batch_op.drop_column("is_active")
        batch_op.drop_column("password_hash")


def downgrade() -> None:
    with op.batch_alter_table("users") as batch_op:
        batch_op.add_column(
            sa.Column(
                "password_hash",
                sa.String(length=255),
                nullable=False,
                server_default="",
            )
        )
        batch_op.add_column(
            sa.Column(
                "is_active",
                sa.Boolean(),
                nullable=False,
                server_default=sa.true(),
            )
        )
