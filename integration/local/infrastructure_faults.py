#!/usr/bin/env python3
"""Bounded, isolated real Redis/Kafka/PostgreSQL infrastructure probes."""

import argparse
from datetime import datetime, timezone
import hashlib
import json
import math
import os
from pathlib import Path
import shutil
import signal
import socket
import subprocess
import tempfile
import threading
import time
from urllib.parse import urlsplit

from support import load_environment, pg_environment

ROOT = Path(__file__).resolve().parents[2]
DATABASE = "agenttrace_test_infrastructurefault_20261028"
SHARED_PORTS = (6379, 5432, 9192, 8180, 8110, 3300)

TESTS = (
    "TestRedisOutageSSEReplayAndBounds",
    "TestKafkaPauseRetainsOutboxAndRecovers",
    "TestPostgresSessionFailureRollsBackAndDeduplicates",
)
MARKER = "INFRASTRUCTURE_EVIDENCE "


def bounded_timeout(value):
    if not math.isfinite(value) or not 0 < value < 600:
        raise ValueError("timeout must be finite, positive and below ten minutes")
    return value


class OwnedProcesses:
    def __init__(self):
        self.children = []

    def start(self, command, **kwargs):
        process = subprocess.Popen(command, start_new_session=True, **kwargs)
        self.children.append(process)
        return process

    def stop(self, process):
        if not any(process is child for child in self.children):
            raise ValueError("refusing to signal an unowned process")
        if process.pid == os.getpid() or process.pid == os.getpgrp():
            raise ValueError("refusing to signal launcher process group")
        # A completed Go parent can leave children behind after a test timeout.
        # All children inherit the exclusively created launcher process group.
        for sig in (signal.SIGCONT, signal.SIGTERM):
            try:
                os.killpg(process.pid, sig)
            except ProcessLookupError:
                pass
        try:
            process.wait(timeout=3)
        except subprocess.TimeoutExpired:
            pass
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        process.wait(timeout=3)
        self.children.remove(process)

    def close(self):
        for process in list(self.children):
            self.stop(process)


def collect_evidence(events):
    passed, skipped, measured = set(), set(), {}
    output_by_test = {}
    for row in events:
        test = row.get("Test")
        if row.get("Action") == "pass":
            passed.add(test)
        if row.get("Action") in ("skip", "fail"):
            skipped.add(test)
        output_by_test[test] = output_by_test.get(test, "") + row.get("Output", "")
    for test, output in output_by_test.items():
        for line in output.splitlines():
            if MARKER in line:
                measured[test] = json.loads(line.split(MARKER, 1)[1])
    if not set(TESTS) <= passed or set(TESTS) & skipped:
        raise ValueError("all three real infrastructure tests must pass without skips")
    for test in TESTS:
        item = measured.get(test, {})
        if item.get("fault_real") is not True or not item.get("metrics"):
            raise ValueError("passing tests require measured real fault evidence")
    return {test: measured[test] for test in TESTS}


def source_hashes():
    paths = list((ROOT / "backend/internal").rglob("*.go"))
    paths += list((ROOT / "backend/internal/postgres/migrations").glob("*.sql"))
    paths += [ROOT / "backend/go.mod", ROOT / "backend/go.sum",
              Path(__file__), Path(__file__).with_name("infrastructure_faults_test.py"),
              Path(__file__).with_name("support.py")]
    return {str(path.relative_to(ROOT)): hashlib.sha256(path.read_bytes()).hexdigest()
            for path in sorted(paths) if not path.name.endswith("_test.go") or
            "infrastructurefaults" in path.parts}


def shared_listeners(env):
    result = {}
    for port in SHARED_PORTS:
        proc = subprocess.run(
            ["lsof", "-nP", f"-iTCP:{port}", "-sTCP:LISTEN", "-Fp"],
            env=env, capture_output=True, text=True, timeout=3,
        )
        result[str(port)] = sorted({int(line[1:]) for line in proc.stdout.splitlines()
                                   if line.startswith("p") and line[1:].isdigit()})
    return result


def sql(query, env):
    result = subprocess.run(
        ["psql", "--no-psqlrc", "-Atq", "-v", "ON_ERROR_STOP=1", "-c", query],
        env=env, capture_output=True, text=True, timeout=10,
    )
    if result.returncode:
        raise RuntimeError("isolated database operation failed; credentials/output withheld")
    return result.stdout.strip()


