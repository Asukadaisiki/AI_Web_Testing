# DOM-Aware DSL Generation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make AI-generated DSL targets match actual DOM elements by feeding page element inventories into the generation prompt, and enable VLM visual locator as a fallback by default.

**Architecture:** Two new planning agent tools (`explore_page`, `capture_page_session`) backed by a shared `page_explorer` module that uses Playwright to collect DOM elements and persist browser sessions. VLM default changed to enabled. Session state stored as per-project JSON files.

**Tech Stack:** Playwright sync API, existing DOM extraction script from `fallback.py`, Pydantic, pytest with monkeypatch for mocking.

---

## File Structure

| File | Responsibility |
|---|---|
| `backend/app/ai/page_explorer.py` | **NEW** — Playwright DOM collection, session capture, storage state file I/O, element formatting |
| `backend/app/ai/planning_tools.py` | **MODIFY** — register `explore_page` and `capture_page_session` tools with handlers |
| `backend/app/ai/test_planning_prompts.py` | **NO CHANGE** — tool descriptions injected automatically via `get_tool_descriptions_for_prompt()` |
| `backend/app/ai/test_planning_agent.py` | **MODIFY** — add DOM-aware hint to `_build_draft_prompt` |
| `backend/app/core/config.py` | **MODIFY** — add `storage_state_dir`, change `enable_ai_visual_locate` default to `True` |
| `backend/app/main.py` | **MODIFY** — ensure `storage_states/` directory created on startup |
| `backend/tests/unit/test_page_explorer.py` | **NEW** — unit tests for page_explorer |
| `backend/tests/unit/test_planning_tools.py` | **MODIFY** — add tests for new tools |
| `backend/tests/integration/test_dom_aware_generation.py` | **NEW** — integration test for full flow |

---

### Task 1: Config Changes

**Files:**
- Modify: `backend/app/core/config.py:47-126`
- Test: `backend/tests/unit/test_config.py`

- [ ] **Step 1: Write the failing test**

Add to `backend/tests/unit/test_config.py`:

```python
def test_storage_state_dir_defaults_to_storage_states(monkeypatch, reset_cached_state) -> None:
    """Should default to 'storage_states' directory."""
    monkeypatch.setenv("AUTH_SESSION_SECRET", "test-secret")
    from app.core.config import get_settings
    settings = get_settings()
    assert settings.storage_state_dir == "storage_states"


def test_enable_ai_visual_locate_defaults_to_true(monkeypatch, reset_cached_state) -> None:
    """VLM visual locate should be enabled by default."""
    monkeypatch.setenv("AUTH_SESSION_SECRET", "test-secret")
    from app.core.config import get_settings
    settings = get_settings()
    assert settings.enable_ai_visual_locate is True


def test_enable_ai_visual_locate_can_be_disabled(monkeypatch, reset_cached_state) -> None:
    """Should respect ENABLE_AI_VISUAL_LOCATE=false."""
    monkeypatch.setenv("AUTH_SESSION_SECRET", "test-secret")
    monkeypatch.setenv("ENABLE_AI_VISUAL_LOCATE", "false")
    from app.core.config import get_settings
    settings = get_settings()
    assert settings.enable_ai_visual_locate is False
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && uv run pytest tests/unit/test_config.py -k "storage_state_dir or enable_ai_visual_locate" -v`
Expected: FAIL — `storage_state_dir` attribute and new default don't exist yet.

- [ ] **Step 3: Write minimal implementation**

In `backend/app/core/config.py`, add field to `Settings` dataclass after line 83:

```python
    storage_state_dir: str = "storage_states"
```

Change line 69:

```python
    enable_ai_visual_locate: bool = True
```

In `get_settings()`, change line 111:

```python
        enable_ai_visual_locate=_get_bool(os.getenv("ENABLE_AI_VISUAL_LOCATE"), default=True),
```

Add after line 125:

```python
        storage_state_dir=os.getenv("STORAGE_STATE_DIR", "storage_states").strip(),
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && uv run pytest tests/unit/test_config.py -k "storage_state_dir or enable_ai_visual_locate" -v`
Expected: PASS

- [ ] **Step 5: Run full config tests to verify no regressions**

