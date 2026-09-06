from __future__ import annotations

import json
import tempfile
import time
import unittest
from pathlib import Path
from unittest.mock import patch

from app.runners.playwright_runner import _attach_final_dom_snapshot
from app.schemas.executions import StepExecutionEvidence
from scripts.run_agentic_e2e import (
    AgenticE2EError,
    CANONICAL_GOAL,
    HTTPAgenticClient,
    _failure_result,
    _go_json_sha256,
    evaluate_cart_oracle,
    main,
    oracle_expectation,
    run_agentic_goal,
    validate_canonical_search_contract,
    validate_goal,
)


CART_HTML = """
<table>
  <tr id="product-1">
    <td class="cart_description"><h4><a>Blue Top</a></h4></td>
    <td class="cart_price"><p>Rs. 500</p></td>
    <td class="cart_quantity"><button>1</button></td>
    <td class="cart_total"><p>Rs. 500</p></td>
  </tr>
</table>
"""

EXECUTION_29_CART_HTML = (
    Path(__file__).parent / "fixtures" / "execution-29-cart-fragment.html"
).read_text(encoding="utf-8")


class FakeClient:
    def __init__(self) -> None:
        self.approved = False
        self.cancelled = False
        self.cancel_calls = []
        self.case = {
            "name": "Blue Top",
            "base_url": "https://automationexercise.com",
            "steps": [
                {
                    "action": "goto",
                    "value": "https://automationexercise.com/products",
                },
                {
                    "action": "input",
                    "target": "#search_product",
                    "value": "Blue Top",
                    "candidates": [
                        {
                            "strategy": "verified_css",
                            "selector": "#search_product",
                            "pre_score": 1.0,
                            "pre_features": {"verified": True},
                        }
                    ],
                },
                {
                    "action": "click",
                    "target": "#submit_search",
                    "candidates": [
                        {
                            "strategy": "verified_css",
                            "selector": "#submit_search",
                            "pre_score": 1.0,
                            "pre_features": {"verified": True},
                        }
                    ],
                },
            ],
        }
        self.dsl_hash = _go_json_sha256(self.case)

    def create_project(self, name):
        self.project_name = name
        return {"id": 11}

    def create_session(self, project_id):
        self.project_id = project_id
        return {"session": {"id": 22}}

    def list_batches(self, project_id):
        return [{"id": 55}] if self.approved else []

    def start_run(self, session_id, goal):
        self.goal = goal
        return {"id": "run-33", "status": "running"}

    def get_run(self, run_id):
        if self.cancelled:
            return {"id": run_id, "status": "cancelled"}
        if not self.approved:
            return {
                "id": run_id,
                "status": "waiting_user",
                "pending_tool_call_id": "approval-1",
                "latest_generation_id": 44,
            }
        return {
            "id": run_id,
            "status": "completed",
            "approved_generation_id": 44,
        }

    def cancel_run(self, run_id, reason):
        self.cancel_calls.append((run_id, reason))
        self.cancelled = True
        return {"id": run_id, "status": "cancelled"}

    def stream_events(self, run_id, after_seq):
        raise AssertionError("boundary waiting must not depend on SSE")

    def list_events(self, run_id, after_seq):
        if not self.approved:
            events = [
                {
                    "seq": 1,
                    "type": "tool.result",
                    "tool_call_id": "generate-1",
                    "payload": {
                        "tool": "generate_dsl",
                        "content": {"generation_id": 44, "case": self.case},
                    },
                },
                {
                    "seq": 2,
                    "type": "artifact.published",
                    "tool_call_id": "generate-1",
                    "payload": {"type": "dsl_generation", "id": "44"},
                },
                {
                    "seq": 3,
                    "type": "tool.pending",
                    "tool_call_id": "approval-1",
                    "checkpoint_id": "checkpoint-1",
                    "payload": {
                        "questions": [
                            {"id": "approve_dsl", "type": "confirm"}
                        ]
                    },
                },
            ]
        else:
            events = [
                {
                    "seq": 4,
                    "type": "artifact.published",
                    "tool_call_id": "execute-1",
                    "payload": {"type": "execution_batch", "id": "55"},
                },
                {
                    "seq": 5,
                    "type": "artifact.published",
                    "tool_call_id": "report-1",
                    "payload": {"type": "execution_report", "id": "55"},
                },
                {"seq": 6, "type": "run.finished", "payload": {}},
            ]
        return [event for event in events if event["seq"] > after_seq]

    def approve(self, run_id, tool_call_id):
        self.approved = True
        return {"id": run_id, "status": "running"}

    def get_report(self, batch_id):
        return {
            "id": batch_id,
            "status": "passed",
            "jobs": [
                {
                    "id": 66,
                    "status": "passed",
                    "latest_execution": {
                        "id": 77,
                        "status": "passed",
                        "dsl_sha256": self.dsl_hash,
                        "report_schema_version": "execution.report.v1",
                        "latest_url": "https://automationexercise.com/view_cart",
                        "report": {
                            "steps": [
                                {
                                    "status": "passed",
                                    "url": "https://automationexercise.com/view_cart",
                                    "screenshot_url": "/artifacts/executions/77/step-01.png",
                                    "dom_snapshot_url": "/artifacts/executions/77/final.html",
                                    "vlm_preverify_used": False,
                                }
                            ]
                        },
                    },
                }
            ],
        }

    def get_artifact(self, artifact_url):
        return CART_HTML.encode()


