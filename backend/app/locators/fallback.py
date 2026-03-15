"""Fallback locator chain with manual correction and intervention capture."""

from __future__ import annotations

import base64
import logging
import re

from app.locators.ai_visual import AILocateResult, locate_element_by_vision
from app.locators.corrections import CorrectionRecord, CorrectionStore
from app.locators.semantic import LocatorResolutionError, ResolvedLocator, resolve_semantic_locator
from app.schemas.executions import DOMElementSnapshot, LocatorTrace


logger = logging.getLogger(__name__)
TOKEN_PATTERN = re.compile(r"[0-9a-z]+|[\u4e00-\u9fff]+", re.IGNORECASE)
SELECTOR_HELPERS_JS = """
const buildCssSelector = (node) => {
  if (!(node instanceof Element)) {
    return null;
  }
  if (node.id) {
    return `#${CSS.escape(node.id)}`;
  }
  const segments = [];
  let current = node;
  while (current instanceof Element && current !== document.body) {
    const tag = current.tagName.toLowerCase();
    const parent = current.parentElement;
    if (!parent) {
      segments.unshift(tag);
      break;
    }
    const siblings = Array.from(parent.children).filter(
      (child) => child.tagName === current.tagName,
    );
    const index = siblings.indexOf(current);
    segments.unshift(
      siblings.length > 1 ? `${tag}:nth-of-type(${index + 1})` : tag,
    );
    current = parent;
  }
  return segments.join(" > ");
};

const buildXPath = (node) => {
  if (!(node instanceof Element)) {
    return null;
  }
  const segments = [];
  let current = node;
  while (current instanceof Element) {
    let index = 1;
    let sibling = current.previousElementSibling;
    while (sibling) {
      if (sibling.tagName === current.tagName) {
        index += 1;
      }
      sibling = sibling.previousElementSibling;
    }
    segments.unshift(`${current.tagName.toLowerCase()}[${index}]`);
    current = current.parentElement;
  }
  return `/${segments.join("/")}`;
};
"""
SNAPSHOT_DOM_AT_POINT_SCRIPT = (
    """
    ([pointX, pointY]) => {
    """
    + SELECTOR_HELPERS_JS
    + """
      const element = document.elementFromPoint(pointX, pointY);
      if (!element) {
        return null;
      }

      const rect = element.getBoundingClientRect();
      const style = window.getComputedStyle(element);
      const visible = rect.width > 0 && rect.height > 0 && style.visibility !== "hidden" && style.display !== "none";
      const enabled = !(element.disabled) && element.getAttribute("aria-disabled") !== "true";
      const text = (element.innerText || element.textContent || "").replace(/\\s+/g, " ").trim().slice(0, 200);

      return {
        tag: element.tagName.toLowerCase(),
        text: text || null,
        role: element.getAttribute("role"),
        aria_label: element.getAttribute("aria-label"),
        placeholder: element.getAttribute("placeholder"),
        data_testid: element.getAttribute("data-testid"),
        css_selector: buildCssSelector(element),
        xpath: buildXPath(element),
        rect: {
          x: rect.x,
          y: rect.y,
          width: rect.width,
          height: rect.height,
        },
        visible,
        enabled,
      };
    }
    """
)
EXTRACT_INTERACTABLE_ELEMENTS_SCRIPT = (
    """
    () => {
      const selector = "button, input, select, textarea, a, [role], [data-testid], [onclick]";
      const nodes = Array.from(document.querySelectorAll(selector)).slice(0, 50);
    """
    + SELECTOR_HELPERS_JS
    + """
      return nodes.map((element) => {
        const rect = element.getBoundingClientRect();
        const style = window.getComputedStyle(element);
        const visible = rect.width > 0 && rect.height > 0 && style.visibility !== "hidden" && style.display !== "none";
        const enabled = !(element.disabled) && element.getAttribute("aria-disabled") !== "true";
        const text = (element.innerText || element.textContent || "").replace(/\\s+/g, " ").trim().slice(0, 160);
        return {
          tag: element.tagName.toLowerCase(),
          text: text || null,
          role: element.getAttribute("role"),
          aria_label: element.getAttribute("aria-label"),
          placeholder: element.getAttribute("placeholder"),
          data_testid: element.getAttribute("data-testid"),
          css_selector: buildCssSelector(element),
          xpath: buildXPath(element),
          rect: {
            x: rect.x,
            y: rect.y,
            width: rect.width,
            height: rect.height,
          },
          visible,
          enabled,
        };
      });
    }
    """
)


class InterventionNeededError(RuntimeError):
    """Raised when all active locator tiers fail."""

    def __init__(
        self,
        *,
        target: str,
        page_url: str,
        dom_snapshot: list[DOMElementSnapshot],
        ai_candidate: AILocateResult | None = None,
        tier1_trace: LocatorTrace | None = None,
    ) -> None:
        super().__init__(f"All locate tiers failed for target: {target}")
        self.target = target
        self.page_url = page_url
        self.dom_snapshot = dom_snapshot
        self.ai_candidate = ai_candidate
        self.tier1_trace = tier1_trace


