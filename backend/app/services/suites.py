"""Test suite persistence and execution services."""

from __future__ import annotations

from datetime import UTC, datetime
from typing import Any

from sqlalchemy import delete, select
from sqlalchemy.orm import Session

from app.models import Project, SuiteCase, SuiteRun, SuiteRunItem, TestCase, TestSuite, User
from app.schemas.dsl import DSLCase
from app.schemas.executions import ContextVariableReadEvidence, ContextVariableWriteEvidence
from app.schemas.executions import CaseExecutionRequest
from app.schemas.suites import (
    StoredSuiteCase,
    StoredSuiteDetail,
    StoredSuiteRunDetail,
    StoredSuiteRunItem,
    StoredSuiteRunSummary,
    StoredSuiteSummary,
    SuiteCreateRequest,
    SuiteExecutionItem,
    SuiteExecutionRequest,
    SuiteExecutionResult,
    SuiteRerunContextMode,
    SuiteRunSource,
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


def list_suite_runs(session: Session, suite_id: int) -> list[StoredSuiteRunSummary]:
    suite = session.get(TestSuite, suite_id)
    if suite is None:
        raise EntityNotFoundError(f"Suite {suite_id} not found.")
    return _list_suite_run_summaries(session, suite_id=suite_id, suite_name=suite.name)


def get_suite_run(session: Session, suite_id: int, run_id: int) -> StoredSuiteRunDetail | None:
    suite = session.get(TestSuite, suite_id)
    if suite is None:
        raise EntityNotFoundError(f"Suite {suite_id} not found.")

    run = _get_suite_run_model(session, suite_id=suite_id, run_id=run_id)
    if run is None:
        return None
    return _to_stored_suite_run_detail(session, run=run, suite_name=suite.name)


def execute_suite(session: Session, suite_id: int, payload: SuiteExecutionRequest) -> SuiteExecutionResult:
    suite = session.get(TestSuite, suite_id)
    if suite is None:
        raise EntityNotFoundError(f"Suite {suite_id} not found.")

    _ensure_user_exists(session, payload.actor_user_id)
    suite_cases = _get_stored_suite_cases(session, suite.id)
    if not suite_cases:
        raise SuiteValidationError("Suite must contain at least one case before execution.")

    return _execute_suite_batch(
        session,
        suite=suite,
        suite_cases=suite_cases,
        payload=payload,
        source="manual",
        source_suite_run_id=None,
        context_source="empty",
        context_source_suite_run_id=None,
        rerun_context_mode="not_applicable",
        context_snapshot={},
    )


def rerun_failed_suite_run(
    session: Session,
    suite_id: int,
    run_id: int,
    payload: SuiteExecutionRequest,
) -> SuiteExecutionResult:
    suite = session.get(TestSuite, suite_id)
    if suite is None:
        raise EntityNotFoundError(f"Suite {suite_id} not found.")

    _ensure_user_exists(session, payload.actor_user_id)
    source_run = _get_suite_run_model(session, suite_id=suite_id, run_id=run_id)
    if source_run is None:
        raise EntityNotFoundError(f"Suite run {run_id} not found.")

    source_items = _get_suite_run_item_models(session, run_id)
    failed_items = [item for item in source_items if item.status == "failed"]
    if not failed_items:
        raise SuiteValidationError("Suite run does not contain failed cases to rerun.")

    suite_cases = _build_suite_cases_from_run_items(session, suite.project_id, failed_items)
    rerun_context_mode = payload.rerun_context_mode or "reuse_source_context"
    context_source = "suite_run_snapshot" if rerun_context_mode == "reuse_source_context" else "empty"
    context_source_suite_run_id = source_run.id if context_source == "suite_run_snapshot" else None
    context_snapshot = source_run.context_snapshot if context_source == "suite_run_snapshot" else {}
    return _execute_suite_batch(
        session,
        suite=suite,
        suite_cases=suite_cases,
        payload=payload,
        source="rerun_failed",
        source_suite_run_id=source_run.id,
        context_source=context_source,
        context_source_suite_run_id=context_source_suite_run_id,
        rerun_context_mode=rerun_context_mode,
        context_snapshot=context_snapshot,
    )


def _execute_suite_batch(
    session: Session,
    *,
    suite: TestSuite,
    suite_cases: list[StoredSuiteCase],
    payload: SuiteExecutionRequest,
    source: SuiteRunSource,
    source_suite_run_id: int | None,
    context_source: str,
    context_source_suite_run_id: int | None,
    rerun_context_mode: SuiteRerunContextMode,
    context_snapshot: dict[str, Any],
) -> SuiteExecutionResult:
    started_at = datetime.now(UTC).replace(tzinfo=None)
    run = SuiteRun(
        suite_id=suite.id,
        triggered_by=payload.actor_user_id,
        source=source,
        source_suite_run_id=source_suite_run_id,
        context_source=context_source,
        context_source_suite_run_id=context_source_suite_run_id,
        rerun_context_mode=rerun_context_mode,
        context_snapshot=dict(context_snapshot),
        status="running",
        total_cases=len(suite_cases),
        passed_cases=0,
        failed_cases=0,
        base_url_override=payload.base_url,
        started_at=started_at,
    )
    session.add(run)
    session.commit()
    session.refresh(run)

    passed_cases = 0
    failed_cases = 0
    for suite_case in suite_cases:
        context_reads, context_writes = _build_context_contract_snapshots(session, case_id=suite_case.case_id)
        execution = execute_case(
            session,
            suite_case.case_id,
            CaseExecutionRequest(actor_user_id=payload.actor_user_id, base_url=payload.base_url),
        )
        if execution.status == "passed":
            passed_cases += 1
        else:
            failed_cases += 1

        session.add(
            SuiteRunItem(
                suite_run_id=run.id,
                case_id=suite_case.case_id,
                case_name_snapshot=suite_case.case_name,
                order_index=suite_case.order_index,
                execution_id=execution.id,
                status=execution.status,
                context_reads=[item.model_dump(mode="json") for item in context_reads],
                context_writes=[item.model_dump(mode="json") for item in context_writes],
                context_resolution_error=None,
            )
        )
        session.commit()

    run.passed_cases = passed_cases
    run.failed_cases = failed_cases
    run.status = "passed" if failed_cases == 0 else "failed"
    run.finished_at = datetime.now(UTC).replace(tzinfo=None)
    session.add(run)
    session.commit()
    session.refresh(run)

    detail = _to_stored_suite_run_detail(session, run=run, suite_name=suite.name)
    return _to_suite_execution_result(detail)


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


def _build_suite_cases_from_run_items(
    session: Session,
    project_id: int,
    items: list[SuiteRunItem],
) -> list[StoredSuiteCase]:
    ordered_items = sorted(items, key=lambda item: (item.order_index, item.id))
    case_ids = [item.case_id for item in ordered_items]
    cases = session.scalars(select(TestCase).where(TestCase.id.in_(case_ids))).all()
    case_map = {case.id: case for case in cases}

    suite_cases: list[StoredSuiteCase] = []
    for item in ordered_items:
        case = case_map.get(item.case_id)
        if case is None:
            raise EntityNotFoundError(f"Case {item.case_id} not found.")
        if case.project_id != project_id:
            raise SuiteValidationError("Suite can only rerun cases from the same project.")
        suite_cases.append(
            StoredSuiteCase(
                case_id=case.id,
                case_name=case.name,
                order_index=item.order_index,
            )
        )
    return suite_cases


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


def _get_suite_run_model(session: Session, *, suite_id: int, run_id: int) -> SuiteRun | None:
    statement = select(SuiteRun).where(SuiteRun.id == run_id, SuiteRun.suite_id == suite_id)
    return session.scalar(statement)


def _get_suite_run_item_models(session: Session, run_id: int) -> list[SuiteRunItem]:
    statement = (
        select(SuiteRunItem)
        .where(SuiteRunItem.suite_run_id == run_id)
        .order_by(SuiteRunItem.order_index.asc(), SuiteRunItem.id.asc())
    )
    return session.scalars(statement).all()


def _get_stored_suite_run_items(session: Session, run_id: int) -> list[StoredSuiteRunItem]:
    return [
        StoredSuiteRunItem(
            id=item.id,
            case_id=item.case_id,
            case_name_snapshot=item.case_name_snapshot,
            order_index=item.order_index,
            execution_id=item.execution_id,
            status=item.status,
            context_reads=[
                ContextVariableReadEvidence.model_validate(entry)
                for entry in (item.context_reads or [])
            ],
            context_writes=[
                ContextVariableWriteEvidence.model_validate(entry)
                for entry in (item.context_writes or [])
            ],
            context_resolution_error=item.context_resolution_error,
        )
        for item in _get_suite_run_item_models(session, run_id)
    ]


def _get_latest_suite_run_summary(
    session: Session,
    *,
    suite_id: int,
    suite_name: str,
) -> StoredSuiteRunSummary | None:
    statement = (
        select(SuiteRun)
        .where(SuiteRun.suite_id == suite_id)
        .order_by(SuiteRun.started_at.desc(), SuiteRun.id.desc())
        .limit(1)
    )
    run = session.scalar(statement)
    if run is None:
        return None
    return _to_stored_suite_run_summary(run=run, suite_name=suite_name)


def _list_suite_run_summaries(
    session: Session,
    *,
    suite_id: int,
    suite_name: str,
) -> list[StoredSuiteRunSummary]:
    statement = (
        select(SuiteRun)
        .where(SuiteRun.suite_id == suite_id)
        .order_by(SuiteRun.started_at.desc(), SuiteRun.id.desc())
    )
    runs = session.scalars(statement).all()
    return [_to_stored_suite_run_summary(run=run, suite_name=suite_name) for run in runs]


def _to_stored_suite_summary(session: Session, suite: TestSuite) -> StoredSuiteSummary:
    suite_cases = _get_stored_suite_cases(session, suite.id)
    latest_run = _get_latest_suite_run_summary(session, suite_id=suite.id, suite_name=suite.name)
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
        latest_run=latest_run,
    )


