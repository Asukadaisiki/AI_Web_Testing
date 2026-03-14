"""Services for locator correction records."""

from __future__ import annotations

from sqlalchemy import select
from sqlalchemy.orm import Session

from app.locators.url_pattern import generalize_url
from app.models import LocatorCorrection, TestCaseRun, User
from app.schemas.corrections import CreateCorrectionRequest, StoredLocatorCorrection
from app.services.cases import EntityNotFoundError


def create_correction(session: Session, payload: CreateCorrectionRequest) -> StoredLocatorCorrection:
    _ensure_user_exists(session, payload.created_by)
    _ensure_execution_exists(session, payload.source_execution_id)

    correction = LocatorCorrection(
        page_url_pattern=generalize_url(payload.page_url),
        target_description=payload.target_description,
        correction_type=payload.correction_type,
        correction_value=payload.correction_value,
        source_execution_id=payload.source_execution_id,
        created_by=payload.created_by,
    )
    session.add(correction)
    session.commit()
    session.refresh(correction)
    return _to_stored_locator_correction(correction)


def list_corrections(
    session: Session,
    *,
    target_description: str | None = None,
    is_active: bool | None = None,
) -> list[StoredLocatorCorrection]:
    statement = select(LocatorCorrection).order_by(LocatorCorrection.updated_at.desc(), LocatorCorrection.id.desc())
    if target_description is not None:
        statement = statement.where(LocatorCorrection.target_description == target_description)
    if is_active is not None:
        statement = statement.where(LocatorCorrection.is_active.is_(is_active))
    return [_to_stored_locator_correction(record) for record in session.scalars(statement).all()]


def deactivate_correction(session: Session, correction_id: int) -> StoredLocatorCorrection:
    correction = session.get(LocatorCorrection, correction_id)
    if correction is None:
        raise EntityNotFoundError(f"Correction {correction_id} not found.")
    correction.is_active = False
    session.add(correction)
    session.commit()
    session.refresh(correction)
    return _to_stored_locator_correction(correction)


def _ensure_user_exists(session: Session, user_id: int) -> None:
    if session.get(User, user_id) is None:
        raise EntityNotFoundError(f"User {user_id} not found.")


def _ensure_execution_exists(session: Session, execution_id: int) -> None:
    if session.get(TestCaseRun, execution_id) is None:
        raise EntityNotFoundError(f"Execution {execution_id} not found.")


def _to_stored_locator_correction(record: LocatorCorrection) -> StoredLocatorCorrection:
    return StoredLocatorCorrection(
        id=record.id,
        page_url_pattern=record.page_url_pattern,
        target_description=record.target_description,
        correction_type=record.correction_type,
        correction_value=record.correction_value,
        verified_count=record.verified_count,
        consecutive_failures=record.consecutive_failures,
        is_active=record.is_active,
        source_execution_id=record.source_execution_id,
        created_by=record.created_by,
        created_at=record.created_at,
        updated_at=record.updated_at,
    )
