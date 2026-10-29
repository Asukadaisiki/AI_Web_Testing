"""Regression checks for the native composition acceptance assertions."""
import unittest

from acceptance import CheckFailure
from composition import Composition, RUNTIMES


class LangfuseUsageFixture(Composition):
    """Controlled API responses; the production acceptance assertion runs unchanged."""
    def __init__(self, generation_usage):
        super().__init__({
            "AGENTTRACE_LANGFUSE_PUBLIC_KEY": "test-public",
            "AGENTTRACE_LANGFUSE_SECRET_KEY": "test-secret",
        }, 1)
        self.generation_usage = generation_usage
        self.report["runs"] = {
            "browser-" + runtime: {"run_id": runtime} for runtime in RUNTIMES
        }

    def request(self, path, **_kwargs):
        runtime = path.split("/")[2]
        return {"spans": [
            {"id": "root", "kind": "run"},
            {"id": "message", "kind": "model"},
        ], "usage": [{
            "id": "query", "span_id": "root",
            "details": {"granularity": "sdk_query_model_cumulative"},
        }] if runtime == "claude-code" else []}

    def http(self, url, **_kwargs):
        runtime = url.split("agenttrace-trace-")[-1]
        return {"metadata": {
            "agenttrace_status": "succeeded", "agenttrace_run_id": runtime,
            "agenttrace_sdk_query_usage": [{"id": "query"}],
        }, "observations": [
            {"type": "SPAN", "id": "root"},
            {"type": "GENERATION", "id": "message", **self.generation_usage},
        ]}


class LangfuseUsageAcceptanceTests(unittest.TestCase):
    def test_accepts_langfuse_default_zero_legacy_usage_when_query_totals_are_metadata(self):
        runner = LangfuseUsageFixture({
            "usageDetails": {},
            "usage": {"unit": "TOKENS", "input": 0, "output": 0, "total": 0},
        })
        runner.langfuse()
        self.assertTrue(runner.report["runs"]["browser-claude-code"]["sdk_query_usage_scope_verified"])

    def test_rejects_actual_tokens_assigned_to_query_message_generations(self):
        for usage in [
            {"usageDetails": {"input": 100, "output": 5, "total": 105}},
            {"usageDetails": {"input_cached": 100}},
            {"usageDetails": {}, "usage": {"unit": "TOKENS", "input": 100}},
        ]:
            with self.subTest(usage=usage), self.assertRaisesRegex(
                CheckFailure, "cumulative query tokens"
            ):
                LangfuseUsageFixture(usage).langfuse()


if __name__ == "__main__":
    unittest.main()
