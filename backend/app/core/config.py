"""Application configuration helpers."""

from __future__ import annotations

from dataclasses import dataclass
from functools import lru_cache
import os
from pathlib import Path


ENV_FILE_PATH = Path(__file__).resolve().parents[2] / ".env"


def _load_env_file() -> None:
    if not ENV_FILE_PATH.exists():
        return

    for raw_line in ENV_FILE_PATH.read_text(encoding="utf-8").splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue

        key, value = line.split("=", 1)
        os.environ.setdefault(key.strip(), value.strip())


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
    database_url: str = "sqlite:///./app.db"
    database_echo: bool = False


@lru_cache
def get_settings() -> Settings:
    _load_env_file()
    return Settings(
        app_env=os.getenv("APP_ENV", "development"),
        debug=_get_bool(os.getenv("APP_DEBUG"), default=True),
        database_url=os.getenv("DATABASE_URL", "sqlite:///./app.db"),
        database_echo=_get_bool(os.getenv("DATABASE_ECHO"), default=False),
    )
