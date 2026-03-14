"""Services for locator correction records."""

from __future__ import annotations

import logging

from sqlalchemy import select
from sqlalchemy.orm import Session

from app.locators.corrections import normalize_target_description
from app.locators.url_pattern import generalize_url
from app.models import LocatorCorrection, TestCaseRun, User
from app.schemas.corrections import (
    CreateCorrectionRequest,
    StoredLocatorCorrection,
    UpdateCorrectionStateRequest,
)
from app.services.cases import EntityNotFoundError


logger = logging.getLogger(__name__)


def create_correction(session: Session, payload: CreateCorrectionRequest) -> StoredLocatorCorrection:
    _ensure_user_exists(session, payload.created_by)
    _ensure_execution_exists(session, payload.source_execution_id)
    page_url_pattern = generalize_url(payload.page_url)
    normalized_target = normalize_target_description(payload.target_description)

    existing_active_records = session.scalars(
        select(LocatorCorrection).where(
            LocatorCorrection.page_url_pattern == page_url_pattern,
            LocatorCorrection.normalized_target_description == normalized_target,
            LocatorCorrection.is_active.is_(True),
        )
    ).all()
    for existing in existing_active_records:
        existing.is_active = False
        session.add(existing)

    correction = LocatorCorrection(
        page_url_pattern=page_url_pattern,
        target_description=payload.target_description,
        normalized_target_description=normalized_target,
        correction_type=payload.correction_type,
        correction_value=payload.correction_value,
        source_execution_id=payload.source_execution_id,
        created_by=payload.created_by,
    )
    session.add(correction)
    session.commit()
    session.refresh(correction)
    logger.warning(
        "Created locator correction id=%s page_url_pattern=%s target=%s deactivated_existing=%s",
        correction.id,
        correction.page_url_pattern,
        correction.target_description,
        len(existing_active_records),
    )
    return _to_stored_locator_correction(correction)


def list_corrections(
    session: Session,
    *,
    target_description: str | None = None,
    page_url: str | None = None,
    is_active: bool | None = None,
    limit: int = 50,
    offset: int = 0,
) -> list[StoredLocatorCorrection]:
    statement = select(LocatorCorrection).order_by(LocatorCorrection.updated_at.desc(), LocatorCorrection.id.desc())
    if target_description is not None:
        statement = statement.where(
            LocatorCorrection.normalized_target_description == normalize_target_description(target_description)
        )
    if page_url is not None:
        statement = statement.where(LocatorCorrection.page_url_pattern == generalize_url(page_url))
    if is_active is not None:
        statement = statement.where(LocatorCorrection.is_active.is_(is_active))
    statement = statement.limit(limit).offset(offset)
    records = session.scalars(statement).all()
    logger.debug(
        "Listed locator corrections target_filter=%s page_filter=%s is_active=%s limit=%s offset=%s count=%s",
        target_description,
        page_url,
        is_active,
        limit,
        offset,
        len(records),
    )
    return [_to_stored_locator_correction(record) for record in records]


def update_correction_state(
    session: Session,
    correction_id: int,
    payload: UpdateCorrectionStateRequest,
) -> StoredLocatorCorrection:
    correction = session.get(LocatorCorrection, correction_id)
    if correction is None:
        raise EntityNotFoundError(f"Correction {correction_id} not found.")
    correction.is_active = payload.is_active
    session.add(correction)
    session.commit()
    session.refresh(correction)
    logger.warning(
        "Updated locator correction state id=%s is_active=%s",
        correction.id,
        correction.is_active,
    )
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
