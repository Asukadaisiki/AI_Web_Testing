"""Schemas for persisted test suites and suite executions."""

from __future__ import annotations

from datetime import datetime

from pydantic import Field

from app.schemas.dsl import DSLModel
from app.schemas.executions import ExecutionStatus


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


class StoredSuiteDetail(StoredSuiteSummary):
    cases: list[StoredSuiteCase]


class SuiteExecutionRequest(DSLModel):
    actor_user_id: int = Field(default=1, ge=1)
    base_url: str | None = Field(default=None, min_length=1, max_length=500)


class SuiteExecutionItem(DSLModel):
    execution_id: int
    case_id: int
    case_name: str
    status: ExecutionStatus


class SuiteExecutionResult(DSLModel):
    suite_id: int
    suite_name: str
    started_at: datetime
    finished_at: datetime
    total_cases: int
    passed_cases: int
    failed_cases: int
    status: ExecutionStatus
    executions: list[SuiteExecutionItem]
