"""Playwright-based page exploration and browser session management."""
from __future__ import annotations

from contextlib import suppress
import json
import logging
import re
import threading
import time as _time_module
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any
from urllib.parse import urldefrag, urljoin, urlparse

from playwright.sync_api import sync_playwright

logger = logging.getLogger(__name__)

# ── A11y roles we EXCLUDE for the LLM (blacklist approach) ──────────────────
# Using blacklist instead of whitelist to avoid missing useful elements
IGNORED_A11Y_ROLES: set[str] = {
    # Internal / non-semantic roles that clutter the tree
    "none", "InlineTextBox", "layout table", "layout table cell",
    "layout table row", "LineBreak", "generic", "separator", "RootWebArea",
    "StaticText",
    # Abstraction roles (rarely useful for targeting)
    "roletype", "structure", "widget", "window",
}

_DOM_INTERACTIVE_SELECTOR = ", ".join(
    (
        "a[href]",
        "button",
        "input:not([type='hidden'])",
        "select",
        "textarea",
        "[contenteditable='true']",
        "[role='button']",
        "[role='link']",
        "[role='checkbox']",
        "[role='radio']",
        "[role='switch']",
        "[role='tab']",
        "[role='combobox']",
        "[role='searchbox']",
        "[role='textbox']",
        "[role='spinbutton']",
    )
)
_DOM_ATTR_WHITELIST = {
    "aria-label",
    "data-cy",
    "data-product-id",
    "data-qa",
    "data-test",
    "data-testid",
    "href",
    "id",
    "name",
    "placeholder",
    "role",
    "title",
    "type",
}

def _a11y_node_in_viewport(node: dict, viewport: dict) -> bool:
    bb = node.get("boundingBox")
    if not bb or not isinstance(bb, dict):
        return True
    vp_w = viewport.get("width", 1280)
    vp_h = viewport.get("height", 720)
    x, y, w, h = bb.get("x", 0), bb.get("y", 0), bb.get("width", 0), bb.get("height", 0)
    if w <= 0 or h <= 0:
        return True
    return x < vp_w and y < vp_h and (x + w) > 0 and (y + h) > 0


def _filter_a11y_nodes(
    raw_nodes: list[dict],
    *,
    viewport: dict | None = None,
) -> list[dict]:
    """Filter a11y nodes using blacklist approach to avoid missing useful elements."""
    if viewport is None:
        viewport = {"width": 1280, "height": 720}
    result: list[dict] = []
    for n in raw_nodes:
        if n.get("ignored", False):
            continue
        role = n.get("role", "unknown")
        if isinstance(role, dict):
            role = role.get("value", "unknown")
        # Blacklist: skip only known useless roles
        if role in IGNORED_A11Y_ROLES:
            continue
        if not _a11y_node_in_viewport(n, viewport):
            continue
        result.append(n)
    return result


# ── Stop words for keyword extraction ──────────────────────────────────────
_STOP_WORDS: set[str] = {
    "the", "a", "an", "is", "are", "was", "were", "be", "been", "being",
    "have", "has", "had", "do", "does", "did", "will", "would", "could",
    "should", "may", "might", "can", "shall", "to", "of", "in", "for",
    "on", "with", "at", "by", "from", "and", "or", "not", "no", "this",
    "that", "it", "its", "if", "so", "but", "as", "than", "then",
    "的", "是", "在", "和", "了", "有", "不", "人", "这", "中",
    "大", "为", "上", "个", "国", "我", "以", "要", "他", "时",
    "来", "用", "们", "生", "到", "作", "地", "于", "出", "会",
    "可", "也", "你", "对", "就", "能", "而", "那", "着", "得",
    "将", "下", "去", "说", "过", "看", "吧", "吗", "嗯",
    "需要", "然后", "用户", "点击", "操作", "进入", "验证", "检查",
    "确认", "确保", "之前", "之后", "使用", "已有", "测试", "页面",
}


def _extract_flow_keywords(core_user_flow_text: str | None) -> set[str]:
    if not core_user_flow_text or not core_user_flow_text.strip():
        return set()
    tokens = re.findall(r"[\w一-鿿]{2,}", core_user_flow_text, re.IGNORECASE)
    keywords: set[str] = set()
    for t in tokens:
        low = t.strip().lower()
        if low and low not in _STOP_WORDS:
            keywords.add(low)
    return keywords


def _cdp_to_a11y_nodes(
    cdp_result: dict,
    *,
    page_state: str = "S0",
) -> list[dict]:
    """Convert CDP a11y tree to standardized nodes using blacklist approach."""
    standardized: list[dict] = []
    for n in cdp_result.get("nodes", []):
        if n.get("ignored", False):
            continue
        role = (n.get("role") or {}).get("value", "unknown")
        # Blacklist: skip only known useless roles
        if role in IGNORED_A11Y_ROLES:
            continue
        name = (n.get("name") or {}).get("value", "") or ""
        props: dict[str, Any] = {}
        for p in n.get("properties", []):
            if "name" not in p or "value" not in p:
                continue
            props[p["name"]] = p["value"].get("value")
        standardized.append({
            "node_id": f"e{n.get('nodeId', '?')}",
            "backend_dom_node_id": n.get("backendDOMNodeId"),
            "role": role,
            "name": (name or "")[:120],
            "level": props.get("level") or None,
            "parent_id": f"e{n['parentId']}" if n.get("parentId") else None,
            "focusable": bool(props.get("focusable", False)),
            "disabled": bool(props.get("disabled", False)),
            "page_state": page_state,
        })
    return standardized


def _css_attr_value(value: str) -> str:
    return value.replace("\\", "\\\\").replace('"', '\\"')


def _stable_dom_selectors(dom: dict[str, Any]) -> list[tuple[str, str]]:
    tag = (dom.get("tag") or "").lower()
    attrs = dom.get("attrs") or {}
    selectors: list[tuple[str, str]] = []

    data_testid = attrs.get("data-testid")
    if data_testid:
        selectors.append(("data-testid", str(data_testid)))

    for attribute in ("data-test", "data-qa", "data-cy", "data-product-id"):
        value = attrs.get(attribute)
        if value:
            selectors.append(
                ("css", f'{tag}[{attribute}="{_css_attr_value(str(value))}"]')
            )

    href = attrs.get("href")
    if href and tag == "a" and not str(href).startswith(("javascript:", "#")):
        selectors.append(("css", f'a[href="{_css_attr_value(str(href))}"]'))

    elem_id = attrs.get("id")
    if elem_id and re.match(r"^[A-Za-z_][\w-]*$", str(elem_id)):
        selectors.append(("css", f"#{elem_id}"))

    for attribute in ("name", "placeholder", "aria-label", "title"):
        value = attrs.get(attribute)
        if value and tag:
            selectors.append(
                ("css", f'{tag}[{attribute}="{_css_attr_value(str(value))}"]')
            )

    return selectors


