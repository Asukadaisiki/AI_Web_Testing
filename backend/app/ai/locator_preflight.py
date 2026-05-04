"""Static locator preflight — validate DSL targets against collected page elements.

Runs without a live browser; all data comes from the DOM snapshots that
``explore_page`` / ``explore_flow`` already collected.
"""

from __future__ import annotations

import logging
import re
from typing import Any

logger = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# Target-type detection (mirrors semantic.py's _resolve_explicit_locator)
# ---------------------------------------------------------------------------

_CHAINED_SELECTOR_RE = re.compile(
    r"^(\.\w[\w-]*|#[\w-]+|\w[\w-]*\.\w[\w-]*)\s*>>?\s*text\s*=\s*(.+)$"
)
_COMPOUND_CSS_RE = re.compile(r"^[a-zA-Z][a-zA-Z0-9]*[\.\#\[\s\>:,~\+]")
_KNOWN_TAGS = {
    "button", "input", "select", "textarea", "a", "form",
    "div", "span", "p", "h1", "h2", "h3", "h4", "h5", "h6",
    "li", "ul", "ol", "img", "label", "nav", "header", "main", "footer",
}


def _classify_target(target: str) -> tuple[str, str]:
    """Return (strategy, value) for a target string.

    Mirrors the detection logic from ``semantic.py:_resolve_explicit_locator``.
    """
    t = target.strip()

    if _CHAINED_SELECTOR_RE.match(t):
        return "chained_css_text", t
    if t.startswith("css="):
        return "css", t[4:]
    if t.startswith("xpath="):
        return "xpath", t[6:]
    if t.startswith("//"):
        return "xpath", t
    if t.startswith("#") or t.startswith("[") or t.startswith("."):
        return "css", t
    if t.startswith("data-testid="):
        return "data-testid", t[12:]
    tag_name = re.split(r"[\.\#\[\s\>:,~\+]", t, maxsplit=1)[0]
    if tag_name in _KNOWN_TAGS and _COMPOUND_CSS_RE.match(t):
        return "css_tag", t
    return "semantic", t


# ---------------------------------------------------------------------------
# Text matching (mirrors fallback.py's _dom_snapshot_matches_target)
# ---------------------------------------------------------------------------

TOKEN_PATTERN = re.compile(r"[0-9a-z]+|[一-鿿]+", re.IGNORECASE)


def _normalize_text(value: str | None) -> str:
    if not value:
        return ""
    return re.sub(r"\s+", " ", value.strip().casefold())


def _tokenize(value: str | None) -> set[str]:
    if not value:
        return set()
    return set(TOKEN_PATTERN.findall(value))


def _cjk_char_tokens(value: str | None) -> set[str]:
    if not value:
        return set()
    return {ch for ch in value if "一" <= ch <= "鿿"}


def _jaccard_similarity(left: set[str], right: set[str]) -> float:
    if not left or not right:
        return 0.0
    return len(left & right) / len(left | right)


def _text_matches_target(element: dict[str, Any], target: str) -> bool:
    """Check whether *element* matches a semantic *target*."""
    norm_target = _normalize_text(target)
    target_tokens = _tokenize(target)
    target_cjk = _cjk_char_tokens(target)

    for field in ("text", "aria_label", "placeholder", "data_testid"):
        val = element.get(field)
        if not val:
            continue

        if _normalize_text(val) == norm_target:
            return True

        tokens = _tokenize(val)
        if target_tokens and target_tokens.issubset(tokens):
            return True
        if _jaccard_similarity(target_tokens, tokens) >= 0.5:
            return True

        cjk_set = _cjk_char_tokens(val)
        if target_cjk and _jaccard_similarity(target_cjk, cjk_set) >= 0.5:
            return True

    return False


# ---------------------------------------------------------------------------
# Stability scoring
# ---------------------------------------------------------------------------

