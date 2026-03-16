"""Structured DSL schemas for runnable test cases."""

from __future__ import annotations

from datetime import datetime
from typing import Annotated, Literal

from pydantic import BaseModel, ConfigDict, Field


class DSLModel(BaseModel):
    model_config = ConfigDict(extra="forbid", str_strip_whitespace=True)


class GotoStep(DSLModel):
    action: Literal["goto"]
    value: str = Field(min_length=1, description="Target URL or path.")


class ClickStep(DSLModel):
    action: Literal["click"]
    target: str = Field(min_length=1, description="Semantic or explicit locator.")


class InputStep(DSLModel):
    action: Literal["input"]
    target: str = Field(min_length=1, description="Semantic or explicit locator.")
    value: str = Field(description="Input text.")


class WaitForStep(DSLModel):
    action: Literal["wait_for"]
    target: str = Field(min_length=1, description="Target to wait for.")
    timeout_ms: int = Field(default=5000, ge=1, le=60000)


class AssertTextStep(DSLModel):
    action: Literal["assert_text"]
    target: str = Field(min_length=1, description="Target to assert against.")
    value: str = Field(min_length=1, description="Expected text.")


class AssertUrlContainsStep(DSLModel):
    action: Literal["assert_url_contains"]
    value: str = Field(min_length=1, description="Expected URL fragment.")


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
    | AssertUrlContainsStep,
    Field(discriminator="action"),
]


class DSLCase(DSLModel):
    name: str = Field(min_length=1, max_length=200)
    description: str | None = Field(default=None, max_length=1000)
    base_url: str | None = Field(default=None, min_length=1, max_length=500)
    input_contract: list[DSLCaseInputContract] = Field(default_factory=list)
    output_contract: list[DSLCaseOutputContract] = Field(default_factory=list)
    steps: list[DSLStep] = Field(min_length=1)


GenerateDslMode = Literal["draft", "strict_steps_only"]
GenerateDslImportMode = Literal["replace", "steps_only", "contracts_only"]
GenerateDslBaseUrlSource = Literal["ai_output", "request", "current_case", "none"]
DslGenerationRunStatus = Literal["success", "failed"]


class GenerateDslRequest(DSLModel):
    prompt: str = Field(min_length=1, max_length=4000)
    base_url: str | None = Field(default=None, min_length=1, max_length=500)
    actor_user_id: int = Field(ge=1)
    generation_mode: GenerateDslMode = "draft"
    import_mode: GenerateDslImportMode = "replace"
    current_case: DSLCase | None = None
    current_steps: list[DSLStep] | None = None
    preserve_contracts: bool = False


class DSLValidationResult(DSLModel):
    valid: bool = True
    case: DSLCase
    supported_actions: list[str]


class GenerateDslMeta(DSLModel):
    model: str | None = Field(default=None, max_length=200)
    generation_mode: GenerateDslMode
    import_mode: GenerateDslImportMode
    base_url_source: GenerateDslBaseUrlSource
    base_url_backfilled: bool = False
    repaired_invalid_actions: int = Field(default=0, ge=0)
    removed_invalid_steps: int = Field(default=0, ge=0)
    removed_invalid_contracts: int = Field(default=0, ge=0)
    preserve_contracts_applied: bool = False
    used_current_case_context: bool = False
    used_current_steps_context: bool = False


class GenerateDslResponse(DSLModel):
    generation_id: int = Field(ge=1)
    case: DSLCase
    supported_actions: list[str]
    warnings: list[str] = Field(default_factory=list)
    normalization_notes: list[str] = Field(default_factory=list)
    generation_meta: GenerateDslMeta


class StoredDslGenerationRunSummary(DSLModel):
    id: int = Field(ge=1)
    created_at: datetime
    success: bool
    model_name: str | None = Field(default=None, max_length=200)
    generation_mode: GenerateDslMode
    import_mode: GenerateDslImportMode
    error_type: str | None = Field(default=None, max_length=200)
    error_message: str | None = Field(default=None, max_length=2000)
    repaired_invalid_actions: int = Field(ge=0)
    removed_invalid_steps: int = Field(ge=0)
    removed_invalid_contracts: int = Field(ge=0)
    warnings_count: int = Field(ge=0)
    normalization_notes_count: int = Field(ge=0)
    prompt_preview: str = Field(min_length=1, max_length=200)
