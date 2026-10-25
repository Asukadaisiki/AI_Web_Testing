"""Unit tests for focused concurrent submission and failed-Run rerun acceptance."""

from __future__ import annotations

import copy
from datetime import datetime, timedelta, timezone
import unittest

from concurrency_rerun_scenario import peak_concurrency, validate_summary


class SummaryValidationTest(unittest.TestCase):
    def test_peak_concurrency_treats_adjacent_intervals_as_non_overlapping(
        self,
    ) -> None:
        start = datetime(2026, 10, 25, tzinfo=timezone.utc)

        peak = peak_concurrency(
            [
                (start, start + timedelta(seconds=10)),
                (start + timedelta(seconds=10), start + timedelta(seconds=20)),
            ]
        )

        self.assertEqual(peak, 1)

    def test_peak_concurrency_counts_genuine_overlap(self) -> None:
        start = datetime(2026, 10, 25, tzinfo=timezone.utc)

        peak = peak_concurrency(
            [
                (start, start + timedelta(seconds=20)),
                (start + timedelta(seconds=5), start + timedelta(seconds=25)),
                (start + timedelta(seconds=10), start + timedelta(seconds=15)),
            ]
        )

        self.assertEqual(peak, 3)

    def test_peak_concurrency_ignores_empty_and_invalid_intervals(self) -> None:
        start = datetime(2026, 10, 25, tzinfo=timezone.utc)

        self.assertEqual(peak_concurrency([]), 0)

        peak = peak_concurrency(
            [
                (None, start + timedelta(seconds=10)),
                (start, None),
                (start, start),
                (start + timedelta(seconds=10), start),
            ]
        )

        self.assertEqual(peak, 0)

    def test_validation_accepts_the_required_concurrent_rerun_summary(self) -> None:
        validate_summary(
            {
                "requested_runs": 8,
                "terminal_runs": 8,
                "projects": 2,
                "conversations": 4,
                "cross_project_runs": 0,
                "cross_conversation_messages": 0,
                "duplicate_message_ordinals": 0,
                "minimum_peak_concurrency": 4,
                "peak_concurrency": 4,
                "pending_outbox": 0,
                "unprocessed_commands": 0,
                "source_status": "failed",
                "rerun_status": "succeeded",
                "rerun_count": 1,
                "user_message_count": 1,
                "lineage_valid": True,
                "source_evidence_unchanged": True,
                "rerun_evidence_disjoint": True,
                "same_key_rerun_idempotent": True,
            }
        )

    def test_validation_rejects_each_required_invariant(self) -> None:
        summary = {
            "requested_runs": 8,
            "terminal_runs": 8,
            "projects": 2,
            "conversations": 4,
            "cross_project_runs": 0,
            "cross_conversation_messages": 0,
            "duplicate_message_ordinals": 0,
            "minimum_peak_concurrency": 4,
            "peak_concurrency": 4,
            "pending_outbox": 0,
            "unprocessed_commands": 0,
            "source_status": "failed",
            "rerun_status": "succeeded",
            "rerun_count": 1,
            "user_message_count": 1,
            "lineage_valid": True,
            "source_evidence_unchanged": True,
            "rerun_evidence_disjoint": True,
            "same_key_rerun_idempotent": True,
        }
        mutations = {
            "requested_runs": 7,
            "terminal_runs": 7,
            "projects": 1,
            "conversations": 3,
            "cross_project_runs": 1,
            "cross_conversation_messages": 1,
            "duplicate_message_ordinals": 1,
            "minimum_peak_concurrency": 3,
            "peak_concurrency": 3,
            "pending_outbox": 1,
            "unprocessed_commands": 1,
            "source_status": "succeeded",
            "rerun_status": "failed",
            "rerun_count": 2,
            "user_message_count": 2,
            "lineage_valid": False,
            "source_evidence_unchanged": False,
            "rerun_evidence_disjoint": False,
            "same_key_rerun_idempotent": False,
        }

        for name, value in mutations.items():
            with self.subTest(invariant=name):
                invalid = copy.deepcopy(summary)
                invalid[name] = value
                with self.assertRaisesRegex(AssertionError, name):
                    validate_summary(invalid)


if __name__ == "__main__":
    unittest.main()
