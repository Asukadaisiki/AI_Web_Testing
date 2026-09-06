from __future__ import annotations

import unittest

from app.runners.postcondition_verifier import PostconditionVerifier, StepNetworkObserver
from app.schemas.dsl import Postcondition
from app.schemas.executions import NetworkEvent


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


class ObserverPage(FakePage):
    def __init__(self) -> None:
        super().__init__([])
        self.listeners: dict[str, list] = {}
        self.pending_response = None

    def on(self, event_name: str, callback) -> None:
        self.listeners.setdefault(event_name, []).append(callback)

    def remove_listener(self, event_name: str, callback) -> None:
        self.listeners[event_name].remove(callback)

    def emit(self, event_name: str, event) -> None:
        for callback in list(self.listeners.get(event_name, [])):
            callback(event)

    def wait_for_timeout(self, _timeout_ms: int) -> None:
        if self.pending_response is not None:
            response = self.pending_response
            self.pending_response = None
            self.emit("response", response)


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

    def test_network_request_requires_one_event_to_match_every_field(self) -> None:
        verifier = PostconditionVerifier(FakePage([]))  # type: ignore[arg-type]
        events = [
            NetworkEvent(
                event_type="response",
                url="https://example.com/api/cart",
                method="GET",
                status=201,
            ),
            NetworkEvent(
                event_type="response",
                url="https://example.com/api/profile",
                method="POST",
                status=200,
            ),
        ]

        result = verifier.verify(
            [
                Postcondition(
                    type="network_request",
                    value="/api/cart",
                    method="POST",
                    status=201,
                    timeout_ms=100,
                )
            ],
            network_events=events,
        )

        self.assertFalse(result.passed)
        self.assertEqual(result.results[0].actual, {"observed_events": 2})

    def test_network_request_records_duplicate_conditions_by_index(self) -> None:
        verifier = PostconditionVerifier(FakePage([]))  # type: ignore[arg-type]
        event = NetworkEvent(
            event_type="response",
            url="https://example.com/api/cart/items",
            method="POST",
            status=201,
        )

        result = verifier.verify(
            [
                Postcondition(
                    type="network_request",
                    value="/api/cart",
                    method="POST",
                    status=201,
                    timeout_ms=100,
                ),
                Postcondition(
                    type="network_request",
                    value="/items",
                    method="POST",
                    status=201,
                    timeout_ms=100,
                ),
            ],
            network_events=[event],
        )

        self.assertTrue(result.passed)
        self.assertEqual([item.index for item in result.results], [0, 1])
        self.assertEqual(
            [item.type for item in result.results],
            ["network_request", "network_request"],
        )

    def test_network_request_fails_without_observation(self) -> None:
        verifier = PostconditionVerifier(FakePage([]))  # type: ignore[arg-type]

        result = verifier.verify(
            [
                Postcondition(
                    type="network_request",
                    value="/missing",
                    timeout_ms=100,
                )
            ],
            network_events=[],
        )

        self.assertFalse(result.passed)
        self.assertEqual(result.results[0].status, "failed")

    def test_step_network_observer_isolates_all_lifecycle_event_types(self) -> None:
        page = ObserverPage()
        request = type(
            "Request",
            (),
            {
                "url": "https://example.com/api/cart",
                "method": "POST",
                "resource_type": "fetch",
                "failure": "connection reset",
            },
        )()
        response = type(
            "Response",
            (),
            {"url": request.url, "status": 201, "request": request},
        )()
        observer = StepNetworkObserver(page).start()  # type: ignore[arg-type]

        page.emit("request", request)
        page.emit("response", response)
        page.emit("requestfailed", request)
        observer.stop()

        self.assertEqual(
            [event.event_type for event in observer.events],
            ["request", "response", "requestfailed"],
        )
        self.assertTrue(all(not callbacks for callbacks in page.listeners.values()))

    def test_network_wait_pumps_delayed_playwright_events(self) -> None:
        page = ObserverPage()
        request = type(
            "Request",
            (),
            {
                "url": "https://example.com/api/cart",
                "method": "POST",
                "resource_type": "fetch",
            },
        )()
        page.pending_response = type(
            "Response",
            (),
            {"url": request.url, "status": 201, "request": request},
        )()
        observer = StepNetworkObserver(page).start()  # type: ignore[arg-type]
        verifier = PostconditionVerifier(page)  # type: ignore[arg-type]

        result = verifier.verify(
            [
                Postcondition(
                    type="network_request",
                    value="/api/cart",
                    method="POST",
                    status=201,
                    timeout_ms=200,
                )
            ],
            network_events=observer.events,
        )
        observer.stop()

        self.assertTrue(result.passed)


if __name__ == "__main__":
    unittest.main()
