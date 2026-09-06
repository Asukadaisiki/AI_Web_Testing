from __future__ import annotations

import json
from pathlib import Path
import unittest

from pydantic import ValidationError

from app.schemas.executions import AgentEventReference, ExecutionReport, FailureSignal
from app.services.failure_signals import build_failure_signal


FIXTURE_PATH = Path(__file__).parents[2] / "testdata" / "failure_signal_contract.json"


class FailureSignalContractTests(unittest.TestCase):
    def test_v2_golden_mapping(self) -> None:
        fixture = json.loads(FIXTURE_PATH.read_text())
        observed_categories = set()

        for case in fixture["cases"]:
            with self.subTest(case=case["name"]):
                report = (
                    ExecutionReport.model_validate(case["report"])
                    if case["report"]
                    else None
                )
                signal = build_failure_signal(
                    report,
                    case["error_message"],
                    execution_id=case["execution_id"],
                )
                self.assertIsNotNone(signal)
                payload = signal.model_dump(mode="json")
                for key, value in case["expected"].items():
                    self.assertEqual(payload[key], value)
                observed_categories.add(payload["category"])

        self.assertEqual(
            observed_categories,
            {"configuration", "locator", "assertion", "navigation", "network", "runner"},
        )

    def test_structured_postcondition_wins_over_locator_network_and_text(self) -> None:
        report = ExecutionReport.model_validate(
            {
                "status": "failed",
                "steps": [
                    {
                        "step_index": 1,
                        "action": "click",
                        "status": "failed",
                        "condition_results": [
                            {
                                "phase": "postcondition",
                                "index": 0,
                                "type": "text_visible",
                                "expected": "Done",
                                "actual": False,
                                "status": "failed",
                                "duration_ms": 1,
                            }
                        ],
                        "action_outcome": {
                            "status": "succeeded",
                            "side_effect_state": "committed",
                        },
                        "locator_trace": {
                            "target": "Save",
                            "failure_reason": "stale locator",
                        },
                        "network_events": [
                            {
                                "url": "https://example.test/api",
                                "method": "POST",
                                "status": 503,
                            }
                        ],
                        "error_message": "TimeoutError: locator network mismatch",
                    }
                ],
            }
        )

        signal = build_failure_signal(report, None, execution_id=201)

        self.assertEqual(signal.category, "assertion")
        self.assertEqual(signal.stage, "postcondition")
        self.assertEqual(signal.code, "condition.postcondition.text_visible.failed")
        self.assertTrue(signal.side_effect_committed)
        self.assertFalse(signal.retryable)

    def test_fingerprint_is_stable_and_agent_event_is_not_fabricated(self) -> None:
        fixture = json.loads(FIXTURE_PATH.read_text())
        case = fixture["cases"][1]
        report = ExecutionReport.model_validate(case["report"])

        first = build_failure_signal(report, case["error_message"], execution_id=102)
        second = build_failure_signal(report, case["error_message"], execution_id=102)
        linked = build_failure_signal(
            report,
            case["error_message"],
            execution_id=102,
            agent_event_reference=AgentEventReference(run_id="run-real", seq=9),
        )

        self.assertEqual(first.fingerprint, second.fingerprint)
        self.assertIsNone(first.agent_event_reference)
        self.assertNotIn("agent_event_reference", first.model_dump(mode="json"))
        self.assertIn("side_effect_committed", first.model_dump(mode="json"))
        self.assertIn("agent_event_reference", linked.model_dump(mode="json"))
        self.assertEqual(linked.agent_event_reference.run_id, "run-real")
        self.assertEqual(linked.agent_event_reference.seq, 9)

    def test_network_event_wins_over_exception_and_exception_wins_over_text(self) -> None:
        network_report = ExecutionReport.model_validate(
            {
                "status": "failed",
                "steps": [
                    {
                        "step_index": 0,
                        "action": "wait_for",
                        "status": "failed",
                        "action_outcome": {
                            "status": "failed",
                            "side_effect_state": "not_applicable",
                        },
                        "network_events": [
                            {
                                "url": "https://example.test/api",
                                "method": "GET",
                                "status": 503,
                            }
                        ],
                        "error_message": "AssertionError: selector mismatch",
                    }
                ],
            }
        )
        exception_report = ExecutionReport.model_validate(
            {
                "status": "failed",
                "steps": [
                    {
                        "step_index": 0,
                        "action": "wait_for",
                        "status": "failed",
                        "action_outcome": {
                            "status": "failed",
                            "side_effect_state": "not_applicable",
                        },
                        "error_message": "AssertionError: network connection mismatch",
                    }
                ],
            }
        )

        network = build_failure_signal(network_report, None, execution_id=202)
        exception = build_failure_signal(exception_report, None, execution_id=203)

        self.assertEqual(network.category, "network")
        self.assertEqual(network.code, "network.http_5xx")
        self.assertEqual(exception.category, "assertion")
        self.assertEqual(exception.code, "exception.assertion_error")

    def test_v1_remains_readable_and_v2_requires_new_contract_fields(self) -> None:
        fixture = json.loads(FIXTURE_PATH.read_text())
        legacy = FailureSignal.model_validate(fixture["legacy_v1"])

        self.assertIsNone(legacy.schema_version)
        self.assertIsNone(legacy.side_effect_committed)
        self.assertEqual(legacy.category, "locator")

        with self.assertRaises(ValidationError):
            FailureSignal.model_validate(
                {
                    **fixture["legacy_v1"],
                    "schema_version": "failure.signal.v2",
                }
            )


if __name__ == "__main__":
    unittest.main()
