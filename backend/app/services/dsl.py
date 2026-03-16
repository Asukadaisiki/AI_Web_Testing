"""DSL validation service."""

from __future__ import annotations

from app.ai.dsl_generator import DslGenerationConfigError, DslGenerationError, generate_case_draft
from app.schemas.dsl import DSLCase, DSLValidationResult, GenerateDslRequest, GenerateDslResponse


SUPPORTED_DSL_ACTIONS = [
    "goto",
    "click",
    "input",
    "wait_for",
    "assert_text",
    "assert_url_contains",
]


def validate_dsl_case(test_case: DSLCase) -> DSLValidationResult:
    return DSLValidationResult(
        case=test_case,
        supported_actions=SUPPORTED_DSL_ACTIONS,
    )


def generate_dsl_case(payload: GenerateDslRequest) -> GenerateDslResponse:
    generated_case, warnings = generate_case_draft(
        prompt=payload.prompt,
        base_url=payload.base_url,
        supported_actions=SUPPORTED_DSL_ACTIONS,
    )
    return GenerateDslResponse(
        case=generated_case,
        supported_actions=SUPPORTED_DSL_ACTIONS,
        warnings=warnings,
    )


__all__ = [
    "DslGenerationConfigError",
    "DslGenerationError",
    "SUPPORTED_DSL_ACTIONS",
    "generate_dsl_case",
    "validate_dsl_case",
]
