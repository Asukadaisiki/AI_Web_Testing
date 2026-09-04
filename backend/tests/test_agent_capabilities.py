from __future__ import annotations

import unittest
from unittest.mock import patch

from app.application.agent_capabilities.service import prepare_fix_and_retry


class AgentCapabilityContractTest(unittest.TestCase):
    def test_fix_and_retry_routes_locator_failure_to_reexplore(self) -> None:
        report = {
            "id": 7,
            "status": "failed",
            "analysis": {
                "failure_signals": [
                    {
                        "category": "locator",
                        "title": "Target not found",
                    }
                ]
            },
            "jobs": [
                {
                    "latest_execution": {
                        "id": 11,
                        "status": "failed",
                        "dsl_snapshot": {
                            "name": "Login",
                            "steps": [{"action": "click", "target": "Login"}],
                        },
                    }
                }
            ],
        }

        with patch(
            "app.application.agent_capabilities.service.get_report",
            return_value=report,
        ):
            result = prepare_fix_and_retry(
                None,  # type: ignore[arg-type]
                project_id=1,
                arguments={"batch_id": 7},
            )

        self.assertEqual(result["status"], "repair_ready")
        self.assertEqual(result["strategy"], "re_explore")
        self.assertEqual(result["source_execution_id"], 11)
        self.assertEqual(result["source_dsl"]["name"], "Login")

    def test_fix_and_retry_does_not_repair_passed_batch(self) -> None:
        with patch(
            "app.application.agent_capabilities.service.get_report",
            return_value={"id": 7, "status": "passed"},
        ):
            result = prepare_fix_and_retry(
                None,  # type: ignore[arg-type]
                project_id=1,
                arguments={"batch_id": 7},
            )

        self.assertEqual(result["status"], "not_required")
        self.assertEqual(result["strategy"], "none")

    def test_fix_and_retry_routes_assertion_failure_to_regeneration(self) -> None:
        report = {
            "id": 8,
            "status": "failed",
            "analysis": {
                "failure_signals": [
                    {
                        "category": "assertion",
                        "title": "Expected text did not match",
                    }
                ]
            },
            "jobs": [
                {
                    "latest_execution": {
                        "id": 12,
                        "status": "failed",
                        "dsl_snapshot": {
                            "name": "Heading check",
                            "steps": [{"action": "assert_text"}],
                        },
                    }
                }
            ],
        }

        with patch(
            "app.application.agent_capabilities.service.get_report",
            return_value=report,
        ):
            result = prepare_fix_and_retry(
                None,  # type: ignore[arg-type]
                project_id=1,
                arguments={"batch_id": 8},
            )

        self.assertEqual(result["status"], "repair_ready")
        self.assertEqual(result["strategy"], "regenerate_dsl")
        self.assertEqual(result["failure_signals"][0]["category"], "assertion")

    def test_fix_and_retry_uses_run_signal_before_analysis_completes(self) -> None:
        report = {
            "id": 9,
            "status": "failed",
            "analysis_status": "pending",
            "analysis": None,
            "jobs": [
                {
                    "latest_execution": {
                        "id": 13,
                        "status": "failed",
                        "failure_signal": {
                            "category": "navigation",
                            "title": "Navigation timed out",
                        },
                        "dsl_snapshot": {
                            "name": "Checkout",
                            "steps": [{"action": "goto"}],
                        },
                    }
                }
            ],
        }

        with patch(
            "app.application.agent_capabilities.service.get_report",
            return_value=report,
        ):
            result = prepare_fix_and_retry(
                None,  # type: ignore[arg-type]
                project_id=1,
                arguments={"batch_id": 9},
            )

        self.assertEqual(result["status"], "repair_ready")
        self.assertEqual(result["strategy"], "re_explore")
        self.assertEqual(result["failure_signals"][0]["category"], "navigation")

    def test_fix_and_retry_waits_for_active_batch(self) -> None:
        with patch(
            "app.application.agent_capabilities.service.get_report",
            return_value={"id": 10, "status": "running"},
        ):
            result = prepare_fix_and_retry(
                None,  # type: ignore[arg-type]
                project_id=1,
                arguments={"batch_id": 10},
            )

        self.assertEqual(result["status"], "wait_execution")
        self.assertEqual(result["strategy"], "wait")


if __name__ == "__main__":
    unittest.main()
