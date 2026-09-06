"""Structural mappings for versioned research persistence."""

from __future__ import annotations

from datetime import datetime
from typing import Any

from sqlalchemy import (
    BigInteger,
    Boolean,
    CheckConstraint,
    DateTime,
    ForeignKey,
    Index,
    Integer,
    JSON,
    String,
    Text,
    UniqueConstraint,
    func,
)
from sqlalchemy.orm import Mapped, mapped_column

from app.db.base import Base


class ResearchExperiment(Base):
    __tablename__ = "research_experiments"
    __table_args__ = (
        CheckConstraint(
            "status IN ('draft', 'active', 'completed', 'cancelled')",
            name="status",
        ),
        CheckConstraint("repetitions >= 1", name="repetitions_positive"),
        CheckConstraint(
            "length(code_sha256) = 64 AND lower(code_sha256) = code_sha256",
            name="code_sha256",
        ),
        CheckConstraint(
            "policy_version = 'research.policy.v1'",
            name="policy_version",
        ),
        Index("ix_research_experiments_project_created", "project_id", "created_at"),
    )

    id: Mapped[str] = mapped_column(String(64), primary_key=True)
    project_id: Mapped[int] = mapped_column(
        Integer,
        ForeignKey("projects.id", ondelete="RESTRICT"),
        index=True,
        nullable=False,
    )
    name: Mapped[str] = mapped_column(String(200), nullable=False)
    goal: Mapped[str] = mapped_column(Text, nullable=False)
    dataset_version: Mapped[str] = mapped_column(String(200), nullable=False)
    model_provider: Mapped[str] = mapped_column(String(200), nullable=False)
    model_name: Mapped[str] = mapped_column(String(200), nullable=False)
    model_version: Mapped[str] = mapped_column(String(200), nullable=False)
    prompt_version: Mapped[str] = mapped_column(String(200), nullable=False)
    browser_name: Mapped[str] = mapped_column(String(200), nullable=False)
    browser_version: Mapped[str] = mapped_column(String(200), nullable=False)
    viewport_json: Mapped[dict[str, Any]] = mapped_column(JSON, nullable=False)
    code_sha256: Mapped[str] = mapped_column(String(64), nullable=False)
    policy_version: Mapped[str] = mapped_column(String(200), nullable=False)
    observation_profile: Mapped[str] = mapped_column(String(200), nullable=False)
    dsl_profile: Mapped[str] = mapped_column(String(200), nullable=False)
    seed: Mapped[int] = mapped_column(BigInteger, nullable=False)
    variant: Mapped[str] = mapped_column(String(200), nullable=False)
    repetitions: Mapped[int] = mapped_column(Integer, nullable=False)
    status: Mapped[str] = mapped_column(String(32), index=True, nullable=False)
    config_json: Mapped[dict[str, Any]] = mapped_column(JSON, nullable=False)
    created_at: Mapped[datetime] = mapped_column(
        DateTime,
        server_default=func.now(),
        nullable=False,
    )
    updated_at: Mapped[datetime] = mapped_column(
        DateTime,
        server_default=func.now(),
        nullable=False,
    )


