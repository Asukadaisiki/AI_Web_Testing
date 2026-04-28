"""AI-assisted DSL generation helpers."""

from __future__ import annotations

import json
import logging
import re
from dataclasses import dataclass
from typing import Any
from urllib import request

from pydantic import TypeAdapter, ValidationError

from app.core.config import get_settings
from app.schemas.dsl import (
    AssertTextStep,
    AssertUrlContainsStep,
    CaptureTextStep,
    ClickStep,
    DSLCase,
    DSLCaseInputContract,
    DSLCaseOutputContract,
    DSLStep,
    GenerateDslBaseUrlSource,
    DslGenerationContextProfile,
    DslGenerationPromptVariant,
    DslGenerationRejectionReasonCode,
    DslGenerationRiskFlag,
    GenerateDslMeta,
    GenerateDslMode,
    GenerateDslRequest,
    GotoStep,
    InputStep,
    WaitForStep,
)


logger = logging.getLogger(__name__)


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
    "extract_text": "capture_text",
    "get_text": "capture_text",
    "save_text": "capture_text",
    "store_text": "capture_text",
}
_STEP_MODELS = {
    "goto": GotoStep,
    "click": ClickStep,
    "input": InputStep,
    "wait_for": WaitForStep,
    "assert_text": AssertTextStep,
    "assert_url_contains": AssertUrlContainsStep,
    "capture_text": CaptureTextStep,
}
_VALUE_TYPE_ALIASES = {
    "str": "string",
    "string": "string",
    "text": "string",
    "int": "number",
    "integer": "number",
    "float": "number",
    "double": "number",
    "number": "number",
    "bool": "boolean",
    "boolean": "boolean",
    "dict": "object",
    "map": "object",
    "json": "object",
    "object": "object",
    "list": "array",
    "array": "array",
}
_OUTPUT_SOURCE_ALIASES = {
    "url": "latest_url",
    "page_url": "latest_url",
    "current_url": "latest_url",
    "latest_url": "latest_url",
    "error_message": "error_message",
    "status": "status",
    "step_url": "last_step_url",
    "last_step_url": "last_step_url",
    "page_title": "last_step_page_title",
    "last_step_page_title": "last_step_page_title",
    "step_target": "last_step_target",
    "last_step_target": "last_step_target",
    "step_value": "last_step_value",
    "last_step_value": "last_step_value",
    "step_error_message": "last_step_error_message",
    "last_step_error_message": "last_step_error_message",
}
AI_DSL_PROMPT_VERSION = "2026-04-24.target-format-v2"
_BASE_SYSTEM_PROMPT_LINES = [
    "You generate structured web testing DSL in JSON only.",
    "Do not use any other action names.",
    "Return exactly one JSON object with keys:",
    "name, description, base_url, input_contract, output_contract, steps.",
    "",
    "## target format rules",
    "Use semantic target descriptions matching the actual page language (English text for English pages, Chinese for Chinese pages).",
    "When the page has visible labels, buttons, links, or input placeholders, use their EXACT visible text as the target value — as a plain string, NOT as a CSS selector.",
    "Correct examples: \"Login\", \"Email Address\", \"Signup / Login\", \"Submit\", \"Cart\".",
    "Wrong examples: \"input[placeholder='Email Address']\", \"button.login\", \"a[href='/login']\", \"#submit-btn\".",
    "Only use CSS/XPath selectors when no visible text exists (e.g., invisible divs, generic containers).",
    "Supported selector formats when needed: (1) CSS: css= prefix or #id, .class, tag.class; (2) XPath: xpath= prefix or //; (3) data-testid=value; (4) bare HTML tag names.",
    "NEVER invent compound formats like 'tag[placeholder=val]' or 'input placeholder=val'. These are invalid.",
    "",
    "## variable reference format",
    "When input_contract defines a variable with context_key (e.g., login_email), reference it in step value fields using ${context_key} syntax: ${login_email}.",
    "Correct: {\"action\": \"input\", \"target\": \"Email Address\", \"value\": \"${login_email}\"}.",
    "Wrong: {{login_email}}, {login_email}, %login_email%, <login_email>.",
    "Do NOT hardcode test data values in steps when a matching input_contract variable exists.",
    "",
    "Do not include markdown fences or explanations.",
    "Keep input_contract/output_contract/steps as arrays even when empty or single-item.",
    "Do not wrap the DSL under other keys such as case, data, result, response, or draft.",
    "Every contract must include name, context_key, value_type.",
    "Every output_contract item must also include source.",
    "context_key must use stable snake_case and match ^[A-Za-z_][A-Za-z0-9_]*$.",
    "If contract quality is uncertain, return an empty array instead of malformed entries.",
    "",
    "## capture_text action",
    "Use capture_text to extract visible text from a page element and store it as a runtime variable.",
    "It requires: target (element locator) and context_key (variable name in snake_case).",
    "Captured variables can be referenced in subsequent steps via ${context_key}, for example to assert cross-page data consistency.",
    "Example: capture a product price on the detail page, then assert the cart page contains the same price text.",
    "",
    "## form field coverage",
    "IMPORTANT: You MUST generate a step for EVERY form field mentioned in the prompt.",
    "If the prompt mentions filling, selecting, or checking a field, there must be a corresponding step.",
    "For <select> / dropdown fields: use action \"input\" with target being the field label and value being the option text.",
    "Example: {\"action\": \"input\", \"target\": \"Country\", \"value\": \"India\"}.",
    "For checkbox / toggle fields: use action \"click\" targeting the checkbox label.",
    "Example: {\"action\": \"click\", \"target\": \"Subscribe newsletter\"}.",
    "Before outputting, review your steps against the prompt. If any mentioned field lacks a step, add it.",
    "",
    "## step verification rules",
    "Every step in a DSL must be defensible — the executor should be able to confirm it worked before moving on.",
    "After an action that is expected to change the page state, add a verification step:",
    "  - Page navigation (click a link/menu that goes to a new page) → add wait_for for an element unique to the target page, or assert_url_contains to confirm the URL changed.",
    "  - Form submission (click Signup, Login, Submit, Create Account, etc.) → add wait_for or assert_text to confirm the expected result (success message, new page element, URL change).",
    "  - Async action (Add to Cart, Subscribe, Delete) → add wait_for for the confirmation message or UI change.",
    "Do NOT blindly add wait_for after every click. Only add verification where the step has a meaningful expected outcome that confirms it worked.",
    "Example of correct pattern: click \"Signup\" → wait_for \"Enter Account Information\" → input \"Password\".",
    "Example of wrong pattern: click \"Signup\" → immediately input \"Password\" (no verification that the signup form actually loaded).",
]
_PROMPT_VARIANT_RULES: dict[DslGenerationPromptVariant, list[str]] = {
    "contracts_focus": [
        "Prioritize high-quality input/output contracts and keep steps conservative.",
        "Do not rewrite the business flow unless the prompt explicitly asks for it.",
    ],
    "repair_steps": [
        "Focus on returning a stable, high-quality steps array.",
        "Do not change contracts unless the prompt explicitly asks for contract edits.",
    ],
    "rewrite_from_case": [
        "Rewrite from the provided current DSL while preserving the original business intent.",
        "Prefer editing existing flow over inventing unrelated new flow.",
    ],
    "baseline_draft": [
        "Return a complete first-draft DSL that is directly editable by users.",
    ],
}
_BASE_USER_RULE_LINES = [
    "要求：",
    "- steps 必须是数组，且每个 step 只能使用允许的 action。",
    "- input_contract 和 output_contract 如无需要，返回空数组。",
    "- 如果是相对路径跳转，优先保留为相对路径，并在 base_url 中提供站点地址。",
    "- 如果提供了当前 DSL 或当前 steps，请把它们视为改写上下文，而不是忽略。",
    "- target 必须使用元素的实际可见文本、label 或 placeholder 值，作为纯文本字符串（如 \"Email Address\"），不要构造 CSS 选择器格式的 target（如 \"input[placeholder='Email Address']\"）。仅在无可见文本时才使用 CSS/XPath 选择器。",
    "- 【定位器稳定性优先级】当页面元素清单中包含 stable 分数时，优先使用 stable>=0.70 的元素属性作为 target。如果目标元素有 data-testid，优先以 data-testid 值作为 target 并设置 target_strategy=\"data-testid\"。",
    "- 【同类重复元素消歧】当页面上有多个相同文本的元素（如多个 \"Add to cart\"），必须利用元素清单中的区分性属性（如 href、data-testid、aria-label、css_selector）或上下文描述（如 \"Product Name 附近的 Add to cart\"）来精确指定 target，不要只使用重复的文本。",
    "- 【置信度自评】对每个包含 target 的 step，添加 locator_confidence 字段：high（目标有唯一 data-testid/aria-label/text）、medium（有稳定属性但存在 2-3 个同类）、low（只能靠 XPath 位置或无区分属性的多个同类元素）。",
    "- 表单字段覆盖：必须为 prompt 中提到的每个表单字段生成对应步骤。下拉框用 input action（target 为字段标签，value 为选项文本），复选框用 click action（target 为复选框标签）。输出前检查是否有遗漏字段。",
    "- 当 input_contract 中定义了变量（如 context_key: login_email），step 的 value 字段必须用 ${context_key} 格式引用（如 \"${login_email}\"），不要硬编码值或使用其他占位符格式（如 {{}}、%%、<>）。",
    "- 如果需要明确指定定位策略，可在 step 中添加 target_strategy 字段（可选值：css, xpath, data-testid, element_id, tag, semantic）。不填则自动推断。",
    "- base_url 应为站点根地址（如 https://example.com），页面路径放在 goto 步骤中（如 /login）。不要将完整页面 URL 填入 base_url。",
    "- 生成前评估测试信息完整性：前置条件（系统初始状态）、入口（目标页面 URL 或导航路径）、操作步骤、预期结果。如果描述中缺少入口信息，通过 base_url + goto 步骤明确入口。",
]
DEFAULT_GOVERNANCE_REJECTION_REASONS: tuple[DslGenerationRejectionReasonCode, DslGenerationRejectionReasonCode] = (
    "context_mismatch",
    "bad_contracts",
)
SETTLED_GOVERNANCE_REJECTION_REASONS: tuple[DslGenerationRejectionReasonCode, DslGenerationRejectionReasonCode] = (
    "wrong_actions",
    "invalid_structure",
)
REJECTION_REASON_STRATEGIES: dict[DslGenerationRejectionReasonCode, list[str]] = {
    "wrong_actions": [
        "The previous draft used unsupported or overly aggressive actions.",
        "Use only the allowed actions and prefer conservative, low-risk steps.",
        "Only map well-known action aliases such as open/goto, tap/click, and fill/input.",
    ],
    "invalid_structure": [
        "The previous draft had invalid JSON/DSL structure.",
        "Always return all top-level keys and keep steps/input_contract/output_contract as arrays.",
        "Never nest the DSL under wrapper keys like case, data, result, or draft.",
        "If a single step or wrapped step list is returned, normalize it into a plain steps array.",
    ],
    "context_mismatch": [
        "The previous draft drifted away from the provided business context.",
        "Preserve the original business goal and reuse current_case/current_steps semantics when they exist.",
        "If existing case context is provided, keep names, URLs, and flow intent aligned unless the prompt explicitly asks to rewrite them.",
        "When the current case already has stable name or description, prefer preserving them over replacing them with generic placeholders.",
        "When current_case already provides a stable base_url or contract semantics, do not casually rewrite them with unrelated placeholders.",
    ],
    "bad_contracts": [
        "The previous draft produced low-quality contracts.",
        "Keep contracts minimal, stable, and use clear context_key naming without inventing unnecessary fields.",
        "Each contract must include name, context_key, value_type; output contracts must also include source.",
        "If contract quality is uncertain, prefer omitting unstable contracts instead of returning malformed entries.",
        "Normalize context_key into stable snake_case and drop output contracts that still do not have a stable source after repair.",
        "If current contracts are provided for preservation, reuse their stable names/descriptions and keep missing stable contracts instead of inventing generic placeholders.",
    ],
    "other": [],
}
_CONTRACT_FIELD_ALIASES: dict[str, tuple[str, ...]] = {
    "name": ("name", "label", "title"),
    "context_key": ("context_key", "contextKey", "key"),
    "value_type": ("value_type", "valueType", "type"),
    "required": ("required", "is_required", "isRequired"),
    "source": ("source", "value_from", "valueFrom", "extract_from", "extractFrom", "from"),
    "description": ("description", "desc", "notes"),
}
_CASE_WRAPPER_KEYS = ("case", "data", "result", "response", "draft")
_CASE_STEPS_ALIASES = ("step", "step_list", "stepList", "actions")
_STEP_ACTION_KEYS = ("action", "type", "command", "step_action", "stepAction")
_STEP_COLLECTION_KEYS = ("steps", "items", "list", "value", "data")
_STEP_TARGET_ALIASES: dict[str, tuple[str, ...]] = {
    "click": ("target", "element", "label", "selector", "locator", "description"),
    "input": ("target", "element", "label", "selector", "locator", "description"),
    "wait_for": ("target", "element", "label", "selector", "locator", "description"),
    "assert_text": ("target", "element", "label", "selector", "locator", "description"),
}
_STEP_VALUE_ALIASES: dict[str, tuple[str, ...]] = {
    "goto": ("value", "url", "path", "href", "target"),
    "input": ("value", "text", "input", "content"),
    "assert_text": ("value", "expected", "expected_text", "expectedText", "text"),
    "assert_url_contains": ("value", "expected", "url", "path", "contains", "target"),
}
_STEP_TIMEOUT_ALIASES = ("timeout_ms", "timeoutMs", "timeout")
_GENERIC_CASE_NAMES = {"ai 生成用例", "ai生成用例", "generated test case", "test case", "测试用例"}
_GENERIC_CASE_DESCRIPTIONS = {
    "ai 自动生成测试用例",
    "自动生成测试用例",
    "自动生成",
    "generated by ai",
    "ai generated test case",
}
_GENERIC_CONTRACT_NAMES = {
    "input",
    "output",
    "value",
    "values",
    "data",
    "result",
    "field",
    "item",
    "param",
    "params",
    "输入",
    "输出",
    "值",
    "数据",
    "结果",
    "字段",
    "参数",
}


