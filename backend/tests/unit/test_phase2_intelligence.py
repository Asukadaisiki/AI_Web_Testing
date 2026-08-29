"""Unit tests for Phase 2 intelligence features."""
from __future__ import annotations

from sqlalchemy.orm import Session

from app.schemas.ai_planning import AIPlanningRequirements


class TestMergeTestContext:
    """Tests for _merge_test_context in the agent."""

    def test_merges_test_context_into_requirements(self) -> None:
        from app.ai.test_planning_agent import _merge_test_context

        reqs = AIPlanningRequirements()
        _merge_test_context(reqs, {
            "project_id": 1,
            "test_point_status": "partial",
            "last_run_failures": ["step 3"],
            "suspected_root_cause": "locator stale",
            "next_action": "targeted_retest",
        })
        assert reqs.test_context is not None
        assert reqs.test_context["project_id"] == 1
        assert reqs.test_context["suspected_root_cause"] == "locator stale"

    def test_preserves_existing_context(self) -> None:
        from app.ai.test_planning_agent import _merge_test_context

        reqs = AIPlanningRequirements(test_context={"project_id": 1, "old_field": "kept"})
        _merge_test_context(reqs, {"project_id": 1, "new_field": "added"})
        assert reqs.test_context["old_field"] == "kept"
        assert reqs.test_context["new_field"] == "added"

    def test_ignores_none_values(self) -> None:
        from app.ai.test_planning_agent import _merge_test_context

        reqs = AIPlanningRequirements(test_context={"project_id": 1})
        _merge_test_context(reqs, {"project_id": None, "root_cause": "bug"})
        assert reqs.test_context["project_id"] == 1
        assert reqs.test_context["root_cause"] == "bug"

    def test_ignores_non_dict_input(self) -> None:
        from app.ai.test_planning_agent import _merge_test_context

        reqs = AIPlanningRequirements(test_context={"project_id": 1})
        _merge_test_context(reqs, "not a dict")
        _merge_test_context(reqs, None)
        _merge_test_context(reqs, 42)
        assert reqs.test_context == {"project_id": 1}


class TestBuildSessionContextPreamble:
    """Tests for _build_session_context_preamble."""

    def test_returns_none_on_first_turn(self, db_session: Session) -> None:
        from app.services.ai_planning import _build_session_context_preamble
        from app.models import AIPlanningSession

        session_record = AIPlanningSession(
            actor_user_id=1,
            status="collecting",
            requirements_json={},
            missing_slots_json=[],
        )
        db_session.add(session_record)
        db_session.commit()

        result = _build_session_context_preamble(session_record, db_session, existing_msg_count=1)
        assert result is None

    def test_returns_none_without_project(self, db_session: Session) -> None:
        from app.services.ai_planning import _build_session_context_preamble
        from app.models import AIPlanningSession

        session_record = AIPlanningSession(
            actor_user_id=1,
            status="collecting",
            requirements_json={},
            missing_slots_json=[],
        )
        db_session.add(session_record)
        db_session.commit()

        result = _build_session_context_preamble(session_record, db_session, existing_msg_count=5)
        assert result is None

    def test_injects_status_when_has_history(self, db_session: Session) -> None:
        from app.services.ai_planning import _build_session_context_preamble
        from app.models import AIPlanningSession, TestCaseRun
        from app.models.session_project import SessionProject
        from app.services import cases as case_service
        from app.schemas.cases import CaseCreateRequest

        # Create a case and a passing execution
        case = case_service.create_case(
            db_session,
            CaseCreateRequest(project_id=1, name="Existing Case", description=None, steps=[{"action": "goto", "value": "/"}]),
            actor_user_id=1,
        )
        db_session.flush()
        db_session.add(TestCaseRun(case_id=case.id, project_id=1, triggered_by=1, status="passed"))
        db_session.commit()

        session_record = AIPlanningSession(
            actor_user_id=1,
            status="completed",
            requirements_json={},
            missing_slots_json=[],
        )
        db_session.add(session_record)
        db_session.flush()
        db_session.add(SessionProject(session_id=session_record.id, project_id=1))
        db_session.commit()

        result = _build_session_context_preamble(session_record, db_session, existing_msg_count=5)
        assert result is not None
        assert "系统自动注入" in result
        assert "全部通过" in result


class TestInjectAutoContext:
    """Tests for _inject_auto_context."""

    def test_returns_transcript_unchanged_when_no_injection(self, db_session: Session) -> None:
        from app.application.planning.context_service import inject_auto_context
        from app.models import AIPlanningSession

        session_record = AIPlanningSession(
            actor_user_id=1,
            status="collecting",
            requirements_json={},
            missing_slots_json=[],
        )
        db_session.add(session_record)
        db_session.commit()

        transcript = [{"role": "user", "content": "hello"}]
        result = inject_auto_context(transcript, session_record, db_session, existing_msg_count=1)
        assert len(result) == 1

    def test_prepends_preamble_when_injection_needed(self, db_session: Session) -> None:
        from app.application.planning.context_service import inject_auto_context
        from app.models import AIPlanningSession, TestCaseRun
        from app.models.session_project import SessionProject
        from app.services import cases as case_service
        from app.schemas.cases import CaseCreateRequest

        case = case_service.create_case(
            db_session,
            CaseCreateRequest(project_id=1, name="Case A", description=None, steps=[{"action": "goto", "value": "/"}]),
            actor_user_id=1,
        )
        db_session.flush()
        db_session.add(TestCaseRun(case_id=case.id, project_id=1, triggered_by=1, status="failed"))
        db_session.commit()

        session_record = AIPlanningSession(
            actor_user_id=1,
            status="completed",
            requirements_json={},
            missing_slots_json=[],
        )
        db_session.add(session_record)
        db_session.flush()
        db_session.add(SessionProject(session_id=session_record.id, project_id=1))
        db_session.commit()

        transcript = [{"role": "user", "content": "how are results?"}]
        result = inject_auto_context(transcript, session_record, db_session, existing_msg_count=5)
        assert len(result) == 2
        assert result[0]["role"] == "system"
        assert "系统自动注入" in result[0]["content"]


class TestRetestCases:
    """Tests for retest_cases service function."""

    def test_returns_no_retest_message_when_no_failures(self, db_session: Session) -> None:
        from app.services.ai_planning import retest_cases
        from app.models import AIPlanningSession
        from app.models.session_project import SessionProject

        session_record = AIPlanningSession(
            actor_user_id=1,
            status="completed",
            requirements_json={},
            missing_slots_json=[],
        )
        db_session.add(session_record)
        db_session.flush()
        db_session.add(SessionProject(session_id=session_record.id, project_id=1))
        db_session.commit()

        result = retest_cases(
            db_session, session_record.id,
            actor_user_id=1,
            failed_only=True,
        )
        assert "没有需要复测" in result.assistant_message

    def test_requires_case_ids_or_failed_only(self, db_session: Session) -> None:
        from app.services.ai_planning import retest_cases
        from app.models import AIPlanningSession

        session_record = AIPlanningSession(
            actor_user_id=1,
            status="completed",
            requirements_json={},
            missing_slots_json=[],
        )
        db_session.add(session_record)
        db_session.commit()

        result = retest_cases(
            db_session, session_record.id,
            actor_user_id=1,
        )
        assert "请指定" in result.assistant_message
