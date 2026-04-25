"""Services for AI planning sessions and drafts."""

from __future__ import annotations

import logging

from sqlalchemy import func, select
from sqlalchemy.orm import Session

from app.ai.test_planning_agent import REQUIRED_REQUIREMENT_SLOTS, run_planning_turn, stream_planning_turn
from app.models import AIPlanningDraft, AIPlanningMessage, AIPlanningSession, DslGenerationRun, Project, TestCase
from app.schemas.ai_planning import (
    AIPlanningDraft as AIPlanningDraftSchema,
    AIPlanningMessage as AIPlanningMessageSchema,
    AIPlanningRequirements,
    AIPlanningSession as AIPlanningSessionSchema,
    AIPlanningSessionDetail,
    AIPlanningSessionSummary,
    AIPlanningToolCall,
    AIPlanningTurnResponse,
    CreateAIPlanningSessionRequest,
    ExecutionSummaryResult,
    GenerateAIPlanningDraftsRequest,
    SavedCaseResult,
    UpdateAIPlanningDraftStatusRequest,
)
from app.schemas.cases import CaseCreateRequest
from app.schemas.dsl import GenerateDslRequest
from app.schemas.executions import CaseExecutionRequest
from app.services.cases import EntityNotFoundError, _ensure_project_member, create_case
from app.services.dsl import generate_dsl_case
from app.services.executions import execute_case, execute_case_streaming


logger = logging.getLogger(__name__)


class AIPlanningAccessError(ValueError):
    """Raised when a planning session or draft is inaccessible."""


def list_planning_sessions(
    session: Session,
    *,
    actor_user_id: int,
    project_id: int | None = None,
) -> list[AIPlanningSessionSummary]:
    q = session.query(AIPlanningSession).filter(AIPlanningSession.actor_user_id == actor_user_id)
    if project_id is not None:
        q = q.filter(AIPlanningSession.project_id == project_id)
    q = q.order_by(AIPlanningSession.updated_at.desc())
    rows = q.all()
    return [
        AIPlanningSessionSummary(
            id=r.id,
            title=r.title or (r.requirements_json or {}).get("app_under_test"),
            status=r.status,
            created_at=r.created_at,
            updated_at=r.updated_at,
        )
        for r in rows
    ]


def create_planning_session(
    session: Session,
    payload: CreateAIPlanningSessionRequest,
    *,
    actor_user_id: int,
) -> AIPlanningSessionDetail:
    if payload.project_id is not None:
        _ensure_project_access(session, project_id=payload.project_id, actor_user_id=actor_user_id)
        if payload.case_id is not None:
            _ensure_case_access(session, case_id=payload.case_id, project_id=payload.project_id, actor_user_id=actor_user_id)

    record = AIPlanningSession(
        actor_user_id=actor_user_id,
        project_id=payload.project_id,
        case_id=payload.case_id,
        status="collecting",
        requirements_json=AIPlanningRequirements().model_dump(mode="json"),
        missing_slots_json=list(REQUIRED_REQUIREMENT_SLOTS),
    )
    session.add(record)
    session.commit()
    session.refresh(record)
    return get_planning_session_detail(session, record.id, actor_user_id=actor_user_id)


def get_planning_session_detail(session: Session, planning_session_id: int, *, actor_user_id: int) -> AIPlanningSessionDetail:
    planning_session = _get_session(session, planning_session_id, actor_user_id=actor_user_id)
    messages = session.scalars(
        select(AIPlanningMessage).where(AIPlanningMessage.session_id == planning_session_id).order_by(AIPlanningMessage.id.asc())
    ).all()
    drafts = session.scalars(
        select(AIPlanningDraft).where(AIPlanningDraft.session_id == planning_session_id).order_by(AIPlanningDraft.id.asc())
    ).all()
    return AIPlanningSessionDetail(
        session=_to_session_schema(planning_session),
        messages=[_to_message_schema(item) for item in messages],
        drafts=[_to_draft_schema(item) for item in drafts],
    )


