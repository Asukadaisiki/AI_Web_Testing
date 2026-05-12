"""Multi-Agent Orchestrator for test planning.

Splits the monolithic ReAct loop into specialized agents:
- ExplorerAgent: pure page executor (fast model, no decisions)
- PlannerAgent: thinking brain (pro model, effort=max, reviews elements then decides)

The Orchestrator coordinates them in a feedback loop:
  Planner sees requirements → Explorer collects page → Planner sees elements →
  Planner decides actions → Explorer executes → repeat until complete → Planner generates DSL
"""

from __future__ import annotations

import json
import logging
import threading
import time
from pathlib import Path
from typing import Any, Generator

from playwright.sync_api import sync_playwright, Browser, BrowserContext, Page
from sqlalchemy.orm import Session

from app.core.config import get_settings
from app.ai.planning_tools import execute_tool
from app.ai.page_explorer import collect_interactable_elements, collect_flow_elements
from app.ai.test_planning_agent import (
    AIPlanningToolCall,
    _extract_page_elements,
    _safe_parse_json,
    _normalize_json_text,
    run_compression_subagent,
)

logger = logging.getLogger(__name__)


class _ExplorerBrowser:
    """Manages a single Playwright browser instance for the ExplorerAgent.

    Unlike BrowserSessionManager, this is designed to work in worker threads
    without asyncio conflicts. The browser is created lazily on first use
    and reused across all explore calls within one orchestrator run.
    """

    def __init__(self, base_url: str = ""):
        self._base_url = base_url
        self._pw = None
        self._browser: Browser | None = None
        self._context: BrowserContext | None = None
        self._page: Page | None = None

    def get_page(self) -> Page:
        """Get or create the shared browser page."""
        if self._page is not None:
            try:
                self._page.evaluate("1")  # health check
                return self._page
            except Exception:
                self.close()

        self._pw = sync_playwright()
        playwright = self._pw.__enter__()
        self._browser = playwright.chromium.launch(headless=True)
        self._context = self._browser.new_context()
        self._page = self._context.new_page()
        return self._page

    def close(self):
        """Close browser and cleanup."""
        for attr in ("_page", "_context", "_browser"):
            obj = getattr(self, attr, None)
            if obj is not None:
                try:
                    obj.close()
                except Exception:
                    pass
            setattr(self, attr, None)
        if self._pw is not None:
            try:
                self._pw.__exit__(None, None, None)
            except Exception:
                pass
            self._pw = None

# ─── Explorer Agent ───────────────────────────────────────────────────────────

class ExplorerAgent:
    """Pure page executor — no decision making.

    Takes explicit commands (URL + actions) and returns page elements.
    Uses the fast model only for tool execution, not for planning.
    """

    def __init__(
        self,
        *,
        db_session: Session,
        project_id: int,
        actor_user_id: int,
        base_url: str = "",
    ):
        self.db_session = db_session
        self.project_id = project_id
        self.actor_user_id = actor_user_id
        self.base_url = base_url.rstrip("/")
        self._browser = _ExplorerBrowser(base_url)
        self.tool_calls: list[AIPlanningToolCall] = []

    def _resolve_url(self, url: str) -> str:
        """Resolve relative URL to absolute using base_url."""
        if not url:
            return self.base_url
        if url.startswith("http://") or url.startswith("https://"):
            return url
        return f"{self.base_url}{url}" if url.startswith("/") else f"{self.base_url}/{url}"

    def explore_page(self, url: str, description: str = "") -> dict[str, Any]:
        """Visit a URL and collect all interactive elements using shared browser."""
        resolved_url = self._resolve_url(url)
        try:
            page = self._browser.get_page()
            elements = collect_interactable_elements(
                resolved_url,
                timeout_ms=60000,
                page=page,
            )
            result = {
                "url": resolved_url,
                "element_count": len(elements),
                "elements": elements,
                "description": description,
            }
        except Exception as exc:
            logger.warning("ExplorerAgent.explore_page failed for %s: %s", resolved_url, exc)
            result = {"url": resolved_url, "element_count": 0, "elements": [], "error": str(exc)}

        self.tool_calls.append(AIPlanningToolCall(
            tool="explore_page", params={"url": resolved_url}, result=result,
        ))
        return result

    def explore_flow(self, steps: list[dict[str, Any]]) -> dict[str, Any]:
        """Execute a flow of steps using shared browser and collect elements."""
        try:
            page = self._browser.get_page()
            # Resolve URLs in steps
            resolved_steps = []
            for step in steps:
                s = dict(step)
                if "url" in s:
                    s["url"] = self._resolve_url(s["url"])
                resolved_steps.append(s)

            flow_result = collect_flow_elements(
                resolved_steps,
                timeout_ms=60000,
                page=page,
            )
            result = {
                "pages": flow_result,
                "total_elements": sum(p.get("element_count", 0) for p in flow_result),
            }
        except Exception as exc:
            logger.warning("ExplorerAgent.explore_flow failed: %s", exc)
            result = {"pages": [], "total_elements": 0, "error": str(exc)}

        # Run compression for large results
        if result and isinstance(result, dict):
            settings = get_settings()
            compressed = run_compression_subagent("explore_flow", result, settings)
            if compressed:
                result["_compressed"] = compressed

        self.tool_calls.append(AIPlanningToolCall(
            tool="explore_flow", params={"steps": steps}, result=result,
        ))
        return result

    def close(self):
        """Close the shared browser."""
        self._browser.close()

    def get_all_page_elements(self) -> str | None:
        """Extract formatted page elements from all explore tool calls."""
        return _extract_page_elements(self.tool_calls)


