"""Centralized logging configuration for the backend."""

from __future__ import annotations

import logging
import os
import sys
from pathlib import Path

LOG_FORMAT = "%(asctime)s | %(levelname)-8s | %(name)s | %(message)s"
DATE_FORMAT = "%Y-%m-%d %H:%M:%S"

# Third-party loggers that should stay quiet
_THIRD_PARTY_LOGGERS = [
    "uvicorn.access",
    "httpx",
    "httpcore",
    "sqlalchemy.engine",
    "aiosqlite",
    "multipart",
]


def setup_logging(level: str | None = None) -> None:
    """Configure structured logging for the entire application.

    Args:
        level: Override log level. Falls back to env var ``LOG_LEVEL``,
               then defaults to ``INFO``.
    """
    effective_level = level or os.getenv("LOG_LEVEL", "INFO").upper()

    formatter = logging.Formatter(LOG_FORMAT, datefmt=DATE_FORMAT)

    # Console handler
    console = logging.StreamHandler(sys.stdout)
    console.setFormatter(formatter)

    # File handler - output to backend root directory
    backend_root = Path(__file__).resolve().parents[2]
    log_file = backend_root / "backend.log"
    file_handler = logging.FileHandler(log_file, encoding="utf-8")
    file_handler.setFormatter(formatter)

    # Root logger
    root = logging.getLogger()
    root.handlers.clear()
    root.addHandler(console)
    root.addHandler(file_handler)
    root.setLevel(logging.WARNING)

    # Application loggers
    app_logger = logging.getLogger("app")
    app_logger.setLevel(getattr(logging, effective_level, logging.INFO))
    app_logger.handlers.clear()
    app_logger.addHandler(console)
    app_logger.addHandler(file_handler)
    app_logger.propagate = False

    # Quiet third-party loggers
    for name in _THIRD_PARTY_LOGGERS:
        third_party = logging.getLogger(name)
        third_party.setLevel(logging.WARNING)
        third_party.propagate = False


def get_uvicorn_log_config() -> dict:
    """Return a uvicorn-compatible log config dict that matches our format."""
    return {
        "version": 1,
        "disable_existing_loggers": False,
        "formatters": {
            "default": {
                "format": LOG_FORMAT,
                "datefmt": DATE_FORMAT,
            },
        },
        "handlers": {
            "default": {
                "class": "logging.StreamHandler",
                "formatter": "default",
                "stream": "ext://sys.stdout",
            },
        },
        "loggers": {
            "uvicorn": {
                "handlers": ["default"],
                "level": "INFO",
                "propagate": False,
            },
            "uvicorn.error": {
                "handlers": ["default"],
                "level": "INFO",
                "propagate": False,
            },
            "uvicorn.access": {
                "handlers": ["default"],
                "level": "WARNING",
                "propagate": False,
            },
        },
    }
