"""Tests for locator preflight (a11y_nodes input) and segment regeneration."""

from __future__ import annotations

import json
from unittest.mock import patch

import pytest

from app.ai.locator_preflight import apply_preflight_to_dsl
from app.ai.dsl_generator import DslGenerationError, _regen_segment


# ── Helpers ──────────────────────────────────────────────────────────────────

def _make_a11y_node(name: str, role: str = "button", **overrides) -> dict:
    node = {
        "node_id": f"e{hash(name) % 1000}",
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


def _make_dsl_case(*targets: str) -> dict:
    steps = []
    for i, t in enumerate(targets):
        steps.append({
            "step_index": i + 1,
            "action": "click",
            "target": t,
        })
    return {"steps": steps, "base_url": "https://example.com"}


# ── apply_preflight_to_dsl ───────────────────────────────────────────────────

class TestPreflightA11yNodes:
    def test_exact_match_single_node(self):
        nodes = [_make_a11y_node("Login")]
        case = _make_dsl_case("Login")
        result = apply_preflight_to_dsl(case, nodes)

        step = result["steps"][0]
        assert step["locator_confidence"] == "high"
        assert step["match_count"] == 1
        assert len(step["candidates"]) == 3

    def test_substring_match(self):
        nodes = [_make_a11y_node("Add to Cart Button")]
        case = _make_dsl_case("Add to Cart")
        result = apply_preflight_to_dsl(case, nodes)

        step = result["steps"][0]
        assert step["locator_confidence"] == "high"
        assert step["match_count"] == 1

    def test_case_insensitive(self):
        nodes = [_make_a11y_node("LOGIN")]
        case = _make_dsl_case("login")
        result = apply_preflight_to_dsl(case, nodes)

        step = result["steps"][0]
        assert step["locator_confidence"] == "high"

    def test_no_match(self):
        nodes = [_make_a11y_node("Login")]
        case = _make_dsl_case("NonexistentElement")
        result = apply_preflight_to_dsl(case, nodes)

        step = result["steps"][0]
        assert step["locator_confidence"] == "low"
        assert step["match_count"] == 0
        assert step["candidates"] == []

    def test_ambiguous_match(self):
        nodes = [
            _make_a11y_node("Add to Cart", node_id="e1"),
            _make_a11y_node("Add to Cart", node_id="e2"),
        ]
        case = _make_dsl_case("Add to Cart")
        result = apply_preflight_to_dsl(case, nodes)

        step = result["steps"][0]
        assert step["locator_confidence"] == "medium"
        assert step["match_count"] == 2
        assert len(step["candidates"]) == 6  # 3 per node × 2 nodes

    def test_empty_steps(self):
        case = {"steps": [], "base_url": "https://example.com"}
        result = apply_preflight_to_dsl(case, [_make_a11y_node("Login")])
        assert result == case

    def test_empty_nodes(self):
        case = _make_dsl_case("Login")
        result = apply_preflight_to_dsl(case, [])
        assert result == case

    def test_candidates_structure(self):
        nodes = [_make_a11y_node("Login", role="button")]
        case = _make_dsl_case("Login")
        result = apply_preflight_to_dsl(case, nodes)

        candidates = result["steps"][0]["candidates"]
        assert len(candidates) == 3

        role_exact = candidates[0]
        assert role_exact["strategy"] == "role"
        assert role_exact["selector"] == "button"
        assert role_exact["semantic_value"] == "Login"
        assert role_exact["pre_score"] == 0.90

        role_fuzzy = candidates[1]
        assert role_fuzzy["strategy"] == "role_fuzzy"
        assert role_fuzzy["pre_score"] == 0.75

        text = candidates[2]
        assert text["strategy"] == "text"
        assert text["pre_score"] == 0.55

    def test_overall_confidence_low_wins(self):
        nodes = [_make_a11y_node("Login")]
        case = _make_dsl_case("Login", "Nonexistent")
        result = apply_preflight_to_dsl(case, nodes)

        pf = result["_preflight"]
        assert pf["locator_confidence"] == "low"

    def test_warnings_for_unmatched(self):
        nodes = [_make_a11y_node("Login")]
        case = _make_dsl_case("Login", "Nonexistent")
        result = apply_preflight_to_dsl(case, nodes)

        pf = result["_preflight"]
        assert len(pf["warnings"]) == 1
        assert "Nonexistent" not in pf["warnings"][0]  # warning says step index, not name
        assert "match_count=0" in pf["warnings"][0]


# ── _regen_segment ───────────────────────────────────────────────────────────

class TestRegenSegment:
    def test_valid_response(self):
        fake_response = json.dumps({
            "steps": [
                {"action": "click", "target": "Login", "step_index": 1},
                {"action": "wait_for", "target": "Welcome", "step_index": 2},
            ]
        })

        with patch("app.ai.dsl_generator._call_dsl_flash_llm", return_value=fake_response):
            steps = _regen_segment(
                scenario_key="sc1",
                page_state="S0",
                missing_targets=["Signup / Login", "Password"],
                a11y_nodes=[
                    _make_a11y_node("Login", role="button"),
                    _make_a11y_node("Welcome", role="heading"),
                ],
                base_url="https://example.com",
            )

        assert isinstance(steps, list)
        assert len(steps) == 2
        assert all("action" in s for s in steps)

    def test_empty_steps(self):
        fake_response = json.dumps({"steps": []})

        with patch("app.ai.dsl_generator._call_dsl_flash_llm", return_value=fake_response):
            steps = _regen_segment(
                scenario_key="sc1",
                page_state="S0",
                missing_targets=["X"],
                a11y_nodes=[_make_a11y_node("Login")],
                base_url="https://example.com",
            )

        assert steps == []

    def test_invalid_json(self):
        with patch("app.ai.dsl_generator._call_dsl_flash_llm", return_value="not json"):
            with pytest.raises((DslGenerationError, json.JSONDecodeError)):
                _regen_segment(
                    scenario_key="sc1",
                    page_state="S0",
                    missing_targets=["X"],
                    a11y_nodes=[],
                    base_url="https://example.com",
                )

    def test_non_dict_response(self):
        with patch("app.ai.dsl_generator._call_dsl_flash_llm", return_value='[1, 2, 3]'):
            with pytest.raises(DslGenerationError):
                _regen_segment(
                    scenario_key="sc1",
                    page_state="S0",
                    missing_targets=["X"],
                    a11y_nodes=[],
                    base_url="https://example.com",
                )

    def test_prompt_contains_missing_targets(self):
        captured_messages = []

        def mock_llm(messages, **kwargs):
            captured_messages.extend(messages)
            return json.dumps({"steps": []})

        with patch("app.ai.dsl_generator._call_dsl_flash_llm", side_effect=mock_llm):
            _regen_segment(
                scenario_key="sc1",
                page_state="S0",
                missing_targets=["Signup / Login", "Password"],
                a11y_nodes=[_make_a11y_node("Login")],
                base_url="https://example.com",
            )

        user_msg = captured_messages[1]["content"]
        assert "Signup / Login" in user_msg
        assert "Password" in user_msg

    def test_prompt_contains_node_names(self):
        captured_messages = []

        def mock_llm(messages, **kwargs):
            captured_messages.extend(messages)
            return json.dumps({"steps": []})

        with patch("app.ai.dsl_generator._call_dsl_flash_llm", side_effect=mock_llm):
            _regen_segment(
                scenario_key="sc1",
                page_state="S0",
                missing_targets=["X"],
                a11y_nodes=[
                    _make_a11y_node("Login", role="button"),
                    _make_a11y_node("Products", role="link"),
                ],
                base_url="https://example.com",
            )

        user_msg = captured_messages[1]["content"]
        assert "Login" in user_msg
        assert "button" in user_msg
        assert "Products" in user_msg
        assert "link" in user_msg