class ClarificationClient(FakeClient):
    def get_run(self, run_id):
        if self.cancelled:
            return {"id": run_id, "status": "cancelled"}
        return {
            "id": run_id,
            "status": "waiting_user",
            "pending_tool_call_id": "clarification-1",
            "latest_generation_id": None,
        }

    def list_events(self, run_id, after_seq):
        events = [
            {
                "seq": 20,
                "type": "tool.pending",
                "tool_call_id": "stale-approval",
                "checkpoint_id": "checkpoint-old",
                "payload": {
                    "questions": [{"id": "approve_dsl", "type": "confirm"}]
                },
            },
            {
                "seq": 22,
                "type": "tool.pending",
                "tool_call_id": "clarification-1",
                "checkpoint_id": "checkpoint-clarification",
                "payload": {
                    "questions": [
                        {
                            "id": "browser_backend_down",
                            "type": "choice",
                            "prompt": "Browser backend is unavailable.",
                        }
                    ]
                },
            }
        ]
        return [event for event in events if event["seq"] > after_seq]


class MultiApprovalClient(FakeClient):
    def __init__(self) -> None:
        super().__init__()
        self.approval_count = 0
        self.repaired_case = {
            **self.case,
            "name": "Blue Top repaired",
        }
        self.repaired_hash = _go_json_sha256(self.repaired_case)

    def list_batches(self, project_id):
        if self.approval_count == 0:
            return []
        if self.approval_count == 1:
            return [{"id": 55}]
        return [{"id": 55}, {"id": 56}]

    def get_run(self, run_id):
        if self.cancelled:
            return {"id": run_id, "status": "cancelled"}
        if self.approval_count == 0:
            return {
                "id": run_id,
                "status": "waiting_user",
                "pending_tool_call_id": "approval-1",
                "latest_generation_id": 44,
            }
        if self.approval_count == 1:
            return {
                "id": run_id,
                "status": "waiting_user",
                "pending_tool_call_id": "approval-2",
                "latest_generation_id": 45,
                "approved_generation_id": 44,
            }
        return {
            "id": run_id,
            "status": "completed",
            "latest_generation_id": 45,
            "approved_generation_id": 45,
        }

    def list_events(self, run_id, after_seq):
        first = super().list_events(run_id, after_seq) if self.approval_count == 0 else []
        if self.approval_count == 0:
            return first
        if self.approval_count == 1:
            events = [
                {
                    "seq": 4,
                    "type": "artifact.published",
                    "payload": {"type": "execution_batch", "id": "55"},
                },
                {
                    "seq": 5,
                    "type": "artifact.published",
                    "payload": {"type": "execution_report", "id": "55"},
                },
                {
                    "seq": 6,
                    "type": "tool.result",
                    "payload": {
                        "tool": "generate_dsl",
                        "content": {
                            "generation_id": 45,
                            "case": self.repaired_case,
                            "dsl_sha256": self.repaired_hash,
                        },
                    },
                },
                {
                    "seq": 7,
                    "type": "artifact.published",
                    "payload": {"type": "dsl_generation", "id": "45"},
                },
                {
                    "seq": 8,
                    "type": "tool.pending",
                    "tool_call_id": "approval-2",
                    "checkpoint_id": "checkpoint-2",
                    "payload": {
                        "questions": [{"id": "approve_dsl", "type": "confirm"}]
                    },
                },
            ]
        else:
            events = [
                {
                    "seq": 9,
                    "type": "artifact.published",
                    "payload": {"type": "execution_batch", "id": "56"},
                },
                {
                    "seq": 10,
                    "type": "artifact.published",
                    "payload": {"type": "execution_report", "id": "56"},
                },
                {"seq": 11, "type": "run.finished", "payload": {}},
            ]
        return [event for event in events if event["seq"] > after_seq]

    def approve(self, run_id, tool_call_id):
        self.approval_count += 1
        self.approved = True
        return {"id": run_id, "status": "running"}

    def get_report(self, batch_id):
        report = super().get_report(batch_id)
        execution = report["jobs"][0]["latest_execution"]
        if batch_id == 55:
            report["status"] = "failed"
            report["jobs"][0]["status"] = "failed"
            execution["status"] = "failed"
            execution["dsl_sha256"] = self.dsl_hash
        else:
            execution["dsl_sha256"] = self.repaired_hash
        return report


