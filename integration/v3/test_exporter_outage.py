"""Live exporter outage resilience proof."""

from __future__ import annotations

import json
import os
import subprocess
import unittest

from failure_drills import EXPORTER_CONSUMER, ROOT, FailureDrills


class ExporterOutageTest(unittest.TestCase):
    def test_exporter_outage_leaves_local_run_complete_and_retries_export(self) -> None:
        FailureDrills().exporter_outage()

    def test_export_retry_requires_a_second_delivery_attempt(self) -> None:
        drills = FailureDrills()
        observed_attempt_counts = iter([(1,), (2,)])
        drills._row = lambda _query, _values: next(observed_attempt_counts)  # type: ignore[method-assign]

        self.assertFalse(drills._export_retry_visible("evt_retry"))
        self.assertTrue(drills._export_retry_visible("evt_retry"))

    def test_langfuse_profile_defaults_are_empty_without_operator_values(self) -> None:
        environment = os.environ.copy()
        for key in (
            "AGENTTRACE_LANGFUSE_BASE_URL",
            "AGENTTRACE_LANGFUSE_PUBLIC_KEY",
            "AGENTTRACE_LANGFUSE_SECRET_KEY",
        ):
            environment.pop(key, None)
        result = subprocess.run(
            ["docker", "compose", "--profile", "langfuse", "config", "--format", "json"],
            cwd=ROOT,
            env=environment,
            text=True,
            capture_output=True,
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        exporter = json.loads(result.stdout)["services"]["exporter"]["environment"]
        self.assertEqual(exporter["AGENTTRACE_LANGFUSE_BASE_URL"], "")
        self.assertEqual(exporter["AGENTTRACE_LANGFUSE_PUBLIC_KEY"], "")
        self.assertEqual(exporter["AGENTTRACE_LANGFUSE_SECRET_KEY"], "")
        self.assertEqual(EXPORTER_CONSUMER, exporter["AGENTTRACE_LANGFUSE_GROUP_ID"])

    def test_exporter_fixture_provisioning_injects_non_secret_values(self) -> None:
        class FixtureProvisioningDrills(FailureDrills):
            def __init__(self, *, exporter_consumer: str) -> None:
                super().__init__(exporter_consumer=exporter_consumer)
                self.environment: dict[str, str] | None = None
                self.commands: list[tuple[str, ...]] = []

            def _service_running(self, service: str) -> bool:
                if service != "exporter":
                    raise AssertionError(f"unexpected fixture service: {service}")
                return False

            def _compose(
                self,
                *args: str,
                input_text: str | None = None,
                environment: dict[str, str] | None = None,
            ) -> subprocess.CompletedProcess[str]:
                self.commands.append(args)
                if args == ("--profile", "langfuse", "up", "-d", "exporter"):
                    self.environment = environment
                return subprocess.CompletedProcess(args, 0)

            def _wait_service_running(self, service: str) -> None:
                if service != "exporter":
                    raise AssertionError(f"unexpected fixture service: {service}")

        drills = FixtureProvisioningDrills(
            exporter_consumer="agenttrace-langfuse-exporter-failure-test"
        )
        drills._ensure_exporter()

        self.assertEqual(
            drills.environment,
            {
                "AGENTTRACE_LANGFUSE_BASE_URL": "http://127.0.0.1:9",
                "AGENTTRACE_LANGFUSE_PUBLIC_KEY": "agenttrace-failure-drill-public",
                "AGENTTRACE_LANGFUSE_SECRET_KEY": "agenttrace-failure-drill-secret",
                "AGENTTRACE_LANGFUSE_GROUP_ID": (
                    "agenttrace-langfuse-exporter-failure-test"
                ),
            },
        )
        self.assertEqual(
            drills.commands,
            [
                (
                    "exec",
                    "-T",
                    "kafka",
                    "/opt/kafka/bin/kafka-consumer-groups.sh",
                    "--bootstrap-server",
                    "kafka:9092",
                    "--group",
                    "agenttrace-langfuse-exporter-failure-test",
                    "--topic",
                    "agenttrace.run.events.v1",
                    "--reset-offsets",
                    "--to-latest",
                    "--execute",
                ),
                ("--profile", "langfuse", "up", "-d", "exporter"),
            ],
        )

    def test_exporter_fixture_refuses_to_replace_a_running_operator_exporter(self) -> None:
        class RunningExporterDrills(FailureDrills):
            def _service_running(self, service: str) -> bool:
                self.assert_service = service
                return True

            def _compose(
                self,
                *args: str,
                input_text: str | None = None,
                environment: dict[str, str] | None = None,
            ) -> subprocess.CompletedProcess[str]:
                raise AssertionError(f"must not replace a running exporter: {args}")

        drills = RunningExporterDrills()

        with self.assertRaisesRegex(AssertionError, "already running"):
            drills._ensure_exporter()

        self.assertEqual(drills.assert_service, "exporter")

    def test_exporter_fixture_cleanup_removes_the_test_container(self) -> None:
        class CleanupDrills(FailureDrills):
            def __init__(self) -> None:
                super().__init__()
                self.commands: list[tuple[str, ...]] = []

            def _compose(
                self,
                *args: str,
                input_text: str | None = None,
                environment: dict[str, str] | None = None,
            ) -> subprocess.CompletedProcess[str]:
                self.commands.append(args)
                return subprocess.CompletedProcess(args, 0)

        drills = CleanupDrills()
        drills._remove_exporter_fixture()

        self.assertEqual(
            drills.commands,
            [("--profile", "langfuse", "rm", "-f", "-s", "exporter")],
        )

    def test_partial_exporter_provisioning_failure_still_removes_fixture(self) -> None:
        class PartialProvisioningDrills(FailureDrills):
            def __init__(self) -> None:
                super().__init__()
                self.commands: list[tuple[str, ...]] = []

            def _service_running(self, service: str) -> bool:
                if service != "exporter":
                    raise AssertionError(f"unexpected fixture service: {service}")
                return False

            def _compose(
                self,
                *args: str,
                input_text: str | None = None,
                environment: dict[str, str] | None = None,
            ) -> subprocess.CompletedProcess[str]:
                self.commands.append(args)
                if args == ("--profile", "langfuse", "up", "-d", "exporter"):
                    raise AssertionError("partial compose provisioning failure")
                return subprocess.CompletedProcess(args, 0)

        drills = PartialProvisioningDrills()

        with self.assertRaisesRegex(AssertionError, "partial compose"):
            drills.exporter_outage()

        self.assertEqual(
            drills.commands,
            [
                (
                    "exec",
                    "-T",
                    "kafka",
                    "/opt/kafka/bin/kafka-consumer-groups.sh",
                    "--bootstrap-server",
                    "kafka:9092",
                    "--group",
                    drills.exporter_consumer,
                    "--topic",
                    "agenttrace.run.events.v1",
                    "--reset-offsets",
                    "--to-latest",
                    "--execute",
                ),
                ("--profile", "langfuse", "up", "-d", "exporter"),
                ("--profile", "langfuse", "rm", "-f", "-s", "exporter"),
            ],
        )


if __name__ == "__main__":
    unittest.main()
