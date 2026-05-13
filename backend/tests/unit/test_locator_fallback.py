"""Tests for fallback locator chain."""

from __future__ import annotations

import logging
from unittest.mock import MagicMock

import pytest

import app.locators.fallback as fallback_module
from app.locators.ai_visual import get_ai_visual_runtime_stats, reset_ai_visual_runtime_state
from app.locators.corrections import SQLAlchemyCorrectionStore
from app.locators import InterventionNeededError, resolve_with_fallback
from app.locators.semantic import LocatorResolutionError
from app.models import LocatorCorrection, LocatorCorrectionEvent, TestCase, TestCaseRun
from app.schemas.executions import LocatorTrace


class FakeLocatorCollection:
    def __init__(self, *, page=None, target: str = "", should_fail: bool = False) -> None:
        self.page = page
        self.target = target
        self.should_fail = should_fail

    def wait_for(self, *, state: str, timeout: int) -> None:
        should_fail = self.should_fail
        if self.page is not None and self.target:
            should_fail = self.page.locator_failures.get(self.target, should_fail)
        if should_fail:
            raise RuntimeError("correction failed")

    def count(self) -> int:
        return 0

    def evaluate(self, script: str, *_args):
        if self.page is None:
            return None
        if "getBoundingClientRect" in script:
            return self.page.locator_payloads.get(self.target)
        return None


class FakePage:
    def __init__(
        self,
        *,
        url: str,
        correction_should_fail: bool = False,
        ai_payload: dict | None = None,
        locator_payloads: dict[str, dict] | None = None,
        locator_failures: dict[str, bool] | None = None,
        screenshot_error: Exception | None = None,
    ) -> None:
        self.url = url
        self.viewport_size = {"width": 1280, "height": 720}
        self.correction_should_fail = correction_should_fail
        self.ai_payload = ai_payload
        self.locator_payloads = locator_payloads or {}
        self.locator_failures = locator_failures or {}
        self.screenshot_error = screenshot_error
        self.screenshot_calls: list[bool] = []

    def locator(self, target: str):
        should_fail = self.correction_should_fail and target not in self.locator_failures
        return FakeLocatorCollection(page=self, target=target, should_fail=should_fail)

    def get_by_test_id(self, _target: str):
        return FakeLocatorCollection(should_fail=self.correction_should_fail)

    def get_by_label(self, _target: str, exact: bool = True):
        return FakeLocatorCollection()

    def get_by_placeholder(self, _target: str, exact: bool = True):
        return FakeLocatorCollection()

    def get_by_role(self, role: str, name: str, exact: bool = True):
        return FakeLocatorCollection()

    def get_by_text(self, _target: str, exact: bool = True):
        return FakeLocatorCollection()

    def evaluate(self, script: str, *_args):
        if "elementsFromPoint" in script or "elementFromPoint" in script:
            return self.ai_payload
        if "querySelectorAll" in script:
            return []
        return None

    def screenshot(self, *, full_page: bool = True):
        self.screenshot_calls.append(full_page)
        if self.screenshot_error is not None:
            raise self.screenshot_error
        return b"fake"


@pytest.fixture(autouse=True)
def _reset_logging_propagation() -> None:
    """Ensure app loggers propagate to root so caplog can capture them.

    setup_logging() (called by create_app) sets app logger propagate=False,
    which breaks caplog in the full test suite when other test files create
    a TestClient (which calls create_app -> setup_logging).
    """
    fallback_module._clear_ai_visual_session_cache()
    reset_ai_visual_runtime_state()
    logging.getLogger("app").propagate = True


def _create_source_execution(db_session) -> int:
    case = TestCase(
        project_id=1,
        created_by=1,
        updated_by=1,
        name="fallback case",
        description=None,
        dsl={"name": "fallback case", "steps": [{"action": "click", "target": "登录按钮"}]},
    )
    db_session.add(case)
    db_session.commit()
    db_session.refresh(case)

    execution = TestCaseRun(
        case_id=case.id,
        project_id=1,
        triggered_by=1,
        status="failed",
        error_message="boom",
    )
    db_session.add(execution)
    db_session.commit()
    db_session.refresh(execution)
    return execution.id


