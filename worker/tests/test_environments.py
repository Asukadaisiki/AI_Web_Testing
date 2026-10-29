"""Provider HTTP contract and fencing tests; no Chromium in this module."""

import asyncio
import base64
import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import httpx

from loop_worker.main import create_app
from tests.case_builder import build_case, goto_step

KEY = base64.urlsafe_b64encode(b"k" * 32).decode()
AUTH = {"Authorization": "Bearer internal-test-token", "X-Project-ID": "prj_a"}


class MemoryPage:
    url = "about:blank"
    viewport_size = {"width": 1280, "height": 800}

    def __init__(self, context):
        self.context = context
        self.keyboard = self
        self.mouse = self
        self.inputs = []

    def on(self, *args):
        pass

    def set_default_timeout(self, value):
        pass

    def set_default_navigation_timeout(self, value):
        pass

    async def screenshot(self, **kwargs):
        return b"\x89PNG\r\n\x1a\n"

    async def click(self, x, y, **kwargs):
        self.inputs.append(("click", x, y))

    async def press(self, key):
        self.inputs.append(("key", key))

    async def insert_text(self, text):
        self.inputs.append(("type", text))

    async def goto(self, url, **kwargs):
        self.url = url


class MemoryContext:
    def __init__(self, state):
        self.state = state or {"cookies": [], "origins": []}
        self.closed = False

    async def route(self, *args):
        pass

    async def route_web_socket(self, *args):
        pass

    async def new_page(self):
        self.page = MemoryPage(self)
        return self.page

    async def storage_state(self, **kwargs):
        return self.state

    async def close(self):
        self.closed = True


class MemoryBrowser:
    async def new_context(self, **kwargs):
        return MemoryContext(kwargs.get("storage_state"))


