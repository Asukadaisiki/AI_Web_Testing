"""Regression checks for native startup isolation and connection handling."""

import importlib.util
from pathlib import Path
import socket
import sys
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("native_runner", Path(__file__).with_name("run.py"))
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)


class StartupTests(unittest.TestCase):
    def test_management_environment_has_stable_private_key_and_scoped_defaults(self):
        with tempfile.TemporaryDirectory() as directory:
            env = {"AGENTTRACE_MANAGEMENT_ENABLED": "true",
                   "AGENTTRACE_BRIDGE_TOKEN": "bridge-test",
                   "AGENTTRACE_BROWSER_PROVIDER_TOKEN": "worker-test",
                   "LOOP_WORKER_INTERNAL_TOKEN": "", "LOOP_ENVIRONMENTS_DIR": ""}
            runner.configure_management(env, Path(directory))
            first = env["LOOP_ENVIRONMENT_ENCRYPTION_KEY"]
            other = dict(env)
            other.pop("LOOP_ENVIRONMENT_ENCRYPTION_KEY")
            runner.configure_management(other, Path(directory))
            self.assertEqual(first, other["LOOP_ENVIRONMENT_ENCRYPTION_KEY"])
            self.assertEqual(env["LOOP_WORKER_INTERNAL_TOKEN"], "worker-test")
            self.assertEqual(env["AGENTTRACE_BROWSER_ALLOWED_DOMAINS"], "127.0.0.1,localhost")
            key = Path(directory) / "environment.key"
            self.assertEqual(key.stat().st_mode & 0o777, 0o600)
            self.assertEqual(env["LOOP_ENVIRONMENTS_DIR"], str(Path(directory) / "environments"))

    def test_management_fails_closed_without_bridge_or_with_mismatched_worker_token(self):
        with tempfile.TemporaryDirectory() as directory:
            for env in (
                {"AGENTTRACE_MANAGEMENT_ENABLED": "true"},
                {"AGENTTRACE_MANAGEMENT_ENABLED": "true", "AGENTTRACE_BRIDGE_TOKEN": "test",
                 "AGENTTRACE_BROWSER_PROVIDER_TOKEN": "one", "LOOP_WORKER_INTERNAL_TOKEN": "two"},
            ):
                with self.assertRaises(SystemExit):
                    runner.configure_management(env, Path(directory))

    def test_management_service_plan_uses_configured_ports_and_no_old_web(self):
        env = {"AGENTTRACE_MANAGEMENT_ENABLED": "true",
               "AGENTTRACE_HTTP_ADDR": "127.0.0.1:8181",
               "AGENTTRACE_WORKER_URL": "http://127.0.0.1:8111",
               "AGENTTRACE_LOCAL_FIXTURE_PORT": "8125"}
        services = runner.service_plan(env)
        self.assertNotIn("web", [s[0] for s in services])
        self.assertEqual(next(s[3] for s in services if s[0] == "api"),
                         "http://127.0.0.1:8181/api/v1/health")
        self.assertIn("8111", next(s[1] for s in services if s[0] == "worker"))
        self.assertIn("agenttrace.environment.commands.v1", runner.kafka_topics(env))

    def test_native_file_environment_overrides_defaults(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / ".env").write_text("AGENTTRACE_HTTP_ADDR=127.0.0.1:8181\n")
            with patch.object(runner, "ROOT", root), \
                    patch.dict(runner.os.environ, {"AGENTTRACE_LOCAL_STATE": directory}, clear=True):
                self.assertEqual(runner.environment()["AGENTTRACE_HTTP_ADDR"], "127.0.0.1:8181")

    def test_kafka_restart_preserves_existing_cluster_metadata(self):
        with tempfile.TemporaryDirectory() as directory:
            state = Path(directory)
            (state / "data").mkdir()
            metadata = state / "data/meta.properties"
            metadata.write_text("cluster.id=retained-cluster\n")
            with patch.object(runner.subprocess, "run") as command:
                runner.initialize_kafka(state, state / "server.properties", {})
                command.assert_not_called()
            self.assertEqual(metadata.read_text(), "cluster.id=retained-cluster\n")

    def test_percent_encoded_credentials_stay_in_environment(self):
        env = runner.postgres_environment(
            {}, "postgresql://test%40user:p%40ss%3Aword@127.0.0.1:5432/agenttrace_local?sslmode=require",
        )
        self.assertEqual(env["PGUSER"], "test@user")
        self.assertEqual(env["PGPASSWORD"], "p@ss:word")
        self.assertEqual(env["PGSSLMODE"], "require")
        self.assertEqual(env["PGDATABASE"], "postgres")

    def test_occupied_port_does_not_launch_or_stop_any_process(self):
        with socket.socket() as occupied:
            try:
                occupied.bind(("127.0.0.1", 8124))
            except OSError:
                # The real local stack may already own this listener.
                pass
            with patch.object(sys, "argv", ["run.py", "--skip-build"]), \
                    patch.object(runner, "environment", return_value={
                        "AGENTTRACE_MODEL_PROVIDER": "scripted",
                    }), \
                    patch.object(runner.subprocess, "Popen") as launch, \
                    patch.object(runner.os, "killpg") as stop:
                with self.assertRaisesRegex(RuntimeError, "8124.*occupied"):
                    runner.main()
                launch.assert_not_called()
                stop.assert_not_called()

    def test_missing_exporter_configuration_fails_before_launch(self):
        with patch.object(sys, "argv", ["run.py", "--with-exporter"]), \
                patch.object(runner, "environment", return_value={
                    "AGENTTRACE_MODEL_PROVIDER": "scripted",
                }), \
                patch.object(runner.subprocess, "Popen") as launch:
            with self.assertRaisesRegex(SystemExit, "Langfuse exporter environment"):
                runner.main()
            launch.assert_not_called()


if __name__ == "__main__":
    unittest.main()
