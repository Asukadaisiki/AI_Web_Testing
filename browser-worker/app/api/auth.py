"""Current-user dependencies for the single-user local environment."""

from __future__ import annotations

from fastapi import Depends, HTTPException, Request, status
from sqlalchemy.orm import Session

from app.core.auth import SESSION_USER_ID_KEY
from app.db import get_db_session
from app.models import User


def require_authenticated_user(
    request: Request,
    session: Session = Depends(get_db_session),
) -> User:
    """Return the active user referenced by the signed request session."""
    raw_user_id = request.session.get(SESSION_USER_ID_KEY)
    if isinstance(raw_user_id, bool):
        raw_user_id = None
    try:
        user_id = int(raw_user_id)
    except (TypeError, ValueError):
        user_id = 0
    user = session.get(User, user_id) if user_id > 0 else None
    if user is None:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="请先登录。",
        )
    if not user.is_active:
        request.session.clear()
        raise HTTPException(
            status_code=status.HTTP_403_FORBIDDEN,
            detail="当前账号已停用。",
        )
    return user


def require_demo_user(
    request: Request,
    session: Session = Depends(get_db_session),
) -> User:
    """Backward-compatible dependency used by existing business routes."""
    return require_authenticated_user(request, session)
