"""Fake-client unit tests; these are not formal Research E2E executions."""

from __future__ import annotations

import tempfile
import unittest
from datetime import UTC, datetime, timedelta
from pathlib import Path
from unittest.mock import call, patch

from scripts.research_e2e import (
    DEFAULT_SPEC,
    ResearchAPIClient,
    ResearchE2EError,
    _assert_audit_safe,
    compute_code_snapshot_sha256,
    export_research,
    load_experiment_spec,
    load_experiment_spec_from_value,
    run_experiment,
    verify_experiment,
)


class OrchestrationClient:
    def __init__(self, runs, *, deadline_seconds=3600):
        self.runs = runs
        self.deadline_seconds = deadline_seconds
        self.calls = []

    def create_experiment(self, payload):
        self.calls.append(("create_experiment", payload))
        return {"experiment": {"id": "experiment-1"}}

    def start_experiment(self, experiment_id, project_id):
        self.calls.append(("start_experiment", experiment_id, project_id))
        return {}

    def list_runs(self, experiment_id, project_id):
        self.calls.append(("list_runs", experiment_id, project_id))
        return self.runs

    def start_run(self, run_id, project_id):
        self.calls.append(("start_run", run_id, project_id))
        deadline = datetime.now(UTC) + timedelta(
            seconds=self.deadline_seconds
        )
        return {
            "run": {"id": run_id, "status": "running"},
            "deadline": deadline.isoformat().replace("+00:00", "Z"),
        }

    def update_links(self, run_id, project_id, links):
        self.calls.append(("update_links", run_id, project_id, links))
        return {"id": run_id, "links": links}

    def put_oracle(self, run_id, project_id, execution_id, oracle):
        self.calls.append(
            ("put_oracle", run_id, project_id, execution_id, oracle)
        )
        return oracle

    def project_metrics(self, run_id, project_id):
        self.calls.append(("project_metrics", run_id, project_id))
        return {}

    def finish_run(self, run_id, project_id):
        self.calls.append(("finish_run", run_id, project_id))
        return {"id": run_id, "status": "completed"}

    def cancel_run(self, run_id, project_id):
        self.calls.append(("cancel_run", run_id, project_id))
        return {"id": run_id, "status": "cancelled"}


def driver_result(index: int, project_id: int = 7) -> dict:
    return {
        "ids": {
            "project_id": project_id,
            "planning_session_id": 100 + index,
            "browser_context_key": 100 + index,
            "agent_run_id": f"agent-{index}",
            "generation_id": 200 + index,
            "batch_id": 300 + index,
            "execution_id": 400 + index,
        },
        "approval": {"dsl_sha256": f"{index + 1:064x}"},
        "formal_execution": {"passed": True},
        "oracle": {
            "schema_version": "automationexercise.cart-oracle.v1",
            "passed": True,
            "checks": {"name": True},
            "expected": {"name": "Blue Top"},
            "actual": {"name": "Blue Top"},
        },
        "artifacts": {
            "final_dom": {
                "url": f"/artifacts/{index}/final.html",
                "sha256": f"{index + 10:064x}",
            }
        },
        "success": True,
    }


