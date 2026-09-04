"""Planning execution analysis and retest use cases."""

from __future__ import annotations

import logging
from typing import Any


from app.application.planning.context_service import (
    categorize_error,
)
from app.application.planning.execution_inputs import build_input_values_from_session
from app.application.planning.project_context import (
    get_active_project_id as _get_active_project_id,
    get_owned_session as _get_session,
    get_session_project_ids as _get_session_project_ids,
)
from app.core.structured_logging import get_structured_logger
from sqlalchemy.orm import Session

from app.ai.test_planning_agent import run_planning_turn
from app.models import AIPlanningMessage, TestCase
from app.schemas.ai_planning import (
    AIPlanningRequirements,
    AIPlanningTurnResponse,
    ExecutionSummaryResult,
)
from app.schemas.executions import CaseExecutionRequest
from app.services.executions import execute_case


logger = logging.getLogger(__name__)
slog = get_structured_logger(__name__)


def should_run_analysis(
    execution_summaries: list[ExecutionSummaryResult],
) -> bool:
    """Return True if any execution result is not passed."""
    return any(s.status != "passed" for s in execution_summaries)


def build_analysis_context(
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
        if ex.status != "passed":
            from app.services.executions import get_case_execution

            detail = get_case_execution(db_session, ex.execution_id)
            signal = detail.failure_signal if detail else None
            if signal:
                lines.append(
                    f"  FailureSignal: category={signal.category}, action={signal.action or '-'}, "
                    f"target={signal.target or '-'}, error={signal.error_message or signal.title}"
                )
    lines.append("\n请使用 analyze_results 模式输出分析报告。")
    return "\n".join(lines)


def run_analysis_turn(
    *,
    execution_summaries: list[ExecutionSummaryResult],
    db_session: Session,
    project_id: int,
) -> AIPlanningTurnResponse | None:
    """Run an analysis turn using the AI agent with execution results as context."""
    try:
        context_message = build_analysis_context(execution_summaries, db_session)
        transcript = [{"role": "user", "content": context_message}]
        response = run_planning_turn(
            transcript=transcript,
            existing_requirements=None,
            db_session=db_session,
            project_id=project_id,
        )
        # Auto-update insights after analysis
        auto_update_insights(db_session, project_id, execution_summaries, response)
        return response
    except Exception:
        logger.warning("Auto-analysis turn failed", exc_info=True)
        return None


def auto_update_insights(
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
                category = categorize_error(error_msg)
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
    project_ids = _get_session_project_ids(planning_session)

    if not case_ids and failed_only:
        from app.ai.planning_tools import get_recommended_retest
        recommendation = get_recommended_retest(
            params={}, db_session=session, project_id=_get_active_project_id(planning_session) or 0,
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

    # Auto-fill input_values from session test data if not provided
    if not input_values:
        # Collect dsl_case_json from all cases being retested
        cases_json: list[dict[str, Any] | None] = []
        for cid in case_ids:
            cr = session.get(TestCase, cid)
            cases_json.append(cr.dsl if cr else None)
        input_values = build_input_values_from_session(
            planning_session.requirements_json or {}, cases_json,
        )
        logger.info(
            "Retest auto-resolved input_values: %s",
            {k: v[:3] + '***' for k, v in input_values.items()} if input_values else {},
        )

    execution_summaries: list[ExecutionSummaryResult] = []
    for case_id in case_ids:
        case_record = session.get(TestCase, case_id)
        if case_record is None or (project_ids and case_record.project_id != _get_active_project_id(planning_session)):
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
            report_url=f"/reports/{result.id}",
        ))

    lines = [f"复测完成（{len(execution_summaries)} 个用例）：\n"]
    for ex in execution_summaries:
        icon = "✅" if ex.status == "passed" else "❌"
        lines.append(f"{icon} {ex.case_name} — {ex.status} ({ex.passed_steps}/{ex.total_steps}步)")

    assistant_message = "\n".join(lines)
    planning_session.status = "completed"

    execution_analysis = None
    if should_run_analysis(execution_summaries):
        from app.application.reporting.analysis_service import analyze_runs

        execution_analysis = analyze_runs(
            session,
            [item.execution_id for item in execution_summaries],
            project_id=_get_active_project_id(planning_session) or 0,
        )
        assistant_message = f"{assistant_message}\n\n---\n\n{execution_analysis.summary}"

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