def _is_business_candidate(node: dict[str, Any]) -> bool:
    dom = node.get("dom") or {}
    return not (
        dom.get("third_party_frame")
        or dom.get("advertising_context")
    )


def _augment_a11y_nodes_with_dom(page, client, nodes: list[dict[str, Any]]) -> None:
    """Attach verified Playwright candidates to a11y nodes via backendDOMNodeId."""
    for node in nodes:
        backend_id = node.get("backend_dom_node_id")
        if not backend_id:
            continue
        object_id = None
        try:
            resolved = client.send("DOM.resolveNode", {"backendNodeId": int(backend_id)})
            object_id = resolved.get("object", {}).get("objectId")
            if not object_id:
                continue
            payload = client.send("Runtime.callFunctionOn", {
                "objectId": object_id,
                "returnByValue": True,
                "functionDeclaration": """
                function() {
                  const attrs = {};
                  for (const attr of this.attributes || []) {
                    if ([
                      "aria-label", "data-cy", "data-product-id", "data-qa", "data-test",
                      "data-testid", "href", "id", "name", "placeholder",
                      "role", "title", "type"
                    ].includes(attr.name)) {
                      attrs[attr.name] = attr.value;
                    }
                  }
                  const rect = this.getBoundingClientRect();
                  const style = window.getComputedStyle(this);
                  const advertisingContext = Boolean(this.closest(
                    "iframe[src*='ad' i], [data-ad], [data-ad-slot], " +
                    "[data-ad-unit], .adsbygoogle, [aria-label*='advertisement' i], " +
                    "[aria-label*='sponsored' i]"
                  ));
                  return {
                    tag: this.tagName ? this.tagName.toLowerCase() : "",
                    attrs,
                    connected: this.isConnected,
                    visible: rect.width > 0 && rect.height > 0 &&
                      style.visibility !== "hidden" && style.display !== "none" &&
                      style.opacity !== "0" && !this.hidden,
                    enabled: !this.disabled &&
                      this.getAttribute("aria-disabled") !== "true" &&
                      !this.closest("[inert]"),
                    textContent: this.textContent || "",
                    third_party_frame: window !== window.top && window.frameElement === null,
                    advertising_context: advertisingContext
                  };
                }
                """,
            }).get("result", {}).get("value") or {}
            if str((payload.get("attrs") or {}).get("type") or "").lower() == "password":
                payload["attrs"] = {}
            node["dom"] = payload
            # Use textContent instead of innerText to avoid CSS text-transform issues
            text_content = payload.get("textContent", "")
            if text_content and text_content != node.get("name"):
                node["original_name"] = node.get("name")
                node["name"] = text_content
            node["verified_selectors"] = []
            if not (
                payload.get("connected")
                and payload.get("visible")
                and payload.get("enabled")
            ):
                continue
            for strategy, selector in _stable_dom_selectors(payload):
                try:
                    locator = page.get_by_test_id(selector) if strategy == "data-testid" else page.locator(selector)
                    if locator.count() == 1:
                        node["verified_selectors"].append({
                            "strategy": strategy,
                            "selector": selector,
                            "name": node.get("name") or "",
                            "source": "a11y_backend_dom_node",
                        })
                except Exception:
                    continue
        except Exception:
            continue
        finally:
            if object_id:
                try:
                    client.send("Runtime.releaseObject", {"objectId": object_id})
                except Exception:
                    pass


def _dom_role(tag: str, attrs: dict[str, str]) -> str:
    explicit = str(attrs.get("role") or "").strip().lower()
    if explicit:
        return explicit
    input_type = str(attrs.get("type") or "text").strip().lower()
    if tag == "a":
        return "link"
    if tag == "button" or (tag == "input" and input_type in {"button", "submit", "reset"}):
        return "button"
    if tag == "select":
        return "combobox"
    if tag == "textarea" or (tag == "input" and input_type not in {"checkbox", "radio", "range", "number"}):
        return "searchbox" if input_type == "search" else "textbox"
    return {
        "checkbox": "checkbox",
        "radio": "radio",
        "range": "slider",
        "number": "spinbutton",
    }.get(input_type, "generic")


