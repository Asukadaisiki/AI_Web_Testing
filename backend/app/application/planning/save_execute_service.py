"""Planning draft persistence and execution use cases."""

from __future__ import annotations

import logging
import time
from collections.abc import Callable
from typing import Any, Protocol

from sqlalchemy import select

from app.application.planning.analysis_retest_service import (
    run_analysis_turn,
    should_run_analysis,
)
from app.application.planning.execution_inputs import build_input_values_from_session
from app.application.planning.project_context import (
    ensure_project_member_for_session_projects,
    get_active_project_id as _get_active_project_id,
    get_owned_session as _get_session,
    get_session_project_ids as _get_session_project_ids,
)
from app.core.structured_logging import get_structured_logger
from sqlalchemy.orm import Session

from app.models import AIPlanningDraft, AIPlanningMessage, TestCase
from app.schemas.ai_planning import (
    AIPlanningRequirements,
    AIPlanningTurnResponse,
    ExecutionSummaryResult,
    SavedCaseResult,
)
from app.schemas.cases import CaseCreateRequest
from app.schemas.executions import CaseExecutionRequest
from app.services.cases import create_case
from app.services.executions import execute_case, execute_case_streaming


logger = logging.getLogger(__name__)
slog = get_structured_logger(__name__)


class PlanningEventLog(Protocol):
    def write(self, event_type: str, event: dict) -> None: ...

    def flush(self) -> None: ...


PlanningEventLogFactory = Callable[..., PlanningEventLog]


def _record_execution_anti_patterns(
    session: Session,
    case_id: int,
    scenario_key: str,
    project_id: int,
) -> None:
    """Analyze the latest execution of *case_id* and record failed steps as anti-patterns.

    Only processes the most recent execution run. Each failed step becomes an
    anti-pattern entry that the DSL generator can use as a few-shot negative example
    on the next retry — the AI sees what went wrong and self-corrects.
    """
    from sqlalchemy import desc
    from app.models import TestCaseRun
    from app.services.anti_patterns import (
        record_anti_pattern, TARGET_NOT_FOUND, MISSING_STEP, WRONG_PAGE_STATE,
    )

    latest_run = session.execute(
        select(TestCaseRun)
        .where(TestCaseRun.case_id == case_id)
        .order_by(desc(TestCaseRun.id))
        .limit(1)
    ).scalar_one_or_none()

    if not latest_run or not latest_run.report:
        return

    report = latest_run.report if isinstance(latest_run.report, dict) else {}
    steps = report.get("steps") or []
    if not steps:
        return

    for step in steps:
        if step.get("status") != "failed":
            continue
        action = step.get("action", "")
        target = step.get("target") or ""
        error_msg = step.get("error_message") or ""
        resolved_by = step.get("resolved_by") or "unknown"
        dom_text = ""
        dom_snap = step.get("dom_summary") or {}
        if isinstance(dom_snap, dict):
            dom_text = dom_snap.get("text_preview", "")[:200]

        # Classify the failure
        if action == "assert_text":
            # Extract expected vs actual from error message
            import re
            expected_match = re.search(r"to contain text '([^']*)'", error_msg)
            actual_match = re.search(r"unexpected value \"([^\"]*)\"", error_msg)
            expected_val = expected_match.group(1) if expected_match else ""
            actual_val = actual_match.group(1) if actual_match else ""
            context_note = (
                f"assert_text target='{target[:80]}' value='{expected_val[:50]}' 失败"
                f"——实际定位到的是 '{actual_val[:50]}'"
                f"（定位策略: {resolved_by}）。↓"
                f"可能原因: 1) target 文本在页面上匹配了错误元素"
                f" 2) 缺少来自无障碍树预检的 verified candidate"
                f" 3) 商品/列表场景应使用可预检的结构化候选，而不是裸文本 target"
            )
        elif action == "click":
            if "timeout" in error_msg.lower() or "not found" in error_msg.lower():
                context_note = (
                    f"click target='{target[:80]}' 失败——元素未找到或不可见。"
                    f"↓ 需要检查 target 是否与实际页面文本一致"
                    f"（DOM 片段: {dom_text[:100]}）"
                )
            else:
                context_note = f"click target='{target[:80]}' 失败: {error_msg[:200]}"
        elif action == "input":
            context_note = (
                f"input target='{target[:80]}' 失败: {error_msg[:200]}。"
                f"↓ 检查 target 是否匹配正确的输入框"
            )
        else:
            context_note = f"{action} target='{target[:80]}' 失败: {error_msg[:200]}"

        # Build the wrong snippet
        snippet: dict[str, Any] = {
            "action": action,
            "target": target,
            "value": step.get("value"),
            "resolved_by": resolved_by,
        }

        # Determine category
        if "assert_text" in action and actual_val and expected_val != actual_val:
            category = WRONG_PAGE_STATE  # assertion matched wrong element
        elif "timeout" in error_msg.lower() or "not found" in error_msg.lower():
            category = TARGET_NOT_FOUND
        else:
            category = MISSING_STEP

        record_anti_pattern(
            session,
            error_category=category,
            wrong_snippet=snippet,
            context_note=context_note,
            source="execution",
            project_id=project_id,
        )
        logger.info(
            "Execution anti-pattern recorded: case=%d step=%s target=%s category=%s",
            case_id, action, target[:60], category,
        )