class AgenticE2EDriverTest(unittest.TestCase):
    def test_goal_accepts_natural_language(self) -> None:
        self.assertEqual(validate_goal(CANONICAL_GOAL), CANONICAL_GOAL)

    def test_canonical_search_requires_verified_input_then_click(self) -> None:
        validate_canonical_search_contract(FakeClient().case)

        with self.assertRaisesRegex(AgenticE2EError, "input followed by"):
            validate_canonical_search_contract(
                {
                    "steps": [
                        {"action": "goto", "value": "/products"},
                        {"action": "click", "target": "#submit_search"},
                    ]
                }
            )
        with self.assertRaisesRegex(AgenticE2EError, "input followed by"):
            validate_canonical_search_contract(
                {
                    "steps": [
                        {
                            "action": "input",
                            "target": "#search_product",
                            "value": "Blue Top",
                        },
                        {"action": "click", "target": "#submit_search"},
                    ]
                }
            )

    def test_canonical_search_rejects_direct_search_url(self) -> None:
        with self.assertRaisesRegex(AgenticE2EError, "not goto"):
            validate_canonical_search_contract(
                {
                    "steps": [
                        {
                            "action": "goto",
                            "value": "/products?search=Blue%20Top",
                        }
                    ]
                }
            )

    def test_goal_rejects_dsl_css_xpath_and_candidates(self) -> None:
        invalid = [
            '{"steps":[{"action":"click"}]}',
            "点击 CSS #product-1",
            "点击 xpath=//tr[@id='product-1']",
            "使用 candidates 列表定位商品",
            "点击 button.cart",
        ]
        for goal in invalid:
            with self.subTest(goal=goal), self.assertRaises(ValueError):
                validate_goal(goal)

    def test_oracle_passes_and_negative_mutations_fail(self) -> None:
        self.assertTrue(evaluate_cart_oracle(CART_HTML)["passed"])
        self.assertFalse(
            evaluate_cart_oracle(
                CART_HTML, expected=oracle_expectation("wrong-price")
            )["passed"]
        )
        self.assertFalse(
            evaluate_cart_oracle(
                CART_HTML, expected=oracle_expectation("wrong-product")
            )["passed"]
        )

    def test_oracle_parses_execution_29_dom_shape_precisely(self) -> None:
        result = evaluate_cart_oracle(EXECUTION_29_CART_HTML)

        self.assertTrue(result["passed"])
        self.assertEqual(
            result["actual"],
            {
                "id": "product-1",
                "name": "Blue Top",
                "unit_price": "Rs. 500",
                "quantity": "1",
                "total_price": "Rs. 500",
            },
        )
        self.assertEqual(result["observed_row_ids"], ["product-1"])

    def test_oracle_handles_implicit_table_cell_and_row_closing(self) -> None:
        html = """
        <table><tr id="product-1">
          <td class="cart_description"><h4><a>Blue Top</a></h4><p>not the name
          <td class="cart_price"><p>Rs. 500
          <td class="cart_quantity"><button>1
          <td class="cart_total"><p>Rs. 500
        <tr id="summary"><td class="cart_price"><p>Rs. 999</table>
        <footer><img src="footer.png"></footer></body></html>
        """

        result = evaluate_cart_oracle(html)

        self.assertTrue(result["passed"])
        self.assertEqual(result["observed_row_ids"], ["product-1"])

    def test_cli_exit_codes_follow_oracle_result(self) -> None:
        def run_with_oracle(*args, mutation, **kwargs):
            return {
                "success": evaluate_cart_oracle(
                    EXECUTION_29_CART_HTML,
                    expected=oracle_expectation(mutation),
                )["passed"]
            }

        with tempfile.TemporaryDirectory() as directory:
            for mutation, expected_code in (
                ("none", 0),
                ("wrong-price", 1),
                ("wrong-product", 1),
            ):
                output = Path(directory) / f"{mutation}.json"
                argv = [
                    "run_agentic_e2e.py",
                    CANONICAL_GOAL,
                    "--oracle-mutation",
                    mutation,
                    "--output",
                    str(output),
                ]
                with (
                    self.subTest(mutation=mutation),
                    patch("sys.argv", argv),
                    patch(
                        "scripts.run_agentic_e2e.run_agentic_goal",
                        side_effect=run_with_oracle,
                    ),
                    patch("builtins.print"),
                ):
                    self.assertEqual(main(), expected_code)
                self.assertEqual(
                    json.loads(output.read_text(encoding="utf-8"))["success"],
                    expected_code == 0,
                )

    def test_run_drives_approval_report_and_oracle(self) -> None:
        result = run_agentic_goal(CANONICAL_GOAL, client=FakeClient())

        self.assertTrue(result["success"])
        self.assertTrue(result["configuration"]["clean_browser_context"])
        self.assertEqual(result["ids"]["agent_run_id"], "run-33")
        self.assertEqual(result["ids"]["generation_id"], 44)
        self.assertEqual(result["ids"]["batch_id"], 55)
        self.assertEqual(result["ids"]["job_id"], 66)
        self.assertEqual(result["ids"]["execution_id"], 77)
        self.assertEqual(result["schema_version"], "agentic-e2e.result.v1")

    def test_oracle_mutation_overrides_formal_pass(self) -> None:
        result = run_agentic_goal(
            CANONICAL_GOAL,
            client=FakeClient(),
            mutation="wrong-price",
        )

        self.assertTrue(result["formal_execution"]["passed"])
        self.assertFalse(result["oracle"]["passed"])
        self.assertFalse(result["success"])

    def test_multiple_approvals_preserve_failed_first_batch(self) -> None:
        result = run_agentic_goal(
            CANONICAL_GOAL,
            client=MultiApprovalClient(),
        )

        self.assertEqual(
            [approval["generation_id"] for approval in result["approvals"]],
            [44, 45],
        )
        self.assertEqual(
            [batch["status"] for batch in result["batch_rounds"]],
            ["failed", "passed"],
        )
        self.assertEqual(
            result["recovery"],
            [
                {
                    "from_generation_id": 44,
                    "failed_batch_id": 55,
                    "failed_batch_status": "failed",
                    "to_generation_id": 45,
                    "approval_round": 2,
                }
            ],
        )
        self.assertTrue(result["formal_execution"]["passed"])
        self.assertFalse(result["stage0"]["first_pass"])
        self.assertFalse(result["success"])

    def test_clarification_is_not_approved_and_keeps_failure_diagnostic(self) -> None:
        client = ClarificationClient()

        with self.assertRaises(AgenticE2EError) as raised:
            run_agentic_goal(CANONICAL_GOAL, client=client)

        diagnostic = raised.exception.diagnostic
        self.assertFalse(client.approved)
        self.assertEqual(
            diagnostic["ids"],
            {
                "project_id": 11,
                "session_id": 22,
                "agent_run_id": "run-33",
            },
        )
        self.assertEqual(diagnostic["run"]["status"], "waiting_user")
        self.assertEqual(
            diagnostic["run"]["pending_tool_call_id"], "clarification-1"
        )
        self.assertEqual(diagnostic["checkpoint"]["kind"], "clarification")
        self.assertEqual(
            diagnostic["checkpoint"]["questions"][0]["id"],
            "browser_backend_down",
        )
        self.assertEqual(diagnostic["events"]["last_seq"], 22)
        self.assertEqual(
            diagnostic["events"]["references"][-1]["tool_call_id"],
            "clarification-1",
        )
        failure = _failure_result(
            CANONICAL_GOAL, "none", raised.exception
        )
        self.assertFalse(failure["success"])
        self.assertEqual(failure["ids"], diagnostic["ids"])
        self.assertEqual(failure["events"]["last_seq"], 22)
        self.assertEqual(diagnostic["cancellation"]["status"], "cancelled")
        self.assertEqual(len(client.cancel_calls), 1)
        json.dumps(failure)

    def test_timeout_snapshots_diagnostic_before_cancel(self) -> None:
        class TimeoutClient(FakeClient):
            def __init__(self):
                super().__init__()
                self.actions = []

            def get_run(self, run_id):
                self.actions.append("get_run")
                return super().get_run(run_id)

            def list_events(self, run_id, after_seq):
                return []

            def cancel_run(self, run_id, reason):
                self.actions.append("cancel_run")
                return super().cancel_run(run_id, reason)

        client = TimeoutClient()

        with self.assertRaises(AgenticE2EError) as raised:
            run_agentic_goal(
                CANONICAL_GOAL,
                client=client,
                timeout_seconds=0,
            )

        diagnostic = raised.exception.diagnostic
        self.assertIn("did not reach a boundary", str(raised.exception))
        self.assertEqual(diagnostic["run"]["status"], "waiting_user")
        self.assertEqual(diagnostic["cancellation"]["status"], "cancelled")
        self.assertLess(
            client.actions.index("get_run"),
            client.actions.index("cancel_run"),
        )

    def test_cancel_error_does_not_replace_original_timeout(self) -> None:
        class CancelFailureClient(FakeClient):
            def list_events(self, run_id, after_seq):
                return []

            def cancel_run(self, run_id, reason):
                raise RuntimeError("cancel endpoint unavailable")

        with self.assertRaises(AgenticE2EError) as raised:
            run_agentic_goal(
                CANONICAL_GOAL,
                client=CancelFailureClient(),
                timeout_seconds=0,
            )

        self.assertIn("did not reach a boundary", str(raised.exception))
        self.assertEqual(
            raised.exception.diagnostic["cancellation"]["error"],
            "cancel endpoint unavailable",
        )

    def test_sse_keepalive_has_wall_clock_bound(self) -> None:
        class KeepaliveResponse:
            def __enter__(self):
                return self

            def __exit__(self, *args):
                return False

            def __iter__(self):
                while True:
                    time.sleep(0.001)
                    yield b": keepalive\n"

        client = HTTPAgenticClient(
            agent_url="http://agent.test",
            browser_url="http://browser.test",
            stream_window_seconds=0.01,
        )
        started = time.monotonic()
        with patch(
            "scripts.run_agentic_e2e.urlopen",
            return_value=KeepaliveResponse(),
        ):
            events = client.stream_events("run-33", 0)

        self.assertEqual(events, [])
        self.assertLess(time.monotonic() - started, 0.5)


class FinalDOMArtifactTest(unittest.TestCase):
    def test_attaches_final_dom_snapshot_to_last_step(self) -> None:
        class Page:
            @staticmethod
            def content():
                return "<html><body>final</body></html>"

        artifacts_root = Path(__file__).resolve().parents[1] / "artifacts"
        with tempfile.TemporaryDirectory(dir=artifacts_root) as directory:
            artifact_dir = Path(directory)
            steps = [
                StepExecutionEvidence(
                    step_index=0,
                    action="goto",
                    status="passed",
                )
            ]

            _attach_final_dom_snapshot(Page(), artifact_dir, steps)

            self.assertEqual(
                (artifact_dir / "final.html").read_text(encoding="utf-8"),
                "<html><body>final</body></html>",
            )
            self.assertTrue(steps[0].dom_snapshot_path.endswith("/final.html"))
            self.assertTrue(steps[0].dom_snapshot_url.endswith("/final.html"))


if __name__ == "__main__":
    unittest.main()
