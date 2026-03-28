"""Authentication dependencies and helpers."""

from __future__ import annotations

from fastapi import Depends, HTTPException, Request, status
from sqlalchemy.orm import Session

from app.core.auth import SESSION_USER_ID_KEY
from app.db import get_db_session
from app.models import User
from app.services.auth import get_user_by_id


def require_authenticated_user(
    request: Request,
    session: Session = Depends(get_db_session),
) -> User:
    user_id = request.session.get(SESSION_USER_ID_KEY)
    if not isinstance(user_id, int):
        raise HTTPException(status_code=status.HTTP_401_UNAUTHORIZED, detail="未登录或登录态已失效。")

    user = get_user_by_id(session, user_id)
    if user is None or not user.is_active:
        request.session.clear()
        raise HTTPException(status_code=status.HTTP_401_UNAUTHORIZED, detail="未登录或登录态已失效。")
    return user
