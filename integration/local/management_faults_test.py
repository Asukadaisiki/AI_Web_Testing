"""Offline fault-harness safety/evidence tests; never signal a real process."""
import importlib.util
from pathlib import Path
import json
import os
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).parent))
path = Path(__file__).with_name("management_faults.py")
module = None
if path.exists():
    spec = importlib.util.spec_from_file_location("management_faults", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)


class FaultTests(unittest.TestCase):
    def setUp(self):
        self.assertIsNotNone(module, "finite fault harness has not been implemented")

    def table(self):
        return module.parse_processes(
            "100 1 Wed Oct 28 12:00:00 2026 100 /bin/owner\n"
            "101 100 Wed Oct 28 12:00:01 2026 200 /tmp/agenttrace-runtime\n"
            "102 101 Wed Oct 28 12:00:02 2026 300 node sdk-host --secret private\n"
            "999 1 Wed Oct 28 12:00:03 2026 400 /tmp/agenttrace-runtime\n")

    def test_process_memory_includes_only_selected_descendants(self):
        table = self.table()
        self.assertEqual(module.tree_rss(table, 101), 500)
        self.assertEqual(module.tree_rss(table, 100), 600)

    def test_live_process_inspection_is_independent_of_caller_locale(self):
        with patch.dict(os.environ, {"LC_ALL": "zh_CN.UTF-8", "LANG": "zh_CN.UTF-8"}):
            table = module.processes()
        self.assertIn(os.getpid(), table)
        self.assertEqual(table[os.getpid()]["ppid"], os.getppid())
        self.assertGreater(table[os.getpid()]["rss_kib"], 0)

    def test_signal_guard_refuses_nonowned_process_or_owner_itself(self):
        guard = module.ProcessGuard(self.table(), 100, Path("/tmp/agenttrace-runtime"))
        for pid in (100, 999, 12345):
            with self.assertRaises(module.CheckFailure):
                guard.verify(pid, self.table())

    def test_signal_guard_refuses_pid_reuse_and_wrong_executable(self):
        guard = module.ProcessGuard(self.table(), 100, Path("/tmp/agenttrace-runtime"))
        self.assertEqual(guard.verify(101, self.table())["pid"], 101)
        changed = self.table()
        changed[101]["started"] = "Wed Oct 28 13:00:01 2026"
        with self.assertRaises(module.CheckFailure):
            guard.verify(101, changed)
        with self.assertRaises(module.CheckFailure):
            guard.verify(102, self.table())

    def test_signal_guard_refuses_supervisor_pid_reuse(self):
        guard = module.ProcessGuard(self.table(), 100, Path("/tmp/agenttrace-runtime"))
        changed = self.table()
        changed[100]["started"] = "Wed Oct 28 13:00:01 2026"
        with self.assertRaises(module.CheckFailure):
            guard.verify(101, changed)

    def test_signal_requires_explicit_optin_even_for_owned_runtime(self):
        guard = module.ProcessGuard(self.table(), 100, Path("/tmp/agenttrace-runtime"))
        with self.assertRaises(module.CheckFailure):
            module.signal_evidence(guard, 101, self.table(), allowed=False)
        evidence = module.signal_evidence(guard, 101, self.table(), allowed=True)
        self.assertEqual(evidence["pid"], 101)
        self.assertNotIn("command", evidence)

    def test_latency_statistics_do_not_turn_missing_measurements_into_zero(self):
        values = module.distribution([None, 1.0, 2.0, 3.0, 4.0, 5.0, 6.0, 7.0, 8.0, 9.0, 10.0])
        self.assertEqual(values["count"], 10)
        self.assertEqual(values["p50"], 5.0)
        self.assertEqual(values["p95"], 10.0)
        self.assertEqual(module.distribution([None])["count"], 0)
        self.assertIsNone(module.distribution([None])["p95"])

    def test_postsideeffect_proof_rejects_model_claim_without_browser_assertion(self):
        trace = {"spans": [
            {"name": "browser_click", "status": "succeeded", "input": {}},
            {"name": "browser_assert_text", "status": "succeeded",
             "input": {"text": "Signed in: owned@example.test"}},
        ]}
        self.assertTrue(module.witnessed_login(trace, "owned@example.test"))
        self.assertFalse(module.witnessed_login(trace, "different@example.test"))
        trace["spans"][0]["status"] = "failed"
        self.assertFalse(module.witnessed_login(trace, "owned@example.test"))

    def test_postsideeffect_proof_accepts_privacy_safe_browser_assertion_output(self):
        trace = {"spans": [
            {"name": "browser_click", "status": "succeeded", "input": {}},
            {"name": "browser_assert_text", "status": "succeeded",
             "input": {"tool_call_id": "private-arguments-omitted"},
             "output": {"result": {"structuredContent": {
                 "ok": True, "steps": [{"action": "assert_text",
                                       "value": "Signed in: owned@example.test"}]}}}},
        ]}
        self.assertTrue(module.witnessed_login(trace, "owned@example.test"))
        self.assertFalse(module.witnessed_login(trace, "different@example.test"))
        trace["spans"][1]["output"]["result"]["structuredContent"]["ok"] = False
        self.assertFalse(module.witnessed_login(trace, "owned@example.test"))

    def test_postsideeffect_proof_rejects_transport_success_with_failed_assertion(self):
        trace = {"spans": [
            {"name": "browser_click", "status": "succeeded", "input": {}},
            {"name": "browser_assert_text", "status": "succeeded",
             "input": {"text": "Signed in: owned@example.test"},
             "output": {"structured_result": {"ok": False}}},
        ]}
        self.assertFalse(module.witnessed_login(trace, "owned@example.test"))

    def test_browser_action_measurement_counts_actual_operation_spans_not_observations(self):
        trace = {"spans": [
            {"kind": "operation", "name": "mcp__browser_harness__open_page", "status": "succeeded"},
            {"kind": "tool", "name": "browser_assert_text", "status": "succeeded"},
            {"kind": "operation", "name": "observation", "status": "succeeded"},
            {"kind": "model", "name": "message", "status": "succeeded"},
            {"kind": "operation", "name": "mcp__browser_harness__click", "status": "failed"},
        ]}
        self.assertEqual(module.count_browser_actions(trace), 2)

    def test_recovery_requires_lost_attempt_expired_lease_and_advanced_fence(self):
        value = {"status": "failed", "error_code": "execution_lost", "environment_state": "needs_review",
                 "attempt_status": "lost", "assignment_state": "expired", "lease_state": "expired",
                 "fencing_token": 8, "attempt_count": 1}
        self.assertTrue(module.recovered_after_effect(value, fence_before=7, attempt_count=1))
        for key, changed in (("fencing_token", 7), ("attempt_count", 2),
                             ("environment_state", "ready"), ("attempt_status", "active"),
                             ("status", "succeeded"), ("lease_state", "active")):
            self.assertFalse(module.recovered_after_effect({**value, key: changed}, 7, 1))

    def test_load_and_deadline_are_finite(self):
        for count, timeout in ((3, 600), (25, 600), (6, 0), (6, 901)):
            with self.assertRaises(module.CheckFailure):
                module.validate_bounds(count, timeout)
        module.validate_bounds(6, 600)

    def test_cli_invalid_load_records_failure_without_contacting_stack(self):
        with tempfile.TemporaryDirectory() as directory:
            report = Path(directory) / "report.json"
            result = subprocess.run([sys.executable, str(path), "--runs", "25",
                                     "--runtime-worker", "first", "--runtime-worker", "second",
                                     "--report", str(report)], capture_output=True, text=True, timeout=5)
            self.assertEqual(result.returncode, 1)
            evidence = json.loads(report.read_text())
            self.assertEqual(evidence["status"], "failed")
            self.assertEqual(evidence["checks"], {})
            self.assertNotIn("token", json.dumps(evidence))
            self.assertEqual(report.stat().st_mode & 0o777, 0o600)

    def test_production_database_is_rejected_before_control_api_mutation(self):
        args = module.parser().parse_args(["--runtime-worker", "first", "--runtime-worker", "second"])
        runner = module.Faults({"AGENTTRACE_PROJECT_ID": "prj_test", "AGENTTRACE_LANGFUSE_PROJECT_ID": "lf-test",
                                "AGENTTRACE_BRIDGE_TOKEN": "private",
                                "AGENTTRACE_E2E_POSTGRES_DSN": "postgresql:///agenttrace_local"}, args)
        with patch.object(runner.opener, "open") as transport:
            with self.assertRaises(module.CheckFailure):
                runner.preflight_faults()
            transport.assert_not_called()


if __name__ == "__main__":
    unittest.main()
