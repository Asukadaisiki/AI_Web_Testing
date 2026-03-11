"""Suite persistence and execution routes."""

from __future__ import annotations

from fastapi import APIRouter, Depends, HTTPException, Response, status
from sqlalchemy.orm import Session

from app.db import get_db_session
from app.schemas.suites import (
    StoredSuiteDetail,
    StoredSuiteSummary,
    SuiteCreateRequest,
    SuiteExecutionRequest,
    SuiteExecutionResult,
    SuiteUpdateRequest,
)
from app.services import (
    EntityNotFoundError,
    SuiteValidationError,
    create_suite,
    execute_suite,
    get_suite,
    list_suites,
    update_suite,
)


router = APIRouter(prefix="/suites", tags=["suites"])


@router.post("", response_model=StoredSuiteDetail, status_code=status.HTTP_201_CREATED)
def create_suite_route(
    payload: SuiteCreateRequest,
    response: Response,
    session: Session = Depends(get_db_session),
) -> StoredSuiteDetail:
    try:
        created_suite = create_suite(session, payload)
    except EntityNotFoundError as exc:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail=str(exc)) from exc
    except SuiteValidationError as exc:
        raise HTTPException(status_code=status.HTTP_422_UNPROCESSABLE_ENTITY, detail=str(exc)) from exc

    response.headers["Location"] = f"/api/v1/suites/{created_suite.id}"
    return created_suite


@router.get("", response_model=list[StoredSuiteSummary])
def list_suites_route(session: Session = Depends(get_db_session)) -> list[StoredSuiteSummary]:
    return list_suites(session)


@router.get("/{suite_id}", response_model=StoredSuiteDetail)
def get_suite_route(suite_id: int, session: Session = Depends(get_db_session)) -> StoredSuiteDetail:
    suite = get_suite(session, suite_id)
    if suite is None:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Suite not found.")
    return suite


@router.put("/{suite_id}", response_model=StoredSuiteDetail)
def update_suite_route(
    suite_id: int,
    payload: SuiteUpdateRequest,
    session: Session = Depends(get_db_session),
) -> StoredSuiteDetail:
    try:
        return update_suite(session, suite_id, payload)
    except EntityNotFoundError as exc:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail=str(exc)) from exc
    except SuiteValidationError as exc:
        raise HTTPException(status_code=status.HTTP_422_UNPROCESSABLE_ENTITY, detail=str(exc)) from exc


@router.post("/{suite_id}/execute", response_model=SuiteExecutionResult)
def execute_suite_route(
    suite_id: int,
    payload: SuiteExecutionRequest,
    session: Session = Depends(get_db_session),
) -> SuiteExecutionResult:
    try:
        return execute_suite(session, suite_id, payload)
    except EntityNotFoundError as exc:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail=str(exc)) from exc
    except SuiteValidationError as exc:
        raise HTTPException(status_code=status.HTTP_422_UNPROCESSABLE_ENTITY, detail=str(exc)) from exc
