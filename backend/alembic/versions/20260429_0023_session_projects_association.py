"""Create session_projects association table and migrate existing project_id data.

Revisions:
    revise = '0cf285e27ae1'
"""

from alembic import op
import sqlalchemy as sa

revision = '20260429_0023'
down_revision = '0cf285e27ae1'
branch_labels = None
depends_on = None


def upgrade() -> None:
    # 1. Create association table
    op.create_table(
        'session_projects',
        sa.Column('id', sa.Integer(), primary_key=True, autoincrement=True),
        sa.Column('session_id', sa.Integer(), sa.ForeignKey(
            'ai_planning_sessions.id', ondelete='CASCADE'
        ), nullable=False, index=True),
        sa.Column('project_id', sa.Integer(), sa.ForeignKey(
            'projects.id', ondelete='CASCADE'
        ), nullable=False, index=True),
        sa.Column('created_at', sa.DateTime(), server_default=sa.func.now(), nullable=False),
        sa.UniqueConstraint('session_id', 'project_id', name='uq_session_projects'),
    )

    # 2. Migrate existing data: copy non-null project_id values into association table
    op.execute("""
        INSERT INTO session_projects (session_id, project_id, created_at)
        SELECT id, project_id, created_at
        FROM ai_planning_sessions
        WHERE project_id IS NOT NULL
    """)

    # 3. Drop the project_id column from ai_planning_sessions
    with op.batch_alter_table('ai_planning_sessions') as batch_op:
        batch_op.drop_column('project_id')


def downgrade() -> None:
    # 1. Re-add project_id column
    with op.batch_alter_table('ai_planning_sessions') as batch_op:
        batch_op.add_column(sa.Column('project_id', sa.Integer(), nullable=True))

    # 2. Restore data from association table (pick first project per session)
    op.execute("""
        UPDATE ai_planning_sessions
        SET project_id = (
            SELECT sp.project_id
            FROM session_projects sp
            WHERE sp.session_id = ai_planning_sessions.id
            ORDER BY sp.created_at ASC
            LIMIT 1
        )
    """)

    # 3. Re-add FK constraint
    with op.batch_alter_table('ai_planning_sessions') as batch_op:
        batch_op.create_foreign_key(
            'fk_ai_planning_sessions_project_id',
            'projects', ['project_id'], ['id'],
            ondelete='SET NULL',
        )

    # 4. Drop association table
    op.drop_table('session_projects')
