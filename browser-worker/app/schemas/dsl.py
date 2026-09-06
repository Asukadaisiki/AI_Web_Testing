"""Structured DSL schemas for runnable test cases."""

from __future__ import annotations

import hashlib
import json
from typing import Annotated, Literal

from pydantic import BaseModel, ConfigDict, Field, field_validator, model_validator


DSL_CANONICAL_VERSION = "dsl.canonical.v1"


class DSLModel(BaseModel):
    model_config = ConfigDict(extra="ignore", str_strip_whitespace=True)


TargetStrategy = Literal[
    "css",
    "xpath",
    "data-testid",
    "element_id",
    "tag",
]
LocatorConfidence = Literal["high", "medium", "low"]


# Strategy name normalization map: AI-generated variant -> canonical name.
_STRATEGY_NORMALIZE: dict[str, str] = {
    "css_selector": "css",
    "data_testid": "data-testid",
    "elementId": "element_id",
    "href": "css",
    "link": "role",
    "button": "role",
    "aria": "role",
    "id": "element_id",
    "name": "tag",
}


class LocatorCandidate(BaseModel):
    """Pre-scored candidate locator strategy for a DSL step."""
    model_config = ConfigDict(extra="ignore", str_strip_whitespace=True)

    strategy: str = Field(description="Locator strategy name (normalized at runtime)")

    @field_validator("strategy", mode="before")
    @classmethod
    def _normalize_strategy(cls, v: str) -> str:
        return _STRATEGY_NORMALIZE.get(v, v)
    selector: str | None = Field(default=None, description="Explicit selector value (for css/xpath/data-testid/etc).")
    semantic_value: str | None = Field(default=None, description="Semantic value (role name, label text, etc).")
    pre_score: float = Field(ge=0.0, le=1.0, description="Generation-time pre-score 0.0-1.0.")
    pre_features: dict | None = Field(default=None, description="Pre-score feature breakdown for debugging.")


class Postcondition(BaseModel):
    """Post-action verification condition."""
    model_config = ConfigDict(extra="ignore", str_strip_whitespace=True)

    type: Literal[
        "url_contains", "url_changes", "text_visible",
        "text_gone", "element_visible", "element_gone",
        "network_request", "dom_changed", "value_changed",
    ]
    value: str | None = Field(default=None, description="Expected value (URL fragment, text, selector).")
    timeout_ms: int = Field(default=3000, ge=100, le=30000)

    @model_validator(mode="after")
    def validate_required_value(self) -> "Postcondition":
        if self.type == "url_contains" and not self.value:
            raise ValueError("url_contains postcondition requires a target URL value")
        return self


class GotoStep(DSLModel):
    action: Literal["goto"]
    value: str = Field(min_length=1, description="Target URL or path.")


class ClickStep(DSLModel):
    action: Literal["click"]
    target: str = Field(min_length=1, description="Semantic or explicit locator.")
    page_state: str | None = Field(default=None, description="Page state this step belongs to (S0, S1, …).")
    target_strategy: TargetStrategy | None = Field(default=None, description="Locator strategy hint.")
    locator_confidence: LocatorConfidence | None = Field(
        default=None, description="AI-assessed locator confidence. low triggers VLM pre-verification.",
    )
    candidates: list[LocatorCandidate] = Field(default_factory=list, description="Pre-scored candidate locators.")
    postconditions: list[Postcondition] = Field(default_factory=list, description="Post-action verification conditions.")


class InputStep(DSLModel):
    action: Literal["input"]
    target: str = Field(min_length=1, description="Semantic or explicit locator.")
    value: str = Field(description="Input text.")
    trigger: str | None = Field(default=None, description="Key to press after input (e.g. Enter for quantity/search fields).")
    page_state: str | None = Field(default=None, description="Page state this step belongs to (S0, S1, …).")
    target_strategy: TargetStrategy | None = Field(default=None, description="Locator strategy hint.")
    locator_confidence: LocatorConfidence | None = Field(
        default=None, description="AI-assessed locator confidence. low triggers VLM pre-verification.",
    )
    candidates: list[LocatorCandidate] = Field(default_factory=list, description="Pre-scored candidate locators.")
    postconditions: list[Postcondition] = Field(default_factory=list, description="Post-action verification conditions.")


class WaitForStep(DSLModel):
    action: Literal["wait_for"]
    target: str = Field(min_length=1, description="Target to wait for.")
    timeout_ms: int = Field(default=5000, ge=1, le=60000)
    page_state: str | None = Field(default=None, description="Page state this step belongs to (S0, S1, …).")
    target_strategy: TargetStrategy | None = Field(default=None, description="Locator strategy hint.")
    locator_confidence: LocatorConfidence | None = Field(
        default=None, description="AI-assessed locator confidence. low triggers VLM pre-verification.",
    )
    candidates: list[LocatorCandidate] = Field(default_factory=list, description="Pre-scored candidate locators.")
    postconditions: list[Postcondition] = Field(default_factory=list, description="Post-action verification conditions.")


