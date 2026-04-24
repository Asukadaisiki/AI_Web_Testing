"""LLM-driven ReAct agent for AI test planning."""

from __future__ import annotations

import json
import logging
import re
from typing import Any, Generator
from urllib import request

import httpx
from sqlalchemy.orm import Session

from app.ai.planning_tools import execute_tool
from app.ai.test_planning_prompts import FORCE_GENERATE_HINT, FORCE_GENERATE_MARKER, build_system_prompt
from app.core.config import get_settings
from app.schemas.ai_planning import (
    AIPlanningPlan,
    AIPlanningRequirements,
    AIPlanningScenario,
    AIPlanningTestDataRequirement,
    AIPlanningToolCall,
    AIPlanningTurnResponse,
)


logger = logging.getLogger(__name__)
URL_PATTERN = re.compile(r"https?://[^\s，。；;]+", re.IGNORECASE)
REQUIRED_REQUIREMENT_SLOTS = [
    "app_under_test",
    "business_goal",
    "entry_url_or_page",
    "core_user_flow",
    "main_assertions",
    "test_data_or_account",
    "scope_limits",
]


def run_planning_turn(
    *,
    transcript: list[dict[str, str]],
    existing_requirements: AIPlanningRequirements | None,
    db_session: Session,
    project_id: int,
) -> AIPlanningTurnResponse:
    """Synchronous wrapper around :func:`stream_planning_turn`.

    Consumes all events from the streaming generator and returns the final
    ``AIPlanningTurnResponse``.  Used by REST API fallback.
    """
    stream = stream_planning_turn(
        transcript=transcript,
        existing_requirements=existing_requirements,
        db_session=db_session,
        project_id=project_id,
    )
    while True:
        try:
            next(stream)
        except StopIteration as stop:
            return stop.value


