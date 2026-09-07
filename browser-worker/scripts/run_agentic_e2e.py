"""Drive the official AgentRun -> approval -> execution -> report workflow."""

from __future__ import annotations

import argparse
import hashlib
from html.parser import HTMLParser
import json
import re
import sys
import time
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any, Protocol
from urllib.error import HTTPError
from urllib.parse import urlencode, urljoin
from urllib.request import Request, urlopen

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from app.schemas.dsl import DSLCase


REPOSITORY_ROOT = Path(__file__).resolve().parents[2]
RESULT_SCHEMA_VERSION = "agentic-e2e.result.v1"
TERMINAL_RUN_STATUSES = {"completed", "failed", "cancelled"}
TERMINAL_BATCH_STATUSES = {"passed", "failed", "needs_intervention", "cancelled"}
TERMINAL_JOB_STATUSES = {"passed", "failed", "needs_intervention", "cancelled"}
DEFAULT_REQUEST_TIMEOUT_SECONDS = 30.0
DEFAULT_CANCEL_GRACE_SECONDS = 10.0
CANCEL_POLL_SECONDS = 0.05
CANONICAL_GOAL = (
    "匿名访问 Automation Exercise，从 Products 页面搜索 Blue Top，确认搜索结果，"
    "进入商品详情，将数量保持为 1，加入购物车，通过加购弹层打开 View Cart，"
    "并验证购物车中商品名为 Blue Top、单价和总价均为 Rs. 500、数量为 1。"
    "不得注册、登录、结账或填写个人信息；默认不使用 Vision。"
)

_FORBIDDEN_GOAL_PATTERNS = (
    (re.compile(r"^\s*[\[{]"), "structured DSL/JSON"),
    (re.compile(r"\b(?:dsl|css|xpath|selectors?|candidates?)\b", re.IGNORECASE), "execution hints"),
    (re.compile(r"#[A-Za-z_][\w-]*"), "CSS id selector"),
    (re.compile(r"(?:^|\s)//?[A-Za-z*][\w-]*(?:/|\[)"), "XPath"),
    (re.compile(r"\[[\w:-]+\s*[*^$|~]?="), "CSS attribute selector"),
    (re.compile(r"\b[A-Za-z][\w-]*\.[A-Za-z_][\w-]*"), "CSS class selector"),
    (
        re.compile(
            r'"(?:action|steps|target_strategy|locator_confidence|candidates)"\s*:'
        ),
        "DSL fields",
    ),
)


class AgenticE2EError(RuntimeError):
    def __init__(
        self,
        message: str,
        *,
        diagnostic: dict[str, Any] | None = None,
    ) -> None:
        super().__init__(message)
        self.diagnostic = diagnostic or {}


class AgenticClient(Protocol):
    def create_project(self, name: str) -> dict[str, Any]: ...
    def create_session(
        self, project_id: int, clean_context: bool
    ) -> dict[str, Any]: ...
    def list_batches(self, project_id: int) -> list[dict[str, Any]]: ...
    def get_batch(self, batch_id: int) -> dict[str, Any]: ...
    def cancel_batch(self, batch_id: int) -> dict[str, Any]: ...
    def start_run(self, session_id: int, goal: str) -> dict[str, Any]: ...
    def get_run(self, run_id: str) -> dict[str, Any]: ...
    def cancel_run(self, run_id: str, reason: str) -> dict[str, Any]: ...
    def stream_events(self, run_id: str, after_seq: int) -> list[dict[str, Any]]: ...
    def list_events(self, run_id: str, after_seq: int) -> list[dict[str, Any]]: ...
    def approve(self, run_id: str, tool_call_id: str) -> dict[str, Any]: ...
    def get_report(self, batch_id: int) -> dict[str, Any]: ...
    def get_artifact(self, artifact_url: str) -> bytes: ...


