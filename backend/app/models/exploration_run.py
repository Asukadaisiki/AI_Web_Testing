"""Explorer-Judge cycle tracking: one exploration run per case execution."""

from __future__ import annotations

from datetime import datetime
from typing import Any

from sqlalchemy import JSON, Boolean, DateTime, ForeignKey, Integer, String, func
from sqlalchemy.orm import Mapped, mapped_column

from app.db.base import Base


class ExplorationRun(Base):
    """Records a full Explorer-Judge cycle for a test case."""

    __tablename__ = "exploration_runs"

    id: Mapped[int] = mapped_column(primary_key=True)
    session_id: Mapped[int] = mapped_column(
        Integer,
        ForeignKey("ai_planning_sessions.id", ondelete="CASCADE"),
        index=True,
        nullable=False,
    )
    case_id: Mapped[int] = mapped_column(
        Integer,
        ForeignKey("test_cases.id", ondelete="CASCADE"),
        index=True,
        nullable=False,
    )
    case_run_id: Mapped[int | None] = mapped_column(
        Integer,
        ForeignKey("test_case_runs.id", ondelete="SET NULL"),
        nullable=True,
    )
    role: Mapped[str] = mapped_column(String(20), nullable=False, default="explorer")
    status: Mapped[str] = mapped_column(String(20), nullable=False, default="running")
    failure_records_json: Mapped[list[Any] | None] = mapped_column(JSON, nullable=True)
    judge_conclusions_json: Mapped[list[Any] | None] = mapped_column(JSON, nullable=True)
    router_decision_json: Mapped[dict[str, Any] | None] = mapped_column(JSON, nullable=True)
    auto_fix_attempted: Mapped[bool] = mapped_column(Boolean, nullable=False, default=False)
    created_at: Mapped[datetime] = mapped_column(
        DateTime(),
        server_default=func.now(),
        nullable=False,
    )
