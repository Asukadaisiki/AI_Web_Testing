# 代码库架构与文件导航

本文回答三个问题：

1. 一个目录或文件为什么这样命名。
2. 修改某类功能时应该从哪里开始找。
3. 各模块之间如何调用，哪些依赖属于当前过渡状态。

## 一屏地图

```text
backend-go/
  cmd/agentservice/    Hertz AgentService 入口
  internal/agent/      纯 Agent loop 与消息合同
  internal/harness/    Prompt、工具和运行编排
  internal/agentservice/ Run、Checkpoint、事件和持久化
  internal/tools/      Agent 工具
  internal/platform/   LLM 与 Python Worker 适配器

frontend/src/
  app/                 应用启动、路由和全局 Provider
  pages/               URL 对应的页面级组件
  features/            按业务域组织的 API、类型、状态和局部 UI
  components/          尚未完全归入业务域的复用组件
  shared/              无业务归属的 API 基础设施和通用 UI
  services/, types/    迁移中的旧兼容入口

browser-worker/app/
  main.py              Python Worker/FastAPI 入口
  api/                 迁移期平台 API 与内部 capability
  application/browser  Go AgentService 使用的浏览器能力
  application/reporting 确定性报告投影和失败分析
  services/            执行、DSL、Case 等迁移期领域能力
  ai/                  DSL 生成、页面探索和 locator preflight
  runners/             解释 DSL 并驱动 Playwright
  locators/            元素定位、修正和 fallback
  reporters/           将执行证据组装为结构化报告
  schemas/             Pydantic 请求、响应和运行时数据合同
  models/              SQLAlchemy 持久化模型
  db/                  数据库连接、Session 和 Base
  core/                配置、日志、中间件等横切基础设施
```

最重要的业务链路：

```text
规划：
Frontend Planning
  -> Go /api/v2/agent
  -> harness
  -> agent loop + tools

草案：
Go generate_dsl tool
  -> Python Browser Worker locator preflight
  -> Go DSL validation and persistence
  -> dsl_generation_runs

执行：
Go execute_dsl tool
  -> Go Case/ExecutionBatch/ExecutionJob
  -> Python execution worker
  -> services/executions.py
  -> runners/playwright_runner.py
  -> locators/*
  -> reporters/json_report.py
  -> models/test_case_run.py

展示：
Frontend pages
  -> features/*/api.ts
  -> Go transport/http/*
  -> services/*
  -> schemas/*
```

## 文件命名词典

| 名称片段 | 在本仓库中的含义 |
|---|---|
| `route` / `routes` | HTTP 接口入口，不代表业务实现 |
| `service` | 完成一个业务能力或业务用例 |
| `agent` | 让 LLM 在多轮决策中选择动作和工具 |
| `tools` | Agent 可以调用的受控函数集合 |
| `generator` | 将输入转换成 DSL、Prompt 等结构化结果 |
| `runner` | 执行已经确定的 DSL，不负责自由规划 |
| `locator` | 将 DSL 中的 target 解析成页面元素 |
| `preflight` | 正式执行前的静态或页面预检查 |
| `reporter` | 将执行证据转换成报告结构 |
| `presenter` | 将内部 ORM 数据转换成对外 Schema |
| `store` | 保存跨组件或跨请求生命周期的状态 |
| `schema` | 数据结构和校验合同，不负责持久化 |
| `model` | 数据库表的 ORM 映射 |
| `context` | 为一次 Planning/AI 调用准备的背景信息 |
| `streaming` / `sse` | 增量事件传输，不是另一套业务实现 |

带下划线的 Python 名称，例如 `_get_session`，表示模块私有实现。其他模块不应长期依赖这类名称。

## 后端分类

### `api/`：HTTP 适配层

负责：

- 定义 URL、HTTP method、参数和 response model。
- 注入数据库 Session 和当前用户。
- 将业务异常转换成 HTTP 状态码。
- SSE 路由负责建立连接，但不应承载核心业务规则。

不应负责：

