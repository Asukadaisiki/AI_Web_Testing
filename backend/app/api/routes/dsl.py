"""DSL validation routes."""

from fastapi import APIRouter, HTTPException

from app.schemas.dsl import DSLCase, DSLValidationResult, GenerateDslRequest, GenerateDslResponse
from app.services.dsl import (
    DslGenerationConfigError,
    DslGenerationError,
    generate_dsl_case,
    validate_dsl_case,
)


router = APIRouter(prefix="/dsl", tags=["dsl"])


@router.post("/validate", response_model=DSLValidationResult, summary="Validate structured DSL")
def validate_case(payload: DSLCase) -> DSLValidationResult:
    return validate_dsl_case(payload)


@router.post("/generate", response_model=GenerateDslResponse, summary="Generate structured DSL draft")
def generate_case(payload: GenerateDslRequest) -> GenerateDslResponse:
    try:
        return generate_dsl_case(payload)
    except DslGenerationConfigError as exc:
        raise HTTPException(status_code=503, detail=str(exc)) from exc
    except DslGenerationError as exc:
        raise HTTPException(status_code=502, detail=str(exc)) from exc
