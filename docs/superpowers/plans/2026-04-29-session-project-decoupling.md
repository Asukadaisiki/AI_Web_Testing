# Session-Project Decoupling Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Decouple AI planning sessions from projects — sessions can exist without projects, with a many-to-many relationship via an association table.

**Architecture:** Replace the direct FK `ai_planning_sessions.project_id` with a `session_projects` association table. Backend already has nullable `project_id`; we migrate existing data, remove the column, and add new CRUD endpoints for managing session-project links. Frontend reorganizes around a session-list entry point instead of project-first flow.

**Tech Stack:** Python/FastAPI/SQLAlchemy 2.x (backend), TypeScript/React/Ant Design (frontend), Alembic (migrations), Vitest (frontend tests), pytest (backend tests)

---

## File Structure

### New Files
- `backend/app/models/session_project.py` — SessionProject association ORM model
- `backend/alembic/versions/20260429_0023_session_projects_association.py` — migration
- `frontend/src/pages/SessionListPage.tsx` — session list entry page
- `frontend/src/components/SessionProjectPanel.tsx` — project management within session
- `backend/tests/unit/test_session_project_service.py` — unit tests for association CRUD
- `backend/tests/unit/test_session_project_routes.py` — unit tests for new routes

### Modified Files
- `backend/app/models/ai_planning_session.py` — remove project_id, add relationship
- `backend/app/models/project.py` — add sessions relationship
- `backend/app/models/__init__.py` — add SessionProject export
- `backend/app/schemas/ai_planning.py` — remove project_id, add projects field, new schemas
- `backend/app/api/routes/ai_planning.py` — modify create/list routes, add project link routes
- `backend/app/services/ai_planning.py` — remove project_id usage, add association functions
- `backend/app/ai/test_planning_agent.py` — project_id → project_ids
- `backend/app/ai/planning_tools.py` — project_id → project_ids in execute_tool
- `frontend/src/types/api.ts` — update session types, add new types
- `frontend/src/services/api.ts` — update API functions, add new ones
- `frontend/src/pages/PlanningPage.tsx` — become session detail page
- `frontend/src/components/AITestPlanningPanel.tsx` — remove projectId dependency
- `frontend/src/app/AppRouter.tsx` — add session list route, update session detail route

---

### Task 1: Database Migration — Create `session_projects` Association Table

**Files:**
- Create: `backend/alembic/versions/20260429_0023_session_projects_association.py`

- [ ] **Step 1: Create the migration file**

```python
"""Create session_projects association table and migrate existing project_id data.

Revisions:
    revise = '20260426_0022'
"""

from alembic import op
import sqlalchemy as sa

revision = '20260429_0023'
down_revision = '20260426_0022'
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
```

- [ ] **Step 2: Run migration**

Run: `cd backend && uv run alembic upgrade head`
Expected: Migration applies without errors.

- [ ] **Step 3: Verify table structure**

Run: `cd backend && uv run python -c "from sqlalchemy import inspect; from app.db.base import Base; from app.db import engine; insp = inspect(engine); print(insp.get_columns('session_projects')); print([c['name'] for c in insp.get_columns('ai_planning_sessions')])"`
Expected: `session_projects` has columns `id, session_id, project_id, created_at`. `ai_planning_sessions` no longer has `project_id`.

- [ ] **Step 4: Commit**

```bash
git add backend/alembic/versions/20260429_0023_session_projects_association.py
git commit -m "feat(db): create session_projects association table, migrate project_id"
```

### Task 2: ORM Models — SessionProject Association + Model Updates

**Files:**
- Create: `backend/app/models/session_project.py`
- Modify: `backend/app/models/ai_planning_session.py:21` — remove `project_id` column, add `projects` relationship
- Modify: `backend/app/models/project.py` — add `sessions` relationship
- Modify: `backend/app/models/__init__.py:3,12,20` — add `SessionProject` import/export

- [ ] **Step 1: Create `backend/app/models/session_project.py`**

```python
"""Session-Project many-to-many association model."""

from __future__ import annotations

from datetime import datetime

from sqlalchemy import DateTime, ForeignKey, Integer, UniqueConstraint, func
from sqlalchemy.orm import Mapped, mapped_column

from app.db.base import Base


class SessionProject(Base):
    """Association table linking planning sessions to projects."""

    __tablename__ = "session_projects"
    __table_args__ = (UniqueConstraint("session_id", "project_id", name="uq_session_projects"),)

    id: Mapped[int] = mapped_column(primary_key=True)
    session_id: Mapped[int] = mapped_column(Integer, ForeignKey("ai_planning_sessions.id", ondelete="CASCADE"), nullable=False, index=True)
    project_id: Mapped[int] = mapped_column(Integer, ForeignKey("projects.id", ondelete="CASCADE"), nullable=False, index=True)
    created_at: Mapped[datetime] = mapped_column(DateTime(), server_default=func.now(), nullable=False)
```

- [ ] **Step 2: Modify `backend/app/models/ai_planning_session.py`**

Remove line 21 (`project_id` column) and add a relationship. The file should become:

```python
"""Persisted AI planning sessions."""

from __future__ import annotations

from datetime import datetime
from typing import Any, TYPE_CHECKING

from sqlalchemy import DateTime, ForeignKey, Integer, JSON, String, Text, func
from sqlalchemy.orm import Mapped, mapped_column, relationship

from app.db.base import Base

if TYPE_CHECKING:
    from app.models.project import Project


class AIPlanningSession(Base):
    """Conversation state for AI-assisted test planning."""

    __tablename__ = "ai_planning_sessions"

    id: Mapped[int] = mapped_column(primary_key=True)
    actor_user_id: Mapped[int] = mapped_column(Integer, ForeignKey("users.id", ondelete="RESTRICT"), index=True, nullable=False)
    case_id: Mapped[int | None] = mapped_column(
        Integer,
        ForeignKey("test_cases.id", ondelete="SET NULL"),
        index=True,
        nullable=True,
    )
    title: Mapped[str | None] = mapped_column(String(200), nullable=True)
    status: Mapped[str] = mapped_column(String(32), nullable=False, default="collecting")
    requirements_json: Mapped[dict[str, Any]] = mapped_column(JSON, nullable=False, default=dict)
    plan_json: Mapped[dict[str, Any] | None] = mapped_column(JSON, nullable=True)
    missing_slots_json: Mapped[list[str]] = mapped_column(JSON, nullable=False, default=list)
    last_error_message: Mapped[str | None] = mapped_column(Text, nullable=True)
    created_at: Mapped[datetime] = mapped_column(DateTime(), server_default=func.now(), nullable=False, index=True)
    updated_at: Mapped[datetime] = mapped_column(DateTime(), server_default=func.now(), onupdate=func.now(), nullable=False)

    projects: Mapped[list["Project"]] = relationship(
        "Project", secondary="session_projects", back_populates="sessions", lazy="selectin",
    )
```

