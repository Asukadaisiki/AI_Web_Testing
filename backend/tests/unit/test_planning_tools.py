"""Unit tests for planning_tools.py tool handlers."""

from __future__ import annotations

import json

import pytest
from sqlalchemy.orm import Session

from app.ai.planning_tools import (
    _handle_get_case_detail,
    _handle_get_case_stats,
    _handle_get_project_info,
    _handle_list_recent_executions,
    _handle_list_test_cases,
    execute_tool,
    list_available_tools,
)
from app.schemas.cases import CaseCreateRequest


class TestListAvailableTools:
    """Tests for list_available_tools function."""

    def test_returns_all_registered_tools(self) -> None:
        """Should return all 5 registered tools."""
        tools = list_available_tools()
        assert len(tools) == 5
        tool_names = {t.name for t in tools}
        assert tool_names == {
            "get_project_info",
            "list_test_cases",
            "get_case_detail",
            "list_recent_executions",
            "get_case_stats",
        }


class TestExecuteTool:
    """Tests for execute_tool dispatcher."""

    def test_unknown_tool_returns_error(self, db_session: Session) -> None:
        """Should return error for non-existent tool."""
        result = execute_tool(
            tool_name="unknown_tool",
            params={},
            db_session=db_session,
            project_id=1,
        )
        data = json.loads(result)
        assert "error" in data
        assert "不存在" in data["error"]

    def test_dispatches_to_correct_handler(self, db_session: Session) -> None:
        """Should call get_project_info and return actual project data from seed DB."""
        result = execute_tool(
            tool_name="get_project_info",
            params={},
            db_session=db_session,
            project_id=1,
        )
        data = json.loads(result)
        assert data["id"] == 1
        assert data["name"] == "Default Project"


class TestHandleGetProjectInfo:
    """Tests for _handle_get_project_info handler."""

    def test_returns_project_details_from_db(self, db_session: Session) -> None:
        """Should return project id, name, and description from seed data."""
        result = _handle_get_project_info(params={}, db_session=db_session, project_id=1)
        assert result["id"] == 1
        assert result["name"] == "Default Project"
        assert result["description"] == "Seed project for tests."

    def test_nonexistent_project_returns_error(self, db_session: Session) -> None:
        """Should return error when project not found."""
        result = _handle_get_project_info(params={}, db_session=db_session, project_id=999)
        assert "error" in result
        assert "不存在" in result["error"]


