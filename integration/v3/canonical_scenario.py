"""Run the deterministic AgentTrace v3 browser evaluation through Compose."""

from __future__ import annotations

import json
import os
import socket
import sys
import time
import urllib.error
import urllib.request
import uuid
from pathlib import Path
from typing import Any

from assert_postgres import assert_canonical_run
from wait_for_stack import request_json, wait_for_stack

ROOT = Path(__file__).resolve().parents[2]
API_BASE_URL = os.environ.get("AGENTTRACE_API_URL", "http://127.0.0.1:8080").rstrip("/")
POSTGRES_DSN = os.environ.get(
    "AGENTTRACE_POSTGRES_DSN",
    "postgres://agenttrace:agenttrace@127.0.0.1:5433/agenttrace?sslmode=disable",
)
GOAL = (
    "Filter the local catalog for alpha, open its detail page, add two items to "
    "the cart, and verify the cart summary."
)
EXPECTED_REPORT = "Report: the grounded local storefront case passed with browser evidence."
EXPECTED_FINAL = "Final: Alpha x 2 is in the cart."
SSE_IDLE_TIMEOUT = 20
SSE_RETRY_DELAY = 0.1


def request_api(
    path: str,
    *,
    method: str = "GET",
    body: dict[str, Any] | None = None,
    timeout: float = 10,
) -> tuple[int, dict[str, Any]]:
    payload = None if body is None else json.dumps(body).encode("utf-8")
    request = urllib.request.Request(
        f"{API_BASE_URL}{path}",
        data=payload,
        method=method,
        headers={"Accept": "application/json", "Content-Type": "application/json"},
    )
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            raw = response.read()
            return response.status, json.loads(raw) if raw else {}
    except urllib.error.HTTPError as error:
        raw = error.read()
        return error.code, json.loads(raw) if raw else {}


def request_bytes(path: str, *, timeout: float = 10) -> tuple[int, bytes]:
    request = urllib.request.Request(
        f"{API_BASE_URL}{path}",
        headers={"Accept": "*/*"},
    )
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            return response.status, response.read()
    except urllib.error.HTTPError as error:
        return error.code, error.read()


def require_status(
    expected: int, actual: int, body: dict[str, Any], description: str
) -> dict[str, Any]:
    if actual != expected:
        raise AssertionError(
            f"{description} returned HTTP {actual}, expected {expected}: {body}"
        )
    return body


def consume_terminal_sse(
    open_stream: Any, *, timeout: float, idle_timeout: float
) -> dict[str, Any]:
    """Read/reconnect an SSE stream until a succeeded runtime completion arrives."""
    deadline = time.monotonic() + timeout
    last_event: dict[str, Any] = {}
    last_event_id = ""
    while time.monotonic() < deadline:
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            break
        response = None
        current: dict[str, str] = {}
        try:
            response = open_stream(last_event_id, min(idle_timeout, remaining))
            while time.monotonic() < deadline:
                try:
                    raw_line = response.readline()
                except (TimeoutError, socket.timeout):
                    break
                if not raw_line:
                    break
                line = raw_line.decode("utf-8").rstrip("\r\n")
                if not line:
                    if "data" not in current:
                        continue
                    data = json.loads(current["data"])
                    last_event = {
                        "id": current.get("id", ""),
                        "event": current.get("event", ""),
                        "data": data,
                    }
                    if last_event["id"]:
                        last_event_id = last_event["id"]
                    current = {}
                    completion = data.get("payload", {})
                    if (
                        last_event["event"] == "runtime.completed"
                        and completion.get("Status", completion.get("status")) == "succeeded"
                    ):
                        return last_event
                    continue
                if line.startswith(":"):
                    continue
                field, separator, value = line.partition(":")
                if separator:
                    current[field] = value.lstrip()
        except (OSError, urllib.error.URLError, socket.timeout, TimeoutError):
            pass
        finally:
            close = getattr(response, "close", None)
            if callable(close):
                close()
        remaining = deadline - time.monotonic()
        if remaining > 0:
            time.sleep(min(SSE_RETRY_DELAY, max(idle_timeout, 0.001), remaining))
    raise AssertionError(
        f"did not reach succeeded through SSE in {timeout}s; "
        f"last event={last_event}"
    )


def wait_for_terminal_sse(run_id: str, *, timeout: float = 120) -> dict[str, Any]:
    """Consume durable SSE replay until the runtime's terminal event arrives."""
    def open_stream(last_event_id: str, read_timeout: float) -> Any:
        headers = {"Accept": "text/event-stream"}
        if last_event_id:
            headers["Last-Event-ID"] = last_event_id
        request = urllib.request.Request(
            f"{API_BASE_URL}/api/v1/runs/{run_id}/events", headers=headers
        )
        return urllib.request.urlopen(request, timeout=read_timeout)

    try:
        return consume_terminal_sse(
            open_stream, timeout=timeout, idle_timeout=SSE_IDLE_TIMEOUT
        )
    except (OSError, urllib.error.URLError, json.JSONDecodeError) as error:
        raise AssertionError(
            f"run_id={run_id} SSE stream failed: {error}"
        ) from error