def _compute_element_stability_static(element: dict[str, Any]) -> float:
    """Simplified stability score for a single element (no cross-element comparison)."""
    score = 0.30  # fallback
    if element.get("data_testid"):
        return 0.95
    eid = element.get("id") or ""
    if eid and not re.search(r"[0-9a-f]{8,}|auto\d+|tmp|rnd", eid):
        return 0.90
    if element.get("aria_label"):
        return 0.78
    if element.get("text"):
        return 0.55
    return score


# ---------------------------------------------------------------------------
# Main preflight function
# ---------------------------------------------------------------------------

def preflight_locators(
    dsl_steps: list[dict[str, Any]],
    page_elements: list[dict[str, Any]],
) -> dict[str, Any]:
    """Validate every target-bearing step against collected page elements.

    Parameters
    ----------
    dsl_steps:
        The raw ``steps`` list from a DSL case dict.
    page_elements:
        Flat list of element dicts from all explored pages.  Each element
        must have at least: ``tag``, ``text``, ``role``, ``aria_label``,
        ``placeholder``, ``data_testid``, ``css_selector``, ``id``,
        ``visible``, ``enabled``, and optionally ``page_state``,
        ``candidates``.

    Returns
    -------
    dict with:
      ``locator_confidence`` — overall "high" / "medium" / "low"
      ``step_results`` — per-step list of {step_index, target, confidence,
                         match_count, matched_elements, warnings}
      ``warnings`` — human-readable top-level warnings
    """
    step_results: list[dict[str, Any]] = []
    all_confidences: list[str] = []

    for idx, step in enumerate(dsl_steps):
        target = (step.get("target") or "").strip()
        if not target:
            continue

        strategy, parsed_value = _classify_target(target)
        matches: list[dict[str, Any]] = []

        if strategy in ("css", "xpath", "css_tag"):
            # Explicit selector: check css_selector/xpath/id match
            for el in page_elements:
                css = el.get("css_selector", "") or ""
                xp = el.get("xpath", "") or ""
                eid = el.get("id", "") or ""
                if parsed_value in css or parsed_value in xp or parsed_value == eid:
                    matches.append(el)
        elif strategy == "data-testid":
            for el in page_elements:
                if (el.get("data_testid") or "") == parsed_value:
                    matches.append(el)
        elif strategy == "chained_css_text":
            # Rough match: just check text portion
            text_part = parsed_value.split("text", 1)[-1].strip().lstrip("=").strip().strip("'\"")
            for el in page_elements:
                if _text_matches_target(el, text_part):
                    matches.append(el)
        else:
            # Semantic: text/label/placeholder matching
            for el in page_elements:
                if _text_matches_target(el, target):
                    matches.append(el)

        match_count = len(matches)

        # Determine confidence per step
        if match_count == 0:
            confidence = "low"
            warnings = [f"target \"{target}\" 在已采集的 {len(page_elements)} 个元素中未找到匹配"]
        elif match_count == 1:
            best = matches[0]
            stable = _compute_element_stability_static(best)
            if stable >= 0.70 and best.get("visible") and best.get("enabled"):
                confidence = "high"
                warnings = []
            elif best.get("visible") and best.get("enabled"):
                confidence = "medium"
                warnings = [f"target \"{target}\" 唯一匹配但稳定性不足 (stable≈{stable:.2f})"]
            else:
                confidence = "low"
                reasons = []
                if not best.get("visible"):
                    reasons.append("不可见")
                if not best.get("enabled"):
                    reasons.append("未启用")
                warnings = [f"target \"{target}\" 唯一匹配但元素{'且'.join(reasons)}"]
        elif match_count <= 3:
            confidence = "medium"
            visible_matches = [m for m in matches if m.get("visible")]
            warnings = [
                f"target \"{target}\" 匹配到 {match_count} 个元素（预期唯一），请检查是否有歧义"
            ]
            if not visible_matches:
                confidence = "low"
                warnings.append("所有匹配元素均不可见")
        else:
            confidence = "low"
            warnings = [f"target \"{target}\" 匹配到 {match_count} 个元素，歧义过高"]

        all_confidences.append(confidence)
        step_results.append({
            "step_index": idx,
            "target": target,
            "strategy": strategy,
            "confidence": confidence,
            "match_count": match_count,
            "matched_elements": [
                {
                    "tag": m.get("tag"),
                    "text": m.get("text"),
                    "css_selector": m.get("css_selector"),
                    "visible": m.get("visible"),
                    "enabled": m.get("enabled"),
                    "candidates": m.get("candidates", []),
                }
                for m in matches[:5]
            ],
            "warnings": warnings,
        })

    # Overall confidence: lowest of all steps
    if not all_confidences:
        overall = "high"
    elif "low" in all_confidences:
        overall = "low"
    elif "medium" in all_confidences:
        overall = "medium"
    else:
        overall = "high"

    top_warnings: list[str] = []
    for sr in step_results:
        for w in sr.get("warnings", []):
            top_warnings.append(f"Step {sr['step_index']}: {w}")

    return {
        "locator_confidence": overall,
        "step_results": step_results,
        "warnings": top_warnings,
    }


