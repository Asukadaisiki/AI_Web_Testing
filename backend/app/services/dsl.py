"""DSL validation service."""

from __future__ import annotations

import hashlib
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from threading import Lock

from sqlalchemy import case, func, or_, select
from sqlalchemy.orm import Session

from app.ai.dsl_generator import (
    AI_DSL_PROMPT_VERSION,
    DslGenerationConfigError,
    DslGenerationError,
    generate_case_draft,
    resolve_generation_mode,
    resolve_generation_profile,
)
from app.core.config import get_settings
from app.models import DslGenerationRun, Project, TestCase, User
from app.schemas.dsl import (
    DSLCase,
    DSLValidationResult,
    DslGenerationFeedbackRequest,
    DslGenerationFeedbackStatus,
    DslGenerationPromptVariant,
    DslGenerationRejectionReasonCode,
    GenerateDslMeta,
    GenerateDslMode,
    GenerateDslImportMode,
    GenerateDslRequest,
    GenerateDslResponse,
    StoredDslGenerationRunDetail,
    StoredDslGenerationRunSummary,
)
from app.schemas.settings import (
    AIDslGenerationErrorTypeCount,
    AIDslGenerationContextProfileBreakdown,
    AIDslGenerationImportModeCount,
    AIDslGenerationModeBreakdown,
    AIDslGenerationModelOutcome,
    AIDslGenerationRejectionReasonCount,
    AIDslGenerationRejectionReasonByVariant,
    AIDslGenerationPromptVariantBreakdown,
    AIDslGenerationStats,
)
from app.services.cases import EntityNotFoundError


SUPPORTED_DSL_ACTIONS = [
    "goto",
    "click",
    "input",
    "wait_for",
    "assert_text",
    "assert_url_contains",
]


@dataclass
class DslGenerationRuntimeStats:
    total_requests: int = 0
    success_count: int = 0
    failure_count: int = 0
    last_model: str | None = None
    last_error_type: str | None = None
    last_error_message: str | None = None


_RUNTIME_STATS = DslGenerationRuntimeStats()
_RUNTIME_STATS_LOCK = Lock()


class DslGenerationFeedbackConflictError(RuntimeError):
    """Raised when generation feedback was already recorded with a different decision."""


class DslGenerationFeedbackPermissionError(RuntimeError):
    """Raised when a user tries to record feedback for another actor's generation run."""


def validate_dsl_case(test_case: DSLCase) -> DSLValidationResult:
    return DSLValidationResult(
        case=test_case,
        supported_actions=SUPPORTED_DSL_ACTIONS,
    )


def generate_dsl_case(session: Session, payload: GenerateDslRequest) -> GenerateDslResponse:
    _ensure_user_exists(session, payload.actor_user_id)
    if payload.project_id is not None:
        _ensure_project_exists(session, payload.project_id)
    if payload.case_id is not None:
        _ensure_case_exists(session, payload.case_id)
    resolved_generation_mode = resolve_generation_mode(payload.generation_mode)

    with _RUNTIME_STATS_LOCK:
        _RUNTIME_STATS.total_requests += 1

    try:
        generated_case, warnings, normalization_notes, generation_meta = generate_case_draft(
            payload=payload,
            supported_actions=SUPPORTED_DSL_ACTIONS,
        )
    except (DslGenerationConfigError, DslGenerationError) as exc:
        model_name = get_settings().ai_dsl_model
        _record_generation_failure(model_name=model_name, error=exc)
        _persist_generation_run(
            session,
            payload=payload,
            generation_mode=resolved_generation_mode,
            success=False,
            model_name=model_name,
            warnings=[],
            normalization_notes=[],
            generated_case=None,
            generation_meta=None,
            error=exc,
        )
        raise

    _record_generation_success(generation_meta)
    generation_run = _persist_generation_run(
        session,
        payload=payload,
        generation_mode=generation_meta.generation_mode,
        success=True,
        model_name=generation_meta.model,
        warnings=warnings,
        normalization_notes=normalization_notes,
        generated_case=generated_case,
        generation_meta=generation_meta,
        error=None,
    )
    return GenerateDslResponse(
        generation_id=generation_run.id,
        case=generated_case,
        supported_actions=SUPPORTED_DSL_ACTIONS,
        warnings=warnings,
        normalization_notes=normalization_notes,
        generation_meta=generation_meta,
    )


