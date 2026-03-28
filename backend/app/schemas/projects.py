"""Schemas for project selection surfaces."""

from __future__ import annotations

from pydantic import Field

from app.schemas.dsl import DSLModel


class ProjectSummary(DSLModel):
    id: int = Field(ge=1)
    name: str = Field(min_length=1, max_length=200)
    description: str | None = Field(default=None, max_length=1000)
