#!/usr/bin/env python3
"""Management Phases 0-4 acceptance against already-running native services.

All Control API requests carry server-authenticated project/actor headers.
SQL is read-only, scoped evidence; no service is started/stopped by acceptance.
The optional `fixture` command serves a localStorage login fixture separately.
"""
from __future__ import annotations

import argparse
import base64
from contextlib import contextmanager
import hashlib
import http.client
from http.server import BaseHTTPRequestHandler, HTTPServer
import json
import os
from pathlib import Path
import socket
import tempfile
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid

from support import AcceptanceSupport, CheckFailure, load_environment, parse_sse, require, ROOT, TERMINAL, sql_id

RUNTIMES = ("pi", "claude-code", "codex")
PNG = b"\x89PNG\r\n\x1a\n"
PUBLIC_SECRET_FIELDS = {"authorization", "cookie", "password", "token", "api_key", "secret"}
IDENTITY_HTML = """<!doctype html>
<html lang="en"><meta charset="utf-8"><title>Management Identity Fixture</title>
<body><h1>Management Identity Fixture</h1><p id="identity"></p>
<form id="login"><label>Email <input id="email" type="email" required></label>
<label>Password <input id="password" type="password" required></label>
<button type="submit">Sign in</button></form><button id="logout">Sign out</button>
<script>
const key="management-acceptance-identity";
function render(){const email=localStorage.getItem(key);
document.getElementById("identity").textContent=email?"Signed in: "+email:"Signed out";
document.getElementById("login").hidden=!!email;}
document.getElementById("login").onsubmit=function(e){e.preventDefault();
localStorage.setItem(key,document.getElementById("email").value);render();};
document.getElementById("logout").onclick=function(){localStorage.removeItem(key);render();};
render();
</script></body></html>"""


def sanitize(value):
    hidden = {"authorization", "token", "api_key", "secret", "password", "cookie",
              "control_url", "provider_session_id", "profile_ref", "secret_refs"}
    if isinstance(value, dict):
        return {k: sanitize(v) for k, v in value.items() if k.lower() not in hidden}
    if isinstance(value, list):
        return [sanitize(item) for item in value]
    return value


def _public_sse_value(value):
    # Mirror runs.go's public projection, not the broader report sanitizer.
    if isinstance(value, dict):
        return {key: _public_sse_value(item) for key, item in value.items()
                if key.strip().lower() not in PUBLIC_SECRET_FIELDS}
    if isinstance(value, list):
        return [_public_sse_value(item) for item in value]
    return value


def _assert_sse_safe(value, secrets):
    if isinstance(value, dict):
        require(not any(key.strip().lower() in PUBLIC_SECRET_FIELDS for key in value),
                "SSE leaked a secret-bearing field")
        for key, item in value.items():
            _assert_sse_safe(key, secrets)
            _assert_sse_safe(item, secrets)
    elif isinstance(value, list):
        for item in value:
            _assert_sse_safe(item, secrets)
    elif isinstance(value, str):
        require(not any(secret in value for secret in secrets), "SSE leaked a forbidden secret value")


def record_id(value, prefix):
    return sql_id(value, prefix)

def has_failed_assertion(trace):
    for span in trace.get("spans", []):
        if not span["name"].endswith("assert_text"):
            continue
        output = span.get("output") or {}
        domain = output.get("structured_result") or (output.get("result") or {}).get("structuredContent") or {}
        if span["status"] == "failed" or domain.get("ok") is False:
            return True
    return False


class NoCredentialRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise CheckFailure("Control API redirects are refused to protect bridge credentials")


def _sse_remaining(deadline):
    budget = deadline - time.monotonic()
    require(budget > 0, "timed out: authenticated SSE replay")
    return budget


@contextmanager
def _sse_socket_deadline(transport, deadline):
    require(isinstance(transport, socket.socket), "unsupported SSE response transport")
    def expire():
        # A line/header/chunk read may contain many timely socket operations.
        try:
            transport.shutdown(socket.SHUT_RDWR)
        except OSError:
            pass
    timer = threading.Timer(_sse_remaining(deadline), expire)
    timer.daemon = True
    timer.start()
    try:
        yield
        _sse_remaining(deadline)
    finally:
        timer.cancel()
        timer.join()


class _SSEDeadlineResponse(http.client.HTTPResponse):
    def __init__(self, *args, deadline, **kwargs):
        super().__init__(*args, **kwargs)
        self.deadline = deadline

    def begin(self):
        with _sse_socket_deadline(self.fp.raw._sock, self.deadline):
            super().begin()


