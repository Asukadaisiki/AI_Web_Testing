"""AI-assisted DSL generation helpers."""

from __future__ import annotations

import json
import logging
import re
import socket
import time as _time
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass
from typing import Any
from urllib import request
from urllib.error import HTTPError, URLError

from pydantic import TypeAdapter, ValidationError

from app.ai.page_explorer import format_elements_for_prompt
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


class DslGenerationNetworkError(DslGenerationError):
    """Raised when the LLM HTTP endpoint is unreachable (DNS/TCP/connection timeout)."""


def _is_transient_network_error(exc: BaseException) -> bool:
    """Return True if exc represents a retriable transient failure."""
    if isinstance(exc, socket.timeout):
        return True
    if isinstance(exc, HTTPError):
        return exc.code == 429 or 500 <= exc.code < 600
    if isinstance(exc, URLError):
        reason = exc.reason
        if isinstance(reason, (socket.timeout, ConnectionError, TimeoutError, OSError)):
            return True
        return True  # generic URLError (DNS, etc.) — also retry once
    if isinstance(exc, (ConnectionError, TimeoutError)):
        return True
    return False


def _urlopen_with_retry(
    http_request: "request.Request",
    *,
    timeout_seconds: float,
    max_retries: int = 2,
    initial_backoff: float = 1.0,
):
    """urlopen with exponential backoff on transient network errors.

    Returns the raw response on success. Raises the last exception otherwise.
    On non-retriable errors (4xx, JSON errors), raises immediately without retry.
    """
    attempt = 0
    while True:
        try:
            return request.urlopen(http_request, timeout=timeout_seconds)
        except Exception as exc:
            if not _is_transient_network_error(exc) or attempt >= max_retries:
                raise
            wait = initial_backoff * (2 ** attempt)
            logger.warning(
                "LLM call attempt %d failed (%s: %s); retrying in %.1fs",
                attempt + 1, type(exc).__name__, exc, wait,
            )
            _time.sleep(wait)
            attempt += 1


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
AI_DSL_PROMPT_VERSION = "2026-05-06.assertion-and-navigation-v3"
_BASE_SYSTEM_PROMPT_LINES = [
    "You generate structured web testing DSL in JSON only. Return one JSON object with keys:",
    "name, description, base_url, input_contract, output_contract, steps.",
    "Generate ONLY steps — no markdown, no explanation, no extra text.",
    "",
    "━━━ CRITICAL RULES — VIOLATING ANY OF THESE = FAILED DRAFT ━━━",
    "",
    "## R1: PAGE NAVIGATION — DO NOT SKIP NAVIGATION STEPS",
    "MUST generate click or goto to reach a page BEFORE any wait_for/input/capture_text on that page.",
    "Each page in the flow requires a navigation step (click or goto) to get there.",
    "Example WRONG: goto / → wait_for Email Address → input Email Address.  (Email is on /login, not on /!)",
    "Example CORRECT: goto / → click \"Signup / Login\" → wait_for \"Login to your account\" → input Email Address.",
    "The first step after goto / MUST be a click to navigate to the starting page (login, products, etc.),",
    "UNLESS the very first step of the flow happens on the homepage itself.",
    "",
    "## R2: NO wait_for WITHOUT PRECEDING NAVIGATION",
    "Every wait_for MUST be preceded by a click or goto in the immediately prior step(s).",
    "wait_for \"ALL PRODUCTS\" without a prior click \"Products\" is INVALID.",
    "wait_for \"Shopping Cart\" without a prior click \"View Cart\" or goto /view_cart is INVALID.",
    "For every wait_for, there must be a click/goto within the last 2 steps that navigates to that page.",
    "",
    "## R3: MODIFY-THEN-ASSERT — MUST INPUT BEFORE ASSERTING CHANGED VALUES",
    "When the flow says to change a value (quantity, price, etc.):",
    "  1. input the new value (action=\"input\", target=<element>, value=<new value>, trigger=\"Enter\")",
    "  2. wait_for the UI to reflect the change",
    "  3. assert_text to verify the new value",
    "WRONG: directly assert_text value='2' with no input step. The field still has value '1'.",
    "CORRECT: input target=\"数量按钮\" value=\"2\" trigger=\"Enter\" → wait_for \"Rs. 1400\" → assert_text value=\"Rs. 1400\".",
    "",
    "## R4: USE trigger FIELD FOR KEYBOARD EVENTS",
    "When an input step needs Enter/Tab/Escape to activate the change, set the trigger field.",
    "trigger=\"Enter\" fires Enter after fill (submits form, triggers JS change event).",
    "trigger=\"Tab\" moves to next field.",
    "Do NOT generate separate keyboard steps. The executor handles trigger automatically.",
    "",
    "## R5: CAPTURE MUST ASSERT",
    "Every capture_text step MUST be followed by at least one assert_text referencing the captured variable.",
    "capture_text reads data but does NOT verify it. assert_text confirms the captured value is correct.",
    "assert_text target must be page text (e.g., \"Blue Top\"), value must be the variable (e.g., \"${product_a_name}\").",
    "Do NOT put ${var} in target — target is for locating the element, value is the expected content.",
    "",
    "## R6: FORM FIELD COVERAGE",
    "Generate a step for EVERY form field mentioned in the prompt.",
    "Dropdown (<select>): action=\"input\" target=<field label> value=<option text>.",
    "Checkbox: action=\"click\" target=<checkbox label>.",
    "Review steps against the prompt before outputting. Missing fields = invalid DSL.",
    "",
    "## R7: STEP VERIFICATION",
    "After state-changing actions, add verification:",
    "  Navigation click → wait_for an element unique to the target page.",
    "  Form submit → wait_for success message or new page element.",
    "  Add to Cart → wait_for \"Added!\" or modal confirmation.",
    "CORRECT: click \"Signup\" → wait_for \"Enter Account Information\" → input \"Password\".",
    "WRONG: click \"Signup\" → immediately input \"Password\" (no verification form loaded).",
    "",
    "## R8: PAGE_STATE ISOLATION",
    "When elements are grouped by page_state (S0, S1, ...), each step references elements from its state only.",
    "Never use S1 elements in an S0 step.",
    "",
    "━━━ FORMAT RULES ━━━",
    "",
    "## target format",
    "Use EXACT visible text/label/placeholder as target (plain string), NOT CSS selectors.",
    "CORRECT: \"Login\", \"Email Address\", \"Signup / Login\", \"Submit\", \"Cart\".",
    "WRONG: \"input[placeholder='Email Address']\", \"button.login\", \"a[href='/login']\".",
    "Only use CSS/XPath (css=/xpath=/ #id .class) when no visible text exists.",
    "Never invent compound formats like 'tag[placeholder=val]'.",
    "",
    "## variable format",
    "Reference input_contract variables with ${context_key} syntax: ${login_email}.",
    "Do not hardcode test data values when a variable exists.",
    "",
    "## JSON structure",
    "Do not include markdown fences. Keep contracts/steps as arrays even when empty.",
    "Do not wrap DSL under other keys (case, data, result, draft).",
    "context_key must be stable snake_case matching ^[A-Za-z_][A-Za-z0-9_]*$.",
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
    "- 【同类重复元素消歧 — 适用于所有动态值】页面元素清单已按视觉分组（### Group Label），每组是一个 UI 区块（产品卡片、表单等）。当页面上有多个相同或相似的元素时，必须用父元素上下文来精确指定目标。这不仅适用于按钮，也适用于价格、数量、名称等任何可能重复的文本。规则：如果该值属于某个产品卡片（如 Blue Top 卡片内的价格 Rs. 500、数量 1），必须使用 `\"父元素文本\" 附近的 \"子元素文本\"` 格式。错误示例：capture_text \"Rs. 400\"（会匹配多个产品的价格）；正确示例：capture_text \"Men Tshirt 附近的 Rs. 400\"。绝对不要使用 `section:nth-of-type(N) > div:nth-of-type(N)`。唯一例外：购物车页面只有已添加的商品，可以直接用价格文本。",
    "- 【置信度自评】对每个包含 target 的 step，添加 locator_confidence 字段：high（目标有唯一 data-testid/aria-label/text）、medium（有稳定属性但存在 2-3 个同类）、low（只能靠 XPath 位置或无区分属性的多个同类元素）。",
    "- 表单字段覆盖：必须为 prompt 中提到的每个表单字段生成对应步骤。下拉框用 input action（target 为字段标签，value 为选项文本），复选框用 click action（target 为复选框标签）。输出前检查是否有遗漏字段。",
    "- 当 input_contract 中定义了变量（如 context_key: login_email），step 的 value 字段必须用 ${context_key} 格式引用（如 \"${login_email}\"），不要硬编码值或使用其他占位符格式（如 {{}}、%%、<>）。",
    "- 如果需要明确指定定位策略，可在 step 中添加 target_strategy 字段（可选值：css, xpath, data-testid, element_id, tag, semantic）。不填则自动推断。",
    "- 【测试常识 — 输入后确认】修改表单字段（如数量、价格、搜索框）的值后，页面通常需要键盘事件才能触发更新。仅 input 步骤的 fill 操作可能不会触发 JavaScript 的 change/update 事件。因此：1) 修改数量/价格后，必须在 input 步骤后添加 wait_for 等待更新结果出现；2) 如果 wait_for 的目标是总价、计算结果等动态值，使用具体的预期值作为 target（如 \"Rs. 1400\"）而非 CSS 选择器。正确示例：input value='2' → wait_for \"Rs. 1400\" → assert_text。",
    "- base_url 应为站点根地址（如 https://example.com），页面路径放在 goto 步骤中（如 /login）。不要将完整页面 URL 填入 base_url。",
    "- 生成前评估测试信息完整性：前置条件（系统初始状态）、入口（目标页面 URL 或导航路径）、操作步骤、预期结果。如果描述中缺少入口信息，通过 base_url + goto 步骤明确入口。",
    "- 【页面导航完整性】每个页面的元素只能在该页面加载后才能操作。进入新页面必须通过 click/goto 步骤。例如：登录页面的邮箱输入框必须在 click \"Signup / Login\"（或 goto /login）之后才能操作，不能从首页直接 input \"Email Address\"。确保步骤顺序与实际页面跳转逻辑一致。",
    "- 【capture 必须 assert】使用 capture_text 提取数据后，必须在后续步骤中用 assert_text 验证该值。capture 只是读取数据，不能发现任何 bug。每条核心断言（如价格一致性、跨页面数据匹配）必须有对应的 assert_text 步骤。capture_text 捕获的 ${context_key} 必须在至少一个 assert_text 中被引用。",
    "- 【修改值必须先 input】如果测试流程要求修改某个字段的值（如修改数量为 2、修改价格为 100），必须先有 input 步骤执行修改，再有 assert_text 验证修改结果。不能跳过 input 直接 assert 修改后的值。错误示例：直接 assert_text value='2' 但没有 input value='2'。正确示例：input value='2' → wait_for → assert_text value='2'。",
    "- 【trigger 字段】如果 input 步骤后需要键盘事件触发更新（如购物车数量修改后按 Enter），在 input 步骤中添加 trigger='Enter'（或 trigger='Tab'、trigger='Escape'）。不要单独生成键盘事件步骤，执行器会自动处理。",
    "- 【页面状态隔离】如果页面元素按\"页面状态 S0/S1...\"分组，每个状态的步骤只能使用该状态的元素。不要在 S0 的步骤中使用 S1 的元素 target。",
]
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
    thinking_enabled = _should_enable_thinking_mode(base_url=base_url, model=model)
    logger.info("DSL _call_llm: model=%s, thinking=%s, base_url=%s", model, thinking_enabled, base_url)
    if thinking_enabled:
        payload["thinking"] = {"type": "enabled", "effort": "medium"}
        payload["max_tokens"] = 65536
        payload["temperature"] = 0.0
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
    with _urlopen_with_retry(http_request, timeout_seconds=timeout_seconds) as response:
        raw_body = response.read()
        # Two-pass decode to eliminate lone surrogates at byte level
        response_text = raw_body.decode("utf-8", errors="surrogateescape")
        response_text = response_text.encode("utf-8", errors="replace").decode("utf-8", errors="replace")
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

    _log_dsl_cache_usage(raw_payload)
    return _extract_message_content(raw_payload)


