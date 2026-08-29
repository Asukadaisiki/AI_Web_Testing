"""Cache lookup helpers for persisted planning tool results."""

from __future__ import annotations

from datetime import UTC, datetime, timedelta
from urllib.parse import parse_qs, urlencode, urlparse, urlunparse

from sqlalchemy import select
from sqlalchemy.orm import Session

from app.models.ai_planning_tool_result import AIPlanningToolResult
from app.schemas.ai_planning import AIPlanningToolCall


_TRACKING_PARAMS = {
    "utm_source",
    "utm_medium",
    "utm_campaign",
    "utm_content",
    "utm_term",
    "_t",
    "ref",
    "fbclid",
    "gclid",
}


def normalize_cache_url(raw_url: str) -> str:
    """Normalize a URL for planning-tool cache keys."""
    parsed = urlparse(raw_url)
    query_params = parse_qs(parsed.query)
    cleaned_params = {
        key: value
        for key, value in query_params.items()
        if key.lower() not in _TRACKING_PARAMS
    }
    query = urlencode(cleaned_params, doseq=True)
    path = parsed.path.rstrip("/") or "/"
    return urlunparse((parsed.scheme, parsed.netloc.lower(), path, "", query, ""))


def lookup_tool_cache(
    db_session: Session,
    key: tuple,
    *,
    ttl_hours: int = 4,
) -> dict | None:
    """Look up a cached explore result by composite key."""
    tool_name, session_id, normalized_url, *_ = key
    cutoff = datetime.now(UTC) - timedelta(hours=ttl_hours)
    records = db_session.scalars(
        select(AIPlanningToolResult)
        .where(
            AIPlanningToolResult.session_id == session_id,
            AIPlanningToolResult.tool_name == tool_name,
            AIPlanningToolResult.created_at >= cutoff,
        )
        .order_by(AIPlanningToolResult.id.desc())
    ).all()

    for record in records:
        raw = record.raw_result_json
        if isinstance(raw, dict) and normalize_cache_url(raw.get("url", "")) == normalized_url:
            return raw
    return None


def extract_raw_page_results(tool_calls: list[AIPlanningToolCall]) -> list[dict]:
    """Extract raw page results from the most recent exploration tool call."""
    for call in reversed(tool_calls):
        if not isinstance(call.result, dict):
            continue
        if call.tool == "explore_flow":
            pages = call.result.get("pages")
            if isinstance(pages, list):
                return pages
        if call.tool == "explore_page":
            nodes = call.result.get("a11y_nodes", call.result.get("elements"))
            if isinstance(nodes, list):
                return [{"url": call.result.get("url", ""), "a11y_nodes": nodes}]
    return []
