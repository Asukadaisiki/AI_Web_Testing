#!/usr/bin/env python3
"""Native acceptance against running services; requires psql and redis-cli.

Credentials come from the ignored root .env, --env-file, or the environment
(environment wins). Set AGENTTRACE_E2E_POSTGRES_DSN to the native database.
Defaults: API 8180, fixture 8124, Redis DB 3, Langfuse 3300.
Creates and retains an isolated acceptance project, never alters existing runs.
Reports contain IDs, counts and check results only, not payloads or credentials.
"""

from __future__ import annotations

import argparse
import base64
from datetime import datetime
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import subprocess
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid

ROOT = Path(__file__).resolve().parents[2]
TERMINAL = {"succeeded", "failed", "cancelled"}
MODEL = {"provider": "openai-compatible", "name": "glm-5.3-flash"}
PG_ENV = {
    "host": "PGHOST", "hostaddr": "PGHOSTADDR", "port": "PGPORT",
    "user": "PGUSER", "password": "PGPASSWORD", "dbname": "PGDATABASE",
    "sslmode": "PGSSLMODE", "sslcert": "PGSSLCERT", "sslkey": "PGSSLKEY",
    "sslrootcert": "PGSSLROOTCERT", "sslcrl": "PGSSLCRL", "sslcrldir": "PGSSLCRLDIR",
    "ssl_min_protocol_version": "PGSSLMINPROTOCOLVERSION",
    "ssl_max_protocol_version": "PGSSLMAXPROTOCOLVERSION",
    "connect_timeout": "PGCONNECT_TIMEOUT", "options": "PGOPTIONS",
    "application_name": "PGAPPNAME", "client_encoding": "PGCLIENTENCODING",
    "target_session_attrs": "PGTARGETSESSIONATTRS", "channel_binding": "PGCHANNELBINDING",
    "gssencmode": "PGGSSENCMODE", "krbsrvname": "PGKRBSRVNAME",
    "passfile": "PGPASSFILE", "service": "PGSERVICE",
}


class CheckFailure(RuntimeError):
    pass


def require(condition: bool, message: str) -> None:
    if not condition:
        raise CheckFailure(message)


def load_environment(files: list[Path]) -> dict[str, str]:
    values: dict[str, str] = {}
    for path in files:
        if not path.exists():
            continue
        for line in path.read_text().splitlines():
            parts = shlex.split(line, comments=True)
            if not parts:
                continue
            assignment = " ".join(parts[1:] if parts[0] == "export" else parts)
            key, separator, value = assignment.partition("=")
            if separator and re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", key):
                values[key] = value
    values.update(os.environ)
    return values


def sql_id(value: str, prefix: str = "run") -> str:
    require(bool(re.fullmatch(prefix + r"_[A-Za-z0-9]+", value)), "invalid scoped record ID")
    return "'" + value + "'"


def pg_environment(dsn: str, base: dict[str, str]) -> dict[str, str]:
    parameters = {}
    if dsn.startswith(("postgres://", "postgresql://")):
        uri = urllib.parse.urlsplit(dsn)
        require(not uri.fragment, "PostgreSQL DSN fragments are unsupported")
        for key, value in (("host", uri.hostname), ("port", uri.port),
                           ("user", uri.username), ("password", uri.password),
                           ("dbname", uri.path.removeprefix("/"))):
            if value is not None and value != "":
                parameters[key] = urllib.parse.unquote(str(value))
        parameters.update(urllib.parse.parse_qsl(uri.query, keep_blank_values=True))
    else:
        for part in shlex.split(dsn):
            key, separator, value = part.partition("=")
            require(bool(separator), "PostgreSQL DSN must be a URI or libpq key=value string")
            parameters[key] = value
    require(bool(parameters), "PostgreSQL connection parameters missing")
    require(all(key in PG_ENV for key in parameters), "unsupported PostgreSQL connection option")
    child = dict(base)
    child.update({PG_ENV[key]: value for key, value in parameters.items()})
    return child


def parse_sse(lines):
    event: dict = {}
    data: list[str] = []
    for raw in lines:
        line = raw.decode("utf-8").rstrip("\r\n") if isinstance(raw, bytes) else raw.rstrip("\r\n")
        if not line:
            if data:
                event["data"] = json.loads("\n".join(data))
                yield event
            event, data = {}, []
        elif not line.startswith(":"):
            name, _, value = line.partition(":")
            value = value.removeprefix(" ")
            if name == "data":
                data.append(value)
            elif name in {"id", "event"}:
                event[name] = value


