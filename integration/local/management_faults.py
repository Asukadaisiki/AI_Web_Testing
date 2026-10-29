#!/usr/bin/env python3
"""Finite real-SDK multiworker load/drain and optional owned-runtime crash.

Uses existing services only. No process launch, automatic restart, SQL mutation,
or automatic Environment review. Signals require explicit operator authorization.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import math
import os
from pathlib import Path
import shlex
import signal
import subprocess
import tempfile
import time
import urllib.parse
import uuid

from acceptance import pg_environment
from management import CheckFailure, Management, PNG, ROOT, TERMINAL, load_environment, record_id, require, sanitize


def validate_bounds(count, timeout):
    require(4 <= count <= 24, "--runs must be between 4 and 24")
    require(0 < timeout <= 900, "--timeout must be between 0 and 900 seconds")


def distribution(values):
    values = sorted(float(v) for v in values if v is not None)
    def percentile(p):
        return values[max(0, math.ceil(len(values) * p) - 1)] if values else None
    return {"count": len(values), "min": values[0] if values else None,
            "p50": percentile(.50), "p95": percentile(.95), "max": values[-1] if values else None}


def parse_processes(output):
    result = {}
    for line in output.splitlines():
        parts = line.split(None, 8)
        if len(parts) != 9:
            continue
        pid, ppid = int(parts[0]), int(parts[1])
        result[pid] = {"pid": pid, "ppid": ppid, "started": " ".join(parts[2:7]),
                       "rss_kib": int(parts[7]), "command": parts[8]}
    return result


def processes(timeout=5):
    # command is retained in memory for identity checking, never in evidence.
    try:
        result = subprocess.run(["ps", "-axo", "pid=,ppid=,lstart=,rss=,command="],
                                capture_output=True, text=True, timeout=timeout,
                                env={**os.environ, "LC_ALL": "C"})
    except subprocess.TimeoutExpired:
        raise CheckFailure("process ownership inspection timed out") from None
    require(result.returncode == 0, "cannot inspect process ownership")
    return parse_processes(result.stdout)


def descendant(table, pid, owner):
    visited = set()
    while pid in table and pid not in visited:
        if pid == owner:
            return True
        visited.add(pid)
        pid = table[pid]["ppid"]
    return False


def tree_rss(table, root):
    if root not in table:
        return None
    return sum(row["rss_kib"] for pid, row in table.items() if descendant(table, pid, root))


class ProcessGuard:
    def __init__(self, table, owner, executable):
        require(owner in table and owner > 1, "owner PID must be a live operator-owned supervisor")
        self.baseline, self.owner = table, owner
        self.executable = str(executable)

    def verify(self, pid, current):
        require(pid != self.owner and pid > 1, "refuse to signal supervisor or system process")
        original = self.baseline.get(pid)
        require(original is not None and pid in current, "runtime PID was not present at preflight")
        require(self.owner in current and
                current[self.owner]["started"] == self.baseline[self.owner]["started"] and
                current[self.owner]["command"] == self.baseline[self.owner]["command"],
                "supervisor PID identity changed")
        row = current[pid]
        require(all(row[k] == original[k] for k in ("ppid", "started", "command")),
                "runtime PID identity changed")
        require(descendant(self.baseline, pid, self.owner) and descendant(current, pid, self.owner),
                "runtime PID is not a descendant of the authorized supervisor")
        try:
            argv = shlex.split(row["command"])
        except ValueError:
            raise CheckFailure("runtime executable cannot be verified") from None
        require(bool(argv) and argv[0] == self.executable,
                "runtime must be a directly launched binary matching --runtime-executable")
        return row


def signal_evidence(guard, pid, current, allowed):
    require(allowed, "owned-runtime signals require --allow-owned-signals")
    row = guard.verify(pid, current)
    return {key: row[key] for key in ("pid", "ppid", "started")}


def witnessed_login(trace, email):
    spans = trace.get("spans", [])
    def asserted(span):
        if not span["name"].endswith("assert_text") or span["status"] != "succeeded":
            return False
        output = span.get("output") or {}
        domain = output.get("structured_result") or (output.get("result") or {}).get("structuredContent") or {}
        if domain.get("ok") is False:
            return False
        text = "Signed in: " + email
        return (text in json.dumps(span.get("input") or {}) or
                any(step.get("action") == "assert_text" and step.get("value") == text
                    for step in domain.get("steps", [])))
    return (any(s["name"].endswith("click") and s["status"] == "succeeded" for s in spans) and
            any(asserted(s) for s in spans))


def count_browser_actions(trace):
    return sum(s.get("kind") in {"tool", "operation"} and s.get("status") == "succeeded" and
               s.get("name", "").startswith(("mcp__browser_harness__", "browser_"))
               for s in trace.get("spans", []))


def recovered_after_effect(value, fence_before, attempt_count):
    return (value.get("status") == "failed" and value.get("error_code") == "execution_lost" and
            value.get("environment_state") == "needs_review" and value.get("attempt_status") == "lost" and
            value.get("assignment_state") == "expired" and value.get("lease_state") == "expired" and
            value.get("fencing_token", 0) > fence_before and value.get("attempt_count") == attempt_count)


class Faults(Management):
    def __init__(self, env, args):
        validate_bounds(args.runs, args.timeout)
        super().__init__(env, args.timeout)
        self.args = args
        self.started = time.monotonic()
        self.deadline = self.started + args.timeout
        self.next_sample = 0
        self.report.update({"schema": "agenttrace.management-faults.v1", "samples": [],
                            "limitations": [
                                "Finite local workload, not a production soak or horizontal browser-store validation.",
                                "Langfuse UI/RBAC acceptance is external to this scenario."],
                            "requested_load_runs": args.runs, "deadline_seconds": args.timeout})
        self.restore = {}
        self.retained = set()
        self.guard = None
        self.runtime_pids = {}
        self.measure_baseline = {}
        self.fault_processes = {}
        self.deleted_conversations = set()

    def remaining(self):
        remaining = self.deadline - time.monotonic()
        require(remaining > 0, "global finite scenario deadline reached")
        return remaining

    def request(self, *args, **kwargs):
        self.timeout = min(35, self.remaining())
        return super().request(*args, **kwargs)

    def sql(self, query):
        # Enforce read-only access in PostgreSQL as well as in the query code.
        try:
            result = subprocess.run(
                ["psql", "--no-psqlrc", "-Atq", "-v", "ON_ERROR_STOP=1", "-c",
                 "BEGIN READ ONLY; SET LOCAL statement_timeout='5s'; " + query + "; COMMIT"],
                env=pg_environment(self.dsn, self.env), capture_output=True, text=True,
                timeout=min(6, self.remaining()))
        except subprocess.TimeoutExpired:
            raise CheckFailure("read-only evidence query exceeded finite deadline") from None
        require(result.returncode == 0, "read-only evidence query failed (diagnostics suppressed)")
        return json.loads(result.stdout)

    def poll(self, callback, description):
        while True:
            self.remaining()
            result = callback()
            self.sample()
            if result:
                return result
            time.sleep(min(.5, self.remaining()))

    def run_ids(self):
        return ",".join(record_id(r["run_id"], "run") for r in self.report["runs"].values()) or "NULL"

    def sample(self, force=False):
        now = time.monotonic()
        if not force and now < self.next_sample:
            return
        self.next_sample = now + 2
        ids = self.run_ids()
        value = self.sql(
            "SELECT json_build_object("
            "'database_connections',(SELECT count(*) FROM pg_stat_activity WHERE datname=current_database()),"
            "'active_database_connections',(SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND state='active'),"
            "'queued_runs',(SELECT count(*) FROM runs WHERE id IN (" + ids + ") AND status='queued'),"
            "'active_runs',(SELECT count(*) FROM runs WHERE id IN (" + ids + ") AND status IN ('running','waiting_input','cancelling')),"
            "'active_assignments',(SELECT count(*) FROM worker_assignments WHERE run_id IN (" + ids + ") AND state='active'),"
            "'runtime_owners',(SELECT COALESCE(json_agg(DISTINCT runtime_worker_id),'[]') FROM worker_assignments WHERE run_id IN (" + ids + ") AND state='active'))")
        value["elapsed_seconds"] = round(now - self.started, 3)
        value["process_tree_rss_kib"] = {}
        if self.measure_baseline:
            table = processes(timeout=min(5, self.remaining()))
            for pid, baseline in self.measure_baseline.items():
                row = table.get(pid)
                if row and all(row[k] == baseline[k] for k in ("started", "command", "ppid")):
                    value["process_tree_rss_kib"][str(pid)] = tree_rss(table, pid)
                else:
                    value["process_tree_rss_kib"][str(pid)] = None
        self.report["samples"].append(value)

    def preflight_faults(self):
        require(bool(self.dsn), "AGENTTRACE_E2E_POSTGRES_DSN is required")
        database = pg_environment(self.dsn, self.env).get("PGDATABASE", "")
        require(database.startswith("agenttrace_test_"), "fault/load scenario requires an isolated agenttrace_test_* database")
        require(len(self.args.runtime_worker) == 2 and len(set(self.args.runtime_worker)) == 2,
                "exactly two distinct --runtime-worker IDs are required")
        view = self.request(f"/projects/{self.project_id}/services")
        by_id = {w["id"]: w for w in view["workers"]}
        workers = []
        for wid in self.args.runtime_worker:
            w = by_id.get(wid, {})
            require(w.get("role") == "runtime" and w.get("alive") and w.get("in_use") == 0 and not w.get("draining"),
                    "selected runtime worker must be live, idle and enabled: " + wid)
            require(w.get("project_id") == self.project_id or self.headers["X-Agenttrace-Role"] == "system_admin",
                    "shared worker drain requires explicit system_admin")
            workers.append(w)
            self.restore[wid] = w["draining"]
        selected = self.select_runtimes(self.request("/runtimes")["runtimes"])
        common = [r for r in selected if all(not w["runtime_ids"] or r in w["runtime_ids"] for w in workers)]
        self.runtime = self.args.runtime_id or (common[0] if common else "")
        require(self.runtime in common, "selected real SDK must be available on both runtime workers")
        other_owners = [w["id"] for w in view["workers"] if w["role"] == "runtime" and w["alive"] and
                        not w["draining"] and w["id"] not in self.args.runtime_worker and
                        (not w["runtime_ids"] or self.runtime in w["runtime_ids"])]
        require(not other_owners, "isolate the scenario: other eligible enabled runtime workers are registered")
        require(any(w["role"] == "browser" and w["alive"] and not w["draining"] and w["capacity"] >= 2
                    for w in view["workers"]), "concurrent scenario requires a live browser provider with capacity >=2")
        self.report["runtime_id"] = self.runtime
        self.report["runtime_workers"] = sanitize(workers)
        if self.args.allow_owned_signals or self.args.measure_pid:
            require(self.args.owner_pid is not None, "--owner-pid is required for process sampling/signals")
            table = processes(timeout=min(5, self.remaining()))
            require(self.args.owner_pid in table, "owner PID is not live")
            for pid in self.args.measure_pid:
                require(pid != self.args.owner_pid and descendant(table, pid, self.args.owner_pid),
                        "memory sampling PID must be an owned child")
                self.measure_baseline[pid] = table[pid]
            if self.args.allow_owned_signals:
                require(self.args.runtime_executable is not None and self.args.runtime_executable.is_absolute(),
                        "signals require an absolute --runtime-executable")
                self.guard = ProcessGuard(table, self.args.owner_pid, self.args.runtime_executable)
                for mapping in self.args.runtime_process:
                    wid, separator, pid = mapping.partition("=")
                    require(separator and wid in self.args.runtime_worker and pid.isdigit(),
                            "--runtime-process must be selected-worker-id=PID")
                    require(wid not in self.runtime_pids, "duplicate runtime process mapping")
                    self.runtime_pids[wid] = int(pid)
                    self.guard.verify(int(pid), table)
                    self.measure_baseline[int(pid)] = table[int(pid)]
                require(set(self.runtime_pids) == set(self.args.runtime_worker) and
                        len(set(self.runtime_pids.values())) == 2, "signals require two unique runtime worker/PID mappings")
                require(bool(self.env.get("AGENTTRACE_MANAGEMENT_IDENTITY_URL")),
                        "crash requires the explicit persistent login fixture URL")
        self.sample(force=True)
        return {"database_scope": database, "runtime_workers": self.args.runtime_worker,
                "runtime_id": self.runtime, "signals_authorized": self.args.allow_owned_signals}

    def new_conversation(self, label):
        c = self.request(f"/projects/{self.project_id}/conversations",
                         {"title": "Management faults " + label + " " + uuid.uuid4().hex[:8],
                          "metadata": {"management_faults": True}}, expected=201)
        self.report["conversations"].append({"id": c["id"], "label": label})
        self.environment_ready(c["id"])
        return c["id"]

    def minimal_goal(self):
        return (f"Open {self.fixture}. Verify Alpha is visible with assert_text. "
                "Use finish_case for exactly one minimal browser case and pass its fresh-context dry run. Stop.")

    def assignments(self, rid):
        return self.sql("SELECT COALESCE(json_agg(json_build_object('attempt_id',a.attempt_id,"
                        "'runtime_worker_id',a.runtime_worker_id,'browser_worker_id',a.browser_worker_id,"
                        "'state',a.state,'fencing_token',a.fencing_token,'sdk_started',h.sdk_started_at IS NOT NULL)), '[]') "
                        "FROM worker_assignments a JOIN harness_attempts h ON h.id=a.attempt_id WHERE a.run_id=" +
                        record_id(rid, "run"))

    def wait_success_batch(self, names):
        def completed():
            states = [self.request("/runs/" + self.report["runs"][n]["run_id"])["status"] for n in names]
            require(not any(s in {"failed", "cancelled"} for s in states), "real concurrent workload failed/cancelled")
            return all(s == "succeeded" for s in states)
        self.poll(completed, "finite concurrent real SDK workload")
        for name in names:
            self.wait_run(name, "succeeded")
            rid = self.report["runs"][name]["run_id"]
            evidence = self.browser_evidence(rid, self.request("/runs/" + rid + "/trace"))
            evidence["assignments"] = self.assignments(rid)
            self.report["runs"][name].update(evidence)
        return {"run_count": len(names), "run_ids": [self.report["runs"][n]["run_id"] for n in names]}

    def load(self):
        names = []
        conversations = [self.new_conversation("load-" + str(i)) for i in range(self.args.runs)]
        for index in range(self.args.runs):
            name = "load-" + str(index)
            self.submit(name, conversations[index], self.runtime, self.minimal_goal())
            names.append(name)
        result = self.wait_success_batch(names)
        require(any(s["active_assignments"] >= 2 for s in self.report["samples"]),
                "no concurrent managed lease execution was actually sampled")
        owners = {a["runtime_worker_id"] for n in names for a in self.report["runs"][n]["assignments"]}
        result["runtime_owners"] = sorted(owners)
        # Forced drain below proves both selected owners even if the initial
        # Kafka partition distribution favours one of them.
        return result

    def set_drain(self, wid, draining):
        value = self.request(f"/projects/{self.project_id}/workers/{wid}", {"draining": draining}, method="PATCH")
        require(value["draining"] == draining, "drain state acknowledgement missing")
        return value

    def drain_rebalance(self):
        first, second = self.args.runtime_worker
        self.set_drain(second, True)
        conversation = self.new_conversation("active-drain")
        rid = self.submit("drain-cancel", conversation, self.runtime,
                          f"Open {self.fixture}. Assert_text Alpha at least 100 times. "
                          "Do not finish_case. Continue until cancelled.")
        def executing():
            run = self.request("/runs/" + rid)
            require(run["status"] not in TERMINAL, "Run finished before active drain")
            rows = self.assignments(rid)
            trace = self.request("/runs/" + rid + "/trace")
            if any(a["sdk_started"] and a["state"] == "active" and a["runtime_worker_id"] == first for a in rows) and any(
                    s["name"].endswith("open_page") and s["status"] == "succeeded" for s in trace["spans"]):
                return rows
            return None
        before = self.poll(executing, "first owner real browser action before active drain")
        drained = self.set_drain(first, True)
        require(drained["in_use"] > 0, "active drain did not retain an in-flight lease")
        started = time.monotonic()
        self.request("/runs/" + rid + "/cancel", {}, expected=202)
        self.wait_run("drain-cancel", "cancelled")
        latency = time.monotonic() - started
        self.set_drain(second, False)
        names = []
        for index in range(2):
            name = "rebalance-" + str(index)
            self.submit(name, self.new_conversation(name), self.runtime, self.minimal_goal())
            names.append(name)
        completed = self.wait_success_batch(names)
        rows = [a for n in names for a in self.report["runs"][n]["assignments"]]
        require(rows and all(a["runtime_worker_id"] == second for a in rows),
                "post-drain work did not move exclusively to second owner")
        services = self.request(f"/projects/{self.project_id}/services")
        owner = next(w for w in services["workers"] if w["id"] == first)
        require(owner["in_use"] == 0 and owner["draining"], "first owner did not drain its capacity")
        self.set_drain(first, False)
        return {"first_owner_assignments": before, "drained_in_use": drained["in_use"],
                "cancel_observed_seconds": latency, "second_owner_assignments": rows, **completed}

    def recovery(self, rid):
        return self.sql("SELECT json_build_object('status',r.status,'error_code',r.error->>'code',"
                        "'environment_state',e.state,'fencing_token',e.fencing_token,'attempt_status',h.status,"
                        "'attempt_count',(SELECT count(*) FROM harness_attempts WHERE run_id=r.id),"
                        "'assignment_state',a.state,'lease_state',l.state) "
                        "FROM runs r JOIN worker_assignments a ON a.run_id=r.id JOIN harness_attempts h ON h.id=a.attempt_id "
                        "JOIN environments e ON e.id=a.environment_id JOIN environment_leases l ON l.attempt_id=h.id "
                        "WHERE r.id=" + record_id(rid, "run") + " ORDER BY a.assigned_at DESC LIMIT 1")

    def crash_after_effect(self):
        first, second = self.args.runtime_worker
        self.set_drain(second, True)
        conversation = self.new_conversation("post-sideeffect-crash")
        self.retained.add(conversation)
        email = "fault-" + uuid.uuid4().hex[:12] + "@example.test"
        url = self.env["AGENTTRACE_MANAGEMENT_IDENTITY_URL"]
        rid = self.submit("post-sideeffect-crash", conversation, self.runtime,
                          f"Open {url}. Fill Email with {email} and Password with fixture-only. Click Sign in. "
                          f"Assert_text 'Signed in: {email}'. Then repeat that assertion at least 100 times. "
                          "Do not finish_case. Continue until cancelled.")
        def sideeffect():
            run = self.request("/runs/" + rid)
            require(run["status"] not in TERMINAL, "crash Run finished before fault injection")
            trace = self.request("/runs/" + rid + "/trace")
            rows = self.assignments(rid)
            active = next((a for a in rows if a["state"] == "active" and a["sdk_started"]), None)
            if active and witnessed_login(trace, email):
                require(active["runtime_worker_id"] == first, "sideeffect execution owner differs from explicit target")
                return trace, active
            return None
        trace, assignment = self.poll(sideeffect, "actual browser login side effect before owned-runtime crash")
        paths = [p for e in trace.get("operation_evidence", [])
                 for p in (e.get("artifacts") or {}).get("screenshot_paths", [])]
        require(bool(paths), "sideeffect crash has no actual browser artifact")
        path = paths[-1]
        require(path.startswith(rid + "/") and ".." not in Path(path).parts, "cross-Run crash artifact")
        image = self.request(self.api.removesuffix("/api/v1") + "/artifacts/" + urllib.parse.quote(path), binary=True)
        require(image.startswith(PNG), "sideeffect artifact is not PNG")
        baseline = self.recovery(rid)
        pid = self.runtime_pids[first]
        require(self.request("/runs/" + rid)["status"] not in TERMINAL,
                "sideeffect Run terminated before signal ownership verification")
        fresh = self.assignments(rid)
        require(any(a["attempt_id"] == assignment["attempt_id"] and a["state"] == "active" and
                    a["runtime_worker_id"] == first for a in fresh), "sideeffect lease ended before signal")
        self.fault_processes = processes(timeout=min(5, self.remaining()))
        identity = signal_evidence(self.guard, pid, self.fault_processes, self.args.allow_owned_signals)
        # Persist useful evidence in memory before the only destructive action.
        evidence = {"run_id": rid, "conversation_id": conversation, "runtime_worker_id": first,
                    "process_identity": identity, "signal": "SIGKILL", "assignment_before": assignment,
                    "browser_asserted_text": "Signed in: " + email,
                    "screenshot_bytes": len(image), "screenshot_sha256": hashlib.sha256(image).hexdigest(),
                    "external_effects": "uncertain; operator review required"}
        self.report["fault_injection"] = evidence
        self.remaining()
        os.kill(pid, signal.SIGKILL)
        evidence["signal_sent"] = True
        self.report["limitations"].append(
            "Killed runtime is not restarted. Orphan SDK/browser children may remain; parent must inspect owned descendants. "
            "Crash Conversation retained for review; no automatic review/reset/delete.")
        self.set_drain(second, False)
        def recovered():
            value = self.recovery(rid)
            require(value["attempt_count"] == baseline["attempt_count"], "started external-effect Run was automatically replayed")
            return value if recovered_after_effect(value, assignment["fencing_token"], baseline["attempt_count"]) else None
        evidence["recovery"] = self.poll(recovered, "lost started attempt and needs_review fencing")
        # Observe the same run after real survivor work, not just a unit probe.
        name = "post-crash-survivor"
        self.submit(name, self.new_conversation(name), self.runtime, self.minimal_goal())
        evidence["survivor"] = self.wait_success_batch([name])
        require(all(a["runtime_worker_id"] == second for a in self.report["runs"][name]["assignments"]),
                "survivor work did not execute on second owner")
        later = self.recovery(rid)
        require(recovered_after_effect(later, assignment["fencing_token"], baseline["attempt_count"]),
                "uncertain sideeffect was replayed or silently made ready")
        evidence["recovery_after_survivor_work"] = later
        return evidence

    def measurements(self):
        self.sample(force=True)
        ids = self.run_ids()
        values = self.sql("SELECT COALESCE(json_agg(json_build_object('run_id',r.id,'status',r.status,"
                          "'runtime_worker_id',a.runtime_worker_id,"
                          "'queue_seconds',EXTRACT(EPOCH FROM a.assigned_at-r.created_at),"
                          "'sdk_queue_seconds',EXTRACT(EPOCH FROM h.sdk_started_at-r.created_at),"
                          "'lease_seconds',EXTRACT(EPOCH FROM a.released_at-a.assigned_at),"
                          "'cancel_seconds',CASE WHEN r.status='cancelled' THEN EXTRACT(EPOCH FROM r.finished_at-r.cancel_requested_at) END)), '[]') "
                          "FROM runs r LEFT JOIN worker_assignments a ON a.run_id=r.id "
                          "LEFT JOIN harness_attempts h ON h.id=a.attempt_id WHERE r.id IN (" + ids + ")")
        samples = self.report["samples"]
        metrics = {"elapsed_seconds": time.monotonic() - self.started, "sample_count": len(samples),
                   "scope": "DB connections include all sessions in selected test DB, including this read-only probe. "
                            "Queue/lease/cancel measurements include only scenario-created runs. RSS includes selected child trees; overlapping trees are not summed.",
                   "per_attempt": values,
                   "queue_seconds": distribution(v["queue_seconds"] for v in values),
                   "sdk_queue_seconds": distribution(v["sdk_queue_seconds"] for v in values),
                   "lease_seconds": distribution(v["lease_seconds"] for v in values),
                   "cancel_seconds": distribution(v["cancel_seconds"] for v in values),
                   "database_connections": distribution(s["database_connections"] for s in samples),
                   "process_tree_rss_kib": {str(pid): distribution(s["process_tree_rss_kib"].get(str(pid)) for s in samples)
                                            for pid in self.measure_baseline}}
        self.report["measurements"] = metrics
        if not self.measure_baseline:
            self.report["limitations"].append("Process memory not measured: no explicitly owned --measure-pid supplied.")
        for argument, metric, statistic in (
                ("max_db_connections", "database_connections", "max"),
                ("max_queue_seconds", "queue_seconds", "p95"),
                ("max_cancel_seconds", "cancel_seconds", "max")):
            threshold = getattr(self.args, argument)
            if threshold:
                actual = metrics[metric][statistic]
                require(actual is not None and actual <= threshold, argument + " operator budget exceeded or not measured")
        return metrics

    def run(self):
        self.check("finite-fault-preflight", self.preflight_faults)
        self.check("immutable-registry-bootstrap", self.publish_acceptance_harness)
        self.check("concurrent-real-sdk-load", self.load)
        self.check("active-drain-cancel-rebalance", self.drain_rebalance)
        if self.args.allow_owned_signals:
            self.check("owned-crash-after-external-effect", self.crash_after_effect)
        else:
            self.skip("owned-crash-after-external-effect", "No explicit --allow-owned-signals authorization")
        self.check("phase3-actual-measurements", self.measurements)
        self.delete_healthy()

    def delete_healthy(self):
        # Preserve fault records (and their needs_review identity) for the operator.
        original = self.report["conversations"]
        healthy = [c for c in original if c["id"] not in self.retained]
        self.report["conversations"] = healthy
        try:
            evidence = self.check("healthy-conversation-delete", self.delete_conversations)
            self.deleted_conversations.update(c["conversation_id"] for c in evidence)
        finally:
            self.report["conversations"] = original
        self.report["retained_conversations"] = sorted(self.retained)

    def cleanup(self):
        # Cleanup also has one finite budget, never one timeout per created Run.
        self.deadline = time.monotonic() + 30
        for wid, previous in self.restore.items():
            try:
                self.set_drain(wid, previous)
            except (CheckFailure, OSError):
                self.report.setdefault("cleanup_incomplete", []).append("worker-drain:" + wid)
        for entry in self.report["runs"].values():
            rid = entry["run_id"]
            if rid not in self.created:
                continue
            try:
                run = self.request("/runs/" + rid)
                if run["status"] not in TERMINAL:
                    self.request("/runs/" + rid + "/cancel", {}, expected=202)
                    self.poll(lambda: self.request("/runs/" + rid)["status"] in TERMINAL, "bounded cleanup cancellation")
            except (CheckFailure, OSError):
                self.report.setdefault("cleanup_incomplete", []).append("run:" + rid)
        if self.report.get("fault_injection", {}).get("signal_sent"):
            # Inspection only; never signal SDK descendants opportunistically.
            try:
                table = processes(timeout=min(5, self.remaining()))
                survivors = [{"pid": pid, "ppid": row["ppid"], "started": row["started"]}
                             for pid, row in table.items() if self.guard and pid in self.fault_processes and
                             pid not in self.runtime_pids.values() and
                             descendant(self.fault_processes, pid, self.runtime_pids[self.args.runtime_worker[0]]) and
                             row["started"] == self.fault_processes[pid]["started"]]
                self.report["surviving_fault_time_runtime_descendants"] = survivors
            except (CheckFailure, OSError):
                self.report["descendant_inspection"] = "unavailable; operator must inspect"
        self.report["elapsed_total_seconds"] = round(time.monotonic() - self.started, 3)
        self.report["retained_conversations"] = sorted(c["id"] for c in self.report["conversations"]
                                                      if c["id"] not in self.deleted_conversations)


def parser():
    result = argparse.ArgumentParser(description=__doc__)
    result.add_argument("--env-file", type=Path, action="append", default=[])
    result.add_argument("--runtime-worker", action="append", required=True)
    result.add_argument("--runtime-id", choices=("pi", "claude-code", "codex"))
    result.add_argument("--runs", type=int, default=6, help="finite initial concurrent run count, 4-24")
    result.add_argument("--timeout", type=float, default=600, help="global deadline <=900 seconds plus <=30s cleanup")
    result.add_argument("--report", type=Path, default=Path(tempfile.gettempdir()) / "management-faults.json")
    result.add_argument("--owner-pid", type=int)
    result.add_argument("--measure-pid", type=int, action="append", default=[])
    result.add_argument("--allow-owned-signals", action="store_true")
    result.add_argument("--runtime-executable", type=Path)
    result.add_argument("--runtime-process", action="append", default=[], help="selected-worker-id=direct-binary-PID")
    result.add_argument("--max-db-connections", type=int, default=0, help="optional measured peak connection budget")
    result.add_argument("--max-queue-seconds", type=float, default=0, help="optional measured p95 assignment delay budget")
    result.add_argument("--max-cancel-seconds", type=float, default=0, help="optional durable max cancel delay budget")
    return result


def main():
    cli = parser()
    args = cli.parse_args()
    runner = None
    report = {"schema": "agenttrace.management-faults.v1", "checks": {}, "status": "failed"}
    result = 1
    try:
        validate_bounds(args.runs, args.timeout)
        require(all(getattr(args, k) >= 0 for k in ("max_db_connections", "max_queue_seconds", "max_cancel_seconds")),
                "measurement budgets cannot be negative")
        runner = Faults(load_environment([ROOT / ".env", *args.env_file]), args)
        runner.run()
        result = 0
    except (CheckFailure, OSError, ValueError, KeyError, StopIteration) as error:
        report = runner.report if runner else report
        report["status"] = "failed"
        report["error"] = str(error) if isinstance(error, CheckFailure) else type(error).__name__
    except KeyboardInterrupt:
        report = runner.report if runner else report
        report["status"] = "interrupted"
        result = 130
    finally:
        if runner:
            runner.cleanup()
            report = runner.report
            report.setdefault("status", runner.outcome())
            if report["status"] == "failed":
                result = 1
        args.report.parent.mkdir(parents=True, exist_ok=True)
        with args.report.open("w") as output:
            os.chmod(args.report, 0o600)
            json.dump(sanitize(report), output, indent=2)
            output.write("\n")
    print("Management faults: " + report.get("status", "failed") + "; report: " + str(args.report), flush=True)
    return result


if __name__ == "__main__":
    raise SystemExit(main())
