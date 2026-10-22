"""Live Kafka outage resilience proof."""

from __future__ import annotations

import unittest

from failure_drills import FailureDrills


class KafkaOutageTest(unittest.TestCase):
    def test_kafka_outage_keeps_a_queued_durable_run_until_recovery(self) -> None:
        FailureDrills().kafka_outage()


if __name__ == "__main__":
    unittest.main()
