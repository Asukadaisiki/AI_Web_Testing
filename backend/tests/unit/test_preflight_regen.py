"""Tests for locator preflight (a11y_nodes input)."""

from __future__ import annotations

from app.ai.locator_preflight import apply_preflight_to_dsl


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
        assert step["locator_confidence"] == "low"
        assert step["match_count"] == 2
        assert len(step["candidates"]) == 6  # 3 per node × 2 nodes
        assert "repeated product action" in result["_preflight"]["warnings"][0]

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