- [ ] **Step 3: Modify `backend/app/models/project.py`**

Add `sessions` relationship. Add imports and the relationship field:

```python
"""Project model."""

from __future__ import annotations

from datetime import datetime
from typing import TYPE_CHECKING

from sqlalchemy import DateTime, String, func
from sqlalchemy.orm import Mapped, mapped_column, relationship

from app.db.base import Base

if TYPE_CHECKING:
    from app.models.ai_planning_session import AIPlanningSession


class Project(Base):
    """Project is the top-level resource boundary for test cases."""

    __tablename__ = "projects"

    id: Mapped[int] = mapped_column(primary_key=True)
    name: Mapped[str] = mapped_column(String(200), unique=True, index=True)
    description: Mapped[str | None] = mapped_column(String(1000), nullable=True)
    created_at: Mapped[datetime] = mapped_column(
        DateTime(),
        server_default=func.now(),
        nullable=False,
    )
    updated_at: Mapped[datetime] = mapped_column(
        DateTime(),
        server_default=func.now(),
        onupdate=func.now(),
        nullable=False,
    )

    sessions: Mapped[list["AIPlanningSession"]] = relationship(
        "AIPlanningSession", secondary="session_projects", back_populates="projects", lazy="noload",
    )
```

- [ ] **Step 4: Modify `backend/app/models/__init__.py`**

Add `SessionProject` import and export. Add after the existing imports:

```python
from app.models.session_project import SessionProject
```

Add `"SessionProject"` to the `__all__` list.

- [ ] **Step 5: Verify models load**

Run: `cd backend && uv run python -c "from app.models import AIPlanningSession, Project, SessionProject; print('OK')"`
Expected: `OK`

- [ ] **Step 6: Commit**

```bash
git add backend/app/models/session_project.py backend/app/models/ai_planning_session.py backend/app/models/project.py backend/app/models/__init__.py
git commit -m "feat(models): add SessionProject association, replace session.project_id with many-to-many"
```

### Task 3: Pydantic Schemas — Update Session Schemas + Add New Request Types

**Files:**
- Modify: `backend/app/schemas/ai_planning.py:69-72,84-89,123-125` — update `AIPlanningSession`, `AIPlanningSessionSummary`, `CreateAIPlanningSessionRequest`
- Add new schemas at end of file

- [ ] **Step 1: Update `AIPlanningSession` schema (line 69-82)**

Remove `project_id` field, add `projects` field:

```python
class AIPlanningSession(DSLModel):
    id: int = Field(ge=1)
    actor_user_id: int = Field(ge=1)
    case_id: int | None = Field(default=None, ge=1)
    title: str | None = Field(default=None, max_length=200)
    status: AIPlanningSessionStatus
    requirements: AIPlanningRequirements = Field(default_factory=AIPlanningRequirements)
    plan: AIPlanningPlan | None = None
    missing_slots: list[str] = Field(default_factory=list)
    last_error_message: str | None = Field(default=None, max_length=4000)
    created_at: datetime
    updated_at: datetime
    projects: list[ProjectSummaryInSession] = Field(default_factory=list)
```

- [ ] **Step 2: Update `AIPlanningSessionSummary` schema (line 84-89)**

Add `projects` field for list display:

```python
class AIPlanningSessionSummary(DSLModel):
    id: int = Field(ge=1)
    title: str | None = Field(default=None, max_length=200)
    status: AIPlanningSessionStatus
    created_at: datetime
    updated_at: datetime
    projects: list[ProjectSummaryInSession] = Field(default_factory=list)
```

- [ ] **Step 3: Update `CreateAIPlanningSessionRequest` schema (line 123-125)**

Remove `project_id`:

```python
class CreateAIPlanningSessionRequest(DSLModel):
    case_id: int | None = Field(default=None, ge=1)
```

- [ ] **Step 4: Add new schemas at end of file**

```python
class ProjectSummaryInSession(DSLModel):
    """Minimal project info returned within session schemas."""
    id: int = Field(ge=1)
    name: str
    description: str | None = None


class LinkProjectRequest(DSLModel):
    project_id: int = Field(ge=1)


class CreateProjectInSessionRequest(DSLModel):
    name: str = Field(min_length=1, max_length=200)
    description: str | None = Field(default=None, max_length=1000)
```

- [ ] **Step 5: Verify schemas load**

Run: `cd backend && uv run python -c "from app.schemas.ai_planning import AIPlanningSession, CreateAIPlanningSessionRequest, LinkProjectRequest, CreateProjectInSessionRequest; print('OK')"`
Expected: `OK`

- [ ] **Step 6: Commit**

```bash
git add backend/app/schemas/ai_planning.py
git commit -m "feat(schemas): remove project_id from session schemas, add association request types"
```

### Task 4: Service Layer — Association CRUD Functions + Update `_to_session_schema`

**Files:**
- Modify: `backend/app/services/ai_planning.py` — add 4 new functions, update `_to_session_schema`, update `list_planning_sessions`, update `create_planning_session`

- [ ] **Step 1: Add imports at top of file (line 12)**

Add `SessionProject` to the models import line:

```python
from app.models import AIPlanningDraft, AIPlanningMessage, AIPlanningSession, DslGenerationRun, Project, SessionProject, TestCase
```

Add new schema imports:

```python
from app.schemas.ai_planning import (
    ...existing...,
    ProjectSummaryInSession,
    LinkProjectRequest,
    CreateProjectInSessionRequest,
)
```

- [ ] **Step 2: Update `_to_session_schema` (line 1246-1260)**

Replace with version that reads `projects` from the relationship instead of `project_id`:

```python
def _to_session_schema(record: AIPlanningSession) -> AIPlanningSessionSchema:
    return AIPlanningSessionSchema(
        id=record.id,
        actor_user_id=record.actor_user_id,
        case_id=record.case_id,
        title=record.title,
        status=record.status,
        requirements=AIPlanningRequirements.model_validate(record.requirements_json or {}),
        plan=record.plan_json,
        missing_slots=record.missing_slots_json or [],
        last_error_message=record.last_error_message,
        created_at=record.created_at,
        updated_at=record.updated_at,
        projects=[
            ProjectSummaryInSession(id=p.id, name=p.name, description=p.description)
            for p in (record.projects or [])
        ],
    )
```

- [ ] **Step 3: Update `list_planning_sessions` (line 43-63)**

Remove `project_id` parameter and filter. Build summaries with projects:

```python
def list_planning_sessions(
    session: Session,
    *,
    actor_user_id: int,
) -> list[AIPlanningSessionSummary]:
    q = session.query(AIPlanningSession).filter(AIPlanningSession.actor_user_id == actor_user_id)
    q = q.order_by(AIPlanningSession.updated_at.desc())
    rows = q.all()
    return [
        AIPlanningSessionSummary(
            id=r.id,
            title=r.title or (r.requirements_json or {}).get("app_under_test"),
            status=r.status,
            created_at=r.created_at,
            updated_at=r.updated_at,
            projects=[
                ProjectSummaryInSession(id=p.id, name=p.name, description=p.description)
                for p in (r.projects or [])
            ],
        )
        for r in rows
    ]
```

