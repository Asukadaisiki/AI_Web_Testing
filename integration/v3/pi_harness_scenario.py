"""Canonical acceptance for the Pi browser harness migration.

Asserts the production path goes through a Run-scoped Pi harness subprocess
instead of a Go model loop or network AgentServer, and that EvalHub PostgreSQL
remains the only source of truth for Run, Trace, cancellation, and rerun history.
"""

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

from wait_for_stack import wait_for_stack

ROOT = Path(__file__).resolve().parents[2]
API_BASE_URL = os.environ.get("AGENTTRACE_API_URL", "http://127.0.0.1:8080").rstrip("/")
POSTGRES_DSN = os.environ.get(
    "AGENTTRACE_POSTGRES_DSN",
    "postgres://agenttrace:agenttrace@127.0.0.1:5433/agenttrace?sslmode=disable",
)
TERMINAL = {"succeeded", "failed", "cancelled"}
HARNESS_EVENT_TYPES = (
    "harness.started",
    "pi.message.completed",
    "pi.usage",
    "pi.tool.started",
    "pi.tool.completed",
    "browser.observation",
    "browser.case.completed",
    "harness.settled",
)
GOAL = (
    "Filter the local catalog for alpha, open its detail page, add two items to "
    "the cart, and verify the cart summary."
)
FAILING_GOAL = (
    "Open the local storefront and assert that the product named 'DoesNotExist' "
    "is visible on the index page."
)


@dataclass(frozen=True)
class RunFixture:
    project_id: str
    conversation_id: str
    run_id: str


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
        timeout=120,
    )
    if completed.returncode != 0:
        output = (completed.stdout + completed.stderr)[-4000:]
        raise AssertionError(
            f"Compose command failed ({completed.returncode}): {' '.join(command)}; "
            f"output={output}"
        )


def create_workspace(batch: str, *, failing: bool = False) -> RunFixture:
    project = request(
        "/api/v1/projects",
        method="POST",
        body={
            "name": f"Pi harness {batch} {'fail' if failing else 'pass'}",
            "metadata": {"scenario": "pi-harness", "batch": batch},
        },
        expected=201,
    )
    project_id = str(project["id"])
    conversation = request(
        f"/api/v1/projects/{project_id}/conversations",
        method="POST",
        body={
            "title": f"Pi harness {'failing' if failing else 'canonical'} run",
            "metadata": {"scenario": "pi-harness", "batch": batch},
        },
        expected=201,
    )
    conversation_id = str(conversation["id"])
    agents = request("/api/v1/agents").get("agents", [])
    if not agents:
        raise AssertionError("no configured agent")
    agent = agents[0]
    accepted = request(
        f"/api/v1/conversations/{conversation_id}/messages",
        method="POST",
        body={
            "content": FAILING_GOAL if failing else GOAL,
            "agent_id": str(agent["id"]),
            "model": {
                "provider": str(agent["model"]["provider"]),
                "name": str(agent["model"]["name"]),
                "reasoning_effort": str(agent["model"].get("reasoning_effort", "")),
            },
            "idempotency_key": f"pi-harness-{batch}-{uuid.uuid4()}",
        },
        expected=202,
    )
    return RunFixture(project_id, conversation_id, str(accepted["run_id"]))


def wait_for_terminal(run_id: str, *, timeout: float = 180) -> str:
    deadline = time.monotonic() + timeout
    latest = ""
    while time.monotonic() < deadline:
        latest = str(request(f"/api/v1/runs/{run_id}")["status"])
        if latest in TERMINAL:
            return latest
        time.sleep(0.25)
    raise AssertionError(f"run {run_id} did not become terminal; latest={latest!r}")


def cancel_run(run_id: str) -> None:
    request(
        f"/api/v1/runs/{run_id}/cancel",
        method="POST",
        body={},
        expected=202,
    )


def harness_attempt_for_run(run_id: str) -> dict[str, Any]:
    import psycopg

    with psycopg.connect(POSTGRES_DSN) as connection, connection.cursor() as cursor:
        cursor.execute(
            """
            SELECT id, run_id, attempt_number, provider_attempt_id, status
            FROM harness_attempts
            WHERE run_id = %s
            ORDER BY attempt_number
            """,
            (run_id,),
        )
        rows = cursor.fetchall()
    if not rows:
        raise AssertionError(f"run {run_id} has no harness attempt")
    return {
        "id": str(rows[0][0]),
        "run_id": str(rows[0][1]),
        "attempt_number": int(rows[0][2]),
        "provider_attempt_id": str(rows[0][3]),
        "status": str(rows[0][4]),
    }


def harness_event_types(run_id: str) -> set[str]:
    import psycopg

    with psycopg.connect(POSTGRES_DSN) as connection, connection.cursor() as cursor:
        cursor.execute(
            """
            SELECT DISTINCT payload ->> 'event_type'
            FROM run_events
            WHERE run_id = %s AND harness_attempt_id IS NOT NULL
            """,
            (run_id,),
        )
        return {str(row[0]) for row in cursor.fetchall() if row[0]}


