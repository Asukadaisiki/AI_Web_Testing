# 代码库架构与文件导航

本文回答三个问题：

1. 一个目录或文件为什么这样命名。
2. 修改某类功能时应该从哪里开始找。
3. 各模块之间如何调用，哪些依赖属于当前过渡状态。

## 一屏地图

```text
frontend/src/
  app/                 应用启动、路由和全局 Provider
  pages/               URL 对应的页面级组件
  features/            按业务域组织的 API、类型、状态和局部 UI
  components/          尚未完全归入业务域的复用组件
  shared/              无业务归属的 API 基础设施和通用 UI
  services/, types/    迁移中的旧兼容入口

backend/app/
  main.py              后端进程和 FastAPI 应用入口
  api/                 HTTP 协议层：路由、鉴权依赖、状态码
  application/         跨能力的业务用例编排
  services/            可复用的领域能力和数据操作
  ai/                  LLM、Agent、Prompt、页面探索和 AI 工具
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
  -> api/routes/ai_planning.py
  -> application/planning/*
  -> ai/test_planning_agent.py + ai/planning_tools.py

草案：
application/planning/draft_service.py
  -> services/dsl.py
  -> ai/dsl_generator.py
  -> ai/locator_preflight.py
  -> models/ai_planning_draft.py

执行：
api/routes/executions.py 或 Planning save_execute_service.py
  -> services/executions.py
  -> runners/playwright_runner.py
  -> locators/*
  -> reporters/json_report.py
  -> models/test_case_run.py

展示：
Frontend pages
  -> features/*/api.ts
  -> backend api/routes/*
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
- `api/routes/ai_planning.py`：Planning HTTP/SSE 接口。
- `api/auth.py`：路由使用的认证依赖。

### `application/`：用例编排层

这里的“用例”是业务 Use Case，不是自动化测试 Case。

它负责将多个底层能力组合成一次完整操作，例如：

- 保存草案后创建正式 Case，再触发执行。
- 收到 Planning 消息后组装上下文、调用 Agent、持久化消息。
- 根据历史执行结果分析并发起复测。

当前只有复杂度最高的 Planning 域进入了这一层：

| 文件 | 职责 |
|---|---|
| `planning/session_service.py` | Planning Session 的创建、列表、详情、删除 |
| `planning/project_context.py` | Session 所有权、关联项目、active project |
| `planning/conversation_service.py` | 对话消息、Agent 调用、流式消息持久化 |
| `planning/context_service.py` | 拼装项目状态、历史洞察和错误上下文 |
| `planning/draft_service.py` | 生成、更新、删除 Planning DSL 草案 |
| `planning/save_execute_service.py` | 草案转正式 Case，并选择是否执行 |
| `planning/analysis_retest_service.py` | 执行结果分析和复测 |
| `planning/execution_inputs.py` | 从 Planning 数据解析执行输入变量 |
| `planning/presenters.py` | ORM 对象到 Planning API Schema 的转换 |

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
| `sse_event_log.py` | SSE 事件持久化与重放 |
| `ai_planning_streaming.py` | Planning SSE 到 application service 的桥接 |
| `ai_planning.py` | 旧 Planning service 的兼容导出，非新代码入口 |

`application` 与 `services` 的区别：

- `application` 回答“完成一次用户操作需要哪些步骤”。
- `services` 回答“某个业务能力本身如何工作”。
- 新的 Planning 路由优先调用 `application/planning/`。
- 非 Planning 的简单 CRUD 路由目前直接调用 `services/`。

### `ai/`：模型与 Agent 能力

| 文件 | 职责 |
|---|---|
| `test_planning_agent.py` | Planning ReAct 循环和规划状态机 |
| `planning_tools.py` | Agent 可调用工具的注册、分发和实现 |
| `page_explorer.py` | 使用 Playwright/CDP 采集页面 A11y 信息 |
| `dsl_generator.py` | 调用 LLM 生成结构化 DSL |
| `locator_preflight.py` | 草案执行前检查 locator candidates |
| `tool_result_cache.py` | 页面探索等工具结果缓存 |
| `prompts/registry.py` | Prompt 模板和 stage 注册中心 |
| `test_planning_prompts.py` | Planning Prompt 的组装与兼容入口 |

这里的 `test_` 指“测试规划”，不是自动化测试文件。当前仅恢复了 `backend/tests/`
中的执行分析聚焦合同测试，完整测试分层仍待重建。

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
schemas.cases.StoredCaseDetail   API 返回的数据形状
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

当前已知过渡依赖：

- Planning `application` 仍直接调用多个 `services`，尚未抽成稳定 port。
- `ai/planning_tools.py` 同时调用 ORM 和 service。
- `locators/ai_visual.py` 依赖统一 Prompt Registry。

这些依赖不一定造成运行错误，但会降低“只看目录即可判断调用方向”的能力。

## 如何定位代码

| 需求或问题 | 第一入口 | 继续追踪 |
|---|---|---|
| 新增/修改 HTTP 接口 | `api/routes/<domain>.py` | `application/` 或 `services/` |
| Planning 对话异常 | `application/planning/conversation_service.py` | `ai/test_planning_agent.py` |
| Agent 工具行为异常 | `ai/planning_tools.py` | 对应 service 或 `page_explorer.py` |
| DSL 生成错误 | `services/dsl.py` | `ai/dsl_generator.py`、`schemas/dsl.py` |
| 草案生成或保存错误 | `application/planning/draft_service.py` | `save_execute_service.py` |
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
2. `backend/app/main.py`
3. `backend/app/api/router.py`
4. 选择一个具体 route，例如 `api/routes/executions.py`
5. 进入对应 service，例如 `services/executions.py`
6. 再进入 Runner、Locator、Schema 和 Model
7. 最后阅读 Planning Agent，因为它是当前复杂度最高的模块

需要理解产品核心时，建议先走“创建 Case -> 执行 -> 查看报告”，再进入 AI Planning 和 DSL 自动生成。