Run: `cd backend && uv run pytest tests/unit/test_config.py -v`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add backend/app/core/config.py backend/tests/unit/test_config.py
git commit -m "feat: add storage_state_dir config, enable VLM visual locate by default"
```

---

### Task 2: Storage State File Management

**Files:**
- Create: `backend/app/ai/page_explorer.py`
- Test: `backend/tests/unit/test_page_explorer.py`

- [ ] **Step 1: Write the failing test**

Create `backend/tests/unit/test_page_explorer.py`:

```python
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
    """Tests for get_storage_state_path."""

    def test_returns_path_with_project_id(self, tmp_path: Path) -> None:
        state_path, meta_path = get_storage_state_path(tmp_path, project_id=1)
        assert state_path == tmp_path / "1.json"
        assert meta_path == tmp_path / "1.meta.json"


class TestSaveStorageState:
    """Tests for save_storage_state."""

    def test_writes_state_and_meta_files(self, tmp_path: Path) -> None:
        state = {"cookies": [{"name": "session", "value": "abc"}], "origins": []}
        save_storage_state(tmp_path, project_id=1, state=state, source_url="https://example.com/login")

        state_file = tmp_path / "1.json"
        meta_file = tmp_path / "1.meta.json"
        assert state_file.exists()
        assert meta_file.exists()

        saved_state = json.loads(state_file.read_text(encoding="utf-8"))
        assert saved_state == state

        meta = json.loads(meta_file.read_text(encoding="utf-8"))
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
    """Tests for load_storage_state_meta."""

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
    """Tests for format_elements_for_prompt."""

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

    def test_formats_multiple_elements_with_newlines(self) -> None:
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

    def test_staleness_warning_when_meta_is_old(self) -> None:
        from datetime import datetime, timedelta

        meta = {"source_url": "https://example.com", "saved_at": (datetime.utcnow() - timedelta(hours=25)).isoformat()}
        from app.ai.page_explorer import is_storage_state_stale
        assert is_storage_state_stale(meta) is True

    def test_not_stale_when_recent(self) -> None:
        from datetime import datetime

        meta = {"source_url": "https://example.com", "saved_at": datetime.utcnow().isoformat()}
        from app.ai.page_explorer import is_storage_state_stale
        assert is_storage_state_stale(meta) is False
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && uv run pytest tests/unit/test_page_explorer.py -v`
Expected: FAIL — module `app.ai.page_explorer` does not exist.

- [ ] **Step 3: Write minimal implementation**

Create `backend/app/ai/page_explorer.py`:

```python
"""Playwright-based page exploration and browser session management."""
from __future__ import annotations

import json
import logging
from datetime import datetime, timedelta
from pathlib import Path
from typing import Any

logger = logging.getLogger(__name__)

STALE_THRESHOLD_HOURS = 24


def get_storage_state_path(base_dir: Path, *, project_id: int) -> tuple[Path, Path]:
    """Return (state_path, meta_path) for a given project."""
    return base_dir / f"{project_id}.json", base_dir / f"{project_id}.meta.json"


def save_storage_state(
    base_dir: Path,
    *,
    project_id: int,
    state: dict[str, Any],
    source_url: str,
) -> None:
    """Persist Playwright storage_state and metadata for a project."""
    base_dir.mkdir(parents=True, exist_ok=True)
    state_path, meta_path = get_storage_state_path(base_dir, project_id=project_id)

    state_path.write_text(json.dumps(state, ensure_ascii=False), encoding="utf-8")
    meta = {"source_url": source_url, "saved_at": datetime.utcnow().isoformat()}
    meta_path.write_text(json.dumps(meta, ensure_ascii=False), encoding="utf-8")
    logger.info("Saved storage state for project_id=%s source_url=%s", project_id, source_url)


def load_storage_state_meta(base_dir: Path, *, project_id: int) -> dict[str, Any] | None:
    """Load storage state metadata, returning None if missing or corrupt."""
    _, meta_path = get_storage_state_path(base_dir, project_id=project_id)
    if not meta_path.exists():
        return None
    try:
        return json.loads(meta_path.read_text(encoding="utf-8"))
    except (json.JSONDecodeError, OSError):
        return None


def is_storage_state_stale(meta: dict[str, Any]) -> bool:
    """Check if storage state metadata indicates it's older than STALE_THRESHOLD_HOURS."""
    saved_at_str = meta.get("saved_at")
    if not saved_at_str:
        return True
    try:
        saved_at = datetime.fromisoformat(saved_at_str)
        return (datetime.utcnow() - saved_at) > timedelta(hours=STALE_THRESHOLD_HOURS)
    except (ValueError, TypeError):
        return True