def send_planning_message(
    session: Session,
    planning_session_id: int,
    *,
    actor_user_id: int,
    content: str,
) -> AIPlanningTurnResponse:
    planning_session = _get_session(session, planning_session_id, actor_user_id=actor_user_id)
    session.add(
        AIPlanningMessage(
            session_id=planning_session.id,
            role="user",
            turn_type="user",
            content=content,
            structured_payload_json=None,
        )
    )
    session.flush()

    transcript_records = session.scalars(
        select(AIPlanningMessage).where(AIPlanningMessage.session_id == planning_session.id).order_by(AIPlanningMessage.id.asc())
    ).all()
    base_transcript = [{"role": item.role, "content": item.content} for item in transcript_records if item.turn_type != "tool_call"]
    base_transcript = _inject_auto_context(base_transcript, planning_session, session, len(transcript_records))
    agent_response = run_planning_turn(
        transcript=base_transcript,
        existing_requirements=AIPlanningRequirements.model_validate(planning_session.requirements_json or {}),
        db_session=session,
        project_id=planning_session.project_id,
    )

    planning_session.status = agent_response.session_status
    planning_session.requirements_json = agent_response.requirements.model_dump(mode="json")
    planning_session.plan_json = agent_response.plan.model_dump(mode="json") if agent_response.plan is not None else None
    planning_session.missing_slots_json = agent_response.missing_slots
    planning_session.title = planning_session.title or agent_response.requirements.business_goal or "AI 测试规划"
    planning_session.last_error_message = (
        agent_response.assistant_message if agent_response.session_status == "error" else None
    )

    for tool_call in agent_response.tool_calls:
        session.add(
            AIPlanningMessage(
                session_id=planning_session.id,
                role="assistant",
                turn_type="tool_call",
                content=f"调用工具 {tool_call.tool}",
                structured_payload_json={
                    "type": "tool_call",
                    **tool_call.model_dump(mode="json"),
                },
            )
        )

    turn_type = "system_error" if agent_response.session_status == "error" else ("plan" if agent_response.plan is not None else "followup")
    session.add(
        AIPlanningMessage(
            session_id=planning_session.id,
            role="assistant",
            turn_type=turn_type,
            content=agent_response.assistant_message,
            structured_payload_json={
                "missing_slots": agent_response.missing_slots,
                "suggested_questions": agent_response.suggested_questions,
                "plan": agent_response.plan.model_dump(mode="json") if agent_response.plan is not None else None,
                "tool_calls": [item.model_dump(mode="json") for item in agent_response.tool_calls],
                "todo_list": [item.model_dump(mode="json") for item in agent_response.todo_list],
            },
        )
    )
    session.commit()
    session.refresh(planning_session)
    return agent_response


def stream_planning_message(
    session: Session,
    planning_session_id: int,
    *,
    actor_user_id: int,
    content: str,
):
    """Generator: save user msg, stream AI turn, save AI msg, yield events."""
    from typing import Generator

    planning_session = _get_session(session, planning_session_id, actor_user_id=actor_user_id)
    session.add(
        AIPlanningMessage(
            session_id=planning_session.id,
            role="user",
            turn_type="user",
            content=content,
            structured_payload_json=None,
        )
    )
    session.flush()

    transcript_records = session.scalars(
        select(AIPlanningMessage).where(AIPlanningMessage.session_id == planning_session.id).order_by(AIPlanningMessage.id.asc())
    ).all()

    base_transcript = [{"role": item.role, "content": item.content} for item in transcript_records if item.turn_type != "tool_call"]
    base_transcript = _inject_auto_context(base_transcript, planning_session, session, len(transcript_records))
    stream = stream_planning_turn(
        transcript=base_transcript,
        existing_requirements=AIPlanningRequirements.model_validate(planning_session.requirements_json or {}),
        db_session=session,
        project_id=planning_session.project_id,
    )
    response = None
    while True:
        try:
            event = next(stream)
            yield event
        except StopIteration as stop:
            response = stop.value
            break

    planning_session.status = response.session_status
    planning_session.requirements_json = response.requirements.model_dump(mode="json")
    planning_session.plan_json = response.plan.model_dump(mode="json") if response.plan is not None else None
    planning_session.missing_slots_json = response.missing_slots
    planning_session.title = planning_session.title or response.requirements.business_goal or "AI 测试规划"
    planning_session.last_error_message = (
        response.assistant_message if response.session_status == "error" else None
    )

    for tool_call in response.tool_calls:
        session.add(
            AIPlanningMessage(
                session_id=planning_session.id,
                role="assistant",
                turn_type="tool_call",
                content=f"调用工具 {tool_call.tool}",
                structured_payload_json={
                    "type": "tool_call",
                    **tool_call.model_dump(mode="json"),
                },
            )
        )

    turn_type = "system_error" if response.session_status == "error" else ("plan" if response.plan is not None else "followup")
    session.add(
        AIPlanningMessage(
            session_id=planning_session.id,
            role="assistant",
            turn_type=turn_type,
            content=response.assistant_message,
            structured_payload_json={
                "missing_slots": response.missing_slots,
                "suggested_questions": response.suggested_questions,
                "plan": response.plan.model_dump(mode="json") if response.plan is not None else None,
                "tool_calls": [item.model_dump(mode="json") for item in response.tool_calls],
                "todo_list": [item.model_dump(mode="json") for item in response.todo_list],
            },
        )
    )
    session.commit()
    return response