- [ ] **Step 4: Update `create_planning_session` (line 66-88)**

Remove `project_id` handling:

```python
def create_planning_session(
    session: Session,
    payload: CreateAIPlanningSessionRequest,
    *,
    actor_user_id: int,
) -> AIPlanningSessionDetail:
    if payload.case_id is not None:
        raise EntityNotFoundError("Case-based session creation requires a project association first.")

    record = AIPlanningSession(
        actor_user_id=actor_user_id,
        case_id=payload.case_id,
        status="collecting",
        requirements_json=AIPlanningRequirements().model_dump(mode="json"),
        missing_slots_json=list(REQUIRED_REQUIREMENT_SLOTS),
    )
    session.add(record)
    session.commit()
    session.refresh(record)
    return get_planning_session_detail(session, record.id, actor_user_id=actor_user_id)
```

- [ ] **Step 5: Add 4 new CRUD functions before `_ensure_project_access` (before line 1224)**

```python
def link_project_to_session(
    session: Session,
    planning_session_id: int,
    *,
    project_id: int,
    actor_user_id: int,
) -> ProjectSummaryInSession:
    planning_session = _get_session(session, planning_session_id, actor_user_id=actor_user_id)
    project = session.get(Project, project_id)
    if project is None:
        raise EntityNotFoundError(f"Project {project_id} not found.")

    existing = session.scalar(
        select(SessionProject).where(
            SessionProject.session_id == planning_session_id,
            SessionProject.project_id == project_id,
        )
    )
    if existing is not None:
        from fastapi import HTTPException
        raise HTTPException(status_code=409, detail=f"Project {project_id} already linked to session {planning_session_id}.")

    session.add(SessionProject(session_id=planning_session_id, project_id=project_id))
    session.commit()
    return ProjectSummaryInSession(id=project.id, name=project.name, description=project.description)


def unlink_project_from_session(
    session: Session,
    planning_session_id: int,
    *,
    project_id: int,
    actor_user_id: int,
) -> None:
    planning_session = _get_session(session, planning_session_id, actor_user_id=actor_user_id)
    link = session.scalar(
        select(SessionProject).where(
            SessionProject.session_id == planning_session_id,
            SessionProject.project_id == project_id,
        )
    )
    if link is None:
        raise EntityNotFoundError(f"Project {project_id} not linked to session {planning_session_id}.")
    session.delete(link)
    session.commit()


def list_session_projects(
    session: Session,
    planning_session_id: int,
    *,
    actor_user_id: int,
) -> list[ProjectSummaryInSession]:
    planning_session = _get_session(session, planning_session_id, actor_user_id=actor_user_id)
    return [
        ProjectSummaryInSession(id=p.id, name=p.name, description=p.description)
        for p in (planning_session.projects or [])
    ]


def create_project_in_session(
    session: Session,
    planning_session_id: int,
    *,
    name: str,
    description: str | None,
    actor_user_id: int,
) -> ProjectSummaryInSession:
    from app.services.cases import _ensure_project_member

    planning_session = _get_session(session, planning_session_id, actor_user_id=actor_user_id)

    project = Project(name=name, description=description)
    session.add(project)
    session.flush()

    session.add(SessionProject(session_id=planning_session_id, project_id=project.id))
    session.commit()
    session.refresh(project)

    return ProjectSummaryInSession(id=project.id, name=project.name, description=project.description)
```

Also add `from sqlalchemy import select` at top if not already present (it is, via line 8 `from sqlalchemy import func, select`).

- [ ] **Step 6: Verify service imports**

Run: `cd backend && uv run python -c "from app.services.ai_planning import link_project_to_session, unlink_project_from_session, list_session_projects, create_project_in_session; print('OK')"`
Expected: `OK`

- [ ] **Step 7: Commit**

```bash
git add backend/app/services/ai_planning.py
git commit -m "feat(service): add session-project association CRUD, update session creation"
```

### Task 5: Service Layer — Remove All `planning_session.project_id` References

**Files:**
- Modify: `backend/app/services/ai_planning.py` — update `send_planning_message`, `stream_planning_message`, `generate_planning_drafts`, `save_and_execute_selected_drafts`, `save_and_execute_selected_drafts_streaming`, `retest_cases`, `_build_session_context_preamble`, `_run_analysis_turn`, `save_and_execute_with_explorer_judge_streaming`

This is the largest task. The pattern is: everywhere that reads `planning_session.project_id` should instead get the first associated project ID via a helper function.

- [ ] **Step 1: Add helper function `_get_session_project_ids` after `_get_session`**

```python
def _get_session_project_ids(planning_session: AIPlanningSession) -> list[int]:
    """Return project IDs associated with this session, ordered by link creation time."""
    return [p.id for p in (planning_session.projects or [])]
```

- [ ] **Step 2: Update `send_planning_message` (line 130-134)**

Change `project_id=planning_session.project_id` to:

```python
    project_ids = _get_session_project_ids(planning_session)
    agent_response = run_planning_turn(
        transcript=base_transcript,
        existing_requirements=AIPlanningRequirements.model_validate(planning_session.requirements_json or {}),
        db_session=session,
        project_id=project_ids[0] if project_ids else 0,
    )
```

- [ ] **Step 3: Update `stream_planning_message` (line 212-216)**

Same pattern — change `project_id=planning_session.project_id` to:

```python
    project_ids = _get_session_project_ids(planning_session)
    stream = stream_planning_turn(
        transcript=base_transcript,
        existing_requirements=AIPlanningRequirements.model_validate(planning_session.requirements_json or {}),
        db_session=session,
        project_id=project_ids[0] if project_ids else 0,
    )
```

- [ ] **Step 4: Update `generate_planning_drafts` (line 275-353)**

Add validation at the top of the function (after getting `planning_session`):

```python
    project_ids = _get_session_project_ids(planning_session)
    if not project_ids:
        raise ValueError("请先关联至少一个项目再生成 DSL 草案。")
```

Change `project_id=planning_session.project_id` (line 353) to `project_id=project_ids[0]`.

- [ ] **Step 5: Update `save_and_execute_selected_drafts` (line 741-880)**

Change `project_id=planning_session.project_id` (line 765) to:

```python
    project_ids = _get_session_project_ids(planning_session)
    if not project_ids:
        raise ValueError("请先关联至少一个项目再保存用例。")
```

Use `project_id=project_ids[0]` in `CaseCreateRequest`.

Change `project_id=planning_session.project_id or 1` (line 848) to `project_id=project_ids[0]`.

- [ ] **Step 6: Update `save_and_execute_selected_drafts_streaming` (line 883-1067)**

