"""Fallback locator chain with manual correction and intervention capture."""

from __future__ import annotations

import base64
import logging
import re
from collections import OrderedDict
from threading import Lock

from app.locators.accessibility import try_accessibility_locate as _try_accessibility_locate
from app.locators.ai_visual import (
    AILocateResult,
    AIVisionCandidateBox,
    locate_element_by_vision,
    rank_candidates_by_vision,
    record_ai_visual_cache_hit,
    record_ai_visual_cache_invalidation,
    record_ai_visual_cache_miss,
)
from app.locators.corrections import CorrectionRecord, CorrectionStore, normalize_target_description
from app.locators.semantic import (
    LocatorResolutionError,
    ResolvedLocator,
    collect_semantic_candidates,
    resolve_semantic_locator,
    _resolve_by_strategy,
    _build_selection_reason,
    _resolve_failure_reason,
)
from app.locators.url_pattern import generalize_url
from app.schemas.executions import DOMElementSnapshot, LocatorTrace, LocatorCandidateEvidence


logger = logging.getLogger(__name__)
TOKEN_PATTERN = re.compile(r"[0-9a-z]+|[\u4e00-\u9fff]+", re.IGNORECASE)
# Jaccard fallback is intentionally conservative so near-miss tokens can match
# without reopening obvious short-substring false positives like "ok"/"booking".
JACCARD_THRESHOLD = 0.5
# Only rerank when the top semantic candidates are genuinely close, otherwise
# keep the cheaper DOM-only decision path.
SEMANTIC_RERANK_SCORE_GAP = 5
AI_VISUAL_SESSION_CACHE_MAX_ENTRIES = 128
_AI_VISUAL_SESSION_CACHE: OrderedDict[tuple[str, str], str] = OrderedDict()
_AI_VISUAL_SESSION_CACHE_LOCK = Lock()
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
      const isOverlayLike = (element) => {
        if (!(element instanceof Element)) {
          return false;
        }
        const role = (element.getAttribute("role") || "").toLowerCase();
        const className = (element.className || "").toString().toLowerCase();
        const id = (element.id || "").toLowerCase();
        return (
          role === "alert" ||
          role === "dialog" ||
          className.includes("overlay") ||
          className.includes("toast") ||
          className.includes("loading") ||
          className.includes("modal") ||
          id.includes("overlay") ||
          id.includes("toast") ||
          id.includes("loading")
        );
      };

      const stack = document.elementsFromPoint(pointX, pointY);
      if (!stack.length) {
        return null;
      }
      const topElement = stack[0];
      const element = isOverlayLike(topElement)
        ? stack.find((candidate) => !isOverlayLike(candidate)) || topElement
        : topElement;

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
CAPTURE_DOM_CANDIDATE_SCRIPT = (
    """
    (element) => {
    """
    + SELECTOR_HELPERS_JS
    + """
      if (!(element instanceof Element)) {
        return null;
      }
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
    }
    """
)
EXTRACT_INTERACTABLE_ELEMENTS_SCRIPT = (
    """
    (maxElements) => {
      const selector = "button, input, select, textarea, a, [role], [data-testid], [onclick], p, span, h1, h2, h3, h4, h5, h6, li, label, img";
      const nodes = Array.from(document.querySelectorAll(selector)).slice(0, maxElements || 300);
    """
    + SELECTOR_HELPERS_JS
    + """
      return nodes.map((element) => {
        const rect = element.getBoundingClientRect();
        const style = window.getComputedStyle(element);
        const visible = rect.width > 0 && rect.height > 0 && style.visibility !== "hidden" && style.display !== "none";
        const enabled = !(element.disabled) && element.getAttribute("aria-disabled") !== "true";
        const text = (element.innerText || element.textContent || "").replace(/\\s+/g, " ").trim().slice(0, 160);
        const nameAttr = element.getAttribute("name");
        const classAttr = element.className || null;
        const idAttr = element.id || null;
        return {
          tag: element.tagName.toLowerCase(),
          text: text || null,
          role: element.getAttribute("role"),
          aria_label: element.getAttribute("aria-label"),
          placeholder: element.getAttribute("placeholder"),
          data_testid: element.getAttribute("data-testid"),
          href: element.tagName.toLowerCase() === "a" ? (element.getAttribute("href") || null) : null,
          name: nameAttr || null,
          class_name: classAttr || null,
          id: idAttr || null,
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
        vlm_failure_reason: str | None = None,
    ) -> None:
        super().__init__(f"All locate tiers failed for target: {target}")
        self.target = target
        self.page_url = page_url
        self.dom_snapshot = dom_snapshot
        self.ai_candidate = ai_candidate
        self.tier1_trace = tier1_trace
        self.vlm_failure_reason = vlm_failure_reason


def _try_semantic_candidates_in_order(
    page,
    target: str,
    *,
    target_strategy: str | None = None,
    prefer_input: bool = False,
    require_visible: bool = True,
    require_enabled: bool = False,
) -> ResolvedLocator:
    """Try semantic candidates in priority order, returning the first that verifies.

    Unlike :func:`resolve_semantic_locator` which picks the single highest-scored
    candidate and returns immediately, this function verifies each candidate against
    the live page (``wait_for`` + DOM text match) before accepting it, falling
    through to the next candidate on failure.
    """
    normalized_target = target.strip()

    if target_strategy is not None and target_strategy != "semantic":
        try:
            return _resolve_by_strategy(
                page,
                normalized_target,
                target_strategy,
                prefer_input=prefer_input,
                require_visible=require_visible,
                require_enabled=require_enabled,
            )
        except LocatorResolutionError:
            pass

    entries = collect_semantic_candidates(
        page,
        normalized_target,
        prefer_input=prefer_input,
        require_visible=require_visible,
        require_enabled=require_enabled,
    )
    all_candidates = [entry.candidate for entry in entries[:5]]

    for entry in entries:
        if entry.candidate.rejected_reasons:
            continue
        try:
            entry.locator.wait_for(state="visible", timeout=3000)
            snapshot = _snapshot_dom_candidate(entry.locator)
            if snapshot is not None and _dom_snapshot_matches_target(snapshot, normalized_target):
                return ResolvedLocator(
                    strategy=entry.strategy,
                    locator=entry.locator,
                    trace=LocatorTrace(
                        target=normalized_target,
                        match_strategy=entry.strategy,
                        candidates=all_candidates,
                        selected_candidate=entry.candidate,
                        selection_reason=_build_selection_reason(entry.candidate),
                    ),
                )
        except Exception:
            continue

    raise LocatorResolutionError(
        _resolve_failure_reason(
            all_candidates,
            require_visible=require_visible,
            require_enabled=require_enabled,
        ),
        trace=LocatorTrace(
            target=normalized_target,
            candidates=all_candidates,
            failure_reason="All semantic candidates failed verification against live page.",
        ),
    )


def resolve_with_fallback(
    page,
    target: str,
    *,
    target_strategy: str | None = None,
    correction_store: CorrectionStore | None = None,
    execution_id: int | None = None,
    prefer_input: bool = False,
    require_visible: bool = True,
    require_enabled: bool = False,
) -> ResolvedLocator:
    page_url = getattr(page, "url", "") or ""
    tier1_trace: LocatorTrace | None = None
    ai_candidate: AILocateResult | None = None
    cache_key = _build_ai_visual_cache_key(page_url=page_url, target=target)

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

    cached_resolution = _try_resolve_cached_ai_locator(page, target=target, cache_key=cache_key)
    if cached_resolution is not None:
        return cached_resolution

    try:
        return _try_semantic_candidates_in_order(
            page,
            target,
            target_strategy=target_strategy,
            prefer_input=prefer_input,
            require_visible=require_visible,
            require_enabled=require_enabled,
        )
    except LocatorResolutionError as exc:
        tier1_trace = exc.trace
        # Tier 1.5: zero-cost accessibility tree lookup (arxiv 2603.20358)
        a11y_resolved = _try_accessibility_locate(
            page,
            target=target,
            require_visible=require_visible,
        )
        if a11y_resolved is not None:
            return a11y_resolved
        reranked = _try_vlm_rank_candidates(
            page,
            target=target,
            trace=tier1_trace,
            prefer_input=prefer_input,
            require_visible=require_visible,
            require_enabled=require_enabled,
            target_strategy=target_strategy,
        )
        if reranked is not None:
            return reranked

    ai_candidate = _try_ai_visual_locate(page, target=target)
    vlm_failure_reason = _get_vlm_failure_reason()
    if ai_candidate is not None:
        resolved = _build_locator_from_ai_point(
            page,
            target=target,
            ai_candidate=ai_candidate,
            cache_key=cache_key,
        )
        if resolved is not None:
            return resolved
        coord_resolved = _try_coordinate_click_fallback(
            page, target=target, ai_candidate=ai_candidate,
        )
        if coord_resolved is not None:
            return coord_resolved

    raise InterventionNeededError(
        target=target,
        page_url=page_url,
        dom_snapshot=_extract_interactable_elements(page),
        ai_candidate=ai_candidate,
        tier1_trace=tier1_trace,
        vlm_failure_reason=vlm_failure_reason,
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
            deep_locate=True,
        )
    except Exception as exc:
        logger.warning("AI visual fallback failed for target=%s error=%s", target, exc)
        return None


def _get_vlm_failure_reason() -> str | None:
    """Retrieve the last VLM failure reason from the runtime state."""
    try:
        from app.locators.ai_visual import RUNTIME_STATE, _STATE_LOCK
        with _STATE_LOCK:
            return RUNTIME_STATE.last_failure_reason or None
    except Exception:
        return None


def _build_ai_visual_cache_key(*, page_url: str, target: str) -> tuple[str, str] | None:
    if not page_url:
        return None
    normalized_target = normalize_target_description(target)
    if not normalized_target:
        return None
    return (generalize_url(page_url), normalized_target)


def _try_resolve_cached_ai_locator(
    page,
    *,
    target: str,
    cache_key: tuple[str, str] | None,
) -> ResolvedLocator | None:
    if cache_key is None:
        return None
    selector = _get_cached_ai_selector(cache_key)
    if selector is None:
        record_ai_visual_cache_miss()
        logger.debug("AI visual session cache_miss page_url_pattern=%s target=%s", cache_key[0], target)
        return None

    locator = page.locator(selector)
    try:
        locator.wait_for(state="visible", timeout=3000)
        snapshot = _snapshot_dom_candidate(locator)
    except Exception as exc:
        _invalidate_cached_ai_selector(cache_key)
        record_ai_visual_cache_invalidation()
        logger.debug(
            "AI visual session cache_invalidated page_url_pattern=%s target=%s selector=%s error=%s",
            cache_key[0],
            target,
            selector,
            exc,
        )
        return None

    if snapshot is None or not _dom_snapshot_matches_target(snapshot, target):
        _invalidate_cached_ai_selector(cache_key)
        record_ai_visual_cache_invalidation()
        logger.debug(
            "AI visual session cache_invalidated page_url_pattern=%s target=%s selector=%s reason=semantic_mismatch",
            cache_key[0],
            target,
            selector,
        )
        return None

    record_ai_visual_cache_hit()
    logger.debug("AI visual session cache_hit page_url_pattern=%s target=%s", cache_key[0], target)
    return ResolvedLocator(
        strategy="ai_visual_cache",
        locator=locator,
        trace=LocatorTrace(
            target=target,
            match_strategy="ai_visual_cache",
            selection_reason=f"Matched cached AI visual selector for {cache_key[0]}.",
        ),
    )


def _get_cached_ai_selector(cache_key: tuple[str, str]) -> str | None:
    with _AI_VISUAL_SESSION_CACHE_LOCK:
        selector = _AI_VISUAL_SESSION_CACHE.get(cache_key)
        if selector is None:
            return None
        _AI_VISUAL_SESSION_CACHE.move_to_end(cache_key)
        return selector


def _store_cached_ai_selector(cache_key: tuple[str, str], selector: str) -> None:
    with _AI_VISUAL_SESSION_CACHE_LOCK:
        _AI_VISUAL_SESSION_CACHE[cache_key] = selector
        _AI_VISUAL_SESSION_CACHE.move_to_end(cache_key)
        while len(_AI_VISUAL_SESSION_CACHE) > AI_VISUAL_SESSION_CACHE_MAX_ENTRIES:
            _AI_VISUAL_SESSION_CACHE.popitem(last=False)


def _invalidate_cached_ai_selector(cache_key: tuple[str, str]) -> None:
    with _AI_VISUAL_SESSION_CACHE_LOCK:
        _AI_VISUAL_SESSION_CACHE.pop(cache_key, None)


def _clear_ai_visual_session_cache() -> None:
    with _AI_VISUAL_SESSION_CACHE_LOCK:
        _AI_VISUAL_SESSION_CACHE.clear()


def _take_screenshot_base64(page) -> str:
    screenshot_bytes = page.screenshot(full_page=True)
    return base64.b64encode(screenshot_bytes).decode("utf-8")


def _build_locator_from_ai_point(
    page,
    *,
    target: str,
    ai_candidate: AILocateResult,
    cache_key: tuple[str, str] | None = None,
) -> ResolvedLocator | None:
    snapshot = _snapshot_dom_element_at_point(page, *ai_candidate.center)
    if snapshot is None or not _dom_snapshot_matches_target(snapshot, target):
        return None

    selector = snapshot.css_selector or (f"xpath={snapshot.xpath}" if snapshot.xpath else None)
    if selector is None:
        return None
    locator = page.locator(selector)
    try:
        locator.wait_for(state="visible", timeout=3000)
    except Exception:
        return None
    if cache_key is not None:
        _store_cached_ai_selector(cache_key, selector)

    return ResolvedLocator(
        strategy="ai_visual",
        locator=locator,
        trace=LocatorTrace(
            target=target,
            match_strategy="ai_visual",
            selection_reason=f"AI visual locate verified against DOM at {ai_candidate.center}.",
        ),
    )


def _try_coordinate_click_fallback(
    page,
    *,
    target: str,
    ai_candidate: AILocateResult,
) -> ResolvedLocator | None:
    """Tier 2.5: use VLM bbox coordinates directly when DOM selector extraction fails."""
    x, y = ai_candidate.center
    if not (isinstance(x, int) and isinstance(y, int) and x >= 0 and y >= 0):
        return None
    return ResolvedLocator(
        strategy="ai_coordinate_click",
        locator=page.locator("body"),
        click_coordinates=(x, y),
        trace=LocatorTrace(
            target=target,
            match_strategy="ai_coordinate_click",
            selection_reason=f"VLM located at ({x},{y}), DOM selector extraction failed.",
        ),
    )


def _snapshot_dom_element_at_point(page, x: int, y: int) -> DOMElementSnapshot | None:
    payload = page.evaluate(SNAPSHOT_DOM_AT_POINT_SCRIPT, [x, y])
    if payload is None:
        return None
    return DOMElementSnapshot.model_validate(payload)


def _snapshot_dom_candidate(locator) -> DOMElementSnapshot | None:
    payload = locator.evaluate(CAPTURE_DOM_CANDIDATE_SCRIPT)
    if payload is None:
        return None
    return DOMElementSnapshot.model_validate(payload)


def _dom_snapshot_matches_target(snapshot: DOMElementSnapshot, target: str) -> bool:
    semantic_fields = [
        snapshot.text,
        snapshot.aria_label,
        snapshot.placeholder,
        snapshot.data_testid,
        snapshot.name,
    ]
    normalized_target = _normalize_text(target)
    if normalized_target and any(_normalize_text(value) == normalized_target for value in semantic_fields if value):
        return True

    target_tokens = _tokenize(target)
    if not target_tokens:
        return False

    if any(target_tokens.issubset(_tokenize(value)) for value in semantic_fields if value):
        return True
    if any(_jaccard_similarity(target_tokens, _tokenize(value)) >= JACCARD_THRESHOLD for value in semantic_fields if value):
        return True
    target_char_tokens = _cjk_char_tokens(target)
    if target_char_tokens and any(
        _jaccard_similarity(target_char_tokens, _cjk_char_tokens(value)) >= JACCARD_THRESHOLD
        for value in semantic_fields
        if value
    ):
        return True

    fallback_fields = [snapshot.role, snapshot.tag]
    return any(target_tokens.issubset(_tokenize(value)) for value in fallback_fields if value)


def _extract_interactable_elements(page) -> list[DOMElementSnapshot]:
    payload = page.evaluate(EXTRACT_INTERACTABLE_ELEMENTS_SCRIPT, 300)
    if not isinstance(payload, list):
        return []
    return [DOMElementSnapshot.model_validate(entry) for entry in payload]


def _tokenize(value: str | None) -> set[str]:
    if not value:
        return set()
    return {token for token in TOKEN_PATTERN.findall(value.casefold()) if token}


def _normalize_text(value: str | None) -> str:
    if not value:
        return ""
    return " ".join(value.casefold().split())


def _cjk_char_tokens(value: str | None) -> set[str]:
    if not value:
        return set()
    return {char for char in value if "\u4e00" <= char <= "\u9fff"}


def _jaccard_similarity(left: set[str], right: set[str]) -> float:
    if not left or not right:
        return 0.0
    union = left | right
    if not union:
        return 0.0
    return len(left & right) / len(union)


def _try_vlm_rank_candidates(
    page,
    *,
    target: str,
    trace: LocatorTrace | None,
    prefer_input: bool,
    require_visible: bool,
    require_enabled: bool,
    target_strategy: str | None = None,
) -> ResolvedLocator | None:
    try:
        if trace is None or not _should_rerank_trace_candidates(trace.candidates):
            return None

        candidate_entries = _collect_rankable_semantic_candidates(
            page,
            target=target,
            prefer_input=prefer_input,
            require_visible=require_visible,
            require_enabled=require_enabled,
            target_strategy=target_strategy,
        )
        if len(candidate_entries) < 2:
            return None

        screenshot_base64 = _take_screenshot_base64(page)
        ranked_index = rank_candidates_by_vision(
            screenshot_base64=screenshot_base64,
            target_description=target,
            candidates=[
                AIVisionCandidateBox(
                    index=index,
                    label=_format_candidate_label(entry["snapshot"], entry["candidate"]),
                    bbox=_rect_to_bbox(entry["snapshot"]),
                )
                for index, entry in enumerate(candidate_entries)
            ],
        )
        if ranked_index is None or ranked_index < 0 or ranked_index >= len(candidate_entries):
            return None

        selected_entry = candidate_entries[ranked_index]
        candidates = [entry["candidate"] for entry in candidate_entries[:5]]
        return ResolvedLocator(
            strategy="semantic_vlm_rank",
            locator=selected_entry["locator"],
            trace=LocatorTrace(
                target=target,
                match_strategy="semantic_vlm_rank",
                candidates=candidates,
                selected_candidate=selected_entry["candidate"],
                selection_reason=f"VLM ranked close semantic candidates and selected candidate #{ranked_index}.",
            ),
        )
    except Exception as exc:
        logger.warning("Semantic VLM rerank failed for target=%s error=%s", target, exc)
        return None


def _should_rerank_trace_candidates(candidates: list[LocatorCandidateEvidence]) -> bool:
    eligible = [candidate for candidate in candidates if not candidate.rejected_reasons]
    if len(eligible) < 2:
        return False
    ordered = sorted(eligible, key=lambda candidate: candidate.score, reverse=True)
    return (ordered[0].score - ordered[1].score) < SEMANTIC_RERANK_SCORE_GAP


def _collect_rankable_semantic_candidates(
    page,
    *,
    target: str,
    prefer_input: bool,
    require_visible: bool,
    require_enabled: bool,
    target_strategy: str | None = None,
) -> list[dict[str, object]]:
    entries: list[dict[str, object]] = []
    semantic_candidates = collect_semantic_candidates(
        page,
        target,
        target_strategy=target_strategy,
        prefer_input=prefer_input,
        require_visible=require_visible,
        require_enabled=require_enabled,
        max_per_strategy=3,
        max_candidates=10,
    )
    for entry in semantic_candidates:
        try:
            if entry.candidate.rejected_reasons:
                continue
            payload = entry.locator.evaluate(CAPTURE_DOM_CANDIDATE_SCRIPT)
            if payload is None:
                continue
            snapshot = DOMElementSnapshot.model_validate(payload)
            if snapshot.rect is None:
                continue
            entries.append(
                {
                    "locator": entry.locator,
                    "candidate": entry.candidate,
                    "snapshot": snapshot,
                }
            )
        except Exception as exc:
            logger.debug(
                "Skipping semantic rerank candidate for target=%s strategy=%s error=%s",
                target,
                entry.strategy,
                exc,
            )
            continue

    entries.sort(key=lambda entry: entry["candidate"].score, reverse=True)
    return entries[:5]


def _rect_to_bbox(snapshot: DOMElementSnapshot) -> tuple[int, int, int, int]:
    rect = snapshot.rect or {"x": 0, "y": 0, "width": 0, "height": 0}
    xmin = int(rect["x"])
    ymin = int(rect["y"])
    xmax = int(rect["x"] + rect["width"])
    ymax = int(rect["y"] + rect["height"])
    return (xmin, ymin, xmax, ymax)


def _format_candidate_label(snapshot: DOMElementSnapshot, candidate: LocatorCandidateEvidence) -> str:
    return snapshot.text or snapshot.aria_label or candidate.preview_text or snapshot.tag


__all__ = [
    "InterventionNeededError",
    "resolve_with_fallback",
]
