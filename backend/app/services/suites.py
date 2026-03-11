"""Test suite persistence and execution services."""

from __future__ import annotations

from datetime import UTC, datetime

from sqlalchemy import delete, select
from sqlalchemy.orm import Session

from app.models import Project, SuiteCase, TestCase, TestSuite, User
from app.schemas.executions import CaseExecutionRequest
from app.schemas.suites import (
    StoredSuiteCase,
    StoredSuiteDetail,
    StoredSuiteSummary,
    SuiteCreateRequest,
    SuiteExecutionItem,
    SuiteExecutionRequest,
    SuiteExecutionResult,
    SuiteUpdateRequest,
)
from app.services.cases import EntityNotFoundError
from app.services.executions import execute_case


class SuiteValidationError(ValueError):
    """Raised when suite payload fails domain validation."""


def create_suite(session: Session, payload: SuiteCreateRequest) -> StoredSuiteDetail:
    _ensure_project_exists(session, payload.project_id)
    _ensure_user_exists(session, payload.actor_user_id)
    normalized_cases = _normalize_suite_cases(session, payload.project_id, payload.cases)

    suite = TestSuite(
        project_id=payload.project_id,
        created_by=payload.actor_user_id,
        updated_by=payload.actor_user_id,
        name=payload.name,
        description=payload.description,
    )
    session.add(suite)
    session.commit()
    session.refresh(suite)

    _replace_suite_cases(session, suite_id=suite.id, case_ids=normalized_cases)
    session.refresh(suite)
    return _to_stored_suite_detail(session, suite)


def update_suite(session: Session, suite_id: int, payload: SuiteUpdateRequest) -> StoredSuiteDetail:
    suite = session.get(TestSuite, suite_id)
    if suite is None:
        raise EntityNotFoundError(f"Suite {suite_id} not found.")

    _ensure_project_exists(session, payload.project_id)
    _ensure_user_exists(session, payload.actor_user_id)
    normalized_cases = _normalize_suite_cases(session, payload.project_id, payload.cases)

    suite.project_id = payload.project_id
    suite.updated_by = payload.actor_user_id
    suite.name = payload.name
    suite.description = payload.description
    session.add(suite)
    session.commit()

    _replace_suite_cases(session, suite_id=suite.id, case_ids=normalized_cases)
    session.refresh(suite)
    return _to_stored_suite_detail(session, suite)


def list_suites(session: Session) -> list[StoredSuiteSummary]:
    statement = select(TestSuite).order_by(TestSuite.updated_at.desc(), TestSuite.id.desc())
    suites = session.scalars(statement).all()
    return [_to_stored_suite_summary(session, suite) for suite in suites]


def get_suite(session: Session, suite_id: int) -> StoredSuiteDetail | None:
    suite = session.get(TestSuite, suite_id)
    if suite is None:
        return None
    return _to_stored_suite_detail(session, suite)


def execute_suite(session: Session, suite_id: int, payload: SuiteExecutionRequest) -> SuiteExecutionResult:
    suite = session.get(TestSuite, suite_id)
    if suite is None:
        raise EntityNotFoundError(f"Suite {suite_id} not found.")

    _ensure_user_exists(session, payload.actor_user_id)
    stored_suite = _to_stored_suite_detail(session, suite)
    if not stored_suite.cases:
        raise SuiteValidationError("Suite must contain at least one case before execution.")

    started_at = datetime.now(UTC).replace(tzinfo=None)
    executions: list[SuiteExecutionItem] = []
    passed_cases = 0
    failed_cases = 0
    for suite_case in stored_suite.cases:
        execution = execute_case(
            session,
            suite_case.case_id,
            CaseExecutionRequest(actor_user_id=payload.actor_user_id, base_url=payload.base_url),
        )
        executions.append(
            SuiteExecutionItem(
                execution_id=execution.id,
                case_id=execution.case_id,
                case_name=execution.case_name,
                status=execution.status,
            )
        )
        if execution.status == "passed":
            passed_cases += 1
        else:
            failed_cases += 1

    finished_at = datetime.now(UTC).replace(tzinfo=None)
    return SuiteExecutionResult(
        suite_id=stored_suite.id,
        suite_name=stored_suite.name,
        started_at=started_at,
        finished_at=finished_at,
        total_cases=len(stored_suite.cases),
        passed_cases=passed_cases,
        failed_cases=failed_cases,
        status="passed" if failed_cases == 0 else "failed",
        executions=executions,
    )


def _normalize_suite_cases(session: Session, project_id: int, suite_cases) -> list[int]:
    case_ids = [item.case_id for item in suite_cases]
    if not case_ids:
        raise SuiteValidationError("Suite must contain at least one case.")
    if len(case_ids) != len(set(case_ids)):
        raise SuiteValidationError("Suite cannot contain duplicate case_id values.")

    cases = session.scalars(select(TestCase).where(TestCase.id.in_(case_ids))).all()
    case_map = {case.id: case for case in cases}
    missing_case_ids = [case_id for case_id in case_ids if case_id not in case_map]
    if missing_case_ids:
        raise EntityNotFoundError(f"Case {missing_case_ids[0]} not found.")

    for case_id in case_ids:
        case = case_map[case_id]
        if case.project_id != project_id:
            raise SuiteValidationError("Suite can only contain cases from the same project.")

    return case_ids


def _replace_suite_cases(session: Session, *, suite_id: int, case_ids: list[int]) -> None:
    session.execute(delete(SuiteCase).where(SuiteCase.suite_id == suite_id))
    session.add_all(
        [
            SuiteCase(suite_id=suite_id, case_id=case_id, order_index=index)
            for index, case_id in enumerate(case_ids, start=1)
        ]
    )
    session.commit()


def _get_stored_suite_cases(session: Session, suite_id: int) -> list[StoredSuiteCase]:
    statement = (
        select(SuiteCase, TestCase.name)
        .join(TestCase, TestCase.id == SuiteCase.case_id)
        .where(SuiteCase.suite_id == suite_id)
        .order_by(SuiteCase.order_index.asc(), SuiteCase.case_id.asc())
    )
    rows = session.execute(statement).all()
    return [
        StoredSuiteCase(case_id=suite_case.case_id, case_name=case_name, order_index=suite_case.order_index)
        for suite_case, case_name in rows
    ]


def _to_stored_suite_summary(session: Session, suite: TestSuite) -> StoredSuiteSummary:
    suite_cases = _get_stored_suite_cases(session, suite.id)
    return StoredSuiteSummary(
        id=suite.id,
        project_id=suite.project_id,
        name=suite.name,
        description=suite.description,
        case_count=len(suite_cases),
        created_by=suite.created_by,
        updated_by=suite.updated_by,
        created_at=suite.created_at,
        updated_at=suite.updated_at,
    )


def _to_stored_suite_detail(session: Session, suite: TestSuite) -> StoredSuiteDetail:
    suite_cases = _get_stored_suite_cases(session, suite.id)
    summary = _to_stored_suite_summary(session, suite)
    return StoredSuiteDetail(**summary.model_dump(), cases=suite_cases)


def _ensure_project_exists(session: Session, project_id: int) -> None:
    if session.get(Project, project_id) is None:
        raise EntityNotFoundError(f"Project {project_id} not found.")


def _ensure_user_exists(session: Session, user_id: int) -> None:
    if session.get(User, user_id) is None:
        raise EntityNotFoundError(f"User {user_id} not found.")
