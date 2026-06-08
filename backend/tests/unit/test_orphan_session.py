"""Tests for orphan session handling — sessions that exist in frontend but not in DB."""

from __future__ import annotations

import pytest
from sqlalchemy.orm import Session

from app.models import AIPlanningSession


def test_list_sessions_includes_only_existing_sessions(client, db_session: Session) -> None:
    """Verify that list_planning_sessions only returns sessions that exist in DB."""
    # Create two sessions
    resp1 = client.post("/api/v1/ai-planning/sessions", json={})
    assert resp1.status_code == 201
    session_id_1 = resp1.json()["session"]["id"]

    resp2 = client.post("/api/v1/ai-planning/sessions", json={})
    assert resp2.status_code == 201
    session_id_2 = resp2.json()["session"]["id"]

    # List should contain both
    list_resp = client.get("/api/v1/ai-planning/sessions")
    assert list_resp.status_code == 200
    session_ids = [s["id"] for s in list_resp.json()]
    assert session_id_1 in session_ids
    assert session_id_2 in session_ids

    # Delete session 1
    delete_resp = client.delete(f"/api/v1/ai-planning/sessions/{session_id_1}")
    assert delete_resp.status_code == 204

    # List should only contain session 2
    list_resp = client.get("/api/v1/ai-planning/sessions")
    assert list_resp.status_code == 200
    session_ids = [s["id"] for s in list_resp.json()]
    assert session_id_1 not in session_ids
    assert session_id_2 in session_ids


def test_send_message_to_deleted_session_returns_404(client, db_session: Session) -> None:
    """Verify that sending a message to a deleted session returns 404."""
    # Create a session
    create_resp = client.post("/api/v1/ai-planning/sessions", json={})
    assert create_resp.status_code == 201
    session_id = create_resp.json()["session"]["id"]

    # Delete the session
    delete_resp = client.delete(f"/api/v1/ai-planning/sessions/{session_id}")
    assert delete_resp.status_code == 204

    # Try to send a message to the deleted session
    msg_resp = client.post(
        f"/api/v1/ai-planning/sessions/{session_id}/messages",
        json={"content": "test message"},
    )
    assert msg_resp.status_code == 404


def test_get_detail_of_deleted_session_returns_404(client, db_session: Session) -> None:
    """Verify that getting detail of a deleted session returns 404."""
    # Create a session
    create_resp = client.post("/api/v1/ai-planning/sessions", json={})
    assert create_resp.status_code == 201
    session_id = create_resp.json()["session"]["id"]

    # Delete the session
    delete_resp = client.delete(f"/api/v1/ai-planning/sessions/{session_id}")
    assert delete_resp.status_code == 204

    # Try to get detail of the deleted session
    detail_resp = client.get(f"/api/v1/ai-planning/sessions/{session_id}")
    assert detail_resp.status_code == 404


def test_generate_drafts_for_deleted_session_returns_404(client, db_session: Session) -> None:
    """Verify that generating drafts for a deleted session returns 404."""
    # Create a session
    create_resp = client.post("/api/v1/ai-planning/sessions", json={})
    assert create_resp.status_code == 201
    session_id = create_resp.json()["session"]["id"]

    # Delete the session
    delete_resp = client.delete(f"/api/v1/ai-planning/sessions/{session_id}")
    assert delete_resp.status_code == 204

    # Try to generate drafts for the deleted session
    drafts_resp = client.post(
        f"/api/v1/ai-planning/sessions/{session_id}/drafts",
        json={"scenario_keys": ["test"]},
    )
    assert drafts_resp.status_code == 404


def test_concurrent_session_operations(client, db_session: Session) -> None:
    """Verify that concurrent operations on the same session don't cause issues."""
    # Create a session
    create_resp = client.post("/api/v1/ai-planning/sessions", json={})
    assert create_resp.status_code == 201
    session_id = create_resp.json()["session"]["id"]

    # Try to delete the session twice
    delete_resp1 = client.delete(f"/api/v1/ai-planning/sessions/{session_id}")
    assert delete_resp1.status_code == 204

    # Second delete should return 404
    delete_resp2 = client.delete(f"/api/v1/ai-planning/sessions/{session_id}")
    assert delete_resp2.status_code == 404


def test_session_list_refreshes_after_deletion(client, db_session: Session) -> None:
    """Verify that session list is consistent after deletion."""
    # Create multiple sessions
    session_ids = []
    for _ in range(3):
        resp = client.post("/api/v1/ai-planning/sessions", json={})
        assert resp.status_code == 201
        session_ids.append(resp.json()["session"]["id"])

    # List should contain all sessions
    list_resp = client.get("/api/v1/ai-planning/sessions")
    assert list_resp.status_code == 200
    listed_ids = [s["id"] for s in list_resp.json()]
    for sid in session_ids:
        assert sid in listed_ids

    # Delete middle session
    client.delete(f"/api/v1/ai-planning/sessions/{session_ids[1]}")

    # List should not contain deleted session
    list_resp = client.get("/api/v1/ai-planning/sessions")
    assert list_resp.status_code == 200
    listed_ids = [s["id"] for s in list_resp.json()]
    assert session_ids[1] not in listed_ids
    assert session_ids[0] in listed_ids
    assert session_ids[2] in listed_ids
