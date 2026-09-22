"""条件评估的阶段语义（CONTRACT §2.2）—— 用本地静态页验证。

- pre 只能是状态事实，且是**动作前快照**（不轮询）；
- post 带超时轮询；
- `value_equals` 的语义是"本步 target 元素的 value 等于给定值"，不是"值变了"。
"""

from __future__ import annotations

import sys
import time
import unittest
from pathlib import Path

WORKER_ROOT = Path(__file__).resolve().parents[1]
TESTS_DIR = Path(__file__).resolve().parent
for _path in (str(WORKER_ROOT), str(TESTS_DIR)):
    if _path not in sys.path:
        sys.path.insert(0, _path)

from local_site import LocalSite  # noqa: E402
from pw_support import launched_browser, run  # noqa: E402

from loop_worker.conditions import (  # noqa: E402
    ConditionPhaseError,
    evaluate_condition,
    evaluate_postconditions,
    evaluate_preconditions,
    unmet_summary,
    wait_condition,
)
from loop_worker.contracts import Condition, Step  # noqa: E402


def make_step(
    *,
    pre: list[Condition] | None = None,
    post: list[Condition] | None = None,
) -> Step:
    return Step(
        index=0,
        action="assert_text",
        intent="assert",
        value="x",
        preconditions=pre or [],
        postconditions=post or [],
    )


class ConditionPhaseTest(unittest.TestCase):
    def test_url_contains_in_both_phases(self) -> None:
        run(self._url_contains_in_both_phases())

    async def _url_contains_in_both_phases(self) -> None:
        with LocalSite() as site:
            async with launched_browser() as browser:
                page = await browser.new_page()
                await page.goto(site.url("index.html"))

                hit = Condition(type="url_contains", value="/index.html", timeout_ms=500)
                miss = Condition(type="url_contains", value="/nowhere", timeout_ms=500)
                self.assertTrue((await evaluate_condition(page, hit, phase="pre")).satisfied)
                self.assertTrue((await evaluate_condition(page, hit, phase="post")).satisfied)
                self.assertFalse((await evaluate_condition(page, miss, phase="pre")).satisfied)

    def test_text_visible_and_text_gone(self) -> None:
        run(self._text_visible_and_text_gone())

    async def _text_visible_and_text_gone(self) -> None:
        with LocalSite() as site:
            async with launched_browser() as browser:
                page = await browser.new_page()
                await page.goto(site.url("index.html"))

                visible = Condition(type="text_visible", value="Demo Catalog", timeout_ms=500)
                gone = Condition(type="text_gone", value="Demo Catalog", timeout_ms=500)
                self.assertTrue((await evaluate_condition(page, visible, phase="pre")).satisfied)
                self.assertFalse((await evaluate_condition(page, gone, phase="pre")).satisfied)

                # 动态文本：Loading results → Results ready
                loading_gone = Condition(type="text_gone", value="Loading results", timeout_ms=2000)
                result = await wait_condition(page, loading_gone, phase="post")
                self.assertTrue(result.satisfied, result.detail)

                ready = Condition(type="text_visible", value="Results ready", timeout_ms=500)
                self.assertTrue((await evaluate_condition(page, ready, phase="post")).satisfied)

    def test_pre_phase_is_a_snapshot_without_polling(self) -> None:
        run(self._pre_phase_is_a_snapshot_without_polling())

    async def _pre_phase_is_a_snapshot_without_polling(self) -> None:
        async with launched_browser() as browser:
            page = await browser.new_page()
            # 元素 400ms 后才出现
            await page.set_content(
                "<div id='root'></div>"
                "<script>setTimeout(function(){document.getElementById('root')"
                ".textContent='Later text';}, 400);</script>"
            )
            condition = Condition(type="text_visible", value="Later text", timeout_ms=3000)

            started = time.monotonic()
            pre_result = await evaluate_condition(page, condition, phase="pre")
            pre_elapsed = time.monotonic() - started
            self.assertFalse(pre_result.satisfied)
            self.assertLess(pre_elapsed, 0.35, "pre must not poll")

            post_result = await wait_condition(page, condition, phase="post", timeout_ms=3000)
            self.assertTrue(post_result.satisfied, post_result.detail)

    def test_transition_condition_is_rejected_in_pre_phase(self) -> None:
        run(self._transition_condition_is_rejected_in_pre_phase())

    async def _transition_condition_is_rejected_in_pre_phase(self) -> None:
        async with launched_browser() as browser:
            page = await browser.new_page()
            await page.set_content("<p>hello</p>")
            for type_ in ("url_changes", "value_equals"):
                with self.subTest(type=type_):
                    condition = Condition(type=type_, value="anything", timeout_ms=100)
                    with self.assertRaises(ConditionPhaseError):
                        await evaluate_condition(page, condition, phase="pre")

    def test_url_changes_only_makes_sense_after_the_action(self) -> None:
        run(self._url_changes_only_makes_sense_after_the_action())

    async def _url_changes_only_makes_sense_after_the_action(self) -> None:
        with LocalSite() as site:
            async with launched_browser() as browser:
                page = await browser.new_page()
                await page.goto(site.url("index.html"))
                url_before = page.url
                condition = Condition(type="url_changes", value="changed", timeout_ms=200)

                unchanged = await wait_condition(
                    page, condition, phase="post", url_before=url_before
                )
                self.assertFalse(unchanged.satisfied)

                await page.goto(site.url("cart.html"))
                changed = await wait_condition(
                    page, condition, phase="post", url_before=url_before
                )
                self.assertTrue(changed.satisfied, changed.detail)


