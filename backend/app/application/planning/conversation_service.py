"""Planning conversation use cases."""

from __future__ import annotations

import logging
import time
from collections.abc import Callable, Generator
from typing import Protocol

from sqlalchemy import select
from sqlalchemy.orm import Session, sessionmaker

from app.ai.test_planning_agent import (
    AutoDraftGenerator,
    run_planning_turn,
    stream_planning_turn,
)
from app.ai.tool_result_cache import extract_raw_page_results
from app.application.planning.project_context import (
    get_active_project_id,
    get_owned_session,
)
from app.models import AIPlanningMessage, AIPlanningSession
from app.models.ai_planning_tool_result import AIPlanningToolResult
from app.schemas.ai_planning import (
    AIPlanningRequirements,
    AIPlanningTurnResponse,
)


logger = logging.getLogger(__name__)

_STREAMING_FLUSH_INTERVAL = 3


class ConversationContextInjector(Protocol):
    def __call__(
        self,
        transcript: list[dict[str, str]],
        planning_session: AIPlanningSession,
        db_session: Session,
        existing_msg_count: int,
    ) -> list[dict[str, str]]: ...


class ConversationEventLog(Protocol):
    def write(self, event_type: str, data: dict) -> None: ...

    def flush(self) -> None: ...


ConversationEventLogFactory = Callable[..., ConversationEventLog]


def _build_transcript(
    session: Session,
    planning_session: AIPlanningSession,
    context_injector: ConversationContextInjector,
) -> list[dict[str, str]]:
    records = session.scalars(
        select(AIPlanningMessage)
        .where(AIPlanningMessage.session_id == planning_session.id)
        .order_by(AIPlanningMessage.id.asc())
    ).all()
    transcript = [
        {"role": item.role, "content": item.content}
        for item in records
        if item.turn_type != "tool_call"
    ]
    return context_injector(
        transcript,
        planning_session,
        session,
        len(records),
    )


def _apply_turn_response(
    planning_session: AIPlanningSession,
    response: AIPlanningTurnResponse,
) -> None:
    planning_session.status = response.session_status
    planning_session.requirements_json = response.requirements.model_dump(mode="json")
    if response.plan is not None:
        plan_dict = response.plan.model_dump(mode="json")
        plan_dict["_page_results"] = extract_raw_page_results(response.tool_calls)
        planning_session.plan_json = plan_dict
    planning_session.missing_slots_json = response.missing_slots
    planning_session.title = (
        planning_session.title
        or response.requirements.business_goal
        or "AI 测试规划"
    )
    planning_session.last_error_message = (
        response.assistant_message if response.session_status == "error" else None
    )


def _turn_type(response: AIPlanningTurnResponse) -> str:
    if response.session_status == "error":
        return "system_error"
    return "plan" if response.plan is not None else "followup"


def send_planning_message(
    session: Session,
    planning_session_id: int,
    *,
    actor_user_id: int,
    content: str,
    context_injector: ConversationContextInjector,
    auto_draft_generator: AutoDraftGenerator,
) -> AIPlanningTurnResponse:
    planning_session = get_owned_session(
        session,
        planning_session_id,
        actor_user_id=actor_user_id,
    )
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

    response = run_planning_turn(
        transcript=_build_transcript(
            session,
            planning_session,
            context_injector,
        ),
        existing_requirements=AIPlanningRequirements.model_validate(
            planning_session.requirements_json or {}
        ),
        db_session=session,
        project_id=get_active_project_id(planning_session) or 0,
        actor_user_id=actor_user_id,
        planning_session_id=planning_session.id,
        auto_draft_generator=auto_draft_generator,
    )
    _apply_turn_response(planning_session, response)

    for tool_call in response.tool_calls:
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

    session.add(
        AIPlanningMessage(
            session_id=planning_session.id,
            role="assistant",
            turn_type=_turn_type(response),
            content=response.assistant_message,
            structured_payload_json={
                "missing_slots": response.missing_slots,
                "suggested_questions": response.suggested_questions,
                "plan": (
                    response.plan.model_dump(mode="json")
                    if response.plan is not None
                    else None
                ),
                "tool_calls": [
                    item.model_dump(mode="json") for item in response.tool_calls
                ],
                "todo_list": [
                    item.model_dump(mode="json") for item in response.todo_list
                ],
            },
        )
    )
    session.commit()
    session.refresh(planning_session)
    return response


