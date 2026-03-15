"""Locator correction routes."""

from __future__ import annotations

from fastapi import APIRouter, Depends, HTTPException, Query, Response, status
from sqlalchemy.orm import Session

from app.db import get_db_session
from app.schemas.corrections import CreateCorrectionRequest, StoredLocatorCorrection, UpdateCorrectionStateRequest
from app.services import (
    CorrectionConflictError,
    EntityNotFoundError,
    create_correction,
    list_corrections,
    update_correction_state,
)


router = APIRouter(prefix="/corrections", tags=["corrections"])


@router.post("", response_model=StoredLocatorCorrection, status_code=status.HTTP_201_CREATED)
def create_correction_route(
    payload: CreateCorrectionRequest,
    response: Response,
    session: Session = Depends(get_db_session),
) -> StoredLocatorCorrection:
    try:
        correction = create_correction(session, payload)
    except EntityNotFoundError as exc:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail=str(exc)) from exc
    except CorrectionConflictError as exc:
        raise HTTPException(status_code=status.HTTP_409_CONFLICT, detail=str(exc)) from exc

    response.headers["Location"] = f"/api/v1/corrections/{correction.id}"
    return correction


@router.get("", response_model=list[StoredLocatorCorrection])
def list_corrections_route(
    target_description: str | None = Query(default=None, min_length=1),
    page_url: str | None = Query(default=None, min_length=1),
    is_active: bool | None = Query(default=None),
    limit: int = Query(default=50, ge=1, le=200),
    offset: int = Query(default=0, ge=0),
    session: Session = Depends(get_db_session),
) -> list[StoredLocatorCorrection]:
    return list_corrections(
        session,
        target_description=target_description,
        page_url=page_url,
        is_active=is_active,
        limit=limit,
        offset=offset,
    )


@router.patch("/{correction_id}", response_model=StoredLocatorCorrection)
def update_correction_state_route(
    correction_id: int,
    payload: UpdateCorrectionStateRequest,
    session: Session = Depends(get_db_session),
) -> StoredLocatorCorrection:
    try:
        return update_correction_state(session, correction_id, payload)
    except EntityNotFoundError as exc:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail=str(exc)) from exc
    except CorrectionConflictError as exc:
        raise HTTPException(status_code=status.HTTP_409_CONFLICT, detail=str(exc)) from exc