class ResearchRun(Base):
    __tablename__ = "research_runs"
    __table_args__ = (
        CheckConstraint(
            "status IN ('pending', 'running', 'completed', 'failed', 'cancelled')",
            name="status",
        ),
        CheckConstraint("repetition_index >= 0", name="repetition_index_non_negative"),
        CheckConstraint(
            "schema_version = 'research.persistence.v1' AND "
            "projector_version = 'research.projector.v1' AND "
            "metric_version = 'research.metrics.v1' AND "
            "policy_version = 'research.policy.v1'",
            name="versions",
        ),
        CheckConstraint(
            "(status = 'pending' AND started_at IS NULL AND finished_at IS NULL) OR "
            "(status = 'running' AND started_at IS NOT NULL AND finished_at IS NULL) OR "
            "(status IN ('completed', 'failed') AND "
            "started_at IS NOT NULL AND finished_at IS NOT NULL) OR "
            "(status = 'cancelled' AND finished_at IS NOT NULL)",
            name="status_timestamps",
        ),
        CheckConstraint(
            "started_at IS NULL OR finished_at IS NULL OR finished_at >= started_at",
            name="timestamp_order",
        ),
        CheckConstraint(
            "dsl_sha256 IS NULL OR "
            "(length(dsl_sha256) = 64 AND lower(dsl_sha256) = dsl_sha256)",
            name="dsl_sha256",
        ),
        CheckConstraint(
            "(generation_id IS NULL OR agent_run_id IS NOT NULL) AND "
            "(batch_id IS NULL OR generation_id IS NOT NULL) AND "
            "(execution_id IS NULL OR batch_id IS NOT NULL) AND "
            "(dsl_sha256 IS NULL OR generation_id IS NOT NULL)",
            name="link_prefix",
        ),
        UniqueConstraint(
            "experiment_id",
            "idempotency_key",
            name="uq_research_runs_experiment_idempotency",
        ),
        UniqueConstraint(
            "experiment_id",
            "repetition_index",
            "warmup",
            name="uq_research_runs_experiment_repetition_warmup",
        ),
        Index("ix_research_runs_experiment_created", "experiment_id", "created_at"),
    )

    id: Mapped[str] = mapped_column(String(64), primary_key=True)
    experiment_id: Mapped[str] = mapped_column(
        String(64),
        ForeignKey("research_experiments.id", ondelete="CASCADE"),
        index=True,
        nullable=False,
    )
    project_id: Mapped[int] = mapped_column(
        Integer,
        ForeignKey("projects.id", ondelete="RESTRICT"),
        index=True,
        nullable=False,
    )
    idempotency_key: Mapped[str] = mapped_column(String(200), nullable=False)
    repetition_index: Mapped[int] = mapped_column(Integer, nullable=False)
    warmup: Mapped[bool] = mapped_column(Boolean, nullable=False)
    status: Mapped[str] = mapped_column(String(32), index=True, nullable=False)
    schema_version: Mapped[str] = mapped_column(String(64), nullable=False)
    projector_version: Mapped[str] = mapped_column(String(64), nullable=False)
    metric_version: Mapped[str] = mapped_column(String(64), nullable=False)
    policy_version: Mapped[str] = mapped_column(String(64), nullable=False)
    agent_run_id: Mapped[str | None] = mapped_column(
        String(64),
        ForeignKey("agent_runs.id", ondelete="RESTRICT"),
        index=True,
        nullable=True,
    )
    generation_id: Mapped[int | None] = mapped_column(
        Integer,
        ForeignKey("dsl_generation_runs.id", ondelete="RESTRICT"),
        index=True,
        nullable=True,
    )
    batch_id: Mapped[int | None] = mapped_column(
        Integer,
        ForeignKey("execution_batches.id", ondelete="RESTRICT"),
        index=True,
        nullable=True,
    )
    execution_id: Mapped[int | None] = mapped_column(
        Integer,
        ForeignKey("test_case_runs.id", ondelete="RESTRICT"),
        index=True,
        nullable=True,
    )
    dsl_sha256: Mapped[str | None] = mapped_column(String(64), nullable=True)
    metrics_json: Mapped[dict[str, Any] | None] = mapped_column(JSON, nullable=True)
    started_at: Mapped[datetime | None] = mapped_column(DateTime, nullable=True)
    finished_at: Mapped[datetime | None] = mapped_column(DateTime, nullable=True)
    created_at: Mapped[datetime] = mapped_column(
        DateTime,
        server_default=func.now(),
        nullable=False,
    )
    updated_at: Mapped[datetime] = mapped_column(
        DateTime,
        server_default=func.now(),
        nullable=False,
    )


class ResearchTransition(Base):
    __tablename__ = "research_transitions"
    __table_args__ = (
        CheckConstraint("ordinal >= 0", name="ordinal_non_negative"),
        CheckConstraint(
            "length(content_sha256) = 64 AND "
            "lower(content_sha256) = content_sha256",
            name="content_sha256",
        ),
        CheckConstraint(
            "schema_version = 'research.persistence.v1'",
            name="schema_version",
        ),
        UniqueConstraint(
            "research_run_id",
            "ordinal",
            name="uq_research_transitions_run_ordinal",
        ),
        UniqueConstraint(
            "research_run_id",
            "append_key",
            name="uq_research_transitions_run_append_key",
        ),
        Index(
            "ix_research_transitions_run_created",
            "research_run_id",
            "created_at",
        ),
    )

    id: Mapped[int] = mapped_column(BigInteger, primary_key=True, autoincrement=True)
    research_run_id: Mapped[str] = mapped_column(
        String(64),
        ForeignKey("research_runs.id", ondelete="CASCADE"),
        index=True,
        nullable=False,
    )
    ordinal: Mapped[int] = mapped_column(BigInteger, nullable=False)
    append_key: Mapped[str] = mapped_column(String(200), nullable=False)
    content_sha256: Mapped[str] = mapped_column(String(64), nullable=False)
    schema_version: Mapped[str] = mapped_column(String(64), nullable=False)
    transition_json: Mapped[dict[str, Any]] = mapped_column(JSON, nullable=False)
    artifact_refs_json: Mapped[list[dict[str, Any]]] = mapped_column(JSON, nullable=False)
    created_at: Mapped[datetime] = mapped_column(
        DateTime,
        server_default=func.now(),
        nullable=False,
    )