def trace_has_required_shape(run_id: str) -> dict[str, bool]:
    import psycopg

    with psycopg.connect(POSTGRES_DSN) as connection, connection.cursor() as cursor:
        cursor.execute(
            """
            SELECT
              (SELECT count(*) FROM spans WHERE run_id = %s AND kind = 'model'),
              (SELECT count(*) FROM spans WHERE run_id = %s AND kind = 'operation'),
              (SELECT count(*) FROM usage_records WHERE run_id = %s),
              (SELECT count(*) FROM run_events WHERE run_id = %s
                 AND payload ->> 'event_type' = 'browser.observation'),
              (SELECT count(*) FROM run_events WHERE run_id = %s
                 AND payload ->> 'event_type' = 'browser.case.completed')
            """,
            (run_id,) * 5,
        )
        (
            model_spans,
            operation_spans,
            usage_records,
            observations,
            case_completed,
        ) = map(int, cursor.fetchone())

    trace = request(f"/api/v1/runs/{run_id}/trace")
    finish_span_ids = {
        span.get("span_id")
        for span in trace.get("spans", [])
        if span.get("kind") == "operation"
        and span.get("name") == "mcp__browser_harness__finish_case"
        and span.get("span_id")
    }
    linked_finish_span_ids = {
        evidence.get("span_id")
        for evidence in trace.get("operation_evidence", [])
        if evidence.get("span_id") in finish_span_ids
        and evidence.get("span_attempt_id")
    }
    has_projected_finish_step = any(
        node.get("kind") == "step"
        and node.get("parent_span_id") in linked_finish_span_ids
        for node in trace.get("derived_nodes", [])
    )
    return {
        "model_messages": model_spans >= 1,
        "browser_tools": operation_spans >= 1,
        "usage": usage_records >= 1,
        "linked_operation": bool(linked_finish_span_ids),
        "execution_steps": has_projected_finish_step,
        "observations": observations >= 1,
        "case_completed": case_completed >= 1,
    }


def run_has_failed_assertion_evidence(run_id: str) -> bool:
    import psycopg

    with psycopg.connect(POSTGRES_DSN) as connection, connection.cursor() as cursor:
        cursor.execute(
            """
            SELECT
              operation.status,
              operation.error ->> 'error',
              operation.error ->> 'detail',
              operation.span_attempt_id,
              operation.artifacts -> 'screenshot_paths'
            FROM operation_results AS operation
            JOIN span_attempts AS attempt ON attempt.id = operation.span_attempt_id
            JOIN spans AS span ON span.id = attempt.span_id
            WHERE operation.run_id = %s
              AND span.name = 'mcp__browser_harness__assert_text'
              AND operation.status = 'failed'
            ORDER BY operation.id DESC
            LIMIT 1
            """,
            (run_id,),
        )
        row = cursor.fetchone()
    if row is None:
        return False
    status, error, detail, attempt_id, screenshot_paths = row
    return (
        status == "failed"
        and error == "condition_unmet"
        and isinstance(detail, str)
        and detail.strip() != ""
        and isinstance(attempt_id, str)
        and attempt_id.strip() != ""
        and isinstance(screenshot_paths, list)
        and any(isinstance(path, str) and path.strip() != "" for path in screenshot_paths)
    )


def assert_pi_harness_path(run_id: str) -> None:
    """Assert the run went through a Pi harness attempt, not AgentServer."""
    attempt = harness_attempt_for_run(run_id)
    if not attempt["id"].startswith("hat_"):
        raise AssertionError(
            f"run {run_id} harness attempt id {attempt['id']!r} is not a hat_ id"
        )
    if not attempt["provider_attempt_id"]:
        raise AssertionError(f"run {run_id} has no provider_attempt_id")
    event_types = harness_event_types(run_id)
    missing = [t for t in HARNESS_EVENT_TYPES if t not in event_types]
    if missing:
        raise AssertionError(
            f"run {run_id} is missing harness event types: {missing}; "
            f"got={sorted(event_types)}"
        )


def assert_concurrent_isolation(run_ids: list[str]) -> None:
    """Assert concurrent runs own distinct harness attempts and artifact scopes."""
    attempts = [harness_attempt_for_run(run_id) for run_id in run_ids]
    attempt_ids = {attempt["id"] for attempt in attempts}
    provider_ids = {attempt["provider_attempt_id"] for attempt in attempts}
    if len(attempt_ids) != len(run_ids):
        raise AssertionError(
            f"concurrent runs share harness attempt ids: {attempts}"
        )
    if len(provider_ids) != len(run_ids):
        raise AssertionError(
            f"concurrent runs share provider attempt ids: {attempts}"
        )
    import psycopg

    with psycopg.connect(POSTGRES_DSN) as connection, connection.cursor() as cursor:
        cursor.execute(
            """
            SELECT operation.run_id, array_agg(DISTINCT path ORDER BY path)
            FROM operation_results AS operation
            CROSS JOIN LATERAL jsonb_array_elements_text(
              COALESCE(operation.artifacts -> 'screenshot_paths', '[]'::jsonb)
            ) AS path
            WHERE operation.run_id = ANY(%s)
              AND path <> ''
            GROUP BY run_id
            """,
            (run_ids,),
        )
        artifact_paths = {
            str(row[0]): [str(path) for path in row[1]]
            for row in cursor.fetchall()
        }
    for run_id in run_ids:
        paths = artifact_paths.get(run_id, [])
        if not paths:
            raise AssertionError(f"run {run_id} produced no scoped operation artifacts")
        for path in paths:
            if not path.startswith(f"{run_id}/"):
                raise AssertionError(
                    f"run {run_id} has unscoped operation artifact {path!r}"
                )