def get_dsl_generation_runtime_stats() -> DslGenerationRuntimeStats:
    with _RUNTIME_STATS_LOCK:
        return DslGenerationRuntimeStats(
            total_requests=_RUNTIME_STATS.total_requests,
            success_count=_RUNTIME_STATS.success_count,
            failure_count=_RUNTIME_STATS.failure_count,
            last_model=_RUNTIME_STATS.last_model,
            last_error_type=_RUNTIME_STATS.last_error_type,
            last_error_message=_RUNTIME_STATS.last_error_message,
        )


def reset_dsl_generation_runtime_stats() -> None:
    with _RUNTIME_STATS_LOCK:
        _RUNTIME_STATS.total_requests = 0
        _RUNTIME_STATS.success_count = 0
        _RUNTIME_STATS.failure_count = 0
        _RUNTIME_STATS.last_model = None
        _RUNTIME_STATS.last_error_type = None
        _RUNTIME_STATS.last_error_message = None


def list_dsl_generation_runs(
    session: Session,
    *,
    status: str | None = None,
    feedback_status: DslGenerationFeedbackStatus | None = None,
    generation_mode: GenerateDslMode | None = None,
    import_mode: GenerateDslImportMode | None = None,
    prompt_variant: DslGenerationPromptVariant | None = None,
    rejection_reason_code: DslGenerationRejectionReasonCode | None = None,
    has_risk_flags: bool | None = None,
    model_name: str | None = None,
    project_id: int | None = None,
    case_id: int | None = None,
    created_from: datetime | None = None,
    created_to: datetime | None = None,
    limit: int = 20,
    offset: int = 0,
) -> list[StoredDslGenerationRunSummary]:
    statement = select(DslGenerationRun).order_by(DslGenerationRun.created_at.desc(), DslGenerationRun.id.desc())
    if status == "success":
        statement = statement.where(DslGenerationRun.success.is_(True))
    elif status == "failed":
        statement = statement.where(DslGenerationRun.success.is_(False))
    if feedback_status is not None:
        statement = statement.where(DslGenerationRun.feedback_status == feedback_status)
    if generation_mode is not None:
        statement = statement.where(DslGenerationRun.generation_mode == generation_mode)
    if import_mode is not None:
        statement = statement.where(DslGenerationRun.import_mode == import_mode)
    if prompt_variant is not None:
        statement = statement.where(DslGenerationRun.prompt_variant == prompt_variant)
    if rejection_reason_code is not None:
        statement = statement.where(DslGenerationRun.rejection_reason_code == rejection_reason_code)
    if has_risk_flags is not None:
        comparator = func.json_array_length(DslGenerationRun.risk_flags_json)
        statement = statement.where(comparator > 0 if has_risk_flags else comparator == 0)
    if model_name:
        statement = statement.where(DslGenerationRun.model_name == model_name)
    if project_id is not None:
        statement = statement.where(DslGenerationRun.project_id == project_id)
    if case_id is not None:
        statement = statement.where(DslGenerationRun.case_id == case_id)
    if created_from is not None:
        statement = statement.where(DslGenerationRun.created_at >= _normalize_filter_datetime(created_from))
    if created_to is not None:
        statement = statement.where(DslGenerationRun.created_at <= _normalize_filter_datetime(created_to))
    statement = statement.limit(limit).offset(offset)
    records = session.scalars(statement).all()
    return [_to_stored_dsl_generation_run_summary(record) for record in records]


