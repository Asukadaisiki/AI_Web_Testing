"""Unit tests for test_planning_agent module."""

from __future__ import annotations

import pytest

from app.ai.test_planning_agent import _build_draft_prompt
from app.schemas.ai_planning import AIPlanningRequirements


def test_draft_prompt_includes_dom_aware_hint() -> None:
    """_build_draft_prompt should include DOM-aware targeting hint."""
    requirements = AIPlanningRequirements(
        app_under_test="Login Page",
        business_goal="Test login",
        entry_url_or_page="https://example.com/login",
    )
    prompt = _build_draft_prompt(requirements, scenario_title="登录成功", negative_case=False)
    assert "label" in prompt
    assert "placeholder" in prompt or "实际" in prompt
