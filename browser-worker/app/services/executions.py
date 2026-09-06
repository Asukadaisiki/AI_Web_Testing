"""Case execution services."""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime, UTC
import hashlib
import json
from threading import Event
from typing import Generator, cast

from sqlalchemy.orm import Session

from app.locators.corrections import SQLAlchemyCorrectionStore
from app.models import ExecutionBatch, TestCase, TestCaseRun, User
from app.reporters import build_execution_report
from app.runners import RunnerExecutionError, RunnerInterventionError
from app.runners.playwright_runner import RunnerCancelledError, StepStreamEvent, execute_case_with_playwright_streaming
from app.schemas.dsl import DSLCase, GotoStep, load_canonical_dsl
from app.schemas.executions import (
    CaseExecutionRequest,
    ExecutionAnalysis,
    FailureSignal,
    StoredCaseExecutionDetail,
    StoredCaseExecutionSummary,
    StepExecutionEvidence,
)
from app.services.errors import EntityNotFoundError
from app.services.failure_signals import build_failure_signal


@dataclass(frozen=True)
class ExecutionRunContext:
    batch_id: int | None = None
    job_id: int | None = None
    attempt_number: int = 1
    dsl_snapshot: dict | None = None
    dsl_canonical_json: str | None = None
    dsl_sha256: str | None = None
    dsl_canonical_version: str | None = None


def execute_case(
    session: Session,
    case_id: int,
    payload: CaseExecutionRequest,
    *,
    run_context: ExecutionRunContext | None = None,
    cancel_event: Event | None = None,
) -> StoredCaseExecutionDetail:
    stream = execute_case_streaming(
        session,
        case_id,
        payload,
        run_context=run_context,
        cancel_event=cancel_event,
    )
    while True:
        try:
            next(stream)
        except StopIteration as stop:
            return stop.value