class HTTPAgenticClient:
    def __init__(
        self,
        *,
        agent_url: str,
        browser_url: str,
        request_timeout: float = DEFAULT_REQUEST_TIMEOUT_SECONDS,
        stream_timeout: float = 180,
        stream_window_seconds: float = 2,
    ) -> None:
        self.agent_url = agent_url.rstrip("/") + "/"
        self.browser_url = browser_url.rstrip("/") + "/"
        self.request_timeout = request_timeout
        self.stream_timeout = stream_timeout
        self.stream_window_seconds = stream_window_seconds
        self.deadline_monotonic: float | None = None

    def set_run_deadline(self, deadline_monotonic: float | None) -> None:
        self.deadline_monotonic = deadline_monotonic

    def _bounded_timeout(self, requested: float | None = None) -> float:
        timeout = self.request_timeout if requested is None else requested
        if self.deadline_monotonic is None:
            return timeout
        remaining = self.deadline_monotonic - time.monotonic()
        if remaining <= 0:
            raise TimeoutError("agentic E2E absolute deadline exceeded")
        return min(timeout, remaining)

    def _json(
        self,
        method: str,
        path: str,
        payload: dict[str, Any] | None = None,
        *,
        timeout: float | None = None,
    ) -> Any:
        body = None if payload is None else json.dumps(payload).encode()
        request = Request(
            urljoin(self.agent_url, path.lstrip("/")),
            data=body,
            method=method,
            headers={"Content-Type": "application/json"},
        )
        try:
            with urlopen(
                request, timeout=self._bounded_timeout(timeout)
            ) as response:
                return json.loads(response.read())
        except HTTPError as exc:
            detail = exc.read().decode(errors="replace")
            raise AgenticE2EError(f"{method} {path} failed: {exc.code} {detail}") from exc

    def create_project(self, name: str) -> dict[str, Any]:
        return self._json("POST", "/api/v2/projects", {"name": name})

    def create_session(
        self, project_id: int, clean_context: bool
    ) -> dict[str, Any]:
        return self._json(
            "POST",
            "/api/v2/planning/sessions",
            {
                "project_id": project_id,
                "clean_context": clean_context,
            },
        )

    def list_batches(self, project_id: int) -> list[dict[str, Any]]:
        query = urlencode({"project_id": project_id, "limit": 100})
        return self._json("GET", f"/api/v2/execution-batches?{query}")

    def get_batch(self, batch_id: int) -> dict[str, Any]:
        return self._json("GET", f"/api/v2/execution-batches/{batch_id}")

    def cancel_batch(self, batch_id: int) -> dict[str, Any]:
        return self._json(
            "POST", f"/api/v2/execution-batches/{batch_id}/cancel"
        )

    def start_run(self, session_id: int, goal: str) -> dict[str, Any]:
        return self._json(
            "POST",
            "/api/v2/agent/runs",
            {"session_id": session_id, "message": goal},
        )

    def get_run(self, run_id: str) -> dict[str, Any]:
        return self._json("GET", f"/api/v2/agent/runs/{run_id}")

    def cancel_run(self, run_id: str, reason: str) -> dict[str, Any]:
        return self._json(
            "POST",
            f"/api/v2/agent/runs/{run_id}/cancel",
            {"reason": reason},
        )

    def stream_events(self, run_id: str, after_seq: int) -> list[dict[str, Any]]:
        query = urlencode({"after_seq": after_seq})
        request = Request(
            urljoin(
                self.agent_url,
                f"/api/v2/agent/runs/{run_id}/events/stream?{query}",
            )
        )
        events: list[dict[str, Any]] = []
        data_lines: list[str] = []
        deadline = time.monotonic() + self.stream_window_seconds
        with urlopen(
            request,
            timeout=self._bounded_timeout(
                min(self.stream_timeout, self.stream_window_seconds)
            ),
        ) as response:
            for raw_line in response:
                if time.monotonic() >= deadline:
                    return events
                line = raw_line.decode("utf-8").rstrip("\r\n")
                if line.startswith("data:"):
                    data_lines.append(line[5:].lstrip())
                elif not line and data_lines:
                    event = json.loads("\n".join(data_lines))
                    events.append(event)
                    data_lines = []
                    if event.get("type") in {
                        "tool.pending",
                        "run.finished",
                        "run.failed",
                        "run.cancelled",
                    }:
                        return events
        if data_lines:
            events.append(json.loads("\n".join(data_lines)))
        return events

    def list_events(self, run_id: str, after_seq: int) -> list[dict[str, Any]]:
        query = urlencode({"after_seq": after_seq})
        payload = self._json(
            "GET", f"/api/v2/agent/runs/{run_id}/events?{query}"
        )
        return payload["events"]

    def approve(self, run_id: str, tool_call_id: str) -> dict[str, Any]:
        return self._json(
            "POST",
            f"/api/v2/agent/runs/{run_id}/tool-calls/{tool_call_id}/resume",
            {"answers": {"approve_dsl": True}},
        )

    def get_report(self, batch_id: int) -> dict[str, Any]:
        return self._json(
            "GET", f"/api/v2/execution-batches/{batch_id}/report"
        )

    def get_artifact(self, artifact_url: str) -> bytes:
        request = Request(urljoin(self.browser_url, artifact_url.lstrip("/")))
        with urlopen(request, timeout=self._bounded_timeout()) as response:
            return response.read()


class _CartHTMLParser(HTMLParser):
    _FIELD_CLASSES = {
        "cart_description": "name",
        "cart_price": "unit_price",
        "cart_quantity": "quantity",
        "cart_total": "total_price",
    }
    _VOID_ELEMENTS = {
        "area",
        "base",
        "br",
        "col",
        "embed",
        "hr",
        "img",
        "input",
        "link",
        "meta",
        "param",
        "source",
        "track",
        "wbr",
    }

    def __init__(self) -> None:
        super().__init__(convert_charrefs=True)
        self.rows: list[dict[str, Any]] = []
        self._row: dict[str, Any] | None = None
        self._stack: list[tuple[str, str | None, str | None]] = []

    def _finish_row(self) -> None:
        if self._row is not None:
            self.rows.append(self._row)
        self._row = None
        self._stack.clear()

    def _pop_through(self, tag: str) -> None:
        for index in range(len(self._stack) - 1, -1, -1):
            if self._stack[index][0] == tag:
                del self._stack[index:]
                return

    def _current_cell(self) -> str | None:
        return next(
            (cell for _, cell, _ in reversed(self._stack) if cell),
            None,
        )

    def handle_starttag(
        self, tag: str, attrs: list[tuple[str, str | None]]
    ) -> None:
        attributes = dict(attrs)
        if tag == "tr":
            if self._row is not None:
                self._finish_row()
            row_id = attributes.get("id") or ""
            if row_id.startswith("product-"):
                self._row = {
                    "id": row_id,
                    "name": [],
                    "unit_price": [],
                    "quantity": [],
                    "total_price": [],
                }
                self._stack.append(("tr", None, None))
                return
        if self._row is None:
            return
        classes = set((attributes.get("class") or "").split())
        if tag == "td":
            self._pop_through("td")
            cell = next(
                (
                    value
                    for key, value in self._FIELD_CLASSES.items()
                    if key in classes
                ),
                None,
            )
        else:
            cell = self._current_cell()

        ancestor_tags = {entry[0] for entry in self._stack}
        capture = None
        if cell == "name" and tag == "a" and "h4" in ancestor_tags:
            capture = "name"
        elif cell == "unit_price" and tag == "p":
            capture = "unit_price"
        elif cell == "quantity" and tag == "button":
            capture = "quantity"
        elif cell == "total_price" and tag == "p":
            capture = "total_price"

        if tag not in self._VOID_ELEMENTS:
            self._stack.append((tag, cell if tag == "td" else None, capture))

    def handle_data(self, data: str) -> None:
        if self._row is None or not data.strip():
            return
        field = next(
            (capture for _, _, capture in reversed(self._stack) if capture),
            None,
        )
        if field:
            self._row[field].append(data.strip())

    def handle_endtag(self, tag: str) -> None:
        if self._row is None:
            return
        if tag == "tr":
            self._finish_row()
            return
        if tag not in self._VOID_ELEMENTS:
            self._pop_through(tag)

    def close(self) -> None:
        super().close()
        if self._row is not None:
            self._finish_row()


