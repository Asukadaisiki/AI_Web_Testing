"""Contracts exposed by the Python browser capability worker."""

from __future__ import annotations

from typing import Any, Literal

from pydantic import Field, model_validator

from browser_worker.contracts.browser_observation import LocatorSpec
from browser_worker.contracts.dsl import DSLModel

BrowserCapabilityName = Literal[
    "explore_page",
    "explore_flow",
    "validate_page_elements",
]


class ExplorePageArguments(DSLModel):
    url: str = Field(min_length=1)
    core_user_flow_text: str | None = None
    observation_schema_version: Literal["v1", "v2"] = "v1"
    probe_id: str | None = Field(default=None, min_length=1, max_length=64)


class ExploreFlowWaitCondition(DSLModel):
    type: Literal["visible", "value_equals"]
    expected: str | None = None

    @model_validator(mode="after")
    def validate_expected(self) -> ExploreFlowWaitCondition:
        if self.type == "value_equals" and self.expected is None:
            raise ValueError("value_equals requires expected")
        if self.type == "visible" and self.expected is not None:
            raise ValueError("visible must not define expected")
        return self


class ExploreFlowAction(DSLModel):
    action: Literal["click", "input", "wait_for"]
    plan_step_id: str | None = Field(default=None, min_length=1, max_length=64)
    target: str | None = Field(default=None, min_length=1)
    locator: LocatorSpec | None = None
    condition: ExploreFlowWaitCondition | None = None
    value: str | None = None
    timeout_ms: int | None = Field(default=None, ge=1, le=60000)

    @model_validator(mode="after")
    def validate_target_and_condition(self) -> ExploreFlowAction:
        if (self.target is None) == (self.locator is None):
            raise ValueError("provide exactly one of target or locator")
        if self.locator is not None and self.locator.kind not in {
            "role",
            "label",
            "placeholder",
            "text",
        }:
            raise ValueError("exploration locator must use a semantic kind")
        if self.locator is not None and self.action != "wait_for":
            raise ValueError("structured exploration locator is only supported by wait_for")
        if self.condition is not None and self.action != "wait_for":
            raise ValueError("condition is only supported by wait_for")
        if (
            self.condition is not None
            and self.condition.type == "value_equals"
            and (self.locator is None or self.condition.expected is None)
        ):
            raise ValueError(
                "value_equals requires a structured locator and expected value"
            )
        return self


class ExploreFlowStep(DSLModel):
    url: str | None = None
    description: str | None = None
    actions: list[ExploreFlowAction] = Field(default_factory=list)


class ExploreFlowArguments(DSLModel):
    base_url: str | None = None
    flow_description: str | None = None
    observation_schema_version: Literal["v1", "v2"] = "v1"
    probe_id: str | None = Field(default=None, min_length=1, max_length=64)
    steps: list[ExploreFlowStep] = Field(min_length=1)


class RequiredElement(DSLModel):
    id: str
    description: str = Field(min_length=1)
    keywords: list[str] = Field(min_length=1)
    roles: list[str] = Field(default_factory=list)


class ValidatePageElementsArguments(DSLModel):
    dsl_case: dict[str, Any] | None = None
    a11y_nodes_by_state: dict[str, list[dict[str, Any]]] | None = None
    required_elements: list[RequiredElement] | None = None
    a11y_nodes: list[dict[str, Any]] | None = None

    @model_validator(mode="after")
    def validate_mode(self) -> ValidatePageElementsArguments:
        if (
            isinstance(self.dsl_case, dict)
            and self.dsl_case.get("profile") == "research-v2"
            and self.required_elements is None
            and self.a11y_nodes is None
        ):
            return self
        dsl_mode = self.dsl_case is not None or self.a11y_nodes_by_state is not None
        requirements_mode = (
            self.required_elements is not None or self.a11y_nodes is not None
        )
        if dsl_mode == requirements_mode:
            raise ValueError(
                "provide exactly one mode: dsl_case with a11y_nodes_by_state, "
                "or required_elements with a11y_nodes"
            )
        if dsl_mode and (
            self.dsl_case is None or self.a11y_nodes_by_state is None
        ):
            raise ValueError("dsl_case mode requires a11y_nodes_by_state")
        if requirements_mode and (
            self.required_elements is None or self.a11y_nodes is None
        ):
            raise ValueError(
                "required_elements mode requires required_elements and a11y_nodes"
            )
        return self


class BrowserCapabilityContext(DSLModel):
    clean_context: bool = False
    entry_url_or_page: str | None = None


class BrowserCapabilityRequest(DSLModel):
    actor_user_id: int = Field(ge=1)
    project_id: int = Field(ge=1)
    conversation_id: str = Field(min_length=1, max_length=100)
    context: BrowserCapabilityContext = Field(default_factory=BrowserCapabilityContext)
    arguments: dict[str, Any] = Field(default_factory=dict)


class BrowserCapabilityResponse(DSLModel):
    result: dict[str, Any]
