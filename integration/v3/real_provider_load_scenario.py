"""Run a concurrent multi-project scenario through the real model provider."""

from __future__ import annotations

import concurrent.futures
import json
import math
import os
import subprocess
import sys
import time
import urllib.error
import urllib.request
import uuid
from dataclasses import asdict, dataclass
from pathlib import Path
from typing import Any


ROOT = Path(__file__).resolve().parents[2]
API_BASE_URL = os.environ.get("AGENTTRACE_API_URL", "http://127.0.0.1:8080").rstrip("/")
POSTGRES_DSN = os.environ.get(
    "AGENTTRACE_POSTGRES_DSN",
    "postgres://agenttrace:agenttrace@127.0.0.1:5433/agenttrace?sslmode=disable",
)
REPORT_PATH = Path(
    os.environ.get(
        "AGENTTRACE_LOAD_REPORT",
        str(ROOT / "artifacts/load-test/real-provider-load-report.json"),
    )
)
COMMAND_TOPIC = "agenttrace.run.commands.v1"
DLQ_TOPIC = "agenttrace.run.dlq.v1"
TERMINAL = {"succeeded", "failed", "cancelled"}


def positive_int_env(name: str, fallback: int) -> int:
    raw = os.environ.get(name, str(fallback)).strip()
    try:
        value = int(raw)
    except ValueError as error:
        raise ValueError(f"{name} must be an integer") from error
    if value < 1:
        raise ValueError(f"{name} must be at least 1")
    return value