def validate_goal(goal: str) -> str:
    normalized = " ".join(goal.split())
    if not normalized:
        raise ValueError("goal must be non-empty natural language")
    for pattern, label in _FORBIDDEN_GOAL_PATTERNS:
        if pattern.search(normalized):
            raise ValueError(f"goal must not contain {label}")
    return normalized


def validate_canonical_search_contract(dsl_case: dict[str, Any]) -> None:
    steps = dsl_case.get("steps")
    if not isinstance(steps, list):
        raise AgenticE2EError("canonical DSL has no steps")
    for step in steps:
        if not isinstance(step, dict) or step.get("action") != "goto":
            continue
        value = str(step.get("value") or "").casefold()
        if "search=" in value:
            raise AgenticE2EError(
                "canonical DSL must perform search with input and click, not goto a search URL"
            )

    def has_selector(step: dict[str, Any], selector: str) -> bool:
        return any(
            isinstance(candidate, dict)
            and str(candidate.get("selector") or "").strip() == selector
            and bool((candidate.get("pre_features") or {}).get("verified"))
            for candidate in step.get("candidates") or []
        )

    input_index = next(
        (
            index
            for index, step in enumerate(steps)
            if isinstance(step, dict)
            and step.get("action") == "input"
            and has_selector(step, "#search_product")
        ),
        -1,
    )
    click_index = next(
        (
            index
            for index, step in enumerate(steps)
            if isinstance(step, dict)
            and step.get("action") == "click"
            and has_selector(step, "#submit_search")
        ),
        -1,
    )
    if input_index < 0 or click_index <= input_index:
        raise AgenticE2EError(
            "canonical DSL must contain verified #search_product input followed by "
            "verified #submit_search click"
        )


def oracle_expectation(mutation: str = "none") -> dict[str, str]:
    expected = {
        "name": "Blue Top",
        "unit_price": "Rs. 500",
        "quantity": "1",
        "total_price": "Rs. 500",
    }
    if mutation == "wrong-price":
        expected["unit_price"] = "Rs. 501"
    elif mutation == "wrong-product":
        expected["name"] = "Red Top"
    elif mutation != "none":
        raise ValueError(f"unsupported oracle mutation: {mutation}")
    return expected


def evaluate_cart_oracle(
    html: str,
    *,
    expected: dict[str, str] | None = None,
) -> dict[str, Any]:
    parser = _CartHTMLParser()
    parser.feed(html)
    parser.close()
    normalized_rows = [
        {
            key: (
                " ".join(" ".join(value).split())
                if isinstance(value, list)
                else value
            )
            for key, value in row.items()
        }
        for row in parser.rows
    ]
    target_rows = [row for row in normalized_rows if row["id"] == "product-1"]
    actual = target_rows[0] if len(target_rows) == 1 else {}
    expectation = expected or oracle_expectation()
    checks = {
        "single_cart_row": len(normalized_rows) == 1,
        "single_product_1": len(target_rows) == 1,
        **{
            field: actual.get(field) == value
            for field, value in expectation.items()
        },
    }
    return {
        "schema_version": "automationexercise.cart-oracle.v1",
        "passed": all(checks.values()),
        "selector": "#product-1",
        "expected": expectation,
        "actual": actual,
        "checks": checks,
        "observed_row_ids": [row["id"] for row in normalized_rows],
    }


def _merge_events(
    current: list[dict[str, Any]],
    incoming: list[dict[str, Any]],
) -> list[dict[str, Any]]:
    by_seq = {int(event["seq"]): event for event in current}
    for event in incoming:
        by_seq[int(event["seq"])] = event
    return [by_seq[seq] for seq in sorted(by_seq)]


def _wait_for_run_boundary(
    client: AgenticClient,
    run_id: str,
    events: list[dict[str, Any]],
    *,
    deadline_monotonic: float,
) -> tuple[dict[str, Any], list[dict[str, Any]]]:
    while time.monotonic() < deadline_monotonic:
        after_seq = int(events[-1]["seq"]) if events else 0
        events = _merge_events(events, client.list_events(run_id, after_seq))
        run = client.get_run(run_id)
        if run["status"] == "waiting_user" or run["status"] in TERMINAL_RUN_STATUSES:
            return run, events
        time.sleep(0.25)
    raise TimeoutError(f"agent run {run_id} exceeded its absolute deadline")


def _pending_checkpoint(
    run: dict[str, Any],
    events: list[dict[str, Any]],
) -> dict[str, Any] | None:
    pending_tool_call_id = run.get("pending_tool_call_id")
    if not pending_tool_call_id:
        return None
    return next(
        (
            event
            for event in reversed(events)
            if event.get("type") == "tool.pending"
            and event.get("tool_call_id") == pending_tool_call_id
        ),
        None,
    )


def _checkpoint_kind(event: dict[str, Any] | None) -> str | None:
    if event is None:
        return None
    questions = (event.get("payload") or {}).get("questions") or []
    if any(question.get("id") == "approve_dsl" for question in questions):
        return "approve_dsl"
    return "clarification"


