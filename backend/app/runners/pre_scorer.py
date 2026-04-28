"""PreScorer — generation-time DOM element scoring.

Scores DOM elements during page exploration by generating multiple locator
candidates per element and computing pre-scores across 4 dimensions:
selector_stability, semantic_match, uniqueness, and context_match.
"""
from __future__ import annotations

import re
from dataclasses import dataclass, field
from typing import Any


# ---------------------------------------------------------------------------
# 1. DOM feature priors — 17 selector types with prior stability scores
# ---------------------------------------------------------------------------

DOM_FEATURE_PRIORS: dict[str, float] = {
    "data-testid": 0.95,
    "role": 0.90,
    "label": 0.85,
    "stable_id": 0.80,
    "name_type": 0.75,
    "aria_label": 0.75,
    "placeholder": 0.72,
    "text": 0.70,
    "href": 0.68,
    "short_css": 0.60,
    "stable_class": 0.55,
    "relative_xpath": 0.50,
    "bbox": 0.50,
    "tag": 0.45,
    "nth_child": 0.25,
    "canvas_svg": 0.15,
    "absolute_xpath": 0.10,
}


# ---------------------------------------------------------------------------
# 2. Element type scores — 13 element types with dom / vlm scores
# ---------------------------------------------------------------------------

ELEMENT_TYPE_SCORES: dict[str, dict[str, float]] = {
    "button": {"dom": 0.90, "vlm": 0.25},
    "a": {"dom": 0.85, "vlm": 0.30},
    "input": {"dom": 0.80, "vlm": 0.35},
    "select": {"dom": 0.80, "vlm": 0.30},
    "textarea": {"dom": 0.80, "vlm": 0.30},
    "label": {"dom": 0.85, "vlm": 0.25},
    "h1": {"dom": 0.85, "vlm": 0.20},
    "h2": {"dom": 0.80, "vlm": 0.25},
    "h3": {"dom": 0.75, "vlm": 0.30},
    "img": {"dom": 0.50, "vlm": 0.80},
    "canvas": {"dom": 0.10, "vlm": 0.90},
    "svg": {"dom": 0.20, "vlm": 0.85},
    "div": {"dom": 0.40, "vlm": 0.60},
}


# ---------------------------------------------------------------------------
# 3. PreScoreFeatures dataclass
# ---------------------------------------------------------------------------

@dataclass
class PreScoreFeatures:
    """Feature vector used to compute a pre-score for a locator candidate."""
    selector_stability: float
    semantic_match: float
    uniqueness: float
    context_match: float
    fragility_flags: list[str] = field(default_factory=list)


# ---------------------------------------------------------------------------
# 4. Fragility detection
# ---------------------------------------------------------------------------

# Hex-like suffix pattern: 5+ consecutive hex chars at end of a token
_HEX_SUFFIX_RE = re.compile(r"[0-9a-f]{5,}$", re.IGNORECASE)
# Digits-only suffix: 6+ digits at end
_LONG_DIGIT_SUFFIX_RE = re.compile(r"\d{6,}$")
# Deep xpath: count segments
_DEEP_XPATH_THRESHOLD = 5  # more than this many segments => deep


def detect_fragility(selector: str, element_attrs: dict[str, Any]) -> list[str]:
    """Return a list of fragility flags for *selector* given *element_attrs*.

    Flags:
      - ``dynamic_id`` — id contains a long hex or digit suffix.
      - ``css_module_hash`` — class contains a short underscore-separated hash.
      - ``deep_xpath`` — absolute xpath with many segments.
      - ``nth_child`` — selector uses ``:nth-child()``.
    """
    flags: list[str] = []

    # --- dynamic_id ---
    id_val = element_attrs.get("id")
    if id_val and isinstance(id_val, str):
        # Look for long hex suffix or long digit suffix
        parts = re.split(r"[-_]", id_val)
        for part in parts:
            if _HEX_SUFFIX_RE.search(part) or _LONG_DIGIT_SUFFIX_RE.search(part):
                flags.append("dynamic_id")
                break

    # --- css_module_hash ---
    class_val = element_attrs.get("class")
    if class_val and isinstance(class_val, str):
        # CSS module hashes: className followed by underscore + 5-char hash
        tokens = class_val.strip().split()
        for token in tokens:
            if re.match(r".+_[0-9a-zA-Z]{4,7}$", token) and not re.match(
                r".+_(?:active|disabled|selected|hover|focus|open|closed|visible|hidden|loading|error|success|info|warning|dark|light|sm|md|lg|xl)$",
                token,
                re.IGNORECASE,
            ):
                flags.append("css_module_hash")
                break

    # --- deep_xpath ---
    if selector.startswith("/") or selector.startswith("(/"):
        segments = [s for s in selector.split("/") if s]
        if len(segments) > _DEEP_XPATH_THRESHOLD:
            flags.append("deep_xpath")

    # --- nth_child ---
    if ":nth-child(" in selector or ":nth-of-type(" in selector:
        flags.append("nth_child")

    return flags


