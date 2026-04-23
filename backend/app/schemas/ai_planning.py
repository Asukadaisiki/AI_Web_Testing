"""Schemas for AI planning sessions and drafts."""

from __future__ import annotations

from datetime import datetime
from typing import Any, Literal

from pydantic import Field

from app.schemas.dsl import DSLCase, DSLCaseInputContract, DSLCaseOutputContract, DSLModel, DSLStep, DSLVariableType


AIPlanningSessionStatus = Literal["collecting", "plan_ready", "drafts_ready", "reviewing", "saving", "executing", "completed", "closed", "error"]
AIPlanningMessageRole = Literal["user", "assistant"]
AIPlanningMessageTurnType = Literal["user", "followup", "plan", "tool_call", "system_error"]
AIPlanningDraftStatus = Literal["generated", "imported", "rejected", "failed"]
AIPlanningNextAction = Literal["ask_followup", "review_plan", "select_scenarios", "drafts_generated"]


class AIPlanningRequirements(DSLModel):
    app_under_test: str | None = Field(default=None, max_length=500)
    business_goal: str | None = Field(default=None, max_length=1000)
    entry_url_or_page: str | None = Field(default=None, max_length=500)
    core_user_flow: str | None = Field(default=None, max_length=2000)
    main_assertions: list[str] = Field(default_factory=list)
    test_data_or_account: str | None = Field(default=None, max_length=1000)
    scope_limits: str | None = Field(default=None, max_length=1000)


class AIPlanningToolCall(DSLModel):
    tool: str = Field(min_length=1, max_length=100)
    params: dict[str, Any] = Field(default_factory=dict)
    result: Any = None


class AIPlanningTestDataRequirement(DSLModel):
    key: str = Field(min_length=1, max_length=100)
    label: str = Field(min_length=1, max_length=200)
    value_type: DSLVariableType
    required: bool = True
    source_hint: str | None = Field(default=None, max_length=200)


class AIPlanningScenario(DSLModel):
    scenario_key: str = Field(min_length=1, max_length=100)
    title: str = Field(min_length=1, max_length=200)
    goal: str = Field(min_length=1, max_length=1000)
    preconditions: list[str] = Field(default_factory=list)
    priority: Literal["high", "medium", "low"] = "medium"
    test_data_requirements: list[AIPlanningTestDataRequirement] = Field(default_factory=list)
    assertions: list[str] = Field(default_factory=list)
    draft_prompt: str = Field(min_length=1)
    page_elements: str | None = Field(default=None, description="Formatted DOM elements from explore_page tool.")


class AIPlanningPlan(DSLModel):
    summary: str = Field(min_length=1, max_length=2000)
    assumptions: list[str] = Field(default_factory=list)
    risks: list[str] = Field(default_factory=list)
    scenarios: list[AIPlanningScenario] = Field(default_factory=list)


class AIPlanningSession(DSLModel):
    id: int = Field(ge=1)
    actor_user_id: int = Field(ge=1)
    project_id: int = Field(ge=1)
    case_id: int | None = Field(default=None, ge=1)
    title: str | None = Field(default=None, max_length=200)
    status: AIPlanningSessionStatus
    requirements: AIPlanningRequirements = Field(default_factory=AIPlanningRequirements)
    plan: AIPlanningPlan | None = None
    missing_slots: list[str] = Field(default_factory=list)
    last_error_message: str | None = Field(default=None, max_length=4000)
    created_at: datetime
    updated_at: datetime


class AIPlanningSessionSummary(DSLModel):
    id: int = Field(ge=1)
    title: str | None = Field(default=None, max_length=200)
    status: AIPlanningSessionStatus
    created_at: datetime
    updated_at: datetime


class AIPlanningMessage(DSLModel):
    id: int = Field(ge=1)
    session_id: int = Field(ge=1)
    role: AIPlanningMessageRole
    turn_type: AIPlanningMessageTurnType
    content: str = Field(min_length=1)
    structured_payload: dict[str, Any] | None = None
    created_at: datetime


class AIPlanningDraft(DSLModel):
    id: int = Field(ge=1)
    session_id: int = Field(ge=1)
    scenario_key: str = Field(min_length=1, max_length=100)
    title: str = Field(min_length=1, max_length=200)
    status: AIPlanningDraftStatus
    dsl_generation_id: int | None = Field(default=None, ge=1)
    dsl_case: DSLCase | None = None
    warnings: list[str] = Field(default_factory=list)
    normalization_notes: list[str] = Field(default_factory=list)
    error_message: str | None = Field(default=None, max_length=4000)
    created_at: datetime
    updated_at: datetime


class AIPlanningSessionDetail(DSLModel):
    session: AIPlanningSession
    messages: list[AIPlanningMessage] = Field(default_factory=list)
    drafts: list[AIPlanningDraft] = Field(default_factory=list)


class CreateAIPlanningSessionRequest(DSLModel):
    project_id: int = Field(ge=1)
    case_id: int | None = Field(default=None, ge=1)


class AIPlanningMessageCreateRequest(DSLModel):
    content: str = Field(min_length=1, max_length=4000)


class GenerateAIPlanningDraftsRequest(DSLModel):
    scenario_keys: list[str] = Field(min_length=1)
    current_case: DSLCase | None = None
    current_steps: list[DSLStep] | None = None
    current_input_contract: list[DSLCaseInputContract] | None = None
    current_output_contract: list[DSLCaseOutputContract] | None = None
    preserve_contracts: bool = False


class UpdateAIPlanningDraftStatusRequest(DSLModel):
    status: Literal["imported", "rejected"]


class SavedCaseResult(DSLModel):
    case_id: int = Field(ge=1)
    case_name: str
    status: Literal["saved"] = "saved"


class ExecutionSummaryResult(DSLModel):
    execution_id: int = Field(ge=1)
    case_id: int = Field(ge=1)
    case_name: str
    status: Literal["passed", "failed", "needs_intervention", "error"]
    total_steps: int
    passed_steps: int
    failed_steps: int
    duration_ms: int | None = None
    screenshot_url: str | None = None
    report_url: str


class AIPlanningTurnResponse(DSLModel):
    assistant_message: str = Field(min_length=1)
    session_status: AIPlanningSessionStatus
    requirements: AIPlanningRequirements = Field(default_factory=AIPlanningRequirements)
    missing_slots: list[str] = Field(default_factory=list)
    suggested_questions: list[str] = Field(default_factory=list)
    plan: AIPlanningPlan | None = None
    drafts: list[AIPlanningDraft] = Field(default_factory=list)
    next_action: AIPlanningNextAction
    tool_calls: list[AIPlanningToolCall] = Field(default_factory=list)
    saved_cases: list[SavedCaseResult] = Field(default_factory=list)
    execution_summaries: list[ExecutionSummaryResult] = Field(default_factory=list)
