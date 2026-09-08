"""Strict research-v1 Action IR schemas owned by the Go control plane."""

from __future__ import annotations

from typing import Annotated, Any, Literal

from pydantic import (
    BaseModel,
    ConfigDict,
    Field,
    ValidationInfo,
    field_validator,
    model_validator,
)

from browser_worker.contracts.dsl import (
    AssertTextStep,
    AssertUrlContainsStep,
    CaptureTextStep,
    ClickStep,
    ConditionSpec,
    GotoStep,
    InputStep,
    LocatorCandidate,
    WaitForStep,
)


ResearchProfile = Literal["research-v1"]
ResearchIdempotency = Literal["idempotent", "non_idempotent"]
ResearchSideEffect = Literal[
    "none",
    "browser_state",
    "external_state",
    "unknown",
]
ResearchValidationPhase = Literal["draft", "executable"]

_STRICT_CONFIG = ConfigDict(
    extra="forbid",
    strict=True,
    str_strip_whitespace=True,
)

EXECUTABLE_CANDIDATE_STRATEGIES = {
    "css",
    "css_selector",
    "xpath",
    "data-testid",
    "data_testid",
    "role",
    "text",
    "label",
    "placeholder",
    "element_id",
    "tag",
    "semantic",
    "verified_role",
    "verified_role_fuzzy",
    "verified_css",
    "verified_xpath",
    "verified_placeholder",
    "verified_placeholder_fuzzy",
    "verified_label",
    "verified_label_fuzzy",
    "verified_text",
    "verified_data-testid",
    "verified_element_id",
    "a11y_scoped_role_exact",
    "a11y_scoped_role_fuzzy",
    "a11y_scoped_text_exact",
    "a11y_scoped_text_fuzzy",
}


class ResearchConditionSpec(ConditionSpec):
    model_config = _STRICT_CONFIG

    timeout_ms: int | float = Field(default=3000, ge=100, le=30000)

    @field_validator("method", mode="before")
    @classmethod
    def validate_method(cls, value: Any) -> Any:
        if value is not None and (
            not isinstance(value, str) or not value.strip()
        ):
            raise ValueError("condition method must be a non-empty string")
        return value


class ResearchLocatorCandidate(LocatorCandidate):
    model_config = _STRICT_CONFIG

    selector: str = Field(min_length=1)

    @field_validator("strategy", mode="before")
    @classmethod
    def validate_raw_strategy(cls, value: Any) -> Any:
        if not isinstance(value, str) or value.strip() not in (
            EXECUTABLE_CANDIDATE_STRATEGIES
        ):
            raise ValueError("candidate strategy is not supported by the runner")
        return value

    @field_validator("strategy")
    @classmethod
    def validate_strategy(cls, value: str) -> str:
        if value not in EXECUTABLE_CANDIDATE_STRATEGIES:
            raise ValueError("candidate strategy is not supported by the runner")
        return value


class ResearchCaseInputContract(BaseModel):
    model_config = _STRICT_CONFIG

    name: str = Field(min_length=1, max_length=100)
    context_key: str = Field(
        min_length=1,
        max_length=100,
        pattern=r"^[A-Za-z_][A-Za-z0-9_]*$",
    )
    value_type: Literal["string", "number", "boolean", "object", "array"]
    required: bool = True
    description: str | None = None
    value: str | None = None

    @field_validator("name", mode="before")
    @classmethod
    def validate_raw_name(cls, value: Any) -> Any:
        if not isinstance(value, str) or not value.strip() or len(value) > 100:
            raise ValueError("contract name is invalid")
        return value


class ResearchCaseOutputContract(BaseModel):
    model_config = _STRICT_CONFIG

    name: str = Field(min_length=1, max_length=100)
    context_key: str = Field(
        min_length=1,
        max_length=100,
        pattern=r"^[A-Za-z_][A-Za-z0-9_]*$",
    )
    value_type: Literal["string", "number", "boolean", "object", "array"]
    source: Literal[
        "latest_url",
        "error_message",
        "status",
        "last_step_url",
        "last_step_page_title",
        "last_step_target",
        "last_step_value",
        "last_step_error_message",
    ] | None = None
    description: str | None = None

    @field_validator("name", mode="before")
    @classmethod
    def validate_raw_name(cls, value: Any) -> Any:
        if not isinstance(value, str) or not value.strip() or len(value) > 100:
            raise ValueError("contract name is invalid")
        return value


