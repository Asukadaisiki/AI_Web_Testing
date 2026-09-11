"""Stable browser capability boundary used by the Go AgentCore."""

from __future__ import annotations

import hashlib
import json
import threading
from collections.abc import Callable
from concurrent.futures import ThreadPoolExecutor
from copy import deepcopy
from pathlib import Path
from typing import Any, TypeVar
from urllib.parse import urljoin, urlparse

from browser_worker.contracts.browser_capabilities import (
    BrowserCapabilityName,
    ExploreFlowArguments,
    ExplorePageArguments,
    ValidatePageElementsArguments,
)
from browser_worker.exploration.locator_preflight import apply_preflight_to_dsl_by_state
from browser_worker.exploration.observation import (
    attach_observation_artifact,
    build_browser_observation,
)
from browser_worker.exploration.page_explorer import (
    BrowserSessionManager,
    _collect_flow_a11y,
    collect_a11y_nodes,
    is_storage_state_stale,
    load_storage_state_meta,
)
from browser_worker.runtime.config import get_settings
from browser_worker.runtime.paths import PROJECT_ROOT

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
    _session: object | None,
    *,
    capability: BrowserCapabilityName,
    project_id: int,
    conversation_id: str,
    context: dict[str, Any] | None = None,
    arguments: dict[str, Any],
) -> dict[str, Any]:
    del _session
    if capability == "validate_page_elements":
        validated_arguments = ValidatePageElementsArguments.model_validate(arguments)
        return _validate_page_elements(
            validated_arguments.model_dump(exclude_none=True)
        )
    planning_session_id = int(conversation_id) if conversation_id.isdigit() else 0
    browser_context = context or {}
    clean_context_requested = browser_context.get("clean_context") is True
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
        arguments["probe_id"] = _probe_id(
            capability,
            project_id,
            conversation_id,
            arguments,
        )
        url = _resolve_page_url(browser_context, arguments)
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
        arguments["probe_id"] = _probe_id(
            capability,
            project_id,
            conversation_id,
            arguments,
        )
        base_url = str(arguments.get("base_url") or "").strip()
        if not base_url:
            base_url = _session_base_url(browser_context)
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


def _probe_id(
    capability: str,
    project_id: int,
    conversation_id: str,
    arguments: dict[str, Any],
) -> str:
    existing = str(arguments.get("probe_id") or "").strip()
    if existing:
        return existing
    payload = json.dumps(
        {
            "capability": capability,
            "project_id": project_id,
            "conversation_id": conversation_id,
            "arguments": arguments,
        },
        ensure_ascii=False,
        separators=(",", ":"),
        sort_keys=True,
    ).encode("utf-8")
    return "probe_" + hashlib.sha256(payload).hexdigest()[:24]


def _storage_state_path(project_id: int) -> str | None:
    path = Path(get_settings().storage_state_dir) / f"{project_id}.json"
    return str(path) if path.exists() else None


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


def _page_title(page) -> str:
    title = getattr(page, "title", None)
    if not callable(title):
        return ""
    try:
        return str(title() or "")
    except Exception:
        return ""


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
        "probe_id": arguments.get("probe_id"),
        "url": page.url,
        "a11y_nodes": nodes,
        "element_count": len(nodes),
        "context_evidence": context_evidence,
        "observation_v2": build_browser_observation(
            url=page.url,
            title=_page_title(page),
            state_id="S0",
            revision=1,
            nodes=nodes,
            probe_id=arguments.get("probe_id"),
            page=page,
            artifact_root=PROJECT_ROOT / "artifacts",
        ),
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
    if arguments.get("observation_schema_version") == "v2":
        result["a11y_nodes"] = []
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
    probe_context_evidence = {
        **context_evidence,
        "execution_scope": "isolated_probe",
        "state_persisted": False,
    }
    pages = _collect_flow_a11y(
        steps,
        base_url=base_url or None,
        storage_state_path=storage_state_path,
        session_id=planning_session_id,
        isolated_context=True,
        core_user_flow_text=arguments.get("flow_description"),
        probe_id=arguments.get("probe_id"),
    )
    previous_state_sha256 = None
    for page in pages:
        if page.get("status") == "error":
            continue
        observation = page.get("observation_v2")
        if not isinstance(observation, dict):
            observation = build_browser_observation(
                url=str(page.get("url") or ""),
                title=str(page.get("title") or ""),
                state_id=str(page.get("page_state") or "unknown"),
                revision=max(1, int(page.get("revision") or 1)),
                nodes=[
                    node
                    for node in page.get("a11y_nodes", [])
                    if isinstance(node, dict)
                ],
                probe_id=arguments.get("probe_id"),
                previous_state_sha256=previous_state_sha256,
            )
        if not observation.get("artifact"):
            observation = attach_observation_artifact(
                observation,
                PROJECT_ROOT / "artifacts",
            )
        previous_state_sha256 = observation["page_state"]["state_sha256"]
        page["observation_v2"] = observation
        if arguments.get("observation_schema_version") == "v2":
            page["a11y_nodes"] = []
    return {
        "probe_id": arguments.get("probe_id"),
        "pages": pages,
        "success": not any(page.get("status") == "error" for page in pages),
        "failures": [
            page["failure"]
            for page in pages
            if page.get("status") == "error" and isinstance(page.get("failure"), dict)
        ],
        "total_pages": len(pages),
        "total_elements": sum(page.get("element_count", 0) for page in pages),
        "context_evidence": probe_context_evidence,
    }


def _validate_page_elements(arguments: dict[str, Any]) -> dict[str, Any]:
    dsl_case = arguments.get("dsl_case")
    if isinstance(dsl_case, dict) and dsl_case.get("profile") == "research-v2":
        from browser_worker.contracts.action_ir_v2 import (
            validate_research_v2_dsl,
        )

        validated = validate_research_v2_dsl(
            dsl_case,
            phase="executable",
        ).model_dump(mode="json")
        return {
            "dsl_case": validated,
            "valid": True,
            "validation_mode": "target_binding",
            "case_digest": _json_digest(dsl_case),
            "warnings": [],
        }
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