def generate_planning_drafts(
    session: Session,
    planning_session_id: int,
    payload: GenerateAIPlanningDraftsRequest,
    *,
    actor_user_id: int,
) -> AIPlanningTurnResponse:
    planning_session = _get_session(session, planning_session_id, actor_user_id=actor_user_id)
    plan = planning_session.plan_json or {}
    scenarios = {
        item["scenario_key"]: item
        for item in plan.get("scenarios", [])
        if isinstance(item, dict) and isinstance(item.get("scenario_key"), str)
    }
    drafts: list[AIPlanningDraftSchema] = []
    base_url = _normalize_base_url(planning_session.requirements_json or {})
    invalid_scenarios: list[str] = []

    for scenario_key in payload.scenario_keys:
        scenario = scenarios.get(scenario_key)
        if scenario is None:
            invalid_scenarios.append(scenario_key)
            record = AIPlanningDraft(
                session_id=planning_session.id,
                scenario_key=scenario_key,
                title=f"场景 {scenario_key} 不存在",
                status="failed",
                dsl_generation_id=None,
                dsl_case_json=None,
                warnings_json=[f"场景 '{scenario_key}' 未在 AI 生成的测试计划中找到"],
                normalization_notes_json=[],
                error_message=f"场景 '{scenario_key}' 不存在于当前测试计划中。",
            )
            session.add(record)
            session.flush()
            drafts.append(_to_draft_schema(record))
            continue

        existing = session.scalar(
            select(AIPlanningDraft).where(
                AIPlanningDraft.session_id == planning_session.id,
                AIPlanningDraft.scenario_key == scenario_key,
            )
        )
        if existing is not None:
            drafts.append(_to_draft_schema(existing))
            continue

        try:
            generated = generate_dsl_case(
                session,
                GenerateDslRequest(
                    prompt=scenario["draft_prompt"],
                    base_url=base_url,
                    actor_user_id=actor_user_id,
                    project_id=planning_session.project_id,
                    case_id=planning_session.case_id,
                    current_case=payload.current_case,
                    current_steps=payload.current_steps,
                    current_input_contract=payload.current_input_contract,
                    current_output_contract=payload.current_output_contract,
                    preserve_contracts=payload.preserve_contracts,
                    page_elements=scenario.get("page_elements"),
                ),
            )
            record = AIPlanningDraft(
                session_id=planning_session.id,
                scenario_key=scenario_key,
                title=scenario["title"],
                status="generated",
                dsl_generation_id=(
                    generated.generation_id if session.get(DslGenerationRun, generated.generation_id) is not None else None
                ),
                dsl_case_json=generated.case.model_dump(mode="json"),
                warnings_json=generated.warnings,
                normalization_notes_json=generated.normalization_notes,
                error_message=None,
            )
        except Exception as exc:
            logger.error(
                "Failed to generate DSL case for scenario '%s' in session %s",
                scenario_key,
                planning_session.id,
                exc_info=True,
            )
            record = AIPlanningDraft(
                session_id=planning_session.id,
                scenario_key=scenario_key,
                title=scenario["title"],
                status="failed",
                dsl_generation_id=None,
                dsl_case_json=None,
                warnings_json=[],
                normalization_notes_json=[],
                error_message=str(exc),
            )
        session.add(record)
        session.flush()
        drafts.append(_to_draft_schema(record))

    message = "已根据所选场景生成 DSL 草案。"
    if invalid_scenarios:
        message += f" 注意：以下场景不存在于当前测试计划中：{', '.join(invalid_scenarios)}"

    planning_session.status = "drafts_ready"
    session.add(
        AIPlanningMessage(
            session_id=planning_session.id,
            role="assistant",
            turn_type="plan",
            content=message,
            structured_payload_json={
                "type": "draft_generation_result",
                "drafts": [item.model_dump(mode="json") for item in drafts],
            },
        )
    )
    session.commit()
    session.refresh(planning_session)

    return AIPlanningTurnResponse(
        assistant_message=message,
        session_status="drafts_ready",
        requirements=AIPlanningRequirements.model_validate(planning_session.requirements_json or {}),
        missing_slots=planning_session.missing_slots_json or [],
        suggested_questions=[],
        plan=_to_session_schema(planning_session).plan,
        drafts=drafts,
        next_action="drafts_generated",
        tool_calls=[],
    )


