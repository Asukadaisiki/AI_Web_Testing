#!/usr/bin/env python3
"""Bounded native fault drills against the isolated local stack.

Pauses only the dedicated Kafka 9192 broker and launcher-owned Langfuse web.
Uses a scoped PostgreSQL trigger and a separate API for Redis degradation.
All injected faults are removed in finally blocks; existing runs are retained.
"""

import argparse
import json
import os
from pathlib import Path
import signal
import subprocess
import tempfile
import time
import uuid

from acceptance import Acceptance, ROOT, load_environment, require, sql_id
from run import environment


def pause_owned(pid: int, expected: str):
    result = subprocess.run(["ps", "-p", str(pid), "-o", "lstart=", "-o", "command="],
                            capture_output=True, text=True, env={**os.environ, "LC_ALL": "C"})
    require(result.returncode == 0 and result.stdout.strip() == expected,
            "fault target process identity changed")
    os.kill(pid, signal.SIGSTOP)


def pending(runner, run_id):
    return runner.sql(f"""SELECT to_json(count(*)) FROM outbox_events
        WHERE published_at IS NULL AND key={sql_id(run_id)};""")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--env-file", type=Path, action="append", default=[])
    parser.add_argument("--report", type=Path, required=True)
    parser.add_argument("--worker-audit", type=Path, required=True,
                        help="worker_audit.py log; runtime must use its forwarding URL")
    args = parser.parse_args()
    env = environment()
    env.update(load_environment(args.env_file))
    env.setdefault("AGENTTRACE_E2E_POSTGRES_DSN", env["AGENTTRACE_POSTGRES_DSN"])
    runner = Acceptance(env, 180)
    results = {}
    try:
        runner.preflight()
        require(env["AGENTTRACE_WORKER_URL"] == "http://127.0.0.1:8111",
                "fault drills require the scoped Worker audit proxy on 8111")
        project = runner.http(runner.api + "/projects", {
            "name": "Native fault drills " + uuid.uuid4().hex,
        }, expected=201)
        runner.project_id = project["id"]
        goal = (f"Open {runner.fixture}, verify Alpha with assert_text, then use finish_case "
                "to create exactly one minimal case and pass its fresh-context dry run.")

        # Event persistence fails before the first tool is acknowledged.
        marker = "FAULT_" + uuid.uuid4().hex
        function = "acceptance_fault_" + uuid.uuid4().hex
        runner.sql(f"""CREATE FUNCTION {function}() RETURNS trigger LANGUAGE plpgsql AS $$
            BEGIN
              IF NEW.event_type='pi.tool.started' AND EXISTS (
                SELECT 1 FROM runs WHERE id=NEW.run_id AND specification::text LIKE '%{marker}%'
              ) THEN RAISE EXCEPTION 'scoped acceptance evidence failure'; END IF;
              RETURN NEW;
            END $$;
            CREATE TRIGGER {function} BEFORE INSERT ON run_events
            FOR EACH ROW EXECUTE FUNCTION {function}();
            SELECT to_json(true);""")
        try:
            failed = runner.create_run("persistence_failure", goal + " Reference " + marker)
            sibling = runner.create_run("healthy_sibling", goal)
            runner.wait_terminal(failed, "failed")
            runner.wait_terminal(sibling, "succeeded")
            sibling_audit = [record for line in args.worker_audit.read_text().splitlines()
                             if (record := json.loads(line)).get("scope") == sibling]
            require(any(record["path"] == "/execute" for record in sibling_audit),
                    "healthy sibling is missing audited Worker execution; proxy wiring unproven")
            before = runner.counts(failed)
            def actions():
                return [record for line in args.worker_audit.read_text().splitlines()
                        if (record := json.loads(line)).get("scope") == failed
                        and record["method"] != "DELETE"]
            worker_before = actions()
            time.sleep(1)
            after = runner.counts(failed)
            require(before == after, "failed evidence run continued writing after cleanup")
            require(actions() == worker_before, "Worker actions continued after failure cleanup")
            # The first tool may already have reached the Worker before Go receives
            # its started event. Cancellation must prevent a second browser action.
            browser_actions = [a for a in worker_before
                               if a["path"] != "/sessions"]
            require(len(browser_actions) <= 1,
                    "persistence failure allowed subsequent unrecorded Worker actions")
            attempts = runner.sql(f"""SELECT to_json(count(*)) FROM harness_attempts
                WHERE run_id={sql_id(failed)};""")
            require(attempts == 1, "persistence failure replayed browser execution")
            results["persistence_failure_isolation"] = {
                "status": "passed", "failed_run": failed, "healthy_run": sibling,
                "harness_attempts": attempts, "evidence_frozen": True,
                "worker_actions": len(browser_actions), "worker_actions_frozen": True,
                "healthy_worker_execution_audited": True,
            }
        finally:
            runner.sql(f"""DROP TRIGGER IF EXISTS {function} ON run_events;
                DROP FUNCTION IF EXISTS {function}(); SELECT to_json(true);""")

        # Redis failure is injected only into this separate API process.
        degraded_env = {**env, "AGENTTRACE_HTTP_ADDR": "127.0.0.1:8181",
                        "AGENTTRACE_REDIS_ADDR": "127.0.0.1:6399"}
        with tempfile.TemporaryDirectory(prefix="agenttrace-degraded-api-") as directory:
            log = (Path(directory) / "api.log").open("w")
            os.chmod(log.name, 0o600)
            child = subprocess.Popen(["go", "run", "./cmd/agenttrace-api"],
                                     cwd=ROOT / "backend", env=degraded_env,
                                     stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
            try:
                def ready():
                    require(child.poll() is None, "degraded API exited")
                    try:
                        return runner.http("http://127.0.0.1:8181/api/v1/health")
                    except Exception:
                        return None
                health = runner.poll(ready, "degraded API startup")
                require(health["status"] == "degraded", "Redis outage not reflected in health")
                original = runner.api
                runner.api = "http://127.0.0.1:8181/api/v1"
                runner.report["runs"]["redis_degraded"] = {"run_id": sibling}
                try:
                    runner.verify_sse("redis_degraded", sibling)
                finally:
                    runner.api = original
                results["redis_degraded_replay"] = {"status": "passed", "health": "degraded"}
            finally:
                os.killpg(child.pid, signal.SIGTERM)
                try:
                    child.wait(timeout=20)
                except subprocess.TimeoutExpired:
                    os.killpg(child.pid, signal.SIGKILL)
                    child.wait()
                log.close()

        # The broker must be dedicated to this native stack.
        listener = subprocess.run(["lsof", "-t", "-iTCP:9192", "-sTCP:LISTEN"],
                                  capture_output=True, text=True, check=True)
        kafka_pid = int(listener.stdout.strip())
        identity = subprocess.run(["ps", "-p", str(kafka_pid), "-o", "lstart=", "-o", "command="],
                                  env={**os.environ, "LC_ALL": "C"}, capture_output=True,
                                  text=True, check=True).stdout.strip()
        state = Path(env.get("AGENTTRACE_LOCAL_STATE",
                            str(Path(tempfile.gettempdir()) / "agenttrace-local")))
        require("kafka" in identity and str(state / "kafka/server.properties") in identity,
                "broker is not the dedicated native Kafka process")
        pause_owned(kafka_pid, identity)
        try:
            queued = runner.create_run("kafka_recovery", goal)
            runner.poll(lambda: pending(runner, queued), "durable outbox while Kafka is unavailable")
            require(runner.http(runner.api + f"/runs/{queued}")["status"] == "queued",
                    "run executed while its command was undelivered")
        finally:
            os.kill(kafka_pid, signal.SIGCONT)
        runner.wait_terminal(queued, "succeeded")
        runner.poll(lambda: pending(runner, queued) == 0, "outbox drains after Kafka recovers")
        results["kafka_outbox_recovery"] = {"status": "passed", "run_id": queued}

        # Suspend only the verified launcher-owned web process. The HTTP client
        # timeout exercises a real exporter retry; resume after the first retry.
        lf_root = Path(os.environ.get("LANGFUSE_NATIVE_ROOT", "/tmp/agenttrace-native/langfuse"))
        saved = json.loads((lf_root / "web.pid.json").read_text())
        pause_owned(saved["pid"], saved["identity"])
        try:
            export_run = runner.create_run("langfuse_retry", goal)
            def retried():
                return runner.sql(f"""SELECT to_json(coalesce(sum(attempt_count),0))
                    FROM consumer_retry_counts WHERE consumer LIKE '%langfuse%'
                    AND payload::text LIKE '%{export_run}%';""") > 0
            runner.poll(retried, "exporter records retry during Langfuse HTTP outage")
        finally:
            os.kill(saved["pid"], signal.SIGCONT)
        runner.wait_terminal(export_run, "succeeded")
        runner.report["runs"]["success"] = runner.report["runs"]["langfuse_retry"]
        runner.verify_langfuse("success", export_run)
        results["langfuse_async_retry"] = {"status": "passed", "run_id": export_run}
        results["status"] = "passed"
    except Exception as error:
        results["status"] = "failed"
        # Payloads, credentials and database diagnostics never enter reports.
        results["error"] = str(error) if type(error).__name__ == "CheckFailure" else type(error).__name__
    finally:
        runner.cancel_remaining()
        args.report.parent.mkdir(parents=True, exist_ok=True)
        args.report.write_text(json.dumps(results, indent=2) + "\n")
        os.chmod(args.report, 0o600)
    print(f"Native fault drills: {results['status']}; report: {args.report}")
    if results.get("error"):
        print(results["error"])
    return 0 if results["status"] == "passed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
