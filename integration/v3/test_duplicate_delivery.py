"""Live duplicate command delivery proof."""

from __future__ import annotations

import json
import unittest

import failure_drills
from failure_drills import COMMAND_TOPIC, FailureDrills, parse_consumer_group_offsets


class DuplicateDeliveryTest(unittest.TestCase):
    def test_duplicate_command_delivery_has_one_inbox_effect_and_one_result(self) -> None:
        FailureDrills().duplicate_command_delivery()

    def test_runtime_consumer_offsets_select_the_command_topic(self) -> None:
        offsets = parse_consumer_group_offsets(
            """
GROUP              TOPIC                      PARTITION  CURRENT-OFFSET  LOG-END-OFFSET  LAG
agenttrace-runtime agenttrace.run.commands.v1 0          53              53              0
agenttrace-runtime unrelated.topic            0          99              99              0
""",
            COMMAND_TOPIC,
        )

        self.assertEqual(offsets, {0: 53})

    def test_unrelated_partition_advance_does_not_consume_injected_record(self) -> None:
        drills = FailureDrills()
        drills._runtime_consumer_offsets = lambda: {0: 54, 1: 72}  # type: ignore[method-assign]

        self.assertFalse(
            drills._runtime_consumer_record_committed(
                failure_drills.KafkaRecordLocation(partition=1, offset=72)
            )
        )

    def test_target_partition_commit_at_record_offset_does_not_consume_record(self) -> None:
        drills = FailureDrills()
        drills._runtime_consumer_offsets = lambda: {1: 72}  # type: ignore[method-assign]

        self.assertFalse(
            drills._runtime_consumer_record_committed(
                failure_drills.KafkaRecordLocation(partition=1, offset=72)
            )
        )

    def test_target_partition_commit_past_record_offset_consumes_record(self) -> None:
        drills = FailureDrills()
        drills._runtime_consumer_offsets = lambda: {1: 73}  # type: ignore[method-assign]

        self.assertTrue(
            drills._runtime_consumer_record_committed(
                failure_drills.KafkaRecordLocation(partition=1, offset=72)
            )
        )

    def test_kafka_record_parser_binds_exact_event_id_and_bounds_bad_output(self) -> None:
        payload = {"event_id": "event-123", "command": "run", "nonce": "unique"}
        output = (
            "Partition:1\tOffset:72\t"
            f"{json.dumps(payload, separators=(',', ':'))}\n"
        )

        records = failure_drills.parse_kafka_consumer_records(output)

        self.assertEqual(
            records,
            [
                (
                    failure_drills.KafkaRecordLocation(partition=1, offset=72),
                    '{"event_id":"event-123","command":"run","nonce":"unique"}',
                )
            ],
        )
        with self.assertRaisesRegex(AssertionError, "malformed Kafka record"):
            failure_drills.parse_kafka_consumer_records("x" * 10_000)

    def test_duplicate_commit_wait_timeout_diagnostics_are_bounded(self) -> None:
        drills = FailureDrills(wait_seconds=0)
        drills._diagnostics = lambda run_id="": "x" * 10_000  # type: ignore[method-assign]

        with self.assertRaises(AssertionError) as raised:
            drills._wait(
                "target duplicate record commit",
                lambda: False,
                run_id="run-123",
            )

        self.assertLessEqual(len(str(raised.exception)), 6_300)


if __name__ == "__main__":
    unittest.main()
