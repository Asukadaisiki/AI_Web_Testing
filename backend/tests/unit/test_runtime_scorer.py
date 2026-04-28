"""Unit tests for RuntimeScorer."""
from app.runners.runtime_scorer import (
    RuntimeScoreFeatures,
    compute_runtime_score,
    apply_hard_rules,
    compute_final_score,
    decide_strategy,
    HARD_CAP_SCORES,
)


def test_runtime_score_all_good():
    features = RuntimeScoreFeatures(
        actionability=0.95,
        visual_consistency=0.90,
        history_success=0.80,
        rank_margin=0.30,
        overlay_risk=0.0,
    )
    score = compute_runtime_score(features)
    assert 0.75 <= score <= 1.0


def test_runtime_score_all_bad():
    features = RuntimeScoreFeatures(
        actionability=0.10,
        visual_consistency=0.10,
        history_success=0.10,
        rank_margin=0.02,
        overlay_risk=0.80,
    )
    score = compute_runtime_score(features)
    assert score < 0.4


def test_hard_rules_not_visible():
    score = apply_hard_rules(0.90, {"visible": False, "enabled": True, "bbox_area": 100, "receives_events": True})
    assert score <= HARD_CAP_SCORES["not_visible"]


def test_hard_rules_disabled():
    score = apply_hard_rules(0.90, {"visible": True, "enabled": False, "bbox_area": 100, "receives_events": True})
    assert score <= HARD_CAP_SCORES["disabled"]


def test_hard_rules_bbox_zero():
    score = apply_hard_rules(0.90, {"visible": True, "enabled": True, "bbox_area": 0, "receives_events": True})
    assert score <= HARD_CAP_SCORES["bbox_zero"]


def test_hard_rules_no_event():
    score = apply_hard_rules(0.90, {"visible": True, "enabled": True, "bbox_area": 100, "receives_events": False})
    assert score <= HARD_CAP_SCORES["not_receives_events"]


def test_compute_final_score_fusion():
    pre_features = {"selector_stability": 0.90, "semantic_match": 0.80, "uniqueness": 1.0, "context_match": 0.70}
    runtime_features = {"actionability": 0.95, "visual_consistency": 0.85, "history_success": 0.70, "rank_margin": 0.25}
    score = compute_final_score(pre_features, runtime_features)
    assert 0.7 <= score <= 1.0


def test_decide_strategy_dom_action():
    assert decide_strategy(0.90, {"visible": True, "enabled": True, "match_count": 1, "rank_margin": 0.3}) == "dom_action"


def test_decide_strategy_vlm_grounding_low_score():
    assert decide_strategy(0.30, {"visible": True, "enabled": True, "match_count": 1, "rank_margin": 0.3}) == "vlm_grounding"


def test_decide_strategy_vlm_rerank_close_margin():
    assert decide_strategy(0.70, {"visible": True, "enabled": True, "match_count": 2, "rank_margin": 0.05}) == "vlm_rerank"


def test_decide_strategy_zero_matches():
    assert decide_strategy(0.90, {"visible": True, "enabled": True, "match_count": 0, "rank_margin": 0.3}) == "vlm_grounding"
