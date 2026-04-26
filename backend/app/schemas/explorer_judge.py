"""Schemas for the Explorer-Judge architecture."""

from __future__ import annotations

from typing import Any, Literal

from pydantic import Field

from app.schemas.dsl import DSLModel


# --- Failure classification ---
FailureClassification = Literal[
    "test_design_error",
    "automation_implementation",
    "product_defect",
    "environment_dependency",
    "suspected_flaky",
]

ExplorerRunStatus = Literal["running", "completed", "failed"]


# --- Per-failure Judge conclusion ---
class JudgeConclusion(DSLModel):
    failure_record_id: int | None = None
    step_index: int
    classification: FailureClassification
    confidence: Literal["high", "medium", "low"]
    root_cause_analysis: str = Field(min_length=1)
    reproduction_path: str = Field(min_length=1)
    suggested_action: Literal[
        "regenerate_dsl", "fix_automation", "report_bug",
        "skip_environment", "targeted_retest", "manual_intervention",
    ]
    is_product_bug: bool = False
    requires_human_judgment: bool = False
    recommended_regression: bool = False


# --- Explorer step event (non-terminating) ---
class ExplorerStepEvidence(DSLModel):
    step_index: int
    action: str
    target: str | None = None
    value: str | None = None
    status: Literal["passed", "failed", "cascade_blocked"]
    duration_ms: int | None = None
    error_message: str | None = None
    screenshot_path: str | None = None
    dom_summary: dict[str, Any] | None = None
    console_errors: list[str] = Field(default_factory=list)
    network_errors: list[str] = Field(default_factory=list)
    url: str | None = None
    page_title: str | None = None


# --- Aggregate verdict ---
class ExplorerJudgeVerdict(DSLModel):
    exploration_run_id: int | None = None
    case_id: int
    test_point_status: Literal[
        "all_passed", "has_defects", "has_flaky",
        "environment_blocked", "needs_fix",
    ]
    total_steps: int = 0
    passed_steps: int = 0
    failed_steps: int = 0
    first_failed_step: int | None = None
    failure_phenomenon: str | None = None
    verification_actions: list[str] = Field(default_factory=list)
    possible_causes_ranked: list[dict[str, Any]] = Field(default_factory=list)
    is_suspected_product_bug: bool = False
    regression_recommended: bool = False
    manual_intervention_needed: bool = False
    conclusions: list[JudgeConclusion] = Field(default_factory=list)
    error_report: dict[str, Any] | None = None


# --- Router decision ---
class RouterDecision(DSLModel):
    action: Literal["auto_fix_dsl", "report_to_user", "re_run", "finished"]
    reason: str = Field(min_length=1)
    auto_fix_generation_id: int | None = None
    retry_remaining: int = 0


# --- Exploration result from Explorer runner ---
class ExplorationResult(DSLModel):
    steps: list[ExplorerStepEvidence] = Field(default_factory=list)
    failure_records: list[ExplorerStepEvidence] = Field(default_factory=list)
    total_steps: int = 0
    passed_steps: int = 0
    failed_steps: int = 0
    cascade_blocked_steps: int = 0
