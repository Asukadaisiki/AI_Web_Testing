"""Schemas for locator correction records."""

from __future__ import annotations

from datetime import datetime
from typing import Literal

from pydantic import Field

from app.schemas.dsl import DSLModel


CorrectionType = Literal["css", "xpath", "test_id"]


class CreateCorrectionRequest(DSLModel):
    page_url: str = Field(min_length=1, max_length=500)
    target_description: str = Field(min_length=1, max_length=200)
    correction_type: CorrectionType
    correction_value: str = Field(min_length=1, max_length=2000)
    source_execution_id: int = Field(ge=1)
    created_by: int = Field(ge=1)


class UpdateCorrectionStateRequest(DSLModel):
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
