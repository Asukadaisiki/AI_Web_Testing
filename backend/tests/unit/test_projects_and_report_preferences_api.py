"""Tests for project listing and report preference endpoints."""

from __future__ import annotations

from datetime import UTC, datetime, timedelta

from app.core.auth import hash_password
from app.models import Project, ProjectMember, TestCase as CaseModel, TestCaseRun as CaseRunModel, User


def test_list_projects_returns_only_current_user_projects(client, db_session) -> None:
    db_session.add_all(
        [
            Project(id=2, name="Orders Project", description="orders"),
            Project(id=3, name="Hidden Project", description="hidden"),
            User(
                id=2,
                email="another-user@example.com",
                display_name="Another User",
                password_hash=hash_password("password123"),
                is_active=True,
            ),
        ]
    )
    db_session.flush()
    db_session.add_all(
        [
            ProjectMember(project_id=2, user_id=1, role="owner"),
            ProjectMember(project_id=3, user_id=2, role="owner"),
        ]
    )
    db_session.commit()

    response = client.get("/api/v1/projects")

    assert response.status_code == 200
    assert response.json() == [
        {"id": 1, "name": "Default Project", "description": "Seed project for tests."},
        {"id": 2, "name": "Orders Project", "description": "orders"},
    ]


def test_get_report_preference_defaults_to_recent_active_project(client, db_session) -> None:
    db_session.add_all(
        [
            Project(id=2, name="Recent Project", description="recent"),
            ProjectMember(project_id=2, user_id=1, role="owner"),
        ]
    )
    db_session.flush()
    db_session.add(
        CaseModel(
            id=1,
            project_id=2,
            created_by=1,
            updated_by=1,
            name="Recently Edited Case",
            description=None,
            dsl={"name": "Recently Edited Case", "base_url": "https://example.com", "steps": [{"action": "goto", "value": "/"}]},
        )
    )
    db_session.commit()

    response = client.get("/api/v1/reports/preferences")

    assert response.status_code == 200
    assert response.json() == {
        "scope_type": "project",
        "project_id": 2,
        "case_id": None,
        "window_days": 7,
    }


def test_get_report_preference_falls_back_to_first_accessible_project_when_no_activity(client, db_session) -> None:
    db_session.add_all(
        [
            Project(id=2, name="Alpha Project", description="alpha"),
            ProjectMember(project_id=2, user_id=1, role="owner"),
        ]
    )
    db_session.commit()

    response = client.get("/api/v1/reports/preferences")

    assert response.status_code == 200
    assert response.json() == {
        "scope_type": "project",
        "project_id": 2,
        "case_id": None,
        "window_days": 7,
    }


def test_report_preference_round_trip_is_user_scoped(anonymous_client, client, db_session) -> None:
    db_session.add_all(
        [
            Project(id=2, name="Project Two", description="two"),
            User(
                id=2,
                email="report-user@example.com",
                display_name="Report User",
                password_hash=hash_password("password123"),
                is_active=True,
            ),
        ]
    )
    db_session.flush()
    db_session.add_all(
        [
            ProjectMember(project_id=2, user_id=1, role="owner"),
            ProjectMember(project_id=2, user_id=2, role="owner"),
            CaseModel(
                id=1,
                project_id=2,
                created_by=1,
                updated_by=1,
                name="Scoped Case",
                description=None,
                dsl={"name": "Scoped Case", "base_url": "https://example.com", "steps": [{"action": "goto", "value": "/"}]},
            ),
        ]
    )
    db_session.commit()

    update_response = client.put(
        "/api/v1/reports/preferences",
        json={"scope_type": "case", "project_id": 2, "case_id": 1, "window_days": 14},
    )
    assert update_response.status_code == 200
    assert update_response.json() == {
        "scope_type": "case",
        "project_id": 2,
        "case_id": 1,
        "window_days": 14,
    }

    response = client.get("/api/v1/reports/preferences")
    assert response.status_code == 200
    assert response.json() == {
        "scope_type": "case",
        "project_id": 2,
        "case_id": 1,
        "window_days": 14,
    }

    login_response = anonymous_client.post(
        "/api/v1/auth/login",
        json={"email": "report-user@example.com", "password": "password123"},
    )
    assert login_response.status_code == 200

    other_user_response = anonymous_client.get("/api/v1/reports/preferences")
    assert other_user_response.status_code == 200
    assert other_user_response.json() == {
        "scope_type": "project",
        "project_id": 2,
        "case_id": None,
        "window_days": 7,
    }


def test_recent_activity_ignores_suite_updates_and_prefers_case_or_execution_activity(client, db_session) -> None:
    db_session.add_all(
        [
            Project(id=2, name="Project Two", description="two"),
            ProjectMember(project_id=2, user_id=1, role="owner"),
            CaseModel(
                id=1,
                project_id=2,
                created_by=1,
                updated_by=1,
                name="Recently Edited Case",
                description=None,
                dsl={"name": "Recently Edited Case", "base_url": "https://example.com", "steps": [{"action": "goto", "value": "/"}]},
            ),
            CaseModel(
                id=2,
                project_id=2,
                created_by=1,
                updated_by=1,
                name="Executed Case",
                description=None,
                dsl={"name": "Executed Case", "base_url": "https://example.com", "steps": [{"action": "goto", "value": "/"}]},
            ),
        ]
    )
    db_session.flush()
    now = datetime.now(UTC).replace(tzinfo=None, hour=12, minute=0, second=0, microsecond=0)
    db_session.add(
        CaseRunModel(
            id=1,
            case_id=2,
            project_id=2,
            triggered_by=1,
            status="passed",
            error_message=None,
            report={"status": "passed", "steps": [{"step_index": 0, "action": "goto", "value": "/", "status": "passed"}]},
            started_at=now,
            finished_at=now + timedelta(milliseconds=100),
        )
    )
    case = db_session.get(CaseModel, 1)
    assert case is not None
    case.updated_at = now - timedelta(hours=1)
    db_session.commit()

    response = client.get("/api/v1/reports/preferences")

    assert response.status_code == 200
    assert response.json() == {
        "scope_type": "project",
        "project_id": 2,
        "case_id": None,
        "window_days": 7,
    }