def get_dsl_generation_run_detail(session: Session, generation_id: int) -> StoredDslGenerationRunDetail:
    record = session.get(DslGenerationRun, generation_id)
    if record is None:
        raise EntityNotFoundError(f"DSL generation run {generation_id} not found.")
    return _to_stored_dsl_generation_run_detail(record)


def record_dsl_generation_feedback(
    session: Session,
    generation_id: int,
    payload: DslGenerationFeedbackRequest,
) -> StoredDslGenerationRunSummary:
    _ensure_user_exists(session, payload.actor_user_id)
    generation_run = _get_generation_run_for_feedback(session, generation_id)
    if generation_run is None:
        raise EntityNotFoundError(f"DSL generation run {generation_id} not found.")
    if generation_run.actor_user_id != payload.actor_user_id:
        raise DslGenerationFeedbackPermissionError("Only the actor who generated this draft can record feedback.")

    if generation_run.feedback_status == "pending":
        generation_run.feedback_status = payload.feedback_status
        generation_run.feedback_import_mode = payload.feedback_import_mode
        generation_run.rejection_reason_code = payload.rejection_reason_code
        generation_run.feedback_note = payload.feedback_note
        generation_run.feedback_recorded_at = datetime.now(UTC).replace(tzinfo=None)
        try:
            session.commit()
        except Exception:
            session.rollback()
            raise
        session.refresh(generation_run)
        return _to_stored_dsl_generation_run_summary(generation_run)

    if (
        generation_run.feedback_status == payload.feedback_status
        and generation_run.feedback_import_mode == payload.feedback_import_mode
        and generation_run.rejection_reason_code == payload.rejection_reason_code
        and generation_run.feedback_note == payload.feedback_note
    ):
        return _to_stored_dsl_generation_run_summary(generation_run)

    raise DslGenerationFeedbackConflictError("该生成记录的反馈已写入不同决策，不能覆盖。")


