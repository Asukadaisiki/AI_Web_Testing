"""Live runtime kill and restart resilience proof."""

from __future__ import annotations

import unittest

from failure_drills import FailureDrills


class RuntimeRestartTest(unittest.TestCase):
    def test_runtime_restart_completes_one_durable_effect_per_command(self) -> None:
        FailureDrills().runtime_restart()

    def test_published_command_remains_durable_while_runtime_is_stopped(self) -> None:
        drills = FailureDrills()
        drills._row = lambda _query, _values: ("queued", "2026-10-25 08:00:00+00")  # type: ignore[method-assign]

        self.assertTrue(drills._queued_durable_command("run-published"))


if __name__ == "__main__":
    unittest.main()
