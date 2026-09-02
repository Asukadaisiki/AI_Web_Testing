"""Execution batch queue and Report Core routes."""

from __future__ import annotations

from fastapi import APIRouter, Depends, HTTPException, Query, Response, status
from sqlalchemy.orm import Session

from app.api.auth import require_demo_user
from app.application.reporting import (
    build_batch_detail,
    build_batch_report,
    build_project_batch_summaries,
)
from app.db import get_db_session
from app.models import User
from app.schemas.execution_batches import (
    ExecutionBatchCreateRequest,
    ExecutionBatchDetail,
    ExecutionBatchReport,
    ExecutionBatchSummary,
)
from app.services.cases import EntityNotFoundError, _ensure_project_member
from app.services.execution_batches import (
    cancel_execution_batch,
    create_execution_batch,
    get_execution_batch,
)


router = APIRouter(prefix="/execution-batches", tags=["execution-batches"])


@router.post("", response_model=ExecutionBatchDetail, status_code=status.HTTP_201_CREATED)
def create_execution_batch_route(
    payload: ExecutionBatchCreateRequest,
    response: Response,
    session: Session = Depends(get_db_session),
    current_user: User = Depends(require_demo_user),
) -> ExecutionBatchDetail:
    try:
        batch = create_execution_batch(session, payload, actor_user_id=current_user.id)
        detail = build_batch_detail(session, batch.id)
    except EntityNotFoundError as exc:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail=str(exc)) from exc
    except ValueError as exc:
        raise HTTPException(status_code=status.HTTP_422_UNPROCESSABLE_ENTITY, detail=str(exc)) from exc
    response.headers["Location"] = f"/api/v1/execution-batches/{batch.id}"
    return detail


@router.get("", response_model=list[ExecutionBatchSummary])
def list_execution_batches_route(
    project_id: int = Query(ge=1),
    limit: int = Query(default=50, ge=1, le=100),
    session: Session = Depends(get_db_session),
    current_user: User = Depends(require_demo_user),
) -> list[ExecutionBatchSummary]:
    try:
        _ensure_project_member(session, project_id, current_user.id)
    except EntityNotFoundError as exc:
        raise HTTPException(status_code=status.HTTP_403_FORBIDDEN, detail=str(exc)) from exc
    return build_project_batch_summaries(session, project_id, limit=limit)


@router.get("/{batch_id}", response_model=ExecutionBatchDetail)
def get_execution_batch_route(
    batch_id: int,
    session: Session = Depends(get_db_session),
    current_user: User = Depends(require_demo_user),
) -> ExecutionBatchDetail:
    batch = _get_accessible_batch(session, batch_id, current_user.id)
    return build_batch_detail(session, batch.id)


@router.get("/{batch_id}/report", response_model=ExecutionBatchReport)
def get_execution_batch_report_route(
    batch_id: int,
    session: Session = Depends(get_db_session),
    current_user: User = Depends(require_demo_user),
) -> ExecutionBatchReport:
    batch = _get_accessible_batch(session, batch_id, current_user.id)
    return build_batch_report(session, batch.id)


@router.post("/{batch_id}/cancel", response_model=ExecutionBatchDetail)
def cancel_execution_batch_route(
    batch_id: int,
    session: Session = Depends(get_db_session),
    current_user: User = Depends(require_demo_user),
) -> ExecutionBatchDetail:
    batch = _get_accessible_batch(session, batch_id, current_user.id)
    cancel_execution_batch(session, batch.id)
    return build_batch_detail(session, batch.id)


def _get_accessible_batch(session: Session, batch_id: int, actor_user_id: int):
    try:
        batch = get_execution_batch(session, batch_id)
        _ensure_project_member(session, batch.project_id, actor_user_id)
        return batch
    except EntityNotFoundError as exc:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail=str(exc)) from exc
