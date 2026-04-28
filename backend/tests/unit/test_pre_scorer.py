"""Unit tests for PreScorer."""
from app.runners.pre_scorer import (
    DOM_FEATURE_PRIORS,
    ELEMENT_TYPE_SCORES,
    compute_pre_score,
    detect_fragility,
    score_candidates_for_element,
    PreScoreFeatures,
)


def test_dom_feature_priors_data_testid():
    assert DOM_FEATURE_PRIORS["data-testid"] == 0.95


def test_dom_feature_priors_role():
    assert DOM_FEATURE_PRIORS["role"] == 0.90


def test_dom_feature_priors_absolute_xpath():
    assert DOM_FEATURE_PRIORS["absolute_xpath"] == 0.10


def test_element_type_scores_button():
    assert ELEMENT_TYPE_SCORES["button"]["dom"] == 0.90
    assert ELEMENT_TYPE_SCORES["button"]["vlm"] == 0.25


def test_element_type_scores_canvas():
    assert ELEMENT_TYPE_SCORES["canvas"]["dom"] == 0.10
    assert ELEMENT_TYPE_SCORES["canvas"]["vlm"] == 0.90


def test_detect_fragility_dynamic_id():
    flags = detect_fragility("#user-abc12345-def", {"id": "user-abc12345-def"})
    assert "dynamic_id" in flags


def test_detect_fragility_css_module_hash():
    flags = detect_fragility(".btn_abc12", {"class": "btn_abc12"})
    assert "css_module_hash" in flags


def test_detect_fragility_deep_xpath():
    flags = detect_fragility("/html/body/div/div/div/div/div/button", {"id": None})
    assert "deep_xpath" in flags


def test_detect_fragility_nth_child():
    flags = detect_fragility("div:nth-child(2)", {"id": None})
    assert "nth_child" in flags


def test_detect_fragility_clean():
    flags = detect_fragility(".checkout-submit", {"id": "checkout-submit", "class": "checkout-submit"})
    assert flags == []


def test_compute_pre_score_high():
    features = PreScoreFeatures(
        selector_stability=0.90,
        semantic_match=0.95,
        uniqueness=1.0,
        context_match=0.80,
        fragility_flags=[],
    )
    score = compute_pre_score(features)
    assert 0.80 <= score <= 1.0


def test_compute_pre_score_low():
    features = PreScoreFeatures(
        selector_stability=0.10,
        semantic_match=0.20,
        uniqueness=0.3,
        context_match=0.2,
        fragility_flags=["dynamic_id", "deep_xpath", "nth_child"],
    )
    score = compute_pre_score(features)
    assert score < 0.4


def test_score_candidates_for_element():
    element = {
        "tag": "button",
        "text": "Submit",
        "role": "button",
        "aria_label": None,
        "data_testid": None,
        "css_selector": "button.submit-btn",
        "xpath": "/html/body/main/button",
    }
    candidates = score_candidates_for_element(element, intent="Submit")
    assert len(candidates) >= 2
    role_cand = next((c for c in candidates if c["strategy"] == "role"), None)
    xpath_cand = next((c for c in candidates if c["strategy"] == "xpath"), None)
    if role_cand and xpath_cand:
        assert role_cand["pre_score"] > xpath_cand["pre_score"]