# ---------------------------------------------------------------------------
# 5. compute_pre_score
# ---------------------------------------------------------------------------

_FRAGILITY_PENALTY = 0.15  # per flag, applied to context_match


def compute_pre_score(features: PreScoreFeatures) -> float:
    """Weighted sum with fragility penalty on context_match.

    Weights: stability 0.40, semantic 0.30, uniqueness 0.20, context 0.10.
    Each fragility flag reduces context_match by 0.15 (floored at 0.0).
    """
    context_adj = max(
        0.0,
        features.context_match - len(features.fragility_flags) * _FRAGILITY_PENALTY,
    )
    score = (
        features.selector_stability * 0.40
        + features.semantic_match * 0.30
        + features.uniqueness * 0.20
        + context_adj * 0.10
    )
    return round(score, 4)


# ---------------------------------------------------------------------------
# 6. Semantic match helper
# ---------------------------------------------------------------------------


def _compute_semantic_match(element: dict[str, Any], intent: str) -> float:
    """Match element text / aria / placeholder / role against *intent*.

    Returns a value in [0.0, 1.0].
    """
    if not intent:
        return 0.5  # neutral when no intent provided

    intent_lower = intent.lower().strip()
    if not intent_lower:
        return 0.5

    fields: list[str] = []
    for key in ("text", "aria_label", "aria-label", "placeholder", "role"):
        val = element.get(key)
        if val and isinstance(val, str):
            fields.append(val.lower())

    if not fields:
        return 0.0

    best = 0.0
    for field_val in fields:
        # Exact match
        if field_val == intent_lower:
            return 1.0
        # Substring match
        if intent_lower in field_val or field_val in intent_lower:
            best = max(best, 0.75)
            continue
        # Word-level overlap
        intent_words = set(intent_lower.split())
        field_words = set(field_val.split())
        if intent_words and field_words:
            overlap = len(intent_words & field_words) / max(
                len(intent_words), len(field_words)
            )
            best = max(best, overlap * 0.60)

    return best


# ---------------------------------------------------------------------------
# 7. score_candidates_for_element
# ---------------------------------------------------------------------------


def _make_candidate(
    strategy: str,
    selector: str,
    stability: float,
    semantic: float,
    element: dict[str, Any],
) -> dict[str, Any]:
    """Build a candidate dict with pre_features and computed pre_score."""
    fragility = detect_fragility(selector, element)
    # Uniqueness heuristic: role/label/id-based selectors tend to be more unique
    uniqueness_map: dict[str, float] = {
        "data-testid": 1.0,
        "role": 0.80,
        "aria_label": 0.85,
        "label": 0.85,
        "stable_id": 0.95,
        "text": 0.70,
        "css_selector": 0.60,
        "xpath": 0.40,
        "name_type": 0.75,
        "placeholder": 0.70,
        "href": 0.55,
        "tag": 0.30,
    }
    uniqueness = uniqueness_map.get(strategy, 0.50)

    features = PreScoreFeatures(
        selector_stability=stability,
        semantic_match=semantic,
        uniqueness=uniqueness,
        context_match=stability,  # context_match tracks stability as baseline
        fragility_flags=fragility,
    )
    return {
        "strategy": strategy,
        "selector": selector,
        "pre_score": compute_pre_score(features),
        "pre_features": {
            "selector_stability": features.selector_stability,
            "semantic_match": features.semantic_match,
            "uniqueness": features.uniqueness,
            "context_match": features.context_match,
            "fragility_flags": features.fragility_flags,
        },
    }


