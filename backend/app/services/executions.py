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
        report = build_execution_report(status="passed", steps=step_results)
        execution.status = "passed"
        execution.report = report.model_dump(mode="json")
        execution.error_message = None
    except RunnerExecutionError as exc:
        report = build_execution_report(status="failed", steps=exc.step_results)
        execution.status = "failed"
        execution.report = report.model_dump(mode="json")
        execution.error_message = str(exc)
    execution.finished_at = datetime.now(UTC).replace(tzinfo=None)
    session.add(execution)
    session.commit()
    session.refresh(execution)
    return _to_execution_detail(execution)


def list_case_executions(session: Session, case_id: int) -> list[StoredCaseExecutionSummary]:
    if session.get(TestCase, case_id) is None:
        raise EntityNotFoundError(f"Case {case_id} not found.")

    statement = (
        select(TestCaseRun)
        .where(TestCaseRun.case_id == case_id)
        .order_by(TestCaseRun.started_at.desc(), TestCaseRun.id.desc())
    )
    records = session.scalars(statement).all()
    return [_to_execution_summary(record) for record in records]


def get_case_execution(session: Session, execution_id: int) -> StoredCaseExecutionDetail | None:
    record = session.get(TestCaseRun, execution_id)
    if record is None:
        return None
    return _to_execution_detail(record)


def _ensure_user_exists(session: Session, user_id: int) -> None:
    if session.get(User, user_id) is None:
        raise EntityNotFoundError(f"User {user_id} not found.")


def _to_execution_summary(record: TestCaseRun) -> StoredCaseExecutionSummary:
    return StoredCaseExecutionSummary(
        id=record.id,
        case_id=record.case_id,
        project_id=record.project_id,
        triggered_by=record.triggered_by,
        status=record.status,
        error_message=record.error_message,
        started_at=record.started_at,
        finished_at=record.finished_at,
    )


def _to_execution_detail(record: TestCaseRun) -> StoredCaseExecutionDetail:
    summary = _to_execution_summary(record)
    return StoredCaseExecutionDetail(
        **summary.model_dump(),
        report=record.report,
    )
