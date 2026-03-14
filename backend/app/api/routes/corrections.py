"""Locator correction routes."""

from __future__ import annotations

from fastapi import APIRouter, Depends, HTTPException, Query, Response, status
from sqlalchemy.orm import Session

from app.db import get_db_session
from app.schemas.corrections import CreateCorrectionRequest, StoredLocatorCorrection
from app.services import EntityNotFoundError, create_correction, deactivate_correction, list_corrections


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

    response.headers["Location"] = f"/api/v1/corrections/{correction.id}"
    return correction


@router.get("", response_model=list[StoredLocatorCorrection])
def list_corrections_route(
    target_description: str | None = Query(default=None, min_length=1),
    is_active: bool | None = Query(default=None),
    session: Session = Depends(get_db_session),
) -> list[StoredLocatorCorrection]:
    return list_corrections(session, target_description=target_description, is_active=is_active)


@router.put("/{correction_id}/deactivate", response_model=StoredLocatorCorrection)
def deactivate_correction_route(
    correction_id: int,
    session: Session = Depends(get_db_session),
) -> StoredLocatorCorrection:
    try:
        return deactivate_correction(session, correction_id)
    except EntityNotFoundError as exc:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail=str(exc)) from exc
