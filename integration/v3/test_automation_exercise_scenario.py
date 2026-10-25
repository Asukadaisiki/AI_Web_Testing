"""Tests for the two-task Automation Exercise real-provider scenario."""

from __future__ import annotations

import json
import sys
import unittest
from pathlib import Path
from unittest.mock import patch


ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "worker"))
sys.path.insert(0, str(ROOT / "integration/v3"))

from loop_worker.contracts import validate_case  # noqa: E402

import automation_exercise_scenario as scenario  # noqa: E402
from automation_exercise_scenario import (  # noqa: E402
    ScenarioConfig,
    ScenarioRun,
    build_prompt,
    build_tasks,
    validate_summary,
)


class ScenarioConfigTest(unittest.TestCase):
    def test_workload_is_exactly_two_projects_with_one_run_each(self) -> None:
        config = ScenarioConfig()

        self.assertEqual(config.projects, 2)
        self.assertEqual(config.conversations, 2)
        self.assertEqual(config.runs, 2)
        self.assertEqual(config.submit_workers, 1)
        self.assertGreaterEqual(config.run_timeout_seconds, 1_500)
        self.assertEqual(config.as_load_config().minimum_peak_concurrency, 1)

    def test_runs_wait_for_each_business_task_before_submitting_the_next(self) -> None:
        tasks = build_tasks("batch123")
        locations = [("project-account", "conversation-account"), ("project-purchase", "conversation-purchase")]
        events: list[str] = []

        def create_run(task, project_id, conversation_id, **kwargs):
            del project_id, conversation_id, kwargs
            events.append(f"submit:{task.key}")
            return ScenarioRun(task.key, f"project-{task.key}", f"conversation-{task.key}", f"run-{task.key}")

        def wait_for_runs(runs, **kwargs):
            del kwargs
            run_id = runs[0].run_id
            events.append(f"wait:{run_id.removeprefix('run-')}")
            return {run_id: "succeeded"}

        with (
            patch.object(scenario, "_create_run", side_effect=create_run),
            patch.object(scenario, "wait_for_runs", side_effect=wait_for_runs),
        ):
            runs, statuses = scenario._submit_runs_serially(
                tasks,
                locations,
                agent_id="agent",
                model={"provider": "openai-compatible", "name": "model"},
                batch="batch123",
                config=ScenarioConfig(),
            )

        self.assertEqual(
            events,
            ["submit:account", "wait:account", "submit:purchase", "wait:purchase"],
        )
        self.assertEqual([run.task for run in runs], ["account", "purchase"])
        self.assertEqual(statuses, {"run-account": "succeeded", "run-purchase": "succeeded"})


