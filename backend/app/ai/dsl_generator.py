"""AI-assisted DSL generation helpers."""

from __future__ import annotations

import json
from typing import Any
from urllib import request

from pydantic import TypeAdapter, ValidationError

from app.core.config import get_settings
from app.schemas.dsl import (
    AssertTextStep,
    AssertUrlContainsStep,
    ClickStep,
    DSLCase,
    DSLCaseInputContract,
    DSLCaseOutputContract,
    DSLStep,
    GenerateDslBaseUrlSource,
    GenerateDslMeta,
    GenerateDslMode,
    GenerateDslRequest,
    GotoStep,
    InputStep,
    WaitForStep,
)


class DslGenerationError(RuntimeError):
    """Raised when the model response cannot be converted into a valid DSL case."""


class DslGenerationConfigError(DslGenerationError):
    """Raised when AI DSL generation is disabled or missing required configuration."""


_STEP_ADAPTER = TypeAdapter(DSLStep)
_INPUT_CONTRACT_ADAPTER = TypeAdapter(DSLCaseInputContract)
_OUTPUT_CONTRACT_ADAPTER = TypeAdapter(DSLCaseOutputContract)
_ACTION_ALIASES = {
    "open": "goto",
    "navigate": "goto",
    "visit": "goto",
    "tap": "click",
    "press": "click",
    "fill": "input",
    "enter": "input",
    "wait": "wait_for",
    "wait_for_element": "wait_for",
    "assert_contains_text": "assert_text",
    "assert_text_contains": "assert_text",
    "assert_url": "assert_url_contains",
    "assert_url_has": "assert_url_contains",
    "assert_path_contains": "assert_url_contains",
}
_STEP_MODELS = {
    "goto": GotoStep,
    "click": ClickStep,
    "input": InputStep,
    "wait_for": WaitForStep,
    "assert_text": AssertTextStep,
    "assert_url_contains": AssertUrlContainsStep,
}


def build_generation_messages(
    *,
    payload: GenerateDslRequest,
    generation_mode: GenerateDslMode,
    supported_actions: list[str],
) -> list[dict[str, Any]]:
    allowed_actions = ", ".join(supported_actions)
    system_lines = [
        "You generate structured web testing DSL in JSON only.",
        f"Allowed actions: {allowed_actions}.",
        "Do not use any other action names.",
        "Return exactly one JSON object with keys:",
        "name, description, base_url, input_contract, output_contract, steps.",
        "Use semantic Chinese target descriptions when selectors are not explicitly provided.",
        "Do not include markdown fences or explanations.",
    ]
    if generation_mode == "strict_steps_only":
        system_lines.append("In strict_steps_only mode, prioritize returning a high-quality steps array.")
    if payload.import_mode == "contracts_only":
        system_lines.append("When import_mode is contracts_only, include useful input/output contracts when possible.")
    if payload.preserve_contracts:
        system_lines.append("If current contracts are provided, keep them stable unless the prompt explicitly asks to change them.")

    user_lines = [
        "请根据下面的测试需求生成可编辑 DSL 草案。",
        f"测试需求：{payload.prompt.strip()}",
        f"生成模式：{generation_mode}",
        f"预期导入方式：{payload.import_mode}",
        f"建议 Base URL：{payload.base_url.strip() if payload.base_url else '未提供'}",
        f"是否保留当前契约：{'是' if payload.preserve_contracts else '否'}",
        "要求：",
        "- steps 必须是数组，且每个 step 只能使用允许的 action。",
        "- input_contract 和 output_contract 如无需要，返回空数组。",
        "- 如果是相对路径跳转，优先保留为相对路径，并在 base_url 中提供站点地址。",
        "- 如果提供了当前 DSL 或当前 steps，请把它们视为改写上下文，而不是忽略。",
    ]
    if payload.current_case is not None:
        user_lines.extend(
            [
                "当前 DSL：",
                json.dumps(payload.current_case.model_dump(mode="json"), ensure_ascii=False, indent=2),
            ]
        )
    elif payload.preserve_contracts:
        current_input_contract, current_output_contract = _resolve_current_contracts(payload)
        if current_input_contract or current_output_contract:
            user_lines.extend(
                [
                    "当前契约：",
                    json.dumps(
                        {
                            "input_contract": [contract.model_dump(mode="json") for contract in current_input_contract],
                            "output_contract": [contract.model_dump(mode="json") for contract in current_output_contract],
                        },
                        ensure_ascii=False,
                        indent=2,
                    ),
                ]
            )
    if payload.current_steps is not None:
        user_lines.extend(
            [
                "当前步骤：",
                json.dumps(
                    [
                        step.model_dump(mode="json") if hasattr(step, "model_dump") else step
                        for step in payload.current_steps
                    ],
                    ensure_ascii=False,
                    indent=2,
                ),
            ]
        )
    return [
        {"role": "system", "content": " ".join(system_lines)},
        {"role": "user", "content": "\n".join(user_lines)},
    ]