@dataclass
class ContractNormalizationContext:
    adapter: TypeAdapter[Any]
    label: str
    is_output_contract: bool
    allow_auto_repair: bool
    warnings: list[str]
    normalization_notes: list[str]


def build_generation_messages(
    *,
    payload: GenerateDslRequest,
    generation_mode: GenerateDslMode,
    prompt_variant: DslGenerationPromptVariant,
    supported_actions: list[str],
    governance_focus_reasons: list[DslGenerationRejectionReasonCode] | None = None,
) -> list[dict[str, Any]]:
    system_lines = _build_system_prompt_lines(
        payload=payload,
        generation_mode=generation_mode,
        prompt_variant=prompt_variant,
        supported_actions=supported_actions,
        governance_focus_reasons=governance_focus_reasons,
    )
    user_lines = _build_user_prompt_lines(payload=payload, generation_mode=generation_mode)
    return [
        {"role": "system", "content": " ".join(system_lines)},
        {"role": "user", "content": "\n".join(user_lines)},
    ]


def _build_system_prompt_lines(
    *,
    payload: GenerateDslRequest,
    generation_mode: GenerateDslMode,
    prompt_variant: DslGenerationPromptVariant,
    supported_actions: list[str],
    governance_focus_reasons: list[DslGenerationRejectionReasonCode] | None = None,
) -> list[str]:
    system_lines = [
        _BASE_SYSTEM_PROMPT_LINES[0],
        f"Allowed actions: {', '.join(supported_actions)}.",
        *_BASE_SYSTEM_PROMPT_LINES[1:],
        f"Prompt variant: {prompt_variant}.",
        *_PROMPT_VARIANT_RULES[prompt_variant],
    ]
    if generation_mode == "strict_steps_only":
        system_lines.append("In strict_steps_only mode, prioritize returning a high-quality steps array.")
    if payload.import_mode == "contracts_only":
        system_lines.append("When import_mode is contracts_only, include useful input/output contracts when possible.")
    if payload.preserve_contracts:
        system_lines.append(
            "If current contracts are provided, keep them stable unless the prompt explicitly asks to change them."
        )
    active_focus_reasons = governance_focus_reasons or list(DEFAULT_GOVERNANCE_REJECTION_REASONS)
    if active_focus_reasons:
        system_lines.append(f"Current governance focus reasons: {', '.join(active_focus_reasons)}.")
        _append_unique_lines(system_lines, _collect_reason_strategy_lines(active_focus_reasons))
    if payload.retry_reason_code is not None:
        system_lines.append(f"Retry strategy: {payload.retry_reason_code}.")
        _append_unique_lines(system_lines, _collect_reason_strategy_lines([payload.retry_reason_code]))
    return system_lines


