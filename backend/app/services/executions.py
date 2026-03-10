"""Case execution services."""

from __future__ import annotations

from datetime import datetime, UTC

from sqlalchemy import select
from sqlalchemy.orm import Session

from app.core.config import get_settings
from app.models import TestCase, TestCaseRun, User
from app.reporters import build_execution_report
from app.runners import RunnerExecutionError, execute_case_with_playwright
from app.schemas.dsl import DSLCase
from app.schemas.executions import (
    CaseExecutionRequest,
    StoredCaseExecutionDetail,
    StoredCaseExecutionSummary,
    StepExecutionEvidence,
)
from app.services.cases import EntityNotFoundError


def execute_case(session: Session, case_id: int, payload: CaseExecutionRequest) -> StoredCaseExecutionDetail:
    record = session.get(TestCase, case_id)
    if record is None:
        raise EntityNotFoundError(f"Case {case_id} not found.")
    _ensure_user_exists(session, payload.actor_user_id)

    execution = TestCaseRun(
        case_id=record.id,
        project_id=record.project_id,
        triggered_by=payload.actor_user_id,
        status="running",
    )
    session.add(execution)
    session.commit()
    session.refresh(execution)

    try:
        step_results = execute_case_with_playwright(
            case=DSLCase.model_validate(record.dsl),
            execution_id=execution.id,
            base_url=payload.base_url or get_settings().execution_base_url,
        )
        step_results = [_with_artifact_url(step) for step in step_results]
        report = build_execution_report(status="passed", steps=step_results)
        execution.status = "passed"
        execution.report = report.model_dump(mode="json")
        execution.error_message = None
    except RunnerExecutionError as exc:
        step_results = [_with_artifact_url(step) for step in exc.step_results]
        report = build_execution_report(status="failed", steps=step_results)
        execution.status = "failed"
        execution.report = report.model_dump(mode="json")
        execution.error_message = str(exc)
    execution.finished_at = datetime.now(UTC).replace(tzinfo=None)
    session.add(execution)
    session.commit()
    session.refresh(execution)
    return _to_execution_detail(execution, case_name=record.name)


def list_case_executions(session: Session, case_id: int) -> list[StoredCaseExecutionSummary]:
    case = session.get(TestCase, case_id)
    if case is None:
        raise EntityNotFoundError(f"Case {case_id} not found.")

    statement = (
        select(TestCaseRun)
        .where(TestCaseRun.case_id == case_id)
        .order_by(TestCaseRun.started_at.desc(), TestCaseRun.id.desc())
    )
    records = session.scalars(statement).all()
    return [_to_execution_summary(record, case_name=case.name) for record in records]


def list_executions(
    session: Session,
    *,
    project_id: int | None = None,
    case_id: int | None = None,
    status: str | None = None,
    limit: int = 20,
    offset: int = 0,
) -> list[StoredCaseExecutionSummary]:
    statement = (
        select(TestCaseRun, TestCase.name)
        .join(TestCase, TestCase.id == TestCaseRun.case_id)
        .order_by(TestCaseRun.started_at.desc(), TestCaseRun.id.desc())
        .offset(offset)
        .limit(limit)
    )
    if project_id is not None:
        statement = statement.where(TestCaseRun.project_id == project_id)
    if case_id is not None:
        statement = statement.where(TestCaseRun.case_id == case_id)
    if status is not None:
        statement = statement.where(TestCaseRun.status == status)

    rows = session.execute(statement).all()
    return [_to_execution_summary(record, case_name=case_name) for record, case_name in rows]


def get_case_execution(session: Session, execution_id: int) -> StoredCaseExecutionDetail | None:
    record = session.get(TestCaseRun, execution_id)
    if record is None:
        return None
    case = session.get(TestCase, record.case_id)
    case_name = case.name if case is not None else f"Case {record.case_id}"
    return _to_execution_detail(record, case_name=case_name)


def _ensure_user_exists(session: Session, user_id: int) -> None:
    if session.get(User, user_id) is None:
        raise EntityNotFoundError(f"User {user_id} not found.")


def _to_execution_summary(record: TestCaseRun, *, case_name: str) -> StoredCaseExecutionSummary:
    report = _normalize_report(record.report)
    return StoredCaseExecutionSummary(
        id=record.id,
        case_id=record.case_id,
        case_name=case_name,
        project_id=record.project_id,
        triggered_by=record.triggered_by,
        status=record.status,
        error_message=record.error_message,
        started_at=record.started_at,
        finished_at=record.finished_at,
        duration_ms=_derive_duration_ms(record.started_at, record.finished_at),
        total_steps=len(report.steps) if report is not None else 0,
        failed_step_index=_derive_failed_step_index(report),
        latest_screenshot_url=_derive_latest_screenshot_url(report),
    )


def _to_execution_detail(record: TestCaseRun, *, case_name: str) -> StoredCaseExecutionDetail:
    summary = _to_execution_summary(record, case_name=case_name)
    return StoredCaseExecutionDetail(
        **summary.model_dump(),
        report=_normalize_report(record.report),
    )


def _normalize_report(report: dict | None):
    if report is None:
        return None
    steps = [_with_artifact_url(StepExecutionEvidence.model_validate(step)) for step in report.get("steps", [])]
    return build_execution_report(status=report["status"], steps=steps)


def _with_artifact_url(step: StepExecutionEvidence) -> StepExecutionEvidence:
    if step.screenshot_url or not step.screenshot_path:
        return step

    normalized = step.screenshot_path.replace("\\", "/").lstrip("/")
    screenshot_url = f"/{normalized}" if normalized.startswith("artifacts/") else None
    return step.model_copy(update={"screenshot_url": screenshot_url})


def _derive_duration_ms(started_at: datetime, finished_at: datetime | None) -> int | None:
    if finished_at is None:
        return None
    return max(0, int((finished_at - started_at).total_seconds() * 1000))


def _derive_failed_step_index(report) -> int | None:
    if report is None:
        return None
    for step in report.steps:
        if step.status == "failed":
            return step.step_index
    return None


def _derive_latest_screenshot_url(report) -> str | None:
    if report is None:
        return None
    for step in reversed(report.steps):
        if step.screenshot_url:
            return step.screenshot_url
    return None
