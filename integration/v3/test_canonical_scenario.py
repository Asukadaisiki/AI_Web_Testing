"""Focused SSE waiting regressions for the canonical scenario."""

from __future__ import annotations

import urllib.error
import unittest
from unittest.mock import patch

import canonical_scenario
from canonical_scenario import assert_api_outcome, consume_terminal_sse


_HARNESS_COMPLETED_DATA = (
    b'data: {"aggregate_id":"run_7315076dac8ea9792abc259003e9dd24",'
    b'"correlation_id":"run_7315076dac8ea9792abc259003e9dd24",'
    b'"event_id":"event_83ab78e3db854232dc1844395c7ffc88",'
    b'"event_type":"harness.completed",'
    b'"occurred_at":"2026-10-27T08:40:50.241701Z",'
    b'"payload":null,"schema_version":"agenttrace.event.v1"}\n'
)


class _DelayedSSE:
    def __init__(self, lines: list[bytes | Exception]) -> None:
        self._lines = iter(lines)

    def readline(self) -> bytes:
        value = next(self._lines, b"")
        if isinstance(value, Exception):
            raise value
        return value


class _Clock:
    def __init__(self) -> None:
        self.now = 0.0

    def monotonic(self) -> float:
        return self.now

    def sleep(self, seconds: float) -> None:
        self.now += seconds


class CanonicalSSETest(unittest.TestCase):
    def test_reconnects_after_idle_timeout_and_accepts_delayed_heartbeat(self) -> None:
        streams = iter(
            [
                _DelayedSSE([TimeoutError("idle read")]),
                _DelayedSSE(
                    [
                        b": heartbeat\n",
                        b"\n",
                        b"id: 9\n",
                        b"event: harness.completed\n",
                        _HARNESS_COMPLETED_DATA,
                        b"\n",
                    ]
                ),
            ]
        )

        event = consume_terminal_sse(
            lambda _last_id, _read_timeout: next(streams), timeout=1, idle_timeout=0.1
        )

        self.assertEqual(event["id"], "9")
        self.assertEqual(event["event"], "harness.completed")

    def test_retries_open_failure_and_resumes_with_last_durable_event_id(self) -> None:
        clock = _Clock()
        attempts: list[tuple[str, float]] = []
        streams = iter(
            [
                urllib.error.URLError("connection reset"),
                _DelayedSSE(
                    [
                        b"id: 7\n",
                        b"event: runtime.progress\n",
                        b'data: {"payload":{"status":"running"}}\n',
                        b"\n",
                    ]
                ),
                _DelayedSSE(
                    [
                        b"id: 8\n",
                        b"event: harness.completed\n",
                        _HARNESS_COMPLETED_DATA,
                        b"\n",
                    ]
                ),
            ]
        )

        original_monotonic = canonical_scenario.time.monotonic
        original_sleep = canonical_scenario.time.sleep
        canonical_scenario.time.monotonic = clock.monotonic
        canonical_scenario.time.sleep = clock.sleep
        try:
            def open_stream(last_event_id: str, read_timeout: float) -> _DelayedSSE:
                attempts.append((last_event_id, read_timeout))
                result = next(streams)
                if isinstance(result, Exception):
                    raise result
                return result

            event = consume_terminal_sse(
                open_stream, timeout=0.13, idle_timeout=0.05
            )
        finally:
            canonical_scenario.time.monotonic = original_monotonic
            canonical_scenario.time.sleep = original_sleep

        self.assertEqual(event["id"], "8")
        self.assertEqual([event_id for event_id, _ in attempts], ["", "", "7"])
        self.assertEqual([round(value, 2) for _, value in attempts], [0.05, 0.05, 0.03])
        self.assertLessEqual(clock.now, 0.13)


class CanonicalPiTraceTest(unittest.TestCase):
    def test_accepts_linked_pi_finish_evidence_and_derived_steps(self) -> None:
        finish_span_id = "span-finish"
        responses = {
            "/api/v1/runs/run-1": (
                200,
                {
                    "status": "succeeded",
                    "project_id": "project-1",
                    "conversation_id": "conversation-1",
                },
            ),
            "/api/v1/conversations/conversation-1": (
                200,
                {
                    "messages": [
                        {"role": "user", "content": canonical_scenario.GOAL},
                        {
                            "role": "assistant",
                            "content": canonical_scenario.EXPECTED_FINAL,
                        },
                    ]
                },
            ),
            "/api/v1/runs/run-1/trace": (
                200,
                {
                    "spans": [
                        {"span_id": "span-run", "kind": "run", "name": "run"},
                        {
                            "span_id": "span-model",
                            "parent_span_id": "span-run",
                            "kind": "model",
                            "name": "message",
                            "status": "succeeded",
                        },
                        {
                            "span_id": "span-open",
                            "parent_span_id": "span-run",
                            "kind": "operation",
                            "name": "mcp__browser_harness__open_page",
                            "status": "succeeded",
                        },
                        {
                            "span_id": finish_span_id,
                            "parent_span_id": "span-run",
                            "kind": "operation",
                            "name": "mcp__browser_harness__finish_case",
                            "status": "succeeded",
                        },
                    ],
                    "operation_evidence": [
                        {
                            "span_id": finish_span_id,
                            "span_attempt_id": "attempt-finish",
                            "status": "passed",
                            "result": {"status": "passed", "steps": [{"index": 0}]},
                            "artifacts": {
                                "screenshot_paths": ["run-1/final.png"]
                            },
                        }
                    ],
                    "derived_nodes": [
                        {
                            "kind": "step",
                            "parent_span_id": finish_span_id,
                            "source_evidence_id": "evidence-finish",
                        }
                    ],
                    "usage": [
                        {
                            "span_id": "span-model",
                            "input_tokens": 10,
                            "output_tokens": 5,
                            "total_tokens": 15,
                        }
                    ],
                },
            ),
        }

        with (
            patch.object(
                canonical_scenario,
                "request_api",
                side_effect=lambda path: responses[path],
            ),
            patch.object(
                canonical_scenario,
                "request_bytes",
                return_value=(200, b"png"),
            ),
        ):
            assert_api_outcome("project-1", "conversation-1", "run-1")


if __name__ == "__main__":
    unittest.main()