def _build_user_prompt_lines(
    *,
    payload: GenerateDslRequest,
    generation_mode: GenerateDslMode,
) -> list[str]:
    user_lines = [
        "请根据下面的测试需求生成可编辑 DSL 草案。",
        f"测试需求：{payload.prompt.strip()}",
        f"生成模式：{generation_mode}",
        f"预期导入方式：{payload.import_mode}",
        f"建议 Base URL：{payload.base_url.strip() if payload.base_url else '未提供'}",
        f"是否保留当前契约：{'是' if payload.preserve_contracts else '否'}",
        *_BASE_USER_RULE_LINES,
    ]
    if payload.page_elements:
        user_lines.extend(
            [
                "页面可交互元素清单（请严格使用其中的 label、placeholder 或 id 作为 target）：",
                payload.page_elements,
                "",
                "评分与候选策略规则：",
                "- 元素清单中包含 candidates 字段，每个候选有 strategy、selector、pre_score",
                "- 如果元素有 candidates，在对应步骤中使用 candidates 字段（取前 3 个候选，按 pre_score 降序）",
                "- 每个交互步骤的最后一个候选必须是 VLM 策略（strategy='vlm', pre_score=0.0）作为兜底",
                "- 每个交互步骤应推断至少 1 个 postcondition：",
                "  - 导航点击 → {type: 'url_changes'} 或 {type: 'url_contains', value: '...'}",
                "  - 表单提交 → {type: 'url_contains', value: '...'} 或 {type: 'text_visible', value: '...'}",
                "  - 输入操作 → {type: 'value_changed'}",
                "  - 删除操作 → {type: 'text_gone', value: '...'}",
            ]
        )
    if payload.retry_from_generation_id is not None and payload.retry_reason_code is not None:
        user_lines.extend(
            [
                "本次请求是对上一版草案的重试生成。",
                f"上一版 generation_id：{payload.retry_from_generation_id}",
                f"上一版被放弃原因：{payload.retry_reason_code}",
            ]
        )
        if payload.retry_note:
            user_lines.append(f"用户补充说明：{payload.retry_note.strip()}")
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
    return user_lines


def resolve_prompt_version(payload: GenerateDslRequest) -> str:
    if payload.retry_reason_code is None:
        return AI_DSL_PROMPT_VERSION
    return f"{AI_DSL_PROMPT_VERSION}+retry.{payload.retry_reason_code}"