- 直接拼复杂 SQL。
- 实现 DSL 生成、执行或 AI 决策。
- 复制 application/service 中的业务流程。

命名：

- `api/router.py`：汇总所有子路由。
- `api/routes/cases.py`：Case HTTP 接口。
- `api/routes/browser_capabilities.py`：Go 调用的页面探索接口。
- `api/auth.py`：Worker 内部接口使用的认证依赖。

### `application/`：Worker 能力边界

| 文件 | 职责 |
|---|---|
| `browser/service.py` | 页面探索和元素验证 capability |
| `agent_capabilities/service.py` | DSL、执行和报告内部 capability |
| `reporting/service.py` | Run/Batch/Project 报告投影 |
| `reporting/analysis_service.py` | 确定性 FailureSignal 分析 |

### `services/`：可复用业务能力

`services` 以业务实体或稳定能力命名，供普通 API 和 Planning 编排共同调用。

| 文件 | 职责 |
|---|---|
| `cases.py` | Case CRUD、项目成员校验和 Case 统计 |
| `executions.py` | 执行生命周期、持久化、执行查询和报告聚合 |
| `dsl.py` | DSL 校验、生成入口、生成记录和反馈治理 |
| `project_management.py` | Project CRUD 和成员关系 |
| `corrections.py` | Locator 人工修正记录 |
| `anti_patterns.py` | DSL 失败反例的记录和检索 |
| `settings.py` | AI 配置读取、更新和运行状态 |

### `ai/`：Worker 内 AI 辅助能力

| 文件 | 职责 |
|---|---|
| `page_explorer.py` | 使用 Playwright/CDP 采集页面 A11y 信息 |
| `dsl_generator.py` | 调用 LLM 生成结构化 DSL |
| `locator_preflight.py` | 草案执行前检查 locator candidates |
| `prompts/registry.py` | Prompt 模板和 stage 注册中心 |

### `runners/`、`locators/`、`reporters/`

- `runners/playwright_runner.py`：逐步解释已经校验的 DSL，操作浏览器并产生步骤事件。
- `runners/click_preprocessor.py`：点击前处理遮挡、弹窗等页面状态。
- `runners/postcondition_verifier.py`：验证步骤后置条件。
- `locators/semantic.py`：基于 A11y/文本的确定性定位。
- `locators/corrections.py`：读取人工修正。
- `locators/fallback.py`：组织 Correction、Semantic、VLM、Intervention 回退顺序。
- `locators/ai_visual.py`：可选 VLM 定位能力，默认关闭。
- `reporters/json_report.py`：把步骤 evidence 组装成报告。

边界原则：Runner 负责执行，Locator 负责找元素，Reporter 负责组装结果，三者不应决定业务权限或 Planning 流程。

### `schemas/`、`models/`、`db/`

- `schemas/` 是内存中的数据合同，主要使用 Pydantic；文件按业务域命名。
- `models/` 是数据库表映射，主要使用 SQLAlchemy；通常一个主要实体一个文件。
- `db/` 只负责数据库基础设施。

同名概念的区别：

```text
backend-go/internal/cases.Stored API 返回的数据形状
models.test_case.TestCase        数据库中的 Case 记录
schemas.dsl.DSLCase              Runner 消费的结构化 DSL
```

## 前端分类

### `app/`

- `App.tsx`：React Query、主题、Router 等全局 Provider。
- `AppRouter.tsx`：URL 到页面组件的唯一主路由表。

### `pages/`

页面是路由级容器。它负责读取 URL 参数、发起页面级查询并组合业务组件，不应实现后端执行逻辑。

例如：

- `PlanningPage.tsx` 对应 `/planning/sessions/:sessionId`。
- `CasesPage.tsx` 对应 `/cases`。
- `ExecutionDetailPage.tsx` 对应 `/reports/:executionId`；旧 `/run/:executionId` 仅保留兼容重定向。

### `features/`

按业务域分类：

```text
features/
  auth/
  agent/
  planning/
  projects/
  cases/
  executions/
```

常见文件后缀：

