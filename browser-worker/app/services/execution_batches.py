"""Persistent execution batch and job queue services."""

from __future__ import annotations

from datetime import UTC, datetime, timedelta
import logging
from threading import Event, Thread

from sqlalchemy import exists, func, or_, select
from sqlalchemy.orm import Session, sessionmaker

from app.models import ExecutionBatch, ExecutionJob
from app.schemas.executions import CaseExecutionRequest, StoredCaseExecutionDetail
from app.services.errors import EntityNotFoundError


TERMINAL_JOB_STATUSES = {"passed", "failed", "needs_intervention", "cancelled"}
logger = logging.getLogger(__name__)


def get_execution_batch(session: Session, batch_id: int) -> ExecutionBatch:
    batch = session.get(ExecutionBatch, batch_id)
    if batch is None:
        raise EntityNotFoundError(f"Execution batch {batch_id} not found.")
    return batch


def claim_next_execution_job(
    session: Session,
    *,
    worker_id: str,
    lease_seconds: int = 1800,
) -> ExecutionJob | None:
    """Atomically claim one job while respecting its batch concurrency limit."""
    now = datetime.now(UTC).replace(tzinfo=None)
    expired = or_(
        ExecutionJob.status == "pending",
        (ExecutionJob.status == "running") & (ExecutionJob.lease_expires_at < now),
    )
    has_claimable_job = exists(
        select(ExecutionJob.id).where(
            ExecutionJob.batch_id == ExecutionBatch.id,
            ExecutionJob.cancel_requested.is_(False),
            ExecutionJob.attempt_count < ExecutionJob.max_attempts,
            expired,
        )
    )
    candidate_batch_ids = list(
        session.scalars(
            select(ExecutionBatch.id)
            .where(
                ExecutionBatch.status.in_(("pending", "running")),
                has_claimable_job,
            )
            .order_by(ExecutionBatch.created_at, ExecutionBatch.id)
        ).all()
    )

    for batch_id in candidate_batch_ids:
        batch = session.scalar(
            select(ExecutionBatch)
            .where(ExecutionBatch.id == batch_id)
            .with_for_update(skip_locked=True)
        )
        if batch is None:
            continue
        running_count = session.scalar(
            select(func.count(ExecutionJob.id)).where(
                ExecutionJob.batch_id == batch.id,
                ExecutionJob.status == "running",
                ExecutionJob.lease_expires_at >= now,
            )
        ) or 0
        if running_count >= batch.concurrency_limit:
            session.rollback()
            continue

        job = session.scalar(
            select(ExecutionJob)
            .where(
                ExecutionJob.batch_id == batch.id,
                ExecutionJob.cancel_requested.is_(False),
                ExecutionJob.attempt_count < ExecutionJob.max_attempts,
                expired,
            )
            .order_by(ExecutionJob.order_index, ExecutionJob.id)
            .with_for_update(skip_locked=True)
            .limit(1)
        )
        if job is None:
            session.rollback()
            continue

        job.status = "running"
        job.attempt_count += 1
        job.lease_owner = worker_id
        job.lease_expires_at = now + timedelta(seconds=lease_seconds)
        job.heartbeat_at = now
        job.started_at = job.started_at or now
        job.finished_at = None
        batch.status = "running"
        batch.started_at = batch.started_at or now
        session.commit()
        session.refresh(job)
        return job

    return None


def finish_execution_job(
    session: Session,
    job: ExecutionJob,
    *,
    status: str,
    error_message: str | None = None,
) -> None:
    if status not in TERMINAL_JOB_STATUSES:
        raise ValueError(f"Invalid terminal job status: {status}.")
    job.status = status
    job.last_error_message = error_message
    job.finished_at = datetime.now(UTC).replace(tzinfo=None)
    job.lease_owner = None
    job.lease_expires_at = None
    job.heartbeat_at = None
    session.flush()
    batch = session.scalar(
        select(ExecutionBatch)
        .where(ExecutionBatch.id == job.batch_id)
        .with_for_update()
    )
    if batch is None:
        raise EntityNotFoundError(f"Execution batch {job.batch_id} not found.")
    _refresh_batch_status(session, batch)
    session.commit()


