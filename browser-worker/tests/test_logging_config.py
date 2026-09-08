from __future__ import annotations

import json
import logging
import unittest

from browser_worker.runtime.logging_config import setup_logging
from browser_worker.runtime.structured_logging import StructuredJsonFormatter


class LoggingConfigTest(unittest.TestCase):
    def setUp(self) -> None:
        self.root = logging.getLogger()
        self.browser_worker = logging.getLogger("browser_worker")
        self.root_state = (list(self.root.handlers), self.root.level)
        self.worker_state = (
            list(self.browser_worker.handlers),
            self.browser_worker.level,
            self.browser_worker.propagate,
        )

    def tearDown(self) -> None:
        self.root.handlers, self.root.level = self.root_state
        (
            self.browser_worker.handlers,
            self.browser_worker.level,
            self.browser_worker.propagate,
        ) = self.worker_state

    def test_browser_worker_logger_uses_structured_stdout(self) -> None:
        setup_logging("INFO")

        self.assertEqual(self.browser_worker.level, logging.INFO)
        self.assertFalse(self.browser_worker.propagate)
        self.assertEqual(len(self.browser_worker.handlers), 1)
        self.assertIsInstance(
            self.browser_worker.handlers[0].formatter,
            StructuredJsonFormatter,
        )

    def test_structured_formatter_preserves_execution_context(self) -> None:
        record = logging.LogRecord(
            "browser_worker.runner",
            logging.INFO,
            __file__,
            1,
            "step_complete",
            (),
            None,
        )
        record.category = "dsl_execution"
        record.event_type = "step_complete"
        record.data = {"status": "passed"}
        record.execution_id = 42

        payload = json.loads(StructuredJsonFormatter().format(record))

        self.assertEqual(payload["logger"], "browser_worker.runner")
        self.assertEqual(payload["data"], {"status": "passed"})
        self.assertEqual(payload["trace"], {"execution_id": 42})


if __name__ == "__main__":
    unittest.main()
