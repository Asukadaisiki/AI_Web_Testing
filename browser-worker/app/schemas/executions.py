"""Schemas for case execution requests and reports."""

from __future__ import annotations

from typing import Any, Literal
from datetime import datetime

from pydantic import Field, model_serializer, model_validator

from app.schemas.dsl import DSLModel


ExecutionStatus = Literal[
    "running",
    "passed",
    "failed",
    "needs_intervention",
    "cancelled",
]
FailureCategory = Literal["configuration", "locator", "assertion", "navigation", "network", "runner"]
FailureStage = Literal[
    "configuration",
    "precondition",
    "locator",
    "action",
    "postcondition",
    "network",
    "runner",
]
ExecutionAnalysisStatus = Literal["pending", "running", "completed", "skipped", "failed"]
ExecutionAnalysisSource = Literal["deterministic", "ai"]


class CaseExecutionRequest(DSLModel):
    actor_user_id: int = Field(default=1, ge=1)
    base_url: str | None = Field(default=None, min_length=1, max_length=500)
    input_values: dict[str, str] = Field(
        default_factory=dict,
        description="Variable substitutions for ${context_key} placeholders in step values.",
    )


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
    event_type: Literal["request", "response", "requestfailed"] = "response"
    url: str
    method: str
    status: int | None = None
    resource_type: str | None = None
    failure_text: str | None = None


ConditionPhase = Literal["precondition", "postcondition"]
ConditionStatus = Literal["passed", "failed", "error"]
ActionOutcomeStatus = Literal["not_executed", "succeeded", "failed", "unknown"]
SideEffectState = Literal["not_applicable", "not_committed", "committed", "unknown"]


class PageStateSnapshot(DSLModel):
    url: str
    dom_hash: str
    visible_texts: list[str] = Field(default_factory=list)
    input_values: dict[str, str] = Field(default_factory=dict)


class ConditionResult(DSLModel):
    phase: ConditionPhase
    index: int = Field(ge=0)
    type: str
    expected: Any = None
    actual: Any = None
    status: ConditionStatus
    duration_ms: int = Field(ge=0)
    error: str | None = None


class ActionOutcome(DSLModel):
    status: ActionOutcomeStatus
    side_effect_state: SideEffectState
    error: str | None = None


class DOMElementSnapshot(DSLModel):
    tag: str
    text: str | None = None
    role: str | None = None
    aria_label: str | None = None
    placeholder: str | None = None
    data_testid: str | None = None
    css_selector: str | None = None
    xpath: str | None = None
    href: str | None = None
    id: str | None = None
    name: str | None = None
    class_name: str | None = None
    rect: dict[Literal["x", "y", "width", "height"], float] | None = None
    visible: bool = False
    enabled: bool = False


class AILocateCandidate(DSLModel):
    center: tuple[int, int] = (0, 0)
    bbox: tuple[int, int, int, int] = (0, 0, 0, 0)
    confidence: float = 0.0
    raw_response: str | None = None


class InterventionRequest(DSLModel):
    screenshot_url: str | None = None
    page_url: str
    target_description: str
    dom_snapshot: list[DOMElementSnapshot] = Field(default_factory=list, deprecated=True)
    ai_candidate: AILocateCandidate | None = None
    locator_trace: LocatorTrace | None = None
    vlm_failure_reason: str | None = None


class StepExecutionEvidence(DSLModel):
    step_index: int = Field(ge=0)
    action: str
    target: str | None = None
    value: str | None = None
    status: Literal["passed", "failed"]
    duration_ms: int | None = Field(default=None, ge=0)
    pre_state: PageStateSnapshot | None = None
    condition_results: list[ConditionResult] = Field(default_factory=list)
    action_outcome: ActionOutcome = Field(
        default_factory=lambda: ActionOutcome(
            status="unknown",
            side_effect_state="unknown",
        )
    )
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
    dom_snapshot_path: str | None = None
    dom_snapshot_url: str | None = None
    error_message: str | None = None
    intervention_request: InterventionRequest | None = None
    click_recovery: str | None = Field(
        default=None,
        description="Click recovery strategy, including href_navigation_fallback for a verified anchor.",
    )
    click_recovery_detail: str | None = Field(
        default=None,
        description="Detail of the recovery action taken for click interception.",
    )
    locator_confidence: Literal["high", "medium", "low"] | None = Field(
        default=None,
        description="AI-assessed locator confidence for this step's target.",
    )
    vlm_preverify_used: bool = Field(
        default=False,
        description="Whether VLM pre-verification was triggered due to low confidence.",
    )


