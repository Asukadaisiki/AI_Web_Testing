"""Unit tests for the real-provider concurrent load scenario."""

from __future__ import annotations

import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from real_provider_load_scenario import ROOT, LoadConfig, percentile, validate_summary


class ComposeRealProviderConfigTest(unittest.TestCase):
    def test_example_environment_keeps_runtime_concurrent(self) -> None:
        environment = os.environ.copy()
        environment.pop("AGENTTRACE_RUNTIME_CONCURRENCY", None)
        result = subprocess.run(
            [
                "docker",
                "compose",
                "--env-file",
                ".env.example",
                "config",
                "--format",
                "json",
            ],
            cwd=ROOT,
            env=environment,
            text=True,
            capture_output=True,
            check=False,
        )

        self.assertEqual(result.returncode, 0, result.stderr)
        runtime = json.loads(result.stdout)["services"]["runtime"]["environment"]
        self.assertEqual(runtime["AGENTTRACE_RUNTIME_CONCURRENCY"], "4")

    def test_web_publishes_the_vite_listener_port(self) -> None:
        result = subprocess.run(
            ["docker", "compose", "config", "--format", "json"],
            cwd=ROOT,
            text=True,
            capture_output=True,
            check=False,
        )

        self.assertEqual(result.returncode, 0, result.stderr)
        ports = json.loads(result.stdout)["services"]["web"]["ports"]
        self.assertEqual(
            [(port["published"], port["target"]) for port in ports],
            [("5174", 5174)],
        )

    def test_pi_harness_entry_is_configured_for_runtime(self) -> None:
        result = subprocess.run(
            ["docker", "compose", "config", "--format", "json"],
            cwd=ROOT,
            text=True,
            capture_output=True,
            check=False,
        )

        self.assertEqual(result.returncode, 0, result.stderr)
        services = json.loads(result.stdout)["services"]
        runtime_env = services["runtime"]["environment"]
        self.assertIn("AGENTTRACE_PI_HARNESS_ENTRY", runtime_env)
        self.assertNotIn("AGENTTRACE_MODEL_SCRIPT", runtime_env)


class KafkaTopicInitTest(unittest.TestCase):
    def test_existing_topic_is_upgraded_to_the_configured_partition_count(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            temporary = Path(directory)
            state = temporary / "partitions"
            state.write_text("1\n", encoding="utf-8")
            fake_topics = temporary / "kafka-topics.sh"
            fake_topics.write_text(
                """#!/usr/bin/env bash
set -euo pipefail
case " $* " in
  *" --create "*) exit 0 ;;
  *" --describe "*)
    printf 'Topic: test-topic PartitionCount: %s ReplicationFactor: 1\\n' \
"$(tr -d '\\n' < "$FAKE_KAFKA_STATE")"
    ;;
  *" --alter "*)
    arguments=("$@")
    for ((index = 0; index < ${#arguments[@]}; index++)); do
      if [[ "${arguments[$index]}" == "--partitions" ]]; then
        printf '%s\\n' "${arguments[$((index + 1))]}" > "$FAKE_KAFKA_STATE"
        exit 0
      fi
    done
    exit 2
    ;;
  *) exit 2 ;;
esac
""",
                encoding="utf-8",
            )
            fake_topics.chmod(0o700)
            environment = os.environ.copy()
            environment.update(
                {
                    "AGENTTRACE_KAFKA_TOPICS": "test-topic",
                    "AGENTTRACE_KAFKA_PARTITIONS": "8",
                    "AGENTTRACE_KAFKA_TOPICS_BIN": str(fake_topics),
                    "AGENTTRACE_KAFKA_BOOTSTRAP_SERVER": "unused:9092",
                    "FAKE_KAFKA_STATE": str(state),
                }
            )

            result = subprocess.run(
                ["bash", str(ROOT / "integration/v3/kafka_init_topics.sh")],
                cwd=ROOT,
                env=environment,
                text=True,
                capture_output=True,
                check=False,
            )

            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertEqual(state.read_text(encoding="utf-8"), "8\n")


class LoadConfigTest(unittest.TestCase):
    def test_defaults_define_the_approved_forty_run_workload(self) -> None:
        with patch.dict(os.environ, {}, clear=True):
            config = LoadConfig.from_env()

        self.assertEqual(config.projects, 5)
        self.assertEqual(config.conversations_per_project, 4)
        self.assertEqual(config.runs_per_conversation, 2)
        self.assertEqual(config.submit_workers, 40)
        self.assertEqual(config.total_runs, 40)
        self.assertEqual(config.minimum_peak_concurrency, 8)

    def test_rejects_non_positive_dimensions(self) -> None:
        with patch.dict(os.environ, {"AGENTTRACE_LOAD_PROJECTS": "0"}, clear=True):
            with self.assertRaisesRegex(ValueError, "AGENTTRACE_LOAD_PROJECTS"):
                LoadConfig.from_env()


class SummaryTest(unittest.TestCase):
    def test_percentile_uses_nearest_rank(self) -> None:
        self.assertEqual(percentile([1.0, 2.0, 3.0, 4.0], 0.95), 4.0)
        self.assertEqual(percentile([4.0, 1.0, 2.0, 3.0], 0.50), 2.0)

    def test_validation_requires_all_full_chain_signals(self) -> None:
        summary = {
            "requested_runs": 40,
            "succeeded_runs": 40,
            "failed_runs": 0,
            "projects": 5,
            "conversations": 20,
            "operation_results": 40,
            "runs_with_screenshots": 40,
            "runs_with_spans": 40,
            "runs_with_usage": 40,
            "redis_streams": 40,
            "pending_outbox": 0,
            "dlq_events": 0,
            "unprocessed_commands": 0,
            "cross_project_runs": 0,
            "cross_conversation_messages": 0,
            "duplicate_message_ordinals": 0,
            "kafka_partitions": 4,
            "peak_concurrency": 8,
        }

        validate_summary(summary)

        summary["pending_outbox"] = 1
        with self.assertRaisesRegex(AssertionError, "pending_outbox=1"):
            validate_summary(summary)
        summary["pending_outbox"] = 0
        summary["duplicate_message_ordinals"] = 1
        with self.assertRaisesRegex(
            AssertionError, "duplicate_message_ordinals=1"
        ):
            validate_summary(summary)

    def test_single_run_smoke_accepts_one_observed_kafka_partition(self) -> None:
        summary = {
            "requested_runs": 1,
            "succeeded_runs": 1,
            "failed_runs": 0,
            "projects": 1,
            "conversations": 1,
            "expected_projects": 1,
            "expected_conversations": 1,
            "operation_results": 1,
            "runs_with_operations": 1,
            "runs_with_screenshots": 1,
            "runs_with_spans": 1,
            "runs_with_usage": 1,
            "redis_streams": 1,
            "pending_outbox": 0,
            "dlq_events": 0,
            "unprocessed_commands": 0,
            "cross_project_runs": 0,
            "cross_conversation_messages": 0,
            "duplicate_message_ordinals": 0,
            "kafka_partitions": 1,
            "peak_concurrency": 1,
        }

        validate_summary(summary)


if __name__ == "__main__":
    unittest.main()
