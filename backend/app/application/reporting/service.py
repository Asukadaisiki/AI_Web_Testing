"""Report Core projections for runs, batches, and projects."""

from __future__ import annotations

from collections import Counter

from sqlalchemy import select
from sqlalchemy.orm import Session

from app.models import ExecutionBatch, ExecutionJob, TestCase, TestCaseRun
from app.schemas.execution_batches import (
    ExecutionBatchDetail,
    ExecutionBatchReport,
    ExecutionBatchSummary,
    ExecutionJobSummary,
)
from app.services.cases import EntityNotFoundError
from app.services.executions import get_case_execution


def build_batch_detail(session: Session, batch_id: int) -> ExecutionBatchDetail:
    batch = session.get(ExecutionBatch, batch_id)
    if batch is None:
        raise EntityNotFoundError(f"Execution batch {batch_id} not found.")
    jobs = _build_job_summaries(session, batch.id)
    return ExecutionBatchDetail(
        **_build_batch_summary(batch, jobs).model_dump(),
        jobs=jobs,
    )


def build_batch_report(session: Session, batch_id: int) -> ExecutionBatchReport:
    detail = build_batch_detail(session, batch_id)
    completed_jobs = (
        detail.passed_jobs
        + detail.failed_jobs
        + detail.intervention_jobs
        + detail.cancelled_jobs
    )
    decisive_jobs = detail.passed_jobs + detail.failed_jobs + detail.intervention_jobs
    return ExecutionBatchReport(
        **detail.model_dump(),
        completed_jobs=completed_jobs,
        pass_rate=round(detail.passed_jobs / decisive_jobs, 4) if decisive_jobs else 0,
    )


def build_project_batch_summaries(
    session: Session,
    project_id: int,
    *,
    limit: int = 50,
) -> list[ExecutionBatchSummary]:
    batches = list(
        session.scalars(
            select(ExecutionBatch)
            .where(ExecutionBatch.project_id == project_id)
            .order_by(ExecutionBatch.created_at.desc(), ExecutionBatch.id.desc())
            .limit(limit)
        ).all()
    )
    return [
        _build_batch_summary(batch, _build_job_summaries(session, batch.id))
        for batch in batches
    ]


def _build_job_summaries(session: Session, batch_id: int) -> list[ExecutionJobSummary]:
    rows = session.execute(
        select(ExecutionJob, TestCase.name)
        .join(TestCase, TestCase.id == ExecutionJob.case_id)
        .where(ExecutionJob.batch_id == batch_id)
        .order_by(ExecutionJob.order_index, ExecutionJob.id)
    ).all()
    summaries: list[ExecutionJobSummary] = []
    for job, case_name in rows:
        latest_run = session.scalar(
            select(TestCaseRun)
            .where(TestCaseRun.job_id == job.id)
            .order_by(TestCaseRun.attempt_number.desc(), TestCaseRun.id.desc())
            .limit(1)
        )
        latest_execution = (
            get_case_execution(session, latest_run.id)
            if latest_run is not None
            else None
        )
        summaries.append(
            ExecutionJobSummary(
                id=job.id,
                batch_id=job.batch_id,
                project_id=job.project_id,
                case_id=job.case_id,
                case_name=case_name,
                order_index=job.order_index,
                status=job.status,
                attempt_count=job.attempt_count,
                max_attempts=job.max_attempts,
                cancel_requested=job.cancel_requested,
                last_error_message=job.last_error_message,
                created_at=job.created_at,
                started_at=job.started_at,
                finished_at=job.finished_at,
                latest_execution=latest_execution,
            )
        )
    return summaries


def _build_batch_summary(
    batch: ExecutionBatch,
    jobs: list[ExecutionJobSummary],
) -> ExecutionBatchSummary:
    counts = Counter(job.status for job in jobs)
    return ExecutionBatchSummary(
        id=batch.id,
        project_id=batch.project_id,
        planning_session_id=batch.planning_session_id,
        triggered_by=batch.triggered_by,
        status=batch.status,
        idempotency_key=batch.idempotency_key,
        concurrency_limit=batch.concurrency_limit,
        total_jobs=len(jobs),
        pending_jobs=counts["pending"],
        running_jobs=counts["running"],
        passed_jobs=counts["passed"],
        failed_jobs=counts["failed"],
        intervention_jobs=counts["needs_intervention"],
        cancelled_jobs=counts["cancelled"],
        created_at=batch.created_at,
        started_at=batch.started_at,
        finished_at=batch.finished_at,
    )
