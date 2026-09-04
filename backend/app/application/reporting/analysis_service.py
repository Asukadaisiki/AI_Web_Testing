"""Persisted execution analysis shared by direct, batch, and Planning flows."""

from __future__ import annotations

import logging

from sqlalchemy import select
from sqlalchemy.orm import Session

from app.application.planning.analysis_retest_service import (
    run_analysis_turn,
)
from app.models import ExecutionBatch, ExecutionJob, TestCaseRun
from app.schemas.ai_planning import ExecutionSummaryResult
from app.schemas.executions import (
    CaseAnalysisResult,
    ExecutionAnalysis,
    FailureDetail,
    StoredCaseExecutionDetail,
)
from app.services.anti_patterns import record_execution_anti_patterns
from app.services.cases import EntityNotFoundError
from app.services.executions import get_case_execution


logger = logging.getLogger(__name__)
TERMINAL_BATCH_STATUSES = {"passed", "failed", "needs_intervention", "cancelled"}


def analyze_run(session: Session, execution_id: int) -> ExecutionAnalysis:
    run = session.scalar(
        select(TestCaseRun)
        .where(TestCaseRun.id == execution_id)
        .with_for_update()
    )
    if run is None:
        raise EntityNotFoundError(f"Execution {execution_id} not found.")
    if run.analysis_status == "completed" and run.analysis_json:
        return ExecutionAnalysis.model_validate(run.analysis_json)

    run.analysis_status = "running"
    session.commit()
    detail = get_case_execution(session, execution_id)
    if detail is None:
        raise EntityNotFoundError(f"Execution {execution_id} not found.")
    analysis = _analyze_details(session, [detail], project_id=run.project_id)
    run = session.get(TestCaseRun, execution_id)
    if run is None:
        raise EntityNotFoundError(f"Execution {execution_id} not found.")
    run.analysis_json = analysis.model_dump(mode="json")
    run.analysis_status = "completed"
    session.commit()
    _record_anti_patterns_safely(session, [run.id])
    return analysis


def analyze_runs(
    session: Session,
    execution_ids: list[int],
    *,
    project_id: int,
) -> ExecutionAnalysis:
    unique_ids = list(dict.fromkeys(execution_ids))
    details = []
    for execution_id in unique_ids:
        detail = get_case_execution(session, execution_id)
        if detail is None:
            raise EntityNotFoundError(f"Execution {execution_id} not found.")
        if detail.project_id != project_id:
            raise EntityNotFoundError(
                f"Execution {execution_id} does not belong to project {project_id}."
            )
        details.append(detail)

    analysis = _analyze_details(session, details, project_id=project_id)
    for execution_id in unique_ids:
        run = session.get(TestCaseRun, execution_id)
        if run is None:
            continue
        run.analysis_json = analysis.model_dump(mode="json")
        run.analysis_status = "completed"
    session.commit()
    _record_anti_patterns_safely(session, unique_ids)
    return analysis


def analyze_batch(session: Session, batch_id: int) -> ExecutionAnalysis | None:
    batch = session.scalar(
        select(ExecutionBatch)
        .where(ExecutionBatch.id == batch_id)
        .with_for_update()
    )
    if batch is None:
        raise EntityNotFoundError(f"Execution batch {batch_id} not found.")
    if batch.status not in TERMINAL_BATCH_STATUSES:
        return None
    if batch.analysis_status == "completed" and batch.analysis_json:
        return ExecutionAnalysis.model_validate(batch.analysis_json)
    if batch.analysis_status == "running":
        return None

    batch.analysis_status = "running"
    session.commit()
    details = _latest_batch_executions(session, batch_id)
    analysis = _analyze_details(session, details, project_id=batch.project_id)

    batch = session.get(ExecutionBatch, batch_id)
    if batch is None:
        raise EntityNotFoundError(f"Execution batch {batch_id} not found.")
    batch.analysis_json = analysis.model_dump(mode="json")
    batch.analysis_status = "completed"
    run_ids: list[int] = []
    for detail in details:
        run = session.get(TestCaseRun, detail.id)
        if run is None:
            continue
        run.analysis_json = batch.analysis_json
        run.analysis_status = "completed"
        run_ids.append(run.id)
    session.commit()
    _record_anti_patterns_safely(session, run_ids)
    return analysis


def _record_anti_patterns_safely(
    session: Session,
    execution_ids: list[int],
) -> None:
    try:
        for execution_id in execution_ids:
            record_execution_anti_patterns(session, execution_id)
        session.commit()
    except Exception:
        session.rollback()
        logger.warning("Execution anti-pattern persistence failed", exc_info=True)


