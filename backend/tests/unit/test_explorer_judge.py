"""Tests for Explorer-Judge architecture: Router, schemas, and prompt logic."""

from __future__ import annotations

import json
import pytest

from app.schemas.explorer_judge import (
    ExplorerJudgeVerdict,
    ExplorerStepEvidence,
    ExplorationResult,
    JudgeConclusion,
    RouterDecision,
)
from app.schemas.dsl import DSLModel
from app.services.ai_planning import router_decide, build_aggregate_verdict


# --- Schema validation ---

class TestExplorerSchemas:
    def test_explorer_step_evidence_passed(self):
        e = ExplorerStepEvidence(
            step_index=0, action="goto", status="passed", duration_ms=100,
        )
        assert e.status == "passed"
        assert e.error_message is None

    def test_explorer_step_evidence_failed(self):
        e = ExplorerStepEvidence(
            step_index=2, action="click", status="failed",
            error_message="Element not found",
        )
        assert e.status == "failed"
        assert e.error_message == "Element not found"

    def test_explorer_step_evidence_cascade_blocked(self):
        e = ExplorerStepEvidence(
            step_index=3, action="input", status="cascade_blocked",
            error_message="Previous step failure left page in inconsistent state",
        )
        assert e.status == "cascade_blocked"

    def test_exploration_result(self):
        r = ExplorationResult(
            total_steps=5, passed_steps=3, failed_steps=1, cascade_blocked_steps=1,
        )
        assert r.total_steps == 5
        assert r.passed_steps == 3

    def test_judge_conclusion(self):
        c = JudgeConclusion(
            step_index=1,
            classification="product_defect",
            confidence="high",
            root_cause_analysis="Button does not respond to click",
            reproduction_path="1. Open page 2. Click submit 3. Observe no response",
            suggested_action="report_bug",
            is_product_bug=True,
        )
        assert c.is_product_bug is True
        assert c.classification == "product_defect"

    def test_router_decision(self):
        d = RouterDecision(action="report_to_user", reason="Product defect confirmed")
        assert d.action == "report_to_user"
        assert d.retry_remaining == 0

    def test_verdict_all_passed(self):
        v = ExplorerJudgeVerdict(
            case_id=1,
            test_point_status="all_passed",
            total_steps=5,
            passed_steps=5,
        )
        assert v.failed_steps == 0
        assert v.conclusions == []


# --- Router decision logic ---

class TestRouterDecision:
    def _make_verdict(
        self,
        *,
        is_product_bug=False,
        manual_intervention=False,
        conclusions=None,
    ) -> ExplorerJudgeVerdict:
        return ExplorerJudgeVerdict(
            case_id=1,
            test_point_status="has_defects",
            total_steps=5,
            passed_steps=3,
            failed_steps=2,
            is_suspected_product_bug=is_product_bug,
            manual_intervention_needed=manual_intervention,
            conclusions=conclusions or [],
        )

    def test_product_bug_reports_to_user(self):
        verdict = self._make_verdict(is_product_bug=True)
        decision = router_decide(verdict, auto_fix_already_attempted=False)
        assert decision.action == "report_to_user"
        assert "产品缺陷" in decision.reason

    def test_human_judgment_reports_to_user(self):
        verdict = self._make_verdict(manual_intervention=True)
        decision = router_decide(verdict, auto_fix_already_attempted=False)
        assert decision.action == "report_to_user"
        assert "人工" in decision.reason

    def test_test_design_error_triggers_auto_fix(self):
        conclusions = [
            JudgeConclusion(
                step_index=1,
                classification="test_design_error",
                confidence="high",
                root_cause_analysis="Wrong element selector",
                reproduction_path="1. Open page 2. Look for element",
                suggested_action="regenerate_dsl",
            )
        ]
        verdict = self._make_verdict(conclusions=conclusions)
        decision = router_decide(verdict, auto_fix_already_attempted=False)
        assert decision.action == "auto_fix_dsl"
        assert decision.retry_remaining == 1

    def test_test_design_error_no_retry_after_attempt(self):
        conclusions = [
            JudgeConclusion(
                step_index=1,
                classification="test_design_error",
                confidence="high",
                root_cause_analysis="Wrong element selector",
                reproduction_path="1. Open page 2. Look for element",
                suggested_action="regenerate_dsl",
            )
        ]
        verdict = self._make_verdict(conclusions=conclusions)
        decision = router_decide(verdict, auto_fix_already_attempted=True)
        assert decision.action == "report_to_user"

    def test_environment_issue_reports_to_user(self):
        conclusions = [
            JudgeConclusion(
                step_index=0,
                classification="environment_dependency",
                confidence="high",
                root_cause_analysis="Page returns 503",
                reproduction_path="1. Open URL 2. Check response",
                suggested_action="skip_environment",
            )
        ]
        verdict = self._make_verdict(conclusions=conclusions)
        decision = router_decide(verdict, auto_fix_already_attempted=False)
        assert decision.action == "report_to_user"
        assert "环境" in decision.reason

    def test_default_reports_to_user(self):
        conclusions = [
            JudgeConclusion(
                step_index=2,
                classification="automation_implementation",
                confidence="medium",
                root_cause_analysis="Wait timeout too short",
                reproduction_path="1. Open page 2. Wait for element",
                suggested_action="fix_automation",
            )
        ]
        verdict = self._make_verdict(conclusions=conclusions)
        decision = router_decide(verdict, auto_fix_already_attempted=False)
        assert decision.action == "report_to_user"

    def test_priority_product_bug_over_test_design_error(self):
        conclusions = [
            JudgeConclusion(
                step_index=0,
                classification="test_design_error",
                confidence="high",
                root_cause_analysis="Wrong element",
                reproduction_path="1. Open page",
                suggested_action="regenerate_dsl",
            ),
            JudgeConclusion(
                step_index=3,
                classification="product_defect",
                confidence="high",
                root_cause_analysis="Button broken",
                reproduction_path="1. Click button",
                suggested_action="report_bug",
                is_product_bug=True,
            ),
        ]
        verdict = self._make_verdict(is_product_bug=True, conclusions=conclusions)
        decision = router_decide(verdict, auto_fix_already_attempted=False)
        assert decision.action == "report_to_user"
        assert "产品缺陷" in decision.reason


