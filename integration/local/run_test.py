"""Regression checks for native startup isolation and connection handling."""

import importlib.util
import json
from pathlib import Path
import socket
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).parent))
spec = importlib.util.spec_from_file_location("native_runner", Path(__file__).with_name("run.py"))
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)


class StartupTests(unittest.TestCase):
    def private_env(self):
        return {"AGENTTRACE_BRIDGE_TOKEN": "bridge-test",
                "AGENTTRACE_BROWSER_PROVIDER_TOKEN": "worker-test",
                "AGENTTRACE_LANGFUSE_PROJECT_ID": "lf-test",
                "AGENTTRACE_MODEL_PROVIDER": "scripted"}

    def test_deprecated_flag_never_selects_old_services_or_skips_binding(self):
        for flag in (None, "", "false", "true", "FaLsE", "TrUe"):
            with self.subTest(flag=flag), tempfile.TemporaryDirectory() as directory:
                env = self.private_env()
                if flag is not None:
                    env["AGENTTRACE_MANAGEMENT_ENABLED"] = flag
                runner.configure_management(env, Path(directory))
                self.assertEqual(env["LOOP_WORKER_INTERNAL_TOKEN"], "worker-test")
                self.assertEqual([s[0] for s in runner.service_plan(env)],
                                 ["fixture", "worker", "api", "runtime"])
                self.assertEqual(runner.kafka_topics(env), (
                    "agenttrace.run.commands.v1", "agenttrace.run.events.v1",
                    "agenttrace.run.dlq.v1", "agenttrace.environment.commands.v1"))

    def test_required_private_binding_fails_before_state_creation(self):
        for key in ("AGENTTRACE_BRIDGE_TOKEN", "AGENTTRACE_BROWSER_PROVIDER_TOKEN",
                    "AGENTTRACE_LANGFUSE_PROJECT_ID"):
            for value in ("", "   "):
                with self.subTest(key=key, value=value), tempfile.TemporaryDirectory() as directory:
                    root = Path(directory)
                    env = {**self.private_env(), key: value,
                           "AGENTTRACE_LOCAL_STATE": str(root / "missing")}
                    with patch.object(runner, "ROOT", root), patch.dict(runner.os.environ, env, clear=True):
                        with self.assertRaises(SystemExit):
                            runner.environment()
                    self.assertFalse((root / "missing").exists())

    def test_api_origin_is_derived_and_conflicting_origin_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            env = {**self.private_env(), "AGENTTRACE_HTTP_ADDR": "127.0.0.1:8181",
                   "AGENTTRACE_LOCAL_STATE": directory}
            with patch.object(runner, "ROOT", Path(directory)), patch.dict(runner.os.environ, env, clear=True):
                self.assertEqual(runner.environment()["AGENTTRACE_API_URL"], "http://127.0.0.1:8181")
                runner.os.environ["AGENTTRACE_API_URL"] = "http://127.0.0.1:8180"
                with self.assertRaises(SystemExit):
                    runner.environment()

    def test_retained_profiles_never_receive_a_replacement_key(self):
        with tempfile.TemporaryDirectory() as directory:
            state = Path(directory)
            profiles = state / "environments"
            profiles.mkdir()
            (profiles / "retained.enc").write_bytes(b"retained ciphertext")
            with self.assertRaisesRegex(SystemExit, "Retained profiles"):
                runner.configure_management(self.private_env(), state)
            self.assertFalse((state / "environment.key").exists())
            self.assertEqual((profiles / "retained.enc").read_bytes(), b"retained ciphertext")

    def test_explicit_key_and_allowlist_remain_unchanged(self):
        with tempfile.TemporaryDirectory() as directory:
            key = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
            env = {**self.private_env(), "LOOP_ENVIRONMENT_ENCRYPTION_KEY": key,
                   "AGENTTRACE_BROWSER_ALLOWED_DOMAINS": "",
                   "LOOP_ENVIRONMENTS_DIR": str(Path(directory) / "profiles")}
            runner.configure_management(env, Path(directory))
            self.assertEqual(env["LOOP_ENVIRONMENT_ENCRYPTION_KEY"], key)
            self.assertEqual(env["AGENTTRACE_BROWSER_ALLOWED_DOMAINS"], "")
            self.assertFalse((Path(directory) / "environment.key").exists())

    def test_invalid_or_public_key_fails_without_replacing_it(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "environment.key"
            path.write_text("not-a-key\n")
            path.chmod(0o644)
            with self.assertRaisesRegex(SystemExit, "private"):
                runner.configure_management(self.private_env(), Path(directory))
            path.chmod(0o600)
            with self.assertRaisesRegex(SystemExit, "valid Fernet"):
                runner.configure_management(self.private_env(), Path(directory))
            self.assertEqual(path.read_text(), "not-a-key\n")

    def test_kafka_existing_undersized_topic_fails_without_alter(self):
        commands = []
        def execute(command, **kwargs):
            commands.append(command)
            return runner.subprocess.CompletedProcess(command, 0,
                "Topic: topic\tPartitionCount: 4\tReplicationFactor: 1\n")
        with patch.object(runner.subprocess, "run", side_effect=execute):
            with self.assertRaisesRegex(RuntimeError, "eight partitions"):
                runner.configure_kafka_topic("topic", {})
        self.assertFalse(any("--alter" in command for command in commands))

    def test_kafka_eight_partitions_and_preserved_metadata(self):
        commands = []
        def execute(command, **kwargs):
            commands.append(command)
            return runner.subprocess.CompletedProcess(command, 0,
                "Topic: topic\tPartitionCount: 8\tReplicationFactor: 1\n")
        with patch.object(runner.subprocess, "run", side_effect=execute):
            runner.configure_kafka_topic("topic", {"AGENTTRACE_KAFKA_BROKERS": "broker:9092"})
        self.assertEqual(len(commands), 2)
        self.assertIn("--create", commands[0])
        self.assertIn("--if-not-exists", commands[0])
        self.assertIn("--describe", commands[1])

    def test_seed_precedes_application_children_and_hands_off_canonical_ids(self):
        with tempfile.TemporaryDirectory() as directory:
            env = {**self.private_env(), "AGENTTRACE_POSTGRES_DSN": "postgresql:///agenttrace_test_unused",
                   "AGENTTRACE_KAFKA_BROKERS": "external:9092", "AGENTTRACE_LOCAL_STATE": directory,
                   "AGENTTRACE_EXPORT_PROJECT_ID": "prj_stale"}
            sequence = []
            def execute(command, **kwargs):
                sequence.append("seed")
                return runner.subprocess.CompletedProcess(command, 0, json.dumps({
                    "project_id": "prj_canonical", "conversation_id": "conv_canonical"}))
            def launch(command, **kwargs):
                sequence.append("child")
                self.assertEqual(kwargs["env"]["AGENTTRACE_PROJECT_ID"], "prj_canonical")
                self.assertEqual(kwargs["env"]["AGENTTRACE_CONVERSATION_ID"], "conv_canonical")
                self.assertEqual(kwargs["env"]["AGENTTRACE_EXPORT_PROJECT_ID"], "prj_canonical")
                raise KeyboardInterrupt
            with patch.object(sys, "argv", ["run.py", "--skip-build"]), \
                    patch.object(runner, "environment", return_value=env), \
                    patch.object(runner.socket, "socket"), patch.object(runner.socket, "create_connection"), \
                    patch.object(runner, "configure_kafka_topic"), \
                    patch.object(runner.signal, "signal"), \
                    patch.object(runner.subprocess, "run", side_effect=execute), \
                    patch.object(runner.subprocess, "Popen", side_effect=launch):
                runner.main()
            self.assertEqual(sequence, ["seed", "child"])

    def test_management_environment_has_stable_private_key_and_scoped_defaults(self):
        with tempfile.TemporaryDirectory() as directory:
            env = {"AGENTTRACE_MANAGEMENT_ENABLED": "true",
                   "AGENTTRACE_BRIDGE_TOKEN": "bridge-test",
                   "AGENTTRACE_BROWSER_PROVIDER_TOKEN": "worker-test",
                   "AGENTTRACE_LANGFUSE_PROJECT_ID": "lf-test",
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
                 "AGENTTRACE_LANGFUSE_PROJECT_ID": "lf-test",
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
            (root / ".env").write_text("export AGENTTRACE_HTTP_ADDR='127.0.0.1:8181' # local\n")
            with patch.object(runner, "ROOT", root), \
                    patch.dict(runner.os.environ, {**self.private_env(), "AGENTTRACE_LOCAL_STATE": directory}, clear=True):
                self.assertEqual(runner.environment()["AGENTTRACE_HTTP_ADDR"], "127.0.0.1:8181")

    def test_native_environment_preserves_nonempty_json_headers(self):
        headers = '{"X-Provider-Route": "fixture # route", "X-Equals": "one=two"}'
        values = (headers, "'" + headers + "'", '"' + headers.replace('"', '\\"') + '"')
        for value in values:
            with self.subTest(value=value), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                (root / ".env").write_text(
                    f"export AGENTTRACE_CODEX_HTTP_HEADERS={value} # local\n"
                    "AGENTTRACE_MODEL_PROVIDER=openai-compatible\n"
                    "AGENTTRACE_CODEX_API_KEY=synthetic-sdk-key\n"
                )
                process = {**self.private_env(), "AGENTTRACE_LOCAL_STATE": directory,
                           "LOOP_ENVIRONMENT_ENCRYPTION_KEY": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}
                process.pop("AGENTTRACE_MODEL_PROVIDER")
                with patch.object(runner, "ROOT", root), patch.dict(runner.os.environ, process, clear=True):
                    env = runner.environment()
                self.assertEqual(env["AGENTTRACE_CODEX_HTTP_HEADERS"], headers)
                self.assertEqual(json.loads(env["AGENTTRACE_CODEX_HTTP_HEADERS"]),
                                 {"X-Provider-Route": "fixture # route", "X-Equals": "one=two"})
                self.assertEqual(env["AGENTTRACE_MODEL_PROVIDER"], "openai-compatible")
                self.assertEqual(env["AGENTTRACE_CODEX_API_KEY"], "synthetic-sdk-key")
                self.assertFalse((root / "environment.key").exists())

    def test_native_json_headers_process_environment_wins(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / ".env").write_text('AGENTTRACE_CODEX_HTTP_HEADERS={"X-Provider-Route":"file"}\n')
            for headers in ('{"X-Provider-Route":"process"}', ""):
                with self.subTest(headers=headers), patch.object(runner, "ROOT", root), \
                        patch.dict(runner.os.environ, {
                            **self.private_env(), "AGENTTRACE_LOCAL_STATE": directory,
                            "LOOP_ENVIRONMENT_ENCRYPTION_KEY": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
                            "AGENTTRACE_CODEX_HTTP_HEADERS": headers,
                        }, clear=True):
                    self.assertEqual(runner.environment()["AGENTTRACE_CODEX_HTTP_HEADERS"], headers)

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
                        **self.private_env(),
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
                    **self.private_env(),
                    "AGENTTRACE_MODEL_PROVIDER": "scripted",
                }), \
                patch.object(runner.subprocess, "Popen") as launch:
            with self.assertRaisesRegex(SystemExit, "Langfuse exporter environment"):
                runner.main()
            launch.assert_not_called()


if __name__ == "__main__":
    unittest.main()