def save_and_execute_selected_drafts(
    session: Session,
    planning_session_id: int,
    draft_ids: list[int],
    actor_user_id: int,
    execute: bool = True,
    input_values: dict[str, str] | None = None,
) -> AIPlanningTurnResponse:
    planning_session = _get_session(session, planning_session_id, actor_user_id=actor_user_id)
    project_ids = _get_session_project_ids(planning_session)
    if not project_ids:
        raise ValueError("请先关联至少一个项目再保存和执行用例。")

    # Ensure user is a member of all linked projects (fix for projects created before ProjectMember fix)
    ensure_project_member_for_session_projects(session, planning_session_id, actor_user_id)

    drafts = (
        session.query(AIPlanningDraft)
        .filter(
            AIPlanningDraft.session_id == planning_session_id,
            AIPlanningDraft.id.in_(draft_ids),
        )
        .all()
    )

    saved_cases: list[SavedCaseResult] = []
    for draft in drafts:
        if not draft.dsl_case_json:
            continue
        case_payload = CaseCreateRequest(
            project_id=_get_active_project_id(planning_session),
            actor_user_id=actor_user_id,
            **draft.dsl_case_json,
        )
        case = create_case(session, case_payload, actor_user_id=actor_user_id)
        saved_cases.append(SavedCaseResult(case_id=case.id, case_name=case.name))
        draft.status = "imported"

    if not execute or not saved_cases:
        assistant_message = f"已保存 {len(saved_cases)} 个测试用例。" + ("\n是否立即执行？" if saved_cases else "")
        planning_session.status = "saving"
        session.add(
            AIPlanningMessage(
                session_id=planning_session.id,
                role="assistant",
                turn_type="followup",
                content=assistant_message,
                structured_payload_json={
                    "type": "save_result",
                    "saved_cases": [item.model_dump(mode="json") for item in saved_cases],
                },
            )
        )
        session.commit()
        session.refresh(planning_session)
        return AIPlanningTurnResponse(
            assistant_message=assistant_message,
            session_status="saving",
            requirements=AIPlanningRequirements.model_validate(planning_session.requirements_json or {}),
            missing_slots=[],
            plan=None,
            drafts=[],
            next_action="ask_followup",
            saved_cases=saved_cases,
        )

    # Auto-fill input_values from session test data if not provided by caller
    if not input_values:
        input_values = build_input_values_from_session(
            planning_session.requirements_json or {},
            [d.dsl_case_json for d in drafts if d.dsl_case_json],
        )
        logger.info(
            "Auto-resolved input_values from session data: %s",
            {k: v[:3] + '***' for k, v in input_values.items()} if input_values else {},
        )

    execution_summaries: list[ExecutionSummaryResult] = []
    for saved in saved_cases:
        payload = CaseExecutionRequest(actor_user_id=actor_user_id, input_values=input_values or {})
        result = execute_case(session, saved.case_id, payload)
        passed = sum(1 for s in (result.report.steps or []) if s.status == "passed")
        failed = sum(1 for s in (result.report.steps or []) if s.status == "failed")
        execution_summaries.append(ExecutionSummaryResult(
            execution_id=result.id,
            case_id=saved.case_id,
            case_name=result.case_name,
            status=result.status,
            total_steps=result.total_steps,
            passed_steps=passed,
            failed_steps=failed,
            duration_ms=result.duration_ms,
            screenshot_url=result.latest_screenshot_url,
            report_url=f"/run/{result.id}",
        ))

    # --- Self-healing: record execution failures as anti-patterns ---
    for saved in saved_cases:
        draft = next((d for d in drafts if d.dsl_case_json and d.dsl_case_json.get("name") == saved.case_name), None)
        if draft is None:
            continue
        try:
            _record_execution_anti_patterns(
                session, saved.case_id, draft.scenario_key, _get_active_project_id(planning_session),
            )
        except Exception as ap_exc:
            logger.warning("Execution anti-pattern recording failed: %s", ap_exc)

    lines = ["测试执行完成：\n"]
    for ex in execution_summaries:
        icon = "✅" if ex.status == "passed" else "❌"
        lines.append(f"{icon} {ex.case_name} — {ex.status} ({ex.passed_steps}/{ex.total_steps}步)")

    assistant_message = "\n".join(lines)
    planning_session.status = "completed"
    session.add(
        AIPlanningMessage(
            session_id=planning_session.id,
            role="assistant",
            turn_type="plan",
            content=assistant_message,
            structured_payload_json={
                "type": "execution_summary",
                "saved_cases": [item.model_dump(mode="json") for item in saved_cases],
                "execution_summaries": [item.model_dump(mode="json") for item in execution_summaries],
            },
        )
    )
    session.commit()
    session.refresh(planning_session)

    execution_analysis = None
    if should_run_analysis(execution_summaries):
        analysis_response = run_analysis_turn(
            execution_summaries=execution_summaries,
            db_session=session,
            project_id=_get_active_project_id(planning_session),
        )
        if analysis_response and analysis_response.execution_analysis:
            execution_analysis = analysis_response.execution_analysis
            analysis_msg = analysis_response.assistant_message
            session.add(
                AIPlanningMessage(
                    session_id=planning_session.id,
                    role="assistant",
                    turn_type="followup",
                    content=analysis_msg,
                    structured_payload_json={
                        "type": "execution_analysis",
                        "analysis": execution_analysis.model_dump(mode="json"),
                    },
                )
            )
            session.commit()
            assistant_message = f"{assistant_message}\n\n---\n\n{analysis_msg}"

    return AIPlanningTurnResponse(
        assistant_message=assistant_message,
        session_status="completed",
        requirements=AIPlanningRequirements.model_validate(planning_session.requirements_json or {}),
        missing_slots=[],
        suggested_questions=[],
        plan=None,
        drafts=[],
        next_action="ask_followup",
        saved_cases=saved_cases,
        execution_summaries=execution_summaries,
        execution_analysis=execution_analysis,
    )


