"""Persisted execution batch for a suite run."""

from __future__ import annotations

from datetime import datetime
from typing import Any

from sqlalchemy import JSON, DateTime, ForeignKey, Integer, String, func
from sqlalchemy.orm import Mapped, mapped_column

from app.db.base import Base


class SuiteRun(Base):
    """Aggregated execution record for one suite batch."""

    __tablename__ = "suite_runs"

    id: Mapped[int] = mapped_column(primary_key=True)
    suite_id: Mapped[int] = mapped_column(
        Integer,
        ForeignKey("test_suites.id", ondelete="CASCADE"),
        index=True,
        nullable=False,
    )
    triggered_by: Mapped[int] = mapped_column(
        Integer,
        ForeignKey("users.id", ondelete="RESTRICT"),
        index=True,
        nullable=False,
    )
    source: Mapped[str] = mapped_column(String(50), nullable=False)
    source_suite_run_id: Mapped[int | None] = mapped_column(
        Integer,
        ForeignKey("suite_runs.id", ondelete="SET NULL"),
        index=True,
        nullable=True,
    )
    context_source: Mapped[str] = mapped_column(String(50), nullable=False, default="empty")
    context_source_suite_run_id: Mapped[int | None] = mapped_column(
        Integer,
        ForeignKey("suite_runs.id", ondelete="SET NULL"),
        index=True,
        nullable=True,
    )
    rerun_context_mode: Mapped[str] = mapped_column(String(50), nullable=False, default="not_applicable")
    context_snapshot: Mapped[dict[str, Any]] = mapped_column(JSON, nullable=False, default=dict)
    status: Mapped[str] = mapped_column(String(20), index=True, nullable=False)
    total_cases: Mapped[int] = mapped_column(Integer, nullable=False, default=0)
    passed_cases: Mapped[int] = mapped_column(Integer, nullable=False, default=0)
    failed_cases: Mapped[int] = mapped_column(Integer, nullable=False, default=0)
    base_url_override: Mapped[str | None] = mapped_column(String(500), nullable=True)
    started_at: Mapped[datetime] = mapped_column(
        DateTime(),
        server_default=func.now(),
        nullable=False,
    )
    finished_at: Mapped[datetime | None] = mapped_column(DateTime(), nullable=True)