def format_elements_for_prompt(elements: list[dict[str, Any]]) -> str:
    """Format collected DOM elements into a concise text block for AI prompt injection."""
    lines: list[str] = []
    for element in elements:
        if not element.get("visible", True):
            continue

        tag = element.get("tag", "unknown")
        elem_id = element.get("id") or ""

        parts: list[str] = [f"{tag}"]
        if elem_id:
            parts[0] = f"{tag}#{elem_id}"

        for attr in ("aria_label", "placeholder", "text", "role"):
            value = element.get(attr)
            if value:
                attr_display = attr.replace("_", "-")
                parts.append(f"[{attr_display}='{value}']")

        lines.append(" ".join(parts))

    return "\n".join(lines)


__all__ = [
    "format_elements_for_prompt",
    "get_storage_state_path",
    "is_storage_state_stale",
    "load_storage_state_meta",
    "save_storage_state",
]
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && uv run pytest tests/unit/test_page_explorer.py -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add backend/app/ai/page_explorer.py backend/tests/unit/test_page_explorer.py
git commit -m "feat: add page_explorer module with storage state management and element formatting"
```

---

### Task 3: Playwright DOM Collection Function

**Files:**
- Modify: `backend/app/ai/page_explorer.py`
- Test: `backend/tests/unit/test_page_explorer.py`

- [ ] **Step 1: Write the failing test**

Add to `backend/tests/unit/test_page_explorer.py`:

```python
class TestCollectInteractableElements:
    """Tests for collect_interactable_elements (mocked Playwright)."""

    def test_collects_elements_from_page(self, tmp_path: Path) -> None:
        from app.ai.page_explorer import collect_interactable_elements

        fake_elements = [
            {
                "tag": "input",
                "id": "username",
                "text": None,
                "role": None,
                "aria_label": None,
                "placeholder": "Username",
                "data_testid": None,
                "css_selector": "#username",
                "xpath": "/html/body/input[1]",
                "rect": {"x": 10, "y": 20, "width": 200, "height": 30},
                "visible": True,
                "enabled": True,
            }
        ]

        class FakePage:
            url = "https://example.com/login"
            def goto(self, url, **kwargs): ...
            def wait_for_load_state(self, state): ...
            def evaluate(self, script):
                return fake_elements

        class FakeContext:
            def new_page(self):
                return FakePage()
            def close(self): ...

        class FakeBrowser:
            def new_context(self, **kwargs):
                return FakeContext()
            def close(self): ...

        class FakePlaywright:
            class chromium:
                @staticmethod
                def launch(**kwargs):
                    return FakeBrowser()

        import app.ai.page_explorer as mod
        original = getattr(mod, "_sync_playwright_context", None)
        mod._sync_playwright_context = lambda: FakePlaywright()

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
        from app.ai.page_explorer import collect_interactable_elements

        import app.ai.page_explorer as mod

        def fake_context_error():
            raise RuntimeError("Playwright not installed")

        original = getattr(mod, "_sync_playwright_context", None)
        mod._sync_playwright_context = fake_context_error
        try:
            result = collect_interactable_elements("https://example.com", storage_state_path=None)
        finally:
            if original is not None:
                mod._sync_playwright_context = original
            else:
                delattr(mod, "_sync_playwright_context")

        assert result == []
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && uv run pytest tests/unit/test_page_explorer.py::TestCollectInteractableElements -v`
Expected: FAIL — `collect_interactable_elements` not defined.

- [ ] **Step 3: Write minimal implementation**

Add to `backend/app/ai/page_explorer.py`:

```python
from playwright.sync_api import sync_playwright

# Import the existing DOM extraction script.
from app.locators.fallback import EXTRACT_INTERACTABLE_ELEMENTS_SCRIPT


def _sync_playwright_context():
    """Indirection point for testing — returns the sync_playwright context manager."""
    return sync_playwright()