def generate_case_draft(
    *,
    payload: GenerateDslRequest,
    supported_actions: list[str],
    governance_focus_reasons: list[DslGenerationRejectionReasonCode] | None = None,
) -> tuple[DSLCase, list[str], list[str], GenerateDslMeta]:
    settings = get_settings()
    resolved_generation_mode = resolve_generation_mode(payload.generation_mode, settings=settings)
    prompt_variant, context_profile = resolve_generation_profile(payload=payload, generation_mode=resolved_generation_mode)
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
            prompt_variant=prompt_variant,
            supported_actions=supported_actions,
            governance_focus_reasons=governance_focus_reasons,
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
        prompt_variant=prompt_variant,
        context_profile=context_profile,
        model_name=settings.ai_dsl_model,
        allow_auto_repair=settings.ai_dsl_allow_auto_repair,
        governance_focus_reasons=governance_focus_reasons,
    )

    # Auto-fix: inject wait_for after navigation/submit clicks missing verification
    _auto_inject_verification_steps(normalized_case, normalization_notes)

    _verify_field_coverage(
        prompt=payload.prompt,
        steps=normalized_case.get("steps", []),
        warnings=warnings,
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
    prompt_variant: DslGenerationPromptVariant,
    context_profile: DslGenerationContextProfile,
    model_name: str,
    allow_auto_repair: bool,
    governance_focus_reasons: list[DslGenerationRejectionReasonCode] | None,
) -> tuple[dict[str, Any], list[str], list[str], GenerateDslMeta]:
    raw_case = _repair_case_payload(
        raw_case=raw_case,
        allow_auto_repair=allow_auto_repair,
    )
    warnings: list[str] = []
    normalization_notes: list[str] = list(raw_case.pop("_normalization_notes", []))
    removed_invalid_steps = 0
    removed_invalid_contracts = 0
    repaired_invalid_actions = 0

    current_case = payload.current_case
    current_input_contract, current_output_contract = _resolve_current_contracts(payload)
    used_current_case_context = current_case is not None
    used_current_steps_context = payload.current_steps is not None
    risk_flags: list[DslGenerationRiskFlag] = []

    normalized_name = _normalize_string(raw_case.get("name"))
    normalized_description = _normalize_optional_string(raw_case.get("description"))

    if generation_mode == "strict_steps_only" and current_case is not None:
        normalized_name = current_case.name
        normalized_description = current_case.description
        normalization_notes.append("strict_steps_only 模式下沿用了当前 DSL 的名称与描述。")
    elif not normalized_name:
        normalized_name = current_case.name if current_case is not None else "AI 生成用例"
        warnings.append("AI 草案未提供有效 name，已使用当前上下文中的名称或默认名称。")
        risk_flags.append("missing_name_fallback")

    active_governance_focus_reasons = resolve_active_governance_reasons(
        governance_focus_reasons=governance_focus_reasons,
        retry_reason_code=payload.retry_reason_code,
    )

    normalized_name, normalized_description = _apply_context_mismatch_repairs(
        normalized_name=normalized_name,
        normalized_description=normalized_description,
        current_case=current_case,
        active_focus_reasons=active_governance_focus_reasons,
        normalization_notes=normalization_notes,
    )

    base_url_value, base_url_source, base_url_backfilled = _resolve_base_url(
        raw_case=raw_case,
        payload=payload,
        active_focus_reasons=active_governance_focus_reasons,
        warnings=warnings,
        normalization_notes=normalization_notes,
    )

    input_contract, input_removed = _normalize_contracts(
        raw_contracts=raw_case.get("input_contract"),
        context=ContractNormalizationContext(
            adapter=_INPUT_CONTRACT_ADAPTER,
            label="输入契约",
            is_output_contract=False,
            allow_auto_repair=allow_auto_repair,
            warnings=warnings,
            normalization_notes=normalization_notes,
        ),
    )
    output_contract, output_removed = _normalize_contracts(
        raw_contracts=raw_case.get("output_contract"),
        context=ContractNormalizationContext(
            adapter=_OUTPUT_CONTRACT_ADAPTER,
            label="输出契约",
            is_output_contract=True,
            allow_auto_repair=allow_auto_repair,
            warnings=warnings,
            normalization_notes=normalization_notes,
        ),
    )
    removed_invalid_contracts += input_removed + output_removed

    preserve_contracts_applied = False
    if payload.preserve_contracts:
        missing_input_contract = not input_contract and bool(current_input_contract)
        missing_output_contract = not output_contract and bool(current_output_contract)
        if missing_input_contract or missing_output_contract:
            if missing_input_contract:
                input_contract = current_input_contract
            if missing_output_contract:
                output_contract = current_output_contract
            preserve_contracts_applied = True
            if missing_input_contract and missing_output_contract:
                normalization_notes.append("AI 草案未提供有效契约，已沿用当前 DSL 的输入/输出契约。")
            elif missing_input_contract:
                normalization_notes.append("AI 草案未提供有效输入契约，已沿用当前 DSL 的输入契约。")
            else:
                normalization_notes.append("AI 草案未提供有效输出契约，已沿用当前 DSL 的输出契约。")
            risk_flags.append("contracts_preserved_fallback")
            preserve_contracts_applied = True

        input_contract, input_contract_stabilized = _stabilize_contracts_from_current(
            generated_contracts=input_contract,
            current_contracts=current_input_contract,
            label="输入契约",
            active_focus_reasons=active_governance_focus_reasons,
            normalization_notes=normalization_notes,
        )
        output_contract, output_contract_stabilized = _stabilize_contracts_from_current(
            generated_contracts=output_contract,
            current_contracts=current_output_contract,
            label="输出契约",
            active_focus_reasons=active_governance_focus_reasons,
            normalization_notes=normalization_notes,
        )
        if input_contract_stabilized or output_contract_stabilized:
            preserve_contracts_applied = True
            if "contracts_preserved_fallback" not in risk_flags:
                risk_flags.append("contracts_preserved_fallback")

    steps, removed_invalid_steps, repaired_invalid_actions = _normalize_steps(
        raw_steps=raw_case.get("steps"),
        allow_auto_repair=allow_auto_repair,
        warnings=warnings,
        normalization_notes=normalization_notes,
    )

    _check_dsl_completeness(
        {"base_url": base_url_value, "steps": steps},
        warnings,
        normalization_notes,
    )

    if not steps:
        raise DslGenerationError("AI 生成草案中没有可导入的有效 steps。")

    if base_url_backfilled:
        risk_flags.append("base_url_backfilled")
    if repaired_invalid_actions > 0:
        risk_flags.append("invalid_actions_repaired")
    if removed_invalid_steps > 0:
        risk_flags.append("invalid_steps_removed")
    if removed_invalid_contracts > 0:
        risk_flags.append("invalid_contracts_removed")

    generation_meta = GenerateDslMeta(
        model=model_name,
        generation_mode=generation_mode,
        import_mode=payload.import_mode,
        prompt_variant=prompt_variant,
        context_profile=context_profile,
        active_governance_focus_reasons=active_governance_focus_reasons,
        risk_flags=risk_flags,
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
    context: ContractNormalizationContext,
) -> tuple[list[Any], int]:
    if raw_contracts is None:
        return [], 0
    if isinstance(raw_contracts, dict):
        if not context.allow_auto_repair:
            raise DslGenerationError(f"AI 草案中的{context.label}必须是数组。")
        raw_contracts = [raw_contracts]
        context.normalization_notes.append(f"AI 草案中的{context.label}已从单个对象包装为数组。")
    if not isinstance(raw_contracts, list):
        context.warnings.append(f"AI 草案中的{context.label}不是数组，已忽略该字段。")
        return [], 1

    normalized_contracts: list[Any] = []
    seen_context_keys: set[str] = set()
    removed_count = 0
    for index, raw_contract in enumerate(raw_contracts, start=1):
        if not isinstance(raw_contract, dict):
            removed_count += 1
            context.warnings.append(f"{context.label} #{index} 不是对象，已忽略。")
            continue
        candidate_contract = (
            _repair_contract_payload(
                raw_contract=raw_contract,
                index=index,
                context=context,
            )
            if context.allow_auto_repair
            else raw_contract
        )
        try:
            contract = context.adapter.validate_python(candidate_contract)
        except ValidationError:
            removed_count += 1
            if context.allow_auto_repair:
                context.warnings.append(f"{context.label} #{index} 结构非法，已忽略。")
            else:
                raise DslGenerationError(f"AI 草案中的{context.label} #{index} 不符合当前 schema。")
            continue
        if contract.context_key in seen_context_keys:
            removed_count += 1
            context.warnings.append(f"{context.label} #{index} 的 context_key 与前文重复，已忽略。")
            continue
        if context.is_output_contract and contract.source is None:
            removed_count += 1
            context.warnings.append(f"{context.label} #{index} 缺少稳定 source，已忽略。")
            continue
        seen_context_keys.add(contract.context_key)
        normalized_contracts.append(contract)
    return normalized_contracts, removed_count


def _check_dsl_completeness(
    case_data: dict[str, Any],
    warnings: list[str],
    normalization_notes: list[str] | None = None,
) -> None:
    """检查生成 DSL 的完整性，对可疑模式发出 warning。

    不阻断生成，仅发出提示，保持用例灵活性。
    """
    if normalization_notes is None:
        normalization_notes = []

    base_url = case_data.get("base_url") or ""
    steps = case_data.get("steps") or []

    # 检测 base_url 是否包含页面路径（如 https://example.com/login）
    if base_url:
        from urllib.parse import urlparse
        parsed = urlparse(base_url)
        if parsed.path and parsed.path.strip("/"):
            warnings.append(
                f"base_url 疑似包含页面路径（{parsed.path}），建议将站点根地址放在 base_url，"
                f"页面路径放在 goto 步骤中（如 goto {parsed.path}）。"
            )

    # 检测有 base_url 和 steps 但无 goto 步骤的情况
    has_goto = any(
        (isinstance(s, dict) and s.get("action") == "goto")
        or (hasattr(s, "action") and getattr(s, "action", None) == "goto")
        for s in steps
    )
    if base_url and steps and not has_goto:
        normalization_notes.append(
            "DSL 中没有 goto 步骤。如果测试需要先导航到目标页面，建议添加 goto 步骤。"
        )

    # 语义校验：检测关键状态变化步骤后缺少验证
    _check_step_verification(steps, warnings)


