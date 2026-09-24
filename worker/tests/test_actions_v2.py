"""Generic Action/Condition v2 executor behavior."""

from __future__ import annotations

import sys
import tempfile
import unittest
from pathlib import Path

WORKER_ROOT = Path(__file__).resolve().parents[1]
TESTS_DIR = Path(__file__).resolve().parent
for _path in (str(WORKER_ROOT), str(TESTS_DIR)):
    if _path not in sys.path:
        sys.path.insert(0, _path)

from local_site import LocalSite  # noqa: E402
from pw_support import launched_browser, run  # noqa: E402

from loop_worker import actions  # noqa: E402
from loop_worker.conditions import evaluate_condition  # noqa: E402
from loop_worker.contracts import Condition, LocatorSpec  # noqa: E402
from loop_worker.runner import run_case  # noqa: E402


class GenericActionV2Test(unittest.TestCase):
    def test_form_actions_and_target_assertions_execute_in_browser(self) -> None:
        run(self._form_actions_and_target_assertions_execute_in_browser())

    async def _form_actions_and_target_assertions_execute_in_browser(self) -> None:
        async with launched_browser() as browser:
            page = await browser.new_page()
            await page.set_content(
                """
                <select aria-label="Status"><option>Draft</option><option>Active</option></select>
                <label><input aria-label="Public" type="checkbox"> Public</label>
                <button id="menu" title="Actions">Actions</button>
                <dialog open><button aria-label="Close">x</button><p>Modal text</p></dialog>
                <ul><li class="row">One</li><li class="row">Two</li><li class="row">Three</li></ul>
                <input aria-label="Avatar" type="file">
                <script>
                  document.querySelector('#menu').addEventListener('mouseover', () => {
                    document.querySelector('#menu').setAttribute('data-open', 'true');
                  });
                  document.querySelector('dialog button').addEventListener('click', () => {
                    document.querySelector('dialog').close();
                  });
                </script>
                """
            )

            select = page.get_by_role("combobox", name="Status")
            await actions.select_target(page, select, "Active", 2000)
            self.assertEqual(await select.input_value(), "Active")

            checkbox = page.get_by_role("checkbox", name="Public")
            await actions.check_target(page, checkbox, 2000)
            self.assertTrue(await checkbox.is_checked())
            await actions.uncheck_target(page, checkbox, 2000)
            self.assertFalse(await checkbox.is_checked())

            button = page.locator("#menu")
            await actions.hover_target(page, button, 2000)
            self.assertEqual(await button.get_attribute("data-open"), "true")

            dialog = page.get_by_role("dialog")
            await actions.dismiss_dialog(page, dialog, 2000)
            self.assertFalse(await dialog.is_visible())

            element_result = await evaluate_condition(
                page,
                Condition(type="element_state", value="hidden"),
                phase="post",
                target_locator=dialog,
            )
            self.assertTrue(element_result.satisfied, element_result.detail)
            attr_result = await evaluate_condition(
                page,
                Condition(type="attribute_equals", value="data-open=true"),
                phase="post",
                target_locator=button,
            )
            self.assertTrue(attr_result.satisfied, attr_result.detail)
            count_result = await evaluate_condition(
                page,
                Condition(type="count_equals", value="3"),
                phase="post",
                target_locator=page.locator(".row"),
            )
            self.assertTrue(count_result.satisfied, count_result.detail)

            with tempfile.NamedTemporaryFile() as handle:
                handle.write(b"hello")
                handle.flush()
                upload = page.get_by_label("Avatar")
                await actions.upload_file_target(page, upload, handle.name, 2000)
                self.assertEqual(await upload.evaluate("el => el.files.length"), 1)

    def test_resolve_target_still_requires_unique_locator(self) -> None:
        run(self._resolve_target_still_requires_unique_locator())

    async def _resolve_target_still_requires_unique_locator(self) -> None:
        async with launched_browser() as browser:
            page = await browser.new_page()
            await page.set_content("<button>Same</button><button>Same</button>")
            with self.assertRaises(actions.ActionFailure):
                await actions.resolve_target(
                    page,
                    LocatorSpec(kind="role", role="button", name="Same", exact=True),
                    200,
                )

    def test_run_case_executes_select_check_and_element_assertions(self) -> None:
        run(self._run_case_executes_select_check_and_element_assertions())

    async def _run_case_executes_select_check_and_element_assertions(self) -> None:
        with LocalSite() as site:
            page_url = site.url("generic_form.html")
            case = {
                "case_version": "loop.case.v1",
                "name": "generic form",
                "goal": "exercise generic form actions",
                "base_url": site.url(""),
                "steps": [
                    {
                        "index": 0,
                        "action": "goto",
                        "intent": "open form",
                        "value": page_url,
                        "preconditions": [],
                        "postconditions": [{"type": "url_contains", "value": "/generic_form.html", "timeout_ms": 1000}],
                        "timeout_ms": 5000,
                    },
                    {
                        "index": 1,
                        "action": "select",
                        "intent": "select active status",
                        "value": "Active",
                        "target": self._target("Status", {"kind": "role", "role": "combobox", "name": "Status", "exact": True}, page_url),
                        "preconditions": [{"type": "url_contains", "value": "/generic_form.html", "timeout_ms": 1000}],
                        "postconditions": [{"type": "value_equals", "value": "active", "timeout_ms": 1000}],
                        "timeout_ms": 5000,
                    },
                    {
                        "index": 2,
                        "action": "check",
                        "intent": "make it public",
                        "target": self._target("Public", {"kind": "role", "role": "checkbox", "name": "Public", "exact": True}, page_url),
                        "preconditions": [{"type": "url_contains", "value": "/generic_form.html", "timeout_ms": 1000}],
                        "postconditions": [{"type": "element_state", "value": "checked", "timeout_ms": 1000}],
                        "timeout_ms": 5000,
                    },
                    {
                        "index": 3,
                        "action": "assert_attribute",
                        "intent": "save button is ready",
                        "target": self._target("Save", {"kind": "css", "css": "#save"}, page_url),
                        "preconditions": [{"type": "url_contains", "value": "/generic_form.html", "timeout_ms": 1000}],
                        "postconditions": [{"type": "attribute_equals", "value": "data-state=ready", "timeout_ms": 1000}],
                        "timeout_ms": 5000,
                    },
                ],
            }
            async with launched_browser() as browser:
                result = await run_case(case, browser=browser, session_id="sess_generic")
        self.assertEqual(result.status, "passed", result)

    @staticmethod
    def _target(hint: str, locator: dict, page_url: str = "http://127.0.0.1/") -> dict:
        locator = {**locator, "match_count": locator.get("match_count", 1)}
        return {
            "hint": hint,
            "locator": locator,
            "grounding": {
                "observation_id": "obs_test",
                "page_state_id": "ps_test",
                "candidate_id": "e1:0",
                "page_url": page_url,
            },
        }


if __name__ == "__main__":
    unittest.main()
