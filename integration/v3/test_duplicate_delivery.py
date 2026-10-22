"""Live duplicate command delivery proof."""

from __future__ import annotations

import unittest

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


if __name__ == "__main__":
    unittest.main()