def _flush_streaming_message(
    session: Session,
    message_id: int,
    content: str,
    *,
    phase: str | None = None,
    phase_message: str | None = None,
) -> None:
    try:
        if not session.is_active:
            session.rollback()
        message = session.merge(session.get(AIPlanningMessage, message_id))
        message.content = content
        payload = message.structured_payload_json or {}
        payload["_streaming"] = True
        if phase is not None:
            payload["_phase"] = phase
        if phase_message is not None:
            payload["_phaseMessage"] = phase_message
        message.structured_payload_json = payload
        session.commit()
    except Exception:
        logger.warning(
            "Failed to flush streaming message %d, skipping",
            message_id,
            exc_info=True,
        )
        if session.is_active:
            session.rollback()


def _flush_streaming_content_block(
    session: Session,
    message_id: int,
    event: dict,
    *,
    phase: str | None = None,
    phase_message: str | None = None,
) -> None:
    """Persist ordered pi-style content blocks onto the streaming message.

    The final assistant text is still mirrored to ``message.content`` for
    backward compatibility with the existing transcript model, but the
    authoritative rendering structure is now
    ``structured_payload_json["content_blocks"]``.
    """
    try:
        if not session.is_active:
            session.rollback()
        message = session.merge(session.get(AIPlanningMessage, message_id))
        payload = message.structured_payload_json or {}
        payload["_streaming"] = True
        if phase is not None:
            payload["_phase"] = phase
        if phase_message is not None:
            payload["_phaseMessage"] = phase_message

        blocks = list(payload.get("content_blocks") or [])
        content_index = int(event.get("content_index", 0))
        kind = event.get("kind", "text")

        event_type = event.get("type")
        if event_type == "content_block_start":
            if content_index >= len(blocks):
                blocks.extend(
                    {"type": kind, "content": ""}
                    for _ in range(content_index - len(blocks) + 1)
                )
            blocks[content_index] = {"type": kind, "content": ""}
        elif event_type == "content_block_delta":
            if content_index >= len(blocks):
                blocks.extend(
                    {"type": kind, "content": ""}
                    for _ in range(content_index - len(blocks) + 1)
                )
            if blocks[content_index].get("type") != kind:
                blocks[content_index] = {"type": kind, "content": ""}
            blocks[content_index]["content"] += str(event.get("delta", ""))
        elif event_type == "content_block_end":
            if content_index >= len(blocks):
                blocks.extend(
                    {"type": kind, "content": ""}
                    for _ in range(content_index - len(blocks) + 1)
                )
            blocks[content_index] = {
                "type": kind,
                "content": str(event.get("content", blocks[content_index].get("content", ""))),
            }

        payload["content_blocks"] = blocks
        # Keep ``content`` as a plain-text mirror of all text blocks for the
        # existing ``item.content`` render path.
        message.content = "".join(
            block.get("content", "")
            for block in blocks
            if block.get("type") == "text"
        )
        message.structured_payload_json = payload
        session.commit()
    except Exception:
        logger.warning(
            "Failed to flush streaming content block for message %d, skipping",
            message_id,
            exc_info=True,
        )
        if session.is_active:
            session.rollback()