class ResearchGotoStep(GotoStep):
    model_config = _STRICT_CONFIG

    target: str = Field(min_length=1)
    intent: str = Field(min_length=1, max_length=500)
    preconditions: list[ResearchConditionSpec]
    postconditions: list[ResearchConditionSpec] = Field(min_length=1)
    idempotency: Literal["idempotent"]
    side_effect: Literal["browser_state"]


class ResearchClickStep(ClickStep):
    model_config = _STRICT_CONFIG

    intent: str = Field(min_length=1, max_length=500)
    preconditions: list[ResearchConditionSpec] = Field(min_length=1)
    postconditions: list[ResearchConditionSpec] = Field(min_length=1)
    idempotency: ResearchIdempotency
    side_effect: ResearchSideEffect
    candidates: list[ResearchLocatorCandidate] = Field(default_factory=list)


class ResearchInputStep(InputStep):
    model_config = _STRICT_CONFIG

    intent: str = Field(min_length=1, max_length=500)
    preconditions: list[ResearchConditionSpec] = Field(min_length=1)
    postconditions: list[ResearchConditionSpec] = Field(min_length=1)
    idempotency: Literal["idempotent"]
    side_effect: Literal["browser_state"]
    candidates: list[ResearchLocatorCandidate] = Field(default_factory=list)


class ResearchWaitForStep(WaitForStep):
    model_config = _STRICT_CONFIG

    intent: str = Field(min_length=1, max_length=500)
    preconditions: list[ResearchConditionSpec]
    postconditions: list[ResearchConditionSpec]
    idempotency: Literal["idempotent"]
    side_effect: Literal["none"]
    candidates: list[ResearchLocatorCandidate] = Field(default_factory=list)
    timeout_ms: int | float = Field(default=5000, ge=1, le=60000)


class ResearchAssertTextStep(AssertTextStep):
    model_config = _STRICT_CONFIG

    intent: str = Field(min_length=1, max_length=500)
    preconditions: list[ResearchConditionSpec]
    postconditions: list[ResearchConditionSpec]
    idempotency: Literal["idempotent"]
    side_effect: Literal["none"]
    candidates: list[ResearchLocatorCandidate] = Field(default_factory=list)


class ResearchAssertUrlContainsStep(AssertUrlContainsStep):
    model_config = _STRICT_CONFIG

    target: str = Field(min_length=1)
    intent: str = Field(min_length=1, max_length=500)
    preconditions: list[ResearchConditionSpec]
    postconditions: list[ResearchConditionSpec]
    idempotency: Literal["idempotent"]
    side_effect: Literal["none"]


class ResearchCaptureTextStep(CaptureTextStep):
    model_config = _STRICT_CONFIG

    intent: str = Field(min_length=1, max_length=500)
    preconditions: list[ResearchConditionSpec]
    postconditions: list[ResearchConditionSpec]
    idempotency: Literal["idempotent"]
    side_effect: Literal["none"]
    candidates: list[ResearchLocatorCandidate] = Field(default_factory=list)


ResearchDSLStep = Annotated[
    ResearchGotoStep
    | ResearchClickStep
    | ResearchInputStep
    | ResearchWaitForStep
    | ResearchAssertTextStep
    | ResearchAssertUrlContainsStep
    | ResearchCaptureTextStep,
    Field(discriminator="action"),
]

_LOCATOR_ACTIONS = {
    "click",
    "input",
    "wait_for",
    "assert_text",
    "capture_text",
}
_PREFLIGHT_REQUIRED_ACTIONS = {
    "click",
    "input",
    "capture_text",
}


def _step_requires_preflight_candidates(step: Any) -> bool:
    if step.action in _PREFLIGHT_REQUIRED_ACTIONS:
        return True
    if getattr(step, "target_strategy", None):
        return True
    target = str(getattr(step, "target", "") or "").strip()
    return target.startswith(("css=", "xpath=", "#", ".", "//"))


