"""Case execution routes."""

from __future__ import annotations

from fastapi import APIRouter, Depends, HTTPException, status
from sqlalchemy.orm import Session

from app.db import get_db_session
from app.schemas.executions import (
    CaseExecutionRequest,
    StoredCaseExecutionDetail,
    StoredCaseExecutionSummary,
)
from app.services import (
    EntityNotFoundError,
    execute_case,
    get_case_execution,
    list_case_executions,
)


router = APIRouter(tags=["executions"])


@router.post("/cases/{case_id}/execute", response_model=StoredCaseExecutionDetail)
def execute_case_route(
    case_id: int,
    payload: CaseExecutionRequest,
    session: Session = Depends(get_db_session),
) -> StoredCaseExecutionDetail:
    try:
        return execute_case(session, case_id, payload)
    except EntityNotFoundError as exc:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail=str(exc)) from exc


@router.get("/cases/{case_id}/executions", response_model=list[StoredCaseExecutionSummary])
def list_case_executions_route(
    case_id: int,
    session: Session = Depends(get_db_session),
) -> list[StoredCaseExecutionSummary]:
    try:
        return list_case_executions(session, case_id)
    except EntityNotFoundError as exc:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail=str(exc)) from exc


@router.get("/executions/{execution_id}", response_model=StoredCaseExecutionDetail)
def get_case_execution_route(
    execution_id: int,
    session: Session = Depends(get_db_session),
) -> StoredCaseExecutionDetail:
    execution = get_case_execution(session, execution_id)
    if execution is None:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Execution not found.")
    return execution
