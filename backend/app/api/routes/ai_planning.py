"""Routes for AI planning sessions and drafts."""

from __future__ import annotations

import asyncio
import logging

from fastapi import APIRouter, Depends, HTTPException, Query, Response, WebSocket, WebSocketDisconnect, status
from sqlalchemy.orm import Session

from app.api.auth import get_demo_user_or_raise, require_demo_user
from app.db import get_db_session
from app.db.session import get_session_factory
from app.models import User
from app.schemas.ai_planning import (
    AIPlanningDraft,
    AIPlanningMessageCreateRequest,
    AIPlanningSessionDetail,
    AIPlanningSessionSummary,
    AIPlanningTurnResponse,
    CreateAIPlanningSessionRequest,
    GenerateAIPlanningDraftsRequest,
    UpdateAIPlanningDraftStatusRequest,
)
from app.schemas.dsl import DSLModel
from pydantic import Field
from app.services.ai_planning import (
    AIPlanningAccessError,
    create_planning_session,
    delete_planning_draft,
    delete_planning_session,
    generate_planning_drafts,
    get_planning_session_detail,
    list_planning_sessions,
    retest_cases,
    save_and_execute_selected_drafts,
    send_planning_message,
    update_planning_draft_status,
)
from app.services.ai_planning_streaming import (
    CancellationManager,
    stream_explorer_judge,
    stream_planning_chat,
    stream_planning_drafts,
    stream_save_and_execute,
)
from app.services.cases import EntityNotFoundError

logger = logging.getLogger(__name__)

_cancellation_manager = CancellationManager()


router = APIRouter(prefix="/ai-planning", tags=["ai-planning"])


@router.post("/sessions", response_model=AIPlanningSessionDetail, status_code=status.HTTP_201_CREATED)
def create_planning_session_route(
    payload: CreateAIPlanningSessionRequest,
    response: Response,
    session: Session = Depends(get_db_session),
    current_user: User = Depends(require_demo_user),
) -> AIPlanningSessionDetail:
    try:
        detail = create_planning_session(session, payload, actor_user_id=current_user.id)
    except EntityNotFoundError as exc:
        raise HTTPException(status_code=404, detail=str(exc)) from exc
    response.headers["Location"] = f"/api/v1/ai-planning/sessions/{detail.session.id}"
    return detail


@router.get("/sessions", response_model=list[AIPlanningSessionSummary])
def list_planning_sessions_route(
    project_id: int | None = None,
    session: Session = Depends(get_db_session),
    current_user: User = Depends(require_demo_user),
) -> list[AIPlanningSessionSummary]:
    return list_planning_sessions(session, actor_user_id=current_user.id, project_id=project_id)


@router.get("/sessions/{session_id}", response_model=AIPlanningSessionDetail)
def get_planning_session_route(
    session_id: int,
    session: Session = Depends(get_db_session),
    current_user: User = Depends(require_demo_user),
) -> AIPlanningSessionDetail:
    try:
        return get_planning_session_detail(session, session_id, actor_user_id=current_user.id)
    except EntityNotFoundError as exc:
        raise HTTPException(status_code=404, detail=str(exc)) from exc
    except AIPlanningAccessError as exc:
        raise HTTPException(status_code=403, detail=str(exc)) from exc


@router.delete("/sessions/{session_id}", status_code=status.HTTP_204_NO_CONTENT)
def delete_planning_session_route(
    session_id: int,
    session: Session = Depends(get_db_session),
    current_user: User = Depends(require_demo_user),
) -> Response:
    try:
        delete_planning_session(session, session_id, actor_user_id=current_user.id)
    except (EntityNotFoundError, AIPlanningAccessError) as exc:
        raise HTTPException(status_code=404, detail=str(exc)) from exc
    return Response(status_code=status.HTTP_204_NO_CONTENT)


@router.post("/sessions/{session_id}/messages", response_model=AIPlanningTurnResponse)
def send_planning_message_route(
    session_id: int,
    payload: AIPlanningMessageCreateRequest,
    session: Session = Depends(get_db_session),
    current_user: User = Depends(require_demo_user),
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
    current_user: User = Depends(require_demo_user),
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
    current_user: User = Depends(require_demo_user),
) -> AIPlanningDraft:
    try:
        return update_planning_draft_status(session, draft_id, payload, actor_user_id=current_user.id)
    except EntityNotFoundError as exc:
        raise HTTPException(status_code=404, detail=str(exc)) from exc
    except AIPlanningAccessError as exc:
        raise HTTPException(status_code=403, detail=str(exc)) from exc


@router.delete("/drafts/{draft_id}", status_code=status.HTTP_204_NO_CONTENT)
def delete_planning_draft_route(
    draft_id: int,
    session: Session = Depends(get_db_session),
    current_user: User = Depends(require_demo_user),
) -> Response:
    try:
        delete_planning_draft(session, draft_id, actor_user_id=current_user.id)
    except (EntityNotFoundError, AIPlanningAccessError) as exc:
        raise HTTPException(status_code=404, detail=str(exc)) from exc
    return Response(status_code=status.HTTP_204_NO_CONTENT)


class SaveAndExecuteRequest(DSLModel):
    draft_ids: list[int]
    execute: bool = True
    input_values: dict[str, str] = Field(
        default_factory=dict,
        description="Variable substitutions for ${context_key} placeholders.",
    )


class RetestRequest(DSLModel):
    case_ids: list[int] | None = Field(default=None, description="要复测的用例 ID 列表")
    failed_only: bool = Field(default=False, description="仅复测最近失败的用例")
    input_values: dict[str, str] = Field(default_factory=dict)


