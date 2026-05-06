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


def _req(method: str, path: str, body: dict | None = None) -> tuple[int, Any]:
    url = f"{BASE_URL}{path}"
    data = json.dumps(body).encode() if body else None
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


def _wait_for_drafts(session_id: int, max_wait: int = 300) -> list[dict]:
    """Poll session messages until drafts are ready or timeout."""
    deadline = time.time() + max_wait
    while time.time() < deadline:
        time.sleep(5)
        session = _get(f"/api/v1/ai-planning/sessions/{session_id}")
        if not isinstance(session, dict):
            continue
        msgs = session.get("messages", [])
        for msg in msgs:
            sp = msg.get("structured_payload") or {}
            if isinstance(sp, dict) and "drafts" in sp:
                drafts = sp["drafts"]
                # Wait for at least one generated/imported draft
                if any(d.get("status") in ("generated", "imported") for d in drafts):
                    return drafts
        inner = session.get("session", session)
        status = inner.get("status", "")
        print(f"  Waiting for drafts... status={status}")
    return []


def _trigger_draft_generation(session_id: int) -> Any:
    """Trigger draft generation via the API."""
    return _post(
        f"/api/v1/ai-planning/sessions/{session_id}/drafts:generate",
        {"scenario_keys": [], "force": True},
    )


def _execute_draft(session_id: int, draft_ids: list[int], input_values: dict) -> dict | None:
    """Save and execute drafts, return result."""
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
        session = _post("/api/v1/ai-planning/sessions", {"title": f"E2E Regression Round {round_num}"})
        inner = session.get("session", session) if isinstance(session, dict) else {}
        session_id = inner.get("id") if isinstance(inner, dict) else None
        if not session_id:
            print(f"  FAIL: Could not create session: {json.dumps(session, ensure_ascii=False)[:500]}")
            continue
        print(f"  Session {session_id} created")

        # 2. Send test requirements
        print("  Sending requirements...")
        msg_resp = _post(
            f"/api/v1/ai-planning/sessions/{session_id}/chat",
            {"message": test_content},
        )
        if not isinstance(msg_resp, dict):
            print(f"  FAIL: chat response: {str(msg_resp)[:500]}")
            continue
        status = msg_resp.get("session_status", "?")
        print(f"  Requirements sent, status={status}")

        # 3. Wait for AI to generate plan (ReAct loop may take time)
        print("  Waiting for plan...")
        time.sleep(10)

        # 4. Trigger draft generation
        print("  Triggering draft generation...")
        draft_result = _trigger_draft_generation(session_id)
        print(f"  Draft generation triggered: {json.dumps(draft_result, ensure_ascii=False)[:300] if draft_result else 'None'}")

        # 5. Wait for drafts
        drafts = _wait_for_drafts(session_id)

        if not drafts:
            print("  No drafts found — skipping round")
            continue

        print(f"  Got {len(drafts)} drafts:")
        for d in drafts:
            print(f"    Draft {d.get('id')}: {d.get('scenario_title', '?')} [{d.get('status', '?')}]")

        # 6. Pick the first generated/imported draft
        usable = [d for d in drafts if d.get("status") in ("generated", "imported")]
        if not usable:
            print("  No usable drafts")
            continue
        draft = usable[0]
        draft_id = draft["id"]
        print(f"  Selected draft {draft_id}: {draft.get('scenario_title', '?')}")

        # 7. Execute
        input_values = {
            "login_email": "Xjy13302412005@outlook.com",
            "login_password": "123456",
        }
        print("  Executing...")
        exec_result = _execute_draft(session_id, [draft_id], input_values)
        if not isinstance(exec_result, dict):
            print(f"  FAIL: Execution returned: {str(exec_result)[:500]}")
            continue

        # Check for SSE-style streaming responses
        if exec_result.get("error"):
            print(f"  Execution error: {exec_result['error']}")
            continue

        summaries = exec_result.get("execution_summaries", [])
        if not summaries:
            # Try other keys
            print(f"  No execution_summaries key. Response keys: {list(exec_result.keys())[:10]}")
            print(f"  Raw (first 1K): {json.dumps(exec_result, ensure_ascii=False)[:1000]}")
            continue

        # 8. Get full execution details
        for summary in summaries:
            exec_id = summary.get("execution_id") or summary.get("id")
            if not exec_id:
                continue

            time.sleep(2)  # Give DB time to flush
            execution = _get_execution_result(exec_id)
            if not isinstance(execution, dict):
                print(f"  Could not fetch execution {exec_id}: {execution}")
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
