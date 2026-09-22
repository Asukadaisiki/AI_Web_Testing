"""定位器就地验证与偏好排序（CONTRACT §3.1）—— 用本地静态页验证。

关键断言：观测里出现的每一条定位器都必须**当场真实命中且只命中 1 个**元素；
同名元素不得输出 role/text 定位器；一条都验证不出来的元素不进 elements。
"""

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

from loop_worker.contracts import ElementObservation, LocatorSpec, Observation  # noqa: E402
from loop_worker.locators import build_locators, to_playwright_locator  # noqa: E402
from loop_worker.observer import accessible_name, observe_page  # noqa: E402

KIND_ORDER = {"role": 0, "text": 1, "css": 2}


def css_of(element) -> list[str]:  # noqa: ANN001
    return [locator.css for locator in element.locators if locator.kind == "css"]


class ObservationLocatorTest(unittest.TestCase):
    """对 locators.html 的真实观测。"""

    def test_every_emitted_locator_matches_exactly_once(self) -> None:
        run(self._every_emitted_locator_matches_exactly_once())

    async def _every_emitted_locator_matches_exactly_once(self) -> None:
        with LocalSite() as site:
            async with launched_browser() as browser:
                page = await browser.new_page()
                await page.goto(site.url("locators.html"))
                observation = await observe_page(page)

                self.assertIsInstance(observation, Observation)
                self.assertTrue(observation.elements, "observation must contain elements")

                for element in observation.elements:
                    self.assertTrue(element.locators, f"{element.ref} has no locator")
                    for locator in element.locators:
                        # 契约：只有 match_count == 1 的定位器才允许出现
                        self.assertEqual(locator.match_count, 1, locator)
                        spec = LocatorSpec(**locator.model_dump())
                        count = await to_playwright_locator(page, spec).count()
                        self.assertEqual(
                            count,
                            1,
                            f"{element.ref} {locator.model_dump()} matched {count} elements",
                        )

    def test_locator_preference_order_is_role_text_css(self) -> None:
        run(self._locator_preference_order_is_role_text_css())

    async def _locator_preference_order_is_role_text_css(self) -> None:
        with LocalSite() as site:
            async with launched_browser() as browser:
                page = await browser.new_page()
                await page.goto(site.url("locators.html"))
                observation = await observe_page(page)
                for element in observation.elements:
                    kinds = [locator.kind for locator in element.locators]
                    self.assertEqual(
                        kinds,
                        sorted(kinds, key=lambda kind: KIND_ORDER[kind]),
                        f"{element.ref} locators are not in preference order: {kinds}",
                    )
                    self.assertEqual(len(kinds), len(set(kinds)), kinds)

    def test_unique_named_button_gets_role_locator_first(self) -> None:
        run(self._unique_named_button_gets_role_locator_first())

    async def _unique_named_button_gets_role_locator_first(self) -> None:
        with LocalSite() as site:
            async with launched_browser() as browser:
                page = await browser.new_page()
                await page.goto(site.url("locators.html"))
                observation = await observe_page(page)

                button = self._find(observation, lambda css: "#unique-button" in css)
                self.assertEqual(button.role, "button")
                self.assertEqual(button.name, "Save")
                self.assertEqual(button.locators[0].kind, "role")
                self.assertEqual(button.locators[0].role, "button")
                self.assertEqual(button.locators[0].name, "Save")
                self.assertTrue(button.locators[0].exact)
                self.assertEqual([locator.kind for locator in button.locators], ["role", "text", "css"])

    def test_duplicate_text_elements_fall_back_to_css_only(self) -> None:
        run(self._duplicate_text_elements_fall_back_to_css_only())

    async def _duplicate_text_elements_fall_back_to_css_only(self) -> None:
        with LocalSite() as site:
            async with launched_browser() as browser:
                page = await browser.new_page()
                await page.goto(site.url("locators.html"))
                observation = await observe_page(page)

                duplicates = [
                    element
                    for element in observation.elements
                    if any("duplicate-row" in css for css in css_of(element))
                ]
                self.assertEqual(len(duplicates), 2, "fixture must expose two same-name buttons")

                for element in duplicates:
                    kinds = [locator.kind for locator in element.locators]
                    # 同名元素：role/text 命中数都是 2，只能落到 css
                    self.assertEqual(kinds, ["css"], f"{element.ref} -> {kinds}")
                    # 可访问名唯一实现：不能唯一命中就不给名字
                    self.assertIsNone(element.name)

                first_css, second_css = (css_of(element)[0] for element in duplicates)
                self.assertNotEqual(first_css, second_css)

    def test_nameless_button_gets_css_only(self) -> None:
        run(self._nameless_button_gets_css_only())

    async def _nameless_button_gets_css_only(self) -> None:
        with LocalSite() as site:
            async with launched_browser() as browser:
                page = await browser.new_page()
                await page.goto(site.url("locators.html"))
                observation = await observe_page(page)

                button = self._find(observation, lambda css: "#nameless-button" in css)
                self.assertEqual(button.role, "button")
                self.assertIsNone(button.name)
                self.assertEqual([locator.kind for locator in button.locators], ["css"])
                self.assertEqual(button.locators[0].match_count, 1)

    def test_text_bearing_element_gets_text_locator(self) -> None:
        run(self._text_bearing_element_gets_text_locator())

    async def _text_bearing_element_gets_text_locator(self) -> None:
        with LocalSite() as site:
            async with launched_browser() as browser:
                page = await browser.new_page()
                await page.goto(site.url("locators.html"))
                observation = await observe_page(page)

                paragraph = self._find(observation, lambda css: "#plain-text" in css)
                kinds = [locator.kind for locator in paragraph.locators]
                self.assertIn("text", kinds)
                text_locator = next(loc for loc in paragraph.locators if loc.kind == "text")
                self.assertEqual(text_locator.text, "Plain paragraph text")
                self.assertTrue(text_locator.exact)

    @staticmethod
    def _find(observation: Observation, predicate):  # noqa: ANN001
        for element in observation.elements:
            if any(predicate(css) for css in css_of(element)):
                return element
        raise AssertionError("no element matched the predicate")


