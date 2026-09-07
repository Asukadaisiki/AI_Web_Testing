"""Stable browser capability boundary used by the Go AgentCore."""

from __future__ import annotations

from concurrent.futures import ThreadPoolExecutor
from copy import deepcopy
import hashlib
import json
from pathlib import Path
import threading
from typing import Any, Callable, TypeVar
from urllib.parse import urljoin, urlparse

from sqlalchemy.orm import Session

from app.ai.locator_preflight import apply_preflight_to_dsl_by_state
from app.ai.page_explorer import (
    BrowserSessionManager,
    _collect_flow_a11y,
    collect_a11y_nodes,
    is_storage_state_stale,
    load_storage_state_meta,
)
from app.core.config import get_settings
from app.models import AIPlanningSession
from app.schemas.browser_capabilities import (
    BrowserCapabilityName,
    ExploreFlowArguments,
    ExplorePageArguments,
    ValidatePageElementsArguments,
)


T = TypeVar("T")


class _BrowserCapabilityRuntime:
    """Keep all Sync Playwright work on one dedicated thread."""

    _lock = threading.Lock()
    _executor: ThreadPoolExecutor | None = None

    @classmethod
    def run(cls, operation: Callable[[], T]) -> T:
        with cls._lock:
            if cls._executor is None:
                cls._executor = ThreadPoolExecutor(
                    max_workers=1,
                    thread_name_prefix="browser-capability",
                )
            future = cls._executor.submit(operation)
        return future.result()

    @classmethod
    def shutdown(cls) -> None:
        with cls._lock:
            executor = cls._executor
            cls._executor = None
            if executor is None:
                return
            close_future = executor.submit(BrowserSessionManager.close_all)
        close_future.result()
        executor.shutdown(wait=True)


def execute_browser_capability(
    session: Session,
    *,
    capability: BrowserCapabilityName,
    project_id: int,
    conversation_id: str,
    arguments: dict[str, Any],
) -> dict[str, Any]:
    if capability == "validate_page_elements":
        validated_arguments = ValidatePageElementsArguments.model_validate(arguments)
        return _validate_page_elements(
            validated_arguments.model_dump(exclude_none=True)
        )
    planning_session_id = int(conversation_id) if conversation_id.isdigit() else 0
    requirements = _planning_session_requirements(session, planning_session_id)
    clean_context_requested = requirements.get("clean_context") is True
    storage_state_path = (
        None if clean_context_requested else _storage_state_path(project_id)
    )
    context_evidence = _context_evidence(
        planning_session_id=planning_session_id,
        clean_context_requested=clean_context_requested,
        storage_state_path=storage_state_path,
    )
    if capability == "explore_page":
        arguments = ExplorePageArguments.model_validate(arguments).model_dump(
            exclude_none=True
        )
        url = _resolve_page_url(requirements, arguments)
        return _BrowserCapabilityRuntime.run(
            lambda: _explore_page(
                project_id,
                planning_session_id,
                url,
                arguments,
                storage_state_path,
                context_evidence,
            )
        )
    if capability == "explore_flow":
        arguments = ExploreFlowArguments.model_validate(arguments).model_dump(
            exclude_none=True
        )
        base_url = str(arguments.get("base_url") or "").strip()
        if not base_url:
            base_url = _session_base_url(requirements)
        return _BrowserCapabilityRuntime.run(
            lambda: _explore_flow(
                planning_session_id,
                base_url,
                arguments,
                storage_state_path,
                context_evidence,
            )
        )
    raise ValueError(f"unsupported browser capability: {capability}")


def shutdown_browser_capabilities() -> None:
    _BrowserCapabilityRuntime.shutdown()


def _storage_state_path(project_id: int) -> str | None:
    path = Path(get_settings().storage_state_dir) / f"{project_id}.json"
    return str(path) if path.exists() else None


def _planning_session_requirements(
    session: Session,
    planning_session_id: int,
) -> dict[str, Any]:
    if planning_session_id < 1:
        return {}
    record = session.get(AIPlanningSession, planning_session_id)
    if record is None or not isinstance(record.requirements_json, dict):
        return {}
    return record.requirements_json


def _session_base_url(requirements: dict[str, Any]) -> str:
    entry = requirements.get("entry_url_or_page", "")
    if not isinstance(entry, str) or not entry.startswith(("http://", "https://")):
        return ""
    parsed = urlparse(entry)
    return f"{parsed.scheme}://{parsed.netloc}"


