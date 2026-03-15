"""Tests for fallback locator chain."""

from __future__ import annotations

from unittest.mock import MagicMock

import pytest

from app.locators.corrections import SQLAlchemyCorrectionStore
from app.locators import InterventionNeededError, resolve_with_fallback
from app.locators.semantic import LocatorResolutionError
from app.models import LocatorCorrection, LocatorCorrectionEvent, TestCase, TestCaseRun
from app.schemas.executions import LocatorTrace


class FakeLocatorCollection:
    def __init__(self, *, should_fail: bool = False) -> None:
        self.should_fail = should_fail

    def wait_for(self, *, state: str, timeout: int) -> None:
        if self.should_fail:
            raise RuntimeError("correction failed")

    def count(self) -> int:
        return 0


class FakePage:
    def __init__(
        self,
        *,
        url: str,
        correction_should_fail: bool = False,
        ai_payload: dict | None = None,
        screenshot_error: Exception | None = None,
    ) -> None:
        self.url = url
        self.viewport_size = {"width": 1280, "height": 720}
        self.correction_should_fail = correction_should_fail
        self.ai_payload = ai_payload
        self.screenshot_error = screenshot_error
        self.screenshot_calls: list[bool] = []

    def locator(self, _target: str):
        return FakeLocatorCollection(should_fail=self.correction_should_fail)

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
        if "elementFromPoint" in script:
            return self.ai_payload
        if "querySelectorAll" in script:
            return []
        return None

    def screenshot(self, *, full_page: bool = True):
        self.screenshot_calls.append(full_page)
        if self.screenshot_error is not None:
            raise self.screenshot_error
        return b"fake"


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
        "app.locators.fallback.resolve_semantic_locator",
        MagicMock(side_effect=LocatorResolutionError("skip", trace=LocatorTrace(target="登录按钮"))),
    )

    resolved = resolve_with_fallback(page, "登录按钮")

    assert resolved.strategy == "ai_visual"
    assert page.screenshot_calls == [False]


def test_resolve_with_fallback_logs_ai_capture_failures(monkeypatch, caplog) -> None:
    page = FakePage(
        url="https://app.example.com/login",
        screenshot_error=RuntimeError("screenshot boom"),
    )
    monkeypatch.setattr(
        "app.locators.fallback.resolve_semantic_locator",
        MagicMock(side_effect=LocatorResolutionError("skip", trace=LocatorTrace(target="登录按钮"))),
    )

    with pytest.raises(InterventionNeededError):
        resolve_with_fallback(page, "登录按钮")

    assert "AI visual fallback failed" in caplog.text