# ─── Planner Agent ────────────────────────────────────────────────────────────

_PLANNER_SYSTEM_PROMPT = """你是一个 Web 自动化测试规划专家。你的任务是：

1. 分析用户的测试需求
2. 根据页面实际元素规划测试步骤
3. 生成高质量的 DSL 草案

## 工作流程

你会收到两类信息：
- **用户需求**：测试目标、流程、断言
- **页面元素**：由 Explorer Agent 采集的真实页面元素（含 tag、text、candidates 等）

## 关键决策规则

### 选择测试商品
当页面有多个商品时，选择**不同的、有明确区分**的商品。例如：
- Blue Top (Rs. 500) 和 Fancy Green Top (Rs. 700) — 不同名称，不同价格
- 不要选两个同名商品

### actions 消歧
页面上有多个同类按钮时，必须用消歧格式：
- ✅ "Blue Top 附近的 Add to cart"
- ✅ "Fancy Green Top 附近的 Add to cart"
- ❌ "Add to cart" （会匹配到第一个）

### 数量修改
购物车页面如果有商品，必须包含：
1. input 步骤修改数量
2. wait_for 等待总价更新
3. assert_text 验证新总价

### R1-R8 规则
严格遵循 DSL 生成规则（见下方详细规则）。

## 输出格式

你需要输出一个 JSON 对象，包含以下字段：

```json
{
  "thought": "分析当前状态和下一步决策",
  "phase": "exploring | planning | generating",
  "actions_to_execute": [
    {"action": "click", "target": "Blue Top 附近的 Add to cart"},
    {"action": "click", "target": "Continue Shopping"}
  ],
  "pages_to_explore": ["/products", "/view_cart"],
  "dsl_draft": { ... },
  "is_complete": false
}
```

- `phase`: "exploring" 表示还需要采集页面，"generating" 表示可以生成 DSL
- `actions_to_execute`: 告诉 Explorer 要执行的具体 actions（精确消歧）
- `pages_to_explore`: 告诉 Explorer 要访问的 URL
- `dsl_draft`: 当 phase="generating" 时，输出完整的 DSL 草案
- `is_complete`: true 表示任务完成

## DSL 规则摘要

R1: 导航后才能操作目标页面的元素
R2: wait_for 前必须有 click/goto
R3: 修改值后必须断言（input → wait_for → assert_text）
R4: 用 trigger 字段处理键盘事件
R5: capture_text 后必须 assert_text 引用变量
R6: 覆盖所有表单字段
R7: 状态变更后添加验证步骤
R8: 页面状态隔离（S0 元素不能在 S1 步骤中使用）
"""