def _failure_diagnostic(context: dict[str, Any]) -> dict[str, Any]:
    run = context.get("run") or {}
    events = context.get("events") or []
    pending = _pending_checkpoint(run, events)
    questions = (pending.get("payload") or {}).get("questions") if pending else []
    return {
        "ids": dict(context.get("ids") or {}),
        "run": {
            "status": run.get("status"),
            "pending_tool_call_id": run.get("pending_tool_call_id"),
            "latest_generation_id": run.get("latest_generation_id"),
            "approved_generation_id": run.get("approved_generation_id"),
        },
        "checkpoint": {
            "kind": _checkpoint_kind(pending),
            "checkpoint_id": pending.get("checkpoint_id") if pending else None,
            "tool_call_id": pending.get("tool_call_id") if pending else None,
            "questions": questions or [],
        },
        "events": {
            "count": len(events),
            "last_seq": events[-1]["seq"] if events else 0,
            "references": [
                {
                    "seq": event["seq"],
                    "type": event["type"],
                    "tool_call_id": event.get("tool_call_id"),
                }
                for event in events
                if event.get("type")
                in {
                    "tool.result",
                    "tool.pending",
                    "artifact.published",
                    "run.finished",
                    "run.failed",
                }
            ],
        },
    }


def _refresh_failure_context(
    client: AgenticClient,
    context: dict[str, Any],
) -> None:
    run_id = (context.get("ids") or {}).get("agent_run_id")
    if not run_id:
        return
    events = context.get("events") or []
    after_seq = int(events[-1]["seq"]) if events else 0
    try:
        context["events"] = _merge_events(
            events, client.list_events(str(run_id), after_seq)
        )
    except Exception:
        pass
    try:
        context["run"] = client.get_run(str(run_id))
    except Exception:
        pass
    project_id = (context.get("ids") or {}).get("project_id")
    if project_id:
        try:
            current = {
                int(batch["id"])
                for batch in client.list_batches(int(project_id))
            }
            context.setdefault("batch_tracking", {})["current_ids"] = sorted(
                current
            )
        except Exception:
            pass


def _failure_batch_ids(context: dict[str, Any]) -> list[int]:
    tracking = context.get("batch_tracking") or {}
    baseline = {int(value) for value in tracking.get("baseline_ids") or []}
    candidates = {
        int(value)
        for key in ("observed_ids", "current_ids")
        for value in tracking.get(key) or []
    }
    batch_id = (context.get("ids") or {}).get("batch_id")
    if batch_id is not None:
        candidates.add(int(batch_id))
    return sorted(candidates - baseline)


def _batch_terminal(detail: dict[str, Any]) -> bool:
    jobs = detail.get("jobs")
    return (
        detail.get("status") in TERMINAL_BATCH_STATUSES
        and isinstance(jobs, list)
        and bool(jobs)
        and all(
            isinstance(job, dict)
            and job.get("status") in TERMINAL_JOB_STATUSES
            for job in jobs
        )
    )


def _cleanup_failed_batches(
    client: AgenticClient,
    context: dict[str, Any],
    *,
    deadline: float,
) -> dict[str, Any]:
    cleanup: dict[str, Any] = {
        "batch_ids": _failure_batch_ids(context),
        "batches": [],
        "terminal_verified": True,
        "error": None,
    }
    entries: dict[int, dict[str, Any]] = {}
    for batch_id in cleanup["batch_ids"]:
        entry = {
            "batch_id": batch_id,
            "cancel_attempted": False,
            "status": None,
            "job_statuses": [],
            "terminal_verified": False,
            "error": None,
        }
        cleanup["batches"].append(entry)
        entries[batch_id] = entry
        try:
            detail = client.get_batch(batch_id)
            entry["status"] = detail.get("status")
            entry["job_statuses"] = [
                job.get("status")
                for job in detail.get("jobs") or []
                if isinstance(job, dict)
            ]
            if detail.get("status") not in TERMINAL_BATCH_STATUSES:
                entry["cancel_attempted"] = True
                detail = client.cancel_batch(batch_id)
                entry["status"] = detail.get("status")
                entry["job_statuses"] = [
                    job.get("status")
                    for job in detail.get("jobs") or []
                    if isinstance(job, dict)
                ]
            entry["terminal_verified"] = _batch_terminal(detail)
        except Exception as exc:
            entry["error"] = str(exc)

    while entries and not all(
        entry["terminal_verified"] or entry["error"]
        for entry in entries.values()
    ):
        if time.monotonic() >= deadline:
            break
        for batch_id, entry in entries.items():
            if entry["terminal_verified"] or entry["error"]:
                continue
            try:
                detail = client.get_batch(batch_id)
                entry["status"] = detail.get("status")
                entry["job_statuses"] = [
                    job.get("status")
                    for job in detail.get("jobs") or []
                    if isinstance(job, dict)
                ]
                entry["terminal_verified"] = _batch_terminal(detail)
            except Exception as exc:
                entry["error"] = str(exc)
        if not all(
            entry["terminal_verified"] or entry["error"]
            for entry in entries.values()
        ):
            time.sleep(
                min(CANCEL_POLL_SECONDS, max(0, deadline - time.monotonic()))
            )

    for entry in entries.values():
        if not entry["terminal_verified"] and not entry["error"]:
            entry["error"] = (
                "cancel grace expired before batch and jobs reached terminal states"
            )
    errors = [
        f"batch {entry['batch_id']}: {entry['error']}"
        for entry in entries.values()
        if entry["error"]
    ]
    cleanup["terminal_verified"] = not errors
    cleanup["error"] = "; ".join(errors) or None
    return cleanup


