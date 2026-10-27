"""Focused unit tests for the v3 verification runner."""

from __future__ import annotations

import io
import sys
import unittest
from pathlib import Path
from unittest.mock import patch

import run_v3_tests


class RecordingExecutor:
    def __init__(self, failures: set[tuple[str, ...]] | None = None) -> None:
        self.failures = failures or set()
        self.calls: list[tuple[Path, tuple[str, ...], dict[str, str]]] = []

    def __call__(
        self, command: list[str], *, cwd: Path, env: dict[str, str]
    ) -> int:
        recorded = tuple(command)
        self.calls.append((cwd, recorded, env))
        return 1 if recorded in self.failures else 0


class V3RunnerTest(unittest.TestCase):
    def test_runs_all_layers_in_required_order_with_isolated_go_dsn(self) -> None:
        executor = RecordingExecutor()
        output = io.StringIO()
        runner = run_v3_tests.V3TestRunner(
            root=Path("/workspace"),
            postgres_dsn=(
                "postgres://agenttrace:agenttrace@127.0.0.1:5433/"
                "agenttrace_test_v3_runner_unit?sslmode=disable"
            ),
            executor=executor,
            output=output,
        )

        runner.run_layers()

        self.assertEqual(
            [command for _, command, _ in executor.calls],
            [
                (sys.executable, "-m", "unittest", "run_v3_tests_test.py"),
                (
                    "docker",
                    "compose",
                    "exec",
                    "-T",
                    "-e",
                    "AGENTTRACE_TEST_POSTGRES_DSN=postgres://agenttrace:agenttrace@postgres:5432/agenttrace_test_v3_runner_unit?sslmode=disable",
                    "-e",
                    "AGENTTRACE_TEST_KAFKA_BROKERS=kafka:9092",
                    "-e",
                    "AGENTTRACE_TEST_REDIS_ADDR=redis:6379",
                    "-e",
                    "AGENTTRACE_TEST_REDIS_DB=15",
                    "api",
                    "go",
                    "test",
                    "-p",
                    "1",
                    "./...",
                ),
                ("uv", "run", "python", "-m", "unittest", "discover", "-s", "tests", "-t", "."),
                (
                    "uv",
                    "run",
                    "python",
                    "-m",
                    "unittest",
                    "discover",
                    "-s",
                    "../integration/v3",
                    "-p",
                    "test_automation_exercise_scenario.py",
                ),
                (
                    "uv",
                    "run",
                    "python",
                    "-m",
                    "unittest",
                    "discover",
                    "-s",
                    "../integration/v3",
                    "-p",
                    "test_real_provider_load_scenario.py",
                ),
                (
                    "uv",
                    "run",
                    "python",
                    "-m",
                    "unittest",
                    "discover",
                    "-s",
                    "../integration/v3",
                    "-p",
                    "test_pi_harness_scenario.py",
                ),
                ("npm", "test", "--", "--run"),
                ("npm", "run", "build"),
                (
                    "uv",
                    "run",
                    "--with",
                    "psycopg[binary]>=3.2,<4",
                    "python",
                    "integration/v3/canonical_scenario.py",
                ),
                (
                    "uv",
                    "run",
                    "--with",
                    "psycopg[binary]>=3.2,<4",
                    "python",
                    "integration/v3/pi_harness_scenario.py",
                ),
                (
                    "uv",
                    "run",
                    "--with",
                    "psycopg[binary]>=3.2,<4",
                    "python",
                    "integration/v3/concurrency_rerun_scenario.py",
                ),
                (
                    "uv",
                    "run",
                    "--with",
                    "psycopg[binary]>=3.2,<4",
                    "python",
                    "integration/v3/concurrent_delete_scenario.py",
                ),
                (
                    "uv",
                    "run",
                    "--with",
                    "psycopg[binary]>=3.2,<4",
                    "--python",
                    sys.executable,
                    "python",
                    "integration/v3/failure_drills.py",
                ),
                ("npm", "run", "test:e2e"),
            ],
        )
        go_env = executor.calls[1][2]
        self.assertEqual(
            go_env["AGENTTRACE_TEST_POSTGRES_DSN"],
            "postgres://agenttrace:agenttrace@postgres:5432/"
            "agenttrace_test_v3_runner_unit?sslmode=disable",
        )
        self.assertEqual(go_env["AGENTTRACE_TEST_KAFKA_BROKERS"], "kafka:9092")
        self.assertEqual(go_env["AGENTTRACE_TEST_REDIS_ADDR"], "redis:6379")
        self.assertEqual(go_env["AGENTTRACE_TEST_REDIS_DB"], "15")
        self.assertEqual(executor.calls[6][2]["npm_config_cache"], "/tmp/agenttrace-npm-cache")

    def test_stops_at_first_failure_and_prints_the_exact_command(self) -> None:
        executor = RecordingExecutor({("npm", "test", "--", "--run")})
        output = io.StringIO()
        runner = run_v3_tests.V3TestRunner(
            root=Path("/workspace"),
            postgres_dsn=(
                "postgres://agenttrace:agenttrace@127.0.0.1:5433/"
                "agenttrace_test_v3_runner_failure?sslmode=disable"
            ),
            executor=executor,
            output=output,
        )

        with self.assertRaisesRegex(run_v3_tests.CommandFailed, "npm test -- --run"):
            runner.run_layers()

        self.assertEqual(len(executor.calls), 7)
        self.assertIn(
            "FAILED COMMAND: (cd /workspace/web && npm test -- --run)",
            output.getvalue(),
        )

    def test_main_drops_the_isolated_database_after_a_layer_failure(self) -> None:
        events: list[str] = []

        class FailingRunner:
            def run_layers(self) -> None:
                events.append("layers")
                raise run_v3_tests.CommandFailed("go test -p 1 ./...")

        with (
            patch.object(run_v3_tests, "create_isolated_database", return_value="test-dsn") as create,
            patch.object(run_v3_tests, "drop_isolated_database") as drop,
            patch.object(run_v3_tests, "V3TestRunner", return_value=FailingRunner()),
        ):
            self.assertEqual(run_v3_tests.main(), 1)

        create.assert_called_once()
        drop.assert_called_once_with("test-dsn")
        self.assertEqual(events, ["layers"])


if __name__ == "__main__":
    unittest.main()
