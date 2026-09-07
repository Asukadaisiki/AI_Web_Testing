from __future__ import annotations

import unittest
from unittest.mock import patch

from app.application.browser.execution import execute_browser_case
from app.schemas.browser_executions import BrowserExecutionRequest
from app.schemas.executions import StepExecutionEvidence


class BrowserExecutionRPCTest(unittest.TestCase):
    def test_missing_base_url_returns_report_without_database(self) -> None:
        result = execute_browser_case(
            BrowserExecutionRequest(
                execution_id=77,
                dsl_case={"name": "relative", "steps": [{"action": "goto", "value": "/"}]},
            )
        )

        self.assertEqual(result.status, "failed")
        self.assertIn("Relative goto step requires", result.error_message or "")
        self.assertEqual(result.report["status"], "failed")
        self.assertEqual(result.report["steps"][0]["dsl_profile"], "legacy-v1")
        self.assertEqual(result.failure_signal["schema_version"], "failure.signal.v2")
        self.assertEqual(result.failure_signal["source_reference"]["execution_id"], 77)

    def test_runner_steps_are_returned_as_execution_report(self) -> None:
        step = StepExecutionEvidence(
            step_index=0,
            action="goto",
            value="https://example.com",
            status="passed",
            duration_ms=1,
            screenshot_path="artifacts/executions/88/step-01.png",
        )
        with patch(
            "app.application.browser.execution.execute_case_with_playwright",
            return_value=[step],
        ) as runner:
            result = execute_browser_case(
                BrowserExecutionRequest(
                    execution_id=88,
                    dsl_case={
                        "name": "absolute",
                        "steps": [{"action": "goto", "value": "https://example.com"}],
                    },
                )
            )

        runner.assert_called_once()
        self.assertEqual(result.status, "passed")
        self.assertIsNone(result.error_message)
        self.assertIsNone(result.failure_signal)
        self.assertEqual(result.report["status"], "passed")
        self.assertEqual(
            result.report["steps"][0]["screenshot_url"],
            "/artifacts/executions/88/step-01.png",
        )


if __name__ == "__main__":
    unittest.main()