class TaskBuilderTest(unittest.TestCase):
    def test_builds_distinct_account_and_purchase_tasks(self) -> None:
        tasks = build_tasks("batch123")

        self.assertEqual([task.key for task in tasks], ["account", "purchase"])
        self.assertEqual(len({task.email for task in tasks}), 2)
        self.assertTrue(all(email.endswith("@example.com") for email in (task.email for task in tasks)))
        self.assertIn("delete", tasks[0].case["goal"].lower())
        self.assertIn("purchase", tasks[1].case["goal"].lower())

    def test_both_cases_satisfy_the_worker_contract(self) -> None:
        tasks = build_tasks("batch123")

        for task in tasks:
            with self.subTest(task=task.key):
                validated = validate_case(task.case)
                self.assertGreater(len(validated.steps), 10)
                self.assertEqual(validated.steps[0].action, "goto")
                self.assertEqual(validated.steps[-1].action, "assert_text")

    def test_birth_month_asserts_the_select_dom_value(self) -> None:
        account = build_tasks("batch123")[0]
        month_step = next(
            step
            for step in account.case["steps"]
            if step.get("target", {}).get("locator", {}).get("css") == "#months"
        )

        self.assertEqual(month_step["value"], "May")
        self.assertEqual(month_step["postconditions"][0]["value"], "5")

    def test_purchase_uses_the_site_search_url_after_entering_the_term(self) -> None:
        purchase = build_tasks("batch123")[1]
        search_steps = [
            step
            for step in purchase.case["steps"]
            if "products?search=" in str(step.get("value", ""))
        ]
        submit_targets = [
            step
            for step in purchase.case["steps"]
            if step.get("target", {}).get("locator", {}).get("css")
            == "#submit_search"
        ]

        self.assertEqual(
            [step["value"] for step in search_steps],
            ["https://automationexercise.com/products?search=Blue%20Top"],
        )
        self.assertEqual(submit_targets, [])

    def test_purchase_opens_the_selected_product_by_stable_detail_url(self) -> None:
        purchase = build_tasks("batch123")[1]
        detail_steps = [
            step
            for step in purchase.case["steps"]
            if step.get("value")
            == "https://automationexercise.com/product_details/1"
        ]
        detail_link_clicks = [
            step
            for step in purchase.case["steps"]
            if step.get("target", {}).get("locator", {}).get("css")
            == "a[href='/product_details/1']"
        ]

        self.assertEqual(len(detail_steps), 1)
        self.assertEqual(detail_steps[0]["action"], "goto")
        self.assertEqual(detail_link_clicks, [])

    def test_forms_submit_natively_and_fixed_links_use_navigation(self) -> None:
        tasks = build_tasks("batch123")
        submit_targets = {
            step.get("target", {}).get("locator", {}).get("css")
            for task in tasks
            for step in task.case["steps"]
            if step.get("submit")
        }
        fragile_click_targets = {
            "[data-qa='signup-button']",
            "[data-qa='create-account']",
            "[data-qa='continue-button']",
            "[data-qa='login-button']",
            "[data-qa='pay-button']",
            "a[href='/logout']",
            "a[href='/delete_account']",
            "div.modal-content a[href='/view_cart']",
            "a.check_out",
            "a[href='/payment']",
        }
        observed_fragile_clicks = {
            step.get("target", {}).get("locator", {}).get("css")
            for task in tasks
            for step in task.case["steps"]
            if step["action"] == "click"
        } & fragile_click_targets

        self.assertEqual(
            submit_targets,
            {
                "[data-qa='signup-email']",
                "#mobile_number",
                "[data-qa='login-password']",
                "[data-qa='expiry-year']",
            },
        )
        self.assertEqual(observed_fragile_clicks, set())

    def test_prompt_requires_one_exact_tool_call_and_embeds_the_case(self) -> None:
        task = build_tasks("batch123")[0]

        prompt = build_prompt(task, "browser-session-123")
        arguments = json.loads(prompt.rsplit("\n", 1)[1])

        self.assertEqual(prompt.count("browser_execute_case"), 1)
        self.assertIn("exact JSON arguments", prompt)
        self.assertEqual(arguments["session_id"], "browser-session-123")
        self.assertEqual(arguments["case"], task.case)

    def test_operation_input_must_exactly_match_prompt_arguments(self) -> None:
        task = build_tasks("batch123")[0]
        prompt = build_prompt(task, "browser-session-123")
        arguments = json.loads(prompt.rsplit("\n", 1)[1])

        self.assertTrue(
            scenario._operation_matches_prompt(
                prompt,
                {
                    "name": "browser_execute_case",
                    "arguments": arguments,
                },
            )
        )
        changed = json.loads(json.dumps(arguments))
        changed["case"]["goal"] = "Run a different passing case"
        self.assertFalse(
            scenario._operation_matches_prompt(
                prompt,
                {
                    "name": "browser_execute_case",
                    "arguments": changed,
                },
            )
        )
        changed_type = json.loads(json.dumps(arguments))
        changed_type["case"]["steps"][0]["index"] = False
        self.assertFalse(
            scenario._operation_matches_prompt(
                prompt,
                {
                    "name": "browser_execute_case",
                    "arguments": changed_type,
                },
            )
        )


class SummaryValidationTest(unittest.TestCase):
    def test_requires_two_successful_isolated_business_runs(self) -> None:
        summary = {
            "requested_runs": 2,
            "submitted_runs": 2,
            "succeeded_runs": 2,
            "failed_runs": 0,
            "projects": 2,
            "conversations": 2,
            "operation_results": 2,
            "runs_with_operations": 2,
            "runs_with_screenshots": 2,
            "runs_with_spans": 2,
            "runs_with_usage": 2,
            "redis_streams": 2,
            "pending_outbox": 0,
            "dlq_events": 0,
            "unprocessed_commands": 0,
            "cross_project_runs": 0,
            "cross_conversation_messages": 0,
            "duplicate_message_ordinals": 0,
            "business_results": [
                {
                    "task": "account",
                    "operation_status": "succeeded",
                    "case_status": "passed",
                    "operation_matches_prompt": True,
                },
                {
                    "task": "purchase",
                    "operation_status": "succeeded",
                    "case_status": "passed",
                    "operation_matches_prompt": True,
                },
            ],
        }

        validate_summary(summary)

        summary["business_results"][1]["operation_matches_prompt"] = False
        with self.assertRaisesRegex(AssertionError, "purchase"):
            validate_summary(summary)


if __name__ == "__main__":
    unittest.main()