def wait_for_published_completion(
    project_id: str, conversation_id: str, run_id: str, *, timeout: float = 30
) -> None:
    """Retry the structured PostgreSQL assertions while the API relay publishes."""
    deadline = time.monotonic() + timeout
    last_error: Exception | None = None
    while time.monotonic() < deadline:
        try:
            assert_canonical_run(
                POSTGRES_DSN,
                project_id=project_id,
                conversation_id=conversation_id,
                run_id=run_id,
            )
            return
        except AssertionError as error:
            last_error = error
            time.sleep(0.5)
    raise AssertionError(
        f"run_id={run_id} PostgreSQL assertions did not converge: {last_error}"
    )


def assert_api_outcome(project_id: str, conversation_id: str, run_id: str) -> None:
    status, run = request_api(f"/api/v1/runs/{run_id}")
    require_status(200, status, run, "get run")
    if run.get("status") != "succeeded":
        raise AssertionError(f"run_id={run_id} has non-success status: {run}")
    if run.get("project_id") != project_id or run.get("conversation_id") != conversation_id:
        raise AssertionError(f"run_id={run_id} is not scoped to the created workspace: {run}")

    status, conversation = request_api(f"/api/v1/conversations/{conversation_id}")
    require_status(200, status, conversation, "get conversation")
    messages = conversation.get("messages", [])
    if len(messages) != 2 or [message.get("role") for message in messages] != [
        "user",
        "assistant",
    ]:
        raise AssertionError(f"run_id={run_id} expected user/assistant messages: {messages}")
    if messages[1].get("content") != EXPECTED_FINAL:
        raise AssertionError(
            f"run_id={run_id} assistant final mismatch: {messages[1]!r}"
        )

    status, trace = request_api(f"/api/v1/runs/{run_id}/trace")
    require_status(200, status, trace, "get trace")
    spans = trace.get("spans", [])
    roots = [span for span in spans if span.get("kind") == "run"]
    root_ids = {root.get("span_id") for root in roots}
    if len(roots) != 1:
        raise AssertionError(f"run_id={run_id} expected one Trace root: {spans}")
    children = [
        span
        for span in spans
        if span.get("parent_span_id") in root_ids and span.get("kind") != "run"
    ]
    shape = [(span.get("kind"), span.get("name")) for span in children]
    expected_shape = [
        ("model", "plan"),
        ("operation", "operation"),
        ("report", "report"),
        ("model", "final"),
    ]
    if shape != expected_shape:
        raise AssertionError(
            f"run_id={run_id} canonical trace shape={shape}, want {expected_shape}"
        )
    report = children[2]
    if EXPECTED_REPORT not in json.dumps(report.get("output", {}), sort_keys=True):
        raise AssertionError(f"run_id={run_id} report summary missing from span: {report}")

    evidence = trace.get("operation_evidence", [])
    if len(evidence) != 1 or evidence[0].get("status") != "succeeded":
        raise AssertionError(f"run_id={run_id} missing successful operation evidence: {evidence}")
    screenshot_paths = evidence[0].get("artifacts", {}).get("screenshot_paths", [])
    if not screenshot_paths:
        raise AssertionError(f"run_id={run_id} operation evidence has no screenshots: {evidence}")

    status, artifact = request_bytes(f"/artifacts/{screenshot_paths[-1]}")
    if status != 200 or not artifact:
        raise AssertionError(
            f"run_id={run_id} API did not serve browser artifact "
            f"{screenshot_paths[-1]!r}: status={status}, body={artifact}"
        )

    usage = trace.get("usage", [])
    if len(usage) < 2 or usage[-1].get("total_tokens") != 0:
        raise AssertionError(f"run_id={run_id} final scripted usage is missing: {usage}")


def main() -> int:
    run_id = ""
    try:
        wait_for_stack(API_BASE_URL)

        status, project = request_api(
            "/api/v1/projects",
            method="POST",
            body={"name": "Storefront QA", "metadata": {"scenario": "canonical-v3"}},
        )
        require_status(201, status, project, "create project")
        project_id = str(project.get("id", ""))
        if not project_id:
            raise AssertionError(f"create project returned no ID: {project}")

        status, conversation = request_api(
            f"/api/v1/projects/{project_id}/conversations",
            method="POST",
            body={
                "title": "Canonical browser evaluation",
                "metadata": {"scenario": "canonical-v3"},
            },
        )
        require_status(201, status, conversation, "create conversation")
        conversation_id = str(conversation.get("id", ""))
        if not conversation_id:
            raise AssertionError(f"create conversation returned no ID: {conversation}")

        status, agents = request_api("/api/v1/agents")
        require_status(200, status, agents, "get agents")
        agent = agents.get("agents", [{}])[0]
        status, accepted = request_api(
            f"/api/v1/conversations/{conversation_id}/messages",
            method="POST",
            body={
                "content": GOAL,
                "agent_id": agent.get("id"),
                "model": {
                    "provider": agent.get("model", {}).get("provider"),
                    "name": agent.get("model", {}).get("name"),
                },
                "idempotency_key": f"canonical-{uuid.uuid4()}",
            },
        )
        require_status(202, status, accepted, "create canonical run")
        run_id = str(accepted.get("run_id", ""))
        if not run_id:
            raise AssertionError(f"create canonical run returned no run ID: {accepted}")

        wait_for_terminal_sse(run_id)
        assert_api_outcome(project_id, conversation_id, run_id)
        wait_for_published_completion(project_id, conversation_id, run_id)
    except Exception as error:
        print(f"canonical scenario failed; run_id={run_id or '<not-created>'}: {error}", file=sys.stderr)
        return 1

    print(f"canonical scenario passed; run_id={run_id}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
