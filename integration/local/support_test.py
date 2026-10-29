"""Offline adapters shared by managed acceptance and owned infrastructure."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).parent))


class SupportTests(unittest.TestCase):
    def setUp(self):
        path = Path(__file__).with_name("support.py")
        self.assertTrue(path.is_file(), "managed support module is required without acceptance.py")
        self.s = __import__("support")

    def test_sse_multiline_ephemeral_comments_and_crlf(self):
        self.assertEqual(list(self.s.parse_sse([
            b": heartbeat\r\n", b"\r\n", b"id: 1\r\n", b"event: run.completed\r\n",
            b'data: {"status":\r\n', b'data: "succeeded"}\r\n', b"\r\n",
            b"event: token.delta\n", b'data: {"text":"ok"}\n', b"\n",
        ])), [{"id": "1", "event": "run.completed", "data": {"status": "succeeded"}},
              {"event": "token.delta", "data": {"text": "ok"}}])

    def test_environment_file_order_quotes_export_comments_and_process_precedence(self):
        with tempfile.TemporaryDirectory() as directory:
            a, b = Path(directory) / "a", Path(directory) / "b"
            a.write_text("export ONE='first value' # comment\nTWO=first\ninvalid-key=no\n")
            b.write_text('TWO="second value"\nTHREE=file\n')
            with patch.dict(os.environ, {"THREE": "process"}, clear=True):
                self.assertEqual(self.s.load_environment([a, Path(directory) / "missing", b]),
                                 {"ONE": "first value", "TWO": "second value", "THREE": "process"})

    def test_environment_preserves_unquoted_json_headers(self):
        cases = (
            ('{"X-Provider-Route":"fixture-route"}',
             {"X-Provider-Route": "fixture-route"}),
            (r'''{"X-Provider-Route": "fixture # route", "X-Quoted": "say \"hello\"", "X-Path": "C:\\fixture", "X-Equals": "one=two", "X-Owner": "fixture's route"}''',
             {"X-Provider-Route": "fixture # route", "X-Quoted": 'say "hello"',
              "X-Path": "C:\\fixture", "X-Equals": "one=two", "X-Owner": "fixture's route"}),
        )
        for headers, expected in cases:
            for prefix, suffix in (("", ""), ("export ", " # local comment")):
                with self.subTest(headers=headers, prefix=prefix), tempfile.TemporaryDirectory() as directory:
                    source = Path(directory) / ".env"
                    source.write_text(f"{prefix}AGENTTRACE_CODEX_HTTP_HEADERS={headers}{suffix}\n")
                    with patch.dict(os.environ, {}, clear=True):
                        value = self.s.load_environment([source])["AGENTTRACE_CODEX_HTTP_HEADERS"]
                    self.assertEqual(value, headers)
                    self.assertEqual(json.loads(value), expected)

    def test_environment_outer_quotes_comments_and_literal_unquoted_values(self):
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory) / ".env"
            source.write_text(
                "# ignored\n"
                "export SINGLE='first # value' # outside\n"
                'DOUBLE="second # value" # outside\n'
                r'ESCAPED="say \"hello\" in C:\\fixture"' + "\n"
                "EMPTY='' # outside\n"
                "PLAIN=first  second # outside\n"
                'INTERIOR=one"two" # outside\n'
                r"PATH=C:\fixture\route" + "\n"
                "invalid-key=ignored\n"
            )
            with patch.dict(os.environ, {}, clear=True):
                self.assertEqual(self.s.load_environment([source]), {
                    "SINGLE": "first # value", "DOUBLE": "second # value",
                    "ESCAPED": 'say "hello" in C:\\fixture', "EMPTY": "",
                    "PLAIN": "first  second", "INTERIOR": 'one"two"',
                    "PATH": "C:\\fixture\\route",
                })

    def test_environment_json_file_order_and_process_precedence(self):
        with tempfile.TemporaryDirectory() as directory:
            a, b = Path(directory) / "a", Path(directory) / "b"
            a.write_text('AGENTTRACE_CODEX_HTTP_HEADERS={"X-Provider-Route":"first"}\n')
            b.write_text('AGENTTRACE_CODEX_HTTP_HEADERS={"X-Provider-Route":"second"}\n')
            with patch.dict(os.environ, {}, clear=True):
                value = self.s.load_environment([a, b])["AGENTTRACE_CODEX_HTTP_HEADERS"]
                self.assertEqual(value, '{"X-Provider-Route":"second"}')
                self.assertEqual(json.loads(value), {"X-Provider-Route": "second"})
            for process_value in ('{"X-Provider-Route":"process"}', ""):
                with self.subTest(process_value=process_value), \
                        patch.dict(os.environ, {"AGENTTRACE_CODEX_HTTP_HEADERS": process_value}, clear=True):
                    self.assertEqual(self.s.load_environment([a, b])["AGENTTRACE_CODEX_HTTP_HEADERS"],
                                     process_value)

    def test_environment_outer_quoted_json_headers(self):
        headers = '{"X-Provider-Route":"fixture # route"}'
        for value in ("'" + headers + "'", '"' + headers.replace('"', '\\"') + '"'):
            with self.subTest(value=value), tempfile.TemporaryDirectory() as directory:
                source = Path(directory) / ".env"
                source.write_text(f"export AGENTTRACE_CODEX_HTTP_HEADERS={value} # outside\n")
                with patch.dict(os.environ, {}, clear=True):
                    actual = self.s.load_environment([source])["AGENTTRACE_CODEX_HTTP_HEADERS"]
                self.assertEqual(actual, headers)
                self.assertEqual(json.loads(actual), {"X-Provider-Route": "fixture # route"})

    def test_environment_unclosed_quotes_do_not_disclose_values(self):
        for value in ("'synthetic-private", '"synthetic-private'):
            with self.subTest(value=value), tempfile.TemporaryDirectory() as directory:
                source = Path(directory) / ".env"
                source.write_text(f"VALUE={value}\n")
                with patch.dict(os.environ, {}, clear=True), self.assertRaises(ValueError) as failure:
                    self.s.load_environment([source])
                self.assertNotIn("synthetic-private", str(failure.exception))

    def test_scoped_ids_and_single_exception_identity(self):
        self.assertEqual(self.s.sql_id("run_abc123"), "'run_abc123'")
        self.assertEqual(self.s.sql_id("conv_abc123", "conv"), "'conv_abc123'")
        with self.assertRaises(self.s.CheckFailure):
            self.s.sql_id("run_abc'; DELETE FROM runs;--")
        import management
        self.assertIs(management.CheckFailure, self.s.CheckFailure)
        self.assertIsInstance(management.Management({
            "AGENTTRACE_BRIDGE_TOKEN": "test", "AGENTTRACE_PROJECT_ID": "prj_test",
            "AGENTTRACE_LANGFUSE_PROJECT_ID": "lf-test"}, 1), self.s.AcceptanceSupport)

    def test_pg_uri_credentials_options_and_base_isolation(self):
        base = {"PGHOST": "old", "PGPASSWORD": "unrelated-secret", "OTHER": "keep"}
        child = self.s.pg_environment(
            "postgresql://test%40user:p%40ss@127.0.0.1:5433/native?sslmode=verify-full&sslrootcert=%2Ftmp%2Fca.pem", base)
        self.assertEqual(child["PGUSER"], "test@user")
        self.assertEqual(child["PGPASSWORD"], "p@ss")
        self.assertEqual(child["PGHOST"], "127.0.0.1")
        self.assertEqual(child["PGPORT"], "5433")
        self.assertEqual(child["PGDATABASE"], "native")
        self.assertEqual(child["PGSSLMODE"], "verify-full")
        self.assertEqual(child["PGSSLROOTCERT"], "/tmp/ca.pem")
        self.assertEqual(base["PGPASSWORD"], "unrelated-secret")
        self.assertNotIn("PGPASSWORD", self.s.pg_environment("postgresql:///native", base))

    def test_libpq_and_unsupported_options(self):
        self.assertEqual(self.s.pg_environment(
            "host=localhost dbname=native password='test value' sslmode=disable", {})["PGPASSWORD"], "test value")
        for dsn in ("postgresql:///native?unsupported=value", "postgresql:///native#fragment", "invalid"):
            with self.assertRaises(self.s.CheckFailure):
                self.s.pg_environment(dsn, {})

    def test_libpq_quoted_escapes_and_non_shell_values(self):
        for dsn, expected in (
            (r"host = localhost password = 'pa\'ss\\word'", "pa'ss\\word"),
            (r"password=pa\ ss", "pa ss"),
            ('password=one"two', 'one"two'),
            ("password=pa'ss", "pa'ss"),
            ("password=tail\\", "tail"),
            (r"password='a\nb'", "anb"),
            ("password='' dbname=native", ""),
            ("password=one password=two", "two"),
            ("password= dbname=native", "dbname=native"),
        ):
            with self.subTest(dsn=dsn):
                self.assertEqual(self.s.pg_environment(dsn, {})["PGPASSWORD"], expected)
        for dsn in ("password='unique-secret", "password='unique-secret'junk", "password unique-secret"):
            with self.subTest(dsn=dsn):
                with self.assertRaises(self.s.CheckFailure) as failure:
                    self.s.pg_environment(dsn, {})
                self.assertNotIn("unique-secret", str(failure.exception))

    def test_libpq_escaped_credentials_reach_sql_and_native_admin_child_only(self):
        import run
        dsn = r"host=localhost dbname=native password='pa\'ss\\word' sslmode=disable"
        env = {"OTHER": "keep", "PGPASSWORD": "unrelated-secret", "AGENTTRACE_E2E_POSTGRES_DSN": dsn}
        base = self.s.AcceptanceSupport(env, 1)
        with patch.object(self.s.subprocess, "run", return_value=subprocess.CompletedProcess([], 0, "1")) as execute:
            self.assertEqual(base.sql("SELECT to_json(1)"), 1)
        self.assertEqual(execute.call_args.kwargs["env"]["PGPASSWORD"], "pa'ss\\word")
        self.assertNotIn("pa'ss", " ".join(execute.call_args.args[0]))
        self.assertEqual(env["PGPASSWORD"], "unrelated-secret")
        admin = run.postgres_environment(env, dsn)
        self.assertEqual(admin["PGDATABASE"], "postgres")
        self.assertEqual(admin["PGPASSWORD"], "pa'ss\\word")
        self.assertEqual(env["PGPASSWORD"], "unrelated-secret")

    def test_support_has_no_unscoped_http_preflight_or_run_creator(self):
        base = self.s.AcceptanceSupport({}, 1)
        for name in ("http", "preflight", "create_run", "run", "redis"):
            self.assertFalse(hasattr(base, name), name)

    def test_sql_credentials_are_child_only_and_diagnostics_suppressed(self):
        base = self.s.AcceptanceSupport({"OTHER": "keep", "AGENTTRACE_E2E_POSTGRES_DSN":
                                         "postgresql://test:p%40ss@localhost/native"}, 2)
        with patch.object(self.s.subprocess, "run", return_value=subprocess.CompletedProcess([], 0, '{"ok":true}')) as execute:
            self.assertEqual(base.sql("SELECT json_build_object('ok',true)"), {"ok": True})
        command = execute.call_args.args[0]
        self.assertEqual(command[:5], ["psql", "--no-psqlrc", "-Atq", "-v", "ON_ERROR_STOP=1"])
        self.assertNotIn("p@ss", " ".join(command))
        self.assertEqual(execute.call_args.kwargs["env"]["PGPASSWORD"], "p@ss")
        self.assertNotIn("PGPASSWORD", base.env)
        with patch.object(self.s.subprocess, "run", return_value=subprocess.CompletedProcess([], 1, "", "p@ss")):
            with self.assertRaises(self.s.CheckFailure) as failure:
                base.sql("SELECT to_json(1)")
            self.assertNotIn("p@ss", str(failure.exception))

    def test_bounded_poll_and_callback_error_propagation(self):
        base = self.s.AcceptanceSupport({}, 1)
        with patch.object(self.s.time, "monotonic", side_effect=[0, 2]):
            with self.assertRaisesRegex(self.s.CheckFailure, "timed out"):
                base.poll(lambda: False, "probe")
        with self.assertRaisesRegex(ValueError, "callback"):
            base.poll(lambda: (_ for _ in ()).throw(ValueError("callback")), "probe")
        for timeout in (0, -1, float("nan"), float("inf")):
            with self.assertRaises(self.s.CheckFailure):
                self.s.AcceptanceSupport({}, timeout)

    def test_cleanup_dispatches_only_tracked_runs_through_authenticated_management(self):
        import management
        runner = management.Management({"AGENTTRACE_BRIDGE_TOKEN": "private",
            "AGENTTRACE_PROJECT_ID": "prj_test", "AGENTTRACE_LANGFUSE_PROJECT_ID": "lf-test"}, 1)
        runner.created = ["run_owned", "run_failedcleanup"]
        requests = []
        class Response:
            status = 200
            def __enter__(self): return self
            def __exit__(self, *args): pass
            def read(self): return b'{"status":"running"}'
        def open_request(req, **kwargs):
            requests.append(req)
            if "run_failedcleanup" in req.full_url:
                raise OSError("private diagnostic")
            response = Response()
            if req.full_url.endswith("/cancel"):
                response.status = 202
            else:
                response.read = lambda: b'{"status":"cancelled"}' if len(requests) > 1 else b'{"status":"running"}'
            return response
        with patch.object(runner.opener, "open", side_effect=open_request):
            runner.cancel_remaining()
        self.assertEqual(runner.report["cleanup_incomplete"], ["run_failedcleanup"])
        self.assertTrue(all(req.get_header("Authorization") == "Bearer private" for req in requests))
        self.assertTrue(all("run_owned" in req.full_url or "run_failedcleanup" in req.full_url for req in requests))


if __name__ == "__main__":
    unittest.main()