def stream_planning_turn(
    *,
    transcript: list[dict[str, str]],
    existing_requirements: AIPlanningRequirements | None,
    db_session: Session,
    project_id: int,
) -> Generator[dict[str, Any], None, AIPlanningTurnResponse]:
    """Streaming ReAct planning turn.

    Yields status / text_chunk / tool_call events during processing.
    Returns the final ``AIPlanningTurnResponse`` via the generator return value.
    """
    requirements = existing_requirements.model_copy(deep=True) if existing_requirements else AIPlanningRequirements()
    settings = get_settings()
    tool_calls: list[AIPlanningToolCall] = []
    transcript_messages, force_generate = _prepare_transcript_for_llm(transcript)

    if not _planning_llm_enabled(settings):
        response = _run_fallback_turn(
            transcript=transcript,
            requirements=requirements,
            assistant_message=None,
            force_generate=force_generate,
            tool_calls=tool_calls,
        )
        yield {
            "type": "turn_complete",
            "session_status": response.session_status,
            "payload": {
                "assistant_message": response.assistant_message,
                "missing_slots": response.missing_slots,
                "suggested_questions": response.suggested_questions,
                "plan": response.plan.model_dump(mode="json") if response.plan else None,
                "tool_calls": [item.model_dump(mode="json") for item in response.tool_calls],
            },
        }
        return response

    conversation: list[dict[str, str]] = [{"role": "system", "content": build_system_prompt()}, *transcript_messages]
    max_rounds = max(1, settings.ai_planning_max_react_rounds)

    for round_index in range(max_rounds):
        yield {"type": "status", "phase": "thinking", "message": "正在分析需求..."}

        raw_response = ""
        try:
            for event in _stream_planning_llm(
                messages=conversation,
                api_key=settings.ai_planning_api_key or "",
                model=settings.ai_planning_model or "",
                base_url=settings.ai_planning_base_url,
                timeout_seconds=max(1.0, settings.ai_planning_timeout_ms / 1000),
            ):
                if event["type"] == "text_chunk":
                    yield event
                elif event["type"] == "raw_response":
                    raw_response = event["text"]
        except Exception:
            logger.exception("Streaming LLM call failed in round %s", round_index + 1)
            raw_response = ""

        if not raw_response:
            response = _error_response(requirements=requirements, tool_calls=tool_calls)
            yield {
                "type": "turn_complete",
                "session_status": response.session_status,
                "payload": {
                    "assistant_message": response.assistant_message,
                    "missing_slots": response.missing_slots,
                    "suggested_questions": response.suggested_questions,
                    "plan": response.plan.model_dump(mode="json") if response.plan else None,
                    "tool_calls": [item.model_dump(mode="json") for item in response.tool_calls],
                },
            }
            return response

        parsed = _parse_llm_response(raw_response)
        if parsed is None:
            response = _run_fallback_turn(
                transcript=transcript,
                requirements=requirements,
                assistant_message="遇到了解析问题，我先按已有信息给你整理一个测试方案。",
                force_generate=force_generate,
                tool_calls=tool_calls,
            )
            yield {
                "type": "turn_complete",
                "session_status": response.session_status,
                "payload": {
                    "assistant_message": response.assistant_message,
                    "missing_slots": response.missing_slots,
                    "suggested_questions": response.suggested_questions,
                    "plan": response.plan.model_dump(mode="json") if response.plan else None,
                    "tool_calls": [item.model_dump(mode="json") for item in response.tool_calls],
                },
            }
            return response

        _merge_requirements(requirements, parsed.get("collected_info"))
        action = str(parsed.get("action") or "").strip()
        action_input = parsed.get("action_input")
        if not isinstance(action_input, dict):
            action_input = {}

        # --- Force explore_page before generating plan (BUG-052) ---
        if action == "generate_plan" and not _has_explored_pages(tool_calls):
            explored, tool_calls = _auto_explore_entry_url(
                requirements, tool_calls, db_session, project_id,
            )
            if explored:
                yield {"type": "status", "phase": "tool_call", "message": "正在自动采集入口页面元素..."}
                conversation.append(
                    {"role": "system", "content": (
                        "系统已自动采集了入口页面的可交互元素（见上方工具返回结果）。"
                        "请基于这些元素信息重新生成测试方案，"
                        "确保 target 使用元素的实际 label、placeholder 或 id。"
                    )},
                )
                continue

        if force_generate and action != "generate_plan":
            response = _plan_response(
                requirements=requirements,
                plan_payload=action_input,
                assistant_message="我先基于当前已知信息生成一版测试方案，缺失信息会体现在假设和风险里。",
                tool_calls=tool_calls,
            )
            yield {
                "type": "turn_complete",
                "session_status": response.session_status,
                "payload": {
                    "assistant_message": response.assistant_message,
                    "missing_slots": response.missing_slots,
                    "suggested_questions": response.suggested_questions,
                    "plan": response.plan.model_dump(mode="json") if response.plan else None,
                    "tool_calls": [item.model_dump(mode="json") for item in response.tool_calls],
                },
            }
            return response

        if action == "call_tool":
            tool_name = str(action_input.get("tool") or "").strip()
            params = action_input.get("params")
            if not isinstance(params, dict):
                params = {}
            yield {"type": "tool_call_start", "tool": tool_name, "params": params}
            tool_result_text = execute_tool(
                tool_name=tool_name,
                params=params,
                db_session=db_session,
                project_id=project_id,
            )
            parsed_result = _safe_parse_json(tool_result_text)
            tool_calls.append(
                AIPlanningToolCall(
                    tool=tool_name or "unknown_tool",
                    params=params,
                    result=parsed_result,
                )
            )
            yield {"type": "tool_call_end", "tool": tool_name, "result": parsed_result}
            conversation.extend(
                [
                    {"role": "assistant", "content": _normalize_json_text(raw_response)},
                    {
                        "role": "system",
                        "content": f"工具 {tool_name or 'unknown_tool'} 返回结果：{tool_result_text}",
                    },
                ]
            )
            continue

        if action == "generate_plan":
            response = _plan_response(
                requirements=requirements,
                plan_payload=action_input,
                assistant_message="信息已经足够，我先给出结构化测试方案。",
                tool_calls=tool_calls,
            )
            yield {
                "type": "turn_complete",
                "session_status": response.session_status,
                "payload": {
                    "assistant_message": response.assistant_message,
                    "missing_slots": response.missing_slots,
                    "suggested_questions": response.suggested_questions,
                    "plan": response.plan.model_dump(mode="json") if response.plan else None,
                    "tool_calls": [item.model_dump(mode="json") for item in response.tool_calls],
                },
            }
            return response

        # ask_user or unsupported action — ask follow-up
        message = str(action_input.get("message") or "").strip() or _default_followup_question(requirements)
        missing_slots = _collect_missing_slots(requirements)
        response = AIPlanningTurnResponse(
            assistant_message=message,
            session_status="collecting",
            requirements=requirements,
            missing_slots=missing_slots,
            suggested_questions=[message],
            plan=None,
            drafts=[],
            next_action="ask_followup",
            tool_calls=tool_calls,
        )
        yield {
            "type": "turn_complete",
            "session_status": response.session_status,
            "payload": {
                "assistant_message": response.assistant_message,
                "missing_slots": response.missing_slots,
                "suggested_questions": response.suggested_questions,
                "plan": response.plan.model_dump(mode="json") if response.plan else None,
                "tool_calls": [item.model_dump(mode="json") for item in response.tool_calls],
            },
        }
        return response

    # Exhausted all rounds — force-generate a plan
    response = _run_fallback_turn(
        transcript=transcript,
        requirements=requirements,
        assistant_message="我先根据当前上下文整理一版测试方案。",
        force_generate=True,
        tool_calls=tool_calls,
    )
    yield {
        "type": "turn_complete",
        "session_status": response.session_status,
        "payload": {
            "assistant_message": response.assistant_message,
            "missing_slots": response.missing_slots,
            "suggested_questions": response.suggested_questions,
            "plan": response.plan.model_dump(mode="json") if response.plan else None,
            "tool_calls": [item.model_dump(mode="json") for item in response.tool_calls],
        },
    }
    return response


