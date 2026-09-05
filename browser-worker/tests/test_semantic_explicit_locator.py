from __future__ import annotations

import unittest

from app.locators.semantic import _resolve_explicit_locator


class FakePage:
    def __init__(self) -> None:
        self.selectors: list[str] = []

    def locator(self, selector: str) -> str:
        self.selectors.append(selector)
        return selector


class ExplicitLocatorTest(unittest.TestCase):
    def test_recognizes_tag_class_selector(self) -> None:
        page = FakePage()

        resolved = _resolve_explicit_locator(page, "button.cart")

        self.assertIsNotNone(resolved)
        strategy, builder = resolved  # type: ignore[misc]
        self.assertEqual(strategy, "css")
        self.assertEqual(builder(), "button.cart")

    def test_recognizes_tag_attribute_selector(self) -> None:
        page = FakePage()

        resolved = _resolve_explicit_locator(page, 'a[href="/view_cart"]')

        self.assertIsNotNone(resolved)
        strategy, builder = resolved  # type: ignore[misc]
        self.assertEqual(strategy, "css")
        self.assertEqual(builder(), 'a[href="/view_cart"]')

    def test_plain_text_remains_semantic(self) -> None:
        page = FakePage()

        self.assertIsNone(_resolve_explicit_locator(page, "Add to cart"))


if __name__ == "__main__":
    unittest.main()
