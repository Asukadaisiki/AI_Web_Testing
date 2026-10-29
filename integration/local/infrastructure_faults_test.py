"""Safety and evidence contracts for the isolated infrastructure launcher."""

import os
from pathlib import Path
import subprocess
import sys
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parent))
import infrastructure_faults as faults


class InfrastructureLauncherTests(unittest.TestCase):
    def test_deadline_is_strictly_under_ten_minutes(self):
        self.assertEqual(faults.bounded_timeout(180), 180)
        for value in (0, -1, 600, 601, float("inf"), float("nan")):
            with self.subTest(value=value), self.assertRaises(ValueError):
                faults.bounded_timeout(value)

    def test_cleanup_refuses_process_it_did_not_start(self):
        owned = faults.OwnedProcesses()
        outsider = subprocess.Popen([sys.executable, "-c", "import time; time.sleep(30)"])
        try:
            with self.assertRaises(ValueError):
                owned.stop(outsider)
            self.assertIsNone(outsider.poll())
            child = owned.start([sys.executable, "-c", "import time; time.sleep(30)"])
            self.assertNotEqual(os.getpgid(child.pid), os.getpgrp())
            owned.stop(child)
            self.assertIsNotNone(child.poll())
        finally:
            owned.close()
            outsider.terminate()
            outsider.wait(timeout=5)

    def test_skipped_tests_cannot_be_reported_as_verified(self):
        events = [{"Action": "skip", "Test": name} for name in faults.TESTS]
        with self.assertRaises(ValueError):
            faults.collect_evidence(events)

    def test_pass_without_measured_fault_evidence_is_rejected(self):
        events = [{"Action": "pass", "Test": name} for name in faults.TESTS]
        with self.assertRaises(ValueError):
            faults.collect_evidence(events)

    def test_verified_requires_all_three_measured_real_faults(self):
        events = []
        for name in faults.TESTS:
            import json
            events.extend([
                {"Action": "output", "Test": name, "Output": faults.MARKER + json.dumps(
                    {"fault_real": True, "metrics": {"elapsed_ms": 1}})},
                {"Action": "pass", "Test": name},
            ])
        self.assertEqual(set(faults.collect_evidence(events)), set(faults.TESTS))
        events[0]["Output"] = faults.MARKER + '{"fault_real":false,"metrics":{"elapsed_ms":1}}'
        with self.assertRaises(ValueError):
            faults.collect_evidence(events)

    def test_go_json_fragmented_output_preserves_measured_evidence(self):
        import json
        events = []
        for name in faults.TESTS:
            evidence = faults.MARKER + json.dumps({
                "fault_real": True, "metrics": {"value": 1, "detail": "x" * 2048}
            }) + "\n"
            for start in range(0, len(evidence), 512):
                events.append({"Action": "output", "Test": name,
                               "Output": evidence[start:start + 512]})
            events.append({"Action": "pass", "Test": name})
        try:
            measured = faults.collect_evidence(events)
        except ValueError:
            self.fail("Go JSON chunk boundaries must not truncate real fault evidence")
        self.assertEqual(measured[faults.TESTS[0]]["metrics"]["detail"], "x" * 2048)


if __name__ == "__main__":
    unittest.main()
