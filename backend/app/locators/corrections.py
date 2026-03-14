"""Helpers for querying manual locator corrections."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Protocol

from sqlalchemy import select
from sqlalchemy.orm import Session

from app.models import LocatorCorrection
from app.locators.url_pattern import generalize_url


MAX_CONSECUTIVE_FAILURES = 3


@dataclass(frozen=True)
class CorrectionRecord:
    id: int
    correction_type: str
    correction_value: str
    verified_count: int
    consecutive_failures: int
    is_active: bool


class CorrectionStore(Protocol):
    def find_active_correction(self, *, page_url: str, target_description: str) -> CorrectionRecord | None: ...

    def record_success(self, correction_id: int) -> CorrectionRecord | None: ...

    def record_failure(self, correction_id: int) -> CorrectionRecord | None: ...


def normalize_target_description(target_description: str) -> str:
    return " ".join(target_description.strip().casefold().split())


def find_active_correction(
    session: Session,
    *,
    page_url: str,
    target_description: str,
) -> LocatorCorrection | None:
    pattern = generalize_url(page_url)
    normalized_target = normalize_target_description(target_description)
    statement = (
        select(LocatorCorrection)
        .where(
            LocatorCorrection.page_url_pattern == pattern,
            LocatorCorrection.normalized_target_description == normalized_target,
            LocatorCorrection.is_active.is_(True),
        )
        .order_by(
            LocatorCorrection.verified_count.desc(),
            LocatorCorrection.updated_at.desc(),
            LocatorCorrection.id.desc(),
        )
    )
    return session.scalar(statement)


class SQLAlchemyCorrectionStore:
    def __init__(self, session: Session) -> None:
        self._session = session

    def find_active_correction(self, *, page_url: str, target_description: str) -> CorrectionRecord | None:
        record = find_active_correction(
            self._session,
            page_url=page_url,
            target_description=target_description,
        )
        return _to_correction_record(record) if record is not None else None

    def record_success(self, correction_id: int) -> CorrectionRecord | None:
        record = self._session.get(LocatorCorrection, correction_id)
        if record is None:
            return None
        record.verified_count += 1
        record.consecutive_failures = 0
        self._session.flush()
        return _to_correction_record(record)

    def record_failure(self, correction_id: int) -> CorrectionRecord | None:
        record = self._session.get(LocatorCorrection, correction_id)
        if record is None:
            return None
        record.consecutive_failures += 1
        if record.consecutive_failures >= MAX_CONSECUTIVE_FAILURES:
            record.is_active = False
        self._session.flush()
        return _to_correction_record(record)


def _to_correction_record(record: LocatorCorrection) -> CorrectionRecord:
    return CorrectionRecord(
        id=record.id,
        correction_type=record.correction_type,
        correction_value=record.correction_value,
        verified_count=record.verified_count,
        consecutive_failures=record.consecutive_failures,
        is_active=record.is_active,
    )


__all__ = [
    "CorrectionRecord",
    "CorrectionStore",
    "MAX_CONSECUTIVE_FAILURES",
    "SQLAlchemyCorrectionStore",
    "find_active_correction",
    "normalize_target_description",
]