class TestHandleListTestCases:
    """Tests for _handle_list_test_cases handler."""

    def test_returns_empty_list_when_no_cases(self, db_session: Session) -> None:
        """Should return empty list when no test cases exist."""
        result = _handle_list_test_cases(params={}, db_session=db_session, project_id=1)
        assert "cases" in result
        assert result["cases"] == []
        assert result["total"] == 0

    def test_returns_case_summaries(self, db_session: Session) -> None:
        """Should return list of cases with id, name, description."""
        from app.services import cases as case_service

        payload = CaseCreateRequest(
            project_id=1,
            name="Test Case A",
            description="Test Description A",
            steps=[{"action": "goto", "value": "/test"}],
        )
        case_service.create_case(db_session, payload, actor_user_id=1)
        db_session.commit()

        result = _handle_list_test_cases(params={}, db_session=db_session, project_id=1)
        assert len(result["cases"]) == 1
        assert result["cases"][0]["name"] == "Test Case A"
        assert result["cases"][0]["description"] == "Test Description A"
        assert result["total"] == 1

    def test_search_filters_by_name_and_description(self, db_session: Session) -> None:
        """Should filter cases by search keyword in name or description."""
        from app.services import cases as case_service

        case_service.create_case(
            db_session,
            CaseCreateRequest(
                project_id=1,
                name="Login Test",
                description="Test login flow",
                steps=[{"action": "goto", "value": "/login"}],
            ),
            actor_user_id=1,
        )
        case_service.create_case(
            db_session,
            CaseCreateRequest(
                project_id=1,
                name="Logout Test",
                description="Test logout flow",
                steps=[{"action": "goto", "value": "/logout"}],
            ),
            actor_user_id=1,
        )
        db_session.commit()

        result = _handle_list_test_cases(
            params={"search": "login"},
            db_session=db_session,
            project_id=1,
        )
        assert len(result["cases"]) == 1
        assert result["cases"][0]["name"] == "Login Test"

    def test_search_case_insensitive(self, db_session: Session) -> None:
        """Search should be case-insensitive."""
        from app.services import cases as case_service

        case_service.create_case(
            db_session,
            CaseCreateRequest(
                project_id=1,
                name="LOGIN",
                description=None,
                steps=[{"action": "goto", "value": "/"}],
            ),
            actor_user_id=1,
        )
        db_session.commit()

        result = _handle_list_test_cases(
            params={"search": "login"},
            db_session=db_session,
            project_id=1,
        )
        assert len(result["cases"]) == 1

    def test_limit_capped_at_20(self, db_session: Session) -> None:
        """Should cap limit at maximum of 20."""
        from app.services import cases as case_service

        for i in range(25):
            case_service.create_case(
                db_session,
                CaseCreateRequest(
                    project_id=1,
                    name=f"Case {i}",
                    description=None,
                    steps=[{"action": "goto", "value": "/test"}],
                ),
                actor_user_id=1,
            )
        db_session.commit()

        result = _handle_list_test_cases(
            params={"limit": 100},
            db_session=db_session,
            project_id=1,
        )
        assert len(result["cases"]) == 20
        assert result["total"] == 25

    def test_default_limit_is_10(self, db_session: Session) -> None:
        """Should default to limit of 10."""
        from app.services import cases as case_service

        for i in range(15):
            case_service.create_case(
                db_session,
                CaseCreateRequest(
                    project_id=1,
                    name=f"Case {i}",
                    description=None,
                    steps=[{"action": "goto", "value": "/test"}],
                ),
                actor_user_id=1,
            )
        db_session.commit()

        result = _handle_list_test_cases(
            params={},
            db_session=db_session,
            project_id=1,
        )
        assert len(result["cases"]) == 10
        assert result["total"] == 15


class TestHandleGetCaseDetail:
    """Tests for _handle_get_case_detail handler."""

    def test_returns_full_case_details(self, db_session: Session) -> None:
        """Should return case with steps and contracts."""
        from app.services import cases as case_service

        payload = CaseCreateRequest(
            project_id=1,
            name="Login Case",
            description="Test login",
            base_url="https://example.com",
            steps=[{"action": "click", "target": "#btn"}],
            input_contract=[{"name": "username", "context_key": "u", "value_type": "string"}],
            output_contract=[{"name": "token", "context_key": "t", "value_type": "string"}],
        )
        created = case_service.create_case(db_session, payload, actor_user_id=1)
        db_session.commit()

        result = _handle_get_case_detail(
            params={"case_id": str(created.id)},
            db_session=db_session,
            project_id=1,
        )
        assert result["id"] == created.id
        assert result["name"] == "Login Case"
        assert result["base_url"] == "https://example.com"
        assert len(result["steps"]) == 1
        assert result["steps"][0]["action"] == "click"

    def test_missing_case_id_returns_error(self, db_session: Session) -> None:
        """Should return error when case_id is missing."""
        result = _handle_get_case_detail(
            params={},
            db_session=db_session,
            project_id=1,
        )
        assert "error" in result
        assert "必须提供" in result["error"]

    def test_zero_case_id_returns_error(self, db_session: Session) -> None:
        """Should return error when case_id is 0."""
        result = _handle_get_case_detail(
            params={"case_id": "0"},
            db_session=db_session,
            project_id=1,
        )
        assert "error" in result
        assert "必须提供" in result["error"]

    def test_nonexistent_case_returns_error(self, db_session: Session) -> None:
        """Should return error when case not found."""
        result = _handle_get_case_detail(
            params={"case_id": "999"},
            db_session=db_session,
            project_id=1,
        )
        assert "error" in result
        assert "不存在" in result["error"]


