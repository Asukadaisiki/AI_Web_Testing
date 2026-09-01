"""Current-user dependencies for the single-user local environment."""

from __future__ import annotations

from fastapi import Depends, HTTPException, status
from sqlalchemy.orm import Session

from app.core.config import get_settings
from app.db import get_db_session
from app.models import User
from app.services.auth import get_user_by_email


def require_authenticated_user(
    session: Session = Depends(get_db_session),
) -> User:
    """Return the database-backed admin user without requiring a login session."""
    user = get_user_by_email(session, get_settings().auth_auto_login_email)
    if user is None or not user.is_active:
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail="默认管理员账号不存在或已停用。",
        )
    return user


def require_demo_user(
    session: Session = Depends(get_db_session),
) -> User:
    """Backward-compatible dependency used by existing business routes."""
    return require_authenticated_user(session)
