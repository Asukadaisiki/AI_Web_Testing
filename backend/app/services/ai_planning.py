"""Services for AI planning sessions and drafts."""

from __future__ import annotations

import logging

from sqlalchemy import select
from sqlalchemy.orm import Session

from app.ai.test_planning_agent import REQUIRED_REQUIREMENT_SLOTS, run_planning_turn
from app.models import AIPlanningDraft, AIPlanningMessage, AIPlanningSession, DslGenerationRun, Project, TestCase
from app.schemas.ai_planning import (
    AIPlanningDraft as AIPlanningDraftSchema,
    AIPlanningMessage as AIPlanningMessageSchema,
    AIPlanningRequirements,
    AIPlanningSession as AIPlanningSessionSchema,
    AIPlanningSessionDetail,
    AIPlanningSessionSummary,
    AIPlanningToolCall,
    AIPlanningTurnResponse,
    CreateAIPlanningSessionRequest,
    GenerateAIPlanningDraftsRequest,
    UpdateAIPlanningDraftStatusRequest,
)
from app.schemas.dsl import GenerateDslRequest
from app.services.cases import EntityNotFoundError, _ensure_project_member
from app.services.dsl import generate_dsl_case


logger = logging.getLogger(__name__)


class AIPlanningAccessError(ValueError):
    """Raised when a planning session or draft is inaccessible."""


def list_planning_sessions(
    session: Session,
    *,
    actor_user_id: int,
    project_id: int | None = None,
) -> list[AIPlanningSessionSummary]:
    q = session.query(AIPlanningSession).filter(AIPlanningSession.actor_user_id == actor_user_id)
    if project_id is not None:
        q = q.filter(AIPlanningSession.project_id == project_id)
    q = q.order_by(AIPlanningSession.updated_at.desc())
    rows = q.all()
    return [
        AIPlanningSessionSummary(
            id=r.id,
            title=r.title or (r.requirements_json or {}).get("app_under_test"),
            status=r.status,
            created_at=r.created_at,
            updated_at=r.updated_at,
        )
        for r in rows
    ]


def create_planning_session(
    session: Session,
    payload: CreateAIPlanningSessionRequest,
    *,
    actor_user_id: int,
) -> AIPlanningSessionDetail:
    _ensure_project_access(session, project_id=payload.project_id, actor_user_id=actor_user_id)
    if payload.case_id is not None:
        _ensure_case_access(session, case_id=payload.case_id, project_id=payload.project_id, actor_user_id=actor_user_id)

    record = AIPlanningSession(
        actor_user_id=actor_user_id,
        project_id=payload.project_id,
        case_id=payload.case_id,
        status="collecting",
        requirements_json=AIPlanningRequirements().model_dump(mode="json"),
        missing_slots_json=list(REQUIRED_REQUIREMENT_SLOTS),
    )
    session.add(record)
    session.commit()
    session.refresh(record)
    return get_planning_session_detail(session, record.id, actor_user_id=actor_user_id)


def get_planning_session_detail(session: Session, planning_session_id: int, *, actor_user_id: int) -> AIPlanningSessionDetail:
    planning_session = _get_session(session, planning_session_id, actor_user_id=actor_user_id)
    messages = session.scalars(
        select(AIPlanningMessage).where(AIPlanningMessage.session_id == planning_session_id).order_by(AIPlanningMessage.id.asc())
    ).all()
    drafts = session.scalars(
        select(AIPlanningDraft).where(AIPlanningDraft.session_id == planning_session_id).order_by(AIPlanningDraft.id.asc())
    ).all()
    return AIPlanningSessionDetail(
        session=_to_session_schema(planning_session),
        messages=[_to_message_schema(item) for item in messages],
        drafts=[_to_draft_schema(item) for item in drafts],
    )