def test_resolve_with_fallback_uses_active_correction(db_session, monkeypatch) -> None:
    execution_id = _create_source_execution(db_session)
    correction = LocatorCorrection(
        page_url_pattern="https://app.example.com/users/*",
        target_description="登录按钮",
        normalized_target_description="登录按钮",
        correction_type="css",
        correction_value="#login-btn",
        source_execution_id=execution_id,
        created_by=1,
    )
    db_session.add(correction)
    db_session.commit()
    db_session.refresh(correction)

    flush_spy = MagicMock(wraps=db_session.flush)
    monkeypatch.setattr(db_session, "flush", flush_spy)

    page = FakePage(url="https://app.example.com/users/123")
    resolved = resolve_with_fallback(
        page,
        "登录按钮",
        correction_store=SQLAlchemyCorrectionStore(db_session),
        execution_id=execution_id,
        require_enabled=True,
    )

    assert resolved.strategy == "correction:css"
    assert correction.verified_count == 1
    assert correction.consecutive_failures == 0
    assert correction.is_active is True
    assert flush_spy.call_count == 1
    events = db_session.query(LocatorCorrectionEvent).order_by(LocatorCorrectionEvent.id.asc()).all()
    assert [event.event_type for event in events] == ["tier0_hit"]
    assert events[0].execution_id == execution_id


def test_resolve_with_fallback_matches_correction_case_insensitively(db_session) -> None:
    execution_id = _create_source_execution(db_session)
    correction = LocatorCorrection(
        page_url_pattern="https://app.example.com/users/*",
        target_description="Login Button",
        normalized_target_description="login button",
        correction_type="css",
        correction_value="#login-btn",
        source_execution_id=execution_id,
        created_by=1,
    )
    db_session.add(correction)
    db_session.commit()

    page = FakePage(url="https://app.example.com/users/123")
    resolved = resolve_with_fallback(
        page,
        "login button",
        correction_store=SQLAlchemyCorrectionStore(db_session),
        execution_id=execution_id,
        require_enabled=True,
    )

    assert resolved.strategy == "correction:css"


def test_resolve_with_fallback_disables_correction_after_three_failures(db_session, monkeypatch, caplog) -> None:
    execution_id = _create_source_execution(db_session)
    correction = LocatorCorrection(
        page_url_pattern="https://app.example.com/users/*",
        target_description="登录按钮",
        normalized_target_description="登录按钮",
        correction_type="css",
        correction_value="#login-btn",
        source_execution_id=execution_id,
        created_by=1,
    )
    db_session.add(correction)
    db_session.commit()
    db_session.refresh(correction)

    flush_spy = MagicMock(wraps=db_session.flush)
    monkeypatch.setattr(db_session, "flush", flush_spy)

    page = FakePage(url="https://app.example.com/users/123", correction_should_fail=True)
    with caplog.at_level("WARNING", logger="app"):
        for _ in range(3):
            with pytest.raises(InterventionNeededError):
                resolve_with_fallback(
                    page,
                    "登录按钮",
                    correction_store=SQLAlchemyCorrectionStore(db_session),
                    execution_id=execution_id,
                    require_enabled=True,
                )

    assert correction.consecutive_failures == 3
    assert correction.is_active is False
    assert flush_spy.call_count == 3
    assert "Correction reuse failed" in caplog.text
    events = db_session.query(LocatorCorrectionEvent).order_by(LocatorCorrectionEvent.id.asc()).all()
    assert [event.event_type for event in events] == [
        "tier0_miss",
        "tier0_miss",
        "tier0_miss",
        "auto_deactivated",
    ]
    assert events[-1].is_active_after is False


def test_resolve_with_fallback_uses_viewport_screenshot_for_ai_candidate(monkeypatch) -> None:
    ai_payload = {
        "tag": "button",
        "text": "登录按钮",
        "role": "button",
        "aria_label": "登录按钮",
        "placeholder": None,
        "data_testid": "login-button",
        "css_selector": "#login-btn",
        "xpath": "/html/body/button[1]",
        "rect": {"x": 150, "y": 80, "width": 100, "height": 40},
        "visible": True,
        "enabled": True,
    }
    page = FakePage(url="https://app.example.com/login", ai_payload=ai_payload)

    monkeypatch.setattr(
        "app.locators.fallback.locate_element_by_vision",
        lambda **_kwargs: type(
            "Candidate",
            (),
            {"center": (200, 100), "bbox": (150, 80, 250, 120), "confidence": 0.7, "raw_response": "{}"},
        )(),
    )
    monkeypatch.setattr(
        "app.locators.fallback.collect_semantic_candidates",
        MagicMock(return_value=[]),
    )

    resolved = resolve_with_fallback(page, "登录按钮")

    assert resolved.strategy == "ai_visual"
    assert page.screenshot_calls == [True]


def test_resolve_with_fallback_logs_ai_capture_failures(monkeypatch, caplog) -> None:
    page = FakePage(
        url="https://app.example.com/login",
        screenshot_error=RuntimeError("screenshot boom"),
    )
    monkeypatch.setattr(
        "app.locators.fallback.collect_semantic_candidates",
        MagicMock(return_value=[]),
    )

    with caplog.at_level("WARNING", logger="app"):
        with pytest.raises(InterventionNeededError):
            resolve_with_fallback(page, "登录按钮")

    assert "AI visual fallback failed" in caplog.text


