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
import hashlib
from typing import Any

from playwright.async_api import Page

from .blockers import detect_blockers
from .contracts import (
    ACTION_CHECK,
    ACTION_CLICK,
    ACTION_INPUT,
    ACTION_SELECT,
    ActionCandidate,
    BoundingBox,
    CandidateRelation,
    ElementFormInfo,
    ElementObservation,
    Observation,
    StructureNode,
)
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
  const bbox = (el) => {
    const rect = el.getBoundingClientRect();
    return { x: rect.x, y: rect.y, width: rect.width, height: rect.height };
  };
  const visibleInViewport = (el) => {
    const rect = el.getBoundingClientRect();
    return rect.width > 0 && rect.height > 0 && rect.bottom >= 0 && rect.right >= 0 &&
      rect.top <= window.innerHeight && rect.left <= window.innerWidth;
  };
  const attrs = (el) => {
    const out = {};
    for (const key of ['id', 'name', 'class', 'type', 'title', 'placeholder', 'aria-label', 'data-testid', 'href']) {
      const value = el.getAttribute(key);
      if (value !== null && value !== '') out[key] = value;
    }
    return out;
  };
  const zIndex = (el) => {
    const value = window.getComputedStyle(el).zIndex;
    if (!value || value === 'auto') return null;
    const parsed = parseInt(value, 10);
    return Number.isFinite(parsed) ? parsed : null;
  };
  const structureKind = (el, role) => {
    const tag = el.tagName;
    const className = String(el.getAttribute('class') || '').toLowerCase();
    if (tag === 'FORM' || role === 'form') return 'form';
    if (tag === 'DIALOG' || role === 'dialog' || role === 'alertdialog') return 'dialog';
    if (tag === 'IFRAME' || tag === 'FRAME') return 'frame';
    if (tag === 'TR' || role === 'row') return 'table_row';
    if (tag === 'LI' || role === 'listitem') return 'list_item';
    if (tag === 'TABLE' || role === 'table' || role === 'grid' || role === 'treegrid') return 'table';
    if (tag === 'UL' || tag === 'OL' || role === 'list') return 'list';
    if (className.includes('card') || className.includes('product') || el.hasAttribute('data-item')) return 'card';
    return null;
  };
  const submitCandidate = (form) => {
    if (!form) return null;
    return form.querySelector('button, input[type="submit"], input[type="button"], input[type="image"]');
  };
  const canEnterSubmit = (el) => {
    if (!el.form) return false;
    const tag = el.tagName;
    if (tag === 'TEXTAREA') return false;
    if (tag !== 'INPUT') return false;
    const type = (el.getAttribute('type') || 'text').toLowerCase();
    return !['button', 'checkbox', 'file', 'hidden', 'image', 'radio', 'range', 'reset', 'submit'].includes(type);
  };

  const out = [];
  const all = document.body ? document.body.querySelectorAll('*') : [];
  const structureRefs = new Map();
  const structures = [];
  for (const el of all) {
    const tag = el.tagName;
    if (TEXT_TAGS.has(tag)) continue;
    const role = roleOf(el);
    const kind = structureKind(el, role);
    if (kind) {
      const ref = 's' + structures.length;
      structureRefs.set(el, ref);
      const submit = kind === 'form' ? submitCandidate(el) : null;
      structures.push({
        ref: ref,
        element: el,
        kind: kind,
        tag: tag.toLowerCase(),
        role: role,
        parent_ref: null,
        full_text: fullText(el),
        visible: isVisible(el),
        bbox: bbox(el),
        attributes: attrs(el),
        submit_element: submit,
        enter_submittable: kind === 'form' && !!el.querySelector('input:not([type]), input[type="text"], input[type="search"], input[type="email"], input[type="url"], input[type="tel"], input[type="number"]'),
      });
    }
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
      bbox: bbox(el),
      visible_in_viewport: visibleInViewport(el),
      z_index: zIndex(el),
      attributes: attrs(el),
      form_element: el.form || null,
      submit_element: el.form ? submitCandidate(el.form) : null,
      enter_submittable: canEnterSubmit(el),
    });
  }

  const nearestStructureRef = (el) => {
    let cur = el ? el.parentElement : null;
    while (cur) {
      const ref = structureRefs.get(cur);
      if (ref) return ref;
      cur = cur.parentElement;
    }
    return null;
  };
  for (const item of structures) {
    item.parent_ref = nearestStructureRef(item.element);
  }

  // 上限 200：优先保留可见且 enabled 的，其次可交互的，最后按面积从大到小（页面主内容优先）
  const score = (x) => (x.visible && x.enabled ? 2 : 0) + (x.interactive ? 1 : 0);
  out.sort((a, b) => {
    const diff = score(b) - score(a);
    if (diff !== 0) return diff;
    return b.rect_area - a.rect_area;
  });
  const truncated = out.length > maxElements;
  const kept = out.slice(0, maxElements);

  const result = [];
  const elementRefs = new Map();
  kept.forEach((item, i) => {
    const ref = 'e' + i;
    item.element.setAttribute(refAttr, ref);
    elementRefs.set(item.element, ref);
  });
  kept.forEach((item) => {
    const ref = elementRefs.get(item.element);
    const formRef = item.form_element ? structureRefs.get(item.form_element) : null;
    const submitRef = item.submit_element ? elementRefs.get(item.submit_element) : null;
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
      parent_ref: structureRefs.get(item.element.parentElement) || nearestStructureRef(item.element),
      container_ref: nearestStructureRef(item.element),
      bbox: item.bbox,
      visible_in_viewport: item.visible_in_viewport,
      z_index: item.z_index,
      attributes: item.attributes,
      form: formRef ? {
        form_ref: formRef,
        submit_candidate_ref: submitRef || null,
        enter_submittable: item.enter_submittable,
      } : null,
    });
  });
  return {
    elements: result,
    structures: structures.map((item) => ({
      ref: item.ref,
      kind: item.kind,
      tag: item.tag,
      role: item.role,
      parent_ref: item.parent_ref,
      full_text: item.full_text,
      visible: item.visible,
      bbox: item.bbox,
      attributes: item.attributes,
      submit_candidate_ref: item.submit_element ? elementRefs.get(item.submit_element) || null : null,
      enter_submittable: item.enter_submittable,
    })),
    truncated: truncated,
    truncation_reason: truncated ? 'max_elements=' + maxElements + ' kept=' + kept.length + ' candidates=' + out.length : null,
  };
}
"""

#: 取单元素的可访问名：用浏览器自己的 a11y 快照（`locator.aria_snapshot()`），而不是 DOM 文本拼接。
_ARIA_SNAPSHOT_NAME_RE = re.compile(r'^-\s+([A-Za-z]+)(?:\s+"((?:[^"\\]|\\.)*)")?')


def new_id(prefix: str) -> str:
    """契约里的 id 形态：`obs_7f3a...` / `ps_...` / `bsess_...` / `exec_...`。

    注意 `bsess_` 是浏览器会话句柄，不是领域会话 `sess_`（CONTRACT §9.3）。
    """
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


def _append_unique(items: list[str], value: str | None) -> None:
    if not value:
        return
    cleaned = " ".join(value.split()).strip()
    if cleaned and cleaned not in items:
        items.append(cleaned)


def _attribute_aliases(attributes: dict[str, str]) -> list[str]:
    aliases: list[str] = []
    for key in ("aria-label", "title", "placeholder", "id", "name", "data-testid"):
        value = attributes.get(key)
        _append_unique(aliases, value)
        if value and ("_" in value or "-" in value):
            _append_unique(aliases, value.replace("_", " ").replace("-", " "))
    return aliases


def _candidate_id(kind: str, action: str, target: ElementObservation) -> str:
    locator = target.locators[0].model_dump_json()
    raw = f"{kind}|{action}|{target.tag}|{target.role or ''}|{locator}"
    return "act_" + hashlib.sha1(raw.encode("utf-8")).hexdigest()[:12]


def _candidate_action(element: ElementObservation) -> str | None:
    tag = element.tag.lower()
    input_type = element.attributes.get("type", "").lower()
    if tag in {"textarea"} or element.role in {"textbox", "searchbox"}:
        return ACTION_INPUT
    if tag == "select" or element.role in {"combobox", "listbox"}:
        return ACTION_SELECT
    if element.role in {"checkbox", "radio"} or input_type in {"checkbox", "radio"}:
        return ACTION_CHECK
    if tag in {"a", "button"} or element.role in {"button", "link"} or input_type in {
        "button",
        "submit",
        "image",
        "reset",
    }:
        return ACTION_CLICK
    return None


def _candidate_aliases(element: ElementObservation) -> list[str]:
    aliases: list[str] = []
    _append_unique(aliases, element.name)
    _append_unique(aliases, element.text)
    for alias in _attribute_aliases(element.attributes):
        _append_unique(aliases, alias)
    return aliases


def _build_action_candidates(
    elements: list[ElementObservation], structures: list[StructureNode]
) -> list[ActionCandidate]:
    by_ref = {element.ref: element for element in elements}
    candidates: list[ActionCandidate] = []
    seen: set[str] = set()

    def add_candidate(
        *,
        kind: str,
        action: str,
        target: ElementObservation,
        aliases: list[str],
        relations: list[CandidateRelation] | None = None,
        confidence: str = "high",
    ) -> None:
        if not target.visible or not target.enabled or not target.locators:
            return
        candidate_id = _candidate_id(kind, action, target)
        if candidate_id in seen:
            return
        seen.add(candidate_id)
        candidates.append(
            ActionCandidate(
                candidate_id=candidate_id,
                kind=kind,
                action=action,
                target_ref=target.ref,
                role=target.role,
                name=target.name,
                text=target.text,
                aliases=aliases,
                attributes=target.attributes,
                relations=relations or [],
                locator=target.locators[0],
                confidence=confidence,  # type: ignore[arg-type]
            )
        )

    for element in elements:
        action = _candidate_action(element)
        if action is None:
            continue
        add_candidate(
            kind="element_candidate",
            action=action,
            target=element,
            aliases=_candidate_aliases(element),
        )

    for form in structures:
        if form.kind != "form" or not form.submit_candidate_ref:
            continue
        target = by_ref.get(form.submit_candidate_ref)
        if target is None:
            continue
        aliases = ["form submit", "submit"]
        for alias in _candidate_aliases(target):
            _append_unique(aliases, alias)
        relations = [
            CandidateRelation(type="form_submit_candidate", ref=form.ref),
        ]
        for control in elements:
            if control.form is None or control.form.form_ref != form.ref:
                continue
            if control.ref == target.ref:
                continue
            if control.role not in {"textbox", "searchbox", "combobox", "spinbutton"}:
                continue
            label = control.name or control.attributes.get("placeholder") or control.attributes.get("name")
            if label:
                _append_unique(aliases, f"{label} submit")
            if control.role == "searchbox" or "search" in " ".join(
                [label or "", control.attributes.get("id", ""), control.attributes.get("name", "")]
            ).lower():
                _append_unique(aliases, "search submit")
            relations.append(CandidateRelation(type="near_control", ref=control.ref, label=label))
        add_candidate(
            kind="form_submit_candidate",
            action=ACTION_CLICK,
            target=target,
            aliases=aliases,
            relations=relations,
        )
    return candidates


class ObservationRecorder:
    """观测采集器：DOM 采集 → 可访问名 → 定位器就地验证 → elements 组装。"""

    def __init__(self, page: Page) -> None:
        self._page = page

    async def collect_raw(self, max_elements: int = MAX_ELEMENTS) -> dict[str, Any]:
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
        browser_session_id: str | None = None,
        max_elements: int = MAX_ELEMENTS,
    ) -> Observation:
        raw_payload = await self.collect_raw(max_elements)
        raw = raw_payload.get("elements", [])
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
                    parent_ref=meta.get("parent_ref"),
                    container_ref=meta.get("container_ref"),
                    own_text=meta.get("own_text"),
                    full_text=meta.get("full_text"),
                    bbox=BoundingBox(**(meta.get("bbox") or {})),
                    visible_in_viewport=bool(meta.get("visible_in_viewport")),
                    z_index=meta.get("z_index"),
                    attributes=dict(meta.get("attributes") or {}),
                    form=ElementFormInfo(**meta["form"]) if meta.get("form") else None,
                )
            )
        structures = [
            StructureNode(
                ref=str(meta.get("ref")),
                kind=str(meta.get("kind")),
                tag=str(meta.get("tag")),
                role=meta.get("role"),
                parent_ref=meta.get("parent_ref"),
                full_text=str(meta.get("full_text") or ""),
                visible=bool(meta.get("visible")),
                bbox=BoundingBox(**(meta.get("bbox") or {})),
                attributes=dict(meta.get("attributes") or {}),
                submit_candidate_ref=meta.get("submit_candidate_ref"),
                enter_submittable=bool(meta.get("enter_submittable")),
            )
            for meta in raw_payload.get("structures", [])
        ]
        action_candidates = _build_action_candidates(elements, structures)
        blockers = await detect_blockers(self._page)
        await self.cleanup()
        return Observation(
            observation_id=observation_id or new_id("obs"),
            page_state_id=page_state_id or new_id("ps"),
            browser_session_id=browser_session_id,
            url=self._page.url,
            title=await self._page.title(),
            elements=elements,
            structures=structures,
            blockers=blockers,
            action_candidates=action_candidates,
            truncated=bool(raw_payload.get("truncated")),
            truncation_reason=raw_payload.get("truncation_reason"),
            screenshot_path=screenshot_path,
        )


async def observe_page(
    page: Page,
    *,
    screenshot_path: str | None = None,
    observation_id: str | None = None,
    page_state_id: str | None = None,
    browser_session_id: str | None = None,
    max_elements: int = MAX_ELEMENTS,
) -> Observation:
    """对当前页面做一次完整观测（会话 `/navigate`、`/act` 与 runner 共用同一条路径）。"""
    return await ObservationRecorder(page).observe(
        screenshot_path=screenshot_path,
        observation_id=observation_id,
        page_state_id=page_state_id,
        browser_session_id=browser_session_id,
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
