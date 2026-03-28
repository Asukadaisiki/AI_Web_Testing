"""Shared pytest fixtures."""

from __future__ import annotations

from collections.abc import Generator

import pytest
from fastapi.testclient import TestClient
from sqlalchemy.orm import Session

from app.core.auth import hash_password
from app.db import Base, get_db_session
from app.core.config import get_settings
from app.db.session import get_engine, get_session_factory
from app.main import create_app
from app.models import Project, ProjectMember, User
from app.services.dsl import reset_dsl_generation_runtime_stats


@pytest.fixture(autouse=True)
def reset_cached_state(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delenv("APP_ENV", raising=False)
    monkeypatch.delenv("APP_DEBUG", raising=False)
    monkeypatch.delenv("AUTH_SESSION_SECRET", raising=False)
    monkeypatch.delenv("AUTH_SESSION_COOKIE_NAME", raising=False)
    monkeypatch.delenv("AUTH_SESSION_MAX_AGE_SECONDS", raising=False)
    monkeypatch.delenv("AUTH_SESSION_SAME_SITE", raising=False)
    monkeypatch.delenv("AUTH_SESSION_HTTPS_ONLY", raising=False)
    monkeypatch.delenv("DATABASE_URL", raising=False)
    monkeypatch.delenv("DATABASE_ECHO", raising=False)
    monkeypatch.delenv("ENABLE_AI_DSL_GENERATE", raising=False)
    monkeypatch.delenv("AI_DSL_TIMEOUT_MS", raising=False)
    monkeypatch.delenv("AI_DSL_API_KEY", raising=False)
    monkeypatch.delenv("AI_DSL_BASE_URL", raising=False)
    monkeypatch.delenv("AI_DSL_MODEL", raising=False)
    monkeypatch.delenv("AI_DSL_STRICT_MODE", raising=False)
    monkeypatch.delenv("AI_DSL_ALLOW_AUTO_REPAIR", raising=False)
    get_settings.cache_clear()
    get_engine.cache_clear()
    get_session_factory.cache_clear()
    reset_dsl_generation_runtime_stats()
    yield
    get_settings.cache_clear()
    get_engine.cache_clear()
    get_session_factory.cache_clear()
    reset_dsl_generation_runtime_stats()


@pytest.fixture
def db_session(monkeypatch: pytest.MonkeyPatch, tmp_path) -> Generator[Session, None, None]:
    database_path = tmp_path / "test.db"
    monkeypatch.setenv("DATABASE_URL", f"sqlite:///{database_path.as_posix()}")
    get_settings.cache_clear()
    get_engine.cache_clear()
    get_session_factory.cache_clear()

    engine = get_engine()
    Base.metadata.create_all(bind=engine)
    session_factory = get_session_factory()

    with session_factory() as session:
        session.add(
            User(
                id=1,
                email="seed-owner@example.com",
                display_name="Seed Owner",
                password_hash=hash_password("password123"),
                is_active=True,
            )
        )
        session.add(Project(id=1, name="Default Project", description="Seed project for tests."))
        session.flush()
        session.add(ProjectMember(id=1, project_id=1, user_id=1, role="owner"))
        session.commit()
        yield session


@pytest.fixture
def app_instance(db_session: Session):
    return create_app()


@pytest.fixture
def client(app_instance, db_session: Session) -> Generator[TestClient, None, None]:
    def override_get_db_session() -> Generator[Session, None, None]:
        yield db_session

    app_instance.dependency_overrides[get_db_session] = override_get_db_session
    try:
        with TestClient(app_instance) as test_client:
            login_response = test_client.post(
                "/api/v1/auth/login",
                json={"email": "seed-owner@example.com", "password": "password123"},
            )
            assert login_response.status_code == 200
            yield test_client
    finally:
        app_instance.dependency_overrides.clear()


@pytest.fixture
def anonymous_client(app_instance, db_session: Session) -> Generator[TestClient, None, None]:
    def override_get_db_session() -> Generator[Session, None, None]:
        yield db_session

    app_instance.dependency_overrides[get_db_session] = override_get_db_session
    try:
        with TestClient(app_instance) as test_client:
            yield test_client
    finally:
        app_instance.dependency_overrides.clear()
