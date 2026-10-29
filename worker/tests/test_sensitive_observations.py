"""Password values must never leave the browser in observations."""

import unittest
from unittest.mock import patch

from playwright.async_api import Error as PlaywrightError

from loop_worker.actions import ActionFailure, fill_target
from loop_worker.observer import ObservationRecorder, observe_page
from tests.managed_api import api_client
from tests.pw_support import launched_browser


CANARY = "password-canary-do-not-export-7429"
FORM = (
    "<form><label for='password'>Password</label>"
    "<input id='password' type='PASSWORD'>"
    "<input id='username' aria-label='Username' value='public-user'>"
    "<button>Sign in</button></form>"
)


class SensitiveObservationTest(unittest.IsolatedAsyncioTestCase):
    async def test_password_is_redacted_before_raw_collection_leaves_chromium(self):
        async with launched_browser() as browser:
            page = await browser.new_page()
            await page.set_content(FORM)
            await page.locator("#password").fill(CANARY)
            recorder = ObservationRecorder(page)
            try:
                raw = await recorder.collect_raw()
            finally:
                await recorder.cleanup()
            password = next(
                element for element in raw["elements"]
                if element["attributes"].get("id") == "password"
            )
            self.assertIsNone(password["value"])
            self.assertNotIn(CANARY, str(raw))
            self.assertEqual(await page.locator("#password").input_value(), CANARY)
            observation = await observe_page(page)
            self.assertNotIn(CANARY, observation.model_dump_json())
            password = next(
                element for element in observation.elements
                if element.attributes.get("id") == "password"
            )
            self.assertIsNone(password.value)
            self.assertTrue(password.locators)
            self.assertTrue(any(
                candidate.target_ref == password.ref and candidate.action == "input"
                for candidate in observation.action_candidates
            ))
            username = next(
                element for element in observation.elements
                if element.attributes.get("id") == "username"
            )
            self.assertEqual(username.value, "public-user")

    async def test_http_act_observation_does_not_echo_password_canary(self):
        async with api_client() as client:
            opened = await client.open("sess_password")
            self.assertEqual(opened.status_code, 200, opened.text)
            sid = opened.json()["session_id"]
            page = client.manager.get(sid).page
            await page.set_content(FORM)
            response = await client.post(f"/sessions/{sid}/act", json={
                "action": "input", "locator": {"kind": "css", "css": "#password"},
                "value": CANARY,
            })
            self.assertEqual(response.status_code, 200, response.text)
            self.assertEqual(response.json()["status"], "passed")
            self.assertNotIn(CANARY, response.text)
            password = next(
                element for element in response.json()["observation"]["elements"]
                if element["attributes"].get("id") == "password"
            )
            self.assertIsNone(password["value"])
            self.assertEqual(await page.locator("#password").input_value(), CANARY)
            screenshot = (
                client.root / "artifacts" / response.json()["observation"]["screenshot_path"]
            )
            self.assertTrue(screenshot.read_bytes().startswith(b"\x89PNG\r\n\x1a\n"))
            await page.locator("#password").fill("x" * len(CANARY))
            self.assertEqual(
                screenshot.read_bytes(), await page.screenshot(full_page=True),
                "password screenshot must not reveal the typed characters",
            )
            for expected, status in ((CANARY, "passed"), ("wrong-password", "failed")):
                checked = await client.post(f"/sessions/{sid}/act", json={
                    "action": "input", "locator": {"kind": "css", "css": "#password"},
                    "value": CANARY, "postconditions": [{
                        "type": "value_equals", "value": expected, "timeout_ms": 100,
                    }],
                })
                self.assertEqual(checked.status_code, 200, checked.text)
                self.assertEqual(checked.json()["status"], status)
                self.assertNotIn(CANARY, checked.text)
                self.assertNotIn("wrong-password", checked.text)
                self.assertEqual(checked.json()["conditions"][0]["value"], "[redacted]")
                self.assertEqual(await page.locator("#password").input_value(), CANARY)

    async def test_actual_password_fill_error_does_not_export_playwright_argument_text(self):
        async with launched_browser() as browser:
            page = await browser.new_page()
            await page.set_content(FORM)
            locator = page.locator("#password")
            async def fail_press(*args, **kwargs):
                raise PlaywrightError(f"input action text: {CANARY}")
            with patch.object(locator, "press", fail_press):
                with self.assertRaises(ActionFailure) as failed:
                    await fill_target(page, locator, CANARY, 1000, submit=True)
            self.assertEqual(await locator.input_value(), CANARY)
            self.assertNotIn(CANARY, failed.exception.detail)
