"""Live PostgreSQL outage rejection proof."""

from __future__ import annotations

import unittest

from failure_drills import FailureDrills


class PostgresOutageTest(unittest.TestCase):
    def test_postgres_outage_returns_503_without_creating_a_command(self) -> None:
        FailureDrills().postgres_outage()

    def test_rejected_request_has_no_scoped_message_run_or_command(self) -> None:
        drills = FailureDrills()
        observed: list[tuple[str, tuple[object, ...]]] = []

        def scoped_counts(query: str, values: tuple[object, ...]) -> tuple[int, int, int, int]:
            observed.append((query, values))
            return (0, 0, 0, 0)

        drills._row = scoped_counts  # type: ignore[method-assign]
        drills._assert_rejected_postgres_request_absent(
            conversation_id="conv_postgres_outage",
            idempotency_key="failure-drill-postgres-outage-idempotency",
        )

        self.assertEqual(len(observed), 1)
        query, values = observed[0]
        self.assertIn("messages", query)
        self.assertIn("runs", query)
        self.assertIn("outbox_events", query)
        self.assertEqual(
            values,
            (
                "conv_postgres_outage",
                "conv_postgres_outage",
                "conv_postgres_outage",
                "failure-drill-postgres-outage-idempotency",
            ),
        )


if __name__ == "__main__":
    unittest.main()
