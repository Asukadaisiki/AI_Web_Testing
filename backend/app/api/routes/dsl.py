"""DSL validation routes."""

from fastapi import APIRouter, Depends, HTTPException, Query
from sqlalchemy.orm import Session

from app.db import get_db_session
from app.schemas.dsl import (
    DSLCase,
    DSLValidationResult,
    DslGenerationFeedbackRequest,
    DslGenerationRunStatus,
    GenerateDslRequest,
    GenerateDslResponse,
    StoredDslGenerationRunSummary,
)
from app.services import EntityNotFoundError
from app.services.dsl import (
    DslGenerationConfigError,
    DslGenerationError,
    DslGenerationFeedbackConflictError,
    generate_dsl_case,
    list_dsl_generation_runs,
    record_dsl_generation_feedback,
    validate_dsl_case,
)


router = APIRouter(prefix="/dsl", tags=["dsl"])


@router.post("/validate", response_model=DSLValidationResult, summary="Validate structured DSL")
def validate_case(payload: DSLCase) -> DSLValidationResult:
    return validate_dsl_case(payload)


@router.post("/generate", response_model=GenerateDslResponse, summary="Generate structured DSL draft")
def generate_case(
    payload: GenerateDslRequest,
    session: Session = Depends(get_db_session),
) -> GenerateDslResponse:
    try:
        return generate_dsl_case(session, payload)
    except EntityNotFoundError as exc:
        raise HTTPException(status_code=404, detail=str(exc)) from exc
    except DslGenerationConfigError as exc:
        raise HTTPException(status_code=503, detail=str(exc)) from exc
    except DslGenerationError as exc:
        raise HTTPException(status_code=502, detail=str(exc)) from exc


@router.get("/generations", response_model=list[StoredDslGenerationRunSummary], summary="List DSL generation runs")
def list_generation_runs_route(
    status: DslGenerationRunStatus | None = Query(default=None),
    limit: int = Query(default=20, ge=1, le=200),
    offset: int = Query(default=0, ge=0),
    session: Session = Depends(get_db_session),
) -> list[StoredDslGenerationRunSummary]:
    return list_dsl_generation_runs(session, status=status, limit=limit, offset=offset)


@router.patch(
    "/generations/{generation_id}/feedback",
    response_model=StoredDslGenerationRunSummary,
    summary="Record DSL generation feedback",
)
def record_generation_feedback_route(
    generation_id: int,
    payload: DslGenerationFeedbackRequest,
    session: Session = Depends(get_db_session),
) -> StoredDslGenerationRunSummary:
    try:
        return record_dsl_generation_feedback(session, generation_id, payload)
    except EntityNotFoundError as exc:
        raise HTTPException(status_code=404, detail=str(exc)) from exc
    except DslGenerationFeedbackConflictError as exc:
        raise HTTPException(status_code=409, detail=str(exc)) from exc