def _resolve_page_url(
    requirements: dict[str, Any],
    arguments: dict[str, Any],
) -> str:
    url = str(arguments.get("url") or "").strip()
    if not url:
        raise ValueError("url is required")
    if not url.startswith(("http://", "https://")):
        base_url = _session_base_url(requirements)
        if not base_url:
            raise ValueError("relative url requires a session base URL")
        url = urljoin(base_url + "/", url.lstrip("/"))
    return url


def _context_evidence(
    *,
    planning_session_id: int,
    clean_context_requested: bool,
    storage_state_path: str | None,
) -> dict[str, Any]:
    return {
        "version": "v1",
        "clean_context_requested": clean_context_requested,
        "storage_state_loaded": storage_state_path is not None,
        "planning_session_id": planning_session_id,
    }


def _explore_page(
    project_id: int,
    planning_session_id: int,
    url: str,
    arguments: dict[str, Any],
    storage_state_path: str | None,
    context_evidence: dict[str, Any],
) -> dict[str, Any]:
    _, page = BrowserSessionManager.get_or_create_context(
        planning_session_id,
        storage_state_path=storage_state_path,
    )
    page.goto(url, timeout=30000, wait_until="domcontentloaded")
    try:
        page.wait_for_load_state("networkidle", timeout=30000)
    except Exception:
        pass
    if page.url == "about:blank":
        raise ValueError(f"page did not load: {url}")

    nodes = collect_a11y_nodes(
        page,
        page_state="S0",
        core_user_flow_text=arguments.get("core_user_flow_text"),
    )
    result: dict[str, Any] = {
        "url": page.url,
        "a11y_nodes": nodes,
        "element_count": len(nodes),
        "context_evidence": context_evidence,
    }
    meta = (
        load_storage_state_meta(
            Path(get_settings().storage_state_dir),
            project_id=project_id,
        )
        if storage_state_path
        else None
    )
    if meta and is_storage_state_stale(meta):
        result["warning"] = "会话状态超过24小时未更新"
    elif not nodes:
        result["warning"] = "页面未发现可用 A11y 交互元素"
    return result


def _explore_flow(
    planning_session_id: int,
    base_url: str,
    arguments: dict[str, Any],
    storage_state_path: str | None,
    context_evidence: dict[str, Any],
) -> dict[str, Any]:
    steps = arguments.get("steps")
    if not isinstance(steps, list) or not steps:
        raise ValueError("steps must be a non-empty array")
    pages = _collect_flow_a11y(
        steps,
        base_url=base_url or None,
        storage_state_path=storage_state_path,
        session_id=planning_session_id,
        core_user_flow_text=arguments.get("flow_description"),
    )
    return {
        "pages": pages,
        "success": not any(page.get("status") == "error" for page in pages),
        "failures": [
            page["failure"]
            for page in pages
            if page.get("status") == "error" and isinstance(page.get("failure"), dict)
        ],
        "total_pages": len(pages),
        "total_elements": sum(page.get("element_count", 0) for page in pages),
        "context_evidence": context_evidence,
    }


def _validate_page_elements(arguments: dict[str, Any]) -> dict[str, Any]:
    dsl_case = arguments.get("dsl_case")
    required_elements = arguments.get("required_elements")
    if isinstance(required_elements, list):
        a11y_nodes = arguments["a11y_nodes"]
        return _validate_required_elements(required_elements, a11y_nodes)

    a11y_nodes_by_state = arguments["a11y_nodes_by_state"]
    validated = apply_preflight_to_dsl_by_state(
        deepcopy(dsl_case),
        a11y_nodes_by_state,
    )
    preflight = validated.get("_preflight") or {}
    return {
        "dsl_case": validated,
        "valid": preflight.get("locator_confidence") != "low",
        "validation_mode": "dsl_case",
        "case_digest": _json_digest(dsl_case),
        "evidence_digest": _json_digest(a11y_nodes_by_state),
        "locator_confidence": preflight.get("locator_confidence", "low"),
        "warnings": preflight.get("warnings", []),
    }


def _json_digest(value: Any) -> str:
    payload = json.dumps(
        value,
        ensure_ascii=False,
        separators=(",", ":"),
        sort_keys=True,
    ).encode("utf-8")
    return hashlib.sha256(payload).hexdigest()


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
