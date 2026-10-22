"""Live runtime kill and restart resilience proof."""

from __future__ import annotations

import unittest

from failure_drills import FailureDrills


class RuntimeRestartTest(unittest.TestCase):
    def test_runtime_restart_completes_one_durable_effect_per_command(self) -> None:
        FailureDrills().runtime_restart()


if __name__ == "__main__":
    unittest.main()
