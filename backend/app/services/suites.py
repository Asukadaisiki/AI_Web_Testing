"""Test suite persistence and execution services."""

from __future__ import annotations

import json
import re
from datetime import UTC, datetime
from typing import Any

from sqlalchemy import delete, select
from sqlalchemy.orm import Session

from app.models import Project, SuiteCase, SuiteRun, SuiteRunItem, TestCase, TestSuite, User
from app.schemas.dsl import DSLCase
from app.schemas.executions import (
    ContextVariableReadEvidence,
    ContextVariableWriteEvidence,
    StepExecutionEvidence,
    StoredCaseExecutionDetail,
)
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
from app.services.executions import execute_case_with_override, mark_execution_failed


class SuiteValidationError(ValueError):
    """Raised when suite payload fails domain validation."""


class SuiteContextResolutionError(ValueError):
    """Raised when suite context resolution fails before or after a case execution."""


PLACEHOLDER_PATTERN = re.compile(r"\$\{([A-Za-z_][A-Za-z0-9_]*)\}")


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
    mutable_context_snapshot = dict(context_snapshot)
    context_sources = {
        key: context_source_suite_run_id
        for key in mutable_context_snapshot
        if context_source_suite_run_id is not None
    }
    for suite_case in suite_cases:
        normalized_case = _get_normalized_case(session, case_id=suite_case.case_id)
        execution_request = CaseExecutionRequest(actor_user_id=payload.actor_user_id, base_url=payload.base_url)
        context_reads = _build_context_read_snapshots(normalized_case)
        context_writes = _build_context_write_snapshots(normalized_case)
        context_resolution_error: str | None = None

        try:
            resolved_case = _resolve_case_runtime_dsl(
                normalized_case,
                context_snapshot=mutable_context_snapshot,
                context_sources=context_sources,
                reads=context_reads,
            )
            execution = execute_case_with_override(
                session,
                suite_case.case_id,
                execution_request,
                case_override=resolved_case,
            )
        except SuiteContextResolutionError as exc:
            context_resolution_error = str(exc)
            _mark_context_writes_skipped(
                context_writes,
                error_message="Context write skipped because suite context resolution failed.",
                preserve_existing_errors=True,
            )
            execution = execute_case_with_override(
                session,
                suite_case.case_id,
                execution_request,
                precomputed_error=_build_context_resolution_step(normalized_case, str(exc)),
            )

        if execution.status == "passed":
            try:
                output_updates = _resolve_case_output_updates(
                    normalized_case,
                    execution,
                    writes=context_writes,
                )
            except SuiteContextResolutionError as exc:
                context_resolution_error = str(exc)
                _mark_context_writes_skipped(
                    context_writes,
                    error_message="Context write aborted because one or more output values could not be extracted.",
                    preserve_existing_errors=True,
                )
                execution = mark_execution_failed(
                    session,
                    execution.id,
                    error_message=str(exc),
                    failed_step=_build_context_resolution_step(normalized_case, str(exc)),
                )
            else:
                mutable_context_snapshot.update(output_updates)
                context_sources.update({key: run.id for key in output_updates})

        if execution.status == "passed":
            passed_cases += 1
        else:
            failed_cases += 1

        run.context_snapshot = dict(mutable_context_snapshot)
        session.add(run)
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
                context_resolution_error=context_resolution_error,
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


def _get_normalized_case(session: Session, *, case_id: int) -> DSLCase:
    case = session.get(TestCase, case_id)
    if case is None:
        raise EntityNotFoundError(f"Case {case_id} not found.")
    return DSLCase.model_validate(case.dsl)


def _build_context_read_snapshots(case: DSLCase) -> list[ContextVariableReadEvidence]:
    return [
        ContextVariableReadEvidence(
            name=item.name,
            context_key=item.context_key,
            value_type=item.value_type,
            resolved=False,
            source_suite_run_id=None,
            error_message=None,
        )
        for item in case.input_contract
    ]


def _build_context_write_snapshots(case: DSLCase) -> list[ContextVariableWriteEvidence]:
    return [
        ContextVariableWriteEvidence(
            name=item.name,
            context_key=item.context_key,
            value_type=item.value_type,
            source=item.source,
            status="pending",
            error_message=None,
        )
        for item in case.output_contract
    ]


def _resolve_case_runtime_dsl(
    case: DSLCase,
    *,
    context_snapshot: dict[str, Any],
    context_sources: dict[str, int | None],
    reads: list[ContextVariableReadEvidence],
) -> DSLCase:
    contract_by_key = {item.context_key: item for item in case.input_contract}
    resolved_values: dict[str, Any] = {}
    errors: list[str] = []

    for evidence in reads:
        contract = contract_by_key[evidence.context_key]
        if evidence.context_key not in context_snapshot:
            if contract.required:
                evidence.error_message = f"Required context key '{evidence.context_key}' is missing."
                errors.append(evidence.error_message)
            continue

        value = context_snapshot[evidence.context_key]
        if not _matches_value_type(value, evidence.value_type):
            evidence.error_message = (
                f"Context key '{evidence.context_key}' expected {evidence.value_type} "
                f"but received {_infer_value_type(value)}."
            )
            errors.append(evidence.error_message)
            continue

        evidence.resolved = True
        evidence.source_suite_run_id = context_sources.get(evidence.context_key)
        resolved_values[evidence.context_key] = value

    if errors:
        raise SuiteContextResolutionError("; ".join(errors))

    rendered_payload = _render_value_placeholders(
        case.model_dump(mode="json"),
        allowed_keys=set(contract_by_key),
        context_values=resolved_values,
    )
    return DSLCase.model_validate(rendered_payload)


