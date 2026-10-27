"""Focused unit tests for the Pi harness canonical acceptance scenario."""

from __future__ import annotations

import json
import os
import sys
import unittest
from pathlib import Path
from unittest.mock import MagicMock, patch

TEST_DIR = str(Path(__file__).resolve().parent)
if TEST_DIR not in sys.path:
    sys.path.insert(0, TEST_DIR)

import pi_harness_scenario
from pi_harness_scenario import (
    HARNESS_EVENT_TYPES,
    assert_concurrent_isolation,
    assert_no_agentserver_endpoint,
    harness_attempt_for_run,
    run_has_failed_assertion_evidence,
    trace_has_required_shape,
)


def fake_psycopg(connection):
    """Return a module-like object whose connect() yields *connection*."""
    module = MagicMock()
    module.connect.return_value = connection
    return module


class HarnessEventTypesTest(unittest.TestCase):
    def test_enumerates_the_complete_protocol_discriminants(self) -> None:
        self.assertEqual(
            HARNESS_EVENT_TYPES,
            (
                "harness.started",
                "pi.message.completed",
                "pi.usage",
                "pi.tool.started",
                "pi.tool.completed",
                "browser.observation",
                "browser.case.completed",
                "harness.settled",
            ),
        )


class _Cursor:
    def __init__(self, *, one=None, all=None):
        self._one = one
        self._all = all
        self.query = ""

    def execute(self, query, params=None):
        self.query = query

    def fetchone(self):
        return self._one

    def fetchall(self):
        return self._all

    def __enter__(self):
        return self

    def __exit__(self, *exc):
        return False


class _Connection:
    def __init__(self, cursor):
        self._cursor = cursor

    def cursor(self):
        return self._cursor

    def __enter__(self):
        return self

    def __exit__(self, *exc):
        return False


def finish_trace(
    *,
    span_attempt_id: str = "attempt-finish",
    derived_kind: str = "step",
):
    finish_span_id = "span-finish"
    return {
        "spans": [
            {
                "span_id": finish_span_id,
                "kind": "operation",
                "name": "mcp__browser_harness__finish_case",
            }
        ],
        "operation_evidence": [
            {
                "span_id": finish_span_id,
                "span_attempt_id": span_attempt_id,
                "result": {"steps": [{"index": 0}]},
            }
        ],
        "derived_nodes": [
            {
                "kind": derived_kind,
                "parent_span_id": finish_span_id,
            }
        ],
    }


class TraceShapeTest(unittest.TestCase):
    def test_requires_model_tools_usage_linked_operation_and_execution_steps(self) -> None:
        cursor = _Cursor(one=(1, 1, 1, 1, 1))
        connection = _Connection(cursor)

        with (
            patch.dict(sys.modules, {"psycopg": fake_psycopg(connection)}),
            patch.object(
                pi_harness_scenario,
                "request",
                return_value=finish_trace(),
            ),
        ):
            shape = trace_has_required_shape("run-1")

        self.assertEqual(
            shape,
            {
                "model_messages": True,
                "browser_tools": True,
                "usage": True,
                "linked_operation": True,
                "execution_steps": True,
                "observations": True,
                "case_completed": True,
            },
        )

    def test_rejects_unlinked_operation_evidence(self) -> None:
        cursor = _Cursor(one=(1, 1, 1, 1, 1))
        connection = _Connection(cursor)

        with (
            patch.dict(sys.modules, {"psycopg": fake_psycopg(connection)}),
            patch.object(
                pi_harness_scenario,
                "request",
                return_value=finish_trace(span_attempt_id=""),
            ),
        ):
            shape = trace_has_required_shape("run-1")

        self.assertFalse(shape["linked_operation"])

    def test_rejects_raw_steps_projected_as_unknown_tool(self) -> None:
        cursor = _Cursor(one=(1, 1, 1, 1, 1))
        connection = _Connection(cursor)

        with (
            patch.dict(sys.modules, {"psycopg": fake_psycopg(connection)}),
            patch.object(
                pi_harness_scenario,
                "request",
                return_value=finish_trace(derived_kind="unknown_tool"),
            ),
        ):
            shape = trace_has_required_shape("run-1")

        self.assertFalse(shape["execution_steps"])

    def test_reports_missing_shape_when_counts_are_zero(self) -> None:
        cursor = _Cursor(one=(0, 0, 0, 0, 0))
        connection = _Connection(cursor)

        with (
            patch.dict(sys.modules, {"psycopg": fake_psycopg(connection)}),
            patch.object(
                pi_harness_scenario,
                "request",
                return_value={
                    "spans": [],
                    "operation_evidence": [],
                    "derived_nodes": [],
                },
            ),
        ):
            shape = trace_has_required_shape("run-1")

        self.assertFalse(any(shape.values()))