def get_dsl_generation_durable_stats(session: Session) -> AIDslGenerationStats:
    total_requests = session.scalar(select(func.count()).select_from(DslGenerationRun)) or 0
    success_count = (
        session.scalar(
            select(func.count()).select_from(DslGenerationRun).where(DslGenerationRun.success.is_(True))
    )
    or 0
    )
    failure_count = max(0, total_requests - success_count)
    accepted_count = (
        session.scalar(
            select(func.count())
            .select_from(DslGenerationRun)
            .where(DslGenerationRun.feedback_status == "accepted")
        )
        or 0
    )
    rejected_count = (
        session.scalar(
            select(func.count())
            .select_from(DslGenerationRun)
            .where(DslGenerationRun.feedback_status == "rejected")
        )
        or 0
    )
    pending_count = max(0, total_requests - accepted_count - rejected_count)

    latest_record = session.scalar(
        select(DslGenerationRun).order_by(DslGenerationRun.created_at.desc(), DslGenerationRun.id.desc()).limit(1)
    )

    last_24h_threshold = datetime.now(UTC).replace(tzinfo=None) - timedelta(hours=24)
    last_24h_requests = (
        session.scalar(
            select(func.count()).select_from(DslGenerationRun).where(DslGenerationRun.created_at >= last_24h_threshold)
        )
        or 0
    )
    last_24h_success_count = (
        session.scalar(
            select(func.count())
            .select_from(DslGenerationRun)
            .where(
                DslGenerationRun.created_at >= last_24h_threshold,
                DslGenerationRun.success.is_(True),
            )
        )
        or 0
    )
    last_24h_failure_count = max(0, last_24h_requests - last_24h_success_count)
    last_24h_auto_repair_count = (
        session.scalar(
            select(func.count())
            .select_from(DslGenerationRun)
            .where(
                DslGenerationRun.created_at >= last_24h_threshold,
                or_(
                    DslGenerationRun.repaired_invalid_actions > 0,
                    DslGenerationRun.removed_invalid_steps > 0,
                    DslGenerationRun.removed_invalid_contracts > 0,
                ),
            )
        )
        or 0
    )
    top_error_rows = session.execute(
        select(
            DslGenerationRun.error_type,
            func.count().label("count"),
        )
        .where(
            DslGenerationRun.created_at >= last_24h_threshold,
            DslGenerationRun.success.is_(False),
            DslGenerationRun.error_type.is_not(None),
        )
        .group_by(DslGenerationRun.error_type)
        .order_by(func.count().desc(), DslGenerationRun.error_type.asc())
        .limit(5)
    ).all()
    accepted_import_mode_rows = session.execute(
        select(
            DslGenerationRun.feedback_import_mode,
            func.count().label("count"),
        )
        .where(
            DslGenerationRun.feedback_status == "accepted",
            DslGenerationRun.feedback_import_mode.is_not(None),
        )
        .group_by(DslGenerationRun.feedback_import_mode)
        .order_by(func.count().desc(), DslGenerationRun.feedback_import_mode.asc())
    ).all()
    rejection_reason_rows = session.execute(
        select(
            DslGenerationRun.rejection_reason_code,
            func.count().label("count"),
        )
        .where(
            DslGenerationRun.feedback_status == "rejected",
            DslGenerationRun.rejection_reason_code.is_not(None),
        )
        .group_by(DslGenerationRun.rejection_reason_code)
        .order_by(func.count().desc(), DslGenerationRun.rejection_reason_code.asc())
        .limit(5)
    ).all()
    model_outcome_rows = session.execute(
        select(
            DslGenerationRun.model_name,
            func.count().label("total_requests"),
            func.sum(case((DslGenerationRun.success.is_(True), 1), else_=0)).label("success_count"),
            func.sum(case((DslGenerationRun.feedback_status == "accepted", 1), else_=0)).label("accepted_count"),
            func.sum(case((DslGenerationRun.feedback_status == "rejected", 1), else_=0)).label("rejected_count"),
        )
        .group_by(DslGenerationRun.model_name)
        .order_by(func.count().desc(), DslGenerationRun.model_name.asc())
    ).all()
    generation_mode_rows = session.execute(
        select(
            DslGenerationRun.generation_mode,
            func.count().label("total_requests"),
            func.sum(case((DslGenerationRun.success.is_(True), 1), else_=0)).label("success_count"),
            func.sum(case((DslGenerationRun.feedback_status == "accepted", 1), else_=0)).label("accepted_count"),
            func.sum(case((DslGenerationRun.feedback_status == "rejected", 1), else_=0)).label("rejected_count"),
        )
        .group_by(DslGenerationRun.generation_mode)
        .order_by(func.count().desc(), DslGenerationRun.generation_mode.asc())
    ).all()
    prompt_variant_rows = session.execute(
        select(
            DslGenerationRun.prompt_variant,
            func.count().label("total_requests"),
            func.sum(case((DslGenerationRun.success.is_(True), 1), else_=0)).label("success_count"),
            func.sum(case((DslGenerationRun.feedback_status == "accepted", 1), else_=0)).label("accepted_count"),
            func.sum(case((DslGenerationRun.feedback_status == "rejected", 1), else_=0)).label("rejected_count"),
        )
        .group_by(DslGenerationRun.prompt_variant)
        .order_by(func.count().desc(), DslGenerationRun.prompt_variant.asc())
    ).all()
    context_profile_rows = session.execute(
        select(
            DslGenerationRun.context_profile,
            func.count().label("total_requests"),
            func.sum(case((DslGenerationRun.success.is_(True), 1), else_=0)).label("success_count"),
            func.sum(case((DslGenerationRun.feedback_status == "accepted", 1), else_=0)).label("accepted_count"),
            func.sum(case((DslGenerationRun.feedback_status == "rejected", 1), else_=0)).label("rejected_count"),
        )
        .group_by(DslGenerationRun.context_profile)
        .order_by(func.count().desc(), DslGenerationRun.context_profile.asc())
    ).all()
    rejection_reason_by_variant_rows = session.execute(
        select(
            DslGenerationRun.prompt_variant,
            DslGenerationRun.rejection_reason_code,
            func.count().label("count"),
        )
        .where(
            DslGenerationRun.feedback_status == "rejected",
            DslGenerationRun.rejection_reason_code.is_not(None),
        )
        .group_by(DslGenerationRun.prompt_variant, DslGenerationRun.rejection_reason_code)
        .order_by(func.count().desc(), DslGenerationRun.prompt_variant.asc(), DslGenerationRun.rejection_reason_code.asc())
    ).all()

    return AIDslGenerationStats(
        total_requests=total_requests,
        success_count=success_count,
        failure_count=failure_count,
        accepted_count=accepted_count,
        rejected_count=rejected_count,
        pending_count=pending_count,
        decision_coverage_rate=((accepted_count + rejected_count) / total_requests if total_requests else 0.0),
        last_model=latest_record.model_name if latest_record is not None else None,
        last_error_type=latest_record.error_type if latest_record is not None else None,
        last_error_message=latest_record.error_message if latest_record is not None else None,
        last_24h_requests=last_24h_requests,
        last_24h_success_count=last_24h_success_count,
        last_24h_failure_count=last_24h_failure_count,
        last_24h_auto_repair_rate=(
            last_24h_auto_repair_count / last_24h_requests if last_24h_requests else 0.0
        ),
        top_error_types=[
            AIDslGenerationErrorTypeCount(error_type=error_type, count=count)
            for error_type, count in top_error_rows
            if error_type is not None
        ],
        accepted_import_mode_breakdown=[
            AIDslGenerationImportModeCount(import_mode=import_mode, count=count)
            for import_mode, count in accepted_import_mode_rows
            if import_mode is not None
        ],
        top_rejection_reasons=[
            AIDslGenerationRejectionReasonCount(rejection_reason_code=rejection_reason_code, count=count)
            for rejection_reason_code, count in rejection_reason_rows
            if rejection_reason_code is not None
        ],
        prompt_variant_breakdown=[
            AIDslGenerationPromptVariantBreakdown(
                prompt_variant=prompt_variant,
                total_requests=total_requests,
                success_count=success_count or 0,
                accepted_count=accepted_count or 0,
                rejected_count=rejected_count or 0,
            )
            for prompt_variant, total_requests, success_count, accepted_count, rejected_count in prompt_variant_rows
        ],
        context_profile_breakdown=[
            AIDslGenerationContextProfileBreakdown(
                context_profile=context_profile,
                total_requests=total_requests,
                success_count=success_count or 0,
                accepted_count=accepted_count or 0,
                rejected_count=rejected_count or 0,
            )
            for context_profile, total_requests, success_count, accepted_count, rejected_count in context_profile_rows
        ],
        rejection_reason_by_variant=[
            AIDslGenerationRejectionReasonByVariant(
                prompt_variant=prompt_variant,
                rejection_reason_code=rejection_reason_code,
                count=count,
            )
            for prompt_variant, rejection_reason_code, count in rejection_reason_by_variant_rows
            if rejection_reason_code is not None
        ],
        model_outcome_breakdown=[
            AIDslGenerationModelOutcome(
                model_name=model_name,
                total_requests=total_requests,
                success_count=success_count or 0,
                accepted_count=accepted_count or 0,
                rejected_count=rejected_count or 0,
            )
            for model_name, total_requests, success_count, accepted_count, rejected_count in model_outcome_rows
        ],
        generation_mode_breakdown=[
            AIDslGenerationModeBreakdown(
                generation_mode=generation_mode,
                total_requests=total_requests,
                success_count=success_count or 0,
                accepted_count=accepted_count or 0,
                rejected_count=rejected_count or 0,
            )
            for generation_mode, total_requests, success_count, accepted_count, rejected_count in generation_mode_rows
        ],
    )


