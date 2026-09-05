"""Planning session lifecycle use cases."""

from __future__ import annotations

from sqlalchemy import select
from sqlalchemy.orm import Session

from app.application.planning.presenters import (
    to_draft_schema,
    to_message_schema,
    to_session_schema,
)
from app.application.planning.project_context import (
    get_active_project_id,
    get_owned_session,
)
from app.models import (
    AIPlanningDraft,
    AIPlanningMessage,
    AIPlanningSession,
    Project,
    ProjectMember,
    SessionProject,
)
from app.schemas.ai_planning import (
    AIPlanningRequirements,
    AIPlanningSessionDetail,
    AIPlanningSessionSummary,
    CreateAIPlanningSessionRequest,
    ProjectSummaryInSession,
    REQUIRED_REQUIREMENT_SLOTS,
)


def list_planning_sessions(
    session: Session,
    *,
    actor_user_id: int,
) -> list[AIPlanningSessionSummary]:
    records = (
        session.query(AIPlanningSession)
        .filter(AIPlanningSession.actor_user_id == actor_user_id)
        .order_by(AIPlanningSession.updated_at.desc())
        .all()
    )
    return [
        AIPlanningSessionSummary(
            id=record.id,
            runtime_owner=record.runtime_owner,
            active_project_id=get_active_project_id(record),
            title=record.title
            or (record.requirements_json or {}).get("app_under_test"),
            status=record.status,
            created_at=record.created_at,
            updated_at=record.updated_at,
            projects=[
                ProjectSummaryInSession(
                    id=project.id,
                    name=project.name,
                    description=project.description,
                    is_active=project.id == get_active_project_id(record),
                )
                for project in (record.projects or [])
            ],
        )
        for record in records
    ]


def create_planning_session(
    session: Session,
    payload: CreateAIPlanningSessionRequest,
    *,
    actor_user_id: int,
) -> AIPlanningSessionDetail:
    record = AIPlanningSession(
        actor_user_id=actor_user_id,
        case_id=payload.case_id,
        status="collecting",
        requirements_json=AIPlanningRequirements().model_dump(mode="json"),
        missing_slots_json=list(REQUIRED_REQUIREMENT_SLOTS),
    )
    session.add(record)
    session.flush()

    if payload.project_id is None:
        project = _get_default_project_for_user(session, actor_user_id)
        if project is None:
            project = Project(
                name=f"default-{record.id}",
                description="auto-created temporary project",
                is_default=True,
            )
            session.add(project)
            session.flush()
            session.add(
                ProjectMember(
                    project_id=project.id,
                    user_id=actor_user_id,
                    role="owner",
                )
            )
    else:
        project = session.get(Project, payload.project_id)
        if project is None:
            from app.services.cases import EntityNotFoundError

            raise EntityNotFoundError(f"Project {payload.project_id} not found.")

    session.add(
        SessionProject(
            session_id=record.id,
            project_id=project.id,
        )
    )
    record.active_project_id = project.id
    session.commit()
    session.refresh(record)
    return get_planning_session_detail(
        session,
        record.id,
        actor_user_id=actor_user_id,
    )


def _get_default_project_for_user(
    session: Session,
    actor_user_id: int,
) -> Project | None:
    return session.scalar(
        select(Project)
        .join(ProjectMember, ProjectMember.project_id == Project.id)
        .where(ProjectMember.user_id == actor_user_id)
        .order_by(Project.is_default.desc(), Project.id.asc())
        .limit(1)
    )


def get_planning_session_detail(
    session: Session,
    planning_session_id: int,
    *,
    actor_user_id: int,
) -> AIPlanningSessionDetail:
    planning_session = get_owned_session(
        session,
        planning_session_id,
        actor_user_id=actor_user_id,
    )
    messages = session.scalars(
        select(AIPlanningMessage)
        .where(AIPlanningMessage.session_id == planning_session_id)
        .order_by(AIPlanningMessage.id.asc())
    ).all()
    drafts = session.scalars(
        select(AIPlanningDraft)
        .where(AIPlanningDraft.session_id == planning_session_id)
        .order_by(AIPlanningDraft.id.asc())
    ).all()
    return AIPlanningSessionDetail(
        session=to_session_schema(planning_session),
        messages=[to_message_schema(item) for item in messages],
        drafts=[to_draft_schema(item) for item in drafts],
    )


def delete_planning_session(
    session: Session,
    planning_session_id: int,
    *,
    actor_user_id: int,
) -> None:
    planning_session = get_owned_session(
        session,
        planning_session_id,
        actor_user_id=actor_user_id,
    )
    session.delete(planning_session)
    session.commit()