@dataclass(frozen=True)
class LoadConfig:
    projects: int
    conversations_per_project: int
    runs_per_conversation: int
    submit_workers: int
    run_timeout_seconds: int
    delivery_timeout_seconds: int

    @property
    def total_runs(self) -> int:
        return (
            self.projects
            * self.conversations_per_project
            * self.runs_per_conversation
        )

    @property
    def total_conversations(self) -> int:
        return self.projects * self.conversations_per_project

    @property
    def minimum_peak_concurrency(self) -> int:
        return min(8, self.total_runs, self.submit_workers)

    @classmethod
    def from_env(cls) -> "LoadConfig":
        projects = positive_int_env("AGENTTRACE_LOAD_PROJECTS", 5)
        conversations = positive_int_env("AGENTTRACE_LOAD_CONVERSATIONS", 4)
        runs = positive_int_env("AGENTTRACE_LOAD_RUNS_PER_CONVERSATION", 2)
        total_runs = projects * conversations * runs
        return cls(
            projects=projects,
            conversations_per_project=conversations,
            runs_per_conversation=runs,
            submit_workers=positive_int_env(
                "AGENTTRACE_LOAD_SUBMIT_WORKERS", total_runs
            ),
            run_timeout_seconds=positive_int_env(
                "AGENTTRACE_LOAD_RUN_TIMEOUT_SECONDS", 900
            ),
            delivery_timeout_seconds=positive_int_env(
                "AGENTTRACE_LOAD_DELIVERY_TIMEOUT_SECONDS", 60
            ),
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


def percentile(values: list[float], quantile: float) -> float:
    if not values:
        return 0.0
    if quantile <= 0 or quantile > 1:
        raise ValueError("quantile must be greater than 0 and at most 1")
    ordered = sorted(values)
    rank = max(1, math.ceil(len(ordered) * quantile))
    return ordered[rank - 1]


def request(
    path: str,
    *,
    method: str = "GET",
    body: dict[str, Any] = None,
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


def load_case_arguments(session_id: str) -> dict[str, Any]:
    return {
        "session_id": session_id,
        "case": {
            "case_version": "loop.case.v1",
            "name": "Real-provider concurrent storefront check",
            "goal": "Open the local storefront and verify that Alpha is visible.",
            "base_url": "http://fixture-site",
            "steps": [
                {
                    "index": 0,
                    "action": "goto",
                    "intent": "Open the local storefront",
                    "value": "http://fixture-site/index.html",
                    "preconditions": [],
                    "postconditions": [
                        {
                            "type": "url_contains",
                            "value": "index.html",
                            "timeout_ms": 5000,
                        }
                    ],
                    "timeout_ms": 10000,
                },
                {
                    "index": 1,
                    "action": "assert_text",
                    "intent": "Verify the Alpha product is visible",
                    "value": "Alpha",
                    "preconditions": [
                        {
                            "type": "url_contains",
                            "value": "index.html",
                            "timeout_ms": 5000,
                        }
                    ],
                    "postconditions": [
                        {
                            "type": "text_visible",
                            "value": "Alpha",
                            "timeout_ms": 5000,
                        }
                    ],
                    "timeout_ms": 10000,
                },
            ],
        },
    }


def load_prompt(session_id: str) -> str:
    arguments = json.dumps(
        load_case_arguments(session_id), ensure_ascii=True, separators=(",", ":")
    )
    return (
        "Call browser_execute_case exactly once using the exact JSON arguments "
        "below. Do not modify, omit, or add fields. After the tool result, return "
        "one concise sentence that states whether the check passed.\n"
        f"{arguments}"
    )


def create_project(index: int, batch: str) -> str:
    project = request(
        "/api/v1/projects",
        method="POST",
        body={
            "name": f"Real-provider load project {index} {batch}",
            "metadata": {"scenario": "real-provider-load", "batch": batch},
        },
        expected=201,
    )
    return str(project["id"])


def create_conversation(
    item: tuple[str, int], batch: str
) -> ConversationFixture:
    project_id, index = item
    conversation = request(
        f"/api/v1/projects/{project_id}/conversations",
        method="POST",
        body={
            "title": f"Concurrent real-provider session {index}",
            "metadata": {"scenario": "real-provider-load", "batch": batch},
        },
        expected=201,
    )
    return ConversationFixture(project_id, str(conversation["id"]))


def create_run(
    item: tuple[ConversationFixture, int],
    *,
    agent_id: str,
    model: dict[str, str],
    batch: str,
) -> RunFixture:
    conversation, run_index = item
    session_id = f"load-{batch}-{uuid.uuid4().hex}"
    accepted = request(
        f"/api/v1/conversations/{conversation.conversation_id}/messages",
        method="POST",
        body={
            "content": load_prompt(session_id),
            "agent_id": agent_id,
            "model": model,
            "idempotency_key": (
                f"real-load-{batch}-{conversation.conversation_id}-{run_index}"
            ),
        },
        expected=202,
    )
    return RunFixture(
        project_id=conversation.project_id,
        conversation_id=conversation.conversation_id,
        run_id=str(accepted["run_id"]),
    )


def wait_for_runs(
    runs: list[RunFixture], *, timeout: float, workers: int
) -> dict[str, str]:
    deadline = time.monotonic() + timeout
    statuses = {run.run_id: "queued" for run in runs}
    pending = set(statuses)
    max_workers = min(max(1, workers), len(runs))
    while pending and time.monotonic() < deadline:
        run_ids = sorted(pending)
        with concurrent.futures.ThreadPoolExecutor(
            max_workers=max_workers
        ) as executor:
            results = executor.map(
                lambda run_id: request(f"/api/v1/runs/{run_id}"),
                run_ids,
            )
            for run_id, result in zip(run_ids, results):
                status = str(result["status"])
                statuses[run_id] = status
                if status in TERMINAL:
                    pending.remove(run_id)
        if pending:
            time.sleep(0.5)
    for run_id in pending:
        statuses[run_id] = "timed_out"
    return statuses


def redis_stream_count(run_ids: list[str]) -> int:
    completed = subprocess.run(
        [
            "docker",
            "compose",
            "exec",
            "-T",
            "redis",
            "redis-cli",
            "-n",
            "0",
            "--raw",
            "--scan",
            "--pattern",
            "run:*:live",
        ],
        cwd=ROOT,
        check=True,
        text=True,
        capture_output=True,
    )
    available = set(completed.stdout.splitlines())
    return sum(f"run:{run_id}:live" in available for run_id in run_ids)


def collect_summary(
    config: LoadConfig,
    runs: list[RunFixture],
    statuses: dict[str, str],
    *,
    batch: str,
    provider: str,
    model_name: str,
    wall_seconds: float,
) -> dict[str, Any]:
    import psycopg

    run_ids = [run.run_id for run in runs]
    with psycopg.connect(POSTGRES_DSN) as connection, connection.cursor() as cursor:
        cursor.execute(
            """
            SELECT
              count(*) FILTER (WHERE status = 'succeeded'),
              count(*) FILTER (WHERE status = 'failed'),
              count(*) FILTER (WHERE status = 'cancelled'),
              count(DISTINCT project_id),
              count(DISTINCT conversation_id)
            FROM runs WHERE id = ANY(%s)
            """,
            (run_ids,),
        )
        succeeded, failed, cancelled, projects, conversations = map(
            int, cursor.fetchone()
        )
        cursor.execute(
            """
            SELECT
              (SELECT count(*) FROM runs AS run
               JOIN conversations AS conversation ON conversation.id = run.conversation_id
               WHERE run.id = ANY(%s) AND run.project_id <> conversation.project_id),
              (SELECT count(*) FROM messages AS message
               JOIN runs AS run ON run.id = message.run_id
               WHERE run.id = ANY(%s)
                 AND message.conversation_id <> run.conversation_id),
              (SELECT count(*) FROM (
                 SELECT conversation_id, ordinal
                 FROM messages
                 WHERE conversation_id = ANY(%s)
                 GROUP BY conversation_id, ordinal
                 HAVING count(*) > 1
               ) AS duplicate)
            """,
            (
                run_ids,
                run_ids,
                sorted({run.conversation_id for run in runs}),
            ),
        )
        cross_project, cross_conversation, duplicate_ordinals = map(
            int, cursor.fetchone()
        )
        cursor.execute(
            """
            SELECT
              (SELECT count(*) FROM operation_results
               WHERE run_id = ANY(%s) AND status = 'succeeded'),
              (SELECT count(DISTINCT run_id) FROM operation_results
               WHERE run_id = ANY(%s) AND status = 'succeeded'),
              (SELECT count(DISTINCT run_id) FROM operation_results
               WHERE run_id = ANY(%s)
                 AND jsonb_typeof(artifacts) = 'object'
                 AND artifacts ? 'screenshot_paths'
                 AND jsonb_array_length(artifacts -> 'screenshot_paths') > 0),
              (SELECT count(DISTINCT run_id) FROM spans WHERE run_id = ANY(%s)),
              (SELECT count(DISTINCT run_id) FROM usage_records WHERE run_id = ANY(%s)),
              (SELECT coalesce(sum(total_tokens), 0) FROM usage_records
               WHERE run_id = ANY(%s)),
              (SELECT count(DISTINCT run_id) FROM run_events WHERE run_id = ANY(%s)),
              (SELECT count(DISTINCT run_id) FROM artifacts WHERE run_id = ANY(%s))
            """,
            (run_ids, run_ids, run_ids, run_ids, run_ids, run_ids, run_ids, run_ids),
        )
        (
            operation_results,
            runs_with_operations,
            runs_with_screenshots,
            runs_with_spans,
            runs_with_usage,
            total_tokens,
            runs_with_events,
            runs_with_artifacts,
        ) = map(int, cursor.fetchone())
        cursor.execute(
            """
            SELECT
              (SELECT count(*) FROM outbox_events
               WHERE key = ANY(%s) AND published_at IS NULL),
              (SELECT count(*) FROM outbox_events
               WHERE key = ANY(%s) AND topic = %s),
              (SELECT count(*) FROM outbox_events AS command
               WHERE command.key = ANY(%s)
                 AND command.topic = %s
                 AND NOT EXISTS (
                   SELECT 1 FROM inbox_events AS inbox
                   WHERE inbox.consumer = 'agenttrace-runtime'
                     AND inbox.event_id = command.event_id
                     AND inbox.processed_at IS NOT NULL
                 )),
              (SELECT count(DISTINCT broker_partition) FROM outbox_events
               WHERE key = ANY(%s)
                 AND topic = %s
                 AND published_at IS NOT NULL)
            """,
            (
                run_ids,
                run_ids,
                DLQ_TOPIC,
                run_ids,
                COMMAND_TOPIC,
                run_ids,
                COMMAND_TOPIC,
            ),
        )
        pending_outbox, dlq_events, unprocessed_commands, kafka_partitions = map(
            int, cursor.fetchone()
        )
        cursor.execute(
            """
            WITH points AS (
              SELECT started_at AS moment, 1 AS delta
              FROM runs WHERE id = ANY(%s)
              UNION ALL
              SELECT finished_at AS moment, -1 AS delta
              FROM runs WHERE id = ANY(%s)
            ), concurrency AS (
              SELECT sum(delta) OVER (ORDER BY moment, delta DESC) AS active
              FROM points WHERE moment IS NOT NULL
            )
            SELECT coalesce(max(active), 0) FROM concurrency
            """,
            (run_ids, run_ids),
        )
        peak_concurrency = int(cursor.fetchone()[0])
        cursor.execute(
            """
            SELECT extract(epoch FROM (finished_at - started_at))
            FROM runs
            WHERE id = ANY(%s)
              AND started_at IS NOT NULL
              AND finished_at IS NOT NULL
            """,
            (run_ids,),
        )
        durations = [float(row[0]) for row in cursor.fetchall()]
        cursor.execute(
            """
            SELECT id, status, coalesce(error::text, '')
            FROM runs
            WHERE id = ANY(%s) AND status <> 'succeeded'
            ORDER BY id
            """,
            (run_ids,),
        )
        failures = [
            {"run_id": str(run_id), "status": str(status), "error": str(error)[:1000]}
            for run_id, status, error in cursor.fetchall()
        ]

    return {
        "scenario": "real-provider-load",
        "batch": batch,
        "provider": provider,
        "model": model_name,
        "requested_runs": config.total_runs,
        "expected_projects": config.projects,
        "expected_conversations": config.total_conversations,
        "minimum_peak_concurrency": config.minimum_peak_concurrency,
        "submitted_runs": len(runs),
        "succeeded_runs": succeeded,
        "failed_runs": failed,
        "cancelled_runs": cancelled,
        "timed_out_runs": sum(status == "timed_out" for status in statuses.values()),
        "projects": projects,
        "conversations": conversations,
        "operation_results": operation_results,
        "runs_with_operations": runs_with_operations,
        "runs_with_screenshots": runs_with_screenshots,
        "runs_with_spans": runs_with_spans,
        "runs_with_usage": runs_with_usage,
        "runs_with_events": runs_with_events,
        "runs_with_artifacts": runs_with_artifacts,
        "redis_streams": redis_stream_count(run_ids),
        "pending_outbox": pending_outbox,
        "dlq_events": dlq_events,
        "unprocessed_commands": unprocessed_commands,
        "cross_project_runs": cross_project,
        "cross_conversation_messages": cross_conversation,
        "duplicate_message_ordinals": duplicate_ordinals,
        "kafka_partitions": kafka_partitions,
        "peak_concurrency": peak_concurrency,
        "total_tokens": total_tokens,
        "wall_seconds": round(wall_seconds, 3),
        "run_duration_seconds": {
            "min": round(min(durations), 3) if durations else 0.0,
            "p50": round(percentile(durations, 0.50), 3),
            "p95": round(percentile(durations, 0.95), 3),
            "max": round(max(durations), 3) if durations else 0.0,
        },
        "failures": failures,
        "run_ids": run_ids,
        "config": asdict(config),
    }


def wait_for_delivery(
    config: LoadConfig,
    runs: list[RunFixture],
    statuses: dict[str, str],
    *,
    batch: str,
    provider: str,
    model_name: str,
    wall_seconds: float,
) -> dict[str, Any]:
    deadline = time.monotonic() + config.delivery_timeout_seconds
    summary: dict[str, Any] = {}
    while time.monotonic() < deadline:
        summary = collect_summary(
            config,
            runs,
            statuses,
            batch=batch,
            provider=provider,
            model_name=model_name,
            wall_seconds=wall_seconds,
        )
        if (
            summary["pending_outbox"] == 0
            and summary["unprocessed_commands"] == 0
        ):
            return summary
        time.sleep(0.5)
    return summary


def validate_summary(summary: dict[str, Any]) -> None:
    requested = int(summary["requested_runs"])
    expected_projects = int(summary.get("expected_projects", 5))
    expected_conversations = int(summary.get("expected_conversations", 20))
    minimum_peak = int(summary.get("minimum_peak_concurrency", min(8, requested)))
    failures = []
    exact = {
        "succeeded_runs": requested,
        "failed_runs": 0,
        "projects": expected_projects,
        "conversations": expected_conversations,
        "runs_with_screenshots": requested,
        "runs_with_spans": requested,
        "runs_with_usage": requested,
        "redis_streams": requested,
        "pending_outbox": 0,
        "dlq_events": 0,
        "unprocessed_commands": 0,
        "cross_project_runs": 0,
        "cross_conversation_messages": 0,
        "duplicate_message_ordinals": 0,
    }
    for name, expected in exact.items():
        actual = int(summary[name])
        if actual != expected:
            failures.append(f"{name}={actual}, expected={expected}")
    runs_with_operations = int(
        summary.get("runs_with_operations", summary["operation_results"])
    )
    if runs_with_operations != requested:
        failures.append(
            f"runs_with_operations={runs_with_operations}, expected={requested}"
        )
    minimum_partitions = min(2, requested)
    if int(summary["kafka_partitions"]) < minimum_partitions:
        failures.append(
            f"kafka_partitions={summary['kafka_partitions']}, "
            f"expected>={minimum_partitions}"
        )
    if int(summary["peak_concurrency"]) < minimum_peak:
        failures.append(
            f"peak_concurrency={summary['peak_concurrency']}, expected>={minimum_peak}"
        )
    if failures:
        raise AssertionError("full-chain validation failed: " + "; ".join(failures))


def write_report(summary: dict[str, Any]) -> None:
    REPORT_PATH.parent.mkdir(parents=True, exist_ok=True)
    REPORT_PATH.write_text(
        json.dumps(summary, indent=2, sort_keys=True) + "\n", encoding="utf-8"
    )


def run_scenario(config: LoadConfig) -> dict[str, Any]:
    health = request("/api/v1/health")
    if health.get("status") not in {"ok", "degraded"}:
        raise AssertionError(f"stack is not ready: {health}")
    agents = request("/api/v1/agents").get("agents", [])
    if len(agents) != 1:
        raise AssertionError(f"expected one configured agent: {agents}")
    agent = agents[0]
    provider = str(agent.get("model", {}).get("provider", ""))
    model_name = str(agent.get("model", {}).get("name", ""))
    reasoning_effort = str(agent.get("model", {}).get("reasoning_effort", ""))
    if provider != "openai-compatible":
        raise AssertionError(
            f"real-provider load requires openai-compatible, got {provider!r}"
        )

    batch = uuid.uuid4().hex[:12]
    started = time.monotonic()
    with concurrent.futures.ThreadPoolExecutor(
        max_workers=config.projects
    ) as executor:
        project_ids = list(
            executor.map(
                lambda index: create_project(index, batch),
                range(config.projects),
            )
        )
    conversation_work = [
        (project_id, index)
        for project_id in project_ids
        for index in range(config.conversations_per_project)
    ]
    with concurrent.futures.ThreadPoolExecutor(
        max_workers=len(conversation_work)
    ) as executor:
        conversations = list(
            executor.map(
                lambda item: create_conversation(item, batch),
                conversation_work,
            )
        )
    run_work = [
        (conversation, run_index)
        for conversation in conversations
        for run_index in range(config.runs_per_conversation)
    ]
    model = {"provider": provider, "name": model_name, "reasoning_effort": reasoning_effort}
    with concurrent.futures.ThreadPoolExecutor(
        max_workers=config.submit_workers
    ) as executor:
        runs = list(
            executor.map(
                lambda item: create_run(
                    item,
                    agent_id=str(agent["id"]),
                    model=model,
                    batch=batch,
                ),
                run_work,
            )
        )
    statuses = wait_for_runs(
        runs,
        timeout=config.run_timeout_seconds,
        workers=config.submit_workers,
    )
    wall_seconds = time.monotonic() - started
    summary = wait_for_delivery(
        config,
        runs,
        statuses,
        batch=batch,
        provider=provider,
        model_name=model_name,
        wall_seconds=wall_seconds,
    )
    write_report(summary)
    validate_summary(summary)
    return summary


def main() -> int:
    try:
        config = LoadConfig.from_env()
        summary = run_scenario(config)
    except Exception as error:
        print(
            f"real-provider load scenario failed: {error}; report={REPORT_PATH}",
            file=sys.stderr,
        )
        return 1
    print(
        "real-provider load scenario passed: "
        + json.dumps(
            {
                "report": str(REPORT_PATH),
                "runs": summary["requested_runs"],
                "peak_concurrency": summary["peak_concurrency"],
                "p95_seconds": summary["run_duration_seconds"]["p95"],
                "total_tokens": summary["total_tokens"],
            },
            sort_keys=True,
        )
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