def _record_generation_success(meta: GenerateDslMeta) -> None:
    with _RUNTIME_STATS_LOCK:
        _RUNTIME_STATS.success_count += 1
        _RUNTIME_STATS.last_model = meta.model
        _RUNTIME_STATS.last_error_type = None
        _RUNTIME_STATS.last_error_message = None


def _record_generation_failure(*, model_name: str | None, error: Exception) -> None:
    with _RUNTIME_STATS_LOCK:
        _RUNTIME_STATS.failure_count += 1
        _RUNTIME_STATS.last_model = model_name
        _RUNTIME_STATS.last_error_type = type(error).__name__
        _RUNTIME_STATS.last_error_message = str(error)


def _ensure_user_exists(session: Session, user_id: int) -> None:
    if session.get(User, user_id) is None:
        raise EntityNotFoundError(f"User {user_id} not found.")


def _ensure_project_exists(session: Session, project_id: int) -> None:
    if session.get(Project, project_id) is None:
        raise EntityNotFoundError(f"Project {project_id} not found.")


def _ensure_case_exists(session: Session, case_id: int) -> None:
    if session.get(TestCase, case_id) is None:
        raise EntityNotFoundError(f"Case {case_id} not found.")


def _get_generation_run_for_feedback(session: Session, generation_id: int) -> DslGenerationRun | None:
    if _supports_for_update(session):
        statement = (
            select(DslGenerationRun)
            .where(DslGenerationRun.id == generation_id)
            .with_for_update()
        )
        return session.scalars(statement).first()
    return session.get(DslGenerationRun, generation_id)


