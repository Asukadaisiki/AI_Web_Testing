"""Structured research-v2 DSL compiled from persisted target bindings."""

from __future__ import annotations

from typing import Any, Literal

from pydantic import Field, model_validator

from browser_worker.contracts.action_ir import (
    ResearchCaseInputContract,
    ResearchCaseOutputContract,
    ResearchConditionSpec,
)
from browser_worker.contracts.browser_observation import (
    LocatorCandidate,
    StrictContract,
)

ResearchV2Action = Literal[
    "goto",
    "click",
    "input",
    "wait_for",
    "assert_text",
    "assert_url_contains",
    "capture_text",
]


class PlanBindingV2(StrictContract):
    plan_id: str = Field(min_length=1)
    version: int = Field(ge=1)
    sha256: str = Field(pattern=r"^[0-9a-f]{64}$")


class ObservationBindingV2(StrictContract):
    binding_id: str = Field(min_length=1)
    binding_sha256: str = Field(pattern=r"^[0-9a-f]{64}$")
    probe_id: str | None = Field(default=None, min_length=1)
    observation_id: str = Field(min_length=1)
    observation_sha256: str = Field(pattern=r"^[0-9a-f]{64}$")
    page_state_id: str | None = Field(default=None, min_length=1)


class ResearchV2Step(StrictContract):
    plan_step_id: str = Field(min_length=1, max_length=64)
    action: ResearchV2Action
    intent: str = Field(min_length=1, max_length=500)
    target_binding_id: str | None = Field(default=None, max_length=64)
    probe_id: str | None = Field(default=None, max_length=64)
    observation_id: str | None = Field(default=None, max_length=64)
    observation_sha256: str | None = Field(
        default=None,
        pattern=r"^[0-9a-f]{64}$",
    )
    page_state_id: str | None = Field(default=None, max_length=256)
    selected_candidate_id: str | None = Field(default=None, max_length=64)
    semantic_target: str | None = Field(default=None, max_length=500)
    locator_candidates: list[LocatorCandidate] | None = None
    value: str | None = None
    trigger: Literal["Enter", "Tab"] | None = None
    context_key: str | None = Field(
        default=None,
        max_length=100,
        pattern=r"^[A-Za-z_][A-Za-z0-9_]*$",
    )
    timeout_ms: int | float | None = Field(default=None, ge=1, le=60000)
    preconditions: list[ResearchConditionSpec]
    postconditions: list[ResearchConditionSpec]
    idempotency: Literal["idempotent", "non_idempotent"]
    side_effect: Literal["none", "browser_state", "external_state", "unknown"]

    @property
    def target(self) -> str | None:
        return self.semantic_target

    @property
    def candidates(self) -> list[LocatorCandidate]:
        return self.locator_candidates or []

    @property
    def target_strategy(self) -> None:
        return None

    @property
    def locator_confidence(self) -> Literal["high", "medium"]:
        return "high" if self.locator_candidates else "medium"

    @model_validator(mode="after")
    def validate_action_fields(self) -> ResearchV2Step:
        for condition in [*self.preconditions, *self.postconditions]:
            if condition.type in {"element_visible", "element_gone"}:
                raise ValueError(
                    "research-v2 conditions must not use unbound element selectors"
                )
        if (
            self.action in {"goto", "assert_url_contains", "assert_text", "input"}
            and self.value is None
        ):
            raise ValueError(f"{self.action} requires value")
        if self.action == "capture_text" and self.context_key is None:
            raise ValueError("capture_text requires context_key")
        if self.action in {"goto", "input"} and (
            self.idempotency != "idempotent"
            or self.side_effect != "browser_state"
        ):
            raise ValueError(
                f"{self.action} must be idempotent with browser_state side effect"
            )
        if self.action in {
            "wait_for",
            "assert_text",
            "assert_url_contains",
            "capture_text",
        } and (
            self.idempotency != "idempotent" or self.side_effect != "none"
        ):
            raise ValueError(
                f"{self.action} must be idempotent with no side effect"
            )
        if self.action in {"click", "input"} and (
            not self.preconditions or not self.postconditions
        ):
            raise ValueError(
                f"{self.action} requires preconditions and postconditions"
            )
        if self.action == "goto" and not self.postconditions:
            raise ValueError("goto requires a postcondition")
        return self


class ResearchV2Case(StrictContract):
    profile: Literal["research-v2"]
    name: str = Field(min_length=1, max_length=200)
    description: str | None = Field(default=None, max_length=1000)
    base_url: str | None = Field(default=None, min_length=1, max_length=500)
    input_contract: list[ResearchCaseInputContract] = Field(default_factory=list)
    output_contract: list[ResearchCaseOutputContract] = Field(default_factory=list)
    plan_binding: PlanBindingV2 | None = None
    observation_bindings: list[ObservationBindingV2] | None = None
    steps: list[ResearchV2Step] = Field(min_length=1)


def validate_research_v2_dsl(
    payload: dict[str, Any],
    *,
    phase: Literal["draft", "executable"] = "executable",
) -> ResearchV2Case:
    case = ResearchV2Case.model_validate(payload)
    if phase == "draft":
        if case.plan_binding is not None or case.observation_bindings is not None:
            raise ValueError("draft must not contain compiler-owned bindings")
        for step in case.steps:
            if any(
                value is not None
                for value in (
                    step.semantic_target,
                    step.locator_candidates,
                    step.probe_id,
                    step.observation_id,
                    step.observation_sha256,
                    step.page_state_id,
                    step.selected_candidate_id,
                )
            ):
                raise ValueError("draft must not contain compiler-owned locator fields")
            if _requires_binding(step.action) and not step.target_binding_id:
                raise ValueError(f"{step.action} requires target_binding_id")
        return case
    if case.plan_binding is None or case.observation_bindings is None:
        raise ValueError("executable case requires plan and observation bindings")
    known_bindings = {
        binding.binding_id: binding for binding in case.observation_bindings
    }
    for step in case.steps:
        if _has_target(step.action) and not step.semantic_target:
            raise ValueError(f"{step.action} requires semantic_target")
        if _requires_binding(step.action):
            if not step.target_binding_id or step.target_binding_id not in known_bindings:
                raise ValueError(f"{step.action} has an unknown target binding")
            if not step.locator_candidates:
                raise ValueError(f"{step.action} requires locator candidates")
            candidate_ids = {
                candidate.candidate_id for candidate in step.locator_candidates
            }
            if (
                step.selected_candidate_id is not None
                and step.selected_candidate_id not in candidate_ids
            ):
                raise ValueError(
                    f"{step.action} selected_candidate_id is unknown"
                )
            binding = known_bindings[step.target_binding_id]
            lineage = (
                step.probe_id,
                step.observation_id,
                step.observation_sha256,
                step.page_state_id,
                binding.probe_id,
                binding.page_state_id,
            )
            if any(value is not None for value in lineage) and (
                any(value is None for value in lineage)
                or step.probe_id != binding.probe_id
                or step.observation_id != binding.observation_id
                or step.observation_sha256 != binding.observation_sha256
                or step.page_state_id != binding.page_state_id
            ):
                raise ValueError(
                    f"{step.action} lineage does not match its target binding"
                )
    return case


def _requires_binding(action: str) -> bool:
    return action in {"click", "input", "capture_text"}


def _has_target(action: str) -> bool:
    return action in {"click", "input", "wait_for", "assert_text", "capture_text"}
