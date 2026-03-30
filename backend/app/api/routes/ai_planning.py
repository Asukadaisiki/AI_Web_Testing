"""Routes for AI planning sessions and drafts."""

from __future__ import annotations

from fastapi import APIRouter, Depends, HTTPException, Response, status
from sqlalchemy.orm import Session

from app.api.auth import require_authenticated_user
from app.db import get_db_session
from app.models import User
from app.schemas.ai_planning import (
    AIPlanningDraft,
    AIPlanningMessageCreateRequest,
    AIPlanningSessionDetail,
    AIPlanningTurnResponse,
    CreateAIPlanningSessionRequest,
    GenerateAIPlanningDraftsRequest,
    UpdateAIPlanningDraftStatusRequest,
)
from app.services.ai_planning import (
    AIPlanningAccessError,
    create_planning_session,
    generate_planning_drafts,
    get_planning_session_detail,
    send_planning_message,
    update_planning_draft_status,
)
from app.services.cases import EntityNotFoundError


router = APIRouter(prefix="/ai-planning", tags=["ai-planning"])


@router.post("/sessions", response_model=AIPlanningSessionDetail, status_code=status.HTTP_201_CREATED)
def create_planning_session_route(
    payload: CreateAIPlanningSessionRequest,
    response: Response,
    session: Session = Depends(get_db_session),
    current_user: User = Depends(require_authenticated_user),
) -> AIPlanningSessionDetail:
    try:
        detail = create_planning_session(session, payload, actor_user_id=current_user.id)
    except EntityNotFoundError as exc:
        raise HTTPException(status_code=404, detail=str(exc)) from exc
    response.headers["Location"] = f"/api/v1/ai-planning/sessions/{detail.session.id}"
    return detail


@router.get("/sessions/{session_id}", response_model=AIPlanningSessionDetail)
def get_planning_session_route(
    session_id: int,
    session: Session = Depends(get_db_session),
    current_user: User = Depends(require_authenticated_user),
) -> AIPlanningSessionDetail:
    try:
        return get_planning_session_detail(session, session_id, actor_user_id=current_user.id)
    except EntityNotFoundError as exc:
        raise HTTPException(status_code=404, detail=str(exc)) from exc
    except AIPlanningAccessError as exc:
        raise HTTPException(status_code=403, detail=str(exc)) from exc


@router.post("/sessions/{session_id}/messages", response_model=AIPlanningTurnResponse)
def send_planning_message_route(
    session_id: int,
    payload: AIPlanningMessageCreateRequest,
    session: Session = Depends(get_db_session),
    current_user: User = Depends(require_authenticated_user),
) -> AIPlanningTurnResponse:
    try:
        return send_planning_message(session, session_id, actor_user_id=current_user.id, content=payload.content)
    except EntityNotFoundError as exc:
        raise HTTPException(status_code=404, detail=str(exc)) from exc
    except AIPlanningAccessError as exc:
        raise HTTPException(status_code=403, detail=str(exc)) from exc


@router.post("/sessions/{session_id}/drafts:generate", response_model=AIPlanningTurnResponse)
def generate_planning_drafts_route(
    session_id: int,
    payload: GenerateAIPlanningDraftsRequest,
    session: Session = Depends(get_db_session),
    current_user: User = Depends(require_authenticated_user),
) -> AIPlanningTurnResponse:
    try:
        return generate_planning_drafts(session, session_id, payload, actor_user_id=current_user.id)
    except EntityNotFoundError as exc:
        raise HTTPException(status_code=404, detail=str(exc)) from exc
    except AIPlanningAccessError as exc:
        raise HTTPException(status_code=403, detail=str(exc)) from exc


@router.patch("/drafts/{draft_id}", response_model=AIPlanningDraft)
def update_planning_draft_status_route(
    draft_id: int,
    payload: UpdateAIPlanningDraftStatusRequest,
    session: Session = Depends(get_db_session),
    current_user: User = Depends(require_authenticated_user),
) -> AIPlanningDraft:
    try:
        return update_planning_draft_status(session, draft_id, payload, actor_user_id=current_user.id)
    except EntityNotFoundError as exc:
        raise HTTPException(status_code=404, detail=str(exc)) from exc
    except AIPlanningAccessError as exc:
        raise HTTPException(status_code=403, detail=str(exc)) from exc
