"""Unit tests for Phase 3: cross-session persistence, insights tools, improved flaky detection."""
from __future__ import annotations

import pytest
from sqlalchemy.orm import Session

from app.schemas.cases import CaseCreateRequest


class TestTestPointInsightModel:
    """Tests for the TestPointInsight model and basic CRUD."""

    def test_create_insight_for_project(self, db_session: Session) -> None:
        from app.models import TestPointInsight

        insight = TestPointInsight(
            project_id=1,
            flaky_case_ids=[10, 20],
            failure_patterns={"locator_stale": {"count": 3, "cases": [10]}},
            regression_risk="high",
            last_analysis_summary="Locator stale on login page",
        )
        db_session.add(insight)
        db_session.commit()

        loaded = db_session.scalar(
            __import__("sqlalchemy").select(TestPointInsight).where(TestPointInsight.project_id == 1)
        )
        assert loaded is not None
        assert loaded.flaky_case_ids == [10, 20]
        assert loaded.failure_patterns["locator_stale"]["count"] == 3
        assert loaded.regression_risk == "high"

    def test_unique_project_id_constraint(self, db_session: Session) -> None:
        from app.models import TestPointInsight

        db_session.add(TestPointInsight(project_id=1))
        db_session.flush()
        db_session.add(TestPointInsight(project_id=1))
        with pytest.raises(Exception):
            db_session.commit()


class TestGetProjectInsightsTool:
    """Tests for _handle_get_project_insights tool handler."""

    def test_returns_no_insights_when_none_exist(self, db_session: Session) -> None:
        from app.ai.planning_tools import _handle_get_project_insights

        result = _handle_get_project_insights(params={}, db_session=db_session, project_id=1)
        assert result["has_insights"] is False

    def test_returns_stored_insights(self, db_session: Session) -> None:
        from app.ai.planning_tools import _handle_get_project_insights
        from app.models import TestPointInsight

        db_session.add(TestPointInsight(
            project_id=1,
            flaky_case_ids=[5],
            regression_risk="medium",
            last_analysis_summary="Some failures",
        ))
        db_session.commit()

        result = _handle_get_project_insights(params={}, db_session=db_session, project_id=1)
        assert result["has_insights"] is True
        assert result["flaky_case_ids"] == [5]
        assert result["regression_risk"] == "medium"


class TestUpdateInsightsTool:
    """Tests for _handle_update_insights tool handler."""

    def test_creates_insight_when_none_exists(self, db_session: Session) -> None:
        from sqlalchemy import select
        from app.ai.planning_tools import _handle_update_insights
        from app.models import TestPointInsight

        result = _handle_update_insights(
            params={
                "flaky_case_ids": [3, 7],
                "regression_risk": "high",
                "summary": "Multiple locator failures detected",
            },
            db_session=db_session,
            project_id=1,
        )
        assert result["status"] == "updated"
        assert result["flaky_count"] == 2

        insight = db_session.scalar(select(TestPointInsight).where(TestPointInsight.project_id == 1))
        assert insight is not None
        assert insight.flaky_case_ids == [3, 7]
        assert insight.regression_risk == "high"

    def test_updates_existing_insight(self, db_session: Session) -> None:
        from app.ai.planning_tools import _handle_update_insights
        from app.models import TestPointInsight

        db_session.add(TestPointInsight(project_id=1, regression_risk="low"))
        db_session.commit()

        _handle_update_insights(
            params={"regression_risk": "critical"},
            db_session=db_session,
            project_id=1,
        )

        from sqlalchemy import select
        insight = db_session.scalar(select(TestPointInsight).where(TestPointInsight.project_id == 1))
        assert insight.regression_risk == "critical"

    def test_merges_failure_patterns(self, db_session: Session) -> None:
        from app.ai.planning_tools import _handle_update_insights
        from app.models import TestPointInsight

        db_session.add(TestPointInsight(
            project_id=1,
            failure_patterns={"locator_stale": {"count": 2, "cases": [1]}},
        ))
        db_session.commit()

        _handle_update_insights(
            params={"failure_patterns": {"timeout": {"count": 5, "cases": [2, 3]}}},
            db_session=db_session,
            project_id=1,
        )

        from sqlalchemy import select
        insight = db_session.scalar(select(TestPointInsight).where(TestPointInsight.project_id == 1))
        assert "locator_stale" in insight.failure_patterns
        assert "timeout" in insight.failure_patterns

    def test_ignores_invalid_params(self, db_session: Session) -> None:
        from app.ai.planning_tools import _handle_update_insights

        result = _handle_update_insights(
            params={"invalid_key": "value", "regression_risk": 123},
            db_session=db_session,
            project_id=1,
        )
        assert result["status"] == "updated"