def execute_case_streaming(
    session: Session,
    case_id: int,
    payload: CaseExecutionRequest,
    *,
    cancel_event: Event | None = None,
    run_context: ExecutionRunContext | None = None,
) -> Generator[StepStreamEvent, None, StoredCaseExecutionDetail]:
    """Stream step events for a case execution.

    Yields :class:`StepStreamEvent` objects and returns the persisted
    :class:`StoredCaseExecutionDetail` via ``StopIteration.value``.
    """
    record = session.get(TestCase, case_id)
    if record is None:
        raise EntityNotFoundError(f"Case {case_id} not found.")
    _ensure_user_exists(session, payload.actor_user_id)

    context = run_context or ExecutionRunContext()
    canonical_fields = (
        context.dsl_canonical_json,
        context.dsl_sha256,
        context.dsl_canonical_version,
    )
    if any(value is not None for value in canonical_fields) and any(
        value is None for value in canonical_fields
    ):
        raise ValueError("Canonical DSL JSON, SHA-256, and version must be provided together.")
    if (
        context.dsl_canonical_json is not None
        and context.dsl_sha256 is not None
        and context.dsl_canonical_version is not None
    ):
        normalized_case, dsl_snapshot = load_canonical_dsl(
            context.dsl_canonical_json,
            context.dsl_sha256,
            context.dsl_canonical_version,
        )
        if context.dsl_snapshot != dsl_snapshot:
            raise ValueError("Queued DSL snapshot does not match its canonical JSON.")
        if dsl_snapshot != record.dsl:
            raise ValueError("Approved canonical DSL does not match the persisted case.")
        dsl_sha256 = context.dsl_sha256
    else:
        normalized_case = DSLCase.model_validate(record.dsl)
        dsl_snapshot = normalized_case.model_dump(mode="json")
        dsl_sha256 = hashlib.sha256(
            json.dumps(dsl_snapshot, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode("utf-8")
        ).hexdigest()
    execution = TestCaseRun(
        case_id=record.id,
        project_id=record.project_id,
        batch_id=context.batch_id,
        job_id=context.job_id,
        triggered_by=payload.actor_user_id,
        status="running",
        attempt_number=context.attempt_number,
        dsl_snapshot=dsl_snapshot,
        dsl_sha256=dsl_sha256,
        report_schema_version="execution.report.v2",
        started_at=datetime.now(UTC).replace(tzinfo=None),
    )
    session.add(execution)
    session.commit()
    session.refresh(execution)

    effective_base_url = payload.base_url or normalized_case.base_url
    missing_base_url_error = _build_missing_base_url_error(normalized_case, effective_base_url)

    # Merge input_contract defaults with user-provided input_values
    merged_input_values: dict[str, str] = {}
    for contract in normalized_case.input_contract:
        if contract.value is not None:
            merged_input_values[contract.context_key] = contract.value
    if payload.input_values:
        merged_input_values.update(payload.input_values)

    step_results: list[StepExecutionEvidence] = []
    try:
        if missing_base_url_error is not None:
            report = build_execution_report(status="failed", steps=[_with_artifact_url(missing_base_url_error)])
            execution.status = "failed"
            execution.report = report.model_dump(mode="json")
            execution.error_message = missing_base_url_error.error_message
        else:
            step_results = yield from execute_case_with_playwright_streaming(
                case=normalized_case,
                execution_id=execution.id,
                base_url=effective_base_url,
                cancel_event=cancel_event,
                correction_store=SQLAlchemyCorrectionStore(session),
                input_values=merged_input_values,
            )
            step_results = [_with_artifact_url(step) for step in step_results]
            has_failure = any(s.status in ("failed", "cascade_blocked") for s in step_results)
            report = build_execution_report(
                status="failed" if has_failure else "passed",
                steps=step_results,
            )
            execution.status = "failed" if has_failure else "passed"
            execution.report = report.model_dump(mode="json")
            execution.error_message = None
    except RunnerInterventionError as exc:
        step_results = [_with_artifact_url(step) for step in exc.step_results]
        report = build_execution_report(status="failed", steps=step_results)
        execution.status = "needs_intervention"
        execution.report = report.model_dump(mode="json")
        execution.error_message = str(exc)
    except RunnerExecutionError as exc:
        step_results = [_with_artifact_url(step) for step in exc.step_results]
        report = build_execution_report(status="failed", steps=step_results)
        execution.status = "failed"
        execution.report = report.model_dump(mode="json")
        execution.error_message = str(exc)
    except RunnerCancelledError as exc:
        step_results = [_with_artifact_url(step) for step in exc.step_results]
        report = build_execution_report(status="failed", steps=step_results)
        execution.status = "cancelled"
        execution.report = report.model_dump(mode="json")
        execution.error_message = "Execution cancelled by user."
        _set_failure_signal(execution)
        execution.finished_at = datetime.now(UTC).replace(tzinfo=None)
        session.add(execution)
        session.commit()
        raise
    except Exception as exc:
        session.rollback()
        persisted_execution = session.get(TestCaseRun, execution.id)
        if persisted_execution is not None:
            raw_step_results = getattr(exc, "step_results", step_results)
            normalized_steps = [_with_artifact_url(step) for step in raw_step_results]
            report = build_execution_report(status="failed", steps=normalized_steps)
            persisted_execution.status = "failed"
            persisted_execution.report = report.model_dump(mode="json")
            persisted_execution.error_message = f"{type(exc).__name__}: {exc}"
            _set_failure_signal(persisted_execution)
            persisted_execution.finished_at = datetime.now(UTC).replace(tzinfo=None)
            session.add(persisted_execution)
            session.commit()
        raise
    _set_failure_signal(execution)
    execution.finished_at = datetime.now(UTC).replace(tzinfo=None)
    session.add(execution)
    session.commit()
    session.refresh(execution)
    return _to_execution_detail(session, execution, case_name=record.name)
def get_case_execution(session: Session, execution_id: int) -> StoredCaseExecutionDetail | None:
    record = session.get(TestCaseRun, execution_id)
    if record is None:
        return None
    case = session.get(TestCase, record.case_id)
    case_name = case.name if case is not None else f"Case {record.case_id}"
    return _to_execution_detail(session, record, case_name=case_name)
def _ensure_user_exists(session: Session, user_id: int) -> None:
    if session.get(User, user_id) is None:
        raise EntityNotFoundError(f"User {user_id} not found.")
def _build_missing_base_url_error(case: DSLCase, base_url: str | None) -> StepExecutionEvidence | None:
    if base_url:
        return None

    for index, step in enumerate(case.steps):
        if step.action != "goto":
            continue
        goto_step = cast(GotoStep, step)
        if _is_absolute_url(goto_step.value):
            continue
        return StepExecutionEvidence(
            step_index=index,
            action="goto",
            value=goto_step.value,
            status="failed",
            duration_ms=0,
            error_message="Relative goto step requires case.base_url or execution request base_url.",
        )
    return None


def _is_absolute_url(value: str) -> bool:
    return value.startswith(("http://", "https://"))


def _to_execution_summary(
    record: TestCaseRun,
    *,
    case_name: str,
    report=None,
) -> StoredCaseExecutionSummary:
    report = _normalize_report(record.report) if report is None else report
    failed_step = _derive_failed_step(report)
    failure_signal = None
    if record.status != "cancelled":
        failure_signal = _load_failure_signal(record.failure_signal_json) or build_failure_signal(
            report,
            record.error_message,
            execution_id=record.id,
        )
    return StoredCaseExecutionSummary(
        id=record.id,
        case_id=record.case_id,
        case_name=case_name,
        project_id=record.project_id,
        batch_id=record.batch_id,
        job_id=record.job_id,
        attempt_number=record.attempt_number,
        dsl_sha256=record.dsl_sha256,
        report_schema_version=record.report_schema_version,
        triggered_by=record.triggered_by,
        status=record.status,
        error_message=record.error_message,
        started_at=record.started_at,
        finished_at=record.finished_at,
        duration_ms=_derive_duration_ms(record.started_at, record.finished_at),
        total_steps=len(report.steps) if report is not None else 0,
        failed_step_index=failed_step.step_index if failed_step is not None else None,
        failure_category=failure_signal.category if failure_signal else None,
        failure_signal=failure_signal,
        failure_step_action=failed_step.action if failed_step is not None else None,
        latest_url=_derive_latest_url(report),
        latest_screenshot_url=_derive_latest_screenshot_url(report),
    )


def _to_execution_detail(session: Session, record: TestCaseRun, *, case_name: str) -> StoredCaseExecutionDetail:
    report = _normalize_report(record.report)
    summary = _to_execution_summary(record, case_name=case_name, report=report)
    analysis_status = record.analysis_status
    analysis_json = record.analysis_json
    if analysis_json is None and record.batch_id is not None:
        batch = session.get(ExecutionBatch, record.batch_id)
        if batch is not None:
            analysis_status = batch.analysis_status
            analysis_json = batch.analysis_json
    return StoredCaseExecutionDetail(
        **summary.model_dump(),
        dsl_snapshot=record.dsl_snapshot,
        report=report,
        analysis_status=analysis_status,
        analysis=ExecutionAnalysis.model_validate(analysis_json) if analysis_json else None,
    )


def _normalize_report(report: dict | None):
    if report is None:
        return None
    steps = [_with_artifact_url(StepExecutionEvidence.model_validate(step)) for step in report.get("steps", [])]
    return build_execution_report(status=report["status"], steps=steps)


def _set_failure_signal(record: TestCaseRun) -> None:
    if record.status == "cancelled":
        record.failure_signal_json = None
        return
    signal = build_failure_signal(
        _normalize_report(record.report),
        record.error_message,
        execution_id=record.id,
    )
    record.failure_signal_json = signal.model_dump(mode="json") if signal else None


def _load_failure_signal(payload: dict | None) -> FailureSignal | None:
    return FailureSignal.model_validate(payload) if payload else None


def _with_artifact_url(step: StepExecutionEvidence) -> StepExecutionEvidence:
    updates: dict[str, str] = {}
    if not step.screenshot_url and step.screenshot_path:
        normalized = step.screenshot_path.replace("\\", "/").lstrip("/")
        if normalized.startswith("artifacts/"):
            updates["screenshot_url"] = f"/{normalized}"
    if not step.dom_snapshot_url and step.dom_snapshot_path:
        normalized = step.dom_snapshot_path.replace("\\", "/").lstrip("/")
        if normalized.startswith("artifacts/"):
            updates["dom_snapshot_url"] = f"/{normalized}"
    return step.model_copy(update=updates) if updates else step


def _derive_duration_ms(started_at: datetime, finished_at: datetime | None) -> int | None:
    if finished_at is None:
        return None
    return max(0, int((finished_at - started_at).total_seconds() * 1000))


def _derive_failed_step(report) -> StepExecutionEvidence | None:
    if report is None:
        return None
    for step in report.steps:
        if step.status == "failed":
            return step
    return None


def _derive_latest_url(report) -> str | None:
    if report is None:
        return None
    for step in reversed(report.steps):
        if step.url:
            return step.url
    return None


def _derive_latest_screenshot_url(report) -> str | None:
    if report is None:
        return None
    for step in reversed(report.steps):
        if step.screenshot_url:
            return step.screenshot_url
    return None
