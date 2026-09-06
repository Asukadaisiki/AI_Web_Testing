from __future__ import annotations

from datetime import UTC, datetime, timedelta
import unittest

from sqlalchemy import create_engine
from sqlalchemy.orm import Session

from app.db.base import Base
from app.models import ExecutionBatch, ExecutionJob, Project, TestCase, TestCaseRun, User
from app.services.execution_batches import claim_next_execution_job


class ExecutionLeaseReclaimTest(unittest.TestCase):
    def setUp(self) -> None:
        self.engine = create_engine("sqlite+pysqlite:///:memory:")
        Base.metadata.create_all(self.engine)
        self.session = Session(self.engine)
        user = User(email="lease@test.local", display_name="Lease Test")
        project = Project(name="lease-project")
        self.session.add_all([user, project])
        self.session.flush()
        self.user_id = user.id
        self.project_id = project.id

    def tearDown(self) -> None:
        self.session.close()
        self.engine.dispose()

    def _expired_job(self, *, dsl: dict) -> tuple[ExecutionBatch, ExecutionJob]:
        case = TestCase(
            project_id=self.project_id,
            created_by=self.user_id,
            updated_by=self.user_id,
            name="lease case",
            dsl=dsl,
        )
        batch = ExecutionBatch(
            project_id=self.project_id,
            triggered_by=self.user_id,
            status="running",
            concurrency_limit=1,
            input_values_json={},
        )
        self.session.add_all([case, batch])
        self.session.flush()
        job = ExecutionJob(
            batch_id=batch.id,
            project_id=self.project_id,
            case_id=case.id,
            order_index=0,
            status="running",
            attempt_count=1,
            max_attempts=2,
            lease_owner="crashed-worker",
            lease_expires_at=datetime.now(UTC).replace(tzinfo=None)
            - timedelta(seconds=10),
            dsl_snapshot=dsl,
        )
        self.session.add(job)
        self.session.commit()
        return batch, job

    def test_expired_non_idempotent_job_with_committed_click_is_not_replayed(self) -> None:
        batch, job = self._expired_job(
            dsl={
                "name": "cart",
                "steps": [{"action": "click", "target": "Add to cart"}],
            }
        )
        evidence = {
            "status": "failed",
            "steps": [
                {
                    "step_index": 0,
                    "action": "click",
                    "status": "failed",
                    "action_outcome": {
                        "status": "succeeded",
                        "side_effect_state": "committed",
                    },
                    "error_message": "postcondition failed",
                }
            ],
        }
        run = TestCaseRun(
            case_id=job.case_id,
            project_id=self.project_id,
            batch_id=batch.id,
            job_id=job.id,
            triggered_by=self.user_id,
            status="running",
            attempt_number=1,
            dsl_snapshot=job.dsl_snapshot,
            report_schema_version="execution.report.v2",
            report=evidence,
        )
        self.session.add(run)
        self.session.commit()

        claimed = claim_next_execution_job(self.session, worker_id="replacement")

        self.assertIsNone(claimed)
        self.session.refresh(job)
        self.session.refresh(run)
        self.session.refresh(batch)
        self.assertEqual(job.status, "needs_intervention")
        self.assertEqual(run.status, "needs_intervention")
        self.assertEqual(batch.status, "needs_intervention")
        self.assertEqual(run.report, evidence)
        self.assertEqual(job.attempt_count, 1)

    def test_expired_idempotent_job_can_be_reclaimed(self) -> None:
        _batch, job = self._expired_job(
            dsl={
                "name": "read only",
                "steps": [
                    {
                        "action": "assert_url_contains",
                        "value": "example.test",
                    }
                ],
            }
        )

        claimed = claim_next_execution_job(self.session, worker_id="replacement")

        self.assertIsNotNone(claimed)
        self.assertEqual(claimed.id, job.id)
        self.assertEqual(claimed.attempt_count, 2)
        self.assertEqual(claimed.lease_owner, "replacement")

    def test_legacy_click_evidence_without_outcome_is_treated_as_uncertain(self) -> None:
        batch, job = self._expired_job(
            dsl={
                "name": "legacy cart",
                "steps": [{"action": "click", "target": "Add to cart"}],
            }
        )
        run = TestCaseRun(
            case_id=job.case_id,
            project_id=self.project_id,
            batch_id=batch.id,
            job_id=job.id,
            triggered_by=self.user_id,
            status="failed",
            attempt_number=1,
            dsl_snapshot=job.dsl_snapshot,
            report_schema_version="execution.report.v1",
            report={
                "status": "failed",
                "steps": [
                    {
                        "step_index": 0,
                        "action": "click",
                        "status": "failed",
                    }
                ],
            },
        )
        self.session.add(run)
        self.session.commit()

        claimed = claim_next_execution_job(self.session, worker_id="replacement")

        self.assertIsNone(claimed)
        self.session.refresh(job)
        self.assertEqual(job.status, "needs_intervention")
        self.assertEqual(job.attempt_count, 1)


if __name__ == "__main__":
    unittest.main()
