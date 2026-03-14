"""Persisted human locator corrections."""

from __future__ import annotations

from datetime import datetime

from sqlalchemy import Boolean, DateTime, ForeignKey, Integer, String, Text, func
from sqlalchemy.orm import Mapped, mapped_column

from app.db.base import Base


class LocatorCorrection(Base):
    """Reusable manual correction for a page URL pattern and target description."""

    __tablename__ = "locator_corrections"

    id: Mapped[int] = mapped_column(primary_key=True)
    page_url_pattern: Mapped[str] = mapped_column(String(500), index=True, nullable=False)
    target_description: Mapped[str] = mapped_column(String(200), index=True, nullable=False)
    correction_type: Mapped[str] = mapped_column(String(20), nullable=False)
    correction_value: Mapped[str] = mapped_column(Text(), nullable=False)
    verified_count: Mapped[int] = mapped_column(Integer, nullable=False, default=0)
    consecutive_failures: Mapped[int] = mapped_column(Integer, nullable=False, default=0)
    is_active: Mapped[bool] = mapped_column(Boolean(), nullable=False, default=True)
    source_execution_id: Mapped[int] = mapped_column(
        Integer,
        ForeignKey("test_case_runs.id", ondelete="CASCADE"),
        index=True,
        nullable=False,
    )
    created_by: Mapped[int] = mapped_column(
        Integer,
        ForeignKey("users.id", ondelete="RESTRICT"),
        index=True,
        nullable=False,
    )
    created_at: Mapped[datetime] = mapped_column(
        DateTime(),
        server_default=func.now(),
        nullable=False,
    )
    updated_at: Mapped[datetime] = mapped_column(
        DateTime(),
        server_default=func.now(),
        onupdate=func.now(),
        nullable=False,
    )
