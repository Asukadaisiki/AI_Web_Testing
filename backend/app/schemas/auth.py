"""Schemas for local cookie-session authentication."""

from __future__ import annotations

from pydantic import Field

from app.schemas.dsl import DSLModel


class LoginRequest(DSLModel):
    email: str = Field(min_length=1, max_length=255)
    password: str = Field(min_length=1, max_length=200)


class CurrentUserResponse(DSLModel):
    id: int = Field(ge=1)
    email: str = Field(min_length=1, max_length=255)
    display_name: str = Field(min_length=1, max_length=100)


class LogoutResponse(DSLModel):
    success: bool = True
