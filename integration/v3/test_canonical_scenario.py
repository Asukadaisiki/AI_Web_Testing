"""Focused SSE waiting regressions for the canonical scenario."""

from __future__ import annotations

import urllib.error
import unittest

import canonical_scenario
from canonical_scenario import consume_terminal_sse


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
                        b"event: runtime.completed\n",
                        b'data: {"payload":{"status":"succeeded"}}\n',
                        b"\n",
                    ]
                ),
            ]
        )

        event = consume_terminal_sse(
            lambda _last_id, _read_timeout: next(streams), timeout=1, idle_timeout=0.1
        )

        self.assertEqual(event["id"], "9")
        self.assertEqual(event["event"], "runtime.completed")

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
                        b"event: runtime.completed\n",
                        b'data: {"payload":{"status":"succeeded"}}\n',
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


if __name__ == "__main__":
    unittest.main()
