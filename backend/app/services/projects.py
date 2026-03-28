"""Project query services."""

from __future__ import annotations

from sqlalchemy import select
from sqlalchemy.orm import Session

from app.models import Project, ProjectMember
from app.schemas.projects import ProjectSummary


def list_accessible_projects(session: Session, *, user_id: int) -> list[ProjectSummary]:
    statement = (
        select(Project)
        .join(ProjectMember, ProjectMember.project_id == Project.id)
        .where(ProjectMember.user_id == user_id)
        .order_by(Project.name.asc(), Project.id.asc())
    )
    return [
        ProjectSummary(id=record.id, name=record.name, description=record.description)
        for record in session.scalars(statement).all()
    ]
