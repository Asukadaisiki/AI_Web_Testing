"""Focused Compose acceptance for concurrent submissions and failed-Run reruns."""

from __future__ import annotations

import concurrent.futures
import hashlib
import json
import os
import subprocess
import sys
import time
import urllib.error
import urllib.request
import uuid
from collections.abc import Iterable
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path
from typing import Any

from canonical_scenario import GOAL
from wait_for_stack import wait_for_stack


ROOT = Path(__file__).resolve().parents[2]
API_BASE_URL = os.environ.get("AGENTTRACE_API_URL", "http://127.0.0.1:8080").rstrip("/")
POSTGRES_DSN = os.environ.get(
    "AGENTTRACE_POSTGRES_DSN",
    "postgres://agenttrace:agenttrace@127.0.0.1:5433/agenttrace?sslmode=disable",
)
COMMAND_TOPIC = "agenttrace.run.commands.v1"
TERMINAL = {"succeeded", "failed", "cancelled"}
EVIDENCE_TABLES = (
    "spans",
    "usage_records",
    "operation_results",
    "run_events",
    "artifacts",
)


@dataclass(frozen=True)
class ConversationFixture:
    project_id: str
    conversation_id: str


@dataclass(frozen=True)
class RunFixture:
    project_id: str
    conversation_id: str
    run_id: str
    message_id: str


@dataclass(frozen=True)
class EvidenceSnapshot:
    ids: frozenset[str]
    count: int
    digest: str


def request(
    path: str,
    *,
    method: str = "GET",
    body: dict[str, Any] | None = None,
    expected: int = 200,
    timeout: float = 30,
) -> dict[str, Any]:
    payload = None if body is None else json.dumps(body).encode("utf-8")
    http_request = urllib.request.Request(
        f"{API_BASE_URL}{path}",
        data=payload,
        method=method,
        headers={"accept": "application/json", "content-type": "application/json"},
    )
    try:
        with urllib.request.urlopen(http_request, timeout=timeout) as response:
            raw = response.read()
            status = response.status
    except urllib.error.HTTPError as error:
        raw = error.read()
        status = error.code
    result = json.loads(raw) if raw else {}
    if status != expected:
        raise AssertionError(
            f"{method} {path} returned HTTP {status}, expected {expected}: {result}"
        )
    return result


def compose(*args: str) -> None:
    command = ["docker", "compose", *args]
    completed = subprocess.run(
        command,
        cwd=ROOT,
        text=True,
        capture_output=True,
        check=False,
        timeout=60,
    )
    if completed.returncode != 0:
        output = (completed.stdout + completed.stderr)[-4000:]
        raise AssertionError(
            f"Compose command failed ({completed.returncode}): {' '.join(command)}; "
            f"output={output}"
        )


def create_project(index: int, batch: str) -> str:
    project = request(
        "/api/v1/projects",
        method="POST",
        body={
            "name": f"Concurrency rerun project {index} {batch}",
            "metadata": {"scenario": "concurrency-rerun", "batch": batch},
        },
        expected=201,
    )
    return str(project["id"])


def create_conversation(
    project_id: str, index: int, batch: str
) -> ConversationFixture:
    conversation = request(
        f"/api/v1/projects/{project_id}/conversations",
        method="POST",
        body={
            "title": f"Concurrent rerun conversation {index}",
            "metadata": {"scenario": "concurrency-rerun", "batch": batch},
        },
        expected=201,
    )
    return ConversationFixture(project_id, str(conversation["id"]))


def create_run(
    conversation: ConversationFixture,
    index: int,
    *,
    agent: dict[str, Any],
    batch: str,
) -> RunFixture:
    accepted = request(
        f"/api/v1/conversations/{conversation.conversation_id}/messages",
        method="POST",
        body={
            "content": GOAL,
            "agent_id": str(agent["id"]),
            "model": {
                "provider": str(agent["model"]["provider"]),
                "name": str(agent["model"]["name"]),
                "reasoning_effort": str(agent["model"].get("reasoning_effort", "")),
            },
            "idempotency_key": (
                f"concurrency-rerun-{batch}-{conversation.conversation_id}-{index}"
            ),
        },
        expected=202,
    )
    return RunFixture(
        project_id=conversation.project_id,
        conversation_id=conversation.conversation_id,
        run_id=str(accepted["run_id"]),
        message_id=str(accepted["message_id"]),
    )


