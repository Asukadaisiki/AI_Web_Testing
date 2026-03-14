"""Fallback locator chain with manual correction and intervention capture."""

from __future__ import annotations

import base64
from dataclasses import dataclass

from sqlalchemy.orm import Session

from app.models import LocatorCorrection
from app.locators.ai_visual import AILocateResult, locate_element_by_vision
from app.locators.corrections import MAX_CONSECUTIVE_FAILURES, find_active_correction
from app.locators.semantic import LocatorResolutionError, ResolvedLocator, resolve_semantic_locator
from app.schemas.executions import DOMElementSnapshot, LocatorTrace


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
    db_session: Session | None,
    prefer_input: bool = False,
    require_visible: bool = True,
    require_enabled: bool = False,
) -> ResolvedLocator:
    page_url = getattr(page, "url", "") or ""
    tier1_trace: LocatorTrace | None = None
    ai_candidate: AILocateResult | None = None

    correction = (
        find_active_correction(db_session, page_url=page_url, target_description=target)
        if db_session is not None and page_url
        else None
    )
    if correction is not None:
        resolved = _try_resolve_correction(page, target=target, correction=correction)
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


def _try_resolve_correction(page, *, target: str, correction: LocatorCorrection) -> ResolvedLocator | None:
    try:
        locator = _build_locator_from_correction(page, correction)
        locator.wait_for(state="visible", timeout=3000)
    except Exception:
        correction.consecutive_failures += 1
        if correction.consecutive_failures >= MAX_CONSECUTIVE_FAILURES:
            correction.is_active = False
        return None

    correction.verified_count += 1
    correction.consecutive_failures = 0
    strategy = f"correction:{correction.correction_type}"
    return ResolvedLocator(
        strategy=strategy,
        locator=locator,
        trace=LocatorTrace(
            target=target,
            match_strategy=strategy,
            selection_reason=f"Matched correction #{correction.id} after {correction.verified_count} successful reuses.",
        ),
    )


def _build_locator_from_correction(page, correction: LocatorCorrection):
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
    except Exception:
        return None


def _take_screenshot_base64(page) -> str:
    screenshot_bytes = page.screenshot(full_page=True)
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
    payload = page.evaluate(
        """
        ([pointX, pointY]) => {
          const element = document.elementFromPoint(pointX, pointY);
          if (!element) {
            return null;
          }

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
        """,
        [x, y],
    )
    if payload is None:
        return None
    return DOMElementSnapshot.model_validate(payload)


def _dom_snapshot_matches_target(snapshot: DOMElementSnapshot, target: str) -> bool:
    normalized_target = target.strip().casefold()
    if not normalized_target:
        return False
    haystacks = [
        snapshot.text or "",
        snapshot.aria_label or "",
        snapshot.placeholder or "",
        snapshot.data_testid or "",
        snapshot.role or "",
        snapshot.tag or "",
    ]
    return any(normalized_target in value.casefold() for value in haystacks if value)


def _extract_interactable_elements(page) -> list[DOMElementSnapshot]:
    payload = page.evaluate(
        """
        () => {
          const selector = "button, input, select, textarea, a, [role], [data-testid], [onclick]";
          const nodes = Array.from(document.querySelectorAll(selector)).slice(0, 50);

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
    if not isinstance(payload, list):
        return []
    return [DOMElementSnapshot.model_validate(entry) for entry in payload]


__all__ = [
    "InterventionNeededError",
    "resolve_with_fallback",
    "_build_locator_from_ai_point",
    "_dom_snapshot_matches_target",
]