def _auto_inject_verification_steps(
    case_data: dict[str, Any],
    normalization_notes: list[str],
) -> None:
    """Auto-inject wait_for steps after navigation/submit clicks that lack verification.

    This is a safety net — the AI should already produce correct DSL thanks to
    the system prompt rules, but this catches cases where it doesn't.
    Mutates case_data['steps'] in place.
    """
    steps = case_data.get("steps")
    if not steps or len(steps) < 2:
        return

    # Normalize steps to dicts (Pydantic objects need conversion)
    dict_steps = []
    for s in steps:
        if isinstance(s, dict):
            dict_steps.append(s)
        elif hasattr(s, "model_dump"):
            dict_steps.append(s.model_dump(exclude_none=True))
        elif hasattr(s, "__dict__"):
            dict_steps.append({k: v for k, v in vars(s).items() if not k.startswith("_")})
        else:
            dict_steps.append(s)

    if dict_steps and not isinstance(dict_steps[0], dict):
        return

    nav_patterns = {
        "signup": "Enter Account Information",
        "login": "Logout",
        "create account": "ACCOUNT CREATED",
        "delete account": "ACCOUNT DELETED",
        "logout": "Signup / Login",
        "add to cart": "View Cart",
        "view cart": "Shopping Cart",
        "subscribe": "successfully subscribed",
        "submit": "Success",
        "contact us": "contact_us",
    }
    verify_actions = {"wait_for", "assert_text", "assert_url_contains"}

    new_steps = []
    injected = 0
    for i, step in enumerate(dict_steps):
        new_steps.append(step)
        if isinstance(step, dict):
            action = step.get("action", "")
            target = (step.get("target") or "").lower().strip()
        elif hasattr(step, "action"):
            action = getattr(step, "action", "")
            target = (getattr(step, "target", "") or "").lower().strip()
        else:
            continue

        if action != "click":
            continue

        wait_target = None
        for kw, expected in nav_patterns.items():
            if kw in target:
                wait_target = expected
                break

        if not wait_target:
            continue

        if i + 1 < len(dict_steps):
            next_step = dict_steps[i + 1]
            if isinstance(next_step, dict):
                next_action = next_step.get("action", "")
            elif hasattr(next_step, "action"):
                next_action = getattr(next_step, "action", "")
            else:
                next_action = ""
            if next_action in verify_actions:
                continue

        inject = {"action": "wait_for", "target": wait_target, "timeout_ms": 5000}
        new_steps.append(inject)
        injected += 1

    if injected > 0:
        logger.debug("auto_inject: added %d wait_for steps (total %d)", injected, len(new_steps))
        case_data["steps"] = new_steps
        normalization_notes.append(
            f"自动注入了 {injected} 个 wait_for 验证步骤，确保关键页面跳转后有状态验证。"
        )


# --- Field coverage verification ---

_FILL_VERB_PATTERNS = [
    re.compile(r"(?:fill\s+(?:in\s+)?|enter\s+|input\s+|select\s+|choose\s+|type\s+)[\"'“‘]([^\"'”’]+)[\"'”’]", re.IGNORECASE),
    re.compile(r"(?:填写|选择|输入|勾选)\s*[\"'“‘]([^\"'”’]+)[\"'”’]"),
]

_FORM_FIELD_KEYWORDS: list[tuple[str, str]] = [
    ("country", "country"), ("state", "state"), ("city", "city"),
    ("address", "address"), ("zip", "zip"), ("zipcode", "zipcode"),
    ("date", "date"), ("birthday", "birthday"), ("birth", "birth"),
    ("first name", "first name"), ("last name", "last name"),
    ("password", "password"), ("email", "email"), ("phone", "phone"),
    ("mobile", "mobile"), ("company", "company"), ("name", "name"),
    ("username", "username"), ("title", "title"),
]

_CHECKBOX_KEYWORDS = [
    "subscribe", "newsletter", "agree", "terms", "consent",
    "checkbox", "opt-in", "opt in", "receive", "accept",
    "订阅", "接受", "同意", "勾选",
]

_CN_FIELD_MAP: dict[str, str] = {
    "国家": "country", "州": "state", "城市": "city",
    "地址": "address", "邮编": "zip", "日期": "date",
    "生日": "birthday", "姓名": "name", "密码": "password",
    "邮箱": "email", "电话": "phone", "公司": "company",
    "用户名": "username", "手机": "mobile",
}


def _verify_field_coverage(
    *,
    prompt: str,
    steps: list[Any],
    warnings: list[str],
) -> None:
    """Check that fields mentioned in the prompt are covered by DSL steps."""
    if not prompt or not steps:
        return

    prompt_lower = prompt.lower()

    # Extract expected fields from prompt
    expected_fields: set[str] = set()

    # Pattern 1: explicit fill verbs with quoted field names
    for pat in _FILL_VERB_PATTERNS:
        for m in pat.finditer(prompt):
            field = m.group(1).strip().lower()
            if field:
                expected_fields.add(field)

    # Pattern 2: known form field keywords present in prompt
    for keyword, label in _FORM_FIELD_KEYWORDS:
        if keyword in prompt_lower:
            expected_fields.add(label)

    # Pattern 3: Chinese field names
    for cn, en in _CN_FIELD_MAP.items():
        if cn in prompt:
            expected_fields.add(en)

    # Pattern 4: checkbox keywords
    for kw in _CHECKBOX_KEYWORDS:
        if kw in prompt_lower:
            expected_fields.add(kw)

    if not expected_fields:
        return

    # Extract covered fields from DSL steps
    covered: set[str] = set()
    for step in steps:
        sd = step if isinstance(step, dict) else (step.model_dump(exclude_none=True) if hasattr(step, "model_dump") else {})
        action = sd.get("action", "")
        target = (sd.get("target") or "").strip().lower()
        if action in ("input", "click") and target:
            covered.add(target)

    # Compare: check each expected field against covered targets
    for expected in sorted(expected_fields):
        matched = any(
            expected in c or c in expected
            for c in covered
        )
        if not matched:
            warnings.append(
                f"字段覆盖检查：prompt 中提到了 \"{expected}\"，"
                f"但生成的步骤中没有对应的 input/click 操作。"
            )


