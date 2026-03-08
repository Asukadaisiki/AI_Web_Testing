"""Schemas for persisted test cases."""

from __future__ import annotations

from datetime import datetime

from pydantic import Field

from app.schemas.dsl import DSLCase, DSLModel, DSLStep


class CaseCreateRequest(DSLCase):
    project_id: int = Field(ge=1)
    actor_user_id: int = Field(default=1, ge=1)


class StoredCaseSummary(DSLModel):
    id: int
    project_id: int
    name: str
    description: str | None = None
    steps: list[DSLStep]
    created_by: int
    updated_by: int
    created_at: datetime
    updated_at: datetime


class StoredCaseDetail(StoredCaseSummary):
    pass
