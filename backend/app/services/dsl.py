"""DSL validation service."""

from __future__ import annotations

from dataclasses import dataclass
from threading import Lock

from app.ai.dsl_generator import DslGenerationConfigError, DslGenerationError, generate_case_draft
from app.core.config import get_settings
from app.schemas.dsl import (
    DSLCase,
    DSLValidationResult,
    GenerateDslMeta,
    GenerateDslRequest,
    GenerateDslResponse,
)


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


def generate_dsl_case(payload: GenerateDslRequest) -> GenerateDslResponse:
    with _RUNTIME_STATS_LOCK:
        _RUNTIME_STATS.total_requests += 1

    try:
        generated_case, warnings, normalization_notes, generation_meta = generate_case_draft(
            payload=payload,
            supported_actions=SUPPORTED_DSL_ACTIONS,
        )
    except (DslGenerationConfigError, DslGenerationError) as exc:
        _record_generation_failure(model_name=get_settings().ai_dsl_model, error=exc)
        raise

    _record_generation_success(generation_meta)
    return GenerateDslResponse(
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


__all__ = [
    "DslGenerationConfigError",
    "DslGenerationError",
    "SUPPORTED_DSL_ACTIONS",
    "generate_dsl_case",
    "get_dsl_generation_runtime_stats",
    "reset_dsl_generation_runtime_stats",
    "validate_dsl_case",
]