def collect_interactable_elements(
    url: str,
    *,
    storage_state_path: str | None = None,
    timeout_ms: int = 10000,
) -> list[dict[str, Any]]:
    """Open *url* in a temporary Playwright context and return interactable elements."""
    pw = _sync_playwright_context()
    try:
        with pw as playwright:
            browser = playwright.chromium.launch(headless=True)
            context_kwargs: dict[str, Any] = {}
            if storage_state_path and Path(storage_state_path).exists():
                context_kwargs["storage_state"] = storage_state_path
            context = browser.new_context(**context_kwargs)
            page = context.new_page()
            try:
                page.goto(url, timeout=timeout_ms, wait_until="domcontentloaded")
                page.wait_for_load_state("networkidle", timeout=timeout_ms)
            except Exception as exc:
                logger.warning("Page load issue for %s: %s", url, exc)
            payload = page.evaluate(EXTRACT_INTERACTABLE_ELEMENTS_SCRIPT)
            context.close()
            browser.close()
    except Exception as exc:
        logger.warning("collect_interactable_elements failed for url=%s: %s", url, exc)
        return []

    if not isinstance(payload, list):
        return []

    return [
        {
            "tag": elem.get("tag", "unknown"),
            "id": elem.get("id") or elem.get("data_testid") or "",
            "text": elem.get("text"),
            "role": elem.get("role"),
            "aria_label": elem.get("aria_label"),
            "placeholder": elem.get("placeholder"),
            "visible": elem.get("visible", False),
            "enabled": elem.get("enabled", False),
        }
        for elem in payload
        if isinstance(elem, dict)
    ]
```

Also add `collect_interactable_elements` to `__all__`.

Note: the JS script returns `aria_label` not `id` as a top-level key. The elements don't have `id` as a separate extracted field — they have `css_selector` and `data_testid`. We should extract the id from the CSS selector or add it to the JS. Since `buildCssSelector` returns `#id` if present, we can parse it, but it's simpler to adjust the returned dict to extract id. Let's use `data_testid` as a fallback id. For the actual `id` attribute, the existing JS doesn't extract it directly. We need to add `element.id` to the script output... but we shouldn't modify `EXTRACT_INTERACTABLE_ELEMENTS_SCRIPT`. Instead, we can run a second small script or parse `css_selector`.

Actually, looking again at the JS: it builds `css_selector` using `node.id` first. So if the element has an ID, `css_selector` will be `#username`. We can extract it:

```python
def _extract_id_from_element(elem: dict) -> str:
    css = elem.get("css_selector", "")
    if css.startswith("#") and " > " not in css:
        return css[1:]
    return elem.get("data_testid") or ""
```

Update the element building code to use this helper.

Add helper function before `collect_interactable_elements`:

```python
def _extract_element_id(elem: dict[str, Any]) -> str:
    """Extract element id from css_selector (#id) or fall back to data_testid."""
    css = elem.get("css_selector", "")
    if css.startswith("#") and " > " not in css:
        return css[1:]
    return elem.get("data_testid") or ""
```

