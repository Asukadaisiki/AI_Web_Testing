from __future__ import annotations

import unittest
from unittest.mock import patch
from types import SimpleNamespace

from sqlalchemy import create_engine
from sqlalchemy.orm import Session

from app.application.reporting.analysis_service import analyze_batch, analyze_run, analyze_runs
from app.db.base import Base
from app.models import (
    DSLAntiPattern,
    ExecutionBatch,
    ExecutionJob,
    Project,
    TestCase,
    TestCaseRun,
    User,
)
from app.reporters import build_execution_report
from app.schemas.executions import ExecutionAnalysis, LocatorTrace, StepExecutionEvidence
from app.services.failure_signals import build_failure_signal


class ExecutionAnalysisContractTest(unittest.TestCase):
    def setUp(self) -> None:
        self.engine = create_engine("sqlite+pysqlite:///:memory:")
        Base.metadata.create_all(self.engine)
        self.session = Session(self.engine)
        user = User(
            email="analysis@test.local",
            display_name="Analysis Test",
            password_hash="not-used",
        )
        project = Project(name="analysis-project")
        self.session.add_all([user, project])
        self.session.flush()
        case = TestCase(
            project_id=project.id,
            created_by=user.id,
            updated_by=user.id,
            name="failing case",
            dsl={"name": "failing case", "steps": []},
        )
        self.session.add(case)
        self.session.commit()
        self.user_id = user.id
        self.project_id = project.id
        self.case_id = case.id

    def tearDown(self) -> None:
        self.session.close()
        self.engine.dispose()

    def _create_failed_run(
        self,
        *,
        batch_id: int | None = None,
        job_id: int | None = None,
    ) -> TestCaseRun:
        step = StepExecutionEvidence(
            step_index=0,
            action="click",
            target='button="Submit"',
            status="failed",
            error_message="Locator target not found",
            locator_trace=LocatorTrace(
                target='button="Submit"',
                failure_reason="No visible candidate",
            ),
        )
        report = build_execution_report(status="failed", steps=[step])
        signal = build_failure_signal(report, step.error_message)
        run = TestCaseRun(
            case_id=self.case_id,
            project_id=self.project_id,
            batch_id=batch_id,
            job_id=job_id,
            triggered_by=self.user_id,
            status="failed",
            report=report.model_dump(mode="json"),
            error_message=step.error_message,
            failure_signal_json=signal.model_dump(mode="json") if signal else None,
        )
        self.session.add(run)
        self.session.commit()
        return run

    def test_run_analysis_persists_failure_signal_and_anti_pattern(self) -> None:
        run = self._create_failed_run()

        with patch(
            "app.application.reporting.analysis_service.run_analysis_turn",
            return_value=None,
        ):
            analysis = analyze_run(self.session, run.id)

        refreshed = self.session.get(TestCaseRun, run.id)
        anti_pattern = self.session.query(DSLAntiPattern).one()
        self.assertEqual("deterministic", analysis.source)
        self.assertEqual("all_failed", analysis.conclusion)
        self.assertEqual("locator", analysis.failure_signals[0].category)
        self.assertEqual("completed", refreshed.analysis_status)
        self.assertEqual("locator", anti_pattern.failure_category)

    def test_batch_analysis_is_shared_with_its_run(self) -> None:
        batch = ExecutionBatch(
            project_id=self.project_id,
            triggered_by=self.user_id,
            status="failed",
            concurrency_limit=1,
            input_values_json={},
        )
        self.session.add(batch)
        self.session.flush()
        job = ExecutionJob(
            batch_id=batch.id,
            project_id=self.project_id,
            case_id=self.case_id,
            order_index=0,
            status="failed",
            attempt_count=1,
            max_attempts=2,
        )
        self.session.add(job)
        self.session.commit()
        run = self._create_failed_run(batch_id=batch.id, job_id=job.id)

        with patch(
            "app.application.reporting.analysis_service.run_analysis_turn",
            return_value=None,
        ):
            analysis = analyze_batch(self.session, batch.id)

        refreshed_batch = self.session.get(ExecutionBatch, batch.id)
        refreshed_run = self.session.get(TestCaseRun, run.id)
        self.assertEqual("all_failed", analysis.conclusion)
        self.assertEqual("completed", refreshed_batch.analysis_status)
        self.assertEqual(refreshed_batch.analysis_json, refreshed_run.analysis_json)

    def test_ai_analysis_cannot_override_deterministic_failure_outcome(self) -> None:
        run = self._create_failed_run()
        invalid_ai_result = SimpleNamespace(
            assistant_message="执行结果分析：全部通过。",
            execution_analysis=ExecutionAnalysis(
                source="ai",
                conclusion="all_passed",
                recommended_action="done",
            ),
        )

        with patch(
            "app.application.reporting.analysis_service.run_analysis_turn",
            return_value=invalid_ai_result,
        ):
            analysis = analyze_run(self.session, run.id)

        self.assertEqual("ai", analysis.source)
        self.assertEqual("all_failed", analysis.conclusion)
        self.assertEqual("targeted_retest", analysis.recommended_action)
        self.assertEqual("locator", analysis.failure_signals[0].category)
        self.assertEqual(1, len(analysis.failure_details))
        self.assertEqual("1 个用例均未通过，已提取 1 个失败信号。", analysis.summary)

    def test_group_analysis_persists_the_same_fact_for_retest_runs(self) -> None:
        first = self._create_failed_run()
        second = self._create_failed_run()

        with patch(
            "app.application.reporting.analysis_service.run_analysis_turn",
            return_value=None,
        ):
            analysis = analyze_runs(
                self.session,
                [first.id, second.id],
                project_id=self.project_id,
            )

        refreshed_first = self.session.get(TestCaseRun, first.id)
        refreshed_second = self.session.get(TestCaseRun, second.id)
        self.assertEqual("all_failed", analysis.conclusion)
        self.assertEqual("completed", refreshed_first.analysis_status)
        self.assertEqual(refreshed_first.analysis_json, refreshed_second.analysis_json)


if __name__ == "__main__":
    unittest.main()
