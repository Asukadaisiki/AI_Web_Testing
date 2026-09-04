"""Internal browser capability routes used during the Go migration."""

from __future__ import annotations

from fastapi import APIRouter, Depends, HTTPException
from sqlalchemy.orm import Session

from app.application.browser import execute_browser_capability
from app.db import get_db_session
from app.schemas.browser_capabilities import (
    BrowserCapabilityName,
    BrowserCapabilityRequest,
    BrowserCapabilityResponse,
)


router = APIRouter(prefix="/internal/browser-capabilities", tags=["internal-browser"])


@router.post("/{capability}", response_model=BrowserCapabilityResponse)
def invoke_browser_capability(
    capability: BrowserCapabilityName,
    payload: BrowserCapabilityRequest,
    session: Session = Depends(get_db_session),
) -> BrowserCapabilityResponse:
    try:
        result = execute_browser_capability(
            session,
            capability=capability,
            project_id=payload.project_id,
            conversation_id=payload.conversation_id,
            arguments=payload.arguments,
        )
    except ValueError as exc:
        raise HTTPException(status_code=422, detail=str(exc)) from exc
    return BrowserCapabilityResponse(result=result)
