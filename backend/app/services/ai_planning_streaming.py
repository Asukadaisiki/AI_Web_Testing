"""Worker-thread bridge for streaming AI planning operations over WebSocket."""

from __future__ import annotations

import asyncio
import json
import logging
import time
import traceback as _traceback
from threading import Event, Thread
from typing import AsyncGenerator, Callable, Generator

from sqlalchemy.orm import Session, sessionmaker

from app.runners.playwright_runner import RunnerCancelledError
from app.services.sse_event_log import EventLogWriter

logger = logging.getLogger(__name__)


class CancellationManager:
    """Tracks per-session cancellation events for WebSocket execution."""

    def __init__(self) -> None:
        self._events: dict[int, Event] = {}

    def register(self, session_id: int) -> Event:
        event = Event()
        self._events[session_id] = event
        return event

    def get(self, session_id: int) -> Event | None:
        return self._events.get(session_id)

    def clear(self, session_id: int) -> None:
        self._events.pop(session_id, None)


class _TerminalSignal:
    """Sentinel placed in the queue to signal the async generator should stop."""


def _serialize_event(event: dict) -> dict:
    """Ensure all event values are JSON-serializable."""
    return json.loads(json.dumps(event, default=str))


def sse_event(event_type: str, data: dict) -> str:
    """Format a dict as an SSE event string."""
    payload = json.dumps(data, default=str, ensure_ascii=False)
    return f"event: {event_type}\ndata: {payload}\n\n"


def _run_sync_generator(
    *,
    generator_factory: Callable[[Session], Generator[dict, None, object]],
    session_factory: sessionmaker,
    queue: asyncio.Queue,
    loop: asyncio.AbstractEventLoop,
    cancel_event: Event | None = None,
    phase: str = "unknown",
) -> None:
    """Run a sync generator in a worker thread, forwarding events to the async queue."""
    try:
        with session_factory() as session:
            stream = generator_factory(session)
            while True:
                if cancel_event is not None and cancel_event.is_set():
                    loop.call_soon_threadsafe(queue.put_nowait, {"type": "cancelled"})
                    break
                try:
                    event = next(stream)
                except StopIteration:
                    break
                except Exception as exc:
                    tb = _traceback.format_exc()
                    logger.exception("Stream iteration error in phase '%s'", phase)
                    loop.call_soon_threadsafe(queue.put_nowait, {
                        "type": "error",
                        "message": str(exc),
                        "error_type": type(exc).__name__,
                        "phase": phase,
                        "traceback": tb[:2000],
                    })
                    break
                loop.call_soon_threadsafe(queue.put_nowait, _serialize_event(event))
    except RunnerCancelledError:
        loop.call_soon_threadsafe(queue.put_nowait, {"type": "cancelled"})
    except Exception as exc:
        tb = _traceback.format_exc()
        logger.exception("Planning stream worker error in phase '%s'", phase)
        loop.call_soon_threadsafe(queue.put_nowait, {
            "type": "error",
            "message": str(exc),
            "error_type": type(exc).__name__,
            "phase": phase,
            "traceback": tb[:2000],
        })
    finally:
        loop.call_soon_threadsafe(queue.put_nowait, _TerminalSignal())


async def _bridge_sync_generator(
    *,
    session_factory: sessionmaker,
    generator_factory: Callable[[Session], Generator[dict, None, object]],
    cancel_event: Event | None = None,
    phase: str = "unknown",
) -> AsyncGenerator[dict, None]:
    """Bridge a sync generator to an async generator for WebSocket delivery."""
    loop = asyncio.get_running_loop()
    queue: asyncio.Queue[dict | _TerminalSignal] = asyncio.Queue()

    thread = Thread(
        target=_run_sync_generator,
        kwargs={
            "generator_factory": generator_factory,
            "session_factory": session_factory,
            "queue": queue,
            "loop": loop,
            "cancel_event": cancel_event,
            "phase": phase,
        },
        daemon=True,
    )
    thread.start()

    while True:
        item = await queue.get()
        if isinstance(item, _TerminalSignal):
            return
        yield _serialize_event(item)


async def stream_planning_chat(
    *,
    session_factory: sessionmaker,
    planning_session_id: int,
    content: str,
    actor_user_id: int,
) -> AsyncGenerator[dict, None]:
    """Bridge streaming planning chat to async WebSocket events."""
    from app.application.planning.conversation_service import (
        stream_planning_message,
    )
    from app.application.planning.context_service import inject_auto_context
    from app.application.planning.draft_service import generate_auto_drafts_for_scenarios

    logger.info("Starting planning chat stream for session %d", planning_session_id)
    async for event in _bridge_sync_generator(
        session_factory=session_factory,
        generator_factory=lambda db: stream_planning_message(
            db, planning_session_id, actor_user_id=actor_user_id, content=content,
            context_injector=inject_auto_context,
            auto_draft_generator=generate_auto_drafts_for_scenarios,
            event_log_factory=EventLogWriter,
            session_factory=session_factory,
        ),
        phase="chat",
    ):
        yield event