class TestImprovedFlakyDetection:
    """Tests for improved flaky detection with confidence scoring."""

    def test_strong_alternating_pattern_high_score(self, db_session: Session) -> None:
        from app.models import TestCaseRun
        from app.services import cases as case_service
        from app.ai.planning_tools import _handle_get_failure_analysis

        case = case_service.create_case(
            db_session,
            CaseCreateRequest(project_id=1, name="Very Flaky", description=None, steps=[{"action": "goto", "value": "/"}]),
            actor_user_id=1,
        )
        db_session.flush()
        # Strong alternating: P-F-P-F-P-F
        for status in ["passed", "failed", "passed", "failed", "passed", "failed"]:
            db_session.add(TestCaseRun(case_id=case.id, project_id=1, triggered_by=1, status=status))
        db_session.commit()

        result = _handle_get_failure_analysis(params={}, db_session=db_session, project_id=1)
        assert len(result["failure_patterns"]) == 1
        pattern = result["failure_patterns"][0]
        assert pattern["suspected_flaky"] is True
        assert pattern["flaky_score"] >= 0.8

    def test_consecutive_failures_low_flaky_score(self, db_session: Session) -> None:
        from app.models import TestCaseRun
        from app.services import cases as case_service
        from app.ai.planning_tools import _handle_get_failure_analysis

        case = case_service.create_case(
            db_session,
            CaseCreateRequest(project_id=1, name="Stable Fail", description=None, steps=[{"action": "goto", "value": "/"}]),
            actor_user_id=1,
        )
        db_session.flush()
        for _ in range(5):
            db_session.add(TestCaseRun(case_id=case.id, project_id=1, triggered_by=1, status="failed"))
        db_session.commit()

        result = _handle_get_failure_analysis(params={}, db_session=db_session, project_id=1)
        pattern = result["failure_patterns"][0]
        assert pattern["suspected_flaky"] is False
        assert pattern["flaky_score"] == 0.0

    def test_fewer_than_three_runs_no_score(self, db_session: Session) -> None:
        from app.models import TestCaseRun
        from app.services import cases as case_service
        from app.ai.planning_tools import _handle_get_failure_analysis

        case = case_service.create_case(
            db_session,
            CaseCreateRequest(project_id=1, name="New Case", description=None, steps=[{"action": "goto", "value": "/"}]),
            actor_user_id=1,
        )
        db_session.flush()
        db_session.add(TestCaseRun(case_id=case.id, project_id=1, triggered_by=1, status="failed"))
        db_session.add(TestCaseRun(case_id=case.id, project_id=1, triggered_by=1, status="passed"))
        db_session.commit()

        result = _handle_get_failure_analysis(params={}, db_session=db_session, project_id=1)
        pattern = result["failure_patterns"][0]
        assert pattern["flaky_score"] == 0.0
        assert pattern["suspected_flaky"] is False

    def test_mostly_passing_with_occasional_failure_moderate_score(self, db_session: Session) -> None:
        from app.models import TestCaseRun
        from app.services import cases as case_service
        from app.ai.planning_tools import _handle_get_failure_analysis

        case = case_service.create_case(
            db_session,
            CaseCreateRequest(project_id=1, name="Occasional Fail", description=None, steps=[{"action": "goto", "value": "/"}]),
            actor_user_id=1,
        )
        db_session.flush()
        # P-P-P-F-P-P — mostly passing with one failure, not strongly flaky
        for status in ["passed", "passed", "passed", "failed", "passed", "passed"]:
            db_session.add(TestCaseRun(case_id=case.id, project_id=1, triggered_by=1, status=status))
        db_session.commit()

        result = _handle_get_failure_analysis(params={}, db_session=db_session, project_id=1)
        pattern = result["failure_patterns"][0]
        assert pattern["flaky_score"] < 0.4


