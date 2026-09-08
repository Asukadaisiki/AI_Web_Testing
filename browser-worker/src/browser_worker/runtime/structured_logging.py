"""Structured logging helpers for browser execution and locator fallback."""

from __future__ import annotations

import json
import logging
from datetime import UTC, datetime
from typing import Any

CATEGORY_DSL_EXECUTION = "dsl_execution"
CATEGORY_LOCATOR_FALLBACK = "locator_fallback"


class StructuredJsonFormatter(logging.Formatter):
    """Serialize application logs to one JSON object per line."""

    def format(self, record: logging.LogRecord) -> str:
        envelope: dict[str, Any] = {
            "timestamp": datetime.fromtimestamp(record.created, tz=UTC).isoformat(),
            "level": record.levelname,
            "logger": record.name,
            "category": getattr(record, "category", None),
            "event_type": getattr(record, "event_type", None),
            "message": getattr(record, "message_override", None) or record.getMessage(),
        }
        data = getattr(record, "data", None)
        if data is not None:
            envelope["data"] = _truncate_data(data)
        execution_id = getattr(record, "execution_id", None)
        if execution_id is not None:
            envelope["trace"] = {"execution_id": execution_id}
        return json.dumps(envelope, ensure_ascii=False, separators=(",", ":"))


def _truncate_data(data: Any, max_field_len: int = 4096) -> Any:
    if not isinstance(data, dict):
        return data
    return {
        key: (
            value[:max_field_len] + "...[truncated]"
            if isinstance(value, str) and len(value) > max_field_len
            else _truncate_data(value, max_field_len)
            if isinstance(value, dict)
            else value
        )
        for key, value in data.items()
    }


class CategoryLogger:
    """Thin wrapper around logging.Logger with category convenience methods."""

    def __init__(self, logger: logging.Logger) -> None:
        self._logger = logger

    def dsl_execution(
        self,
        event_type: str,
        *,
        data: dict[str, Any] | None = None,
        message: str = "",
        execution_id: int | None = None,
        level: int = logging.INFO,
    ) -> None:
        self._log(level, CATEGORY_DSL_EXECUTION, event_type, message, data, execution_id=execution_id)

    def locator_fallback(
        self,
        event_type: str,
        *,
        data: dict[str, Any] | None = None,
        message: str = "",
        execution_id: int | None = None,
        level: int = logging.INFO,
    ) -> None:
        self._log(level, CATEGORY_LOCATOR_FALLBACK, event_type, message, data, execution_id=execution_id)

    def _log(
        self,
        level: int,
        category: str,
        event_type: str,
        message: str,
        data: dict[str, Any] | None,
        *,
        execution_id: int | None = None,
    ) -> None:
        if not self._logger.isEnabledFor(level):
            return
        extra = {
            "category": category,
            "event_type": event_type,
            "data": data,
            "message_override": message or None,
        }
        if execution_id is not None:
            extra["execution_id"] = execution_id
        self._logger._log(level, message or event_type, (), extra=extra)


def get_structured_logger(name: str) -> CategoryLogger:
    """Get a CategoryLogger for the given module name."""
    return CategoryLogger(logging.getLogger(name))