def _collect_dom_interactive_supplement(
    page,
    client,
    *,
    page_state: str,
    existing_nodes: list[dict[str, Any]],
) -> list[dict[str, Any]]:
    existing_backend_ids = {
        int(node["backend_dom_node_id"])
        for node in existing_nodes
        if node.get("backend_dom_node_id")
    }
    existing_selectors = {
        (str(selector.get("strategy") or ""), str(selector.get("selector") or ""))
        for node in existing_nodes
        for selector in node.get("verified_selectors", [])
        if isinstance(selector, dict)
    }
    search_id = None
    supplement: list[dict[str, Any]] = []
    try:
        client.send("DOM.getDocument", {"depth": -1, "pierce": False})
        search = client.send(
            "DOM.performSearch",
            {"query": _DOM_INTERACTIVE_SELECTOR, "includeUserAgentShadowDOM": False},
        )
        search_id = search.get("searchId")
        count = int(search.get("resultCount") or 0)
        if not search_id or count < 1:
            return []
        node_ids = client.send(
            "DOM.getSearchResults",
            {"searchId": search_id, "fromIndex": 0, "toIndex": count},
        ).get("nodeIds", [])
        for node_id in node_ids:
            object_id = None
            try:
                described = client.send(
                    "DOM.describeNode", {"nodeId": int(node_id), "depth": 0}
                ).get("node", {})
                backend_id = int(described.get("backendNodeId") or 0)
                if not backend_id or backend_id in existing_backend_ids:
                    continue
                resolved = client.send("DOM.resolveNode", {"nodeId": int(node_id)})
                object_id = resolved.get("object", {}).get("objectId")
                if not object_id:
                    continue
                payload = client.send(
                    "Runtime.callFunctionOn",
                    {
                        "objectId": object_id,
                        "returnByValue": True,
                        "functionDeclaration": """
                        function() {
                          const allowed = new Set([
                            "aria-label", "data-cy", "data-product-id", "data-qa", "data-test",
                            "data-testid", "href", "id", "name", "placeholder",
                            "role", "title", "type"
                          ]);
                          const attrs = {};
                          for (const attr of this.attributes || []) {
                            if (allowed.has(attr.name)) attrs[attr.name] = attr.value;
                          }
                          const rect = this.getBoundingClientRect();
                          const style = window.getComputedStyle(this);
                          const advertisingContext = Boolean(this.closest(
                            "iframe[src*='ad' i], [data-ad], [data-ad-slot], " +
                            "[data-ad-unit], .adsbygoogle, [aria-label*='advertisement' i], " +
                            "[aria-label*='sponsored' i]"
                          ));
                          return {
                            tag: this.tagName ? this.tagName.toLowerCase() : "",
                            attrs,
                            connected: this.isConnected,
                            visible: rect.width > 0 && rect.height > 0 &&
                              style.visibility !== "hidden" && style.display !== "none" &&
                              style.opacity !== "0" && !this.hidden,
                            enabled: !this.disabled &&
                              this.getAttribute("aria-disabled") !== "true" &&
                              !this.closest("[inert]"),
                            textContent: (this.textContent || "").trim().slice(0, 120),
                            advertising_context: advertisingContext,
                            third_party_frame: window !== window.top && window.frameElement === null
                          };
                        }
                        """,
                    },
                ).get("result", {}).get("value") or {}
                attrs = {
                    str(key): str(value)
                    for key, value in (payload.get("attrs") or {}).items()
                    if key in _DOM_ATTR_WHITELIST
                }
                if (
                    not payload.get("connected")
                    or not payload.get("visible")
                    or not payload.get("enabled")
                    or payload.get("advertising_context")
                    or payload.get("third_party_frame")
                    or attrs.get("type", "").lower() == "password"
                ):
                    continue
                payload["attrs"] = attrs
                verified: list[dict[str, Any]] = []
                for strategy, selector in _stable_dom_selectors(payload):
                    locator = (
                        page.get_by_test_id(selector)
                        if strategy == "data-testid"
                        else page.locator(selector)
                    )
                    if locator.count() != 1:
                        continue
                    key = (strategy, selector)
                    if key in existing_selectors:
                        verified = []
                        break
                    verified.append(
                        {
                            "strategy": strategy,
                            "selector": selector,
                            "name": "",
                            "source": "dom_verified_interactive_control",
                        }
                    )
                if not verified:
                    continue
                name = next(
                    (
                        attrs.get(attribute, "").strip()
                        for attribute in ("aria-label", "placeholder", "title", "name")
                        if attrs.get(attribute, "").strip()
                    ),
                    str(payload.get("textContent") or "").strip(),
                )[:120]
                for selector in verified:
                    selector["name"] = name
                    existing_selectors.add(
                        (str(selector["strategy"]), str(selector["selector"]))
                    )
                existing_backend_ids.add(backend_id)
                supplement.append(
                    {
                        "node_id": f"d{backend_id}",
                        "backend_dom_node_id": backend_id,
                        "role": _dom_role(str(payload.get("tag") or ""), attrs),
                        "name": name,
                        "level": None,
                        "parent_id": None,
                        "focusable": True,
                        "disabled": False,
                        "page_state": page_state,
                        "source": "dom_verified_interactive_control",
                        "dom": payload,
                        "verified_selectors": verified,
                    }
                )
            except Exception:
                continue
            finally:
                if object_id:
                    with suppress(Exception):
                        client.send("Runtime.releaseObject", {"objectId": object_id})
    except Exception:
        logger.warning("DOM interactive supplement collection failed", exc_info=True)
    finally:
        if search_id:
            with suppress(Exception):
                client.send("DOM.discardSearchResults", {"searchId": search_id})
    return supplement


def collect_a11y_nodes(
    page,
    *,
    page_state: str = "S0",
    viewport: dict | None = None,
    core_user_flow_text: str | None = None,
) -> list[dict]:
    if viewport is None:
        vs = getattr(page, "viewport_size", None) or {}
        viewport = {"width": int(vs.get("width", 1280)), "height": int(vs.get("height", 720))}
    if core_user_flow_text:
        keywords = _extract_flow_keywords(core_user_flow_text)
        _expand_collapsed_components(page, keywords)
    client = page.context.new_cdp_session(page)
    try:
        client.send("DOM.enable")
        client.send("Accessibility.enable")
        result = client.send("Accessibility.getFullAXTree", {})
        raw_nodes = result.get("nodes", [])
        filter_pass = _filter_a11y_nodes(raw_nodes, viewport=viewport)
        nodes = _cdp_to_a11y_nodes({"nodes": filter_pass}, page_state=page_state)
        _augment_a11y_nodes_with_dom(page, client, nodes)
        nodes = [node for node in nodes if _is_business_candidate(node)]
        nodes.extend(
            _collect_dom_interactive_supplement(
                page,
                client,
                page_state=page_state,
                existing_nodes=nodes,
            )
        )
        return nodes
    finally:
        try:
            client.send("Accessibility.disable")
        except Exception:
            pass
        try:
            client.send("DOM.disable")
        except Exception:
            pass
        try:
            client.detach()
        except Exception:
            pass



def _expand_collapsed_components(page, keywords: set[str], max_clicks: int = 10) -> list[str]:
    if not keywords:
        return []
    expanded: list[str] = []
    collapsed = page.locator('[aria-expanded="false"], details:not([open])')
    cnt = collapsed.count()
    for i in range(min(cnt, max_clicks * 2)):
        try:
            el = collapsed.nth(i)
            text = (el.evaluate(
                "el => (el.outerText || el.textContent || '').slice(0, 200)") or "").lower()
            matched = False
            for kw in keywords:
                if len(kw) >= 2 and kw in text:
                    matched = True
                    break
            if matched:
                el.click()
                page.wait_for_timeout(200)
                expanded.append(text[:40])
                if len(expanded) >= max_clicks:
                    break
        except Exception:
            continue
    return expanded


STALE_THRESHOLD_HOURS = 24


