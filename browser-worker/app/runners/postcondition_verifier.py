"""Step-scoped condition verification and network observation."""

from __future__ import annotations

import hashlib
import logging
from dataclasses import dataclass, field
from time import monotonic, sleep
from typing import Callable

from playwright.sync_api import Page

from app.schemas.dsl import ConditionSpec
from app.schemas.executions import ConditionResult, NetworkEvent, PageStateSnapshot

logger = logging.getLogger(__name__)


@dataclass
class VerificationResult:
    passed: bool
    results: list[ConditionResult] = field(default_factory=list)

    @property
    def details(self) -> dict[str, str]:
        return {
            f"{result.phase}[{result.index}]/{result.type}": result.error or "failed"
            for result in self.results
            if result.status != "passed"
        }


PostconditionResult = VerificationResult


class StepNetworkObserver:
    """Collect request lifecycle events for exactly one executing step."""

    def __init__(self, page: Page) -> None:
        self._page = page
        self.events: list[NetworkEvent] = []
        self._listeners: list[tuple[str, Callable]] = []

    def start(self) -> "StepNetworkObserver":
        add_listener = getattr(self._page, "on", None)
        if add_listener is None:
            return self
        listeners = (
            ("request", self._on_request),
            ("response", self._on_response),
            ("requestfailed", self._on_request_failed),
        )
        for event_name, callback in listeners:
            add_listener(event_name, callback)
            self._listeners.append((event_name, callback))
        return self

    def stop(self) -> None:
        remove_listener = getattr(self._page, "remove_listener", None) or getattr(
            self._page, "off", None
        )
        if remove_listener is not None:
            for event_name, callback in self._listeners:
                remove_listener(event_name, callback)
        self._listeners.clear()

    def _on_request(self, request) -> None:
        self.events.append(
            NetworkEvent(
                event_type="request",
                url=request.url,
                method=request.method,
                resource_type=request.resource_type,
            )
        )

    def _on_response(self, response) -> None:
        request = response.request
        self.events.append(
            NetworkEvent(
                event_type="response",
                url=response.url,
                method=request.method,
                status=response.status,
                resource_type=request.resource_type,
            )
        )

    def _on_request_failed(self, request) -> None:
        failure = request.failure
        failure_text = (
            failure if isinstance(failure, str) else (failure or {}).get("errorText")
        )
        self.events.append(
            NetworkEvent(
                event_type="requestfailed",
                url=request.url,
                method=request.method,
                resource_type=request.resource_type,
                failure_text=failure_text,
            )
        )


