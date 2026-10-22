"""Live duplicate command delivery proof."""

from __future__ import annotations

import unittest

from failure_drills import FailureDrills


class DuplicateDeliveryTest(unittest.TestCase):
    def test_duplicate_command_delivery_has_one_inbox_effect_and_one_result(self) -> None:
        FailureDrills().duplicate_command_delivery()


if __name__ == "__main__":
    unittest.main()
