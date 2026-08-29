"""Unit tests for session-project association CRUD."""

from __future__ import annotations

import pytest
from sqlalchemy.orm import Session as SA_Session

from app.application.planning.project_context import (
    create_project_in_session,
    link_project_to_session,
    list_session_projects,
    unlink_project_from_session,
)
from app.application.planning.session_service import (
    create_planning_session,
    list_planning_sessions,
)
from app.models import AIPlanningSession, Project, User
from app.schemas.ai_planning import CreateAIPlanningSessionRequest


def _user_id(db_session: SA_Session) -> int:
    """Return the seeded user id from conftest db_session fixture."""
    return db_session.query(User).first().id


class TestCreateSessionWithoutProject:
    def test_creates_session_without_project(self, db_session: SA_Session) -> None:
        detail = create_planning_session(
            db_session,
            CreateAIPlanningSessionRequest(),
            actor_user_id=_user_id(db_session),
        )
        # Auto-creates a default project
        assert len(detail.session.projects) == 1
        assert detail.session.active_project_id == detail.session.projects[0].id
        assert detail.session.projects[0].is_active is True
        assert detail.session.status == "collecting"


class TestLinkProject:
    def test_link_project_to_session(self, db_session: SA_Session) -> None:
        uid = _user_id(db_session)
        project = Project(name="TestProject")
        db_session.add(project)
        db_session.commit()

        detail = create_planning_session(db_session, CreateAIPlanningSessionRequest(), actor_user_id=uid)
        result = link_project_to_session(db_session, detail.session.id, project_id=project.id, actor_user_id=uid)

        assert result.id == project.id
        assert result.name == "TestProject"
        assert result.is_active is True

    def test_link_nonexistent_project_raises(self, db_session: SA_Session) -> None:
        uid = _user_id(db_session)
        detail = create_planning_session(db_session, CreateAIPlanningSessionRequest(), actor_user_id=uid)
        with pytest.raises(Exception):
            link_project_to_session(db_session, detail.session.id, project_id=999, actor_user_id=uid)

    def test_link_existing_project_switches_active_project(self, db_session: SA_Session) -> None:
        uid = _user_id(db_session)
        project = Project(name="DupProject")
        db_session.add(project)
        db_session.commit()

        detail = create_planning_session(db_session, CreateAIPlanningSessionRequest(), actor_user_id=uid)
        link_project_to_session(db_session, detail.session.id, project_id=project.id, actor_user_id=uid)
        default_project_id = detail.session.active_project_id
        assert default_project_id is not None

        result = link_project_to_session(
            db_session,
            detail.session.id,
            project_id=default_project_id,
            actor_user_id=uid,
        )

        assert result.is_active is True
        assert db_session.get(AIPlanningSession, detail.session.id).active_project_id == default_project_id


class TestUnlinkProject:
    def test_unlink_project(self, db_session: SA_Session) -> None:
        uid = _user_id(db_session)
        project = Project(name="TestProject")
        db_session.add(project)
        db_session.commit()

        detail = create_planning_session(db_session, CreateAIPlanningSessionRequest(), actor_user_id=uid)
        link_project_to_session(db_session, detail.session.id, project_id=project.id, actor_user_id=uid)

        unlink_project_from_session(db_session, detail.session.id, project_id=project.id, actor_user_id=uid)

        projects = list_session_projects(db_session, detail.session.id, actor_user_id=uid)
        # default project still linked
        assert len(projects) == 1
        assert projects[0].is_active is True

    def test_unlink_nonexistent_raises(self, db_session: SA_Session) -> None:
        uid = _user_id(db_session)
        detail = create_planning_session(db_session, CreateAIPlanningSessionRequest(), actor_user_id=uid)
        with pytest.raises(Exception):
            unlink_project_from_session(db_session, detail.session.id, project_id=999, actor_user_id=uid)


class TestListSessionProjects:
    def test_returns_empty_when_no_projects(self, db_session: SA_Session) -> None:
        uid = _user_id(db_session)
        detail = create_planning_session(db_session, CreateAIPlanningSessionRequest(), actor_user_id=uid)
        projects = list_session_projects(db_session, detail.session.id, actor_user_id=uid)
        # auto-created default project
        assert len(projects) == 1

    def test_returns_linked_projects(self, db_session: SA_Session) -> None:
        uid = _user_id(db_session)
        p1 = Project(name="P1")
        p2 = Project(name="P2")
        db_session.add_all([p1, p2])
        db_session.commit()

        detail = create_planning_session(db_session, CreateAIPlanningSessionRequest(), actor_user_id=uid)
        link_project_to_session(db_session, detail.session.id, project_id=p1.id, actor_user_id=uid)
        link_project_to_session(db_session, detail.session.id, project_id=p2.id, actor_user_id=uid)

        projects = list_session_projects(db_session, detail.session.id, actor_user_id=uid)
        # default + P1 + P2
        assert len(projects) == 3
        names = {p.name for p in projects}
        assert names == {"P1", "P2", f"default-{detail.session.id}"}
        assert [p.name for p in projects if p.is_active] == ["P2"]


class TestCreateProjectInSession:
    def test_creates_and_links(self, db_session: SA_Session) -> None:
        uid = _user_id(db_session)
        detail = create_planning_session(db_session, CreateAIPlanningSessionRequest(), actor_user_id=uid)

        result = create_project_in_session(
            db_session, detail.session.id,
            name="NewProject", description="desc", actor_user_id=uid,
        )
        assert result.name == "NewProject"
        assert result.description == "desc"
        assert result.is_active is True

        projects = list_session_projects(db_session, detail.session.id, actor_user_id=uid)
        # default + NewProject
        assert len(projects) == 2
        pnames = {p.name for p in projects}
        assert "NewProject" in pnames
        assert [p.name for p in projects if p.is_active] == ["NewProject"]


class TestListSessionsWithProjects:
    def test_sessions_include_projects(self, db_session: SA_Session) -> None:
        uid = _user_id(db_session)
        project = Project(name="SharedProject")
        db_session.add(project)
        db_session.commit()

        detail = create_planning_session(db_session, CreateAIPlanningSessionRequest(), actor_user_id=uid)
        link_project_to_session(db_session, detail.session.id, project_id=project.id, actor_user_id=uid)

        sessions = list_planning_sessions(db_session, actor_user_id=uid)
        assert len(sessions) >= 1
        found = next(s for s in sessions if s.id == detail.session.id)
        # default project + SharedProject
        assert len(found.projects) == 2
        assert found.active_project_id == project.id
