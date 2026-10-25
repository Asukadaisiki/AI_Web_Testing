"""Exercise concurrent workspaces and idempotent deletion on a healthy v3 stack."""

from __future__ import annotations

import concurrent.futures
import json
import os
import subprocess
import sys
import time
import urllib.error
import urllib.request
import uuid
from dataclasses import dataclass
from pathlib import Path
from typing import Any

import psycopg


API_BASE_URL = os.environ.get("AGENTTRACE_API_URL", "http://127.0.0.1:8080").rstrip("/")
POSTGRES_DSN = os.environ.get(
    "AGENTTRACE_POSTGRES_DSN",
    "postgres://agenttrace:agenttrace@127.0.0.1:5433/agenttrace?sslmode=disable",
)
ROOT = Path(__file__).resolve().parents[2]
REDIS_CONTAINER = os.environ.get("AGENTTRACE_REDIS_CONTAINER", "")
REDIS_DB = os.environ.get("AGENTTRACE_REDIS_DB", "0")
KAFKA_CONTAINER = os.environ.get("AGENTTRACE_KAFKA_CONTAINER", "")
KAFKA_GROUP_ID = os.environ.get("AGENTTRACE_KAFKA_GROUP_ID", "agenttrace-runtime")
COMMAND_TOPIC = "agenttrace.run.commands.v1"
TERMINAL = {"succeeded", "failed", "cancelled"}


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
    content: str


def service_command(
    service: str,
    container: str,
    command: list[str],
    *,
    interactive: bool = False,
) -> list[str]:
    if container:
        return ["docker", "exec", *(["-i"] if interactive else []), container, *command]
    return [
        "docker",
        "compose",
        "-f",
        str(ROOT / "compose.yaml"),
        "exec",
        "-T",
        service,
        *command,
    ]


def request(
    path: str,
    *,
    method: str = "GET",
    body: dict[str, Any] | None = None,
    expected: int = 200,
) -> dict[str, Any]:
    payload = None if body is None else json.dumps(body).encode()
    http_request = urllib.request.Request(
        f"{API_BASE_URL}{path}",
        data=payload,
        method=method,
        headers={"accept": "application/json", "content-type": "application/json"},
    )
    try:
        with urllib.request.urlopen(http_request, timeout=20) as response:
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


def create_project(index: int, conversations: int) -> list[ConversationFixture]:
    suffix = uuid.uuid4().hex[:8]
    project = request(
        "/api/v1/projects",
        method="POST",
        body={"name": f"Concurrent full-stack {index} {suffix}", "metadata": {"batch": suffix}},
        expected=201,
    )

    def create_conversation(conversation_index: int) -> ConversationFixture:
        conversation = request(
            f"/api/v1/projects/{project['id']}/conversations",
            method="POST",
            body={
                "title": f"Parallel conversation {conversation_index}",
                "metadata": {"batch": suffix},
            },
            expected=201,
        )
        return ConversationFixture(project["id"], conversation["id"])

    with concurrent.futures.ThreadPoolExecutor(max_workers=conversations) as executor:
        return list(executor.map(create_conversation, range(conversations)))


def create_run(
    conversation: ConversationFixture,
    run_index: int,
    *,
    idempotency_key: str | None = None,
    content: str | None = None,
) -> RunFixture:
    content = content or (
        f"Parallel browser evaluation project={conversation.project_id} "
        f"conversation={conversation.conversation_id} run={run_index}"
    )
    accepted = request(
        f"/api/v1/conversations/{conversation.conversation_id}/messages",
        method="POST",
        body={
            "content": content,
            "agent_id": "browser-eval",
            "model": {"provider": "scripted", "name": "browser-eval-fixture"},
            "idempotency_key": idempotency_key or f"parallel-{uuid.uuid4()}",
        },
        expected=202,
    )
    return RunFixture(
        conversation.project_id,
        conversation.conversation_id,
        accepted["run_id"],
        accepted["message_id"],
        content,
    )


def wait_for_runs(runs: list[RunFixture], *, timeout: float = 180) -> None:
    deadline = time.monotonic() + timeout
    pending = {run.run_id for run in runs}
    statuses: dict[str, str] = {}
    while pending and time.monotonic() < deadline:
        for run_id in list(pending):
            status = str(request(f"/api/v1/runs/{run_id}")["status"])
            statuses[run_id] = status
            if status in TERMINAL:
                pending.remove(run_id)
        if pending:
            time.sleep(0.2)
    if pending:
        raise AssertionError(f"runs did not finish: {sorted(pending)}; statuses={statuses}")
    failed = {run_id: status for run_id, status in statuses.items() if status != "succeeded"}
    if failed:
        raise AssertionError(f"runs did not succeed: {failed}")