class HarnessAttemptTest(unittest.TestCase):
    def test_returns_the_first_attempt_for_a_run(self) -> None:
        cursor = _Cursor(all=[("hat_1", "run-1", 1, "provider-1", "active")])
        connection = _Connection(cursor)

        with patch.dict(sys.modules, {"psycopg": fake_psycopg(connection)}):
            attempt = harness_attempt_for_run("run-1")

        self.assertEqual(attempt["id"], "hat_1")
        self.assertEqual(attempt["attempt_number"], 1)
        self.assertEqual(attempt["provider_attempt_id"], "provider-1")

    def test_raises_when_no_harness_attempt_exists(self) -> None:
        cursor = _Cursor(all=[])
        connection = _Connection(cursor)

        with patch.dict(sys.modules, {"psycopg": fake_psycopg(connection)}):
            with self.assertRaisesRegex(AssertionError, "no harness attempt"):
                harness_attempt_for_run("run-1")


class FailedEvidenceTest(unittest.TestCase):
    def test_requires_failed_assert_text_with_stable_error_attempt_and_screenshot(self) -> None:
        cursor = _Cursor(
            one=(
                "failed",
                "condition_unmet",
                "assertion detail",
                "attempt-1",
                ["run-1/obs_failed.png"],
            )
        )
        connection = _Connection(cursor)

        with patch.dict(sys.modules, {"psycopg": fake_psycopg(connection)}):
            self.assertTrue(run_has_failed_assertion_evidence("run-1"))

    def test_rejects_missing_screenshot_path(self) -> None:
        cursor = _Cursor(
            one=("failed", "condition_unmet", "assertion detail", "attempt-1", [])
        )
        connection = _Connection(cursor)

        with patch.dict(sys.modules, {"psycopg": fake_psycopg(connection)}):
            self.assertFalse(run_has_failed_assertion_evidence("run-1"))