# --- Build aggregate verdict ---

class TestBuildVerdict:
    def test_all_passed_status(self):
        result = ExplorationResult(total_steps=3, passed_steps=3, failed_steps=0)
        verdict = build_aggregate_verdict(result, {
            "conclusions": [],
            "aggregate": {},
        }, case_id=1)
        assert verdict.test_point_status == "all_passed"

    def test_product_defect_status(self):
        result = ExplorationResult(total_steps=3, passed_steps=1, failed_steps=2)
        verdict = build_aggregate_verdict(result, {
            "conclusions": [
                {"step_index": 1, "classification": "product_defect", "confidence": "high",
                 "root_cause_analysis": "bug", "reproduction_path": "repro",
                 "suggested_action": "report_bug", "is_product_bug": True},
            ],
            "aggregate": {},
        }, case_id=1)
        assert verdict.test_point_status == "has_defects"

    def test_environment_blocked_status(self):
        result = ExplorationResult(total_steps=2, passed_steps=0, failed_steps=2)
        verdict = build_aggregate_verdict(result, {
            "conclusions": [
                {"step_index": 0, "classification": "environment_dependency", "confidence": "high",
                 "root_cause_analysis": "503", "reproduction_path": "repro",
                 "suggested_action": "skip_environment"},
            ],
            "aggregate": {},
        }, case_id=1)
        assert verdict.test_point_status == "environment_blocked"

    def test_aggregate_fields_populated(self):
        result = ExplorationResult(total_steps=5, passed_steps=3, failed_steps=2)
        verdict = build_aggregate_verdict(result, {
            "conclusions": [],
            "aggregate": {
                "first_failed_step": 2,
                "failure_phenomenon": "Button not responding",
                "is_suspected_product_bug": True,
                "regression_recommended": True,
                "manual_intervention_needed": False,
            },
        }, case_id=42)
        assert verdict.first_failed_step == 2
        assert verdict.failure_phenomenon == "Button not responding"
        assert verdict.is_suspected_product_bug is True
        assert verdict.regression_recommended is True
        assert verdict.case_id == 42


# --- Judge prompt builder ---

class TestJudgePromptBuilder:
    def test_builds_valid_prompt(self):
        from app.ai.judge_prompts import build_judge_user_prompt
        records = [
            ExplorerStepEvidence(
                step_index=0, action="goto", status="failed",
                error_message="Page not found", url="https://example.com",
            ),
            ExplorerStepEvidence(
                step_index=3, action="click", status="failed",
                error_message="Button not found", target="#submit",
            ),
        ]
        prompt = build_judge_user_prompt(records, case_name="Login Test")
        assert "Login Test" in prompt
        assert "Page not found" in prompt
        assert "Button not found" in prompt
        assert "共 2 条" in prompt

    def test_empty_records(self):
        from app.ai.judge_prompts import build_judge_user_prompt
        prompt = build_judge_user_prompt([])
        assert "共 0 条" in prompt


# --- Judge response parser ---

class TestJudgeResponseParser:
    def test_parse_valid_response(self):
        from app.ai.judge_agent import parse_judge_response
        response = json.dumps({
            "conclusions": [
                {"step_index": 0, "classification": "test_design_error",
                 "confidence": "high", "root_cause_analysis": "bad selector",
                 "reproduction_path": "repro", "suggested_action": "regenerate_dsl"},
            ],
            "aggregate": {"first_failed_step": 0},
        })
        result = parse_judge_response(response)
        assert len(result["conclusions"]) == 1
        assert result["aggregate"]["first_failed_step"] == 0

    def test_parse_missing_conclusions_raises(self):
        from app.ai.judge_agent import parse_judge_response
        with pytest.raises(RuntimeError, match="conclusions"):
            parse_judge_response(json.dumps({"aggregate": {}}))

    def test_parse_missing_aggregate_raises(self):
        from app.ai.judge_agent import parse_judge_response
        with pytest.raises(RuntimeError, match="aggregate"):
            parse_judge_response(json.dumps({"conclusions": []}))

    def test_parse_invalid_json_raises(self):
        from app.ai.judge_agent import parse_judge_response
        with pytest.raises(RuntimeError, match="JSON"):
            parse_judge_response("not valid json {{{")