def generate_case_draft(
    *,
    payload: GenerateDslRequest,
    supported_actions: list[str],
) -> tuple[DSLCase, list[str], list[str], GenerateDslMeta]:
    settings = get_settings()
    resolved_generation_mode = resolve_generation_mode(payload.generation_mode, settings=settings)
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
            payload=payload,
            generation_mode=resolved_generation_mode,
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

    if not isinstance(raw_case, dict):
        raise DslGenerationError("AI 返回的 DSL 根对象必须是 JSON object。")

    normalized_case, warnings, normalization_notes, generation_meta = _normalize_generated_case(
        raw_case=raw_case,
        payload=payload,
        generation_mode=resolved_generation_mode,
        model_name=settings.ai_dsl_model,
        allow_auto_repair=settings.ai_dsl_allow_auto_repair,
    )

    try:
        case = DSLCase.model_validate(normalized_case)
    except ValidationError as exc:
        raise DslGenerationError(_format_validation_error(exc)) from exc

    return case, warnings, normalization_notes, generation_meta


def _normalize_generated_case(
    *,
    raw_case: dict[str, Any],
    payload: GenerateDslRequest,
    generation_mode: GenerateDslMode,
    model_name: str,
    allow_auto_repair: bool,
) -> tuple[dict[str, Any], list[str], list[str], GenerateDslMeta]:
    warnings: list[str] = []
    normalization_notes: list[str] = []
    removed_invalid_steps = 0
    removed_invalid_contracts = 0
    repaired_invalid_actions = 0

    current_case = payload.current_case
    current_input_contract, current_output_contract = _resolve_current_contracts(payload)
    used_current_case_context = current_case is not None
    used_current_steps_context = payload.current_steps is not None

    normalized_name = _normalize_string(raw_case.get("name"))
    normalized_description = _normalize_optional_string(raw_case.get("description"))

    if generation_mode == "strict_steps_only" and current_case is not None:
        normalized_name = current_case.name
        normalized_description = current_case.description
        normalization_notes.append("strict_steps_only 模式下沿用了当前 DSL 的名称与描述。")
    elif not normalized_name:
        normalized_name = current_case.name if current_case is not None else "AI 生成用例"
        warnings.append("AI 草案未提供有效 name，已使用当前上下文中的名称或默认名称。")

    base_url_value, base_url_source, base_url_backfilled = _resolve_base_url(
        raw_case=raw_case,
        payload=payload,
        warnings=warnings,
        normalization_notes=normalization_notes,
    )

    input_contract, input_removed = _normalize_contracts(
        raw_contracts=raw_case.get("input_contract"),
        adapter=_INPUT_CONTRACT_ADAPTER,
        label="输入契约",
        allow_auto_repair=allow_auto_repair,
        warnings=warnings,
    )
    output_contract, output_removed = _normalize_contracts(
        raw_contracts=raw_case.get("output_contract"),
        adapter=_OUTPUT_CONTRACT_ADAPTER,
        label="输出契约",
        allow_auto_repair=allow_auto_repair,
        warnings=warnings,
    )
    removed_invalid_contracts += input_removed + output_removed

    preserve_contracts_applied = False
    if payload.preserve_contracts and (not input_contract and not output_contract):
        input_contract = current_input_contract
        output_contract = current_output_contract
        preserve_contracts_applied = bool(input_contract or output_contract)
        if preserve_contracts_applied:
            normalization_notes.append("AI 草案未提供有效契约，已沿用当前 DSL 的输入/输出契约。")

    steps, removed_invalid_steps, repaired_invalid_actions = _normalize_steps(
        raw_steps=raw_case.get("steps"),
        allow_auto_repair=allow_auto_repair,
        warnings=warnings,
        normalization_notes=normalization_notes,
    )

    if not steps:
        raise DslGenerationError("AI 生成草案中没有可导入的有效 steps。")

    generation_meta = GenerateDslMeta(
        model=model_name,
        generation_mode=generation_mode,
        import_mode=payload.import_mode,
        base_url_source=base_url_source,
        base_url_backfilled=base_url_backfilled,
        repaired_invalid_actions=repaired_invalid_actions,
        removed_invalid_steps=removed_invalid_steps,
        removed_invalid_contracts=removed_invalid_contracts,
        preserve_contracts_applied=preserve_contracts_applied,
        used_current_case_context=used_current_case_context,
        used_current_steps_context=used_current_steps_context,
    )

    return {
        "name": normalized_name,
        "description": normalized_description,
        "base_url": base_url_value,
        "input_contract": input_contract,
        "output_contract": output_contract,
        "steps": steps,
    }, warnings, normalization_notes, generation_meta


