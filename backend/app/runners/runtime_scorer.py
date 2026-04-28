"""RuntimeScorer — runtime scoring for locator confidence.

Supplements generation-time scores with runtime features (actionability,
visual_consistency, history_success, rank_margin) and applies hard rules
(score caps) and strategy decisions.
"""

from __future__ import annotations

from dataclasses import dataclass

# ---------------------------------------------------------------------------
# 8-dimension global weights (generation-time + runtime)
# ---------------------------------------------------------------------------
SCORING_WEIGHTS: dict[str, float] = {
    "selector_stability": 0.20,
    "semantic_match": 0.20,
    "uniqueness": 0.15,
    "actionability": 0.15,
    "context_match": 0.10,
    "visual_consistency": 0.10,
    "history_success": 0.05,
    "rank_margin": 0.05,
}

PRE_SCORE_DIMENSIONS: set[str] = {
    "selector_stability",
    "semantic_match",
    "uniqueness",
    "context_match",
}

RUNTIME_SCORE_DIMENSIONS: set[str] = {
    "actionability",
    "visual_consistency",
    "history_success",
    "rank_margin",
}

# ---------------------------------------------------------------------------
# Hard cap scores — maximum score allowed when a hard-rule violation is detected
# ---------------------------------------------------------------------------
HARD_CAP_SCORES: dict[str, float] = {
    "not_visible": 0.40,
    "disabled": 0.30,
    "bbox_zero": 0.20,
    "not_receives_events": 0.45,
}

# ---------------------------------------------------------------------------
# Thresholds for strategy decisions
# ---------------------------------------------------------------------------
THRESHOLD_HIGH: float = 0.85
THRESHOLD_MEDIUM: float = 0.65
THRESHOLD_LOW: float = 0.45
RANK_MARGIN_VLM_TRIGGER: float = 0.08


# ---------------------------------------------------------------------------
# Runtime feature dataclass
# ---------------------------------------------------------------------------
@dataclass(frozen=True)
class RuntimeScoreFeatures:
    """Features computed at runtime for scoring locator confidence."""

    actionability: float  # 0-1: element is interactable
    visual_consistency: float  # 0-1: visual match to expected appearance
    history_success: float  # 0-1: historical success rate for this selector
    rank_margin: float  # 0-1: gap between top candidate and runner-up
    overlay_risk: float = 0.0  # 0-1: risk of overlay遮挡 (default 0.0)


# ---------------------------------------------------------------------------
# Runtime score computation
# ---------------------------------------------------------------------------
def compute_runtime_score(features: RuntimeScoreFeatures) -> float:
    """Compute a weighted runtime score from runtime features.

    Weights:
        actionability      0.30
        visual_consistency 0.25
        history_success    0.20
        rank_margin        0.15  (normalized to 0-1)
        (1 - overlay_risk) 0.10
    """
    # Normalize rank_margin to 0-1 range (already 0-1 from dataclass, but clamp)
    normalized_margin = max(0.0, min(1.0, features.rank_margin))

    score = (
        features.actionability * 0.30
        + features.visual_consistency * 0.25
        + features.history_success * 0.20
        + normalized_margin * 0.15
        + (1.0 - features.overlay_risk) * 0.10
    )
    return max(0.0, min(1.0, score))


# ---------------------------------------------------------------------------
# Hard rules — cap score based on element state
# ---------------------------------------------------------------------------
def apply_hard_rules(score: float, state: dict) -> float:
    """Apply hard-rule caps based on element runtime state.

    state keys:
        visible (bool)         — element is visible
        enabled (bool)         — element is enabled (not disabled)
        bbox_area (int/float)  — bounding box area in pixels
        receives_events (bool) — element can receive pointer events
    """
    if not state.get("visible", True):
        score = min(score, HARD_CAP_SCORES["not_visible"])

    if not state.get("enabled", True):
        score = min(score, HARD_CAP_SCORES["disabled"])

    if state.get("bbox_area", 1) == 0:
        score = min(score, HARD_CAP_SCORES["bbox_zero"])

    if not state.get("receives_events", True):
        score = min(score, HARD_CAP_SCORES["not_receives_events"])

    return score


# ---------------------------------------------------------------------------
# Final score — fuse pre-score and runtime features
# ---------------------------------------------------------------------------
def compute_final_score(
    pre_features: dict[str, float],
    runtime_features: dict[str, float],
) -> float:
    """Compute final score by fusing pre-generation and runtime features.

    Uses SCORING_WEIGHTS to combine all 8 dimensions, then applies hard
    rules from runtime_features.get("_hard_overrides", {}).
    """
    all_features: dict[str, float] = {}
    all_features.update(pre_features)
    all_features.update(runtime_features)

    # Compute weighted sum using only known dimensions
    weighted_sum = 0.0
    weight_total = 0.0
    for dim, weight in SCORING_WEIGHTS.items():
        value = all_features.get(dim, 0.0)
        weighted_sum += value * weight
        weight_total += weight

    # Normalize by actual weight total (in case some dimensions are missing)
    score = weighted_sum / weight_total if weight_total > 0 else 0.0
    score = max(0.0, min(1.0, score))

    # Apply hard rules from runtime overrides
    hard_overrides = runtime_features.get("_hard_overrides", {})
    if hard_overrides:
        score = apply_hard_rules(score, hard_overrides)

    return score


# ---------------------------------------------------------------------------
# Strategy decision
# ---------------------------------------------------------------------------
def decide_strategy(final_score: float, runtime_state: dict) -> str:
    """Decide the locator strategy based on final score and runtime state.

    Returns one of:
        dom_action              — high confidence, single match, good margin
        dom_action_strong_verify — medium-high confidence, verify recommended
        vlm_rerank              — multiple matches with close margin
        vlm_grounding           — low score or zero matches
        vlm_or_repair           — very low score, needs repair
    """
    visible = runtime_state.get("visible", True)
    enabled = runtime_state.get("enabled", True)
    match_count = runtime_state.get("match_count", 1)
    rank_margin = runtime_state.get("rank_margin", 0.0)

    # Zero matches — must use VLM grounding
    if match_count == 0:
        return "vlm_grounding"

    # Element not visible or not enabled — need VLM grounding
    if not visible or not enabled:
        return "vlm_grounding"

    # Low score below medium threshold — use VLM grounding
    if final_score < THRESHOLD_MEDIUM:
        return "vlm_grounding"

    # Multiple matches with close margin — rerank with VLM
    if match_count > 1 and rank_margin < RANK_MARGIN_VLM_TRIGGER:
        return "vlm_rerank"

    # Medium-high score — dom_action with strong verification
    if final_score < THRESHOLD_HIGH:
        return "dom_action_strong_verify"

    # High confidence, single match, good margin
    return "dom_action"
