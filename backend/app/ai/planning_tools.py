"""Planning tool registry — each tool is a callable the ReAct agent can invoke."""

from __future__ import annotations

import json
import logging
from dataclasses import dataclass
from typing import Any

from sqlalchemy.orm import Session

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
}

_TOOL_HANDLERS: dict[str, Any] = {
    "get_project_info": _handle_get_project_info,
    "list_test_cases": _handle_list_test_cases,
    "get_case_detail": _handle_get_case_detail,
    "list_recent_executions": _handle_list_recent_executions,
    "get_case_stats": _handle_get_case_stats,
}