class ResearchE2EOrchestrationTest(unittest.TestCase):
    def test_spec_contains_only_natural_language_goal_and_controls(self) -> None:
        spec = load_experiment_spec(DEFAULT_SPEC)

        self.assertEqual(spec["repetitions"], 3)
        self.assertNotIn("dsl_case", spec)
        self.assertNotIn("selector", str(spec).casefold())
        self.assertNotIn("candidates", str(spec).casefold())

    def test_spec_rejects_dsl_selector_and_candidates(self) -> None:
        base = load_experiment_spec(DEFAULT_SPEC)
        for field in ("dsl_case", "selector", "candidates"):
            invalid = {**base, field: {}}
            with self.subTest(field=field), self.assertRaisesRegex(
                ValueError, "forbidden"
            ):
                load_experiment_spec_from_value(invalid)

    def test_run_uses_real_driver_contract_with_fresh_sessions(self) -> None:
        spec = load_experiment_spec(DEFAULT_SPEC)
        spec["warmup_runs"] = 1
        runs = [
            {
                "id": f"research-{index}",
                "repetition_index": index if index < 3 else 0,
                "warmup": index == 3,
            }
            for index in range(4)
        ]
        client = OrchestrationClient(runs)
        invocations = []

        def driver(goal, **kwargs):
            invocations.append((goal, kwargs))
            return driver_result(len(invocations), kwargs["project_id"])

        result = run_experiment(
            spec,
            project_id=7,
            research_client=client,
            agent_url="http://agent.test",
            browser_url="http://browser.test",
            driver=driver,
            agent_client_factory=lambda **kwargs: kwargs,
            code_snapshot_sha256=lambda: spec["controls"]["code_sha256"],
        )

        self.assertTrue(result["success"])
        self.assertEqual(len(invocations), 4)
        self.assertEqual(
            {run["planning_session_id"] for run in result["runs"]},
            {101, 102, 103, 104},
        )
        self.assertTrue(
            all(item[1]["project_id"] == 7 for item in invocations)
        )
        self.assertTrue(
            all(item[1]["clean_context"] is True for item in invocations)
        )
        self.assertTrue(
            all(
                item[1]["timeout_seconds"] == 900
                for item in invocations
            )
        )
        self.assertEqual(
            len([entry for entry in client.calls if entry[0] == "finish_run"]),
            4,
        )
        create_payload = client.calls[0][1]
        self.assertEqual(create_payload["project_id"], 7)
        self.assertTrue(
            create_payload["config"]["clean_context"]
        )
        oracle_call = next(
            entry for entry in client.calls if entry[0] == "put_oracle"
        )
        fact = oracle_call[4]["decision_facts"][0]
        self.assertEqual(fact["expected"], "Blue Top")
        self.assertEqual(fact["actual"], "Blue Top")
        self.assertEqual(fact["passed"], oracle_call[4]["passed"])

    def test_driver_failure_cancels_research_run(self) -> None:
        spec = load_experiment_spec(DEFAULT_SPEC)
        spec["repetitions"] = 1
        client = OrchestrationClient(
            [{"id": "research-1", "repetition_index": 0, "warmup": False}]
        )

        result = run_experiment(
            spec,
            project_id=7,
            research_client=client,
            agent_url="http://agent.test",
            browser_url="http://browser.test",
            driver=lambda *args, **kwargs: (_ for _ in ()).throw(
                TimeoutError("driver deadline")
            ),
            agent_client_factory=lambda **kwargs: kwargs,
            code_snapshot_sha256=lambda: spec["controls"]["code_sha256"],
        )

        self.assertFalse(result["success"])
        self.assertEqual(result["failures"][0]["research_run_id"], "research-1")
        self.assertTrue(
            any(entry[0] == "cancel_run" for entry in client.calls)
        )

    def test_server_deadline_limits_reentered_running_run(self) -> None:
        spec = load_experiment_spec(DEFAULT_SPEC)
        spec["repetitions"] = 1
        client = OrchestrationClient(
            [{"id": "research-1", "repetition_index": 0, "warmup": False}],
            deadline_seconds=5,
        )
        observed_timeouts = []

        def driver(goal, **kwargs):
            observed_timeouts.append(kwargs["timeout_seconds"])
            return driver_result(1)

        result = run_experiment(
            spec,
            project_id=7,
            research_client=client,
            agent_url="http://agent.test",
            browser_url="http://browser.test",
            driver=driver,
            agent_client_factory=lambda **kwargs: kwargs,
            code_snapshot_sha256=lambda: spec["controls"]["code_sha256"],
        )

        self.assertTrue(result["success"])
        self.assertEqual(len(observed_timeouts), 1)
        self.assertGreater(observed_timeouts[0], 0)
        self.assertLessEqual(observed_timeouts[0], 5)

    def test_business_failure_is_persisted_and_finished(self) -> None:
        spec = load_experiment_spec(DEFAULT_SPEC)
        spec["repetitions"] = 1
        client = OrchestrationClient(
            [{"id": "research-1", "repetition_index": 0, "warmup": False}]
        )
        failed = driver_result(1)
        failed["success"] = False
        failed["oracle"]["passed"] = False
        failed["oracle"]["checks"]["name"] = False
        failed["oracle"]["actual"]["name"] = "Red Top"

        result = run_experiment(
            spec,
            project_id=7,
            research_client=client,
            agent_url="http://agent.test",
            browser_url="http://browser.test",
            driver=lambda *args, **kwargs: failed,
            agent_client_factory=lambda **kwargs: kwargs,
            code_snapshot_sha256=lambda: spec["controls"]["code_sha256"],
        )

        self.assertFalse(result["success"])
        self.assertEqual(result["runs"][0]["status"], "completed")
        self.assertEqual(
            result["failures"][0]["failure_kind"], "business_failure"
        )
        call_names = [entry[0] for entry in client.calls]
        self.assertLess(
            call_names.index("update_links"), call_names.index("put_oracle")
        )
        self.assertLess(
            call_names.index("put_oracle"),
            call_names.index("project_metrics"),
        )
        self.assertLess(
            call_names.index("project_metrics"),
            call_names.index("finish_run"),
        )
        self.assertNotIn("cancel_run", call_names)

    def test_code_hash_mismatch_rejects_before_api_calls(self) -> None:
        spec = load_experiment_spec(DEFAULT_SPEC)
        client = OrchestrationClient([])

        with self.assertRaisesRegex(
            ResearchE2EError, "code snapshot SHA-256 mismatch"
        ):
            run_experiment(
                spec,
                project_id=7,
                research_client=client,
                agent_url="http://agent.test",
                browser_url="http://browser.test",
                code_snapshot_sha256=lambda: "0" * 64,
            )

        self.assertEqual(client.calls, [])

    def test_code_snapshot_hash_is_deterministic_and_content_sensitive(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / "browser-worker" / "app" / "worker.py"
            source.parent.mkdir(parents=True)
            source.write_text("value = 1\n", encoding="utf-8")
            with patch(
                "scripts.research_e2e.code_snapshot_files",
                return_value=[source.resolve()],
            ):
                first = compute_code_snapshot_sha256(root)
                second = compute_code_snapshot_sha256(root)
                source.write_text("value = 2\n", encoding="utf-8")
                changed = compute_code_snapshot_sha256(root)

        self.assertEqual(first, second)
        self.assertNotEqual(first, changed)

    def test_audit_payload_rejects_hidden_reasoning(self) -> None:
        for key in ("thought", "reasoning_content", "scratchpad"):
            with self.subTest(key=key), self.assertRaises(ResearchE2EError):
                _assert_audit_safe({"event": {key: "private"}})


class VerificationClient:
    def __init__(
        self,
        *,
        oracle_passed: bool = True,
        inconsistent_task_metric: bool = False,
        clean_context: bool = True,
    ):
        self.oracle_passed = oracle_passed
        self.inconsistent_task_metric = inconsistent_task_metric
        self.clean_context = clean_context
        self.runs = [
            {
                "id": f"research-{index}",
                "project_id": 7,
                "repetition_index": index,
                "warmup": False,
            }
            for index in range(3)
        ]

    def list_runs(self, experiment_id, project_id):
        return self.runs

    def get_run(self, run_id, project_id):
        index = int(run_id.rsplit("-", 1)[1])
        oracle_passed = self.oracle_passed
        task_value = (
            not oracle_passed if self.inconsistent_task_metric and index == 1
            else oracle_passed
        )
        return {
            **self.runs[index],
            "status": "completed",
            "links": {
                "agent_run_id": f"agent-{index}",
                "generation_id": 10 + index,
                "batch_id": 20 + index,
                "execution_id": 30 + index,
                "dsl_sha256": f"{index + 1:064x}",
            },
            "metrics": {
                "task_success": {
                    "value": task_value,
                    "unavailable_reason": None,
                },
                "execution_success": {
                    "value": True,
                    "unavailable_reason": None,
                },
                "verification_success": {
                    "value": True,
                    "unavailable_reason": None,
                },
                "vision_calls": {
                    "value": 0,
                    "unavailable_reason": None,
                },
            },
        }

    def get_agent_run(self, run_id):
        index = int(run_id.rsplit("-", 1)[1])
        return {
            "id": run_id,
            "status": "completed",
            "conversation_id": str(100 + index),
            "project_id": 7,
        }

    def get_batch_report(self, batch_id):
        index = batch_id - 20
        return {
            "id": batch_id,
            "status": "passed",
            "jobs": [
                {
                    "latest_execution": {
                        "id": 30 + index,
                        "status": "passed",
                        "dsl_sha256": f"{index + 1:064x}",
                        "report": {
                            "steps": [
                                {
                                    "condition_results": [
                                        {"status": "passed"}
                                    ]
                                }
                            ]
                        },
                    }
                }
            ],
        }

    def get_planning_session(self, session_id):
        return {
            "session": {
                "id": session_id,
                "requirements": {"clean_context": self.clean_context},
            }
        }

    def get_oracle(self, run_id, project_id):
        return {"passed": self.oracle_passed}


class ResearchE2EVerificationTest(unittest.TestCase):
    def test_verify_requires_three_canonical_successes(self) -> None:
        result = verify_experiment(
            "experiment-1",
            client=VerificationClient(),
            project_id=7,
        )

        self.assertTrue(result["passed"])
        self.assertEqual(result["non_warmup_runs"], 3)
        self.assertEqual(result["clean_session_count"], 3)
        self.assertTrue(all(run["task_success"] for run in result["runs"]))
        self.assertTrue(all(run["oracle_passed"] for run in result["runs"]))

    def test_canonical_oracle_false_fails_verification(self) -> None:
        with self.assertRaisesRegex(
            ResearchE2EError, "expected True"
        ):
            verify_experiment(
                "experiment-1",
                client=VerificationClient(oracle_passed=False),
                project_id=7,
            )

    def test_negative_expected_false_passes_verification(self) -> None:
        result = verify_experiment(
            "experiment-1",
            client=VerificationClient(oracle_passed=False),
            project_id=7,
            expected_task_success=False,
        )

        self.assertTrue(result["passed"])
        self.assertFalse(result["expected_task_success"])

    def test_verify_rejects_task_metric_oracle_disagreement(self) -> None:
        with self.assertRaisesRegex(
            ResearchE2EError, "task_success disagrees with oracle"
        ):
            verify_experiment(
                "experiment-1",
                client=VerificationClient(
                    oracle_passed=False,
                    inconsistent_task_metric=True,
                ),
                project_id=7,
                expected_task_success=False,
            )

    def test_verify_rejects_unclean_planning_session(self) -> None:
        with self.assertRaisesRegex(
            ResearchE2EError, "planning session is not clean"
        ):
            verify_experiment(
                "experiment-1",
                client=VerificationClient(clean_context=False),
                project_id=7,
            )


class ResearchAPIClientTest(unittest.TestCase):
    def test_research_routes_are_centralized_in_client(self) -> None:
        client = ResearchAPIClient("http://agent.test")
        with patch.object(
            client,
            "_json",
            side_effect=[
                {},
                {},
                {"runs": []},
                {},
                {
                    "run": {"id": "run-1", "status": "running"},
                    "deadline": "2026-09-07T12:00:00Z",
                },
                {},
                {},
                {},
                {},
                {},
                {},
                {},
            ],
        ) as request:
            client.create_experiment({"name": "example"})
            client.start_experiment("experiment-1", 7)
            client.list_runs("experiment-1", 7)
            client.get_run("run-1", 7)
            client.start_run("run-1", 7)
            client.update_links("run-1", 7, {"agent_run_id": "agent-1"})
            client.put_oracle("run-1", 7, 9, {"passed": True})
            client.project_metrics("run-1", 7)
            client.finish_run("run-1", 7)
            client.cancel_run("run-1", 7)
            client.get_oracle("run-1", 7)
            client.get_planning_session(42)

        self.assertEqual(
            request.call_args_list,
            [
                call(
                    "POST",
                    "/api/v2/research/experiments",
                    {"name": "example"},
                ),
                call(
                    "POST",
                    "/api/v2/research/experiments/experiment-1/start",
                    {"project_id": 7},
                ),
                call(
                    "GET",
                    "/api/v2/research/experiments/experiment-1/runs?project_id=7",
                ),
                call("GET", "/api/v2/research/runs/run-1?project_id=7"),
                call(
                    "POST",
                    "/api/v2/research/runs/run-1/start",
                    {"project_id": 7},
                ),
                call(
                    "PUT",
                    "/api/v2/research/runs/run-1/links",
                    {
                        "project_id": 7,
                        "links": {"agent_run_id": "agent-1"},
                    },
                ),
                call(
                    "PUT",
                    "/api/v2/research/runs/run-1/oracle",
                    {
                        "project_id": 7,
                        "execution_id": 9,
                        "decision": {"passed": True},
                    },
                ),
                call(
                    "POST",
                    "/api/v2/research/runs/run-1/project-metrics",
                    {"project_id": 7},
                ),
                call(
                    "POST",
                    "/api/v2/research/runs/run-1/finish",
                    {"project_id": 7, "status": "completed"},
                ),
                call(
                    "POST",
                    "/api/v2/research/runs/run-1/cancel",
                    {"project_id": 7},
                ),
                call(
                    "GET",
                    "/api/v2/research/runs/run-1/oracle?project_id=7",
                ),
                call("GET", "/api/v2/planning/sessions/42"),
            ],
        )

    def test_start_run_preserves_run_and_deadline_envelope(self) -> None:
        client = ResearchAPIClient("http://agent.test")
        payload = {
            "run": {"id": "run-1", "status": "running"},
            "deadline": "2026-09-07T12:00:00Z",
        }
        with patch.object(client, "_json", return_value=payload):
            self.assertEqual(client.start_run("run-1", 7), payload)

    def test_export_delegates_to_go_command_and_atomically_replaces(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "trajectory.jsonl"

            def run(command, **kwargs):
                target = Path(command[command.index("--output") + 1])
                target.write_text('{"ordinal":0}\n', encoding="utf-8")

            with patch(
                "scripts.research_e2e.subprocess.run",
                side_effect=run,
            ) as invoked:
                export_research(
                    output=output,
                    experiment_id="experiment-1",
                    timeout_seconds=12,
                    command=("research-export",),
                )

            self.assertEqual(
                output.read_text(encoding="utf-8"),
                '{"ordinal":0}\n',
            )
            self.assertEqual(invoked.call_args.kwargs["timeout"], 12)
            self.assertTrue(invoked.call_args.kwargs["check"])


if __name__ == "__main__":
    unittest.main()