async def stream_planning_drafts(
    *,
    session_factory: sessionmaker,
    planning_session_id: int,
    payload: object,
    actor_user_id: int,
) -> AsyncGenerator[dict, None]:
    """Bridge streaming draft generation to async WebSocket events."""
    from app.application.planning.draft_service import stream_generate_planning_drafts
    from app.schemas.ai_planning import GenerateAIPlanningDraftsRequest

    if not isinstance(payload, GenerateAIPlanningDraftsRequest):
        payload = GenerateAIPlanningDraftsRequest.model_validate(payload)

    logger.info("Starting planning drafts stream for session %d", planning_session_id)
    async for event in _bridge_sync_generator(
        session_factory=session_factory,
        generator_factory=lambda db: stream_generate_planning_drafts(
            db, planning_session_id, payload, actor_user_id=actor_user_id,
            session_factory=session_factory,
        ),
        phase="drafts",
    ):
        yield event


def _run_sync_save_and_execute(
    *,
    session_factory: sessionmaker,
    planning_session_id: int,
    draft_ids: list[int],
    actor_user_id: int,
    input_values: dict[str, str],
    idempotency_key: str | None,
    concurrency_limit: int,
    cancel_event: Event,
    queue: asyncio.Queue,
    loop: asyncio.AbstractEventLoop,
) -> None:
    """Queue a batch and forward persisted execution progress to the async stream."""

    try:
        logger.info("Starting save-and-execute stream for session %d, drafts=%s", planning_session_id, draft_ids)
        with session_factory() as session:
            for event in _queue_and_stream_planning_execution(
                session,
                planning_session_id,
                draft_ids,
                actor_user_id,
                input_values=input_values,
                idempotency_key=idempotency_key,
                concurrency_limit=concurrency_limit,
                cancel_event=cancel_event,
            ):
                if cancel_event.is_set():
                    break
                loop.call_soon_threadsafe(queue.put_nowait, _serialize_event(event))
    except RunnerCancelledError:
        loop.call_soon_threadsafe(queue.put_nowait, {"type": "cancelled"})
    except Exception as exc:
        tb = _traceback.format_exc()
        logger.exception("Streaming worker error for planning session %s in execute phase", planning_session_id)
        loop.call_soon_threadsafe(queue.put_nowait, {
            "type": "error",
            "message": str(exc),
            "error_type": type(exc).__name__,
            "phase": "execute",
            "traceback": tb[:2000],
        })
    finally:
        loop.call_soon_threadsafe(queue.put_nowait, _TerminalSignal())


async def stream_save_and_execute(
    *,
    session_factory: sessionmaker,
    planning_session_id: int,
    draft_ids: list[int],
    actor_user_id: int,
    cancel_event: Event,
    input_values: dict[str, str] | None = None,
    idempotency_key: str | None = None,
    concurrency_limit: int = 1,
) -> AsyncGenerator[dict, None]:
    """Bridge the synchronous streaming execution to an async generator for WebSocket delivery."""
    loop = asyncio.get_running_loop()
    queue: asyncio.Queue[dict | _TerminalSignal] = asyncio.Queue()

    thread = Thread(
        target=_run_sync_save_and_execute,
        kwargs={
            "session_factory": session_factory,
            "planning_session_id": planning_session_id,
            "draft_ids": draft_ids,
            "actor_user_id": actor_user_id,
            "input_values": input_values or {},
            "idempotency_key": idempotency_key,
            "concurrency_limit": concurrency_limit,
            "cancel_event": cancel_event,
            "queue": queue,
            "loop": loop,
        },
        daemon=True,
    )
    thread.start()

    while True:
        item = await queue.get()
        if isinstance(item, _TerminalSignal):
            return
        yield _serialize_event(item)


