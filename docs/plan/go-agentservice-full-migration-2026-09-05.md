# Go AgentService 全面迁移计划

日期：2026-09-05
状态：已完成

进度：

- M1 Go 包边界：完成。
- M2 报告去 Python Agent 化：完成。
- M3 Browser capability 收缩：完成。
- M4 Python legacy Planning 删除：完成。
- M5 控制面归 Go：完成。公开与内部非浏览器控制面均已迁移，Python
  目录已重命名为 `browser-worker/`。

## 目标

以 Go AgentService 作为唯一 Agent 和业务控制面，将 Python 收缩为只负责浏览器运行时的
Browser Worker，并删除迁移期遗留的 Python Planning Agent、公开 Planning API、SSE、
草案编排和无消费者数据模型。

## 目标依赖

```text
Hertz HTTP/SSE
    -> agentservice
        -> harness
            -> agent
            -> tools
        -> repositories
    -> planning
    -> case/execution/report services

tools -> capability ports -> platform adapters
platform/browserworker -> Python internal worker API

Python Browser Worker
    -> Playwright
    -> A11y exploration
    -> locator
    -> evidence capture
```

## pi-agent 对齐原则

- `agent`：只包含消息、事件、状态和模型/工具循环。
- `harness`：组装 Prompt、上下文转换、工具、调用前后策略和事件 sink。
- `agentservice`：负责 Run 生命周期、所有权、持久化、checkpoint、恢复和订阅。
- `tools`：每项能力拥有独立合同，不反向依赖 AgentService。
- `platform`：数据库、模型 Provider、Python Worker 等外部适配器。
- 浏览器、数据库、HTTP 和业务 ORM 不进入纯 Agent loop。

## 迁移阶段

### M1：Go 包边界

1. 将纯模型循环从 `internal/agentcore` 提取到 `internal/agent`。
2. 将 Run/Checkpoint/Event Repository 与 Broker 移入 `internal/agentservice`。
3. 新增 `internal/harness`，负责 Prompt、工具集合、上下文和 policy hook。
4. HTTP Handler 仅依赖 AgentService facade。
5. 保持现有 `/api/v2/agent` 合同和 PostgreSQL 表兼容。

验收：

- `agent` 不导入 Hertz、SQL、Python Worker。
- `agentservice` 不实现具体工具。
- 工具调用前必须经过 policy hook。
- Go 单元测试、vet、build 通过。

### M2：报告去 Python Agent 化

1. Python Report Core 仅生成确定性 `ExecutionAnalysis` 和 `FailureSignal`。
2. 删除 `reporting -> application.planning -> test_planning_agent` 依赖。
3. Go Agent 通过 `get_report` 读取事实并负责解释及修复决策。
4. 删除 Python 自动分析中的 Planning schema 依赖。

验收：

- 报告结果不依赖 LLM 可用性。
- Python 中不存在从 reporting 到 planning/agent 的导入。
- 失败分类、anti-pattern 持久化和修复策略保持可用。

### M3：Python Browser Capability 收缩

1. 将 `explore_page`、`explore_flow` 从旧 `planning_tools.py` 提取到 browser application。
2. Python internal API 只暴露探索、元素验证和执行能力。
3. 移除 Python 工具注册表中的项目查询、复测建议、洞察更新等 Agent 工具。
4. 删除失去调用方的缓存和 Planning helper。

验收：

- Browser capability 不调用 Python Planning Agent。
- Worker 输入输出均为结构化 Schema。
- Playwright、A11y、Locator、Evidence 合同测试通过。

### M4：删除 Python legacy Planning

删除：

- `/api/v1/ai-planning`
- `app/ai/test_planning_agent.py`
- `app/ai/test_planning_prompts.py`
- `app/application/planning/`
- `app/services/ai_planning_streaming.py`
- `app/services/sse_event_log.py`
- legacy message/draft/event/tool-result ORM 与表
- 仅为旧 Planning 服务的配置、schema 和前端类型

保留：

- `AIPlanningSession`，直至 Go Session 表重命名迁移完成。
- `SessionProject`，作为会话与项目归属关系。
- `DslGenerationRun`，直至 DSL 生成完全迁入 Go。

验收：

- Python 路由表不存在 `/api/v1/ai-planning`。
- 全仓不存在 Python ReAct loop。
- Go 是唯一调用规划模型和决定工具顺序的进程。

### M5：控制面归 Go

1. 将 DSL Schema 校验和候选版本持久化迁至 Go。
2. 将 Case、Batch/Job、Report 查询和 actor 上下文逐域迁至 Go。
3. Python API 改为仅内网可见的 Browser Worker。
4. 将 `backend/` 重命名为 `browser-worker/`。

当前进度：

- 已完成 Go Project、Case、Batch/Job、Execution、Overview、Correction HTTP
  API，并将前端全部切至 `/api/v2`；当前使用固定 actor，不启用鉴权。
- 已完成完整 Overview 统计语义，包括双窗口对比、补零趋势、失败分类、
  失败动作、高频失败用例和根因聚合。
- 已通过真实 PostgreSQL 纵向测试验证 Project → Case → Batch → Execution →
  Correction → Project 删除链路。
- 已删除 Python 公开控制面路由及其无调用查询服务。
- 已将 `generate_dsl`、`execute_dsl`、`get_report`、`fix_and_retry` 迁至
  Go，并删除 Python `/internal/agent-capabilities`。
- Python 当前仅保留 `/internal/browser-capabilities`、执行 Worker、
  Playwright/A11y/Locator、确定性失败信号和 evidence。
- 已将 `backend/` 物理重命名为 `browser-worker/` 并更新工程路径。

验收：

- 公网仅暴露 Go `/api/v2` 和受保护 artifact。
- Python 不提供用户业务 API，不调用 LLM。
- 正式执行仍只接受已批准且通过校验的 DSL。

## 删除门禁

- 生产调用图无引用。
- FastAPI 路由、SQLAlchemy metadata、Alembic 和 CLI 隐式入口已核对。
- 每阶段先迁调用方，再删除实现。
- 历史 migration 不删除；新增 migration 下线现存表。
- 每阶段通过 Go、Python、Vitest、Playwright smoke、Alembic check 和静态无用代码扫描。