def _supports_for_update(session: Session) -> bool:
    return session.get_bind().dialect.name == "postgresql"


def _persist_generation_run(
    session: Session,
    *,
    payload: GenerateDslRequest,
    generation_mode: str,
    success: bool,
    model_name: str | None,
    warnings: list[str],
    normalization_notes: list[str],
    generated_case: DSLCase | None,
    generation_meta: GenerateDslMeta | None,
    error: Exception | None,
) -> DslGenerationRun:
    derived_prompt_variant, derived_context_profile = resolve_generation_profile(
        payload=payload,
        generation_mode=generation_mode,
    )
    generation_run = DslGenerationRun(
        actor_user_id=payload.actor_user_id,
        project_id=payload.project_id,
        case_id=payload.case_id,
        prompt_preview=_build_prompt_preview(payload.prompt),
        prompt_sha256=_hash_prompt(payload.prompt),
        prompt_version=AI_DSL_PROMPT_VERSION,
        prompt_variant=generation_meta.prompt_variant if generation_meta is not None else derived_prompt_variant,
        request_base_url=payload.base_url,
        generation_mode=generation_mode,
        import_mode=payload.import_mode,
        model_name=model_name,
        success=success,
        error_type=type(error).__name__ if error is not None else None,
        error_message=str(error) if error is not None else None,
        used_current_case_context=(
            generation_meta.used_current_case_context if generation_meta is not None else payload.current_case is not None
        ),
        used_current_steps_context=(
            generation_meta.used_current_steps_context if generation_meta is not None else payload.current_steps is not None
        ),
        context_profile=generation_meta.context_profile if generation_meta is not None else derived_context_profile,
        base_url_source=generation_meta.base_url_source if generation_meta is not None else "none",
        base_url_backfilled=generation_meta.base_url_backfilled if generation_meta is not None else False,
        repaired_invalid_actions=generation_meta.repaired_invalid_actions if generation_meta is not None else 0,
        removed_invalid_steps=generation_meta.removed_invalid_steps if generation_meta is not None else 0,
        removed_invalid_contracts=generation_meta.removed_invalid_contracts if generation_meta is not None else 0,
        preserve_contracts_requested=payload.preserve_contracts,
        preserve_contracts_applied=generation_meta.preserve_contracts_applied if generation_meta is not None else False,
        warnings_count=len(warnings),
        normalization_notes_count=len(normalization_notes),
        warnings_json=warnings,
        normalization_notes_json=normalization_notes,
        risk_flags_json=list(generation_meta.risk_flags) if generation_meta is not None else [],
        generated_case_json=generated_case.model_dump(mode="json") if generated_case is not None else None,
        feedback_status="pending",
        feedback_import_mode=None,
        rejection_reason_code=None,
        feedback_note=None,
        feedback_recorded_at=None,
    )
    session.add(generation_run)
    try:
        session.commit()
    except Exception:
        session.rollback()
        raise
    session.refresh(generation_run)
    return generation_run


