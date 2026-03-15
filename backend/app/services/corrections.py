"""Services for locator correction records."""

from __future__ import annotations

import logging

from sqlalchemy import select
from sqlalchemy.exc import IntegrityError
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


class CorrectionConflictError(ValueError):
    """Raised when a correction activation conflicts with an existing active record."""


CONFLICT_DETAIL = "Another active correction already exists for the same page URL pattern and target description."


def create_correction(session: Session, payload: CreateCorrectionRequest) -> StoredLocatorCorrection:
    _ensure_user_exists(session, payload.created_by)
    _ensure_execution_exists(session, payload.source_execution_id)
    page_url_pattern = generalize_url(payload.page_url)
    normalized_target = normalize_target_description(payload.target_description)

    existing_records = _lock_lookup_records(
        session,
        page_url_pattern=page_url_pattern,
        normalized_target_description=normalized_target,
    )
    existing_active_records = [record for record in existing_records if record.is_active]
    correction = LocatorCorrection(
        page_url_pattern=page_url_pattern,
        target_description=payload.target_description,
        normalized_target_description=normalized_target,
        correction_type=payload.correction_type,
        correction_value=payload.correction_value,
        source_execution_id=payload.source_execution_id,
        created_by=payload.created_by,
    )

    try:
        for existing in existing_active_records:
            existing.is_active = False
            session.add(existing)
        if existing_active_records:
            session.flush()

        session.add(correction)
        session.flush()
        session.commit()
        session.refresh(correction)
    except IntegrityError as exc:
        session.rollback()
        raise CorrectionConflictError(CONFLICT_DETAIL) from exc

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

    if payload.is_active and not correction.is_active:
        related_records = _lock_lookup_records(
            session,
            page_url_pattern=correction.page_url_pattern,
            normalized_target_description=correction.normalized_target_description,
        )
        conflicting_active_record = next(
            (
                record
                for record in related_records
                if record.id != correction.id and record.is_active
            ),
            None,
        )
        if conflicting_active_record is not None:
            raise CorrectionConflictError(CONFLICT_DETAIL)

    correction.is_active = payload.is_active
    session.add(correction)
    try:
        session.flush()
        session.commit()
        session.refresh(correction)
    except IntegrityError as exc:
        session.rollback()
        raise CorrectionConflictError(CONFLICT_DETAIL) from exc

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


def _lock_lookup_records(
    session: Session,
    *,
    page_url_pattern: str,
    normalized_target_description: str,
) -> list[LocatorCorrection]:
    statement = (
        select(LocatorCorrection)
        .where(
            LocatorCorrection.page_url_pattern == page_url_pattern,
            LocatorCorrection.normalized_target_description == normalized_target_description,
        )
        .order_by(LocatorCorrection.updated_at.desc(), LocatorCorrection.id.desc())
    )
    if _supports_for_update(session):
        statement = statement.with_for_update()
    return session.scalars(statement).all()


def _supports_for_update(session: Session) -> bool:
    return session.get_bind().dialect.name == "postgresql"


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
