"""Planning tool registry — each tool is a callable the ReAct agent can invoke."""

from __future__ import annotations

import json
import logging
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from sqlalchemy.orm import Session

from app.ai.page_explorer import (
    capture_browser_session,
    collect_interactable_elements,
    collect_multi_page_elements,
    format_elements_for_prompt,
    is_storage_state_stale,
    load_storage_state_meta,
)

logger = logging.getLogger(__name__)


@dataclass(frozen=True)
class PlanningTool:
    """Declarative descriptor for a planning tool."""

    name: str
    description: str
    parameters: dict[str, Any]  # JSON-Schema style


def list_available_tools() -> list[PlanningTool]:
    """Return all registered tools."""
    return list(_TOOL_REGISTRY.values())


def get_tool_descriptions_for_prompt() -> str:
    """Build a text block describing all tools for the LLM system prompt."""
    lines: list[str] = []
    for tool in _TOOL_REGISTRY.values():
        lines.append(f"### {tool.name}")
        lines.append(tool.description)
        params = tool.parameters
        if params.get("properties"):
            lines.append("参数:")
            for pname, pinfo in params["properties"].items():
                required = pname in params.get("required", [])
                req_mark = " (必填)" if required else ""
                lines.append(f"  - {pname}{req_mark}: {pinfo.get('description', '')}")
        lines.append("")
    return "\n".join(lines)


def execute_tool(
    *,
    tool_name: str,
    params: dict[str, Any],
    db_session: Session,
    project_id: int,
) -> str:
    """Execute a tool by name and return the result as a JSON string."""
    tool_def = _TOOL_REGISTRY.get(tool_name)
    if tool_def is None:
        return json.dumps({"error": f"工具 '{tool_name}' 不存在"}, ensure_ascii=False)

    handler = _TOOL_HANDLERS.get(tool_name)
    if handler is None:
        return json.dumps({"error": f"工具 '{tool_name}' 未注册处理函数"}, ensure_ascii=False)

    try:
        result = handler(params=params, db_session=db_session, project_id=project_id)
        return json.dumps(result, ensure_ascii=False, default=str)
    except Exception as exc:
        logger.warning("Tool %s execution failed: %s", tool_name, exc)
        return json.dumps({"error": f"工具执行失败: {exc!s}"}, ensure_ascii=False)


# ---------------------------------------------------------------------------
# Individual tool implementations
# ---------------------------------------------------------------------------

def _handle_get_project_info(
    *,
    params: dict[str, Any],
    db_session: Session,
    project_id: int,
) -> dict[str, Any]:
    from app.services.project_management import get_project
    project = get_project(db_session, project_id)
    if project is None:
        return {"error": f"项目 {project_id} 不存在"}
    return {"id": project.id, "name": project.name, "description": project.description}


def _handle_list_test_cases(
    *,
    params: dict[str, Any],
    db_session: Session,
    project_id: int,
) -> dict[str, Any]:
    from sqlalchemy import select
    from app.models import TestCase
    statement = select(TestCase).where(TestCase.project_id == project_id).order_by(TestCase.created_at.desc())
    records = db_session.scalars(statement).all()

    results = [
        {"id": r.id, "name": r.name, "description": r.description}
        for r in records
    ]

    search = params.get("search")
    if search:
        keyword = search.lower()
        results = [
            c for c in results
            if keyword in (c.get("name") or "").lower() or keyword in (c.get("description") or "").lower()
        ]
    limit = min(int(params.get("limit", 10)), 20)
    return {"cases": results[:limit], "total": len(results)}


def _handle_get_case_detail(
    *,
    params: dict[str, Any],
    db_session: Session,
    project_id: int,
) -> dict[str, Any]:
    from app.services.cases import get_case
    case_id = int(params.get("case_id", 0))
    if not case_id:
        return {"error": "必须提供 case_id 参数"}
    case = get_case(db_session, case_id)
    if case is None:
        return {"error": f"用例 {case_id} 不存在"}
    return {
        "id": case.id,
        "name": case.name,
        "description": case.description,
        "base_url": getattr(case, "base_url", None),
        "steps": [
            s.model_dump(mode="json") if hasattr(s, "model_dump") else s
            for s in getattr(case, "steps", [])
        ],
        "input_contract": [
            c.model_dump(mode="json") if hasattr(c, "model_dump") else c
            for c in getattr(case, "input_contract", [])
        ],
        "output_contract": [
            c.model_dump(mode="json") if hasattr(c, "model_dump") else c
            for c in getattr(case, "output_contract", [])
        ],
    }