def stream_generate_planning_drafts(
    session: Session,
    planning_session_id: int,
    payload: GenerateAIPlanningDraftsRequest,
    *,
    actor_user_id: int,
):
    """Generator: yield draft_generating events, then delegate to generate_planning_drafts."""
    for scenario_key in payload.scenario_keys:
        yield {
            "type": "draft_generating",
            "scenario_key": scenario_key,
            "message": f"正在生成 {scenario_key} 的 DSL...",
        }

    result = generate_planning_drafts(
        session,
        planning_session_id,
        payload,
        actor_user_id=actor_user_id,
    )
    yield {
        "type": "turn_complete",
        "session_status": result.session_status,
        "payload": {
            "assistant_message": result.assistant_message,
            "drafts": [item.model_dump(mode="json") for item in result.drafts],
            "plan": result.plan.model_dump(mode="json") if result.plan else None,
        },
    }
    return result


def update_planning_draft_status(
    session: Session,
    draft_id: int,
    payload: UpdateAIPlanningDraftStatusRequest,
    *,
    actor_user_id: int,
) -> AIPlanningDraftSchema:
    draft = session.get(AIPlanningDraft, draft_id)
    if draft is None:
        raise EntityNotFoundError(f"AI planning draft {draft_id} not found.")
    _get_session(session, draft.session_id, actor_user_id=actor_user_id)
    draft.status = payload.status
    session.add(draft)
    session.commit()
    session.refresh(draft)
    return _to_draft_schema(draft)


def _should_run_analysis(
    execution_summaries: list[ExecutionSummaryResult],
) -> bool:
    """Return True if any execution result is not passed."""
    return any(s.status != "passed" for s in execution_summaries)


def _build_analysis_context(
    execution_summaries: list[ExecutionSummaryResult],
    db_session: Session,
) -> str:
    """Build a context message for the analysis turn from execution summaries."""
    lines = ["本轮执行已完成，请分析以下结果：\n"]
    for ex in execution_summaries:
        icon = "✅" if ex.status == "passed" else "❌"
        failure_info = ""
        if ex.status != "passed":
            failure_info = f" [失败步骤: {ex.failed_steps}步]"
        lines.append(
            f"{icon} {ex.case_name} — {ex.status} "
            f"({ex.passed_steps}/{ex.total_steps}步){failure_info}"
        )
    lines.append("\n请使用 analyze_results 模式输出分析报告。")
    return "\n".join(lines)


def _run_analysis_turn(
    *,
    execution_summaries: list[ExecutionSummaryResult],
    db_session: Session,
    project_id: int,
) -> AIPlanningTurnResponse | None:
    """Run an analysis turn using the AI agent with execution results as context."""
    try:
        context_message = _build_analysis_context(execution_summaries, db_session)
        transcript = [{"role": "user", "content": context_message}]
        return run_planning_turn(
            transcript=transcript,
            existing_requirements=None,
            db_session=db_session,
            project_id=project_id,
        )
    except Exception:
        logger.warning("Auto-analysis turn failed", exc_info=True)
        return None


