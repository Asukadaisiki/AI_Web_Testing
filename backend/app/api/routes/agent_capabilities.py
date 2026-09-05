"""Internal non-browser capabilities used by the Go AgentCore."""

from __future__ import annotations

from fastapi import APIRouter, Depends, HTTPException
from pydantic import ValidationError
from sqlalchemy.orm import Session

from app.api.auth import require_demo_user
from app.api.capability_auth import require_capability_access
from app.application.agent_capabilities import (
    execute_dsl,
    generate_dsl,
    get_report,
    prepare_fix_and_retry,
)
from app.db import get_db_session
from app.models import User
from app.schemas.agent_capabilities import AgentCapabilityRequest, AgentCapabilityResponse
from app.services.cases import EntityNotFoundError
from app.services.dsl import DslGenerationConfigError, DslGenerationError


router = APIRouter(prefix="/internal/agent-capabilities", tags=["internal-agent"])


@router.post("/generate-dsl", response_model=AgentCapabilityResponse)
def generate_dsl_capability(
    payload: AgentCapabilityRequest,
    session: Session = Depends(get_db_session),
    current_user: User = Depends(require_demo_user),
) -> AgentCapabilityResponse:
    require_capability_access(
        session,
        current_user,
        project_id=payload.project_id,
        conversation_id=payload.conversation_id,
    )
    try:
        result = generate_dsl(
            session,
            project_id=payload.project_id,
            actor_user_id=current_user.id,
            arguments=payload.arguments,
        )
    except (ValidationError, ValueError) as exc:
        raise HTTPException(status_code=422, detail=str(exc)) from exc
    except EntityNotFoundError as exc:
        raise HTTPException(status_code=404, detail=str(exc)) from exc
    except DslGenerationConfigError as exc:
        raise HTTPException(status_code=503, detail=str(exc)) from exc
    except DslGenerationError as exc:
        raise HTTPException(status_code=502, detail=str(exc)) from exc
    return AgentCapabilityResponse(result=result)


@router.post("/execute-dsl", response_model=AgentCapabilityResponse)
def execute_dsl_capability(
    payload: AgentCapabilityRequest,
    session: Session = Depends(get_db_session),
    current_user: User = Depends(require_demo_user),
) -> AgentCapabilityResponse:
    require_capability_access(
        session,
        current_user,
        project_id=payload.project_id,
        conversation_id=payload.conversation_id,
    )
    if not payload.run_id:
        raise HTTPException(status_code=422, detail="run_id is required")
    try:
        result = execute_dsl(
            session,
            project_id=payload.project_id,
            actor_user_id=current_user.id,
            conversation_id=payload.conversation_id,
            agent_run_id=payload.run_id,
            arguments=payload.arguments,
        )
    except EntityNotFoundError as exc:
        raise HTTPException(status_code=404, detail=str(exc)) from exc
    except ValueError as exc:
        raise HTTPException(status_code=422, detail=str(exc)) from exc
    return AgentCapabilityResponse(result=result)


@router.post("/get-report", response_model=AgentCapabilityResponse)
def get_report_capability(
    payload: AgentCapabilityRequest,
    session: Session = Depends(get_db_session),
    current_user: User = Depends(require_demo_user),
) -> AgentCapabilityResponse:
    require_capability_access(
        session,
        current_user,
        project_id=payload.project_id,
        conversation_id=payload.conversation_id,
    )
    try:
        result = get_report(
            session,
            project_id=payload.project_id,
            arguments=payload.arguments,
        )
    except EntityNotFoundError as exc:
        raise HTTPException(status_code=404, detail=str(exc)) from exc
    except ValueError as exc:
        raise HTTPException(status_code=422, detail=str(exc)) from exc
    return AgentCapabilityResponse(result=result)


@router.post("/fix-and-retry", response_model=AgentCapabilityResponse)
def fix_and_retry_capability(
    payload: AgentCapabilityRequest,
    session: Session = Depends(get_db_session),
    current_user: User = Depends(require_demo_user),
) -> AgentCapabilityResponse:
    require_capability_access(
        session,
        current_user,
        project_id=payload.project_id,
        conversation_id=payload.conversation_id,
    )
    try:
        result = prepare_fix_and_retry(
            session,
            project_id=payload.project_id,
            arguments=payload.arguments,
        )
    except EntityNotFoundError as exc:
        raise HTTPException(status_code=404, detail=str(exc)) from exc
    except ValueError as exc:
        raise HTTPException(status_code=422, detail=str(exc)) from exc
    return AgentCapabilityResponse(result=result)
