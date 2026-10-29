"""Offline tests; never start or mutate an application stack."""
import importlib.util
import json
from pathlib import Path
import sys
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).parent))
spec = importlib.util.spec_from_file_location("management_acceptance", Path(__file__).with_name("management.py"))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class ManagementTests(unittest.TestCase):
    def env(self):
        return {"AGENTTRACE_BRIDGE_TOKEN": "private-bridge",
                "AGENTTRACE_PROJECT_ID": "prj_test",
                "AGENTTRACE_LANGFUSE_PROJECT_ID": "lf-test",
                "AGENTTRACE_API_URL": "http://127.0.0.1:8181",
                "AGENTTRACE_E2E_POSTGRES_DSN": "postgresql:///agenttrace_test_unused"}

    def test_every_request_has_authenticated_project_scope(self):
        runner = module.Management(self.env(), 1)
        class Response:
            status = 200
            def __enter__(self):
                return self
            def __exit__(self, *args):
                pass
            def read(self):
                return b'{"status":"ok"}'
        with patch.object(runner.opener, "open", return_value=Response()) as request:
            runner.request("/health")
        sent = request.call_args[0][0]
        self.assertEqual(sent.full_url, "http://127.0.0.1:8181/api/v1/health")
        for key, value in runner.headers.items():
            self.assertEqual(sent.get_header(key.capitalize()), value)

    def test_missing_binding_fails_before_any_request(self):
        for key in ("AGENTTRACE_PROJECT_ID", "AGENTTRACE_LANGFUSE_PROJECT_ID", "AGENTTRACE_BRIDGE_TOKEN"):
            env = self.env()
            env.pop(key)
            with self.assertRaises(module.CheckFailure):
                module.Management(env, 1)

    def test_bridge_credentials_are_not_forwarded_outside_api_origin(self):
        runner = module.Management(self.env(), 1)
        with patch.object(runner.opener, "open") as request:
            with self.assertRaises(module.CheckFailure):
                runner.request("http://untrusted.example/private")
            request.assert_not_called()

    def test_bridge_redirects_fail_closed_before_forwarding_headers(self):
        handler = module.NoCredentialRedirect()
        request = module.urllib.request.Request("http://127.0.0.1:8181/api/v1/health",
                                              headers={"Authorization": "Bearer private"})
        with self.assertRaises(module.CheckFailure):
            handler.redirect_request(request, None, 302, "Found", {},
                                     "http://untrusted.example/private")

    def test_unavailable_or_scripted_sdk_is_not_counted_as_success(self):
        runner = module.Management(self.env(), 1)
        selected = runner.select_runtimes([
            {"id": "pi", "available": True, "model": {"provider": "scripted"}},
            {"id": "claude-code", "available": False, "unavailable_reason": "no credentials"},
            {"id": "codex", "available": True, "model": {"provider": "openai-responses"}},
        ])
        self.assertEqual(selected, ["codex"])
        self.assertEqual(runner.report["checks"]["sdk-pi"]["status"], "skipped")
        self.assertEqual(runner.report["checks"]["sdk-claude-code"]["status"], "skipped")
        self.assertEqual(runner.outcome(), "provisional")

    def test_browser_evidence_requires_real_tools_tokens_and_png(self):
        runner = module.Management(self.env(), 1)
        trace = {"spans": [{"kind": "model", "name": "completion", "status": "succeeded"}, {"kind": "tool", "name": "open_page", "status": "succeeded"},
                           {"kind": "tool", "name": "assert_text", "status": "succeeded"}],
                 "usage": [{"total_tokens": 3}], "operation_evidence": []}
        with self.assertRaises(module.CheckFailure):
            runner.browser_evidence("run_test", trace, finish_case=True)

    def test_evidence_redacts_capabilities_and_credentials(self):
        sanitized = module.sanitize({"token": "one-time-secret", "authorization": "secret",
                                     "control_url": "http://worker/private?token=secret",
                                     "run_id": "run_test", "nested": {"api_key": "secret"}})
        self.assertNotIn("secret", json.dumps(sanitized))
        self.assertEqual(sanitized["run_id"], "run_test")

    def test_identity_fixture_uses_persistable_local_storage(self):
        self.assertIn("localStorage", module.IDENTITY_HTML)
        self.assertNotIn("sessionStorage", module.IDENTITY_HTML)
        self.assertIn("Signed out", module.IDENTITY_HTML)

    def test_read_only_sql_scope_rejects_injection(self):
        self.assertEqual(module.record_id("conv_abc123", "conv"), "'conv_abc123'")
        with self.assertRaises(module.CheckFailure):
            module.record_id("conv_abc'; DROP TABLE conversations;--", "conv")

    def test_failure_is_not_hidden_by_provisional_limitations(self):
        runner = module.Management(self.env(), 1)
        runner.report["checks"]["real-sdk"] = {"status": "failed"}
        self.assertEqual(runner.outcome(), "failed")

    def test_negative_assertion_checks_domain_failure_independently_of_transport(self):
        trace = {"spans": [{"name": "mcp__browser_harness__assert_text", "status": "succeeded",
                           "output": {"result": {"structuredContent": {"ok": False,
                             "result": {"error": {"code": "condition_unmet"}}}}}}]}
        self.assertTrue(module.has_failed_assertion(trace))
        trace["spans"][0]["output"]["result"]["structuredContent"]["ok"] = True
        self.assertFalse(module.has_failed_assertion(trace))


if __name__ == "__main__":
    unittest.main()