def _check_step_verification(
    steps: list[Any],
    warnings: list[str],
) -> None:
    """Check that critical state-changing steps have follow-up verification.

    Scans for clicks on navigation/form-submit actions that are NOT followed
    by a verification step (wait_for, assert_text, assert_url_contains).
    Emits warnings but does NOT block generation.
    """
    if not steps or len(steps) < 2:
        return

    nav_keywords = {
        "signup", "login", "submit", "create", "delete", "logout",
        "register", "continue", "next", "checkout", "place order",
        "view cart", "view product", "add to cart", "subscribe",
        "contact us", "send", "confirm", "proceed",
    }
    verify_actions = {"wait_for", "assert_text", "assert_url_contains"}

    for i in range(len(steps) - 1):
        step = steps[i]
        if isinstance(step, dict):
            action = step.get("action", "")
            target = (step.get("target") or "").lower()
        elif hasattr(step, "action"):
            action = getattr(step, "action", "")
            target = (getattr(step, "target", "") or "").lower()
        else:
            continue

        if action != "click":
            continue

        is_nav_click = any(kw in target for kw in nav_keywords)
        if not is_nav_click:
            continue

        next_step = steps[i + 1]
        if isinstance(next_step, dict):
            next_action = next_step.get("action", "")
        elif hasattr(next_step, "action"):
            next_action = getattr(next_step, "action", "")
        else:
            next_action = ""

        if next_action not in verify_actions:
            target_display = step.get("target", "") if isinstance(step, dict) else getattr(step, "target", "")
            warnings.append(
                f"步骤 {i + 1} click \"{target_display}\" 可能触发页面跳转或状态变化，"
                f"但下一步是 {next_action} 而非验证步骤（wait_for/assert_text/assert_url_contains）。"
                f"建议在 click 后添加验证步骤确认操作结果。"
            )


def _normalize_steps(
    *,
    raw_steps: Any,
    allow_auto_repair: bool,
    warnings: list[str],
    normalization_notes: list[str],
) -> tuple[list[DSLStep], int, int]:
    if isinstance(raw_steps, dict):
        if not allow_auto_repair:
            raise DslGenerationError("AI 草案中的 steps 必须是数组。")
        wrapped_steps = _extract_wrapped_steps(raw_steps)
        if wrapped_steps is not None:
            raw_steps = wrapped_steps
            normalization_notes.append("AI 草案中的 steps 已从包装对象中提取为数组。")
        else:
            raw_steps = [raw_steps]
            normalization_notes.append("AI 草案中的 steps 已从单个对象包装为数组。")
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
    repaired_step = _repair_step_shape(
        raw_step=raw_step,
        index=index,
        allow_auto_repair=allow_auto_repair,
        normalization_notes=normalization_notes,
    )
    action_value = repaired_step.get("action")
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

    if "target_strategy" in repaired_step and repaired_step["target_strategy"] is not None:
        valid_strategies = {"css", "xpath", "data-testid", "element_id", "tag", "semantic"}
        strategy_value = repaired_step["target_strategy"]
        if isinstance(strategy_value, str):
            normalized_strategy = strategy_value.strip().lower()
            if normalized_strategy in valid_strategies:
                repaired_step["target_strategy"] = normalized_strategy
            else:
                repaired_step.pop("target_strategy", None)
                normalization_notes.append(
                    f"步骤 #{index} 的 target_strategy 值 '{strategy_value}' 无效，已忽略。"
                )
        else:
            repaired_step.pop("target_strategy", None)

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
    active_focus_reasons: list[DslGenerationRejectionReasonCode],
    warnings: list[str],
    normalization_notes: list[str],
) -> tuple[str | None, GenerateDslBaseUrlSource, bool]:
    raw_base_url = _normalize_optional_string(raw_case.get("base_url"))
    request_base_url = _normalize_optional_string(payload.base_url)
    current_case_base_url = (
        _normalize_optional_string(payload.current_case.base_url)
        if payload.current_case is not None
        else None
    )

    if raw_base_url:
        if (
            current_case_base_url
            and "context_mismatch" in active_focus_reasons
            and raw_base_url != current_case_base_url
        ):
            normalization_notes.append(
                "AI 草案的 base_url 与当前 DSL 的稳定 Base URL 不一致，已沿用当前 DSL 的 Base URL。"
            )
            return current_case_base_url, "current_case", True
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


def _repair_case_payload(
    *,
    raw_case: dict[str, Any],
    allow_auto_repair: bool,
) -> dict[str, Any]:
    if not allow_auto_repair:
        return raw_case

    repaired = dict(raw_case)
    wrapper_key = next(
        (
            key
            for key in _CASE_WRAPPER_KEYS
            if isinstance(repaired.get(key), dict)
            and _looks_like_case_payload(repaired[key])
            and not _looks_like_case_payload(repaired)
        ),
        None,
    )
    if wrapper_key is not None:
        repaired = dict(repaired[wrapper_key])
        repaired["_normalization_notes"] = [f"AI 草案已从 {wrapper_key} 包装层中提取 DSL 根对象。"]

    if "name" not in repaired and isinstance(repaired.get("title"), str) and repaired.get("title").strip():
        repaired["name"] = repaired["title"]
        repaired.setdefault("_normalization_notes", []).append("AI 草案中的 title 已映射为 name。")

    if "steps" not in repaired:
        for alias_key in _CASE_STEPS_ALIASES:
            if alias_key not in repaired:
                continue
            repaired["steps"] = repaired[alias_key]
            repaired.setdefault("_normalization_notes", []).append(f"AI 草案中的 {alias_key} 已映射为 steps。")
            break

    return repaired


def _repair_contract_payload(
    *,
    raw_contract: dict[str, Any],
    index: int,
    context: ContractNormalizationContext,
) -> dict[str, Any]:
    if not context.allow_auto_repair:
        return raw_contract

    repaired = dict(raw_contract)
    name = _normalize_string(_promote_contract_alias(repaired, canonical_key="name", index=index, context=context))
    context_key = _normalize_string(
        _promote_contract_alias(repaired, canonical_key="context_key", index=index, context=context)
    )
    if name:
        repaired["name"] = name
    if context_key:
        repaired["context_key"] = context_key

    if not name and context_key:
        repaired["name"] = context_key
        context.normalization_notes.append(f"{context.label} #{index} 缺少 name，已回填为 {context_key}。")
    if not context_key and name:
        derived_context_key = _derive_context_key(name)
        if derived_context_key is not None:
            repaired["context_key"] = derived_context_key
            context.normalization_notes.append(
                f"{context.label} #{index} 缺少 context_key，已从 name 派生为 {derived_context_key}。"
            )
    elif context_key:
        normalized_context_key = _derive_context_key(context_key)
        if normalized_context_key is not None and normalized_context_key != context_key:
            repaired["context_key"] = normalized_context_key
            context.normalization_notes.append(
                f"{context.label} #{index} 的 context_key 已自动修正为 {normalized_context_key}。"
            )

    value_type = _promote_contract_alias(repaired, canonical_key="value_type", index=index, context=context)
    if isinstance(value_type, str):
        normalized_value_type = _VALUE_TYPE_ALIASES.get(value_type.strip().casefold())
        if normalized_value_type is not None and normalized_value_type != value_type:
            repaired["value_type"] = normalized_value_type
            context.normalization_notes.append(
                f"{context.label} #{index} 的 value_type 已从 {value_type} 自动修正为 {normalized_value_type}。"
            )

    required_value = _promote_contract_alias(repaired, canonical_key="required", index=index, context=context)
    if "required" in repaired or required_value is not None:
        normalized_required = _coerce_bool(required_value)
        if normalized_required is not None and normalized_required != repaired["required"]:
            repaired["required"] = normalized_required
            context.normalization_notes.append(f"{context.label} #{index} 的 required 已自动修正为布尔值。")

    if context.is_output_contract:
        source = _promote_contract_alias(repaired, canonical_key="source", index=index, context=context)
        if isinstance(source, str):
            normalized_source = _OUTPUT_SOURCE_ALIASES.get(source.strip().casefold())
            if normalized_source is not None and normalized_source != source:
                repaired["source"] = normalized_source
                context.normalization_notes.append(
                    f"{context.label} #{index} 的 source 已从 {source} 自动修正为 {normalized_source}。"
                )
        elif source is None:
            inferred_source = _infer_output_source_from_contract(repaired)
            if inferred_source is not None:
                repaired["source"] = inferred_source
                context.normalization_notes.append(
                    f"{context.label} #{index} 的 source 已根据契约语义自动补全为 {inferred_source}。"
                )

    description = _promote_contract_alias(repaired, canonical_key="description", index=index, context=context)
    if description is not None and not isinstance(description, str):
        repaired["description"] = str(description)
        context.normalization_notes.append(f"{context.label} #{index} 的 description 已自动转换为字符串。")

    return repaired


