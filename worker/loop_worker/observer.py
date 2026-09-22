"""观测采集 + 可访问名（accessible name）的唯一实现。

CONTRACT §3 / §3.1 / §6：

- `elements` 只保留可交互或有文本的元素，上限 200 条（超出时优先保留可见且 enabled 的）；
- `name` 一律取**浏览器计算的可访问名**，不允许用 DOM 文本拼接代替；
- `locators` 必须在观测时**就地验证**（实现在 `locators.py`），一条都验证不出来的元素不进 `elements`。

`accessible_name()` 是本仓库 Python 侧可访问名的**唯一实现**。它用浏览器自己的 a11y 快照
（`locator.aria_snapshot()`）取得候选名，再用 `locators.verify_role_locator()`（即
`page.get_by_role(role, name=..., exact=True)` 的唯一命中 + 元素同一性）当场验证。执行期
`locators.to_playwright_locator()` 走完全相同的查询路径，因此"观测时能解析 ⇒ 执行时能命中"。
"""

from __future__ import annotations

import re
import secrets
from typing import Any

from playwright.async_api import Page

from .contracts import ElementObservation, Observation
from .locators import (
    PROBE_TIMEOUT_MS,
    REF_ATTRIBUTE,
    build_locators,
    verify_role_locator,
)

#: CONTRACT §3：elements 上限
MAX_ELEMENTS = 200

#: 文本候选的长度上限（超长文本不做 text 定位器候选，避免病态查询）
MAX_TEXT_CANDIDATE_LEN = 300

#: 采集脚本：把"可交互或有文本"的候选元素连同可见/可用状态一起取回，并临时打上 ref 属性。
#: 注意 `page.evaluate` 只接受**一个**参数，因此这里用对象入参（不能写成两个形参）。
_COLLECT_JS = """
(args) => {
  const refAttr = args.refAttr;
  const maxElements = args.maxElements;
  const TEXT_TAGS = new Set(['SCRIPT','STYLE','NOSCRIPT','TEMPLATE','META','LINK','HEAD','TITLE','BR','HTML']);
  const INTERACTIVE_TAGS = new Set(['A','BUTTON','INPUT','SELECT','TEXTAREA','LABEL','SUMMARY','OPTION','DETAILS']);
  const normalize = (s) => (s || '').replace(/\\s+/g, ' ').trim();

  const isVisible = (el) => {
    const style = window.getComputedStyle(el);
    if (style.display === 'none' || style.visibility === 'hidden' || style.visibility === 'collapse') return false;
    if (parseFloat(style.opacity) === 0) return false;
    if (el.hidden) return false;
    const rects = el.getClientRects();
    if (!rects || rects.length === 0) return false;
    const rect = el.getBoundingClientRect();
    return rect.width > 0 && rect.height > 0;
  };

  const cssEscape = (value) => (window.CSS && CSS.escape) ? CSS.escape(value) : String(value).replace(/([^a-zA-Z0-9_-])/g, '\\\\$1');
  const unique = (selector) => {
    try { return document.querySelectorAll(selector).length === 1; } catch (e) { return false; }
  };
  const cssPath = (el) => {
    if (el.id && unique('#' + cssEscape(el.id))) return '#' + cssEscape(el.id);
    const parts = [];
    let cur = el;
    let guard = 0;
    while (cur && cur.nodeType === 1 && cur.tagName !== 'HTML' && guard++ < 64) {
      if (cur.id && unique('#' + cssEscape(cur.id))) {
        parts.unshift('#' + cssEscape(cur.id));
        break;
      }
      let sel = cur.tagName.toLowerCase();
      const parent = cur.parentElement;
      if (parent) {
        const sameTag = Array.prototype.filter.call(parent.children, (c) => c.tagName === cur.tagName);
        if (sameTag.length > 1) sel += ':nth-of-type(' + (sameTag.indexOf(cur) + 1) + ')';
      }
      parts.unshift(sel);
      cur = cur.parentElement;
    }
    const path = parts.join(' > ');
    return unique(path) ? path : null;
  };

  const roleOf = (el) => {
    const explicit = el.getAttribute('role');
    if (explicit) return normalize(explicit).split(' ')[0].toLowerCase() || null;
    const tag = el.tagName;
    if (tag === 'INPUT') {
      const type = (el.getAttribute('type') || 'text').toLowerCase();
      if (type === 'hidden') return null;
      return ({
        button: 'button', checkbox: 'checkbox', email: 'textbox', image: 'button',
        number: 'spinbutton', password: 'textbox', radio: 'radio', range: 'slider',
        reset: 'button', search: 'searchbox', submit: 'button', tel: 'textbox',
        text: 'textbox', url: 'textbox'
      })[type] || null;
    }
    if (tag === 'A') return el.hasAttribute('href') ? 'link' : 'generic';
    if (tag === 'IMG') return el.getAttribute('alt') ? 'img' : 'presentation';
    if (tag === 'SELECT') return el.multiple ? 'listbox' : 'combobox';
    if (/^H[1-6]$/.test(tag)) return 'heading';
    return ({
      ARTICLE: 'article', ASIDE: 'complementary', BUTTON: 'button', DIALOG: 'dialog',
      FIELDSET: 'group', FOOTER: 'contentinfo', FORM: 'form', HEADER: 'banner',
      LI: 'listitem', MAIN: 'main', NAV: 'navigation', OL: 'list', OPTION: 'option',
      OUTPUT: 'status', P: 'paragraph', PROGRESS: 'progressbar', SECTION: 'region',
      SUMMARY: 'button', TABLE: 'table', TD: 'cell', TEXTAREA: 'textbox',
      TH: 'columnheader', TR: 'row', UL: 'list'
    })[tag] || null;
  };

  const ownText = (el) => normalize(Array.prototype.filter.call(el.childNodes, (n) => n.nodeType === 3)
      .map((n) => n.textContent).join(' '));
  const fullText = (el) => normalize(el.innerText !== undefined ? el.innerText : el.textContent);

  const out = [];
  const all = document.body ? document.body.querySelectorAll('*') : [];
  for (const el of all) {
    const tag = el.tagName;
    if (TEXT_TAGS.has(tag)) continue;
    const role = roleOf(el);
    const own = ownText(el);
    const full = fullText(el);
    const interactive = INTERACTIVE_TAGS.has(tag) || !!el.getAttribute('role') ||
      el.hasAttribute('tabindex') || el.isContentEditable === true ||
      el.hasAttribute('onclick') || el.hasAttribute('contenteditable');
    if (!interactive && own.length === 0 && !(el.children.length === 0 && full.length > 0)) continue;
    const visible = isVisible(el);
    const enabled = !el.disabled && el.getAttribute('aria-disabled') !== 'true';
    const value = (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') ? String(el.value) : null;
    const rect = el.getBoundingClientRect();
    out.push({
      element: el,
      tag: tag.toLowerCase(),
      role: role,
      text: own.length ? own : full,
      own_text: own,
      full_text: full,
      value: value,
      visible: visible,
      enabled: enabled,
      interactive: interactive,
      rect_area: rect.width * rect.height,
    });
  }

  // 上限 200：优先保留可见且 enabled 的，其次可交互的，最后按面积从大到小（页面主内容优先）
  const score = (x) => (x.visible && x.enabled ? 2 : 0) + (x.interactive ? 1 : 0);
  out.sort((a, b) => {
    const diff = score(b) - score(a);
    if (diff !== 0) return diff;
    return b.rect_area - a.rect_area;
  });
  const kept = out.slice(0, maxElements);

  const result = [];
  kept.forEach((item, i) => {
    const ref = 'e' + i;
    item.element.setAttribute(refAttr, ref);
    result.push({
      ref: ref,
      tag: item.tag,
      role: item.role,
      text: item.text || null,
      own_text: item.own_text || null,
      full_text: item.full_text || null,
      value: item.value,
      visible: item.visible,
      enabled: item.enabled,
      interactive: item.interactive,
      css: cssPath(item.element),
    });
  });
  return result;
}
"""