class BrowserSessionManager:
    """Per-session browser lifecycle manager.

    Maintains a single Playwright browser context per planning *session_id*
    so that all explore_page / explore_flow / capture_page_session calls
    within one planning session reuse the same browser.  This eliminates
    3-4 redundant browser cold-starts and keeps cookies/auth state alive
    across exploration steps.
    """

    _lock = threading.Lock()
    _sessions: dict[int, dict[str, Any]] = {}
    _runtime_pw: Any | None = None
    _runtime_playwright: Any | None = None
    _runtime_browser: Any | None = None
    _MAX_AGE_SECONDS: float = 600.0  # auto-close after 10 min of inactivity

    @classmethod
    def get_or_create_context(
        cls,
        session_id: int,
        *,
        storage_state_path: str | None = None,
    ):
        """Return ``(BrowserContext, Page)`` for *session_id*.

        If a browser for this session already exists and passes a health
        check it is returned immediately.  Otherwise a new headless
        Chromium instance is created.
        """
        cls._cleanup()
        with cls._lock:
            entry = cls._sessions.get(session_id)
            if entry is not None:
                try:
                    page = entry["page"]
                    page.evaluate("1")  # health check
                    # If page is stuck on about:blank after being used, reset it
                    if page.url == "about:blank" and entry.get("used", False):
                        logger.info("BrowserSessionManager: session %d stuck on about:blank, recreating", session_id)
                        cls._close_locked(session_id)
                    else:
                        entry["used"] = True
                        return entry["context"], page
                except Exception as e:
                    logger.warning("BrowserSessionManager: session %d health check failed: %s", session_id, e)
                    cls._close_locked(session_id)

            if cls._runtime_browser is None:
                pw = sync_playwright()
                entered = False
                try:
                    playwright = pw.__enter__()
                    entered = True
                    browser = playwright.chromium.launch(headless=True)
                except Exception:
                    if entered:
                        with suppress(Exception):
                            pw.__exit__(None, None, None)
                    raise
                cls._runtime_pw = pw
                cls._runtime_playwright = playwright
                cls._runtime_browser = browser

            browser = cls._runtime_browser
            context = None
            try:
                context_kwargs: dict = {}
                if storage_state_path and Path(storage_state_path).exists():
                    context_kwargs["storage_state"] = storage_state_path
                context = browser.new_context(**context_kwargs)
                page = context.new_page()
            except Exception:
                if context is not None:
                    with suppress(Exception):
                        context.close()
                raise

            cls._sessions[session_id] = {
                "pw": cls._runtime_pw,
                "playwright": cls._runtime_playwright,
                "browser": browser,
                "context": context,
                "page": page,
                "created_at": _time_module.monotonic(),
                "used": False,
            }
            logger.info("BrowserSessionManager: created new session %d", session_id)
            return context, page

    @classmethod
    def close_session(cls, session_id: int) -> None:
        """Explicitly close and remove a session's browser."""
        with cls._lock:
            cls._close_locked(session_id)

    @classmethod
    def close_all(cls) -> None:
        """Close every managed browser on the owning Playwright thread."""
        with cls._lock:
            for session_id in list(cls._sessions):
                cls._close_locked(session_id)
            browser = cls._runtime_browser
            pw = cls._runtime_pw
            cls._runtime_browser = None
            cls._runtime_playwright = None
            cls._runtime_pw = None
            if browser is not None:
                with suppress(Exception):
                    browser.close()
            if pw is not None:
                with suppress(Exception):
                    pw.__exit__(None, None, None)

    @classmethod
    def _close_locked(cls, session_id: int) -> None:
        entry = cls._sessions.pop(session_id, None)
        if entry is None:
            return
        with suppress(Exception):
            entry["context"].close()

    @classmethod
    def _cleanup(cls) -> None:
        now = _time_module.monotonic()
        stale = [
            sid
            for sid, e in cls._sessions.items()
            if now - e["created_at"] > cls._MAX_AGE_SECONDS
        ]
        for sid in stale:
            cls._close_locked(sid)


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


def _resolve_from_collected_nodes(page, target: str, prev_nodes: list[dict] | None):
    """Try to resolve *target* using verified_selectors or DOM attrs from *prev_nodes*.

    When the previous page state's a11y nodes contain a matching element with
    precise CSS selectors, use them directly — bypassing the a11y text-matching
    locator that can match the wrong element (e.g. ``click "Polo"`` matching
    a product paragraph instead of the brand link).

    Returns a Playwright locator or None.
    """
    if not prev_nodes:
        return None

    cleaned_target = target.strip().strip('"').strip("'")

    # Find nodes whose name contains the target text
    candidates = []
    for n in prev_nodes:
        name = str(n.get("name", "")).strip()
        if not name:
            continue
        # Match: exact, contains, or target is substring of name
        if cleaned_target.lower() == name.lower() or cleaned_target.lower() in name.lower():
            candidates.append(n)

    if not candidates:
        # Try fuzzy: target words appear in name
        target_words = cleaned_target.lower().split()
        for n in prev_nodes:
            name = str(n.get("name", "")).strip().lower()
            if not name:
                continue
            if all(w in name for w in target_words):
                candidates.append(n)

    if not candidates:
        logger.info(
            "_resolve_from_collected_nodes: target=%r, no candidates in %d prev_nodes",
            target, len(prev_nodes),
        )
        return None

    logger.info(
        "_resolve_from_collected_nodes: target=%r, found %d candidates: %s",
        target, len(candidates),
        [(c.get("role"), str(c.get("name", ""))[:50]) for c in candidates[:8]],
    )

    # Prefer candidates with verified_selectors or DOM attrs
    for n in candidates:
        # Try verified_selectors first (already tested as unique)
        verified = n.get("verified_selectors") or []
        if isinstance(verified, list):
            for vs in verified:
                if isinstance(vs, dict) and vs.get("selector"):
                    sel = vs["selector"]
                    try:
                        loc = page.locator(sel)
                        if loc.count() == 1:
                            logger.info(
                                "_resolve_from_collected_nodes: target=%r → verified selector %s (source=%s)",
                                target, sel, vs.get("source", "?"),
                            )
                            return loc.first
                    except Exception:
                        continue

        # Try DOM attrs (tag + key attributes)
        dom = n.get("dom") or {}
        attrs = dom.get("attrs") or {} if isinstance(dom, dict) else {}
        tag = dom.get("tag") if isinstance(dom, dict) else None

        if tag and attrs:
            # Build candidate selectors from attributes
            selectors_to_try = []
            for attr in ("data-product-id", "href", "id", "name"):
                val = attrs.get(attr)
                if val and isinstance(val, str):
                    selectors_to_try.append(f'{tag}[{attr}="{val}"]')

            for css in selectors_to_try:
                try:
                    loc = page.locator(css)
                    if loc.count() == 1:
                        logger.info(
                            "_resolve_from_collected_nodes: target=%r → DOM selector %s",
                            target, css,
                        )
                        return loc.first
                except Exception:
                    continue

    return None


