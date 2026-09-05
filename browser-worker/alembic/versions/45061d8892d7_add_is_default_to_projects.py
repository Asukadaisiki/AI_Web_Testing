"""Add is_default to projects."""

from alembic import op
import sqlalchemy as sa


revision = "45061d8892d7"
down_revision = "1c65d6ff37db"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.add_column(
        "projects",
        sa.Column("is_default", sa.Boolean(), server_default=sa.false(), nullable=False),
    )


def downgrade() -> None:
    with op.batch_alter_table("projects") as batch_op:
        batch_op.drop_column("is_default")
