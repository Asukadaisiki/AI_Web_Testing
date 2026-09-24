"""P5 blocker observation and grounding recovery behavior."""

from __future__ import annotations

import sys
import unittest
from pathlib import Path

WORKER_ROOT = Path(__file__).resolve().parents[1]
TESTS_DIR = Path(__file__).resolve().parent
for _path in (str(WORKER_ROOT), str(TESTS_DIR)):
    if _path not in sys.path:
        sys.path.insert(0, _path)

from case_builder import action_step, build_case, condition, css_locator, goto_step  # noqa: E402
from local_site import LocalSite  # noqa: E402
from pw_support import launched_browser, run  # noqa: E402

from loop_worker.observer import observe_page  # noqa: E402
from loop_worker.runner import run_case  # noqa: E402


TEST_SESSION = "sess_p5"


class BlockerP5Test(unittest.TestCase):
    def test_observation_summarizes_cookie_banner_and_captcha(self) -> None:
        run(self._observation_summarizes_cookie_banner_and_captcha())

    async def _observation_summarizes_cookie_banner_and_captcha(self) -> None:
        async with launched_browser() as browser:
            page = await browser.new_page()
            await page.set_content(
                """
                <main><button id="continue">Continue</button></main>
                <section id="cookie" style="position:fixed;left:0;right:0;bottom:0;z-index:1000;background:white">
                  <p>We use cookies to improve this site.</p>
                  <button id="accept">Accept cookies</button>
                </section>
                <div id="captcha" class="g-recaptcha">captcha challenge</div>
                """
            )

            observation = await observe_page(page)

        kinds = {blocker.kind for blocker in observation.blockers}
        self.assertIn("cookie_banner", kinds)
        self.assertIn("captcha", kinds)
        cookie = next(blocker for blocker in observation.blockers if blocker.kind == "cookie_banner")
        self.assertEqual(cookie.confidence, "high")
        self.assertTrue(cookie.dismiss_candidates, cookie)
        captcha = next(blocker for blocker in observation.blockers if blocker.kind == "captcha")
        self.assertFalse(captcha.dismiss_candidates)

    def test_click_recovers_from_dismissible_overlay_and_records_recovery(self) -> None:
        run(self._click_recovers_from_dismissible_overlay_and_records_recovery())

    async def _click_recovers_from_dismissible_overlay_and_records_recovery(self) -> None:
        with LocalSite() as site:
            page_url = site.url("blockers.html")
            case = build_case(
                [
                    goto_step(0, page_url, "/blockers.html"),
                    action_step(
                        1,
                        "click",
                        locator=css_locator("#target"),
                        page_url=page_url,
                        pre=[condition("text_visible", "Continue")],
                        post=[condition("attribute_equals", "data-clicked=yes")],
                    ),
                ]
            )
            async with launched_browser() as browser:
                result = await run_case(case, browser=browser, session_id=TEST_SESSION)

        self.assertEqual(result.status, "passed", result.model_dump())
        click_step = result.steps[1]
        self.assertTrue(click_step.recovery, click_step.model_dump())
        self.assertEqual(click_step.recovery[0].blocker.kind, "overlay")
        self.assertTrue(click_step.recovery[0].succeeded)
        self.assertTrue(click_step.recovery[0].retried_original_action)

    def test_auth_wall_blocks_without_auto_recovery(self) -> None:
        run(self._auth_wall_blocks_without_auto_recovery())

    async def _auth_wall_blocks_without_auto_recovery(self) -> None:
        with LocalSite() as site:
            page_url = site.url("blockers.html?mode=auth")
            case = build_case(
                [
                    goto_step(0, page_url, "/blockers.html", timeout_ms=2000),
                    action_step(
                        1,
                        "click",
                        locator=css_locator("#target"),
                        page_url=page_url,
                        pre=[condition("text_visible", "Continue", 1000)],
                        post=[condition("text_visible", "Done", 1000)],
                        timeout_ms=2000,
                    ),
                ]
            )

            async with launched_browser() as browser:
                result = await run_case(case, browser=browser, session_id=TEST_SESSION)

        self.assertEqual(result.status, "failed")
        failed = result.steps[1]
        self.assertIsNotNone(failed.error)
        self.assertEqual(failed.error.kind, "blocked_by_auth")
        self.assertIsNotNone(failed.blocker)
        self.assertEqual(failed.blocker.kind, "auth_wall")
        self.assertEqual(failed.recovery, [])


if __name__ == "__main__":
    unittest.main()
