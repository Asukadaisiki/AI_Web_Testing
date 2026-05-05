"""Playwright-based page exploration and browser session management."""
from __future__ import annotations

import json
import logging
import re
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any

from playwright.sync_api import sync_playwright

from app.core.config import get_settings
from app.locators.fallback import EXTRACT_INTERACTABLE_ELEMENTS_SCRIPT
from app.runners.pre_scorer import score_candidates_for_element, ELEMENT_TYPE_SCORES

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


def _compute_element_stability(element: dict[str, Any], all_elements: list[dict[str, Any]]) -> float:
    """Compute a stability score for an element based on its distinguishing attributes.

    Scoring rules (aligned with research doc Section 5.1):
    - data-testid unique: 0.95
    - stable id (non-hash): 0.90
    - aria-label + role unique: 0.80
    - name/type combo: 0.75
    - href with business path: 0.70
    - css_selector short & stable: 0.55
    - text-only match: 0.40
    - xpath with position index: 0.20
    """
    tag = element.get("tag", "")
    text = element.get("text") or ""
    data_testid = element.get("data_testid")
    elem_id = element.get("id") or ""
    aria_label = element.get("aria_label")
    role = element.get("role")
    href = element.get("href")
    css = element.get("css_selector") or ""
    xpath = element.get("xpath") or ""

    # Count how many elements share the same tag+text combo (duplicates)
    same_tag_text = sum(
        1 for e in all_elements
        if e.get("tag") == tag and (e.get("text") or "") == text
    )
    has_duplicates = same_tag_text > 1

    # Highest priority: unique data-testid
    if data_testid:
        testid_count = sum(1 for e in all_elements if e.get("data_testid") == data_testid)
        if testid_count == 1:
            return 0.95
        return 0.85

    # Stable element id (not hash/uuid pattern)
    _DYNAMIC_ID = re.compile(r"[0-9a-f]{8,}|auto\d+|tmp| rnd", re.IGNORECASE)
    if elem_id and not _DYNAMIC_ID.search(elem_id):
        id_count = sum(1 for e in all_elements if e.get("id") == elem_id)
        if id_count == 1:
            return 0.90

    # Unique aria-label + role
    if aria_label and role:
        combo = (aria_label, role)
        combo_count = sum(
            1 for e in all_elements
            if e.get("aria_label") == combo[0] and e.get("role") == combo[1]
        )
        if combo_count == 1:
            return 0.80

    # Unique aria-label alone
    if aria_label:
        al_count = sum(1 for e in all_elements if e.get("aria_label") == aria_label)
        if al_count == 1:
            return 0.78

    # href with business path (not # or javascript:)
    if href and tag == "a" and not href.startswith(("#", "javascript:")):
        href_count = sum(1 for e in all_elements if e.get("href") == href and e.get("tag") == "a")
        if href_count == 1:
            return 0.70
        if href_count <= 3:
            return 0.60

    # Unique text (but only if not duplicated)
    if text and not has_duplicates:
        return 0.55

    # Text with duplicates — moderate
    if text and has_duplicates:
        if css and len(css) < 60:
            return 0.45
        return 0.35

    # XPath with position index — least stable
    if re.search(r"\[\d+\]", xpath):
        return 0.20

    # Fallback
    return 0.30


