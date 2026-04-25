"""Unit tests for post-execution auto-analysis flow."""
from __future__ import annotations

from unittest.mock import patch, MagicMock

import pytest
from sqlalchemy.orm import Session

from app.schemas.ai_planning import (
    ExecutionSummaryResult,
)


class TestBuildAnalysisContext:
    """Tests for _build_analysis_context helper."""

    def test_builds_context_from_execution_summaries(self, db_session: Session) -> None:
        """Should produce a context message containing all case statuses."""
        from app.services.ai_planning import _build_analysis_context

        summaries = [
            ExecutionSummaryResult(
                execution_id=1, case_id=1, case_name="Login Test",
                status="failed", total_steps=5, passed_steps=3, failed_steps=2,
                duration_ms=1000, screenshot_url=None, report_url="/run/1",
            ),
            ExecutionSummaryResult(
                execution_id=2, case_id=2, case_name="Search Test",
                status="passed", total_steps=3, passed_steps=3, failed_steps=0,
                duration_ms=500, screenshot_url=None, report_url="/run/2",
            ),
        ]
        context = _build_analysis_context(summaries, db_session)
        assert "Login Test" in context
        assert "failed" in context
        assert "Search Test" in context
        assert "passed" in context


class TestShouldRunAnalysis:
    """Tests that auto-analysis triggers when execution has failures."""

    def test_no_analysis_when_all_passed(self, db_session: Session) -> None:
        """Should not trigger AI analysis when all cases pass."""
        from app.services.ai_planning import _should_run_analysis

        summaries = [
            ExecutionSummaryResult(
                execution_id=1, case_id=1, case_name="A",
                status="passed", total_steps=3, passed_steps=3, failed_steps=0,
                duration_ms=100, screenshot_url=None, report_url="/run/1",
            ),
        ]
        assert _should_run_analysis(summaries) is False

    def test_analysis_triggered_on_failure(self, db_session: Session) -> None:
        """Should trigger AI analysis when any case fails."""
        from app.services.ai_planning import _should_run_analysis

        summaries = [
            ExecutionSummaryResult(
                execution_id=1, case_id=1, case_name="A",
                status="passed", total_steps=3, passed_steps=3, failed_steps=0,
                duration_ms=100, screenshot_url=None, report_url="/run/1",
            ),
            ExecutionSummaryResult(
                execution_id=2, case_id=2, case_name="B",
                status="failed", total_steps=5, passed_steps=3, failed_steps=2,
                duration_ms=500, screenshot_url=None, report_url="/run/2",
            ),
        ]
        assert _should_run_analysis(summaries) is True

    def test_analysis_triggered_on_needs_intervention(self, db_session: Session) -> None:
        """Should trigger AI analysis when any case needs intervention."""
        from app.services.ai_planning import _should_run_analysis

        summaries = [
            ExecutionSummaryResult(
                execution_id=1, case_id=1, case_name="A",
                status="needs_intervention", total_steps=2, passed_steps=1, failed_steps=1,
                duration_ms=100, screenshot_url=None, report_url="/run/1",
            ),
        ]
        assert _should_run_analysis(summaries) is True
