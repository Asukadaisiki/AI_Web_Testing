"""Lightweight SSE event logger — persists every event during AI planning streams.

Events are buffered and flushed to the database in batches to avoid
per-event commit overhead.  The logger is designed to be used inside
the synchronous generator workers that produce SSE events.
"""

from __future__ import annotations

import logging
from typing import Any

from sqlalchemy import func, select
from sqlalchemy.orm import Session

from app.models.ai_planning_event_log import AIPlanningEventLog

logger = logging.getLogger(__name__)


class SSEEventLogger:
    """Persists SSE events to ``ai_planning_event_logs`` during streaming.

    Usage inside a sync generator::

        event_logger = SSEEventLogger(db, session_id, message_id)
        for event in stream:
            event_logger.log(event["type"], event)
            yield event
        event_logger.flush()   # flush remaining events

    The logger tracks its own per-session sequence number so callers
    don't need to manage it.
    """

    def __init__(
        self,
        session: Session,
        session_id: int,
        message_id: int | None = None,
        flush_interval: int = 5,
    ) -> None:
        self._session = session
        self._session_id = session_id
        self._message_id = message_id
        self._flush_interval = flush_interval
        self._pending_count = 0

        # Determine the next sequence number for this session.
        max_seq = session.scalar(
            select(func.max(AIPlanningEventLog.seq)).where(
                AIPlanningEventLog.session_id == session_id,
            )
        )
        self._next_seq: int = (max_seq or 0) + 1

    # ------------------------------------------------------------------
    # Public API
    # ------------------------------------------------------------------

    def log(self, event_type: str, event_data: dict[str, Any]) -> None:
        """Buffer one event.  Auto-flushes every *flush_interval* calls."""
        self._session.add(AIPlanningEventLog(
            session_id=self._session_id,
            message_id=self._message_id,
            event_type=event_type,
            event_data=event_data,
            seq=self._next_seq,
        ))
        self._next_seq += 1
        self._pending_count += 1

        if self._pending_count >= self._flush_interval:
            self.flush()

    def flush(self) -> None:
        """Commit all buffered events to the database."""
        if self._pending_count == 0:
            return
        try:
            self._session.commit()
            logger.debug(
                "Flushed %d SSE events for session %d (next_seq=%d)",
                self._pending_count, self._session_id, self._next_seq,
            )
        except Exception:
            logger.exception(
                "Failed to flush SSE events for session %d", self._session_id,
            )
            self._session.rollback()
        finally:
            self._pending_count = 0

    def update_message_id(self, message_id: int) -> None:
        """Update the message_id for subsequent events (e.g. after stub creation)."""
        self._message_id = message_id

    @property
    def next_seq(self) -> int:
        """The sequence number that will be assigned to the next event."""
        return self._next_seq
