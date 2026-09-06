from __future__ import annotations

import hashlib
import json
from pathlib import Path
from types import SimpleNamespace
import unittest

from pydantic import ValidationError

from app.models import TestCase, TestCaseRun, User
from app.schemas.dsl import DSLCase, load_canonical_dsl
from app.schemas.executions import CaseExecutionRequest
from app.services.executions import ExecutionRunContext, _normalize_report, execute_case


FIXTURE_PATH = Path(__file__).parents[2] / "testdata" / "dsl_canonical_contract.json"


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
        session = _FakeSession(payload)

        result = execute_case(
            session,
            7,
            CaseExecutionRequest(actor_user_id=3),
            run_context=ExecutionRunContext(
                job_id=11,
                dsl_snapshot=payload,
                dsl_canonical_json=fixture["canonical_json"],
                dsl_sha256=fixture["sha256"],
                dsl_canonical_version=fixture["canonical_version"],
            ),
        )

        self.assertEqual(result.dsl_snapshot, payload)
        self.assertEqual(result.dsl_sha256, fixture["sha256"])
        self.assertEqual(session.execution.dsl_snapshot, payload)
        self.assertEqual(session.execution.dsl_sha256, fixture["sha256"])

    def test_execution_report_v1_is_read_with_v2_defaults(self) -> None:
        report = _normalize_report(
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


class _FakeSession:
    def __init__(self, dsl: dict) -> None:
        self.case = SimpleNamespace(id=7, project_id=5, name=dsl["name"], dsl=dsl)
        self.execution: TestCaseRun | None = None

    def get(self, model, record_id):
        if model is TestCase and record_id == 7:
            return self.case
        if model is User and record_id == 3:
            return object()
        if model is TestCaseRun and self.execution is not None and record_id == self.execution.id:
            return self.execution
        return None

    def add(self, record) -> None:
        if isinstance(record, TestCaseRun):
            self.execution = record

    def commit(self) -> None:
        pass

    def rollback(self) -> None:
        pass

    def refresh(self, record) -> None:
        if isinstance(record, TestCaseRun) and record.id is None:
            record.id = 91
            record.analysis_status = "pending"


if __name__ == "__main__":
    unittest.main()
