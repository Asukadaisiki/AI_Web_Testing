"""Worker-thread bridge for streaming AI planning operations over WebSocket."""

from __future__ import annotations

import asyncio
import json
import logging
from threading import Event, Thread
from typing import AsyncGenerator, Callable, Generator

from sqlalchemy.orm import Session, sessionmaker

from app.runners.playwright_runner import RunnerCancelledError

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


def _run_sync_generator(
    *,
    generator_factory: Callable[[Session], Generator[dict, None, object]],
    session_factory: sessionmaker,
    queue: asyncio.Queue,
    loop: asyncio.AbstractEventLoop,
    cancel_event: Event | None = None,
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
                loop.call_soon_threadsafe(queue.put_nowait, _serialize_event(event))
    except RunnerCancelledError:
        loop.call_soon_threadsafe(queue.put_nowait, {"type": "cancelled"})
    except Exception as exc:
        logger.exception("Planning stream worker error")
        loop.call_soon_threadsafe(queue.put_nowait, {"type": "error", "message": str(exc)})
    finally:
        loop.call_soon_threadsafe(queue.put_nowait, _TerminalSignal())


async def _bridge_sync_generator(
    *,
    session_factory: sessionmaker,
    generator_factory: Callable[[Session], Generator[dict, None, object]],
    cancel_event: Event | None = None,
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
    from app.services.ai_planning import stream_planning_message

    async for event in _bridge_sync_generator(
        session_factory=session_factory,
        generator_factory=lambda db: stream_planning_message(
            db, planning_session_id, actor_user_id=actor_user_id, content=content,
        ),
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
    from app.services.ai_planning import stream_generate_planning_drafts
    from app.schemas.ai_planning import GenerateAIPlanningDraftsRequest

    if not isinstance(payload, GenerateAIPlanningDraftsRequest):
        payload = GenerateAIPlanningDraftsRequest.model_validate(payload)

    async for event in _bridge_sync_generator(
        session_factory=session_factory,
        generator_factory=lambda db: stream_generate_planning_drafts(
            db, planning_session_id, payload, actor_user_id=actor_user_id,
        ),
    ):
        yield event


def _run_sync_save_and_execute(
    *,
    session_factory: sessionmaker,
    planning_session_id: int,
    draft_ids: list[int],
    actor_user_id: int,
    cancel_event: Event,
    queue: asyncio.Queue,
    loop: asyncio.AbstractEventLoop,
) -> None:
    """Run the synchronous save-and-execute in a worker thread, forwarding events to the async queue."""
    from app.services.ai_planning import save_and_execute_selected_drafts_streaming

    try:
        with session_factory() as session:
            for event in save_and_execute_selected_drafts_streaming(
                session,
                planning_session_id,
                draft_ids,
                actor_user_id,
                cancel_event=cancel_event,
            ):
                if cancel_event.is_set():
                    break
                loop.call_soon_threadsafe(queue.put_nowait, _serialize_event(event))
    except RunnerCancelledError:
        loop.call_soon_threadsafe(queue.put_nowait, {"type": "cancelled"})
    except Exception as exc:
        logger.exception("Streaming worker error for planning session %s", planning_session_id)
        loop.call_soon_threadsafe(queue.put_nowait, {"type": "error", "message": str(exc)})
    finally:
        loop.call_soon_threadsafe(queue.put_nowait, _TerminalSignal())


async def stream_save_and_execute(
    *,
    session_factory: sessionmaker,
    planning_session_id: int,
    draft_ids: list[int],
    actor_user_id: int,
    cancel_event: Event,
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
