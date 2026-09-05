"""Authorization checks shared by internal capability routes."""

from __future__ import annotations

from fastapi import HTTPException, status
from sqlalchemy import select
from sqlalchemy.orm import Session

from app.models import AIPlanningSession, ProjectMember, SessionProject, User


def require_capability_access(
    session: Session,
    current_user: User,
    *,
    project_id: int,
    conversation_id: str,
) -> None:
    membership = session.scalar(
        select(ProjectMember.id).where(
            ProjectMember.project_id == project_id,
            ProjectMember.user_id == current_user.id,
        )
    )
    if membership is None:
        raise HTTPException(
            status_code=status.HTTP_403_FORBIDDEN,
            detail="Project access denied.",
        )

    if not conversation_id.isdigit():
        return
    planning_session_id = int(conversation_id)
    owns_context = session.scalar(
        select(AIPlanningSession.id)
        .join(SessionProject, SessionProject.session_id == AIPlanningSession.id)
        .where(
            AIPlanningSession.id == planning_session_id,
            AIPlanningSession.actor_user_id == current_user.id,
            SessionProject.project_id == project_id,
        )
    )
    if owns_context is None:
        raise HTTPException(
            status_code=status.HTTP_403_FORBIDDEN,
            detail="Planning session access denied.",
        )
