"""端到端：用本地静态站点夹具跑完整 case（goto → input → click → assert_text → assert_url）。

覆盖：
- 全链路 passed，且首步从 about:blank 开始（执行器不做隐式预导航）；
- 每步证据（截图落盘、console、network、url_before/url_after）；
- 故意失败的 case：断言不存在的文本 → failed + `condition_unmet`；
- 目标不存在 → failed + `target_not_found`；
- 首步非 goto → error + `case_invalid`。
"""

from __future__ import annotations

import os
import sys
import unittest
from pathlib import Path
from unittest import mock

WORKER_ROOT = Path(__file__).resolve().parents[1]
TESTS_DIR = Path(__file__).resolve().parent
for _path in (str(WORKER_ROOT), str(TESTS_DIR)):
    if _path not in sys.path:
        sys.path.insert(0, _path)

from case_builder import build_case, condition, goto_step  # noqa: E402
from local_site import LocalSite  # noqa: E402
from pw_support import launched_browser, run  # noqa: E402

from loop_worker.contracts import ExecutionResult, Observation  # noqa: E402
from loop_worker.evidence import REPO_ROOT, artifacts_dir, display_path  # noqa: E402
from loop_worker.observer import observe_page  # noqa: E402
from loop_worker.runner import run_case  # noqa: E402


def locator_from(observation: Observation, *, kind: str, **matches: str) -> dict:
    """从真实观测里取一条已就地验证过的定位器（去掉 match_count）。"""
    for element in observation.elements:
        for locator in element.locators:
            if locator.kind != kind:
                continue
            payload = locator.model_dump()
            if all(payload.get(key) == value for key, value in matches.items()):
                payload.pop("match_count", None)
                return payload
    raise AssertionError(f"no {kind} locator matching {matches} in observation")


def step_target(locator: dict, page_url: str) -> dict:
    return {
        "hint": "fixture target",
        "locator": locator,
        "grounding": {
            "observation_id": "obs_fixture",
            "page_state_id": "ps_fixture",
            "candidate_id": "e0:0",
            "page_url": page_url,
        },
    }


def action_step(
    index: int,
    action: str,
    *,
    page_url: str,
    locator: dict | None = None,
    pre_value: str,
    post: list[dict],
    value: str | None = None,
    timeout_ms: int = 5000,
) -> dict:
    step: dict = {
        "index": index,
        "action": action,
        "intent": f"{action} on the fixture",
        "preconditions": [condition("url_contains", pre_value)],
        "postconditions": post,
        "timeout_ms": timeout_ms,
    }
    if locator is not None:
        step["target"] = step_target(locator, page_url)
    if value is not None:
        step["value"] = value
    return step


