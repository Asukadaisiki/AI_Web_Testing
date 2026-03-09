"""Tests for semantic locator resolution."""

from __future__ import annotations

import pytest

from app.locators import LocatorResolutionError, resolve_semantic_locator


class FakeNodeLocator:
    def __init__(self, payload: dict) -> None:
        self.payload = payload

    def evaluate(self, _script: str):
        return self.payload


class FakeLocatorCollection:
    def __init__(self, payloads: list[dict]) -> None:
        self.payloads = payloads

    def count(self) -> int:
        return len(self.payloads)

    def nth(self, index: int) -> FakeNodeLocator:
        return FakeNodeLocator(self.payloads[index])


class FakePage:
    def __init__(self, mapping: dict[str, list[dict]]) -> None:
        self.mapping = mapping

    def locator(self, target: str):
        return FakeLocatorCollection(self.mapping.get(f"locator:{target}", []))

    def get_by_label(self, target: str, exact: bool = True):
        return FakeLocatorCollection(self.mapping.get(f"label:{target}:{exact}", []))

    def get_by_placeholder(self, target: str, exact: bool = True):
        return FakeLocatorCollection(self.mapping.get(f"placeholder:{target}:{exact}", []))

    def get_by_role(self, role: str, name: str, exact: bool = True):
        return FakeLocatorCollection(self.mapping.get(f"role:{role}:{name}:{exact}", []))

    def get_by_text(self, target: str, exact: bool = True):
        return FakeLocatorCollection(self.mapping.get(f"text:{target}:{exact}", []))

    def get_by_test_id(self, target: str):
        return FakeLocatorCollection(self.mapping.get(f"testid:{target}", []))


def _candidate(*, preview_text: str, visible: bool, enabled: bool, role: str = "button") -> dict:
    return {
        "preview_text": preview_text,
        "role": role,
        "attributes": {
            "aria_label": preview_text,
            "placeholder": None,
            "data_testid": None,
        },
        "visible": visible,
        "enabled": enabled,
    }


def test_resolve_semantic_locator_returns_trace_with_candidates() -> None:
    page = FakePage(
        {
            "role:button:登录按钮:True": [_candidate(preview_text="登录", visible=True, enabled=True)],
        }
    )

    resolved = resolve_semantic_locator(page, "登录按钮", require_visible=True, require_enabled=True)

    assert resolved.strategy == "button_role"
    assert resolved.trace.match_strategy == "button_role"
    assert resolved.trace.selected_candidate is not None
    assert resolved.trace.selected_candidate.preview_text == "登录"


def test_resolve_semantic_locator_reports_visibility_failure() -> None:
    page = FakePage(
        {
            "role:button:提交按钮:True": [_candidate(preview_text="提交", visible=False, enabled=True)],
        }
    )

    with pytest.raises(LocatorResolutionError) as exc_info:
        resolve_semantic_locator(page, "提交按钮", require_visible=True, require_enabled=True)

    assert exc_info.value.trace is not None
    assert exc_info.value.trace.failure_reason == "Locator candidates matched target but none are visible."
    assert exc_info.value.trace.candidates[0].enabled is True