class TestAutoUpdateInsights:
    """Tests for _auto_update_insights helper."""

    def test_creates_insight_on_first_analysis(self, db_session: Session) -> None:
        from sqlalchemy import select
        from app.models import TestPointInsight, TestCaseRun
        from app.services import cases as case_service
        from app.services.ai_planning import _auto_update_insights
        from app.schemas.ai_planning import ExecutionSummaryResult

        case = case_service.create_case(
            db_session,
            CaseCreateRequest(project_id=1, name="Test", description=None, steps=[{"action": "goto", "value": "/"}]),
            actor_user_id=1,
        )
        db_session.flush()
        db_session.add(TestCaseRun(case_id=case.id, project_id=1, triggered_by=1, status="failed"))
        db_session.commit()

        summaries = [ExecutionSummaryResult(
            execution_id=1, case_id=case.id, case_name="Test",
            status="failed", total_steps=1, passed_steps=0, failed_steps=1,
            report_url="/run/1",
        )]
        _auto_update_insights(db_session, 1, summaries)

        insight = db_session.scalar(select(TestPointInsight).where(TestPointInsight.project_id == 1))
        assert insight is not None
        assert insight.regression_risk == "critical"

    def test_detects_flaky_and_updates(self, db_session: Session) -> None:
        from sqlalchemy import select
        from app.models import TestPointInsight, TestCaseRun
        from app.services import cases as case_service
        from app.services.ai_planning import _auto_update_insights
        from app.schemas.ai_planning import ExecutionSummaryResult

        case = case_service.create_case(
            db_session,
            CaseCreateRequest(project_id=1, name="Unstable", description=None, steps=[{"action": "goto", "value": "/"}]),
            actor_user_id=1,
        )
        db_session.flush()
        for status in ["passed", "failed", "passed", "failed", "passed", "failed"]:
            db_session.add(TestCaseRun(case_id=case.id, project_id=1, triggered_by=1, status=status))
        db_session.commit()

        summaries = [ExecutionSummaryResult(
            execution_id=1, case_id=case.id, case_name="Unstable",
            status="failed", total_steps=1, passed_steps=0, failed_steps=1,
            report_url="/run/1",
        )]
        _auto_update_insights(db_session, 1, summaries)

        insight = db_session.scalar(select(TestPointInsight).where(TestPointInsight.project_id == 1))
        assert case.id in (insight.flaky_case_ids or [])


class TestEnhancedContextPreamble:
    """Tests for enhanced _build_session_context_preamble with cross-session insights."""

    def test_injects_insights_when_available(self, db_session: Session) -> None:
        from app.models import AIPlanningSession, TestPointInsight, TestCaseRun
        from app.services import cases as case_service
        from app.services.ai_planning import _build_session_context_preamble

        case = case_service.create_case(
            db_session,
            CaseCreateRequest(project_id=1, name="Case A", description=None, steps=[{"action": "goto", "value": "/"}]),
            actor_user_id=1,
        )
        db_session.flush()
        db_session.add(TestCaseRun(case_id=case.id, project_id=1, triggered_by=1, status="passed"))
        db_session.add(TestPointInsight(
            project_id=1,
            flaky_case_ids=[10],
            regression_risk="high",
            last_analysis_summary="Locator stale",
            failure_patterns={"locator_stale": {"count": 3}},
        ))
        session_record = AIPlanningSession(
            actor_user_id=1, status="completed",
            requirements_json={}, missing_slots_json=[],
        )
        db_session.add(session_record)
        db_session.flush()
        from app.models.session_project import SessionProject
        db_session.add(SessionProject(session_id=session_record.id, project_id=1))
        db_session.commit()

        result = _build_session_context_preamble(session_record, db_session, existing_msg_count=5)
        assert result is not None
        assert "历史洞察" in result
        assert "high" in result

    def test_no_insights_section_when_none_exist(self, db_session: Session) -> None:
        from app.models import AIPlanningSession, TestCaseRun
        from app.models.session_project import SessionProject
        from app.services import cases as case_service
        from app.services.ai_planning import _build_session_context_preamble

        case = case_service.create_case(
            db_session,
            CaseCreateRequest(project_id=1, name="Case", description=None, steps=[{"action": "goto", "value": "/"}]),
            actor_user_id=1,
        )
        db_session.flush()
        db_session.add(TestCaseRun(case_id=case.id, project_id=1, triggered_by=1, status="passed"))
        session_record = AIPlanningSession(
            actor_user_id=1, status="completed",
            requirements_json={}, missing_slots_json=[],
        )
        db_session.add(session_record)
        db_session.flush()
        db_session.add(SessionProject(session_id=session_record.id, project_id=1))
        db_session.commit()

        result = _build_session_context_preamble(session_record, db_session, existing_msg_count=5)
        assert result is not None
        assert "历史洞察" not in result


class TestCategorizeError:
    """Tests for _categorize_error helper."""

    def test_categorizes_locator_error(self) -> None:
        from app.services.ai_planning import _categorize_error
        assert _categorize_error("Element not found on page") == "locator_stale"
        assert _categorize_error("Locator timeout for #btn") == "locator_stale"

    def test_categorizes_assertion_error(self) -> None:
        from app.services.ai_planning import _categorize_error
        assert _categorize_error("Assertion failed: expected 200") == "assertion_mismatch"

    def test_categorizes_timeout_error(self) -> None:
        from app.services.ai_planning import _categorize_error
        assert _categorize_error("Operation timed out after 30s") == "timeout"

    def test_categorizes_network_error(self) -> None:
        from app.services.ai_planning import _categorize_error
        assert _categorize_error("Connection refused: ECONNREFUSED") == "network_error"

    def test_categorizes_unknown(self) -> None:
        from app.services.ai_planning import _categorize_error
        assert _categorize_error("Something went wrong") == "unknown"