class ValueEqualsTest(unittest.TestCase):
    """value_equals = 本步 target 元素的 value 等于给定值。"""

    def test_value_equals_compares_the_target_value(self) -> None:
        run(self._value_equals_compares_the_target_value())

    async def _value_equals_compares_the_target_value(self) -> None:
        with LocalSite() as site:
            async with launched_browser() as browser:
                page = await browser.new_page()
                await page.goto(site.url("detail.html?item=alpha"))
                target = page.locator("#quantity")

                same_as_initial = Condition(type="value_equals", value="1", timeout_ms=200)
                result = await evaluate_condition(
                    page, same_as_initial, phase="post", target_locator=target
                )
                # 没有发生任何"变化"，但值确实等于 1 → 满足
                self.assertTrue(result.satisfied, result.detail)

                different = Condition(type="value_equals", value="2", timeout_ms=200)
                result = await evaluate_condition(
                    page, different, phase="post", target_locator=target
                )
                self.assertFalse(result.satisfied)

                await target.fill("2")
                result = await evaluate_condition(
                    page, different, phase="post", target_locator=target
                )
                self.assertTrue(result.satisfied, result.detail)

    def test_value_equals_polls_until_the_value_arrives(self) -> None:
        run(self._value_equals_polls_until_the_value_arrives())

    async def _value_equals_polls_until_the_value_arrives(self) -> None:
        async with launched_browser() as browser:
            page = await browser.new_page()
            await page.set_content(
                "<input id='field' value='0'>"
                "<script>setTimeout(function(){document.getElementById('field').value='7';}, 300);"
                "</script>"
            )
            condition = Condition(type="value_equals", value="7", timeout_ms=2000)
            started = time.monotonic()
            result = await wait_condition(
                page, condition, phase="post", target_locator=page.locator("#field")
            )
            self.assertTrue(result.satisfied, result.detail)
            self.assertLess(time.monotonic() - started, 2.0)

    def test_value_equals_without_target_is_unsatisfiable(self) -> None:
        run(self._value_equals_without_target_is_unsatisfiable())

    async def _value_equals_without_target_is_unsatisfiable(self) -> None:
        async with launched_browser() as browser:
            page = await browser.new_page()
            await page.set_content("<p>no target here</p>")
            condition = Condition(type="value_equals", value="1", timeout_ms=100)
            result = await evaluate_condition(page, condition, phase="post")
            self.assertFalse(result.satisfied)
            self.assertIn("target", result.detail or "")


class PhaseRunnerTest(unittest.TestCase):
    def test_pre_and_post_helpers_use_the_right_phases(self) -> None:
        run(self._pre_and_post_helpers_use_the_right_phases())

    async def _pre_and_post_helpers_use_the_right_phases(self) -> None:
        with LocalSite() as site:
            async with launched_browser() as browser:
                page = await browser.new_page()
                await page.goto(site.url("index.html"))
                step = make_step(
                    pre=[Condition(type="text_visible", value="Demo Catalog", timeout_ms=500)],
                    post=[Condition(type="url_contains", value="/index.html", timeout_ms=500)],
                )
                pre_results = await evaluate_preconditions(page, step)
                post_results = await evaluate_postconditions(
                    page, step, url_before=page.url
                )
                self.assertEqual([result.phase for result in pre_results], ["pre"])
                self.assertEqual([result.phase for result in post_results], ["post"])
                self.assertTrue(all(result.satisfied for result in pre_results))
                self.assertTrue(all(result.satisfied for result in post_results))
                self.assertIsNone(unmet_summary(pre_results + post_results))

    def test_failing_postcondition_honours_its_own_timeout(self) -> None:
        run(self._failing_postcondition_honours_its_own_timeout())

    async def _failing_postcondition_honours_its_own_timeout(self) -> None:
        with LocalSite() as site:
            async with launched_browser() as browser:
                page = await browser.new_page()
                await page.goto(site.url("index.html"))
                step = make_step(
                    post=[Condition(type="text_visible", value="Never appears", timeout_ms=300)]
                )
                started = time.monotonic()
                results = await evaluate_postconditions(page, step, url_before=page.url)
                elapsed = time.monotonic() - started
                self.assertFalse(results[0].satisfied)
                self.assertGreaterEqual(elapsed, 0.3)
                self.assertLess(elapsed, 2.0, "timeout_ms must bound the polling")
                summary = unmet_summary(results)
                self.assertIsNotNone(summary)
                self.assertIn("Never appears", summary or "")


if __name__ == "__main__":
    unittest.main()
