"""定位器：就地验证与偏好排序（CONTRACT §3.1）。

硬约束：

- `locators` 是**按偏好排序、且已在当前页面上验证过**的定位器列表；
- 每条都必须当场真实解析过，`match_count` 是实际命中数；
- 只有 `match_count == 1` 的定位器才允许出现；
- 偏好顺序：`role`（浏览器计算的可访问名）→ `text` → `css`（结构路径，最后手段）；
- 一个元素一条定位器都验证不出来时，该元素不进入 `elements`。

执行期只使用 case 里已经记录的那一条定位器（`to_playwright_locator`），不再重新推导。
"""

from __future__ import annotations

from typing import Any

from playwright.async_api import Locator, Page

from .contracts import CssLocator, LocatorSpec, RoleLocator, TextLocator, VerifiedLocator

#: 内部引用属性：采集时临时打在元素上，用于把 Playwright 侧查到的元素与 DOM 采集结果对上号
REF_ATTRIBUTE = "data-loop-ref"

#: Playwright `get_by_role` 支持的 role 集合（不在此集合内的 role 不做 role 定位器）
PLAYWRIGHT_ROLES: frozenset[str] = frozenset(
    {
        "alert",
        "alertdialog",
        "application",
        "article",
        "banner",
        "blockquote",
        "button",
        "caption",
        "cell",
        "checkbox",
        "code",
        "columnheader",
        "combobox",
        "complementary",
        "contentinfo",
        "definition",
        "deletion",
        "dialog",
        "directory",
        "document",
        "emphasis",
        "feed",
        "figure",
        "form",
        "generic",
        "grid",
        "gridcell",
        "group",
        "heading",
        "img",
        "insertion",
        "link",
        "list",
        "listbox",
        "listitem",
        "log",
        "main",
        "marquee",
        "math",
        "menu",
        "menubar",
        "menuitem",
        "menuitemcheckbox",
        "menuitemradio",
        "meter",
        "navigation",
        "none",
        "note",
        "option",
        "paragraph",
        "presentation",
        "progressbar",
        "radio",
        "radiogroup",
        "region",
        "row",
        "rowgroup",
        "rowheader",
        "scrollbar",
        "search",
        "searchbox",
        "separator",
        "slider",
        "spinbutton",
        "status",
        "strong",
        "subscript",
        "superscript",
        "switch",
        "tab",
        "table",
        "tablist",
        "tabpanel",
        "term",
        "textbox",
        "time",
        "timer",
        "toolbar",
        "tooltip",
        "tree",
        "treegrid",
        "treeitem",
    }
)


class LocatorResolutionError(Exception):
    """定位器无法解析（形状不完整 / 命中数不为 1）。"""


def to_playwright_locator(page: Page, spec: LocatorSpec) -> Locator:
    """把契约定位器翻译成 Playwright locator —— 执行期唯一的解析入口。

    与观测期的验证走**同一条** Playwright 查询路径（`get_by_role` / `get_by_text` / `locator`），
    所以"观测时能解析 ⇒ 执行时能命中"。
    """
    if spec.kind == "role":
        if not spec.role or spec.name is None:
            raise LocatorResolutionError("role locator requires both 'role' and 'name'")
        exact = True if spec.exact is None else bool(spec.exact)
        return page.get_by_role(spec.role, name=spec.name, exact=exact)
    if spec.kind == "text":
        if spec.text is None:
            raise LocatorResolutionError("text locator requires 'text'")
        exact = True if spec.exact is None else bool(spec.exact)
        return page.get_by_text(spec.text, exact=exact)
    if spec.kind == "css":
        if not spec.css:
            raise LocatorResolutionError("css locator requires 'css'")
        return page.locator(spec.css)
    raise LocatorResolutionError(f"unknown locator kind: {spec.kind!r}")


#: 探针超时：读回临时 ref 属性时不要吃 Playwright 默认的 30s
PROBE_TIMEOUT_MS = 2000


async def _ref_of(locator: Locator) -> str | None:
    """读回元素上的临时 ref 属性（用于确认命中的就是本元素）。"""
    try:
        return await locator.get_attribute(REF_ATTRIBUTE, timeout=PROBE_TIMEOUT_MS)
    except Exception:
        return None


async def _safe_count(locator: Locator) -> int:
    try:
        return await locator.count()
    except Exception:
        return 0


async def verify_role_locator(page: Page, ref: str, role: str | None, name: str | None) -> bool:
    """当场验证 `role` 定位器：`get_by_role(role, name, exact=True)` 必须唯一命中本元素。"""
    if not role or not name or role not in PLAYWRIGHT_ROLES:
        return False
    try:
        locator = page.get_by_role(role, name=name, exact=True)
    except Exception:
        return False
    if await _safe_count(locator) != 1:
        return False
    return await _ref_of(locator) == ref


async def verify_text_locator(page: Page, ref: str, text: str | None) -> bool:
    """当场验证 `text` 定位器：`get_by_text(text, exact=True)` 必须唯一命中本元素。"""
    if not text:
        return False
    try:
        locator = page.get_by_text(text, exact=True)
    except Exception:
        return False
    if await _safe_count(locator) != 1:
        return False
    return await _ref_of(locator) == ref


async def verify_css_locator(page: Page, ref: str, css: str | None) -> bool:
    """当场验证 `css` 定位器：结构路径必须唯一命中本元素。"""
    if not css:
        return False
    try:
        locator = page.locator(css)
    except Exception:
        return False
    if await _safe_count(locator) != 1:
        return False
    return await _ref_of(locator) == ref


async def build_locators(
    page: Page,
    ref: str,
    *,
    role: str | None,
    name: str | None,
    text: str | None,
    css: str | None,
    role_verified: bool = False,
) -> list[VerifiedLocator]:
    """按偏好顺序 role → text → css 产出**已验证**的定位器列表。

    只输出 `match_count == 1` 的定位器；一条都产不出来时返回空列表（调用方据此丢弃该元素）。
    `role_verified=True` 表示 `name` 已由 `observer.accessible_name` 用同一验证函数验证过，
    避免重复往返；否则本函数自己验证。
    """
    locators: list[VerifiedLocator] = []

    if role and name:
        ok = role_verified or await verify_role_locator(page, ref, role, name)
        if ok:
            locators.append(RoleLocator(role=role, name=name, exact=True, match_count=1))

    if text and await verify_text_locator(page, ref, text):
        locators.append(TextLocator(text=text, exact=True, match_count=1))

    if css and await verify_css_locator(page, ref, css):
        locators.append(CssLocator(css=css, match_count=1))

    return locators


def locator_payload(locator: VerifiedLocator | LocatorSpec) -> dict[str, Any]:
    """定位器的 JSON 形态（便于日志/测试断言）。"""
    return locator.model_dump(exclude_none=True)


__all__ = [
    "PLAYWRIGHT_ROLES",
    "REF_ATTRIBUTE",
    "LocatorResolutionError",
    "build_locators",
    "locator_payload",
    "to_playwright_locator",
    "verify_css_locator",
    "verify_role_locator",
    "verify_text_locator",
]
