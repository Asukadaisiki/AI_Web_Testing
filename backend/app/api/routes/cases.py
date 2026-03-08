"""Case persistence routes."""

from __future__ import annotations

from fastapi import APIRouter, Depends, HTTPException, Response, status
from sqlalchemy.orm import Session

from app.db import get_db_session
from app.schemas.cases import CaseCreateRequest, StoredCaseDetail, StoredCaseSummary
from app.services import EntityNotFoundError, create_case, get_case, list_cases


router = APIRouter(prefix="/cases", tags=["cases"])


@router.post("", response_model=StoredCaseDetail, status_code=status.HTTP_201_CREATED)
def create_case_route(
    payload: CaseCreateRequest,
    response: Response,
    session: Session = Depends(get_db_session),
) -> StoredCaseDetail:
    try:
        created_case = create_case(session, payload)
    except EntityNotFoundError as exc:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail=str(exc)) from exc

    response.headers["Location"] = f"/api/v1/cases/{created_case.id}"
    return created_case


@router.get("", response_model=list[StoredCaseSummary])
def list_cases_route(session: Session = Depends(get_db_session)) -> list[StoredCaseSummary]:
    return list_cases(session)


@router.get("/{case_id}", response_model=StoredCaseDetail)
def get_case_route(
    case_id: int,
    session: Session = Depends(get_db_session),
) -> StoredCaseDetail:
    stored_case = get_case(session, case_id)
    if stored_case is None:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Case not found.")
    return stored_case
