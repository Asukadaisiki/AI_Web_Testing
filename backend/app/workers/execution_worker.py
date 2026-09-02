"""Bounded worker pool for PostgreSQL-backed execution jobs."""

from __future__ import annotations

import argparse
import logging
import os
import signal
import socket
from concurrent.futures import ThreadPoolExecutor
from threading import Event

from app.core.logging_config import setup_logging
from app.db.session import get_session_factory, verify_database_connection
from app.services.execution_batches import claim_next_execution_job, execute_claimed_job


logger = logging.getLogger(__name__)


def run_worker_pool(*, concurrency: int, poll_seconds: float) -> None:
    """Run a fixed number of job consumers until SIGINT or SIGTERM."""
    stop_event = Event()

    def request_stop(_signum, _frame) -> None:
        stop_event.set()

    signal.signal(signal.SIGINT, request_stop)
    signal.signal(signal.SIGTERM, request_stop)

    worker_prefix = f"{socket.gethostname()}:{os.getpid()}"
    with ThreadPoolExecutor(max_workers=concurrency, thread_name_prefix="execution-worker") as pool:
        futures = [
            pool.submit(
                _worker_loop,
                worker_id=f"{worker_prefix}:{index}",
                poll_seconds=poll_seconds,
                stop_event=stop_event,
            )
            for index in range(concurrency)
        ]
        for future in futures:
            future.result()


def _worker_loop(*, worker_id: str, poll_seconds: float, stop_event: Event) -> None:
    session_factory = get_session_factory()
    logger.info("Execution worker started: %s", worker_id)
    while not stop_event.is_set():
        job_id: int | None = None
        try:
            with session_factory() as session:
                job = claim_next_execution_job(session, worker_id=worker_id)
                if job is not None:
                    job_id = job.id
                    execute_claimed_job(session, job.id, worker_id=worker_id)
        except Exception:
            logger.exception("Execution worker %s failed while processing job %s", worker_id, job_id)
        if job_id is None:
            stop_event.wait(poll_seconds)
    logger.info("Execution worker stopped: %s", worker_id)


def main() -> None:
    parser = argparse.ArgumentParser(description="Run queued web test execution jobs.")
    parser.add_argument("--concurrency", type=int, default=2)
    parser.add_argument("--poll-seconds", type=float, default=1.0)
    args = parser.parse_args()
    if not 1 <= args.concurrency <= 16:
        parser.error("--concurrency must be between 1 and 16")
    if args.poll_seconds <= 0:
        parser.error("--poll-seconds must be positive")

    setup_logging()
    verify_database_connection()
    run_worker_pool(concurrency=args.concurrency, poll_seconds=args.poll_seconds)


if __name__ == "__main__":
    main()