def _build_session_context_preamble(
    planning_session: AIPlanningSession,
    db_session: Session,
    existing_msg_count: int,
) -> str | None:
    """Build an auto-context preamble with current project test status.

    Returns None if injection is not needed (first turn or no project).
    """
    if not planning_session.project_id or existing_msg_count <= 1:
        return None

    from app.ai.planning_tools import _handle_get_project_test_status
    try:
        status = _handle_get_project_test_status(
            params={}, db_session=db_session, project_id=planning_session.project_id,
        )
    except Exception:
        logger.warning("Auto-context injection: failed to query project status", exc_info=True)
        return None

    conclusion_labels = {
        "all_passed": "全部通过", "partial": "部分通过",
        "all_failed": "全部失败", "no_runs": "无执行记录",
    }
    conclusion = status.get("conclusion", "unknown")
    if conclusion == "no_runs":
        return None

    lines = ["[系统自动注入 - 当前项目测试状态]"]
    lines.append(f"整体结论：{conclusion_labels.get(conclusion, conclusion)}")
    for case in status.get("cases", []):
        cs = case.get("latest_status", "unknown")
        if cs == "no_runs":
            continue
        icon = "✅" if cs == "passed" else "❌"
        p = case.get("passed_steps", 0)
        t = case.get("total_steps", 0)
        err = case.get("error_message", "")
        line = f"{icon} {case.get('case_name', '?')} — {cs} ({p}/{t}步)"
        if err:
            line += f" | 错误: {err}"
        lines.append(line)

    requirements = AIPlanningRequirements.model_validate(planning_session.requirements_json or {})
    tc = requirements.test_context
    if tc:
        if tc.get("suspected_root_cause"):
            lines.append(f"上次分析根因：{tc['suspected_root_cause']}")
        if tc.get("next_action"):
            lines.append(f"上次建议动作：{tc['next_action']}")
        if tc.get("regression_scope"):
            lines.append(f"上次回归范围：{tc['regression_scope']}")

    return "\n".join(lines)


def _inject_auto_context(
    transcript: list[dict[str, str]],
    planning_session: AIPlanningSession,
    db_session: Session,
    existing_msg_count: int,
) -> list[dict[str, str]]:
    """Prepend auto-context preamble to transcript if applicable."""
    preamble = _build_session_context_preamble(planning_session, db_session, existing_msg_count)
    if preamble is None:
        return transcript
    return [{"role": "system", "content": preamble}, *transcript]


