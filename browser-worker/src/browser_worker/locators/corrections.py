"""Manual locator correction protocol used by the Playwright runner."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Protocol

MAX_CONSECUTIVE_FAILURES = 3
EVENT_TIER0_HIT = "tier0_hit"
EVENT_TIER0_MISS = "tier0_miss"
EVENT_AUTO_DEACTIVATED = "auto_deactivated"


@dataclass(frozen=True)
class CorrectionRecord:
    id: int
    correction_type: str
    correction_value: str
    verified_count: int
    consecutive_failures: int
    is_active: bool


class CorrectionStore(Protocol):
    def find_active_correction(
        self,
        *,
        page_url: str,
        target_description: str,
    ) -> CorrectionRecord | None: ...

    def record_success(
        self,
        correction_id: int,
        *,
        execution_id: int | None = None,
    ) -> CorrectionRecord | None: ...

    def record_failure(
        self,
        correction_id: int,
        *,
        execution_id: int | None = None,
    ) -> CorrectionRecord | None: ...


def normalize_target_description(target_description: str) -> str:
    return " ".join(target_description.strip().casefold().split())


__all__ = [
    "CorrectionRecord",
    "CorrectionStore",
    "EVENT_AUTO_DEACTIVATED",
    "EVENT_TIER0_HIT",
    "EVENT_TIER0_MISS",
    "MAX_CONSECUTIVE_FAILURES",
    "normalize_target_description",
]
