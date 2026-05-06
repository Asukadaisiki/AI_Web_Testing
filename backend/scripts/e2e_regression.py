#!/usr/bin/env python3
"""E2E regression loop — feeds test_brand_filter_cart to the AI agent,
executes the generated test case, measures step success rate, and iterates
until the target rate is met or the loop limit is reached.

Usage: uv run python scripts/e2e_regression.py
"""

from __future__ import annotations

import json
import os
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path
from typing import Any

BASE_URL = os.environ.get("E2E_BASE_URL", "http://127.0.0.1:8000")
MAX_ROUNDS = 10
TARGET_PASS_RATE = 0.80
CHAT_TIMEOUT = 300  # seconds for SSE streaming chat


def _req(method: str, path: str, body: dict | None = None) -> tuple[int, Any]:
    url = f"{BASE_URL}{path}"
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req, timeout=120) as resp:
            return resp.status, json.loads(resp.read()) if resp.status != 204 else None
    except urllib.error.HTTPError as exc:
        try:
            err_body = exc.read().decode()
        except Exception:
            err_body = str(exc)
        return exc.code, err_body


def _get(path: str) -> Any:
    _, data = _req("GET", path)
    return data


def _post(path: str, body: dict) -> Any:
    _, data = _req("POST", path, body)
    return data


def _read_test_file() -> str:
    test_file = Path(__file__).resolve().parent.parent.parent / "test_brand_filter_cart"
    return test_file.read_text(encoding="utf-8")


def _stream_chat(session_id: int, content: str, timeout: int = CHAT_TIMEOUT) -> dict | None:
    """Send chat message via SSE and wait for turn_complete event."""
    url = f"{BASE_URL}/api/v1/ai-planning/sessions/{session_id}/chat"
    body = json.dumps({"content": content}).encode()
    req = urllib.request.Request(url, data=body, method="POST")
    req.add_header("Content-Type", "application/json")
    req.add_header("Accept", "text/event-stream")

    deadline = time.time() + timeout
    last_event: dict | None = None
    buffer = b""

    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            while time.time() < deadline:
                chunk = resp.read(4096)
                if not chunk:
                    break
                buffer += chunk
                # Process complete SSE events from buffer
                while b"\n" in buffer:
                    line_end = buffer.index(b"\n")
                    line = buffer[:line_end].strip()
                    buffer = buffer[line_end + 1:]

                    if not line or line.startswith(b":"):
                        continue

                    if line.startswith(b"data: "):
                        try:
                            event_data = json.loads(line[6:])
                            event_type = event_data.get("type", "")
                            if event_type == "turn_complete":
                                print(f"  SSE: turn_complete received")
                                last_event = event_data
                            elif event_type == "status":
                                phase = event_data.get("phase", "")
                                msg = event_data.get("message", "")
                                if msg:
                                    print(f"  SSE: [{phase}] {msg[:120]}")
                            elif event_type == "tool_call_start":
                                print(f"  SSE: tool_call_start {event_data.get('tool', '?')}")
                            elif event_type == "tool_call_end":
                                print(f"  SSE: tool_call_end {event_data.get('tool', '?')}")
                            elif event_type == "error":
                                print(f"  SSE: ERROR {event_data.get('message', str(event_data)[:200])}")
                        except json.JSONDecodeError:
                            pass
    except urllib.error.HTTPError as exc:
        try:
            err = exc.read().decode()
        except Exception:
            err = str(exc)
        print(f"  Chat HTTP error {exc.code}: {err[:500]}")
        return None
    except Exception as exc:
        print(f"  Chat error: {exc}")
        return None

    return last_event


def _poll_session_plan(session_id: int, max_wait: int = 120) -> str:
    """Poll session until plan is ready. Returns final status."""
    deadline = time.time() + max_wait
    while time.time() < deadline:
        time.sleep(3)
        session = _get(f"/api/v1/ai-planning/sessions/{session_id}")
        if isinstance(session, dict):
            inner = session.get("session", session)
            status = inner.get("status", "")
            if status in ("plan_ready", "drafts_ready", "completed", "error"):
                return status
    return "timeout"


def _poll_for_drafts(session_id: int, max_wait: int = 600) -> list[dict]:
    """Poll session messages until drafts are available."""
    deadline = time.time() + max_wait
    waited_for_generate = False
    while time.time() < deadline:
        time.sleep(5)
        session = _get(f"/api/v1/ai-planning/sessions/{session_id}")
        if not isinstance(session, dict):
            continue
        inner = session.get("session", session)
        status = inner.get("status", "")
        msgs = session.get("messages", [])

        for msg in msgs:
            sp = msg.get("structured_payload") or {}
            if isinstance(sp, dict) and "drafts" in sp:
                drafts = sp["drafts"]
                if any(d.get("status") in ("generated", "imported") for d in drafts):
                    print(f"  Found drafts in session messages")
                    return drafts

        # If status is plan_ready, trigger draft generation via SSE
        if status == "plan_ready" and not waited_for_generate:
            print(f"  Plan ready, generating drafts via SSE...")
            # The draft generation endpoint is also SSE-based
            # We use the regular non-SSE approach: read from session messages after request
            _post(
                f"/api/v1/ai-planning/sessions/{session_id}/drafts:generate",
                {"scenario_keys": ["all"]},
            )
            waited_for_generate = True

        print(f"  Waiting... status={status}")

        if status == "error":
            err_msg = inner.get("last_error_message", "")
            print(f"  Session error: {err_msg[:200]}")
            return []

    return []