class AssertTextStep(DSLModel):
    action: Literal["assert_text"]
    target: str = Field(min_length=1, description="Target to assert against.")
    value: str = Field(min_length=1, description="Expected text.")
    page_state: str | None = Field(default=None, description="Page state this step belongs to (S0, S1, …).")
    target_strategy: TargetStrategy | None = Field(default=None, description="Locator strategy hint.")
    locator_confidence: LocatorConfidence | None = Field(
        default=None, description="AI-assessed locator confidence. low triggers VLM pre-verification.",
    )
    candidates: list[LocatorCandidate] = Field(default_factory=list, description="Pre-scored candidate locators.")
    postconditions: list[Postcondition] = Field(default_factory=list, description="Post-action verification conditions.")


class AssertUrlContainsStep(DSLModel):
    action: Literal["assert_url_contains"]
    value: str = Field(min_length=1, description="Expected URL fragment.")


class CaptureTextStep(DSLModel):
    action: Literal["capture_text"]
    target: str = Field(min_length=1, description="Element to capture text from.")
    context_key: str = Field(
        min_length=1,
        max_length=100,
        pattern=r"^[A-Za-z_][A-Za-z0-9_]*$",
        description="Runtime variable name to store the captured text.",
    )
    page_state: str | None = Field(default=None, description="Page state this step belongs to (S0, S1, …).")
    target_strategy: TargetStrategy | None = Field(default=None, description="Locator strategy hint.")
    locator_confidence: LocatorConfidence | None = Field(
        default=None, description="AI-assessed locator confidence. low triggers VLM pre-verification.",
    )
    candidates: list[LocatorCandidate] = Field(default_factory=list, description="Pre-scored candidate locators.")
    postconditions: list[Postcondition] = Field(default_factory=list, description="Post-action verification conditions.")


DSLVariableType = Literal["string", "number", "boolean", "object", "array"]
DSLVariableSource = Literal[
    "latest_url",
    "error_message",
    "status",
    "last_step_url",
    "last_step_page_title",
    "last_step_target",
    "last_step_value",
    "last_step_error_message",
]


class DSLCaseInputContract(DSLModel):
    name: str = Field(min_length=1, max_length=100)
    context_key: str = Field(min_length=1, max_length=100, pattern=r"^[A-Za-z_][A-Za-z0-9_]*$")
    value_type: DSLVariableType
    required: bool = True
    description: str | None = Field(default=None, max_length=500)
    value: str | None = Field(default=None, description="Default value for this variable.")


class DSLCaseOutputContract(DSLModel):
    name: str = Field(min_length=1, max_length=100)
    context_key: str = Field(min_length=1, max_length=100, pattern=r"^[A-Za-z_][A-Za-z0-9_]*$")
    value_type: DSLVariableType
    source: DSLVariableSource | None = None
    description: str | None = Field(default=None, max_length=500)


DSLStep = Annotated[
    GotoStep
    | ClickStep
    | InputStep
    | WaitForStep
    | AssertTextStep
    | AssertUrlContainsStep
    | CaptureTextStep,
    Field(discriminator="action"),
]


class DSLCase(DSLModel):
    name: str = Field(min_length=1, max_length=200)
    description: str | None = Field(default=None, max_length=1000)
    base_url: str | None = Field(default=None, min_length=1, max_length=500)
    input_contract: list[DSLCaseInputContract] = Field(default_factory=list)
    output_contract: list[DSLCaseOutputContract] = Field(default_factory=list)
    steps: list[DSLStep] = Field(min_length=1)


def load_canonical_dsl(
    canonical_json: str,
    expected_sha256: str,
    canonical_version: str,
) -> tuple[DSLCase, dict]:
    """Verify Go-owned canonical bytes and reject semantic normalization drift."""
    if canonical_version != DSL_CANONICAL_VERSION:
        raise ValueError(f"Unsupported DSL canonical version: {canonical_version}.")
    actual_sha256 = hashlib.sha256(canonical_json.encode("utf-8")).hexdigest()
    if actual_sha256 != expected_sha256:
        raise ValueError("Canonical DSL SHA-256 does not match the approved generation.")
    payload = json.loads(canonical_json)
    if not isinstance(payload, dict):
        raise ValueError("Canonical DSL must be a JSON object.")
    case = DSLCase.model_validate(payload)
    if case.model_dump(mode="json") != payload:
        raise ValueError("Canonical DSL is not fully materialized or violates the worker schema.")
    return case, payload
