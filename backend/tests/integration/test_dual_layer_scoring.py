"""Integration test for dual-layer locator scoring end-to-end."""
import pytest
from app.schemas.dsl import DSLCase, ClickStep, InputStep, LocatorCandidate, Postcondition
from app.runners.pre_scorer import score_candidates_for_element, compute_pre_score, PreScoreFeatures
from app.runners.runtime_scorer import compute_final_score, decide_strategy, apply_hard_rules
from app.runners.postcondition_verifier import verify_default_postcondition, PostconditionResult
from app.models.locator_attempt_log import LocatorAttemptLog


class TestDSLWithCandidates:
    def test_dsl_parse_with_candidates(self):
        case = DSLCase(
            name="Test with candidates",
            steps=[
                {"action": "goto", "value": "https://example.com"},
                {
                    "action": "click",
                    "target": "More information",
                    "candidates": [
                        {"strategy": "text", "selector": "More information", "pre_score": 0.65,
                         "pre_features": {"selector_stability": 0.70, "semantic_match": 0.80, "uniqueness": 0.50, "context_match": 0.60}},
                        {"strategy": "vlm", "semantic_value": "More information link", "pre_score": 0.0},
                    ],
                    "postconditions": [
                        {"type": "url_changes"},
                    ],
                },
            ],
        )
        click_step = case.steps[1]
        assert len(click_step.candidates) == 2
        assert click_step.candidates[0].strategy == "text"
        assert click_step.candidates[1].strategy == "vlm"
        assert len(click_step.postconditions) == 1
        assert click_step.postconditions[0].type == "url_changes"

    def test_dsl_without_candidates_backward_compatible(self):
        case = DSLCase(
            name="Legacy test",
            steps=[
                {"action": "goto", "value": "https://example.com"},
                {"action": "click", "target": "Submit"},
            ],
        )
        click_step = case.steps[1]
        assert click_step.candidates == []
        assert click_step.postconditions == []

    def test_input_step_with_candidates(self):
        case = DSLCase(
            name="Input test",
            steps=[
                {"action": "goto", "value": "https://example.com"},
                {
                    "action": "input",
                    "target": "Email",
                    "value": "test@example.com",
                    "candidates": [
                        {"strategy": "label", "selector": "[aria-label='Email']", "pre_score": 0.78},
                    ],
                    "postconditions": [
                        {"type": "value_changed"},
                    ],
                },
            ],
        )
        input_step = case.steps[1]
        assert len(input_step.candidates) == 1
        assert input_step.candidates[0].strategy == "label"
        assert len(input_step.postconditions) == 1


class TestScoringPipeline:
    def test_pre_score_then_final_score(self):
        """Test the full scoring pipeline: element -> pre_score -> final_score -> strategy."""
        element = {
            "tag": "button",
            "text": "Submit Order",
            "role": "button",
            "aria_label": None,
            "placeholder": None,
            "data_testid": "submit-order",
            "css_selector": "button.btn-primary",
            "xpath": "/html/body/main/form/button",
        }
        candidates = score_candidates_for_element(element, intent="Submit Order")
        assert len(candidates) >= 2
        # data-testid candidate should have highest score
        top = candidates[0]
        assert top["pre_score"] > 0.5
        assert "pre_features" in top

        # Simulate runtime scoring
        pre_features = top["pre_features"]
        runtime_features = {
            "actionability": 0.95,
            "visual_consistency": 0.85,
            "history_success": 0.70,
            "rank_margin": 0.30,
        }
        final_score = compute_final_score(pre_features, runtime_features)
        assert final_score > 0.6

        strategy = decide_strategy(final_score, {"visible": True, "enabled": True, "match_count": 1, "rank_margin": 0.30})
        assert strategy == "dom_action"

    def test_low_score_triggers_vlm(self):
        """Test that a low final score triggers VLM fallback."""
        pre_features = {"selector_stability": 0.10, "semantic_match": 0.15, "uniqueness": 0.3, "context_match": 0.2}
        runtime_features = {"actionability": 0.30, "visual_consistency": 0.20, "history_success": 0.20, "rank_margin": 0.05}
        final_score = compute_final_score(pre_features, runtime_features)
        assert final_score < 0.45

        strategy = decide_strategy(final_score, {"visible": True, "enabled": True, "match_count": 1, "rank_margin": 0.05})
        assert strategy == "vlm_grounding"

    def test_hard_rules_cap_invisible_element(self):
        """Test that invisible elements get capped regardless of other scores."""
        pre_features = {"selector_stability": 0.95, "semantic_match": 0.95, "uniqueness": 1.0, "context_match": 0.90}
        runtime_features = {"actionability": 0.95, "visual_consistency": 0.90, "history_success": 0.90, "rank_margin": 0.40, "_hard_overrides": {"visible": False, "enabled": True, "bbox_area": 0, "receives_events": True}}
        final_score = compute_final_score(pre_features, runtime_features)
        assert final_score <= 0.40


class TestPostconditionPipeline:
    def test_default_postcondition_detects_url_change(self):
        assert verify_default_postcondition(
            {"url": "https://example.com/form"},
            {"url": "https://example.com/success"},
        )

    def test_default_postcondition_detects_dom_change(self):
        assert verify_default_postcondition(
            {"url": "https://example.com/page", "dom_hash": "abc"},
            {"url": "https://example.com/page", "dom_hash": "def"},
        )

    def test_default_postcondition_no_change(self):
        assert not verify_default_postcondition(
            {"url": "https://example.com/page", "dom_hash": "abc"},
            {"url": "https://example.com/page", "dom_hash": "abc"},
        )


class TestLocatorAttemptLog:
    def test_log_model_stores_full_attempt(self):
        log = LocatorAttemptLog(
            run_id=1,
            project_id=1,
            step_index=0,
            step_action="click",
            target_description="Submit",
            page_url="https://example.com/form",
            page_url_pattern="https://example.com/form",
            candidates_json='[{"strategy": "role", "pre_score": 0.87}]',
            selected_candidate='{"strategy": "role", "final_score": 0.85}',
            strategy_used="role",
            fallback_tier_reached=1,
            pre_features='{"selector_stability": 0.9}',
            runtime_features='{"actionability": 0.95}',
            final_score=0.85,
            action_success=True,
            postcondition_result='{"passed": true}',
            postcondition_passed=True,
            overall_success=True,
            element_type="button",
            selector_type="role",
            domain="example.com",
            route="/form",
        )
        assert log.overall_success is True
        assert log.final_score == 0.85
