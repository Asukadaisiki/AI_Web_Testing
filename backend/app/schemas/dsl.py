"""Structured DSL schemas for runnable test cases."""

from __future__ import annotations

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


class GenerateDslRequest(DSLModel):
    prompt: str = Field(min_length=1, max_length=4000)
    base_url: str | None = Field(default=None, min_length=1, max_length=500)
    actor_user_id: int = Field(ge=1)


class DSLValidationResult(DSLModel):
    valid: bool = True
    case: DSLCase
    supported_actions: list[str]


class GenerateDslResponse(DSLModel):
    case: DSLCase
    supported_actions: list[str]
    warnings: list[str] = Field(default_factory=list)