def wait_for_terminal(run_id: str, *, timeout: float = 180) -> str:
    deadline = time.monotonic() + timeout
    latest = ""
    while time.monotonic() < deadline:
        latest = str(request(f"/api/v1/runs/{run_id}")["status"])
        if latest in TERMINAL:
            return latest
        time.sleep(0.25)
    raise AssertionError(f"run {run_id} did not become terminal; latest={latest!r}")


def wait_for_runs(run_ids: list[str], *, timeout: float = 180) -> dict[str, str]:
    deadline = time.monotonic() + timeout
    pending = set(run_ids)
    statuses: dict[str, str] = {}
    while pending and time.monotonic() < deadline:
        with concurrent.futures.ThreadPoolExecutor(
            max_workers=min(8, len(pending))
        ) as executor:
            responses = executor.map(
                lambda run_id: request(f"/api/v1/runs/{run_id}"), sorted(pending)
            )
            for run_id, response in zip(sorted(pending), responses):
                status = str(response["status"])
                statuses[run_id] = status
                if status in TERMINAL:
                    pending.remove(run_id)
        if pending:
            time.sleep(0.25)
    if pending:
        raise AssertionError(f"runs did not become terminal: {sorted(pending)}")
    return statuses


def delivery_counts(run_ids: list[str]) -> tuple[int, int]:
    import psycopg

    with psycopg.connect(POSTGRES_DSN) as connection, connection.cursor() as cursor:
        cursor.execute(
            """
            SELECT
              (SELECT count(*) FROM outbox_events
               WHERE key = ANY(%s) AND published_at IS NULL),
              (SELECT count(*) FROM outbox_events AS command
               WHERE command.key = ANY(%s)
                 AND command.topic = %s
                 AND NOT EXISTS (
                   SELECT 1 FROM inbox_events AS inbox
                   WHERE inbox.consumer = 'agenttrace-runtime'
                     AND inbox.event_id = command.event_id
                     AND inbox.processed_at IS NOT NULL
                 ))
            """,
            (run_ids, run_ids, COMMAND_TOPIC),
        )
        return tuple(map(int, cursor.fetchone()))


def wait_for_delivery(run_ids: list[str], *, timeout: float = 60) -> tuple[int, int]:
    deadline = time.monotonic() + timeout
    counts = (0, 0)
    while time.monotonic() < deadline:
        counts = delivery_counts(run_ids)
        if counts == (0, 0):
            return counts
        time.sleep(0.25)
    raise AssertionError(
        f"delivery did not converge for runs {run_ids}: "
        f"pending_outbox={counts[0]}, unprocessed_commands={counts[1]}"
    )


def snapshot_evidence(run_id: str) -> dict[str, EvidenceSnapshot]:
    import psycopg

    snapshots: dict[str, EvidenceSnapshot] = {}
    with psycopg.connect(POSTGRES_DSN) as connection, connection.cursor() as cursor:
        for table in EVIDENCE_TABLES:
            cursor.execute(
                f"SELECT id, to_jsonb(record)::text FROM {table} AS record "
                "WHERE run_id = %s ORDER BY id",
                (run_id,),
            )
            rows = [(str(row_id), str(payload)) for row_id, payload in cursor.fetchall()]
            digest = hashlib.sha256(
                "\n".join(f"{row_id}:{payload}" for row_id, payload in rows).encode(
                    "utf-8"
                )
            ).hexdigest()
            snapshots[table] = EvidenceSnapshot(
                ids=frozenset(row_id for row_id, _payload in rows),
                count=len(rows),
                digest=digest,
            )
    return snapshots


def evidence_unchanged(
    before: dict[str, EvidenceSnapshot], after: dict[str, EvidenceSnapshot]
) -> bool:
    return before == after


def evidence_ids_disjoint(
    source: dict[str, EvidenceSnapshot], rerun: dict[str, EvidenceSnapshot]
) -> bool:
    return any(rerun[table].count > 0 for table in EVIDENCE_TABLES) and all(
        source[table].ids.isdisjoint(rerun[table].ids) for table in EVIDENCE_TABLES
    )


def peak_concurrency(
    intervals: Iterable[tuple[datetime | None, datetime | None]],
) -> int:
    """Return the maximum active interval count for half-open [start, finish) work."""
    events: list[tuple[datetime, int]] = []
    for started_at, finished_at in intervals:
        if started_at is None or finished_at is None or finished_at <= started_at:
            continue
        events.extend(((started_at, 1), (finished_at, -1)))

    active = 0
    peak = 0
    for _moment, delta in sorted(events):
        active += delta
        peak = max(peak, active)
    return peak