- `api.ts`：该业务域的 HTTP 请求函数。
- `types.ts`：该业务域公开使用的 TypeScript 类型。
- `useXxx.ts`：React hook，封装状态或副作用。
- `xxxStore.tsx`：跨页面生命周期的外部状态仓库。
- `XxxPanel.tsx`：业务域内部的视图组件。

Agent 与 Planning 的边界：

| 文件 | 职责 |
|---|---|
| `features/agent/useAgentRun.ts` | 管理 Go AgentRun、SSE 订阅和 checkpoint 恢复 |
| `features/agent/events.ts` | 将 Agent 事件归并为消息、工具轨迹和 artifact |
| `features/planning/api.ts` | Go Planning Session 元数据与项目关联客户端 |
| `components/AgentWorkbench.tsx` | 当前 Planning 工作台 |

### `shared/`

只放没有业务归属、可被多个 feature 使用的代码：

- `shared/api/client.ts`：统一 HTTP client。
- `shared/ui/`：通用状态组件。

### 当前过渡目录

- `components/` 中仍有较大的业务组件，后续应逐步归入对应 feature。
- `types/api.ts` 是手写总类型文件，feature types 目前大多仍从这里重新导出。

新增代码不应继续扩大这些兼容入口。

## 依赖规则

推荐依赖方向：

```text
Backend:
api -> application -> services/ai/runners
services -> models/schemas/runners/locators/reporters
runners -> locators + schemas
models -> db

Frontend:
app -> pages -> features -> shared
```

禁止方向：

- `models` 不依赖 `services` 或 `api`。
- `runners` 不依赖 Planning。
- `locators` 不依赖 HTTP route。
- `shared` 不依赖具体 feature。
- 前端不得实现正式测试执行逻辑。

当前保留的跨进程依赖：

- Go 通过内部 Browser capability 调用 Python 的探索与 locator preflight。
- Python `locators/ai_visual.py` 依赖只包含 VLM 的 Prompt Registry。

这些依赖不一定造成运行错误，但会降低“只看目录即可判断调用方向”的能力。

## 如何定位代码

| 需求或问题 | 第一入口 | 继续追踪 |
|---|---|---|
| 新增/修改 HTTP 接口 | `api/routes/<domain>.py` | `application/` 或 `services/` |
| Planning 对话异常 | `backend-go/internal/harness/` | `internal/agent/loop.go` |
| Agent 工具行为异常 | `backend-go/internal/tools/` | 对应 Go Store 或 Python Browser capability |
| DSL 生成错误 | `services/dsl.py` | `ai/dsl_generator.py`、`schemas/dsl.py` |
| DSL 候选生成错误 | `application/agent_capabilities/service.py` | `services/dsl.py` |
| 浏览器步骤执行错误 | `services/executions.py` | `runners/playwright_runner.py` |
| 元素找不到 | `locators/fallback.py` | `semantic.py`、`corrections.py`、`ai_visual.py` |
| 报告统计错误 | `services/executions.py` | `reporters/json_report.py` |
| 数据表或关系问题 | `models/` | `alembic/versions/` |
| 前端路由问题 | `frontend/src/app/AppRouter.tsx` | 对应 `pages/` |
| 前端 Agent 状态问题 | `features/agent/useAgentRun.ts` | `features/agent/events.ts` |
| 前端 API 类型问题 | `features/<domain>/api.ts` | `types.ts`、`types/api.ts` |

## 推荐阅读顺序

第一次阅读不要按目录逐文件阅读，按一条业务链走：

1. `frontend/src/app/AppRouter.tsx`
2. `browser-worker/app/main.py`
3. `browser-worker/app/api/router.py`
4. 选择一个具体 route，例如 `api/routes/executions.py`
5. 进入对应 service，例如 `services/executions.py`
6. 再进入 Runner、Locator、Schema 和 Model
7. 最后阅读 Go `internal/agent`、`internal/harness` 和 `internal/agentservice`

需要理解产品核心时，建议按“AgentRun -> ToolCall -> DSL 审批 -> Batch/Job -> Runner -> Report”阅读。