class EnvironmentTest(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.root = Path(self.directory.name).resolve()
        self.config = patch.dict(os.environ, {
            "LOOP_ENVIRONMENTS_DIR": str(self.root),
            "LOOP_ENVIRONMENT_ENCRYPTION_KEY": KEY,
            "LOOP_WORKER_INTERNAL_TOKEN": "internal-test-token",
        })
        self.config.start()
        await self.start_app()

    async def start_app(self):
        self.app = create_app()
        self.manager = self.app.state.session_manager
        self.browser_patch = patch.object(self.manager, "browser", MemoryBrowserFactory())
        self.browser_patch.start()
        self.client = httpx.AsyncClient(
            transport=httpx.ASGITransport(app=self.app, raise_app_exceptions=False),
            base_url="http://worker.test", headers=AUTH,
        )

    async def stop_app(self):
        await self.client.aclose()
        await self.manager.shutdown()
        self.browser_patch.stop()

    async def asyncTearDown(self):
        await self.stop_app()
        self.config.stop()
        self.directory.cleanup()

    async def provision(self, environment="env_a", project="prj_a"):
        response = await self.client.post(f"/environments/{environment}/provision", json={
            "profile_generation": 1, "fencing_token": 1,
            "policy": {"allowed_domains": ["example.test"]},
            **({"project_id": project} if project is not None else {}),
        })
        self.assertEqual(response.status_code, 200, response.text)
        return response.json()

    async def open(self, environment="env_a", generation=1, token=2, project="prj_a"):
        response = await self.client.post("/sessions", json={
            "session_id": "sess_test", "environment_id": environment,
            "profile_generation": generation, "fencing_token": token,
            **({"project_id": project} if project is not None else {}),
        })
        self.assertEqual(response.status_code, 200, response.text)
        return response.json()["session_id"]

    def path(self, sid, suffix):
        return f"/environments/env_a/sessions/{sid}/{suffix}"

    def headers(self, generation=1, token=2):
        return {"X-Profile-Generation": str(generation), "X-Fencing-Token": str(token)}

    async def close(self, sid, generation=1, token=2):
        return await self.client.delete(
            f"/sessions/{sid}", headers=self.headers(generation, token)
        )

    async def test_internal_auth_and_disabled_configuration_fail_closed(self):
        for headers in ({}, {"Authorization": "Bearer wrong"}):
            async with httpx.AsyncClient(
                transport=httpx.ASGITransport(app=self.app), base_url="http://worker.test",
                headers=headers,
            ) as client:
                response = await client.post("/environments/env_a/provision", json={})
                self.assertEqual(response.status_code, 401, response.text)
        with patch.dict(os.environ, {"LOOP_WORKER_INTERNAL_TOKEN": ""}):
            app = create_app()
            async with httpx.AsyncClient(
                transport=httpx.ASGITransport(app=app), base_url="http://worker.test"
            ) as client:
                response = await client.get("/environments/env_a")
                self.assertEqual(response.status_code, 503, response.text)
            await app.state.session_manager.shutdown()

    async def test_provision_is_idempotent_and_metadata_never_exposes_state(self):
        first = await self.provision()
        self.assertEqual(first, await self.provision())
        self.assertEqual(first["profile_generation"], 1)
        self.assertNotIn("storage_state", first)
        self.assertNotIn(str(self.root), str(first))

    async def test_scoped_open_requires_all_identity_fields_and_auth(self):
        response = await self.client.post("/sessions", json={
            "session_id": "sess_test", "environment_id": "env_a",
        })
        self.assertEqual(response.status_code, 400, response.text)
        await self.provision()
        async with httpx.AsyncClient(
            transport=httpx.ASGITransport(app=self.app), base_url="http://worker.test"
        ) as client:
            response = await client.post("/sessions", json={
                "session_id": "sess_test", "environment_id": "env_a",
                "project_id": "prj_a",
                "profile_generation": 1, "fencing_token": 2,
            })
            self.assertEqual(response.status_code, 401, response.text)

    async def test_managed_contract_requires_project_on_provision_and_sessions(self):
        provision = await self.client.post("/environments/env_a/provision", json={
            "profile_generation": 1, "fencing_token": 1,
            "policy": {"allowed_domains": ["example.test"]},
        })
        self.assertEqual(provision.status_code, 400, provision.text)
        await self.provision(project="prj_a")
        opened = await self.client.post("/sessions", json={
            "session_id": "sess_test", "environment_id": "env_a",
            "profile_generation": 1, "fencing_token": 2,
        })
        self.assertEqual(opened.status_code, 400, opened.text)

    async def test_exclusive_contexts_and_stale_lease_tokens(self):
        await self.provision()
        results = await asyncio.gather(*[
            self.client.post("/sessions", json={
                "session_id": "sess_test", "environment_id": "env_a",
                "project_id": "prj_a",
                "profile_generation": 1, "fencing_token": 2,
            }) for _ in range(2)
        ])
        self.assertEqual(sorted(r.status_code for r in results), [200, 409])
        sid = next(r.json()["session_id"] for r in results if r.status_code == 200)
        self.assertEqual((await self.close(sid)).status_code, 200)
        replay = await self.client.post("/sessions", json={
            "session_id": "sess_test", "environment_id": "env_a",
            "project_id": "prj_a",
            "profile_generation": 1, "fencing_token": 2,
        })
        self.assertEqual(replay.status_code, 200, replay.text)
        self.assertNotEqual(replay.json()["session_id"], sid)
        stale = await self.client.post("/sessions", json={
            "session_id": "sess_test", "environment_id": "env_a",
            "project_id": "prj_a",
            "profile_generation": 1, "fencing_token": 1,
        })
        self.assertEqual(stale.status_code, 409, stale.text)

    async def test_encrypted_persistence_isolated_and_restored_after_restart(self):
        await self.provision()
        await self.provision("env_b")
        a = await self.open()
        b = await self.open("env_b")
        self.manager.get(a).context.state["cookies"].append({"name": "identity", "value": "secret"})
        self.assertEqual(self.manager.get(b).context.state["cookies"], [])
        self.assertEqual((await self.close(a)).status_code, 200)
        self.assertEqual((await self.close(b)).status_code, 200)
        for file in self.root.iterdir():
            if file.is_file():
                self.assertNotIn(b"secret", file.read_bytes())
                self.assertNotIn(b"cookies", file.read_bytes())
                self.assertEqual(file.stat().st_mode & 0o777, 0o600)
        await self.stop_app()
        await self.start_app()
        restored = await self.open(token=3)
        self.assertEqual(self.manager.get(restored).context.state["cookies"][0]["value"], "secret")

    async def test_reset_fences_old_writes_and_removes_identity(self):
        await self.provision()
        old = await self.open()
        old_context = self.manager.get(old).context
        old_context.state["cookies"] = [{"name": "identity", "value": "old"}]
        reset = await self.client.post("/environments/env_a/reset", json={
            "profile_generation": 2, "fencing_token": 3,
        })
        self.assertEqual(reset.status_code, 200, reset.text)
        self.assertTrue(old_context.closed)
        stale = await self.close(old)
        self.assertIn(stale.status_code, (404, 409))
        current = await self.open(generation=2, token=4)
        self.assertEqual(self.manager.get(current).context.state["cookies"], [])
        stale_create = await self.client.post("/sessions", json={
            "session_id": "sess_test", "environment_id": "env_a",
            "project_id": "prj_a",
            "profile_generation": 1, "fencing_token": 10,
        })
        self.assertEqual(stale_create.status_code, 409, stale_create.text)

    async def test_shutdown_discards_unacknowledged_state(self):
        await self.provision()
        old = await self.open()
        self.manager.get(old).context.state["cookies"] = [{"name": "identity", "value": "lost"}]
        await self.stop_app()
        await self.start_app()
        restored = await self.open(token=3)
        self.assertEqual(self.manager.get(restored).context.state["cookies"], [])

    async def test_delete_requires_release_and_retains_fencing_tombstone(self):
        await self.provision()
        sid = await self.open()
        response = await self.client.request("DELETE", "/environments/env_a", json={
            "profile_generation": 1, "fencing_token": 3,
        })
        self.assertEqual(response.status_code, 409, response.text)
        await self.close(sid)
        response = await self.client.request("DELETE", "/environments/env_a", json={
            "profile_generation": 1, "fencing_token": 3,
        })
        self.assertEqual(response.status_code, 200, response.text)
        response = await self.client.post("/environments/env_a/provision", json={
            "project_id": "prj_a",
            "profile_generation": 1, "fencing_token": 1,
            "policy": {"allowed_domains": ["example.test"]},
        })
        self.assertEqual(response.status_code, 409, response.text)

    async def test_control_screenshot_input_scope_validation_and_domain_policy(self):
        await self.provision()
        sid = await self.open()
        screenshot = await self.client.get(self.path(sid, "screenshot"), headers=self.headers())
        self.assertEqual(screenshot.status_code, 200, screenshot.text)
        self.assertEqual(screenshot.headers["content-type"], "image/png")
        self.assertIn("no-store", screenshot.headers["cache-control"])
        for payload in (
            {"action": "click", "x": 1280, "y": 2},
            {"action": "click", "x": -1, "y": 2},
            {"action": "key", "key": "Meta+R"},
            {"action": "type", "text": "x" * 4097},
            {"action": "navigate", "url": "file:///etc/passwd"},
            {"action": "navigate", "url": "https://example.test.evil/"},
            {"action": "navigate", "url": "https://user:pass@example.test/"},
            {"action": "evaluate", "text": "alert(1)"},
            {"action": "type", "text": "ok", "key": "Enter"},
        ):
            response = await self.client.post(self.path(sid, "input"), json={
                "profile_generation": 1, "fencing_token": 2, **payload,
            })
            self.assertEqual(response.status_code, 400, response.text)
        page = self.manager.get(sid).page
        self.assertEqual(page.inputs, [])
        for payload in (
            {"action": "click", "x": 10, "y": 20},
            {"action": "key", "key": "Tab"},
            {"action": "type", "text": "login"},
            {"action": "navigate", "url": "https://example.test/login"},
        ):
            response = await self.client.post(self.path(sid, "input"), json={
                "profile_generation": 1, "fencing_token": 2, **payload,
            })
            self.assertEqual(response.status_code, 200, response.text)
        self.assertEqual(page.inputs, [("click", 10, 20), ("key", "Tab"), ("type", "login")])
        self.assertEqual(page.url, "https://example.test/login")
        stale = await self.client.get(self.path(sid, "screenshot"), headers=self.headers(token=1))
        self.assertEqual(stale.status_code, 409, stale.text)
        cross = await self.client.get(
            f"/environments/env_b/sessions/{sid}/screenshot", headers=self.headers()
        )
        self.assertEqual(cross.status_code, 409, cross.text)
        unscoped = await self.client.delete(f"/sessions/{sid}")
        self.assertEqual(unscoped.status_code, 400, unscoped.text)

    async def test_other_provider_cannot_claim_or_reset_an_active_context(self):
        await self.provision()
        sid = await self.open()
        other = create_app()
        async with httpx.AsyncClient(
            transport=httpx.ASGITransport(app=other, raise_app_exceptions=False),
            base_url="http://worker.test", headers=AUTH,
        ) as client:
            opened = await client.post("/sessions", json={
                "session_id": "sess_other", "environment_id": "env_a",
                "project_id": "prj_a",
                "profile_generation": 1, "fencing_token": 3,
            })
            self.assertEqual(opened.status_code, 409, opened.text)
            reset = await client.post("/environments/env_a/reset", json={
                "profile_generation": 2, "fencing_token": 3,
            })
            self.assertEqual(reset.status_code, 409, reset.text)
        await other.state.session_manager.shutdown()
        self.assertEqual((await self.close(sid)).status_code, 200)

    async def test_corrupted_or_wrong_key_profiles_fail_closed(self):
        await self.provision()
        profile = self.root / "env_a.profile"
        original = profile.read_bytes()
        profile.write_bytes(b"not authenticated ciphertext")
        response = await self.client.get("/environments/env_a")
        self.assertEqual(response.status_code, 409, response.text)
        self.assertEqual(response.json()["error"], "environment_broken")
        profile.write_bytes(original)
        with patch.dict(os.environ, {
            "LOOP_ENVIRONMENT_ENCRYPTION_KEY": base64.urlsafe_b64encode(b"z" * 32).decode(),
        }):
            other = create_app()
            async with httpx.AsyncClient(
                transport=httpx.ASGITransport(app=other), base_url="http://worker.test", headers=AUTH,
            ) as client:
                response = await client.get("/environments/env_a")
                self.assertEqual(response.status_code, 409, response.text)
            await other.state.session_manager.shutdown()

    async def test_failed_context_close_does_not_persist_or_acknowledge(self):
        await self.provision()
        sid = await self.open()
        context = self.manager.get(sid).context
        context.state["cookies"] = [{"name": "identity", "value": "not-acknowledged"}]
        async def fail_close():
            raise RuntimeError("close failed")
        with patch.object(context, "close", fail_close), self.assertLogs("loop_worker", "ERROR"):
            response = await self.close(sid)
        self.assertEqual(response.status_code, 500, response.text)
        record = self.manager.environments.read("env_a")
        self.assertEqual(record["storage_state"]["cookies"], [])
        self.assertEqual(record["state"], "leased")

    async def test_reset_replay_retries_failed_context_disposal(self):
        await self.provision()
        sid = await self.open()
        context = self.manager.get(sid).context
        async def fail_close():
            raise RuntimeError("close failed")
        with patch.object(context, "close", fail_close), self.assertLogs("loop_worker", "ERROR"):
            failed = await self.client.post("/environments/env_a/reset", json={
                "profile_generation": 2, "fencing_token": 3,
            })
        self.assertEqual(failed.status_code, 500, failed.text)
        with self.assertRaisesRegex(Exception, "stale"):
            await self.manager.close(sid)
        retry = await self.client.post("/environments/env_a/reset", json={
            "profile_generation": 2, "fencing_token": 3,
        })
        self.assertEqual(retry.status_code, 200, retry.text)
        self.assertTrue(context.closed)
        await self.open(generation=2, token=4)

    async def test_reset_waits_for_close_then_discards_closed_identity(self):
        await self.provision()
        sid = await self.open()
        context = self.manager.get(sid).context
        context.state["cookies"] = [{"name": "identity", "value": "old"}]
        entered, release = asyncio.Event(), asyncio.Event()
        original = context.storage_state
        async def blocked_state(**kwargs):
            entered.set()
            await release.wait()
            return await original(**kwargs)
        with patch.object(context, "storage_state", blocked_state):
            closing = asyncio.create_task(self.close(sid))
            await entered.wait()
            resetting = asyncio.create_task(self.client.post("/environments/env_a/reset", json={
                "profile_generation": 2, "fencing_token": 3,
            }))
            await asyncio.sleep(0.01)
            self.assertFalse(resetting.done())
            release.set()
            self.assertEqual((await closing).status_code, 200)
            self.assertEqual((await resetting).status_code, 200)
        current = await self.open(generation=2, token=4)
        self.assertEqual(self.manager.get(current).context.state["cookies"], [])

    async def test_reset_and_delete_clean_only_the_managed_generation_uploads(self):
        uploads = self.root / "uploads"
        uploads.mkdir()
        outside = self.root / "outside.txt"
        outside.write_bytes(b"keep")
        with patch.dict(os.environ, {"LOOP_UPLOADS_DIR": str(uploads)}):
            await self.provision()
            old = uploads / "env_a_g1"
            self.assertTrue(old.is_dir())
            (old / "upload").write_bytes(b"private")
            (old / "link").symlink_to(outside)
            reset = await self.client.post("/environments/env_a/reset", json={
                "profile_generation": 2, "fencing_token": 2,
            })
            self.assertEqual(reset.status_code, 200, reset.text)
            self.assertFalse(old.exists())
            self.assertTrue((uploads / "env_a_g2").is_dir())
            deleted = await self.client.request("DELETE", "/environments/env_a", json={
                "profile_generation": 2, "fencing_token": 3,
            })
            self.assertEqual(deleted.status_code, 200, deleted.text)
            self.assertFalse((uploads / "env_a_g2").exists())
            self.assertEqual(outside.read_bytes(), b"keep")

    async def test_managed_execute_requires_auth_current_lease_and_complete_identity(self):
        await self.provision()
        sid = await self.open()
        payload = {
            "session_id": "sess_test", "environment_id": "env_a",
            "project_id": "prj_a", "browser_session_id": sid,
            "profile_generation": 1, "fencing_token": 1,
            "case": build_case([goto_step(0, "https://example.test/", "example.test")]),
        }
        stale = await self.client.post("/execute", json=payload)
        self.assertEqual(stale.status_code, 409, stale.text)
        partial = await self.client.post("/execute", json={
            key: value for key, value in payload.items() if key != "fencing_token"
        })
        self.assertEqual(partial.status_code, 400, partial.text)
        omitted = await self.client.post("/execute", json={
            "session_id": "sess_test", "case": payload["case"],
        })
        self.assertEqual(omitted.status_code, 400, omitted.text)
        payload["fencing_token"] = 2
        async with httpx.AsyncClient(
            transport=httpx.ASGITransport(app=self.app), base_url="http://worker.test"
        ) as client:
            response = await client.post("/execute", json=payload)
            self.assertEqual(response.status_code, 401, response.text)

    async def test_execute_holds_lease_guard_until_runner_releases(self):
        await self.provision()
        sid = await self.open()
        entered, release = asyncio.Event(), asyncio.Event()
        async def blocked_runner(*args, **kwargs):
            entered.set()
            await release.wait()
            from loop_worker.contracts import ExecutionResult
            return ExecutionResult(
                execution_id="exec_test", status="passed", started_at="now",
                finished_at="now", final_url="about:blank",
            )
        with patch("loop_worker.main.run_case", blocked_runner):
            executing = asyncio.create_task(self.client.post("/execute", json={
                "session_id": "sess_test", "environment_id": "env_a",
                "project_id": "prj_a", "browser_session_id": sid,
                "profile_generation": 1, "fencing_token": 2,
                "case": build_case([goto_step(0, "https://example.test/", "example.test")]),
            }))
            await entered.wait()
            resetting = asyncio.create_task(self.client.post("/environments/env_a/reset", json={
                "profile_generation": 2, "fencing_token": 3,
            }))
            await asyncio.sleep(0.01)
            self.assertFalse(resetting.done())
            release.set()
            self.assertEqual((await executing).status_code, 200)
            self.assertEqual((await resetting).status_code, 200)

    async def test_node_managed_payload_accepts_and_validates_project_and_incarnation(self):
        await self.provision(project="prj_a")
        sid = await self.open(project="prj_a")
        identity = {
            "session_id": "sess_test", "project_id": "prj_a", "environment_id": "env_a",
            "profile_generation": 1, "fencing_token": 2, "browser_session_id": sid,
            "case": build_case([goto_step(0, "https://example.test/", "example.test")]),
        }
        from loop_worker.contracts import ExecutionResult
        async def completed_runner(*args, **kwargs):
            return ExecutionResult(
                execution_id="exec_node", status="passed", started_at="now",
                finished_at="now", final_url="https://example.test/",
            )
        with patch("loop_worker.main.run_case", completed_runner):
            accepted = await self.client.post("/execute", json=identity)
            self.assertEqual(accepted.status_code, 200, accepted.text)
            self.assertEqual(accepted.json()["status"], "passed")
            for changes in (
                {"project_id": "prj_b"}, {"browser_session_id": "bsess_old"},
                {"session_id": "sess_other"}, {"environment_id": "env_b"},
                {"profile_generation": 2}, {"fencing_token": 3},
            ):
                with self.subTest(changes=changes):
                    denied = await self.client.post("/execute", json={**identity, **changes})
                    self.assertEqual(denied.status_code, 409, denied.text)
            for missing in ("project_id", "browser_session_id"):
                denied = await self.client.post("/execute", json={
                    key: value for key, value in identity.items() if key != missing
                })
                self.assertIn(denied.status_code, (400, 409), denied.text)
        headers = {
            **self.headers(), "X-Project-ID": "prj_a", "X-Environment-ID": "env_a",
        }
        screenshot = await self.client.get(self.path(sid, "screenshot"), headers=headers)
        self.assertEqual(screenshot.status_code, 200, screenshot.text)
        page = self.manager.get(sid).page
        for changes in (
            {"X-Project-ID": "prj_b"}, {"X-Project-ID": ""},
            {"X-Environment-ID": "env_b"},
        ):
            with self.subTest(changes=changes):
                forged = {**headers, **changes}
                screenshot = await self.client.get(self.path(sid, "screenshot"), headers=forged)
                self.assertIn(screenshot.status_code, (400, 409), screenshot.text)
                for method, path, payload in (
                    ("POST", f"/sessions/{sid}/navigate", {"url": "https://example.test/"}),
                    ("POST", f"/sessions/{sid}/act", {
                        "action": "input", "locator": {"kind": "css", "css": "#login"},
                        "value": "not-authorized",
                    }),
                    ("POST", self.path(sid, "input"), {
                        "profile_generation": 1, "fencing_token": 2,
                        "action": "type", "text": "not-authorized",
                    }),
                    ("DELETE", f"/sessions/{sid}", None),
                ):
                    response = await self.client.request(
                        method, path, headers=forged,
                        **({"json": payload} if payload is not None else {}),
                    )
                    self.assertIn(response.status_code, (400, 409), response.text)
                self.assertEqual(page.inputs, [])
                self.assertEqual(page.url, "about:blank")
        closed = await self.client.delete(f"/sessions/{sid}", headers=headers)
        self.assertEqual(closed.status_code, 200, closed.text)

    async def test_project_owner_cannot_be_forged_or_rebound_after_restart(self):
        await self.provision(project="prj_a")
        before = (self.root / "env_a.profile").read_bytes()
        for project in ("prj_b", None):
            denied = await self.client.post("/sessions", json={
                "session_id": "sess_test", "environment_id": "env_a",
                "profile_generation": 1, "fencing_token": 2,
                **({"project_id": project} if project is not None else {}),
            })
            self.assertEqual(denied.status_code, 409 if project is not None else 400, denied.text)
        self.assertEqual((self.root / "env_a.profile").read_bytes(), before)
        rebound = await self.client.post("/environments/env_a/provision", json={
            "project_id": "prj_b", "profile_generation": 1, "fencing_token": 1,
            "policy": {"allowed_domains": ["example.test"]},
        })
        self.assertEqual(rebound.status_code, 409, rebound.text)
        await self.stop_app()
        await self.start_app()
        await self.open(project="prj_a")

    async def test_same_lease_reopen_requires_clean_close_same_owner_and_new_handle(self):
        await self.provision(project="prj_a")
        sid = await self.open(project="prj_a")
        old = self.manager.get(sid)
        old.context.state["cookies"] = [{"name": "identity", "value": "retained"}]
        headers = {**self.headers(), "X-Project-ID": "prj_a", "X-Environment-ID": "env_a"}
        self.assertEqual((await self.client.delete(f"/sessions/{sid}", headers=headers)).status_code, 200)
        for changes in ({"session_id": "sess_other"}, {"project_id": "prj_b"}, {"fencing_token": 1}):
            denied = await self.client.post("/sessions", json={
                "session_id": "sess_test", "project_id": "prj_a", "environment_id": "env_a",
                "profile_generation": 1, "fencing_token": 2, **changes,
            })
            self.assertEqual(denied.status_code, 409, denied.text)
        current = await self.open(project="prj_a")
        self.assertNotEqual(current, sid)
        self.assertEqual(self.manager.get(current).context.state["cookies"][0]["value"], "retained")
        denied = await self.client.post("/execute", json={
            "session_id": "sess_test", "project_id": "prj_a", "environment_id": "env_a",
            "profile_generation": 1, "fencing_token": 2, "browser_session_id": sid,
            "case": build_case([goto_step(0, "https://example.test/", "example.test")]),
        })
        self.assertEqual(denied.status_code, 409, denied.text)
        denied = await self.client.delete(f"/sessions/{sid}", headers=headers)
        self.assertEqual(denied.status_code, 404, denied.text)
        with self.assertRaisesRegex(Exception, "stale"):
            async with self.manager.environments.guard(old, 1, 2, "env_a", "prj_a"):
                self.fail("old incarnation entered the guard")

    async def test_clean_closed_lease_can_reopen_after_provider_restart(self):
        await self.provision(project="prj_a")
        sid = await self.open(project="prj_a")
        response = await self.client.delete(f"/sessions/{sid}", headers={
            **self.headers(), "X-Project-ID": "prj_a",
        })
        self.assertEqual(response.status_code, 200, response.text)
        await self.stop_app()
        await self.start_app()
        self.assertNotEqual(await self.open(project="prj_a"), sid)

    async def test_same_fence_reopen_after_unacknowledged_shutdown_is_denied(self):
        await self.provision(project="prj_a")
        await self.open(project="prj_a")
        await self.stop_app()
        await self.start_app()
        denied = await self.client.post("/sessions", json={
            "session_id": "sess_test", "project_id": "prj_a", "environment_id": "env_a",
            "profile_generation": 1, "fencing_token": 2,
        })
        self.assertEqual(denied.status_code, 409, denied.text)
        await self.open(project="prj_a", token=3)

    async def test_reset_fence_cannot_reopen_the_previous_lease_incarnation(self):
        await self.provision()
        await self.open()
        reset = await self.client.post("/environments/env_a/reset", json={
            "profile_generation": 2, "fencing_token": 3,
        })
        self.assertEqual(reset.status_code, 200, reset.text)
        denied = await self.client.post("/sessions", json={
            "session_id": "sess_test", "project_id": "prj_a", "environment_id": "env_a",
            "profile_generation": 2, "fencing_token": 3,
        })
        self.assertEqual(denied.status_code, 409, denied.text)
        await self.open(generation=2, token=4)


class MemoryBrowserFactory:
    async def __call__(self):
        return MemoryBrowser()
