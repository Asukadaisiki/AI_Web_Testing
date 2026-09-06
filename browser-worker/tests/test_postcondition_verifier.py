from __future__ import annotations

import unittest

from app.runners.postcondition_verifier import PostconditionVerifier
from app.schemas.dsl import Postcondition


class FakeLocator:
    def __init__(self, visibility: list[bool]) -> None:
        self.visibility = visibility
        self.index = 0

    def count(self) -> int:
        return len(self.visibility)

    def nth(self, index: int) -> "FakeLocator":
        item = FakeLocator(self.visibility)
        item.index = index
        return item

    def is_visible(self) -> bool:
        return self.visibility[self.index]

    def all(self) -> list["FakeLocator"]:
        return []


class FakePage:
    url = "https://example.com"

    def __init__(self, visibility: list[bool]) -> None:
        self.visibility = visibility

    def locator(self, selector: str) -> FakeLocator:
        self.last_selector = selector
        return FakeLocator(self.visibility)

    def evaluate(self, script: str) -> str:
        self.last_script = script
        return ""


class ChangingURLPage(FakePage):
    def __init__(self) -> None:
        super().__init__([])
        self.url_reads = 0

    @property
    def url(self) -> str:
        self.url_reads += 1
        if self.url_reads >= 4:
            return "https://example.com/details/1"
        return "https://example.com/products"


class PostconditionVerifierTest(unittest.TestCase):
    def test_text_visible_passes_when_any_match_is_visible(self) -> None:
        verifier = PostconditionVerifier(FakePage([False, True]))  # type: ignore[arg-type]

        result = verifier.verify(
            [Postcondition(type="text_visible", value="Blue Top")]
        )

        self.assertTrue(result.passed)

    def test_text_gone_fails_when_any_match_is_visible(self) -> None:
        verifier = PostconditionVerifier(FakePage([False, True]))  # type: ignore[arg-type]

        result = verifier.verify(
            [Postcondition(type="text_gone", value="Blue Top", timeout_ms=100)]
        )

        self.assertFalse(result.passed)

    def test_url_postcondition_waits_until_destination_arrives(self) -> None:
        page = ChangingURLPage()
        verifier = PostconditionVerifier(page)  # type: ignore[arg-type]
        verifier.capture_pre_state()

        result = verifier.verify(
            [
                Postcondition(
                    type="url_contains",
                    value="/details/1",
                    timeout_ms=500,
                )
            ]
        )

        self.assertTrue(result.passed)
        self.assertGreaterEqual(page.url_reads, 4)


if __name__ == "__main__":
    unittest.main()