#: 取单元素的可访问名：用浏览器自己的 a11y 快照（`locator.aria_snapshot()`），而不是 DOM 文本拼接。
_ARIA_SNAPSHOT_NAME_RE = re.compile(r'^-\s+([A-Za-z]+)(?:\s+"((?:[^"\\]|\\.)*)")?')


def new_id(prefix: str) -> str:
    """契约里的 id 形态：`obs_7f3a...` / `ps_...` / `sess_...` / `exec_...`。"""
    return f"{prefix}_{secrets.token_hex(8)}"


def parse_aria_snapshot_name(snapshot: str) -> str | None:
    """从 `locator.aria_snapshot()` 的首行取浏览器计算出的可访问名。

    形态：`- button "Add to cart"` / `- heading "Cart" [level=1]` / `- button`（无名）。
    """
    if not snapshot:
        return None
    lines = snapshot.splitlines()
    if not lines:
        return None
    match = _ARIA_SNAPSHOT_NAME_RE.match(lines[0].strip())
    if not match:
        return None
    raw = match.group(2)
    if raw is None:
        return None
    return raw.replace('\\"', '"').replace("\\\\", "\\").strip() or None


def _dom_name_candidates(meta: dict[str, Any]) -> list[str]:
    """DOM 侧候选名（仅在 a11y 快照拿不到名字时作为兜底候选）。

    这里给出的只是**候选**：最终是否采用，仍由 `locators.verify_role_locator()` 的唯一命中验证决定，
    因此不会出现"DOM 拼接名被当成可访问名"的情况。
    """
    candidates: list[str] = []
    for key in ("aria_label", "alt", "title", "placeholder", "text", "full_text", "value"):
        value = meta.get(key)
        if isinstance(value, str) and value.strip():
            candidates.append(value.strip())
    seen: set[str] = set()
    unique: list[str] = []
    for cand in candidates:
        if cand not in seen:
            seen.add(cand)
            unique.append(cand)
    return unique


