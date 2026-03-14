"""Helpers for querying manual locator corrections."""

from __future__ import annotations

from sqlalchemy import select
from sqlalchemy.orm import Session

from app.models import LocatorCorrection
from app.locators.url_pattern import generalize_url


MAX_CONSECUTIVE_FAILURES = 3


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