def _build_prompt_preview(prompt: str) -> str:
    return prompt.strip()[:200] or prompt[:200]


def _hash_prompt(prompt: str) -> str:
    return hashlib.sha256(prompt.strip().encode("utf-8")).hexdigest()


def _normalize_filter_datetime(value: datetime) -> datetime:
    if value.tzinfo is None:
        return value
    return value.astimezone(UTC).replace(tzinfo=None)


def _to_stored_dsl_generation_run_summary(record: DslGenerationRun) -> StoredDslGenerationRunSummary:
    return StoredDslGenerationRunSummary(
        id=record.id,
        created_at=record.created_at,
        success=record.success,
        model_name=record.model_name,
        generation_mode=record.generation_mode,
        import_mode=record.import_mode,
        prompt_variant=record.prompt_variant,
        project_id=record.project_id,
        case_id=record.case_id,
        prompt_version=record.prompt_version,
        error_type=record.error_type,
        error_message=record.error_message,
        repaired_invalid_actions=record.repaired_invalid_actions,
        removed_invalid_steps=record.removed_invalid_steps,
        removed_invalid_contracts=record.removed_invalid_contracts,
        warnings_count=record.warnings_count,
        normalization_notes_count=record.normalization_notes_count,
        prompt_preview=record.prompt_preview,
        risk_flags=list(record.risk_flags_json or []),
        feedback_status=record.feedback_status,
        feedback_import_mode=record.feedback_import_mode,
        rejection_reason_code=record.rejection_reason_code,
        feedback_recorded_at=record.feedback_recorded_at,
    )


def _to_stored_dsl_generation_run_detail(record: DslGenerationRun) -> StoredDslGenerationRunDetail:
    return StoredDslGenerationRunDetail(
        **_to_stored_dsl_generation_run_summary(record).model_dump(),
        request_base_url=record.request_base_url,
        generated_case_json=(
            DSLCase.model_validate(record.generated_case_json) if record.generated_case_json is not None else None
        ),
        warnings_json=list(record.warnings_json or []),
        normalization_notes_json=list(record.normalization_notes_json or []),
        feedback_note=record.feedback_note,
        context_profile=record.context_profile,
        used_current_case_context=record.used_current_case_context,
        used_current_steps_context=record.used_current_steps_context,
        preserve_contracts_requested=record.preserve_contracts_requested,
        preserve_contracts_applied=record.preserve_contracts_applied,
    )


__all__ = [
    "DslGenerationConfigError",
    "DslGenerationError",
    "DslGenerationFeedbackConflictError",
    "DslGenerationFeedbackPermissionError",
    "SUPPORTED_DSL_ACTIONS",
    "get_dsl_generation_run_detail",
    "get_dsl_generation_durable_stats",
    "generate_dsl_case",
    "get_dsl_generation_runtime_stats",
    "list_dsl_generation_runs",
    "record_dsl_generation_feedback",
    "reset_dsl_generation_runtime_stats",
    "validate_dsl_case",
]