def timestamp(value: str) -> datetime:
    # Python 3.9 accepts only three or six fractional digits in fromisoformat.
    value = re.sub(r"\.(\d+)(?=Z|[+-]\d{2}:\d{2}$)",
                   lambda match: "." + match[1][:6].ljust(6, "0"), value)
    parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    require(parsed.tzinfo is not None, "timestamp is missing a timezone")
    return parsed


def model_evidence(spans: list[dict]) -> dict:
    models = [span for span in spans if span["kind"] == "model"]
    require(bool(models), "missing completed model spans")
    durations = []
    for span in models:
        require(bool(span.get("started_at") and span.get("ended_at")), "model call timing missing")
        duration = (timestamp(span["ended_at"]) - timestamp(span["started_at"])).total_seconds()
        require(duration > 0, "model call duration must be positive")
        context = span.get("input")
        require(isinstance(context, dict) and bool(context.get("messages")),
                "model call input messages missing")
        durations.append(duration)
    return {"calls": len(models), "min_duration_seconds": min(durations),
            "max_duration_seconds": max(durations), "inputs_captured": True}


class Acceptance:
    def __init__(self, env: dict[str, str], timeout: float):
        self.env = env
        self.timeout = timeout
        self.api = env.get("AGENTTRACE_API_URL", "http://127.0.0.1:8180").rstrip("/") + "/api/v1"
        self.fixture = env.get("AGENTTRACE_E2E_GOAL_URL", "http://127.0.0.1:8124/index.html")
        self.dsn = env.get("AGENTTRACE_E2E_POSTGRES_DSN", "")
        self.report: dict = {"model": MODEL, "checks": {}, "runs": {}}
        self.created: list[str] = []

    def http(self, url: str, body=None, headers=None, expected: int = 200):
        request = urllib.request.Request(
            url, data=None if body is None else json.dumps(body).encode(),
            headers={"Content-Type": "application/json", **(headers or {})},
        )
        try:
            with urllib.request.urlopen(request, timeout=15) as response:
                require(response.status == expected, f"HTTP expected {expected}, got {response.status}")
                return json.load(response)
        except urllib.error.HTTPError as error:
            raise CheckFailure(f"HTTP expected {expected}, got {error.code}") from None
        except (OSError, ValueError):
            raise CheckFailure("HTTP request unavailable or invalid JSON") from None

    def sql(self, query: str):
        result = subprocess.run(
            ["psql", "--no-psqlrc", "-Atq", "-v", "ON_ERROR_STOP=1", "-c", query],
            env=pg_environment(self.dsn, self.env), capture_output=True, text=True, timeout=15,
        )
        require(result.returncode == 0, "native PostgreSQL query failed (diagnostics suppressed)")
        return json.loads(result.stdout)

    def redis(self, *args: str):
        host, separator, port = self.env.get("AGENTTRACE_REDIS_ADDR", "127.0.0.1:6379").rpartition(":")
        require(bool(separator and host and port.isdigit()), "invalid native Redis address")
        env = dict(self.env)
        if env.get("AGENTTRACE_REDIS_PASSWORD"):
            env["REDISCLI_AUTH"] = env["AGENTTRACE_REDIS_PASSWORD"]
        result = subprocess.run(
            ["redis-cli", "-h", host.strip("[]"), "-p", port, "-n",
             env.get("AGENTTRACE_REDIS_DB", "3"), "--json", *args],
            env=env, capture_output=True, text=True, timeout=15,
        )
        require(result.returncode == 0, "native Redis query failed (diagnostics suppressed)")
        try:
            return json.loads(result.stdout)
        except ValueError:
            raise CheckFailure("invalid native Redis response") from None

    def poll(self, callback, description: str):
        deadline = time.monotonic() + self.timeout
        while True:
            result = callback()
            if result:
                return result
            require(time.monotonic() < deadline, f"timed out: {description}")
            time.sleep(0.5)

    def preflight(self):
        require(bool(self.dsn), "AGENTTRACE_E2E_POSTGRES_DSN is required")
        require(bool(shutil.which("psql") and shutil.which("redis-cli")), "psql and redis-cli are required")
        require(self.sql("SELECT to_json(1);") == 1, "PostgreSQL probe failed")
        require(self.redis("PING") == "PONG", "Redis probe failed")
        health = self.http(self.api + "/health")
        require(health["status"] == "ok", "native API dependencies are not healthy")
        agents = self.http(self.api + "/agents")["agents"]
        matching = [a for a in agents if all(a["model"].get(k) == v for k, v in MODEL.items())]
        require(bool(matching), "GET /agents must advertise openai-compatible glm-5.3-flash")
        self.agent_id = matching[0]["id"]
        self.request_model = matching[0]["model"]
        self.lf_url = self.env.get("AGENTTRACE_LANGFUSE_BASE_URL",
                                  self.env.get("LANGFUSE_HOST", "http://127.0.0.1:3300")).rstrip("/")
        public = self.env.get("AGENTTRACE_LANGFUSE_PUBLIC_KEY") or self.env.get("LANGFUSE_PUBLIC_KEY")
        secret = self.env.get("AGENTTRACE_LANGFUSE_SECRET_KEY") or self.env.get("LANGFUSE_SECRET_KEY")
        require(bool(public and secret), "Langfuse public/secret keys are required; use --env-file")
        self.lf_headers = {"Authorization": "Basic " + base64.b64encode(f"{public}:{secret}".encode()).decode()}
        self.http(self.lf_url + "/api/public/traces?limit=1", headers=self.lf_headers)
        with urllib.request.urlopen(self.fixture, timeout=10) as response:
            require(response.status == 200, "native fixture unavailable")
        self.report["checks"]["preflight"] = "passed"

    def create_run(self, kind: str, goal: str) -> str:
        conversation = self.http(
            self.api + f"/projects/{self.project_id}/conversations",
            {"title": f"Native acceptance {kind}", "metadata": {"acceptance": True}}, expected=201,
        )
        body = {"content": goal, "agent_id": self.agent_id, "model": self.request_model,
                "idempotency_key": "acceptance-" + uuid.uuid4().hex}
        response = self.http(self.api + f"/conversations/{conversation['id']}/messages",
                             body, expected=202)
        run_id = response["run_id"]
        sql_id(run_id)
        self.created.append(run_id)
        self.report["runs"][kind] = {"run_id": run_id, "conversation_id": conversation["id"]}
        duplicate = self.http(self.api + f"/conversations/{conversation['id']}/messages",
                              body, expected=202)
        require(duplicate["run_id"] == run_id and duplicate["message_id"] == response["message_id"],
                "idempotent message submission duplicated the run")
        return run_id

    def wait_terminal(self, run_id: str, expected: str):
        def finished():
            run = self.http(self.api + f"/runs/{run_id}")
            return run if run["status"] in TERMINAL else None
        run = self.poll(finished, f"{expected} run completion")
        require(run["status"] == expected, f"expected {expected}, got {run['status']}")
        require(bool(run.get("finished_at")), "terminal run has no finish timestamp")
        return run

    def counts(self, run_id: str):
        rid = sql_id(run_id)
        return self.sql(f"""SELECT json_build_object(
            'messages', (SELECT count(*) FROM messages WHERE run_id={rid} AND role='user'),
            'spans', (SELECT count(*) FROM spans WHERE run_id={rid}),
            'attempts', (SELECT count(*) FROM span_attempts WHERE span_id IN (SELECT id FROM spans WHERE run_id={rid})),
            'usage', (SELECT count(*) FROM usage_records WHERE run_id={rid}),
            'operations', (SELECT count(*) FROM operation_results WHERE run_id={rid}),
            'events', (SELECT count(*) FROM run_events WHERE run_id={rid}),
            'completed', (SELECT count(*) FROM run_events WHERE run_id={rid} AND event_type='run.completed'),
            'outbox', (SELECT count(*) FROM outbox_events WHERE key={rid}),
            'pending_outbox', (SELECT count(*) FROM outbox_events WHERE key={rid} AND published_at IS NULL),
            'commands', (SELECT count(*) FROM outbox_events WHERE key={rid} AND topic='agenttrace.run.commands.v1'),
            'processed_commands', (SELECT count(*) FROM outbox_events o JOIN inbox_events i ON i.event_id=o.event_id
                WHERE o.key={rid} AND o.topic='agenttrace.run.commands.v1'
                  AND i.consumer='agenttrace-runtime' AND i.processed_at IS NOT NULL));""")

    def verify_persistence(self, kind: str, run_id: str):
        counts = self.poll(
            lambda: (c if c["pending_outbox"] == 0 and c["processed_commands"] == c["commands"] else None)
            if (c := self.counts(run_id)) else None, "outbox publication and command processing",
        )
        require(counts["messages"] == 1 and counts["commands"] == 1, "duplicate message/command")
        require(counts["completed"] == 1, "run must have exactly one durable completion")
        require(counts["spans"] > 0 and counts["events"] > 1 and counts["outbox"] > 1,
                "missing durable execution records")
        if kind == "success":
            require(all(counts[key] > 0 for key in ("attempts", "usage", "operations")),
                    "successful real-model execution lacks evidence/usage")
        trace = self.http(self.api + f"/runs/{run_id}/trace")
        for api_key, count_key in (("spans", "spans"), ("attempts", "attempts"),
                                   ("usage", "usage"), ("operation_evidence", "operations")):
            require(len(trace[api_key]) == counts[count_key], "API trace differs from PostgreSQL counts")
        pg_usage = self.sql(f"""SELECT coalesce(json_agg(json_build_object(
            'id', id, 'input_tokens', input_tokens, 'output_tokens', output_tokens,
            'total_tokens', total_tokens, 'cost_micros', cost_micros)), '[]'::json)
            FROM usage_records WHERE run_id={sql_id(run_id)};""")
        api_usage = {record["id"]: record for record in trace["usage"]}
        require(all(record["id"] in api_usage and
                    all(api_usage[record["id"]].get(key) == value for key, value in record.items())
                    for record in pg_usage), "API token usage differs from PostgreSQL")
        if kind == "success":
            require(any(u.get("total_tokens", 0) > 0 for u in trace["usage"]),
                    "missing real-model token usage")
            require(all(not u.get("model") or u["model"] == MODEL["name"] for u in trace["usage"]),
                    "usage names a different model")
            self.report["runs"][kind]["usage_model_attributed"] = all(
                u.get("model") == MODEL["name"] for u in trace["usage"])
            request_model = self.sql(f"""SELECT specification #> '{{command,model}}'
                FROM runs WHERE id={sql_id(run_id)};""")
            require(all(request_model.get(key) == value for key, value in MODEL.items()),
                    "persisted request did not use exact GLM model/provider")
            self.report["runs"][kind]["model_evidence"] = model_evidence(trace["spans"])
            require(any(s["name"].endswith("finish_case") and s["status"] == "succeeded"
                        for s in trace["spans"]), "minimal case dry run did not succeed")
            paths = [path for evidence in trace["operation_evidence"]
                     for path in (evidence.get("artifacts") or {}).get("screenshot_paths", [])]
            require(bool(paths), "successful browser case has no screenshot artifacts")
            for path in paths:
                require(path.startswith(run_id + "/") and ".." not in Path(path).parts,
                        "artifact is outside its run scope")
                url = self.api.removesuffix("/api/v1") + "/artifacts/" + urllib.parse.quote(path)
                with urllib.request.urlopen(url, timeout=15) as response:
                    require(response.status == 200 and response.read(8) == b"\x89PNG\r\n\x1a\n",
                            "browser screenshot cannot be downloaded as PNG")
            self.report["runs"][kind]["artifacts_downloaded"] = len(paths)
        if kind == "failure":
            require(any(e["status"] == "failed" and
                        (e.get("error") or {}).get("error") == "condition_unmet"
                        for e in trace["operation_evidence"]),
                    "negative run did not record the expected failed text assertion")
        key = f"run:{run_id}:live"
        entries = self.redis("XRANGE", key, "-", "+")
        require(bool(entries), "missing Redis live stream")
        ids = [tuple(map(int, entry[0].split("-"))) for entry in entries]
        require(all(a < b for a, b in zip(ids, ids[1:])), "Redis stream IDs are not strictly ordered")
        for _, fields in entries:
            values = dict(zip(fields[::2], fields[1::2]))
            event = json.loads(values["event"])
            require(bool(event.get("type")), "Redis live event missing type")
        ttl = self.redis("TTL", key)
        require(0 < ttl <= 86400, "Redis live stream does not expire within 24 hours")
        self.report["runs"][kind].update(counts=counts, redis_entries=len(entries), redis_ttl=ttl)
        self.report["checks"][kind + "_persistence"] = "passed"

    def verify_sse(self, kind: str, run_id: str):
        durable = self.sql(f"""SELECT coalesce(json_agg(json_build_object(
            'id', seq::text, 'event', event_type, 'data', payload) ORDER BY seq), '[]'::json)
            FROM run_events WHERE run_id={sql_id(run_id)};""")
        require(len(durable) >= 2, "insufficient durable events to verify reconnect")
        sequences = [int(e["id"]) for e in durable]
        require(sequences == list(range(1, len(durable) + 1)), "durable events contain sequence gaps")

        def read(cursor: int, count: int):
            request = urllib.request.Request(self.api + f"/runs/{run_id}/events",
                                             headers={"Last-Event-ID": str(cursor)})
            result = []
            with urllib.request.urlopen(request, timeout=15) as response:
                require(response.headers.get_content_type() == "text/event-stream", "not an SSE response")
                for event in parse_sse(response):
                    require(event.get("event") != "error", "SSE reported an error")
                    if "id" not in event:
                        require(event.get("event") == "token.delta", "unexpected unsequenced SSE event")
                        continue
                    result.append(event)
                    if len(result) == count:
                        break
            require(len(result) == count, "SSE ended before durable replay completed")
            return result

        split = max(1, len(durable) // 2)
        first = read(0, split)
        second = read(int(first[-1]["id"]), len(durable) - split)
        require(first + second == durable, "ordered SSE reconnect differs from canonical PostgreSQL replay")
        self.report["runs"][kind]["sse_events"] = len(durable)
        self.report["checks"][kind + "_sse_reconnect"] = "passed"

    def verify_langfuse(self, kind: str, run_id: str):
        trace_id = "agenttrace-trace-" + run_id
        canonical = self.http(self.api + f"/runs/{run_id}/trace")
        model_spans = [s for s in canonical["spans"] if s["kind"] == "model"]
        goal = self.sql(f"""SELECT to_json(specification #>> '{{command,messages,-1,content}}')
            FROM runs WHERE id={sql_id(run_id)};""")
        def exported():
            try:
                trace = self.http(self.lf_url + "/api/public/traces/" +
                                  urllib.parse.quote(trace_id, safe=""), headers=self.lf_headers)
            except CheckFailure as error:
                if str(error) == "HTTP expected 200, got 404":
                    return None
                raise
            metadata = trace.get("metadata") or {}
            status = {"success": "succeeded", "failure": "failed", "active_cancel": "cancelled"}[kind]
            if metadata.get("agenttrace_status") != status or not metadata.get("agenttrace_end_time"):
                return None
            require(metadata.get("agenttrace_run_id") == run_id, "Langfuse run correlation mismatch")
            require(trace.get("sessionId") ==
                    "agenttrace-session-" + self.report["runs"][kind]["conversation_id"],
                    "Langfuse session correlation mismatch")
            require(trace.get("input") == {"goal": goal}, "Langfuse trace input differs from current goal")
            observations = trace.get("observations") or []
            if len(observations) < len(canonical["spans"]):
                return None
            return trace if observations else None
        trace = self.poll(exported, "terminal Langfuse trace and observations")
        if kind == "success":
            generations = [o for o in trace["observations"] if o.get("type") == "GENERATION"]
            require(len(generations) == len(model_spans), "Langfuse model generation count differs from PG")
            generations_by_id = {generation["id"]: generation for generation in generations}
            for span in model_spans:
                generation = generations_by_id.get("agenttrace-observation-" + span["id"])
                require(generation is not None, "Langfuse model span correlation missing")
                require(generation.get("model") == MODEL["name"], "Langfuse GENERATION.model is not exact GLM")
                require(generation.get("input") == span["input"], "Langfuse model input differs from PG/API")
                require(bool(generation.get("startTime") and generation.get("endTime")),
                        "Langfuse model timing missing")
                require(timestamp(generation["endTime"]) > timestamp(generation["startTime"]),
                        "Langfuse model duration is not positive")
                usage = [u for u in canonical["usage"] if u.get("span_id") == span["id"]]
                expected = {target: sum(u[source] for u in usage) for target, source in
                            (("input", "input_tokens"), ("output", "output_tokens"), ("total", "total_tokens"))}
                actual = generation.get("usageDetails") or generation.get("usage") or {}
                require(all(actual.get(key) == value for key, value in expected.items()),
                        "Langfuse generation usage differs from PG/API")
        self.report["runs"][kind]["langfuse"] = {
            "trace_id": trace_id, "observations": len(trace["observations"]),
            "status": trace["metadata"]["agenttrace_status"],
            "current_input_verified": True,
        }
        if kind == "success":
            self.report["runs"][kind]["langfuse"].update(
                generation_model=MODEL["name"], generation_usage_verified=True,
                generation_inputs_verified=True,
            )
        self.report["checks"][kind + "_langfuse"] = "passed"

    def run(self):
        self.preflight()
        project = self.http(self.api + "/projects", {
            "name": "Native acceptance " + uuid.uuid4().hex,
            "metadata": {"acceptance": True},
        }, expected=201)
        self.project_id = project["id"]
        self.report["project_id"] = self.project_id
        success = self.create_run("success", (
            f"Open {self.fixture}, verify Alpha is visible using assert_text, then use finish_case "
            "to create exactly one minimal case and pass its fresh-context dry run."
        ))
        failure = self.create_run("failure", (
            f"Open {self.fixture}. Assert that the literal text "
            f"ACCEPTANCE_MISSING_{uuid.uuid4().hex} is visible using assert_text. "
            "This is an intentional negative test. Call assert_text exactly once. When it returns "
            "false, immediately stop all browser actions and respond 'Expected negative assertion "
            "failed'. Do not repair the assertion, do not retry, do not call finish_case, "
            "and do not report success."
        ))
        cancelled = self.create_run("active_cancel", (
            f"Open {self.fixture}, then inspect Alpha, Beta and Gamma in separate browser actions. "
            "Do not finish until every item has been inspected."
        ))
        def active():
            run = self.http(self.api + f"/runs/{cancelled}")
            require(run["status"] not in TERMINAL, "cancel run finished before active cancellation")
            events = self.sql(f"""SELECT json_build_object('started', count(*))
                FROM run_events WHERE run_id={sql_id(cancelled)} AND event_type='harness.started';""")
            return run if run["status"] == "running" and run.get("started_at") and events["started"] else None
        self.poll(active, "active harness before cancellation")
        self.http(self.api + f"/runs/{cancelled}/cancel", {}, expected=202)
        self.report["checks"]["cancel_requested_while_active"] = "passed"
        failures = []
        for kind, run_id, status in (("success", success, "succeeded"), ("failure", failure, "failed"),
                                     ("active_cancel", cancelled, "cancelled")):
            print(f"Checking {kind}: {run_id}", flush=True)
            try:
                run = self.wait_terminal(run_id, status)
                if kind == "active_cancel":
                    require(bool(run.get("cancel_requested_at")), "missing durable cancellation request")
                self.report["runs"][kind]["status"] = run["status"]
                self.verify_persistence(kind, run_id)
                self.verify_sse(kind, run_id)
                self.verify_langfuse(kind, run_id)
            except (CheckFailure, OSError, ValueError, KeyError, subprocess.SubprocessError) as error:
                message = str(error) if isinstance(error, CheckFailure) else type(error).__name__
                self.report["runs"][kind]["error"] = message
                failures.append(f"{kind}: {message}")
        require(not failures, "; ".join(failures))

    def cancel_remaining(self):
        for run_id in self.created:
            try:
                run = self.http(self.api + f"/runs/{run_id}")
                if run["status"] not in TERMINAL:
                    self.http(self.api + f"/runs/{run_id}/cancel", {}, expected=202)
                    self.poll(lambda: self.http(self.api + f"/runs/{run_id}")["status"] in TERMINAL,
                              "cleanup cancellation")
            except (CheckFailure, OSError):
                self.report.setdefault("cleanup_incomplete", []).append(run_id)


def self_test():
    import unittest

    class AdapterTests(unittest.TestCase):
        def test_sse_multiline_and_ephemeral(self):
            events = list(parse_sse([
                b": heartbeat\n", b"\n", b"id: 1\n", b"event: run.completed\n",
                b'data: {"status":\n', b'data: "succeeded"}\n', b"\n",
                b"event: token.delta\n", b'data: {"text":"ok"}\n', b"\n",
            ]))
            self.assertEqual(events, [
                {"id": "1", "event": "run.completed", "data": {"status": "succeeded"}},
                {"event": "token.delta", "data": {"text": "ok"}},
            ])

        def test_ids_reject_sql_injection(self):
            self.assertEqual(sql_id("run_abc123"), "'run_abc123'")
            with self.assertRaises(CheckFailure):
                sql_id("run_abc'; DELETE FROM runs;--")

        def test_explicit_environment_wins(self):
            self.assertEqual(load_environment([]), dict(os.environ))

        def test_model_evidence_rejects_old_zero_duration(self):
            span = {"kind": "model", "started_at": "2026-10-28T00:00:00Z",
                    "ended_at": "2026-10-28T00:00:01Z",
                    "input": {"messages": [{"role": "user"}], "systemPrompt": "test",
                              "tools": [{"name": "assert_text"}]}}
            self.assertEqual(model_evidence([span])["min_duration_seconds"], 1)
            with self.assertRaises(CheckFailure):
                model_evidence([{**span, "ended_at": span["started_at"]}])
            with self.assertRaises(CheckFailure):
                model_evidence([{**span, "input": None}])

        def test_rfc3339_fractional_precision_on_python39(self):
            self.assertEqual(timestamp("2026-10-28T12:51:49.64+08:00").microsecond, 640000)
            self.assertEqual(timestamp("2026-10-28T04:51:49.123456789Z").microsecond, 123456)
            self.assertEqual(timestamp("2026-10-28T04:51:49Z").microsecond, 0)

        def test_pg_uri_credentials_and_ssl_stay_in_child_environment(self):
            base = {"PGHOST": "old", "OTHER": "keep"}
            child = pg_environment(
                "postgresql://test:p%40ss@127.0.0.1:5433/native?sslmode=verify-full&sslrootcert=%2Ftmp%2Fca.pem",
                base,
            )
            self.assertEqual(child["PGPASSWORD"], "p@ss")
            self.assertEqual(child["PGHOST"], "127.0.0.1")
            self.assertEqual(child["PGPORT"], "5433")
            self.assertEqual(child["PGDATABASE"], "native")
            self.assertEqual(child["PGSSLMODE"], "verify-full")
            self.assertEqual(child["PGSSLROOTCERT"], "/tmp/ca.pem")
            self.assertEqual(base, {"PGHOST": "old", "OTHER": "keep"})

        def test_pg_libpq_options_and_unsupported_parameters(self):
            child = pg_environment("host=localhost dbname=native password='test value' sslmode=disable", {})
            self.assertEqual(child["PGPASSWORD"], "test value")
            self.assertEqual(child["PGSSLMODE"], "disable")
            with self.assertRaises(CheckFailure):
                pg_environment("postgresql:///native?unsupported=value", {})

    result = unittest.TextTestRunner().run(unittest.defaultTestLoader.loadTestsFromTestCase(AdapterTests))
    return 0 if result.wasSuccessful() else 1


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--env-file", type=Path, action="append", default=[])
    parser.add_argument("--timeout", type=float, default=300, help="seconds per bounded wait")
    parser.add_argument("--report", type=Path, help="sanitized JSON report; defaults outside the workspace")
    parser.add_argument("--self-test", action="store_true", help="offline adapter checks")
    args = parser.parse_args()
    if args.self_test:
        return self_test()
    if args.timeout <= 0:
        parser.error("--timeout must be positive")
    env = load_environment([ROOT / ".env"])
    env.update({
        "AGENTTRACE_API_URL": "http://127.0.0.1:8180",
        "AGENTTRACE_REDIS_ADDR": "127.0.0.1:6379",
        "AGENTTRACE_REDIS_DB": "3",
        "AGENTTRACE_LANGFUSE_BASE_URL": "http://127.0.0.1:3300",
    })
    env.update(load_environment(args.env_file))
    runner = Acceptance(env, args.timeout)
    report_path = args.report or Path(tempfile.mkdtemp(prefix="agenttrace-native-acceptance-")) / "acceptance.json"
    exit_code = 0
    try:
        runner.run()
        runner.report["status"] = "passed"
    except (CheckFailure, OSError, ValueError, KeyError, subprocess.SubprocessError) as error:
        runner.report["status"] = "failed"
        runner.report["error"] = str(error) if isinstance(error, CheckFailure) else type(error).__name__
        exit_code = 1
    except KeyboardInterrupt:
        runner.report["status"] = "interrupted"
        exit_code = 130
    finally:
        runner.cancel_remaining()
        report_path.parent.mkdir(parents=True, exist_ok=True)
        with report_path.open("w") as output:
            os.chmod(report_path, 0o600)
            json.dump(runner.report, output, indent=2)
            output.write("\n")
    print(f"Native acceptance: {runner.report['status']}; sanitized report: {report_path}")
    if "error" in runner.report:
        print(runner.report["error"])
    return exit_code


if __name__ == "__main__":
    raise SystemExit(main())
