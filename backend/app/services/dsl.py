"""DSL validation service."""

from __future__ import annotations

import hashlib
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from threading import Lock

from sqlalchemy import func, or_, select
from sqlalchemy.orm import Session

from app.ai.dsl_generator import DslGenerationConfigError, DslGenerationError, generate_case_draft
from app.core.config import get_settings
from app.models import DslGenerationRun, User
from app.schemas.dsl import (
    DSLCase,
    DSLValidationResult,
    GenerateDslMeta,
    GenerateDslRequest,
    GenerateDslResponse,
    StoredDslGenerationRunSummary,
)
from app.schemas.settings import AIDslGenerationErrorTypeCount, AIDslGenerationStats
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


def validate_dsl_case(test_case: DSLCase) -> DSLValidationResult:
    return DSLValidationResult(
        case=test_case,
        supported_actions=SUPPORTED_DSL_ACTIONS,
    )


def generate_dsl_case(session: Session, payload: GenerateDslRequest) -> GenerateDslResponse:
    _ensure_user_exists(session, payload.actor_user_id)

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
            success=False,
            model_name=model_name,
            warnings_count=0,
            normalization_notes_count=0,
            generated_case=None,
            generation_meta=None,
            error=exc,
        )
        raise

    _record_generation_success(generation_meta)
    generation_run = _persist_generation_run(
        session,
        payload=payload,
        success=True,
        model_name=generation_meta.model,
        warnings_count=len(warnings),
        normalization_notes_count=len(normalization_notes),
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
    limit: int = 20,
    offset: int = 0,
) -> list[StoredDslGenerationRunSummary]:
    statement = select(DslGenerationRun).order_by(DslGenerationRun.created_at.desc(), DslGenerationRun.id.desc())
    if status == "success":
        statement = statement.where(DslGenerationRun.success.is_(True))
    elif status == "failed":
        statement = statement.where(DslGenerationRun.success.is_(False))
    statement = statement.limit(limit).offset(offset)
    records = session.scalars(statement).all()
    return [_to_stored_dsl_generation_run_summary(record) for record in records]


def get_dsl_generation_durable_stats(session: Session) -> AIDslGenerationStats:
    total_requests = session.scalar(select(func.count()).select_from(DslGenerationRun)) or 0
    success_count = (
        session.scalar(
            select(func.count()).select_from(DslGenerationRun).where(DslGenerationRun.success.is_(True))
        )
        or 0
    )
    failure_count = max(0, total_requests - success_count)

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

    return AIDslGenerationStats(
        total_requests=total_requests,
        success_count=success_count,
        failure_count=failure_count,
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


def _persist_generation_run(
    session: Session,
    *,
    payload: GenerateDslRequest,
    success: bool,
    model_name: str | None,
    warnings_count: int,
    normalization_notes_count: int,
    generated_case: DSLCase | None,
    generation_meta: GenerateDslMeta | None,
    error: Exception | None,
) -> DslGenerationRun:
    generation_run = DslGenerationRun(
        actor_user_id=payload.actor_user_id,
        prompt_preview=_build_prompt_preview(payload.prompt),
        prompt_sha256=_hash_prompt(payload.prompt),
        request_base_url=payload.base_url,
        generation_mode=payload.generation_mode,
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
        base_url_source=generation_meta.base_url_source if generation_meta is not None else "none",
        base_url_backfilled=generation_meta.base_url_backfilled if generation_meta is not None else False,
        repaired_invalid_actions=generation_meta.repaired_invalid_actions if generation_meta is not None else 0,
        removed_invalid_steps=generation_meta.removed_invalid_steps if generation_meta is not None else 0,
        removed_invalid_contracts=generation_meta.removed_invalid_contracts if generation_meta is not None else 0,
        warnings_count=warnings_count,
        normalization_notes_count=normalization_notes_count,
        generated_case_json=generated_case.model_dump(mode="json") if generated_case is not None else None,
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


def _to_stored_dsl_generation_run_summary(record: DslGenerationRun) -> StoredDslGenerationRunSummary:
    return StoredDslGenerationRunSummary(
        id=record.id,
        created_at=record.created_at,
        success=record.success,
        model_name=record.model_name,
        generation_mode=record.generation_mode,
        import_mode=record.import_mode,
        error_type=record.error_type,
        error_message=record.error_message,
        repaired_invalid_actions=record.repaired_invalid_actions,
        removed_invalid_steps=record.removed_invalid_steps,
        removed_invalid_contracts=record.removed_invalid_contracts,
        warnings_count=record.warnings_count,
        normalization_notes_count=record.normalization_notes_count,
        prompt_preview=record.prompt_preview,
    )


__all__ = [
    "DslGenerationConfigError",
    "DslGenerationError",
    "SUPPORTED_DSL_ACTIONS",
    "get_dsl_generation_durable_stats",
    "generate_dsl_case",
    "get_dsl_generation_runtime_stats",
    "list_dsl_generation_runs",
    "reset_dsl_generation_runtime_stats",
    "validate_dsl_case",
]
