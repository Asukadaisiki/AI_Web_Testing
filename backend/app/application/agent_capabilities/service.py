"""Non-browser capabilities exposed to the Go AgentCore."""

from __future__ import annotations

from typing import Any

from sqlalchemy import select
from sqlalchemy.orm import Session

from app.application.reporting import build_batch_detail, build_batch_report
from app.models import AIPlanningSession, DslGenerationRun, ExecutionBatch
from app.schemas.cases import CaseCreateRequest
from app.schemas.dsl import GenerateDslRequest
from app.schemas.execution_batches import ExecutionBatchCreateRequest
from app.services.cases import EntityNotFoundError, create_case
from app.services.dsl import generate_dsl_case
from app.services.execution_batches import create_execution_batch


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


def execute_dsl(
    session: Session,
    *,
    project_id: int,
    actor_user_id: int,
    conversation_id: str,
    agent_run_id: str,
    arguments: dict[str, Any],
) -> dict[str, Any]:
    generation_id = int(arguments.get("generation_id", 0))
    if generation_id < 1:
        raise ValueError("generation_id must be a positive integer")
    generation = session.get(DslGenerationRun, generation_id)
    if generation is None:
        raise EntityNotFoundError(f"DSL generation {generation_id} not found.")
    if generation.project_id != project_id:
        raise EntityNotFoundError(
            f"DSL generation {generation_id} does not belong to project {project_id}."
        )
    if not generation.success or not generation.generated_case_json:
        raise ValueError(f"DSL generation {generation_id} has no executable case.")

    idempotency_key = f"agent:{agent_run_id}:generation:{generation_id}"
    existing_batch = session.scalar(
        select(ExecutionBatch).where(
            ExecutionBatch.triggered_by == actor_user_id,
            ExecutionBatch.idempotency_key == idempotency_key,
        )
    )
    if existing_batch is not None:
        return _execution_result(session, existing_batch.id)

    dsl_case = dict(generation.generated_case_json)
    case = create_case(
        session,
        CaseCreateRequest.model_validate(
            {
                **dsl_case,
                "project_id": project_id,
                "actor_user_id": actor_user_id,
            }
        ),
        actor_user_id=actor_user_id,
    )
    planning_session_id = _existing_planning_session_id(session, conversation_id)
    batch = create_execution_batch(
        session,
        ExecutionBatchCreateRequest(
            project_id=project_id,
            case_ids=[case.id],
            planning_session_id=planning_session_id,
            idempotency_key=idempotency_key,
            concurrency_limit=1,
            input_values=arguments.get("input_values") or {},
        ),
        actor_user_id=actor_user_id,
    )
    return _execution_result(session, batch.id)


def get_report(
    session: Session,
    *,
    project_id: int,
    arguments: dict[str, Any],
) -> dict[str, Any]:
    batch_id = int(arguments.get("batch_id", 0))
    if batch_id < 1:
        raise ValueError("batch_id must be a positive integer")
    batch = session.get(ExecutionBatch, batch_id)
    if batch is None or batch.project_id != project_id:
        raise EntityNotFoundError(f"Execution batch {batch_id} not found.")
    return build_batch_report(session, batch_id).model_dump(mode="json")


def _execution_result(session: Session, batch_id: int) -> dict[str, Any]:
    detail = build_batch_detail(session, batch_id)
    case_id = detail.jobs[0].case_id if detail.jobs else None
    return {
        "batch_id": detail.id,
        "case_id": case_id,
        "status": detail.status,
        "report_api_url": f"/api/v1/execution-batches/{detail.id}/report",
    }


def _existing_planning_session_id(session: Session, conversation_id: str) -> int | None:
    if not conversation_id.isdigit():
        return None
    session_id = int(conversation_id)
    if session_id < 1 or session.get(AIPlanningSession, session_id) is None:
        return None
    return session_id