@router.post("/sessions/{session_id}/retest", response_model=AIPlanningTurnResponse)
def retest_cases_route(
    session_id: int,
    payload: RetestRequest,
    session: Session = Depends(get_db_session),
    current_user: User = Depends(require_demo_user),
) -> AIPlanningTurnResponse:
    try:
        return retest_cases(
            session, session_id,
            actor_user_id=current_user.id,
            case_ids=payload.case_ids,
            failed_only=payload.failed_only,
            input_values=payload.input_values,
        )
    except EntityNotFoundError as exc:
        raise HTTPException(status_code=404, detail=str(exc)) from exc
    except AIPlanningAccessError as exc:
        raise HTTPException(status_code=403, detail=str(exc)) from exc


@router.post("/sessions/{session_id}/drafts:save-and-execute", response_model=AIPlanningTurnResponse)
def save_and_execute_route(
    session_id: int,
    payload: SaveAndExecuteRequest,
    session: Session = Depends(get_db_session),
    current_user: User = Depends(require_demo_user),
) -> AIPlanningTurnResponse:
    try:
        return save_and_execute_selected_drafts(session, session_id, payload.draft_ids, current_user.id, payload.execute, input_values=payload.input_values)
    except EntityNotFoundError as exc:
        raise HTTPException(status_code=404, detail=str(exc)) from exc
    except AIPlanningAccessError as exc:
        raise HTTPException(status_code=403, detail=str(exc)) from exc


@router.websocket("/sessions/{session_id}/ws")
async def ai_planning_session_ws(
    websocket: WebSocket,
    session_id: int,
    user_id: int | None = Query(default=None),
) -> None:
    """WebSocket endpoint for streaming AI planning operations.

    Accepts JSON messages:
      - ``{"type": "chat", "content": "..."}`` — streaming AI conversation
      - ``{"type": "generate_drafts", "scenario_keys": [...]}`` — streaming draft generation
      - ``{"type": "execute", "draft_ids": [...]}`` — start streaming execution (stop at first failure)
      - ``{"type": "execute_with_judge", "draft_ids": [...]}`` — Explorer-Judge execution (full path + judge)
      - ``{"type": "cancel"}`` — cancel the in-progress execution

    Emits events: ``status``, ``text_chunk``, ``tool_call_start``, ``tool_call_end``,
    ``draft_generating``, ``turn_complete``, ``save_progress``, ``case_start``,
    ``step_start``, ``step_complete``, ``done``, ``cancelled``, ``error``,
    ``explorer_start``, ``explorer_complete``, ``judge_start``, ``judge_complete``,
    ``auto_fix_attempt``, ``auto_fix_result``, ``verdict_report``.
    """
    session_factory = get_session_factory()
    with session_factory() as db:
        try:
            current_user = get_demo_user_or_raise(db, user_id=user_id)
        except HTTPException:
            await websocket.close(code=4001, reason="Unauthorized")
            return

    await websocket.accept()
    cancel_event = _cancellation_manager.register(session_id)

    try:
        while True:
            data = await websocket.receive_json()
            msg_type = data.get("type")

            if msg_type == "chat":
                try:
                    async for event in stream_planning_chat(
                        session_factory=session_factory,
                        planning_session_id=session_id,
                        content=str(data.get("content") or ""),
                        actor_user_id=current_user.id,
                    ):
                        await websocket.send_json(event)
                except Exception as exc:
                    logger.exception("WebSocket chat streaming error for session %s", session_id)
                    await websocket.send_json({"type": "error", "message": str(exc)})
                continue

            if msg_type == "generate_drafts":
                payload = GenerateAIPlanningDraftsRequest.model_validate(
                    {
                        "scenario_keys": data.get("scenario_keys", []),
                        "current_case": data.get("current_case"),
                        "current_steps": data.get("current_steps"),
                        "current_input_contract": data.get("current_input_contract"),
                        "current_output_contract": data.get("current_output_contract"),
                        "preserve_contracts": data.get("preserve_contracts", True),
                    }
                )
                try:
                    async for event in stream_planning_drafts(
                        session_factory=session_factory,
                        planning_session_id=session_id,
                        payload=payload,
                        actor_user_id=current_user.id,
                    ):
                        await websocket.send_json(event)
                except Exception as exc:
                    logger.exception("WebSocket draft streaming error for session %s", session_id)
                    await websocket.send_json({"type": "error", "message": str(exc)})
                continue

            if msg_type == "execute":
                draft_ids = data.get("draft_ids", [])
                try:
                    async for event in stream_save_and_execute(
                        session_factory=session_factory,
                        planning_session_id=session_id,
                        draft_ids=draft_ids,
                        actor_user_id=current_user.id,
                        cancel_event=cancel_event,
                    ):
                        await websocket.send_json(event)
                except Exception as exc:
                    logger.exception("WebSocket streaming error for session %s", session_id)
                    await websocket.send_json({"type": "error", "message": str(exc)})
                continue

            if msg_type == "execute_with_judge":
                draft_ids = data.get("draft_ids", [])
                try:
                    async for event in stream_explorer_judge(
                        session_factory=session_factory,
                        planning_session_id=session_id,
                        draft_ids=draft_ids,
                        actor_user_id=current_user.id,
                        cancel_event=cancel_event,
                    ):
                        await websocket.send_json(event)
                except Exception as exc:
                    logger.exception("Explorer-Judge WebSocket error for session %s", session_id)
                    await websocket.send_json({"type": "error", "message": str(exc)})
                continue

            if msg_type == "cancel":
                cancel_event.set()
                await websocket.send_json({"type": "cancelled"})
                continue

            await websocket.send_json({"type": "error", "message": f"Unsupported message type: {msg_type}"})

    except WebSocketDisconnect:
        logger.debug("WebSocket disconnected for planning session %s", session_id)
    finally:
        _cancellation_manager.clear(session_id)
