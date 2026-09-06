"""Persisted execution result for a single test case run."""

from __future__ import annotations

from datetime import datetime
from typing import Any

from sqlalchemy import JSON, DateTime, ForeignKey, Integer, String, Text, func
from sqlalchemy.orm import Mapped, mapped_column

from app.db.base import Base


class TestCaseRun(Base):
    """Execution record for a stored test case."""

    __tablename__ = "test_case_runs"

    id: Mapped[int] = mapped_column(primary_key=True)
    case_id: Mapped[int] = mapped_column(
        Integer,
        ForeignKey("test_cases.id", ondelete="CASCADE"),
        index=True,
        nullable=False,
    )
    project_id: Mapped[int] = mapped_column(
        Integer,
        ForeignKey("projects.id", ondelete="RESTRICT"),
        index=True,
        nullable=False,
    )
    batch_id: Mapped[int | None] = mapped_column(
        Integer,
        ForeignKey("execution_batches.id", ondelete="SET NULL"),
        index=True,
        nullable=True,
    )
    job_id: Mapped[int | None] = mapped_column(
        Integer,
        ForeignKey("execution_jobs.id", ondelete="SET NULL"),
        index=True,
        nullable=True,
    )
    triggered_by: Mapped[int] = mapped_column(
        Integer,
        ForeignKey("users.id", ondelete="RESTRICT"),
        index=True,
        nullable=False,
    )
    status: Mapped[str] = mapped_column(String(20), index=True, nullable=False)
    attempt_number: Mapped[int] = mapped_column(Integer, nullable=False, default=1)
    dsl_snapshot: Mapped[dict[str, Any] | None] = mapped_column(JSON, nullable=True)
    dsl_sha256: Mapped[str | None] = mapped_column(String(64), index=True, nullable=True)
    report_schema_version: Mapped[str] = mapped_column(String(32), nullable=False, default="execution.report.v2")
    error_message: Mapped[str | None] = mapped_column(Text, nullable=True)
    report: Mapped[dict[str, Any] | None] = mapped_column(JSON, nullable=True)
    failure_signal_json: Mapped[dict[str, Any] | None] = mapped_column(JSON, nullable=True)
    analysis_status: Mapped[str] = mapped_column(
        String(20),
        nullable=False,
        default="pending",
        server_default="pending",
    )
    analysis_json: Mapped[dict[str, Any] | None] = mapped_column(JSON, nullable=True)
    started_at: Mapped[datetime] = mapped_column(
        DateTime(),
        server_default=func.now(),
        nullable=False,
    )
    finished_at: Mapped[datetime | None] = mapped_column(DateTime(), nullable=True)
