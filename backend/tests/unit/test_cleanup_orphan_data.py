"""Safety tests for the orphan-data cleanup script."""

from __future__ import annotations

import importlib.util
from pathlib import Path

import pytest

from app.models import AIPlanningSession, Project, ProjectMember, SessionProject, TestCase


def test_find_orphaned_projects_only_returns_empty_unlinked_projects(db_session) -> None:
    cleanup = _load_cleanup_module()
    db_session.add_all(
        [
            Project(id=2, name="Default", is_default=True),
            Project(id=3, name="Member Owned"),
            Project(id=4, name="Has Cases"),
            Project(id=5, name="Empty"),
            Project(id=6, name="Linked Session"),
        ]
    )
    db_session.flush()
    db_session.add(ProjectMember(project_id=3, user_id=1, role="owner"))
    db_session.add(
        TestCase(
            project_id=4,
            created_by=1,
            updated_by=1,
            name="Protected Case",
            dsl={"name": "Protected Case", "steps": [{"action": "goto", "value": "/"}]},
        )
    )
    db_session.add(AIPlanningSession(id=1, actor_user_id=1))
    db_session.flush()
    db_session.add(SessionProject(session_id=1, project_id=6))
    db_session.commit()

    orphaned_ids = {project.id for project in cleanup.find_orphaned_projects(db_session)}

    assert orphaned_ids == {5}


def test_cleanup_requires_explicit_confirmation() -> None:
    cleanup = _load_cleanup_module()

    with pytest.raises(ValueError, match="DELETE_ORPHANED_DATA"):
        cleanup.cleanup_orphaned_data(dry_run=False)


def _load_cleanup_module():
    script_path = Path(__file__).resolve().parents[2] / "scripts" / "cleanup_orphan_data.py"
    spec = importlib.util.spec_from_file_location("cleanup_orphan_data", script_path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"Unable to load cleanup script from {script_path}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module