async def accessible_name(page: Page, ref: str, role: str | None) -> str | None:
    """**全 Python 侧唯一的可访问名实现**（CONTRACT §6）。

    返回该元素的浏览器计算可访问名；只有当这个名字能用
    `page.get_by_role(role, name=name, exact=True)` **唯一命中本元素**时才返回，否则返回 None
    （表示该元素没有可用的 role 定位器，观测侧应改用 text / css）。
    """
    if not role:
        return None
    handle = page.locator(f'[{REF_ATTRIBUTE}="{ref}"]')

    candidates: list[str] = []
    try:
        snapshot_name = parse_aria_snapshot_name(
            await handle.aria_snapshot(timeout=PROBE_TIMEOUT_MS)
        )
    except Exception:
        snapshot_name = None
    if snapshot_name:
        candidates.append(snapshot_name)

    try:
        meta = await handle.evaluate(
            """(el) => ({
                aria_label: el.getAttribute('aria-label'),
                alt: el.getAttribute('alt'),
                title: el.getAttribute('title'),
                placeholder: el.getAttribute('placeholder'),
                text: (el.innerText !== undefined ? el.innerText : el.textContent || '')
                    .replace(/\\s+/g, ' ').trim(),
                value: (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA') ? String(el.value) : null,
            })""",
            timeout=PROBE_TIMEOUT_MS,
        ) or {}
    except Exception:
        meta = {}

    for cand in _dom_name_candidates(meta):
        if cand not in candidates:
            candidates.append(cand)

    for cand in candidates:
        if await verify_role_locator(page, ref, role, cand):
            return cand
    return None


class ObservationRecorder:
    """观测采集器：DOM 采集 → 可访问名 → 定位器就地验证 → elements 组装。"""

    def __init__(self, page: Page) -> None:
        self._page = page

    async def collect_raw(self, max_elements: int = MAX_ELEMENTS) -> list[dict[str, Any]]:
        """采集候选元素（含临时 ref 属性），返回原始元数据列表。"""
        return await self._page.evaluate(
            _COLLECT_JS, {"refAttr": REF_ATTRIBUTE, "maxElements": max_elements}
        )

    async def cleanup(self) -> None:
        """移除采集期临时属性，保证页面 DOM 与观测前一致。"""
        try:
            await self._page.evaluate(
                """(refAttr) => {
                    document.querySelectorAll('[' + refAttr + ']').forEach((el) => el.removeAttribute(refAttr));
                }""",
                REF_ATTRIBUTE,
            )
        except Exception:
            pass

    async def observe(
        self,
        *,
        screenshot_path: str | None = None,
        observation_id: str | None = None,
        page_state_id: str | None = None,
        max_elements: int = MAX_ELEMENTS,
    ) -> Observation:
        raw = await self.collect_raw(max_elements)
        elements: list[ElementObservation] = []
        for meta in raw:
            ref = str(meta.get("ref"))
            role = meta.get("role")
            name = await accessible_name(self._page, ref, role)
            text = meta.get("text") or meta.get("full_text") or None
            if isinstance(text, str) and len(text) > MAX_TEXT_CANDIDATE_LEN:
                text = None
            locators = await build_locators(
                self._page,
                ref,
                role=role,
                name=name,
                text=text,
                css=meta.get("css"),
                role_verified=bool(name),
            )
            if not locators:
                # CONTRACT §3.1：一条定位器都验证不出来的元素不进 elements
                continue
            elements.append(
                ElementObservation(
                    ref=ref,
                    tag=str(meta.get("tag")),
                    role=role,
                    name=name,
                    text=text,
                    value=meta.get("value"),
                    visible=bool(meta.get("visible")),
                    enabled=bool(meta.get("enabled")),
                    locators=locators,
                )
            )
        await self.cleanup()
        return Observation(
            observation_id=observation_id or new_id("obs"),
            page_state_id=page_state_id or new_id("ps"),
            url=self._page.url,
            title=await self._page.title(),
            elements=elements,
            screenshot_path=screenshot_path,
        )


async def observe_page(
    page: Page,
    *,
    screenshot_path: str | None = None,
    observation_id: str | None = None,
    page_state_id: str | None = None,
    max_elements: int = MAX_ELEMENTS,
) -> Observation:
    """对当前页面做一次完整观测（会话 `/navigate`、`/act` 与 runner 共用同一条路径）。"""
    return await ObservationRecorder(page).observe(
        screenshot_path=screenshot_path,
        observation_id=observation_id,
        page_state_id=page_state_id,
        max_elements=max_elements,
    )


__all__ = [
    "MAX_ELEMENTS",
    "ObservationRecorder",
    "accessible_name",
    "new_id",
    "observe_page",
    "parse_aria_snapshot_name",
]
