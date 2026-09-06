"""Static locator preflight — validate DSL targets against collected page elements.

Runs without a live browser; all data comes from the DOM snapshots that
``explore_page`` / ``explore_flow`` already collected.
"""

from __future__ import annotations

import logging
import re
from copy import deepcopy
from typing import Any

logger = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# A11y role → Playwright role normalization
# ---------------------------------------------------------------------------

_A11Y_TO_PLAYWRIGHT_ROLE: dict[str, str] = {
    "searchbox": "search",
    "menuitemcheckbox": "checkbox",
    "menuitemradio": "radio",
}
"""Roles that differ between the a11y tree and Playwright's get_by_role()."""


def _normalize_role_for_playwright(role: str) -> str:
    """Map a11y roles to Playwright-compatible role names."""
    return _A11Y_TO_PLAYWRIGHT_ROLE.get(role, role)


_GENERIC_REPEATED_TARGETS = {"add to cart", "view product"}

# Matches: ... inside product "name"
_SCOPE_RE = re.compile(
    r"""\s+inside\s+product\s*["'](.+?)["']""",
    re.IGNORECASE,
)

def _target_is_generic_repeated_action(target: str) -> bool:
    return _normalize_text(target) in _GENERIC_REPEATED_TARGETS


def _normalize_text(value: str | None) -> str:
    if not value:
        return ""
    return re.sub(r"\s+", " ", value.strip().casefold())


def _is_business_node(node: dict[str, Any]) -> bool:
    dom = node.get("dom") or {}
    return not (dom.get("third_party_frame") or dom.get("advertising_context"))


def _verified_anchor_href(step: dict[str, Any]) -> str:
    for candidate in step.get("candidates") or []:
        features = candidate.get("pre_features") or {}
        href = str(features.get("verified_href") or "").strip()
        if href and not href.startswith("#"):
            return href
    return ""


def _has_navigation_postcondition(step: dict[str, Any], href: str) -> bool:
    for postcondition in step.get("postconditions") or []:
        if postcondition.get("type") != "url_contains":
            continue
        value = str(postcondition.get("value") or "").strip()
        if value and (value in href or href in value):
            return True
    return False