def _repair_step_shape(
    *,
    raw_step: dict[str, Any],
    index: int,
    allow_auto_repair: bool,
    normalization_notes: list[str],
) -> dict[str, Any]:
    if not allow_auto_repair:
        return dict(raw_step)

    repaired = dict(raw_step)
    action_key = next(
        (key for key in _STEP_ACTION_KEYS if isinstance(repaired.get(key), str) and repaired.get(key).strip()),
        None,
    )
    if action_key is not None and action_key != "action" and repaired.get("action") in (None, ""):
        repaired["action"] = repaired[action_key]
        normalization_notes.append(f"步骤 #{index} 的 {action_key} 已映射为 action。")
    if action_key is not None and action_key != "action":
        repaired.pop(action_key, None)

    action_value = repaired.get("action")
    normalized_action = None
    if isinstance(action_value, str) and action_value.strip():
        normalized_action = _ACTION_ALIASES.get(action_value.strip(), action_value.strip())

    if normalized_action in _STEP_TARGET_ALIASES:
        _promote_step_field_alias(
            repaired,
            canonical_key="target",
            alias_keys=_STEP_TARGET_ALIASES[normalized_action],
            index=index,
            normalization_notes=normalization_notes,
        )
    if normalized_action in _STEP_VALUE_ALIASES:
        _promote_step_field_alias(
            repaired,
            canonical_key="value",
            alias_keys=_STEP_VALUE_ALIASES[normalized_action],
            index=index,
            normalization_notes=normalization_notes,
        )
    if normalized_action == "wait_for":
        _promote_step_field_alias(
            repaired,
            canonical_key="timeout_ms",
            alias_keys=_STEP_TIMEOUT_ALIASES,
            index=index,
            normalization_notes=normalization_notes,
        )

    return repaired


def _promote_step_field_alias(
    repaired: dict[str, Any],
    *,
    canonical_key: str,
    alias_keys: tuple[str, ...],
    index: int,
    normalization_notes: list[str],
) -> Any:
    alias_key = next(
        (
            key
            for key in alias_keys
            if key in repaired and repaired.get(key) not in (None, "") and repaired.get(canonical_key) in (None, "")
        ),
        None,
    )
    if alias_key is None:
        return repaired.get(canonical_key)
    repaired[canonical_key] = repaired[alias_key]
    if alias_key != canonical_key:
        normalization_notes.append(f"步骤 #{index} 的 {alias_key} 已映射为 {canonical_key}。")
        repaired.pop(alias_key, None)
    return repaired.get(canonical_key)


def _extract_wrapped_steps(raw_steps: dict[str, Any]) -> list[Any] | None:
    if any(key in raw_steps for key in _STEP_ACTION_KEYS):
        return None
    for key in _STEP_COLLECTION_KEYS:
        candidate = raw_steps.get(key)
        if isinstance(candidate, list):
            return candidate
        if isinstance(candidate, dict):
            nested = _extract_wrapped_steps(candidate)
            if nested is not None:
                return nested
    return None


def _looks_like_case_payload(raw_case: dict[str, Any]) -> bool:
    return any(key in raw_case for key in ("name", "title", "steps", "step", "input_contract", "output_contract"))


def _derive_context_key(name: str) -> str | None:
    candidate = name.strip()
    if not candidate:
        return None
    candidate = re.sub(r"([a-z0-9])([A-Z])", r"\1_\2", candidate)
    candidate = re.sub(r"[^A-Za-z0-9]+", "_", candidate)
    candidate = candidate.strip("_").casefold()
    if not candidate:
        return None
    if candidate[0].isdigit():
        candidate = f"context_{candidate}"
    return candidate if re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", candidate) else None


def _infer_output_source_from_contract(contract: dict[str, Any]) -> str | None:
    candidates = [
        contract.get("source"),
        contract.get("context_key"),
        contract.get("name"),
    ]
    for candidate in candidates:
        normalized = _normalize_string(candidate)
        if not normalized:
            continue
        alias_key = _derive_context_key(normalized) or normalized.strip().casefold()
        resolved = _OUTPUT_SOURCE_ALIASES.get(alias_key)
        if resolved is not None:
            return resolved
    return None


def _stabilize_contracts_from_current(
    *,
    generated_contracts: list[Any],
    current_contracts: list[Any],
    label: str,
    active_focus_reasons: list[DslGenerationRejectionReasonCode],
    normalization_notes: list[str],
) -> tuple[list[Any], bool]:
    if "bad_contracts" not in active_focus_reasons or not generated_contracts or not current_contracts:
        return generated_contracts, False

    current_by_context_key = {contract.context_key: contract for contract in current_contracts}
    merged_contracts: list[Any] = []
    changed = False
    existing_keys: set[str] = set()

    for contract in generated_contracts:
        current_contract = current_by_context_key.get(contract.context_key)
        updated_contract = contract
        updates: dict[str, Any] = {}
        if current_contract is not None:
            if _looks_like_generic_contract_name(contract.name) and current_contract.name != contract.name:
                updates["name"] = current_contract.name
                normalization_notes.append(
                    f"{label} {contract.context_key} 的 name 过于泛化，已沿用当前 DSL 中同 context_key 契约的名称。"
                )
            if contract.description is None and current_contract.description:
                updates["description"] = current_contract.description
                normalization_notes.append(
                    f"{label} {contract.context_key} 缺少 description，已沿用当前 DSL 中同 context_key 契约的描述。"
                )
            if contract.value_type != current_contract.value_type:
                updates["value_type"] = current_contract.value_type
                normalization_notes.append(
                    f"{label} {contract.context_key} 的 value_type 与当前 DSL 稳定语义不一致，已沿用当前 DSL 中同 context_key 契约的 value_type。"
                )
            if hasattr(contract, "required") and contract.required != current_contract.required:
                updates["required"] = current_contract.required
                normalization_notes.append(
                    f"{label} {contract.context_key} 的 required 与当前 DSL 稳定语义不一致，已沿用当前 DSL 中同 context_key 契约的 required。"
                )
            current_source = getattr(current_contract, "source", None)
            if hasattr(contract, "source") and current_source is not None and contract.source != current_source:
                updates["source"] = current_source
                normalization_notes.append(
                    f"{label} {contract.context_key} 的 source 与当前 DSL 稳定语义不一致，已沿用当前 DSL 中同 context_key 契约的 source。"
                )
        if updates:
            updated_contract = contract.model_copy(update=updates)
            changed = True
        merged_contracts.append(updated_contract)
        existing_keys.add(updated_contract.context_key)

    appended_count = 0
    for current_contract in current_contracts:
        if current_contract.context_key in existing_keys:
            continue
        merged_contracts.append(current_contract)
        existing_keys.add(current_contract.context_key)
        appended_count += 1

    if appended_count > 0:
        changed = True
        normalization_notes.append(f"AI 草案未覆盖全部稳定{label}，已补回当前 DSL 中的 {appended_count} 个契约。")

    return merged_contracts, changed