def _normalize_contracts(
    *,
    raw_contracts: Any,
    adapter: TypeAdapter[Any],
    label: str,
    allow_auto_repair: bool,
    warnings: list[str],
) -> tuple[list[Any], int]:
    if raw_contracts is None:
        return [], 0
    if not isinstance(raw_contracts, list):
        warnings.append(f"AI 草案中的{label}不是数组，已忽略该字段。")
        return [], 1

    normalized_contracts: list[Any] = []
    removed_count = 0
    for index, raw_contract in enumerate(raw_contracts, start=1):
        if not isinstance(raw_contract, dict):
            removed_count += 1
            warnings.append(f"{label} #{index} 不是对象，已忽略。")
            continue
        try:
            normalized_contracts.append(adapter.validate_python(raw_contract))
        except ValidationError:
            removed_count += 1
            if allow_auto_repair:
                warnings.append(f"{label} #{index} 结构非法，已忽略。")
            else:
                raise DslGenerationError(f"AI 草案中的{label} #{index} 不符合当前 schema。")
    return normalized_contracts, removed_count


def _normalize_steps(
    *,
    raw_steps: Any,
    allow_auto_repair: bool,
    warnings: list[str],
    normalization_notes: list[str],
) -> tuple[list[DSLStep], int, int]:
    if not isinstance(raw_steps, list):
        raise DslGenerationError("AI 草案中的 steps 必须是数组。")

    normalized_steps: list[DSLStep] = []
    removed_invalid_steps = 0
    repaired_invalid_actions = 0
    for index, raw_step in enumerate(raw_steps, start=1):
        if not isinstance(raw_step, dict):
            removed_invalid_steps += 1
            warnings.append(f"步骤 #{index} 不是对象，已忽略。")
            continue

        try:
            normalized_step, action_repaired = _normalize_single_step(
                raw_step=raw_step,
                index=index,
                allow_auto_repair=allow_auto_repair,
                normalization_notes=normalization_notes,
            )
        except DslGenerationError:
            if not allow_auto_repair:
                raise
            normalized_step = None
            action_repaired = 0

        if normalized_step is None:
            removed_invalid_steps += 1
            warnings.append(f"步骤 #{index} 无法修正为合法 DSL，已忽略。")
            continue

        repaired_invalid_actions += action_repaired
        normalized_steps.append(normalized_step)

    return normalized_steps, removed_invalid_steps, repaired_invalid_actions