def _cancel_failed_run(
    client: AgenticClient,
    context: dict[str, Any],
    diagnostic: dict[str, Any],
    cause: Exception,
    *,
    cancel_grace_seconds: float,
) -> None:
    run_id = (context.get("ids") or {}).get("agent_run_id")
    run_status = (context.get("run") or {}).get("status")
    cancellation = {
        "attempted": False,
        "reason": None,
        "status": run_status,
        "terminal_verified": run_status in TERMINAL_RUN_STATUSES,
        "no_subsequent_events": None,
        "last_seq": (
            context["events"][-1]["seq"] if context.get("events") else 0
        ),
        "error": None,
    }
    prefix = "driver timeout" if isinstance(cause, TimeoutError) else "driver error"
    reason = f"{prefix}: {cause}"
    deadline = time.monotonic() + cancel_grace_seconds
    batch_cleanup = _cleanup_failed_batches(
        client,
        context,
        deadline=deadline,
    )
    diagnostic["batch_cleanup"] = batch_cleanup
    diagnostic["cancellation"] = cancellation
    if not run_id:
        cancellation["error"] = "agent run was not created"
        diagnostic["cleanup_success"] = (
            batch_cleanup["terminal_verified"] and not cancellation["error"]
        )
        return

    cancellation["reason"] = reason
    try:
        if run_status in {"running", "waiting_user"}:
            cancellation["attempted"] = True
            run = client.cancel_run(str(run_id), reason)
            cancellation["status"] = run.get("status")
        events = context.get("events") or []
        terminal_seq: int | None = None
        while True:
            after_seq = int(events[-1]["seq"]) if events else 0
            incoming = client.list_events(str(run_id), after_seq)
            if terminal_seq is not None:
                unexpected = [
                    event
                    for event in incoming
                    if event.get("type")
                    not in {"run.finished", "run.failed", "run.cancelled"}
                ]
                if unexpected:
                    cancellation["no_subsequent_events"] = False
                    cancellation["error"] = (
                        "events observed after terminal status: "
                        + ", ".join(str(event.get("seq")) for event in unexpected)
                    )
                    break
            events = _merge_events(events, incoming)
            context["events"] = events
            cancellation["last_seq"] = (
                events[-1]["seq"] if events else after_seq
            )
            run = client.get_run(str(run_id))
            context["run"] = run
            cancellation["status"] = run.get("status")
            if cancellation["status"] in TERMINAL_RUN_STATUSES:
                cancellation["terminal_verified"] = True
                if terminal_seq is None or incoming:
                    terminal_seq = int(events[-1]["seq"]) if events else 0
                elif not incoming:
                    cancellation["no_subsequent_events"] = True
                    break
            if time.monotonic() >= deadline:
                missing = []
                if not cancellation["terminal_verified"]:
                    missing.append("terminal status")
                if cancellation["no_subsequent_events"] is not True:
                    missing.append("event quiescence")
                cancellation["error"] = (
                    "cancel grace expired before " + " and ".join(missing)
                )
                break
            time.sleep(min(CANCEL_POLL_SECONDS, max(0, deadline - time.monotonic())))
    except Exception as cancel_error:
        cancellation["error"] = str(cancel_error)
    diagnostic["cleanup_success"] = (
        batch_cleanup["terminal_verified"]
        and cancellation["terminal_verified"]
        and cancellation["no_subsequent_events"] is True
        and cancellation["error"] is None
    )


def _artifact(
    events: list[dict[str, Any]],
    artifact_type: str,
    artifact_id: int | None = None,
) -> dict[str, Any]:
    for event in reversed(events):
        payload = event.get("payload") or {}
        if (
            event.get("type") == "artifact.published"
            and payload.get("type") == artifact_type
            and (
                artifact_id is None
                or int(payload.get("id", 0)) == artifact_id
            )
        ):
            return event
    raise AgenticE2EError(f"missing {artifact_type} artifact")


def _generation_result(
    events: list[dict[str, Any]],
    generation_id: int,
) -> dict[str, Any]:
    for event in reversed(events):
        if event.get("type") != "tool.result":
            continue
        payload = event.get("payload") or {}
        content = payload.get("content")
        if (
            payload.get("tool") == "generate_dsl"
            and isinstance(content, dict)
            and int(content.get("generation_id", 0)) == generation_id
        ):
            return content
    raise AgenticE2EError(
        f"missing generate_dsl result for generation {generation_id}"
    )


def _validate_batch_binding(
    client: AgenticClient,
    events: list[dict[str, Any]],
    approval: dict[str, Any],
    batch_id: int,
) -> dict[str, Any]:
    _artifact(events, "execution_batch", batch_id)
    _artifact(events, "execution_report", batch_id)
    report = client.get_report(batch_id)
    if report.get("status") not in TERMINAL_BATCH_STATUSES:
        raise AgenticE2EError(f"batch {batch_id} report is not terminal")
    jobs = report.get("jobs") or []
    execution = jobs[0].get("latest_execution") if len(jobs) == 1 else None
    if not isinstance(execution, dict):
        raise AgenticE2EError(f"batch {batch_id} has no single execution")
    if execution.get("dsl_sha256") != approval["dsl_sha256"]:
        raise AgenticE2EError(
            f"batch {batch_id} DSL SHA does not match generation "
            f"{approval['generation_id']}"
        )
    return report


def _go_json_sha256(payload: dict[str, Any]) -> str:
    encoded = json.dumps(
        payload, ensure_ascii=False, sort_keys=True, separators=(",", ":")
    )
    encoded = (
        encoded.replace("&", "\\u0026")
        .replace("<", "\\u003c")
        .replace(">", "\\u003e")
        .replace("\u2028", "\\u2028")
        .replace("\u2029", "\\u2029")
    )
    return hashlib.sha256(encoded.encode()).hexdigest()