def test_resolve_with_fallback_reuses_cached_ai_visual_selector(monkeypatch) -> None:
    ai_payload = {
        "tag": "button",
        "text": "登录按钮",
        "role": "button",
        "aria_label": "登录按钮",
        "placeholder": None,
        "data_testid": "login-button",
        "css_selector": "#login-btn",
        "xpath": "/html/body/button[1]",
        "rect": {"x": 150, "y": 80, "width": 100, "height": 40},
        "visible": True,
        "enabled": True,
    }
    first_page = FakePage(url="https://app.example.com/users/123", ai_payload=ai_payload)
    second_page = FakePage(
        url="https://app.example.com/users/456",
        locator_payloads={"#login-btn": ai_payload},
    )

    semantic_spy = MagicMock(return_value=[])
    monkeypatch.setattr("app.locators.fallback.collect_semantic_candidates", semantic_spy)
    monkeypatch.setattr(
        "app.locators.fallback.locate_element_by_vision",
        lambda **_kwargs: type(
            "Candidate",
            (),
            {"center": (200, 100), "bbox": (150, 80, 250, 120), "confidence": 0.7, "raw_response": "{}"},
        )(),
    )

    first_resolved = resolve_with_fallback(first_page, "登录按钮")
    assert first_resolved.strategy == "ai_visual"

    semantic_spy.reset_mock()
    second_resolved = resolve_with_fallback(second_page, "登录按钮")

    assert second_resolved.strategy == "ai_visual_cache"
    assert semantic_spy.call_count == 0
    assert second_page.screenshot_calls == []
    stats = get_ai_visual_runtime_stats()
    assert stats.cache_miss_count == 1
    assert stats.cache_hit_count == 1
    assert stats.cache_invalidated_count == 0


def test_resolve_with_fallback_invalidates_cached_visible_selector_when_semantics_drift(monkeypatch, caplog) -> None:
    ai_payload = {
        "tag": "button",
        "text": "鐧诲綍鎸夐挳",
        "role": "button",
        "aria_label": "鐧诲綍鎸夐挳",
        "placeholder": None,
        "data_testid": "login-button",
        "css_selector": "#login-btn",
        "xpath": "/html/body/button[1]",
        "rect": {"x": 150, "y": 80, "width": 100, "height": 40},
        "visible": True,
        "enabled": True,
    }
    wrong_visible_payload = {
        **ai_payload,
        "text": "鍙栨秷",
        "aria_label": "鍙栨秷",
        "data_testid": "cancel-button",
    }
    first_page = FakePage(url="https://app.example.com/users/123", ai_payload=ai_payload)
    second_page = FakePage(
        url="https://app.example.com/users/456",
        ai_payload=ai_payload,
        locator_payloads={"#login-btn": wrong_visible_payload},
    )

    semantic_spy = MagicMock(return_value=[])
    monkeypatch.setattr("app.locators.fallback.collect_semantic_candidates", semantic_spy)
    monkeypatch.setattr(
        "app.locators.fallback.locate_element_by_vision",
        lambda **_kwargs: type(
            "Candidate",
            (),
            {"center": (200, 100), "bbox": (150, 80, 250, 120), "confidence": 0.7, "raw_response": "{}"},
        )(),
    )

    resolve_with_fallback(first_page, "鐧诲綍鎸夐挳")

    semantic_spy.reset_mock()
    with caplog.at_level("DEBUG", logger="app"):
        resolved = resolve_with_fallback(second_page, "鐧诲綍鎸夐挳")

    assert resolved.strategy == "ai_visual"
    assert semantic_spy.call_count == 1
    assert second_page.screenshot_calls == [True]
    assert "reason=semantic_mismatch" in caplog.text
    stats = get_ai_visual_runtime_stats()
    assert stats.cache_miss_count == 1
    assert stats.cache_hit_count == 0
    assert stats.cache_invalidated_count == 1


def test_resolve_with_fallback_invalidates_cached_ai_visual_selector_when_locator_is_stale(monkeypatch, caplog) -> None:
    ai_payload = {
        "tag": "button",
        "text": "登录按钮",
        "role": "button",
        "aria_label": "登录按钮",
        "placeholder": None,
        "data_testid": "login-button",
        "css_selector": "#login-btn",
        "xpath": "/html/body/button[1]",
        "rect": {"x": 150, "y": 80, "width": 100, "height": 40},
        "visible": True,
        "enabled": True,
    }
    first_page = FakePage(url="https://app.example.com/users/123", ai_payload=ai_payload)
    stale_page = FakePage(
        url="https://app.example.com/users/456",
        locator_failures={"#login-btn": True},
    )

    monkeypatch.setattr(
        "app.locators.fallback.collect_semantic_candidates",
        MagicMock(return_value=[]),
    )
    monkeypatch.setattr(
        "app.locators.fallback.locate_element_by_vision",
        lambda **_kwargs: type(
            "Candidate",
            (),
            {"center": (200, 100), "bbox": (150, 80, 250, 120), "confidence": 0.7, "raw_response": "{}"},
        )(),
    )

    resolve_with_fallback(first_page, "登录按钮")

    with caplog.at_level("DEBUG", logger="app"):
        result = resolve_with_fallback(stale_page, "登录按钮")
        # Tier 2.5 coordinate click fallback returns coordinates instead of raising
        assert result.click_coordinates == (200, 100)
        assert result.strategy == "ai_coordinate_click"

    assert "cache_invalidated" in caplog.text