Same pattern — `project_id=planning_session.project_id` (line 924) → `project_ids[0]`.

Change `project_id=planning_session.project_id or 1` (line 1039) → `project_id=project_ids[0]`.

- [ ] **Step 7: Update `retest_cases` (line 1070-1190)**

Change `planning_session.project_id or 0` (line 1085) to:

```python
    project_ids = _get_session_project_ids(planning_session)
```

Change `case_record.project_id != planning_session.project_id` (line 1118) to `case_record.project_id not in project_ids`.

Change `project_id=planning_session.project_id or 1` (line 1150) → `project_id=project_ids[0] if project_ids else 0`.

- [ ] **Step 8: Update `_build_session_context_preamble` (line 647-725)**

Change `if not planning_session.project_id or existing_msg_count <= 1:` to:

```python
    project_ids = _get_session_project_ids(planning_session)
    if not project_ids or existing_msg_count <= 1:
        return None
```

Change all `project_id=planning_session.project_id` in this function to `project_id=project_ids[0]`.

- [ ] **Step 9: Update `save_and_execute_with_explorer_judge_streaming` (line 1389-...)**

Change `project_id=planning_session.project_id` (line 1434) to:

```python
    project_ids = _get_session_project_ids(planning_session)
    if not project_ids:
        raise ValueError("请先关联至少一个项目再执行。")
```

Use `project_id=project_ids[0]` in `CaseCreateRequest`.

- [ ] **Step 10: Verify the service loads without import/syntax errors**

Run: `cd backend && uv run python -c "from app.services.ai_planning import create_planning_session, link_project_to_session; print('OK')"`
Expected: `OK`

- [ ] **Step 11: Commit**

```bash
git add backend/app/services/ai_planning.py
git commit -m "refactor(service): replace all planning_session.project_id with association helper"
```

### Task 6: Backend Routes — Update Existing + Add Session-Project Endpoints

**Files:**
- Modify: `backend/app/api/routes/ai_planning.py:59-81` — update create/list route signatures
- Modify: `backend/app/api/routes/ai_planning.py` — add 4 new route handlers

- [ ] **Step 1: Update imports (line 16-25)**

Add new schema imports:

```python
from app.schemas.ai_planning import (
    AIPlanningDraft,
    AIPlanningMessageCreateRequest,
    AIPlanningSessionDetail,
    AIPlanningSessionSummary,
    AIPlanningTurnResponse,
    CreateAIPlanningSessionRequest,
    CreateProjectInSessionRequest,
    GenerateAIPlanningDraftsRequest,
    LinkProjectRequest,
    ProjectSummaryInSession,
    UpdateAIPlanningDraftStatusRequest,
)
```

Add new service imports:

```python
from app.services.ai_planning import (
    AIPlanningAccessError,
    create_planning_session,
    create_project_in_session,
    delete_planning_draft,
    delete_planning_session,
    generate_planning_drafts,
    get_planning_session_detail,
    link_project_to_session,
    list_planning_sessions,
    list_session_projects,
    retest_cases,
    save_and_execute_selected_drafts,
    send_planning_message,
    unlink_project_from_session,
    update_planning_draft_status,
)
```

- [ ] **Step 2: Update `list_planning_sessions_route` (line 74-80)**

Remove `project_id` query parameter:

```python
@router.get("/sessions", response_model=list[AIPlanningSessionSummary])
def list_planning_sessions_route(
    session: Session = Depends(get_db_session),
    current_user: User = Depends(require_demo_user),
) -> list[AIPlanningSessionSummary]:
    return list_planning_sessions(session, actor_user_id=current_user.id)
```

- [ ] **Step 3: Add new route handlers before the SSE routes (before line 228)**

```python
# ---------------------------------------------------------------------------
# Session-Project association endpoints
# ---------------------------------------------------------------------------

@router.get("/sessions/{session_id}/projects", response_model=list[ProjectSummaryInSession])
def list_session_projects_route(
    session_id: int,
    session: Session = Depends(get_db_session),
    current_user: User = Depends(require_demo_user),
) -> list[ProjectSummaryInSession]:
    try:
        return list_session_projects(session, session_id, actor_user_id=current_user.id)
    except EntityNotFoundError as exc:
        raise HTTPException(status_code=404, detail=str(exc)) from exc
    except AIPlanningAccessError as exc:
        raise HTTPException(status_code=403, detail=str(exc)) from exc


@router.post("/sessions/{session_id}/projects", response_model=ProjectSummaryInSession, status_code=status.HTTP_201_CREATED)
def link_project_route(
    session_id: int,
    payload: LinkProjectRequest,
    session: Session = Depends(get_db_session),
    current_user: User = Depends(require_demo_user),
) -> ProjectSummaryInSession:
    try:
        return link_project_to_session(session, session_id, project_id=payload.project_id, actor_user_id=current_user.id)
    except EntityNotFoundError as exc:
        raise HTTPException(status_code=404, detail=str(exc)) from exc
    except AIPlanningAccessError as exc:
        raise HTTPException(status_code=403, detail=str(exc)) from exc


@router.delete("/sessions/{session_id}/projects/{project_id}", status_code=status.HTTP_204_NO_CONTENT)
def unlink_project_route(
    session_id: int,
    project_id: int,
    session: Session = Depends(get_db_session),
    current_user: User = Depends(require_demo_user),
) -> Response:
    try:
        unlink_project_from_session(session, session_id, project_id=project_id, actor_user_id=current_user.id)
    except EntityNotFoundError as exc:
        raise HTTPException(status_code=404, detail=str(exc)) from exc
    except AIPlanningAccessError as exc:
        raise HTTPException(status_code=403, detail=str(exc)) from exc
    return Response(status_code=status.HTTP_204_NO_CONTENT)


@router.post("/sessions/{session_id}/projects:create", response_model=ProjectSummaryInSession, status_code=status.HTTP_201_CREATED)
def create_project_in_session_route(
    session_id: int,
    payload: CreateProjectInSessionRequest,
    session: Session = Depends(get_db_session),
    current_user: User = Depends(require_demo_user),
) -> ProjectSummaryInSession:
    try:
        return create_project_in_session(
            session, session_id,
            name=payload.name, description=payload.description,
            actor_user_id=current_user.id,
        )
    except EntityNotFoundError as exc:
        raise HTTPException(status_code=404, detail=str(exc)) from exc
    except AIPlanningAccessError as exc:
        raise HTTPException(status_code=403, detail=str(exc)) from exc
```

- [ ] **Step 4: Verify routes register**

Run: `cd backend && uv run python -c "from app.api.routes.ai_planning import router; print(len(router.routes), 'routes')"`
Expected: Number of routes increased by 4.

- [ ] **Step 5: Commit**

```bash
git add backend/app/api/routes/ai_planning.py
git commit -m "feat(routes): add session-project link/unlink/list/create endpoints, remove project_id filter"
```

### Task 7: AI Agent/Tools — `project_id: int` → `project_id: int` with No-Project Guard

