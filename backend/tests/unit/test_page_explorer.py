"""Unit tests for page_explorer.py storage state management."""
from __future__ import annotations

import json
from pathlib import Path
from typing import Any

import pytest

from app.ai.page_explorer import (
    capture_browser_session,
    collect_interactable_elements,
    format_elements_for_prompt,
    get_storage_state_path,
    load_storage_state_meta,
    save_storage_state,
)


class TestGetStorageStatePath:
    def test_returns_path_with_project_id(self, tmp_path: Path) -> None:
        state_path, meta_path = get_storage_state_path(tmp_path, project_id=1)
        assert state_path == tmp_path / "1.json"
        assert meta_path == tmp_path / "1.meta.json"


class TestSaveStorageState:
    def test_writes_state_and_meta_files(self, tmp_path: Path) -> None:
        state = {"cookies": [{"name": "session", "value": "abc"}], "origins": []}
        save_storage_state(
            tmp_path,
            project_id=1,
            state=state,
            source_url="https://example.com/login",
        )
        assert (tmp_path / "1.json").exists()
        assert (tmp_path / "1.meta.json").exists()
        saved_state = json.loads((tmp_path / "1.json").read_text(encoding="utf-8"))
        assert saved_state == state
        meta = json.loads((tmp_path / "1.meta.json").read_text(encoding="utf-8"))
        assert meta["source_url"] == "https://example.com/login"
        assert "saved_at" in meta

    def test_overwrites_existing_state(self, tmp_path: Path) -> None:
        state_v1 = {"cookies": [{"name": "session", "value": "old"}], "origins": []}
        state_v2 = {"cookies": [{"name": "session", "value": "new"}], "origins": []}
        save_storage_state(tmp_path, project_id=1, state=state_v1, source_url="https://example.com")
        save_storage_state(tmp_path, project_id=1, state=state_v2, source_url="https://example.com")
        saved = json.loads((tmp_path / "1.json").read_text(encoding="utf-8"))
        assert saved["cookies"][0]["value"] == "new"


class TestLoadStorageStateMeta:
    def test_returns_none_when_no_meta_exists(self, tmp_path: Path) -> None:
        result = load_storage_state_meta(tmp_path, project_id=1)
        assert result is None

    def test_returns_meta_when_exists(self, tmp_path: Path) -> None:
        state = {"cookies": [], "origins": []}
        save_storage_state(tmp_path, project_id=1, state=state, source_url="https://example.com")
        meta = load_storage_state_meta(tmp_path, project_id=1)
        assert meta is not None
        assert meta["source_url"] == "https://example.com"
        assert "saved_at" in meta

    def test_returns_none_for_corrupt_meta(self, tmp_path: Path) -> None:
        meta_file = tmp_path / "1.meta.json"
        meta_file.write_text("not valid json{{{", encoding="utf-8")
        result = load_storage_state_meta(tmp_path, project_id=1)
        assert result is None


class TestFormatElementsForPrompt:
    def test_formats_input_with_placeholder(self) -> None:
        elements = [
            {
                "tag": "input",
                "id": "username",
                "aria_label": None,
                "placeholder": "Username",
                "role": None,
                "text": None,
                "visible": True,
                "enabled": True,
            }
        ]
        result = format_elements_for_prompt(elements)
        assert "input#username" in result
        assert "[placeholder='Username']" in result

    def test_formats_button_with_text(self) -> None:
        elements = [
            {
                "tag": "button",
                "id": "",
                "aria_label": None,
                "placeholder": None,
                "role": None,
                "text": "Login",
                "visible": True,
                "enabled": True,
            }
        ]
        result = format_elements_for_prompt(elements)
        assert "button" in result
        assert "[text='Login']" in result

    def test_skips_invisible_non_interactive_elements(self) -> None:
        elements = [
            {
                "tag": "div",
                "id": "hidden",
                "aria_label": None,
                "placeholder": None,
                "role": None,
                "text": None,
                "visible": False,
                "enabled": True,
            }
        ]
        result = format_elements_for_prompt(elements)
        assert result.strip() == ""

    def test_keeps_invisible_interactive_elements(self) -> None:
        elements = [
            {
                "tag": "a",
                "id": "add-to-cart",
                "aria_label": None,
                "placeholder": None,
                "role": None,
                "text": "Add to cart",
                "css_selector": "a.add-to-cart",
                "xpath": "//a[@class='add-to-cart']",
                "visible": False,
                "enabled": True,
            }
        ]
        result = format_elements_for_prompt(elements)
        assert "HIDDEN" in result
        assert "Add to cart" in result

    def test_formats_multiple_elements(self) -> None:
        elements = [
            {
                "tag": "input",
                "id": "user",
                "aria_label": None,
                "placeholder": "User",
                "role": None,
                "text": None,
                "visible": True,
                "enabled": True,
            },
            {
                "tag": "button",
                "id": "",
                "aria_label": None,
                "placeholder": None,
                "role": None,
                "text": "Submit",
                "visible": True,
                "enabled": True,
            },
        ]
        result = format_elements_for_prompt(elements)
        lines = [line for line in result.strip().split("\n") if line.strip()]
        assert len(lines) == 2

    def test_includes_aria_label(self) -> None:
        elements = [
            {
                "tag": "div",
                "id": "",
                "aria_label": "Close dialog",
                "placeholder": None,
                "role": "button",
                "text": None,
                "visible": True,
                "enabled": True,
            }
        ]
        result = format_elements_for_prompt(elements)
        assert "[aria-label='Close dialog']" in result
        assert "[role='button']" in result

    def test_empty_list_returns_empty_string(self) -> None:
        result = format_elements_for_prompt([])
        assert result == ""


