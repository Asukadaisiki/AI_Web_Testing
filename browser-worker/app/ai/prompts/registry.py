"""Versioned prompts used by Browser Worker visual capabilities."""

from __future__ import annotations

from dataclasses import dataclass, field
from enum import StrEnum
from string import Formatter
from typing import Any


class PromptStage(StrEnum):
    VLM_LOCATE_SYSTEM = "vlm.locate.system"
    VLM_SECTION_SYSTEM = "vlm.section.system"
    VLM_RANK_CANDIDATE_SYSTEM = "vlm.rank_candidate.system"
    VLM_PAGE_ANNOTATION_SYSTEM = "vlm.page_annotation.system"


@dataclass(frozen=True)
class PromptDefinition:
    stage: PromptStage
    version: str
    template: str
    description: str = ""
    required_variables: tuple[str, ...] = field(default_factory=tuple)
    extension_slots: tuple[str, ...] = field(default_factory=tuple)


@dataclass(frozen=True)
class PromptBuildResult:
    stage: PromptStage
    version: str
    content: str
    loaded_variables: tuple[str, ...]
    extension_slots: tuple[str, ...] = field(default_factory=tuple)


_PROMPTS: dict[PromptStage, PromptDefinition] = {
    PromptStage.VLM_LOCATE_SYSTEM: PromptDefinition(
        stage=PromptStage.VLM_LOCATE_SYSTEM,
        version="vlm.locate.system.v1",
        template=(
            "You are an AI assistant that locates a UI element in a screenshot.\n"
            'Return JSON only in the shape {"bbox":[xmin,ymin,xmax,ymax],"errors":["..."]?}.'
        ),
        description="System prompt for screenshot element localization.",
    ),
    PromptStage.VLM_SECTION_SYSTEM: PromptDefinition(
        stage=PromptStage.VLM_SECTION_SYSTEM,
        version="vlm.section.system.v1",
        template=(
            "You are an AI assistant that finds the broad page area that contains a UI element.\n"
            'Return JSON only in the shape {"bbox":[xmin,ymin,xmax,ymax],"errors":["..."]?}.'
        ),
        description="System prompt for broad screenshot section localization.",
    ),
    PromptStage.VLM_RANK_CANDIDATE_SYSTEM: PromptDefinition(
        stage=PromptStage.VLM_RANK_CANDIDATE_SYSTEM,
        version="vlm.rank_candidate.system.v1",
        template=(
            "You rank numbered UI candidates in a screenshot.\n"
            'Return JSON only in the shape {"candidate_index": number,"errors":["..."]?}.'
        ),
        description="System prompt for ranking visual locator candidates.",
    ),
    PromptStage.VLM_PAGE_ANNOTATION_SYSTEM: PromptDefinition(
        stage=PromptStage.VLM_PAGE_ANNOTATION_SYSTEM,
        version="vlm.page_annotation.system.v1",
        template=(
            "You are an AI assistant that describes the layout structure of a web page screenshot.\n"
            "Return a concise text description (not JSON) covering:\n"
            "1. Overall page layout (header, navigation, main content, sidebar, footer)\n"
            "2. Form sections and their purpose\n"
            "3. Key interactive regions (buttons, links, inputs)\n"
            "4. Any modal or overlay elements\n"
            "Keep the description under 200 words. Focus on spatial layout and element relationships, "
            "not individual element details."
        ),
        description="System prompt for page layout annotation.",
    ),
}


def get_prompt_definition(stage: PromptStage | str) -> PromptDefinition:
    return _PROMPTS[PromptStage(stage)]


def render_prompt(
    stage: PromptStage | str,
    variables: dict[str, Any] | None = None,
) -> PromptBuildResult:
    definition = get_prompt_definition(stage)
    variables = variables or {}
    missing = [name for name in definition.required_variables if name not in variables]
    if missing:
        raise ValueError(f"Missing prompt variables for {definition.stage}: {', '.join(missing)}")

    if definition.required_variables or variables:
        format_fields = {
            field_name
            for _, field_name, _, _ in Formatter().parse(definition.template)
            if field_name
        }
        render_vars = {name: variables.get(name, "") for name in format_fields}
        content = definition.template.format(**render_vars)
    else:
        render_vars = {}
        content = definition.template
    return PromptBuildResult(
        stage=definition.stage,
        version=definition.version,
        content=content,
        loaded_variables=tuple(sorted(render_vars)),
        extension_slots=definition.extension_slots,
    )
