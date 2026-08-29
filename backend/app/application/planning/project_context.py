"""Planning session ownership and active-project use cases."""

from __future__ import annotations

import logging

from sqlalchemy import select
from sqlalchemy.orm import Session

from app.models import AIPlanningSession, Project, ProjectMember, SessionProject
from app.schemas.ai_planning import ProjectSummaryInSession
from app.services.cases import EntityNotFoundError


logger = logging.getLogger(__name__)


class AIPlanningAccessError(ValueError):
    """Raised when a planning session is inaccessible."""


def get_owned_session(
    session: Session,
    planning_session_id: int,
    *,
    actor_user_id: int,
) -> AIPlanningSession:
    planning_session = session.get(AIPlanningSession, planning_session_id)
    if planning_session is None:
        raise EntityNotFoundError(f"AI planning session {planning_session_id} not found.")
    if planning_session.actor_user_id != actor_user_id:
        raise AIPlanningAccessError("AI planning session access denied.")
    return planning_session


def get_session_project_ids(planning_session: AIPlanningSession) -> list[int]:
    """Return linked project IDs with the active project first."""
    project_ids = sorted(project.id for project in (planning_session.projects or []))
    active_project_id = planning_session.active_project_id
    if active_project_id in project_ids:
        return [
            active_project_id,
            *(project_id for project_id in project_ids if project_id != active_project_id),
        ]
    return project_ids


def get_active_project_id(planning_session: AIPlanningSession) -> int | None:
    project_ids = get_session_project_ids(planning_session)
    return project_ids[0] if project_ids else None


def link_project_to_session(
    session: Session,
    planning_session_id: int,
    *,
    project_id: int,
    actor_user_id: int,
) -> ProjectSummaryInSession:
    planning_session = get_owned_session(
        session,
        planning_session_id,
        actor_user_id=actor_user_id,
    )
    project = session.get(Project, project_id)
    if project is None:
        raise EntityNotFoundError(f"Project {project_id} not found.")

    existing = session.scalar(
        select(SessionProject).where(
            SessionProject.session_id == planning_session_id,
            SessionProject.project_id == project_id,
        )
    )
    if existing is None:
        session.add(
            SessionProject(
                session_id=planning_session_id,
                project_id=project_id,
            )
        )

    planning_session.active_project_id = project_id
    session.commit()
    return ProjectSummaryInSession(
        id=project.id,
        name=project.name,
        description=project.description,
        is_active=True,
    )


def unlink_project_from_session(
    session: Session,
    planning_session_id: int,
    *,
    project_id: int,
    actor_user_id: int,
) -> None:
    planning_session = get_owned_session(
        session,
        planning_session_id,
        actor_user_id=actor_user_id,
    )
    link = session.scalar(
        select(SessionProject).where(
            SessionProject.session_id == planning_session_id,
            SessionProject.project_id == project_id,
        )
    )
    if link is None:
        raise EntityNotFoundError(
            f"Project {project_id} not linked to session {planning_session_id}."
        )

    session.delete(link)
    if planning_session.active_project_id == project_id:
        planning_session.active_project_id = session.scalar(
            select(SessionProject.project_id)
            .where(
                SessionProject.session_id == planning_session_id,
                SessionProject.id != link.id,
            )
            .order_by(SessionProject.id.asc())
            .limit(1)
        )
    session.commit()


def list_session_projects(
    session: Session,
    planning_session_id: int,
    *,
    actor_user_id: int,
) -> list[ProjectSummaryInSession]:
    planning_session = get_owned_session(
        session,
        planning_session_id,
        actor_user_id=actor_user_id,
    )
    active_project_id = get_active_project_id(planning_session)
    return [
        ProjectSummaryInSession(
            id=project.id,
            name=project.name,
            description=project.description,
            is_active=project.id == active_project_id,
        )
        for project in (planning_session.projects or [])
    ]


def create_project_in_session(
    session: Session,
    planning_session_id: int,
    *,
    name: str,
    description: str | None,
    actor_user_id: int,
) -> ProjectSummaryInSession:
    planning_session = get_owned_session(
        session,
        planning_session_id,
        actor_user_id=actor_user_id,
    )
    project = Project(name=name, description=description)
    session.add(project)
    session.flush()
    session.add(
        ProjectMember(
            project_id=project.id,
            user_id=actor_user_id,
            role="owner",
        )
    )
    session.add(
        SessionProject(
            session_id=planning_session_id,
            project_id=project.id,
        )
    )
    planning_session.active_project_id = project.id
    session.commit()
    session.refresh(project)
    return ProjectSummaryInSession(
        id=project.id,
        name=project.name,
        description=project.description,
        is_active=True,
    )


def ensure_project_member_for_session_projects(
    session: Session,
    planning_session_id: int,
    actor_user_id: int,
) -> None:
    """Ensure the session owner is a member of every linked project."""
    planning_session = get_owned_session(
        session,
        planning_session_id,
        actor_user_id=actor_user_id,
    )
    for project_id in get_session_project_ids(planning_session):
        existing = session.scalar(
            select(ProjectMember).where(
                ProjectMember.project_id == project_id,
                ProjectMember.user_id == actor_user_id,
            )
        )
        if existing is None:
            logger.info(
                "Adding user %d as owner of project %d",
                actor_user_id,
                project_id,
            )
            session.add(
                ProjectMember(
                    project_id=project_id,
                    user_id=actor_user_id,
                    role="owner",
                )
            )
    session.commit()
