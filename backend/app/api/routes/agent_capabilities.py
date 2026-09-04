"""Internal non-browser capabilities used by the Go AgentCore."""

from __future__ import annotations

from fastapi import APIRouter, Depends, HTTPException
from pydantic import ValidationError
from sqlalchemy.orm import Session

from app.api.auth import require_demo_user
from app.application.agent_capabilities import generate_dsl
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
