from __future__ import annotations

import hashlib
import json
from pathlib import Path
import unittest
from unittest.mock import patch

from pydantic import ValidationError

from app.application.browser.execution import execute_browser_case
from app.schemas.dsl import DSLCase, load_canonical_dsl
from app.schemas.browser_executions import BrowserExecutionRequest
from app.schemas.executions import ExecutionReport, StepExecutionEvidence


FIXTURE_PATH = Path(__file__).parents[2] / "testdata" / "dsl_canonical_contract.json"
RESEARCH_FIXTURE_PATH = (
    Path(__file__).parents[2] / "testdata" / "dsl_research_v1_contract.json"
)


class DSLCanonicalContractTests(unittest.TestCase):
    def test_optional_locator_enums_match_go_validation(self) -> None:
        for step in (
            {"action": "click", "target": "Login"},
            {
                "action": "click",
                "target": "Login",
                "target_strategy": None,
                "locator_confidence": None,
            },
        ):
            case = DSLCase.model_validate({"name": "optional", "steps": [step]})
            self.assertIsNone(case.steps[0].target_strategy)
            self.assertIsNone(case.steps[0].locator_confidence)

        for field, value in (
            ("target_strategy", ""),
            ("target_strategy", "semantic"),
            ("target_strategy", "unknown"),
            ("locator_confidence", ""),
            ("locator_confidence", "certain"),
        ):
            with self.subTest(field=field, value=value):
                with self.assertRaises(ValidationError):
                    DSLCase.model_validate(
                        {
                            "name": "invalid",
                            "steps": [
                                {"action": "click", "target": "Login", field: value}
                            ],
                        }
                    )

    def test_input_trigger_contract_matches_go_validation(self) -> None:
        for trigger in ("absent", None, "Enter", "Tab"):
            step = {
                "action": "input",
                "target": "Search",
                "value": "Blue Top",
            }
            if trigger != "absent":
                step["trigger"] = trigger
            case = DSLCase.model_validate({"name": "trigger", "steps": [step]})
            self.assertEqual(
                case.steps[0].trigger,
                None if trigger == "absent" else trigger,
            )

        for trigger in ("", "Search Product textbox", "Escape"):
            with self.subTest(trigger=trigger), self.assertRaises(ValidationError):
                DSLCase.model_validate(
                    {
                        "name": "invalid trigger",
                        "steps": [
                            {
                                "action": "input",
                                "target": "Search",
                                "value": "Blue Top",
                                "trigger": trigger,
                            }
                        ],
                    }
                )

    def test_go_canonical_bytes_are_fully_materialized_for_python(self) -> None:
        fixture = json.loads(FIXTURE_PATH.read_text())

        case, payload = load_canonical_dsl(
            fixture["canonical_json"],
            fixture["sha256"],
            fixture["canonical_version"],
        )

        self.assertEqual(case.model_dump(mode="json"), payload)
        self.assertEqual(case.input_contract[0].required, True)
        self.assertEqual(case.steps[1].candidates[0].strategy, "css")
        self.assertEqual(case.steps[2].timeout_ms, 5000)

    def test_rejects_sha_or_default_materialization_drift(self) -> None:
        fixture = json.loads(FIXTURE_PATH.read_text())
        with self.assertRaisesRegex(ValueError, "SHA-256"):
            load_canonical_dsl(
                fixture["canonical_json"],
                "0" * 64,
                fixture["canonical_version"],
            )

        incomplete = json.dumps(
            {"name": "x", "steps": [{"action": "wait_for", "target": "x"}]},
            separators=(",", ":"),
        )
        with self.assertRaisesRegex(ValueError, "fully materialized"):
            load_canonical_dsl(
                incomplete,
                hashlib.sha256(incomplete.encode()).hexdigest(),
                fixture["canonical_version"],
            )

    def test_execution_snapshot_and_report_sha_use_authoritative_binding(self) -> None:
        fixture = json.loads(FIXTURE_PATH.read_text())
        payload = json.loads(fixture["canonical_json"])
        with patch(
            "app.application.browser.execution.execute_case_with_playwright",
            return_value=[
                StepExecutionEvidence(
                    step_index=0,
                    action="goto",
                    value="https://example.com",
                    status="passed",
                )
            ],
        ):
            result = execute_browser_case(
                BrowserExecutionRequest(
                    execution_id=91,
                    dsl_case=payload,
                    base_url="https://example.com",
                )
            )

        self.assertEqual(result.status, "passed")
        self.assertEqual(result.report["dsl_profile"], "legacy-v1")
        self.assertEqual(result.report["steps"][0]["dsl_profile"], "legacy-v1")

    def test_research_execution_preserves_action_ir_evidence(self) -> None:
        fixture = json.loads(RESEARCH_FIXTURE_PATH.read_text())
        payload = json.loads(fixture["canonical_json"])
        with patch(
            "app.application.browser.execution.execute_case_with_playwright",
            return_value=[
                StepExecutionEvidence(
                    step_index=0,
                    action="goto",
                    value="/checkout",
                    status="passed",
                )
            ],
        ):
            result = execute_browser_case(
                BrowserExecutionRequest(
                    execution_id=92,
                    dsl_case=payload,
                )
            )

        self.assertEqual(result.report["dsl_profile"], "research-v1")
        step = result.report["steps"][0]
        self.assertEqual(step["dsl_profile"], "research-v1")
        self.assertEqual(step["intent"], "Open the checkout page")
        self.assertEqual(step["idempotency"], "idempotent")
        self.assertEqual(step["declared_side_effect"], "browser_state")

    def test_execution_report_v1_is_read_with_v2_defaults(self) -> None:
        report = ExecutionReport.model_validate(
            {
                "status": "passed",
                "steps": [
                    {
                        "step_index": 0,
                        "action": "goto",
                        "status": "passed",
                        "network_events": [
                            {
                                "url": "https://example.test",
                                "method": "GET",
                                "status": 200,
                            }
                        ],
                    }
                ],
            }
        )

        self.assertEqual(report.steps[0].condition_results, [])
        self.assertEqual(report.steps[0].action_outcome.status, "unknown")
        self.assertEqual(report.steps[0].network_events[0].event_type, "response")
        self.assertIsNone(report.dsl_profile)


if __name__ == "__main__":
    unittest.main()