def _format_element_rich(element: dict[str, Any], stability: float) -> str:
    """Format a single element with full attributes and stability score."""
    tag = element.get("tag", "unknown")
    parts: list[str] = [tag]

    # Primary distinguishing attributes (ordered by stability)
    data_testid = element.get("data_testid")
    if data_testid:
        parts.append(f"[data-testid='{data_testid}']")

    elem_id = element.get("id")
    if elem_id:
        parts[0] = f"{tag}#{elem_id}"

    role = element.get("role")
    if role:
        parts.append(f"[role='{role}']")

    aria_label = element.get("aria_label")
    if aria_label:
        parts.append(f"[aria-label='{aria_label}']")

    placeholder = element.get("placeholder")
    if placeholder:
        parts.append(f"[placeholder='{placeholder}']")

    text = element.get("text")
    if text:
        truncated = text[:80] + ("..." if len(text) > 80 else "")
        parts.append(f"[text='{truncated}']")

    href = element.get("href")
    if href:
        parts.append(f"[href='{href}']")

    if element.get("discovered_via_interaction"):
        parts.append("[dynamic]")

    primary = "".join(parts)

    # Secondary attributes (pipe-separated)
    extras: list[str] = []
    css = element.get("css_selector")
    if css:
        extras.append(f"css={css}")

    xpath = element.get("xpath")
    if xpath:
        extras.append(f"xpath={xpath}")

    rect = element.get("rect")
    if rect and isinstance(rect, dict):
        extras.append(f"rect={rect.get('x', 0):.0f},{rect.get('y', 0):.0f},{rect.get('width', 0):.0f},{rect.get('height', 0):.0f}")

    enabled = element.get("enabled")
    if enabled is False:
        extras.append("disabled")

    verified = element.get("verified_selectors")
    if verified:
        v_strategies = [v["strategy"] for v in verified[:5]]
        extras.append(f"verified={len(verified)}({','.join(v_strategies)})")
    extras.append(f"stable={stability:.2f}")

    if element.get("candidates"):
        top3 = element["candidates"][:3]
        cand_strs = []
        for cand in top3:
            sel_short = str(cand.get("selector", ""))[:40]
            cand_strs.append(f"{cand['strategy']}={sel_short}({cand['pre_score']:.2f})")
        extras.append(f"candidates={'|'.join(cand_strs)}")

    secondary = " | ".join(extras)
    return f"{primary} | {secondary}"


MAX_PROMPT_ELEMENTS_CHARS = 80000


def _extract_stability(line: str) -> float:
    """Extract the stable=X.XX value from a formatted element line."""
    m = re.search(r"stable=([\d.]+)", line)
    return float(m.group(1)) if m else 0.0


def format_elements_for_prompt(elements: list[dict[str, Any]]) -> str:
    """Format collected DOM elements into rich text for AI prompt injection.

    Each element is formatted with full attributes and a stability score,
    enabling the AI to make informed decisions about which locator strategy to use.
    """
    visible = [e for e in elements if e.get("visible", True)]
    lines: list[str] = []
    for element in visible:
        stability = _compute_element_stability(element, visible)
        lines.append(_format_element_rich(element, stability))
    result = "\n".join(lines)
    if len(result) > MAX_PROMPT_ELEMENTS_CHARS:
        prioritized = sorted(
            [(line, _extract_stability(line)) for line in lines],
            key=lambda x: x[1], reverse=True,
        )
        kept: list[str] = []
        char_count = 0
        truncated_count = 0
        for line, _ in prioritized:
            if char_count + len(line) + 1 > MAX_PROMPT_ELEMENTS_CHARS:
                truncated_count += 1
                continue
            kept.append(line)
            char_count += len(line) + 1
        kept.sort(key=lambda x: lines.index(x))
        result = "\n".join(kept)
        result += f"\n... [truncated {truncated_count} low-stability elements to fit prompt limits]"
    return result


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
    "filter", "brand", "brands", "category", "categories",
    "sort", "search", "apply",
]