**Files:**
- Modify: `backend/app/ai/planning_tools.py:56-77` — update `execute_tool` signature and handlers
- Modify: `backend/app/ai/test_planning_agent.py:65-96` — update `run_planning_turn` and `stream_planning_turn`

The agent/tools layer currently takes `project_id: int` as a required parameter. We keep the same parameter name but handle `0` as "no project". Individual tools that need a project return a friendly message when `project_id == 0`.

- [ ] **Step 1: Update `execute_tool` in `backend/app/ai/planning_tools.py` (line 56-77)**

No signature change needed — `project_id: int` stays. But add a guard inside `execute_tool`:

```python
def execute_tool(
    *,
    tool_name: str,
    params: dict[str, Any],
    db_session: Session,
    project_id: int,
) -> str:
    """Execute a tool by name and return the result as a JSON string."""
    # project_id == 0 means no project linked to session
    _NO_PROJECT_MSG = json.dumps(
        {"info": "当前会话未关联项目，请先创建或关联项目后再使用此功能。"},
        ensure_ascii=False,
    )

    _PROJECT_REQUIRED_TOOLS = {
        "get_project_test_cases",
        "get_project_test_status",
        "get_recommended_retest",
        "get_project_insights",
        "explore_page",
        "explore_flow",
    }

    tool_def = _TOOL_REGISTRY.get(tool_name)
    if tool_def is None:
        return json.dumps({"error": f"工具 '{tool_name}' 不存在"}, ensure_ascii=False)

    handler = _TOOL_HANDLERS.get(tool_name)
    if handler is None:
        return json.dumps({"error": f"工具 '{tool_name}' 未注册处理函数"}, ensure_ascii=False)

    if not project_id and tool_name in _PROJECT_REQUIRED_TOOLS:
        return _NO_PROJECT_MSG

    try:
        result = handler(params=params, db_session=db_session, project_id=project_id)
        return json.dumps(result, ensure_ascii=False, default=str)
    except Exception as exc:
        logger.warning("Tool %s execution failed: %s", tool_name, exc)
        return json.dumps({"error": f"工具执行失败: {exc!s}"}, ensure_ascii=False)
```

- [ ] **Step 2: Verify agent imports**

Run: `cd backend && uv run python -c "from app.ai.planning_tools import execute_tool; print('OK')"`
Expected: `OK`

- [ ] **Step 3: Commit**

```bash
git add backend/app/ai/planning_tools.py
git commit -m "feat(tools): guard project-dependent tools when session has no project"
```

### Task 8: Frontend Types — Update Session Types + Add New Types

**Files:**
- Modify: `frontend/src/types/api.ts:232-245,278-289` — update `AIPlanningSession`, `AIPlanningSessionSummary`, `CreatePlanningSessionPayload`

- [ ] **Step 1: Add `ProjectSummaryInSession` interface (before `AIPlanningSession`)**

```typescript
export interface ProjectSummaryInSession {
  id: number;
  name: string;
  description: string | null;
}
```

- [ ] **Step 2: Update `AIPlanningSession` interface (line 232-245)**

Remove `project_id`, add `projects`:

```typescript
export interface AIPlanningSession {
  id: number;
  actor_user_id: number;
  case_id?: number | null;
  title?: string | null;
  status: AIPlanningSessionStatus;
  requirements: AIPlanningRequirements;
  plan?: AIPlanningPlan | null;
  missing_slots: string[];
  last_error_message?: string | null;
  created_at: string;
  updated_at: string;
  projects: ProjectSummaryInSession[];
}
```

- [ ] **Step 3: Update `AIPlanningSessionSummary` interface (line 278-284)**

Add `projects`:

```typescript
export interface AIPlanningSessionSummary {
  id: number;
  title: string | null;
  status: AIPlanningSessionStatus;
  created_at: string;
  updated_at: string;
  projects: ProjectSummaryInSession[];
}
```

- [ ] **Step 4: Update `CreatePlanningSessionPayload` (line 286-289)**

Remove `project_id`:

```typescript
export interface CreatePlanningSessionPayload {
  case_id?: number | null;
}
```

- [ ] **Step 5: Add new request types at end of file**

```typescript
export interface LinkProjectPayload {
  project_id: number;
}

export interface CreateProjectInSessionPayload {
  name: string;
  description?: string | null;
}
```

- [ ] **Step 6: Verify TypeScript compiles**

Run: `cd frontend && npx tsc --noEmit 2>&1 | head -20`
Expected: Errors in files that still reference `project_id` on session objects — those will be fixed in later tasks.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/types/api.ts
git commit -m "feat(types): remove project_id from session types, add association types"
```

### Task 9: Frontend API Service — Update Functions + Add New Ones

**Files:**
- Modify: `frontend/src/services/api.ts:277-290` — update `createPlanningSession`, `listPlanningSessions`
- Add new functions for session-project CRUD

- [ ] **Step 1: Update type imports at top of file**

Add new types to the import block:

```typescript
import type {
  ...existing...,
  LinkProjectPayload,
  CreateProjectInSessionPayload,
  ProjectSummaryInSession,
} from "../types/api";
```

- [ ] **Step 2: Update `createPlanningSession` (line 277-281)**

Remove `project_id` from payload:

```typescript
export function createPlanningSession(payload: CreatePlanningSessionPayload) {
  return request<AIPlanningSessionDetail>("/api/v1/ai-planning/sessions", {
    method: "POST",
    body: JSON.stringify(payload),
  });
}
```

- [ ] **Step 3: Update `listPlanningSessions` (line 288-291)**

Remove `projectId` parameter:

```typescript
export function listPlanningSessions() {
  return request<AIPlanningSessionSummary[]>("/api/v1/ai-planning/sessions");
}
```

- [ ] **Step 4: Add new API functions after existing planning functions**

```typescript
export function listSessionProjects(sessionId: number) {
  return request<ProjectSummaryInSession[]>(`/api/v1/ai-planning/sessions/${sessionId}/projects`);
}

export function linkProjectToSession(sessionId: number, payload: LinkProjectPayload) {
  return request<ProjectSummaryInSession>(`/api/v1/ai-planning/sessions/${sessionId}/projects`, {
    method: "POST",
    body: JSON.stringify(payload),
  });
}

export function unlinkProjectFromSession(sessionId: number, projectId: number) {
  return request<void>(`/api/v1/ai-planning/sessions/${sessionId}/projects/${projectId}`, {
    method: "DELETE",
  });
}

