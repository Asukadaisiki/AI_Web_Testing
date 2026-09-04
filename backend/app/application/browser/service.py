"""Stable browser capability boundary used by the Go AgentCore."""

from __future__ import annotations

from copy import deepcopy
import json
from typing import Any

from sqlalchemy.orm import Session

from app.ai.locator_preflight import apply_preflight_to_dsl
from app.ai.planning_tools import execute_tool
from app.schemas.browser_capabilities import BrowserCapabilityName


def execute_browser_capability(
    session: Session,
    *,
    capability: BrowserCapabilityName,
    project_id: int,
    conversation_id: str,
    arguments: dict[str, Any],
) -> dict[str, Any]:
    if capability == "validate_page_elements":
        return _validate_page_elements(arguments)

    params = dict(arguments)
    if conversation_id.isdigit():
        params.setdefault("planning_session_id", int(conversation_id))
    raw_result = execute_tool(
        tool_name=capability,
        params=params,
        db_session=session,
        project_id=project_id,
        planning_session_id=int(params.get("planning_session_id", 0)),
    )
    result = json.loads(raw_result)
    if not isinstance(result, dict):
        raise ValueError(f"{capability} returned a non-object result")
    return result


def _validate_page_elements(arguments: dict[str, Any]) -> dict[str, Any]:
    dsl_case = arguments.get("dsl_case")
    a11y_nodes = arguments.get("a11y_nodes")
    if not isinstance(a11y_nodes, list):
        raise ValueError("a11y_nodes must be an array")

    required_elements = arguments.get("required_elements")
    if isinstance(required_elements, list):
        return _validate_required_elements(required_elements, a11y_nodes)
    if not isinstance(dsl_case, dict):
        raise ValueError("provide required_elements or dsl_case")

    validated = apply_preflight_to_dsl(deepcopy(dsl_case), a11y_nodes)
    preflight = validated.get("_preflight") or {}
    return {
        "dsl_case": validated,
        "valid": preflight.get("locator_confidence") != "low",
        "locator_confidence": preflight.get("locator_confidence", "low"),
        "warnings": preflight.get("warnings", []),
    }


def _validate_required_elements(
    required_elements: list[Any],
    a11y_nodes: list[Any],
) -> dict[str, Any]:
    normalized_nodes = [node for node in a11y_nodes if isinstance(node, dict)]
    checks: list[dict[str, Any]] = []
    for index, requirement in enumerate(required_elements):
        if not isinstance(requirement, dict):
            raise ValueError(f"required_elements[{index}] must be an object")
        requirement_id = str(requirement.get("id") or f"requirement_{index}")
        description = str(requirement.get("description") or "").strip()
        keywords = [
            str(keyword).strip().casefold()
            for keyword in requirement.get("keywords", [])
            if str(keyword).strip()
        ]
        roles = {
            str(role).strip().casefold()
            for role in requirement.get("roles", [])
            if str(role).strip()
        }
        if not description or not keywords:
            raise ValueError(
                f"required_elements[{index}] requires description and keywords"
            )

        candidates = []
        for node in normalized_nodes:
            name = str(node.get("name") or "").casefold()
            role = str(node.get("role") or "").casefold()
            if roles and role not in roles:
                continue
            if any(keyword in name for keyword in keywords):
                candidates.append(
                    {
                        "node_id": node.get("node_id"),
                        "role": node.get("role"),
                        "name": node.get("name"),
                        "page_state": node.get("page_state"),
                        "verified_selectors": node.get("verified_selectors", []),
                    }
                )

        status = "missing"
        if len(candidates) == 1:
            status = "unique"
        elif len(candidates) > 1:
            status = "ambiguous"
        checks.append(
            {
                "id": requirement_id,
                "description": description,
                "status": status,
                "candidate_count": len(candidates),
                "candidates": candidates,
            }
        )

    missing = [item["id"] for item in checks if item["status"] == "missing"]
    ambiguous = [item["id"] for item in checks if item["status"] == "ambiguous"]
    return {
        "valid": not missing,
        "checks": checks,
        "missing_requirement_ids": missing,
        "ambiguous_requirement_ids": ambiguous,
        "recommended_action": "re_explore" if missing else "generate_dsl",
    }