def _discover_interactive_elements(
    page,
    *,
    max_clicks: int = 5,
) -> list[dict[str, Any]]:
    """Click key trigger buttons and capture dynamically appearing elements."""
    baseline: set[str] = set()
    baseline_payload = page.evaluate(EXTRACT_INTERACTABLE_ELEMENTS_SCRIPT, get_settings().explore_max_elements)
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

            new_payload = page.evaluate(EXTRACT_INTERACTABLE_ELEMENTS_SCRIPT, get_settings().explore_max_elements)
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
            payload = page.evaluate(EXTRACT_INTERACTABLE_ELEMENTS_SCRIPT, get_settings().explore_max_elements)

            # --- 构建元素列表 ---
            if not isinstance(payload, list):
                payload = []
            result: list[dict[str, Any]] = []
            for elem in payload:
                if not isinstance(elem, dict):
                    continue
                element = {
                    "tag": elem.get("tag", "unknown"),
                    "id": _extract_element_id(elem),
                    "text": elem.get("text"),
                    "role": elem.get("role"),
                    "aria_label": elem.get("aria_label"),
                    "placeholder": elem.get("placeholder"),
                    "href": elem.get("href"),
                    "data_testid": elem.get("data_testid"),
                    "css_selector": elem.get("css_selector"),
                    "xpath": elem.get("xpath"),
                    "rect": elem.get("rect"),
                    "visible": elem.get("visible", False),
                    "enabled": elem.get("enabled", False),
                }
                element["candidates"] = score_candidates_for_element(element)
                tag = element.get("tag", "")
                element["element_type_score"] = ELEMENT_TYPE_SCORES.get(tag, {"dom": 0.60, "vlm": 0.40})
                result.append(element)

            # --- Live-element verification: 当场验证选择器 (page 还活着) ---
            result = _verify_locators_on_page(page, result)

            context.close()
            browser.close()
    except Exception as exc:
        logger.warning("collect_interactable_elements failed for url=%s: %s", url, exc)
        return []

    return result


def _locator_matches_element(page, locator, elem: dict[str, Any]) -> bool:
    """验证 locator.first 指向的元素与 elem 是同一个（比较 tag + text）。"""
    try:
        actual = locator.first.evaluate(
            """el => ({
                tag: el.tagName.toLowerCase(),
                text: (el.innerText || el.textContent || "").replace(/\\s+/g, " ").trim().slice(0, 80),
            })"""
        )
        expected_tag = (elem.get("tag") or "").lower()
        expected_text = ((elem.get("text") or "").replace("\n", " ").strip())[:80]
        return actual["tag"] == expected_tag and actual["text"] == expected_text
    except Exception:
        return False


