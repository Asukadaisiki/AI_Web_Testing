"""Observation v2 structural world-model tests."""

from __future__ import annotations

import sys
import unittest
from pathlib import Path

WORKER_ROOT = Path(__file__).resolve().parents[1]
TESTS_DIR = Path(__file__).resolve().parent
for _path in (str(WORKER_ROOT), str(TESTS_DIR)):
    if _path not in sys.path:
        sys.path.insert(0, _path)

from local_site import LocalSite  # noqa: E402
from pw_support import launched_browser, run  # noqa: E402

from loop_worker.observer import observe_page  # noqa: E402


def css_locators(element) -> list[str]:  # noqa: ANN001
    return [locator.css for locator in element.locators if locator.kind == "css"]


def by_css(observation, css: str):  # noqa: ANN001
    for element in observation.elements:
        if css in css_locators(element):
            return element
    return None


class ObservationV2Test(unittest.TestCase):
    def test_capability_fixture_exposes_cards_and_form_relationships(self) -> None:
        run(self._capability_fixture_exposes_cards_and_form_relationships())

    async def _capability_fixture_exposes_cards_and_form_relationships(self) -> None:
        with LocalSite() as site:
            async with launched_browser() as browser:
                page = await browser.new_page()
                await page.goto(site.url("capability.html"))
                observation = await observe_page(page)

        cards = [node for node in observation.structures if node.kind == "card"]
        blue_card = next(
            (node for node in cards if "Blue Top" in node.full_text and "View Product" in node.full_text),
            None,
        )
        self.assertIsNotNone(blue_card, "Blue Top card must be a scope-capable structure")

        blue_text = by_css(observation, "#cards > div:nth-of-type(1) > p")
        blue_link = by_css(observation, "#cards > div:nth-of-type(1) > a")
        self.assertIsNotNone(blue_text)
        self.assertIsNotNone(blue_link)
        self.assertEqual(blue_text.container_ref, blue_card.ref)
        self.assertEqual(blue_link.container_ref, blue_card.ref)
        self.assertEqual(blue_link.parent_ref, blue_card.ref)
        self.assertEqual(blue_link.attributes.get("href"), "detail.html?item=alpha")
        self.assertGreater(blue_link.bbox.width, 0)
        self.assertTrue(blue_link.visible_in_viewport)

        search_form = next(
            node
            for node in observation.structures
            if node.kind == "form" and node.attributes.get("id") == "search-form"
        )
        search_input = by_css(observation, "#search-input")
        search_submit = by_css(observation, "#search-submit")
        self.assertIsNotNone(search_input)
        self.assertIsNotNone(search_submit)
        self.assertEqual(search_input.form.form_ref, search_form.ref)
        self.assertEqual(search_submit.form.form_ref, search_form.ref)
        self.assertEqual(search_input.form.submit_candidate_ref, search_submit.ref)
        self.assertTrue(search_input.form.enter_submittable)
        self.assertEqual(search_submit.attributes.get("id"), "search-submit")

        submit_candidates = [
            candidate
            for candidate in observation.action_candidates
            if candidate.kind == "form_submit_candidate"
        ]
        self.assertEqual(len(submit_candidates), 1, observation.action_candidates)
        candidate = submit_candidates[0]
        self.assertEqual(candidate.action, "click")
        self.assertEqual(candidate.target_ref, search_submit.ref)
        self.assertEqual(candidate.locator, search_submit.locators[0])
        self.assertIn("form submit", candidate.aliases)
        self.assertIn("search submit", candidate.aliases)
        self.assertIn("search-submit", candidate.aliases)
        self.assertIn(
            ("form_submit_candidate", search_form.ref),
            [(relation.type, relation.ref) for relation in candidate.relations],
        )
        self.assertIn(
            ("near_control", search_input.ref),
            [(relation.type, relation.ref) for relation in candidate.relations],
        )

    def test_observation_marks_truncation_when_element_cap_is_reached(self) -> None:
        run(self._observation_marks_truncation_when_element_cap_is_reached())

    async def _observation_marks_truncation_when_element_cap_is_reached(self) -> None:
        async with launched_browser() as browser:
            page = await browser.new_page()
            await page.set_content(
                "<main>"
                + "".join(f'<button id="b{i}">Button {i}</button>' for i in range(20))
                + "</main>"
            )
            observation = await observe_page(page, max_elements=5)

        self.assertTrue(observation.truncated)
        self.assertLessEqual(len(observation.elements), 5)
        self.assertIn("max_elements", observation.truncation_reason)


if __name__ == "__main__":
    unittest.main()