def resource_checks(probes, env):
    checks = {}
    for test, item in probes.items():
        topology = item["topology"]
        for key in ("redis", "kafka", "http"):
            if key not in topology:
                continue
            endpoint = urlsplit(topology[key] if key == "http" else "tcp://" + topology[key])
            try:
                with socket.create_connection((endpoint.hostname, endpoint.port), timeout=.2):
                    checks[test + "_" + key + "_port_closed"] = False
            except OSError:
                checks[test + "_" + key + "_port_closed"] = True
        for key in ("redis_owned_pid", "kafka_owned_pid"):
            if key in topology:
                result = subprocess.run(["ps", "-o", "stat=", "-p", str(topology[key])],
                                        env=env, capture_output=True, text=True, timeout=3)
                checks[test + "_" + key + "_exited"] = not result.stdout.strip() or "Z" in result.stdout
    return checks


def go_run(owned, env, deadline, test=None, overlay=None):
    remaining = deadline - time.monotonic()
    if remaining < 15:
        raise RuntimeError("infrastructure probe deadline exhausted")
    timeout = min(180, remaining - 5)
    command = ["go", "test", "-json", "-count=1", f"-timeout={int(timeout - 5)}s"]
    if overlay:
        command.append("-overlay=" + str(overlay))
    if test:
        command += ["-run", "^" + test + "$"]
    command.append("./internal/infrastructurefaults")
    before = source_hashes()
    started = time.monotonic()
    with tempfile.TemporaryFile(mode="w+") as output:
        process = owned.start(command, cwd=ROOT / "backend", env=env,
                              stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
        def drain():
            for line in process.stdout:
                output.write(line)
                try:
                    row = json.loads(line)
                except json.JSONDecodeError:
                    continue
                if row.get("Action") == "pass" and row.get("Test") in TESTS:
                    print("real probe passed: " + row["Test"], flush=True)
        reader = threading.Thread(target=drain, daemon=True)
        reader.start()
        try:
            process.wait(timeout=timeout)
        except subprocess.TimeoutExpired:
            raise RuntimeError("bounded Go infrastructure test timed out") from None
        finally:
            owned.stop(process)
            reader.join(timeout=5)
            process.stdout.close()
        if reader.is_alive():
            raise RuntimeError("owned Go output drain did not stop")
        output.seek(0)
        raw = output.read()
    events = []
    for line in raw.splitlines():
        try:
            events.append(json.loads(line))
        except json.JSONDecodeError:
            continue
    after = source_hashes()
    return {
        "returncode": process.returncode, "duration_seconds": round(time.monotonic() - started, 3),
        "events": events, "source_sha256_before": before,
        "source_changed_during_run": [name for name, value in before.items() if after.get(name) != value],
        "non_json_output_present": any(not line.startswith("{") for line in raw.splitlines()),
    }


def negative_controls(owned, env, deadline):
    mutations = [
        ("redis_fallback_disabled", TESTS[0], "stream/service.go",
         """\t\t\tif readErr != nil {
\t\t\t\tif ctx.Err() != nil {
\t\t\t\t\treturn
\t\t\t\t}
""",
         """\t\t\tif readErr != nil {
\t\t\t\tif ctx.Err() != nil {
\t\t\t\t\treturn
\t\t\t\t}
\t\t\t\treturn
"""),
        ("outbox_ack_without_broker", TESTS[1], "outbox/relay.go",
         "\tfor _, record := range records {\n",
         "\tfor _, record := range records {\n\t\t_, _ = r.recordPublished(context.Background(), record.id, record.claimToken, eventbus.Metadata{})\n"),
        ("inbox_duplicate_guard_disabled", TESTS[2], "consumer/deduplicator.go",
         "\tif tag.RowsAffected() == 0 {", "\tif false && tag.RowsAffected() == 0 {"),
        ("inbox_handler_failure_ignored", TESTS[2], "consumer/deduplicator.go",
         '\t\treturn fmt.Errorf("handle inbox event %q: %w", envelope.EventID, err)',
         '\t\t_ = err'),
    ]
    controls = []
    for name, test, relative, original, replacement in mutations:
        path = ROOT / "backend/internal" / relative
        content = path.read_text()
        if content.count(original) != 1:
            raise RuntimeError("negative-control source anchor changed; refusing ambiguous overlay")
        with tempfile.TemporaryDirectory(prefix="agenttrace-owned-infra-overlay-") as directory:
            changed = Path(directory) / path.name
            changed.write_text(content.replace(original, replacement, 1))
            overlay = Path(directory) / "overlay.json"
            overlay.write_text(json.dumps({"Replace": {str(path): str(changed)}}))
            result = go_run(owned, env, deadline, test, overlay)
            failures = [row.get("Output", "").strip() for row in result["events"]
                        if row.get("Test") == test and
                        "infrastructure_integration_test.go:" in row.get("Output", "")]
            failed = any(row.get("Action") == "fail" and row.get("Test") == test
                         for row in result["events"])
            if not result["returncode"] or not failed or not failures:
                raise RuntimeError("negative control was not caught by a real fault assertion")
            controls.append({
                "name": name, "test": test, "production_path": str(path.relative_to(ROOT)),
                "original_source_sha256": hashlib.sha256(content.encode()).hexdigest(),
                "mutation_sha256": hashlib.sha256(changed.read_bytes()).hexdigest(),
                "expected": "target real test fails at behavioral assertion",
                "observed": failures, "caught": True,
                "duration_seconds": result["duration_seconds"],
                "source_changed_during_run": result["source_changed_during_run"],
            })
            print(f"negative control caught: {name}", flush=True)
    return controls


def write_report(report, path):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(report, indent=2, sort_keys=True) + "\n")
    lines = [
        "# Management Infrastructure Faults, 2026-10-28", "",
        f"Status: **{report['status']}**. Duration: {report.get('duration_seconds', 0)} seconds.",
        "", "## Scope", "",
        "Isolated Phase 3 component integration. Real Redis, Kafka and PostgreSQL;",
        "real production HTTP SSE, stream, outbox relay, Kafka consumer and inbox deduplicator.",
        "Synthetic SQL/event fixtures only: no SDK, LLM, browser, UI or full management-runtime workload.",
        "Does not claim completion of phases 0-4 or the parent's separate acceptance work.", "",
        "## Results", "",
    ]
    for test, item in report.get("probes", {}).items():
        lines += [f"### {test}", "", "Expected: " + item["expected"] + ".",
                  "Observed: " + item["observed"] + ".", "",
                  "```json", json.dumps(item["metrics"], indent=2), "```", ""]
    lines += ["## Negative Controls", "",
              "Temporary Go overlays only; production workspace files were not modified.", ""]
    for control in report.get("negative_controls", []):
        lines += [f"- `{control['name']}`: caught; " + "; ".join(control["observed"])]
    lines += ["", "## Isolation And Limits", ""]
    lines += ["- " + limit for limit in report["limits"]]
    lines += ["", "JSON companion records per-file SHA-256, measured ports/PIDs, versions,",
              "timestamps, baseline test actions, source changes and cleanup checks.", ""]
    path.with_suffix(".md").write_text("\n".join(lines))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--timeout", type=float, default=540)
    parser.add_argument("--admin-dsn", default=None,
                        help="administrative PG connection; prefer environment AGENTTRACE_INFRAFAULT_ADMIN_DSN")
    parser.add_argument("--report", type=Path,
                        default=ROOT / "docs/management-infrastructure-faults-2026-10-28.json")
    args = parser.parse_args()
    bounded_timeout(args.timeout)
    if args.timeout > 540:
        parser.error("maximum workload deadline is 540 seconds, reserving cleanup below ten minutes")
    env = load_environment([ROOT / ".env"])
    for binary in ("go", "redis-server", "kafka-server-start", "kafka-storage", "psql", "lsof"):
        if not shutil.which(binary, path=env.get("PATH")):
            parser.error("required native command unavailable: " + binary)
    admin = pg_environment(args.admin_dsn or env.get("AGENTTRACE_INFRAFAULT_ADMIN_DSN", "dbname=postgres"), env)
    admin["PGAPPNAME"] = "agenttrace_owned_infrastructure_fault_admin"
    started = time.monotonic()
    deadline = started + args.timeout
    owned = OwnedProcesses()
    created = False
    report = {
        "status": "failed", "started_at": datetime.now(timezone.utc).isoformat(),
        "workload": "native component integration; synthetic canonical fixtures, no fake SDK",
        "database": DATABASE, "deadline_seconds": args.timeout,
        "limits": [
            "No shared Redis6379, PG5432, Kafka9192, API8180, Worker8110 or Langfuse3300 process is signalled.",
            "Private Redis/Kafka listen on dynamically selected loopback ports with temporary state.",
            "PostgreSQL fault terminates only the exact transaction backend in the exclusively created test database.",
            "PG server-wide outage, Kafka cluster partition/rebalance, SDK/runtime/LLM/UI and scale-load behavior are not claimed.",
            "Redis timings use test client timeouts and 100ms polling, not deployment defaults.",
            "Capacity guard is verified by counters and stream closure; API may return EOF instead of an explicit SSE error frame due to closed-channel select ordering.",
            "Kafka is one KRaft broker/controller, replication factor one; SIGSTOP/SIGCONT only its own PID.",
            "Franz-go idempotent in-flight publish is not strictly bounded by its caller context; the probe resumes its own broker after a 750ms observation and bounds the post-resume wait.",
            "Deadline-expired relay retains claim until natural lease expiry; recovery does not manually unlock SQL rows.",
            "Existing test database causes refusal; only database created by this invocation may be dropped, never FORCE.",
            "Other agents may change sources/services concurrently; per-run source hashes and observed changes are recorded.",
        ],
    }
    def interrupted(_signum, _frame):
        raise KeyboardInterrupt
    previous = signal.signal(signal.SIGTERM, interrupted)
    try:
        report["shared_listeners_before"] = shared_listeners(env)
        # Exclusive CREATE, never DROP/RESET an existing database.
        sql(f'CREATE DATABASE "{DATABASE}"', admin)
        created = True
        test_env = dict(admin)
        test_env.update({
            "PGDATABASE": DATABASE, "AGENTTRACE_INFRAFAULT_ENABLED": "1",
            "AGENTTRACE_INFRAFAULT_POSTGRES_DSN": "dbname=" + DATABASE,
        })
        report["versions"] = {
            "postgres": sql("SHOW server_version", admin),
            "go": subprocess.check_output(["go", "version"], env=env, text=True, timeout=5).strip(),
            "redis": subprocess.check_output(["redis-server", "--version"], env=env, text=True, timeout=5).strip(),
            "kafka": subprocess.check_output(["kafka-topics", "--version"], env=env, text=True,
                                             stderr=subprocess.DEVNULL, timeout=10).strip(),
        }
        result = go_run(owned, test_env, deadline)
        report["baseline"] = {key: value for key, value in result.items() if key != "events"}
        report["baseline_test_actions"] = [
            {"test": row["Test"], "action": row["Action"], "elapsed": row.get("Elapsed")}
            for row in result["events"] if row.get("Test") and row.get("Action") in ("pass", "fail", "skip")
        ]
        if result["returncode"]:
            report["baseline_failures"] = [
                row["Output"].strip() for row in result["events"]
                if "infrastructure_integration_test.go:" in row.get("Output", "")
            ]
            raise RuntimeError("real infrastructure test suite failed")
        report["probes"] = collect_evidence(result["events"])
        print("three real infrastructure fault probes passed", flush=True)
        report["negative_controls"] = negative_controls(owned, test_env, deadline)
        report["status"] = "passed"
    except (RuntimeError, ValueError, subprocess.SubprocessError, KeyboardInterrupt) as error:
        report["error"] = "interrupted" if isinstance(error, KeyboardInterrupt) else str(error)
    finally:
        owned.close()
        report["cleanup"] = {"owned_process_groups_closed": not owned.children, "database_dropped": False}
        if created:
            try:
                sql(f'DROP DATABASE "{DATABASE}"', admin)
                report["cleanup"]["database_dropped"] = True
            except RuntimeError:
                report["status"] = "failed"
                report["cleanup"]["database_drop_error"] = "drop refused or owned connections not yet released"
        report["cleanup"]["resource_checks"] = resource_checks(report.get("probes", {}), env)
        if not all(report["cleanup"]["resource_checks"].values()):
            report["status"] = "failed"
            report["error"] = "owned infrastructure resource remained after cleanup"
        report["shared_listeners_after"] = shared_listeners(env)
        report["finished_at"] = datetime.now(timezone.utc).isoformat()
        report["duration_seconds"] = round(time.monotonic() - started, 3)
        if report["duration_seconds"] >= 600:
            report["status"] = "failed"
            report["error"] = "total duration exceeded ten-minute bound"
        signal.signal(signal.SIGTERM, previous)
        write_report(report, args.report)
    print(f"infrastructure report: {report['status']}; duration={report['duration_seconds']}s", flush=True)
    return 0 if report["status"] == "passed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
