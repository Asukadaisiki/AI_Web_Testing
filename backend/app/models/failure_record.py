"""Individual failure point recorded during Explorer execution."""

from __future__ import annotations

from typing import Any

from sqlalchemy import JSON, ForeignKey, Integer, String, Text
from sqlalchemy.orm import Mapped, mapped_column

from app.db.base import Base


class FailureRecord(Base):
    """A single failure captured by the Explorer, with evidence for Judge analysis."""

    __tablename__ = "failure_records"

    id: Mapped[int] = mapped_column(primary_key=True)
    exploration_run_id: Mapped[int] = mapped_column(
        Integer,
        ForeignKey("exploration_runs.id", ondelete="CASCADE"),
        index=True,
        nullable=False,
    )
    step_index: Mapped[int] = mapped_column(Integer, nullable=False)
    action: Mapped[str] = mapped_column(String(50), nullable=False)
    target: Mapped[str | None] = mapped_column(String(500), nullable=True)
    value: Mapped[str | None] = mapped_column(String(500), nullable=True)
    error_message: Mapped[str] = mapped_column(Text, nullable=False)
    evidence_json: Mapped[dict[str, Any] | None] = mapped_column(JSON, nullable=True)
    classification: Mapped[str | None] = mapped_column(String(50), nullable=True)
    retry_count: Mapped[int] = mapped_column(Integer, nullable=False, default=0)