def _handle_list_recent_executions(
    *,
    params: dict[str, Any],
    db_session: Session,
    project_id: int,
) -> dict[str, Any]:
    from app.services.executions import list_executions
    limit = min(int(params.get("limit", 5)), 10)
    executions = list_executions(db_session, project_id=project_id, limit=limit)
    results = []
    for ex in executions:
        results.append({
            "id": ex.id,
            "case_name": ex.case_name,
            "status": ex.status,
            "started_at": str(ex.started_at),
        })
    return {"executions": results}


def _handle_get_case_stats(
    *,
    params: dict[str, Any],
    db_session: Session,
    project_id: int,
) -> dict[str, Any]:
    from app.services.cases import get_project_test_case_stats
    stats = get_project_test_case_stats(db_session, project_id)
    return stats if isinstance(stats, dict) else {"stats": str(stats)}


def _resolve_storage_state_dir() -> Path:
    """Resolve storage state directory from app config."""
    from app.core.config import get_settings
    return Path(get_settings().storage_state_dir)


def _handle_explore_page(
    *,
    params: dict[str, Any],
    db_session: Session,
    project_id: int,
) -> dict[str, Any]:
    url = params.get("url")
    if not url or not isinstance(url, str) or not url.strip():
        return {"error": "必须提供 url 参数"}

    storage_dir = _resolve_storage_state_dir()
    storage_path = str(storage_dir / f"{project_id}.json") if (storage_dir / f"{project_id}.json").exists() else None

    elements = collect_interactable_elements(url.strip(), storage_state_path=storage_path)
    formatted = format_elements_for_prompt(elements)

    result: dict[str, Any] = {
        "url": url.strip(),
        "elements": elements,
        "formatted": formatted,
        "element_count": len(elements),
    }

    if not elements:
        result["warning"] = "页面未发现可交互元素"

    meta = load_storage_state_meta(storage_dir, project_id=project_id)
    if meta and is_storage_state_stale(meta):
        result["warning"] = "会话状态超过24小时未更新，元素可能不完整"

    return result


def _handle_capture_page_session(
    *,
    params: dict[str, Any],
    db_session: Session,
    project_id: int,
) -> dict[str, Any]:
    url = params.get("url")
    if not url or not isinstance(url, str) or not url.strip():
        return {"error": "必须提供 url 参数"}

    steps = params.get("steps")
    if not isinstance(steps, list):
        steps = []

    storage_dir = _resolve_storage_state_dir()
    return capture_browser_session(
        url=url.strip(),
        steps=steps,
        storage_dir=storage_dir,
        project_id=project_id,
    )


def _handle_explore_flow(
    *,
    params: dict[str, Any],
    db_session: Session,
    project_id: int,
) -> dict[str, Any]:
    urls = params.get("urls")
    if not isinstance(urls, list) or not urls:
        return {"error": "必须提供 urls 参数（非空 URL 列表）"}

    valid_urls = [u for u in urls if isinstance(u, str) and u.strip()]
    if not valid_urls:
        return {"error": "urls 列表中没有有效的 URL"}

    storage_dir = _resolve_storage_state_dir()
    storage_path = str(storage_dir / f"{project_id}.json") if (storage_dir / f"{project_id}.json").exists() else None

    page_results = collect_multi_page_elements(
        valid_urls,
        storage_state_path=storage_path,
        enable_vlm_annotation=True,
    )

    # Build a backward-compatible combined formatted string
    sections: list[str] = []
    for pr in page_results:
        url = pr.get("url", "")
        formatted = pr.get("formatted", "")
        annotation = pr.get("vlm_annotation")
        section = f"=== 页面: {url} ===\n{formatted}"
        if annotation:
            section += f"\n\n页面布局描述: {annotation}"
        sections.append(section)

    combined_formatted = "\n\n".join(sections)
    total_elements = sum(pr.get("element_count", 0) for pr in page_results)

    return {
        "pages": page_results,
        "formatted": combined_formatted,
        "total_pages": len(page_results),
        "total_elements": total_elements,
    }