def apply_preflight_to_dsl(
    dsl_case: dict[str, Any],
    a11y_nodes: list[dict[str, Any]],
) -> dict[str, Any]:
    """Run preflight against a11y_nodes, write 1:N candidates + confidence.

    Matches step.target against a11y_node.name (exact + substring).
    Each match produces 3 candidates (role exact / role fuzzy / text).
    Mutates *dsl_case* in place and returns it.
    """
    steps = dsl_case.get("steps", [])
    a11y_nodes = [node for node in a11y_nodes if _is_business_node(node)]
    if not steps or not a11y_nodes:
        return dsl_case

    # Build parent→children index for scoped matching
    node_by_id: dict[str, dict[str, Any]] = {}
    children_of: dict[str, list[dict[str, Any]]] = {}
    for n in a11y_nodes:
        nid = n.get("node_id", "")
        if nid:
            node_by_id[nid] = n
        pid = n.get("parent_id")
        if pid:
            children_of.setdefault(pid, []).append(n)

    confidences: list[str] = []
    for step in steps:
        if not isinstance(step, dict):
            continue
        target = (step.get("target") or "").strip()
        if not target:
            continue

        # Parse scope: "link "Add to cart" inside product "Blue Top""
        scope_name = None
        scope_match = _SCOPE_RE.search(target)
        if scope_match:
            scope_name = scope_match.group(1).strip().lower()
            # Strip scope suffix for element matching
            target_core = target[:scope_match.start()].strip()
        else:
            target_core = target

        target_lower = target_core.lower()
        matches = [
            node
            for node in a11y_nodes
            if any(
                _normalize_text(selector.get("selector")) == _normalize_text(target_core)
                for selector in node.get("verified_selectors", [])
                if isinstance(selector, dict)
            )
        ]

        if not matches and scope_name:
            # Scoped matching: find the product container whose children
            # include the product name, then match target against its children.
            for n in a11y_nodes:
                if (n.get("role") or "").lower() != "product":
                    continue
                pid = n.get("node_id", "")
                children = children_of.get(pid, [])
                # Check if any child contains the product name
                container_matches_scope = False
                for child in children:
                    child_name = (child.get("name") or "").lower()
                    if child_name and (child_name == scope_name or scope_name in child_name):
                        container_matches_scope = True
                        break
                if not container_matches_scope:
                    continue
                # Found the right product container — match target against children
                for child in children:
                    cname = (child.get("name") or "").lower()
                    if cname and (cname == target_lower or target_lower in cname):
                        matches.append(child)
        elif not matches:
            # Unscoped matching: match against all nodes
            for n in a11y_nodes:
                name = (n.get("name") or "").lower()
                if not name:
                    continue
                if name == target_lower or target_lower in name:
                    matches.append(n)

        match_count = len(matches)
        candidates: list[dict] = []

        if match_count > 0:
            for n in matches:
                role = _normalize_role_for_playwright(n["role"])
                name = n["name"]
                scope_ctx = {"scope_name": scope_name} if scope_name else {}

                for vs in n.get("verified_selectors", []):
                    vs_strategy = vs.get("strategy", "")
                    vs_selector = vs.get("selector", "")
                    if vs_strategy and vs_selector:
                        dom = n.get("dom") or {}
                        attrs = dom.get("attrs") or {}
                        anchor_href = (
                            str(attrs.get("href") or "").strip()
                            if str(dom.get("tag") or "").lower() == "a"
                            and not attrs.get("download")
                            else ""
                        )
                        candidates.append({
                            "strategy": f"verified_{vs_strategy}",
                            "selector": vs_selector,
                            "semantic_value": name,
                            "pre_score": 1.0,
                            "pre_features": {
                                "verified": True,
                                "source": vs.get("source") or "a11y_backend_dom_node",
                                "verified_href": anchor_href or None,
                                **scope_ctx,
                            },
                        })

                if scope_name:
                    # Scoped candidates: higher scores to prioritize them
                    candidates.extend([
                        {"strategy": "a11y_scoped_role_exact", "selector": role,
                         "semantic_value": name, "pre_score": 0.95,
                         "pre_features": {"source": "a11y_scoped_role_exact", **scope_ctx}},
                        {"strategy": "a11y_scoped_role_fuzzy", "selector": role,
                         "semantic_value": name, "pre_score": 0.85,
                         "pre_features": {"source": "a11y_scoped_role_fuzzy", **scope_ctx}},
                        {"strategy": "a11y_scoped_text_exact", "selector": name,
                         "semantic_value": name, "pre_score": 0.70,
                         "pre_features": {"source": "a11y_scoped_text_exact", **scope_ctx}},
                        {"strategy": "a11y_scoped_text_fuzzy", "selector": name,
                         "semantic_value": name, "pre_score": 0.60,
                         "pre_features": {"source": "a11y_scoped_text_fuzzy", **scope_ctx}},
                    ])
                else:
                    candidates.extend([
                        {"strategy": "role", "selector": role, "semantic_value": name,
                         "pre_score": 0.90, "pre_features": {"verified": True, "source": "a11y_role_exact"}},
                        {"strategy": "role_fuzzy", "selector": role, "semantic_value": name,
                         "pre_score": 0.75, "pre_features": {"source": "a11y_role_fuzzy"}},
                        {"strategy": "text", "selector": name, "semantic_value": name,
                         "pre_score": 0.55, "pre_features": {"source": "a11y_text_exact"}},
                    ])
            if _target_is_generic_repeated_action(target) and match_count > 1:
                step["locator_confidence"] = "low"
            else:
                step["locator_confidence"] = "high" if match_count == 1 else "medium"
        else:
            step["locator_confidence"] = "low"

        step["candidates"] = candidates
        step["match_count"] = match_count
        confidences.append(step["locator_confidence"])

    overall = "high"
    if "low" in confidences:
        overall = "low"
    elif "medium" in confidences:
        overall = "medium"

    dsl_case["_preflight"] = {
        "locator_confidence": overall,
        "warnings": [
            f"Step {i}: match_count={s.get('match_count',0)}"
            for i, s in enumerate(steps)
            if isinstance(s, dict)
            and str(s.get("target") or "").strip()
            and s.get("match_count", 0) == 0
        ] + [
            f"Step {i}: target '{s.get('target')}' is a repeated product action; add product context"
            for i, s in enumerate(steps)
            if isinstance(s, dict)
            and _target_is_generic_repeated_action(str(s.get("target") or ""))
            and s.get("match_count", 0) > 1
        ],
    }
    return dsl_case