def _log_dsl_cache_usage(raw_payload: dict[str, Any]) -> None:
    """Log prompt-cache hit/miss for DeepSeek-style LLM responses.

    Restored after being orphaned by commit 8d92654 (governance cleanup deleted
    the function but kept two callers). No-op for providers that don't report
    cache usage in the response.
    """
    if not isinstance(raw_payload, dict):
        return
    usage = raw_payload.get("usage", {}) or {}
    if not isinstance(usage, dict):
        return
    hit = usage.get("prompt_cache_hit_tokens", 0) or 0
    miss = usage.get("prompt_cache_miss_tokens", 0) or 0
    if hit or miss:
        ratio = hit / (hit + miss) * 100 if (hit + miss) > 0 else 0
        logger.info(
            "DSL cache: hit=%d miss=%d ratio=%.0f%% total=%d completion=%d",
            hit, miss, ratio,
            usage.get("prompt_tokens", 0), usage.get("completion_tokens", 0),
        )


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


def _call_dsl_flash_llm(
    *,
    messages: list[dict[str, Any]],
    settings=None,
    timeout_seconds: float = 60.0,
) -> str:
    """Call a fast/flash LLM for segmented DSL generation (no thinking mode).

    Uses ``ai_dsl_flash_*`` config, falling back to the main DSL model.
    """
    if settings is None:
        settings = get_settings()

    api_key = settings.ai_dsl_api_key or ""
    model = (
        getattr(settings, "ai_dsl_flash_model", None)
        or settings.ai_dsl_model
        or ""
    )
    base_url = settings.ai_dsl_base_url

    # Log LLM configuration
    logger.info(
        "DSL _call_dsl_flash_llm: model=%s, base_url=%s, has_api_key=%s, timeout=%.1fs",
        model or "(empty)", base_url, bool(api_key), timeout_seconds,
    )

    if not api_key:
        raise DslGenerationConfigError(
            "AI DSL 生成失败：未配置 API Key。请设置 AI_DSL_API_KEY 环境变量。"
        )
    if not model:
        raise DslGenerationConfigError(
            "AI DSL 生成失败：未配置模型。请设置 AI_DSL_FLASH_MODEL 或 AI_DSL_MODEL 环境变量。"
        )

    payload: dict[str, Any] = {
        "model": model,
        "messages": messages,
        "max_tokens": 16384,
        "temperature": 0.0,
        "response_format": {"type": "json_object"},
    }
    endpoint = f"{base_url.rstrip('/')}/chat/completions"

    logger.debug("DSL LLM endpoint: %s", endpoint)

    http_request = request.Request(
        endpoint,
        data=json.dumps(payload).encode("utf-8"),
        headers={
            "Authorization": f"Bearer {api_key}",
            "Content-Type": "application/json",
        },
        method="POST",
    )
    try:
        with _urlopen_with_retry(http_request, timeout_seconds=timeout_seconds) as response:
            raw_body = response.read()
            response_text = raw_body.decode("utf-8", errors="replace")
            try:
                raw_payload = json.loads(response_text)
            except json.JSONDecodeError as exc:
                raise DslGenerationError(
                    f"Flash DSL generation returned non-JSON response: {response_text[:500]}"
                ) from exc
    except (URLError, socket.timeout, ConnectionError, TimeoutError) as exc:
        endpoint_host = base_url.rstrip("/").split("/")[-1] if base_url else "(unknown)"
        logger.error("DSL LLM call failed (network): %s", exc)
        raise DslGenerationNetworkError(
            f"AI DSL 生成失败：无法连接到 LLM API（{endpoint_host}）。"
            f"错误：{type(exc).__name__}: {exc}。"
            f"请检查网络连通性、DNS 解析或代理设置。"
        ) from exc
    except Exception as exc:
        logger.error("DSL LLM call failed: %s", exc)
        raise

    _log_dsl_cache_usage(raw_payload)
    return _extract_message_content(raw_payload)


