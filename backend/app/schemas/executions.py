"""Schemas for case execution requests and reports."""

from __future__ import annotations

from datetime import datetime
from typing import Literal

from pydantic import Field

from app.schemas.dsl import DSLModel


ExecutionStatus = Literal["running", "passed", "failed"]


class CaseExecutionRequest(DSLModel):
    actor_user_id: int = Field(default=1, ge=1)
    base_url: str | None = Field(default=None, min_length=1, max_length=500)


class StepExecutionEvidence(DSLModel):
    step_index: int = Field(ge=0)
    action: str
    target: str | None = None
    value: str | None = None
    status: Literal["passed", "failed"]
    resolved_by: str | None = None
    url: str | None = None
    screenshot_path: str | None = None
    error_message: str | None = None


class ExecutionReport(DSLModel):
    status: ExecutionStatus
    steps: list[StepExecutionEvidence] = Field(default_factory=list)


class StoredCaseExecutionSummary(DSLModel):
    id: int
    case_id: int
    project_id: int
    triggered_by: int
    status: ExecutionStatus
    error_message: str | None = None
    started_at: datetime
    finished_at: datetime | None = None


class StoredCaseExecutionDetail(StoredCaseExecutionSummary):
    report: ExecutionReport | None = None