def wait_for_deletion_safety(run_ids: list[str], *, timeout: float = 30) -> None:
    deadline = time.monotonic() + timeout
    last = (0, 0, 0)
    while time.monotonic() < deadline:
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
                     )),
                  (SELECT count(*) FROM runs
                   WHERE id = ANY(%s)
                     AND status NOT IN ('succeeded', 'failed', 'cancelled'))
                """,
                (run_ids, run_ids, COMMAND_TOPIC, run_ids),
            )
            last = tuple(int(value) for value in cursor.fetchone())
        if last == (0, 0, 0):
            return
        time.sleep(0.2)
    raise AssertionError(f"deletion safety did not converge: {last}")


def redis_stream_lengths(run_ids: list[str]) -> dict[str, int]:
    result: dict[str, int] = {}
    for run_id in run_ids:
        completed = subprocess.run(
            service_command(
                "redis",
                REDIS_CONTAINER,
                [
                "redis-cli",
                "-n",
                REDIS_DB,
                "XLEN",
                f"run:{run_id}:live",
                ],
            ),
            check=True,
            text=True,
            capture_output=True,
        )
        result[run_id] = int(completed.stdout.strip())
    return result


def assert_concurrent_facts(runs: list[RunFixture]) -> dict[str, int]:
    run_ids = [run.run_id for run in runs]
    conversation_ids = sorted({run.conversation_id for run in runs})
    with psycopg.connect(POSTGRES_DSN) as connection, connection.cursor() as cursor:
        cursor.execute(
            """
            SELECT
              count(*) FILTER (WHERE status = 'succeeded'),
              count(DISTINCT project_id),
              count(DISTINCT conversation_id)
            FROM runs WHERE id = ANY(%s)
            """,
            (run_ids,),
        )
        succeeded, projects, conversations = map(int, cursor.fetchone())
        cursor.execute(
            """
            SELECT count(*)
            FROM runs AS run
            JOIN conversations AS conversation ON conversation.id = run.conversation_id
            WHERE run.id = ANY(%s) AND run.project_id <> conversation.project_id
            """,
            (run_ids,),
        )
        cross_project = int(cursor.fetchone()[0])
        cursor.execute(
            """
            SELECT count(*)
            FROM messages AS message
            JOIN runs AS run ON run.id = message.metadata ->> 'run_id'
            WHERE run.id = ANY(%s) AND message.conversation_id <> run.conversation_id
            """,
            (run_ids,),
        )
        cross_conversation = int(cursor.fetchone()[0])
        cursor.execute(
            """
            SELECT count(*) FROM (
              SELECT conversation_id, ordinal
              FROM messages
              WHERE conversation_id = ANY(%s)
              GROUP BY conversation_id, ordinal
              HAVING count(*) > 1
            ) AS duplicate
            """,
            (conversation_ids,),
        )
        duplicate_ordinals = int(cursor.fetchone()[0])
        cursor.execute(
            """
            SELECT count(*)
            FROM operation_results
            WHERE run_id = ANY(%s)
              AND status = 'succeeded'
              AND artifacts ? 'screenshot_paths'
              AND jsonb_array_length(artifacts -> 'screenshot_paths') > 0
            """,
            (run_ids,),
        )
        worker_results = int(cursor.fetchone()[0])
        cursor.execute(
            "SELECT count(*) FROM spans WHERE run_id = ANY(%s)",
            (run_ids,),
        )
        spans = int(cursor.fetchone()[0])
        cursor.execute(
            """
            SELECT count(DISTINCT broker_partition)
            FROM outbox_events
            WHERE key = ANY(%s) AND topic = %s AND published_at IS NOT NULL
            """,
            (run_ids, COMMAND_TOPIC),
        )
        kafka_partitions = int(cursor.fetchone()[0])
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

    stream_lengths = redis_stream_lengths(run_ids)
    missing_streams = [run_id for run_id, length in stream_lengths.items() if length < 1]
    expected_spans = len(runs) * 5
    if (
        succeeded != len(runs)
        or projects != 3
        or conversations != 9
        or cross_project != 0
        or cross_conversation != 0
        or duplicate_ordinals != 0
        or worker_results != len(runs)
        or spans != expected_spans
        or kafka_partitions < 2
        or peak_concurrency < 2
        or missing_streams
    ):
        raise AssertionError(
            "concurrent facts mismatch: "
            f"succeeded={succeeded}/{len(runs)}, projects={projects}, "
            f"conversations={conversations}, cross_project={cross_project}, "
            f"cross_conversation={cross_conversation}, duplicate_ordinals={duplicate_ordinals}, "
            f"worker_results={worker_results}, spans={spans}/{expected_spans}, "
            f"kafka_partitions={kafka_partitions}, peak={peak_concurrency}, "
            f"missing_redis={missing_streams}"
        )
    return {
        "runs": len(runs),
        "projects": projects,
        "conversations": conversations,
        "worker_results": worker_results,
        "spans": spans,
        "kafka_partitions": kafka_partitions,
        "peak_concurrency": peak_concurrency,
        "redis_streams": len(stream_lengths),
        "cross_project": cross_project,
        "cross_conversation": cross_conversation,
        "duplicate_ordinals": duplicate_ordinals,
    }


def test_message_idempotency() -> RunFixture:
    conversations = create_project(90, 1)
    fixture = conversations[0]
    key = f"idempotent-{uuid.uuid4()}"
    content = "Concurrent idempotent message"
    with concurrent.futures.ThreadPoolExecutor(max_workers=8) as executor:
        responses = list(
            executor.map(
                lambda _: create_run(
                    fixture,
                    0,
                    idempotency_key=key,
                    content=content,
                ),
                range(8),
            )
        )
    identities = {(run.run_id, run.message_id) for run in responses}
    if len(identities) != 1:
        raise AssertionError(f"idempotent requests created multiple identities: {identities}")
    request(
        f"/api/v1/conversations/{fixture.conversation_id}/messages",
        method="POST",
        body={
            "content": "different content",
            "agent_id": "browser-eval",
            "model": {"provider": "scripted", "name": "browser-eval-fixture"},
            "idempotency_key": key,
        },
        expected=409,
    )
    run = responses[0]
    wait_for_runs([run])
    wait_for_deletion_safety([run.run_id])
    with psycopg.connect(POSTGRES_DSN) as connection, connection.cursor() as cursor:
        cursor.execute(
            """
            SELECT count(*) FROM runs WHERE conversation_id = %s;
            """,
            (fixture.conversation_id,),
        )
        if int(cursor.fetchone()[0]) != 1:
            raise AssertionError("idempotent conversation contains more than one run")
    return run


def test_delete_guard_and_conversation_idempotency() -> None:
    fixture = create_project(91, 1)[0]
    run = create_run(fixture, 0, content="Delete guard run")
    request(
        f"/api/v1/conversations/{fixture.conversation_id}",
        method="DELETE",
        expected=409,
    )
    wait_for_runs([run])
    wait_for_deletion_safety([run.run_id])
    for _ in range(2):
        request(
            f"/api/v1/conversations/{fixture.conversation_id}",
            method="DELETE",
            expected=204,
        )
    for _ in range(2):
        request(f"/api/v1/projects/{fixture.project_id}", method="DELETE", expected=204)


def command_record(run_id: str) -> tuple[str, str, int]:
    with psycopg.connect(POSTGRES_DSN) as connection, connection.cursor() as cursor:
        cursor.execute(
            """
            SELECT event_id, payload::text, broker_partition
            FROM outbox_events
            WHERE key = %s AND topic = %s
            """,
            (run_id, COMMAND_TOPIC),
        )
        row = cursor.fetchone()
    if row is None:
        raise AssertionError(f"command outbox missing for {run_id}")
    if row[2] is None:
        raise AssertionError(f"command outbox has no Kafka partition for {run_id}")
    return str(row[0]), str(row[1]), int(row[2])


def replay_command(run_id: str, payload: str) -> None:
    subprocess.run(
        service_command(
            "kafka",
            KAFKA_CONTAINER,
            [
                "/opt/kafka/bin/kafka-console-producer.sh",
                "--bootstrap-server",
                "localhost:9092",
                "--topic",
                COMMAND_TOPIC,
                "--property",
                "parse.key=true",
                "--property",
                "key.separator=|",
            ],
            interactive=True,
        ),
        input=f"{run_id}|{payload}\n",
        text=True,
        check=True,
        stdout=subprocess.DEVNULL,
    )


def kafka_log_ends() -> dict[int, int]:
    completed = subprocess.run(
        service_command(
            "kafka",
            KAFKA_CONTAINER,
            [
                "/opt/kafka/bin/kafka-get-offsets.sh",
                "--bootstrap-server",
                "localhost:9092",
                "--topic",
                COMMAND_TOPIC,
            ],
        ),
        check=True,
        text=True,
        capture_output=True,
    )
    offsets: dict[int, int] = {}
    for line in completed.stdout.splitlines():
        topic, raw_partition, raw_offset = line.rsplit(":", 2)
        if topic == COMMAND_TOPIC:
            offsets[int(raw_partition)] = int(raw_offset)
    if not offsets:
        raise AssertionError(
            f"Kafka log ends missing for {COMMAND_TOPIC}: {completed.stdout!r}"
        )
    return offsets


def kafka_consumer_offsets() -> dict[int, int]:
    completed = subprocess.run(
        service_command(
            "kafka",
            KAFKA_CONTAINER,
            [
                "/opt/kafka/bin/kafka-consumer-groups.sh",
                "--bootstrap-server",
                "localhost:9092",
                "--group",
                KAFKA_GROUP_ID,
                "--describe",
            ],
        ),
        check=True,
        text=True,
        capture_output=True,
    )
    offsets: dict[int, int] = {}
    for line in completed.stdout.splitlines():
        columns = line.split()
        if (
            len(columns) >= 4
            and columns[1] == COMMAND_TOPIC
            and columns[2].isdigit()
            and columns[3].isdigit()
        ):
            offsets[int(columns[2])] = int(columns[3])
    if not offsets:
        raise AssertionError(
            f"consumer offsets missing for group {KAFKA_GROUP_ID}: "
            f"{completed.stdout!r}"
        )
    return offsets


def wait_for_replay_consumption(previous_log_ends: dict[int, int]) -> None:
    deadline = time.monotonic() + 20
    last_log_ends: dict[int, int] = {}
    last_consumer_offsets: dict[int, int] = {}
    while time.monotonic() < deadline:
        log_ends = kafka_log_ends()
        consumer_offsets = kafka_consumer_offsets()
        last_log_ends = log_ends
        last_consumer_offsets = consumer_offsets
        advanced = [
            partition
            for partition, offset in log_ends.items()
            if offset > previous_log_ends.get(partition, 0)
        ]
        if (
            sum(log_ends.values()) >= sum(previous_log_ends.values()) + 1
            and advanced
            and all(consumer_offsets.get(partition, -1) >= log_ends[partition]
                    for partition in advanced)
        ):
            return
        time.sleep(0.2)
    raise AssertionError(
        "Kafka replay was not consumed: "
        f"log_ends={last_log_ends}, consumer_offsets={last_consumer_offsets}, "
        f"before={previous_log_ends}"
    )


def test_project_delete_and_replay(runs: list[RunFixture]) -> None:
    project_id = runs[0].project_id
    project_runs = [run for run in runs if run.project_id == project_id]
    event_id, payload, _partition = command_record(project_runs[0].run_id)
    previous_log_ends = kafka_log_ends()
    wait_for_deletion_safety([run.run_id for run in project_runs])
    for _ in range(2):
        request(f"/api/v1/projects/{project_id}", method="DELETE", expected=204)

    replay_command(project_runs[0].run_id, payload)
    wait_for_replay_consumption(previous_log_ends)
    with psycopg.connect(POSTGRES_DSN) as connection, connection.cursor() as cursor:
        cursor.execute("SELECT count(*) FROM projects WHERE id = %s", (project_id,))
        projects = int(cursor.fetchone()[0])
        cursor.execute(
            "SELECT count(*) FROM runs WHERE id = ANY(%s)",
            ([run.run_id for run in project_runs],),
        )
        deleted_runs = int(cursor.fetchone()[0])
        cursor.execute(
            """
            SELECT count(*) FROM inbox_events
            WHERE consumer = 'agenttrace-runtime'
              AND event_id = %s
              AND processed_at IS NOT NULL
            """,
            (event_id,),
        )
        tombstones = int(cursor.fetchone()[0])
    if (projects, deleted_runs, tombstones) != (0, 0, 1):
        raise AssertionError(
            "Kafka replay changed deleted scope: "
            f"projects={projects}, runs={deleted_runs}, tombstones={tombstones}"
        )


def main() -> int:
    try:
        request("/api/v1/health")
        with concurrent.futures.ThreadPoolExecutor(max_workers=3) as executor:
            groups = list(executor.map(lambda index: create_project(index, 3), range(3)))
        conversations = [fixture for group in groups for fixture in group]
        work = [
            (conversation, run_index)
            for conversation in conversations
            for run_index in range(2)
        ]
        with concurrent.futures.ThreadPoolExecutor(max_workers=len(work)) as executor:
            runs = list(executor.map(lambda item: create_run(*item), work))
        wait_for_runs(runs)
        wait_for_deletion_safety([run.run_id for run in runs])
        result = assert_concurrent_facts(runs)
        idempotent_run = test_message_idempotency()
        test_delete_guard_and_conversation_idempotency()
        test_project_delete_and_replay(runs)
        result["message_idempotent_run"] = idempotent_run.run_id
        result["delete_idempotency"] = 1
        result["kafka_replay_safe"] = 1
    except Exception as error:
        print(f"concurrent delete scenario failed: {error}", file=sys.stderr)
        return 1
    print("concurrent delete scenario passed: " + json.dumps(result, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
