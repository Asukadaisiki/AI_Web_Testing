"""Bounded live failure drills for the AgentTrace v3 Compose stack.

This module intentionally exercises the deployed services. PostgreSQL is only
read for assertions; no failure state or duplicate delivery is fabricated.
"""

from __future__ import annotations

import json
import os
import re
import subprocess
import sys
import time
import uuid
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Callable

import psycopg

from canonical_scenario import GOAL, assert_api_outcome, request_api, require_status
from wait_for_stack import wait_for_stack

ROOT = Path(__file__).resolve().parents[2]
API_BASE_URL = os.environ.get("AGENTTRACE_API_URL", "http://127.0.0.1:8080").rstrip("/")
POSTGRES_DSN = os.environ.get(
    "AGENTTRACE_POSTGRES_DSN",
    "postgres://agenttrace:agenttrace@127.0.0.1:5433/agenttrace?sslmode=disable",
)

COMMAND_TOPIC = "agenttrace.run.commands.v1"
EVENT_TOPIC = "agenttrace.run.events.v1"
RUNTIME_CONSUMER = "agenttrace-runtime"
EXPORTER_CONSUMER = "agenttrace-langfuse-exporter"
DEFAULT_WAIT_SECONDS = 120.0
EXPORTER_FIXTURE_ENV = {
    "AGENTTRACE_LANGFUSE_BASE_URL": "http://127.0.0.1:9",
    "AGENTTRACE_LANGFUSE_PUBLIC_KEY": "agenttrace-failure-drill-public",
    "AGENTTRACE_LANGFUSE_SECRET_KEY": "agenttrace-failure-drill-secret",
}
KAFKA_RECORD_LINE = re.compile(r"^Partition:(\d+)\tOffset:(\d+)\t(.+)$")
MAX_KAFKA_RECORD_OUTPUT = 20_000


def parse_consumer_group_offsets(output: str, topic: str) -> dict[int, int]:
    """Extract committed offsets for one topic from Kafka's group description."""
    offsets: dict[int, int] = {}
    for line in output.splitlines():
        fields = line.split()
        if len(fields) < 6 or fields[0] == "GROUP" or fields[1] != topic:
            continue
        try:
            partition = int(fields[2])
            offset = int(fields[3])
        except ValueError as error:
            raise AssertionError(
                f"Kafka group has no committed offset for topic {topic!r}: {line}"
            ) from error
        offsets[partition] = offset
    if not offsets:
        raise AssertionError(
            f"Kafka group description has no offsets for topic {topic!r}: {output[-2000:]}"
        )
    return offsets


@dataclass(frozen=True)
class KafkaRecordLocation:
    """A record's immutable location in a Kafka topic."""

    partition: int
    offset: int


def parse_kafka_consumer_records(
    output: str,
) -> list[tuple[KafkaRecordLocation, str]]:
    """Parse Kafka CLI records with their partition and offset metadata."""
    if len(output) > MAX_KAFKA_RECORD_OUTPUT:
        raise AssertionError(
            "malformed Kafka record output exceeds "
            f"{MAX_KAFKA_RECORD_OUTPUT} byte limit"
        )
    records: list[tuple[KafkaRecordLocation, str]] = []
    for line in output.splitlines():
        if not line or line.startswith("Processed a total of "):
            continue
        match = KAFKA_RECORD_LINE.match(line)
        if match is None:
            raise AssertionError(f"malformed Kafka record output: {line[:500]}")
        records.append(
            (
                KafkaRecordLocation(
                    partition=int(match.group(1)),
                    offset=int(match.group(2)),
                ),
                match.group(3),
            )
        )
    return records


@dataclass(frozen=True)
class Scenario:
    """One isolated workspace and durable run created for a drill."""

    project_id: str
    conversation_id: str
    run_id: str


