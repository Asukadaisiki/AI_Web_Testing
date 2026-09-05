"""add SSE event log table for planning session replay"""

revision = '20260608_0025'
down_revision = '45061d8892d7'
branch_labels = None
depends_on = None

from alembic import op
import sqlalchemy as sa


def upgrade() -> None:
    op.create_table(
        'ai_planning_event_logs',
        sa.Column('id', sa.Integer(), primary_key=True),
        sa.Column('session_id', sa.Integer(), sa.ForeignKey('ai_planning_sessions.id', ondelete='CASCADE'), nullable=False),
        sa.Column('message_id', sa.Integer(), sa.ForeignKey('ai_planning_messages.id', ondelete='SET NULL'), nullable=True),
        sa.Column('event_type', sa.String(32), nullable=False),
        sa.Column('event_data', sa.JSON(), nullable=False),
        sa.Column('seq', sa.Integer(), nullable=False),
        sa.Column('created_at', sa.DateTime(), server_default=sa.func.now(), nullable=False),
    )
    op.create_index('ix_event_log_session_seq', 'ai_planning_event_logs', ['session_id', 'seq'])
    op.create_index('ix_ai_planning_event_logs_session_id', 'ai_planning_event_logs', ['session_id'])
    op.create_index('ix_ai_planning_event_logs_created_at', 'ai_planning_event_logs', ['created_at'])


def downgrade() -> None:
    op.drop_index('ix_ai_planning_event_logs_created_at', table_name='ai_planning_event_logs')
    op.drop_index('ix_ai_planning_event_logs_session_id', table_name='ai_planning_event_logs')
    op.drop_index('ix_event_log_session_seq', table_name='ai_planning_event_logs')
    op.drop_table('ai_planning_event_logs')
