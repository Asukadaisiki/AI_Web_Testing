"""Complete offline ecommerce journey against the real Playwright worker."""

from __future__ import annotations

import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

WORKER_ROOT = Path(__file__).resolve().parents[1]
TESTS_DIR = Path(__file__).resolve().parent
for _path in (str(WORKER_ROOT), str(TESTS_DIR)):
    if _path not in sys.path:
        sys.path.insert(0, _path)

from case_builder import action_step, build_case, condition, goto_step  # noqa: E402
from local_site import LocalSite  # noqa: E402
from pw_support import launched_browser, run  # noqa: E402

from loop_worker.evidence import artifacts_dir  # noqa: E402
from loop_worker.observer import observe_page  # noqa: E402
from loop_worker.runner import run_case  # noqa: E402


TEST_SESSION = "sess_ecommerce_baseline"


def _locator_payload(locator) -> dict:
    payload = locator.model_dump()
    payload.pop("match_count", None)
    return payload


def _element(observation, *, role: str, name: str):
    for element in observation.elements:
        if element.role == role and element.name == name and element.visible:
            return element
    raise AssertionError(f"no visible {role} named {name!r} in {observation.url}")


def _element_in_scope(
    observation, *, role: str, name: str, scope_kind: str, scope_text: str
):
    scope_refs = {
        scope.ref
        for scope in observation.structures
        if scope.kind == scope_kind and scope_text in scope.full_text and scope.visible
    }
    for element in observation.elements:
        if (
            element.role == role
            and element.name == name
            and element.visible
            and element.container_ref in scope_refs
        ):
            return element
    raise AssertionError(
        f"no visible {role} named {name!r} within {scope_kind} containing {scope_text!r}"
    )


def _element_target(observation, element, *, spec: dict | None = None) -> dict:
    locator = element.locators[0]
    target = {
        "hint": element.name or element.text or element.ref,
        "locator": _locator_payload(locator),
        "grounding": {
            "observation_id": observation.observation_id,
            "page_state_id": observation.page_state_id,
            "candidate_id": f"{element.ref}:0",
            "page_url": observation.url,
        },
    }
    if spec is not None:
        target["spec"] = spec
    return target


def _candidate_target(observation, candidate) -> dict:
    return {
        "hint": candidate.candidate_id,
        "locator": _locator_payload(candidate.locator),
        "grounding": {
            "observation_id": observation.observation_id,
            "page_state_id": observation.page_state_id,
            "candidate_id": candidate.candidate_id,
            "page_url": observation.url,
        },
    }


def _step_with_target(step: dict, target: dict) -> dict:
    step["target"] = target
    return step


