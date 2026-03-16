"""AI-assisted DSL generation helpers."""

from __future__ import annotations

import json
from typing import Any
from urllib import request

from pydantic import ValidationError

from app.core.config import get_settings
from app.schemas.dsl import DSLCase


class DslGenerationError(RuntimeError):
    """Raised when the model response cannot be converted into a valid DSL case."""


class DslGenerationConfigError(DslGenerationError):
    """Raised when AI DSL generation is disabled or missing required configuration."""


def build_generation_messages(
    *,
    prompt: str,
    base_url: str | None,
    supported_actions: list[str],
) -> list[dict[str, Any]]:
    allowed_actions = ", ".join(supported_actions)
    system_prompt = (
        "You generate structured web testing DSL in JSON only. "
        f"Allowed actions: {allowed_actions}. "
        "Do not use any other action names. "
        "Return exactly one JSON object with keys: "
        "name, description, base_url, input_contract, output_contract, steps. "
        "Use semantic Chinese target descriptions when selectors are not explicitly provided. "
        "Do not include markdown fences or explanations."
    )

    user_lines = [
        "请根据下面的测试需求生成可编辑 DSL 草案。",
        f"测试需求：{prompt.strip()}",
        f"建议 Base URL：{base_url.strip() if base_url else '未提供'}",
        "要求：",
        "- steps 必须是数组，且每个 step 只能使用允许的 action。",
        "- input_contract 和 output_contract 如无需要，返回空数组。",
        "- 如果是相对路径跳转，优先保留为相对路径，并在 base_url 中提供站点地址。",
    ]
    return [
        {"role": "system", "content": system_prompt},
        {"role": "user", "content": "\n".join(user_lines)},
    ]


def generate_case_draft(
    *,
    prompt: str,
    base_url: str | None,
    supported_actions: list[str],
) -> tuple[DSLCase, list[str]]:
    settings = get_settings()
    if not settings.enable_ai_dsl_generate:
        raise DslGenerationConfigError(
            "AI DSL 生成功能未开启。请设置 ENABLE_AI_DSL_GENERATE=true 并配置 AI_DSL_API_KEY、AI_DSL_MODEL。"
        )
    if not settings.ai_dsl_api_key or not settings.ai_dsl_model:
        raise DslGenerationConfigError(
            "AI DSL 生成配置不完整。请提供 AI_DSL_API_KEY 与 AI_DSL_MODEL。"
        )

    response_text = _call_llm(
        messages=build_generation_messages(
            prompt=prompt,
            base_url=base_url,
            supported_actions=supported_actions,
        ),
        api_key=settings.ai_dsl_api_key,
        model=settings.ai_dsl_model,
        base_url=settings.ai_dsl_base_url,
        timeout_seconds=max(1.0, settings.ai_dsl_timeout_ms / 1000),
    )

    try:
        raw_case = json.loads(_extract_json_object(response_text))
    except json.JSONDecodeError as exc:
        raise DslGenerationError("AI 返回了无法解析的 DSL JSON。") from exc

    warnings: list[str] = []
    normalized_base_url = base_url.strip() if base_url else None
    if normalized_base_url and not raw_case.get("base_url"):
        raw_case["base_url"] = normalized_base_url
        warnings.append("AI 草案未提供 base_url，已回填请求中的 Base URL。")

    try:
        case = DSLCase.model_validate(raw_case)
    except ValidationError as exc:
        raise DslGenerationError(_format_validation_error(exc)) from exc

    return case, warnings


def _call_llm(
    *,
    messages: list[dict[str, Any]],
    api_key: str,
    model: str,
    base_url: str,
    timeout_seconds: float,
) -> str:
    payload = {
        "model": model,
        "messages": messages,
        "response_format": {"type": "json_object"},
    }
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
    with request.urlopen(http_request, timeout=timeout_seconds) as response:
        raw_payload = json.loads(response.read().decode("utf-8"))

    return _extract_message_content(raw_payload)


def _extract_message_content(payload: dict[str, Any]) -> str:
    content = payload.get("choices", [{}])[0].get("message", {}).get("content", "")
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        text_parts: list[str] = []
        for item in content:
            if isinstance(item, dict) and isinstance(item.get("text"), str):
                text_parts.append(item["text"])
        return "\n".join(text_parts)
    return ""


def _extract_json_object(response_text: str) -> str:
    stripped = response_text.strip()
    if not stripped:
        return stripped

    if stripped.startswith("```"):
        first_newline = stripped.find("\n")
        if first_newline != -1:
            end_fence = stripped.rfind("```", first_newline)
            if end_fence > first_newline:
                stripped = stripped[first_newline + 1 : end_fence].strip()

    in_string = False
    escape_next = False
    depth = 0
    start = -1
    for index, char in enumerate(stripped):
        if escape_next:
            escape_next = False
            continue
        if in_string:
            if char == "\\":
                escape_next = True
            elif char == '"':
                in_string = False
            continue
        if char == '"' and depth > 0:
            in_string = True
        elif char == "{":
            if depth == 0:
                start = index
            depth += 1
        elif char == "}":
            depth -= 1
            if depth == 0 and start != -1:
                return stripped[start : index + 1]

    return stripped


def _format_validation_error(exc: ValidationError) -> str:
    first_error = exc.errors(include_url=False)[0]
    location = ".".join(str(item) for item in first_error.get("loc", ()))
    message = first_error.get("msg", "unknown validation error")
    return f"AI 返回的 DSL 不符合当前 schema：{location} {message}".strip()

