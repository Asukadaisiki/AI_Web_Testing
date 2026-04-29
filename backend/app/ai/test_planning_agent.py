"""LLM-driven ReAct agent for AI test planning."""

from __future__ import annotations

import json
import logging
import re
import time
import traceback as _traceback
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
    AIPlanningTodoItem,
    AIPlanningToolCall,
    AIPlanningTurnResponse,
)


logger = logging.getLogger(__name__)
URL_PATTERN = re.compile(r"https?://[^\s，。；;]+", re.IGNORECASE)

_NEW_REQUIREMENT_KEYWORDS = [
    "新需求", "换一个", "重新", "改一下", "调整方案", "变更",
    "新增测试", "还有一个", "另外还要", "再来一个", "补充测试",
    "新增场景", "换种", "不同方案", "换个思路",
]

REQUIRED_REQUIREMENT_SLOTS = [
    "app_under_test",
    "business_goal",
    "entry_url_or_page",
    "core_user_flow",
    "main_assertions",
    "test_data_or_account",
    "scope_limits",
]


def _turn_complete_payload(response: AIPlanningTurnResponse) -> dict[str, Any]:
    return {
        "type": "turn_complete",
        "session_status": response.session_status,
        "payload": {
            "assistant_message": response.assistant_message,
            "missing_slots": response.missing_slots,
            "suggested_questions": response.suggested_questions,
            "plan": response.plan.model_dump(mode="json") if response.plan else None,
            "tool_calls": [item.model_dump(mode="json") for item in response.tool_calls],
            "todo_list": [t.model_dump(mode="json") for t in response.todo_list],
        },
    }


