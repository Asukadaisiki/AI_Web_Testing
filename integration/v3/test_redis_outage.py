"""Live Redis outage resilience proof."""

from __future__ import annotations

import unittest

from failure_drills import FailureDrills


class RedisOutageTest(unittest.TestCase):
    def test_redis_outage_uses_postgres_poll_and_keeps_trace_available(self) -> None:
        FailureDrills().redis_outage()


if __name__ == "__main__":
    unittest.main()