class _SSEHTTPHandler(urllib.request.HTTPHandler, urllib.request.HTTPSHandler):
    def do_open(self, http_class, req, **http_conn_args):
        deadline = getattr(req, "sse_deadline", None)
        if deadline is not None:
            connection_class = http_class
            def http_class(*args, **kwargs):
                connection = connection_class(*args, **kwargs)
                connection.response_class = lambda *a, **kw: _SSEDeadlineResponse(*a, deadline=deadline, **kw)
                return connection
        return super().do_open(http_class, req, **http_conn_args)


class Management(AcceptanceSupport):
    def __init__(self, env, timeout):
        super().__init__(env, timeout)
        self.project_id = env.get("AGENTTRACE_PROJECT_ID", "")
        lf = env.get("AGENTTRACE_LANGFUSE_PROJECT_ID", "")
        token = env.get("AGENTTRACE_BRIDGE_TOKEN", "")
        require(all(value.strip() for value in (self.project_id, lf, token)),
                "project ID, LF project ID and BridgeToken are required")
        self.opener = urllib.request.build_opener(NoCredentialRedirect(), _SSEHTTPHandler())
        self.headers = {"Authorization": "Bearer " + token,
                        "Content-Type": "application/json",
                        "X-Agenttrace-Project-Id": self.project_id,
                        "X-Langfuse-Project-Id": lf,
                        "X-Agenttrace-Actor-Id": env.get("AGENTTRACE_ACCEPTANCE_ACTOR_ID", "management-acceptance"),
                        "X-Agenttrace-Role": env.get("AGENTTRACE_ACCEPTANCE_ROLE", "project_admin")}
        self.report = {"schema": "agenttrace.management-acceptance.v1",
                       "project_id": self.project_id, "langfuse_project_id": lf,
                       "checks": {}, "runs": {}, "conversations": [],
                       "limitations": ["Langfuse browser/RBAC UI acceptance is a separate parent-run check.",
                                       "Horizontal worker failure/expiry requires controlled fault injection; this scenario does not kill services."]}
        self.controls = []
        self.drain_restore = None

    def request(self, path, body=None, method=None, expected=200, binary=False, headers=None):
        url = self.api + path if path.startswith("/") else path
        target, origin = urllib.parse.urlsplit(url), urllib.parse.urlsplit(self.api)
        require((target.scheme, target.netloc) == (origin.scheme, origin.netloc),
                "refuse to forward bridge credentials outside the Control API origin")
        req = urllib.request.Request(url, data=None if body is None else json.dumps(body).encode(),
                                     headers={**self.headers, **(headers or {})}, method=method)
        try:
            with self.opener.open(req, timeout=min(35, self.timeout)) as response:
                require(response.status == expected, "Control API unexpected HTTP status")
                raw = response.read()
                return raw if binary else (json.loads(raw) if raw else None)
        except urllib.error.HTTPError as error:
            if error.code == expected:
                return {"http_status": error.code}
            raise CheckFailure(f"Control API {path.split('?')[0]}: HTTP {error.code}, expected {expected}") from None
        except (OSError, ValueError):
            raise CheckFailure("Control API unavailable or invalid response") from None

    def http(self, url, body=None, headers=None, expected=200):
        return self.request(url, body, expected=expected, headers=headers)

    def check(self, name, callback):
        try:
            evidence = callback()
            self.report["checks"][name] = {"status": "passed", "evidence": sanitize(evidence)}
            print(name + ": passed", flush=True)
            return evidence
        except (CheckFailure, OSError, ValueError, KeyError) as error:
            self.report["checks"][name] = {"status": "failed", "error":
                str(error) if isinstance(error, CheckFailure) else type(error).__name__}
            raise

    def skip(self, name, reason):
        self.report["checks"][name] = {"status": "skipped", "reason": reason}

    def outcome(self):
        statuses = [c["status"] for c in self.report["checks"].values()]
        if "failed" in statuses or self.report.get("cleanup_incomplete"):
            return "failed"
        # UI and fault injection are external to this scenario, so even a
        # complete API pass is never a full product-acceptance claim.
        return "provisional" if self.report["limitations"] or "skipped" in statuses else "passed"

    def select_runtimes(self, catalog):
        selected = []
        by_id = {r["id"]: r for r in catalog}
        for runtime in RUNTIMES:
            candidate = by_id.get(runtime, {})
            if candidate.get("available") and candidate.get("model", {}).get("provider") != "scripted":
                selected.append(runtime)
            else:
                self.skip("sdk-" + runtime, candidate.get("unavailable_reason") or "real SDK unavailable/scripted")
        self.report["catalog"] = sanitize(catalog)
        return selected

    def environment_ready(self, conversation):
        def ready():
            e = self.request(f"/conversations/{conversation}/environment")
            require(e["state"] not in {"broken", "needs_review", "deleting"},
                    "environment is not safely ready: " + e["state"])
            return e if e["state"] == "ready" else None
        return self.poll(ready, "managed Environment provisioning/lease release")

    def create_conversations(self):
        for label in ("a", "b"):
            c = self.request(f"/projects/{self.project_id}/conversations",
                {"title": "Management acceptance " + label + " " + uuid.uuid4().hex[:8],
                 "metadata": {"management_acceptance": True}}, expected=201)
            self.report["conversations"].append({"id": c["id"], "label": label})
        for c in self.report["conversations"]:
            c["environment"] = self.environment_ready(c["id"])
        a, b = self.report["conversations"]
        require(a["id"] != b["id"] and a["environment"]["id"] != b["environment"]["id"],
                "Conversations share browser identity")
        require(all(c["environment"]["fencing_token"] >= 1 for c in (a, b)),
                "initial environment fencing is invalid")
        return self.report["conversations"]

    def publish_acceptance_harness(self):
        base = self.request(f"/projects/{self.project_id}/harnesses")["harnesses"]
        seed = next((h for h in base if h["name"] == "Browser Harness (Builtin)" and not h.get("revoked_at")), None)
        require(seed is not None, "run management seed first")
        self.skill_name = "management-acceptance-" + uuid.uuid4().hex[:12]
        skill = self.upload_skill("Use only the reviewed browser tools. Stop after the minimal requested case.")
        self.skill1 = skill
        self.manifest = {"name": "Management acceptance " + uuid.uuid4().hex[:12],
                         "system_prompt": seed["system_prompt"],
                         "skill_version_ids": seed["skill_version_ids"] + [skill["id"]],
                         "mcp_binding_ids": seed["mcp_binding_ids"], "runtime_ids": list(RUNTIMES)}
        self.harness = self.request(f"/projects/{self.project_id}/harnesses", self.manifest, expected=201)
        return {"harness_version_id": self.harness["id"], "harness_id": self.harness["harness_id"],
                "skill_version_id": skill["id"], "runtime_ids": self.harness["runtime_ids"]}

    def upload_skill(self, instructions):
        v = self.request(f"/projects/{self.project_id}/skills/uploads",
                        {"name": self.skill_name, "files": [{"path": "SKILL.md", "content":
                         "---\nname: " + self.skill_name + "\ndescription: Management acceptance\n---\n" + instructions}]},
                        expected=201)
        return self.request(f"/projects/{self.project_id}/skill-versions/{v['id']}/publish", {})

    def submit(self, name, conversation, runtime, goal, harness=None):
        manifest = harness or self.harness
        body = {"content": goal, "runtime_id": runtime, "harness_id": manifest["id"],
                "idempotency_key": "management-" + uuid.uuid4().hex}
        path = f"/conversations/{conversation}/messages"
        result = self.request(path, body, expected=202)
        self.created.append(result["run_id"])
        self.report["runs"][name] = {"run_id": result["run_id"], "conversation_id": conversation,
                                    "runtime_id": runtime, "harness_version_id": manifest["id"]}
        replay = self.request(path, body, expected=202)
        require(result["run_id"] == replay["run_id"] and result["message_id"] == replay["message_id"],
                "idempotent request duplicated a Run")
        return result["run_id"]

    def wait_run(self, name, expected):
        entry = self.report["runs"][name]
        def terminal():
            run = self.request("/runs/" + entry["run_id"])
            entry["status"] = run["status"]
            return run if run["status"] in TERMINAL else None
        run = self.poll(terminal, name + " terminal acknowledgement")
        require(run["status"] == expected, name + ": expected " + expected + ", got " + run["status"])
        require(bool(run.get("finished_at")), "terminal run has no finish timestamp")
        require(run["conversation_id"] == entry["conversation_id"] and run["runtime_id"] == entry["runtime_id"],
                "Run lost canonical execution identity")
        self.environment_ready(entry["conversation_id"])
        return run

    def snapshot_digest(self, run):
        value = self.sql("SELECT specification #> '{command,execution,harness}' FROM runs WHERE id=" + record_id(run, "run"))
        require(isinstance(value, dict), "missing durable immutable Harness snapshot")
        return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(",", ":")).encode()).hexdigest()

    def verify_persistence(self, run, trace):
        rid, pid = record_id(run, "run"), record_id(self.project_id, "prj")
        query = f"""SELECT json_build_object(
            'spans', (SELECT count(*) FROM spans WHERE run_id=r.id),
            'attempts', (SELECT count(*) FROM span_attempts WHERE span_id IN (SELECT id FROM spans WHERE run_id=r.id)),
            'usage', (SELECT count(*) FROM usage_records WHERE run_id=r.id),
            'operations', (SELECT count(*) FROM operation_results WHERE run_id=r.id),
            'completed', (SELECT count(*) FROM run_events WHERE run_id=r.id AND event_type='run.completed'),
            'published_outbox', (SELECT count(*) FROM outbox_events WHERE key=r.id AND published_at IS NOT NULL),
            'pending_outbox', (SELECT count(*) FROM outbox_events WHERE key=r.id AND published_at IS NULL))
            FROM runs r WHERE id={rid} AND project_id={pid};"""
        counts = self.poll(lambda: (c if c["pending_outbox"] == 0 else None)
                           if (c := self.sql(query)) else None, "scoped outbox publication")
        require(counts["published_outbox"] > 0, "Run has no published scoped outbox")
        require(counts["completed"] == 1, "Run must have exactly one durable completion")
        for api_key, count_key in (("spans", "spans"), ("attempts", "attempts"),
                                   ("usage", "usage"), ("operation_evidence", "operations")):
            require(len(trace[api_key]) == counts[count_key], "API trace differs from scoped PostgreSQL counts")
        return counts

    def verify_sse(self, run, forbidden_values=()):
        deadline = time.monotonic() + self.timeout
        def remaining():
            return _sse_remaining(deadline)
        durable = self.sql(f"""SELECT coalesce(json_agg(json_build_object(
            'id', e.seq::text, 'event', e.event_type, 'data', e.payload) ORDER BY e.seq), '[]'::json)
            FROM run_events e JOIN runs r ON r.id=e.run_id
            WHERE r.id={record_id(run, "run")} AND r.project_id={record_id(self.project_id, "prj")};""")
        require(len(durable) >= 2, "insufficient durable events to verify reconnect")
        require([int(e["id"]) for e in durable] == list(range(1, len(durable) + 1)),
                "durable events contain sequence gaps")
        require(all(isinstance(value, str) for value in forbidden_values), "SSE forbidden values must be strings")
        # Only explicit canaries are forbidden substrings, not inferred payload values.
        secrets = {value for value in forbidden_values if value}
        public = [{**event, "data": _public_sse_value(event["data"])} for event in durable]
        def read(cursor, count):
            req = urllib.request.Request(self.api + f"/runs/{run}/events",
                headers={**self.headers, "Last-Event-ID": str(cursor)})
            req.sse_deadline = deadline
            result = []
            try:
                with self.opener.open(req, timeout=min(15, remaining())) as response:
                    require(response.status == 200 and response.headers.get_content_type() == "text/event-stream",
                            "not an authenticated SSE response")
                    transport = getattr(getattr(getattr(response, "fp", None), "raw", None), "_sock", None)
                    with _sse_socket_deadline(transport, deadline):
                        def checked_lines():
                            while True:
                                transport.settimeout(min(15, remaining()))
                                raw = response.readline()
                                remaining()
                                if not raw:
                                    return
                                _assert_sse_safe(raw.decode("utf-8"), secrets)
                                yield raw
                        for event in parse_sse(checked_lines()):
                            _assert_sse_safe(event, secrets)
                            require(event.get("event") != "error", "SSE reported an error")
                            if "id" not in event:
                                require(event.get("event") == "token.delta", "unexpected unsequenced SSE event")
                                continue
                            result.append(event)
                            if len(result) == count:
                                break
            except urllib.error.HTTPError as error:
                error.close()
                remaining()
                raise CheckFailure("authenticated SSE request failed") from None
            except (OSError, ValueError, http.client.HTTPException):
                remaining()
                raise CheckFailure("authenticated SSE unavailable or invalid response") from None
            require(len(result) == count, "SSE ended before durable replay completed")
            return result
        split = max(1, len(durable) // 2)
        first = read(0, split)
        second = read(int(first[-1]["id"]), len(durable) - split)
        require(first + second == public, "ordered SSE reconnect differs from canonical PostgreSQL replay")
        return {"events": len(durable), "reconnect": True}

    def validate_export(self, run, conversation, status, canonical, exported):
        metadata = exported.get("metadata") or {}
        require(metadata.get("agenttrace_run_id") == run and metadata.get("agenttrace_status") == status
                and bool(metadata.get("agenttrace_end_time")), "Langfuse terminal Run correlation missing")
        require(exported.get("sessionId") == "agenttrace-session-" + conversation,
                "Langfuse Conversation correlation mismatch")
        observations = {o["id"]: o for o in exported.get("observations", [])}
        for span in canonical["spans"]:
            if span["kind"] != "model":
                continue
            generation = observations.get("agenttrace-observation-" + span["id"])
            require(generation is not None and generation.get("type") == "GENERATION",
                    "Langfuse model generation missing")
            if span.get("input") is not None:
                require(generation.get("input") == span["input"], "Langfuse model input differs from canonical trace")
            usage = [u for u in canonical["usage"] if u.get("span_id") == span["span_id"]
                     and not (isinstance(u.get("details"), dict)
                              and u["details"].get("granularity") == "sdk_query_model_cumulative")]
            if usage and any(u.get("total_tokens", 0) > 0 for u in usage):
                actual = generation.get("usageDetails") or generation.get("usage") or {}
                for source, target in (("input_tokens", "input"), ("output_tokens", "output"), ("total_tokens", "total")):
                    require(actual.get(target) == sum(u.get(source, 0) for u in usage),
                            "Langfuse generation usage differs from canonical trace")

    def verify_export(self, name, canonical):
        public = self.env.get("AGENTTRACE_LANGFUSE_PUBLIC_KEY") or self.env.get("LANGFUSE_PUBLIC_KEY")
        secret = self.env.get("AGENTTRACE_LANGFUSE_SECRET_KEY") or self.env.get("LANGFUSE_SECRET_KEY")
        if not (public and secret):
            self.skip(name + "-export", "Private Langfuse export credentials not supplied")
            return None
        base = self.env.get("AGENTTRACE_LANGFUSE_BASE_URL", "http://127.0.0.1:3300").rstrip("/")
        entry = self.report["runs"][name]
        req = urllib.request.Request(base + "/api/public/traces/agenttrace-trace-" + entry["run_id"],
            headers={"Authorization": "Basic " + base64.b64encode(f"{public}:{secret}".encode()).decode()})
        def exported():
            try:
                with self.opener.open(req, timeout=min(15, self.timeout)) as response:
                    result = json.load(response)
            except urllib.error.HTTPError as error:
                if error.code == 404:
                    return None
                raise CheckFailure("Langfuse export request failed") from None
            metadata = result.get("metadata") or {}
            if metadata.get("agenttrace_status") != entry["status"] or not metadata.get("agenttrace_end_time"):
                return None
            if len(result.get("observations", [])) < len(canonical["spans"]):
                return None
            self.validate_export(entry["run_id"], entry["conversation_id"], entry["status"], canonical, result)
            return {"trace_id": "agenttrace-trace-" + entry["run_id"], "observations": len(result["observations"])}
        return self.poll(exported, "real terminal Langfuse export")

    def browser_evidence(self, run, trace, finish_case=True, asserted=None):
        spans = trace.get("spans", [])
        total = sum(u.get("total_tokens", 0) for u in trace.get("usage", []))
        require(any(s["kind"] == "model" for s in spans) and total > 0,
                "real SDK model/usage evidence is missing")
        for tool in (["open_page", "assert_text", "finish_case"] if finish_case else ["open_page", "assert_text"]):
            require(any(s["name"].endswith(tool) and s["status"] == "succeeded" for s in spans),
                    "actual browser tool did not succeed: " + tool)
        if asserted:
            require(any(s["name"].endswith("assert_text") and s["status"] == "succeeded" and
                        (asserted in json.dumps(s.get("input") or {}) or
                         any(step.get("action") == "assert_text" and step.get("value") == asserted
                             for step in ((s.get("output") or {}).get("result") or {}).get("structuredContent", {}).get("steps", [])))
                        for s in spans),
                    "identity assertion was not executed by browser")
        paths = [p for e in trace.get("operation_evidence", [])
                 for p in (e.get("artifacts") or {}).get("screenshot_paths", [])]
        require(bool(paths), "no actual browser screenshot artifact")
        screenshots = []
        for path in paths:
            require(path.startswith(run + "/") and ".." not in Path(path).parts, "cross-Run artifact path")
            image = self.request(self.api.removesuffix("/api/v1") + "/artifacts/" +
                                 urllib.parse.quote(path), binary=True)
            require(image.startswith(PNG), "browser artifact is not PNG")
            screenshots.append({"path": path, "bytes": len(image), "sha256": hashlib.sha256(image).hexdigest()})
        return {"model_spans": sum(s["kind"] == "model" for s in spans),
                "total_tokens": total, "screenshots": screenshots, "asserted_text": asserted}

    def sdk_case(self, runtime, conversation):
        name = "sdk-" + runtime
        rid = self.submit(name, conversation, runtime,
            f"Open {self.fixture}. Verify Alpha is visible with assert_text. "
            "Use finish_case to create exactly one minimal case and pass its fresh-context dry run. Stop.")
        self.wait_run(name, "succeeded")
        trace = self.request("/runs/" + rid + "/trace")
        evidence = self.browser_evidence(rid, trace)
        evidence["persistence"] = self.verify_persistence(rid, trace)
        evidence["sse"] = self.verify_sse(rid)
        evidence["export"] = self.verify_export(name, trace)
        evidence["snapshot_sha256"] = self.snapshot_digest(rid)
        self.report["runs"][name].update(evidence)
        return evidence

    def cancelled_run(self, runtime, conversation):
        rid = self.submit("cancel", conversation, runtime,
            f"Open {self.fixture}. Verify Alpha with assert_text at least 100 separate times. "
            "Do not finish_case. Continue until cancelled.")
        def browser_active():
            run = self.request("/runs/" + rid)
            require(run["status"] not in TERMINAL, "Run finished before active cancellation")
            spans = self.request("/runs/" + rid + "/trace")["spans"]
            return any(s["name"].endswith("open_page") and s["status"] == "succeeded" for s in spans)
        self.poll(browser_active, "real browser action before cancellation")
        self.request("/runs/" + rid + "/cancel", {}, expected=202)
        run = self.wait_run("cancel", "cancelled")
        require(bool(run.get("cancel_requested_at")), "cancelled Run lacks durable cancellation request")
        return {"run_id": rid, "cancel_requested_at": run.get("cancel_requested_at"), "status": run["status"]}

    def snapshot_publish_revoke_rerun(self, runtime, conversation):
        old = dict(self.harness)
        rid = self.submit("negative", conversation, runtime,
            f"Open {self.fixture}. Assert the literal text MANAGEMENT_MISSING_{uuid.uuid4().hex} "
            "with assert_text exactly once. When false stop immediately. Do not retry/repair/finish_case.")
        self.wait_run("negative", "failed")
        negative = self.request("/runs/" + rid + "/trace")
        require(has_failed_assertion(negative),
                "negative Run did not execute the actual failing browser assertion")
        require(sum(u.get("total_tokens", 0) for u in negative["usage"]) > 0,
                "negative Run lacks real SDK usage")
        before = self.snapshot_digest(rid)
        newer = self.upload_skill("Use the reviewed browser tools. Never change a previous immutable snapshot.")
        manifest = {**self.manifest,
                    "skill_version_ids": [v for v in self.manifest["skill_version_ids"] if v != self.skill1["id"]] + [newer["id"]]}
        updated = self.request(f"/projects/{self.project_id}/harnesses/{old['harness_id']}/versions",
                               manifest, expected=201)
        require(updated["version"] > old["version"], "publish did not advance immutable version")
        self.request(f"/projects/{self.project_id}/harness-versions/{old['id']}/revoke", {}, expected=204)
        self.request(f"/projects/{self.project_id}/skill-versions/{self.skill1['id']}/revoke", {}, expected=204)
        require(self.snapshot_digest(rid) == before, "publish/revoke mutated existing Run")
        rerun = self.request("/runs/" + rid + "/reruns", {"idempotency_key": uuid.uuid4().hex}, expected=202)
        new_id = rerun["run_id"]
        self.created.append(new_id)
        self.report["runs"]["rerun"] = {**self.report["runs"]["negative"], "run_id": new_id, "retry_of_run_id": rid}
        require(self.snapshot_digest(new_id) == before, "explicit rerun did not preserve original immutable snapshot")
        self.wait_run("rerun", "failed")
        self.harness = updated
        return {"source_run_id": rid, "rerun_id": new_id, "original_snapshot_sha256": before,
                "new_harness_version_id": updated["id"], "revoked_harness_version_id": old["id"]}

    def takeover(self, conversation):
        e = self.environment_ready(conversation)
        control = self.request(f"/environments/{e['id']}/control-session", {"ttl_seconds": 120}, expected=201)
        cid, token = control["id"], control["token"]
        self.controls.append(cid)
        self.request("/control-sessions/" + cid + "/consume", {"token": token})
        self.request("/control-sessions/" + cid + "/consume", {"token": token}, expected=409)
        self.request("/control-sessions/" + cid + "/input", {"action": "navigate", "url": self.fixture})
        self.request("/control-sessions/" + cid + "/input", {"action": "key", "key": "Tab"})
        image = self.request("/control-sessions/" + cid + "/screenshot", binary=True)
        require(image.startswith(PNG) and len(image) > 100, "takeover did not return actual browser screenshot")
        closed = self.request("/control-sessions/" + cid, method="DELETE")
        self.controls.remove(cid)
        return {"control_session_id": cid, "one_time_replay_rejected": True,
                "screenshot_bytes": len(image), "screenshot_sha256": hashlib.sha256(image).hexdigest(),
                "closed_state": closed["state"]}

    def reset(self, conversation):
        e = self.request(f"/conversations/{conversation}/environment")
        generation = e["profile_generation"]
        self.poll(lambda: self.retry_conflict(f"/environments/{e['id']}/reset", {}), "reset delivery quiescence")
        updated = self.environment_ready(conversation)
        require(updated["profile_generation"] == generation + 1, "reset did not advance profile generation")
        return {"environment_id": updated["id"], "generation_before": generation,
                "generation_after": updated["profile_generation"]}

    def retry_conflict(self, path, body=None, method=None):
        try:
            return self.request(path, body, method=method, expected=202)
        except CheckFailure as error:
            if "HTTP 409," in str(error):
                return None
            raise

    def identity(self, runtimes):
        url = self.env.get("AGENTTRACE_MANAGEMENT_IDENTITY_URL")
        if not url:
            self.skip("login-identity", "Set AGENTTRACE_MANAGEMENT_IDENTITY_URL to the opt-in persistent login fixture")
            return
        a, b = self.report["conversations"]
        runtime = runtimes[0]
        email = "management-a-" + uuid.uuid4().hex[:8] + "@example.test"
        goals = [
            ("identity-login", a["id"], runtime, f"Open {url}. Fill Email with {email} and Password with fixture-only. "
             f"Click Sign in and assert_text 'Signed in: {email}'. Use finish_case for one minimal dry-run case. Stop.",
             "Signed in: " + email),
        ]
        for sdk in runtimes:
            goals.extend([
                ("identity-retained-" + sdk, a["id"], sdk, f"Open {url}. Do not log in. Assert_text 'Signed in: {email}'. "
                 "Use finish_case for a minimal case and stop.", "Signed in: " + email),
                ("identity-isolated-" + sdk, b["id"], sdk, f"Open {url}. Assert_text 'Signed out'. Do not log in. "
                 "Use finish_case for one minimal case and stop.", "Signed out"),
            ])
        for name, conversation, sdk, goal, assertion in goals:
            rid = self.submit(name, conversation, sdk, goal)
            self.wait_run(name, "succeeded")
            self.report["runs"][name].update(self.browser_evidence(rid, self.request("/runs/" + rid + "/trace"),
                                                                   asserted=assertion))
        self.reset(a["id"])
        rid = self.submit("identity-reset", a["id"], runtime, f"Open {url}. Assert_text 'Signed out'. "
                          "Do not log in. Use finish_case for one minimal case and stop.")
        self.wait_run("identity-reset", "succeeded")
        return self.browser_evidence(rid, self.request("/runs/" + rid + "/trace"), asserted="Signed out")

    def drain(self):
        services = self.request(f"/projects/{self.project_id}/services")
        system = self.headers["X-Agenttrace-Role"] == "system_admin"
        candidates = [w for w in services["workers"] if w["alive"] and w["in_use"] == 0 and
                      (w.get("project_id") == self.project_id or system)]
        if not candidates:
            self.skip("worker-drain", "No idle project-dedicated worker; shared worker requires explicit system_admin")
            return
        w = candidates[0]
        self.drain_restore = (w["id"], w["draining"])
        path = f"/projects/{self.project_id}/workers/{w['id']}"
        drained = self.request(path, {"draining": True}, method="PATCH")
        require(drained["draining"] and drained["in_use"] == 0, "drain state acknowledgement missing")
        restored = self.request(path, {"draining": w["draining"]}, method="PATCH")
        self.drain_restore = None
        return {"worker_id": w["id"], "drained_in_use": drained["in_use"], "restored_draining": restored["draining"]}

    def delete_conversations(self):
        deleted = []
        for c in self.report["conversations"]:
            path = "/conversations/" + c["id"]
            self.poll(lambda: self.retry_conflict(path, method="DELETE"), "delete Run/delivery/lease quiescence")
            def removed():
                rows = self.request(f"/projects/{self.project_id}/conversations")["conversations"]
                return all(row["id"] != c["id"] for row in rows)
            self.poll(removed, "environment delete acknowledgement and tombstone")
            evidence = self.sql("SELECT json_build_object('state',state,'messages',(SELECT count(*) FROM messages WHERE conversation_id=c.id),"
                                "'environments',(SELECT count(*) FROM environments WHERE conversation_id=c.id)) FROM conversations c WHERE id=" +
                                record_id(c["id"], "conv"))
            require(evidence["state"] == "deleted" and evidence["messages"] == 0 and evidence["environments"] == 0,
                    "managed deletion did not complete cleanup")
            deleted.append({"conversation_id": c["id"], **evidence})
            removed_runs = {r["run_id"] for r in self.report["runs"].values() if r["conversation_id"] == c["id"]}
            self.created = [rid for rid in self.created if rid not in removed_runs]
        return deleted

    def run(self):
        require(bool(self.dsn), "AGENTTRACE_E2E_POSTGRES_DSN is required for read-only evidence")
        record_id(self.project_id, "prj")
        self.check("management-health", lambda: self.request("/health"))
        self.check("service-authentication-required", lambda: self.request("/runtimes",
            headers={"Authorization": ""}, expected=401))
        selected = self.select_runtimes(self.request("/runtimes")["runtimes"])
        self.check("unsafe-skill-path", lambda: self.request(f"/projects/{self.project_id}/skills/uploads",
            {"name": "unsafe-acceptance", "files": [{"path": "../SKILL.md", "content": "unsafe"}]}, expected=400))
        self.check("cross-project-refusal", lambda: self.request("/projects/prj_outsideacceptance/conversations",
            expected=403))
        self.check("two-independent-conversations", self.create_conversations)
        self.check("immutable-registry-bootstrap", self.publish_acceptance_harness)
        for index, runtime in enumerate(selected):
            try:
                self.check("sdk-" + runtime, lambda r=runtime, i=index: self.sdk_case(r, self.report["conversations"][i % 2]["id"]))
            except CheckFailure:
                # Retain the actual failure, continue independent SDK evidence.
                pass
        if selected:
            first = selected[0]
            a, b = self.report["conversations"]
            self.check("active-cancel", lambda: self.cancelled_run(first, b["id"]))
            self.check("publish-revoke-immutable-rerun", lambda: self.snapshot_publish_revoke_rerun(first, b["id"]))
            self.check("takeover-input-screenshot-stop", lambda: self.takeover(a["id"]))
            self.check("reset-generation", lambda: self.reset(a["id"]))
            if self.env.get("AGENTTRACE_MANAGEMENT_IDENTITY_URL"):
                self.check("login-identity", lambda: self.identity(selected))
            else:
                self.identity(selected)
        else:
            for name in ("active-cancel", "publish-revoke-immutable-rerun", "takeover-input-screenshot-stop", "reset-generation", "login-identity"):
                self.skip(name, "No configured real SDK; cannot prove execution workflow")
        evidence = self.drain()
        if evidence:
            self.report["checks"]["worker-drain"] = {"status": "passed", "evidence": evidence}
        self.check("delete-delivery-lease-drain", self.delete_conversations)

    def cleanup(self):
        for cid in self.controls[:]:
            try:
                self.request("/control-sessions/" + cid, method="DELETE")
                self.controls.remove(cid)
            except CheckFailure:
                self.report.setdefault("cleanup_incomplete", []).append("control-session:" + cid)
        if self.drain_restore:
            wid, draining = self.drain_restore
            try:
                self.request(f"/projects/{self.project_id}/workers/{wid}", {"draining": draining}, method="PATCH")
            except CheckFailure:
                self.report.setdefault("cleanup_incomplete", []).append("worker:" + wid)
        self.cancel_remaining()


def fixture(port):
    class Handler(BaseHTTPRequestHandler):
        def do_GET(self):
            if self.path.split("?")[0] not in ("/", "/management_identity.html"):
                self.send_error(404)
                return
            body = IDENTITY_HTML.encode()
            self.send_response(200)
            self.send_header("Content-Type", "text/html; charset=utf-8")
            self.send_header("Cache-Control", "no-store")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
        def log_message(self, *args):
            pass
    with HTTPServer(("127.0.0.1", port), Handler) as server:
        print(f"Identity fixture: http://127.0.0.1:{port}/management_identity.html", flush=True)
        server.serve_forever()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    serve = commands.add_parser("fixture", help="opt-in persistent login fixture only, no application stack")
    serve.add_argument("--port", type=int, default=8125)
    accept = commands.add_parser("accept", help="use already-running management API/workers")
    accept.add_argument("--env-file", type=Path, action="append", default=[])
    accept.add_argument("--timeout", type=float, default=360)
    accept.add_argument("--report", type=Path, default=Path(tempfile.gettempdir()) / "management-acceptance.json")
    args = parser.parse_args()
    if args.command == "fixture":
        fixture(args.port)
        return 0
    if args.timeout <= 0:
        parser.error("--timeout must be positive")
    env = load_environment([ROOT / ".env", *args.env_file])
    runner = None
    report = {"schema": "agenttrace.management-acceptance.v1", "checks": {}}
    result = 1
    try:
        runner = Management(env, args.timeout)
        runner.run()
        report = runner.report
        result = 0 if runner.outcome() != "failed" else 1
    except (CheckFailure, OSError, ValueError, KeyError) as error:
        report = runner.report if runner else report
        report["error"] = str(error) if isinstance(error, CheckFailure) else type(error).__name__
        report["status"] = "failed"
    except KeyboardInterrupt:
        report = runner.report if runner else report
        report["status"] = "interrupted"
        result = 130
    finally:
        if runner:
            runner.cleanup()
            report.setdefault("status", runner.outcome())
            if report["status"] == "failed":
                result = 1
        args.report.parent.mkdir(parents=True, exist_ok=True)
        with args.report.open("w") as output:
            os.chmod(args.report, 0o600)
            json.dump(sanitize(report), output, indent=2)
            output.write("\n")
    print("Management acceptance: " + report.get("status", "failed") + "; report: " + str(args.report), flush=True)
    return result


if __name__ == "__main__":
    raise SystemExit(main())