def _resolve_step_locator(page, target: str, *, kind: str, skip_vlm: bool = False):
    """Resolve a step target to a Playwright locator using the semantic chain.

    When *skip_vlm* is True, only semantic resolution is attempted (no VLM
    fallback).  This is used during flow exploration to avoid slow VLM calls.
    Returns a strict (unique) locator or None if not found.
    """
    from app.locators import LocatorResolutionError, resolve_with_fallback
    from app.locators.semantic import resolve_semantic_locator

    if skip_vlm:
        try:
            resolved = resolve_semantic_locator(
                page, target,
                prefer_input=(kind == "input"),
                require_visible=True,
                require_enabled=(kind == "input"),
            )
            return resolved.locator
        except LocatorResolutionError:
            pass
        except Exception:
            pass
        return None

    try:
        resolved = resolve_with_fallback(
            page, target,
            prefer_input=(kind == "input"),
            require_visible=True,
            require_enabled=(kind == "input"),
        )
        return resolved.locator
    except LocatorResolutionError:
        pass
    except Exception:
        pass

    return None


def _resolve_flow_action_locator(
    page,
    target: str,
    *,
    kind: str,
    previous_nodes: list[dict[str, Any]] | None,
):
    if target.startswith("text="):
        text = target.removeprefix("text=").strip()
        locator = page.get_by_text(text, exact=True)
        return locator.first if locator.count() > 0 else None
    if target.startswith("css="):
        locator = page.locator(target.removeprefix("css="))
        return locator.first if locator.count() > 0 else None
    if target.startswith(("#", ".")):
        locator = page.locator(target)
        return locator.first if locator.count() > 0 else None
    locator = _resolve_from_collected_nodes(page, target, previous_nodes)
    if locator is not None:
        return locator
    return _resolve_step_locator(page, target, kind=kind, skip_vlm=True)


def _flow_action_timeout(action: dict[str, Any]) -> int:
    raw_timeout = action.get("timeout_ms", action.get("value", 5000))
    try:
        timeout = int(raw_timeout)
    except (TypeError, ValueError):
        timeout = 5000
    return max(1, min(timeout, 60000))


def _wait_for_flow_target(page, target: str, timeout_ms: int) -> None:
    if target.startswith("text="):
        text = target.removeprefix("text=").strip()
        page.get_by_text(text, exact=True).first.wait_for(
            state="visible",
            timeout=timeout_ms,
        )
        return
    if target.startswith(("css=", "#", ".")):
        selector = target.removeprefix("css=")
        page.locator(selector).first.wait_for(
            state="visible",
            timeout=timeout_ms,
        )
        return
    page.get_by_text(target, exact=True).first.wait_for(
        state="visible",
        timeout=timeout_ms,
    )


def capture_browser_session(
    url: str,
    steps: list[dict[str, Any]],
    *,
    storage_dir: Path,
    project_id: int,
    timeout_ms: int = 60000,
    session_id: int = 0,
) -> dict[str, Any]:
    """Execute *steps* on *url*, then persist the browser session state.

    When *session_id* > 0 the browser is obtained from
    :class:`BrowserSessionManager` and reused across calls.
    """
    import time as _time
    try:
        if session_id:
            context, page = BrowserSessionManager.get_or_create_context(session_id)
        else:
            pw = sync_playwright()
            with pw as playwright:
                browser = playwright.chromium.launch(headless=True)
                context = browser.new_context()
                page = context.new_page()

        page.goto(url, timeout=timeout_ms, wait_until="domcontentloaded")
        try:
            page.wait_for_load_state("networkidle", timeout=timeout_ms)
        except Exception:
            pass
        _time.sleep(1.0)

        for step in steps:
            action = (step.get("action") or "").strip().lower()
            target = step.get("target", "")
            value = step.get("value", "")
            if action in ("type", "fill", "input"):
                kind = "input"
            elif action in ("click", "press", "tap"):
                kind = "click"
            else:
                kind = action

            for _retry in range(2):
                locator = _resolve_step_locator(page, target, kind=kind)
                if locator is None:
                    _time.sleep(1.0)
                    continue
                try:
                    tag = locator.evaluate("el => el.tagName.toLowerCase()")
                    if kind == "input" and tag not in ("input", "select", "textarea"):
                        logger.warning("Locator resolved to <%s> instead of input for target=%r, retrying", tag, target)
                        _time.sleep(1.0)
                        continue
                    if kind == "click" and tag in ("body", "html"):
                        _time.sleep(1.0)
                        continue
                    if kind == "input":
                        locator.fill(str(value))
                    elif kind == "click":
                        from app.runners.click_preprocessor import click_with_precheck
                        cr = click_with_precheck(page, locator)
                        if not cr.succeeded:
                            logger.warning(
                                "capture_browser_session click failed: target=%r, strategy=%s",
                                target, cr.recovery_strategy,
                            )
                    break
                except Exception as e:
                    logger.warning("Step action failed for target=%r: %s, retrying", target, e)
                    _time.sleep(1.0)
            else:
                logger.warning("Step failed after retries for target=%r", target)

        state = context.storage_state()
        cookie_count = len(state.get("cookies", []))
        save_storage_state(storage_dir, project_id=project_id, state=dict(state), source_url=url)

        if not session_id:
            context.close()
            browser.close()
    except Exception as exc:
        logger.warning("capture_browser_session failed for url=%s: %s", url, exc)
        return {"success": False, "error": str(exc)}

    return {
        "success": True,
        "message": f"已保存 {url} 的会话状态（包含 {cookie_count} 个 cookie）",
    }


# ── A11y-based flow collection (replaces collect_multi_page_elements + collect_flow_elements)


def _normalize_flow_step(step: dict[str, Any]) -> dict[str, Any]:
    """Normalize DSL format steps to explore_flow format.

    DSL format: {"action": "goto", "target": "https://..."} or {"action": "click", "target": "..."}
    Explore format: {"url": "https://...", "actions": [{"action": "click", "target": "..."}]}
    """
    if not isinstance(step, dict):
        return step

    # Already in explore format (has url or actions)
    if "url" in step or "actions" in step:
        return step

    # DSL format: has action + target
    action = (step.get("action") or "").strip().lower()
    target = step.get("target", "")

    if not action:
        return step

    # goto -> url
    if action == "goto" and target:
        return {"url": target, "description": step.get("description", "")}

    # click/input/wait_for -> actions
    if action in ("click", "input", "wait_for") and target:
        return {"actions": [step], "description": step.get("description", "")}

    return step