def _verify_locators_on_page(page, elements: list[dict[str, Any]]) -> list[dict[str, Any]]:
    """在同一个 page 实例上为每个可见元素验证候选定位器。

    对每个元素的每种候选策略，构建真实的 Playwright locator 并验证:
    1. locator.count() == 1（无歧义）
    2. 解析到的元素与目标元素是同一个（指纹匹配）

    通过验证的策略存入 ``verified_selectors``，供 runner 优先使用。
    """
    _IMPLICIT_ROLE: dict[str, str] = {
        "a": "link", "button": "button", "input": "textbox",
        "select": "combobox", "textarea": "textbox", "img": "img",
    }

    for elem in elements:
        if not elem.get("visible") or not elem.get("enabled"):
            elem["verified_selectors"] = []
            continue

        tag = elem.get("tag", "")
        text = (elem.get("text") or "").strip()
        placeholder = (elem.get("placeholder") or "").strip()
        aria_label = (elem.get("aria_label") or "").strip()
        data_testid = (elem.get("data_testid") or "").strip()
        element_id = (elem.get("id") or "").strip()
        css = (elem.get("css_selector") or "").strip()
        effective_role = elem.get("role") or _IMPLICIT_ROLE.get(tag, "")
        name_attr = (elem.get("name") or "").strip()

        fingerprint = None  # no longer needed
        if not elem.get("text") and not elem.get("placeholder") and not elem.get("aria_label"):
            continue

        verified: list[dict[str, Any]] = []

        # 按论文的优先级构建候选并逐个验证
        def _try_verify(strategy: str, loc_expr: dict[str, Any], factory) -> None:
            try:
                loc = factory()
                if loc.count() == 1 and _locator_matches_element(page, loc, elem):
                    verified.append({"strategy": strategy, **loc_expr})
            except Exception:
                pass

        # data-testid (最高优先级)
        if data_testid:
            _try_verify("data-testid", {"selector": data_testid},
                        lambda: page.get_by_test_id(data_testid))

        # role 精确匹配
        if effective_role and text:
            _try_verify("role", {"role": effective_role, "name": text},
                        lambda: page.get_by_role(effective_role, name=text, exact=True))

        # role 模糊匹配
        if effective_role:
            for candidate_text in (text, placeholder, aria_label):
                if candidate_text:
                    _try_verify("role_fuzzy", {"role": effective_role, "name": candidate_text},
                                lambda t=candidate_text: page.get_by_role(effective_role, name=t))

        # CSS selector (来自 buildCssSelector)
        if css:
            _try_verify("css", {"selector": css},
                        lambda: page.locator(css))

        # placeholder
        if placeholder:
            _try_verify("placeholder", {"selector": placeholder},
                        lambda: page.get_by_placeholder(placeholder, exact=True))
            _try_verify("placeholder_fuzzy", {"selector": placeholder},
                        lambda: page.get_by_placeholder(placeholder))

        # label
        effective_label = aria_label or text
        if effective_label:
            _try_verify("label", {"selector": effective_label},
                        lambda: page.get_by_label(effective_label, exact=True))
            _try_verify("label_fuzzy", {"selector": effective_label},
                        lambda: page.get_by_label(effective_label))

        # text 精确 (只有非链接/按钮才用，链接/按钮已由 role 覆盖)
        if text and effective_role not in ("link", "button"):
            _try_verify("text", {"selector": text},
                        lambda: page.get_by_text(text, exact=True))

        # element id
        if element_id:
            _try_verify("element_id", {"selector": element_id},
                        lambda: page.locator(f"#{element_id}"))

        # name 属性
        if name_attr and tag:
            _try_verify("name", {"selector": f"{tag}[name='{name_attr}']"},
                        lambda: page.locator(f"{tag}[name='{name_attr}']"))

        # XPath
        xpath = elem.get("xpath") or ""
        if xpath:
            _try_verify("xpath", {"selector": xpath},
                        lambda: page.locator(f"xpath={xpath}"))

        elem["verified_selectors"] = verified

    return elements


_CSS_TARGET_PATTERN = re.compile(
    r"""^(?:[a-zA-Z][\w-]*)?"""  # optional tag prefix
    r"""(?:\[(\w+)='([^']*)'\])"""  # [attr='value']
    r"""|^(?:[a-zA-Z][\w-]*):has-text\(['\"]([^'\"]+)['\"]\)$"""  # tag:has-text('value')
    r"""|^(?:[a-zA-Z][\w-]*):contains\(['\"]([^'\"]+)['\"]\)$"""  # tag:contains('value')
)


def _extract_text_from_css_target(target: str) -> str | None:
    """Extract meaningful text from a CSS-selector-like target string.

    Examples:
        input[placeholder='Email Address'] -> Email Address
        button:has-text('Login') -> Login
        a[href='/products'] -> None (path-based, no extractable label)
    """
    # [attr='value'] pattern
    m = re.search(r"\[(\w+)='([^']*)'\]", target)
    if m:
        attr, val = m.group(1), m.group(2)
        if attr in ("placeholder", "aria-label", "title", "name", "alt"):
            return val
        if attr == "id" and val:
            return None  # id-based is structural, not text
        if attr == "href":
            return None  # path-based
        return val  # fallback: try the value
    # :has-text('value') pattern
    m = re.search(r":has-text\(['\"]([^'\"]+)['\"]\)", target)
    if m:
        return m.group(1)
    # :contains('value') pattern
    m = re.search(r":contains\(['\"]([^'\"]+)['\"]\)", target)
    if m:
        return m.group(1)
    # #id pattern
    if re.match(r"^#[a-zA-Z_][\w-]*$", target):
        return None
    return None