def score_candidates_for_element(
    element: dict[str, Any],
    intent: str = "",
) -> list[dict[str, Any]]:
    """Generate all possible locator candidates for *element*, score each,
    and return sorted by pre_score descending.
    """
    candidates: list[dict[str, Any]] = []

    tag = element.get("tag", "div")
    text = element.get("text")
    role = element.get("role")
    aria_label = element.get("aria_label") or element.get("aria-label")
    data_testid = element.get("data_testid") or element.get("data-testid")
    css_selector = element.get("css_selector") or element.get("css")
    xpath = element.get("xpath")
    name = element.get("name")
    placeholder = element.get("placeholder")
    href = element.get("href")
    element_id = element.get("id")

    # --- data-testid ---
    if data_testid:
        candidates.append(
            _make_candidate(
                "data-testid",
                f"[data-testid='{data_testid}']",
                DOM_FEATURE_PRIORS["data-testid"],
                _compute_semantic_match(element, intent),
                element,
            )
        )

    # --- role ---
    if role:
        candidates.append(
            _make_candidate(
                "role",
                f"[role='{role}']",
                DOM_FEATURE_PRIORS["role"],
                _compute_semantic_match(element, intent),
                element,
            )
        )

    # --- aria_label ---
    if aria_label:
        candidates.append(
            _make_candidate(
                "aria_label",
                f"[aria-label='{aria_label}']",
                DOM_FEATURE_PRIORS["aria_label"],
                _compute_semantic_match(element, intent),
                element,
            )
        )

    # --- stable_id ---
    if element_id:
        candidates.append(
            _make_candidate(
                "stable_id",
                f"#{element_id}",
                DOM_FEATURE_PRIORS["stable_id"],
                _compute_semantic_match(element, intent),
                element,
            )
        )

    # --- name_type ---
    if name and tag:
        candidates.append(
            _make_candidate(
                "name_type",
                f"{tag}[name='{name}']",
                DOM_FEATURE_PRIORS["name_type"],
                _compute_semantic_match(element, intent),
                element,
            )
        )

    # --- placeholder ---
    if placeholder:
        candidates.append(
            _make_candidate(
                "placeholder",
                f"[placeholder='{placeholder}']",
                DOM_FEATURE_PRIORS["placeholder"],
                _compute_semantic_match(element, intent),
                element,
            )
        )

    # --- text ---
    if text and len(text.strip()) > 0 and len(text.strip()) < 80:
        candidates.append(
            _make_candidate(
                "text",
                f"{tag}:text('{text.strip()}')",
                DOM_FEATURE_PRIORS["text"],
                _compute_semantic_match(element, intent),
                element,
            )
        )

    # --- href ---
    if href and tag == "a":
        candidates.append(
            _make_candidate(
                "href",
                f"a[href='{href}']",
                DOM_FEATURE_PRIORS["href"],
                _compute_semantic_match(element, intent),
                element,
            )
        )

    # --- css_selector ---
    if css_selector:
        candidates.append(
            _make_candidate(
                "css_selector",
                css_selector,
                DOM_FEATURE_PRIORS["short_css"],
                _compute_semantic_match(element, intent),
                element,
            )
        )

    # --- xpath ---
    if xpath:
        prior = (
            DOM_FEATURE_PRIORS["absolute_xpath"]
            if xpath.startswith("/")
            else DOM_FEATURE_PRIORS["relative_xpath"]
        )
        candidates.append(
            _make_candidate(
                "xpath",
                xpath,
                prior,
                _compute_semantic_match(element, intent),
                element,
            )
        )

    # --- tag fallback ---
    candidates.append(
        _make_candidate(
            "tag",
            tag,
            DOM_FEATURE_PRIORS["tag"],
            _compute_semantic_match(element, intent),
            element,
        )
    )

    # Sort by pre_score descending
    candidates.sort(key=lambda c: c["pre_score"], reverse=True)
    return candidates
