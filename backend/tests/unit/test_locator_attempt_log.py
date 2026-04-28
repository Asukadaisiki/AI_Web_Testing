"""Unit tests for LocatorAttemptLog model."""

from app.models.locator_attempt_log import LocatorAttemptLog


def test_locator_attempt_log_creation():
    log = LocatorAttemptLog(
        run_id=1,
        project_id=1,
        step_index=0,
        step_action="click",
        target_description="Submit button",
        page_url="https://example.com/checkout",
        page_url_pattern="https://example.com/checkout",
        candidates_json="{}",
        selected_candidate="{}",
        strategy_used="role",
        fallback_tier_reached=1,
        pre_features='{"selector_stability": 0.9}',
        runtime_features='{"actionability": 0.95}',
        final_score=0.88,
        action_success=True,
        postcondition_result='{"passed": true}',
        postcondition_passed=True,
        overall_success=True,
        element_type="button",
        selector_type="role",
        domain="example.com",
        route="/checkout",
    )
    assert log.run_id == 1
    assert log.overall_success is True
    assert log.final_score == 0.88
    assert log.click_recovery_used is None
