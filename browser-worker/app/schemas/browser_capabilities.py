"""Contracts exposed by the Python browser capability worker."""

from __future__ import annotations

from typing import Any, Literal

from pydantic import Field, model_validator

from app.schemas.dsl import DSLModel


BrowserCapabilityName = Literal[
    "explore_page",
    "explore_flow",
    "validate_page_elements",
]


class ExplorePageArguments(DSLModel):
    url: str = Field(min_length=1)
    core_user_flow_text: str | None = None


class ExploreFlowAction(DSLModel):
    action: Literal["click", "input", "wait_for"]
    target: str = Field(min_length=1)
    value: str | None = None
    timeout_ms: int | None = Field(default=None, ge=1, le=60000)


class ExploreFlowStep(DSLModel):
    url: str | None = None
    description: str | None = None
    actions: list[ExploreFlowAction] = Field(default_factory=list)


class ExploreFlowArguments(DSLModel):
    base_url: str | None = None
    flow_description: str | None = None
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
    def validate_mode(self) -> "ValidatePageElementsArguments":
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


class BrowserCapabilityRequest(DSLModel):
    actor_user_id: int = Field(ge=1)
    project_id: int = Field(ge=1)
    conversation_id: str = Field(min_length=1, max_length=100)
    arguments: dict[str, Any] = Field(default_factory=dict)


class BrowserCapabilityResponse(DSLModel):
    result: dict[str, Any]