def _build_segment_prompt(
    scenario_prompt: str,
    page_state: str,
    seg_steps: list[dict[str, Any]],
    elements: list[dict[str, Any]],
    base_url: str,
    page_elements: str | None = None,
) -> str:
    """Build a focused prompt for a single page_state segment."""
    step_desc_lines: list[str] = []
    for s in seg_steps:
        act = s.get("action", "?")
        tgt = s.get("target", "") or ""
        val = s.get("value", "")
        trig = s.get("trigger", "")
        extra = []
        if val:
            extra.append(f"value='{val}'")
        if trig:
            extra.append(f"trigger='{trig}'")
        step_desc_lines.append(f"  {s.get('step_index','?')}. {act} target='{tgt}' {' '.join(extra)}")

    if step_desc_lines:
        actions_section = "Actions on this page:\n" + "\n".join(step_desc_lines)
    else:
        actions_section = (
            "Actions on this page: (none provided — derive complete DSL steps from the scenario "
            "and available elements below; cover navigation, interactions, and assertions described "
            "in the scenario)"
        )

    elem_text = format_elements_for_prompt(elements) if elements else "(no elements)"

    # Add page_elements (formatted DOM text) if available
    page_elements_section = ""
    if page_elements:
        page_elements_section = f"\n\nFormatted DOM elements:\n{page_elements}\n"

    return (
        f"Generate DSL steps for page state **{page_state}** only.\n\n"
        f"Scenario: {scenario_prompt}\n\n"
        f"{actions_section}\n\n"
        f"Available elements:\n{elem_text}\n"
        f"{page_elements_section}\n"
        f"Rules:\n"
        f"- Return valid JSON with 'steps' array and 'base_url'.\n"
        f"- base_url: {base_url}\n"
        f"- Only generate steps for THIS page state ({page_state}).\n"
        f"- Use exact visible text from the element list as target.\n"
        f"- If an input step has trigger=Enter/Tab, include the trigger field.\n"
        f"- Every capture_text must be followed by assert_text.\n"
        f"- Limit to 8-12 steps for this segment."
    )