def _planning_llm_enabled(settings: Any) -> bool:
    return bool(
        getattr(settings, "enable_ai_planning", False)
        and getattr(settings, "ai_planning_model", None)
        and getattr(settings, "ai_planning_api_key", None)
    )


def _prepare_transcript_for_llm(transcript: list[dict[str, str]]) -> tuple[list[dict[str, str]], bool]:
    force_generate = False
    prepared: list[dict[str, str]] = []
    for item in transcript:
        role = item.get("role") or "user"
        content = item.get("content") or ""
        if role == "user" and FORCE_GENERATE_MARKER in content:
            force_generate = True
            content = content.replace(FORCE_GENERATE_MARKER, "").strip()
            content = f"{FORCE_GENERATE_HINT}{content}"
        prepared.append({"role": role, "content": content})
    return prepared, force_generate


def _call_llm_with_retry(
    *,
    messages: list[dict[str, Any]],
    api_key: str,
    model: str,
    base_url: str,
    timeout_seconds: float,
) -> str | None:
    for attempt in range(3):
        try:
            return _call_planning_llm(
                messages=messages,
                api_key=api_key,
                model=model,
                base_url=base_url,
                timeout_seconds=timeout_seconds,
            )
        except Exception:
            logger.exception("Planning LLM call failed on attempt %s", attempt + 1)
    return None


