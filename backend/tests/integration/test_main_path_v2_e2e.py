"""Main path v2 E2E integration tests — A11y pipeline.

Layer 1: API-level tests (TestClient, no real browser)
Layer 2: Browser-level tests (@pytest.mark.browser_integration)
"""

from __future__ import annotations

import json
from datetime import UTC, datetime, timedelta
from typing import Any

import pytest
from sqlalchemy.orm import Session

from app.ai.locator_preflight import apply_preflight_to_dsl
from app.ai.page_explorer import USEFUL_A11Y_ROLES, collect_a11y_nodes
from app.models import AIPlanningToolResult, Project, SessionProject
from app.services.ai_planning import (
    _lookup_tool_cache,
    _normalize_cache_url,
    create_planning_session,
)
from app.schemas.ai_planning import CreateAIPlanningSessionRequest


# ── Helpers ──────────────────────────────────────────────────────────────────

def _make_a11y_node(name: str, role: str = "button", **overrides) -> dict:
    node = {
        "node_id": f"e{hash(name) % 10000}",
        "role": role,
        "name": name,
        "level": None,
        "parent_id": None,
        "focusable": True,
        "disabled": False,
        "page_state": "S0",
    }
    node.update(overrides)
    return node


# ── Layer 1: API-level integration tests ─────────────────────────────────────

class TestDefaultProjectAutoCreate:
    def test_session_without_project_creates_default(self, db_session: Session):
        req = CreateAIPlanningSessionRequest(project_id=None, case_id=None)
        detail = create_planning_session(db_session, req, actor_user_id=1)

        # Verify a SessionProject was created
        sp = db_session.scalars(
            __import__("sqlalchemy").select(SessionProject).where(
                SessionProject.session_id == detail.session.id
            )
        ).first()
        assert sp is not None

        # Verify the project has the default naming
        project = db_session.get(Project, sp.project_id)
        assert project is not None
        assert project.name == f"default-{detail.session.id}"

    def test_session_with_project_does_not_create_new(self, db_session: Session):
        req = CreateAIPlanningSessionRequest(project_id=1, case_id=None)
        detail = create_planning_session(db_session, req, actor_user_id=1)

        # Should link to existing project, not create a new one
        sp = db_session.scalars(
            __import__("sqlalchemy").select(SessionProject).where(
                SessionProject.session_id == detail.session.id
            )
        ).first()
        assert sp is not None
        assert sp.project_id == 1


class TestPreflightCandidatesInDraft:
    def test_preflight_produces_candidates(self):
        """apply_preflight_to_dsl with realistic a11y_nodes produces candidates."""
        nodes = [
            _make_a11y_node("Login", role="button"),
            _make_a11y_node("Signup / Login", role="link"),
            _make_a11y_node("Email Address", role="textbox"),
            _make_a11y_node("Password", role="textbox"),
            _make_a11y_node("Products", role="link"),
            _make_a11y_node("Cart", role="link"),
        ]

        dsl_case = {
            "steps": [
                {"step_index": 1, "action": "goto", "target": "/"},
                {"step_index": 2, "action": "click", "target": "Signup / Login"},
                {"step_index": 3, "action": "input", "target": "Email Address", "value": "test@example.com"},
                {"step_index": 4, "action": "input", "target": "Password", "value": "pass123"},
                {"step_index": 5, "action": "click", "target": "Login"},
                {"step_index": 6, "action": "click", "target": "Products"},
                {"step_index": 7, "action": "click", "target": "Cart"},
            ],
            "base_url": "https://automationexercise.com",
        }

        result = apply_preflight_to_dsl(dsl_case, nodes)

        # Steps with targets should have candidates
        steps_with_target = [s for s in result["steps"] if s.get("target")]
        for step in steps_with_target:
            if step["action"] not in ("goto",):
                assert "candidates" in step
                assert "locator_confidence" in step

        # Overall preflight metadata should exist
        assert "_preflight" in result
        assert "locator_confidence" in result["_preflight"]