class ExecutionReport(DSLModel):
    status: ExecutionStatus
    steps: list[StepExecutionEvidence] = Field(default_factory=list)


class FailureSourceReference(DSLModel):
    type: Literal["execution_report", "execution_error"]
    execution_id: int = Field(ge=1)
    step_index: int | None = Field(default=None, ge=0)
    json_pointer: str = Field(min_length=1)


class AgentEventReference(DSLModel):
    run_id: str = Field(min_length=1)
    seq: int = Field(ge=1)


class FailureSignal(DSLModel):
    schema_version: Literal["failure.signal.v2"] | None = None
    category: FailureCategory
    fingerprint: str = Field(min_length=1, max_length=40)
    title: str = Field(min_length=1, max_length=1000)
    stage: FailureStage | None = None
    code: str | None = Field(default=None, min_length=1, max_length=100)
    retryable: bool | None = None
    side_effect_committed: bool | None = None
    source_reference: FailureSourceReference | None = None
    agent_event_reference: AgentEventReference | None = None
    step_index: int | None = Field(default=None, ge=0)
    action: str | None = None
    target: str | None = None
    error_message: str | None = None
    locator_failure_reason: str | None = None
    screenshot_url: str | None = None

    @model_serializer(mode="wrap")
    def omit_absent_agent_event(self, handler):
        payload = handler(self)
        if self.agent_event_reference is None:
            payload.pop("agent_event_reference", None)
        return payload

    @model_validator(mode="after")
    def validate_versioned_contract(self) -> "FailureSignal":
        if self.schema_version == "failure.signal.v2":
            required = {
                "stage": self.stage,
                "code": self.code,
                "retryable": self.retryable,
                "source_reference": self.source_reference,
            }
            missing = [name for name, value in required.items() if value is None]
            if "side_effect_committed" not in self.model_fields_set:
                missing.append("side_effect_committed")
            if missing:
                raise ValueError(
                    "failure.signal.v2 requires " + ", ".join(missing)
                )
        return self


class FailureDetail(DSLModel):
    case_name: str = Field(min_length=1)
    step_index: int = Field(ge=0)
    action: str = Field(min_length=1)
    target: str | None = None
    error_message: str | None = None
    suspected_cause: str = Field(min_length=1)
    cause_probability: Literal["high", "medium", "low"] = "medium"


class CaseAnalysisResult(DSLModel):
    case_id: int = Field(ge=1)
    case_name: str = Field(min_length=1)
    status: str = Field(min_length=1)
    passed_steps: int = Field(ge=0)
    total_steps: int = Field(ge=0)
    failure_summary: str | None = None


class ExecutionAnalysis(DSLModel):
    source: ExecutionAnalysisSource = "deterministic"
    summary: str = ""
    conclusion: Literal["all_passed", "partial", "all_failed", "cancelled"] = "all_passed"
    case_results: list[CaseAnalysisResult] = Field(default_factory=list)
    failure_details: list[FailureDetail] = Field(default_factory=list)
    failure_signals: list[FailureSignal] = Field(default_factory=list)
    suspected_root_cause: str | None = None
    impact_scope: str | None = None
    recommended_action: Literal["targeted_retest", "regression", "manual", "done"] = "done"
    recommended_scope: str | None = None


class StoredCaseExecutionSummary(DSLModel):
    id: int
    case_id: int
    case_name: str
    project_id: int
    batch_id: int | None = None
    job_id: int | None = None
    attempt_number: int = Field(default=1, ge=1)
    dsl_sha256: str | None = None
    report_schema_version: str = "execution.report.v2"
    triggered_by: int
    status: ExecutionStatus
    error_message: str | None = None
    started_at: datetime
    finished_at: datetime | None = None
    duration_ms: int | None = Field(default=None, ge=0)
    total_steps: int = Field(default=0, ge=0)
    failed_step_index: int | None = Field(default=None, ge=0)
    failure_category: FailureCategory | None = None
    failure_signal: FailureSignal | None = None
    failure_step_action: str | None = None
    latest_url: str | None = None
    latest_screenshot_url: str | None = None


class StoredCaseExecutionDetail(StoredCaseExecutionSummary):
    dsl_snapshot: dict[str, Any] | None = None
    report: ExecutionReport | None = None
    analysis_status: ExecutionAnalysisStatus = "pending"
    analysis: ExecutionAnalysis | None = None