def run_planning_turn(
    *,
    transcript: list[dict[str, str]],
    existing_requirements: AIPlanningRequirements | None,
    db_session: Session,
    project_id: int,
    actor_user_id: int = 0,
    planning_session_id: int = 0,
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
        actor_user_id=actor_user_id,
        planning_session_id=planning_session_id,
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
    actor_user_id: int = 0,
    planning_session_id: int = 0,
) -> Generator[dict[str, Any], None, AIPlanningTurnResponse]:
    """Streaming ReAct planning turn.

    Yields status / text_chunk / tool_call events during processing.
    Returns the final ``AIPlanningTurnResponse`` via the generator return value.
    """
    requirements = existing_requirements.model_copy(deep=True) if existing_requirements else AIPlanningRequirements()
    settings = get_settings()
    tool_calls: list[AIPlanningToolCall] = []
    transcript_messages, force_generate = _prepare_transcript_for_llm(
        transcript,
        requirements=existing_requirements,
        plan=None,
        tool_calls=None,
    )
    logger.info("Planning turn start, transcript_len=%d, ai_enabled=%s", len(transcript), _planning_llm_enabled(settings))

    if not _planning_llm_enabled(settings):
        response = _run_fallback_turn(
            transcript=transcript,
            requirements=requirements,
            assistant_message=None,
            force_generate=force_generate,
            tool_calls=tool_calls,
        )
        yield _turn_complete_payload(response)
        return response

    conversation: list[dict[str, str]] = [{"role": "system", "content": build_system_prompt()}, *transcript_messages]
    safety_cap = max(1, settings.ai_planning_max_react_safety_cap)
    round_index = 0
    turn_start_time = time.monotonic()
    while round_index < safety_cap:
        round_index += 1
        yield {"type": "status", "phase": "thinking", "message": "正在分析需求..."}

        raw_response = ""
        llm_error_type = ""
        llm_error_detail = ""
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
        except httpx.ConnectTimeout:
            llm_error_type = "ConnectTimeout"
            llm_error_detail = f"连接 AI 模型服务超时 (base_url={settings.ai_planning_base_url})"
            logger.error("LLM connection timeout in round %d: %s", round_index, llm_error_detail)
        except httpx.ReadTimeout:
            llm_error_type = "ReadTimeout"
            llm_error_detail = f"AI 模型响应超时 (timeout={settings.ai_planning_timeout_ms}ms)"
            logger.error("LLM read timeout in round %d: %s", round_index, llm_error_detail)
        except httpx.HTTPStatusError as exc:
            llm_error_type = "HTTPStatusError"
            llm_error_detail = f"AI 模型返回 HTTP {exc.response.status_code}: {exc.response.text[:500]}"
            logger.error("LLM HTTP error in round %d: %s", round_index, llm_error_detail)
        except httpx.ConnectError:
            llm_error_type = "ConnectError"
            llm_error_detail = f"无法连接 AI 模型服务 (base_url={settings.ai_planning_base_url})"
            logger.error("LLM connection error in round %d: %s", round_index, llm_error_detail)
        except Exception as exc:
            llm_error_type = type(exc).__name__
            llm_error_detail = str(exc)
            logger.exception("Streaming LLM call failed in round %d", round_index)

        if not raw_response:
            error_phase = "timeout" if "Timeout" in llm_error_type else ("connection" if "Connect" in llm_error_type else "llm_call")
            response = _error_response(
                requirements=requirements,
                tool_calls=tool_calls,
                error_type=llm_error_type or "empty_response",
                error_detail=llm_error_detail or "LLM 返回空响应",
                phase=error_phase,
            )
            yield _turn_complete_payload(response)
            return response

        parsed = _parse_llm_response(raw_response)
        if parsed is None:
            logger.warning("LLM response unparseable in round %d, raw (first 300 chars): %s", round_index, raw_response[:300])
            response = _run_fallback_turn(
                transcript=transcript,
                requirements=requirements,
                assistant_message="遇到了解析问题，我先按已有信息给你整理一个测试方案。",
                force_generate=force_generate,
                tool_calls=tool_calls,
            )
            yield _turn_complete_payload(response)
            return response

        _merge_requirements(requirements, parsed.get("collected_info"))
        _merge_test_context(requirements, parsed.get("test_context"))
        action = str(parsed.get("action") or "").strip()
        action_input = parsed.get("action_input")
        if not isinstance(action_input, dict):
            action_input = {}

        logger.debug("ReAct round %d: action=%s", round_index, action)

        # --- Parse todo_list from LLM response ---
        _valid_statuses = {"done", "in_progress", "pending", "failed", "skipped"}
        raw_todo = parsed.get("todo_list") or []
        todo_items = [
            AIPlanningTodoItem(
                item=str(t.get("item", "")),
                status=t.get("status", "pending") if t.get("status") in _valid_statuses else "pending",
            )
            for t in raw_todo if isinstance(t, dict) and str(t.get("item", "")).strip()
        ]

        # --- Force explore_page/explore_flow before generating plan (BUG-052) ---
        if action == "generate_plan":
            has_explore = _has_explored_pages(tool_calls)
            has_flow = any(call.tool == "explore_flow" for call in tool_calls)
            if not has_explore:
                explored, tool_calls = _auto_explore_entry_url(
                    requirements, tool_calls, db_session, project_id,
                )
                if explored:
                    # Check if exploration actually produced useful data
                    page_elements = _extract_page_elements(tool_calls)
                    exploration_error = _extract_exploration_error(tool_calls)
                    if not page_elements and exploration_error:
                        yield {"type": "status", "phase": "tool_call", "message": f"页面探索失败：{exploration_error}"}
                        conversation.append(
                            {"role": "system", "content": (
                                f"⚠️ 页面自动探索失败，错误信息：{exploration_error}\n"
                                "请向用户报告此错误，说明无法采集页面元素，建议用户：\n"
                                "1. 检查入口 URL 是否正确且可访问\n"
                                "2. 稍后重试（可能是网络波动）\n"
                                "3. 提供更多页面信息以辅助规划\n"
                                "不要在没有页面元素数据的情况下生成测试方案。"
                            )},
                        )
                        continue
                    yield {"type": "status", "phase": "tool_call", "message": "正在自动采集入口页面及导航页面元素..."}
                    conversation.append(
                        {"role": "system", "content": (
                            "系统已自动采集了入口页面及其导航链接页面的可交互元素（见上方工具返回结果）。"
                            "请基于这些元素信息重新生成测试方案，"
                            "确保 target 使用元素的实际 label、placeholder 或 id。"
                            "对于未采集到的页面，可使用语义描述作为 target。"
                        )},
                    )
                    continue
            elif not has_flow:
                # AI called explore_page but not explore_flow — supplement with multi-page
                explored, tool_calls = _auto_explore_entry_url(
                    requirements, tool_calls, db_session, project_id,
                )
                if explored:
                    yield {"type": "status", "phase": "tool_call", "message": "正在补充采集导航页面元素..."}
                    conversation.append(
                        {"role": "system", "content": (
                            "系统补充采集了导航链接页面的可交互元素。"
                            "请基于所有已采集的页面元素信息重新生成测试方案，"
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
                todo_list=todo_items,
            )
            yield _turn_complete_payload(response)
            return response

        if action == "call_tool":
            tool_name = str(action_input.get("tool") or "").strip()
            params = action_input.get("params")
            if not isinstance(params, dict):
                params = {}
            logger.info("Tool call: %s, params_keys=%s", tool_name, list(params.keys()))
            yield {"type": "tool_call_start", "tool": tool_name, "params": params}
            try:
                tool_result_text = execute_tool(
                    tool_name=tool_name,
                    params=params,
                    db_session=db_session,
                    project_id=project_id,
                    actor_user_id=actor_user_id,
                    planning_session_id=planning_session_id,
                )
            except Exception as exc:
                logger.error("Tool call %s failed: %s", tool_name, exc, exc_info=True)
                response = _error_response(
                    requirements=requirements,
                    tool_calls=tool_calls,
                    error_type=type(exc).__name__,
                    error_detail=str(exc),
                    phase="tool_call",
                )
                yield _turn_complete_payload(response)
                return response
            parsed_result = _safe_parse_json(tool_result_text)
            tool_calls.append(
                AIPlanningToolCall(
                    tool=tool_name or "unknown_tool",
                    params=params,
                    result=parsed_result,
                )
            )
            yield {"type": "tool_call_end", "tool": tool_name, "result": parsed_result}
            logger.info("Tool call %s completed, result_keys=%s", tool_name, list(parsed_result.keys()) if isinstance(parsed_result, dict) else "non-dict")
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
            logger.info("Generating plan after %d ReAct rounds, tool_calls=%d", round_index, len(tool_calls))
            response = _plan_response(
                requirements=requirements,
                plan_payload=action_input,
                assistant_message="信息已经足够，我先给出结构化测试方案。",
                tool_calls=tool_calls,
                todo_list=todo_items,
            )
            yield _turn_complete_payload(response)
            return response

        if action == "analyze_results":
            analysis_payload = action_input.get("analysis") if isinstance(action_input, dict) else None
            if not isinstance(analysis_payload, dict):
                analysis_payload = {}
            try:
                from app.schemas.ai_planning import ExecutionAnalysis
                analysis = ExecutionAnalysis.model_validate(analysis_payload)
            except Exception:
                analysis = ExecutionAnalysis(conclusion="partial")
            analysis_message = str(action_input.get("summary") or "").strip() if isinstance(action_input, dict) else ""
            if not analysis_message:
                analysis_message = _build_analysis_message(analysis)
            response = AIPlanningTurnResponse(
                assistant_message=analysis_message,
                session_status="completed",
                requirements=requirements,
                missing_slots=[],
                suggested_questions=[],
                plan=None,
                drafts=[],
                next_action="ask_followup",
                tool_calls=tool_calls,
                todo_list=todo_items,
                execution_analysis=analysis,
            )
            yield _turn_complete_payload(response)
            return response

        if action == "plan_regression":
            regression_summary = str(action_input.get("summary") or "").strip() if isinstance(action_input, dict) else ""
            if not regression_summary:
                regression_summary = "根据失败分析，建议进行回归测试。"
            response = _plan_response(
                requirements=requirements,
                plan_payload=action_input,
                assistant_message=regression_summary,
                tool_calls=tool_calls,
                todo_list=todo_items,
            )
            yield _turn_complete_payload(response)
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
            todo_list=todo_items,
        )
        yield _turn_complete_payload(response)
        return response

    # Exhausted safety cap — force-generate a plan
    elapsed = time.monotonic() - turn_start_time
    logger.warning("Safety cap exhausted after %d rounds (%.2fs), forcing fallback plan", round_index, elapsed)
    response = _run_fallback_turn(
        transcript=transcript,
        requirements=requirements,
        assistant_message="我先根据当前上下文整理一版测试方案。",
        force_generate=True,
        tool_calls=tool_calls,
    )
    yield _turn_complete_payload(response)
    return response


def _planning_llm_enabled(settings: Any) -> bool:
    return bool(
        getattr(settings, "enable_ai_planning", False)
        and getattr(settings, "ai_planning_model", None)
        and getattr(settings, "ai_planning_api_key", None)
    )


def _is_new_requirement_intent(user_message: str) -> bool:
    return any(kw in user_message for kw in _NEW_REQUIREMENT_KEYWORDS)


def _build_context_summary(
    requirements: AIPlanningRequirements,
    plan: AIPlanningPlan | None,
    tool_calls: list[AIPlanningToolCall],
) -> str:
    parts = ["[历史对话摘要]"]

    filled = {}
    labels = {
        "app_under_test": "被测系统",
        "business_goal": "业务目标",
        "entry_url_or_page": "入口页面",
        "core_user_flow": "核心流程",
        "main_assertions": "关键断言",
        "test_data_or_account": "测试数据",
        "scope_limits": "范围限制",
    }
    for slot in REQUIRED_REQUIREMENT_SLOTS:
        val = getattr(requirements, slot, None)
        if slot == "main_assertions":
            if val:
                filled[labels[slot]] = ", ".join(val)
        elif val and str(val).strip():
            filled[labels[slot]] = str(val).strip()
    if filled:
        parts.append("- 用户需求：" + "；".join(f"{k}：{v}" for k, v in filled.items()))

    if plan:
        scenario_titles = ", ".join(s.title for s in plan.scenarios) if plan.scenarios else "无"
        parts.append(f"- 已有方案：{plan.summary}（场景：{scenario_titles}）")

    if tool_calls:
        explore_count = sum(1 for c in tool_calls if c.tool in ("explore_page", "explore_flow"))
        total = len(tool_calls)
        if explore_count:
            parts.append(f"- 已产生的结果：共调用 {total} 次工具，其中 {explore_count} 次页面采集")
        else:
            parts.append(f"- 已产生的结果：共调用 {total} 次工具")

    return "\n".join(parts)


def _prepare_transcript_for_llm(
    transcript: list[dict[str, str]],
    *,
    requirements: AIPlanningRequirements | None = None,
    plan: AIPlanningPlan | None = None,
    tool_calls: list[AIPlanningToolCall] | None = None,
) -> tuple[list[dict[str, str]], bool]:
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

    # --- Context compression ---
    settings = get_settings()
    threshold = settings.ai_planning_context_compress_threshold
    keep_recent = settings.ai_planning_context_keep_recent
    if len(prepared) > threshold and requirements is not None:
        last_user_msg = ""
        for msg in reversed(prepared):
            if msg.get("role") == "user":
                last_user_msg = msg.get("content", "")
                break
        if _is_new_requirement_intent(last_user_msg):
            summary = _build_context_summary(requirements, plan, tool_calls or [])
            recent = prepared[-keep_recent:]
            logger.info(
                "Compressing context: %d messages -> summary + %d recent",
                len(prepared), len(recent),
            )
            prepared = [{"role": "system", "content": summary}, *recent]

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
            result = _call_planning_llm(
                messages=messages,
                api_key=api_key,
                model=model,
                base_url=base_url,
                timeout_seconds=timeout_seconds,
            )
            logger.debug("LLM call succeeded on attempt %d", attempt + 1)
            return result
        except Exception as exc:
            logger.exception("Planning LLM call failed on attempt %d/%d: %s", attempt + 1, 3, type(exc).__name__)
    return None


def _should_enable_thinking_mode(*, base_url: str, model: str) -> bool:
    normalized_base_url = base_url.strip().casefold()
    normalized_model = model.strip().casefold()
    return (
        "open.bigmodel.cn" in normalized_base_url
        or normalized_model.startswith("glm-")
        or "api.deepseek.com" in normalized_base_url
        or normalized_model.startswith("deepseek-")
    )


def _call_planning_llm(
    *,
    messages: list[dict[str, Any]],
    api_key: str,
    model: str,
    base_url: str,
    timeout_seconds: float,
) -> str:
    payload: dict[str, Any] = {
        "model": model,
        "messages": messages,
    }
    if _should_enable_thinking_mode(base_url=base_url, model=model):
        payload["thinking"] = {"type": "enabled"}
        payload["max_tokens"] = 65536
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
    payload: dict[str, Any] = {
        "model": model,
        "messages": messages,
        "stream": True,
    }
    if _should_enable_thinking_mode(base_url=base_url, model=model):
        payload["thinking"] = {"type": "enabled"}
        payload["max_tokens"] = 65536
    else:
        payload["response_format"] = {"type": "json_object"}
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
                    logger.debug("SSE parse error, raw_line (first 200 chars): %s", data[:200])
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


def _merge_test_context(requirements: AIPlanningRequirements, test_context: Any) -> None:
    if not isinstance(test_context, dict):
        return
    existing = requirements.test_context or {}
    merged = {**existing, **{k: v for k, v in test_context.items() if v is not None}}
    requirements.test_context = merged


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


def _extract_exploration_error(tool_calls: list[AIPlanningToolCall]) -> str | None:
    """Extract error message from failed explore_page or explore_flow calls."""
    errors = []
    for call in reversed(tool_calls):
        if call.tool in ("explore_page", "explore_flow") and isinstance(call.result, dict):
            error = call.result.get("error")
            if error:
                errors.append(str(error))
            # For explore_flow, check individual page errors
            pages = call.result.get("pages", [])
            page_errors = [p.get("error", "") for p in pages if p.get("error")]
            if page_errors:
                errors.extend(page_errors)
    return "; ".join(errors) if errors else None


def _has_explored_pages(tool_calls: list[AIPlanningToolCall]) -> bool:
    """Return True if any explore_page or explore_flow call exists in tool_calls."""
    return any(call.tool in ("explore_page", "explore_flow") for call in tool_calls)


def _auto_explore_entry_url(
    requirements: AIPlanningRequirements,
    tool_calls: list[AIPlanningToolCall],
    db_session: Session,
    project_id: int,
) -> tuple[bool, list[AIPlanningToolCall]]:
    """Auto-invoke explore_page (or explore_flow) with the entry URL.

    If the entry page contains navigable internal links, automatically
    collects those pages too via explore_flow so that multi-page test
    flows have DOM evidence for every page.

    If an explore_page call already exists in *tool_calls*, reuses its
    result to extract links and only triggers the explore_flow step.

    Returns (explored, tool_calls) where *explored* indicates whether
    exploration was actually triggered.
    """
    entry_url = requirements.entry_url_or_page
    if not entry_url or not isinstance(entry_url, str):
        return False, tool_calls

    match = URL_PATTERN.search(entry_url)
    if not match:
        return False, tool_calls

    base_url = match.group(0)

    # Check if explore_page was already called by the AI
    existing_explore_result = None
    for call in tool_calls:
        if call.tool == "explore_page" and isinstance(call.result, dict):
            existing_explore_result = call.result

    if existing_explore_result is None:
        # Step 1: Explore the entry page first
        try:
            tool_result_text = execute_tool(
                tool_name="explore_page",
                params={"url": base_url},
                db_session=db_session,
                project_id=project_id,
                actor_user_id=actor_user_id,
                planning_session_id=planning_session_id,
            )
            parsed_result = _safe_parse_json(tool_result_text)
        except Exception as exc:
            logger.warning("Auto-explore failed for url=%s: %s", base_url, exc)
            parsed_result = {"error": str(exc), "url": base_url}

        tool_calls.append(
            AIPlanningToolCall(
                tool="explore_page",
                params={"url": base_url},
                result=parsed_result,
            )
        )
    else:
        parsed_result = existing_explore_result

    # Step 2: Extract navigable internal links from the entry page result
    internal_links = _extract_internal_links(parsed_result, base_url)
    logger.info("Auto-explore: found %d internal links from %s", len(internal_links), base_url)
    if internal_links:
        all_urls = [base_url] + internal_links[:4]  # cap at 5 pages total
        logger.info("Auto-explore: triggering explore_flow with %d URLs", len(all_urls))
        try:
            flow_result_text = execute_tool(
                tool_name="explore_flow",
                params={"urls": all_urls},
                db_session=db_session,
                project_id=project_id,
                actor_user_id=actor_user_id,
                planning_session_id=planning_session_id,
            )
            flow_result = _safe_parse_json(flow_result_text)
            tool_calls.append(
                AIPlanningToolCall(
                    tool="explore_flow",
                    params={"urls": all_urls},
                    result=flow_result,
                )
            )
        except Exception as exc:
            logger.warning("Auto-explore_flow failed: %s", exc, exc_info=True)

    return True, tool_calls


def _extract_internal_links(
    explore_result: dict[str, Any] | None,
    base_url: str,
) -> list[str]:
    """Extract navigable internal links from an explore_page result.

    Returns a list of absolute URLs that are on the same domain as *base_url*.
    Skips anchors, javascript: links, and duplicate paths.
    """
    if not explore_result or not isinstance(explore_result, dict):
        return []

    elements = explore_result.get("elements", [])
    if not isinstance(elements, list):
        return []

    from urllib.parse import urljoin, urlparse

    base_parsed = urlparse(base_url)
    base_origin = f"{base_parsed.scheme}://{base_parsed.netloc}"
    seen_paths: set[str] = {base_parsed.path or "/"}
    links: list[str] = []

    for elem in elements:
        if not isinstance(elem, dict):
            continue
        tag = elem.get("tag", "")
        if tag != "a":
            continue
        href = elem.get("href") or ""
        if not href or href.startswith("#") or href.startswith("javascript:") or href.startswith("mailto:"):
            continue

        abs_url = urljoin(base_url, href)
        parsed = urlparse(abs_url)

        # Same domain only
        if parsed.netloc != base_parsed.netloc:
            continue

        path = parsed.path or "/"
        if path in seen_paths:
            continue

        seen_paths.add(path)
        # Reconstruct clean URL (drop query/fragment)
        clean_url = f"{parsed.scheme}://{parsed.netloc}{path}"
        links.append(clean_url)

    return links


def _plan_response(
    *,
    requirements: AIPlanningRequirements,
    plan_payload: dict[str, Any] | None,
    assistant_message: str,
    tool_calls: list[AIPlanningToolCall],
    todo_list: list[AIPlanningTodoItem] | None = None,
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
        todo_list=todo_list or [],
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
    error_type: str = "unknown",
    error_detail: str = "",
    phase: str = "unknown",
) -> AIPlanningTurnResponse:
    suggestions: dict[str, str] = {
        "llm_call": "请检查 AI 模型配置（API Key、Base URL、Model 名称）是否正确，以及模型服务是否可用。",
        "json_parse": "AI 模型返回了无法解析的内容，请检查模型是否支持 JSON 输出模式（response_format=json_object）。",
        "tool_call": "工具调用执行失败，请查看后端日志获取详细堆栈信息。",
        "timeout": "AI 模型调用超时，请检查网络连接或增大超时时间配置。",
        "connection": "无法连接到 AI 模型服务，请检查 Base URL 是否正确、服务是否在运行。",
    }
    suggestion = suggestions.get(phase, "请查看后端日志获取详细错误信息。")
    detail_parts = [f"阶段: {phase}"]
    if error_type != "unknown":
        detail_parts.append(f"错误类型: {error_type}")
    if error_detail:
        detail_parts.append(f"详细信息: {error_detail}")
    detail_parts.append(f"建议: {suggestion}")
    full_message = "AI 规划过程中遇到错误。\n" + "\n".join(detail_parts)
    logger.error("Planning error: type=%s, phase=%s, detail=%s", error_type, phase, error_detail[:500])
    return AIPlanningTurnResponse(
        assistant_message=full_message,
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


def _build_analysis_message(analysis: Any) -> str:
    lines = ["执行结果分析：\n"]
    conclusion_labels = {
        "all_passed": "全部通过",
        "partial": "部分通过",
        "all_failed": "全部失败",
    }
    lines.append(f"本轮结论：{conclusion_labels.get(getattr(analysis, 'conclusion', ''), '未知')}")
    for cr in getattr(analysis, "case_results", []):
        status_icon = "✅" if cr.status == "passed" else "❌"
        lines.append(f"  {status_icon} {cr.case_name} — {cr.status} ({cr.passed_steps}/{cr.total_steps}步)")
    for fd in getattr(analysis, "failure_details", []):
        lines.append(f"  ⚠ 失败点：{fd.case_name} 步骤{fd.step_index}({fd.action}) — {fd.suspected_cause}")
    if getattr(analysis, "suspected_root_cause", None):
        lines.append(f"疑似根因：{analysis.suspected_root_cause}")
    if getattr(analysis, "recommended_action", None):
        action_labels = {
            "targeted_retest": "针对性复测",
            "regression": "回归测试",
            "manual": "人工介入",
            "done": "测试完成",
        }
        lines.append(f"建议下一步：{action_labels.get(analysis.recommended_action, analysis.recommended_action)}")
        if getattr(analysis, "recommended_scope", None):
            scope_labels = {"current": "仅当前用例", "adjacent": "相邻流程", "module": "模块级", "core": "核心链路"}
            lines.append(f"回归范围：{scope_labels.get(analysis.recommended_scope, analysis.recommended_scope)}")
    return "\n".join(lines)


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
        # 1. Happy path
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
        # 2. Input validation / exception
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
        # 3. Data consistency / cross-page verification
        AIPlanningScenario(
            scenario_key="data_consistency",
            title="数据一致性验证",
            goal="验证跨步骤数据传递和状态保持的正确性",
            preconditions=[
                requirements.entry_url_or_page or "提供有效入口页面",
                requirements.test_data_or_account or "准备可用测试数据",
            ],
            priority="medium",
            test_data_requirements=_build_test_data_requirements(requirements, is_login=is_login),
            assertions=["跨页面数据一致", "状态转换符合预期", *assertions[:2]],
            draft_prompt=_build_draft_prompt(requirements, scenario_title="数据一致性验证", negative_case=False, page_elements=page_elements),
            page_elements=page_elements,
        ),
        # 4. Boundary / edge case
        AIPlanningScenario(
            scenario_key="boundary_conditions",
            title="边界条件测试",
            goal="验证系统在边界输入下的健壮性",
            preconditions=[requirements.entry_url_or_page or "提供有效入口页面"],
            priority="low",
            test_data_requirements=_build_test_data_requirements(requirements, is_login=is_login),
            assertions=["边界输入处理正确", "无异常崩溃"],
            draft_prompt=_build_draft_prompt(requirements, scenario_title="边界条件测试", negative_case=True, page_elements=page_elements),
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
            f"{page_elements}\n"
            "注意：标注了 [dynamic] 的元素是交互触发后才出现的动态元素，步骤顺序必须与用户流程一致。"
        )
    # Build test data section with full detail
    test_data = requirements.test_data_or_account
    data_section = ""
    if test_data:
        data_section = (
            f"\n\n测试数据（必须为提到的每个字段生成对应的 input/click 步骤）：\n{test_data}\n"
            "注意：上述测试数据中提到的每个字段（如下拉框、日期选择器、复选框）都必须在 steps 中有对应操作。"
            "下拉框用 input action（target 为字段标签，value 为选项文本），复选框用 click action（target 为复选框标签）。"
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
        "如果已获取到页面元素清单，请严格按照元素的实际可见文本、label、placeholder 或 id 作为 target（纯文本字符串，如 \"Email Address\"），不要构造 CSS 选择器格式。step 的 value 字段如涉及测试数据，必须用 ${context_key} 格式引用 input_contract 变量，不要硬编码。"
        "必须为流程和测试数据中提到的每个表单字段生成对应步骤，不得遗漏任何字段（包括下拉框、日期选择器、复选框等）。"
        f"{data_section}"
        f"{dom_section}"
    )