class TestHandleListRecentExecutions:
    """Tests for _handle_list_recent_executions handler."""

    def test_returns_empty_list_when_no_executions(self, db_session: Session) -> None:
        """Should return empty list when no executions exist."""
        result = _handle_list_recent_executions(
            params={},
            db_session=db_session,
            project_id=1,
        )
        assert "executions" in result
        assert result["executions"] == []

    def test_returns_execution_summaries(self, db_session: Session) -> None:
        """Should return list of recent executions."""
        from app.models import TestCaseRun
        from app.services import cases as case_service

        # First create a test case
        case = case_service.create_case(
            db_session,
            CaseCreateRequest(
                project_id=1,
                name="Login Test",
                description=None,
                steps=[{"action": "goto", "value": "/test"}],
            ),
            actor_user_id=1,
        )
        db_session.flush()

        # Create an execution
        execution = TestCaseRun(
            case_id=case.id,
            project_id=1,
            triggered_by=1,
            status="passed",
        )
        db_session.add(execution)
        db_session.commit()

        result = _handle_list_recent_executions(
            params={},
            db_session=db_session,
            project_id=1,
        )
        assert len(result["executions"]) == 1
        assert result["executions"][0]["case_name"] == "Login Test"
        assert result["executions"][0]["status"] == "passed"

    def test_limit_capped_at_10(self, db_session: Session) -> None:
        """Should cap limit at maximum of 10."""
        from app.models import TestCaseRun
        from app.services import cases as case_service

        case = case_service.create_case(
            db_session,
            CaseCreateRequest(
                project_id=1,
                name="Test Case",
                description=None,
                steps=[{"action": "goto", "value": "/test"}],
            ),
            actor_user_id=1,
        )
        db_session.flush()

        for _ in range(15):
            execution = TestCaseRun(
                case_id=case.id,
                project_id=1,
                triggered_by=1,
                status="running",
            )
            db_session.add(execution)
        db_session.commit()

        result = _handle_list_recent_executions(
            params={"limit": 100},
            db_session=db_session,
            project_id=1,
        )
        assert len(result["executions"]) == 10

    def test_default_limit_is_5(self, db_session: Session) -> None:
        """Should default to limit of 5."""
        from app.models import TestCaseRun
        from app.services import cases as case_service

        case = case_service.create_case(
            db_session,
            CaseCreateRequest(
                project_id=1,
                name="Test Case",
                description=None,
                steps=[{"action": "goto", "value": "/test"}],
            ),
            actor_user_id=1,
        )
        db_session.flush()

        for _ in range(10):
            execution = TestCaseRun(
                case_id=case.id,
                project_id=1,
                triggered_by=1,
                status="running",
            )
            db_session.add(execution)
        db_session.commit()

        result = _handle_list_recent_executions(
            params={},
            db_session=db_session,
            project_id=1,
        )
        assert len(result["executions"]) == 5


class TestHandleGetCaseStats:
    """Tests for _handle_get_case_stats handler."""

    def test_wraps_non_dict_return(self, monkeypatch: pytest.MonkeyPatch, db_session: Session) -> None:
        """Should wrap non-dict returns in stats key when service returns non-dict."""
        # Mock the stats function to return a non-dict value
        monkeypatch.setattr(
            "app.services.cases.get_project_test_case_stats",
            lambda s, pid: "string result",
        )

        result = _handle_get_case_stats(
            params={},
            db_session=db_session,
            project_id=1,
        )
        assert result == {"stats": "string result"}

    def test_service_call_integration(self, monkeypatch: pytest.MonkeyPatch, db_session: Session) -> None:
        """Should call get_project_test_case_stats with correct parameters."""
        call_log = []

        def mock_stats(session, project_id):
            call_log.append(("get_project_test_case_stats", project_id))
            return {
                "project_id": project_id,
                "total_cases": 5,
                "created_by_month": {},
                "created_by_user": {},
                "recent_cases": [],
            }

        monkeypatch.setattr("app.services.cases.get_project_test_case_stats", mock_stats)

        result = _handle_get_case_stats(
            params={},
            db_session=db_session,
            project_id=42,
        )

        assert len(call_log) == 1
        assert call_log[0] == ("get_project_test_case_stats", 42)
        assert result["project_id"] == 42
        assert result["total_cases"] == 5