SUPPORTED_DSL_ACTIONS = [
    "goto", "click", "input", "wait_for",
    "assert_text", "assert_url_contains", "capture_text",
]


def generate_segmented_case_draft(
    *,
    payload: "GenerateDslRequest",
    flow_steps: list[dict[str, Any]],
    page_elements_by_state: dict[str, list[dict[str, Any]]],
) -> "tuple[DSLCase, list[str], list[str], GenerateDslMeta]":
    """Generate DSL by splitting the scenario into page_state segments.

    Each segment is processed by a flash LLM call (no thinking mode).
    Segments run in parallel via ThreadPoolExecutor, then steps are merged
    in page_state order (S0, S1, ...).
    """
    settings = get_settings()
    if not settings.enable_ai_dsl_generate:
        raise DslGenerationConfigError(
            "AI DSL 生成功能未开启。请设置 ENABLE_AI_DSL_GENERATE=true。"
        )

    # Log generation start
    logger.info(
        "DSL segmented generation start: prompt_len=%d, base_url=%s, flow_steps=%d, page_states=%d, has_page_elements=%s",
        len(payload.prompt),
        payload.base_url,
        len(flow_steps),
        len(page_elements_by_state),
        bool(payload.page_elements),
    )

    # Group flow_steps by page_state
    groups: dict[str, list[dict[str, Any]]] = {}
    for fs in flow_steps:
        ps = str(fs.get("page_state", "S0") or "S0")
        groups.setdefault(ps, []).append(fs)

    # Fallback: if flow_steps is empty but we have page_elements_by_state,
    # use those page_states with empty seg_steps. This lets the LLM derive
    # steps from the scenario prompt + element list when the planning agent
    # failed to produce structured flow_steps (e.g., safety-cap fallback plan).
    if not groups and page_elements_by_state:
        for ps in page_elements_by_state.keys():
            groups.setdefault(ps or "S0", [])
        logger.info(
            "flow_steps empty; deriving page_states from page_elements_by_state: %s",
            list(groups.keys()),
        )

    sorted_states = sorted(groups.keys())
    logger.info("Page states: %s, groups: %s", sorted_states, {k: len(v) for k, v in groups.items()})

    all_warnings: list[str] = []
    all_notes: list[str] = []
    merged_steps: list[dict[str, Any]] = []
    base_url = payload.base_url or None

    def _generate_segment(state: str, steps: list[dict[str, Any]]) -> tuple[str, list[dict[str, Any]]]:
        elements = page_elements_by_state.get(state, [])
        logger.info(
            "Generating segment %s: steps=%d, elements=%d, has_page_elements=%s",
            state, len(steps), len(elements), bool(payload.page_elements),
        )
        seg_prompt = _build_segment_prompt(
            scenario_prompt=payload.prompt.strip(),
            page_state=state,
            seg_steps=steps,
            elements=elements,
            base_url=base_url,
            page_elements=payload.page_elements,
        )
        logger.debug("Segment %s prompt length: %d", state, len(seg_prompt))
        messages = [
            {
                "role": "system",
                "content": (
                    "You generate structured web testing DSL in JSON only. "
                    "Return exactly: {\"steps\": [...], \"base_url\": \"...\"}"
                ),
            },
            {"role": "user", "content": seg_prompt},
        ]
        response = _call_dsl_flash_llm(
            messages=messages,
            settings=settings,
            timeout_seconds=max(30.0, getattr(settings, "ai_dsl_flash_timeout_ms", 180000) / 1000),
        )
        cleaned = _extract_json_object(response)
        logger.debug("Segment %s response length: %d", state, len(cleaned))
        raw = json.loads(cleaned)
        if not isinstance(raw, dict):
            raise DslGenerationError(f"Segment {state}: response is not a JSON object")
        steps_result = raw.get("steps", []) or raw.get("data", {}).get("steps", []) or []
        logger.info("Segment %s generated %d steps", state, len(steps_result))
        return state, steps_result

    # Parallel execution
    segment_results: dict[str, list[dict[str, Any]]] = {}
    with ThreadPoolExecutor(max_workers=min(4, len(sorted_states))) as executor:
        futures = {
            executor.submit(_generate_segment, state, groups[state]): state
            for state in sorted_states
        }
        for future in as_completed(futures):
            state = futures[future]
            try:
                s, seg_steps = future.result()
                segment_results[s] = seg_steps
                logger.info(
                    "Segment %s generated: %d steps", s, len(seg_steps),
                )
            except Exception as exc:
                logger.warning("Segment %s failed: %s", state, exc)
                all_warnings.append(f"Segment {state} generation failed: {exc}")
                segment_results[state] = []

    # Merge in page_state order
    for state in sorted_states:
        merged_steps.extend(segment_results.get(state, []))

    # Rewrite step_index
    for i, s in enumerate(merged_steps):
        s["step_index"] = i + 1

    # Log merge result
    logger.info(
        "DSL segmented generation complete: states=%d, total_steps=%d, warnings=%d",
        len(sorted_states), len(merged_steps), len(all_warnings),
    )
    if all_warnings:
        logger.warning("Generation warnings: %s", all_warnings)

    # Build normalized case dict
    normalized_case = {
        "name": payload.prompt.strip()[:200] or "AI 生成用例",
        "description": payload.prompt.strip()[:500],
        "base_url": base_url,
        "input_contract": [],
        "output_contract": [],
        "steps": merged_steps,
    }

    all_notes.append(f"分段生成：{len(sorted_states)} 个页面状态，共 {len(merged_steps)} 步")

    if not base_url:
        logger.error("DSL generation failed: base_url is empty")
        raise DslGenerationError(
            "DSL 生成失败：缺少入口 URL（base_url 为空）。"
            "请确认 AI 已从测试需求中提取到 entry_url_or_page 字段。"
        )
    if not merged_steps:
        logger.error("DSL generation failed: no steps generated from %d segments", len(sorted_states))
        # Distinguish network failures from element-collection failures so the
        # user gets an accurate, actionable error message (Bug B).
        network_keywords = (
            "无法连接到 LLM API",
            "DslGenerationNetworkError",
            "WinError 10060",
            "urlopen error",
            "Connection",
            "timeout",
            "TimedOut",
            "TimeoutError",
            "Name or service not known",
            "Temporary failure in name resolution",
        )
        network_failures = sum(
            1 for w in all_warnings if any(kw in w for kw in network_keywords)
        )
        if all_warnings and network_failures == len(all_warnings):
            raise DslGenerationNetworkError(
                f"DSL 生成失败：所有 {len(sorted_states)} 个分段均因网络问题未能调用到 LLM API。"
                f"已采集页面元素正常，问题在 LLM 接口连通性。"
                f"首个错误：{all_warnings[0]}。"
                f"请检查 AI_DSL_BASE_URL、网络代理或 DNS。"
            )
        raise DslGenerationError(
            f"DSL 生成失败：所有 {len(sorted_states)} 个页面状态分段均未生成步骤。"
            "请检查页面元素采集是否正常，或入口 URL 是否可达。"
        )

    case = DSLCase.model_validate(normalized_case)
    generation_meta = GenerateDslMeta(
        model=getattr(settings, "ai_dsl_flash_model", None) or settings.ai_dsl_model or "",
        generation_mode="draft",
        import_mode=payload.import_mode,
        prompt_variant="baseline_draft",
        context_profile="blank_request",
        active_governance_focus_reasons=["context_mismatch", "bad_contracts"],
        risk_flags=[],
        base_url_source="ai_output" if base_url else "request",
        base_url_backfilled=False,
        repaired_invalid_actions=0,
        removed_invalid_steps=0,
        removed_invalid_contracts=0,
        preserve_contracts_applied=False,
        used_current_case_context=False,
        used_current_steps_context=False,
    )

    return case, all_warnings, all_notes, generation_meta


