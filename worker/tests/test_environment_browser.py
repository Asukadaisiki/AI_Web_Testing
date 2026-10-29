"""Small real-Chromium acceptance suite for provider identity and controls."""

import os
import tempfile
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from unittest.mock import patch

import httpx
from playwright.async_api import Error as PlaywrightError

from tests import test_environments as support
from tests.case_builder import action_step, build_case, condition, css_locator, goto_step
from tests.local_site import LocalSite
from loop_worker.main import create_app


class EnvironmentBrowserTest(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.root = Path(self.directory.name).resolve()
        self.config = patch.dict(os.environ, {
            "LOOP_ENVIRONMENTS_DIR": str(self.root / "profiles"),
            "LOOP_ENVIRONMENT_ENCRYPTION_KEY": support.KEY,
            "LOOP_WORKER_INTERNAL_TOKEN": "internal-test-token",
            "LOOP_UPLOADS_DIR": str(self.root / "uploads"),
            "LOOP_ARTIFACTS_DIR": str(self.root / "artifacts"),
        })
        self.config.start()
        self.app = create_app()
        self.manager = self.app.state.session_manager
        self.client = httpx.AsyncClient(
            transport=httpx.ASGITransport(app=self.app), base_url="http://worker.test",
            headers=support.AUTH,
        )

    async def asyncTearDown(self):
        await self.client.aclose()
        await self.manager.shutdown()
        self.config.stop()
        self.directory.cleanup()

    async def provision(self, environment):
        response = await self.client.post(f"/environments/{environment}/provision", json={
            "project_id": "prj_a",
            "profile_generation": 1, "fencing_token": 1,
            "policy": {"allowed_domains": ["127.0.0.1"]},
        })
        self.assertEqual(response.status_code, 200, response.text)

    async def open(self, environment, generation=1, token=2):
        response = await self.client.post("/sessions", json={
            "session_id": "sess_browser", "environment_id": environment,
            "project_id": "prj_a",
            "profile_generation": generation, "fencing_token": token,
        })
        self.assertEqual(response.status_code, 200, response.text)
        return response.json()["session_id"]

    async def test_isolated_cookies_local_storage_persist_close_and_reset(self):
        with LocalSite() as site:
            for environment in ("env_a", "env_b"):
                await self.provision(environment)
            a, b = await self.open("env_a"), await self.open("env_b")
            first, second = self.manager.get(a), self.manager.get(b)
            await first.page.goto(site.url("generic_form.html"))
            await second.page.goto(site.url("generic_form.html"))
            await first.context.add_cookies([{
                "name": "identity", "value": "private-a", "url": site.base_url,
            }])
            await first.page.evaluate("localStorage.setItem('identity', 'private-a')")
            self.assertEqual(await second.context.cookies(), [])
            self.assertIsNone(await second.page.evaluate("localStorage.getItem('identity')"))
            closed = await self.client.delete(f"/sessions/{a}", headers={
                "X-Profile-Generation": "1", "X-Fencing-Token": "2",
            })
            self.assertEqual(closed.status_code, 200, closed.text)
            restored = self.manager.get(await self.open("env_a", token=3))
            await restored.page.goto(site.url("generic_form.html"))
            self.assertEqual((await restored.context.cookies())[0]["value"], "private-a")
            self.assertEqual(await restored.page.evaluate("localStorage.getItem('identity')"), "private-a")
            reset = await self.client.post("/environments/env_a/reset", json={
                "profile_generation": 2, "fencing_token": 4,
            })
            self.assertEqual(reset.status_code, 200, reset.text)
            fresh = self.manager.get(await self.open("env_a", generation=2, token=5))
            await fresh.page.goto(site.url("generic_form.html"))
            self.assertEqual(await fresh.context.cookies(), [])
            self.assertIsNone(await fresh.page.evaluate("localStorage.getItem('identity')"))

    async def test_real_screenshot_and_click_type_key_navigation(self):
        with LocalSite() as site:
            await self.provision("env_a")
            sid = await self.open("env_a")
            path = f"/environments/env_a/sessions/{sid}"
            async def input(payload):
                response = await self.client.post(path + "/input", json={
                    "profile_generation": 1, "fencing_token": 2, **payload,
                })
                self.assertEqual(response.status_code, 200, response.text)
            await input({"action": "navigate", "url": site.url("generic_form.html")})
            page = self.manager.get(sid).page
            await page.set_content("<input aria-label='Login'><button>Continue</button>")
            box = await page.get_by_label("Login").bounding_box()
            await input({"action": "click", "x": int(box["x"] + 10), "y": int(box["y"] + 10)})
            await input({"action": "type", "text": "user@example.test"})
            await input({"action": "key", "key": "Tab"})
            self.assertEqual(await page.get_by_label("Login").input_value(), "user@example.test")
            self.assertEqual(await page.evaluate("document.activeElement.tagName"), "BUTTON")
            screenshot = await self.client.get(path + "/screenshot", headers={
                "X-Profile-Generation": "1", "X-Fencing-Token": "2",
            })
            self.assertEqual(screenshot.status_code, 200, screenshot.text)
            self.assertTrue(screenshot.content.startswith(b"\x89PNG\r\n\x1a\n"))
            self.assertGreater(len(screenshot.content), 100)
            with self.assertRaises(PlaywrightError):
                await page.goto(site.url("generic_form.html").replace("127.0.0.1", "localhost"))

    async def test_managed_session_and_runner_uploads_cannot_bypass_scope(self):
        (self.root / "upload.html").write_text("<input id='upload' type='file'>")
        with LocalSite(self.root) as site:
            await self.provision("env_a")
            sid = await self.open("env_a")
            page = self.manager.get(sid).page
            await page.set_content("<input id='upload' type='file'>")
            outside = self.root / "secret"
            outside.write_bytes(b"host secret")
            managed = self.root / "uploads" / "env_a_g1"
            (managed / "document").write_bytes(b"allowed")
            (managed / "link").symlink_to(outside)
            for name, expected in (
                (str(outside), 400), ("../secret", 400), ("link", 400), ("document", 200),
            ):
                response = await self.client.post(f"/sessions/{sid}/act", headers={
                    "X-Profile-Generation": "1", "X-Fencing-Token": "2",
                }, json={
                    "action": "upload_file", "locator": {"kind": "css", "css": "#upload"},
                    "value": name,
                })
                self.assertEqual(response.status_code, expected, response.text)
            self.assertEqual(await page.locator("#upload").evaluate(
                "async el => await el.files[0].text()"
            ), "allowed")
            case = build_case([
                goto_step(0, site.url("upload.html"), "/upload.html"),
                action_step(
                    1, "upload_file", page_url=site.url("upload.html"),
                    locator=css_locator("#upload"), value=str(outside),
                    pre=[condition("url_contains", "/upload.html")],
                    post=[condition("url_contains", "/upload.html")],
                ),
            ], base_url=site.base_url)
            response = await self.client.post("/execute", json={
                "session_id": "sess_browser", "case": case, "environment_id": "env_a",
                "project_id": "prj_a", "browser_session_id": sid,
                "profile_generation": 1, "fencing_token": 2,
            })
            self.assertEqual(response.status_code, 200, response.text)
            self.assertEqual(response.json()["steps"][-1]["error"]["kind"], "upload_rejected")
            managed_case = build_case([
                goto_step(0, site.url("upload.html"), "/upload.html"),
                action_step(
                    1, "upload_file", page_url=site.url("upload.html"),
                    locator=css_locator("#upload"), value="document",
                    pre=[condition("url_contains", "/upload.html")],
                    post=[condition("url_contains", "/upload.html")],
                ),
            ], base_url=site.base_url)
            response = await self.client.post("/execute", json={
                "session_id": "sess_browser", "case": managed_case, "environment_id": "env_a",
                "project_id": "prj_a", "browser_session_id": sid,
                "profile_generation": 1, "fencing_token": 2,
            })
            self.assertEqual(response.status_code, 200, response.text)
            self.assertEqual(response.json()["status"], "passed", response.text)

    async def test_disallowed_redirect_never_reaches_the_destination(self):
        destinations = []
        class RedirectHandler(BaseHTTPRequestHandler):
            def do_GET(self):
                if self.path in ("/redirect", "/allowed-redirect"):
                    self.send_response(302)
                    location = (
                        f"http://localhost:{self.server.server_port}/blocked"
                        if self.path == "/redirect" else "/redirect"
                    )
                    self.send_header("Location", location)
                    self.end_headers()
                else:
                    destinations.append(self.path)
                    self.send_response(200)
                    self.end_headers()
                    self.wfile.write(b"blocked destination")

            def log_message(self, *args):
                pass

        server = ThreadingHTTPServer(("127.0.0.1", 0), RedirectHandler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            await self.provision("env_a")
            sid = await self.open("env_a")
            page = self.manager.get(sid).page
            for path in ("/redirect", "/allowed-redirect"):
                try:
                    await page.goto(f"http://127.0.0.1:{server.server_port}{path}", timeout=2000)
                except PlaywrightError:
                    pass
                result = await self.client.post("/execute", json={
                    "session_id": "sess_browser", "environment_id": "env_a",
                    "project_id": "prj_a", "browser_session_id": sid,
                    "profile_generation": 1, "fencing_token": 2,
                    "case": build_case([goto_step(
                        0, f"http://127.0.0.1:{server.server_port}{path}", path,
                    )]),
                })
                self.assertEqual(result.status_code, 200, result.text)
                self.assertEqual(result.json()["status"], "failed", result.text)
            self.assertEqual(destinations, [])
        finally:
            server.shutdown()
            server.server_close()
            thread.join(timeout=5)

    async def test_managed_execute_is_fresh_fenced_policy_bound_and_does_not_write_profile(self):
        with LocalSite() as site:
            await self.provision("env_a")
            sid = await self.open("env_a")
            session = self.manager.get(sid)
            await session.context.add_cookies([{
                "name": "identity", "value": "authoring", "url": site.base_url,
            }])
            before = (self.root / "profiles/env_a.profile").read_bytes()
            case = build_case([goto_step(0, site.url("generic_form.html"), "/generic_form.html")])
            created = []
            browser = await self.manager.browser()
            original = browser.new_context
            async def capture_context(**kwargs):
                context = await original(**kwargs)
                created.append(context)
                self.assertEqual(kwargs.get("storage_state"), {"cookies": [], "origins": []})
                self.assertFalse(kwargs["accept_downloads"])
                self.assertEqual(await context.cookies(), [])
                return context
            async def execute(case):
                return await self.client.post("/execute", json={
                    "session_id": "sess_browser", "environment_id": "env_a",
                    "project_id": "prj_a", "browser_session_id": sid,
                    "profile_generation": 1, "fencing_token": 2, "case": case,
                })
            with patch.object(browser, "new_context", capture_context):
                passed = await execute(case)
            self.assertEqual(passed.status_code, 200, passed.text)
            self.assertEqual(passed.json()["status"], "passed", passed.text)
            self.assertEqual(len(created), 1)
            denied = await execute(build_case([goto_step(
                0, site.url("generic_form.html").replace("127.0.0.1", "localhost"), "/generic_form.html",
            )]))
            self.assertEqual(denied.status_code, 200, denied.text)
            self.assertEqual(denied.json()["steps"][0]["error"]["kind"], "domain_rejected")
            self.assertEqual((await session.context.cookies())[0]["value"], "authoring")
            self.assertEqual(before, (self.root / "profiles/env_a.profile").read_bytes())

    async def test_managed_dryrun_replays_run_start_identity_in_an_isolated_context(self):
        with LocalSite() as site:
            await self.provision("env_a")
            sid = await self.open("env_a")
            first = self.manager.get(sid)
            await first.context.add_cookies([{
                "name": "identity", "value": "saved-before-run", "url": site.base_url,
            }])
            await self.client.delete(f"/sessions/{sid}", headers={
                "X-Profile-Generation": "1", "X-Fencing-Token": "2",
            })
            sid = await self.open("env_a", token=3)
            active = self.manager.get(sid)
            await active.context.add_cookies([{
                "name": "identity", "value": "changed-during-run", "url": site.base_url,
            }])
            before = (self.root / "profiles/env_a.profile").read_bytes()
            browser = await self.manager.browser()
            original = browser.new_context
            observed = []
            async def capture_context(**kwargs):
                context = await original(**kwargs)
                observed.extend(await context.cookies())
                return context
            with patch.object(browser, "new_context", capture_context):
                result = await self.client.post("/execute", json={
                    "session_id": "sess_browser", "project_id": "prj_a",
                    "environment_id": "env_a", "profile_generation": 1, "fencing_token": 3,
                    "browser_session_id": sid,
                    "case": build_case([goto_step(0, site.url("index.html"), "/index.html")]),
                })
            self.assertEqual(result.status_code, 200, result.text)
            self.assertEqual(result.json()["status"], "passed", result.text)
            self.assertEqual([cookie["value"] for cookie in observed], ["saved-before-run"])
            self.assertEqual((await active.context.cookies())[0]["value"], "changed-during-run")
            self.assertEqual(before, (self.root / "profiles/env_a.profile").read_bytes())

    async def test_uncertain_dryrun_cleanup_quarantines_worker_and_denies_release_ack(self):
        with LocalSite() as site:
            await self.provision("env_a")
            sid = await self.open("env_a")
            session = self.manager.get(sid)
            browser = await self.manager.browser()
            context = await browser.new_context()
            async def failing_close():
                raise RuntimeError("fresh context disposal failed")
            payload = {
                "session_id": "sess_browser", "environment_id": "env_a",
                "project_id": "prj_a", "browser_session_id": sid,
                "profile_generation": 1, "fencing_token": 2,
                "case": build_case([goto_step(0, site.url("index.html"), "/index.html")]),
            }
            try:
                with patch.object(browser, "new_context", return_value=context), patch.object(
                    context, "close", failing_close
                ):
                    result = await self.client.post("/execute", json=payload)
                self.assertEqual(result.status_code, 200, result.text)
                self.assertEqual(result.json()["status"], "error", result.text)
                self.assertIn("cleanup_uncertain", result.json()["error"]["message"])
                health = await self.client.get("/health")
                self.assertEqual(health.status_code, 503, health.text)
                self.assertEqual(health.json()["status"], "not_ready")
                self.assertEqual((await self.client.post("/execute", json=payload)).status_code, 503)
                self.assertEqual((await self.client.post("/sessions", json={
                    "session_id": "sess_new", "project_id": "prj_a",
                    "environment_id": "env_a", "profile_generation": 1, "fencing_token": 2,
                })).status_code, 503)
                released = await self.client.delete(f"/sessions/{sid}", headers={
                    "X-Profile-Generation": "1", "X-Fencing-Token": "2",
                })
                self.assertEqual(released.status_code, 503, released.text)
                self.assertEqual(released.json()["error"], "cleanup_uncertain")
                self.assertNotIn(session.context, browser.contexts)
                self.assertEqual((await self.client.get("/health")).status_code, 503)
            finally:
                await context.close()

    async def test_managed_failed_dryrun_reopens_same_lease_for_repair(self):
        with LocalSite() as site:
            self.client.headers["X-Project-ID"] = "prj_browser"
            provisioned = await self.client.post("/environments/env_a/provision", json={
                "project_id": "prj_browser", "profile_generation": 1, "fencing_token": 1,
                "policy": {"allowed_domains": ["127.0.0.1"]},
            })
            self.assertEqual(provisioned.status_code, 200, provisioned.text)
            identity = {
                "project_id": "prj_browser", "session_id": "sess_browser",
                "environment_id": "env_a", "profile_generation": 1, "fencing_token": 2,
            }
            headers = {
                "X-Project-ID": "prj_browser", "X-Environment-ID": "env_a",
                "X-Profile-Generation": "1", "X-Fencing-Token": "2",
            }
            opened = await self.client.post("/sessions", json=identity)
            self.assertEqual(opened.status_code, 200, opened.text)
            old = opened.json()["session_id"]
            failed_case = build_case([
                goto_step(0, site.url("index.html"), "/index.html"),
                action_step(
                    1, "assert_text", page_url=site.url("index.html"), value="missing result",
                    pre=[condition("url_contains", "/index.html")],
                    post=[condition("text_visible", "missing result", 100)],
                ),
            ])
            failed = await self.client.post("/execute", json={
                **identity, "browser_session_id": old, "case": failed_case,
            })
            self.assertEqual(failed.status_code, 200, failed.text)
            self.assertEqual(failed.json()["status"], "failed")
            self.assertEqual((await self.client.delete(
                f"/sessions/{old}", headers=headers,
            )).status_code, 200)
            reopened = await self.client.post("/sessions", json=identity)
            self.assertEqual(reopened.status_code, 200, reopened.text)
            current = reopened.json()["session_id"]
            self.assertNotEqual(current, old)
            stale = await self.client.post("/execute", json={
                **identity, "browser_session_id": old, "case": failed_case,
            })
            self.assertEqual(stale.status_code, 409, stale.text)
            navigated = await self.client.post(f"/sessions/{current}/navigate", headers=headers, json={
                "url": site.url("index.html"),
            })
            self.assertEqual(navigated.status_code, 200, navigated.text)
            acted = await self.client.post(f"/sessions/{current}/act", headers=headers, json={
                "action": "input", "locator": {"kind": "css", "css": "#search"}, "value": "alpha",
            })
            self.assertEqual(acted.status_code, 200, acted.text)
            repaired = build_case([
                goto_step(0, site.url("index.html"), "/index.html"),
                action_step(
                    1, "input", page_url=site.url("index.html"), locator=css_locator("#search"),
                    value="alpha", pre=[condition("url_contains", "/index.html")],
                    post=[condition("value_equals", "alpha")],
                ),
            ])
            passed = await self.client.post("/execute", json={
                **identity, "browser_session_id": current, "case": repaired,
            })
            self.assertEqual(passed.status_code, 200, passed.text)
            self.assertEqual(passed.json()["status"], "passed", passed.text)
            self.assertEqual((await self.client.delete(
                f"/sessions/{current}", headers=headers,
            )).status_code, 200)
            self.assertEqual((await self.client.get("/health")).status_code, 200)