def _to_stored_suite_detail(session: Session, suite: TestSuite) -> StoredSuiteDetail:
    suite_cases = _get_stored_suite_cases(session, suite.id)
    summary = _to_stored_suite_summary(session, suite)
    return StoredSuiteDetail(**summary.model_dump(), cases=suite_cases)


def _to_stored_suite_run_summary(*, run: SuiteRun, suite_name: str) -> StoredSuiteRunSummary:
    return StoredSuiteRunSummary(
        id=run.id,
        suite_id=run.suite_id,
        suite_name=suite_name,
        triggered_by=run.triggered_by,
        source=run.source,
        source_suite_run_id=run.source_suite_run_id,
        status=run.status,
        total_cases=run.total_cases,
        passed_cases=run.passed_cases,
        failed_cases=run.failed_cases,
        base_url_override=run.base_url_override,
        context_source=run.context_source,
        context_source_suite_run_id=run.context_source_suite_run_id,
        rerun_context_mode=run.rerun_context_mode,
        context_snapshot=run.context_snapshot or {},
        started_at=run.started_at,
        finished_at=run.finished_at,
    )


def _to_stored_suite_run_detail(
    session: Session,
    *,
    run: SuiteRun,
    suite_name: str,
) -> StoredSuiteRunDetail:
    summary = _to_stored_suite_run_summary(run=run, suite_name=suite_name)
    return StoredSuiteRunDetail(
        **summary.model_dump(),
        items=_get_stored_suite_run_items(session, run.id),
    )