class TestIsStorageStateStale:
    def test_stale_when_old(self) -> None:
        from datetime import UTC, datetime, timedelta

        meta = {
            "source_url": "https://example.com",
            "saved_at": (datetime.now(UTC) - timedelta(hours=25)).isoformat(),
        }
        from app.ai.page_explorer import is_storage_state_stale

        assert is_storage_state_stale(meta) is True

    def test_not_stale_when_recent(self) -> None:
        from datetime import UTC, datetime

        meta = {
            "source_url": "https://example.com",
            "saved_at": datetime.now(UTC).isoformat(),
        }
        from app.ai.page_explorer import is_storage_state_stale

        assert is_storage_state_stale(meta) is False


class TestCollectInteractableElements:
    def test_collects_elements_from_page(self) -> None:
        fake_elements: list[dict[str, Any]] = [
            {
                "tag": "input", "id": "username", "text": None, "role": None,
                "aria_label": None, "placeholder": "Username", "data_testid": None,
                "css_selector": "#username", "xpath": "/html/body/input[1]",
                "rect": {"x": 10, "y": 20, "width": 200, "height": 30},
                "visible": True, "enabled": True,
            }
        ]

        class FakePage:
            url = "https://example.com/login"

            def goto(self, url, **kwargs): ...

            def wait_for_load_state(self, state): ...

            def evaluate(self, script, *args):
                return fake_elements

        class FakeContext:
            def new_page(self): return FakePage()  # type: ignore[return-value]
            def close(self): ...

        class FakeBrowser:
            def new_context(self, **kwargs): return FakeContext()  # type: ignore[return-value]
            def close(self): ...

        class FakePlaywright:
            class chromium:
                @staticmethod
                def launch(**kwargs): return FakeBrowser()  # type: ignore[return-value]

            def __enter__(self): return self
            def __exit__(self, *args): pass

        import app.ai.page_explorer as mod
        original = getattr(mod, "_sync_playwright_context", None)
        mod._sync_playwright_context = lambda: FakePlaywright()  # type: ignore[assignment]
        try:
            result = collect_interactable_elements("https://example.com/login", storage_state_path=None)
        finally:
            if original is not None:
                mod._sync_playwright_context = original
            else:
                delattr(mod, "_sync_playwright_context")

        assert len(result) == 1
        assert result[0]["id"] == "username"
        assert result[0]["placeholder"] == "Username"

    def test_returns_empty_list_on_error(self) -> None:
        import app.ai.page_explorer as mod

        def fake_error():
            raise RuntimeError("Playwright not installed")

        original = getattr(mod, "_sync_playwright_context", None)
        mod._sync_playwright_context = fake_error  # type: ignore[assignment]
        try:
            result = collect_interactable_elements("https://example.com", storage_state_path=None)
        finally:
            if original is not None:
                mod._sync_playwright_context = original
            else:
                delattr(mod, "_sync_playwright_context")

        assert result == []


class TestCaptureBrowserSession:
    def test_executes_steps_and_saves_state(self, tmp_path: Path) -> None:
        captured_state: dict[str, Any] = {
            "cookies": [{"name": "sid", "value": "xyz"}],
            "origins": [],
        }

        class FakeLocator:
            def fill(self, value): pass
            def click(self): pass

        class FakePage:
            url = "https://example.com/login"

            def goto(self, url, **kwargs): pass

            def wait_for_load_state(self, state, **kwargs): pass

            def get_by_label(self, target, **kwargs): return FakeLocator()  # type: ignore[return-value]
            def get_by_placeholder(self, target, **kwargs): return FakeLocator()  # type: ignore[return-value]
            def get_by_role(self, role, **kwargs): return FakeLocator()  # type: ignore[return-value]
            def locator(self, selector): return FakeLocator()  # type: ignore[return-value]

        class FakeContext:
            def new_page(self): return FakePage()  # type: ignore[return-value]
            def storage_state(self): return captured_state
            def close(self): ...

        class FakeBrowser:
            def new_context(self, **kwargs): return FakeContext()  # type: ignore[return-value]
            def close(self): ...

        class FakePlaywright:
            class chromium:
                @staticmethod
                def launch(**kwargs): return FakeBrowser()  # type: ignore[return-value]

            def __enter__(self): return self
            def __exit__(self, *args): pass

        import app.ai.page_explorer as mod
        original = getattr(mod, "_sync_playwright_context", None)
        mod._sync_playwright_context = lambda: FakePlaywright()  # type: ignore[assignment]
        try:
            result = capture_browser_session(
                url="https://example.com/login",
                steps=[
                    {"action": "input", "target": "username", "value": "admin"},
                    {"action": "click", "target": "Login"},
                ],
                storage_dir=tmp_path,
                project_id=1,
            )
        finally:
            if original is not None:
                mod._sync_playwright_context = original
            else:
                delattr(mod, "_sync_playwright_context")

        assert result["success"] is True
        assert (tmp_path / "1.json").exists()

    def test_returns_error_on_failure(self, tmp_path: Path) -> None:
        import app.ai.page_explorer as mod

        def fake_error():
            raise RuntimeError("Browser crashed")

        original = getattr(mod, "_sync_playwright_context", None)
        mod._sync_playwright_context = fake_error  # type: ignore[assignment]
        try:
            result = capture_browser_session(
                url="https://example.com",
                steps=[],
                storage_dir=tmp_path,
                project_id=1,
            )
        finally:
            if original is not None:
                mod._sync_playwright_context = original
            else:
                delattr(mod, "_sync_playwright_context")

        assert result["success"] is False
        assert "error" in result