def _formal_result(
    run: dict[str, Any],
    report: dict[str, Any],
    *,
    generation_id: int,
    dsl_sha256: str,
) -> tuple[dict[str, Any], int, int]:
    jobs = report.get("jobs") or []
    execution = jobs[0].get("latest_execution") if len(jobs) == 1 else None
    if not isinstance(execution, dict):
        raise AgenticE2EError("formal report has no single latest execution")
    steps = (execution.get("report") or {}).get("steps") or []
    checks = {
        "run_completed": run.get("status") == "completed",
        "batch_passed": report.get("status") == "passed",
        "single_job": len(jobs) == 1,
        "job_passed": jobs[0].get("status") == "passed",
        "execution_passed": execution.get("status") == "passed",
        "generation_approved": run.get("approved_generation_id") == generation_id,
        "dsl_sha256_bound": execution.get("dsl_sha256") == dsl_sha256,
        "all_steps_have_evidence": bool(steps)
        and all(
            step.get("status") == "passed"
            and step.get("url")
            and step.get("screenshot_url")
            for step in steps
        ),
        "final_url": str(execution.get("latest_url") or "").endswith("/view_cart"),
        "vision_disabled": not any(
            bool(step.get("vlm_preverify_used")) for step in steps
        ),
    }
    return (
        {
            "passed": all(checks.values()),
            "checks": checks,
            "report_schema_version": execution.get("report_schema_version"),
            "status": report.get("status"),
            "step_count": len(steps),
        },
        int(jobs[0]["id"]),
        int(execution["id"]),
    )


def _require_deadline(deadline_monotonic: float, operation: str) -> None:
    if time.monotonic() >= deadline_monotonic:
        raise TimeoutError(
            f"agentic E2E absolute deadline exceeded before {operation}"
        )


def _set_client_deadline(
    client: AgenticClient,
    deadline_monotonic: float | None,
) -> None:
    setter = getattr(client, "set_run_deadline", None)
    if callable(setter):
        setter(deadline_monotonic)


