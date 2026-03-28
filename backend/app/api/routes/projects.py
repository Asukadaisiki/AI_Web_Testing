"""Project selection routes."""

from __future__ import annotations

from fastapi import APIRouter, Depends
from sqlalchemy.orm import Session

from app.api.auth import require_authenticated_user
from app.db import get_db_session
from app.models import User
from app.schemas.projects import ProjectSummary
from app.services.projects import list_accessible_projects


router = APIRouter(prefix="/projects", tags=["projects"])


@router.get("", response_model=list[ProjectSummary])
def list_projects_route(
    session: Session = Depends(get_db_session),
    current_user: User = Depends(require_authenticated_user),
) -> list[ProjectSummary]:
    return list_accessible_projects(session, user_id=current_user.id)
