"""Unit tests for page_explorer.py storage state management."""
from __future__ import annotations

import json
from pathlib import Path

import pytest

from app.ai.page_explorer import (
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

    def test_skips_invisible_elements(self) -> None:
        elements = [
            {
                "tag": "input",
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