class EcommerceBaselineTest(unittest.TestCase):
    def test_login_search_detail_modal_cart_passes_with_evidence(self) -> None:
        run(self._login_search_detail_modal_cart_passes_with_evidence())

    async def _login_search_detail_modal_cart_passes_with_evidence(self) -> None:
        with tempfile.TemporaryDirectory() as artifact_root:
            with mock.patch.dict(
                "os.environ", {"LOOP_ARTIFACTS_DIR": artifact_root}
            ):
                await self._run_case()

    async def _run_case(self) -> None:
        with LocalSite() as site:
            login_url = site.url("ecommerce_login.html")
            products_url = site.url("ecommerce_products.html")
            detail_url = site.url("ecommerce_detail.html?product=blue-top")
            cart_url = site.url("ecommerce_cart.html")

            async with launched_browser() as browser:
                scout = await browser.new_page()
                await scout.goto(login_url)
                login_observation = await observe_page(scout)
                email = _element(login_observation, role="textbox", name="Email Address")
                password = _element(login_observation, role="textbox", name="Password")
                login = _element(login_observation, role="button", name="Login")

                await scout.get_by_role("textbox", name="Email Address").fill(
                    "shopper@example.test"
                )
                await scout.get_by_role("textbox", name="Password").fill("offline-secret")
                await scout.get_by_role("button", name="Login").click()
                await scout.wait_for_url("**/ecommerce_products.html")
                signed_in_observation = await observe_page(scout)
                products = _element(signed_in_observation, role="link", name="Products")

                await scout.get_by_role("link", name="Products").click()
                await scout.wait_for_url("**/ecommerce_products.html")
                products_observation = await observe_page(scout)
                search = _element(
                    products_observation, role="searchbox", name="Search products"
                )
                search_candidate = next(
                    candidate
                    for candidate in products_observation.action_candidates
                    if candidate.kind == "form_submit_candidate"
                    and candidate.attributes.get("id") == "submit_search"
                )
                self.assertEqual(search_candidate.action, "click")
                self.assertEqual(search_candidate.attributes.get("type"), "button")
                self.assertIsNone(search_candidate.name)

                await scout.get_by_role("searchbox", name="Search products").fill("Blue Top")
                await scout.locator("#submit_search").click()
                await scout.get_by_text("Searched Products", exact=True).wait_for()
                filtered_observation = await observe_page(scout)
                view_product = _element_in_scope(
                    filtered_observation,
                    role="link",
                    name="View Product",
                    scope_kind="card",
                    scope_text="Blue Top",
                )

                await scout.locator("#blue-top-view").click()
                await scout.wait_for_url("**/ecommerce_detail.html?product=blue-top")
                detail_observation = await observe_page(scout)
                quantity = _element(
                    detail_observation, role="spinbutton", name="Quantity"
                )
                add_to_cart = _element(
                    detail_observation, role="button", name="Add to cart"
                )

                await scout.get_by_role("spinbutton", name="Quantity").fill("3")
                await scout.get_by_role("button", name="Add to cart").click()
                await scout.get_by_text("Added!", exact=True).wait_for()
                modal_observation = await observe_page(scout)
                view_cart = _element(modal_observation, role="link", name="View Cart")
                await scout.get_by_role("link", name="View Cart").click()
                await scout.wait_for_url("**/ecommerce_cart.html")
                cart_row = scout.locator("#cart-row")
                self.assertEqual(
                    await cart_row.get_by_text(
                        "Blue Top quantity 3", exact=True
                    ).count(),
                    1,
                )
                self.assertEqual(await cart_row.locator("input, select").count(), 0)
                await scout.close()

                steps = [
                    goto_step(0, login_url, "/ecommerce_login.html"),
                    _step_with_target(
                        action_step(
                            1,
                            "input",
                            page_url=login_url,
                            pre=[condition("url_contains", "ecommerce_login.html")],
                            post=[condition("value_equals", "shopper@example.test")],
                            value="shopper@example.test",
                        ),
                        _element_target(login_observation, email),
                    ),
                    _step_with_target(
                        action_step(
                            2,
                            "input",
                            page_url=login_url,
                            pre=[condition("url_contains", "ecommerce_login.html")],
                            post=[condition("value_equals", "offline-secret")],
                            value="offline-secret",
                        ),
                        _element_target(login_observation, password),
                    ),
                    _step_with_target(
                        action_step(
                            3,
                            "click",
                            page_url=login_url,
                            pre=[condition("url_contains", "ecommerce_login.html")],
                            post=[
                                condition("url_contains", "ecommerce_products.html")
                            ],
                        ),
                        _element_target(login_observation, login),
                    ),
                    _step_with_target(
                        action_step(
                            4,
                            "click",
                            page_url=products_url,
                            pre=[condition("url_contains", "ecommerce_products.html")],
                            post=[condition("text_visible", "All Products")],
                        ),
                        _element_target(signed_in_observation, products),
                    ),
                    _step_with_target(
                        action_step(
                            5,
                            "input",
                            page_url=products_url,
                            pre=[condition("url_contains", "ecommerce_products.html")],
                            post=[condition("value_equals", "Blue Top")],
                            value="Blue Top",
                        ),
                        _element_target(products_observation, search),
                    ),
                    _step_with_target(
                        action_step(
                            6,
                            "click",
                            page_url=products_url,
                            pre=[condition("url_contains", "ecommerce_products.html")],
                            post=[
                                condition("text_visible", "Searched Products"),
                                condition("text_gone", "Men Tshirt"),
                            ],
                        ),
                        _candidate_target(products_observation, search_candidate),
                    ),
                    _step_with_target(
                        action_step(
                            7,
                            "click",
                            page_url=products_url,
                            pre=[condition("text_visible", "Blue Top")],
                            post=[condition("url_contains", "ecommerce_detail.html")],
                        ),
                        _element_target(
                            filtered_observation,
                            view_product,
                            spec={
                                "object": {
                                    "role": "link",
                                    "text": "View Product",
                                    "aliases": [],
                                },
                                "scope": {
                                    "kind": "card",
                                    "contains_text": "Blue Top",
                                },
                                "relation": "within",
                            },
                        ),
                    ),
                    _step_with_target(
                        action_step(
                            8,
                            "input",
                            page_url=detail_url,
                            pre=[condition("url_contains", "ecommerce_detail.html")],
                            post=[condition("value_equals", "3")],
                            value="3",
                        ),
                        _element_target(detail_observation, quantity),
                    ),
                    _step_with_target(
                        action_step(
                            9,
                            "click",
                            page_url=detail_url,
                            pre=[condition("url_contains", "ecommerce_detail.html")],
                            post=[condition("text_visible", "Added!")],
                        ),
                        _element_target(detail_observation, add_to_cart),
                    ),
                    _step_with_target(
                        action_step(
                            10,
                            "click",
                            page_url=detail_url,
                            pre=[condition("text_visible", "Added!")],
                            post=[
                                condition("url_changes", "changed"),
                                condition("url_contains", "ecommerce_cart.html"),
                            ],
                        ),
                        _element_target(modal_observation, view_cart),
                    ),
                    action_step(
                        11,
                        "assert_text",
                        page_url=cart_url,
                        pre=[condition("url_contains", "ecommerce_cart.html")],
                        post=[condition("text_visible", "Blue Top quantity 3")],
                        value="Blue Top quantity 3",
                    ),
                ]
                case = build_case(
                    steps,
                    name="login, search, and add Blue Top quantity 3",
                    base_url=site.base_url,
                )

                search_step = case["steps"][6]
                self.assertEqual(
                    search_step["target"]["grounding"]["candidate_id"],
                    search_candidate.candidate_id,
                )

                result = await run_case(
                    case, browser=browser, session_id=TEST_SESSION
                )

        self.assertEqual(result.status, "passed", result.model_dump())
        self.assertEqual(len(result.steps), 12)
        self.assertTrue(all(step.status == "passed" for step in result.steps))
        self.assertTrue(result.final_url.endswith("/ecommerce_cart.html"))
        for step in result.steps:
            self.assertTrue(step.evidence.screenshot_path, step.index)
            self.assertTrue(
                (artifacts_dir() / step.evidence.screenshot_path).is_file(),
                step.evidence.screenshot_path,
            )
            self.assertIsInstance(step.url_before, str)
            self.assertIsInstance(step.url_after, str)
            self.assertIsInstance(step.evidence.console, list)
            self.assertIsInstance(step.evidence.network, list)


if __name__ == "__main__":
    unittest.main()