class ResearchDSLCase(BaseModel):
    model_config = _STRICT_CONFIG

    profile: ResearchProfile
    name: str = Field(min_length=1, max_length=200)
    description: str | None = Field(default=None, min_length=1, max_length=1000)
    base_url: str | None = Field(default=None, min_length=1, max_length=500)
    input_contract: list[ResearchCaseInputContract] = Field(default_factory=list)
    output_contract: list[ResearchCaseOutputContract] = Field(default_factory=list)
    steps: list[ResearchDSLStep] = Field(min_length=1)

    @model_validator(mode="before")
    @classmethod
    def validate_raw_actions(cls, value: Any) -> Any:
        if not isinstance(value, dict):
            return value
        for step in value.get("steps") or []:
            if not isinstance(step, dict):
                continue
            action = step.get("action")
            if action not in {
                "goto",
                "click",
                "input",
                "wait_for",
                "assert_text",
                "assert_url_contains",
                "capture_text",
            }:
                raise ValueError(f"unsupported DSL action: {action}")
        return value

    @field_validator("description", "base_url", mode="before")
    @classmethod
    def validate_raw_optional_text(
        cls,
        value: Any,
        info: ValidationInfo,
    ) -> Any:
        if value is None:
            return value
        max_length = 1000 if info.field_name == "description" else 500
        if (
            not isinstance(value, str)
            or not value.strip()
            or len(value) > max_length
        ):
            raise ValueError(f"case.{info.field_name} is invalid")
        return value

    def model_dump(self, *args: Any, **kwargs: Any) -> dict[str, Any]:
        payload = super().model_dump(*args, **kwargs)
        for step_index, step in enumerate(self.steps):
            step_payload = payload["steps"][step_index]
            if step.action in _LOCATOR_ACTIONS:
                for field_name in ("locator_confidence", "candidates"):
                    if field_name not in step.model_fields_set:
                        step_payload.pop(field_name, None)
            for field_name in ("preconditions", "postconditions"):
                conditions = getattr(step, field_name)
                for condition_index, condition in enumerate(conditions):
                    for condition_field in ("method", "status"):
                        if condition_field not in condition.model_fields_set:
                            step_payload[field_name][condition_index].pop(
                                condition_field,
                                None,
                            )
        return payload

    @model_validator(mode="after")
    def validate_phase(self, info: ValidationInfo) -> "ResearchDSLCase":
        context = info.context if isinstance(info.context, dict) else {}
        phase = context.get("phase", "executable")
        if phase not in {"draft", "executable"}:
            raise ValueError(f"unsupported DSL validation phase: {phase}")

        for index, step in enumerate(self.steps):
            if step.action not in _LOCATOR_ACTIONS:
                continue
            if phase == "draft":
                if "candidates" in step.model_fields_set:
                    raise ValueError(
                        f"case.steps[{index}].candidates must be added by locator preflight"
                    )
                if "locator_confidence" in step.model_fields_set:
                    raise ValueError(
                        f"case.steps[{index}].locator_confidence must be added by locator preflight"
                    )
                continue

            if not _step_requires_preflight_candidates(step) and not step.candidates:
                continue

            if step.locator_confidence not in {"high", "medium"}:
                raise ValueError(
                    f"case.steps[{index}].locator_confidence must be high or medium"
                )
            if not step.candidates:
                raise ValueError(
                    f"case.steps[{index}].candidates must be a non-empty array"
                )
            if not any(
                _has_verified_preflight_provenance(candidate)
                for candidate in step.candidates
            ):
                raise ValueError(
                    f"case.steps[{index}].candidates lack verified preflight evidence"
                )
        return self


def _has_verified_preflight_provenance(
    candidate: ResearchLocatorCandidate,
) -> bool:
    features = candidate.pre_features or {}
    if features.get("verified") is not True:
        return False
    source = features.get("source")
    if not isinstance(source, str):
        return False
    source = source.strip()
    strategy = candidate.strategy
    if strategy.startswith("verified_"):
        return source in {
            "a11y_backend_dom_node",
            "dom_verified_interactive_control",
            strategy,
        }
    if strategy == "role":
        return source == "a11y_role_exact"
    if strategy == "text":
        return source == "a11y_text_exact"
    if strategy.startswith("a11y_scoped_"):
        return source == strategy
    return False


def validate_research_dsl(
    payload: dict[str, Any],
    *,
    phase: ResearchValidationPhase = "executable",
) -> ResearchDSLCase:
    return ResearchDSLCase.model_validate(payload, context={"phase": phase})


def is_research_case(case: object) -> bool:
    return isinstance(case, ResearchDSLCase)


def case_dsl_profile(case: object) -> Literal["legacy-v1", "research-v1"]:
    return "research-v1" if is_research_case(case) else "legacy-v1"