def _apply_context_mismatch_repairs(
    *,
    normalized_name: str,
    normalized_description: str | None,
    current_case: DSLCase | None,
    active_focus_reasons: list[DslGenerationRejectionReasonCode],
    normalization_notes: list[str],
) -> tuple[str, str | None]:
    if current_case is None or "context_mismatch" not in active_focus_reasons:
        return normalized_name, normalized_description

    repaired_name = normalized_name
    repaired_description = normalized_description
    if _looks_like_generic_case_name(repaired_name) and current_case.name != repaired_name:
        repaired_name = current_case.name
        normalization_notes.append("AI 草案的名称过于泛化，已沿用当前 DSL 的名称以保持业务上下文。")
    if (
        current_case.description
        and (repaired_description is None or _looks_like_generic_case_description(repaired_description))
    ):
        repaired_description = current_case.description
        if normalized_description is None:
            normalization_notes.append("AI 草案未提供 description，已沿用当前 DSL 的描述以保持业务上下文。")
        else:
            normalization_notes.append("AI 草案的 description 过于泛化，已沿用当前 DSL 的描述以保持业务上下文。")
    return repaired_name, repaired_description


def _looks_like_generic_case_name(name: str) -> bool:
    normalized = name.strip().casefold()
    return normalized in _GENERIC_CASE_NAMES or normalized.startswith("ai ")


def _looks_like_generic_case_description(description: str) -> bool:
    normalized = description.strip().casefold()
    return normalized in _GENERIC_CASE_DESCRIPTIONS or normalized.startswith("ai ")


def _looks_like_generic_contract_name(name: str) -> bool:
    normalized = name.strip().casefold()
    return normalized in _GENERIC_CONTRACT_NAMES or normalized.startswith("field_") or normalized.startswith("value_")


def resolve_active_governance_reasons(
    *,
    governance_focus_reasons: list[DslGenerationRejectionReasonCode] | None,
    retry_reason_code: DslGenerationRejectionReasonCode | None,
) -> list[DslGenerationRejectionReasonCode]:
    active_reasons = list(governance_focus_reasons or DEFAULT_GOVERNANCE_REJECTION_REASONS)
    if retry_reason_code is not None and retry_reason_code not in active_reasons:
        active_reasons.append(retry_reason_code)
    return active_reasons


def _coerce_bool(value: Any) -> bool | None:
    if isinstance(value, bool):
        return value
    if isinstance(value, str):
        normalized = value.strip().casefold()
        if normalized in {"true", "1", "yes", "on"}:
            return True
        if normalized in {"false", "0", "no", "off"}:
            return False
        return None
    if isinstance(value, int):
        if value == 1:
            return True
        if value == 0:
            return False
    return None


def resolve_generation_mode(
    request_generation_mode: GenerateDslMode | None,
    *,
    settings=None,
) -> GenerateDslMode:
    if request_generation_mode is not None:
        return request_generation_mode
    active_settings = settings or get_settings()
    return "strict_steps_only" if active_settings.ai_dsl_strict_mode else "draft"


def _append_unique_lines(system_lines: list[str], extra_lines: list[str]) -> None:
    for line in extra_lines:
        if line not in system_lines:
            system_lines.append(line)


def _collect_reason_strategy_lines(
    reasons: list[DslGenerationRejectionReasonCode],
) -> list[str]:
    lines: list[str] = []
    for reason in reasons:
        for line in REJECTION_REASON_STRATEGIES.get(reason, []):
            if line not in lines:
                lines.append(line)
    return lines


def resolve_generation_profile(
    *,
    payload: GenerateDslRequest,
    generation_mode: GenerateDslMode,
) -> tuple[DslGenerationPromptVariant, DslGenerationContextProfile]:
    if payload.import_mode == "contracts_only":
        return "contracts_focus", "contracts_focus"
    if generation_mode == "strict_steps_only" and payload.current_steps:
        return "repair_steps", "repair_steps"
    if payload.current_case is not None:
        return "rewrite_from_case", "rewrite_from_case"
    return "baseline_draft", "blank_request"


def _normalize_string(value: Any) -> str | None:
    if not isinstance(value, str):
        return None
    normalized = value.strip()
    return normalized or None


def _normalize_optional_string(value: Any) -> str | None:
    return _normalize_string(value)


def _promote_contract_alias(
    repaired: dict[str, Any],
    *,
    canonical_key: str,
    index: int,
    context: ContractNormalizationContext,
) -> Any:
    alias_keys = _CONTRACT_FIELD_ALIASES[canonical_key]
    alias_key = next((key for key in alias_keys if key in repaired), None)
    if alias_key is None:
        return repaired.get(canonical_key)
    value = repaired.get(alias_key)
    if (
        alias_key != canonical_key
        and value not in (None, "")
        and repaired.get(canonical_key) in (None, "")
    ):
        repaired[canonical_key] = value
        context.normalization_notes.append(
            f"{context.label} #{index} 的 {alias_key} 已映射为 {canonical_key}。"
        )
    if alias_key != canonical_key:
        repaired.pop(alias_key, None)
    return repaired.get(canonical_key)


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
    }
    if _should_use_glm_chat_completion(base_url=base_url, model=model):
        payload["thinking"] = {"type": "enabled"}
        payload["max_tokens"] = 65536
        payload["temperature"] = 1.0
    else:
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
    with request.urlopen(http_request, timeout=timeout_seconds) as response:
        raw_body = response.read()
        response_text = raw_body.decode("utf-8", errors="replace")
        content_type = ""
        if hasattr(response, "headers") and response.headers is not None:
            content_type = response.headers.get("Content-Type", "")
        try:
            raw_payload = json.loads(response_text)
        except json.JSONDecodeError as exc:
            raise DslGenerationError(
                _build_non_json_response_error(
                    endpoint=endpoint,
                    base_url=base_url,
                    content_type=content_type,
                    response_text=response_text,
                )
            ) from exc

    return _extract_message_content(raw_payload)


def _build_non_json_response_error(
    *,
    endpoint: str,
    base_url: str,
    content_type: str,
    response_text: str,
) -> str:
    normalized_preview = re.sub(r"\s+", " ", response_text).strip()[:160]
    hint = ""
    if _looks_like_html_response(response_text):
        hint = " 响应看起来像 HTML 页面，请检查 AI_DSL_BASE_URL 是否指向了真正的 OpenAI 兼容 API 根路径。"
        if not base_url.rstrip("/").endswith("/v1"):
            hint += " 当前 base_url 末尾不包含 /v1。"
    return (
        "AI DSL 生成接口返回了无法解析的非 JSON 响应。"
        f" endpoint={endpoint}"
        f" content_type={content_type or 'unknown'}"
        f" preview={normalized_preview or '<empty>'}.{hint}"
    )


def _looks_like_html_response(response_text: str) -> bool:
    normalized = response_text.lstrip().casefold()
    return normalized.startswith("<!doctype html") or normalized.startswith("<html")


def _should_use_glm_chat_completion(*, base_url: str, model: str) -> bool:
    normalized_base_url = base_url.strip().casefold()
    normalized_model = model.strip().casefold()
    return "open.bigmodel.cn" in normalized_base_url or normalized_model.startswith("glm-")


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