def _should_enable_thinking_mode(*, base_url: str, model: str) -> bool:
    normalized_base_url = base_url.strip().casefold()
    normalized_model = model.strip().casefold()
    return (
        "open.bigmodel.cn" in normalized_base_url
        or normalized_model.startswith("glm-")
        or "deepseek" in normalized_model and "flash" not in normalized_model
    )


def _extract_message_content(payload: dict[str, Any]) -> str:
    message = payload.get("choices", [{}])[0].get("message", {})
    content = message.get("content", "")
    if isinstance(content, str) and content.strip():
        return content
    if isinstance(content, list):
        text_parts: list[str] = []
        for item in content:
            if isinstance(item, dict) and isinstance(item.get("text"), str):
                text_parts.append(item["text"])
        result = "\n".join(text_parts)
        if result.strip():
            return result
    # Fallback to reasoning_content when thinking mode produces no content
    reasoning = message.get("reasoning_content")
    if isinstance(reasoning, str) and reasoning.strip():
        logger.warning("LLM returned empty content, falling back to reasoning_content (%d chars)", len(reasoning))
        return reasoning
    if isinstance(content, str):
        return content
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


def _regen_segment(
    *,
    scenario_key: str,
    page_state: str,
    missing_targets: list[str],
    a11y_nodes: list[dict[str, Any]],
    base_url: str,
) -> list[dict[str, Any]]:
    """Regenerate steps for one page_state segment after preflight finds missing targets."""
    node_lines: list[str] = []
    for n in a11y_nodes:
        node_lines.append(f"  - role={n['role']} name=\"{n['name']}\" id={n['node_id']}")

    regen_prompt = (
        f"The previous DSL generation used targets that do not exist on the page:\n"
        f"  {', '.join('\"' + t + '\"' for t in missing_targets)}\n\n"
        f"These targets are NOT in the available element list below. "
        f"Please regenerate the steps for page state {page_state}, choosing targets "
        f"ONLY from the following element names:\n\n"
        + "\n".join(node_lines) + "\n\n"
        f"Return valid JSON: {{\"steps\": [...], \"base_url\": \"{base_url}\"}}"
    )
    messages = [
        {"role": "system", "content": "Regenerate DSL steps. Return JSON only. Choose targets from the provided list."},
        {"role": "user", "content": regen_prompt},
    ]
    response = _call_dsl_flash_llm(
        messages=messages,
        settings=get_settings(),
        timeout_seconds=60.0,
    )
    cleaned = _extract_json_object(response)
    raw = json.loads(cleaned)
    if not isinstance(raw, dict):
        raise DslGenerationError(f"Regen segment {page_state}: response is not a JSON object")
    return raw.get("steps", raw.get("data", {}).get("steps", [])) or []
def resolve_prompt_version(payload: GenerateDslRequest) -> str:
    if payload.retry_reason_code is None:
        return AI_DSL_PROMPT_VERSION
    return f"{AI_DSL_PROMPT_VERSION}+retry.{payload.retry_reason_code}"


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


# ── Governance constants (retained for DB schema compatibility) ─
DEFAULT_GOVERNANCE_REJECTION_REASONS: tuple = ("context_mismatch", "bad_contracts")
SETTLED_GOVERNANCE_REJECTION_REASONS: tuple = ("wrong_actions", "invalid_structure")