def _to_suite_execution_result(detail: StoredSuiteRunDetail) -> SuiteExecutionResult:
    return SuiteExecutionResult(
        **detail.model_dump(),
        executions=[
            SuiteExecutionItem(
                execution_id=item.execution_id,
                case_id=item.case_id,
                case_name=item.case_name_snapshot,
                status=item.status,
            )
            for item in detail.items
        ],
    )


def _ensure_project_exists(session: Session, project_id: int) -> None:
    if session.get(Project, project_id) is None:
        raise EntityNotFoundError(f"Project {project_id} not found.")


def _ensure_user_exists(session: Session, user_id: int) -> None:
    if session.get(User, user_id) is None:
        raise EntityNotFoundError(f"User {user_id} not found.")


def _build_context_contract_snapshots(
    session: Session,
    *,
    case_id: int,
) -> tuple[list[ContextVariableReadEvidence], list[ContextVariableWriteEvidence]]:
    case = session.get(TestCase, case_id)
    if case is None:
        raise EntityNotFoundError(f"Case {case_id} not found.")

    normalized_case = DSLCase.model_validate(case.dsl)
    reads = [
        ContextVariableReadEvidence(
            name=item.name,
            context_key=item.context_key,
            value_type=item.value_type,
            resolved=False,
            source_suite_run_id=None,
            error_message=None,
        )
        for item in normalized_case.input_contract
    ]
    writes = [
        ContextVariableWriteEvidence(
            name=item.name,
            context_key=item.context_key,
            value_type=item.value_type,
            status="pending",
            error_message=None,
        )
        for item in normalized_case.output_contract
    ]
    return reads, writes
