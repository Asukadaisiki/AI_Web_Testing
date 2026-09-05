"""Contracts exposed by the Python browser capability worker."""

from __future__ import annotations

from typing import Any, Literal

from pydantic import Field

from app.schemas.dsl import DSLModel


BrowserCapabilityName = Literal[
    "explore_page",
    "explore_flow",
    "validate_page_elements",
]


class BrowserCapabilityRequest(DSLModel):
    project_id: int = Field(ge=1)
    conversation_id: str = Field(min_length=1, max_length=100)
    arguments: dict[str, Any] = Field(default_factory=dict)


class BrowserCapabilityResponse(DSLModel):
    result: dict[str, Any]