export function createProjectInSession(sessionId: number, payload: CreateProjectInSessionPayload) {
  return request<ProjectSummaryInSession>(`/api/v1/ai-planning/sessions/${sessionId}/projects:create`, {
    method: "POST",
    body: JSON.stringify(payload),
  });
}
```

- [ ] **Step 5: Commit**

```bash
git add frontend/src/services/api.ts
git commit -m "feat(api): update planning session functions, add session-project CRUD"
```

### Task 10: Frontend — New SessionListPage

**Files:**
- Create: `frontend/src/pages/SessionListPage.tsx`

This is the new entry point for the AI planning feature. Shows all sessions as cards with project tags, and a "New Session" button.

- [ ] **Step 1: Create `frontend/src/pages/SessionListPage.tsx`**

```tsx
import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Button, Card, Tag, Empty, Spin, Typography, message } from "antd";
import { PlusOutlined } from "@ant-design/icons";
import {
  createPlanningSession,
  listPlanningSessions,
} from "../services/api";
import type { AIPlanningSessionSummary } from "../types/api";

const STATUS_LABELS: Record<string, { label: string; color: string }> = {
  collecting: { label: "收集中", color: "processing" },
  plan_ready: { label: "计划就绪", color: "warning" },
  drafts_ready: { label: "草案就绪", color: "success" },
  reviewing: { label: "审查中", color: "processing" },
  saving: { label: "保存中", color: "processing" },
  executing: { label: "执行中", color: "processing" },
  completed: { label: "已完成", color: "default" },
  closed: { label: "已关闭", color: "default" },
  error: { label: "错误", color: "error" },
};