def _latest_batch_executions(
    session: Session,
    batch_id: int,
) -> list[StoredCaseExecutionDetail]:
    job_ids = list(
        session.scalars(
            select(ExecutionJob.id)
            .where(ExecutionJob.batch_id == batch_id)
            .order_by(ExecutionJob.order_index)
        ).all()
    )
    details: list[StoredCaseExecutionDetail] = []
    for job_id in job_ids:
        run = session.scalar(
            select(TestCaseRun)
            .where(TestCaseRun.job_id == job_id)
            .order_by(TestCaseRun.attempt_number.desc(), TestCaseRun.id.desc())
            .limit(1)
        )
        if run is None:
            continue
        detail = get_case_execution(session, run.id)
        if detail is not None:
            details.append(detail)
    return details


def _analyze_details(
    session: Session,
    details: list[StoredCaseExecutionDetail],
    *,
    project_id: int,
) -> ExecutionAnalysis:
    deterministic = _build_deterministic_analysis(details)
    if not deterministic.failure_signals:
        return deterministic

    summaries = [_to_planning_summary(detail) for detail in details]
    response = run_analysis_turn(
        execution_summaries=summaries,
        db_session=session,
        project_id=project_id,
    )
    if response is None or response.execution_analysis is None:
        return deterministic

    ai_analysis = response.execution_analysis.model_copy(
        update={
            "source": "ai",
            "summary": response.assistant_message,
            "conclusion": deterministic.conclusion,
            "case_results": deterministic.case_results,
            "failure_details": (
                response.execution_analysis.failure_details
                or deterministic.failure_details
            ),
            "failure_signals": deterministic.failure_signals,
            "suspected_root_cause": (
                response.execution_analysis.suspected_root_cause
                or deterministic.suspected_root_cause
            ),
            "impact_scope": (
                response.execution_analysis.impact_scope
                or deterministic.impact_scope
            ),
            "recommended_action": (
                response.execution_analysis.recommended_action
                if response.execution_analysis.recommended_action != "done"
                else deterministic.recommended_action
            ),
            "recommended_scope": (
                response.execution_analysis.recommended_scope
                or deterministic.recommended_scope
            ),
        }
    )
    return ai_analysis


def _build_deterministic_analysis(
    details: list[StoredCaseExecutionDetail],
) -> ExecutionAnalysis:
    case_results: list[CaseAnalysisResult] = []
    failure_details: list[FailureDetail] = []
    failure_signals = []
    passed_count = 0
    cancelled_count = 0

    for detail in details:
        steps = detail.report.steps if detail.report else []
        passed_steps = sum(1 for step in steps if step.status == "passed")
        if detail.status == "passed":
            passed_count += 1
        if detail.status == "cancelled":
            cancelled_count += 1
        signal = detail.failure_signal
        if signal is not None:
            failure_signals.append(signal)
            failure_details.append(
                FailureDetail(
                    case_name=detail.case_name,
                    step_index=signal.step_index or 0,
                    action=signal.action or "runner",
                    target=signal.target,
                    error_message=signal.error_message,
                    suspected_cause=signal.title,
                    cause_probability="high",
                )
            )
        case_results.append(
            CaseAnalysisResult(
                case_id=detail.case_id,
                case_name=detail.case_name,
                status=detail.status,
                passed_steps=passed_steps,
                total_steps=detail.total_steps,
                failure_summary=signal.title if signal else None,
            )
        )

    total = len(details)
    if total == 0 or cancelled_count == total:
        conclusion = "cancelled"
        summary = "执行已取消，没有可分析的完成结果。"
        action = "done"
    elif passed_count == total:
        conclusion = "all_passed"
        summary = f"{total} 个用例全部通过。"
        action = "done"
    elif passed_count == 0:
        conclusion = "all_failed"
        summary = f"{total} 个用例均未通过，已提取 {len(failure_signals)} 个失败信号。"
        action = "targeted_retest"
    else:
        conclusion = "partial"
        summary = (
            f"{passed_count}/{total} 个用例通过，"
            f"已提取 {len(failure_signals)} 个失败信号。"
        )
        action = "targeted_retest"

    return ExecutionAnalysis(
        source="deterministic",
        summary=summary,
        conclusion=conclusion,
        case_results=case_results,
        failure_details=failure_details,
        failure_signals=failure_signals,
        suspected_root_cause=failure_signals[0].title if failure_signals else None,
        impact_scope=", ".join(sorted({item.case_name for item in case_results if item.failure_summary})) or None,
        recommended_action=action,
        recommended_scope="仅重测失败用例" if failure_signals else None,
    )


def _to_planning_summary(detail: StoredCaseExecutionDetail) -> ExecutionSummaryResult:
    steps = detail.report.steps if detail.report else []
    return ExecutionSummaryResult(
        execution_id=detail.id,
        case_id=detail.case_id,
        case_name=detail.case_name,
        status=detail.status,
        total_steps=detail.total_steps,
        passed_steps=sum(1 for step in steps if step.status == "passed"),
        failed_steps=sum(1 for step in steps if step.status == "failed"),
        duration_ms=detail.duration_ms,
        screenshot_url=detail.latest_screenshot_url,
        report_url=f"/reports/{detail.id}",
    )
