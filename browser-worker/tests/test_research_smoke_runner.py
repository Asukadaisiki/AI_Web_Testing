from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path

from app.schemas.executions import StepExecutionEvidence
from scripts.run_research_smoke import load_goal, run_goal


class ResearchSmokeRunnerTest(unittest.TestCase):
    def test_load_goal_validates_schema(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "goal.json"
            path.write_text(
                json.dumps(
                    {
                        "schema_version": "research.goal.v1",
                        "id": "example",
                        "objective": "Open example.com",
                        "target": {"base_url": "https://example.com"},
                        "success_criteria": ["URL is correct"],
                        "dsl_case": {
                            "name": "Example",
                            "base_url": "https://example.com",
                            "steps": [{"action": "goto", "value": "/"}],
                        },
                    }
                ),
                encoding="utf-8",
            )

            goal = load_goal(path)

        self.assertEqual(goal["id"], "example")

    def test_run_goal_calculates_baseline_metrics(self) -> None:
        goal = {
            "schema_version": "research.goal.v1",
            "id": "example",
            "objective": "Open example.com",
            "target": {"base_url": "https://example.com"},
            "success_criteria": ["URL is correct"],
            "dsl_case": {
                "name": "Example",
                "base_url": "https://example.com",
                "steps": [
                    {"action": "goto", "value": "/"},
                    {"action": "assert_url_contains", "value": "example.com"},
                ],
            },
        }

        def executor(**kwargs):
            self.assertEqual(kwargs["execution_id"], 1)
            return [
                StepExecutionEvidence(
                    step_index=0,
                    action="goto",
                    status="passed",
                    duration_ms=10,
                ),
                StepExecutionEvidence(
                    step_index=1,
                    action="assert_url_contains",
                    status="passed",
                    duration_ms=30,
                    click_recovery="dismiss",
                    vlm_preverify_used=True,
                ),
            ]

        result = run_goal(goal, execution_id=1, executor=executor)

        self.assertTrue(result["success"])
        self.assertEqual(result["metrics"]["passed_steps"], 2)
        self.assertEqual(result["metrics"]["total_steps"], 2)
        self.assertEqual(result["metrics"]["average_step_duration_ms"], 20)
        self.assertEqual(result["metrics"]["recovery_count"], 1)
        self.assertEqual(result["metrics"]["vision_calls"], 1)
        self.assertIsNone(result["failure"])

    def test_run_goal_fails_when_not_all_steps_are_recorded(self) -> None:
        goal = {
            "schema_version": "research.goal.v1",
            "id": "example",
            "objective": "Open example.com",
            "target": {"base_url": "https://example.com"},
            "success_criteria": ["URL is correct"],
            "dsl_case": {
                "name": "Example",
                "steps": [
                    {"action": "goto", "value": "https://example.com"},
                    {"action": "assert_url_contains", "value": "example.com"},
                ],
            },
        }

        def incomplete_executor(**kwargs):
            self.assertEqual(kwargs["execution_id"], 1)
            return [
                StepExecutionEvidence(
                    step_index=0,
                    action="goto",
                    status="passed",
                )
            ]

        result = run_goal(
            goal,
            execution_id=1,
            executor=incomplete_executor,
        )

        self.assertFalse(result["success"])
        self.assertEqual(result["metrics"]["recorded_steps"], 1)


if __name__ == "__main__":
    unittest.main()