def execute_claimed_job(
    session: Session,
    job_id: int,
    *,
    worker_id: str,
    session_factory: sessionmaker[Session] | None = None,
) -> StoredCaseExecutionDetail | None:
    """Execute a job already leased by this worker and persist its terminal state."""
    from app.runners.playwright_runner import RunnerCancelledError
    from app.services.executions import ExecutionRunContext, execute_case

    job = session.get(ExecutionJob, job_id)
    if job is None:
        raise EntityNotFoundError(f"Execution job {job_id} not found.")
    if job.status != "running" or job.lease_owner != worker_id:
        raise ValueError(f"Execution job {job_id} is not leased by worker {worker_id}.")
    batch = get_execution_batch(session, job.batch_id)

    if job.cancel_requested:
        finish_execution_job(session, job, status="cancelled")
        _analyze_batch_if_terminal(session, batch.id)
        return None

    cancel_event = Event()
    monitor_stop = Event()
    monitor = None
    if session_factory is not None:
        monitor = Thread(
            target=_monitor_job,
            kwargs={
                "session_factory": session_factory,
                "job_id": job.id,
                "worker_id": worker_id,
                "cancel_event": cancel_event,
                "stop_event": monitor_stop,
            },
            daemon=True,
        )
        monitor.start()

    try:
        result = execute_case(
            session,
            job.case_id,
            CaseExecutionRequest(
                actor_user_id=batch.triggered_by,
                input_values=batch.input_values_json,
            ),
            run_context=ExecutionRunContext(
                batch_id=batch.id,
                job_id=job.id,
                attempt_number=job.attempt_count,
            ),
            cancel_event=cancel_event,
        )
    except RunnerCancelledError as exc:
        session.rollback()
        persisted_job = session.get(ExecutionJob, job_id)
        if persisted_job is not None:
            finish_execution_job(
                session,
                persisted_job,
                status="cancelled",
                error_message=str(exc),
            )
            _analyze_batch_if_terminal(session, persisted_job.batch_id)
        return None
    except Exception as exc:
        session.rollback()
        persisted_job = session.get(ExecutionJob, job_id)
        if persisted_job is not None:
            finish_execution_job(
                session,
                persisted_job,
                status="failed",
                error_message=f"{type(exc).__name__}: {exc}",
            )
            _analyze_batch_if_terminal(session, persisted_job.batch_id)
        return None
    finally:
        monitor_stop.set()
        if monitor is not None:
            monitor.join(timeout=3)

    persisted_job = session.get(ExecutionJob, job_id)
    if persisted_job is None:
        raise EntityNotFoundError(f"Execution job {job_id} disappeared during execution.")
    terminal_status = (
        result.status
        if result.status in TERMINAL_JOB_STATUSES
        else "failed"
    )
    finish_execution_job(
        session,
        persisted_job,
        status=terminal_status,
        error_message=result.error_message,
    )
    _analyze_batch_if_terminal(session, persisted_job.batch_id)
    return result


def _monitor_job(
    *,
    session_factory: sessionmaker[Session],
    job_id: int,
    worker_id: str,
    cancel_event: Event,
    stop_event: Event,
    heartbeat_seconds: float = 2.0,
    lease_seconds: int = 1800,
) -> None:
    while not stop_event.wait(heartbeat_seconds):
        with session_factory() as monitor_session:
            job = monitor_session.get(ExecutionJob, job_id)
            if job is None or job.status != "running" or job.lease_owner != worker_id:
                cancel_event.set()
                return
            if job.cancel_requested:
                cancel_event.set()
                return
            now = datetime.now(UTC).replace(tzinfo=None)
            job.heartbeat_at = now
            job.lease_expires_at = now + timedelta(seconds=lease_seconds)
            monitor_session.commit()


def _analyze_batch_if_terminal(session: Session, batch_id: int) -> None:
    from app.application.reporting.analysis_service import analyze_batch

    try:
        analyze_batch(session, batch_id)
    except Exception:
        session.rollback()
        logger.exception("Failed to persist analysis for execution batch %s", batch_id)
        batch = session.get(ExecutionBatch, batch_id)
        if batch is not None:
            batch.analysis_status = "failed"
            session.commit()


def _refresh_batch_status(session: Session, batch: ExecutionBatch) -> None:
    statuses = list(
        session.scalars(
            select(ExecutionJob.status)
            .where(ExecutionJob.batch_id == batch.id)
            .order_by(ExecutionJob.order_index)
        ).all()
    )
    if not statuses:
        batch.status = "cancelled"
    elif any(status == "running" for status in statuses):
        batch.status = "running"
    elif any(status == "pending" for status in statuses):
        batch.status = "pending" if batch.started_at is None else "running"
    elif any(status == "needs_intervention" for status in statuses):
        batch.status = "needs_intervention"
    elif any(status == "failed" for status in statuses):
        batch.status = "failed"
    elif all(status == "cancelled" for status in statuses):
        batch.status = "cancelled"
    else:
        batch.status = "passed"

    if all(status in TERMINAL_JOB_STATUSES for status in statuses):
        batch.finished_at = datetime.now(UTC).replace(tzinfo=None)
