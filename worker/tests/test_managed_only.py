"""Managed-only HTTP/model admission must fail before browser/runner effects."""

import itertools
import unittest
from unittest.mock import AsyncMock, patch

import httpx
from pydantic import ValidationError

from loop_worker.contracts import ExecuteRequest, OpenSessionRequest
from loop_worker.main import create_app
from tests.case_builder import build_case, goto_step
from tests.managed_api import IDENTITY, api_client, execute_payload
from tests.test_environments import MemoryBrowserFactory, MemoryContext


CASE = build_case([goto_step(0, "http://127.0.0.1/index.html", "/index.html")])


class ManagedModelTest(unittest.TestCase):
    def test_open_and_execute_require_every_identity_field(self):
        for model, complete in (
            (OpenSessionRequest, IDENTITY),
            (ExecuteRequest, execute_payload(CASE)),
        ):
            fields = list(complete.keys() - {"case", "session_id"})
            for count in range(len(fields)):
                for included in itertools.combinations(fields, count):
                    payload = {key: value for key, value in complete.items()
                               if key in included or key in ("session_id", "case")}
                    with self.subTest(model=model.__name__, included=included):
                        with self.assertRaises(ValidationError):
                            model.model_validate(payload)
            self.assertEqual(model.model_validate(complete).session_id, "sess_test")

    def test_managed_fields_reject_null_empty_and_wrong_types(self):
        complete = execute_payload(CASE)
        for field, invalid in (
            ("project_id", [None, "", "../other"]),
            ("environment_id", [None, "", "../other"]),
            ("profile_generation", [None, 0, -1, True, "1"]),
            ("fencing_token", [None, 0, -1, True, "2"]),
            ("browser_session_id", [None, "", 1]),
        ):
            for value in invalid:
                with self.subTest(field=field, value=value):
                    with self.assertRaises(ValidationError):
                        ExecuteRequest.model_validate({**complete, field: value})


