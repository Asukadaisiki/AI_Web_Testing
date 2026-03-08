"""Suite to case relation model."""

from __future__ import annotations

from sqlalchemy import ForeignKey, Integer, UniqueConstraint
from sqlalchemy.orm import Mapped, mapped_column

from app.db.base import Base


class SuiteCase(Base):
    """Ordering relation between suites and cases."""

    __tablename__ = "suite_cases"
    __table_args__ = (UniqueConstraint("suite_id", "order_index"),)

    suite_id: Mapped[int] = mapped_column(
        Integer,
        ForeignKey("test_suites.id", ondelete="CASCADE"),
        primary_key=True,
    )
    case_id: Mapped[int] = mapped_column(
        Integer,
        ForeignKey("test_cases.id", ondelete="CASCADE"),
        primary_key=True,
    )
    order_index: Mapped[int] = mapped_column(Integer, nullable=False)