def collect_concurrent_summary(run_ids: list[str]) -> dict[str, int]:
    import psycopg

    with psycopg.connect(POSTGRES_DSN) as connection, connection.cursor() as cursor:
        cursor.execute(
            """
            SELECT
              count(*) FILTER (WHERE status IN ('succeeded', 'failed', 'cancelled')),
              count(DISTINCT project_id),
              count(DISTINCT conversation_id),
              (SELECT count(*) FROM runs AS run
               JOIN conversations AS conversation ON conversation.id = run.conversation_id
               WHERE run.id = ANY(%s) AND run.project_id <> conversation.project_id),
              (SELECT count(*) FROM messages AS message
               JOIN runs AS run ON run.source_message_id = message.id
               WHERE run.id = ANY(%s)
                 AND message.conversation_id <> run.conversation_id),
              (SELECT count(*) FROM (
                 SELECT conversation_id, ordinal
                 FROM messages
                 WHERE conversation_id IN (
                   SELECT conversation_id FROM runs WHERE id = ANY(%s)
                 )
                 GROUP BY conversation_id, ordinal
                 HAVING count(*) > 1
               ) AS duplicates)
            FROM runs
            WHERE id = ANY(%s)
            """,
            (run_ids, run_ids, run_ids, run_ids),
        )
        (
            terminal_runs,
            projects,
            conversations,
            cross_project_runs,
            cross_conversation_messages,
            duplicate_message_ordinals,
        ) = map(int, cursor.fetchone())
        cursor.execute(
            """
            SELECT started_at, finished_at
            FROM runs
            WHERE id = ANY(%s)
            """,
            (run_ids,),
        )
        peak = peak_concurrency(cursor.fetchall())
    pending_outbox, unprocessed_commands = delivery_counts(run_ids)
    return {
        "terminal_runs": terminal_runs,
        "projects": projects,
        "conversations": conversations,
        "cross_project_runs": cross_project_runs,
        "cross_conversation_messages": cross_conversation_messages,
        "duplicate_message_ordinals": duplicate_message_ordinals,
        "peak_concurrency": peak,
        "pending_outbox": pending_outbox,
        "unprocessed_commands": unprocessed_commands,
    }


def validate_summary(summary: dict[str, Any]) -> None:
    exact = {
        "requested_runs": 8,
        "terminal_runs": 8,
        "projects": 2,
        "conversations": 4,
        "cross_project_runs": 0,
        "cross_conversation_messages": 0,
        "duplicate_message_ordinals": 0,
        "minimum_peak_concurrency": 4,
        "pending_outbox": 0,
        "unprocessed_commands": 0,
        "source_status": "failed",
        "rerun_status": "succeeded",
        "rerun_count": 1,
        "user_message_count": 1,
        "lineage_valid": True,
        "source_evidence_unchanged": True,
        "rerun_evidence_disjoint": True,
        "same_key_rerun_idempotent": True,
    }
    failures = [
        f"{name}={summary.get(name)!r}, expected={expected!r}"
        for name, expected in exact.items()
        if summary.get(name) != expected
    ]
    if int(summary.get("peak_concurrency", 0)) < int(
        summary.get("minimum_peak_concurrency", 0)
    ):
        failures.append(
            "peak_concurrency="
            f"{summary.get('peak_concurrency')!r}, expected>="
            f"{summary.get('minimum_peak_concurrency')!r}"
        )
    if failures:
        raise AssertionError("concurrency rerun validation failed: " + "; ".join(failures))


