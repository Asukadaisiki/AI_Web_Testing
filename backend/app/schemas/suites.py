"""Schemas for persisted test suites and suite executions."""

from __future__ import annotations

from datetime import datetime
from typing import Any, Literal

from pydantic import Field

from app.schemas.dsl import DSLModel
from app.schemas.executions import (
    ContextVariableReadEvidence,
    ContextVariableWriteEvidence,
    ExecutionStatus,
)


SuiteRunSource = Literal["manual", "rerun_failed"]
SuiteRunContextSource = Literal["empty", "suite_run_snapshot"]
SuiteRerunContextMode = Literal["not_applicable", "reuse_source_context", "empty_context"]


class SuiteCaseRef(DSLModel):
    case_id: int = Field(ge=1)


class SuiteCreateRequest(DSLModel):
    project_id: int = Field(ge=1)
    actor_user_id: int = Field(default=1, ge=1)
    name: str = Field(min_length=1, max_length=200)
    description: str | None = Field(default=None, max_length=1000)
    cases: list[SuiteCaseRef] = Field(min_length=1)


class SuiteUpdateRequest(SuiteCreateRequest):
    pass


class StoredSuiteCase(DSLModel):
    case_id: int
    case_name: str
    order_index: int


class StoredSuiteRunItem(DSLModel):
    id: int
    case_id: int
    case_name_snapshot: str
    order_index: int
    execution_id: int
    status: ExecutionStatus
    context_reads: list[ContextVariableReadEvidence] = Field(default_factory=list)
    context_writes: list[ContextVariableWriteEvidence] = Field(default_factory=list)
    context_resolution_error: str | None = None


class StoredSuiteRunSummary(DSLModel):
    id: int
    suite_id: int
    suite_name: str
    triggered_by: int
    source: SuiteRunSource
    source_suite_run_id: int | None = None
    status: ExecutionStatus
    total_cases: int
    passed_cases: int
    failed_cases: int
    base_url_override: str | None = None
    context_source: SuiteRunContextSource
    context_source_suite_run_id: int | None = None
    rerun_context_mode: SuiteRerunContextMode
    context_snapshot: dict[str, Any] = Field(default_factory=dict)
    started_at: datetime
    finished_at: datetime | None = None


class StoredSuiteRunDetail(StoredSuiteRunSummary):
    items: list[StoredSuiteRunItem]


class StoredSuiteSummary(DSLModel):
    id: int
    project_id: int
    name: str
    description: str | None = None
    case_count: int
    created_by: int
    updated_by: int
    created_at: datetime
    updated_at: datetime
    latest_run: StoredSuiteRunSummary | None = None


class StoredSuiteDetail(StoredSuiteSummary):
    cases: list[StoredSuiteCase]


class SuiteExecutionRequest(DSLModel):
    actor_user_id: int = Field(default=1, ge=1)
    base_url: str | None = Field(default=None, min_length=1, max_length=500)
    rerun_context_mode: Literal["reuse_source_context", "empty_context"] | None = None


class SuiteExecutionItem(DSLModel):
    execution_id: int
    case_id: int
    case_name: str
    status: ExecutionStatus


class SuiteExecutionResult(StoredSuiteRunDetail):
    executions: list[SuiteExecutionItem] = Field(default_factory=list)
