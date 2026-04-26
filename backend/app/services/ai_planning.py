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
        response = run_planning_turn(
            transcript=transcript,
            existing_requirements=None,
            db_session=db_session,
            project_id=project_id,
        )
        # Auto-update insights after analysis
        _auto_update_insights(db_session, project_id, execution_summaries, response)
        return response
    except Exception:
        logger.warning("Auto-analysis turn failed", exc_info=True)
        return None


def _auto_update_insights(
    db_session: Session,
    project_id: int,
    execution_summaries: list[ExecutionSummaryResult],
    analysis_response: AIPlanningTurnResponse | None = None,
) -> None:
    """Auto-update TestPointInsight after analysis with flaky detection and risk assessment."""
    try:
        from sqlalchemy import select as sa_select
        from app.models import TestPointInsight, TestCase, TestCaseRun as Run

        insight = db_session.scalar(
            sa_select(TestPointInsight).where(TestPointInsight.project_id == project_id)
        )
        if insight is None:
            insight = TestPointInsight(project_id=project_id)
            db_session.add(insight)
            db_session.flush()

        # Detect flaky cases with improved scoring
        cases = db_session.scalars(
            sa_select(TestCase).where(TestCase.project_id == project_id)
        ).all()

        flaky_ids: list[int] = []
        pattern_data: dict[str, dict] = {}
        for case in cases:
            recent_runs = db_session.scalars(
                sa_select(Run)
                .where(Run.case_id == case.id)
                .order_by(Run.started_at.desc())
                .limit(6)
            ).all()

            if len(recent_runs) < 3:
                continue

            statuses = [r.status for r in recent_runs]
            pass_count = sum(1 for s in statuses if s == "passed")
            fail_count = sum(1 for s in statuses if s in ("failed", "needs_intervention"))

            if pass_count > 0 and fail_count > 0:
                switches = sum(1 for i in range(1, len(statuses)) if statuses[i] != statuses[i - 1])
                switch_ratio = switches / max(len(statuses) - 1, 1)
                balance = 1.0 - abs(pass_count - fail_count) / len(statuses)
                score = round(switch_ratio * balance, 2)
                if score >= 0.4:
                    flaky_ids.append(case.id)

            # Track failure categories
            consecutive_failures = 0
            for s in statuses:
                if s in ("failed", "needs_intervention"):
                    consecutive_failures += 1
                else:
                    break
            if consecutive_failures >= 2:
                error_msg = recent_runs[0].error_message or "unknown"
                category = _categorize_error(error_msg)
                if category not in pattern_data:
                    pattern_data[category] = {"count": 0, "cases": []}
                pattern_data[category]["count"] += consecutive_failures
                if case.id not in pattern_data[category]["cases"]:
                    pattern_data[category]["cases"].append(case.id)

        # Determine regression risk
        failed_count = sum(1 for s in execution_summaries if s.status != "passed")
        total_count = len(execution_summaries)
        if total_count > 0:
            fail_ratio = failed_count / total_count
            if fail_ratio >= 0.8:
                risk = "critical"
            elif fail_ratio >= 0.5:
                risk = "high"
            elif fail_ratio >= 0.3:
                risk = "medium"
            else:
                risk = "low"
        else:
            risk = "low"

        insight.flaky_case_ids = flaky_ids
        if pattern_data:
            insight.failure_patterns = pattern_data
        insight.regression_risk = risk

        if analysis_response and analysis_response.execution_analysis:
            summary = analysis_response.execution_analysis.suspected_root_cause or ""
            if summary:
                insight.last_analysis_summary = summary[:2000]

        db_session.flush()
    except Exception:
        logger.warning("Auto-update insights failed", exc_info=True)


def _categorize_error(error_message: str) -> str:
    """Categorize an error message into a failure pattern type."""
    msg = error_message.lower()
    if "locator" in msg or "not found" in msg or "no element" in msg:
        return "locator_stale"
    if "assertion" in msg or "expect" in msg or "mismatch" in msg:
        return "assertion_mismatch"
    if "timeout" in msg or "timed out" in msg:
        return "timeout"
    if "network" in msg or "connection" in msg or "econnrefused" in msg:
        return "network_error"
    return "unknown"