def _normalize_single_step(
    *,
    raw_step: dict[str, Any],
    index: int,
    allow_auto_repair: bool,
    normalization_notes: list[str],
) -> tuple[DSLStep | None, int]:
    action_value = raw_step.get("action")
    if not isinstance(action_value, str) or not action_value.strip():
        if allow_auto_repair:
            return None, 0
        raise DslGenerationError(f"步骤 #{index} 缺少合法 action。")

    normalized_action = action_value.strip()
    repaired_invalid_actions = 0
    if normalized_action not in _STEP_MODELS:
        mapped_action = _ACTION_ALIASES.get(normalized_action)
        if mapped_action is None:
            if allow_auto_repair:
                return None, 0
            raise DslGenerationError(f"步骤 #{index} 使用了不支持的 action: {normalized_action}")
        repaired_invalid_actions = 1
        normalization_notes.append(
            f"步骤 #{index} 的 action 已从 {normalized_action} 自动修正为 {mapped_action}。"
        )
        normalized_action = mapped_action

    repaired_step = dict(raw_step)
    repaired_step["action"] = normalized_action
    for field_name in ("target", "value"):
        if field_name in repaired_step and repaired_step[field_name] is not None and not isinstance(repaired_step[field_name], str):
            if not allow_auto_repair:
                raise DslGenerationError(f"步骤 #{index} 的 {field_name} 字段类型非法。")
            repaired_step[field_name] = str(repaired_step[field_name])
            normalization_notes.append(f"步骤 #{index} 的 {field_name} 已自动转换为字符串。")

    if normalized_action == "wait_for" and "timeout_ms" in repaired_step and repaired_step["timeout_ms"] is not None:
        timeout_value = repaired_step["timeout_ms"]
        if isinstance(timeout_value, str):
            if timeout_value.strip().isdigit():
                repaired_step["timeout_ms"] = int(timeout_value.strip())
                normalization_notes.append(f"步骤 #{index} 的 timeout_ms 已自动转换为整数。")
            elif not allow_auto_repair:
                raise DslGenerationError(f"步骤 #{index} 的 timeout_ms 字段类型非法。")

    try:
        return _STEP_ADAPTER.validate_python(repaired_step), repaired_invalid_actions
    except ValidationError:
        if allow_auto_repair:
            return None, repaired_invalid_actions
        raise DslGenerationError(f"步骤 #{index} 不符合当前 DSL schema。")


def _resolve_base_url(
    *,
    raw_case: dict[str, Any],
    payload: GenerateDslRequest,
    warnings: list[str],
    normalization_notes: list[str],
) -> tuple[str | None, GenerateDslBaseUrlSource, bool]:
    raw_base_url = _normalize_optional_string(raw_case.get("base_url"))
    request_base_url = _normalize_optional_string(payload.base_url)
    current_case_base_url = payload.current_case.base_url if payload.current_case is not None else None

    if raw_base_url:
        return raw_base_url, "ai_output", False
    if request_base_url:
        warnings.append("AI 草案未提供 base_url，已回填请求中的 Base URL。")
        return request_base_url, "request", True
    if current_case_base_url:
        normalization_notes.append("AI 草案未提供 base_url，已沿用当前 DSL 的 Base URL。")
        return current_case_base_url, "current_case", True
    return None, "none", False


def _resolve_current_contracts(
    payload: GenerateDslRequest,
) -> tuple[list[DSLCaseInputContract], list[DSLCaseOutputContract]]:
    if payload.current_case is not None:
        return payload.current_case.input_contract, payload.current_case.output_contract
    return payload.current_input_contract or [], payload.current_output_contract or []


def resolve_generation_mode(
    request_generation_mode: GenerateDslMode | None,
    *,
    settings=None,
) -> GenerateDslMode:
    if request_generation_mode is not None:
        return request_generation_mode
    active_settings = settings or get_settings()
    return "strict_steps_only" if active_settings.ai_dsl_strict_mode else "draft"


def _normalize_string(value: Any) -> str | None:
    if not isinstance(value, str):
        return None
    normalized = value.strip()
    return normalized or None


def _normalize_optional_string(value: Any) -> str | None:
    return _normalize_string(value)


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