class TestCacheHitReturnsCachedResult:
    def test_cache_hit_within_ttl(self, db_session: Session):
        """A cached explore_page result within TTL should be returned."""
        # Create a session first (FK requirement)
        from app.models import AIPlanningSession
        session_obj = AIPlanningSession(
            actor_user_id=1, status="collecting",
            requirements_json={}, missing_slots_json=[],
        )
        db_session.add(session_obj)
        db_session.flush()

        cached_data = {
            "url": "https://example.com/",
            "viewport": {"width": 1280, "height": 720},
            "a11y_nodes": [
                _make_a11y_node("Login", role="button"),
                _make_a11y_node("Products", role="link"),
            ],
            "element_count": 2,
        }
        record = AIPlanningToolResult(
            session_id=session_obj.id,
            tool_name="explore_page",
            raw_result_json=cached_data,
            summary_json={"url": "https://example.com/", "node_count": 2},
        )
        db_session.add(record)
        db_session.flush()

        normalized = _normalize_cache_url("https://example.com/")
        key = ("explore_page", session_obj.id, normalized, 1280, 720, "abc123")
        result = _lookup_tool_cache(db_session, key, ttl_hours=4)

        assert result is not None
        assert result["url"] == "https://example.com/"
        assert len(result["a11y_nodes"]) == 2

    def test_cache_miss_expired(self, db_session: Session):
        """An expired cache entry should return None."""
        from app.models import AIPlanningSession
        session_obj = AIPlanningSession(
            actor_user_id=1, status="collecting",
            requirements_json={}, missing_slots_json=[],
        )
        db_session.add(session_obj)
        db_session.flush()

        cached_data = {"url": "https://example.com/", "a11y_nodes": []}
        record = AIPlanningToolResult(
            session_id=session_obj.id,
            tool_name="explore_page",
            raw_result_json=cached_data,
            summary_json={},
            created_at=datetime.now(UTC) - timedelta(hours=5),
        )
        db_session.add(record)
        db_session.flush()

        normalized = _normalize_cache_url("https://example.com/")
        key = ("explore_page", session_obj.id, normalized, 1280, 720, "abc123")
        result = _lookup_tool_cache(db_session, key, ttl_hours=4)

        assert result is None


# ── Layer 2: Browser-level regression tests ──────────────────────────────────

@pytest.mark.browser_integration
class TestA11yExploration:
    def test_a11y_exploration_structure(self):
        """collect_a11y_nodes returns nodes with correct schema."""
        from playwright.sync_api import sync_playwright

        with sync_playwright() as pw:
            browser = pw.chromium.launch(headless=True)
            context = browser.new_context(viewport={"width": 1280, "height": 720})
            page = context.new_page()
            page.goto("https://the-internet.herokuapp.com", timeout=30000)
            page.wait_for_load_state("networkidle", timeout=30000)

            nodes = collect_a11y_nodes(page, page_state="S0")

            assert isinstance(nodes, list)
            assert len(nodes) > 0

            for node in nodes:
                assert "node_id" in node
                assert "role" in node
                assert "name" in node
                assert "focusable" in node
                assert "disabled" in node
                assert "page_state" in node
                assert node["page_state"] == "S0"

            context.close()
            browser.close()

    def test_a11y_roles_in_useful_set(self):
        """All returned nodes have roles in USEFUL_A11Y_ROLES."""
        from playwright.sync_api import sync_playwright

        with sync_playwright() as pw:
            browser = pw.chromium.launch(headless=True)
            context = browser.new_context(viewport={"width": 1280, "height": 720})
            page = context.new_page()
            page.goto("https://the-internet.herokuapp.com", timeout=30000)
            page.wait_for_load_state("networkidle", timeout=30000)

            nodes = collect_a11y_nodes(page, page_state="S0")

            for node in nodes:
                assert node["role"] in USEFUL_A11Y_ROLES, f"Unexpected role: {node['role']}"

            context.close()
            browser.close()

    def test_preflight_with_real_nodes(self):
        """Preflight with real a11y nodes from the-internet finds candidates."""
        from playwright.sync_api import sync_playwright

        with sync_playwright() as pw:
            browser = pw.chromium.launch(headless=True)
            context = browser.new_context(viewport={"width": 1280, "height": 720})
            page = context.new_page()
            page.goto("https://the-internet.herokuapp.com", timeout=30000)
            page.wait_for_load_state("networkidle", timeout=30000)

            nodes = collect_a11y_nodes(page, page_state="S0")
            context.close()
            browser.close()

        # Build a DSL case using real node names
        node_names = [n["name"] for n in nodes if n["name"]]
        if len(node_names) < 2:
            pytest.skip("Not enough named nodes on the page")

        targets = node_names[:3]
        dsl_case = {
            "steps": [
                {"step_index": i + 1, "action": "click", "target": t}
                for i, t in enumerate(targets)
            ],
            "base_url": "https://the-internet.herokuapp.com",
        }

        result = apply_preflight_to_dsl(dsl_case, nodes)

        matched = [s for s in result["steps"] if s.get("match_count", 0) > 0]
        assert len(matched) > 0, "At least one target should match a real a11y node"