class RunnerEndToEndTest(unittest.TestCase):
    def test_full_flow_passes_with_evidence(self) -> None:
        run(self._full_flow_passes_with_evidence())

    async def _full_flow_passes_with_evidence(self) -> None:
        with LocalSite() as site:
            index_url = site.url("index.html")
            detail_url = site.url("detail.html?item=alpha")
            cart_url = site.url("cart.html")

            async with launched_browser() as browser:
                scout = await browser.new_page()
                await scout.goto(index_url)
                index_obs = await observe_page(scout)
                await scout.goto(detail_url)
                detail_obs = await observe_page(scout)
                await scout.close()

                search = locator_from(index_obs, kind="role", role="textbox", name="Search items")
                filter_button = locator_from(index_obs, kind="role", role="button", name="Filter")
                alpha_link = locator_from(index_obs, kind="role", role="link", name="Alpha")
                quantity = locator_from(detail_obs, kind="role", name="Quantity")
                add_to_cart = locator_from(
                    detail_obs, kind="role", role="button", name="Add to cart"
                )
                go_to_cart = locator_from(detail_obs, kind="role", role="link", name="Go to cart")

                case = build_case(
                    [
                        goto_step(0, index_url, "/index.html"),
                        action_step(
                            1,
                            "input",
                            page_url=index_url,
                            locator=search,
                            pre_value="/index.html",
                            post=[
                                condition("value_equals", "alpha"),
                                condition("text_gone", "Tip: type in the search box"),
                            ],
                            value="alpha",
                        ),
                        action_step(
                            2,
                            "click",
                            page_url=index_url,
                            locator=filter_button,
                            pre_value="/index.html",
                            post=[condition("text_gone", "Beta")],
                        ),
                        action_step(
                            3,
                            "click",
                            page_url=index_url,
                            locator=alpha_link,
                            pre_value="/index.html",
                            post=[
                                condition("url_contains", "detail.html"),
                                condition("text_gone", "Loading details"),
                            ],
                        ),
                        action_step(
                            4,
                            "input",
                            page_url=detail_url,
                            locator=quantity,
                            pre_value="detail.html",
                            post=[condition("value_equals", "2")],
                            value="2",
                        ),
                        action_step(
                            5,
                            "click",
                            page_url=detail_url,
                            locator=add_to_cart,
                            pre_value="detail.html",
                            post=[condition("text_visible", "Added to cart")],
                        ),
                        action_step(
                            6,
                            "click",
                            page_url=detail_url,
                            locator=go_to_cart,
                            pre_value="detail.html",
                            post=[
                                condition("url_changes", "changed"),
                                condition("url_contains", "cart.html"),
                            ],
                        ),
                        action_step(
                            7,
                            "assert_text",
                            page_url=cart_url,
                            pre_value="cart.html",
                            post=[condition("text_visible", "Alpha x 2")],
                            value="Alpha x 2",
                        ),
                        action_step(
                            8,
                            "assert_url",
                            page_url=cart_url,
                            pre_value="cart.html",
                            post=[condition("url_contains", "cart.html")],
                            value="cart.html",
                        ),
                    ],
                    base_url=site.base_url,
                )

                result = await run_case(case, browser=browser)

                self.assertIsInstance(result, ExecutionResult)
                self.assertEqual(result.status, "passed", result.model_dump())
                self.assertIsNone(result.error)
                self.assertEqual(len(result.steps), 9)
                self.assertTrue(all(step.status == "passed" for step in result.steps))
                self.assertTrue(result.final_url.endswith("cart.html"), result.final_url)

                # 全新 context：首步从空页面开始，执行器不做隐式预导航
                self.assertEqual(result.steps[0].url_before, "about:blank")
                self.assertEqual(result.steps[0].action, "goto")
                self.assertEqual(result.steps[0].index, 0)

                # 每步都有证据
                for step in result.steps:
                    self.assertIsNotNone(step.evidence.screenshot_path, step.index)
                    screenshot = Path(step.evidence.screenshot_path)
                    if not screenshot.is_absolute():
                        screenshot = REPO_ROOT / screenshot
                    self.assertTrue(screenshot.is_file(), f"missing {screenshot}")
                    self.assertIsInstance(step.url_before, str)
                    self.assertIsInstance(step.url_after, str)
                    self.assertGreaterEqual(step.duration_ms, 0)
                    self.assertTrue(step.started_at.endswith("Z"), step.started_at)

                # 条件结果完整：pre 与 post 都记录在案
                first_step = result.steps[0]
                self.assertEqual(
                    [result_.phase for result_ in first_step.conditions], ["post"]
                )
                self.assertTrue(first_step.conditions[0].satisfied)

                cart_step = result.steps[6]
                url_change = next(
                    item for item in cart_step.conditions if item.type == "url_changes"
                )
                self.assertTrue(url_change.satisfied, url_change.detail)

                # 证据内容：console 与 network 都真实采到了
                console_texts = [
                    event.text for step in result.steps for event in step.evidence.console
                ]
                self.assertIn("catalog ready", console_texts)
                network = [event for step in result.steps for event in step.evidence.network]
                self.assertTrue(network)
                self.assertTrue(any(event.status == 200 for event in network))
                self.assertTrue(any("index.html" in event.url for event in network))

    def test_failing_assertion_reports_condition_unmet(self) -> None:
        run(self._failing_assertion_reports_condition_unmet())

    async def _failing_assertion_reports_condition_unmet(self) -> None:
        with LocalSite() as site:
            index_url = site.url("index.html")
            async with launched_browser() as browser:
                case = build_case(
                    [
                        goto_step(0, index_url, "/index.html"),
                        action_step(
                            1,
                            "assert_text",
                            page_url=index_url,
                            pre_value="/index.html",
                            post=[condition("text_visible", "This text does not exist", 400)],
                            value="This text does not exist",
                        ),
                        goto_step(2, site.url("cart.html"), "cart.html"),
                    ],
                    base_url=site.base_url,
                )
                result = await run_case(case, browser=browser)

                self.assertEqual(result.status, "failed")
                # 失败即止：后续步骤不再执行
                self.assertEqual(len(result.steps), 2)
                failed = result.steps[1]
                self.assertEqual(failed.status, "failed")
                self.assertIsNotNone(failed.error)
                self.assertEqual(failed.error.kind, "condition_unmet")
                self.assertIn("This text does not exist", failed.error.message)
                unmet = [item for item in failed.conditions if not item.satisfied]
                self.assertEqual(len(unmet), 1)
                self.assertEqual(unmet[0].type, "text_visible")
                self.assertEqual(unmet[0].phase, "post")
                self.assertIsNotNone(failed.evidence.screenshot_path)

    def test_missing_target_reports_target_not_found(self) -> None:
        run(self._missing_target_reports_target_not_found())

    async def _missing_target_reports_target_not_found(self) -> None:
        with LocalSite() as site:
            index_url = site.url("index.html")
            async with launched_browser() as browser:
                case = build_case(
                    [
                        goto_step(0, index_url, "/index.html"),
                        action_step(
                            1,
                            "click",
                            page_url=index_url,
                            locator={
                                "kind": "role",
                                "role": "button",
                                "name": "No such button",
                                "exact": True,
                            },
                            pre_value="/index.html",
                            post=[condition("text_visible", "never")],
                            timeout_ms=600,
                        ),
                    ],
                    base_url=site.base_url,
                )
                result = await run_case(case, browser=browser)

                self.assertEqual(result.status, "failed")
                self.assertEqual(len(result.steps), 2)
                failed = result.steps[1]
                self.assertEqual(failed.status, "failed")
                self.assertIsNotNone(failed.error)
                self.assertEqual(failed.error.kind, "target_not_found")
                # 前置条件已满足；动作没跑成，postcondition 不参与评估
                self.assertEqual([item.phase for item in failed.conditions], ["pre"])
                self.assertTrue(failed.conditions[0].satisfied)
                self.assertIsNotNone(failed.evidence.screenshot_path)

    def test_failing_precondition_reports_condition_unmet(self) -> None:
        run(self._failing_precondition_reports_condition_unmet())

    async def _failing_precondition_reports_condition_unmet(self) -> None:
        with LocalSite() as site:
            index_url = site.url("index.html")
            async with launched_browser() as browser:
                case = build_case(
                    [
                        goto_step(0, index_url, "/index.html"),
                        action_step(
                            1,
                            "assert_text",
                            page_url=index_url,
                            pre_value="/somewhere-else",
                            post=[condition("text_visible", "Demo Catalog")],
                            value="Demo Catalog",
                        ),
                    ],
                    base_url=site.base_url,
                )
                result = await run_case(case, browser=browser)
                self.assertEqual(result.status, "failed")
                failed = result.steps[1]
                self.assertEqual(failed.error.kind, "condition_unmet")
                self.assertEqual(failed.conditions[0].phase, "pre")
                self.assertFalse(failed.conditions[0].satisfied)

    def test_first_step_must_be_goto_and_returns_error(self) -> None:
        run(self._first_step_must_be_goto_and_returns_error())

    async def _first_step_must_be_goto_and_returns_error(self) -> None:
        with LocalSite() as site:
            index_url = site.url("index.html")
            async with launched_browser() as browser:
                case = build_case(
                    [
                        action_step(
                            0,
                            "assert_text",
                            page_url=index_url,
                            pre_value="/index.html",
                            post=[condition("text_visible", "Demo Catalog")],
                            value="Demo Catalog",
                        )
                    ],
                    base_url=site.base_url,
                )
                result = await run_case(case, browser=browser)
                self.assertEqual(result.status, "error")
                self.assertEqual(result.steps, [])
                self.assertIsNotNone(result.error)
                self.assertEqual(result.error.kind, "case_invalid")
                self.assertIn("case_not_goto_first", result.error.message)


class ArtifactsDirTest(unittest.TestCase):
    """证据目录：环境变量 LOOP_ARTIFACTS_DIR 优先，默认 v2/data/artifacts。"""

    def test_default_artifacts_dir(self) -> None:
        with mock.patch.dict(os.environ, {}, clear=False):
            os.environ.pop("LOOP_ARTIFACTS_DIR", None)
            self.assertEqual(artifacts_dir(), (REPO_ROOT / "v2" / "data" / "artifacts").resolve())

    def test_env_override_artifacts_dir(self) -> None:
        with mock.patch.dict(os.environ, {"LOOP_ARTIFACTS_DIR": str(TESTS_DIR)}):
            self.assertEqual(artifacts_dir(), TESTS_DIR.resolve())

    def test_display_path_is_relative_to_repo_root(self) -> None:
        path = REPO_ROOT / "v2" / "data" / "artifacts" / "exec_x_0.png"
        self.assertEqual(display_path(path), "v2/data/artifacts/exec_x_0.png")


if __name__ == "__main__":
    unittest.main()