def _call_planning_llm(
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


def _stream_planning_llm(
    *,
    messages: list[dict[str, Any]],
    api_key: str,
    model: str,
    base_url: str,
    timeout_seconds: float,
) -> Generator[dict[str, str], None, None]:
    """Yield streaming events from an SSE-based LLM API call.

    Yields:
        ``{"type": "text_chunk", "text": "..."}`` for each incremental chunk.
        ``{"type": "raw_response", "text": "..."}`` once at the end with the full text.
    """
    payload = {
        "model": model,
        "messages": messages,
        "response_format": {"type": "json_object"},
        "stream": True,
    }
    endpoint = f"{base_url.rstrip('/')}/chat/completions"
    full_text: list[str] = []
    with httpx.Client(timeout=timeout_seconds) as client:
        with client.stream(
            "POST",
            endpoint,
            json=payload,
            headers={
                "Authorization": f"Bearer {api_key}",
                "Content-Type": "application/json",
            },
        ) as response:
            response.raise_for_status()
            for raw_line in response.iter_lines():
                if not raw_line or not raw_line.startswith("data: "):
                    continue
                data = raw_line.removeprefix("data: ").strip()
                if data == "[DONE]":
                    break
                try:
                    chunk_payload = json.loads(data)
                    chunk = chunk_payload["choices"][0].get("delta", {}).get("content")
                except (json.JSONDecodeError, KeyError, IndexError):
                    continue
                if chunk:
                    full_text.append(chunk)
                    yield {"type": "text_chunk", "text": chunk}
    yield {"type": "raw_response", "text": "".join(full_text)}


def _parse_llm_response(response_text: str) -> dict[str, Any] | None:
    try:
        payload = json.loads(_extract_json_object(response_text))
    except json.JSONDecodeError:
        logger.warning("Planning LLM returned unparseable JSON: %r", response_text)
        return None
    if not isinstance(payload, dict):
        return None
    return payload


def _merge_requirements(requirements: AIPlanningRequirements, collected_info: Any) -> None:
    if not isinstance(collected_info, dict):
        return
    for field_name in REQUIRED_REQUIREMENT_SLOTS:
        incoming = collected_info.get(field_name)
        if field_name == "main_assertions":
            if isinstance(incoming, list):
                existing = list(requirements.main_assertions)
                for item in incoming:
                    normalized = str(item).strip()
                    if normalized and normalized not in existing:
                        existing.append(normalized)
                requirements.main_assertions = existing
            elif isinstance(incoming, str):
                if incoming.strip() and incoming.strip() not in requirements.main_assertions:
                    requirements.main_assertions.append(incoming.strip())
            continue
        if incoming in (None, ""):
            continue
        current = getattr(requirements, field_name)
        if not current:
            setattr(requirements, field_name, str(incoming).strip())


def _run_fallback_turn(
    *,
    transcript: list[dict[str, str]],
    requirements: AIPlanningRequirements,
    assistant_message: str | None,
    force_generate: bool,
    tool_calls: list[AIPlanningToolCall],
) -> AIPlanningTurnResponse:
    user_text = "\n".join(item["content"] for item in transcript if item.get("role") == "user")
    _fill_requirements_from_text(requirements, user_text)
    missing_slots = _collect_missing_slots(requirements)

    if missing_slots and not force_generate:
        question = assistant_message or _default_followup_question(requirements)
        return AIPlanningTurnResponse(
            assistant_message=question,
            session_status="collecting",
            requirements=requirements,
            missing_slots=missing_slots,
            suggested_questions=[question],
            plan=None,
            drafts=[],
            next_action="ask_followup",
            tool_calls=tool_calls,
        )

    return _plan_response(
        requirements=requirements,
        plan_payload=None,
        assistant_message=assistant_message or "信息已经足够，我先给出结构化测试方案。",
        tool_calls=tool_calls,
    )


def _extract_page_elements(tool_calls: list[AIPlanningToolCall]) -> str | None:
    """Extract formatted DOM elements from the last explore_page or explore_flow tool call."""
    for call in reversed(tool_calls):
        if call.tool in ("explore_page", "explore_flow") and isinstance(call.result, dict):
            formatted = call.result.get("formatted")
            if isinstance(formatted, str) and formatted.strip():
                return formatted
    return None


def _has_explored_pages(tool_calls: list[AIPlanningToolCall]) -> bool:
    """Return True if any explore_page or explore_flow call exists in tool_calls."""
    return any(call.tool in ("explore_page", "explore_flow") for call in tool_calls)


def _auto_explore_entry_url(
    requirements: AIPlanningRequirements,
    tool_calls: list[AIPlanningToolCall],
    db_session: Session,
    project_id: int,
) -> tuple[bool, list[AIPlanningToolCall]]:
    """Auto-invoke explore_page with the entry URL if available.

    Returns (explored, tool_calls) where *explored* indicates whether exploration
    was actually triggered.
    """
    entry_url = requirements.entry_url_or_page
    if not entry_url or not isinstance(entry_url, str):
        return False, tool_calls

    match = URL_PATTERN.search(entry_url)
    if not match:
        return False, tool_calls

    url = match.group(0)
    try:
        tool_result_text = execute_tool(
            tool_name="explore_page",
            params={"url": url},
            db_session=db_session,
            project_id=project_id,
        )
        parsed_result = _safe_parse_json(tool_result_text)
    except Exception as exc:
        logger.warning("Auto-explore failed for url=%s: %s", url, exc)
        parsed_result = {"error": str(exc), "url": url}

    tool_calls.append(
        AIPlanningToolCall(
            tool="explore_page",
            params={"url": url},
            result=parsed_result,
        )
    )
    return True, tool_calls


def _plan_response(
    *,
    requirements: AIPlanningRequirements,
    plan_payload: dict[str, Any] | None,
    assistant_message: str,
    tool_calls: list[AIPlanningToolCall],
) -> AIPlanningTurnResponse:
    page_elements = _extract_page_elements(tool_calls)
    plan = (
        _coerce_plan(plan_payload, requirements, page_elements=page_elements)
        if plan_payload
        else _build_plan(requirements, page_elements=page_elements)
    )
    return AIPlanningTurnResponse(
        assistant_message=assistant_message,
        session_status="plan_ready",
        requirements=requirements,
        missing_slots=[],
        suggested_questions=[],
        plan=plan,
        drafts=[],
        next_action="select_scenarios",
        tool_calls=tool_calls,
    )


def _coerce_plan(plan_payload: dict[str, Any], requirements: AIPlanningRequirements, *, page_elements: str | None = None) -> AIPlanningPlan:
    candidate = dict(plan_payload)
    if "summary" not in candidate or "scenarios" not in candidate:
        return _build_plan(requirements, page_elements=page_elements)
    try:
        plan = AIPlanningPlan.model_validate(candidate)
        if page_elements:
            plan = plan.model_copy(update={
                "scenarios": [
                    s.model_copy(update={"page_elements": page_elements}) if s.page_elements is None else s
                    for s in plan.scenarios
                ]
            })
        return plan
    except Exception:
        logger.warning("Planning LLM returned invalid plan payload, fallback to deterministic plan.")
        return _build_plan(requirements, page_elements=page_elements)


def _error_response(
    *,
    requirements: AIPlanningRequirements,
    tool_calls: list[AIPlanningToolCall],
) -> AIPlanningTurnResponse:
    return AIPlanningTurnResponse(
        assistant_message="AI 规划模型连续调用失败，请检查 AI planning 模型配置后再试。",
        session_status="error",
        requirements=requirements,
        missing_slots=_collect_missing_slots(requirements),
        suggested_questions=[],
        plan=None,
        drafts=[],
        next_action="ask_followup",
        tool_calls=tool_calls,
    )


def _default_followup_question(requirements: AIPlanningRequirements) -> str:
    missing_slots = _collect_missing_slots(requirements)
    if not missing_slots:
        return "如果信息足够，我可以直接开始生成测试方案。"
    labels = {
        "app_under_test": "被测系统或业务模块",
        "business_goal": "本次测试的业务目标",
        "entry_url_or_page": "入口页面或 URL",
        "core_user_flow": "核心操作流程",
        "main_assertions": "关键断言",
        "test_data_or_account": "测试数据或账号",
        "scope_limits": "范围限制",
    }
    first_two = [labels[item] for item in missing_slots[:2]]
    return f"还需要你补充 { ' 和 '.join(first_two) }，我再继续规划。"


def _collect_missing_slots(requirements: AIPlanningRequirements) -> list[str]:
    return [slot for slot in REQUIRED_REQUIREMENT_SLOTS if _slot_is_missing(requirements, slot)]


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


def _safe_parse_json(value: str) -> Any:
    try:
        return json.loads(value)
    except json.JSONDecodeError:
        return value


def _normalize_json_text(response_text: str) -> str:
    extracted = _extract_json_object(response_text)
    try:
        return json.dumps(json.loads(extracted), ensure_ascii=False)
    except json.JSONDecodeError:
        return response_text.strip()


def _fill_requirements_from_text(requirements: AIPlanningRequirements, text: str) -> None:
    if not requirements.app_under_test:
        requirements.app_under_test = _extract_after_keyword(text, ["被测系统是", "系统是", "应用是"])
    if not requirements.business_goal:
        requirements.business_goal = _extract_after_keyword(text, ["业务目标是", "目标是"])
    if not requirements.entry_url_or_page:
        requirements.entry_url_or_page = _extract_url(text) or _extract_after_keyword(text, ["入口页面是", "入口是", "页面是"])
    if not requirements.core_user_flow:
        requirements.core_user_flow = _extract_after_keyword(text, ["核心流程是", "流程是", "操作流程是"])
    if not requirements.main_assertions:
        assertions = _extract_after_keyword(text, ["主要断言是", "断言是", "预期是"])
        if assertions:
            requirements.main_assertions = _split_items(assertions)
    if not requirements.test_data_or_account:
        requirements.test_data_or_account = _extract_after_keyword(
            text,
            ["测试数据使用", "测试数据是", "测试账号是", "使用管理员账号"],
        )
    if not requirements.scope_limits:
        requirements.scope_limits = _extract_after_keyword(text, ["范围限制是", "限制是", "不覆盖"])


def _extract_after_keyword(text: str, keywords: list[str]) -> str | None:
    for keyword in keywords:
        pattern = re.compile(rf"{re.escape(keyword)}(.+?)(?:[。；;\n]|$)")
        match = pattern.search(text)
        if match:
            value = match.group(1).strip(" ：:")
            if value:
                return value
    return None


def _extract_url(text: str) -> str | None:
    match = URL_PATTERN.search(text)
    return match.group(0) if match else None


def _split_items(value: str) -> list[str]:
    items = re.split(r"[，、]|(?:\s+and\s+)|(?:\s+并且\s+)", value)
    return [item.strip() for item in items if item.strip()]


def _slot_is_missing(requirements: AIPlanningRequirements, slot: str) -> bool:
    value = getattr(requirements, slot)
    if isinstance(value, list):
        return not value
    return not bool(value and str(value).strip())


def _build_plan(requirements: AIPlanningRequirements, *, page_elements: str | None = None) -> AIPlanningPlan:
    assertions = requirements.main_assertions or ["页面状态符合预期"]
    is_login = _looks_like_login(requirements)
    flow_label = "登录" if is_login else "核心流程"
    scenarios = [
        AIPlanningScenario(
            scenario_key="login_success" if is_login else "primary_flow_success",
            title=f"{flow_label}成功",
            goal=requirements.business_goal or "验证主流程可以正常通过",
            preconditions=[
                requirements.entry_url_or_page or "提供有效入口页面",
                requirements.test_data_or_account or "准备可用测试数据",
            ],
            priority="high",
            test_data_requirements=_build_test_data_requirements(requirements, is_login=is_login),
            assertions=assertions,
            draft_prompt=_build_draft_prompt(requirements, scenario_title=f"{flow_label}成功", negative_case=False, page_elements=page_elements),
            page_elements=page_elements,
        ),
        AIPlanningScenario(
            scenario_key="login_error" if is_login else "primary_flow_validation",
            title=f"{flow_label}异常处理",
            goal=f"验证{flow_label}流程在异常输入下的兜底行为",
            preconditions=[requirements.entry_url_or_page or "提供有效入口页面"],
            priority="medium",
            test_data_requirements=_build_test_data_requirements(requirements, is_login=is_login),
            assertions=["错误提示符合预期", *assertions[:1]],
            draft_prompt=_build_draft_prompt(requirements, scenario_title=f"{flow_label}异常处理", negative_case=True, page_elements=page_elements),
            page_elements=page_elements,
        ),
    ]
    assumptions = []
    if requirements.entry_url_or_page:
        assumptions.append(f"入口页面使用 {requirements.entry_url_or_page}")
    if requirements.test_data_or_account:
        assumptions.append(f"测试数据以 {requirements.test_data_or_account} 为准")
    if not assumptions:
        assumptions.append("部分上下文缺失，方案基于当前对话做合理假设")
    risks = [requirements.scope_limits] if requirements.scope_limits else ["仍需补充范围限制与边界条件"]
    return AIPlanningPlan(
        summary=f"{requirements.app_under_test or '待补充系统'} - {requirements.business_goal or '测试规划'}",
        assumptions=assumptions,
        risks=risks,
        scenarios=scenarios,
    )


def _looks_like_login(requirements: AIPlanningRequirements) -> bool:
    haystack = " ".join(filter(None, [requirements.business_goal, requirements.core_user_flow, requirements.entry_url_or_page]))
    lowered = haystack.casefold()
    return "登录" in haystack or "login" in lowered or "signin" in lowered


def _build_test_data_requirements(
    requirements: AIPlanningRequirements,
    *,
    is_login: bool,
) -> list[AIPlanningTestDataRequirement]:
    source = requirements.test_data_or_account or "测试数据"
    if is_login:
        return [
            AIPlanningTestDataRequirement(
                key="username",
                label="登录账号",
                value_type="string",
                required=True,
                source_hint=source,
            ),
            AIPlanningTestDataRequirement(
                key="password",
                label="登录密码",
                value_type="string",
                required=True,
                source_hint="secret",
            ),
        ]
    return [
        AIPlanningTestDataRequirement(
            key="input_data",
            label="主流程输入数据",
            value_type="string",
            required=True,
            source_hint=source,
        )
    ]


def _build_draft_prompt(
    requirements: AIPlanningRequirements,
    *,
    scenario_title: str,
    negative_case: bool,
    page_elements: str | None = None,
) -> str:
    assertions = "；".join(requirements.main_assertions or ["页面状态符合预期"])
    data_labels = "；".join(
        item.label for item in _build_test_data_requirements(requirements, is_login=_looks_like_login(requirements))
    )
    negative_hint = "需要覆盖异常输入和错误提示。" if negative_case else "请覆盖标准主流程。"
    dom_section = ""
    if page_elements:
        dom_section = (
            "\n\n已采集到的页面可交互元素清单（请严格使用其中的 label、placeholder 或 id 作为 target）：\n"
            f"{page_elements}"
        )
    return (
        f"请基于测试规划生成 DSL 草案。场景：{scenario_title}。"
        f"被测系统：{requirements.app_under_test or '待补充'}。"
        f"目标：{requirements.business_goal or '待补充'}。"
        f"入口：{requirements.entry_url_or_page or '待补充'}。"
        f"流程：{requirements.core_user_flow or '待补充'}。"
        f"断言：{assertions}。"
        f"测试数据需求：{data_labels or '待补充'}。"
        f"范围限制：{requirements.scope_limits or '未说明'}。"
        f"{negative_hint}"
        "如果已获取到页面元素清单，请严格按照元素的实际 label、placeholder 或 id 作为 target，不要自行编造描述。"
        f"{dom_section}"
    )
