"""Contracts for stateless browser execution RPCs."""

from __future__ import annotations

from typing import Any

from pydantic import Field

from app.schemas.dsl import DSLModel


class BrowserExecutionRequest(DSLModel):
    execution_id: int = Field(ge=1)
    dsl_case: dict[str, Any]
    base_url: str | None = Field(default=None, min_length=1, max_length=500)
    input_values: dict[str, str] = Field(default_factory=dict)


class BrowserExecutionResponse(DSLModel):
    status: str
    error_message: str | None = None
    report: dict[str, Any]
    failure_signal: dict[str, Any] | None = None