def apply_preflight_to_dsl_by_state(
    dsl_case: dict[str, Any],
    a11y_nodes_by_state: dict[str, list[dict[str, Any]]],
) -> dict[str, Any]:
    """Bind each locator-bearing step to one evidence state.

    A target that matches multiple states must declare ``page_state``. Explicit
    CSS is accepted only when the exact selector was verified by exploration.
    """
    steps = dsl_case.get("steps", [])
    warnings: list[str] = []
    confidences: list[str] = []
    normalized_states = {
        str(state): [node for node in nodes if isinstance(node, dict)]
        for state, nodes in a11y_nodes_by_state.items()
        if isinstance(nodes, list)
    }

    for index, step in enumerate(steps):
        if not isinstance(step, dict) or not str(step.get("target") or "").strip():
            continue
        requested_state = str(step.get("page_state") or "").strip()
        if requested_state:
            state_names = [requested_state]
        else:
            state_names = list(normalized_states)

        matches: list[tuple[str, dict[str, Any]]] = []
        for state in state_names:
            nodes = normalized_states.get(state)
            if nodes is None:
                continue
            probe = {"steps": [deepcopy(step)]}
            apply_preflight_to_dsl(probe, nodes)
            evaluated = probe["steps"][0]
            if evaluated.get("match_count", 0) > 0:
                matches.append((state, evaluated))

        target = str(step.get("target") or "").strip()
        if not matches:
            step["candidates"] = []
            step["match_count"] = 0
            step["locator_confidence"] = "low"
            confidences.append("low")
            if requested_state and requested_state not in normalized_states:
                warnings.append(
                    f"Step {index}: page_state '{requested_state}' has no exploration evidence"
                )
            elif _is_composite_css(target, step.get("target_strategy")):
                warnings.append(
                    f"Step {index}: composite CSS '{target}' was not verified in any "
                    "matching page state; re-explore that state and use an exact "
                    "verified_selectors entry"
                )
            else:
                state_hint = requested_state or ", ".join(state_names) or "none"
                warnings.append(
                    f"Step {index}: target '{target}' matched 0 elements in states [{state_hint}]"
                )
            continue

        if len(matches) > 1 and not requested_state:
            step["candidates"] = []
            step["match_count"] = sum(
                int(evaluated.get("match_count", 0)) for _, evaluated in matches
            )
            step["locator_confidence"] = "low"
            confidences.append("low")
            warnings.append(
                f"Step {index}: target '{target}' matches multiple page states "
                f"{[state for state, _ in matches]}; set page_state explicitly"
            )
            continue

        state, evaluated = matches[0]
        for field in ("candidates", "match_count", "locator_confidence"):
            step[field] = evaluated[field]
        step["page_state"] = state
        href = _verified_anchor_href(step)
        if (
            step.get("action") == "click"
            and href
            and not _has_navigation_postcondition(step, href)
        ):
            step["locator_confidence"] = "low"
            warnings.append(
                f"Step {index}: cross-page anchor '{href}' requires a matching "
                "url_contains postcondition"
            )
        confidences.append(str(step["locator_confidence"]))

    overall = "high"
    if "low" in confidences:
        overall = "low"
    elif "medium" in confidences:
        overall = "medium"
    dsl_case["_preflight"] = {
        "locator_confidence": overall,
        "warnings": warnings,
    }
    return dsl_case


def _is_composite_css(target: str, strategy: Any) -> bool:
    if strategy != "css" and not target.startswith(("css=", "#", ".")):
        return False
    selector = target.removeprefix("css=").strip()
    return bool(re.search(r"\s|>|\+|~", selector))