class FailureDrills:
    """Runs the real Compose failure modes with mandatory service restoration."""

    def __init__(
        self,
        *,
        wait_seconds: float = DEFAULT_WAIT_SECONDS,
        exporter_consumer: str | None = None,
    ) -> None:
        self.wait_seconds = wait_seconds
        self.exporter_consumer = exporter_consumer or (
            f"{EXPORTER_CONSUMER}-failure-{uuid.uuid4().hex}"
        )
        self._exporter_fixture_created = False

    def kafka_outage(self) -> None:
        """A Kafka outage leaves the accepted command durably queued."""
        self._wait_stack()
        workspace = self._create_workspace("kafka-outage")
        scenario: Scenario | None = None
        self._stop("kafka")
        try:
            scenario = self._submit_run(workspace, "kafka-outage")
            self._wait(
                "Kafka outage leaves a queued run and unpublished command outbox event",
                lambda: self._queued_unpublished_command(scenario.run_id),
                run_id=scenario.run_id,
            )
        finally:
            self._start("kafka")
            self._wait_stack()
            # The runtime process intentionally exits when its broker consumer
            # returns an unrecoverable connection error. Restart the named v3
            # consumer only after Kafka is healthy to prove durable recovery.
            self._start("runtime")
            self._wait_service_running("runtime")

        self._wait_for_completion(scenario)
        self._assert_standard_completion(scenario)

    def redis_outage(self) -> None:
        """A Redis outage uses PostgreSQL polling without losing trace reads."""
        self._wait_stack()
        workspace = self._create_workspace("redis-outage")
        scenario: Scenario | None = None
        self._stop("redis")
        try:
            self._wait(
                "Redis outage reports postgres_poll health mode",
                self._redis_postgres_poll,
            )
            scenario = self._submit_run(workspace, "redis-outage")
            self._wait_for_completion(scenario)
            self._assert_trace_readable(scenario.run_id)
        finally:
            self._start("redis")
            self._wait_stack()

        self._assert_standard_completion(scenario)

    def runtime_restart(self) -> None:
        """A killed runtime resumes the durable command exactly once."""
        self._wait_stack()
        workspace = self._create_workspace("runtime-restart")
        scenario: Scenario | None = None
        self._kill("runtime")
        try:
            scenario = self._submit_run(workspace, "runtime-restart")
            self._wait(
                "killed runtime leaves a queued durable command",
                lambda: self._queued_durable_command(scenario.run_id),
                run_id=scenario.run_id,
            )
        finally:
            self._start("runtime")
            self._wait_service_running("runtime")

        self._wait_for_completion(scenario)
        self._assert_standard_completion(scenario)
        self._assert_unique_runtime_effects(scenario.run_id)

    def postgres_outage(self) -> None:
        """PostgreSQL outage rejects a message before a command can be stored."""
        self._wait_stack()
        scenario = self._create_workspace("postgres-outage")
        agent = self._agent()
        body = self._message_body("postgres-outage", agent)
        idempotency_key = str(body["idempotency_key"])
        accepted_status: int | None = None
        accepted_body: dict[str, Any] = {}
        self._stop("postgres")
        try:
            self._wait(
                "PostgreSQL outage makes API health return 503",
                self._postgres_health_is_503,
                run_id="",
            )
            accepted_status, accepted_body = request_api(
                f"/api/v1/conversations/{scenario.conversation_id}/messages",
                method="POST",
                body=body,
                timeout=15,
            )
            if accepted_status != 503:
                raise AssertionError(
                    "PostgreSQL outage accepted a message; "
                    f"got HTTP {accepted_status}: {accepted_body}"
                )
        finally:
            self._start("postgres")
            self._wait_stack()

        self._assert_rejected_postgres_request_absent(
            conversation_id=scenario.conversation_id,
            idempotency_key=idempotency_key,
        )

    def exporter_outage(self) -> None:
        """An optional exporter failure cannot block local completion."""
        try:
            self._ensure_exporter()
            scenario: Scenario | None = None
            completion_event_id = ""
            self._stop("exporter")
            try:
                scenario = self._create_run("exporter-outage")
                self._wait_for_completion(scenario)
                completion_event_id = self._completion_event_id(scenario.run_id)
            finally:
                self._start("exporter")
                self._wait_service_running("exporter")

            self._assert_standard_completion(scenario)
            self._wait(
                "recovered exporter records a durable failed export retry",
                lambda: self._export_retry_visible(completion_event_id),
                run_id=scenario.run_id,
            )
        finally:
            if self._exporter_fixture_created:
                self._remove_exporter_fixture()

    def duplicate_command_delivery(self) -> None:
        """A real duplicate Kafka command produces no second durable effect."""
        scenario = self._create_run("duplicate-delivery")
        self._wait_for_completion(scenario)
        self._assert_standard_completion(scenario)

        payload, event_id = self._command_envelope(scenario.run_id)
        before = self._duplicate_effect_counts(scenario.run_id, event_id)
        if before != (1, 1, 1):
            raise AssertionError(
                "source command did not produce exactly one inbox row, operation result, "
                f"and assistant message before duplicate injection: {before}"
            )
        before_end_offsets = self._command_topic_end_offsets()
        self._inject_duplicate_command(payload)
        duplicate_record = self._find_injected_duplicate_record(
            before_end_offsets=before_end_offsets,
            event_id=event_id,
            payload=payload,
        )

        self._wait(
            "runtime consumer commits past the injected duplicate command "
            f"at partition={duplicate_record.partition}, offset={duplicate_record.offset}",
            lambda: self._runtime_consumer_record_committed(duplicate_record),
            run_id=scenario.run_id,
        )
        after = self._duplicate_effect_counts(scenario.run_id, event_id)
        if after != before:
            raise AssertionError(
                "consumed Kafka duplicate changed durable effect cardinalities: "
                f"before={before}, after={after}"
            )

    def _create_workspace(self, label: str) -> Scenario:
        token = uuid.uuid4().hex
        status, project = request_api(
            "/api/v1/projects",
            method="POST",
            body={
                "name": f"Failure drill {label} {token}",
                "metadata": {"scenario": "failure-drill", "drill": label},
            },
        )
        require_status(201, status, project, f"create {label} project")
        project_id = str(project.get("id", ""))
        if not project_id:
            raise AssertionError(f"create {label} project returned no ID: {project}")

        status, conversation = request_api(
            f"/api/v1/projects/{project_id}/conversations",
            method="POST",
            body={
                "title": f"Failure drill {label}",
                "metadata": {"scenario": "failure-drill", "drill": label},
            },
        )
        require_status(201, status, conversation, f"create {label} conversation")
        conversation_id = str(conversation.get("id", ""))
        if not conversation_id:
            raise AssertionError(
                f"create {label} conversation returned no ID: {conversation}"
            )
        return Scenario(project_id, conversation_id, "")

    def _create_run(self, label: str) -> Scenario:
        self._wait_stack()
        workspace = self._create_workspace(label)
        return self._submit_run(workspace, label)

    def _submit_run(self, workspace: Scenario, label: str) -> Scenario:
        agent = self._agent()
        status, accepted = request_api(
            f"/api/v1/conversations/{workspace.conversation_id}/messages",
            method="POST",
            body=self._message_body(label, agent),
        )
        require_status(202, status, accepted, f"create {label} run")
        run_id = str(accepted.get("run_id", ""))
        if not run_id:
            raise AssertionError(f"create {label} run returned no run ID: {accepted}")
        return Scenario(workspace.project_id, workspace.conversation_id, run_id)

    @staticmethod
    def _agent() -> dict[str, Any]:
        status, agents = request_api("/api/v1/agents")
        require_status(200, status, agents, "get agents")
        agent = agents.get("agents", [{}])[0]
        if not agent.get("id"):
            raise AssertionError(f"get agents returned no agent ID: {agents}")
        return agent

    @staticmethod
    def _message_body(label: str, agent: dict[str, Any] | None = None) -> dict[str, Any]:
        agent = agent or {}
        return {
            "content": GOAL,
            "agent_id": agent.get("id"),
            "model": {
                "provider": agent.get("model", {}).get("provider"),
                "name": agent.get("model", {}).get("name"),
                "reasoning_effort": agent.get("model", {}).get("reasoning_effort"),
            },
            "idempotency_key": f"failure-drill-{label}-{uuid.uuid4()}",
        }

    def _wait_for_completion(self, scenario: Scenario) -> None:
        self._wait(
            "run reaches succeeded state",
            lambda: self._run_succeeded(scenario.run_id),
            run_id=scenario.run_id,
        )

    def _assert_standard_completion(self, scenario: Scenario | None) -> None:
        if scenario is None:
            raise AssertionError("drill did not create a scenario")
        assert_api_outcome(scenario.project_id, scenario.conversation_id, scenario.run_id)

    def _assert_trace_readable(self, run_id: str) -> None:
        status, trace = request_api(f"/api/v1/runs/{run_id}/trace")
        require_status(200, status, trace, "read trace while Redis is down")
        if not trace.get("spans"):
            raise AssertionError(f"trace is empty while Redis is down: {trace}")
        span_count = self._scalar(
            "SELECT count(*) FROM spans WHERE run_id = %s",
            (run_id,),
        )
        if span_count < 1:
            raise AssertionError(
                f"PostgreSQL has no durable trace spans for Redis outage run {run_id}"
            )

    def _assert_unique_runtime_effects(self, run_id: str) -> None:
        assistant_count, completion_count, completed_spans, unique_completed_spans = (
            self._row(
                """
                SELECT
                  (SELECT count(*) FROM messages
                   WHERE metadata ->> 'run_id' = %s AND role = 'assistant'),
                  (SELECT count(*) FROM outbox_events
                   WHERE key = %s AND payload ->> 'event_type' = 'runtime.completed'),
                  (SELECT count(*) FROM spans
                   WHERE run_id = %s AND status = 'succeeded' AND ended_at IS NOT NULL),
                  (SELECT count(DISTINCT span_id) FROM spans
                   WHERE run_id = %s AND status = 'succeeded' AND ended_at IS NOT NULL)
                """,
                (run_id, run_id, run_id, run_id),
            )
        )
        if (
            assistant_count != 1
            or completion_count != 1
            or completed_spans < 1
            or completed_spans != unique_completed_spans
        ):
            raise AssertionError(
                "runtime restart produced non-unique durable completion effects: "
                f"assistant={assistant_count}, completion={completion_count}, "
                f"completed_spans={completed_spans}, "
                f"unique_completed_spans={unique_completed_spans}"
            )

    def _queued_unpublished_command(self, run_id: str) -> bool:
        status, published_at = self._row(
            """
            SELECT runs.status, outbox_events.published_at
            FROM runs
            JOIN outbox_events
              ON outbox_events.key = runs.id
             AND outbox_events.topic = %s
            WHERE runs.id = %s
            """,
            (COMMAND_TOPIC, run_id),
        )
        if status != "queued":
            raise AssertionError(f"run {run_id} status={status!r}, want queued")
        return published_at is None

    def _queued_durable_command(self, run_id: str) -> bool:
        status, _ = self._row(
            """
            SELECT runs.status, outbox_events.published_at
            FROM runs
            JOIN outbox_events
              ON outbox_events.key = runs.id
             AND outbox_events.topic = %s
            WHERE runs.id = %s
            """,
            (COMMAND_TOPIC, run_id),
        )
        if status != "queued":
            raise AssertionError(f"run {run_id} status={status!r}, want queued")
        return True

    def _assert_rejected_postgres_request_absent(
        self, *, conversation_id: str, idempotency_key: str
    ) -> None:
        message_count, run_count, command_count, idempotency_count = self._row(
            """
            SELECT
              (SELECT count(*) FROM messages
               WHERE conversation_id = %s AND role = 'user'),
              (SELECT count(*) FROM runs
               WHERE conversation_id = %s),
              (SELECT count(*) FROM outbox_events AS event
               JOIN runs ON runs.id = event.key
               WHERE runs.conversation_id = %s AND event.topic = 'agenttrace.run.commands.v1'),
              (SELECT count(*) FROM idempotency_keys
               WHERE key = %s)
            """,
            (conversation_id, conversation_id, conversation_id, idempotency_key),
        )
        counts = (
            int(message_count),
            int(run_count),
            int(command_count),
            int(idempotency_count),
        )
        if counts != (0, 0, 0, 0):
            raise AssertionError(
                "PostgreSQL outage request left scoped durable state despite HTTP 503: "
                f"conversation={conversation_id}, message/run/command/idempotency={counts}"
            )

    def _redis_postgres_poll(self) -> bool:
        status, health = request_api("/api/v1/health")
        redis = health.get("dependencies", {}).get("redis", {})
        if status != 200:
            raise AssertionError(f"Redis outage health returned HTTP {status}: {health}")
        return (
            health.get("status") == "degraded"
            and redis.get("status") == "down"
            and redis.get("mode") == "postgres_poll"
        )

    def _postgres_health_is_503(self) -> bool:
        status, health = request_api("/api/v1/health", timeout=5)
        return (
            status == 503
            and health.get("dependencies", {}).get("postgres", {}).get("status") == "down"
        )

    def _run_succeeded(self, run_id: str) -> bool:
        status, run = request_api(f"/api/v1/runs/{run_id}", timeout=10)
        if status == 200:
            if run.get("status") == "succeeded":
                return True
            if run.get("status") in {"failed", "cancelled"}:
                raise AssertionError(f"run {run_id} ended unexpectedly: {run}")
            return False
        if status == 503:
            return False
        raise AssertionError(f"get run {run_id} returned HTTP {status}: {run}")

    def _completion_event_id(self, run_id: str) -> str:
        (event_id,) = self._row(
            """
            SELECT event_id
            FROM outbox_events
            WHERE key = %s
              AND topic = %s
              AND payload ->> 'event_type' = 'runtime.completed'
            """,
            (run_id, EVENT_TOPIC),
        )
        return str(event_id)

    def _export_retry_visible(self, completion_event_id: str) -> bool:
        (attempt_count,) = self._row(
            """
            SELECT attempt_count
            FROM consumer_retry_counts
            WHERE consumer = %s AND event_id = %s
            """,
            (self.exporter_consumer, completion_event_id),
        )
        return int(attempt_count) >= 2

    def _command_envelope(self, run_id: str) -> tuple[str, str]:
        payload, event_id = self._row(
            """
            SELECT payload::text, event_id
            FROM outbox_events
            WHERE key = %s AND topic = %s
            """,
            (run_id, COMMAND_TOPIC),
        )
        return str(payload), str(event_id)

    def _runtime_consumer_offsets(self) -> dict[int, int]:
        result = self._compose(
            "exec",
            "-T",
            "kafka",
            "/opt/kafka/bin/kafka-consumer-groups.sh",
            "--bootstrap-server",
            "kafka:9092",
            "--describe",
            "--group",
            RUNTIME_CONSUMER,
        )
        return parse_consumer_group_offsets(result.stdout, COMMAND_TOPIC)

    def _runtime_consumer_record_committed(self, record: KafkaRecordLocation) -> bool:
        """Kafka consumer commits the next offset, so it must exceed record offset."""
        return self._runtime_consumer_offsets().get(record.partition, -1) > record.offset

    def _command_topic_end_offsets(self) -> dict[int, int]:
        result = self._compose(
            "exec",
            "-T",
            "kafka",
            "/opt/kafka/bin/kafka-get-offsets.sh",
            "--bootstrap-server",
            "kafka:9092",
            "--topic",
            COMMAND_TOPIC,
            "--time",
            "-1",
        )
        offsets: dict[int, int] = {}
        for line in result.stdout.splitlines():
            fields = line.rsplit(":", 2)
            if len(fields) != 3 or fields[0] != COMMAND_TOPIC:
                raise AssertionError(f"malformed Kafka end offset output: {line[:500]}")
            try:
                partition, offset = int(fields[1]), int(fields[2])
            except ValueError as error:
                raise AssertionError(
                    f"malformed Kafka end offset output: {line[:500]}"
                ) from error
            offsets[partition] = offset
        if not offsets:
            raise AssertionError(
                "Kafka returned no command topic end offsets: "
                f"{result.stdout[-2000:]}"
            )
        return offsets

    def _find_injected_duplicate_record(
        self,
        *,
        before_end_offsets: dict[int, int],
        event_id: str,
        payload: str,
    ) -> KafkaRecordLocation:
        """Locate this injection using the immutable event ID and exact JSON payload."""
        try:
            expected_payload = json.loads(payload)
        except json.JSONDecodeError as error:
            raise AssertionError(f"stored command payload is not JSON: {payload[:500]}") from error

        candidates: list[KafkaRecordLocation] = []
        for partition, start_offset in sorted(before_end_offsets.items()):
            records = self._command_topic_records_from(partition, start_offset)
            for location, record_payload in records:
                try:
                    decoded_payload = json.loads(record_payload)
                except json.JSONDecodeError as error:
                    raise AssertionError(
                        "Kafka command record is not JSON at "
                        f"partition={location.partition}, offset={location.offset}: "
                        f"{record_payload[:500]}"
                    ) from error
                if (
                    decoded_payload.get("event_id") == event_id
                    and decoded_payload == expected_payload
                ):
                    candidates.append(location)
        if len(candidates) != 1:
            raise AssertionError(
                "could not uniquely locate injected duplicate Kafka command by event_id "
                f"and exact payload: event_id={event_id}, candidates={candidates}"
            )
        return candidates[0]

    def _command_topic_records_from(
        self, partition: int, start_offset: int
    ) -> list[tuple[KafkaRecordLocation, str]]:
        result = self._compose(
            "exec",
            "-T",
            "kafka",
            "/opt/kafka/bin/kafka-console-consumer.sh",
            "--bootstrap-server",
            "kafka:9092",
            "--topic",
            COMMAND_TOPIC,
            "--partition",
            str(partition),
            "--offset",
            str(start_offset),
            "--timeout-ms",
            "3000",
            "--formatter",
            "kafka.tools.DefaultMessageFormatter",
            "--property",
            "print.partition=true",
            "--property",
            "print.offset=true",
            "--property",
            "print.value=true",
        )
        return parse_kafka_consumer_records(result.stdout)

    def _duplicate_effect_counts(self, run_id: str, event_id: str) -> tuple[int, int, int]:
        inbox, operation_results, assistant_messages = self._row(
            """
            SELECT
              (SELECT count(*) FROM inbox_events
               WHERE consumer = %s AND event_id = %s),
              (SELECT count(*) FROM operation_results WHERE run_id = %s),
              (SELECT count(*) FROM messages
               WHERE metadata ->> 'run_id' = %s AND role = 'assistant')
            """,
            (RUNTIME_CONSUMER, event_id, run_id, run_id),
        )
        return int(inbox), int(operation_results), int(assistant_messages)

    def _inject_duplicate_command(self, payload: str) -> None:
        self._compose(
            "exec",
            "-T",
            "kafka",
            "/opt/kafka/bin/kafka-console-producer.sh",
            "--bootstrap-server",
            "kafka:9092",
            "--topic",
            COMMAND_TOPIC,
            input_text=f"{payload}\n",
        )

    def _ensure_exporter(self) -> None:
        if self._service_running("exporter"):
            raise AssertionError(
                "exporter is already running; refusing to replace operator configuration"
            )
        self._compose(
            "exec",
            "-T",
            "kafka",
            "/opt/kafka/bin/kafka-consumer-groups.sh",
            "--bootstrap-server",
            "kafka:9092",
            "--group",
            self.exporter_consumer,
            "--topic",
            EVENT_TOPIC,
            "--reset-offsets",
            "--to-latest",
            "--execute",
        )
        environment = dict(EXPORTER_FIXTURE_ENV)
        environment["AGENTTRACE_LANGFUSE_GROUP_ID"] = self.exporter_consumer
        # Once provisioning starts, this drill owns any container Compose may
        # create, including one left behind by a partial command failure.
        self._exporter_fixture_created = True
        self._compose(
            "--profile",
            "langfuse",
            "up",
            "-d",
            "exporter",
            environment=environment,
        )
        self._wait_service_running("exporter")

    def _remove_exporter_fixture(self) -> None:
        self._compose("--profile", "langfuse", "rm", "-f", "-s", "exporter")
        self._exporter_fixture_created = False

    def _wait_stack(self) -> None:
        try:
            wait_for_stack(API_BASE_URL, timeout=self.wait_seconds)
        except Exception as error:
            raise AssertionError(
                f"v3 Compose stack did not recover: {error}; {self._diagnostics()}"
            ) from error

    def _wait_service_running(self, service: str) -> None:
        self._wait(
            f"{service} service reports running",
            lambda: self._service_running(service),
        )

    def _service_running(self, service: str) -> bool:
        args = ["ps", "--format", "json", service]
        if service == "exporter":
            args = ["--profile", "langfuse", *args]
        result = self._compose(*args)
        records = [line for line in result.stdout.splitlines() if line.strip()]
        if not records:
            return False
        try:
            statuses = [json.loads(record).get("State") for record in records]
        except json.JSONDecodeError as error:
            raise AssertionError(
                f"could not parse docker compose state for {service}: {result.stdout}"
            ) from error
        return statuses == ["running"]

    def _stop(self, service: str) -> None:
        self._compose(*self._profile_args(service), "stop", service)

    def _start(self, service: str) -> None:
        self._compose(*self._profile_args(service), "start", service)

    def _kill(self, service: str) -> None:
        self._compose(*self._profile_args(service), "kill", service)

    @staticmethod
    def _profile_args(service: str) -> tuple[str, ...]:
        return ("--profile", "langfuse") if service == "exporter" else ()

    def _compose(
        self,
        *args: str,
        input_text: str | None = None,
        environment: dict[str, str] | None = None,
    ) -> subprocess.CompletedProcess[str]:
        command = ["docker", "compose", *args]
        child_environment = None
        if environment is not None:
            child_environment = os.environ.copy()
            child_environment.update(environment)
        try:
            result = subprocess.run(
                command,
                cwd=ROOT,
                input=input_text,
                text=True,
                capture_output=True,
                timeout=30,
                check=False,
                env=child_environment,
            )
        except subprocess.TimeoutExpired as error:
            raise AssertionError(f"timed out after 30s: {' '.join(command)}") from error
        if result.returncode != 0:
            output = (result.stdout + result.stderr)[-4000:]
            raise AssertionError(
                f"Compose command failed ({result.returncode}): {' '.join(command)}; "
                f"output={output}"
            )
        return result

    def _wait(
        self,
        description: str,
        condition: Callable[[], bool],
        *,
        run_id: str = "",
        timeout: float | None = None,
    ) -> None:
        timeout = self.wait_seconds if timeout is None else timeout
        deadline = time.monotonic() + timeout
        last_error: Exception | None = None
        while time.monotonic() < deadline:
            try:
                if condition():
                    return
            except Exception as error:
                last_error = error
            time.sleep(0.25)
        raise AssertionError(
            f"timed out after {timeout:.0f}s waiting for {description}; "
            f"last_error={last_error}; {self._diagnostics(run_id)[:6000]}"
        )

    def _row(self, query: str, values: tuple[object, ...]) -> tuple[Any, ...]:
        with psycopg.connect(POSTGRES_DSN, connect_timeout=5) as connection:
            with connection.cursor() as cursor:
                cursor.execute(query, values)
                row = cursor.fetchone()
        if row is None:
            raise AssertionError(f"query returned no row: {query}")
        return tuple(row)

    def _scalar(self, query: str, values: tuple[object, ...]) -> Any:
        return self._row(query, values)[0]

    def _diagnostics(self, run_id: str = "") -> str:
        """Return a bounded, non-secret snapshot suitable for timeout failures."""
        result: dict[str, Any] = {}
        try:
            status, health = request_api("/api/v1/health", timeout=3)
            result["health"] = {"status": status, "body": health}
        except Exception as error:
            result["health_error"] = str(error)
        if run_id:
            try:
                with psycopg.connect(POSTGRES_DSN, connect_timeout=3) as connection:
                    with connection.cursor() as cursor:
                        cursor.execute(
                            "SELECT status FROM runs WHERE id = %s", (run_id,)
                        )
                        run = cursor.fetchone()
                        cursor.execute(
                            """
                            SELECT topic, published_at IS NOT NULL, attempt_count,
                                   COALESCE(left(last_error, 160), '')
                            FROM outbox_events
                            WHERE key = %s
                            ORDER BY created_at
                            LIMIT 8
                            """,
                            (run_id,),
                        )
                        outbox = cursor.fetchall()
                result["run"] = run[0] if run else "missing"
                result["outbox"] = outbox
            except Exception as error:
                result["postgres_error"] = str(error)
        try:
            result["runtime_command_offsets"] = self._runtime_consumer_offsets()
        except Exception as error:
            result["runtime_command_offsets_error"] = str(error)
        try:
            compose = self._compose("ps", "--format", "json")
            result["services"] = [
                {
                    key: value
                    for key, value in json.loads(line).items()
                    if key in {"Service", "State", "Health", "Status"}
                }
                for line in compose.stdout.splitlines()
                if line.strip()
            ]
        except Exception as error:
            result["compose_error"] = str(error)
        return json.dumps(result, default=str)[:6000]


def main() -> int:
    drills = FailureDrills()
    cases: tuple[tuple[str, Callable[[], None]], ...] = (
        ("kafka outage", drills.kafka_outage),
        ("redis outage", drills.redis_outage),
        ("runtime restart", drills.runtime_restart),
        ("postgres outage", drills.postgres_outage),
        ("exporter outage", drills.exporter_outage),
        ("duplicate delivery", drills.duplicate_command_delivery),
    )
    try:
        for name, drill in cases:
            print(f"failure drill: {name}")
            drill()
            print(f"failure drill passed: {name}")
        drills._wait_stack()
    except Exception as error:
        print(f"failure drills failed: {error}", file=sys.stderr)
        return 1
    print("failure drills passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
