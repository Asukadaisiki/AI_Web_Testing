"""Application configuration helpers."""

from __future__ import annotations

from dataclasses import dataclass
from functools import lru_cache
import os


def _get_bool(value: str | None, default: bool) -> bool:
    if value is None:
        return default

    normalized = value.strip().lower()
    return normalized in {"1", "true", "yes", "on"}


@dataclass(frozen=True)
class Settings:
    app_name: str = "AI Web Testing Backend"
    app_version: str = "0.1.0"
    app_env: str = "development"
    debug: bool = True
    api_v1_prefix: str = "/api/v1"


@lru_cache
def get_settings() -> Settings:
    return Settings(
        app_env=os.getenv("APP_ENV", "development"),
        debug=_get_bool(os.getenv("APP_DEBUG"), default=True),
    )