def run_scenario() -> dict[str, Any]:
    wait_for_stack(API_BASE_URL)
    agents = request("/api/v1/agents").get("agents", [])
    if len(agents) != 1 or agents[0].get("model", {}).get("provider") != "scripted":
        raise AssertionError(
            f"concurrency rerun requires exactly one scripted agent, got {agents}"
        )
    agent = agents[0]
    batch = uuid.uuid4().hex[:12]
    project_ids = [create_project(index, batch) for index in range(2)]
    conversation_work = [
        (project_id, index)
        for project_id in project_ids
        for index in range(2)
    ]
    with concurrent.futures.ThreadPoolExecutor(max_workers=4) as executor:
        conversations = list(
            executor.map(
                lambda item: create_conversation(item[0], item[1], batch),
                conversation_work,
            )
        )
    run_work = [
        (conversation, index) for conversation in conversations for index in range(2)
    ]
    with concurrent.futures.ThreadPoolExecutor(max_workers=8) as executor:
        runs = list(
            executor.map(
                lambda item: create_run(item[0], item[1], agent=agent, batch=batch),
                run_work,
            )
        )
    statuses = wait_for_runs([run.run_id for run in runs])
    failures = {run_id: status for run_id, status in statuses.items() if status != "succeeded"}
    if failures:
        raise AssertionError(f"concurrent submissions did not succeed: {failures}")
    wait_for_delivery([run.run_id for run in runs])
    summary: dict[str, Any] = {
        "requested_runs": len(runs),
        "minimum_peak_concurrency": 4,
        **collect_concurrent_summary([run.run_id for run in runs]),
    }

    rerun_project = create_project(99, batch)
    failed_conversation = create_conversation(rerun_project, 99, batch)
    source: RunFixture | None = None
    worker_stopped = False
    try:
        compose("stop", "worker")
        worker_stopped = True
        source = create_run(failed_conversation, 99, agent=agent, batch=batch)
        source_status = wait_for_terminal(source.run_id)
        if source_status != "failed":
            raise AssertionError(
                f"worker-stopped source run did not fail durably: {source_status!r}"
            )
        wait_for_delivery([source.run_id])
        source_before = snapshot_evidence(source.run_id)
    finally:
        if worker_stopped:
            compose("start", "worker")
            wait_for_stack(API_BASE_URL)

    if source is None:
        raise AssertionError("worker-stopped source run was not created")
    rerun_key = f"concurrency-rerun-retry-{uuid.uuid4()}"
    with concurrent.futures.ThreadPoolExecutor(max_workers=2) as executor:
        rerun_responses = list(
            executor.map(
                lambda _index: request(
                    f"/api/v1/runs/{source.run_id}/reruns",
                    method="POST",
                    body={"idempotency_key": rerun_key},
                    expected=202,
                ),
                range(2),
            )
        )
    rerun_ids = {str(response.get("run_id", "")) for response in rerun_responses}
    same_key_rerun_idempotent = len(rerun_ids) == 1 and "" not in rerun_ids
    if not same_key_rerun_idempotent:
        raise AssertionError(f"same-key rerun responses disagree: {rerun_responses}")
    rerun_id = next(iter(rerun_ids))
    rerun_status = wait_for_terminal(rerun_id)
    if rerun_status != "succeeded":
        raise AssertionError(f"rerun did not succeed: {rerun_status!r}")
    wait_for_delivery([source.run_id, rerun_id])
    source_after = snapshot_evidence(source.run_id)
    rerun_evidence = snapshot_evidence(rerun_id)
    source_run = request(f"/api/v1/runs/{source.run_id}")
    rerun_run = request(f"/api/v1/runs/{rerun_id}")
    conversation = request(f"/api/v1/conversations/{source.conversation_id}")
    conversation_runs = conversation.get("runs", [])
    source_message_id = str(source_run.get("source_message_id", ""))
    lineage_valid = (
        source_message_id == source.message_id
        and source_run.get("status") == "failed"
        and rerun_run.get("status") == "succeeded"
        and rerun_run.get("retry_of_run_id") == source.run_id
        and rerun_run.get("source_message_id") == source_message_id
        and all(response.get("retry_of_run_id") == source.run_id for response in rerun_responses)
        and {str(run.get("id", "")) for run in conversation_runs}
        == {source.run_id, rerun_id}
    )
    pending_outbox, unprocessed_commands = delivery_counts(
        [*(run.run_id for run in runs), source.run_id, rerun_id]
    )
    summary.update(
        {
            "source_status": str(source_run.get("status", "")),
            "rerun_status": str(rerun_run.get("status", "")),
            "rerun_count": sum(
                str(run.get("retry_of_run_id", "")) == source.run_id
                for run in conversation_runs
            ),
            "user_message_count": sum(
                message.get("role") == "user"
                for message in conversation.get("messages", [])
            ),
            "lineage_valid": lineage_valid,
            "source_evidence_unchanged": evidence_unchanged(
                source_before, source_after
            ),
            "rerun_evidence_disjoint": evidence_ids_disjoint(
                source_after, rerun_evidence
            ),
            "same_key_rerun_idempotent": same_key_rerun_idempotent,
            "pending_outbox": pending_outbox,
            "unprocessed_commands": unprocessed_commands,
        }
    )
    validate_summary(summary)
    return summary


def main() -> int:
    try:
        summary = run_scenario()
    except Exception as error:
        print(f"concurrency rerun scenario failed: {error}", file=sys.stderr)
        return 1
    print("concurrency rerun scenario passed: " + json.dumps(summary, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
