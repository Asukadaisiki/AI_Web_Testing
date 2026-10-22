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
            def __init__(self) -> None:
                super().__init__()
                self.environment: dict[str, str] | None = None

            def _compose(
                self,
                *args: str,
                input_text: str | None = None,
                environment: dict[str, str] | None = None,
            ) -> subprocess.CompletedProcess[str]:
                if args != ("--profile", "langfuse", "up", "-d", "exporter"):
                    raise AssertionError(f"unexpected fixture command: {args}")
                self.environment = environment
                return subprocess.CompletedProcess(args, 0)

            def _wait_service_running(self, service: str) -> None:
                if service != "exporter":
                    raise AssertionError(f"unexpected fixture service: {service}")

        drills = FixtureProvisioningDrills()
        drills._ensure_exporter()

        self.assertEqual(
            drills.environment,
            {
                "AGENTTRACE_LANGFUSE_BASE_URL": "http://127.0.0.1:9",
                "AGENTTRACE_LANGFUSE_PUBLIC_KEY": "agenttrace-failure-drill-public",
                "AGENTTRACE_LANGFUSE_SECRET_KEY": "agenttrace-failure-drill-secret",
            },
        )


if __name__ == "__main__":
    unittest.main()
