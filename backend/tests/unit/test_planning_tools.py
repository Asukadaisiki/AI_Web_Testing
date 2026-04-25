"""Unit tests for planning_tools.py tool handlers."""

from __future__ import annotations

import json
from unittest.mock import patch

import pytest
from sqlalchemy.orm import Session

from app.ai.planning_tools import (
    _handle_capture_page_session,
    _handle_explore_flow,
    _handle_explore_page,
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
        """Should return all 12 registered tools."""
        tools = list_available_tools()
        assert len(tools) == 12
        tool_names = {t.name for t in tools}
        assert tool_names == {
            "get_project_info",
            "list_test_cases",
            "get_case_detail",
            "list_recent_executions",
            "get_case_stats",
            "explore_page",
            "capture_page_session",
            "explore_flow",
            "get_execution_detail",
            "get_project_test_status",
            "get_failure_analysis",
            "get_recommended_retest",
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


class TestExplorePageTool:
    """Tests for _handle_explore_page handler."""

    def test_explore_page_returns_elements(self, db_session: Session) -> None:
        fake_elements = [
            {"tag": "input", "id": "username", "text": None, "role": None,
             "aria_label": None, "placeholder": "Username", "visible": True, "enabled": True}
        ]
        with patch("app.ai.planning_tools.collect_interactable_elements", return_value=fake_elements):
            result = _handle_explore_page(
                params={"url": "https://example.com/login"},
                db_session=db_session,
                project_id=1,
            )
        assert "elements" in result
        assert len(result["elements"]) == 1
        assert result["elements"][0]["placeholder"] == "Username"

    def test_explore_page_requires_url(self, db_session: Session) -> None:
        result = _handle_explore_page(params={}, db_session=db_session, project_id=1)
        assert "error" in result
        assert "url" in result["error"].lower()

    def test_explore_page_handles_empty_result(self, db_session: Session) -> None:
        with patch("app.ai.planning_tools.collect_interactable_elements", return_value=[]):
            result = _handle_explore_page(
                params={"url": "https://example.com/blank"},
                db_session=db_session,
                project_id=1,
            )
        assert result["elements"] == []
        assert "warning" in result


class TestCapturePageSessionTool:
    """Tests for _handle_capture_page_session handler."""

    def test_capture_returns_success(self, db_session: Session) -> None:
        with patch("app.ai.planning_tools.capture_browser_session") as mock_capture:
            mock_capture.return_value = {"success": True, "message": "已保存会话状态（包含 2 个 cookie）"}
            result = _handle_capture_page_session(
                params={
                    "url": "https://example.com/login",
                    "steps": [{"action": "input", "target": "username", "value": "admin"}],
                },
                db_session=db_session,
                project_id=1,
            )
        assert result["success"] is True

    def test_capture_requires_url(self, db_session: Session) -> None:
        result = _handle_capture_page_session(params={}, db_session=db_session, project_id=1)
        assert "error" in result
        assert "url" in result["error"].lower()


class TestExploreFlowTool:
    """Tests for _handle_explore_flow handler."""

    def test_explore_flow_returns_multi_page_results(self, db_session: Session) -> None:
        mock_pages = [
            {
                "url": "https://example.com/login",
                "elements": [{"tag": "input", "id": "email", "text": None, "role": None, "aria_label": None, "placeholder": "Email", "visible": True, "enabled": True}],
                "formatted": "input#email [placeholder='Email']",
                "element_count": 1,
                "screenshot_available": True,
                "vlm_annotation": None,
            },
            {
                "url": "https://example.com/products",
                "elements": [{"tag": "button", "id": "", "text": "Search", "role": None, "aria_label": None, "placeholder": None, "visible": True, "enabled": True}],
                "formatted": "button [text='Search']",
                "element_count": 1,
                "screenshot_available": True,
                "vlm_annotation": None,
            },
        ]
        with patch("app.ai.planning_tools.collect_multi_page_elements", return_value=mock_pages):
            result = _handle_explore_flow(
                params={"urls": ["https://example.com/login", "https://example.com/products"]},
                db_session=db_session,
                project_id=1,
            )
        assert result["total_pages"] == 2
        assert result["total_elements"] == 2
        assert "=== 页面: https://example.com/login ===" in result["formatted"]
        assert "=== 页面: https://example.com/products ===" in result["formatted"]
        assert len(result["pages"]) == 2

    def test_explore_flow_requires_urls(self, db_session: Session) -> None:
        result = _handle_explore_flow(params={}, db_session=db_session, project_id=1)
        assert "error" in result

    def test_explore_flow_requires_non_empty_urls(self, db_session: Session) -> None:
        result = _handle_explore_flow(params={"urls": []}, db_session=db_session, project_id=1)
        assert "error" in result

    def test_explore_flow_filters_invalid_urls(self, db_session: Session) -> None:
        with patch("app.ai.planning_tools.collect_multi_page_elements", return_value=[]):
            result = _handle_explore_flow(
                params={"urls": [123, None]},
                db_session=db_session,
                project_id=1,
            )
        assert "error" in result


class TestGetExecutionDetail:
    """Tests for _handle_get_execution_detail handler."""

    def test_returns_step_level_detail(self, db_session: Session) -> None:
        """Should return per-step status and error info for a specific run."""
        from app.models import TestCaseRun
        from app.services import cases as case_service

        case = case_service.create_case(
            db_session,
            CaseCreateRequest(
                project_id=1,
                name="Detail Case",
                description=None,
                steps=[{"action": "goto", "value": "/test"}],
            ),
            actor_user_id=1,
        )
        db_session.flush()

        execution = TestCaseRun(
            case_id=case.id,
            project_id=1,
            triggered_by=1,
            status="failed",
            report={
                "status": "failed",
                "steps": [
                    {"step_index": 0, "action": "goto", "target": None, "value": "/test", "status": "passed", "error_message": None, "resolved_by": None},
                    {"step_index": 1, "action": "click", "target": "#btn", "value": None, "status": "failed", "error_message": "Element not found", "resolved_by": None},
                ],
            },
        )
        db_session.add(execution)
        db_session.commit()

        from app.ai.planning_tools import _handle_get_execution_detail

        result = _handle_get_execution_detail(
            params={"run_id": str(execution.id)},
            db_session=db_session,
            project_id=1,
        )
        assert result["id"] == execution.id
        assert result["status"] == "failed"
        assert len(result["steps"]) == 2
        assert result["steps"][1]["status"] == "failed"
        assert result["steps"][1]["error_message"] == "Element not found"

    def test_missing_run_id_returns_error(self, db_session: Session) -> None:
        """Should return error when run_id is missing."""
        from app.ai.planning_tools import _handle_get_execution_detail

        result = _handle_get_execution_detail(
            params={},
            db_session=db_session,
            project_id=1,
        )
        assert "error" in result

    def test_nonexistent_run_returns_error(self, db_session: Session) -> None:
        """Should return error when run_id does not exist."""
        from app.ai.planning_tools import _handle_get_execution_detail

        result = _handle_get_execution_detail(
            params={"run_id": "99999"},
            db_session=db_session,
            project_id=1,
        )
        assert "error" in result


class TestGetProjectTestStatus:
    """Tests for _handle_get_project_test_status handler."""

    def test_returns_no_runs_when_empty(self, db_session: Session) -> None:
        """Should return no_runs conclusion when no executions exist."""
        from app.ai.planning_tools import _handle_get_project_test_status

        result = _handle_get_project_test_status(
            params={},
            db_session=db_session,
            project_id=1,
        )
        assert result["conclusion"] == "no_runs"
        assert result["cases"] == []

    def test_returns_all_passed(self, db_session: Session) -> None:
        """Should return all_passed when all cases have passing latest runs."""
        from app.models import TestCaseRun
        from app.services import cases as case_service

        case = case_service.create_case(
            db_session,
            CaseCreateRequest(
                project_id=1,
                name="Passing Case",
                description=None,
                steps=[{"action": "goto", "value": "/test"}],
            ),
            actor_user_id=1,
        )
        db_session.flush()
        execution = TestCaseRun(
            case_id=case.id, project_id=1, triggered_by=1, status="passed",
        )
        db_session.add(execution)
        db_session.commit()

        from app.ai.planning_tools import _handle_get_project_test_status

        result = _handle_get_project_test_status(
            params={}, db_session=db_session, project_id=1,
        )
        assert result["conclusion"] == "all_passed"
        assert len(result["cases"]) == 1
        assert result["cases"][0]["latest_status"] == "passed"

    def test_returns_partial_when_mixed(self, db_session: Session) -> None:
        """Should return partial when some cases pass and some fail."""
        from app.models import TestCaseRun
        from app.services import cases as case_service

        case_a = case_service.create_case(
            db_session,
            CaseCreateRequest(project_id=1, name="Case A", description=None, steps=[{"action": "goto", "value": "/a"}]),
            actor_user_id=1,
        )
        case_b = case_service.create_case(
            db_session,
            CaseCreateRequest(project_id=1, name="Case B", description=None, steps=[{"action": "goto", "value": "/b"}]),
            actor_user_id=1,
        )
        db_session.flush()
        db_session.add(TestCaseRun(case_id=case_a.id, project_id=1, triggered_by=1, status="passed"))
        db_session.add(TestCaseRun(case_id=case_b.id, project_id=1, triggered_by=1, status="failed"))
        db_session.commit()

        from app.ai.planning_tools import _handle_get_project_test_status

        result = _handle_get_project_test_status(
            params={}, db_session=db_session, project_id=1,
        )
        assert result["conclusion"] == "partial"


class TestGetFailureAnalysis:
    """Tests for _handle_get_failure_analysis handler."""

    def test_returns_empty_when_no_failures(self, db_session: Session) -> None:
        """Should return empty when all runs passed."""
        from app.models import TestCaseRun
        from app.services import cases as case_service

        case = case_service.create_case(
            db_session,
            CaseCreateRequest(project_id=1, name="OK Case", description=None, steps=[{"action": "goto", "value": "/"}]),
            actor_user_id=1,
        )
        db_session.flush()
        db_session.add(TestCaseRun(case_id=case.id, project_id=1, triggered_by=1, status="passed"))
        db_session.commit()

        from app.ai.planning_tools import _handle_get_failure_analysis

        result = _handle_get_failure_analysis(
            params={}, db_session=db_session, project_id=1,
        )
        assert result["failure_patterns"] == []

    def test_detects_consecutive_failures(self, db_session: Session) -> None:
        """Should detect consecutive failure count for a case."""
        from app.models import TestCaseRun
        from app.services import cases as case_service

        case = case_service.create_case(
            db_session,
            CaseCreateRequest(project_id=1, name="Flaky Case", description=None, steps=[{"action": "goto", "value": "/"}]),
            actor_user_id=1,
        )
        db_session.flush()
        db_session.add(TestCaseRun(case_id=case.id, project_id=1, triggered_by=1, status="failed"))
        db_session.add(TestCaseRun(case_id=case.id, project_id=1, triggered_by=1, status="failed"))
        db_session.commit()

        from app.ai.planning_tools import _handle_get_failure_analysis

        result = _handle_get_failure_analysis(
            params={}, db_session=db_session, project_id=1,
        )
        assert len(result["failure_patterns"]) == 1
        assert result["failure_patterns"][0]["case_name"] == "Flaky Case"
        assert result["failure_patterns"][0]["consecutive_failures"] == 2

    def test_detects_flaky_pattern(self, db_session: Session) -> None:
        """Should flag alternating pass/fail as suspected flaky."""
        from app.models import TestCaseRun
        from app.services import cases as case_service

        case = case_service.create_case(
            db_session,
            CaseCreateRequest(project_id=1, name="Unstable", description=None, steps=[{"action": "goto", "value": "/"}]),
            actor_user_id=1,
        )
        db_session.flush()
        db_session.add(TestCaseRun(case_id=case.id, project_id=1, triggered_by=1, status="passed"))
        db_session.add(TestCaseRun(case_id=case.id, project_id=1, triggered_by=1, status="failed"))
        db_session.add(TestCaseRun(case_id=case.id, project_id=1, triggered_by=1, status="passed"))
        db_session.add(TestCaseRun(case_id=case.id, project_id=1, triggered_by=1, status="failed"))
        db_session.commit()

        from app.ai.planning_tools import _handle_get_failure_analysis

        result = _handle_get_failure_analysis(
            params={}, db_session=db_session, project_id=1,
        )
        assert len(result["failure_patterns"]) == 1
        assert result["failure_patterns"][0]["suspected_flaky"] is True

    def test_filters_by_case_id(self, db_session: Session) -> None:
        """Should only analyze specific case when case_id is provided."""
        from app.ai.planning_tools import _handle_get_failure_analysis

        result = _handle_get_failure_analysis(
            params={"case_id": "99999"},
            db_session=db_session,
            project_id=1,
        )
        assert result["failure_patterns"] == []


class TestGetRecommendedRetest:
    """Tests for _handle_get_recommended_retest handler."""

    def test_no_retest_when_all_passed(self, db_session: Session) -> None:
        """Should recommend no retest when all cases pass."""
        from app.ai.planning_tools import _handle_get_recommended_retest

        result = _handle_get_recommended_retest(
            params={}, db_session=db_session, project_id=1,
        )
        assert result["recommendation"] == "no_retest_needed"
        assert result["retest_cases"] == []

    def test_single_failure_recommends_current_scope(self, db_session: Session) -> None:
        """Should recommend targeted retest when single case fails."""
        from app.models import TestCaseRun
        from app.services import cases as case_service

        case = case_service.create_case(
            db_session,
            CaseCreateRequest(project_id=1, name="Fail Case", description=None, steps=[{"action": "goto", "value": "/"}]),
            actor_user_id=1,
        )
        db_session.flush()
        db_session.add(TestCaseRun(
            case_id=case.id, project_id=1, triggered_by=1, status="failed",
            report={"steps": [{"step_index": 0, "action": "click", "status": "failed", "error_message": "not found"}]},
        ))
        db_session.commit()

        from app.ai.planning_tools import _handle_get_recommended_retest

        result = _handle_get_recommended_retest(
            params={}, db_session=db_session, project_id=1,
        )
        assert result["recommendation"] in ("targeted_retest", "regression")
        assert len(result["retest_cases"]) == 1
        assert result["retest_cases"][0]["case_id"] == case.id
        assert result["regression_scope"] in ("current", "adjacent", "module", "core")

    def test_multiple_failures_widen_scope(self, db_session: Session) -> None:
        """Should recommend wider scope when many cases fail."""
        from app.models import TestCaseRun
        from app.services import cases as case_service

        cases = []
        for i in range(4):
            c = case_service.create_case(
                db_session,
                CaseCreateRequest(project_id=1, name=f"Case {i}", description=None, steps=[{"action": "goto", "value": "/"}]),
                actor_user_id=1,
            )
            cases.append(c)
        db_session.flush()
        for c in cases[:3]:
            db_session.add(TestCaseRun(case_id=c.id, project_id=1, triggered_by=1, status="failed"))
        db_session.commit()

        from app.ai.planning_tools import _handle_get_recommended_retest

        result = _handle_get_recommended_retest(
            params={}, db_session=db_session, project_id=1,
        )
        assert result["regression_scope"] in ("adjacent", "module", "core")
        assert len(result["retest_cases"]) == 3
