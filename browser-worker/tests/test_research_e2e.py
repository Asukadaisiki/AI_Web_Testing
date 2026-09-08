"""Fake-client unit tests; these are not formal Research E2E executions."""

from __future__ import annotations

import json
import subprocess
import tempfile
import unittest
from contextlib import redirect_stdout
from datetime import UTC, datetime, timedelta
from io import StringIO
from pathlib import Path
from unittest.mock import call, patch

from scripts.research_e2e import (
    DEFAULT_SPEC,
    PROVIDER_ATTESTATION_ALGORITHM,
    PROVIDER_ATTESTATION_JSON_SCHEMA,
    PROVIDER_ATTESTATION_KEY_ENV,
    ResearchAPIClient,
    ResearchE2EError,
    _assert_audit_safe,
    _is_code_snapshot_path,
    _provider_attestation_signature_value,
    compute_code_snapshot_sha256,
    export_research,
    load_experiment_spec,
    load_experiment_spec_from_value,
    load_provider_attestation,
    main,
    run_experiment,
    verify_experiment,
    verify_negative_contract_runs,
)

RESEARCH_FIXTURE = json.loads(
    (
        Path(__file__).parents[2]
        / "testdata"
        / "dsl_research_v1_contract.json"
    ).read_text(encoding="utf-8")
)
RESEARCH_CASE = json.loads(RESEARCH_FIXTURE["canonical_json"])
RESEARCH_SHA = RESEARCH_FIXTURE["sha256"]
RESEARCH_VERSION = RESEARCH_FIXTURE["canonical_version"]
ATTESTATION_KEY = "stage6-provider-attestation-test-key-0123456789"
STAGE5_SPEC = (
    Path(__file__).parents[2]
    / "research"
    / "experiments"
    / "stage5-canonical.v1.json"
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
        "approval": {
            "dsl_profile": "research-v1",
            "dsl_canonical_version": RESEARCH_VERSION,
            "dsl_sha256": RESEARCH_SHA,
        },
        "formal_execution": {"passed": True},
        "oracle": {
            "schema_version": "agentic-e2e.oracle.v1",
            "acceptance_id": "fixture-task.v1",
            "passed": True,
            "checks": {
                "outcome": {
                    "passed": True,
                    "expected": "expected",
                    "actual": "expected",
                }
            },
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
    def test_spec_references_versioned_acceptance_and_controls(self) -> None:
        spec = load_experiment_spec(DEFAULT_SPEC)

        self.assertEqual(spec["repetitions"], 3)
        self.assertEqual(spec["controls"]["dsl_profile"], "research-v1")
        self.assertEqual(spec["timeouts"]["run_seconds"], 900)
        self.assertEqual(spec["timeouts"]["experiment_seconds"], 3600)
        self.assertEqual(spec["timeouts"]["request_seconds"], 30)
        self.assertEqual(spec["timeouts"]["long_operation_seconds"], 300)
        self.assertEqual(
            spec["acceptance_spec"],
            "research/acceptance/automationexercise-blue-top-cart.v1.json",
        )
        self.assertEqual(
            spec["acceptance"]["goal"],
            spec["acceptance"]["goal"].strip(),
        )
        self.assertNotIn("dsl_case", spec)
        self.assertNotIn("candidates", str(spec).casefold())

    def test_same_acceptance_supports_legacy_and_research_profiles(self) -> None:
        stage5 = load_experiment_spec(STAGE5_SPEC)
        stage6 = load_experiment_spec(DEFAULT_SPEC)

        self.assertEqual(stage5["controls"]["dsl_profile"], "legacy-v1")
        self.assertEqual(stage6["controls"]["dsl_profile"], "research-v1")
        self.assertEqual(stage5["acceptance"], stage6["acceptance"])

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
            all(
                item[1]["acceptance"]["id"] == spec["acceptance"]["id"]
                for item in invocations
            )
        )
        self.assertTrue(
            all(item[1]["clean_context"] is True for item in invocations)
        )
        self.assertTrue(
            all(
                item[1]["expected_dsl_profile"] == "research-v1"
                for item in invocations
            )
        )
        self.assertTrue(
            all(
                item[1]["timeout_seconds"] == 900
                for item in invocations
            )
        )
        self.assertTrue(
            all(
                item[1]["client"]["request_timeout"] == 30
                and item[1]["client"]["long_operation_timeout"] == 300
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
        self.assertEqual(
            create_payload["config"]["acceptance_spec_id"],
            spec["acceptance"]["id"],
        )
        self.assertEqual(
            len(create_payload["config"]["acceptance_spec_sha256"]),
            64,
        )
        self.assertNotIn(
            "long_operation_timeout_seconds",
            create_payload["config"],
        )
        oracle_call = next(
            entry for entry in client.calls if entry[0] == "put_oracle"
        )
        fact = oracle_call[4]["decision_facts"][0]
        self.assertEqual(fact["expected"], "expected")
        self.assertEqual(fact["actual"], "expected")
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
        failed["oracle"]["checks"]["outcome"] = {
            "passed": False,
            "expected": "expected",
            "actual": "unexpected",
        }

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
                code_snapshot_sha256=lambda: (
                    ("0" if spec["controls"]["code_sha256"][0] != "0" else "1")
                    + spec["controls"]["code_sha256"][1:]
                ),
            )

        self.assertEqual(client.calls, [])

    def test_code_snapshot_hash_is_deterministic_and_content_sensitive(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / "browser-worker" / "src" / "browser_worker" / "worker.py"
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

    def test_code_snapshot_covers_stage6_contract_owners(self) -> None:
        for path in (
            "backend-go/internal/agent/loop.go",
            "backend-go/internal/config/config.go",
            "backend-go/internal/dsl/action_ir.go",
            "backend-go/internal/platform/llm/openai.go",
            "backend-go/internal/tools/dsl.go",
            "backend-go/internal/harness/harness.go",
            "browser-worker/src/browser_worker/contracts/action_ir.py",
            "browser-worker/src/browser_worker/exploration/locator_preflight.py",
        ):
            with self.subTest(path=path):
                self.assertTrue(_is_code_snapshot_path(path))

    def test_code_snapshot_hash_tracks_each_provider_owner_root(self) -> None:
        source_paths = (
            "backend-go/internal/agent/snapshot_probe.go",
            "backend-go/internal/platform/llm/snapshot_probe.go",
            "backend-go/internal/config/snapshot_probe.go",
        )
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            subprocess.run(
                ["git", "init", "--quiet", str(root)],
                check=True,
                capture_output=True,
            )
            for relative_path in source_paths:
                source = root / relative_path
                source.parent.mkdir(parents=True, exist_ok=True)
                source.write_text("package snapshot\n", encoding="utf-8")
            baseline = compute_code_snapshot_sha256(root)

            for relative_path in source_paths:
                with self.subTest(path=relative_path):
                    source = root / relative_path
                    source.write_text(
                        "package snapshot\n\nconst changed = true\n",
                        encoding="utf-8",
                    )
                    self.assertNotEqual(
                        baseline,
                        compute_code_snapshot_sha256(root),
                    )
                    source.write_text(
                        "package snapshot\n",
                        encoding="utf-8",
                    )
                    self.assertEqual(
                        baseline,
                        compute_code_snapshot_sha256(root),
                    )

    def test_code_snapshot_hash_ignores_non_source_tests_and_ignored_paths(
        self,
    ) -> None:
        ignored_paths = (
            "backend-go/internal/agent/README.md",
            "backend-go/internal/platform/llm/openai_test.go",
            "backend-go/internal/config/tests/config.go",
            "backend-go/internal/config/build/generated.go",
        )
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            subprocess.run(
                ["git", "init", "--quiet", str(root)],
                check=True,
                capture_output=True,
            )
            source = root / "backend-go/internal/agent/snapshot_probe.go"
            source.parent.mkdir(parents=True)
            source.write_text("package snapshot\n", encoding="utf-8")
            baseline = compute_code_snapshot_sha256(root)

            for relative_path in ignored_paths:
                ignored = root / relative_path
                ignored.parent.mkdir(parents=True, exist_ok=True)
                ignored.write_text("changed\n", encoding="utf-8")
                with self.subTest(path=relative_path):
                    self.assertEqual(
                        baseline,
                        compute_code_snapshot_sha256(root),
                    )

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

    def get_experiment(self, experiment_id, project_id):
        return {
            "id": experiment_id,
            "project_id": project_id,
            "dsl_profile": "research-v1",
            "model_provider": "deepseek",
            "model_name": "deepseek-v4-flash",
        }

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
            "started_at": f"2026-09-07T06:{index:02d}:00Z",
            "finished_at": f"2026-09-07T06:{index:02d}:59Z",
            "links": {
                "agent_run_id": f"agent-{index}",
                "generation_id": 10 + index,
                "batch_id": 20 + index,
                "execution_id": 30 + index,
                "dsl_sha256": RESEARCH_SHA,
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

    def list_agent_events(self, run_id):
        index = int(run_id.rsplit("-", 1)[1])
        generation_id = 10 + index
        return [
            {
                "seq": 1,
                "type": "tool.result",
                "tool_call_id": f"generation-{index}",
                "payload": {
                    "tool": "generate_dsl",
                    "content": {
                        "generation_id": generation_id,
                        "case": RESEARCH_CASE,
                        "profile": "research-v1",
                        "dsl_canonical_version": RESEARCH_VERSION,
                        "dsl_sha256": RESEARCH_SHA,
                    },
                },
            },
            {
                "seq": 2,
                "type": "artifact.published",
                "tool_call_id": f"generation-{index}",
                "payload": {
                    "type": "dsl_generation",
                    "id": str(generation_id),
                },
            },
            {
                "seq": 3,
                "type": "tool.pending",
                "tool_call_id": f"approval-{index}",
                "payload": {
                    "tool": "ask_user_question",
                    "questions": [{"id": "approve_dsl"}],
                },
            },
            {
                "seq": 4,
                "type": "tool.result",
                "tool_call_id": f"approval-{index}",
                "payload": {
                    "tool": "ask_user_question",
                    "answers": {"approve_dsl": True},
                },
            },
            {
                "seq": 5,
                "type": "research.llm_call",
                "run_id": run_id,
                "payload": {
                    "schema_version": "research.llm_call.v1",
                    "logical_call_id": f"logical-{index}",
                    "provider": "deepseek",
                    "requested_model": "deepseek-v4-flash",
                    "resolved_model": "deepseek-v4-flash",
                    "attempt_status": "succeeded",
                    "attempt_started_at": (
                        f"2026-09-07T06:{index:02d}:30Z"
                    ),
                    "http_status": 200,
                    "endpoint_scheme": "https",
                    "endpoint_host": "api.deepseek.com",
                    "credential_fingerprint": "sha256:v1:" + "a" * 64,
                    "client_request_id": f"client-{index}",
                    "provider_response_id": f"response-{index}",
                    "provider_header_request_id": f"header-{index}",
                    "provider_header_request_id_header": "x-request-id",
                    "local_response_cache": "not_configured",
                    "usage": {
                        "status": "available",
                        "input_tokens": 15,
                        "output_tokens": 5,
                        "total_tokens": 20,
                        "prompt_cache_hit_tokens": 10,
                        "prompt_cache_miss_tokens": 5,
                    },
                },
            },
        ]

    def get_batch_report(self, batch_id):
        index = batch_id - 20
        binding = {
            "dsl_profile": "research-v1",
            "dsl_canonical_version": RESEARCH_VERSION,
            "dsl_sha256": RESEARCH_SHA,
        }
        return {
            "id": batch_id,
            "status": "passed",
            **binding,
            "jobs": [
                {
                    **binding,
                    "latest_execution": {
                        "id": 30 + index,
                        "status": "passed",
                        **binding,
                        "report": {
                            **binding,
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
    def _signed_attestation(
        self,
        *,
        key: str = ATTESTATION_KEY,
    ) -> tuple[dict, str]:
        pending_result = verify_experiment(
            "experiment-1",
            client=VerificationClient(),
            project_id=7,
            allow_pending_platform_attestation=True,
        )
        pending = pending_result["provider_evidence"][
            "pending_platform_attestation"
        ]
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            pending_path = root / "pending.json"
            platform_path = root / "deepseek-usage.json"
            output_path = root / "attestation.json"
            pending_path.write_text(
                json.dumps(pending), encoding="utf-8"
            )
            platform_path.write_bytes(b'{"verified":"on-platform"}\n')
            stdout = StringIO()
            with (
                patch.dict(
                    "os.environ",
                    {PROVIDER_ATTESTATION_KEY_ENV: key},
                    clear=False,
                ),
                redirect_stdout(stdout),
            ):
                exit_code = main(
                    [
                        "provider-attest",
                        "--pending",
                        str(pending_path),
                        "--platform-evidence",
                        str(platform_path),
                        "--source",
                        "deepseek_usage_api",
                        "--reviewer",
                        "Research Reviewer",
                        "--organization",
                        "Research Org",
                        "--key-id",
                        "research-attestation-2026-09",
                        "--output",
                        str(output_path),
                    ]
                )
            self.assertEqual(exit_code, 0)
            return (
                json.loads(output_path.read_text(encoding="utf-8")),
                stdout.getvalue(),
            )

    def test_verify_requires_three_canonical_successes(self) -> None:
        result = verify_experiment(
            "experiment-1",
            client=VerificationClient(),
            project_id=7,
        )

        self.assertFalse(result["passed"])
        self.assertEqual(result["non_warmup_runs"], 3)
        self.assertEqual(result["clean_session_count"], 3)
        self.assertTrue(all(run["task_success"] for run in result["runs"]))
        self.assertTrue(all(run["oracle_passed"] for run in result["runs"]))
        self.assertEqual(result["dsl_profile"], "research-v1")
        self.assertEqual(result["dsl_canonical_version"], RESEARCH_VERSION)
        evidence = result["provider_evidence"]
        self.assertTrue(evidence["local_provider_evidence_verified"])
        self.assertFalse(evidence["provider_e2e_verified"])
        self.assertEqual(
            evidence["reason"], "platform_attestation_required"
        )
        self.assertEqual(evidence["successful_attempt_count"], 3)

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

        self.assertFalse(result["passed"])
        self.assertFalse(result["expected_task_success"])

    def test_pending_attestation_cannot_mark_stage_passed(self) -> None:
        result = verify_experiment(
            "experiment-1",
            client=VerificationClient(),
            project_id=7,
            allow_pending_platform_attestation=True,
        )
        package = result["provider_evidence"][
            "pending_platform_attestation"
        ]

        self.assertFalse(result["passed"])
        evidence = result["provider_evidence"]
        self.assertTrue(evidence["local_provider_evidence_verified"])
        self.assertFalse(evidence["provider_e2e_verified"])
        self.assertEqual(
            evidence["reason"], "platform_attestation_required"
        )
        self.assertEqual(
            package["signature"]["algorithm"],
            PROVIDER_ATTESTATION_ALGORITHM,
        )
        self.assertEqual(
            package["platform_evidence"][
                "matched_provider_response_ids"
            ],
            [],
        )

    def test_valid_platform_attestation_passes_provider_gate(self) -> None:
        attestation, stdout = self._signed_attestation()

        with patch.dict(
            "os.environ",
            {PROVIDER_ATTESTATION_KEY_ENV: ATTESTATION_KEY},
            clear=False,
        ):
            result = verify_experiment(
                "experiment-1",
                client=VerificationClient(),
                project_id=7,
                provider_attestation=attestation,
            )

        self.assertTrue(result["passed"])
        evidence = result["provider_evidence"]
        self.assertTrue(evidence["provider_e2e_verified"])
        self.assertEqual(
            evidence["verification_scope"],
            "local_and_platform_attested",
        )
        self.assertIsNone(evidence["reason"])
        self.assertTrue(evidence["attestation"]["signature_verified"])
        self.assertNotIn(ATTESTATION_KEY, stdout)
        self.assertNotIn("verified", json.dumps(attestation))
        self.assertEqual(
            attestation["platform_evidence"][
                "matched_provider_response_ids"
            ],
            ["response-0", "response-1", "response-2"],
        )

    def test_attestation_load_rejects_wrong_key(self) -> None:
        attestation, _ = self._signed_attestation()
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "attestation.json"
            path.write_text(json.dumps(attestation), encoding="utf-8")
            with (
                patch.dict(
                    "os.environ",
                    {
                        PROVIDER_ATTESTATION_KEY_ENV:
                            "different-attestation-key-01234567890123456789"
                    },
                    clear=False,
                ),
                self.assertRaisesRegex(
                    ResearchE2EError, "signature verification failed"
                ),
            ):
                load_provider_attestation(path)

    def test_verify_rejects_signed_attestation_tampering(self) -> None:
        attestation, _ = self._signed_attestation()
        attestation["runs"][0]["attempts"][0]["usage"][
            "total_tokens"
        ] += 1

        with (
            patch.dict(
                "os.environ",
                {PROVIDER_ATTESTATION_KEY_ENV: ATTESTATION_KEY},
                clear=False,
            ),
            self.assertRaisesRegex(
                ResearchE2EError, "signature verification failed"
            ),
        ):
            verify_experiment(
                "experiment-1",
                client=VerificationClient(),
                project_id=7,
                provider_attestation=attestation,
            )

    def test_verify_rejects_attestation_replay(self) -> None:
        attestation, _ = self._signed_attestation()

        with (
            patch.dict(
                "os.environ",
                {PROVIDER_ATTESTATION_KEY_ENV: ATTESTATION_KEY},
                clear=False,
            ),
            self.assertRaisesRegex(
                ResearchE2EError, "experiment_id does not match"
            ),
        ):
            verify_experiment(
                "experiment-replay",
                client=VerificationClient(),
                project_id=7,
                provider_attestation=attestation,
            )

    def test_verify_rejects_missing_platform_artifact_hash(self) -> None:
        attestation, _ = self._signed_attestation()
        attestation["platform_evidence"].pop("artifact_sha256")

        with (
            patch.dict(
                "os.environ",
                {PROVIDER_ATTESTATION_KEY_ENV: ATTESTATION_KEY},
                clear=False,
            ),
            self.assertRaisesRegex(
                ResearchE2EError, "platform_evidence is invalid"
            ),
        ):
            verify_experiment(
                "experiment-1",
                client=VerificationClient(),
                project_id=7,
                provider_attestation=attestation,
            )

    def test_verify_rejects_incomplete_platform_response_id_coverage(
        self,
    ) -> None:
        attestation, _ = self._signed_attestation()
        attestation["platform_evidence"][
            "matched_provider_response_ids"
        ].pop()
        attestation["signature"]["value"] = (
            _provider_attestation_signature_value(
                attestation, ATTESTATION_KEY.encode("utf-8")
            )
        )

        with (
            patch.dict(
                "os.environ",
                {PROVIDER_ATTESTATION_KEY_ENV: ATTESTATION_KEY},
                clear=False,
            ),
            self.assertRaisesRegex(
                ResearchE2EError, "do not exactly cover"
            ),
        ):
            verify_experiment(
                "experiment-1",
                client=VerificationClient(),
                project_id=7,
                provider_attestation=attestation,
            )

    def test_attestation_schema_exposes_required_audit_fields(self) -> None:
        required = set(PROVIDER_ATTESTATION_JSON_SCHEMA["required"])

        self.assertEqual(
            required,
            {
                "schema_version",
                "experiment_id",
                "source_sha256",
                "evidence_artifact_sha256",
                "runs",
                "platform_evidence",
                "reviewer",
                "signature",
            },
        )
        self.assertEqual(
            PROVIDER_ATTESTATION_JSON_SCHEMA["properties"]["signature"][
                "properties"
            ]["algorithm"]["const"],
            PROVIDER_ATTESTATION_ALGORITHM,
        )

    def test_verify_rejects_missing_provider_field(self) -> None:
        class MissingFieldClient(VerificationClient):
            def list_agent_events(self, run_id):
                events = super().list_agent_events(run_id)
                del events[-1]["payload"]["endpoint_scheme"]
                return events

        with self.assertRaisesRegex(
            ResearchE2EError, "endpoint_scheme"
        ):
            verify_experiment(
                "experiment-1",
                client=MissingFieldClient(),
                project_id=7,
            )

    def test_verify_rejects_wrong_deepseek_host(self) -> None:
        class WrongHostClient(VerificationClient):
            def list_agent_events(self, run_id):
                events = super().list_agent_events(run_id)
                events[-1]["payload"]["endpoint_host"] = "gateway.example"
                return events

        with self.assertRaisesRegex(
            ResearchE2EError, "invalid DeepSeek host"
        ):
            verify_experiment(
                "experiment-1",
                client=WrongHostClient(),
                project_id=7,
            )

    def test_verify_rejects_cross_run_provider_ids(self) -> None:
        class DuplicateIDClient(VerificationClient):
            def list_agent_events(self, run_id):
                events = super().list_agent_events(run_id)
                events[-1]["payload"]["client_request_id"] = "duplicate"
                events[-1]["payload"]["provider_response_id"] = "duplicate"
                return events

        with self.assertRaisesRegex(
            ResearchE2EError, "reused across formal runs"
        ):
            verify_experiment(
                "experiment-1",
                client=DuplicateIDClient(),
                project_id=7,
            )

    def test_verify_rejects_attempt_outside_run_window(self) -> None:
        class OutsideWindowClient(VerificationClient):
            def list_agent_events(self, run_id):
                events = super().list_agent_events(run_id)
                events[-1]["payload"][
                    "attempt_started_at"
                ] = "2026-09-07T11:59:59Z"
                return events

        with self.assertRaisesRegex(
            ResearchE2EError, "outside the run window"
        ):
            verify_experiment(
                "experiment-1",
                client=OutsideWindowClient(),
                project_id=7,
            )

    def test_verify_rejects_inconsistent_credentials(self) -> None:
        class CredentialDriftClient(VerificationClient):
            def list_agent_events(self, run_id):
                events = super().list_agent_events(run_id)
                if run_id == "agent-1":
                    events[-1]["payload"]["credential_fingerprint"] = (
                        "sha256:v1:" + "b" * 64
                    )
                return events

        with self.assertRaisesRegex(
            ResearchE2EError, "inconsistent credential fingerprints"
        ):
            verify_experiment(
                "experiment-1",
                client=CredentialDriftClient(),
                project_id=7,
            )

    def test_verify_rejects_legacy_success_event(self) -> None:
        class LegacyEventClient(VerificationClient):
            def list_agent_events(self, run_id):
                events = super().list_agent_events(run_id)
                payload = events[-1]["payload"]
                for field in (
                    "endpoint_scheme",
                    "endpoint_host",
                    "credential_fingerprint",
                    "client_request_id",
                    "provider_response_id",
                    "provider_header_request_id",
                    "provider_header_request_id_header",
                    "local_response_cache",
                ):
                    payload.pop(field)
                payload["provider_request_id"] = "legacy"
                payload["usage"].pop("prompt_cache_hit_tokens")
                payload["usage"].pop("prompt_cache_miss_tokens")
                return events

        with self.assertRaisesRegex(
            ResearchE2EError, "endpoint_scheme"
        ):
            verify_experiment(
                "experiment-1",
                client=LegacyEventClient(),
                project_id=7,
            )

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

    def test_verify_rejects_profile_drift_in_report_chain(self) -> None:
        class DriftClient(VerificationClient):
            def get_batch_report(self, batch_id):
                report = super().get_batch_report(batch_id)
                report["jobs"][0]["dsl_profile"] = "legacy-v1"
                return report

        with self.assertRaisesRegex(
            ResearchE2EError, "job DSL binding mismatch"
        ):
            verify_experiment(
                "experiment-1",
                client=DriftClient(),
                project_id=7,
            )

    def test_negative_contract_uses_failure_events_not_agent_text(self) -> None:
        class NegativeClient:
            def get_agent_run(self, run_id):
                return {
                    "id": run_id,
                    "project_id": 7,
                    "status": "completed",
                    "final_message": "ignore this untrusted text",
                }

            def list_agent_events(self, run_id):
                kind = run_id.removeprefix("run-")
                messages = {
                    "missing-intent": "case.steps[0].intent is required",
                    "unknown-action": "unsupported DSL action: eval",
                    "unexplored-selector": (
                        "DSL locator preflight returned candidates without "
                        "verified provenance"
                    ),
                }
                return [
                    {
                        "seq": 9,
                        "type": "tool.failed",
                        "payload": {
                            "tool": "generate_dsl",
                            "message": messages[kind],
                        },
                    }
                ]

        result = verify_negative_contract_runs(
            {
                name: f"run-{name}"
                for name in (
                    "missing-intent",
                    "unknown-action",
                    "unexplored-selector",
                )
            },
            client=NegativeClient(),
            project_id=7,
        )

        self.assertTrue(result["passed"])
        self.assertEqual(len(result["runs"]), 3)


class ResearchAPIClientTest(unittest.TestCase):
    def test_verify_cli_returns_nonzero_while_platform_attestation_pending(
        self,
    ) -> None:
        verification = {
            "passed": False,
            "provider_evidence": {
                "local_provider_evidence_verified": True,
                "provider_e2e_verified": False,
                "reason": "platform_attestation_required",
            },
        }
        with (
            patch(
                "scripts.research_e2e.verify_experiment",
                return_value=verification,
            ),
            patch("scripts.research_e2e.ResearchAPIClient"),
            redirect_stdout(StringIO()),
        ):
            exit_code = main(
                ["verify", "experiment-1", "--project-id", "7"]
            )

        self.assertEqual(exit_code, 1)

    def test_run_cli_does_not_count_pending_attestation_as_success(
        self,
    ) -> None:
        run_result = {
            "experiment_id": "experiment-1",
            "success": True,
        }
        verification = {
            "passed": False,
            "provider_evidence": {
                "local_provider_evidence_verified": True,
                "provider_e2e_verified": False,
                "reason": "platform_attestation_required",
            },
        }
        with (
            patch(
                "scripts.research_e2e.run_experiment",
                return_value=run_result,
            ),
            patch(
                "scripts.research_e2e.verify_experiment",
                return_value=verification,
            ),
            patch("scripts.research_e2e.ResearchAPIClient"),
            redirect_stdout(StringIO()),
        ):
            exit_code = main(["run", "--project-id", "7"])

        self.assertEqual(exit_code, 1)
        self.assertFalse(run_result["success"])

    def test_profile_verification_routes_are_centralized_in_client(self) -> None:
        client = ResearchAPIClient("http://agent.test")
        with patch.object(
            client,
            "_json",
            side_effect=[
                {"id": "experiment-1", "dsl_profile": "research-v1"},
                {"events": [{"seq": 1}]},
            ],
        ) as request:
            experiment = client.get_experiment("experiment-1", 7)
            events = client.list_agent_events("agent-1")

        self.assertEqual(experiment["dsl_profile"], "research-v1")
        self.assertEqual(events, [{"seq": 1}])
        self.assertEqual(
            request.call_args_list,
            [
                call(
                    "GET",
                    "/api/v2/research/experiments/experiment-1?project_id=7",
                ),
                call(
                    "GET",
                    "/api/v2/agent/runs/agent-1/events?after_seq=0",
                ),
            ],
        )

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