def send_planning_message(
    session: Session,
    planning_session_id: int,
    *,
    actor_user_id: int,
    content: str,
) -> AIPlanningTurnResponse:
    planning_session = _get_session(session, planning_session_id, actor_user_id=actor_user_id)
    session.add(
        AIPlanningMessage(
            session_id=planning_session.id,
            role="user",
            turn_type="user",
            content=content,
            structured_payload_json=None,
        )
    )
    session.flush()

    transcript_records = session.scalars(
        select(AIPlanningMessage).where(AIPlanningMessage.session_id == planning_session.id).order_by(AIPlanningMessage.id.asc())
    ).all()
    agent_response = run_planning_turn(
        transcript=[{"role": item.role, "content": item.content} for item in transcript_records if item.turn_type != "tool_call"],
        existing_requirements=AIPlanningRequirements.model_validate(planning_session.requirements_json or {}),
        db_session=session,
        project_id=planning_session.project_id,
    )

    planning_session.status = agent_response.session_status
    planning_session.requirements_json = agent_response.requirements.model_dump(mode="json")
    planning_session.plan_json = agent_response.plan.model_dump(mode="json") if agent_response.plan is not None else None
    planning_session.missing_slots_json = agent_response.missing_slots
    planning_session.title = planning_session.title or agent_response.requirements.business_goal or "AI 测试规划"
    planning_session.last_error_message = (
        agent_response.assistant_message if agent_response.session_status == "error" else None
    )

    for tool_call in agent_response.tool_calls:
        session.add(
            AIPlanningMessage(
                session_id=planning_session.id,
                role="assistant",
                turn_type="tool_call",
                content=f"调用工具 {tool_call.tool}",
                structured_payload_json={
                    "type": "tool_call",
                    **tool_call.model_dump(mode="json"),
                },
            )
        )

    turn_type = "system_error" if agent_response.session_status == "error" else ("plan" if agent_response.plan is not None else "followup")
    session.add(
        AIPlanningMessage(
            session_id=planning_session.id,
            role="assistant",
            turn_type=turn_type,
            content=agent_response.assistant_message,
            structured_payload_json={
                "missing_slots": agent_response.missing_slots,
                "suggested_questions": agent_response.suggested_questions,
                "plan": agent_response.plan.model_dump(mode="json") if agent_response.plan is not None else None,
                "tool_calls": [item.model_dump(mode="json") for item in agent_response.tool_calls],
            },
        )
    )
    session.commit()
    session.refresh(planning_session)
    return agent_response


def generate_planning_drafts(
    session: Session,
    planning_session_id: int,
    payload: GenerateAIPlanningDraftsRequest,
    *,
    actor_user_id: int,
) -> AIPlanningTurnResponse:
    planning_session = _get_session(session, planning_session_id, actor_user_id=actor_user_id)
    plan = planning_session.plan_json or {}
    scenarios = {
        item["scenario_key"]: item
        for item in plan.get("scenarios", [])
        if isinstance(item, dict) and isinstance(item.get("scenario_key"), str)
    }
    drafts: list[AIPlanningDraftSchema] = []
    base_url = _normalize_base_url(planning_session.requirements_json or {})
    invalid_scenarios: list[str] = []

    for scenario_key in payload.scenario_keys:
        scenario = scenarios.get(scenario_key)
        if scenario is None:
            invalid_scenarios.append(scenario_key)
            record = AIPlanningDraft(
                session_id=planning_session.id,
                scenario_key=scenario_key,
                title=f"场景 {scenario_key} 不存在",
                status="failed",
                dsl_generation_id=None,
                dsl_case_json=None,
                warnings_json=[f"场景 '{scenario_key}' 未在 AI 生成的测试计划中找到"],
                normalization_notes_json=[],
                error_message=f"场景 '{scenario_key}' 不存在于当前测试计划中。",
            )
            session.add(record)
            session.flush()
            drafts.append(_to_draft_schema(record))
            continue

        existing = session.scalar(
            select(AIPlanningDraft).where(
                AIPlanningDraft.session_id == planning_session.id,
                AIPlanningDraft.scenario_key == scenario_key,
            )
        )
        if existing is not None:
            drafts.append(_to_draft_schema(existing))
            continue

        try:
            generated = generate_dsl_case(
                session,
                GenerateDslRequest(
                    prompt=scenario["draft_prompt"],
                    base_url=base_url,
                    actor_user_id=actor_user_id,
                    project_id=planning_session.project_id,
                    case_id=planning_session.case_id,
                    current_case=payload.current_case,
                    current_steps=payload.current_steps,
                    current_input_contract=payload.current_input_contract,
                    current_output_contract=payload.current_output_contract,
                    preserve_contracts=payload.preserve_contracts,
                ),
            )
            record = AIPlanningDraft(
                session_id=planning_session.id,
                scenario_key=scenario_key,
                title=scenario["title"],
                status="generated",
                dsl_generation_id=(
                    generated.generation_id if session.get(DslGenerationRun, generated.generation_id) is not None else None
                ),
                dsl_case_json=generated.case.model_dump(mode="json"),
                warnings_json=generated.warnings,
                normalization_notes_json=generated.normalization_notes,
                error_message=None,
            )
        except Exception as exc:
            logger.error(
                "Failed to generate DSL case for scenario '%s' in session %s",
                scenario_key,
                planning_session.id,
                exc_info=True,
            )
            record = AIPlanningDraft(
                session_id=planning_session.id,
                scenario_key=scenario_key,
                title=scenario["title"],
                status="failed",
                dsl_generation_id=None,
                dsl_case_json=None,
                warnings_json=[],
                normalization_notes_json=[],
                error_message=str(exc),
            )
        session.add(record)
        session.flush()
        drafts.append(_to_draft_schema(record))

    planning_session.status = "drafts_ready"
    session.commit()
    session.refresh(planning_session)

    message = "已根据所选场景生成 DSL 草案。"
    if invalid_scenarios:
        message += f" 注意：以下场景不存在于当前测试计划中：{', '.join(invalid_scenarios)}"

    return AIPlanningTurnResponse(
        assistant_message=message,
        session_status="drafts_ready",
        requirements=AIPlanningRequirements.model_validate(planning_session.requirements_json or {}),
        missing_slots=planning_session.missing_slots_json or [],
        suggested_questions=[],
        plan=_to_session_schema(planning_session).plan,
        drafts=drafts,
        next_action="drafts_generated",
        tool_calls=[],
    )