# ---------------------------------------------------------------------------
# Registry
# ---------------------------------------------------------------------------

_TOOL_REGISTRY: dict[str, PlanningTool] = {
    "get_project_info": PlanningTool(
        name="get_project_info",
        description="获取当前项目的基本信息，包括项目名称和描述。",
        parameters={
            "type": "object",
            "properties": {},
            "required": [],
        },
    ),
    "list_test_cases": PlanningTool(
        name="list_test_cases",
        description="列出当前项目下的测试用例概要（id、名称、描述），可选按关键词搜索。",
        parameters={
            "type": "object",
            "properties": {
                "search": {
                    "type": "string",
                    "description": "按关键词过滤用例名称或描述",
                },
                "limit": {
                    "type": "integer",
                    "description": "返回数量上限，默认10，最大20",
                },
            },
            "required": [],
        },
    ),
    "get_case_detail": PlanningTool(
        name="get_case_detail",
        description="查看指定测试用例的完整详情，包括步骤、输入输出契约。",
        parameters={
            "type": "object",
            "properties": {
                "case_id": {
                    "type": "integer",
                    "description": "要查看的测试用例 ID",
                },
            },
            "required": ["case_id"],
        },
    ),
    "list_recent_executions": PlanningTool(
        name="list_recent_executions",
        description="查看项目最近的测试执行记录，包括状态和执行时间。",
        parameters={
            "type": "object",
            "properties": {
                "limit": {
                    "type": "integer",
                    "description": "返回数量上限，默认5，最大10",
                },
            },
            "required": [],
        },
    ),
    "get_case_stats": PlanningTool(
        name="get_case_stats",
        description="获取项目下测试用例的统计信息。",
        parameters={
            "type": "object",
            "properties": {},
            "required": [],
        },
    ),
    "explore_page": PlanningTool(
        name="explore_page",
        description="访问指定 URL 页面，采集页面上所有可交互元素（按钮、输入框、链接等），返回元素的 id、label、placeholder 等定位属性。如果项目已保存浏览器会话状态，会自动复用登录态。",
        parameters={
            "type": "object",
            "properties": {
                "url": {
                    "type": "string",
                    "description": "要采集的目标页面 URL",
                },
            },
            "required": ["url"],
        },
    ),
    "capture_page_session": PlanningTool(
        name="capture_page_session",
        description="打开指定 URL 并执行登录步骤（如填写用户名密码、点击登录按钮），然后保存浏览器的会话状态（cookie 等），供后续 explore_page 复用登录态。",
        parameters={
            "type": "object",
            "properties": {
                "url": {
                    "type": "string",
                    "description": "登录页面的 URL",
                },
                "steps": {
                    "type": "array",
                    "description": "登录操作的步骤列表，每步包含 action、target 和 value",
                    "items": {
                        "type": "object",
                        "properties": {
                            "action": {"type": "string", "enum": ["input", "click"]},
                            "target": {"type": "string"},
                            "value": {"type": "string"},
                        },
                    },
                },
            },
            "required": ["url"],
        },
    ),
    "explore_flow": PlanningTool(
        name="explore_flow",
        description="沿用户测试流程依次访问多个页面，采集每个页面的可交互元素和视觉布局信息。适用于需要跨页面的测试场景（如登录→商品列表→详情→购物车），会复用浏览器会话保持登录态。",
        parameters={
            "type": "object",
            "properties": {
                "urls": {
                    "type": "array",
                    "description": "需要依次访问和采集的页面 URL 列表，按流程顺序排列",
                    "items": {"type": "string"},
                },
            },
            "required": ["urls"],
        },
    ),
}

_TOOL_HANDLERS: dict[str, Any] = {
    "get_project_info": _handle_get_project_info,
    "list_test_cases": _handle_list_test_cases,
    "get_case_detail": _handle_get_case_detail,
    "list_recent_executions": _handle_list_recent_executions,
    "get_case_stats": _handle_get_case_stats,
    "explore_page": _handle_explore_page,
    "capture_page_session": _handle_capture_page_session,
    "explore_flow": _handle_explore_flow,
}
