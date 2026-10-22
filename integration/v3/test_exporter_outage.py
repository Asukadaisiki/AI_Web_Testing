"""Live exporter outage resilience proof."""

from __future__ import annotations

import unittest

from failure_drills import FailureDrills


class ExporterOutageTest(unittest.TestCase):
    def test_exporter_outage_leaves_local_run_complete_and_retries_export(self) -> None:
        FailureDrills().exporter_outage()


if __name__ == "__main__":
    unittest.main()