def update_planning_draft_status(
    session: Session,
    draft_id: int,
    payload: UpdateAIPlanningDraftStatusRequest,
    *,
    actor_user_id: int,
) -> AIPlanningDraftSchema:
    draft = session.get(AIPlanningDraft, draft_id)
    if draft is None:
        raise EntityNotFoundError(f"AI planning draft {draft_id} not found.")
    _get_session(session, draft.session_id, actor_user_id=actor_user_id)
    draft.status = payload.status
    session.add(draft)
    session.commit()
    session.refresh(draft)
    return _to_draft_schema(draft)


def _get_session(session: Session, planning_session_id: int, *, actor_user_id: int) -> AIPlanningSession:
    planning_session = session.get(AIPlanningSession, planning_session_id)
    if planning_session is None:
        raise EntityNotFoundError(f"AI planning session {planning_session_id} not found.")
    if planning_session.actor_user_id != actor_user_id:
        raise AIPlanningAccessError("AI planning session access denied.")
    return planning_session


def _ensure_project_access(session: Session, *, project_id: int, actor_user_id: int) -> None:
    if session.get(Project, project_id) is None:
        raise EntityNotFoundError(f"Project {project_id} not found.")
    _ensure_project_member(session, project_id, actor_user_id)


def _ensure_case_access(session: Session, *, case_id: int, project_id: int, actor_user_id: int) -> None:
    case_record = session.get(TestCase, case_id)
    if case_record is None:
        raise EntityNotFoundError(f"Case {case_id} not found.")
    if case_record.project_id != project_id:
        raise EntityNotFoundError(f"Case {case_id} does not belong to project {project_id}.")
    _ensure_project_member(session, project_id, actor_user_id)


def _normalize_base_url(requirements_json: dict) -> str | None:
    value = requirements_json.get("entry_url_or_page")
    if isinstance(value, str) and value.startswith(("http://", "https://")):
        return value
    return None


def _to_session_schema(record: AIPlanningSession) -> AIPlanningSessionSchema:
    return AIPlanningSessionSchema(
        id=record.id,
        actor_user_id=record.actor_user_id,
        project_id=record.project_id,
        case_id=record.case_id,
        title=record.title,
        status=record.status,
        requirements=AIPlanningRequirements.model_validate(record.requirements_json or {}),
        plan=record.plan_json,
        missing_slots=record.missing_slots_json or [],
        last_error_message=record.last_error_message,
        created_at=record.created_at,
        updated_at=record.updated_at,
    )


def _to_message_schema(record: AIPlanningMessage) -> AIPlanningMessageSchema:
    return AIPlanningMessageSchema(
        id=record.id,
        session_id=record.session_id,
        role=record.role,
        turn_type=record.turn_type,
        content=record.content,
        structured_payload=record.structured_payload_json,
        created_at=record.created_at,
    )


def _to_draft_schema(record: AIPlanningDraft) -> AIPlanningDraftSchema:
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
