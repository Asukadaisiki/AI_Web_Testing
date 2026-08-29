"""Schema presenters shared by planning application services."""

from __future__ import annotations

from app.application.planning.project_context import get_active_project_id
from app.models import AIPlanningDraft, AIPlanningMessage, AIPlanningSession
from app.schemas.ai_planning import (
    AIPlanningDraft as AIPlanningDraftSchema,
    AIPlanningMessage as AIPlanningMessageSchema,
    AIPlanningRequirements,
    AIPlanningSession as AIPlanningSessionSchema,
    ProjectSummaryInSession,
)


def to_session_schema(record: AIPlanningSession) -> AIPlanningSessionSchema:
    plan_raw = dict(record.plan_json) if record.plan_json else None
    if plan_raw is not None:
        plan_raw.pop("_page_results", None)
        max_page_element_chars = 50_000
        for scenario in plan_raw.get("scenarios", []) or []:
            page_elements = scenario.get("page_elements", "")
            if isinstance(page_elements, str) and len(page_elements) > max_page_element_chars:
                removed_chars = len(page_elements) - max_page_element_chars
                scenario["page_elements"] = (
                    page_elements[:max_page_element_chars]
                    + f"\n...[truncated {removed_chars} chars]"
                )

    active_project_id = get_active_project_id(record)
    return AIPlanningSessionSchema(
        id=record.id,
        actor_user_id=record.actor_user_id,
        active_project_id=active_project_id,
        case_id=record.case_id,
        title=record.title,
        status=record.status,
        requirements=AIPlanningRequirements.model_validate(record.requirements_json or {}),
        plan=plan_raw,
        missing_slots=record.missing_slots_json or [],
        last_error_message=record.last_error_message,
        created_at=record.created_at,
        updated_at=record.updated_at,
        projects=[
            ProjectSummaryInSession(
                id=project.id,
                name=project.name,
                description=project.description,
                is_active=project.id == active_project_id,
            )
            for project in (record.projects or [])
        ],
    )


def to_message_schema(record: AIPlanningMessage) -> AIPlanningMessageSchema:
    return AIPlanningMessageSchema(
        id=record.id,
        session_id=record.session_id,
        role=record.role,
        turn_type=record.turn_type,
        content=record.content,
        structured_payload=record.structured_payload_json,
        created_at=record.created_at,
    )


def to_draft_schema(record: AIPlanningDraft) -> AIPlanningDraftSchema:
    return AIPlanningDraftSchema(
        id=record.id,
        session_id=record.session_id,
        scenario_key=record.scenario_key,
        title=record.title,
        status=record.status,
        dsl_generation_id=record.dsl_generation_id,
        dsl_case=record.dsl_case_json,
        warnings=record.warnings_json or [],
        normalization_notes=record.normalization_notes_json or [],
        error_message=record.error_message,
        created_at=record.created_at,
        updated_at=record.updated_at,
    )