Then in the list comprehension use `"id": _extract_element_id(elem)` instead.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && uv run pytest tests/unit/test_page_explorer.py -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add backend/app/ai/page_explorer.py backend/tests/unit/test_page_explorer.py
git commit -m "feat: add Playwright DOM collection to page_explorer"
```

---

### Task 4: Session Capture Function

**Files:**
- Modify: `backend/app/ai/page_explorer.py`
- Test: `backend/tests/unit/test_page_explorer.py`

- [ ] **Step 1: Write the failing test**

Add to `backend/tests/unit/test_page_explorer.py`:

```python
class TestCaptureBrowserSession:
    """Tests for capture_browser_session (mocked Playwright)."""

    def test_executes_steps_and_saves_state(self, tmp_path: Path) -> None:
        from app.ai.page_explorer import capture_browser_session

        captured_state = {"cookies": [{"name": "sid", "value": "xyz"}], "origins": []}
        goto_calls = {"count": 0}
        step_actions = {"items": []}

        class FakeLocator:
            def fill(self, value):
                step_actions["items"].append(("fill", value))
            def click(self):
                step_actions["items"].append(("click",))

        class FakePage:
            url = "https://example.com/login"
            def goto(self, url, **kwargs):
                goto_calls["count"] += 1
            def wait_for_load_state(self, state): ...
            def get_by_label(self, target, **kwargs):
                return FakeLocator()
            def get_by_placeholder(self, target, **kwargs):
                return FakeLocator()
            def locator(self, selector):
                return FakeLocator()

        class FakeContext:
            def new_page(self):
                return FakePage()
            def storage_state(self):
                return captured_state
            def close(self): ...

        class FakeBrowser:
            def new_context(self, **kwargs):
                return FakeContext()
            def close(self): ...

        class FakePlaywright:
            class chromium:
                @staticmethod
                def launch(**kwargs):
                    return FakeBrowser()

        import app.ai.page_explorer as mod
        original = getattr(mod, "_sync_playwright_context", None)
        mod._sync_playwright_context = lambda: FakePlaywright()

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
        assert "cookie" in result.get("message", "").lower() or "会话" in result.get("message", "")
        # Verify state was saved
        state_file = tmp_path / "1.json"
        assert state_file.exists()

    def test_returns_error_on_failure(self, tmp_path: Path) -> None:
        from app.ai.page_explorer import capture_browser_session

        import app.ai.page_explorer as mod

        def fake_error():
            raise RuntimeError("Browser crashed")

        original = getattr(mod, "_sync_playwright_context", None)
        mod._sync_playwright_context = fake_error
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && uv run pytest tests/unit/test_page_explorer.py::TestCaptureBrowserSession -v`
Expected: FAIL — `capture_browser_session` not defined.

- [ ] **Step 3: Write minimal implementation**

Add to `backend/app/ai/page_explorer.py`:

```python
def capture_browser_session(
    url: str,
    steps: list[dict[str, Any]],
    *,
    storage_dir: Path,
    project_id: int,
    timeout_ms: int = 10000,
) -> dict[str, Any]:
    """Execute *steps* on *url*, then persist the browser session state."""
    pw = _sync_playwright_context()
    try:
        with pw as playwright:
            browser = playwright.chromium.launch(headless=True)
            context = browser.new_context()
            page = context.new_page()
            page.goto(url, timeout=timeout_ms, wait_until="domcontentloaded")
            page.wait_for_load_state("networkidle", timeout=timeout_ms)

            for step in steps:
                action = step.get("action", "")
                target = step.get("target", "")
                value = step.get("value", "")
                if action == "input" and target:
                    locator = (
                        page.get_by_label(target)
                        or page.get_by_placeholder(target)
                        or page.locator(f"#{target}")
                    )
                    locator.fill(value)
                elif action == "click" and target:
                    locator = (
                        page.get_by_label(target)
                        or page.get_by_role("button", name=target)
                        or page.locator(f"#{target}")
                    )
                    locator.click()

            state = context.storage_state()
            cookie_count = len(state.get("cookies", []))
            save_storage_state(storage_dir, project_id=project_id, state=state, source_url=url)
            context.close()
            browser.close()
    except Exception as exc:
        logger.warning("capture_browser_session failed for url=%s: %s", url, exc)
        return {"success": False, "error": str(exc)}

    return {
        "success": True,
        "message": f"已保存 {url} 的会话状态（包含 {cookie_count} 个 cookie）",
    }
```

Add `capture_browser_session` to `__all__`.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && uv run pytest tests/unit/test_page_explorer.py -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add backend/app/ai/page_explorer.py backend/tests/unit/test_page_explorer.py
git commit -m "feat: add browser session capture to page_explorer"
```

---

### Task 5: Register Planning Tools

**Files:**
- Modify: `backend/app/ai/planning_tools.py:182-255`
- Modify: `backend/tests/unit/test_planning_tools.py`

- [ ] **Step 1: Write the failing test**

Add to `backend/tests/unit/test_planning_tools.py`:

```python
from unittest.mock import patch


class TestExplorePageTool:
    """Tests for explore_page tool handler."""

    def test_explore_page_returns_elements(self, db_session: Session) -> None:
        """Should return formatted DOM elements."""
        fake_elements = [
            {
                "tag": "input",
                "id": "username",
                "text": None,
                "role": None,
                "aria_label": None,
                "placeholder": "Username",
                "visible": True,
                "enabled": True,
            }
        ]
        with patch("app.ai.planning_tools.collect_interactable_elements", return_value=fake_elements):
            result = _handle_explore_page(
                params={"url": "https://example.com/login"},
                db_session=db_session,
                project_id=1,
            )
        assert "elements" in result
        assert len(result["elements"]) == 1
        assert result["elements"][0]["placeholder"] == "Username"

    def test_explore_page_requires_url(self, db_session: Session) -> None:
        """Should return error when URL is missing."""
        result = _handle_explore_page(params={}, db_session=db_session, project_id=1)
        assert "error" in result
        assert "url" in result["error"].lower()

    def test_explore_page_handles_empty_result(self, db_session: Session) -> None:
        """Should handle page with no interactable elements."""
        with patch("app.ai.planning_tools.collect_interactable_elements", return_value=[]):
            result = _handle_explore_page(
                params={"url": "https://example.com/blank"},
                db_session=db_session,
                project_id=1,
            )
        assert result["elements"] == []
        assert "warning" in result


class TestCapturePageSessionTool:
    """Tests for capture_page_session tool handler."""

    def test_capture_returns_success(self, db_session: Session, tmp_path: Path) -> None:
        """Should return success when session is captured."""
        with patch("app.ai.planning_tools.capture_browser_session") as mock_capture:
            mock_capture.return_value = {"success": True, "message": "已保存会话状态（包含 2 个 cookie）"}
            result = _handle_capture_page_session(
                params={
                    "url": "https://example.com/login",
                    "steps": [
                        {"action": "input", "target": "username", "value": "admin"},
                    ],
                },
                db_session=db_session,
                project_id=1,
            )
        assert result["success"] is True

    def test_capture_requires_url(self, db_session: Session) -> None:
        """Should return error when URL is missing."""
        result = _handle_capture_page_session(params={}, db_session=db_session, project_id=1)
        assert "error" in result
        assert "url" in result["error"].lower()
```

