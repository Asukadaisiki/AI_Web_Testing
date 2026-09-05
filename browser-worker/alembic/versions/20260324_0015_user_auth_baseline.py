"""Add local authentication fields to users."""

from __future__ import annotations

from alembic import op
import sqlalchemy as sa


revision = "20260324_0015"
down_revision = "20260322_0014"
branch_labels = None
depends_on = None


LEGACY_PASSWORD_HASH = (
    "pbkdf2_sha256$120000$legacy-password-reset-required$"
    "legacy-password-reset-required"
)


def upgrade() -> None:
    with op.batch_alter_table("users") as batch_op:
        batch_op.add_column(
            sa.Column(
                "password_hash",
                sa.String(length=255),
                nullable=False,
                server_default=LEGACY_PASSWORD_HASH,
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
    with op.batch_alter_table("users") as batch_op:
        batch_op.alter_column("password_hash", server_default=None)
        batch_op.alter_column("is_active", server_default=None)


def downgrade() -> None:
    with op.batch_alter_table("users") as batch_op:
        batch_op.drop_column("is_active")
        batch_op.drop_column("password_hash")
