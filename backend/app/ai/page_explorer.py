"""Playwright-based page exploration and browser session management."""
from __future__ import annotations

import json
import logging
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any

logger = logging.getLogger(__name__)

STALE_THRESHOLD_HOURS = 24


def get_storage_state_path(base_dir: Path, *, project_id: int) -> tuple[Path, Path]:
    """Return (state_path, meta_path) for a given project."""
    return base_dir / f"{project_id}.json", base_dir / f"{project_id}.meta.json"


def save_storage_state(
    base_dir: Path,
    *,
    project_id: int,
    state: dict[str, Any],
    source_url: str,
) -> None:
    """Persist Playwright storage_state and metadata for a project."""
    base_dir.mkdir(parents=True, exist_ok=True)
    state_path, meta_path = get_storage_state_path(base_dir, project_id=project_id)
    state_path.write_text(json.dumps(state, ensure_ascii=False), encoding="utf-8")
    meta = {"source_url": source_url, "saved_at": datetime.now(UTC).isoformat()}
    meta_path.write_text(json.dumps(meta, ensure_ascii=False), encoding="utf-8")
    logger.info(
        "Saved storage state for project_id=%s source_url=%s", project_id, source_url
    )


def load_storage_state_meta(
    base_dir: Path, *, project_id: int
) -> dict[str, Any] | None:
    """Load storage state metadata, returning None if missing or corrupt."""
    _, meta_path = get_storage_state_path(base_dir, project_id=project_id)
    if not meta_path.exists():
        return None
    try:
        return json.loads(meta_path.read_text(encoding="utf-8"))
    except (json.JSONDecodeError, OSError):
        return None


def is_storage_state_stale(meta: dict[str, Any]) -> bool:
    """Check if storage state metadata indicates it's older than STALE_THRESHOLD_HOURS."""
    saved_at_str = meta.get("saved_at")
    if not saved_at_str:
        return True
    try:
        saved_at = datetime.fromisoformat(saved_at_str)
        return (datetime.now(UTC) - saved_at) > timedelta(hours=STALE_THRESHOLD_HOURS)
    except (ValueError, TypeError):
        return True


def format_elements_for_prompt(elements: list[dict[str, Any]]) -> str:
    """Format collected DOM elements into a concise text block for AI prompt injection."""
    lines: list[str] = []
    for element in elements:
        if not element.get("visible", True):
            continue
        tag = element.get("tag", "unknown")
        elem_id = element.get("id") or ""
        parts: list[str] = [f"{tag}"]
        if elem_id:
            parts[0] = f"{tag}#{elem_id}"
        for attr in ("aria_label", "placeholder", "text", "role"):
            value = element.get(attr)
            if value:
                attr_display = attr.replace("_", "-")
                parts.append(f"[{attr_display}='{value}']")
        lines.append(" ".join(parts))
    return "\n".join(lines)


__all__ = [
    "format_elements_for_prompt",
    "get_storage_state_path",
    "is_storage_state_stale",
    "load_storage_state_meta",
    "save_storage_state",
]