def _build_session_context_preamble(
    planning_session: AIPlanningSession,
    db_session: Session,
    existing_msg_count: int,
) -> str | None:
    """Build an auto-context preamble with current project test status and cross-session insights.

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

    # Session-level test_context
    requirements = AIPlanningRequirements.model_validate(planning_session.requirements_json or {})
    tc = requirements.test_context
    if tc:
        if tc.get("suspected_root_cause"):
            lines.append(f"上次分析根因：{tc['suspected_root_cause']}")
        if tc.get("next_action"):
            lines.append(f"上次建议动作：{tc['next_action']}")
        if tc.get("regression_scope"):
            lines.append(f"上次回归范围：{tc['regression_scope']}")

    # Cross-session insights from TestPointInsight
    try:
        from app.ai.planning_tools import _handle_get_project_insights
        insights = _handle_get_project_insights(
            params={}, db_session=db_session, project_id=planning_session.project_id,
        )
        if insights.get("has_insights"):
            lines.append("")
            lines.append("[历史洞察 - 跨会话积累]")
            if insights.get("regression_risk"):
                lines.append(f"回归风险等级：{insights['regression_risk']}")
            if insights.get("flaky_case_ids"):
                lines.append(f"已知 Flaky 用例 ID：{', '.join(str(i) for i in insights['flaky_case_ids'])}")
            if insights.get("last_analysis_summary"):
                lines.append(f"上次分析摘要：{insights['last_analysis_summary']}")
            fp = insights.get("failure_patterns", {})
            if fp:
                for pattern_name, pattern_info in fp.items():
                    if isinstance(pattern_info, dict):
                        lines.append(f"失败模式 {pattern_name}：出现 {pattern_info.get('count', '?')} 次")
    except Exception:
        logger.warning("Auto-context injection: failed to load cross-session insights", exc_info=True)

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


# ---------------------------------------------------------------------------
# Explorer-Judge: Router decision logic
# ---------------------------------------------------------------------------


def router_decide(
    verdict: "ExplorerJudgeVerdict",
    auto_fix_already_attempted: bool,
) -> "RouterDecision":
    """Deterministic routing based on Judge conclusions."""
    from app.schemas.explorer_judge import RouterDecision

    # Mandatory stop: product defect confirmed
    if verdict.is_suspected_product_bug:
        return RouterDecision(action="report_to_user", reason="产品缺陷已确认，需人工介入")

    # Mandatory stop: human judgment required
    if verdict.manual_intervention_needed:
        return RouterDecision(action="report_to_user", reason="需要人工判断业务规则")

    # Auto-fix: test design error, max once
    has_test_design_error = any(
        c.classification == "test_design_error" for c in verdict.conclusions
    )
    if has_test_design_error and not auto_fix_already_attempted:
        return RouterDecision(action="auto_fix_dsl", reason="Judge 判定为测试设计错误，尝试自动修复（最多一次）", retry_remaining=1)

    # Environment issue: report and skip
    has_env_issue = any(
        c.classification == "environment_dependency" for c in verdict.conclusions
    )
    if has_env_issue:
        return RouterDecision(action="report_to_user", reason="环境或依赖问题，标记跳过")

    # Default: report
    return RouterDecision(action="report_to_user", reason="分析完成，报告发现")


def build_aggregate_verdict(
    exploration_result: "ExplorationResult",
    judge_response: dict,
    case_id: int,
) -> "ExplorerJudgeVerdict":
    """Build the aggregate verdict from Judge response + Explorer result."""
    from app.schemas.explorer_judge import ExplorerJudgeVerdict, JudgeConclusion

    aggregate = judge_response.get("aggregate", {})
    conclusions_data = judge_response.get("conclusions", [])

    conclusions = []
    for c in conclusions_data:
        conclusions.append(JudgeConclusion(
            step_index=c.get("step_index", 0),
            classification=c.get("classification", "suspected_flaky"),
            confidence=c.get("confidence", "low"),
            root_cause_analysis=c.get("root_cause_analysis", ""),
            reproduction_path=c.get("reproduction_path", ""),
            suggested_action=c.get("suggested_action", "manual_intervention"),
            is_product_bug=c.get("is_product_bug", False),
            requires_human_judgment=c.get("requires_human_judgment", False),
            recommended_regression=c.get("recommended_regression", False),
        ))

    # Determine test_point_status
    if exploration_result.failed_steps == 0:
        status = "all_passed"
    elif any(c.classification == "product_defect" for c in conclusions):
        status = "has_defects"
    elif any(c.classification == "environment_dependency" for c in conclusions):
        status = "environment_blocked"
    elif any(c.classification == "suspected_flaky" for c in conclusions):
        status = "has_flaky"
    else:
        status = "needs_fix"

    return ExplorerJudgeVerdict(
        case_id=case_id,
        test_point_status=status,
        total_steps=exploration_result.total_steps,
        passed_steps=exploration_result.passed_steps,
        failed_steps=exploration_result.failed_steps,
        first_failed_step=aggregate.get("first_failed_step"),
        failure_phenomenon=aggregate.get("failure_phenomenon"),
        verification_actions=aggregate.get("verification_actions", []),
        possible_causes_ranked=aggregate.get("possible_causes_ranked", []),
        is_suspected_product_bug=aggregate.get("is_suspected_product_bug", False),
        regression_recommended=aggregate.get("regression_recommended", False),
        manual_intervention_needed=aggregate.get("manual_intervention_needed", False),
        conclusions=conclusions,
    )


# ---------------------------------------------------------------------------
# Explorer-Judge: streaming execution generator
# ---------------------------------------------------------------------------


def save_and_execute_with_explorer_judge_streaming(
    session_obj: Session,
    planning_session_id: int,
    draft_ids: list[int],
    actor_user_id: int,
    *,
    input_values: dict[str, str] | None = None,
    cancel_event=None,
):
    """Explorer-Judge streaming execution: full-path explore + batch judge.

    Yields progress event dicts for WebSocket streaming.
    """
    from threading import Event as ThreadEvent
    from app.models import ExplorationRun
    from app.runners.explorer_runner import ExplorerStepEvent, run_explorer
    from app.runners.playwright_runner import RunnerCancelledError
    from app.schemas.explorer_judge import ExplorerStepEvidence, ExplorationResult
    from app.ai.judge_agent import call_judge_llm

    if cancel_event is None:
        cancel_event = ThreadEvent()

    planning_session = _get_session(session_obj, planning_session_id, actor_user_id=actor_user_id)

    # Phase 1: Save drafts as cases (reuse existing logic)
    drafts = (
        session_obj.query(AIPlanningDraft)
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
        case = create_case(session_obj, case_payload, actor_user_id=actor_user_id)
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
        session_obj.commit()
        yield {"type": "done"}
        return

    exploration_results_map: dict[int, ExplorationResult] = {}

    for saved in saved_cases:
        if cancel_event.is_set():
            raise RunnerCancelledError("Execution cancelled by user.", step_results=[])

        case_record = session_obj.query(TestCase).get(saved.case_id)
        if not case_record or not case_record.dsl:
            continue

        from app.schemas.dsl import DSLCase
        dsl_case = DSLCase.model_validate(case_record.dsl)
        base_url = getattr(case_record, "base_url", None) or planning_session.requirements_json and planning_session.requirements_json.get("entry_url_or_page")

        # Create ExplorationRun record
        exploration_run = ExplorationRun(
            session_id=planning_session_id,
            case_id=saved.case_id,
            role="explorer",
            status="running",
        )
        session_obj.add(exploration_run)
        session_obj.flush()

        yield {
            "type": "explorer_start",
            "case_id": saved.case_id,
            "case_name": saved.case_name,
            "exploration_run_id": exploration_run.id,
            "total_steps": len(dsl_case.steps),
        }

        # Phase 2: Run Explorer (non-terminating)
        explorer_stream = run_explorer(
            case=dsl_case,
            execution_id=exploration_run.id,
            base_url=base_url,
            input_values=input_values,
            cancel_event=cancel_event,
        )
        exploration_result = None
        while True:
            try:
                event = next(explorer_stream)
                yield {
                    "type": event.type,
                    "case_id": saved.case_id,
                    "step_index": event.step_index,
                    "action": event.action,
                    **({"target": event.target} if event.target is not None else {}),
                    **({"status": event.status} if event.status is not None else {}),
                    **({"duration_ms": event.duration_ms} if event.duration_ms is not None else {}),
                }
            except StopIteration as stop:
                exploration_result = stop.value
                exploration_results_map[saved.case_id] = exploration_result
                break

        # Persist failure records
        failure_json = [r.model_dump(mode="json") for r in exploration_result.failure_records]
        exploration_run.failure_records_json = failure_json
        exploration_run.status = "completed"
        session_obj.flush()

        yield {
            "type": "explorer_complete",
            "case_id": saved.case_id,
            "exploration_run_id": exploration_run.id,
            "total_steps": exploration_result.total_steps,
            "passed_steps": exploration_result.passed_steps,
            "failed_steps": exploration_result.failed_steps,
            "cascade_blocked_steps": exploration_result.cascade_blocked_steps,
        }

        # Phase 3: Judge (only if there are failures)
        if not exploration_result.failure_records:
            exploration_run.judge_conclusions_json = []
            exploration_run.router_decision_json = {"action": "finished", "reason": "全部通过"}
            session_obj.flush()
            yield {
                "type": "verdict_report",
                "case_id": saved.case_id,
                "exploration_run_id": exploration_run.id,
                "verdict": {
                    "test_point_status": "all_passed",
                    "total_steps": exploration_result.total_steps,
                    "passed_steps": exploration_result.passed_steps,
                    "failed_steps": 0,
                    "conclusions": [],
                },
                "requires_user_action": False,
            }
            continue

        yield {
            "type": "judge_start",
            "case_id": saved.case_id,
            "failure_count": len(exploration_result.failure_records),
        }

        try:
            dsl_summary = [
                {"action": s.action, "target": getattr(s, "target", None), "value": getattr(s, "value", None)}
                for s in dsl_case.steps
            ]
            judge_response = call_judge_llm(
                exploration_result.failure_records,
                case_name=saved.case_name,
                dsl_steps_summary=dsl_summary,
            )
        except Exception as exc:
            logger.exception("Judge LLM call failed for exploration_run %d", exploration_run.id)
            exploration_run.judge_conclusions_json = []
            exploration_run.router_decision_json = {"action": "report_to_user", "reason": f"Judge 调用失败: {exc}"}
            session_obj.flush()
            yield {
                "type": "judge_complete",
                "case_id": saved.case_id,
                "error": str(exc),
            }
            yield {
                "type": "verdict_report",
                "case_id": saved.case_id,
                "exploration_run_id": exploration_run.id,
                "verdict": {
                    "test_point_status": "needs_fix",
                    "error": f"Judge analysis failed: {exc}",
                    "failed_steps": len(exploration_result.failure_records),
                },
                "requires_user_action": True,
            }
            continue

        exploration_run.judge_conclusions_json = judge_response
        session_obj.flush()

        yield {
            "type": "judge_complete",
            "case_id": saved.case_id,
            "conclusions": judge_response.get("conclusions", []),
            "aggregate": judge_response.get("aggregate", {}),
        }

        # Phase 4: Router decision
        verdict = build_aggregate_verdict(exploration_result, judge_response, saved.case_id)
        verdict.exploration_run_id = exploration_run.id
        decision = router_decide(verdict, exploration_run.auto_fix_attempted)

        exploration_run.router_decision_json = decision.model_dump(mode="json")
        session_obj.flush()

        # Phase 5: Auto-fix (max once)
        if decision.action == "auto_fix_dsl" and not exploration_run.auto_fix_attempted:
            yield {
                "type": "auto_fix_attempt",
                "case_id": saved.case_id,
                "reason": decision.reason,
            }
            exploration_run.auto_fix_attempted = True
            session_obj.flush()

            # Attempt DSL regeneration
            try:
                test_design_errors = [
                    c for c in verdict.conclusions if c.classification == "test_design_error"
                ]
                repair_prompt = _build_repair_prompt(case_record, test_design_errors)
                from app.schemas.dsl import GenerateDslRequest
                fix_result = generate_dsl_case(
                    session_obj,
                    GenerateDslRequest(
                        prompt=repair_prompt,
                        base_url=base_url,
                        actor_user_id=actor_user_id,
                        case_id=saved.case_id,
                        generation_mode="strict_steps_only",
                        import_mode="steps_only",
                    ),
                )
                yield {
                    "type": "auto_fix_result",
                    "case_id": saved.case_id,
                    "success": True,
                    "generation_id": fix_result.generation_id,
                }
                # TODO: Re-run Explorer with fixed DSL (next iteration)
            except Exception as exc:
                logger.exception("Auto-fix DSL regeneration failed for case %d", saved.case_id)
                yield {
                    "type": "auto_fix_result",
                    "case_id": saved.case_id,
                    "success": False,
                    "error_message": str(exc),
                }

        # Phase 6: Report verdict
        yield {
            "type": "verdict_report",
            "case_id": saved.case_id,
            "exploration_run_id": exploration_run.id,
            "verdict": verdict.model_dump(mode="json"),
            "requires_user_action": decision.action == "report_to_user",
        }

    # Persist execution summary message for Explorer-Judge flow
    if exploration_results_map:
        execution_summaries_ej: list[ExecutionSummaryResult] = []
        for saved in saved_cases:
            result = exploration_results_map.get(saved.case_id)
            if not result:
                continue
            status_val = "passed" if result.failed_steps == 0 else "failed"
            execution_summaries_ej.append(ExecutionSummaryResult(
                execution_id=0,
                case_id=saved.case_id,
                case_name=saved.case_name,
                status=status_val,
                total_steps=result.total_steps,
                passed_steps=result.passed_steps,
                failed_steps=result.failed_steps,
                duration_ms=None,
                screenshot_url=None,
                report_url="",
            ))

        lines_ej = ["Explorer-Judge 执行完成：\n"]
        for ex in execution_summaries_ej:
            icon = "✅" if ex.status == "passed" else "❌"
            lines_ej.append(f"{icon} {ex.case_name} — {ex.status} ({ex.passed_steps}/{ex.total_steps}步)")
        session_obj.add(
            AIPlanningMessage(
                session_id=planning_session.id,
                role="assistant",
                turn_type="plan",
                content="\n".join(lines_ej),
                structured_payload_json={
                    "type": "execution_summary",
                    "saved_cases": [item.model_dump(mode="json") for item in saved_cases],
                    "execution_summaries": [item.model_dump(mode="json") for item in execution_summaries_ej],
                },
            )
        )
        planning_session.status = "completed"
        session_obj.commit()

    yield {"type": "done"}


def _build_repair_prompt(case_record, test_design_errors: list) -> str:
    """Build a prompt for DSL regeneration from Judge conclusions."""
    parts = [
        f"请修复以下测试用例的 DSL 步骤。用例名称: {case_record.name}\n",
        "## Judge 分析的失败原因:\n",
    ]
    for err in test_design_errors:
        parts.append(f"- 步骤 {err.step_index}: {err.root_cause_analysis}")
        parts.append(f"  建议动作: {err.suggested_action}\n")

    parts.append("## 当前 DSL:\n")
    parts.append(str(case_record.dsl))
    parts.append("\n\n请基于以上分析，生成修复后的 DSL 步骤。保持测试目标不变，修正失败点。")
    return "".join(parts)
