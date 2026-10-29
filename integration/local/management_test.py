"""Offline tests; never start or mutate an application stack."""
import importlib.util
import http.client
import json
from pathlib import Path
import socket
import sys
import threading
import time
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).parent))
spec = importlib.util.spec_from_file_location("management_acceptance", Path(__file__).with_name("management.py"))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


def sse_response(events=(), raw=None, prefix=b""):
    reader, writer = socket.socketpair()
    if raw is None:
        raw = "".join(
            (("id: " + event["id"] + "\n") if "id" in event else "")
            + "event: " + event["event"] + "\ndata: " + json.dumps(event["data"]) + "\n\n"
            for event in events).encode()
    writer.sendall(b"HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\n\r\n" + prefix + raw)
    writer.close()
    response = http.client.HTTPResponse(reader)
    response.begin()
    reader.close()
    return response


class ManagementTests(unittest.TestCase):
    def env(self):
        return {"AGENTTRACE_BRIDGE_TOKEN": "private-bridge",
                "AGENTTRACE_PROJECT_ID": "prj_test",
                "AGENTTRACE_LANGFUSE_PROJECT_ID": "lf-test",
                "AGENTTRACE_API_URL": "http://127.0.0.1:8181",
                "AGENTTRACE_E2E_POSTGRES_DSN": "postgresql:///agenttrace_test_unused"}

    def test_every_request_has_authenticated_project_scope(self):
        runner = module.Management(self.env(), 1)
        class Response:
            status = 200
            def __enter__(self):
                return self
            def __exit__(self, *args):
                pass
            def read(self):
                return b'{"status":"ok"}'
        with patch.object(runner.opener, "open", return_value=Response()) as request:
            runner.request("/health")
        sent = request.call_args[0][0]
        self.assertEqual(sent.full_url, "http://127.0.0.1:8181/api/v1/health")
        for key, value in runner.headers.items():
            self.assertEqual(sent.get_header(key.capitalize()), value)

    def test_missing_binding_fails_before_any_request(self):
        for key in ("AGENTTRACE_PROJECT_ID", "AGENTTRACE_LANGFUSE_PROJECT_ID", "AGENTTRACE_BRIDGE_TOKEN"):
            env = self.env()
            env.pop(key)
            with self.assertRaises(module.CheckFailure):
                module.Management(env, 1)

    def test_blank_binding_is_rejected_before_any_request(self):
        for key in ("AGENTTRACE_PROJECT_ID", "AGENTTRACE_LANGFUSE_PROJECT_ID", "AGENTTRACE_BRIDGE_TOKEN"):
            with self.subTest(key=key):
                env = {**self.env(), key: "   "}
                with self.assertRaises(module.CheckFailure):
                    module.Management(env, 1)

    def test_bridge_credentials_are_not_forwarded_outside_api_origin(self):
        runner = module.Management(self.env(), 1)
        with patch.object(runner.opener, "open") as request:
            with self.assertRaises(module.CheckFailure):
                runner.request("http://untrusted.example/private")
            request.assert_not_called()

    def test_bridge_redirects_fail_closed_before_forwarding_headers(self):
        handler = module.NoCredentialRedirect()
        request = module.urllib.request.Request("http://127.0.0.1:8181/api/v1/health",
                                              headers={"Authorization": "Bearer private"})
        with self.assertRaises(module.CheckFailure):
            handler.redirect_request(request, None, 302, "Found", {},
                                     "http://untrusted.example/private")

    def test_unavailable_or_scripted_sdk_is_not_counted_as_success(self):
        runner = module.Management(self.env(), 1)
        selected = runner.select_runtimes([
            {"id": "pi", "available": True, "model": {"provider": "scripted"}},
            {"id": "claude-code", "available": False, "unavailable_reason": "no credentials"},
            {"id": "codex", "available": True, "model": {"provider": "openai-responses"}},
        ])
        self.assertEqual(selected, ["codex"])
        self.assertEqual(runner.report["checks"]["sdk-pi"]["status"], "skipped")
        self.assertEqual(runner.report["checks"]["sdk-claude-code"]["status"], "skipped")
        self.assertEqual(runner.outcome(), "provisional")

    def test_browser_evidence_requires_real_tools_tokens_and_png(self):
        runner = module.Management(self.env(), 1)
        trace = {"spans": [{"kind": "model", "name": "completion", "status": "succeeded"}, {"kind": "tool", "name": "open_page", "status": "succeeded"},
                           {"kind": "tool", "name": "assert_text", "status": "succeeded"}],
                 "usage": [{"total_tokens": 3}], "operation_evidence": []}
        with self.assertRaises(module.CheckFailure):
            runner.browser_evidence("run_test", trace, finish_case=True)

    def test_evidence_redacts_capabilities_and_credentials(self):
        sanitized = module.sanitize({"token": "one-time-secret", "authorization": "secret",
                                     "control_url": "http://worker/private?token=secret",
                                     "run_id": "run_test", "nested": {"api_key": "secret"}})
        self.assertNotIn("secret", json.dumps(sanitized))
        self.assertEqual(sanitized["run_id"], "run_test")

    def test_identity_fixture_uses_persistable_local_storage(self):
        self.assertIn("localStorage", module.IDENTITY_HTML)
        self.assertNotIn("sessionStorage", module.IDENTITY_HTML)
        self.assertIn("Signed out", module.IDENTITY_HTML)

    def test_read_only_sql_scope_rejects_injection(self):
        self.assertEqual(module.record_id("conv_abc123", "conv"), "'conv_abc123'")
        with self.assertRaises(module.CheckFailure):
            module.record_id("conv_abc'; DROP TABLE conversations;--", "conv")

    def test_failure_is_not_hidden_by_provisional_limitations(self):
        runner = module.Management(self.env(), 1)
        runner.report["checks"]["real-sdk"] = {"status": "failed"}
        self.assertEqual(runner.outcome(), "failed")

    def test_negative_assertion_checks_domain_failure_independently_of_transport(self):
        trace = {"spans": [{"name": "mcp__browser_harness__assert_text", "status": "succeeded",
                           "output": {"result": {"structuredContent": {"ok": False,
                             "result": {"error": {"code": "condition_unmet"}}}}}}]}
        self.assertTrue(module.has_failed_assertion(trace))
        trace["spans"][0]["output"]["result"]["structuredContent"]["ok"] = True
        self.assertFalse(module.has_failed_assertion(trace))

    def test_sse_reconnect_uses_authenticated_opener_and_canonical_events(self):
        runner = module.Management(self.env(), 1)
        durable = [{"id": "1", "event": "harness.started", "data": {"run_id": "run_test"}},
                   {"id": "2", "event": "run.completed", "data": {"status": "succeeded"}}]
        cursors = []
        responses = []
        def open_request(req, **kwargs):
            self.assertEqual(req.get_header("Authorization"), "Bearer private-bridge")
            self.assertEqual(req.get_header("X-agenttrace-project-id"), "prj_test")
            cursor = int(req.get_header("Last-event-id"))
            cursors.append(cursor)
            response = sse_response(durable[cursor:])
            responses.append(response)
            return response
        with patch.object(runner, "sql", return_value=durable), \
                patch.object(runner.opener, "open", side_effect=open_request):
            self.assertEqual(runner.verify_sse("run_test"), {"events": 2, "reconnect": True})
        self.assertEqual(cursors, [0, 1])
        self.assertTrue(all(response.closed for response in responses))

    def test_sse_accepts_public_projection_without_modifying_secret_bearing_pg(self):
        runner = module.Management(self.env(), 1)
        durable = [
            {"id": "1", "event": "harness.started", "data": {
                "password": "unique-password", "items": [{" Authorization ": "private-auth", "ok": 1}],
                "control_url": "public-field", "provider_session_id": "public-session"}},
            {"id": "2", "event": "run.completed", "data": {"status": "succeeded", "TOKEN": "private-token"}},
        ]
        original = json.dumps(durable)
        wire = [
            {"id": "1", "event": "harness.started", "data": {
                "items": [{"ok": 1}], "control_url": "public-field", "provider_session_id": "public-session"}},
            {"id": "2", "event": "run.completed", "data": {"status": "succeeded"}},
        ]
        with patch.object(runner, "sql", return_value=durable), \
                patch.object(runner.opener, "open", side_effect=lambda req, **kw:
                             sse_response(wire[int(req.get_header("Last-event-id")):])):
            self.assertEqual(runner.verify_sse("run_test"), {"events": 2, "reconnect": True})
        self.assertEqual(json.dumps(durable), original)

    def test_sse_redacted_password_data_does_not_collide_with_framing(self):
        runner = module.Management(self.env(), 1)
        durable = [
            {"id": "1", "event": "harness.started", "data": {"password": "data", "ok": True}},
            {"id": "2", "event": "run.completed", "data": {"status": "succeeded"}},
        ]
        wire = [
            {"id": "1", "event": "harness.started", "data": {"ok": True}},
            {"id": "2", "event": "run.completed", "data": {"status": "succeeded"}},
        ]
        original = json.dumps(durable)
        responses = []
        def open_request(req, **kwargs):
            response = sse_response(wire[int(req.get_header("Last-event-id")):])
            responses.append(response)
            return response
        with patch.object(runner, "sql", return_value=durable), \
                patch.object(runner.opener, "open", side_effect=open_request):
            self.assertEqual(runner.verify_sse("run_test"), {"events": 2, "reconnect": True})
        self.assertEqual(json.dumps(durable), original)
        self.assertEqual(len(responses), 2)
        self.assertTrue(all(response.closed for response in responses))

    def test_sse_inferred_secrets_do_not_blacklist_metadata_or_public_values(self):
        for value in ("event", "id", "1", "harness.started", "run.completed",
                      "succeeded", "ok", "status", "true", "heartbeat", "token.delta"):
            with self.subTest(value=value):
                runner = module.Management(self.env(), 1)
                durable = [
                    {"id": "1", "event": "harness.started", "data": {
                        " SeCrEt ": {"nested": [value]}, "ok": True, "label": value}},
                    {"id": "2", "event": "run.completed", "data": {"status": "succeeded"}},
                ]
                wire = [
                    {"id": "1", "event": "harness.started", "data": {"ok": True, "label": value}},
                    {"id": "2", "event": "run.completed", "data": {"status": "succeeded"}},
                ]
                original = json.dumps(durable)
                responses = []
                def open_request(req, **kwargs):
                    events = [{"event": "token.delta", "data": {"text": value}},
                              *wire[int(req.get_header("Last-event-id")):]]
                    response = sse_response(events, prefix=(": " + value + "\n\n").encode())
                    responses.append(response)
                    return response
                with patch.object(runner, "sql", return_value=durable), \
                        patch.object(runner.opener, "open", side_effect=open_request):
                    self.assertEqual(runner.verify_sse("run_test"), {"events": 2, "reconnect": True})
                self.assertEqual(json.dumps(durable), original)
                self.assertTrue(all(response.closed for response in responses))

    def test_sse_secret_key_denial_is_independent_of_inferred_or_caller_values(self):
        durable = [
            {"id": "1", "event": "harness.started", "data": {"ok": True}},
            {"id": "2", "event": "run.completed", "data": {"status": "succeeded"}},
        ]
        for key in (" Authorization ", "COOKIE", " Password ", "ToKeN", " API_KEY ", "secret"):
            with self.subTest(key=key):
                runner = module.Management(self.env(), 1)
                data = json.dumps({"nested": [{key: ""}]}).replace("secret", "\\u0073ecret")
                body = ("event: token.delta\ndata: " + data + "\n\n").encode()
                response = sse_response(raw=body)
                with patch.object(runner, "sql", return_value=durable), \
                        patch.object(runner.opener, "open", return_value=response):
                    with self.assertRaisesRegex(module.CheckFailure, "secret-bearing field"):
                        runner.verify_sse("run_test")
                self.assertTrue(response.closed)

    def test_sse_public_projection_equality_still_denies_changed_replay(self):
        durable = [
            {"id": "1", "event": "harness.started", "data": {"password": "data", "ok": True}},
            {"id": "2", "event": "run.completed", "data": {"status": "succeeded"}},
        ]
        for data in ({"ok": False}, {}, {"ok": True, "extra": "unexpected"}):
            with self.subTest(data=data):
                runner = module.Management(self.env(), 1)
                wire = [{**durable[0], "data": data}, durable[1]]
                responses = []
                def open_request(req, **kwargs):
                    response = sse_response(wire[int(req.get_header("Last-event-id")):])
                    responses.append(response)
                    return response
                with patch.object(runner, "sql", return_value=durable), \
                        patch.object(runner.opener, "open", side_effect=open_request):
                    with self.assertRaisesRegex(module.CheckFailure, "differs from canonical"):
                        runner.verify_sse("run_test")
                self.assertEqual(len(responses), 2)
                self.assertTrue(all(response.closed for response in responses))

    def test_sse_denies_raw_pg_secret_leaked_on_wire(self):
        runner = module.Management(self.env(), 1)
        durable = [
            {"id": "1", "event": "harness.started", "data": {"password": "unique-password"}},
            {"id": "2", "event": "run.completed", "data": {"status": "succeeded"}},
        ]
        with patch.object(runner, "sql", return_value=durable), \
                patch.object(runner.opener, "open", side_effect=lambda req, **kw:
                             sse_response(durable[int(req.get_header("Last-event-id")):])):
            with self.assertRaisesRegex(module.CheckFailure, "SSE.*secret"):
                runner.verify_sse("run_test")

    def test_sse_forbidden_canary_denial_covers_raw_and_ephemeral_public_events(self):
        durable = [
            {"id": "1", "event": "harness.started", "data": {"ok": True}},
            {"id": "2", "event": "run.completed", "data": {"status": "succeeded"}},
        ]
        for prefix in (
            b": unique-password\n\n",
            b'id: unique-password\nevent: token.delta\ndata: {}\n\n',
            b'event: unique-password\ndata: {}\n\n',
            b'id: 1\nevent: harness.started\ndata: {"text":"unique-password"}\n\n',
            b'event: token.delta\ndata: {"text":"unique-password"}\n\n',
            b'event: token.delta\ndata: {"text":"unique-p\\u0061ssword"}\n\n',
            b'event: token.delta\ndata: {"unique-p\\u0061ssword":"value"}\n\n',
            b'event: token.delta\ndata: {"nested":[{" Cookie ":""}]}\n\n',
        ):
            with self.subTest(prefix=prefix):
                runner = module.Management(self.env(), 1)
                body = prefix + b'id: 1\nevent: harness.started\ndata: {"ok":true}\n\n'
                response = sse_response(raw=body)
                with patch.object(runner, "sql", return_value=durable), \
                        patch.object(runner.opener, "open", side_effect=lambda req, **kw:
                                     response if req.get_header("Last-event-id") == "0"
                                     else sse_response(durable[1:])):
                    with self.assertRaisesRegex(module.CheckFailure, "SSE.*secret") as failure:
                        runner.verify_sse("run_test", forbidden_values=("unique-password",))
                self.assertNotIn("unique-password", str(failure.exception))
                self.assertTrue(response.closed)
        runner = module.Management(self.env(), 1)
        with patch.object(runner, "sql", return_value=durable), \
                patch.object(runner.opener, "open", side_effect=lambda req, **kw:
                             sse_response(durable[int(req.get_header("Last-event-id")):])):
            runner.verify_sse("run_test", forbidden_values=("unique-password",))

    def test_sse_explicit_common_canaries_still_check_raw_framing(self):
        durable = [
            {"id": "1", "event": "harness.started", "data": {"ok": True}},
            {"id": "2", "event": "run.completed", "data": {"status": "succeeded"}},
        ]
        for canary in ("data", "event", "id", "1", "harness.started"):
            with self.subTest(canary=canary):
                runner = module.Management(self.env(), 1)
                response = sse_response(durable)
                with patch.object(runner, "sql", return_value=durable), \
                        patch.object(runner.opener, "open", return_value=response):
                    with self.assertRaisesRegex(module.CheckFailure, "forbidden secret value"):
                        runner.verify_sse("run_test", forbidden_values=(canary,))
                self.assertTrue(response.closed)

    def test_sse_heartbeat_and_missing_reconnect_share_total_deadline(self):
        durable = [
            {"id": "1", "event": "harness.started", "data": {"ok": True}},
            {"id": "2", "event": "run.completed", "data": {"status": "succeeded"}},
        ]
        for missing_reconnect in (False, True):
            with self.subTest(missing_reconnect=missing_reconnect):
                runner = module.Management(self.env(), 1)
                clock = [0.0]
                responses, budgets = [], []
                def open_request(req, timeout):
                    budgets.append(timeout)
                    cursor = int(req.get_header("Last-event-id"))
                    if missing_reconnect and cursor == 0:
                        response = sse_response(durable[:1])
                    else:
                        response = sse_response(raw=(
                            b'event: token.delta\ndata: {"text":"ok"}\n\n'
                            if missing_reconnect else b": heartbeat\n\n") * 20)
                    readline = response.readline
                    def advancing_readline(*args):
                        clock[0] += 0.125
                        return readline(*args)
                    response.readline = advancing_readline
                    responses.append(response)
                    return response
                with patch.object(runner, "sql", return_value=durable), \
                        patch.object(runner.opener, "open", side_effect=open_request), \
                        patch.object(module.time, "monotonic", side_effect=lambda: clock[0]):
                    with self.assertRaisesRegex(module.CheckFailure, "timed out"):
                        runner.verify_sse("run_test")
                self.assertTrue(all(response.closed for response in responses))
                self.assertLessEqual(clock[0], 1.125)
                self.assertEqual(len(responses), 2 if missing_reconnect else 1)
                if missing_reconnect:
                    self.assertLess(budgets[1], budgets[0])

    def test_sse_partial_raw_reads_expire_and_close_response(self):
        durable = [
            {"id": "1", "event": "harness.started", "data": {}},
            {"id": "2", "event": "run.completed", "data": {}},
        ]
        for chunked in (False, True):
            with self.subTest(chunked=chunked):
                reader, writer = socket.socketpair()
                headers = b"HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\n"
                writer.sendall(headers + (b"Transfer-Encoding: chunked\r\n" if chunked else b"") + b"\r\n")
                response = http.client.HTTPResponse(reader)
                response.begin()
                stopped = threading.Event()
                def trickle():
                    try:
                        if chunked:
                            writer.sendall(b"1000\r\n")
                        for _ in range(20):
                            writer.sendall(b":")
                            if stopped.wait(0.02):
                                break
                    except OSError:
                        pass
                    finally:
                        writer.close()
                sender = threading.Thread(target=trickle)
                sender.start()
                runner = module.Management(self.env(), 0.08)
                try:
                    started = time.monotonic()
                    with patch.object(runner, "sql", return_value=durable), \
                            patch.object(runner.opener, "open", return_value=response):
                        with self.assertRaisesRegex(module.CheckFailure, "timed out"):
                            runner.verify_sse("run_test")
                    self.assertLess(time.monotonic() - started, 0.25)
                    self.assertTrue(response.closed)
                finally:
                    stopped.set()
                    response.close()
                    reader.close()
                    sender.join()

    def test_sse_partial_response_headers_obey_total_deadline_and_close(self):
        reader, writer = socket.socketpair()
        writer.sendall(b"HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nX-Test: ")
        stopped = threading.Event()
        def trickle():
            try:
                for _ in range(20):
                    writer.sendall(b".")
                    if stopped.wait(0.02):
                        break
            except OSError:
                pass
            finally:
                writer.close()
        def connect(connection):
            connection.sock = reader
            reader.settimeout(connection.timeout)
        responses = []
        close = http.client.HTTPResponse.close
        def close_response(response):
            close(response)
            responses.append(response)
        sender = threading.Thread(target=trickle)
        sender.start()
        runner = module.Management(self.env(), 0.08)
        durable = [{"id": "1", "event": "harness.started", "data": {}},
                   {"id": "2", "event": "run.completed", "data": {}}]
        try:
            started = time.monotonic()
            with patch.object(runner, "sql", return_value=durable), \
                    patch.object(http.client.HTTPConnection, "connect", new=connect), \
                    patch.object(http.client.HTTPResponse, "close", new=close_response):
                with self.assertRaisesRegex(module.CheckFailure, "timed out"):
                    runner.verify_sse("run_test")
            self.assertLess(time.monotonic() - started, 0.25)
            self.assertTrue(responses)
            self.assertTrue(all(response.closed for response in responses))
            self.assertEqual(reader.fileno(), -1)
        finally:
            stopped.set()
            reader.close()
            sender.join()

    def test_persistence_compares_scoped_pg_counts_to_trace(self):
        runner = module.Management(self.env(), 1)
        counts = {"spans": 1, "attempts": 1, "usage": 1, "operations": 1,
                  "completed": 1, "pending_outbox": 0, "published_outbox": 2}
        trace = {"spans": [{}], "attempts": [{}], "usage": [{}], "operation_evidence": [{}]}
        with patch.object(runner, "sql", return_value=counts) as sql:
            self.assertEqual(runner.verify_persistence("run_test", trace), counts)
            self.assertIn("project_id='prj_test'", sql.call_args.args[0])
        with patch.object(runner, "sql", return_value={**counts, "completed": 2}):
            with self.assertRaises(module.CheckFailure):
                runner.verify_persistence("run_test", trace)

    def test_persistence_denies_missing_outbox_despite_zero_pending_rows(self):
        runner = module.Management(self.env(), 1)
        counts = {"spans": 1, "attempts": 1, "usage": 1, "operations": 1,
                  "completed": 1, "pending_outbox": 0, "published_outbox": 0}
        trace = {"spans": [{}], "attempts": [{}], "usage": [{}], "operation_evidence": [{}]}
        with patch.object(runner, "sql", return_value=counts):
            with self.assertRaisesRegex(module.CheckFailure, "published.*outbox"):
                runner.verify_persistence("run_test", trace)

    def test_exported_evidence_requires_actual_correlation_inputs_and_usage(self):
        runner = module.Management(self.env(), 1)
        canonical = {"spans": [{"id": "span_physical", "span_id": "model_logical", "kind": "model",
                                "input": {"messages": ["goal"]}}],
                     "usage": [{"span_id": "model_logical", "input_tokens": 2, "output_tokens": 3,
                                "total_tokens": 5}]}
        exported = {"sessionId": "agenttrace-session-conv_test", "metadata": {
            "agenttrace_run_id": "run_test", "agenttrace_status": "succeeded", "agenttrace_end_time": "now"},
            "observations": [{"id": "agenttrace-observation-span_physical", "type": "GENERATION",
                "input": {"messages": ["goal"]}, "usageDetails": {"input": 2, "output": 3, "total": 5}}]}
        runner.validate_export("run_test", "conv_test", "succeeded", canonical, exported)
        exported["observations"][0]["usageDetails"] = {"input": 800, "output": 1000, "total": 1800}
        with self.assertRaisesRegex(module.CheckFailure, "generation usage differs"):
            runner.validate_export("run_test", "conv_test", "succeeded", canonical, exported)

    def test_export_usage_excludes_query_cumulative_and_unattached_records(self):
        runner = module.Management(self.env(), 1)
        canonical = {"spans": [{"id": "span_physical", "span_id": "model_logical", "kind": "model"}],
                     "usage": [
                         {"span_id": "model_logical", "input_tokens": 2, "output_tokens": 3, "total_tokens": 5},
                         {"span_id": "model_logical", "input_tokens": 100, "output_tokens": 200,
                          "total_tokens": 300, "details": {"granularity": "sdk_query_model_cumulative",
                                                         "usage_scope": "sdk_query_pipeline"}},
                         {"span_id": "", "total_tokens": 900},
                     ]}
        exported = {"sessionId": "agenttrace-session-conv_test", "metadata": {
            "agenttrace_run_id": "run_test", "agenttrace_status": "succeeded", "agenttrace_end_time": "now"},
            "observations": [{"id": "agenttrace-observation-span_physical", "type": "GENERATION",
                              "usageDetails": {"input": 2, "output": 3, "total": 5}}]}
        runner.validate_export("run_test", "conv_test", "succeeded", canonical, exported)
        canonical["usage"] = canonical["usage"][1:]
        exported["observations"][0].pop("usageDetails")
        runner.validate_export("run_test", "conv_test", "succeeded", canonical, exported)

    def test_export_usage_accepts_non_object_details_as_exporter_does(self):
        runner = module.Management(self.env(), 1)
        canonical = {"spans": [{"id": "span_physical", "span_id": "model_logical", "kind": "model"}],
                     "usage": [{"span_id": "model_logical", "input_tokens": 2, "output_tokens": 3,
                                "total_tokens": 5}]}
        exported = {"sessionId": "agenttrace-session-conv_test", "metadata": {
            "agenttrace_run_id": "run_test", "agenttrace_status": "succeeded", "agenttrace_end_time": "now"},
            "observations": [{"id": "agenttrace-observation-span_physical", "type": "GENERATION",
                              "usageDetails": {"input": 2, "output": 3, "total": 5}}]}
        for details in (None, [], "unknown", 7):
            with self.subTest(details=details):
                canonical["usage"][0]["details"] = details
                runner.validate_export("run_test", "conv_test", "succeeded", canonical, exported)


if __name__ == "__main__":
    unittest.main()