def test_resolve_with_fallback_prioritizes_tier_zero_correction_over_ai_visual_cache(db_session, monkeypatch) -> None:
    execution_id = _create_source_execution(db_session)
    correction = LocatorCorrection(
        page_url_pattern="https://app.example.com/users/*",
        target_description="登录按钮",
        normalized_target_description="登录按钮",
        correction_type="css",
        correction_value="#login-btn",
        source_execution_id=execution_id,
        created_by=1,
    )
    db_session.add(correction)
    db_session.commit()

    ai_payload = {
        "tag": "button",
        "text": "登录按钮",
        "role": "button",
        "aria_label": "登录按钮",
        "placeholder": None,
        "data_testid": "login-button",
        "css_selector": "#login-btn",
        "xpath": "/html/body/button[1]",
        "rect": {"x": 150, "y": 80, "width": 100, "height": 40},
        "visible": True,
        "enabled": True,
    }
    cache_seed_page = FakePage(url="https://app.example.com/users/123", ai_payload=ai_payload)
    correction_page = FakePage(url="https://app.example.com/users/456", screenshot_error=RuntimeError("should not capture"))

    monkeypatch.setattr(
        "app.locators.fallback.collect_semantic_candidates",
        MagicMock(return_value=[]),
    )
    monkeypatch.setattr(
        "app.locators.fallback.locate_element_by_vision",
        lambda **_kwargs: type(
            "Candidate",
            (),
            {"center": (200, 100), "bbox": (150, 80, 250, 120), "confidence": 0.7, "raw_response": "{}"},
        )(),
    )

    resolve_with_fallback(cache_seed_page, "登录按钮")

    resolved = resolve_with_fallback(
        correction_page,
        "登录按钮",
        correction_store=SQLAlchemyCorrectionStore(db_session),
        execution_id=execution_id,
    )

    assert resolved.strategy == "correction:css"
    assert correction_page.screenshot_calls == []


def test_coordinate_click_fallback_returns_valid_coordinates(monkeypatch) -> None:
    """When VLM returns bbox but DOM selector extraction fails, Tier 2.5 returns coordinates."""
    page = FakePage(
        url="https://app.example.com/modal",
        ai_payload=None,  # DOM snapshot at point returns None
    )

    monkeypatch.setattr(
        "app.locators.fallback.collect_semantic_candidates",
        MagicMock(return_value=[]),
    )
    monkeypatch.setattr(
        "app.locators.fallback.locate_element_by_vision",
        lambda **_kwargs: type(
            "Candidate",
            (),
            {"center": (300, 200), "bbox": (250, 180, 350, 220), "confidence": 0.8, "raw_response": "{}"},
        )(),
    )

    result = resolve_with_fallback(page, "View Cart")
    assert result.strategy == "ai_coordinate_click"
    assert result.click_coordinates == (300, 200)
    assert result.trace.match_strategy == "ai_coordinate_click"


def test_coordinate_click_fallback_skipped_when_vlm_returns_none(monkeypatch) -> None:
    """When VLM also fails, InterventionNeededError is still raised."""
    page = FakePage(url="https://app.example.com/page")

    monkeypatch.setattr(
        "app.locators.fallback.collect_semantic_candidates",
        MagicMock(return_value=[]),
    )
    monkeypatch.setattr(
        "app.locators.fallback.locate_element_by_vision",
        lambda **_kwargs: None,
    )

    with pytest.raises(InterventionNeededError):
        resolve_with_fallback(page, "missing element")


def test_format_elements_for_prompt_marks_dynamic_elements() -> None:
    """format_elements_for_prompt adds [dynamic] tag for interactive-discovered elements."""
    from app.ai.page_explorer import format_elements_for_prompt

    elements = [
        {"tag": "button", "text": "Login", "visible": True},
        {"tag": "a", "text": "View Cart", "visible": True, "discovered_via_interaction": True},
    ]
    result = format_elements_for_prompt(elements)
    assert "[text='Login']" in result
    assert "[text='View Cart']" in result
    assert "[dynamic]" in result


# ---------------------------------------------------------------------------
# (removed) Accessibility tree tier — module deleted in cleanup phase 1.3
# ---------------------------------------------------------------------------

