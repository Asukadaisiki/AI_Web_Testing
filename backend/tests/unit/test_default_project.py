import pytest
from sqlalchemy import select, func
from sqlalchemy.orm import Session
from sqlalchemy import create_engine

from app.db.base import Base
from app.models import Project, SessionProject
from app.schemas.ai_planning import CreateAIPlanningSessionRequest
from app.application.planning.session_service import create_planning_session


@pytest.fixture
def db_session():
    engine = create_engine("sqlite:///:memory:")
    Base.metadata.create_all(engine)
    with Session(engine) as s:
        yield s


def test_create_session_without_project_id_auto_creates_default(db_session):
    req = CreateAIPlanningSessionRequest(project_id=None, case_id=None)
    detail = create_planning_session(db_session, req, actor_user_id=1)

    sp = db_session.scalars(
        select(SessionProject).where(
            SessionProject.session_id == detail.session.id
        )
    ).first()
    assert sp is not None

    project = db_session.get(Project, sp.project_id)
    assert project is not None
    assert project.name == f"default-{detail.session.id}"
    assert project.description == "auto-created temporary project"


def test_create_session_with_existing_project_does_not_create_duplicate(db_session):
    db_session.add(Project(id=1, name="Existing Project"))
    db_session.commit()

    req = CreateAIPlanningSessionRequest(project_id=1, case_id=None)
    detail = create_planning_session(db_session, req, actor_user_id=1)

    count = db_session.scalar(
        select(func.count()).select_from(SessionProject).where(
            SessionProject.session_id == detail.session.id
        )
    )
    assert count == 1