class BuildLocatorsTest(unittest.TestCase):
    """直接对 `build_locators` 做边界测试（不依赖夹具页面）。"""

    def test_all_locator_kinds_are_verified_in_preference_order(self) -> None:
        run(self._all_locator_kinds_are_verified_in_preference_order())

    async def _all_locator_kinds_are_verified_in_preference_order(self) -> None:
        async with launched_browser() as browser:
            page = await browser.new_page()
            await page.set_content('<button data-loop-ref="e0">Save</button>')
            locators = await build_locators(
                page, "e0", role="button", name="Save", text="Save", css="button"
            )
            self.assertEqual([locator.kind for locator in locators], ["role", "text", "css"])
            self.assertTrue(all(locator.match_count == 1 for locator in locators))

    def test_locator_with_multiple_matches_is_dropped(self) -> None:
        run(self._locator_with_multiple_matches_is_dropped())

    async def _locator_with_multiple_matches_is_dropped(self) -> None:
        async with launched_browser() as browser:
            page = await browser.new_page()
            await page.set_content(
                '<button data-loop-ref="e0">Remove</button>'
                '<button>Remove</button>'
            )
            locators = await build_locators(
                page, "e0", role="button", name="Remove", text="Remove", css="body > button:nth-of-type(1)"
            )
            # role 命中 2 个、text 命中 2 个 → 都不得输出；只剩唯一的结构路径
            self.assertEqual([locator.kind for locator in locators], ["css"])

    def test_locator_with_wrong_name_is_dropped(self) -> None:
        run(self._locator_with_wrong_name_is_dropped())

    async def _locator_with_wrong_name_is_dropped(self) -> None:
        async with launched_browser() as browser:
            page = await browser.new_page()
            await page.set_content('<button data-loop-ref="e0">Save</button>')
            locators = await build_locators(
                page, "e0", role="button", name="Nonexistent", text=None, css=None
            )
            self.assertEqual(locators, [])

    def test_locator_matching_another_element_is_dropped(self) -> None:
        run(self._locator_matching_another_element_is_dropped())

    async def _locator_matching_another_element_is_dropped(self) -> None:
        async with launched_browser() as browser:
            page = await browser.new_page()
            await page.set_content(
                '<button data-loop-ref="e0">Save</button>'
                '<button data-loop-ref="e1">Delete</button>'
            )
            # 名字 "Delete" 唯一命中，但命中的不是 e0 → 不能给 e0 当 role 定位器
            self.assertFalse(await accessible_name(page, "e0", "button") == "Delete")
            locators = await build_locators(
                page, "e0", role="button", name="Delete", text=None, css=None
            )
            self.assertEqual(locators, [])

    def test_element_without_any_locator_is_rejected_by_contract(self) -> None:
        from pydantic import ValidationError

        with self.assertRaises(ValidationError):
            ElementObservation(
                ref="e0",
                tag="button",
                role="button",
                name=None,
                text=None,
                value=None,
                visible=True,
                enabled=True,
                locators=[],
            )


if __name__ == "__main__":
    unittest.main()