def _run_agentic_goal(
    goal: str,
    *,
    client: AgenticClient,
    timeout_seconds: float = 900,
    mutation: str = "none",
    clean_context: bool,
    project_id: int | None,
    started_at: datetime,
    deadline_monotonic: float,
    deadline_at: datetime,
    context: dict[str, Any],
) -> dict[str, Any]:
    goal = validate_goal(goal)
    _require_deadline(deadline_monotonic, "project setup")
    project_origin = "existing"
    if project_id is None:
        project = client.create_project(
            f"Agentic E2E {started_at:%Y%m%dT%H%M%S%fZ}"
        )
        project_id = int(project["id"])
        project_origin = "created"
    elif project_id < 1:
        raise ValueError("project_id must be a positive integer")
    context["ids"]["project_id"] = project_id
    _require_deadline(deadline_monotonic, "planning session creation")
    session_payload = client.create_session(project_id, clean_context)
    session = session_payload.get("session", session_payload)
    session_id = int(session["id"])
    context["ids"].update(
        {
            "session_id": session_id,
            "planning_session_id": session_id,
            "browser_session_id": session_id,
            "browser_context_key": session_id,
        }
    )
    _require_deadline(deadline_monotonic, "baseline batch listing")
    baseline_batch_ids = {
        int(batch["id"]) for batch in client.list_batches(project_id)
    }
    context["batch_tracking"] = {
        "baseline_ids": sorted(baseline_batch_ids),
        "observed_ids": sorted(baseline_batch_ids),
    }

    _require_deadline(deadline_monotonic, "agent run creation")
    run = client.start_run(session_id, goal)
    run_id = str(run["id"])
    context["ids"]["agent_run_id"] = run_id
    context["run"] = run
    events: list[dict[str, Any]] = []
    approvals: list[dict[str, Any]] = []
    batch_rounds: list[dict[str, Any]] = []
    recoveries: list[dict[str, Any]] = []
    observed_batch_ids = set(baseline_batch_ids)
    final_report: dict[str, Any] | None = None

    while True:
        run, events = _wait_for_run_boundary(
            client,
            run_id,
            events,
            deadline_monotonic=deadline_monotonic,
        )
        context["run"] = run
        context["events"] = events
        pending: dict[str, Any] | None = None
        _require_deadline(deadline_monotonic, "batch reconciliation")
        current_batch_ids = {
            int(batch["id"]) for batch in client.list_batches(project_id)
        }
        context["batch_tracking"]["observed_ids"] = sorted(
            current_batch_ids
        )

        if approvals and "batch_id" not in approvals[-1]:
            preceding_approval = approvals[-1]
            new_batch_ids = current_batch_ids - observed_batch_ids
            if len(new_batch_ids) != 1:
                raise AgenticE2EError(
                    "each approval must create exactly one new formal batch"
                )
            batch_id = next(iter(new_batch_ids))
            report = _validate_batch_binding(
                client, events, preceding_approval, batch_id
            )
            preceding_approval["batch_ids_after_approval"] = sorted(
                current_batch_ids
            )
            preceding_approval["batch_id"] = batch_id
            preceding_approval["batch_status"] = report.get("status")
            batch_rounds.append(
                {
                    "round": len(batch_rounds) + 1,
                    "generation_id": preceding_approval["generation_id"],
                    "dsl_sha256": preceding_approval["dsl_sha256"],
                    "batch_id": batch_id,
                    "status": report.get("status"),
                }
            )
            if report.get("status") != "passed":
                recoveries.append(
                    {
                        "from_generation_id": preceding_approval["generation_id"],
                        "failed_batch_id": batch_id,
                        "failed_batch_status": report.get("status"),
                    }
                )
            final_report = report
            observed_batch_ids = current_batch_ids

            approved_generation_id = run.get("approved_generation_id")
            if approved_generation_id != preceding_approval["generation_id"]:
                if run.get("status") == "waiting_user":
                    pending = _pending_checkpoint(run, events)
                latest_generation_id = int(
                    run.get("latest_generation_id") or 0
                )
                is_next_generation_approval = (
                    pending is not None
                    and _checkpoint_kind(pending) == "approve_dsl"
                    and latest_generation_id
                    > preceding_approval["generation_id"]
                    and approved_generation_id is None
                )
                if not is_next_generation_approval:
                    raise AgenticE2EError(
                        "run approval is not bound to the preceding generation"
                    )
        elif current_batch_ids != baseline_batch_ids:
            raise AgenticE2EError("a formal batch was created before DSL approval")

        if run.get("status") != "waiting_user":
            break
        if pending is None:
            pending = _pending_checkpoint(run, events)
        if _checkpoint_kind(pending) != "approve_dsl":
            raise AgenticE2EError("run requires clarification before DSL approval")

        generation_id = int(run.get("latest_generation_id") or 0)
        if generation_id < 1:
            raise AgenticE2EError("approval checkpoint has no latest generation")
        if approvals:
            if generation_id <= approvals[-1]["generation_id"]:
                raise AgenticE2EError(
                    "new approval checkpoint did not advance the generation"
                )
            if pending.get("checkpoint_id") in {
                approval["checkpoint_id"] for approval in approvals
            } or pending.get("tool_call_id") in {
                approval["tool_call_id"] for approval in approvals
            }:
                raise AgenticE2EError(
                    "new approval reused a preceding checkpoint"
                )
        generated = _generation_result(events, generation_id)
        dsl_artifact = _artifact(events, "dsl_generation", generation_id)
        dsl_case = generated.get("case")
        DSLCase.model_validate(dsl_case)
        validate_canonical_search_contract(dsl_case)
        dsl_sha256 = _go_json_sha256(dsl_case)
        declared_sha = generated.get("dsl_sha256")
        if declared_sha and declared_sha != dsl_sha256:
            raise AgenticE2EError(
                f"generation {generation_id} declared an invalid DSL SHA"
            )
        approval = {
            "round": len(approvals) + 1,
            "checkpoint_id": pending.get("checkpoint_id"),
            "tool_call_id": pending.get("tool_call_id"),
            "generation_id": generation_id,
            "dsl_sha256": dsl_sha256,
            "artifact_event_seq": dsl_artifact["seq"],
            "batch_ids_before_approval": sorted(current_batch_ids),
        }
        approvals.append(approval)
        if recoveries:
            recoveries[-1]["to_generation_id"] = generation_id
            recoveries[-1]["approval_round"] = len(approvals)
        context["ids"]["generation_id"] = generation_id
        _require_deadline(deadline_monotonic, "DSL approval")
        client.approve(run_id, str(pending["tool_call_id"]))

    if run.get("status") != "completed":
        raise AgenticE2EError(f"run finished with status {run.get('status')}")
    if not approvals or final_report is None:
        raise AgenticE2EError("completed run has no approved formal execution")

    generation_id = approvals[-1]["generation_id"]
    dsl_sha256 = approvals[-1]["dsl_sha256"]
    batch_id = approvals[-1]["batch_id"]
    context["ids"]["batch_id"] = batch_id
    report_artifact = _artifact(events, "execution_report", batch_id)
    report = final_report
    formal, job_id, execution_id = _formal_result(
        run,
        report,
        generation_id=generation_id,
        dsl_sha256=dsl_sha256,
    )
    context["ids"]["job_id"] = job_id
    context["ids"]["execution_id"] = execution_id
    execution = report["jobs"][0]["latest_execution"]
    steps = execution["report"]["steps"]
    snapshot_url = next(
        (
            step.get("dom_snapshot_url")
            for step in reversed(steps)
            if step.get("dom_snapshot_url")
        ),
        None,
    )
    if not snapshot_url:
        raise AgenticE2EError("formal execution has no final DOM snapshot artifact")
    _require_deadline(deadline_monotonic, "oracle artifact download")
    html_bytes = client.get_artifact(snapshot_url)
    html = html_bytes.decode("utf-8")
    oracle = evaluate_cart_oracle(
        html, expected=oracle_expectation(mutation)
    )

    finished_at = datetime.now(UTC)
    return {
        "schema_version": RESULT_SCHEMA_VERSION,
        "started_at": started_at.isoformat(),
        "finished_at": finished_at.isoformat(),
        "duration_ms": max(
            0, int((finished_at - started_at).total_seconds() * 1000)
        ),
        "goal": goal,
        "configuration": {
            "clean_browser_context": clean_context,
            "project_origin": project_origin,
            "oracle_mutation": mutation,
            "timeout_mode": "absolute_monotonic_deadline",
            "run_timeout_seconds": timeout_seconds,
            "cancel_grace_seconds": context["timeout"][
                "cancel_grace_seconds"
            ],
            "request_timeout_seconds": float(
                getattr(
                    client,
                    "request_timeout",
                    DEFAULT_REQUEST_TIMEOUT_SECONDS,
                )
            ),
            "deadline_started_monotonic": context["timeout"][
                "started_monotonic"
            ],
            "deadline_monotonic": deadline_monotonic,
            "deadline_at": deadline_at.isoformat(),
        },
        "ids": {
            "project_id": project_id,
            "session_id": session_id,
            "planning_session_id": session_id,
            "browser_session_id": session_id,
            "browser_context_key": session_id,
            "agent_run_id": run_id,
            "generation_id": generation_id,
            "batch_id": batch_id,
            "job_id": job_id,
            "execution_id": execution_id,
        },
        "approval": {
            "checkpoint_id": approvals[-1]["checkpoint_id"],
            "tool_call_id": approvals[-1]["tool_call_id"],
            "generation_id": generation_id,
            "dsl_sha256": dsl_sha256,
            "batch_ids_before_approval": approvals[-1][
                "batch_ids_before_approval"
            ],
        },
        "approvals": approvals,
        "recovery": recoveries,
        "batch_rounds": batch_rounds,
        "stage0": {
            "first_pass": bool(batch_rounds)
            and batch_rounds[0]["status"] == "passed",
        },
        "events": {
            "count": len(events),
            "last_seq": events[-1]["seq"] if events else 0,
            "references": [
                {
                    "seq": event["seq"],
                    "type": event["type"],
                    "tool_call_id": event.get("tool_call_id"),
                }
                for event in events
            ],
        },
        "artifacts": {
            "dsl": {
                "type": "dsl_generation",
                "id": str(generation_id),
                "event_seq": dsl_artifact["seq"],
            },
            "report": {
                "type": "execution_report",
                "id": str(batch_id),
                "event_seq": report_artifact["seq"],
            },
            "final_dom": {
                "url": snapshot_url,
                "sha256": hashlib.sha256(html_bytes).hexdigest(),
                "size_bytes": len(html_bytes),
                "media_type": "text/html",
            },
        },
        "formal_execution": formal,
        "oracle": oracle,
        "success": (
            formal["passed"]
            and oracle["passed"]
            and bool(batch_rounds)
            and batch_rounds[0]["status"] == "passed"
        ),
    }