Also update the existing `test_returns_all_registered_tools` to expect 7 tools instead of 5:

```python
    def test_returns_all_registered_tools(self) -> None:
        """Should return all registered tools."""
        tools = list_available_tools()
        assert len(tools) == 7
        tool_names = {t.name for t in tools}
        assert tool_names == {
            "get_project_info",
            "list_test_cases",
            "get_case_detail",
            "list_recent_executions",
            "get_case_stats",
            "explore_page",
            "capture_page_session",
        }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && uv run pytest tests/unit/test_planning_tools.py -v`
Expected: FAIL — `_handle_explore_page` and `_handle_capture_page_session` not imported.

- [ ] **Step 3: Write minimal implementation**

Add to `backend/app/ai/planning_tools.py` imports:

```python
from app.ai.page_explorer import (
    capture_browser_session,
    collect_interactable_elements,
    format_elements_for_prompt,
    is_storage_state_stale,
    load_storage_state_meta,
)
```

Add handler functions before the registry:

```python
def _resolve_storage_state_dir(db_session: Session) -> Path:
    """Resolve storage state directory from app config."""
    from app.core.config import get_settings
    settings = get_settings()
    from pathlib import Path
    return Path(settings.storage_state_dir)


def _handle_explore_page(
    *,
    params: dict[str, Any],
    db_session: Session,
    project_id: int,
) -> dict[str, Any]:
    url = params.get("url")
    if not url or not isinstance(url, str) or not url.strip():
        return {"error": "必须提供 url 参数"}

    storage_dir = _resolve_storage_state_dir(db_session)
    storage_path = str(storage_dir / f"{project_id}.json") if (storage_dir / f"{project_id}.json").exists() else None

    elements = collect_interactable_elements(url.strip(), storage_state_path=storage_path)
    formatted = format_elements_for_prompt(elements)

    result: dict[str, Any] = {
        "url": url.strip(),
        "elements": elements,
        "formatted": formatted,
        "element_count": len(elements),
    }

    if not elements:
        result["warning"] = "页面未发现可交互元素"

    meta = load_storage_state_meta(storage_dir, project_id=project_id)
    if meta and is_storage_state_stale(meta):
        result["warning"] = "会话状态超过24小时未更新，元素可能不完整"

    return result


def _handle_capture_page_session(
    *,
    params: dict[str, Any],
    db_session: Session,
    project_id: int,
) -> dict[str, Any]:
    url = params.get("url")
    if not url or not isinstance(url, str) or not url.strip():
        return {"error": "必须提供 url 参数"}

    steps = params.get("steps")
    if not isinstance(steps, list):
        steps = []

    storage_dir = _resolve_storage_state_dir(db_session)
    return capture_browser_session(
        url=url.strip(),
        steps=steps,
        storage_dir=storage_dir,
        project_id=project_id,
    )
```

Register tools in `_TOOL_REGISTRY`:

```python
    "explore_page": PlanningTool(
        name="explore_page",
        description="访问指定 URL 页面，采集页面上所有可交互元素（按钮、输入框、链接等），返回元素的 id、label、placeholder 等定位属性。如果项目已保存浏览器会话状态，会自动复用登录态。",
        parameters={
            "type": "object",
            "properties": {
                "url": {
                    "type": "string",
                    "description": "要采集的目标页面 URL",
                },
            },
            "required": ["url"],
        },
    ),
    "capture_page_session": PlanningTool(
        name="capture_page_session",
        description="打开指定 URL 并执行登录步骤（如填写用户名密码、点击登录按钮），然后保存浏览器的会话状态（cookie 等），供后续 explore_page 复用登录态。",
        parameters={
            "type": "object",
            "properties": {
                "url": {
                    "type": "string",
                    "description": "登录页面的 URL",
                },
                "steps": {
                    "type": "array",
                    "description": "登录操作的步骤列表，每步包含 action、target 和 value",
                    "items": {
                        "type": "object",
                        "properties": {
                            "action": {"type": "string", "enum": ["input", "click"]},
                            "target": {"type": "string"},
                            "value": {"type": "string"},
                        },
                    },
                },
            },
            "required": ["url"],
        },
    ),
```

Register handlers in `_TOOL_HANDLERS`:

```python
    "explore_page": _handle_explore_page,
    "capture_page_session": _handle_capture_page_session,
```

Update imports in `test_planning_tools.py`:

```python
from app.ai.planning_tools import (
    _handle_get_case_detail,
    _handle_get_case_stats,
    _handle_get_project_info,
    _handle_list_recent_executions,
    _handle_list_test_cases,
    _handle_explore_page,
    _handle_capture_page_session,
    execute_tool,
    list_available_tools,
)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && uv run pytest tests/unit/test_planning_tools.py -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add backend/app/ai/planning_tools.py backend/tests/unit/test_planning_tools.py
git commit -m "feat: register explore_page and capture_page_session planning tools"
```

---

### Task 6: Draft Prompt Safeguard

**Files:**
- Modify: `backend/app/ai/test_planning_agent.py:582-603`

- [ ] **Step 1: Write the failing test**

Add a test that verifies the safeguard line exists in draft prompts. Add to an appropriate test file or create inline:

```python
def test_draft_prompt_includes_dom_aware_hint() -> None:
    """_build_draft_prompt should include DOM-aware targeting hint."""
    from app.ai.test_planning_agent import _build_draft_prompt
    from app.schemas.ai_planning import AIPlanningRequirements

    requirements = AIPlanningRequirements(
        app_under_test="Login Page",
        business_goal="Test login",
        entry_url_or_page="https://example.com/login",
    )
    prompt = _build_draft_prompt(requirements, scenario_title="登录成功", negative_case=False)
    assert "label" in prompt or "placeholder" in prompt or "实际" in prompt
```

- [ ] **Step 2: Run test to verify it fails or passes**

Run: `cd backend && uv run pytest tests/unit/ -k "draft_prompt" -v`
If the current prompt doesn't include the hint, it will fail.

- [ ] **Step 3: Write minimal implementation**

In `backend/app/ai/test_planning_agent.py`, modify `_build_draft_prompt` (around line 593-603), append after the last format line:

```python
    negative_hint = "需要覆盖异常输入和错误提示。" if negative_case else "请覆盖标准主流程。"
    return (
        f"请基于测试规划生成 DSL 草案。场景：{scenario_title}。"
        f"被测系统：{requirements.app_under_test or '待补充'}。"
        f"目标：{requirements.business_goal or '待补充'}。"
        f"入口：{requirements.entry_url_or_page or '待补充'}。"
        f"流程：{requirements.core_user_flow or '待补充'}。"
        f"断言：{assertions}。"
        f"测试数据需求：{data_labels or '待补充'}。"
        f"范围限制：{requirements.scope_limits or '未说明'}。"
        f"{negative_hint}"
        "如果已获取到页面元素清单，请严格按照元素的实际 label、placeholder 或 id 作为 target，不要自行编造描述。"
    )
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && uv run pytest tests/unit/ -k "draft_prompt" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/app/ai/test_planning_agent.py
git commit -m "feat: add DOM-aware targeting hint to draft prompt"
```

---

### Task 7: Startup Directory Creation

**Files:**
- Modify: `backend/app/main.py:16-22`

- [ ] **Step 1: Write the failing test**

```python
def test_create_app_creates_storage_states_dir(monkeypatch, reset_cached_state, tmp_path) -> None:
    """create_app should ensure storage_states directory exists."""
    monkeypatch.setenv("AUTH_SESSION_SECRET", "test-secret")
    monkeypatch.setenv("STORAGE_STATE_DIR", str(tmp_path / "test_states"))
    from app.core.config import get_settings
    get_settings.cache_clear()
    from app.main import create_app
    app = create_app()
    assert (tmp_path / "test_states").exists()
    assert hasattr(app.state, "storage_states_dir")
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && uv run pytest tests/unit/ -k "storage_states_dir" -v`
Expected: FAIL — `storage_states_dir` not set on app.state.