def _resolve_step_locator(page, target: str, *, kind: str):
    """Resolve a step target to a Playwright locator.

    Handles both CSS-selector-style targets (generated by the AI model)
    and simple semantic targets (label, placeholder, text, role name).

    *kind* is ``"input"`` or ``"click"``.
    """
    # Strategy 1: If target looks like a CSS selector, try it directly first
    looks_like_css = any(c in target for c in ("[", ":", "#", "."))
    if looks_like_css:
        try:
            css_loc = page.locator(target)
            if css_loc.count() == 1:
                return css_loc
        except Exception:
            pass

    # Strategy 2: Extract meaningful text from CSS patterns and try semantic matching
    extracted = _extract_text_from_css_target(target)
    semantic_target = extracted if extracted else target

    if kind == "input":
        input_candidates = [
            ("placeholder", lambda: page.get_by_placeholder(semantic_target, exact=True)),
            ("placeholder_fuzzy", lambda: page.get_by_placeholder(semantic_target)),
            ("label", lambda: page.get_by_label(semantic_target, exact=True)),
            ("label_fuzzy", lambda: page.get_by_label(semantic_target)),
            ("textbox_role", lambda: page.get_by_role("textbox", name=semantic_target)),
        ]
        for _name, factory in input_candidates:
            try:
                loc = factory()
                if loc.count() > 0:
                    return loc
            except Exception:
                continue

    elif kind == "click":
        click_candidates = [
            ("button_role", lambda: page.get_by_role("button", name=semantic_target, exact=True)),
            ("button_role_fuzzy", lambda: page.get_by_role("button", name=semantic_target)),
            ("link_role", lambda: page.get_by_role("link", name=semantic_target, exact=True)),
            ("link_role_fuzzy", lambda: page.get_by_role("link", name=semantic_target)),
            ("label", lambda: page.get_by_label(semantic_target)),
            ("text", lambda: page.get_by_text(semantic_target, exact=True)),
            ("text_fuzzy", lambda: page.get_by_text(semantic_target)),
        ]
        for _name, factory in click_candidates:
            try:
                loc = factory()
                if loc.count() > 0:
                    return loc
            except Exception:
                continue

    # Strategy 3: If target might be a CSS selector we should try more broadly
    if looks_like_css:
        try:
            css_loc = page.locator(target)
            if css_loc.count() >= 1:
                return css_loc.first
        except Exception:
            pass

    return None


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
                action = (step.get("action") or "").strip().lower()
                target = step.get("target", "")
                value = step.get("value", "")
                # Normalize AI-generated action names
                if action in ("type", "fill", "input"):
                    kind = "input"
                elif action in ("click", "press", "tap"):
                    kind = "click"
                else:
                    kind = action
                locator = _resolve_step_locator(page, target, kind=kind)
                if locator is not None:
                    if kind == "input":
                        locator.fill(str(value))
                    elif kind == "click":
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
                    payload = page.evaluate(EXTRACT_INTERACTABLE_ELEMENTS_SCRIPT, get_settings().explore_max_elements)
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

                elements: list[dict[str, Any]] = []
                if isinstance(payload, list):
                    for elem in payload:
                        if not isinstance(elem, dict):
                            continue
                        element = {
                            "tag": elem.get("tag", "unknown"),
                            "id": _extract_element_id(elem),
                            "text": elem.get("text"),
                            "role": elem.get("role"),
                            "aria_label": elem.get("aria_label"),
                            "placeholder": elem.get("placeholder"),
                            "href": elem.get("href"),
                            "data_testid": elem.get("data_testid"),
                            "css_selector": elem.get("css_selector"),
                            "xpath": elem.get("xpath"),
                            "rect": elem.get("rect"),
                            "visible": elem.get("visible", False),
                            "enabled": elem.get("enabled", False),
                        }
                        element["candidates"] = score_candidates_for_element(element)
                        tag = element.get("tag", "")
                        element["element_type_score"] = ELEMENT_TYPE_SCORES.get(tag, {"dom": 0.60, "vlm": 0.40})
                        elements.append(element)

                formatted = format_elements_for_prompt(elements)

                # Interactive element discovery
                try:
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