def _extract_scenario_keys(session_id: int) -> list[str]:
    """Extract scenario keys from the session's plan."""
    session = _get(f"/api/v1/ai-planning/sessions/{session_id}")
    if not isinstance(session, dict):
        return []
    inner = session.get("session", session)
    plan = inner.get("plan") or {}
    scenarios = plan.get("scenarios", [])
    keys = [s.get("key", "") for s in scenarios if s.get("key")]
    return keys or ["all"]


def _execute_draft(session_id: int, draft_ids: list[int], input_values: dict) -> Any:
    body = {
        "draft_ids": draft_ids,
        "execute": True,
        "input_values": input_values,
    }
    return _post(f"/api/v1/ai-planning/sessions/{session_id}/drafts:save-and-execute", body)


def _get_execution_result(execution_id: int) -> dict:
    return _get(f"/api/v1/executions/{execution_id}")


def _compute_pass_rate(execution: dict) -> tuple[int, int, list[dict]]:
    steps = execution.get("step_results", [])
    if isinstance(steps, str):
        steps = json.loads(steps)
    if not steps:
        return 0, 0, []
    passed = sum(1 for s in steps if s.get("status") == "passed")
    return passed, len(steps), steps


def main() -> int:
    test_content = _read_test_file()
    print(f"=== E2E Regression: test_brand_filter_cart ===\n")
    print(f"Target: {TARGET_PASS_RATE*100:.0f}% pass rate | Max rounds: {MAX_ROUNDS}\n")

    for round_num in range(1, MAX_ROUNDS + 1):
        print(f"--- Round {round_num}/{MAX_ROUNDS} ---")

        # 1. Create session
        print("  Creating session...")
        session = _post("/api/v1/ai-planning/sessions", {})
        inner = session.get("session", session) if isinstance(session, dict) else {}
        session_id = inner.get("id") if isinstance(inner, dict) else None
        if not session_id:
            print(f"  FAIL: Could not create session: {json.dumps(session, ensure_ascii=False)[:500]}")
            continue
        print(f"  Session {session_id} created")

        # 2. Send chat via SSE and wait for turn_complete
        print("  Sending requirements via SSE...")
        chat_result = _stream_chat(session_id, test_content)
        if chat_result is None:
            print("  Chat SSE failed — skipping round")
            continue

        # 3. Wait for plan to be ready
        print("  Waiting for plan ready status...")
        plan_status = _poll_session_plan(session_id)
        print(f"  Session status: {plan_status}")

        if plan_status in ("error", "timeout"):
            print(f"  Session failed: {plan_status}")
            continue

        # 4. Trigger draft generation and wait
        print("  Waiting for drafts...")
        drafts = _poll_for_drafts(session_id)

        if not drafts:
            print("  No drafts found — skipping round")
            continue

        print(f"  Got {len(drafts)} drafts:")
        for d in drafts:
            print(f"    Draft {d.get('id')}: {d.get('scenario_title', '?')} [{d.get('status', '?')}]")

        # 5. Pick the first generated/imported draft
        usable = [d for d in drafts if d.get("status") in ("generated", "imported")]
        if not usable:
            print("  No usable drafts")
            continue
        draft = usable[0]
        draft_id = draft["id"]
        print(f"  Selected draft {draft_id}: {draft.get('scenario_title', '?')}")

        # 6. Execute
        input_values = {
            "login_email": "Xjy13302412005@outlook.com",
            "login_password": "123456",
        }
        print("  Executing...")
        exec_result = _execute_draft(session_id, [draft_id], input_values)

        if not isinstance(exec_result, dict):
            print(f"  FAIL: Execution returned: {str(exec_result)[:500]}")
            continue

        if exec_result.get("error"):
            print(f"  Execution error: {exec_result['error']}")
            continue

        summaries = exec_result.get("execution_summaries", [])
        if not summaries:
            print(f"  No execution_summaries key. Keys: {list(exec_result.keys())[:10]}")
            print(f"  Raw: {json.dumps(exec_result, ensure_ascii=False)[:500]}")
            continue

        # 7. Analyze results
        for summary in summaries:
            exec_id = summary.get("execution_id") or summary.get("id")
            if not exec_id:
                continue

            time.sleep(2)
            execution = _get_execution_result(exec_id)
            if not isinstance(execution, dict):
                print(f"  Could not fetch execution {exec_id}")
                continue

            passed, total, step_results = _compute_pass_rate(execution)
            rate = passed / total if total > 0 else 0
            print(f"\n  Result: Run {exec_id}: {passed}/{total} passed ({rate*100:.1f}%)")

            for i, s in enumerate(step_results):
                s_status = s.get("status", "?")
                if s_status != "passed":
                    action = s.get("action", "?")
                    target = s.get("target", "")
                    err = str(s.get("error", ""))[:200]
                    strat = s.get("locator_strategy", "?")
                    print(f"    FAIL Step {i}: [{action}] target={target!r} strategy={strat!r}")
                    print(f"      Error: {err}")

            print(f"  Session: http://127.0.0.1:5173/planning/{session_id}")
            print(f"  Report: http://127.0.0.1:5173/run/{exec_id}")

            if rate >= TARGET_PASS_RATE:
                print(f"\nTARGET REACHED in round {round_num}: {rate*100:.1f}%")
                return 0

        print()

    print(f"Max rounds ({MAX_ROUNDS}) reached without hitting {TARGET_PASS_RATE*100:.0f}% target")
    return 1


if __name__ == "__main__":
    sys.exit(main())
