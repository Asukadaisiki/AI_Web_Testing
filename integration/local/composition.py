#!/usr/bin/env python3
"""Real SDK/Harness acceptance against the native application, without Docker."""
from __future__ import annotations

import argparse
import base64
import concurrent.futures
import json
from pathlib import Path
import tempfile
import time
import urllib.error
import urllib.request
import uuid

from acceptance import Acceptance, CheckFailure, load_environment, require, ROOT, sql_id

RUNTIMES = ("pi", "claude-code", "codex")
TERMINAL = {"succeeded", "failed", "cancelled"}

def has_nonzero_usage(observation):
    # Langfuse synthesizes legacy zero usage even when ingestion omitted usage.
    return any(isinstance(value, (int, float)) and not isinstance(value, bool) and value != 0
               for field in ("usageDetails", "usage")
               for value in (observation.get(field) or {}).values())


class Composition(Acceptance):
    def __init__(self, env, timeout):
        super().__init__(env, timeout)
        self.report = {"checks": {}, "runs": {}, "runtime_sdks": {
            "pi": "@earendil-works/pi-coding-agent@1.4.0",
            "claude-code": "@anthropic-ai/claude-agent-sdk@0.3.311",
            "codex": "@openai/codex-sdk@0.165.0",
        }}
        self.harness_ids = []

    def request(self, path, body=None, method=None, expected=200):
        req = urllib.request.Request(self.api + path,
            data=None if body is None else json.dumps(body).encode(),
            headers={"Content-Type": "application/json"}, method=method)
        try:
            with urllib.request.urlopen(req, timeout=20) as response:
                require(response.status == expected, f"{path}: unexpected status")
                return json.load(response) if response.status != 204 else None
        except urllib.error.HTTPError as error:
            raise CheckFailure(f"{path}: HTTP {error.code}") from None

    def submit(self, kind, runtime, harness, goal):
        conversation = self.request(f"/projects/{self.project_id}/conversations",
            {"title": kind, "metadata": {"composition_acceptance": True}}, expected=201)
        body = {"content": goal, "runtime_id": runtime, "harness_id": harness["id"],
                "idempotency_key": uuid.uuid4().hex}
        path = f"/conversations/{conversation['id']}/messages"
        result = self.request(path, body, expected=202)
        self.created.append(result["run_id"])
        duplicate = self.request(path, body, expected=202)
        require(duplicate["run_id"] == result["run_id"], "duplicate submission created another Run")
        self.report["runs"][kind] = {"run_id": result["run_id"], "runtime_id": runtime,
            "harness_id": harness["id"], "harness_revision": harness["revision"],
            "conversation_id": conversation["id"]}
        return result["run_id"]

    def verify(self, kind, expected="succeeded", browser=False, marker=None):
        entry = self.report["runs"][kind]
        rid = entry["run_id"]
        run = self.wait_terminal(rid, expected)
        require(all(run.get(k) == entry[k] for k in
                    ("runtime_id", "harness_id", "harness_revision")), "Run lost execution identity")
        trace = self.request(f"/runs/{rid}/trace")
        entry["status"] = run["status"]
        entry["model_spans"] = sum(s["kind"] == "model" for s in trace["spans"])
        entry["usage_records"] = len(trace["usage"])
        entry["total_tokens"] = sum(u["total_tokens"] for u in trace["usage"])
        if expected != "cancelled":
            require(entry["model_spans"] > 0 and entry["total_tokens"] > 0,
                    f"{kind}: SDK did not emit actual model completion/usage")
            outputs = [json.dumps(s.get("output", {})) for s in trace["spans"] if s["kind"] == "model"]
            if marker:
                require(any(marker in value for value in outputs), f"{kind}: selected prompt/skill was not applied")
            entry["reported_models"] = sorted({u.get("model", "") for u in trace["usage"]})
        if browser:
            require(any(s["name"].endswith("finish_case") and s["status"] == "succeeded"
                        for s in trace["spans"]), f"{kind}: actual browser dry run missing")
            paths = [p for e in trace["operation_evidence"]
                     for p in (e.get("artifacts") or {}).get("screenshot_paths", [])]
            require(bool(paths), f"{kind}: screenshot evidence missing")
            for path in paths:
                require(path.startswith(rid + "/"), "browser artifact leaked across Runs")
                with urllib.request.urlopen(self.api.removesuffix("/api/v1") + "/artifacts/" + path) as r:
                    require(r.read(8) == b"\x89PNG\r\n\x1a\n", "browser artifact is not PNG")
            entry["downloaded_screenshots"] = len(paths)
        counts = self.counts(rid)
        require(counts["completed"] == 1, "Run has more than one terminal event")
        entry["durable_completed_events"] = counts["completed"]
        entry["browser_sessions_remaining"] = self.sql(
            f"SELECT to_json(count(*)) FROM spans WHERE run_id={sql_id(rid)} AND status='running';")
        require(entry["browser_sessions_remaining"] == 0, "Run has unfinished spans")
        audit_log = self.env.get("AGENTTRACE_E2E_WORKER_AUDIT")
        if audit_log and not kind.startswith("generic-"):
            def closed():
                records = [json.loads(line) for line in Path(audit_log).read_text().splitlines()]
                scoped = [row for row in records if row.get("response_scope") == rid
                          or row.get("response_scope", "").startswith(rid + "/")]
                created = [row for row in scoped if row.get("response_path") == "/sessions"
                           and row.get("status") == 200]
                released = [row for row in scoped if row.get("response_method") == "DELETE"
                            and row.get("status") == 200 and row.get("closed_ack") is True]
                return released if created and len(released) == len(created) else None
            acknowledgements = self.poll(closed, "actual browser DELETE acknowledgement")
            entry["browser_cleanup_acknowledgements"] = len(acknowledgements)
        self.verify_sse(kind, rid)
        print(f"{kind}: {expected}, tokens={entry['total_tokens']}", flush=True)
        return trace

    def verify_snapshot(self, rid, snapshot):
        actual = self.sql(f"SELECT specification #> '{{command,execution,harness}}' "
                          f"FROM runs WHERE id={sql_id(rid)}")
        require(actual == snapshot, "immutable execution snapshot differs from submitted Harness")

    def langfuse(self):
        public = self.env.get("AGENTTRACE_LANGFUSE_PUBLIC_KEY")
        secret = self.env.get("AGENTTRACE_LANGFUSE_SECRET_KEY")
        if not public or not secret:
            self.report["checks"]["optional_langfuse"] = "disabled; core SDK matrix ran without exporter"
            return
        self.lf_headers = {"Authorization": "Basic " +
            base64.b64encode(f"{public}:{secret}".encode()).decode()}
        self.lf_url = self.env.get("AGENTTRACE_LANGFUSE_BASE_URL", "http://127.0.0.1:3300").rstrip("/")
        for runtime in RUNTIMES:
            entry = self.report["runs"]["browser-" + runtime]
            trace_id = "agenttrace-trace-" + entry["run_id"]
            canonical = self.request(f"/runs/{entry['run_id']}/trace")
            def ready():
                try:
                    trace = self.http(self.lf_url + "/api/public/traces/" + trace_id,
                                      headers=self.lf_headers)
                except CheckFailure:
                    return None
                if ((trace.get("metadata") or {}).get("agenttrace_status") != "succeeded" or
                    len(trace.get("observations", [])) < len(canonical["spans"])):
                    return None
                return trace
            trace = self.poll(ready, "optional Langfuse export")
            entry["langfuse_observations"] = len(trace["observations"])
            require(trace["metadata"]["agenttrace_run_id"] == entry["run_id"],
                    "Langfuse Run correlation differs from canonical trace")
            query_usage = [u for u in canonical["usage"] if
                (u.get("details") or {}).get("granularity") == "sdk_query_model_cumulative"]
            if query_usage:
                exported = trace["metadata"].get("agenttrace_sdk_query_usage") or []
                require({u["id"] for u in exported} == {u["id"] for u in query_usage},
                        "Langfuse cumulative SDK query usage is missing")
                model_ids = {s["id"] for s in canonical["spans"] if s["kind"] == "model"}
                require(all(u["span_id"] not in model_ids for u in query_usage),
                        "cumulative query usage was attached to an individual assistant message")
                require(all(not has_nonzero_usage(o) for o in
                            trace["observations"] if o["type"] == "GENERATION"),
                        "Langfuse assigned cumulative query tokens to one model generation")
                entry["sdk_query_usage_scope_verified"] = True
        self.report["checks"]["optional_langfuse"] = "passed for all three SDKs"

    def run(self, resume=None, refresh_runtimes=()):
        require(self.request("/health")["status"] == "ok", "native stack is unavailable")
        catalog = self.request("/runtimes")["runtimes"]
        require({r["id"] for r in catalog} == set(RUNTIMES), "runtime catalog is incomplete")
        require(all(r["available"] for r in catalog), "one or more actual runtimes are unavailable")
        harnesses = self.request("/harnesses")["harnesses"]
        if resume:
            self.report = json.loads(resume.read_text())
            self.report.pop("error", None)
            self.project_id = self.report["project_id"]
            self.created = [entry["run_id"] for entry in self.report["runs"].values()]
            def snapshot(kind):
                rid = self.report["runs"][kind]["run_id"]
                return self.sql(f"SELECT specification #> '{{command,execution,harness}}' "
                                f"FROM runs WHERE id={sql_id(rid)}")
            browser, generic = snapshot("browser-pi"), snapshot("generic-pi")
            for kind in list(self.report["runs"]):
                entry = self.report["runs"][kind]
                if entry["runtime_id"] not in refresh_runtimes or kind.startswith("rerun-"):
                    continue
                self.report.setdefault("prior_runs", {}).setdefault(kind, []).append(entry.copy())
                if kind.startswith(("generic-", "browser-")):
                    goal = self.sql(f"SELECT to_json(specification #>> '{{command,messages,-1,content}}') "
                                    f"FROM runs WHERE id={sql_id(entry['run_id'])}")
                    self.submit(kind, entry["runtime_id"],
                                generic if kind.startswith("generic-") else browser, goal)
                else:
                    del self.report["runs"][kind]
            for kind in ["generic-" + r for r in RUNTIMES] + ["browser-" + r for r in RUNTIMES]:
                entry = self.report["runs"][kind]
                if self.request(f"/runs/{entry['run_id']}")["status"] == "failed":
                    source = entry["run_id"]
                    rerun = self.request(f"/runs/{source}/reruns",
                        {"idempotency_key": uuid.uuid4().hex}, expected=202)["run_id"]
                    entry["run_id"] = rerun
                    entry.setdefault("previous_failed_runs", []).append(source)
                    self.created.append(rerun)
        else:
            browser = next(h for h in harnesses if h["id"] == "browser-harness")
            if self.env.get("AGENTTRACE_E2E_WORKER_URL"):
                browser = json.loads(json.dumps(browser))
                browser.pop("id")
                browser.pop("revision")
                browser["name"] = "Audited browser Harness " + uuid.uuid4().hex
                for server in browser["mcp"].values():
                    server.setdefault("env", {})["AGENTTRACE_HARNESS_WORKER_URL"] = self.env["AGENTTRACE_E2E_WORKER_URL"]
                browser = self.request("/harnesses", browser, expected=201)
                self.harness_ids.append(browser["id"])
            generic = self.request("/harnesses", {"name": "Composition acceptance " + uuid.uuid4().hex,
                "system_prompt": "Follow the acceptance skill. Return only its exact required line.",
                "skills": [{"name": "acceptance", "content":
                    "---\nname: acceptance\ndescription: Mandatory acceptance response\n---\n"
                    "The exact required line is HARNESS_ALPHA_SKILL_BETA. Always output it."}],
                "mcp": {}}, expected=201)
            self.harness_ids.append(generic["id"])
            self.project_id = self.request("/projects", {"name": "SDK composition " + uuid.uuid4().hex,
                "metadata": {"composition_acceptance": True}}, expected=201)["id"]
            self.report["project_id"] = self.project_id
            for runtime in RUNTIMES:
                self.submit("generic-" + runtime, runtime, generic, "Use the acceptance skill and answer.")
                self.submit("browser-" + runtime, runtime, browser,
                    f"Open {self.fixture}, verify Alpha is visible using assert_text. "
                    "Use finish_case to create one minimal case and pass its fresh-context dry run. "
                    "Stop after finish_case succeeds.")
        self.report["catalog"] = catalog
        failures = []
        with concurrent.futures.ThreadPoolExecutor(max_workers=3) as pool:
            pending = {pool.submit(self.verify, kind, browser=kind.startswith("browser"),
                marker="HARNESS_ALPHA_SKILL_BETA" if kind.startswith("generic") else None): kind
                for kind in ["generic-" + r for r in RUNTIMES] + ["browser-" + r for r in RUNTIMES]}
            for future in concurrent.futures.as_completed(pending):
                try:
                    future.result()
                except Exception as error:
                    failures.append(pending[future] + ": " +
                                    (str(error) if isinstance(error, CheckFailure) else type(error).__name__))
        require(not failures, "; ".join(failures))
        self.report["checks"]["six_real_sdk_combinations"] = "passed"
        for runtime in RUNTIMES:
            self.verify_snapshot(self.report["runs"]["generic-" + runtime]["run_id"], generic)
        if any(h["id"] == generic["id"] for h in harnesses) or not resume:
            updated = self.request("/harnesses/" + generic["id"], {**generic,
                "system_prompt": "Return only HARNESS_CHANGED.", "skills": []}, method="PUT")
            require(updated["revision"] > generic["revision"], "Harness revision did not increment")
            self.request("/harnesses/" + generic["id"], method="DELETE")
        for runtime in RUNTIMES:
            self.verify_snapshot(self.report["runs"]["generic-" + runtime]["run_id"], generic)
        self.report["checks"]["generic_snapshot_after_edit_delete"] = "passed"
        # Rerun eligibility is deliberately limited to failed Runs.
        for runtime in RUNTIMES:
            kind = "negative-" + runtime
            if kind not in self.report["runs"]:
                self.submit(kind, runtime, browser,
                    f"Open {self.fixture}. Assert the literal text COMPOSITION_MISSING_"
                    f"{uuid.uuid4().hex} is visible, using assert_text exactly once. "
                    "When false, stop browser tools and return 'Expected negative assertion failed'. "
                    "Do not retry or repair, and do not call finish_case.")
            self.verify(kind, expected="failed")
        for runtime in RUNTIMES:
            kind = "cancel-" + runtime
            if kind in self.report["runs"]:
                self.verify(kind, expected="cancelled")
                continue
            rid = self.submit(kind, runtime, browser,
                f"Open {self.fixture}, then call assert_text to verify Alpha is visible "
                "at least 20 times in separate calls. Do not call finish_case; keep checking until instructed.")
            def active_browser():
                run = self.request(f"/runs/{rid}")
                require(run["status"] not in TERMINAL, "SDK finished before active cancellation")
                trace = self.request(f"/runs/{rid}/trace")
                return run["status"] == "running" and any(
                    s["name"].endswith("open_page") and s["status"] == "succeeded" for s in trace["spans"])
            self.poll(active_browser, "actual SDK browser tool before cancellation")
            self.request(f"/runs/{rid}/cancel", {}, expected=202)
            self.verify("cancel-" + runtime, expected="cancelled")
        self.report["checks"]["active_sdk_cancellation"] = "passed"
        require(browser["id"] != "browser-harness", "snapshot deletion needs an isolated browser Harness")
        if any(h["id"] == browser["id"] for h in harnesses) or not resume:
            updated = self.request("/harnesses/" + browser["id"], {**browser,
                "system_prompt": "Return only CHANGED_BROWSER_PROMPT.", "skills": []}, method="PUT")
            require(updated["revision"] > browser["revision"], "browser Harness revision did not increment")
            self.request("/harnesses/" + browser["id"], method="DELETE")
        for runtime in RUNTIMES:
            original = self.report["runs"]["negative-" + runtime]
            kind = "rerun-" + runtime
            if kind not in self.report["runs"]:
                rerun = self.request(f"/runs/{original['run_id']}/reruns",
                                    {"idempotency_key": uuid.uuid4().hex}, expected=202)["run_id"]
                self.created.append(rerun)
                self.report["runs"][kind] = {**original, "run_id": rerun}
            rerun = self.report["runs"][kind]["run_id"]
            self.verify_snapshot(rerun, browser)
            self.verify("rerun-" + runtime, expected="failed")
        self.report["checks"]["snapshot_after_edit_delete_rerun"] = "passed"
        self.langfuse()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--env-file", type=Path, action="append", default=[])
    parser.add_argument("--timeout", type=float, default=360)
    parser.add_argument("--report", type=Path, default=Path(tempfile.gettempdir()) / "composition-acceptance.json")
    parser.add_argument("--resume-report", type=Path, help="reuse previously created acceptance Runs")
    parser.add_argument("--refresh-runtime", choices=RUNTIMES, action="append", default=[],
                        help="create fresh executions for an adapter changed after the saved report")
    args = parser.parse_args()
    env = load_environment([ROOT / ".env", *args.env_file])
    env.setdefault("AGENTTRACE_E2E_POSTGRES_DSN", "postgresql://127.0.0.1:5432/agenttrace_local?sslmode=disable")
    runner = Composition(env, args.timeout)
    try:
        require(not args.refresh_runtime or args.resume_report, "--refresh-runtime requires --resume-report")
        runner.run(args.resume_report, args.refresh_runtime)
        runner.report["status"] = "passed"
        result = 0
    except (CheckFailure, OSError, ValueError, KeyError) as error:
        runner.report.update(status="failed", error=str(error) if isinstance(error, CheckFailure) else type(error).__name__)
        result = 1
    finally:
        runner.cancel_remaining()
        args.report.parent.mkdir(parents=True, exist_ok=True)
        args.report.write_text(json.dumps(runner.report, indent=2) + "\n")
    print(f"Composition acceptance: {runner.report['status']}; report: {args.report}", flush=True)
    if "error" in runner.report:
        print(runner.report["error"], flush=True)
    return result


if __name__ == "__main__":
    raise SystemExit(main())
