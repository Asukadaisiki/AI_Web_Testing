"""Focused SSE waiting regressions for the canonical scenario."""

from __future__ import annotations

import unittest

from canonical_scenario import consume_terminal_sse


class _DelayedSSE:
    def __init__(self, lines: list[bytes | Exception]) -> None:
        self._lines = iter(lines)

    def readline(self) -> bytes:
        value = next(self._lines, b"")
        if isinstance(value, Exception):
            raise value
        return value


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


if __name__ == "__main__":
    unittest.main()