def _collect_flow_a11y(
    flow_steps: list[dict[str, Any]],
    *,
    base_url: str | None = None,
    storage_state_path: str | None = None,
    session_id: int = 0,
    timeout_ms: int = 60000,
    core_user_flow_text: str | None = None,
) -> list[dict[str, Any]]:
    """Execute flow steps using A11y extraction instead of DOM.

    Supports both formats:
    - Explore format: {"url": "...", "actions": [...]}
    - DSL format: {"action": "goto/click/input/...", "target": "..."}
    """
    if not flow_steps:
        return []

    # Normalize all steps to explore format
    flow_steps = [_normalize_flow_step(s) for s in flow_steps]

    managed = not bool(session_id)
    pw = None
    browser = None
    context = None
    managed_cleanup_done = False

    def cleanup_managed_browser() -> None:
        nonlocal managed_cleanup_done
        if not managed or managed_cleanup_done:
            return
        managed_cleanup_done = True
        try:
            if context is not None:
                context.close()
        except Exception:
            logger.warning(
                "_collect_flow_a11y: failed to close managed browser context",
                exc_info=True,
            )
        finally:
            try:
                if browser is not None:
                    browser.close()
            except Exception:
                logger.warning(
                    "_collect_flow_a11y: failed to close managed browser",
                    exc_info=True,
                )
            finally:
                try:
                    if pw is not None:
                        pw.__exit__(None, None, None)
                except Exception:
                    logger.warning(
                        "_collect_flow_a11y: failed to exit managed Playwright",
                        exc_info=True,
                    )

    if session_id:
        _, page = BrowserSessionManager.get_or_create_context(
            session_id, storage_state_path=storage_state_path,
        )
    else:
        pw = sync_playwright()
        startup_complete = False
        try:
            playwright = pw.__enter__()
            browser = playwright.chromium.launch(headless=True)
            context_kwargs: dict[str, Any] = {}
            if storage_state_path and Path(storage_state_path).exists():
                context_kwargs["storage_state"] = storage_state_path
            context = browser.new_context(**context_kwargs)
            page = context.new_page()
            startup_complete = True
        finally:
            if not startup_complete:
                cleanup_managed_browser()

    results: list[dict[str, Any]] = []
    state_index = 0
    revision = 0
    url_to_state: dict[str, str] = {}
    # Tracks the most recently collected a11y nodes for DOM-level click resolution
    prev_action_nodes: list[dict[str, Any]] | None = None

    def state_for(url: str, description: str) -> str:
        nonlocal state_index
        key = f"{urldefrag(url)[0]}|{description}"
        if key not in url_to_state:
            url_to_state[key] = f"S{state_index}"
            state_index += 1
        return url_to_state[key]

    def collect_action_snapshot(
        *,
        step_index: int,
        action_index: int,
        action_description: str,
        target: str,
        description: str,
        phase: str,
    ) -> tuple[dict[str, Any], list[dict[str, Any]]]:
        nonlocal revision
        snapshot_url = page.url
        snapshot_state = state_for(snapshot_url, description)
        nodes = collect_a11y_nodes(
            page,
            page_state=snapshot_state,
            core_user_flow_text=core_user_flow_text,
        )
        revision += 1
        normalized_target = re.sub(r"\s+", " ", target.strip().casefold())
        target_evidence = []
        for node in nodes:
            selector_match = any(
                str(selector.get("selector") or "").strip() == target.removeprefix("css=")
                for selector in node.get("verified_selectors", [])
                if isinstance(selector, dict)
            )
            normalized_name = re.sub(
                r"\s+", " ", str(node.get("name") or "").strip().casefold()
            )
            name_match = bool(
                normalized_target
                and normalized_name
                and (
                    normalized_name == normalized_target
                    or normalized_target in normalized_name
                )
            )
            if not selector_match and not name_match:
                continue
            dom = node.get("dom") or {}
            target_evidence.append(
                {
                    "node_id": node.get("node_id"),
                    "backend_dom_node_id": node.get("backend_dom_node_id"),
                    "role": node.get("role"),
                    "name": node.get("name"),
                    "page_state": node.get("page_state"),
                    "source": node.get("source"),
                    "dom": {
                        "tag": dom.get("tag"),
                        "attrs": dom.get("attrs") or {},
                    },
                    "verified_selectors": node.get("verified_selectors") or [],
                }
            )
            if len(target_evidence) >= 10:
                break
        entry = {
            "url": snapshot_url,
            "page_state": snapshot_state,
            "description": description,
            "actions": [
                {
                    "step_index": step_index,
                    "action_index": action_index,
                    "action": action_description.split(" ", 1)[0],
                    "action_description": action_description,
                    "target": target,
                    "phase": phase,
                    "status": "success",
                    "url": snapshot_url,
                    "page_state": snapshot_state,
                    "target_evidence": target_evidence,
                    "evidence_count": len(target_evidence),
                }
            ],
            "a11y_nodes": nodes,
            "element_count": len(nodes),
            "status": "success",
            "revision": revision,
        }
        results.append(entry)
        return entry, nodes

    try:
        for step_i, step in enumerate(flow_steps):
            logger.info("_collect_flow_a11y: step %d, step=%s", step_i, step)
            if not isinstance(step, dict):
                continue

            step_url = step.get("url")
            if step_url and isinstance(step_url, str) and step_url.strip():
                url_str = step_url.strip()
                if not url_str.startswith(("http://", "https://")):
                    if not base_url:
                        revision += 1
                        results.append({
                            "url": url_str, "page_state": "SKIPPED",
                            "a11y_nodes": [], "element_count": 0,
                            "status": "error",
                            "revision": revision,
                            "failure": {
                                "code": "relative_url_without_base",
                                "step_index": step_i,
                                "message": "relative URL requires base_url",
                            },
                            "description": step.get("description", ""),
                        })
                        break
                    url_str = urljoin(base_url, url_str.lstrip("/"))
                if _same_document_url(page.url, url_str):
                    logger.info(
                        "_collect_flow_a11y: preserving current document for %s",
                        url_str,
                    )
                    url_str = page.url
                else:
                    try:
                        logger.info("_collect_flow_a11y: navigating to %s", url_str)
                        page.goto(url_str, timeout=timeout_ms, wait_until="domcontentloaded")
                        try:
                            page.wait_for_load_state("networkidle", timeout=timeout_ms)
                        except Exception:
                            pass
                        # Check if page actually loaded (not stuck on about:blank)
                        if page.url == "about:blank":
                            logger.warning("_collect_flow_a11y: page stuck on about:blank after goto %s", url_str)
                            revision += 1
                            results.append({
                                "url": url_str, "page_state": "ERROR",
                                "a11y_nodes": [], "element_count": 0,
                                "status": "error",
                                "revision": revision,
                                "failure": {
                                    "code": "navigation_failed",
                                    "step_index": step_i,
                                    "message": "page remained on about:blank",
                                },
                                "error": "页面未能加载（停留在 about:blank），可能是网站无法访问或被反爬虫机制阻止",
                            })
                            break
                    except Exception as exc:
                        logger.warning("_collect_flow_a11y: goto failed for %s: %s", url_str, exc)
                        revision += 1
                        results.append({
                            "url": url_str, "page_state": "ERROR",
                            "a11y_nodes": [], "element_count": 0,
                            "status": "error",
                            "revision": revision,
                            "failure": {
                                "code": "navigation_failed",
                                "step_index": step_i,
                                "message": str(exc),
                            },
                            "error": str(exc),
                        })
                        break

            actions = step.get("actions")
            if isinstance(actions, list) and actions:
                logger.info("_collect_flow_a11y: executing %d actions", len(actions))
                description = step.get("description", "")
                action_failed = False
                for action_idx, action_def in enumerate(actions):
                    if not isinstance(action_def, dict):
                        continue
                    act = str(action_def.get("action") or "").strip().lower()
                    target = str(action_def.get("target") or "").strip()
                    value = action_def.get("value", "")
                    if not act:
                        continue

                    action_desc = f"{act} {target}" if target else act
                    before_entry, before_nodes = collect_action_snapshot(
                        step_index=step_i,
                        action_index=action_idx,
                        action_description=action_desc,
                        target=target,
                        description=description,
                        phase="before",
                    )
                    prev_action_nodes = before_nodes
                    url_before_action = page.url
                    expected_navigation_url = None
                    try:
                        if act == "input":
                            loc = _resolve_flow_action_locator(
                                page,
                                target,
                                kind="input",
                                previous_nodes=prev_action_nodes,
                            )
                            if loc is None:
                                raise RuntimeError(f"input target not found: {target}")
                            try:
                                tag = loc.evaluate("el => el.tagName.toLowerCase()")
                            except Exception:
                                tag = ""
                            if tag not in ("input", "select", "textarea"):
                                loc = _resolve_input_fallback(page, target)
                            if loc is None:
                                raise RuntimeError(
                                    f"input target is not editable: {target}"
                                )
                            loc.fill(str(value))
                        elif act == "click":
                            logger.info(
                                "_collect_flow_a11y: click target=%r, prev_nodes=%d",
                                target, len(prev_action_nodes) if prev_action_nodes else 0,
                            )
                            loc = _resolve_flow_action_locator(
                                page,
                                target,
                                kind="click",
                                previous_nodes=prev_action_nodes,
                            )
                            if loc is None:
                                raise RuntimeError(f"click target not found: {target}")
                            expected_navigation_url = _cross_page_anchor_href(
                                loc, url_before_action
                            )
                            from app.runners.click_preprocessor import (
                                click_with_precheck,
                            )
                            click_result = click_with_precheck(page, loc)
                            if not click_result.succeeded:
                                raise RuntimeError(
                                    "click failed"
                                    + (
                                        f": {click_result.recovery_detail}"
                                        if click_result.recovery_detail
                                        else ""
                                    )
                                )
                        elif act == "wait_for":
                            _wait_for_flow_target(
                                page,
                                target,
                                _flow_action_timeout(action_def),
                            )
                        else:
                            raise RuntimeError(f"unsupported flow action: {act}")
                        if page.url != url_before_action:
                            try:
                                page.wait_for_load_state(
                                    "networkidle", timeout=timeout_ms
                                )
                            except Exception:
                                pass
                        if expected_navigation_url:
                            arrived = _wait_for_expected_navigation(
                                page,
                                expected_navigation_url,
                                _flow_action_timeout(action_def),
                            )
                        else:
                            page.wait_for_timeout(500)
                            arrived = True
                        if not arrived:
                            raise RuntimeError(
                                "click did not reach expected anchor destination: "
                                f"expected={expected_navigation_url}, actual={page.url}"
                            )
                    except Exception as exc:
                        failure = {
                            "code": "flow_action_failed",
                            "step_index": step_i,
                            "action_index": action_idx,
                            "action": act,
                            "target": target,
                            "message": str(exc),
                        }
                        revision += 1
                        results.append(
                            {
                                "url": page.url,
                                "page_state": state_for(page.url, description),
                                "description": description,
                                "actions": [
                                    {
                                        "step_index": step_i,
                                        "action_index": action_idx,
                                        "action": act,
                                        "action_description": action_desc,
                                        "target": target,
                                        "phase": "after",
                                        "status": "error",
                                        "failure": failure,
                                        "url": page.url,
                                        "page_state": state_for(page.url, description),
                                        "target_evidence": [],
                                        "evidence_count": 0,
                                    }
                                ],
                                "a11y_nodes": [],
                                "element_count": 0,
                                "status": "error",
                                "revision": revision,
                                "failure": failure,
                            }
                        )
                        logger.warning(
                            "_collect_flow_a11y: action failed: %s",
                            failure,
                        )
                        action_failed = True
                        break

                    after_entry, after_nodes = collect_action_snapshot(
                        step_index=step_i,
                        action_index=action_idx,
                        action_description=action_desc,
                        target=target,
                        description=description,
                        phase="after",
                    )
                    prev_action_nodes = after_nodes

                    logger.info(
                        "_collect_flow_a11y: step %d action %d (%s) completed, "
                        "before=%s/%s after=%s/%s nodes=%d",
                        step_i,
                        action_idx,
                        action_desc,
                        before_entry["url"],
                        before_entry["page_state"],
                        after_entry["url"],
                        after_entry["page_state"],
                        len(after_nodes),
                    )
                if action_failed:
                    break
            else:
                # No actions, just collect nodes for this step
                current_url = page.url
                description = step.get("description", "")
                state_id = state_for(current_url, description)

                nodes = collect_a11y_nodes(page, page_state=state_id, core_user_flow_text=core_user_flow_text)
                prev_action_nodes = nodes
                result = {
                    "url": current_url, "page_state": state_id,
                    "a11y_nodes": nodes, "element_count": len(nodes),
                    "description": description,
                    "actions": [],
                    "status": "success",
                }
                revision += 1
                result["revision"] = revision
                results.append(result)
                logger.info("_collect_flow_a11y: step %d completed, state=%s, nodes=%d", step_i, state_id, len(nodes))
    except Exception as exc:
        logger.error("_collect_flow_a11y failed: %s", exc, exc_info=True)
        revision += 1
        results.append(
            {
                "url": getattr(page, "url", ""),
                "page_state": "ERROR",
                "a11y_nodes": [],
                "element_count": 0,
                "status": "error",
                "revision": revision,
                "failure": {
                    "code": "flow_failed",
                    "message": str(exc),
                },
            }
        )
    finally:
        cleanup_managed_browser()
    logger.info("_collect_flow_a11y completed: %d results", len(results))

    # Deduplicate results: keep unique pages with their actions
    deduplicated = _deduplicate_explore_results(results)
    logger.info("_collect_flow_a11y after dedup: %d pages", len(deduplicated))
    return deduplicated