def stream_planning_message(
    session: Session,
    planning_session_id: int,
    *,
    actor_user_id: int,
    content: str,
    context_injector: ConversationContextInjector,
    auto_draft_generator: AutoDraftGenerator,
    event_log_factory: ConversationEventLogFactory,
    session_factory: sessionmaker | None = None,
) -> Generator[dict, None, AIPlanningTurnResponse]:
    start_time = time.monotonic()
    logger.info(
        "[session:%d] Planning message stream start, content_len=%d",
        planning_session_id,
        len(content),
    )

    planning_session = get_owned_session(
        session,
        planning_session_id,
        actor_user_id=actor_user_id,
    )
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
    session.commit()

    stream = stream_planning_turn(
        transcript=_build_transcript(
            session,
            planning_session,
            context_injector,
        ),
        existing_requirements=AIPlanningRequirements.model_validate(
            planning_session.requirements_json or {}
        ),
        db_session=session,
        project_id=get_active_project_id(planning_session) or 0,
        actor_user_id=actor_user_id,
        planning_session_id=planning_session.id,
        auto_draft_generator=auto_draft_generator,
    )

    streaming_message = AIPlanningMessage(
        session_id=planning_session.id,
        role="assistant",
        turn_type="streaming",
        content="",
        structured_payload_json={"_streaming": True},
    )
    session.add(streaming_message)
    session.flush()
    session.commit()
    streaming_message_id = streaming_message.id

    event_log = event_log_factory(
        session_factory=session_factory,
        session_id=planning_session_id,
        message_id=streaming_message_id,
        flush_interval=_STREAMING_FLUSH_INTERVAL,
    )

    text_buffer = ""
    chunks_since_flush = 0
    current_phase = None
    current_phase_message = None

    while True:
        try:
            event = next(stream)
        except StopIteration as stop:
            response = stop.value
            break

        event_log.write(event.get("type", "unknown"), event)
        if event.get("type") == "text_chunk" and not event.get("thinking"):
            text_buffer += event.get("text", "")
            chunks_since_flush += 1
            if chunks_since_flush >= _STREAMING_FLUSH_INTERVAL:
                _flush_streaming_message(
                    session,
                    streaming_message_id,
                    text_buffer,
                    phase=current_phase,
                    phase_message=current_phase_message,
                )
                chunks_since_flush = 0
        elif event.get("type") in ("content_block_start", "content_block_delta", "content_block_end"):
            # pi-style ordered content blocks: persist them on the streaming message
            # so history replays thinking/text interleaving faithfully.
            _flush_streaming_content_block(
                session,
                streaming_message_id,
                event,
                phase=current_phase,
                phase_message=current_phase_message,
            )
        elif event.get("type") == "status":
            current_phase = event.get("phase")
            current_phase_message = event.get("message")
            _flush_streaming_message(
                session,
                streaming_message_id,
                text_buffer,
                phase=current_phase,
                phase_message=current_phase_message,
            )
        yield event

    event_log.flush()
    if not session.is_active:
        logger.warning(
            "[session:%d] Session became inactive after tool calls, rolling back",
            planning_session_id,
        )
        session.rollback()

    planning_session = get_owned_session(
        session,
        planning_session_id,
        actor_user_id=actor_user_id,
    )
    _apply_turn_response(planning_session, response)

    for tool_call in response.tool_calls:
        tool_payload = tool_call.model_dump(mode="json")
        tool_payload.pop("result", None)
        message = AIPlanningMessage(
            session_id=planning_session.id,
            role="assistant",
            turn_type="tool_call",
            content=f"调用工具 {tool_call.tool}",
            structured_payload_json={
                "type": "tool_call",
                **tool_payload,
                "result_summary": getattr(
                    tool_call,
                    "_compressed_result",
                    None,
                ),
            },
        )
        session.add(message)
        session.flush()

        compressed_result = getattr(tool_call, "_compressed_result", None)
        if compressed_result is not None:
            session.add(
                AIPlanningToolResult(
                    session_id=planning_session.id,
                    message_id=message.id,
                    tool_name=tool_call.tool,
                    raw_result_json=(
                        tool_call.result
                        if isinstance(tool_call.result, dict)
                        else None
                    ),
                    summary_json=compressed_result,
                )
            )

    streaming_message = session.merge(
        session.get(AIPlanningMessage, streaming_message_id)
    )
    streaming_message.turn_type = _turn_type(response)
    streaming_message.content = response.assistant_message
    final_payload = streaming_message.structured_payload_json or {}
    final_payload.update({
        "missing_slots": response.missing_slots,
        "suggested_questions": response.suggested_questions,
        "plan": (
            response.plan.model_dump(mode="json")
            if response.plan is not None
            else None
        ),
        "tool_calls": [
            {
                "tool": item.tool,
                "params": item.params,
                "result_summary": getattr(
                    item,
                    "_compressed_result",
                    None,
                ),
            }
            for item in response.tool_calls
        ],
        "todo_list": [
            item.model_dump(mode="json") for item in response.todo_list
        ],
        "_streaming": False,
    })
    streaming_message.structured_payload_json = final_payload
    session.commit()

    elapsed = time.monotonic() - start_time
    logger.info(
        "[session:%d] Planning message stream done, status=%s, "
        "tool_calls=%d, todo=%d, duration=%.2fs, assistant=%s",
        planning_session_id,
        response.session_status,
        len(response.tool_calls),
        len(response.todo_list),
        elapsed,
        (response.assistant_message or "")[:120],
    )
    return response