def _resolve_case_output_updates(
    case: DSLCase,
    execution: StoredCaseExecutionDetail,
    *,
    writes: list[ContextVariableWriteEvidence],
) -> dict[str, Any]:
    if not case.output_contract:
        return {}

    if execution.status != "passed":
        _mark_context_writes_skipped(
            writes,
            error_message="Case execution did not complete successfully, so no context values were written.",
        )
        return {}

    pending_updates: dict[str, Any] = {}
    errors: list[str] = []
    for contract, evidence in zip(case.output_contract, writes, strict=True):
        value, error_message = _extract_output_value(contract.source, execution)
        if error_message is not None:
            evidence.status = "skipped"
            evidence.error_message = error_message
            errors.append(error_message)
            continue

        if not _matches_value_type(value, contract.value_type):
            evidence.status = "skipped"
            evidence.error_message = (
                f"Output source '{contract.source}' expected {contract.value_type} "
                f"but received {_infer_value_type(value)}."
            )
            errors.append(evidence.error_message)
            continue

        pending_updates[contract.context_key] = value

    if errors:
        for evidence in writes:
            if evidence.status == "pending":
                evidence.status = "skipped"
                evidence.error_message = "Context write aborted because another output contract failed."
        raise SuiteContextResolutionError("; ".join(errors))

    for evidence in writes:
        evidence.status = "written"
        evidence.error_message = None

    return pending_updates


def _extract_output_value(source: str | None, execution: StoredCaseExecutionDetail) -> tuple[Any, str | None]:
    last_step = execution.report.steps[-1] if execution.report and execution.report.steps else None
    source_map = {
        "latest_url": execution.latest_url,
        "error_message": execution.error_message,
        "status": execution.status,
        "last_step_url": last_step.url if last_step is not None else None,
        "last_step_page_title": last_step.page_title if last_step is not None else None,
        "last_step_target": last_step.target if last_step is not None else None,
        "last_step_value": last_step.value if last_step is not None else None,
        "last_step_error_message": last_step.error_message if last_step is not None else None,
    }
    if source is None:
        return None, "Output contract source is required for suite context write-back."
    if source not in source_map:
        return None, f"Output contract source '{source}' is not supported."

    value = source_map[source]
    if value is None:
        return None, f"Output contract source '{source}' did not produce a value."
    return value, None


def _render_value_placeholders(
    payload: Any,
    *,
    allowed_keys: set[str],
    context_values: dict[str, Any],
) -> Any:
    if isinstance(payload, dict):
        return {
            key: _render_value_placeholders(value, allowed_keys=allowed_keys, context_values=context_values)
            for key, value in payload.items()
        }
    if isinstance(payload, list):
        return [
            _render_value_placeholders(value, allowed_keys=allowed_keys, context_values=context_values)
            for value in payload
        ]
    if isinstance(payload, str):
        errors: list[str] = []

        def replace(match: re.Match[str]) -> str:
            context_key = match.group(1)
            if context_key not in allowed_keys:
                errors.append(f"Placeholder '{context_key}' is not declared in input_contract.")
                return match.group(0)
            if context_key not in context_values:
                errors.append(f"Placeholder '{context_key}' could not be resolved from suite context.")
                return match.group(0)
            return _stringify_context_value(context_values[context_key])

        rendered = PLACEHOLDER_PATTERN.sub(replace, payload)
        if errors:
            raise SuiteContextResolutionError("; ".join(errors))
        return rendered
    return payload


def _stringify_context_value(value: Any) -> str:
    if isinstance(value, str):
        return value
    if isinstance(value, bool):
        return "true" if value else "false"
    if isinstance(value, (int, float)):
        return str(value)
    return json.dumps(value, ensure_ascii=True)


def _matches_value_type(value: Any, value_type: str) -> bool:
    if value_type == "string":
        return isinstance(value, str)
    if value_type == "number":
        return isinstance(value, (int, float)) and not isinstance(value, bool)
    if value_type == "boolean":
        return isinstance(value, bool)
    if value_type == "object":
        return isinstance(value, dict)
    if value_type == "array":
        return isinstance(value, list)
    return False


def _infer_value_type(value: Any) -> str:
    if isinstance(value, bool):
        return "boolean"
    if isinstance(value, str):
        return "string"
    if isinstance(value, (int, float)):
        return "number"
    if isinstance(value, dict):
        return "object"
    if isinstance(value, list):
        return "array"
    return type(value).__name__


def _mark_context_writes_skipped(
    writes: list[ContextVariableWriteEvidence],
    *,
    error_message: str,
    preserve_existing_errors: bool = False,
) -> None:
    for evidence in writes:
        if preserve_existing_errors and evidence.error_message:
            evidence.status = "skipped"
            continue
        evidence.status = "skipped"
        evidence.error_message = error_message


def _build_context_resolution_step(case: DSLCase, error_message: str) -> StepExecutionEvidence:
    # DSLCase enforces steps.min_length=1, so using the first step as context is safe here.
    first_step = case.steps[0]
    target = getattr(first_step, "target", None)
    value = getattr(first_step, "value", None)
    return StepExecutionEvidence(
        step_index=0,
        action="context_resolve",
        target=target,
        value=value,
        status="failed",
        duration_ms=0,
        error_message=error_message,
    )