def _same_document_url(left: str, right: str) -> bool:
    return urldefrag(left)[0] == urldefrag(right)[0]


def _wait_for_expected_navigation(
    page,
    expected_url: str,
    timeout_ms: int,
) -> bool:
    deadline = _time_module.monotonic() + timeout_ms / 1000
    while True:
        if _same_document_url(page.url, expected_url):
            return True
        if _time_module.monotonic() >= deadline:
            return False
        page.wait_for_timeout(50)


def _cross_page_anchor_href(locator, current_url: str) -> str | None:
    try:
        attributes = locator.evaluate(
            "el => ({tag: el.tagName.toLowerCase(), href: el.getAttribute('href') || '', "
            "download: el.hasAttribute('download')})"
        )
    except Exception:
        return None
    if (
        not isinstance(attributes, dict)
        or attributes.get("tag") != "a"
        or attributes.get("download")
    ):
        return None
    href = str(attributes.get("href") or "").strip()
    if not href or href.startswith("#"):
        return None
    resolved = urljoin(current_url, href)
    if urlparse(resolved).scheme not in {"http", "https"}:
        return None
    if _same_document_url(resolved, current_url):
        return None
    return resolved


def _deduplicate_explore_results(results: list[dict[str, Any]]) -> list[dict[str, Any]]:
    """Deduplicate explore results while preserving structure.

    Strategy:
    1. Group by URL (normalized)
    2. For each URL, keep the latest successful revision
    3. Within each page, keep the latest successful action revision
    4. Within each action, deduplicate nodes by (role, name) to reduce size
    """
    # Group by URL
    pages_by_url: dict[str, dict] = {}
    failures: list[dict[str, Any]] = []

    def merge_actions(
        older: list[dict[str, Any]],
        newer: list[dict[str, Any]],
    ) -> list[dict[str, Any]]:
        merged: dict[tuple[Any, ...], dict[str, Any]] = {}
        for action in [*older, *newer]:
            key = (
                action.get("step_index"),
                action.get("action_index"),
                action.get("action_description", ""),
                action.get("phase", ""),
            )
            merged[key] = action
        return list(merged.values())

    for page in results:
        if page.get("status") == "error":
            failures.append(page)
            continue
        url = urldefrag((page.get("url") or "").strip())[0]
        if not url:
            continue
        existing = pages_by_url.get(url)
        if existing is None:
            pages_by_url[url] = page
        else:
            existing_revision = int(existing.get("revision", 0))
            page_revision = int(page.get("revision", 0))
            if page_revision >= existing_revision:
                page["actions"] = merge_actions(
                    existing.get("actions", []),
                    page.get("actions", []),
                )
                pages_by_url[url] = page
            else:
                existing["actions"] = merge_actions(
                    page.get("actions", []),
                    existing.get("actions", []),
                )

    # Deduplicate actions within each page
    deduplicated = []
    for url, page in pages_by_url.items():
        top_level_nodes = page.get("a11y_nodes", [])
        if top_level_nodes:
            page["a11y_nodes"] = _deduplicate_nodes(top_level_nodes)
            page["element_count"] = len(page["a11y_nodes"])
        actions = page.get("actions", [])
        if not actions:
            deduplicated.append(page)
            continue

        # Deduplicate actions by action_description
        seen_actions: dict[tuple[Any, ...], dict] = {}
        for action in actions:
            action_key = (
                action.get("step_index"),
                action.get("action_index"),
                action.get("action_description", ""),
                action.get("phase", ""),
            )
            if action_key not in seen_actions:
                seen_actions[action_key] = action
            else:
                existing_success = (
                    seen_actions[action_key].get("status", "success") == "success"
                )
                action_success = action.get("status", "success") == "success"
                if action_success or not existing_success:
                    seen_actions[action_key] = action

        # Deduplicate nodes within each action
        for action in seen_actions.values():
            evidence = action.get("target_evidence", [])
            if evidence:
                action["target_evidence"] = _deduplicate_nodes(evidence)
                action["evidence_count"] = len(action["target_evidence"])

        page["actions"] = list(seen_actions.values())
        deduplicated.append(page)

    return deduplicated + failures


