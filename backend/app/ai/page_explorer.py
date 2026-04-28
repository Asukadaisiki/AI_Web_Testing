"""Playwright-based page exploration and browser session management."""
from __future__ import annotations

import json
import logging
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any

from playwright.sync_api import sync_playwright

from app.locators.fallback import EXTRACT_INTERACTABLE_ELEMENTS_SCRIPT

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
    meta = {"source_url": source_url, "saved_at": datetime.now(UTC).isoformat()}
    meta_path.write_text(json.dumps(meta, ensure_ascii=False), encoding="utf-8")
    logger.info(
        "Saved storage state for project_id=%s source_url=%s", project_id, source_url
    )


def load_storage_state_meta(
    base_dir: Path, *, project_id: int
) -> dict[str, Any] | None:
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
        return (datetime.now(UTC) - saved_at) > timedelta(hours=STALE_THRESHOLD_HOURS)
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
        if element.get("discovered_via_interaction"):
            parts.append("[dynamic]")
        lines.append(" ".join(parts))
    return "\n".join(lines)


def _sync_playwright_context():
    """Indirection point for testing -- returns the sync_playwright context manager."""
    return sync_playwright()


def _extract_element_id(elem: dict[str, Any]) -> str:
    """Extract element id from css_selector (#id) or fall back to data_testid."""
    css = elem.get("css_selector", "")
    if css.startswith("#") and " > " not in css:
        return css[1:]
    return elem.get("data_testid") or ""


_INTERACTIVE_KEYWORDS = [
    "add to cart", "submit", "view product", "view cart",
    "add to bag", "buy now", "checkout", "place order",
]


def _discover_interactive_elements(
    page,
    *,
    max_clicks: int = 5,
) -> list[dict[str, Any]]:
    """Click key trigger buttons and capture dynamically appearing elements."""
    baseline: set[str] = set()
    baseline_payload = page.evaluate(EXTRACT_INTERACTABLE_ELEMENTS_SCRIPT)
    for elem in (baseline_payload or []):
        css = elem.get("css_selector", "")
        if css:
            baseline.add(css)

    discovered: list[dict[str, Any]] = []
    triggers = page.query_selector_all("button, a")
    clicks = 0

    for trigger in triggers:
        if clicks >= max_clicks:
            break
        try:
            if not trigger.is_visible():
                continue
            text = (trigger.inner_text() or "").strip().lower()
            if not any(kw in text for kw in _INTERACTIVE_KEYWORDS):
                continue
            box = trigger.bounding_box()
            if box and (box["width"] < 10 or box["height"] < 10):
                continue

            trigger.click(timeout=300)
            page.wait_for_timeout(500)

            new_payload = page.evaluate(EXTRACT_INTERACTABLE_ELEMENTS_SCRIPT)
            for elem in (new_payload or []):
                css = elem.get("css_selector", "")
                if css and css not in baseline:
                    elem["discovered_via_interaction"] = True
                    discovered.append(elem)
                    baseline.add(css)

            page.keyboard.press("Escape")
            page.wait_for_timeout(300)
            clicks += 1
        except Exception as exc:
            logger.debug("Interactive trigger failed: %s", exc)
            try:
                page.keyboard.press("Escape")
                page.wait_for_timeout(300)
            except Exception:
                pass

    return discovered


def collect_interactable_elements(
    url: str,
    *,
    storage_state_path: str | None = None,
    timeout_ms: int = 60000,
) -> list[dict[str, Any]]:
    """Open *url* in a temporary Playwright context and return interactable elements."""
    try:
        pw = _sync_playwright_context()
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
            "id": _extract_element_id(elem),
            "text": elem.get("text"),
            "role": elem.get("role"),
            "aria_label": elem.get("aria_label"),
            "placeholder": elem.get("placeholder"),
            "href": elem.get("href"),
            "visible": elem.get("visible", False),
            "enabled": elem.get("enabled", False),
        }
        for elem in payload
        if isinstance(elem, dict)
    ]


