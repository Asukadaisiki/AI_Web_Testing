"""Schemas for case execution requests and reports."""

from __future__ import annotations

from datetime import date, datetime
from typing import Literal

from pydantic import Field

from app.schemas.dsl import DSLModel


ExecutionStatus = Literal["running", "passed", "failed"]
FailureCategory = Literal["configuration", "locator", "assertion", "navigation", "network", "runner"]


class CaseExecutionRequest(DSLModel):
    actor_user_id: int = Field(default=1, ge=1)
    base_url: str | None = Field(default=None, min_length=1, max_length=500)


class ViewportSnapshot(DSLModel):
    width: int = Field(ge=0)
    height: int = Field(ge=0)


class LocatorCandidateAttributes(DSLModel):
    aria_label: str | None = None
    placeholder: str | None = None
    data_testid: str | None = None


class LocatorCandidateEvidence(DSLModel):
    strategy: str
    preview_text: str | None = None
    role: str | None = None
    attributes: LocatorCandidateAttributes = Field(default_factory=LocatorCandidateAttributes)
    score: int = Field(default=0, ge=0)
    matched_rules: list[str] = Field(default_factory=list)
    rejected_reasons: list[str] = Field(default_factory=list)
    visible: bool = False
    enabled: bool = False


class LocatorTrace(DSLModel):
    target: str
    match_strategy: str | None = None
    selection_reason: str | None = None
    candidates: list[LocatorCandidateEvidence] = Field(default_factory=list)
    selected_candidate: LocatorCandidateEvidence | None = None
    failure_reason: str | None = None


class DOMSummary(DSLModel):
    text_preview: str | None = None
    button_count: int = Field(default=0, ge=0)
    input_count: int = Field(default=0, ge=0)
    link_count: int = Field(default=0, ge=0)


class ConsoleEvent(DSLModel):
    level: Literal["error", "warning"]
    text: str
    source_url: str | None = None
    line_number: int | None = None


class NetworkEvent(DSLModel):
    url: str
    method: str
    status: int | None = None
    resource_type: str | None = None
    failure_text: str | None = None


class StepExecutionEvidence(DSLModel):
    step_index: int = Field(ge=0)
    action: str
    target: str | None = None
    value: str | None = None
    status: Literal["passed", "failed"]
    duration_ms: int | None = Field(default=None, ge=0)
    resolved_by: str | None = None
    locator_trace: LocatorTrace | None = None
    url: str | None = None
    page_title: str | None = None
    viewport: ViewportSnapshot | None = None
    dom_summary: DOMSummary | None = None
    console_events: list[ConsoleEvent] = Field(default_factory=list)
    network_events: list[NetworkEvent] = Field(default_factory=list)
    screenshot_path: str | None = None
    screenshot_url: str | None = None
    error_message: str | None = None


class ExecutionReport(DSLModel):
    status: ExecutionStatus
    steps: list[StepExecutionEvidence] = Field(default_factory=list)


class StoredCaseExecutionSummary(DSLModel):
    id: int
    case_id: int
    case_name: str
    project_id: int
    triggered_by: int
    status: ExecutionStatus
    error_message: str | None = None
    started_at: datetime
    finished_at: datetime | None = None
    duration_ms: int | None = Field(default=None, ge=0)
    total_steps: int = Field(default=0, ge=0)
    failed_step_index: int | None = Field(default=None, ge=0)
    failure_category: FailureCategory | None = None
    failure_step_action: str | None = None
    latest_url: str | None = None
    latest_screenshot_url: str | None = None


class StoredCaseExecutionDetail(StoredCaseExecutionSummary):
    report: ExecutionReport | None = None


class FailureCategoryCount(DSLModel):
    category: FailureCategory
    count: int = Field(default=0, ge=0)


class ExecutionTrendPoint(DSLModel):
    date: date
    total_count: int = Field(default=0, ge=0)
    passed_count: int = Field(default=0, ge=0)
    failed_count: int = Field(default=0, ge=0)
    pass_rate: float = Field(default=0, ge=0)
    avg_duration_ms: int = Field(default=0, ge=0)


class FailureStepActionCount(DSLModel):
    action: str
    count: int = Field(default=0, ge=0)


class TopFailedCase(DSLModel):
    case_id: int
    case_name: str
    failure_count: int = Field(default=0, ge=0)
    latest_execution_id: int = Field(ge=1)
    latest_failure_category: FailureCategory | None = None


class ExecutionsOverview(DSLModel):
    total_count: int = Field(default=0, ge=0)
    passed_count: int = Field(default=0, ge=0)
    failed_count: int = Field(default=0, ge=0)
    running_count: int = Field(default=0, ge=0)
    pass_rate: float = Field(default=0, ge=0)
    avg_duration_ms: int = Field(default=0, ge=0)
    latest_failed_runs: list[StoredCaseExecutionSummary] = Field(default_factory=list)
    failure_categories: list[FailureCategoryCount] = Field(default_factory=list)
    trend_points: list[ExecutionTrendPoint] = Field(default_factory=list)
    failure_step_actions: list[FailureStepActionCount] = Field(default_factory=list)
    top_failed_cases: list[TopFailedCase] = Field(default_factory=list)