def _deduplicate_nodes(nodes: list[dict[str, Any]]) -> list[dict[str, Any]]:
    """Deduplicate a11y nodes by (role, name) while preserving parent-child structure.

    Keeps:
    - First occurrence of each (role, name) combination
    - All nodes that are parents (have children)
    - All nodes with verified_selectors
    """
    seen: dict[tuple[str, str], bool] = {}
    result: list[dict[str, Any]] = []

    # Build parent-child relationships
    child_ids: set[str] = set()
    for node in nodes:
        parent_id = node.get("parent_id")
        if parent_id:
            child_ids.add(parent_id)

    for node in nodes:
        role = node.get("role", "unknown")
        name = node.get("name", "")
        key = (role, name)

        # Always keep nodes that are parents (have children)
        node_id = node.get("node_id", "")
        if node_id in child_ids:
            result.append(node)
            continue

        # Always keep nodes with verified_selectors
        verified = node.get("verified_selectors", [])
        if verified:
            result.append(node)
            continue

        # Deduplicate by (role, name)
        if key not in seen:
            seen[key] = True
            result.append(node)

    return result



def _resolve_input_fallback(page, target: str):
    """Try alternative strategies when semantic locator resolves to non-input element."""
    fallback_strategies = [
        lambda: page.get_by_placeholder(target),
        lambda: page.get_by_role("textbox", name=target),
        lambda: page.locator("input, select, textarea").filter(has=page.get_by_text(target)),
        lambda: page.locator(f"input[type='email']").first,
        lambda: page.locator("input:visible").first,
    ]
    for strategy in fallback_strategies:
        try:
            loc = strategy()
            if loc.count() > 0:
                tag = loc.first.evaluate("el => el.tagName.toLowerCase()")
                if tag in ("input", "select", "textarea"):
                    return loc.first
        except Exception:
            continue
    return None