def save_and_execute_selected_drafts_streaming(
    session: Session,
    planning_session_id: int,
    draft_ids: list[int],
    actor_user_id: int,
    *,
    input_values: dict[str, str] | None = None,
    cancel_event=None,
    event_log_factory: PlanningEventLogFactory,
    session_factory=None,
):
    """Generator version of save_and_execute_selected_drafts for WebSocket streaming.

    Yields progress event dicts. After all cases complete, persists the execution
    summary message and yields a ``done`` event.
    """
    from threading import Event as ThreadEvent
    from app.runners.playwright_runner import RunnerCancelledError

    if cancel_event is None:
        cancel_event = ThreadEvent()

    start_time = time.monotonic()
    logger.info("[session:%d] Save-and-execute streaming start, draft_ids=%s", planning_session_id, draft_ids)

    # Event log writer — NO DB query on init, writes inline during streaming.
    event_log = event_log_factory(
        session_factory=session_factory,
        session_id=planning_session_id,
        flush_interval=5,
    )

    planning_session = _get_session(session, planning_session_id, actor_user_id=actor_user_id)
    project_ids = _get_session_project_ids(planning_session)
    if not project_ids:
        raise ValueError("请先关联至少一个项目再保存和执行用例。")

    # Ensure user is a member of all linked projects (fix for projects created before ProjectMember fix)
    ensure_project_member_for_session_projects(session, planning_session_id, actor_user_id)

    drafts = (
        session.query(AIPlanningDraft)
        .filter(
            AIPlanningDraft.session_id == planning_session_id,
            AIPlanningDraft.id.in_(draft_ids),
        )
        .all()
    )

    saved_cases: list[SavedCaseResult] = []
    for draft in drafts:
        if cancel_event.is_set():
            raise RunnerCancelledError("Execution cancelled by user.", step_results=[])
        if not draft.dsl_case_json:
            continue
        case_payload = CaseCreateRequest(
            project_id=_get_active_project_id(planning_session),
            actor_user_id=actor_user_id,
            **draft.dsl_case_json,
        )
        case = create_case(session, case_payload, actor_user_id=actor_user_id)
        saved_cases.append(SavedCaseResult(case_id=case.id, case_name=case.name))
        draft.status = "imported"
        logger.info("[session:%d] Saved case '%s' (id=%d)", planning_session_id, case.name, case.id)
        save_event = {
            "type": "save_progress",
            "saved_count": len(saved_cases),
            "total": len(drafts),
            "case_name": case.name,
        }
        event_log.write("save_progress", save_event)
        yield save_event

    if not saved_cases:
        planning_session.status = "saving"
        session.commit()
        # Check if any drafts failed (e.g. due to exploration failure)
        failed_errors: list[str] = []
        for d in drafts:
            if d.error_message and d.error_message not in failed_errors:
                failed_errors.append(d.error_message)
        detail = "; ".join(failed_errors[:2]) if failed_errors else "所有选中草案均无有效 DSL"
        error_event = {
            "type": "error",
            "message": f"没有可保存的测试用例。{detail}",
            "error_type": "no_saved_cases",
            "phase": "execute",
        }
        event_log.write("error", error_event)
        event_log.flush()
        yield error_event
        return

    if not input_values:
        input_values = build_input_values_from_session(
            planning_session.requirements_json or {},
            [draft.dsl_case_json for draft in drafts if draft.dsl_case_json],
        )
        logger.info(
            "Streaming execution auto-resolved input_values from session data: %s",
            {key: value[:3] + "***" for key, value in input_values.items()} if input_values else {},
        )

    execution_summaries: list[ExecutionSummaryResult] = []
    for saved in saved_cases:
        if cancel_event.is_set():
            raise RunnerCancelledError("Execution cancelled by user.", step_results=[])

        payload = CaseExecutionRequest(actor_user_id=actor_user_id, input_values=input_values or {})
        dsl_case = None
        case_record = session.get(TestCase, saved.case_id)
        if case_record:
            from app.schemas.dsl import DSLCase
            dsl_case = DSLCase.model_validate(case_record.dsl)

        case_start_event = {
            "type": "case_start",
            "case_id": saved.case_id,
            "case_name": saved.case_name,
            "total_steps": len(dsl_case.steps) if dsl_case else 0,
        }
        event_log.write("case_start", case_start_event)
        yield case_start_event

        case_start_time = time.monotonic()
        logger.info("[session:%d] Executing case '%s' (id=%d), steps=%d", planning_session_id, saved.case_name, saved.case_id, len(dsl_case.steps) if dsl_case else 0)

        try:
            stream = execute_case_streaming(
                session, saved.case_id, payload, cancel_event=cancel_event,
            )
            detail = None
            try:
                while True:
                    step_event = next(stream)
                    step_dict = {
                        "type": step_event.type,
                        "case_id": saved.case_id,
                        "step_index": step_event.step_index,
                        "action": step_event.action,
                        **({"target": step_event.target} if step_event.target is not None else {}),
                        **({"value": step_event.value} if step_event.value is not None else {}),
                        **({"status": step_event.status} if step_event.status is not None else {}),
                        **({"duration_ms": step_event.duration_ms} if step_event.duration_ms is not None else {}),
                    }
                    event_log.write(step_event.type, step_dict)
                    yield step_dict
            except StopIteration as stop:
                detail = stop.value

            if detail is not None:
                passed = sum(1 for s in (detail.report.steps or []) if s.status == "passed")
                failed = sum(1 for s in (detail.report.steps or []) if s.status == "failed")
                case_elapsed = time.monotonic() - case_start_time
                logger.info(
                    "[session:%d] Case '%s' done, status=%s, passed=%d, failed=%d, duration=%.2fs",
                    planning_session_id, detail.case_name, detail.status, passed, failed, case_elapsed,
                )
                execution_summaries.append(ExecutionSummaryResult(
                    execution_id=detail.id,
                    case_id=saved.case_id,
                    case_name=detail.case_name,
                    status=detail.status,
                    total_steps=detail.total_steps,
                    passed_steps=passed,
                    failed_steps=failed,
                    duration_ms=detail.duration_ms,
                    screenshot_url=detail.latest_screenshot_url,
                    report_url=f"/run/{detail.id}",
                ))
        except RunnerCancelledError:
            raise

    for saved in saved_cases:
        draft = next(
            (
                item
                for item in drafts
                if item.dsl_case_json and item.dsl_case_json.get("name") == saved.case_name
            ),
            None,
        )
        if draft is None:
            continue
        try:
            _record_execution_anti_patterns(
                session,
                saved.case_id,
                draft.scenario_key,
                _get_active_project_id(planning_session),
            )
        except Exception as exc:
            logger.warning("Streaming execution anti-pattern recording failed: %s", exc)

    # Persist execution summary message
    lines = ["测试执行完成：\n"]
    for ex in execution_summaries:
        icon = "✅" if ex.status == "passed" else "❌"
        lines.append(f"{icon} {ex.case_name} — {ex.status} ({ex.passed_steps}/{ex.total_steps}步)")

    assistant_message = "\n".join(lines)
    planning_session.status = "completed"
    session.add(
        AIPlanningMessage(
            session_id=planning_session.id,
            role="assistant",
            turn_type="plan",
            content=assistant_message,
            structured_payload_json={
                "type": "execution_summary",
                "saved_cases": [item.model_dump(mode="json") for item in saved_cases],
                "execution_summaries": [item.model_dump(mode="json") for item in execution_summaries],
            },
        )
    )
    session.commit()

    if should_run_analysis(execution_summaries):
        status_event = {"type": "status", "phase": "analyzing", "message": "正在分析执行结果..."}
        event_log.write("status", status_event)
        yield status_event
        analysis_response = run_analysis_turn(
            execution_summaries=execution_summaries,
            db_session=session,
            project_id=_get_active_project_id(planning_session),
        )
        if analysis_response and analysis_response.execution_analysis:
            analysis_msg = analysis_response.assistant_message
            session.add(
                AIPlanningMessage(
                    session_id=planning_session.id,
                    role="assistant",
                    turn_type="followup",
                    content=analysis_msg,
                    structured_payload_json={
                        "type": "execution_analysis",
                        "analysis": analysis_response.execution_analysis.model_dump(mode="json"),
                    },
                )
            )
            session.commit()
            analysis_event = {
                "type": "analysis_complete",
                "analysis": analysis_response.execution_analysis.model_dump(mode="json"),
                "message": analysis_msg,
            }
            event_log.write("analysis_complete", analysis_event)
            yield analysis_event

    elapsed_total = time.monotonic() - start_time
    logger.info(
        "[session:%d] Save-and-execute streaming done, cases=%d, duration=%.2fs",
        planning_session_id, len(saved_cases), elapsed_total,
    )
    done_event = {"type": "done"}
    event_log.write("done", done_event)
    event_log.flush()
    yield done_event
