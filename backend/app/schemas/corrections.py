"""Schemas for locator correction records."""

from __future__ import annotations

from datetime import date, datetime
from typing import Literal

from pydantic import Field

from app.schemas.dsl import DSLModel


CorrectionType = Literal["css", "xpath", "test_id"]
CorrectionEventType = Literal["created", "activated", "deactivated", "tier0_hit", "tier0_miss", "auto_deactivated"]


class CreateCorrectionRequest(DSLModel):
    page_url: str = Field(min_length=1, max_length=500)
    target_description: str = Field(min_length=1, max_length=200)
    correction_type: CorrectionType
    correction_value: str = Field(min_length=1, max_length=2000)
    source_execution_id: int = Field(ge=1)
    created_by: int = Field(ge=1)


class UpdateCorrectionStateRequest(DSLModel):
    is_active: bool


class BatchUpdateCorrectionStateRequest(DSLModel):
    correction_ids: list[int] = Field(min_length=1, max_length=100)
    is_active: bool


class StoredLocatorCorrection(DSLModel):
    id: int
    page_url_pattern: str
    target_description: str
    correction_type: CorrectionType
    correction_value: str
    verified_count: int
    consecutive_failures: int
    is_active: bool
    source_execution_id: int | None = None
    created_by: int
    created_at: datetime
    updated_at: datetime


class LocatorCorrectionEventPoint(DSLModel):
    date: date
    hit_count: int = Field(default=0, ge=0)
    miss_count: int = Field(default=0, ge=0)


class StoredLocatorCorrectionEvent(DSLModel):
    id: int
    correction_id: int
    event_type: CorrectionEventType
    page_url_pattern: str
    target_description: str
    execution_id: int | None = None
    verified_count_after: int = Field(default=0, ge=0)
    consecutive_failures_after: int = Field(default=0, ge=0)
    is_active_after: bool
    created_at: datetime


class LocatorCorrectionsOverview(DSLModel):
    total_count: int = Field(default=0, ge=0)
    active_count: int = Field(default=0, ge=0)
    inactive_count: int = Field(default=0, ge=0)
    hit_count: int = Field(default=0, ge=0)
    miss_count: int = Field(default=0, ge=0)
    auto_deactivated_count: int = Field(default=0, ge=0)
    current_window_start: date | None = None
    current_window_end: date | None = None
    trend_points: list[LocatorCorrectionEventPoint] = Field(default_factory=list)
