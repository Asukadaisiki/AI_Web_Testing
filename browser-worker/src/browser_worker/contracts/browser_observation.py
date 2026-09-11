"""Versioned browser observation and structured locator contracts."""

from __future__ import annotations

import hashlib
import json
from typing import Annotated, Any, Literal

from pydantic import BaseModel, ConfigDict, Field, TypeAdapter, model_validator

BROWSER_OBSERVATION_VERSION = "browser.observation.v2"
TARGET_BINDING_VERSION = "grounding.target-binding.v1"
LOCATOR_KINDS = frozenset(
    {"role", "label", "placeholder", "text", "test_id", "css", "xpath", "scoped"}
)


class StrictContract(BaseModel):
    model_config = ConfigDict(extra="forbid", strict=True, str_strip_whitespace=True)


class RoleLocatorSpec(StrictContract):
    kind: Literal["role"]
    role: str = Field(min_length=1)
    name: str | None = None
    exact: bool = True


class ValueLocatorSpec(StrictContract):
    kind: Literal["label", "placeholder", "text", "test_id", "css", "xpath"]
    value: str = Field(min_length=1)
    exact: bool = True


LeafLocatorSpec = Annotated[
    RoleLocatorSpec | ValueLocatorSpec,
    Field(discriminator="kind"),
]


class ScopedLocatorSpec(StrictContract):
    kind: Literal["scoped"]
    scope: LeafLocatorSpec
    target: LeafLocatorSpec


LocatorSpec = Annotated[
    RoleLocatorSpec | ValueLocatorSpec | ScopedLocatorSpec,
    Field(discriminator="kind"),
]
_LOCATOR_ADAPTER = TypeAdapter(LocatorSpec)


class ContextPath(StrictContract):
    frames: list[str] = Field(default_factory=list)
    shadow_hosts: list[str] = Field(default_factory=list)


class A11yFact(StrictContract):
    role: str = ""
    name: str = ""
    description: str | None = None
    value: Any = None
    states: dict[str, Any] = Field(default_factory=dict)
    relations: dict[str, list[str] | str | None] = Field(default_factory=dict)


class DOMFact(StrictContract):
    backend_node_id: int | None = None
    tag: str = ""
    attrs: dict[str, str] = Field(default_factory=dict)
    text: str = Field(default="", max_length=256)


class RuntimeFact(StrictContract):
    connected: bool
    visible: bool
    enabled: bool
    editable: bool


class ObservedLocator(StrictContract):
    candidate_id: str | None = Field(default=None, min_length=1)
    locator: LocatorSpec
    provenance: str = Field(min_length=1)
    observed_count: int = Field(ge=0)


class ElementFact(StrictContract):
    element_ref: str = Field(min_length=1)
    context_path: ContextPath = Field(default_factory=ContextPath)
    a11y: A11yFact | None = None
    dom: DOMFact | None = None
    runtime: RuntimeFact
    locators: list[ObservedLocator] = Field(default_factory=list)


class ObservationRelation(StrictContract):
    kind: str = Field(min_length=1)
    source: str = Field(min_length=1)
    target: str = Field(min_length=1)


class PageStateFact(StrictContract):
    state_id: str = Field(min_length=1)
    revision: int = Field(ge=1)
    url: str = ""
    title: str = ""
    state_sha256: str = Field(pattern=r"^[0-9a-f]{64}$")
    previous_state_sha256: str | None = Field(
        default=None,
        pattern=r"^[0-9a-f]{64}$",
    )


class ObservationArtifact(StrictContract):
    uri: str = Field(min_length=1)
    sha256: str = Field(pattern=r"^[0-9a-f]{64}$")
    content_bytes: int = Field(ge=0)


class BrowserObservation(StrictContract):
    schema_version: Literal["browser.observation.v2"] = BROWSER_OBSERVATION_VERSION
    probe_id: str | None = Field(default=None, min_length=1)
    observation_id: str = Field(min_length=1)
    page_state: PageStateFact
    elements: list[ElementFact] = Field(default_factory=list)
    relations: list[ObservationRelation] = Field(default_factory=list)
    artifact: ObservationArtifact | None = None


class LocatorCandidate(StrictContract):
    candidate_id: str = Field(min_length=1)
    element_ref: str = Field(min_length=1)
    context_path: ContextPath = Field(default_factory=ContextPath)
    locator: LocatorSpec
    provenance: str = Field(min_length=1)
    observed_count: int = Field(ge=0)
    visible: bool
    enabled: bool
    score: float = Field(ge=0.0, le=1.0)

    @model_validator(mode="after")
    def validate_context_compatibility(self) -> LocatorCandidate:
        if self.context_path.shadow_hosts and _contains_xpath(self.locator):
            raise ValueError("xpath cannot cross an open shadow root")
        return self


class TargetBinding(StrictContract):
    schema_version: Literal["grounding.target-binding.v1"] = TARGET_BINDING_VERSION
    binding_id: str = Field(min_length=1)
    plan_id: str = Field(min_length=1)
    plan_version: int = Field(ge=1)
    plan_step_id: str = Field(min_length=1)
    probe_id: str | None = Field(default=None, min_length=1)
    semantic_target: str = Field(min_length=1)
    action: str = Field(min_length=1)
    page_state_id: str = Field(min_length=1)
    observation_id: str = Field(min_length=1)
    observation_sha256: str = Field(pattern=r"^[0-9a-f]{64}$")
    element_refs: list[str] = Field(min_length=1)
    candidates: list[LocatorCandidate] = Field(min_length=1)
    selected_candidate_id: str = Field(min_length=1)
    binding_sha256: str = Field(pattern=r"^[0-9a-f]{64}$")

    @model_validator(mode="after")
    def validate_selected_candidate(self) -> TargetBinding:
        ids = [candidate.candidate_id for candidate in self.candidates]
        if len(ids) != len(set(ids)):
            raise ValueError("candidate_id values must be unique")
        if self.selected_candidate_id not in ids:
            raise ValueError("selected_candidate_id must reference a candidate")
        if len(self.element_refs) != len(set(self.element_refs)):
            raise ValueError("element_refs must be unique")
        if any(
            candidate.element_ref not in self.element_refs
            for candidate in self.candidates
        ):
            raise ValueError("candidate element_ref must belong to the binding")
        return self


def validate_locator_spec(value: object) -> LocatorSpec:
    return _LOCATOR_ADAPTER.validate_python(value)


def _contains_xpath(locator: LocatorSpec) -> bool:
    if isinstance(locator, ValueLocatorSpec):
        return locator.kind == "xpath"
    if isinstance(locator, ScopedLocatorSpec):
        return _contains_xpath(locator.scope) or _contains_xpath(locator.target)
    return False


def canonical_sha256(value: object) -> str:
    payload = json.dumps(
        value,
        ensure_ascii=False,
        separators=(",", ":"),
        sort_keys=True,
    ).encode("utf-8")
    return hashlib.sha256(payload).hexdigest()