def collect_flow_elements(
    steps: list[dict[str, Any]],
    *,
    storage_state_path: str | None = None,
    enable_vlm_annotation: bool = True,
    timeout_ms: int = 60000,
) -> list[dict[str, Any]]:
    """Execute a flow with actions between page visits and collect elements per state.

    Each step in *steps* may have:
      - ``url``: navigate to this URL first (optional; if omitted, stays on current page)
      - ``description``: human label for this step (used in state markers)
      - ``actions``: list of actions to perform after navigation, each with:
        - ``action``: "click", "input", or "wait_for"
        - ``target``: semantic label / placeholder / id for the element
        - ``value``: fill text (for input actions)

    A **page state** is defined by the URL after actions complete.  Each
    distinct URL gets a ``page_state_id`` (S0, S1, …) that is recorded on
    every element and in the formatted output.
    """
    if not steps:
        return []

    settings = get_settings()
    results: list[dict[str, Any]] = []
    state_index = 0
    url_to_state: dict[str, str] = {}

    def _resolve_state_id(url: str, description: str = "") -> str:
        nonlocal state_index
        # Use (url, description) as key so that revisiting the same URL
        # with a different flow context (e.g. before/after login) gets
        # distinct state IDs.
        key = f"{url.rstrip('/')}|{description.strip()}" if description else url.rstrip("/")
        if key not in url_to_state:
            sid = f"S{state_index}"
            url_to_state[key] = sid
            state_index += 1
            return sid
        return url_to_state[key]

    def _execute_action(page, action_def: dict[str, Any]) -> None:
        act = (action_def.get("action") or "").strip().lower()
        target = (action_def.get("target") or "").strip()
        value = action_def.get("value", "")
        if not act or not target:
            return

        # Normalize AI-generated action names
        if act in ("type", "fill", "input"):
            kind = "input"
        elif act in ("click", "press", "tap"):
            kind = "click"
        else:
            kind = act
        loc = _resolve_step_locator(page, target, kind=kind)
        if loc is not None:
            if kind == "input":
                loc.fill(str(value))
            elif kind == "click":
                loc.click()
            return

        logger.debug("_execute_action: no locator matched target=%r for action=%s", target, act)
        if act == "wait_for":
            for factory_desc in [
                ("text", lambda: page.get_by_text(target)),
                ("selector", lambda: page.locator(f"text={target}")),
            ]:
                try:
                    candidate = factory_desc[1]()
                    candidate.first.wait_for(state="visible", timeout=5000)
                    break
                except Exception:
                    continue

    def _collect_current_page(page, url: str, state_id: str) -> dict[str, Any]:
        try:
            payload = page.evaluate(
                EXTRACT_INTERACTABLE_ELEMENTS_SCRIPT, settings.explore_max_elements,
            )
        except Exception as exc:
            logger.warning("collect_flow_elements: evaluate failed for %s: %s", url, exc)
            return {
                "url": url, "page_state": state_id,
                "elements": [], "formatted": "", "element_count": 0,
                "screenshot_available": False, "vlm_annotation": None,
                "error": str(exc),
            }

        elements: list[dict[str, Any]] = []
        if isinstance(payload, list):
            for elem in payload:
                if not isinstance(elem, dict):
                    continue
                element = {
                    "tag": elem.get("tag", "unknown"),
                    "id": _extract_element_id(elem),
                    "text": elem.get("text"),
                    "role": elem.get("role"),
                    "aria_label": elem.get("aria_label"),
                    "placeholder": elem.get("placeholder"),
                    "href": elem.get("href"),
                    "data_testid": elem.get("data_testid"),
                    "css_selector": elem.get("css_selector"),
                    "xpath": elem.get("xpath"),
                    "rect": elem.get("rect"),
                    "visible": elem.get("visible", False),
                    "enabled": elem.get("enabled", False),
                    "page_state": state_id,
                }
                element["candidates"] = score_candidates_for_element(element)
                tag = element.get("tag", "")
                element["element_type_score"] = ELEMENT_TYPE_SCORES.get(tag, {"dom": 0.60, "vlm": 0.40})
                elements.append(element)

        formatted = format_elements_for_prompt(elements)

        # Interactive element discovery
        try:
            interactive = _discover_interactive_elements(
                page, max_clicks=settings.explore_interactive_max_clicks,
            )
            if interactive:
                for el in interactive:
                    el["page_state"] = state_id
                elements.extend(interactive)
                formatted = format_elements_for_prompt(elements)
                logger.info("Discovered %d interactive elements on %s", len(interactive), url)
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

        return {
            "url": url,
            "page_state": state_id,
            "elements": elements,
            "formatted": formatted,
            "element_count": len(elements),
            "screenshot_available": screenshot_available,
            "vlm_annotation": vlm_annotation,
        }

    try:
        pw = _sync_playwright_context()
        with pw as playwright:
            browser = playwright.chromium.launch(headless=True)
            context_kwargs: dict[str, Any] = {}
            if storage_state_path and Path(storage_state_path).exists():
                context_kwargs["storage_state"] = storage_state_path
            context = browser.new_context(**context_kwargs)
            page = context.new_page()

            current_url = "about:blank"

            for step in steps:
                if not isinstance(step, dict):
                    continue

                # Navigate if URL is provided
                step_url = step.get("url")
                if step_url and isinstance(step_url, str) and step_url.strip():
                    try:
                        page.goto(step_url.strip(), timeout=timeout_ms, wait_until="domcontentloaded")
                        try:
                            page.wait_for_load_state("networkidle", timeout=timeout_ms)
                        except Exception:
                            pass
                        current_url = page.url
                    except Exception as exc:
                        logger.warning("collect_flow_elements: goto failed for %s: %s", step_url, exc)
                        results.append({
                            "url": step_url.strip(),
                            "page_state": "ERROR",
                            "elements": [], "formatted": "", "element_count": 0,
                            "screenshot_available": False, "vlm_annotation": None,
                            "error": str(exc),
                        })
                        continue

                # Execute actions if provided
                actions = step.get("actions")
                if isinstance(actions, list):
                    for action_def in actions:
                        try:
                            _execute_action(page, action_def)
                        except Exception as exc:
                            logger.warning(
                                "collect_flow_elements: action failed (%s): %s",
                                action_def.get("action", "?"), exc,
                            )
                current_url = page.url

                # Collect elements for this page state
                description = step.get("description", "")
                state_id = _resolve_state_id(current_url, description)
                result = _collect_current_page(page, current_url, state_id)
                if description:
                    result["description"] = description
                results.append(result)

            context.close()
            browser.close()
    except Exception as exc:
        logger.warning("collect_flow_elements browser crash: %s", exc)

    return results


# ---------------------------------------------------------------------------
# Helpers for building state-aware formatted output
# ---------------------------------------------------------------------------

def build_flow_formatted_output(page_results: list[dict[str, Any]]) -> str:
    """Build a state-aware combined formatted string from flow exploration results."""
    sections: list[str] = []
    for pr in page_results:
        state_id = pr.get("page_state", "?")
        url = pr.get("url", "")
        description = pr.get("description", "")
        formatted = pr.get("formatted", "")
        annotation = pr.get("vlm_annotation")

        if description:
            header = f"=== 页面状态 {state_id}: {url}（{description}）==="
        else:
            header = f"=== 页面状态 {state_id}: {url} ==="
        section = f"{header}\n{formatted}"
        if annotation:
            section += f"\n\n页面布局描述: {annotation}"
        sections.append(section)
    return "\n\n".join(sections)


__all__ = [
    "build_flow_formatted_output",
    "capture_browser_session",
    "collect_flow_elements",
    "collect_interactable_elements",
    "collect_multi_page_elements",
    "format_elements_for_prompt",
    "get_storage_state_path",
    "is_storage_state_stale",
    "load_storage_state_meta",
    "save_storage_state",
]
