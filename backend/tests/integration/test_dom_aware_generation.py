"""Integration tests for DOM-aware DSL generation flow.

These tests require Playwright to be installed.
Run with: uv run pytest tests/integration/test_dom_aware_generation.py -v -m browser_integration
"""
from __future__ import annotations

import json
from pathlib import Path

import pytest

from app.ai.page_explorer import (
    capture_browser_session,
    collect_interactable_elements,
    format_elements_for_prompt,
    save_storage_state,
)


@pytest.fixture
def the_internet_url() -> str:
    """URL for the-internet.herokuapp.com login page."""
    return "https://the-internet.herokuapp.com/login"


class TestCollectInteractableElements:
    """Integration tests using real Playwright against the-internet."""

    @pytest.mark.browser_integration
    def test_collects_login_page_elements(self, the_internet_url: str) -> None:
        """Should discover username, password inputs and login button."""
        elements = collect_interactable_elements(the_internet_url)
        assert len(elements) >= 3
        placeholders = [e["placeholder"] for e in elements if e.get("placeholder")]
        assert "Username" in placeholders
        assert "Password" in placeholders
        texts = [e["text"] for e in elements if e.get("text")]
        assert any("Login" in t for t in texts if t)

    @pytest.mark.browser_integration
    def test_formatted_output_is_readable(self, the_internet_url: str) -> None:
        """Formatted output should contain usable element descriptions."""
        elements = collect_interactable_elements(the_internet_url)
        formatted = format_elements_for_prompt(elements)
        assert "Username" in formatted
        assert "Password" in formatted
        assert "Login" in formatted

    @pytest.mark.browser_integration
    def test_unreachable_url_returns_empty(self) -> None:
        """Should return empty list for unreachable URL."""
        elements = collect_interactable_elements(
            "https://this-domain-does-not-exist-12345.example.com"
        )
        assert elements == []


class TestCaptureBrowserSession:
    """Integration tests for session capture and reuse."""

    @pytest.mark.browser_integration
    def test_capture_and_reuse_session(
        self, the_internet_url: str, tmp_path: Path
    ) -> None:
        """Should capture login session and reuse it for subsequent explore."""
        result = capture_browser_session(
            url=the_internet_url,
            steps=[
                {"action": "input", "target": "username", "value": "tomsmith"},
                {"action": "input", "target": "password", "value": "SuperSecretPassword!"},
                {"action": "click", "target": "Login"},
            ],
            storage_dir=tmp_path,
            project_id=1,
        )
        assert result["success"] is True
        assert (tmp_path / "1.json").exists()

        state_path = str(tmp_path / "1.json")
        elements = collect_interactable_elements(
            "https://the-internet.herokuapp.com/secure",
            storage_state_path=state_path,
        )
        assert len(elements) >= 1