def resolve_with_fallback(
    page,
    target: str,
    *,
    correction_store: CorrectionStore | None = None,
    execution_id: int | None = None,
    prefer_input: bool = False,
    require_visible: bool = True,
    require_enabled: bool = False,
) -> ResolvedLocator:
    page_url = getattr(page, "url", "") or ""
    tier1_trace: LocatorTrace | None = None
    ai_candidate: AILocateResult | None = None

    correction = (
        correction_store.find_active_correction(page_url=page_url, target_description=target)
        if correction_store is not None and page_url
        else None
    )
    if correction is not None:
        resolved = _try_resolve_correction(
            page,
            target=target,
            correction=correction,
            correction_store=correction_store,
            execution_id=execution_id,
        )
        if resolved is not None:
            return resolved

    try:
        return resolve_semantic_locator(
            page,
            target,
            prefer_input=prefer_input,
            require_visible=require_visible,
            require_enabled=require_enabled,
        )
    except LocatorResolutionError as exc:
        tier1_trace = exc.trace

    ai_candidate = _try_ai_visual_locate(page, target=target)
    if ai_candidate is not None:
        resolved = _build_locator_from_ai_point(page, target=target, ai_candidate=ai_candidate)
        if resolved is not None:
            return resolved

    raise InterventionNeededError(
        target=target,
        page_url=page_url,
        dom_snapshot=_extract_interactable_elements(page),
        ai_candidate=ai_candidate,
        tier1_trace=tier1_trace,
    )


def _try_resolve_correction(
    page,
    *,
    target: str,
    correction: CorrectionRecord,
    correction_store: CorrectionStore | None,
    execution_id: int | None,
) -> ResolvedLocator | None:
    try:
        locator = _build_locator_from_correction(page, correction)
        locator.wait_for(state="visible", timeout=3000)
    except Exception as exc:
        updated_correction = (
            correction_store.record_failure(correction.id, execution_id=execution_id)
            if correction_store is not None
            else correction
        )
        logger.warning(
            "Correction reuse failed id=%s target=%s consecutive_failures=%s is_active=%s error=%s",
            correction.id,
            target,
            updated_correction.consecutive_failures if updated_correction is not None else correction.consecutive_failures,
            updated_correction.is_active if updated_correction is not None else correction.is_active,
            exc,
        )
        return None

    updated_correction = (
        correction_store.record_success(correction.id, execution_id=execution_id)
        if correction_store is not None
        else correction
    )
    strategy = f"correction:{correction.correction_type}"
    return ResolvedLocator(
        strategy=strategy,
        locator=locator,
        trace=LocatorTrace(
            target=target,
            match_strategy=strategy,
            selection_reason=(
                f"Matched correction #{correction.id} after "
                f"{(updated_correction.verified_count if updated_correction is not None else correction.verified_count)} "
                "successful reuses."
            ),
        ),
    )


def _build_locator_from_correction(page, correction: CorrectionRecord):
    if correction.correction_type == "test_id":
        return page.get_by_test_id(correction.correction_value)
    if correction.correction_type == "xpath":
        value = correction.correction_value
        selector = value if value.startswith("xpath=") else f"xpath={value}"
        return page.locator(selector)
    return page.locator(correction.correction_value)


def _try_ai_visual_locate(page, *, target: str) -> AILocateResult | None:
    try:
        screenshot_base64 = _take_screenshot_base64(page)
        viewport = getattr(page, "viewport_size", None) or {}
        width = int(viewport.get("width", 0))
        height = int(viewport.get("height", 0))
        if width <= 0 or height <= 0:
            return None
        return locate_element_by_vision(
            screenshot_base64=screenshot_base64,
            target_description=target,
            image_width=width,
            image_height=height,
        )
    except Exception as exc:
        logger.warning("AI visual fallback failed for target=%s error=%s", target, exc)
        return None


def _take_screenshot_base64(page) -> str:
    screenshot_bytes = page.screenshot(full_page=False)
    return base64.b64encode(screenshot_bytes).decode("utf-8")


def _build_locator_from_ai_point(
    page,
    *,
    target: str,
    ai_candidate: AILocateResult,
) -> ResolvedLocator | None:
    snapshot = _snapshot_dom_element_at_point(page, *ai_candidate.center)
    if snapshot is None or not _dom_snapshot_matches_target(snapshot, target):
        return None

    selector = snapshot.css_selector or (f"xpath={snapshot.xpath}" if snapshot.xpath else None)
    if selector is None:
        return None

    return ResolvedLocator(
        strategy="ai_visual",
        locator=page.locator(selector),
        trace=LocatorTrace(
            target=target,
            match_strategy="ai_visual",
            selection_reason=f"AI visual locate verified against DOM at {ai_candidate.center}.",
        ),
    )


def _snapshot_dom_element_at_point(page, x: int, y: int) -> DOMElementSnapshot | None:
    payload = page.evaluate(SNAPSHOT_DOM_AT_POINT_SCRIPT, [x, y])
    if payload is None:
        return None
    return DOMElementSnapshot.model_validate(payload)


def _dom_snapshot_matches_target(snapshot: DOMElementSnapshot, target: str) -> bool:
    target_tokens = _tokenize(target)
    if not target_tokens:
        return False
    semantic_fields = [
        snapshot.text,
        snapshot.aria_label,
        snapshot.placeholder,
        snapshot.data_testid,
    ]
    if any(target_tokens.issubset(_tokenize(value)) for value in semantic_fields if value):
        return True

    fallback_fields = [snapshot.role, snapshot.tag]
    return any(target_tokens.issubset(_tokenize(value)) for value in fallback_fields if value)


def _extract_interactable_elements(page) -> list[DOMElementSnapshot]:
    payload = page.evaluate(EXTRACT_INTERACTABLE_ELEMENTS_SCRIPT)
    if not isinstance(payload, list):
        return []
    return [DOMElementSnapshot.model_validate(entry) for entry in payload]


def _tokenize(value: str | None) -> set[str]:
    if not value:
        return set()
    return {token for token in TOKEN_PATTERN.findall(value.casefold()) if token}


__all__ = [
    "InterventionNeededError",
    "resolve_with_fallback",
]