class PlannerAgent:
    """Thinking brain — analyzes requirements, reviews page elements, decides actions.

    Uses the pro model with thinking mode (effort=max) to:
    1. Review all collected page elements
    2. Decide which specific actions to take next
    3. Generate high-quality DSL drafts with self-review
    """

    def __init__(
        self,
        *,
        api_key: str,
        model: str,
        base_url: str,
        timeout_seconds: float = 600,
    ):
        self.api_key = api_key
        self.model = model
        self.base_url = base_url
        self.timeout_seconds = timeout_seconds
        self.conversation: list[dict[str, str]] = [
            {"role": "system", "content": _PLANNER_SYSTEM_PROMPT},
        ]

    def think(
        self,
        *,
        user_message: str | None = None,
        page_elements: str | None = None,
        explorer_result: dict[str, Any] | None = None,
    ) -> dict[str, Any]:
        """Send context to the planner and get its decision.

        Returns the parsed JSON response with thought, actions, dsl_draft, etc.
        """
        # Build the user message
        parts: list[str] = []
        if user_message:
            parts.append(f"## 用户需求\n{user_message}")
        if page_elements:
            parts.append(f"## 当前页面元素\n{page_elements}")
        if explorer_result:
            # Include a summary, not the full result
            summary = {
                k: v for k, v in explorer_result.items()
                if k in ("url", "urls", "element_count", "total_elements", "pages")
            }
            parts.append(f"## Explorer 返回摘要\n{json.dumps(summary, ensure_ascii=False)}")

        if not parts:
            return {"is_complete": True, "phase": "complete"}

        self.conversation.append({"role": "user", "content": "\n\n".join(parts)})

        # Call LLM with thinking mode
        response_text = self._call_llm_with_thinking()
        self.conversation.append({"role": "assistant", "content": response_text})

        # Parse response
        parsed = _safe_parse_json(response_text)
        if not isinstance(parsed, dict):
            # Try to extract JSON from the response
            import re
            json_match = re.search(r'\{[\s\S]*\}', response_text)
            if json_match:
                parsed = _safe_parse_json(json_match.group(0))

        if not isinstance(parsed, dict):
            return {
                "thought": "无法解析响应",
                "phase": "exploring",
                "actions_to_execute": [],
                "pages_to_explore": [],
                "is_complete": False,
                "error": "JSON parse failed",
            }

        return parsed

    def _call_llm_with_thinking(self) -> str:
        """Call the LLM API with thinking mode enabled."""
        import httpx

        payload: dict[str, Any] = {
            "model": self.model,
            "messages": self.conversation,
            "stream": False,
        }

        # Enable thinking mode for DeepSeek
        if "deepseek" in self.model.lower():
            payload["thinking"] = {"type": "enabled", "effort": "max"}
            payload["max_tokens"] = 65536
        else:
            payload["temperature"] = 0.1

        endpoint = f"{self.base_url.rstrip('/')}/chat/completions"

        with httpx.Client(timeout=self.timeout_seconds) as client:
            response = client.post(
                endpoint,
                json=payload,
                headers={
                    "Authorization": f"Bearer {self.api_key}",
                    "Content-Type": "application/json",
                },
            )
            response.raise_for_status()
            raw = response.json()

        # Extract content from response
        choices = raw.get("choices", [])
        if choices:
            message = choices[0].get("message", {})
            return message.get("content", "")
        return ""


# ─── Orchestrator ─────────────────────────────────────────────────────────────

