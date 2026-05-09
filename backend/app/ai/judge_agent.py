"""Judge agent: single-shot LLM call to classify failures and produce conclusions."""

from __future__ import annotations

import json
import logging
from typing import Any
from urllib import request

import httpx

from app.ai.judge_prompts import JUDGE_SYSTEM_PROMPT, build_judge_user_prompt
from app.core.config import Settings, get_settings
from app.schemas.explorer_judge import ExplorerStepEvidence

logger = logging.getLogger(__name__)


def call_judge_llm(
    failure_records: list[ExplorerStepEvidence],
    *,
    case_name: str | None = None,
    dsl_steps_summary: list[dict[str, Any]] | None = None,
    settings: Settings | None = None,
) -> dict[str, Any]:
    """Call the Judge LLM to classify failures. Returns parsed JSON dict.

    Raises RuntimeError if LLM is not configured or call fails.
    """
    if settings is None:
        settings = get_settings()

    api_key = settings.ai_planning_api_key
    base_url = settings.ai_planning_base_url
    model = settings.ai_planning_model

    if not api_key:
        raise RuntimeError("AI_PLANNING_API_KEY not configured, Judge cannot run.")

    user_prompt = build_judge_user_prompt(
        failure_records,
        case_name=case_name,
        dsl_steps_summary=dsl_steps_summary,
    )

    messages = [
        {"role": "system", "content": JUDGE_SYSTEM_PROMPT},
        {"role": "user", "content": user_prompt},
    ]

    payload: dict[str, Any] = {
        "model": model or "gpt-4o",
        "messages": messages,
    }
    if _should_enable_thinking_mode(base_url=base_url, model=model or "gpt-4o"):
        payload["thinking"] = {"type": "enabled"}
        payload["max_tokens"] = 65536
    else:
        payload["temperature"] = 0.0
        payload["response_format"] = {"type": "json_object"}
    endpoint = f"{base_url.rstrip('/')}/chat/completions"
    http_request = request.Request(
        endpoint,
        data=json.dumps(payload).encode("utf-8"),
        headers={
            "Authorization": f"Bearer {api_key}",
            "Content-Type": "application/json",
        },
        method="POST",
    )

    with request.urlopen(http_request, timeout=60) as response:
        raw = json.loads(response.read().decode("utf-8"))

    content = _extract_message_content(raw)
    if not content:
        raise RuntimeError("Judge LLM returned empty response.")

    return parse_judge_response(content)


def parse_judge_response(raw_text: str) -> dict[str, Any]:
    """Parse Judge LLM JSON response. Returns the structured dict."""
    text = raw_text.strip()
    if text.startswith("```"):
        lines = text.split("\n")
        text = "\n".join(lines[1:-1] if lines[-1].strip() == "```" else lines[1:])

    try:
        data = json.loads(text)
    except json.JSONDecodeError as exc:
        logger.error("Judge response is not valid JSON: %s...", text[:200])
        raise RuntimeError(f"Judge response JSON parse error: {exc}") from exc

    # Validate required keys
    if "conclusions" not in data:
        raise RuntimeError("Judge response missing 'conclusions' key.")
    if "aggregate" not in data:
        raise RuntimeError("Judge response missing 'aggregate' key.")

    return data


def _extract_message_content(payload: dict[str, Any]) -> str | None:
    """Extract text content from an OpenAI-compatible chat completion response."""
    try:
        choices = payload.get("choices", [])
        if choices:
            message = choices[0].get("message", {})
            return message.get("content")
    except (KeyError, IndexError, TypeError):
        pass
    return None


def _should_enable_thinking_mode(*, base_url: str, model: str) -> bool:
    normalized_base_url = base_url.strip().casefold()
    normalized_model = model.strip().casefold()
    return (
        "open.bigmodel.cn" in normalized_base_url
        or normalized_model.startswith("glm-")
    )