class PostconditionVerifier:
    """Capture pre-action state and evaluate ordered conditions."""

    def __init__(self, page: Page) -> None:
        self._page = page
        self._pre_state = PageStateSnapshot(url="", dom_hash="")

    def capture_pre_state(self) -> PageStateSnapshot:
        self._pre_state = PageStateSnapshot(
            url=self._page.url,
            dom_hash=self._compute_dom_hash(),
            visible_texts=self._get_visible_texts(),
            input_values=self._get_input_values(),
        )
        return self._pre_state

    def verify(
        self,
        conditions: list[ConditionSpec],
        *,
        phase: str = "postcondition",
        network_events: list[NetworkEvent] | None = None,
    ) -> VerificationResult:
        results: list[ConditionResult] = []
        for index, condition in enumerate(conditions):
            started_at = monotonic()
            expected = self._expected(condition)
            try:
                passed, actual = self._verify_single(
                    condition,
                    network_events=network_events if network_events is not None else [],
                )
                error = None if passed else (
                    f"{phase} '{condition.type}' was not satisfied"
                )
                status = "passed" if passed else "failed"
            except Exception as exc:
                logger.warning("%s %s check failed: %s", phase, condition.type, exc)
                actual = None
                error = f"{type(exc).__name__}: {exc}"
                status = "error"
            results.append(
                ConditionResult(
                    phase=phase,
                    index=index,
                    type=condition.type,
                    expected=expected,
                    actual=actual,
                    status=status,
                    duration_ms=max(0, int((monotonic() - started_at) * 1000)),
                    error=error,
                )
            )
        return VerificationResult(
            passed=all(result.status == "passed" for result in results),
            results=results,
        )

    def _verify_single(
        self,
        condition: ConditionSpec,
        *,
        network_events: list[NetworkEvent],
    ) -> tuple[bool, object]:
        condition_type = condition.type
        value = condition.value

        if condition_type == "url_contains":
            actual = self._wait_for_url(
                lambda current_url: value is not None and value in current_url,
                timeout_ms=condition.timeout_ms,
            )
            return actual[0], actual[1]
        if condition_type == "url_changes":
            actual = self._wait_for_url(
                lambda current_url: current_url != self._pre_state.url,
                timeout_ms=condition.timeout_ms,
            )
            return actual[0], actual[1]
        if condition_type in {"text_visible", "text_gone"}:
            if value is None:
                return condition_type == "text_gone", False
            passed = self._wait_for_visibility(
                self._page.locator(f"text={value}"),
                visible=condition_type == "text_visible",
                timeout_ms=condition.timeout_ms,
            )
            expected_visible = condition_type == "text_visible"
            return passed, expected_visible if passed else not expected_visible
        if condition_type in {"element_visible", "element_gone"}:
            if value is None:
                return condition_type == "element_gone", False
            passed = self._wait_for_visibility(
                self._page.locator(value),
                visible=condition_type == "element_visible",
                timeout_ms=condition.timeout_ms,
            )
            expected_visible = condition_type == "element_visible"
            return passed, expected_visible if passed else not expected_visible
        if condition_type == "dom_changed":
            post_hash = self._compute_dom_hash()
            changed = post_hash != self._pre_state.dom_hash
            return changed, {
                "before": self._pre_state.dom_hash,
                "after": post_hash,
            }
        if condition_type == "value_changed":
            post_values = self._get_input_values()
            changed = post_values != self._pre_state.input_values
            return changed, {
                "before": self._pre_state.input_values,
                "after": post_values,
            }
        if condition_type == "network_request":
            return self._wait_for_network(condition, network_events)
        return False, None

    def _wait_for_network(
        self,
        condition: ConditionSpec,
        events: list[NetworkEvent],
    ) -> tuple[bool, object]:
        deadline = monotonic() + condition.timeout_ms / 1000
        while True:
            for event in list(events):
                if condition.value and condition.value not in event.url:
                    continue
                if condition.method and condition.method != event.method.upper():
                    continue
                if condition.status is not None and condition.status != event.status:
                    continue
                return True, event.model_dump(mode="json")
            if monotonic() >= deadline:
                return False, {"observed_events": len(events)}
            self._pump_events()

    def _pump_events(self) -> None:
        wait_for_timeout = getattr(self._page, "wait_for_timeout", None)
        if wait_for_timeout is not None:
            wait_for_timeout(50)
        else:
            sleep(0.05)

    @staticmethod
    def _expected(condition: ConditionSpec) -> object:
        if condition.type == "network_request":
            return {
                "url_contains": condition.value,
                "method": condition.method,
                "status": condition.status,
            }
        if condition.type in {"url_changes", "dom_changed", "value_changed"}:
            return True
        if condition.type in {"text_gone", "element_gone"}:
            return False
        return condition.value

    @staticmethod
    def _any_visible(locator) -> bool:
        return any(locator.nth(index).is_visible() for index in range(locator.count()))

    @classmethod
    def _wait_for_visibility(cls, locator, *, visible: bool, timeout_ms: int) -> bool:
        deadline = monotonic() + timeout_ms / 1000
        while True:
            if cls._any_visible(locator) is visible:
                return True
            if monotonic() >= deadline:
                return False
            sleep(0.05)

    def _wait_for_url(self, predicate, *, timeout_ms: int) -> tuple[bool, str]:
        deadline = monotonic() + timeout_ms / 1000
        while True:
            current_url = self._page.url
            if predicate(current_url):
                return True, current_url
            if monotonic() >= deadline:
                return False, current_url
            sleep(0.05)

    def _compute_dom_hash(self) -> str:
        try:
            body_html = self._page.evaluate("() => document.body.innerHTML")
            return hashlib.sha256((body_html or "").encode()).hexdigest()
        except Exception:
            return ""

    def _get_visible_texts(self) -> list[str]:
        try:
            texts: list[str] = []
            for selector in ("h1", "h2", "h3", "p", "span", "button", "a", "label"):
                for element in self._page.locator(selector).all():
                    try:
                        if element.is_visible():
                            text = element.inner_text()
                            if text:
                                texts.append(text.strip())
                    except Exception:
                        continue
            return texts
        except Exception:
            return []

    def _get_input_values(self) -> dict[str, str]:
        try:
            values: dict[str, str] = {}
            for element in self._page.locator("input, select, textarea").all():
                try:
                    name = element.get_attribute("name") or element.get_attribute("id") or ""
                    if name:
                        values[name] = element.input_value()
                except Exception:
                    continue
            return values
        except Exception:
            return {}
