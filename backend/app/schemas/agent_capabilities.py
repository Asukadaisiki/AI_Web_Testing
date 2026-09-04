"""Contracts for non-browser capabilities used by AgentCore."""

from __future__ import annotations

from typing import Any

from pydantic import Field

from app.schemas.dsl import DSLModel


class AgentCapabilityRequest(DSLModel):
    project_id: int = Field(ge=1)
    conversation_id: str = Field(min_length=1, max_length=100)
    arguments: dict[str, Any] = Field(default_factory=dict)


class AgentCapabilityResponse(DSLModel):
    result: dict[str, Any]