class ManagedHTTPTest(unittest.IsolatedAsyncioTestCase):
    async def test_internal_auth_is_first_for_all_browser_routes(self):
        async with api_client(browser_factory=MemoryBrowserFactory()) as client:
            opened = await client.open()
            sid = opened.json()["session_id"]
            session = client.manager.get(sid)
            before = (client.root / "profiles/env_api.profile").read_bytes()
            requests = (
                ("POST", "/sessions", {"session_id": "sess_unmanaged"}),
                ("POST", "/sessions", IDENTITY),
                ("POST", "/execute", {"session_id": "sess_unmanaged", "case": CASE}),
                ("POST", "/execute", execute_payload(CASE, sid)),
                ("POST", f"/sessions/{sid}/navigate", {"url": "http://127.0.0.1/"}),
                ("POST", f"/sessions/{sid}/act", {
                    "action": "input", "locator": {"kind": "css", "css": "#password"},
                    "value": "never-typed",
                }),
                ("DELETE", f"/sessions/{sid}", None),
                ("DELETE", "/sessions/bsess_unknown", None),
            )
            with patch.object(
                client.manager, "browser", new_callable=AsyncMock,
                side_effect=AssertionError("denied request started browser"),
            ) as browser, patch(
                "loop_worker.main.run_case", new_callable=AsyncMock,
            ) as runner:
                for credential in ("", "Bearer wrong"):
                    for method, path, payload in requests:
                        with self.subTest(credential=credential, method=method, path=path):
                            response = await client.request(
                                method, path, headers={"Authorization": credential},
                                **({"json": payload} if payload is not None else {}),
                            )
                            self.assertEqual(response.status_code, 401, response.text)
                            self.assertEqual(response.json()["error"], "unauthorized")
                browser.assert_not_awaited()
                runner.assert_not_awaited()
            self.assertEqual(session.page.inputs, [])
            self.assertEqual(session.page.url, "about:blank")
            self.assertFalse(session.context.closed)
            self.assertEqual(before, (client.root / "profiles/env_api.profile").read_bytes())

    async def test_unconfigured_auth_fails_closed_on_browser_routes(self):
        with patch.dict("os.environ", {"LOOP_WORKER_INTERNAL_TOKEN": ""}):
            app = create_app()
            try:
                async with httpx.AsyncClient(
                    transport=httpx.ASGITransport(app=app), base_url="http://worker.test",
                ) as client:
                    for path in ("/sessions", "/execute", "/sessions/bsess_unknown/navigate"):
                        response = await client.post(path, json={})
                        self.assertEqual(response.status_code, 503, response.text)
                        self.assertEqual(response.json()["error"], "internal_auth_unconfigured")
                    self.assertIsNone(app.state.session_manager._browser)
            finally:
                await app.state.session_manager.shutdown()

    async def test_incomplete_http_identity_has_no_browser_or_runner_effects(self):
        async with api_client(browser_factory=MemoryBrowserFactory()) as client:
            with patch.object(
                client.manager, "browser", new_callable=AsyncMock,
                side_effect=AssertionError("denied request started browser"),
            ) as browser, patch(
                "loop_worker.main.run_case", new_callable=AsyncMock,
            ) as runner:
                for path, complete, error in (
                    ("/sessions", IDENTITY, "invalid_request"),
                    ("/execute", execute_payload(CASE), "case_invalid_json"),
                ):
                    payloads = [{"session_id": "sess_unmanaged", **(
                        {"case": CASE} if path == "/execute" else {}
                    )}]
                    payloads.extend(
                        {key: value for key, value in complete.items() if key != omitted}
                        for omitted in complete if omitted != "case"
                    )
                    for payload in payloads:
                        with self.subTest(path=path, fields=tuple(payload)):
                            response = await client.post(path, json=payload)
                            self.assertEqual(response.status_code, 400, response.text)
                            self.assertEqual(response.json()["error"], error)
                browser.assert_not_awaited()
                runner.assert_not_awaited()
            self.assertEqual(client.manager._sessions, {})
            self.assertFalse((client.root / "profiles").exists())

    async def test_registered_unmanaged_handle_cannot_act_navigate_or_close(self):
        async with api_client(browser_factory=MemoryBrowserFactory()) as client:
            context = MemoryContext(None)
            session = client.manager.register("sess_unmanaged", context, await context.new_page())
            sid = session.browser_session_id
            for method, suffix, payload in (
                ("DELETE", "", None),
                ("POST", "/navigate", {"url": "http://127.0.0.1/"}),
                ("POST", "/act", {"action": "click", "locator": {"kind": "css", "css": "#x"}}),
            ):
                response = await client.request(
                    method, f"/sessions/{sid}{suffix}",
                    **({"json": payload} if payload is not None else {}),
                )
                self.assertEqual(response.status_code, 409, response.text)
                self.assertEqual(response.json()["error"], "environment_conflict")
            self.assertEqual(session.page.url, "about:blank")
            self.assertEqual(session.page.inputs, [])
            self.assertFalse(context.closed)

    async def test_session_actions_require_current_lease_headers_before_effects(self):
        async with api_client(browser_factory=MemoryBrowserFactory()) as client:
            sid = (await client.open()).json()["session_id"]
            session = client.manager.get(sid)
            before = (client.root / "profiles/env_api.profile").read_bytes()
            for changes, status in (
                ({"X-Profile-Generation": ""}, 400),
                ({"X-Fencing-Token": ""}, 400),
                ({"X-Profile-Generation": "0"}, 400),
                ({"X-Fencing-Token": "not-a-number"}, 400),
                ({"X-Profile-Generation": "2"}, 409),
                ({"X-Fencing-Token": "1"}, 409),
                ({"X-Fencing-Token": "3"}, 409),
                ({"X-Environment-ID": "env_other"}, 409),
                ({"X-Project-ID": ""}, 409),
                ({"X-Project-ID": "prj_b"}, 409),
            ):
                for method, suffix, payload in (
                    ("POST", "/navigate", {"url": "http://127.0.0.1/"}),
                    ("POST", "/act", {
                        "action": "input", "locator": {"kind": "css", "css": "#x"},
                        "value": "never-typed",
                    }),
                    ("DELETE", "", None),
                ):
                    with self.subTest(changes=changes, method=method, suffix=suffix):
                        response = await client.request(
                            method, f"/sessions/{sid}{suffix}", headers=changes,
                            **({"json": payload} if payload is not None else {}),
                        )
                        self.assertEqual(response.status_code, status, response.text)
            self.assertEqual(session.page.url, "about:blank")
            self.assertEqual(session.page.inputs, [])
            self.assertFalse(session.context.closed)
            self.assertEqual(before, (client.root / "profiles/env_api.profile").read_bytes())

    async def test_replay_rejects_mismatched_closed_and_stale_handles_without_effects(self):
        async with api_client(browser_factory=MemoryBrowserFactory()) as client:
            sid = (await client.open()).json()["session_id"]
            session = client.manager.get(sid)
            before = (client.root / "profiles/env_api.profile").read_bytes()
            with patch.object(client.manager, "browser", new_callable=AsyncMock) as browser, patch(
                "loop_worker.main.run_case", new_callable=AsyncMock,
            ) as runner:
                for changes in (
                    {"session_id": "sess_other"}, {"project_id": "prj_b"},
                    {"environment_id": "env_other"}, {"profile_generation": 2},
                    {"fencing_token": 1}, {"fencing_token": 3},
                    {"browser_session_id": "bsess_unknown"}, {"browser_session_id": "sess_test"},
                ):
                    response = await client.post("/execute", json=execute_payload(CASE, sid, **changes))
                    self.assertEqual(response.status_code, 409, response.text)
                browser.assert_not_awaited()
                runner.assert_not_awaited()
            self.assertEqual(before, (client.root / "profiles/env_api.profile").read_bytes())
            self.assertFalse(session.context.closed)
            self.assertEqual((await client.delete(f"/sessions/{sid}")).status_code, 200)
            with patch.object(client.manager, "browser", new_callable=AsyncMock) as browser, patch(
                "loop_worker.main.run_case", new_callable=AsyncMock,
            ) as runner:
                closed = await client.post("/execute", json=execute_payload(CASE, sid))
                self.assertEqual(closed.status_code, 409, closed.text)
                browser.assert_not_awaited()
                runner.assert_not_awaited()
            current = (await client.open()).json()["session_id"]
            self.assertNotEqual(current, sid)
            with patch.object(client.manager, "browser", new_callable=AsyncMock) as browser, patch(
                "loop_worker.main.run_case", new_callable=AsyncMock,
            ) as runner:
                stale = await client.post("/execute", json=execute_payload(CASE, sid))
                self.assertEqual(stale.status_code, 409, stale.text)
                browser.assert_not_awaited()
                runner.assert_not_awaited()
