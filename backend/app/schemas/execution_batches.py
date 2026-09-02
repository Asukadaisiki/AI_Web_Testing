"""Schemas for persistent execution batches and queued case jobs."""

from __future__ import annotations

from datetime import datetime
from typing import Literal

from pydantic import Field

from app.schemas.dsl import DSLModel
from app.schemas.executions import (
    ExecutionAnalysis,
    ExecutionAnalysisStatus,
    StoredCaseExecutionDetail,
)


ExecutionBatchStatus = Literal[
    "pending",
    "running",
    "passed",
    "failed",
    "needs_intervention",
    "cancelled",
]
ExecutionJobStatus = ExecutionBatchStatus


class ExecutionBatchCreateRequest(DSLModel):
    project_id: int = Field(ge=1)
    case_ids: list[int] | None = Field(default=None, min_length=1, max_length=500)
    planning_session_id: int | None = Field(default=None, ge=1)
    idempotency_key: str | None = Field(default=None, min_length=1, max_length=100)
    concurrency_limit: int = Field(default=1, ge=1, le=16)
    input_values: dict[str, str] = Field(default_factory=dict)


class ExecutionJobSummary(DSLModel):
    id: int
    batch_id: int
    project_id: int
    case_id: int
    case_name: str
    order_index: int
    status: ExecutionJobStatus
    attempt_count: int = Field(ge=0)
    max_attempts: int = Field(ge=1)
    cancel_requested: bool
    last_error_message: str | None = None
    created_at: datetime
    started_at: datetime | None = None
    heartbeat_at: datetime | None = None
    finished_at: datetime | None = None
    latest_execution: StoredCaseExecutionDetail | None = None


class ExecutionBatchSummary(DSLModel):
    id: int
    project_id: int
    planning_session_id: int | None = None
    triggered_by: int
    status: ExecutionBatchStatus
    idempotency_key: str | None = None
    concurrency_limit: int = Field(ge=1)
    total_jobs: int = Field(ge=0)
    pending_jobs: int = Field(ge=0)
    running_jobs: int = Field(ge=0)
    passed_jobs: int = Field(ge=0)
    failed_jobs: int = Field(ge=0)
    intervention_jobs: int = Field(ge=0)
    cancelled_jobs: int = Field(ge=0)
    analysis_status: ExecutionAnalysisStatus = "pending"
    analysis: ExecutionAnalysis | None = None
    created_at: datetime
    started_at: datetime | None = None
    finished_at: datetime | None = None


class ExecutionBatchDetail(ExecutionBatchSummary):
    jobs: list[ExecutionJobSummary] = Field(default_factory=list)


class ExecutionBatchReport(ExecutionBatchDetail):
    pass_rate: float = Field(ge=0, le=1)
    completed_jobs: int = Field(ge=0)
