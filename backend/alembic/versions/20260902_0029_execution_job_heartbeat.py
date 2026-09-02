"""Add execution job heartbeat timestamp."""

from alembic import op
import sqlalchemy as sa


revision = "20260902_0029"
down_revision = "20260902_0028"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.add_column("execution_jobs", sa.Column("heartbeat_at", sa.DateTime(), nullable=True))


def downgrade() -> None:
    op.drop_column("execution_jobs", "heartbeat_at")