def capture_browser_session(
    url: str,
    steps: list[dict[str, Any]],
    *,
    storage_dir: Path,
    project_id: int,
    timeout_ms: int = 60000,
) -> dict[str, Any]:
    """Execute *steps* on *url*, then persist the browser session state."""
    try:
        pw = _sync_playwright_context()
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
            save_storage_state(storage_dir, project_id=project_id, state=dict(state), source_url=url)
            context.close()
            browser.close()
    except Exception as exc:
        logger.warning("capture_browser_session failed for url=%s: %s", url, exc)
        return {"success": False, "error": str(exc)}

    return {
        "success": True,
        "message": f"已保存 {url} 的会话状态（包含 {cookie_count} 个 cookie）",
    }


def collect_multi_page_elements(
    urls: list[str],
    *,
    storage_state_path: str | None = None,
    enable_vlm_annotation: bool = True,
    timeout_ms: int = 60000,
) -> list[dict[str, Any]]:
    """Open *urls* sequentially in a single Playwright context and collect elements for each page.

    Reuses the same browser session across all URLs so that cookies / auth state
    established by earlier pages carry over to later ones.
    """
    if not urls:
        return []

    results: list[dict[str, Any]] = []
    try:
        pw = _sync_playwright_context()
        with pw as playwright:
            browser = playwright.chromium.launch(headless=True)
            context_kwargs: dict[str, Any] = {}
            if storage_state_path and Path(storage_state_path).exists():
                context_kwargs["storage_state"] = storage_state_path
            context = browser.new_context(**context_kwargs)
            page = context.new_page()

            for url in urls:
                url = url.strip()
                try:
                    page.goto(url, timeout=timeout_ms, wait_until="domcontentloaded")
                    try:
                        page.wait_for_load_state("networkidle", timeout=timeout_ms)
                    except Exception:
                        pass  # non-fatal
                    payload = page.evaluate(EXTRACT_INTERACTABLE_ELEMENTS_SCRIPT)
                except Exception as exc:
                    logger.warning("collect_multi_page_elements: page load failed for %s: %s", url, exc)
                    results.append({
                        "url": url,
                        "elements": [],
                        "formatted": "",
                        "element_count": 0,
                        "screenshot_available": False,
                        "vlm_annotation": None,
                        "error": str(exc),
                    })
                    continue

                elements = [
                    {
                        "tag": elem.get("tag", "unknown"),
                        "id": _extract_element_id(elem),
                        "text": elem.get("text"),
                        "role": elem.get("role"),
                        "aria_label": elem.get("aria_label"),
                        "placeholder": elem.get("placeholder"),
                        "href": elem.get("href"),
                        "visible": elem.get("visible", False),
                        "enabled": elem.get("enabled", False),
                    }
                    for elem in payload
                    if isinstance(elem, dict)
                ] if isinstance(payload, list) else []

                formatted = format_elements_for_prompt(elements)

                # Interactive element discovery
                try:
                    from app.core.config import get_settings
                    settings = get_settings()
                    interactive = _discover_interactive_elements(
                        page, max_clicks=settings.explore_interactive_max_clicks,
                    )
                    if interactive:
                        elements.extend(interactive)
                        formatted = format_elements_for_prompt(elements)
                        logger.info(
                            "Discovered %d interactive elements on %s",
                            len(interactive), url,
                        )
                except Exception as exc:
                    logger.warning("Interactive exploration failed for %s: %s", url, exc)

                screenshot_available = False
                vlm_annotation: str | None = None
                try:
                    _screenshot_bytes = page.screenshot()
                    screenshot_available = True
                    if enable_vlm_annotation:
                        from app.locators.ai_visual import describe_page_layout
                        import base64
                        vlm_annotation = describe_page_layout(
                            screenshot_base64=base64.b64encode(_screenshot_bytes).decode(),
                            page_url=url,
                        )
                except Exception as exc:
                    logger.warning("Screenshot/VLM failed for %s: %s", url, exc)

                results.append({
                    "url": url,
                    "elements": elements,
                    "formatted": formatted,
                    "element_count": len(elements),
                    "screenshot_available": screenshot_available,
                    "vlm_annotation": vlm_annotation,
                })

            context.close()
            browser.close()
    except Exception as exc:
        logger.warning("collect_multi_page_elements browser crash: %s", exc)

    return results


__all__ = [
    "capture_browser_session",
    "collect_interactable_elements",
    "collect_multi_page_elements",
    "format_elements_for_prompt",
    "get_storage_state_path",
    "is_storage_state_stale",
    "load_storage_state_meta",
    "save_storage_state",
]