def run_agentic_goal(
    goal: str,
    *,
    client: AgenticClient,
    timeout_seconds: float = 900,
    mutation: str = "none",
    clean_context: bool = True,
    project_id: int | None = None,
    cancel_grace_seconds: float = DEFAULT_CANCEL_GRACE_SECONDS,
) -> dict[str, Any]:
    if timeout_seconds <= 0:
        deadline_monotonic = time.monotonic()
    else:
        deadline_monotonic = time.monotonic() + timeout_seconds
    if cancel_grace_seconds < 0:
        raise ValueError("cancel_grace_seconds must be non-negative")
    started_at = datetime.now(UTC)
    deadline_at = started_at + timedelta(seconds=max(0, timeout_seconds))
    context: dict[str, Any] = {
        "ids": {},
        "run": {},
        "events": [],
        "timeout": {
            "mode": "absolute_monotonic_deadline",
            "timeout_seconds": timeout_seconds,
            "started_monotonic": deadline_monotonic - max(0, timeout_seconds),
            "deadline_monotonic": deadline_monotonic,
            "deadline_at": deadline_at.isoformat(),
            "cancel_grace_seconds": cancel_grace_seconds,
        },
    }
    _set_client_deadline(client, deadline_monotonic)
    try:
        return _run_agentic_goal(
            goal,
            client=client,
            timeout_seconds=timeout_seconds,
            mutation=mutation,
            clean_context=clean_context,
            project_id=project_id,
            started_at=started_at,
            deadline_monotonic=deadline_monotonic,
            deadline_at=deadline_at,
            context=context,
        )
    except Exception as exc:
        _set_client_deadline(client, None)
        _refresh_failure_context(client, context)
        diagnostic = _failure_diagnostic(context)
        diagnostic["timeout"] = dict(context["timeout"])
        _cancel_failed_run(
            client,
            context,
            diagnostic,
            exc,
            cancel_grace_seconds=cancel_grace_seconds,
        )
        if isinstance(exc, AgenticE2EError):
            exc.diagnostic = {**diagnostic, **exc.diagnostic}
            raise
        raise AgenticE2EError(str(exc), diagnostic=diagnostic) from exc
    finally:
        _set_client_deadline(client, None)


def _default_output(mutation: str) -> Path:
    timestamp = datetime.now(UTC).strftime("%Y%m%dT%H%M%SZ")
    suffix = "" if mutation == "none" else f"-{mutation}"
    return (
        REPOSITORY_ROOT
        / "research"
        / "results"
        / f"agentic-e2e-{timestamp}{suffix}.json"
    )


def _failure_result(
    goal: str,
    mutation: str,
    exc: Exception,
) -> dict[str, Any]:
    result = {
        "schema_version": RESULT_SCHEMA_VERSION,
        "finished_at": datetime.now(UTC).isoformat(),
        "goal": goal,
        "configuration": {"oracle_mutation": mutation},
        "success": False,
        "error": {"type": type(exc).__name__, "message": str(exc)},
    }
    if isinstance(exc, AgenticE2EError):
        result.update(exc.diagnostic)
    return result


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("goal", help="Natural-language business goal only.")
    parser.add_argument(
        "--agent-url",
        default="http://127.0.0.1:8081",
        help="Go AgentService base URL.",
    )
    parser.add_argument(
        "--browser-url",
        default="http://127.0.0.1:8000",
        help="Browser Worker base URL used only to download evidence.",
    )
    parser.add_argument(
        "--project-id",
        type=int,
        help="Use an existing project while still creating a fresh planning/browser session.",
    )
    parser.add_argument("--timeout-seconds", type=float, default=900)
    parser.add_argument(
        "--request-timeout-seconds",
        type=float,
        default=DEFAULT_REQUEST_TIMEOUT_SECONDS,
    )
    parser.add_argument(
        "--cancel-grace-seconds",
        type=float,
        default=DEFAULT_CANCEL_GRACE_SECONDS,
    )
    parser.add_argument(
        "--oracle-mutation",
        choices=("none", "wrong-price", "wrong-product"),
        default="none",
    )
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()

    output = args.output or _default_output(args.oracle_mutation)
    try:
        result = run_agentic_goal(
            args.goal,
            client=HTTPAgenticClient(
                agent_url=args.agent_url,
                browser_url=args.browser_url,
                request_timeout=args.request_timeout_seconds,
            ),
            timeout_seconds=args.timeout_seconds,
            mutation=args.oracle_mutation,
            project_id=args.project_id,
            cancel_grace_seconds=args.cancel_grace_seconds,
        )
    except Exception as exc:
        result = _failure_result(args.goal, args.oracle_mutation, exc)
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(
        json.dumps(result, ensure_ascii=False, indent=2) + "\n",
        encoding="utf-8",
    )
    print(json.dumps({"output": str(output), **result}, ensure_ascii=False))
    return 0 if result["success"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