- [ ] **Step 3: Write minimal implementation**

In `backend/app/main.py`, after line 22 (`ARTIFACTS_DIR.mkdir(parents=True, exist_ok=True)`), add:

```python
    STORAGE_STATES_DIR = Path(settings.storage_state_dir)
    STORAGE_STATES_DIR.mkdir(parents=True, exist_ok=True)
```

After line 28 (`app.state.artifacts_dir = ARTIFACTS_DIR`), add:

```python
    app.state.storage_states_dir = STORAGE_STATES_DIR
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && uv run pytest tests/unit/ -k "storage_states_dir" -v`
Expected: PASS

- [ ] **Step 5: Run full test suite to check no regressions**

Run: `cd backend && uv run pytest tests/unit/ -v`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add backend/app/main.py
git commit -m "feat: create storage_states directory on app startup"
```

---

### Task 8: VLM Default Verification

**Files:**
- No new files — verify existing test coverage.

- [ ] **Step 1: Verify existing VLM tests still pass with new default**

Run: `cd backend && uv run pytest tests/unit/ -k "ai_visual" -v`
Expected: All PASS — existing tests use `monkeypatch` to set values explicitly, so default change shouldn't break them.

- [ ] **Step 2: Write an explicit default test if none exists**

Add to an appropriate test file:

```python
def test_ai_visual_locate_default_is_enabled(monkeypatch, reset_cached_state) -> None:
    """VLM should be enabled by default without explicit env var."""
    monkeypatch.setenv("AUTH_SESSION_SECRET", "test-secret")
    monkeypatch.delenv("ENABLE_AI_VISUAL_LOCATE", raising=False)
    from app.core.config import get_settings
    get_settings.cache_clear()
    settings = get_settings()
    assert settings.enable_ai_visual_locate is True
```

Run: `cd backend && uv run pytest tests/unit/ -k "ai_visual_locate_default" -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add backend/tests/unit/
git commit -m "test: verify VLM visual locate is enabled by default"
```

---

### Task 9: Integration Test

**Files:**
- Create: `backend/tests/integration/test_dom_aware_generation.py`

- [ ] **Step 1: Write the integration test**

Create `backend/tests/integration/test_dom_aware_generation.py`:

```python
"""Integration tests for DOM-aware DSL generation flow.

These tests require Playwright to be installed.
Run with: uv run pytest tests/integration/test_dom_aware_generation.py -v
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
        elements = collect_interactable_elements("https://this-domain-does-not-exist-12345.example.com")
        assert elements == []


class TestCaptureBrowserSession:
    """Integration tests for session capture and reuse."""

    @pytest.mark.browser_integration
    def test_capture_and_reuse_session(self, the_internet_url: str, tmp_path: Path) -> None:
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

        # Now explore with the saved state
        state_path = str(tmp_path / "1.json")
        elements = collect_interactable_elements(
            "https://the-internet.herokuapp.com/secure",
            storage_state_path=state_path,
        )
        # Should have elements on the secure page (logout button etc.)
        assert len(elements) >= 1
```

- [ ] **Step 2: Run integration test**

Run: `cd backend && uv run pytest tests/integration/test_dom_aware_generation.py -v -m browser_integration`
Expected: PASS (requires network and Playwright installed)

- [ ] **Step 3: Commit**

```bash
git add backend/tests/integration/test_dom_aware_generation.py
git commit -m "test: add integration tests for DOM-aware DSL generation"
```

---

## Self-Review

**Spec coverage check:**
- `explore_page` tool: Task 3 (core) + Task 5 (registration) ✓
- `capture_page_session` tool: Task 4 (core) + Task 5 (registration) ✓
- Storage state file management: Task 2 ✓
- VLM default enabled: Task 1 (config) + Task 8 (verification) ✓
- Draft prompt safeguard: Task 6 ✓
- Startup directory creation: Task 7 ✓
- Integration test: Task 9 ✓
- Error handling (unreachable URL, stale state, corrupt meta): covered in Task 2 + Task 3 + Task 5 ✓

**Placeholder scan:** No TBD, TODO, or vague instructions found.

**Type consistency:** All function signatures match between definition (page_explorer.py) and usage (planning_tools.py). `project_id: int` is consistent throughout.
