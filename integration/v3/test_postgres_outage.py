"""Live PostgreSQL outage rejection proof."""

from __future__ import annotations

import unittest

from failure_drills import FailureDrills


class PostgresOutageTest(unittest.TestCase):
    def test_postgres_outage_returns_503_without_creating_a_command(self) -> None:
        FailureDrills().postgres_outage()


if __name__ == "__main__":
    unittest.main()
