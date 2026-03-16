"""Schemas for runtime-manageable AI settings."""

from __future__ import annotations

from typing import Literal

from pydantic import BaseModel, ConfigDict, Field, model_validator


VLMModelFamily = Literal["qwen-vl", "gemini", "gpt-4o", "qwen2.5-vl"]


class SettingsModel(BaseModel):
    model_config = ConfigDict(extra="forbid", str_strip_whitespace=True)


class AISettingsResponse(SettingsModel):
    enable_ai_dsl_generate: bool
    ai_dsl_timeout_ms: int = Field(ge=1000)
    ai_dsl_base_url: str = Field(min_length=1, max_length=500)
    ai_dsl_model: str | None = Field(default=None, max_length=200)
    has_ai_dsl_api_key: bool
    enable_ai_visual_locate: bool
    ai_visual_timeout_ms: int = Field(ge=1000)
    ai_visual_failure_threshold: int = Field(ge=1)
    ai_visual_cooldown_seconds: int = Field(ge=1)
    ai_visual_rate_limit_per_minute: int = Field(ge=1)
    vlm_base_url: str = Field(min_length=1, max_length=500)
    vlm_model: str | None = Field(default=None, max_length=200)
    vlm_model_family: VLMModelFamily
    has_vlm_api_key: bool


class AISettingsUpdateRequest(SettingsModel):
    enable_ai_dsl_generate: bool
    ai_dsl_timeout_ms: int = Field(ge=1000)
    ai_dsl_base_url: str = Field(min_length=1, max_length=500)
    ai_dsl_model: str | None = Field(default=None, max_length=200)
    ai_dsl_api_key: str | None = Field(default=None, max_length=1000)
    clear_ai_dsl_api_key: bool = False
    enable_ai_visual_locate: bool
    ai_visual_timeout_ms: int = Field(ge=1000)
    ai_visual_failure_threshold: int = Field(ge=1)
    ai_visual_cooldown_seconds: int = Field(ge=1)
    ai_visual_rate_limit_per_minute: int = Field(ge=1)
    vlm_base_url: str = Field(min_length=1, max_length=500)
    vlm_model: str | None = Field(default=None, max_length=200)
    vlm_model_family: VLMModelFamily
    vlm_api_key: str | None = Field(default=None, max_length=1000)
    clear_vlm_api_key: bool = False

    @model_validator(mode="after")
    def validate_secret_update_flags(self) -> "AISettingsUpdateRequest":
        if self.clear_ai_dsl_api_key and self.ai_dsl_api_key:
            raise ValueError("ai_dsl_api_key 不能和 clear_ai_dsl_api_key 同时提交。")
        if self.clear_vlm_api_key and self.vlm_api_key:
            raise ValueError("vlm_api_key 不能和 clear_vlm_api_key 同时提交。")
        return self