def assert_no_agentserver_endpoint() -> None:
    """Assert no AgentServer HTTP service is reachable on the expected ports."""
    for port in (8081, 8765, 3000):
        probe = urllib.request.Request(
            f"http://127.0.0.1:{port}/", headers={"Accept": "application/json"}
        )
        try:
            urllib.request.urlopen(probe, timeout=1)
        except (OSError, urllib.error.URLError):
            continue
        raise AssertionError(
            f"AgentServer-like endpoint reachable on port {port}; "
            "migration must not expose a network AgentServer"
        )


def run_scenario() -> dict[str, Any]:
    wait_for_stack(API_BASE_URL)
    assert_no_agentserver_endpoint()

    batch = uuid.uuid4().hex[:12]
    summary: dict[str, Any] = {"batch": batch}

    canonical = create_workspace(batch)
    canonical_status = wait_for_terminal(canonical.run_id)
    if canonical_status != "succeeded":
        raise AssertionError(
            f"canonical Pi harness run did not succeed: {canonical_status}"
        )
    assert_pi_harness_path(canonical.run_id)
    shape = trace_has_required_shape(canonical.run_id)
    missing_shape = [name for name, present in shape.items() if not present]
    if missing_shape:
        raise AssertionError(
            f"canonical run trace is missing required shape: {missing_shape}"
        )
    summary["canonical_run_id"] = canonical.run_id
    summary["canonical_status"] = canonical_status
    summary["canonical_trace_shape"] = shape

    failing = create_workspace(batch, failing=True)
    failing_status = wait_for_terminal(failing.run_id)
    if failing_status != "failed":
        raise AssertionError(
            f"failing assertion run did not fail durably: {failing_status}"
        )
    if not run_has_failed_assertion_evidence(failing.run_id):
        raise AssertionError(
            f"failed run {failing.run_id} has no durable failed assert_text evidence"
        )
    summary["failing_run_id"] = failing.run_id
    summary["failing_status"] = failing_status
    summary["failing_has_evidence"] = True

    cancel_workspace = create_workspace(batch)
    cancel_run(cancel_workspace.run_id)
    cancel_status = wait_for_terminal(cancel_workspace.run_id, timeout=120)
    if cancel_status != "cancelled":
        raise AssertionError(
            f"cancelled run did not reach cancelled status: {cancel_status}"
        )
    attempt = harness_attempt_for_run(cancel_workspace.run_id)
    if attempt["status"] not in ("cancelled", "finished"):
        raise AssertionError(
            f"cancelled run harness attempt not released: status={attempt['status']!r}"
        )
    summary["cancelled_run_id"] = cancel_workspace.run_id
    summary["cancelled_status"] = cancel_status
    summary["cancelled_harness_status"] = attempt["status"]

    with concurrent.futures.ThreadPoolExecutor(max_workers=2) as executor:
        concurrent = list(
            executor.map(
                lambda index: create_workspace(f"{batch}-concurrent-{index}"),
                range(2),
            )
        )
    for run in concurrent:
        wait_for_terminal(run.run_id)
    assert_concurrent_isolation([run.run_id for run in concurrent])
    summary["concurrent_run_ids"] = [run.run_id for run in concurrent]

    rerun_response = request(
        f"/api/v1/runs/{canonical.run_id}/reruns",
        method="POST",
        body={"idempotency_key": f"pi-harness-rerun-{batch}"},
        expected=202,
    )
    rerun_id = str(rerun_response["run_id"])
    rerun_status = wait_for_terminal(rerun_id)
    if rerun_status != "succeeded":
        raise AssertionError(f"rerun did not succeed: {rerun_status}")
    source_attempt = harness_attempt_for_run(canonical.run_id)
    rerun_attempt = harness_attempt_for_run(rerun_id)
    if source_attempt["id"] == rerun_attempt["id"]:
        raise AssertionError("rerun reused the source harness attempt id")
    if rerun_attempt["attempt_number"] != 1:
        raise AssertionError(
            f"rerun harness attempt number is {rerun_attempt['attempt_number']}, "
            "expected 1 for a fresh run"
        )
    summary["rerun_run_id"] = rerun_id
    summary["rerun_status"] = rerun_status
    summary["rerun_fresh_attempt"] = (
        source_attempt["id"] != rerun_attempt["id"]
        and rerun_attempt["attempt_number"] == 1
    )

    return summary


def main() -> int:
    try:
        summary = run_scenario()
    except Exception as error:
        print(f"pi harness scenario failed: {error}", file=sys.stderr)
        return 1
    print("pi harness scenario passed: " + json.dumps(summary, sort_keys=True, default=str))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
