"""Persisted case snapshot within a suite run batch."""

from __future__ import annotations

from typing import Any

from sqlalchemy import JSON, ForeignKey, Integer, String, UniqueConstraint
from sqlalchemy.orm import Mapped, mapped_column

from app.db.base import Base


class SuiteRunItem(Base):
    """Single executed case inside a suite run."""

    __tablename__ = "suite_run_items"
    __table_args__ = (UniqueConstraint("suite_run_id", "order_index"),)

    id: Mapped[int] = mapped_column(primary_key=True)
    suite_run_id: Mapped[int] = mapped_column(
        Integer,
        ForeignKey("suite_runs.id", ondelete="CASCADE"),
        index=True,
        nullable=False,
    )
    case_id: Mapped[int] = mapped_column(
        Integer,
        ForeignKey("test_cases.id", ondelete="RESTRICT"),
        index=True,
        nullable=False,
    )
    case_name_snapshot: Mapped[str] = mapped_column(String(200), nullable=False)
    order_index: Mapped[int] = mapped_column(Integer, nullable=False)
    execution_id: Mapped[int] = mapped_column(
        Integer,
        ForeignKey("test_case_runs.id", ondelete="RESTRICT"),
        unique=True,
        nullable=False,
    )
    status: Mapped[str] = mapped_column(String(20), index=True, nullable=False)
    context_reads: Mapped[list[dict[str, Any]]] = mapped_column(JSON, nullable=False, default=list)
    context_writes: Mapped[list[dict[str, Any]]] = mapped_column(JSON, nullable=False, default=list)
    context_resolution_error: Mapped[str | None] = mapped_column(String(2000), nullable=True)
