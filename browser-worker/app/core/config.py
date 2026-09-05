"""Application configuration helpers."""

from __future__ import annotations

from dataclasses import dataclass, field
from functools import lru_cache
import logging
import os
from pathlib import Path


ENV_FILE_PATH = Path(__file__).resolve().parents[2] / ".env"
logger = logging.getLogger(__name__)


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


def _get_int(value: str | None, default: int) -> int:
    if value is None:
        return default
    try:
        return int(value.strip())
    except ValueError:
        logger.warning("Invalid integer config value=%r, fallback=%s", value, default)
        return default


def _parse_comma_list(value: str | None) -> list[str]:
    if not value or not value.strip():
        return []
    return [item.strip() for item in value.split(",") if item.strip()]


@dataclass(frozen=True)
class Settings:
    app_name: str = "AI Web Testing Browser Worker"
    app_version: str = "0.1.0"
    app_env: str = "development"
    debug: bool = True
    api_v1_prefix: str = "/api/v1"
    database_url: str = "sqlite:///./app.db"
    database_echo: bool = False
    execution_base_url: str | None = None
    enable_ai_visual_locate: bool = False
    ai_visual_timeout_ms: int = 600000
    ai_visual_failure_threshold: int = 3
    ai_visual_cooldown_seconds: int = 60
    ai_visual_rate_limit_per_minute: int = 10
    vlm_api_key: str | None = None
    vlm_base_url: str = "https://api.openai.com/v1"
    vlm_model: str | None = None
    vlm_model_family: str = "gpt-4o"
    vlm_fallback_models: list[str] = field(default_factory=lambda: ["glm-4.6v-flash", "glm-4.6v", "glm-4v-flash"])
    storage_state_dir: str = "storage_states"
    enable_vlm_page_annotation: bool = True
    cors_allow_origins: list[str] = field(default_factory=lambda: ["http://localhost:5173", "http://127.0.0.1:5173"])
    rate_limit_max_requests: int = 10000
    rate_limit_window_seconds: int = 60


@lru_cache
def get_settings() -> Settings:
    _load_env_file()
    return Settings(
        app_env=os.getenv("APP_ENV", "development"),
        debug=_get_bool(os.getenv("APP_DEBUG"), default=True),
        database_url=os.getenv("DATABASE_URL", "sqlite:///./app.db"),
        database_echo=_get_bool(os.getenv("DATABASE_ECHO"), default=False),
        execution_base_url=os.getenv("EXECUTION_BASE_URL") or None,
        enable_ai_visual_locate=_get_bool(os.getenv("ENABLE_AI_VISUAL_LOCATE"), default=False),
        ai_visual_timeout_ms=max(10000, _get_int(os.getenv("AI_VISUAL_TIMEOUT_MS"), default=600000)),
        ai_visual_failure_threshold=max(1, _get_int(os.getenv("AI_VISUAL_FAILURE_THRESHOLD"), default=3)),
        ai_visual_cooldown_seconds=max(1, _get_int(os.getenv("AI_VISUAL_COOLDOWN_SECONDS"), default=60)),
        ai_visual_rate_limit_per_minute=max(1, _get_int(os.getenv("AI_VISUAL_RATE_LIMIT_PER_MINUTE"), default=10)),
        vlm_api_key=os.getenv("VLM_API_KEY") or None,
        vlm_base_url=os.getenv("VLM_BASE_URL", "https://api.openai.com/v1"),
        vlm_model=os.getenv("VLM_MODEL") or None,
        vlm_model_family=os.getenv("VLM_MODEL_FAMILY", "gpt-4o"),
        vlm_fallback_models=_parse_comma_list(os.getenv("VLM_FALLBACK_MODELS")) or ["glm-4.6v-flash", "glm-4.6v", "glm-4.6v-flashx"],
        storage_state_dir=os.getenv("STORAGE_STATE_DIR", "storage_states").strip(),
        enable_vlm_page_annotation=_get_bool(os.getenv("ENABLE_VLM_PAGE_ANNOTATION"), default=True),
        cors_allow_origins=_parse_comma_list(os.getenv("CORS_ALLOW_ORIGINS")) or ["http://localhost:5173", "http://127.0.0.1:5173"],
        rate_limit_max_requests=max(10, _get_int(os.getenv("RATE_LIMIT_MAX_REQUESTS"), default=10000)),
        rate_limit_window_seconds=max(10, _get_int(os.getenv("RATE_LIMIT_WINDOW_SECONDS"), default=60)),
    )