class MultiAgentOrchestrator:
    """Coordinates Explorer and Planner agents in a feedback loop.

    Flow:
    1. Planner analyzes user requirements
    2. Explorer collects page elements
    3. Planner sees elements → decides specific actions
    4. Explorer executes actions
    5. Repeat until Planner has enough info
    6. Planner generates DSL draft
    """

    MAX_ITERATIONS = 10

    def __init__(
        self,
        *,
        db_session: Session,
        project_id: int,
        actor_user_id: int,
        planning_session_id: int,
    ):
        settings = get_settings()

        # Extract base_url from user message if available
        import re
        url_match = re.search(r'https?://\S+', "")
        base_url = ""
        # base_url will be set in run() from user_message

        self.explorer = ExplorerAgent(
            db_session=db_session,
            project_id=project_id,
            actor_user_id=actor_user_id,
        )
        self._base_url = ""
        self.planner = PlannerAgent(
            api_key=settings.ai_planning_api_key,
            model=settings.ai_planning_model,
            base_url=settings.ai_planning_base_url,
            timeout_seconds=max(1.0, settings.ai_planning_timeout_ms / 1000),
        )
        self.db_session = db_session
        self.project_id = project_id

    def run(self, user_message: str) -> Generator[dict[str, Any], None, None]:
        """Execute the multi-agent flow, yielding status events.

        Yields events like:
        - {"type": "status", "phase": "...", "message": "..."}
        - {"type": "tool_call_start", "tool": "...", "params": {...}}
        - {"type": "tool_call_end", "tool": "...", "result_summary": {...}}
        - {"type": "plan_ready", "plan": {...}}
        - {"type": "draft_ready", "draft": {...}}
        """
        settings = get_settings()
        base_url = ""
        # Extract base URL from user message or use default
        import re
        url_match = re.search(r'https?://\S+', user_message)
        if url_match:
            base_url = url_match.group(0).rstrip("/")

        self.explorer.base_url = base_url

        yield {"type": "status", "phase": "starting", "message": "启动多 Agent 规划..."}

        try:
            # Phase 0: Initial planner analysis
            yield {"type": "status", "phase": "planning", "message": "Planner 分析需求中..."}
            planner_result = self.planner.think(user_message=user_message)

            page_elements_collected: str = ""
            iteration = 0
            result: dict[str, Any] = {}

            while iteration < self.MAX_ITERATIONS:
                iteration += 1

                phase = planner_result.get("phase", "exploring")
                is_complete = planner_result.get("is_complete", False)

                if is_complete or phase == "generating":
                    # Planner has enough info, generate DSL
                    yield {"type": "status", "phase": "generating", "message": "Planner 生成 DSL 草案..."}
                    draft = planner_result.get("dsl_draft")
                    if draft:
                        yield {"type": "draft_ready", "draft": draft}
                    else:
                        # Ask planner to generate
                        gen_result = self.planner.think(
                            user_message="请基于已收集的所有页面元素生成完整的 DSL 草案。",
                            page_elements=page_elements_collected,
                        )
                        draft = gen_result.get("dsl_draft")
                        if draft:
                            yield {"type": "draft_ready", "draft": draft}
                    break

                if phase == "exploring":
                    # Explorer collects pages
                    pages_to_explore = planner_result.get("pages_to_explore", [])
                    actions_to_execute = planner_result.get("actions_to_execute", [])

                    if pages_to_explore:
                        for url in pages_to_explore:
                            yield {"type": "status", "phase": "exploring",
                                   "message": f"Explorer 采集页面: {url}"}
                            yield {"type": "tool_call_start", "tool": "explore_page",
                                   "params": {"url": url}}
                            result = self.explorer.explore_page(url)
                            yield {"type": "tool_call_end", "tool": "explore_page",
                                   "result_summary": {"url": url, "elements": result.get("element_count", 0)}}

                    if actions_to_execute:
                        # Build explore_flow steps from planner's actions
                        steps = [{"actions": actions_to_execute}]
                        yield {"type": "status", "phase": "exploring",
                               "message": f"Explorer 执行 {len(actions_to_execute)} 个动作..."}
                        yield {"type": "tool_call_start", "tool": "explore_flow",
                               "params": {"steps": steps}}
                        result = self.explorer.explore_flow(steps)
                        yield {"type": "tool_call_end", "tool": "explore_flow",
                               "result_summary": {"pages": len(result.get("pages", [])),
                                                  "elements": result.get("total_elements", 0)}}

                    # Collect all page elements
                    page_elements_collected = self.explorer.get_all_page_elements() or ""

                    # Ask planner what to do next
                    yield {"type": "status", "phase": "planning",
                           "message": f"Planner 分析 {len(page_elements_collected)} 字符的页面元素..."}
                    planner_result = self.planner.think(
                        page_elements=page_elements_collected,
                        explorer_result=result if actions_to_execute else None,
                    )
                    continue

                # Unknown phase, break
                logger.warning("Unknown planner phase: %s", phase)
                break

            yield {"type": "status", "phase": "complete", "message": "多 Agent 规划完成"}
        finally:
            self.explorer.close()