export function SessionListPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [creating, setCreating] = useState(false);

  const sessionsQuery = useQuery({
    queryKey: ["planning-sessions"],
    queryFn: listPlanningSessions,
  });

  async function handleCreate() {
    setCreating(true);
    try {
      const detail = await createPlanningSession({});
      queryClient.invalidateQueries({ queryKey: ["planning-sessions"] });
      navigate(`/planning/sessions/${detail.session.id}`);
    } catch (err) {
      void message.error(err instanceof Error ? err.message : "创建会话失败");
    } finally {
      setCreating(false);
    }
  }

  const sessions = sessionsQuery.data ?? [];

  return (
    <div style={{ padding: 24, maxWidth: 900, margin: "0 auto" }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 24 }}>
        <Typography.Title level={3} style={{ margin: 0 }}>AI 测试规划</Typography.Title>
        <Button type="primary" icon={<PlusOutlined />} loading={creating} onClick={handleCreate}>
          新建会话
        </Button>
      </div>

      {sessionsQuery.isLoading ? (
        <Spin />
      ) : sessions.length === 0 ? (
        <Empty description="暂无规划会话，点击「新建会话」开始" />
      ) : (
        <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
          {sessions.map((s) => {
            const statusInfo = STATUS_LABELS[s.status] ?? { label: s.status, color: "default" };
            return (
              <Card
                key={s.id}
                hoverable
                size="small"
                onClick={() => navigate(`/planning/sessions/${s.id}`)}
                style={{ cursor: "pointer" }}
              >
                <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                  <div>
                    <Typography.Text strong>
                      {s.title || `会话 #${s.id}`}
                    </Typography.Text>
                    <div style={{ marginTop: 4, display: "flex", gap: 4, flexWrap: "wrap" }}>
                      {s.projects.map((p) => (
                        <Tag key={p.id} color="blue">{p.name}</Tag>
                      ))}
                      {s.projects.length === 0 && (
                        <Tag color="default">未关联项目</Tag>
                      )}
                    </div>
                  </div>
                  <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
                    <Tag color={statusInfo.color}>{statusInfo.label}</Tag>
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      {new Date(s.updated_at).toLocaleString()}
                    </Typography.Text>
                  </div>
                </div>
              </Card>
            );
          })}
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 2: Verify file compiles**

Run: `cd frontend && npx tsc --noEmit 2>&1 | grep SessionListPage`
Expected: No errors for this file (other files may still have errors from `project_id` changes — those are fixed in later tasks).

- [ ] **Step 3: Commit**

```bash
git add frontend/src/pages/SessionListPage.tsx
git commit -m "feat(ui): add SessionListPage as AI planning entry point"
```

### Task 11: Frontend — SessionProjectPanel Component

**Files:**
- Create: `frontend/src/components/SessionProjectPanel.tsx`

This component renders inside `AITestPlanningPanel` to manage project associations.

- [ ] **Step 1: Create `frontend/src/components/SessionProjectPanel.tsx`**

```tsx
import { useState } from "react";
import { Button, Select, Tag, Input, Modal, Space, message } from "antd";
import { PlusOutlined, LinkOutlined, CloseOutlined } from "@ant-design/icons";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  getProjects,
  linkProjectToSession,
  unlinkProjectFromSession,
  createProjectInSession,
  listSessionProjects,
} from "../services/api";
import type { ProjectSummaryInSession } from "../types/api";

interface SessionProjectPanelProps {
  sessionId: number;
  onProjectsChange?: () => void;
}

export function SessionProjectPanel({ sessionId, onProjectsChange }: SessionProjectPanelProps) {
  const queryClient = useQueryClient();
  const [linking, setLinking] = useState(false);
  const [showCreate, setShowCreate] = useState(false);
  const [newName, setNewName] = useState("");
  const [newDesc, setNewDesc] = useState("");
  const [creating, setCreating] = useState(false);

  const projectsQuery = useQuery({
    queryKey: ["session-projects", sessionId],
    queryFn: () => listSessionProjects(sessionId),
  });

  const allProjectsQuery = useQuery({
    queryKey: ["projects"],
    queryFn: getProjects,
  });

  const linkedIds = new Set((projectsQuery.data ?? []).map((p) => p.id));
  const unlinkedProjects = (allProjectsQuery.data ?? []).filter((p) => !linkedIds.has(p.id));

  async function handleLink(projectId: number) {
    setLinking(true);
    try {
      await linkProjectToSession(sessionId, { project_id: projectId });
      queryClient.invalidateQueries({ queryKey: ["session-projects", sessionId] });
      onProjectsChange?.();
      void message.success("项目已关联");
    } catch (err) {
      void message.error(err instanceof Error ? err.message : "关联失败");
    } finally {
      setLinking(false);
    }
  }

  async function handleUnlink(projectId: number) {
    try {
      await unlinkProjectFromSession(sessionId, projectId);
      queryClient.invalidateQueries({ queryKey: ["session-projects", sessionId] });
      onProjectsChange?.();
      void message.success("已取消关联");
    } catch (err) {
      void message.error(err instanceof Error ? err.message : "取消关联失败");
    }
  }

  async function handleCreate() {
    if (!newName.trim()) return;
    setCreating(true);
    try {
      await createProjectInSession(sessionId, { name: newName.trim(), description: newDesc.trim() || null });
      queryClient.invalidateQueries({ queryKey: ["session-projects", sessionId] });
      queryClient.invalidateQueries({ queryKey: ["projects"] });
      setShowCreate(false);
      setNewName("");
      setNewDesc("");
      onProjectsChange?.();
      void message.success("项目已创建并关联");
    } catch (err) {
      void message.error(err instanceof Error ? err.message : "创建失败");
    } finally {
      setCreating(false);
    }
  }

  const projects = projectsQuery.data ?? [];

  return (
    <div style={{ display: "flex", gap: 8, alignItems: "center", flexWrap: "wrap" }}>
      {projects.map((p) => (
        <Tag
          key={p.id}
          closable
          onClose={(e) => { e.preventDefault(); handleUnlink(p.id); }}
          color="blue"
        >
          {p.name}
        </Tag>
      ))}
      {projects.length === 0 && <Tag color="default">未关联项目</Tag>}

      {unlinkedProjects.length > 0 && (
        <Select
          size="small"
          placeholder="关联已有项目"
          style={{ width: 160 }}
          loading={linking}
          value={undefined}
          onChange={(val: number) => handleLink(val)}
          options={unlinkedProjects.map((p) => ({ value: p.id, label: p.name }))}
        />
      )}

      <Button size="small" icon={<PlusOutlined />} onClick={() => setShowCreate(true)}>
        新建项目
      </Button>

      <Modal
        open={showCreate}
        title="创建新项目"
        onCancel={() => setShowCreate(false)}
        onOk={handleCreate}
        confirmLoading={creating}
        okText="创建并关联"
      >
        <Space direction="vertical" style={{ width: "100%" }}>
          <Input placeholder="项目名称" value={newName} onChange={(e) => setNewName(e.target.value)} />
          <Input.TextArea placeholder="项目描述（可选）" value={newDesc} onChange={(e) => setNewDesc(e.target.value)} rows={3} />
        </Space>
      </Modal>
    </div>
  );
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/components/SessionProjectPanel.tsx
git commit -m "feat(ui): add SessionProjectPanel for managing session-project associations"
```

### Task 12: Frontend — Rewrite PlanningPage as Session Detail + Update AITestPlanningPanel

**Files:**
- Modify: `frontend/src/pages/PlanningPage.tsx` — rewrite as session detail page
- Modify: `frontend/src/components/AITestPlanningPanel.tsx:36-46,203,206-209,237-238,264-306,601-603` — remove `projectId` dependency

- [ ] **Step 1: Rewrite `frontend/src/pages/PlanningPage.tsx`**

The page is now a session detail page reached via `/planning/sessions/:sessionId`:

```tsx
import { useParams, useNavigate } from "react-router-dom";
import { AITestPlanningPanel } from "../components/AITestPlanningPanel";
import { useQuery } from "@tanstack/react-query";
import { getAISettings } from "../services/api";

export function PlanningPage() {
  const { sessionId } = useParams<{ sessionId: string }>();
  const navigate = useNavigate();
  const aiSettingsQuery = useQuery({ queryKey: ["ai-settings"], queryFn: getAISettings });

  if (!sessionId) {
    navigate("/planning");
    return null;
  }

  return (
    <AITestPlanningPanel
      aiSettings={aiSettingsQuery.data ?? null}
      sessionId={Number(sessionId)}
      onImportDraft={async () => {
        /* handled within panel via session projects */
      }}
    />
  );
}
```

- [ ] **Step 2: Update `AITestPlanningPanel` props and initialization**

In `frontend/src/components/AITestPlanningPanel.tsx`:

**2a. Update props type (line 36-46):**

```typescript
type AITestPlanningPanelProps = {
  aiSettings?: AISettings | null;
  sessionId: number;
  caseId?: number;
  currentCase?: DSLCasePayload | null;
  currentSteps?: DSLStep[] | null;
  currentInputContract?: DSLCaseInputContract[] | null;
  currentOutputContract?: DSLCaseOutputContract[] | null;
  onImportDraft: (draft: AIPlanningDraft) => void | Promise<void>;
  draftImportLabel?: string;
};
```

**2b. Remove `isDisabled` that gates on `projectId` (line 203):**

```typescript
const planningEnabled = Boolean(aiSettings?.enable_ai_planning);
const isDisabled = !planningEnabled;
```

**2c. Update `loadSessionList` (line 205-216) — remove projectId filter:**

```typescript
async function loadSessionList() {
  setIsLoadingHistory(true);
  try {
    const list = await listPlanningSessions();
    setSessionList(list);
  } catch {
    // silently fail
  } finally {
    setIsLoadingHistory(false);
  }
}
```

**2d. Update `createAndSelectSession` (line 235-243) — remove project_id:**

```typescript
async function createAndSelectSession() {
  const detail = await createPlanningSession({
    case_id: caseId ?? null,
  });
  applySessionDetail(detail);
  return detail;
}
```

**2e. Rewrite `useEffect` init (line 264-306) — use `sessionId` prop instead of `projectId`:**

```typescript
useEffect(() => {
    let cancelled = false;

    async function init() {
      setIsBootstrapping(true);
      try {
        // Load the session detail directly
        await loadSessionDetail(sessionId);

        if (!cancelled) await loadSessionList();
      } catch (err: unknown) {
        if (!cancelled) {
          void messageApi.error(err instanceof Error ? err.message : String(err));
        }
      } finally {
        if (!cancelled) setIsBootstrapping(false);
      }
    }

    void init();
    return () => { cancelled = true; };
  }, [sessionId]);
```

**2f. Replace the "no projectId" warning (line 601-603) with `SessionProjectPanel`:**

```tsx
{!planningEnabled ? (
  <Alert type="warning" showIcon message="AI 规划功能未启用" style={{ marginTop: 12 }} />
) : null}
<div style={{ marginTop: 8 }}>
  <SessionProjectPanel sessionId={sessionId} onProjectsChange={() => {
    queryClient.invalidateQueries({ queryKey: ["planning-sessions"] });
  }} />
</div>
```

Also add the import at top of file:

```typescript
import { SessionProjectPanel } from "./SessionProjectPanel";
```

And `import { useNavigate } from "react-router-dom";` for navigation if not present.

- [ ] **Step 3: Verify TypeScript compiles**

Run: `cd frontend && npx tsc --noEmit 2>&1 | head -30`
Expected: No errors for PlanningPage and AITestPlanningPanel.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/pages/PlanningPage.tsx frontend/src/components/AITestPlanningPanel.tsx
git commit -m "feat(ui): rewrite PlanningPage as session detail, decouple AITestPlanningPanel from projectId"
```

### Task 13: Frontend Router — Add Session List Route

**Files:**
- Modify: `frontend/src/app/AppRouter.tsx:1-43` — add SessionListPage route, update PlanningPage route

- [ ] **Step 1: Add SessionListPage lazy import**

```tsx
const SessionListPage = lazy(() =>
  import("../pages/SessionListPage").then((m) => ({ default: m.SessionListPage })),
);
```

- [ ] **Step 2: Update routes**

```tsx
export function AppRouter() {
  return (
    <Suspense fallback={<LoadingBlock />}>
      <Routes>
        <Route path="/planning" element={<SessionListPage />} />
        <Route path="/planning/sessions/:sessionId" element={<PlanningPage />} />
        <Route path="/" element={<Navigate to="/planning" replace />} />
        <Route path="/cases" element={<CasesPage />} />
        <Route path="/cases/:caseId/edit" element={<CaseEditPage />} />
        <Route path="/reports" element={<ReportPage />} />
        <Route path="/run/:executionId" element={<ExecutionDetailPage />} />
        <Route path="/executions/:executionId" element={<LegacyExecutionRedirect />} />
        <Route path="/dashboard" element={<Navigate to="/planning" replace />} />
        <Route path="/executions" element={<Navigate to="/cases" replace />} />
        <Route path="/login" element={<Navigate to="/planning" replace />} />
      </Routes>
    </Suspense>
  );
}
```

Key changes:
- `/` now redirects to `/planning` (session list)
- `/planning` shows `SessionListPage`
- `/planning/sessions/:sessionId` shows `PlanningPage` (session detail)

- [ ] **Step 3: Verify build**

Run: `cd frontend && npm run build 2>&1 | tail -5`
Expected: Build succeeds.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/app/AppRouter.tsx
git commit -m "feat(router): add session list route, update default path to /planning"
```

### Task 14: Backend Unit Tests — Session-Project Association CRUD

**Files:**
- Create: `backend/tests/unit/test_session_project_service.py`

- [ ] **Step 1: Create test file**

```python
"""Unit tests for session-project association CRUD."""

import pytest
from sqlalchemy import create_engine
from sqlalchemy.orm import Session as SA_Session, sessionmaker

from app.db.base import Base
from app.models import AIPlanningSession, Project, SessionProject, User
from app.services.ai_planning import (
    create_planning_session,
    link_project_to_session,
    unlink_project_from_session,
    list_session_projects,
    create_project_in_session,
    list_planning_sessions,
)
from app.schemas.ai_planning import CreateAIPlanningSessionRequest


@pytest.fixture
def db_session():
    engine = create_engine("sqlite:///:memory:")
    Base.metadata.create_all(engine)
    Session = sessionmaker(bind=engine)
    session = Session()

    # Seed a user
    user = User(username="testuser", email="test@test.com")
    session.add(user)
    session.commit()

    yield session
    session.close()


def _user_id(db_session: SA_Session) -> int:
    return db_session.query(User).first().id


class TestCreateSessionWithoutProject:
    def test_creates_session_without_project(self, db_session):
        detail = create_planning_session(
            db_session,
            CreateAIPlanningSessionRequest(),
            actor_user_id=_user_id(db_session),
        )
        assert detail.session.projects == []
        assert detail.session.status == "collecting"


class TestLinkProject:
    def test_link_project_to_session(self, db_session):
        uid = _user_id(db_session)
        project = Project(name="TestProject")
        db_session.add(project)
        db_session.commit()

        detail = create_planning_session(db_session, CreateAIPlanningSessionRequest(), actor_user_id=uid)
        result = link_project_to_session(db_session, detail.session.id, project_id=project.id, actor_user_id=uid)

        assert result.id == project.id
        assert result.name == "TestProject"

    def test_link_nonexistent_project_raises(self, db_session):
        uid = _user_id(db_session)
        detail = create_planning_session(db_session, CreateAIPlanningSessionRequest(), actor_user_id=uid)
        with pytest.raises(Exception):
            link_project_to_session(db_session, detail.session.id, project_id=999, actor_user_id=uid)


class TestUnlinkProject:
    def test_unlink_project(self, db_session):
        uid = _user_id(db_session)
        project = Project(name="TestProject")
        db_session.add(project)
        db_session.commit()

        detail = create_planning_session(db_session, CreateAIPlanningSessionRequest(), actor_user_id=uid)
        link_project_to_session(db_session, detail.session.id, project_id=project.id, actor_user_id=uid)

        unlink_project_from_session(db_session, detail.session.id, project_id=project.id, actor_user_id=uid)

        projects = list_session_projects(db_session, detail.session.id, actor_user_id=uid)
        assert len(projects) == 0

    def test_unlink_nonexistent_raises(self, db_session):
        uid = _user_id(db_session)
        detail = create_planning_session(db_session, CreateAIPlanningSessionRequest(), actor_user_id=uid)
        with pytest.raises(Exception):
            unlink_project_from_session(db_session, detail.session.id, project_id=999, actor_user_id=uid)


class TestListSessionProjects:
    def test_returns_empty_when_no_projects(self, db_session):
        uid = _user_id(db_session)
        detail = create_planning_session(db_session, CreateAIPlanningSessionRequest(), actor_user_id=uid)
        projects = list_session_projects(db_session, detail.session.id, actor_user_id=uid)
        assert projects == []

    def test_returns_linked_projects(self, db_session):
        uid = _user_id(db_session)
        p1 = Project(name="P1")
        p2 = Project(name="P2")
        db_session.add_all([p1, p2])
        db_session.commit()

        detail = create_planning_session(db_session, CreateAIPlanningSessionRequest(), actor_user_id=uid)
        link_project_to_session(db_session, detail.session.id, project_id=p1.id, actor_user_id=uid)
        link_project_to_session(db_session, detail.session.id, project_id=p2.id, actor_user_id=uid)

        projects = list_session_projects(db_session, detail.session.id, actor_user_id=uid)
        assert len(projects) == 2
        names = {p.name for p in projects}
        assert names == {"P1", "P2"}


class TestCreateProjectInSession:
    def test_creates_and_links(self, db_session):
        uid = _user_id(db_session)
        detail = create_planning_session(db_session, CreateAIPlanningSessionRequest(), actor_user_id=uid)

        result = create_project_in_session(
            db_session, detail.session.id,
            name="NewProject", description="desc", actor_user_id=uid,
        )
        assert result.name == "NewProject"
        assert result.description == "desc"

        projects = list_session_projects(db_session, detail.session.id, actor_user_id=uid)
        assert len(projects) == 1
        assert projects[0].name == "NewProject"


class TestListSessionsWithProjects:
    def test_sessions_include_projects(self, db_session):
        uid = _user_id(db_session)
        project = Project(name="SharedProject")
        db_session.add(project)
        db_session.commit()

        detail = create_planning_session(db_session, CreateAIPlanningSessionRequest(), actor_user_id=uid)
        link_project_to_session(db_session, detail.session.id, project_id=project.id, actor_user_id=uid)

        sessions = list_planning_sessions(db_session, actor_user_id=uid)
        assert len(sessions) >= 1
        found = next(s for s in sessions if s.id == detail.session.id)
        assert len(found.projects) == 1
        assert found.projects[0].name == "SharedProject"
```

- [ ] **Step 2: Run tests**

Run: `cd backend && uv run pytest tests/unit/test_session_project_service.py -v`
Expected: All tests PASS.

- [ ] **Step 3: Commit**

```bash
git add backend/tests/unit/test_session_project_service.py
git commit -m "test: add unit tests for session-project association CRUD"
```