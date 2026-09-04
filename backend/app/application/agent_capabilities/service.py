"""Non-browser capabilities exposed to the Go AgentCore."""

from __future__ import annotations

from typing import Any

from sqlalchemy.orm import Session

from app.schemas.dsl import GenerateDslRequest
from app.services.dsl import generate_dsl_case


def generate_dsl(
    session: Session,
    *,
    project_id: int,
    actor_user_id: int,
    arguments: dict[str, Any],
) -> dict[str, Any]:
    payload = GenerateDslRequest.model_validate(
        {
            **arguments,
            "project_id": project_id,
            "actor_user_id": actor_user_id,
        }
    )
    result = generate_dsl_case(session, payload)
    return result.model_dump(mode="json")
