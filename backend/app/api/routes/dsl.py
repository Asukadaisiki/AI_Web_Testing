"""DSL validation routes."""

from fastapi import APIRouter

from app.schemas.dsl import DSLCase, DSLValidationResult
from app.services.dsl import validate_dsl_case


router = APIRouter(prefix="/dsl", tags=["dsl"])


@router.post("/validate", response_model=DSLValidationResult, summary="Validate structured DSL")
def validate_case(payload: DSLCase) -> DSLValidationResult:
    return validate_dsl_case(payload)