def _queue_and_stream_planning_execution(
    session: Session,
    planning_session_id: int,
    draft_ids: list[int],
    actor_user_id: int,
    *,
    input_values: dict[str, str],
    idempotency_key: str | None,
    concurrency_limit: int,
    cancel_event: Event,
) -> Generator[dict, None, None]:
    from app.application.planning.project_context import get_owned_session
    from app.application.planning.save_execute_service import save_and_execute_selected_drafts
    from app.application.reporting import build_batch_detail
    from app.models import AIPlanningMessage, TestCase
    from app.schemas.ai_planning import ExecutionSummaryResult
    from app.schemas.execution_batches import ExecutionBatchCreateRequest
    from app.services.execution_batches import cancel_execution_batch, create_execution_batch

    save_result = save_and_execute_selected_drafts(
        session,
        planning_session_id,
        draft_ids,
        actor_user_id,
        execute=False,
        input_values=input_values,
    )
    saved_cases = save_result.saved_cases
    for index, saved in enumerate(saved_cases, 1):
        yield {
            "type": "save_progress",
            "saved_count": index,
            "total": len(saved_cases),
            "case_name": saved.case_name,
        }
    if not saved_cases:
        yield {"type": "error", "message": "没有可执行的测试用例。", "phase": "execute"}
        return

    first_case = session.get(TestCase, saved_cases[0].case_id)
    if first_case is None:
        yield {"type": "error", "message": "已保存用例不存在。", "phase": "execute"}
        return
    batch = create_execution_batch(
        session,
        ExecutionBatchCreateRequest(
            project_id=first_case.project_id,
            case_ids=[item.case_id for item in saved_cases],
            planning_session_id=planning_session_id,
            idempotency_key=idempotency_key,
            concurrency_limit=concurrency_limit,
            input_values=input_values,
        ),
        actor_user_id=actor_user_id,
    )
    planning_session = get_owned_session(session, planning_session_id, actor_user_id=actor_user_id)
    planning_session.status = "executing"
    session.commit()
    yield {
        "type": "status",
        "phase": "queued",
        "message": f"执行批次 #{batch.id} 已入队。",
        "batch_id": batch.id,
    }

    observed_statuses: dict[int, str] = {}
    while True:
        if cancel_event.is_set():
            cancel_execution_batch(session, batch.id)
        session.expire_all()
        detail = build_batch_detail(session, batch.id)
        for job in detail.jobs:
            previous = observed_statuses.get(job.id)
            if job.status == "running" and previous != "running":
                case_record = session.get(TestCase, job.case_id)
                total_steps = len((case_record.dsl or {}).get("steps", [])) if case_record else 0
                yield {
                    "type": "case_start",
                    "case_id": job.case_id,
                    "case_name": job.case_name,
                    "total_steps": total_steps,
                }
            if job.status in {"passed", "failed", "needs_intervention", "cancelled"} and previous not in {
                "passed", "failed", "needs_intervention", "cancelled",
            }:
                latest = job.latest_execution
                if latest and latest.report:
                    for step in latest.report.steps:
                        yield {
                            "type": "step_complete",
                            "case_id": job.case_id,
                            "step_index": step.step_index,
                            "action": step.action,
                            "status": step.status,
                            "duration_ms": step.duration_ms or 0,
                        }
            observed_statuses[job.id] = job.status
        if (
            detail.status in {"passed", "failed", "needs_intervention", "cancelled"}
            and detail.analysis_status in {"completed", "skipped", "failed"}
        ):
            break
        time.sleep(0.5)

    execution_summaries: list[ExecutionSummaryResult] = []
    for job in detail.jobs:
        latest = job.latest_execution
        if latest is None:
            continue
        passed_steps = sum(1 for step in (latest.report.steps if latest.report else []) if step.status == "passed")
        failed_steps = sum(1 for step in (latest.report.steps if latest.report else []) if step.status == "failed")
        execution_summaries.append(
            ExecutionSummaryResult(
                execution_id=latest.id,
                case_id=latest.case_id,
                case_name=latest.case_name,
                status=latest.status,
                total_steps=latest.total_steps,
                passed_steps=passed_steps,
                failed_steps=failed_steps,
                duration_ms=latest.duration_ms,
                screenshot_url=latest.latest_screenshot_url,
                report_url=f"/reports/{latest.id}",
            )
        )

    lines = [f"测试执行完成（批次 #{batch.id}）："]
    for summary in execution_summaries:
        icon = "✅" if summary.status == "passed" else "❌"
        lines.append(
            f"{icon} {summary.case_name} — {summary.status} "
            f"({summary.passed_steps}/{summary.total_steps}步)"
        )
    if detail.analysis:
        lines.extend(["", f"分析总结：{detail.analysis.summary}"])
    assistant_message = "\n".join(lines)
    planning_session.status = "completed"
    structured_payload = {
        "type": "execution_summary",
        "batch_id": batch.id,
        "saved_cases": [item.model_dump(mode="json") for item in saved_cases],
        "execution_summaries": [item.model_dump(mode="json") for item in execution_summaries],
        "analysis_status": detail.analysis_status,
        "analysis": detail.analysis.model_dump(mode="json") if detail.analysis else None,
    }
    session.add(
        AIPlanningMessage(
            session_id=planning_session.id,
            role="assistant",
            turn_type="plan",
            content=assistant_message,
            structured_payload_json=structured_payload,
        )
    )
    session.commit()
    yield {
        "type": "execution_summary",
        "message": assistant_message,
        "structured_payload": structured_payload,
    }

    if detail.analysis:
        yield {
            "type": "analysis_complete",
            "batch_id": batch.id,
            "analysis": detail.analysis.model_dump(mode="json"),
            "message": detail.analysis.summary,
        }
    yield {"type": "done"}