class ConcurrentIsolationTest(unittest.TestCase):
    def test_rejects_shared_harness_attempt_ids(self) -> None:
        cursor = _Cursor(all=[("run-1", 3)])
        connection = _Connection(cursor)
        attempts = iter(
            [
                {"id": "hat_shared", "provider_attempt_id": "provider-shared"},
                {"id": "hat_shared", "provider_attempt_id": "provider-shared"},
            ]
        )

        with (
            patch.dict(sys.modules, {"psycopg": fake_psycopg(connection)}),
            patch.object(
                pi_harness_scenario,
                "harness_attempt_for_run",
                side_effect=lambda run_id: next(attempts),
            ),
        ):
            with self.assertRaisesRegex(AssertionError, "share harness attempt ids"):
                assert_concurrent_isolation(["run-1", "run-2"])

    def test_accepts_distinct_attempts_and_run_scoped_operation_artifacts(self) -> None:
        cursor = _Cursor(
            all=[
                ("run-1", ["run-1/obs_1.png"]),
                ("run-2", ["run-2/obs_2.png", "run-2/obs_3.png"]),
            ]
        )
        connection = _Connection(cursor)
        attempts = {
            "run-1": {"id": "hat_1", "provider_attempt_id": "provider-1"},
            "run-2": {"id": "hat_2", "provider_attempt_id": "provider-2"},
        }

        with (
            patch.dict(sys.modules, {"psycopg": fake_psycopg(connection)}),
            patch.object(
                pi_harness_scenario,
                "harness_attempt_for_run",
                side_effect=lambda run_id: attempts[run_id],
            ),
        ):
            assert_concurrent_isolation(["run-1", "run-2"])

    def test_rejects_operation_artifact_outside_its_run_scope(self) -> None:
        cursor = _Cursor(all=[("run-1", ["run-2/obs_1.png"]), ("run-2", ["run-2/obs_2.png"])])
        connection = _Connection(cursor)
        attempts = {
            "run-1": {"id": "hat_1", "provider_attempt_id": "provider-1"},
            "run-2": {"id": "hat_2", "provider_attempt_id": "provider-2"},
        }

        with (
            patch.dict(sys.modules, {"psycopg": fake_psycopg(connection)}),
            patch.object(
                pi_harness_scenario,
                "harness_attempt_for_run",
                side_effect=lambda run_id: attempts[run_id],
            ),
        ):
            with self.assertRaisesRegex(AssertionError, "unscoped operation artifact"):
                assert_concurrent_isolation(["run-1", "run-2"])


class AgentServerProbeTest(unittest.TestCase):
    def test_passes_when_no_agentserver_port_is_listening(self) -> None:
        def urlopen(request, timeout=0):
            raise OSError("connection refused")

        with patch("pi_harness_scenario.urllib.request.urlopen", side_effect=urlopen):
            assert_no_agentserver_endpoint()

    def test_fails_when_a_probe_port_accepts_connections(self) -> None:
        class _Response:
            def __enter__(self):
                return self

            def __exit__(self, *exc):
                return False

        def urlopen(request, timeout=0):
            return _Response()

        with patch("pi_harness_scenario.urllib.request.urlopen", side_effect=urlopen):
            with self.assertRaisesRegex(AssertionError, "AgentServer-like endpoint"):
                assert_no_agentserver_endpoint()


class ComposeConfigTest(unittest.TestCase):
    def test_runtime_image_builds_the_harness_with_node(self) -> None:
        result = subprocess_run(
            ["docker", "compose", "--env-file", ".env.example", "config", "--format", "json"],
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        services = json.loads(result.stdout)["services"]
        runtime = services["runtime"]
        build = runtime.get("build", {})
        self.assertEqual(build.get("dockerfile"), "pi-browser-harness/Dockerfile")
        environment_vars = runtime.get("environment", {})
        self.assertIn("AGENTTRACE_PI_HARNESS_ENTRY", environment_vars)
        self.assertNotIn("AGENTTRACE_MODEL_SCRIPT", environment_vars)

        dockerfile = (
            pi_harness_scenario.ROOT / "pi-browser-harness" / "Dockerfile"
        ).read_text()
        self.assertIn(
            "/build/pi-browser-harness/node_modules",
            dockerfile,
            "runtime image must include the harness production dependencies",
        )

    def test_no_agentserver_service_in_compose(self) -> None:
        result = subprocess_run(["docker", "compose", "config", "--format", "json"])
        self.assertEqual(result.returncode, 0, result.stderr)
        services = json.loads(result.stdout)["services"]
        for name in services:
            self.assertNotIn(
                "agent",
                name.lower(),
                f"service {name!r} looks like an AgentServer; migration must not add one",
            )


def subprocess_run(command):
    import subprocess

    return subprocess.run(
        command,
        cwd=pi_harness_scenario.ROOT,
        text=True,
        capture_output=True,
        check=False,
    )


if __name__ == "__main__":
    unittest.main()