def apply_preflight_to_dsl(
    dsl_case: dict[str, Any],
    page_elements: list[dict[str, Any]],
) -> dict[str, Any]:
    """Run preflight and write confidence / warnings / candidates back into the DSL case dict.

    Mutates *dsl_case* in place and also returns it.
    """
    steps = dsl_case.get("steps", [])
    if not steps or not page_elements:
        return dsl_case

    result = preflight_locators(steps, page_elements)
    for sr in result.get("step_results", []):
        idx = sr["step_index"]
        if idx < len(steps):
            steps[idx]["locator_confidence"] = sr["confidence"]
            matched = sr.get("matched_elements", [])

            # Only set page_state if the matched element has one and the step doesn't
            if not steps[idx].get("page_state") and matched:
                page_state = matched[0].get("page_state")
                if page_state:
                    steps[idx]["page_state"] = page_state

            # Inject pre-scored candidates from matched elements
            if matched and not steps[idx].get("candidates"):
                candidates = _collect_candidates_from_matches(matched, sr["target"])
                if candidates:
                    steps[idx]["candidates"] = candidates

    dsl_case["_preflight"] = {
        "locator_confidence": result["locator_confidence"],
        "warnings": result["warnings"],
    }
    return dsl_case


def _collect_candidates_from_matches(
    matched: list[dict[str, Any]],
    target: str,
) -> list[dict[str, Any]]:
    """Collect and deduplicate pre-scored candidates from matched elements.

    Each element may carry a ``candidates`` list produced by
    :func:`score_candidates_for_element` during page exploration.
    We flatten, deduplicate by (strategy, selector), and sort by
    pre_score descending.
    """
    seen: set[tuple[str, str]] = set()
    flattened: list[dict[str, Any]] = []

    for element in matched:
        for candidate in element.get("candidates", []):
            strategy = candidate.get("strategy", "")
            selector = candidate.get("selector", "") or ""
            key = (strategy, selector)
            if key in seen:
                continue
            seen.add(key)
            flattened.append({
                "strategy": strategy,
                "selector": selector,
                "semantic_value": candidate.get("semantic_value"),
                "pre_score": candidate.get("pre_score", 0.0),
                "pre_features": candidate.get("pre_features"),
            })

    # Deduplicate selectors that differ only in prefix (e.g. "#login" vs "css=#login")
    deduped: list[dict[str, Any]] = []
    dedup_seen: set[str] = set()
    for candidate in sorted(flattened, key=lambda c: c["pre_score"], reverse=True):
        selector = candidate["selector"]
        normalized = selector.lstrip("#") if selector else ""
        if normalized in dedup_seen:
            continue
        dedup_seen.add(normalized)
        deduped.append(candidate)

    # Ensure tag fallbacks don't crowd out high-quality selectors — cap at 20
    return deduped[:20]