def save_and_execute_selected_drafts(
    session: Session,
    planning_session_id: int,
    draft_ids: list[int],
    actor_user_id: int,
    execute: bool = True,
    input_values: dict[str, str] | None = None,
) -> AIPlanningTurnResponse:
    planning_session = _get_session(session, planning_session_id, actor_user_id=actor_user_id)

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
            project_id=planning_session.project_id,
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
    if _should_run_analysis(execution_summaries):
        analysis_response = _run_analysis_turn(
            execution_summaries=execution_summaries,
            db_session=db_session,
            project_id=planning_session.project_id or 1,
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
):
    """Generator version of save_and_execute_selected_drafts for WebSocket streaming.

    Yields progress event dicts. After all cases complete, persists the execution
    summary message and yields a ``done`` event.
    """
    from threading import Event as ThreadEvent
    from app.runners.playwright_runner import RunnerCancelledError

    if cancel_event is None:
        cancel_event = ThreadEvent()

    planning_session = _get_session(session, planning_session_id, actor_user_id=actor_user_id)

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
            project_id=planning_session.project_id,
            actor_user_id=actor_user_id,
            **draft.dsl_case_json,
        )
        case = create_case(session, case_payload, actor_user_id=actor_user_id)
        saved_cases.append(SavedCaseResult(case_id=case.id, case_name=case.name))
        draft.status = "imported"
        yield {
            "type": "save_progress",
            "saved_count": len(saved_cases),
            "total": len(drafts),
            "case_name": case.name,
        }

    if not saved_cases:
        planning_session.status = "saving"
        session.commit()
        yield {"type": "done"}
        return

    execution_summaries: list[ExecutionSummaryResult] = []
    for saved in saved_cases:
        if cancel_event.is_set():
            raise RunnerCancelledError("Execution cancelled by user.", step_results=[])

        payload = CaseExecutionRequest(actor_user_id=actor_user_id, input_values=input_values or {})
        dsl_case = None
        case_record = session.query(TestCase).get(saved.case_id)
        if case_record:
            from app.schemas.dsl import DSLCase
            dsl_case = DSLCase.model_validate(case_record.dsl)

        yield {
            "type": "case_start",
            "case_id": saved.case_id,
            "case_name": saved.case_name,
            "total_steps": len(dsl_case.steps) if dsl_case else 0,
        }

        try:
            stream = execute_case_streaming(
                session, saved.case_id, payload, cancel_event=cancel_event,
            )
            detail = None
            try:
                while True:
                    step_event = next(stream)
                    yield {
                        "type": step_event.type,
                        "case_id": saved.case_id,
                        "step_index": step_event.step_index,
                        "action": step_event.action,
                        **({"target": step_event.target} if step_event.target is not None else {}),
                        **({"value": step_event.value} if step_event.value is not None else {}),
                        **({"status": step_event.status} if step_event.status is not None else {}),
                        **({"duration_ms": step_event.duration_ms} if step_event.duration_ms is not None else {}),
                    }
            except StopIteration as stop:
                detail = stop.value

            if detail is not None:
                passed = sum(1 for s in (detail.report.steps or []) if s.status == "passed")
                failed = sum(1 for s in (detail.report.steps or []) if s.status == "failed")
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

    if _should_run_analysis(execution_summaries):
        yield {"type": "status", "phase": "analyzing", "message": "正在分析执行结果..."}
        analysis_response = _run_analysis_turn(
            execution_summaries=execution_summaries,
            db_session=db_session,
            project_id=planning_session.project_id or 1,
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
            yield {
                "type": "analysis_complete",
                "analysis": analysis_response.execution_analysis.model_dump(mode="json"),
                "message": analysis_msg,
            }

    yield {"type": "done"}


def retest_cases(
    session: Session,
    planning_session_id: int,
    *,
    actor_user_id: int,
    case_ids: list[int] | None = None,
    failed_only: bool = False,
    input_values: dict[str, str] | None = None,
) -> AIPlanningTurnResponse:
    """Re-execute existing test cases from a planning session and run auto-analysis."""
    planning_session = _get_session(session, planning_session_id, actor_user_id=actor_user_id)

    if not case_ids and failed_only:
        from app.ai.planning_tools import _handle_get_recommended_retest
        recommendation = _handle_get_recommended_retest(
            params={}, db_session=session, project_id=planning_session.project_id or 0,
        )
        case_ids = recommendation.get("retest_case_ids", [])
        if not case_ids:
            return AIPlanningTurnResponse(
                assistant_message="当前没有需要复测的失败用例。",
                session_status="completed",
                requirements=AIPlanningRequirements.model_validate(planning_session.requirements_json or {}),
                missing_slots=[],
                suggested_questions=[],
                plan=None,
                drafts=[],
                next_action="ask_followup",
                saved_cases=[],
                execution_summaries=[],
            )
    elif not case_ids:
        return AIPlanningTurnResponse(
            assistant_message="请指定要复测的用例 ID 或使用 failed_only=true。",
            session_status="completed",
            requirements=AIPlanningRequirements.model_validate(planning_session.requirements_json or {}),
            missing_slots=[],
            suggested_questions=[],
            plan=None,
            drafts=[],
            next_action="ask_followup",
            saved_cases=[],
            execution_summaries=[],
        )

    execution_summaries: list[ExecutionSummaryResult] = []
    for case_id in case_ids:
        case_record = session.get(TestCase, case_id)
        if case_record is None or case_record.project_id != planning_session.project_id:
            continue
        payload = CaseExecutionRequest(actor_user_id=actor_user_id, input_values=input_values or {})
        result = execute_case(session, case_id, payload)
        passed = sum(1 for s in (result.report.steps or []) if s.status == "passed")
        failed = sum(1 for s in (result.report.steps or []) if s.status == "failed")
        execution_summaries.append(ExecutionSummaryResult(
            execution_id=result.id,
            case_id=case_id,
            case_name=result.case_name,
            status=result.status,
            total_steps=result.total_steps,
            passed_steps=passed,
            failed_steps=failed,
            duration_ms=result.duration_ms,
            screenshot_url=result.latest_screenshot_url,
            report_url=f"/run/{result.id}",
        ))

    lines = [f"复测完成（{len(execution_summaries)} 个用例）：\n"]
    for ex in execution_summaries:
        icon = "✅" if ex.status == "passed" else "❌"
        lines.append(f"{icon} {ex.case_name} — {ex.status} ({ex.passed_steps}/{ex.total_steps}步)")

    assistant_message = "\n".join(lines)
    planning_session.status = "completed"

    execution_analysis = None
    if _should_run_analysis(execution_summaries):
        analysis_response = _run_analysis_turn(
            execution_summaries=execution_summaries,
            db_session=session,
            project_id=planning_session.project_id or 1,
        )
        if analysis_response and analysis_response.execution_analysis:
            execution_analysis = analysis_response.execution_analysis
            assistant_message = f"{assistant_message}\n\n---\n\n{analysis_response.assistant_message}"

    session.add(
        AIPlanningMessage(
            session_id=planning_session.id,
            role="assistant",
            turn_type="plan",
            content=assistant_message,
            structured_payload_json={
                "type": "retest_summary",
                "retest_case_ids": case_ids,
                "execution_summaries": [item.model_dump(mode="json") for item in execution_summaries],
                "analysis": execution_analysis.model_dump(mode="json") if execution_analysis else None,
            },
        )
    )
    session.commit()
    session.refresh(planning_session)

    return AIPlanningTurnResponse(
        assistant_message=assistant_message,
        session_status="completed",
        requirements=AIPlanningRequirements.model_validate(planning_session.requirements_json or {}),
        missing_slots=[],
        suggested_questions=[],
        plan=None,
        drafts=[],
        next_action="ask_followup",
        saved_cases=[],
        execution_summaries=execution_summaries,
        execution_analysis=execution_analysis,
    )


def delete_planning_session(
    session: Session,
    planning_session_id: int,
    *,
    actor_user_id: int,
) -> None:
    planning_session = _get_session(session, planning_session_id, actor_user_id=actor_user_id)
    session.delete(planning_session)
    session.commit()


def delete_planning_draft(
    session: Session,
    draft_id: int,
    *,
    actor_user_id: int,
) -> None:
    """Delete a single planning draft (owner only)."""
    draft = session.get(AIPlanningDraft, draft_id)
    if draft is None:
        raise EntityNotFoundError(f"AI planning draft {draft_id} not found.")
    # Verify the user owns the parent session
    _get_session(session, draft.session_id, actor_user_id=actor_user_id)
    session.delete(draft)
    session.commit()


def _get_session(session: Session, planning_session_id: int, *, actor_user_id: int) -> AIPlanningSession:
    planning_session = session.get(AIPlanningSession, planning_session_id)
    if planning_session is None:
        raise EntityNotFoundError(f"AI planning session {planning_session_id} not found.")
    if planning_session.actor_user_id != actor_user_id:
        raise AIPlanningAccessError("AI planning session access denied.")
    return planning_session


def _ensure_project_access(session: Session, *, project_id: int, actor_user_id: int) -> None:
    if session.get(Project, project_id) is None:
        raise EntityNotFoundError(f"Project {project_id} not found.")
    _ensure_project_member(session, project_id, actor_user_id)


def _ensure_case_access(session: Session, *, case_id: int, project_id: int, actor_user_id: int) -> None:
    case_record = session.get(TestCase, case_id)
    if case_record is None:
        raise EntityNotFoundError(f"Case {case_id} not found.")
    if case_record.project_id != project_id:
        raise EntityNotFoundError(f"Case {case_id} does not belong to project {project_id}.")
    _ensure_project_member(session, project_id, actor_user_id)


def _normalize_base_url(requirements_json: dict) -> str | None:
    value = requirements_json.get("entry_url_or_page")
    if isinstance(value, str) and value.startswith(("http://", "https://")):
        return value
    return None


def _to_session_schema(record: AIPlanningSession) -> AIPlanningSessionSchema:
    return AIPlanningSessionSchema(
        id=record.id,
        actor_user_id=record.actor_user_id,
        project_id=record.project_id,
        case_id=record.case_id,
        title=record.title,
        status=record.status,
        requirements=AIPlanningRequirements.model_validate(record.requirements_json or {}),
        plan=record.plan_json,
        missing_slots=record.missing_slots_json or [],
        last_error_message=record.last_error_message,
        created_at=record.created_at,
        updated_at=record.updated_at,
    )


def _to_message_schema(record: AIPlanningMessage) -> AIPlanningMessageSchema:
    return AIPlanningMessageSchema(
        id=record.id,
        session_id=record.session_id,
        role=record.role,
        turn_type=record.turn_type,
        content=record.content,
        structured_payload=record.structured_payload_json,
        created_at=record.created_at,
    )


def _to_draft_schema(record: AIPlanningDraft) -> AIPlanningDraftSchema:
    return AIPlanningDraftSchema(
        id=record.id,
        session_id=record.session_id,
        scenario_key=record.scenario_key,
        title=record.title,
        status=record.status,
        dsl_generation_id=record.dsl_generation_id,
        dsl_case=record.dsl_case_json,
        warnings=record.warnings_json or [],
        normalization_notes=record.normalization_notes_json or [],
        error_message=record.error_message,
        created_at=record.created_at,
        updated_at=record.updated_at,
    )
