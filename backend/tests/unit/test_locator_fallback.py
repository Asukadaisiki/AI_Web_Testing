"""Tests for fallback locator chain."""

from __future__ import annotations

import pytest

from app.locators import InterventionNeededError, resolve_with_fallback
from app.models import LocatorCorrection, TestCase, TestCaseRun


class FakeLocatorCollection:
    def __init__(self, *, should_fail: bool = False) -> None:
        self.should_fail = should_fail

    def wait_for(self, *, state: str, timeout: int) -> None:
        if self.should_fail:
            raise RuntimeError("correction failed")

    def count(self) -> int:
        return 0


class FakePage:
    def __init__(self, *, url: str, correction_should_fail: bool = False) -> None:
        self.url = url
        self.viewport_size = {"width": 1280, "height": 720}
        self.correction_should_fail = correction_should_fail

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
        if "querySelectorAll" in script:
            return []
        return None

    def screenshot(self, *, full_page: bool = True):
        return b"fake"


def _create_source_execution(db_session) -> int:
    case = TestCase(
        project_id=1,
        created_by=1,
        updated_by=1,
        name="回退定位用例",
        description=None,
        dsl={"name": "回退定位用例", "steps": [{"action": "click", "target": "登录按钮"}]},
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


def test_resolve_with_fallback_uses_active_correction(db_session) -> None:
    execution_id = _create_source_execution(db_session)
    correction = LocatorCorrection(
        page_url_pattern="https://app.example.com/users/*",
        target_description="登录按钮",
        correction_type="css",
        correction_value="#login-btn",
        source_execution_id=execution_id,
        created_by=1,
    )
    db_session.add(correction)
    db_session.commit()
    db_session.refresh(correction)

    page = FakePage(url="https://app.example.com/users/123")
    resolved = resolve_with_fallback(page, "登录按钮", db_session=db_session, require_enabled=True)

    assert resolved.strategy == "correction:css"
    assert correction.verified_count == 1
    assert correction.consecutive_failures == 0
    assert correction.is_active is True


def test_resolve_with_fallback_disables_correction_after_three_failures(db_session) -> None:
    execution_id = _create_source_execution(db_session)
    correction = LocatorCorrection(
        page_url_pattern="https://app.example.com/users/*",
        target_description="登录按钮",
        correction_type="css",
        correction_value="#login-btn",
        source_execution_id=execution_id,
        created_by=1,
    )
    db_session.add(correction)
    db_session.commit()
    db_session.refresh(correction)

    page = FakePage(url="https://app.example.com/users/123", correction_should_fail=True)
    for _ in range(3):
        with pytest.raises(InterventionNeededError):
            resolve_with_fallback(page, "登录按钮", db_session=db_session, require_enabled=True)

    assert correction.consecutive_failures == 3
    assert correction.is_active is False
