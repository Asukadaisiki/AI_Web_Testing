# 接口总览与用途说明

核对日期：2026-09-10。源码基线：`5e46d6c723bbb924e02be6b020467bb9b44f96f9`。

本文按当前注册路由和实际实现整理，不把历史设计、仓储方法、前端页面路由当作已上线 HTTP 接口。接口表覆盖全部自定义 HTTP 路由；参数和响应列列出联调关键字段，嵌套结构通过各节源码链接进一步查询。本文不是自动生成的 OpenAPI，也不是逐字段完整 Schema。

## 1. 数量与阅读入口

| 接口层 | 数量 | 谁来调用 | 用来做什么 |
|---|---:|---|---|
| Go 平台业务 HTTP/SSE | 37 | 前端、平台调用方、E2E Driver | 会话、项目、用例、Agent、执行、报告和人工修正 |
| Go Research HTTP | 13 | 实验与验收 Driver | 实验调度、证据关联、独立验收和指标计算 |
| Go 健康检查 | 1 | 本地联调、部署探针 | 检查 Go 服务是否响应 |
| Python Worker 自定义 HTTP | 5 | Go 控制面、执行 Worker、证据查看页面 | 浏览器能力、DSL 执行、证据下载、健康和元信息 |
| Agent 工具 | 9 | AgentCore 经 Harness 调用 | 计划、探索、生成、审批提问、执行与修复 |

**合计：56 个自定义 HTTP 路由，其中 Go 51 个、Python 5 个；另有 9 个非 HTTP Agent 工具。**

计数按“HTTP 方法 + 路径模板”，同一路径的 GET/POST 分别计数。Worker 的 `{capability}` 是一个路由，支持三种能力；不重复计为三个接口。FastAPI 自动提供的文档路由、自动 HEAD/OPTIONS 不纳入自定义接口数。Research 在注入 ResearchAPI 时注册，当前正式 `cmd/agentservice` 已注入。

按需求直接阅读：

- 接平台页面：[项目](#3-项目管理5-个)、[会话](#4-planning-会话与项目关联9-个)、[用例](#6-用例管理6-个)。
- 接 AI 工作台：[Agent 与 SSE](#5-agent-run-与事件6-个)、[Agent 工具](#11-agent-工具9-个不是-http-接口)。
- 做回归和报告：[执行与报告](#7-执行与报告10-个)、[人工修正](#8-人工定位修正1-个)。
- 做实验验收：[Research](#9-research-实验与验收13-个)。
- 接 Python 或查截图：[Browser Worker](#10-browser-worker5-个自定义-http-路由)。
- 跑通整体流程：[典型调用链](#12-典型调用链)、[本地调用示例](#13-本地调用示例)。

## 2. 公共约定

### 2.1 地址、身份与数据格式

| 项目 | 当前约定 |
|---|---|
| Go 本地地址 | `http://127.0.0.1:8081`，可用 `AGENTSERVICE_HTTP_ADDR` 修改 |
| 平台 API 前缀 | `/api/v2` |
| Worker 本地地址 | `http://127.0.0.1:8000`，由 `APP_HOST` / `APP_PORT` 控制 |
| Worker 内部 API | 默认 `/api/v1/internal/*`；只供后端使用 |
| 前端开发代理 | Vite 将 `/api/v2` 转发 Go，`/artifacts` 转发 Python |
| 生产入口 | Nginx 代理 `/api/v2/`、Go `/health`、`/artifacts/`；阻断 `/api/v1/internal` 及其子路径 |
| 身份 | 当前不启用登录、Cookie、Token 认证；Go 注入 `DEFAULT_ACTOR_USER_ID`，默认 `1` |
| 归属 | 仍检查服务端 actor 对资源的归属/项目成员关系；客户端不能通过提交 actor 字段切换身份 |
| 请求 | 有 JSON 请求体时使用 `Content-Type: application/json` |
| 响应 | 没有统一的 `{code,data}` 包装；可能是对象、数组、分页对象、SSE 或文件 |
| ID | project/session/case/batch/execution 是正整数；Agent/Research run 和 experiment 是字符串 |
| 空响应 | `204 No Content` 没有 JSON，客户端不要调用 `response.json()` |
| 时间 | JSON 时间字符串；部分结束时间可为 `null`。数据库时区与统计窗口的已知问题见 BUG-177 |

无登录不等于适合直接暴露公网。当前固定 actor 不能替代多用户认证；Worker 本身也不负责校验业务归属。

### 2.2 成功与错误

接口表中的状态码是成功状态，不表示业务测试一定通过。执行结果必须读取 `status` 和结构化报告。

Go 错误结构：

```json
{
  "error": "Not Found",
  "message": "agent run not found"
}
```

| HTTP 状态 | 典型含义 |
|---|---|
| `400` | 请求绑定失败、非法路径/查询参数、Research 合同错误等 |
| `403` | 固定 actor 不具备相应资源归属或项目权限 |
| `404` | 资源不存在，或按当前归属范围不可见 |
| `409` | 状态冲突、恢复的 ToolCall 不匹配、Research 链路/版本不一致等 |
| `500` | 未映射的服务错误、数据库错误；当前部分业务校验错误也落入此分支 |

Worker 通常使用 FastAPI `{"detail": ...}` 错误结构：参数/合同失败为 `422`，证据文件缺失为 `404`，限流为 `429`；其他错误可能为 `500`。不要假设它与 Go 的错误格式相同。

### 2.3 容易混淆的对象

| 对象 | 意义 | 不等于什么 |
|---|---|---|
| Planning Session | 工作台会话及项目上下文 | 不是一次正式浏览器执行 |
| AgentRun | 一次自然语言任务的 AI 编排过程 | `completed` 不代表测试已通过 |
| TaskPlan / PlanStep | 版本化的业务动作、顺序、次数与安全边界 | 不是前端自由改写的步骤列表 |
| DSL Generation | 绑定计划和浏览器证据的一版用例产物 | 生成成功不代表已获用户审批 |
| ExecutionBatch | 一批正式执行任务 | 不等于单条运行记录 |
| ExecutionJob | 批次中某个 Case 的队列任务 | 一个 Job 可对应不同 attempt |
| Execution / TestCaseRun | 一次实际执行及其 DSL 快照、报告 | ID 与 AgentRun ID 不通用 |
| ResearchRun | 实验中的一次测量/预热，关联上述对象 | 不会仅凭 `start` 自动跑完整 AI 流程 |

## 3. 项目管理（5 个）

用于管理测试资产的归属容器。对应项目选择、用例归类、项目回归及报告过滤。

| 方法 | 完整路径 | 用途 | 请求关键字段 | 成功响应 |
|---|---|---|---|---|
| GET | `/api/v2/projects` | 列出当前 actor 可访问的项目 | 无 | `200`，Project 数组 |
| POST | `/api/v2/projects` | 新建项目并建立 owner 成员关系 | `name` 必填，`description` 可选 | `201`，Project |
| GET | `/api/v2/projects/{project_id}` | 查询项目详情 | 路径 ID | `200`，Project |
| PUT | `/api/v2/projects/{project_id}` | 修改项目名称或说明 | `name`、`description` 可选 | `200`，Project |
| DELETE | `/api/v2/projects/{project_id}` | 删除项目及关联测试资产 | 路径 ID | `204` |

Project：`id`、`name`、`description`、`created_at`、`updated_at`。

注意：

- 创建名称长度为 1～200；更新名称会去除首尾空白，不能更新为空。
- PUT 实际是按提供字段更新；省略字段保留旧值，`description: null` 不表示清空。
- 列表没有分页协议。
- 删除检查存储中的 `owner` 成员关系，并删除相关执行、批次、修正及数据库关联资产；不是仅删除项目标签，也不能视为浏览器任务取消接口。

源码：[路由处理](../backend-go/internal/transport/http/projects.go)、[类型](../backend-go/internal/projects/types.go)、[存储行为](../backend-go/internal/projects/postgres.go)。

## 4. Planning 会话与项目关联（9 个）

用于工作台会话列表、会话标题/状态，以及决定 AI 任务在哪个项目下工作。

| 方法 | 完整路径 | 用途 | 请求关键字段 | 成功响应 |
|---|---|---|---|---|
| POST | `/api/v2/planning/sessions` | 创建规划会话 | `case_id?`、`project_id?`、`clean_context?` | `201`，`{"session": Session}` |
| GET | `/api/v2/planning/sessions` | 查询会话列表 | 无 | `200`，SessionSummary 数组 |
| GET | `/api/v2/planning/sessions/{session_id}` | 恢复工作台元信息 | 路径 ID | `200`，`{"session": Session}` |
| PATCH | `/api/v2/planning/sessions/{session_id}` | 更新标题或会话业务状态 | `title?`、`status?` | `200`，`{"session": Session}` |
| DELETE | `/api/v2/planning/sessions/{session_id}` | 删除会话记录 | 路径 ID | `204` |
| GET | `/api/v2/planning/sessions/{session_id}/projects` | 查询会话关联项目 | 路径 ID | `200`，ProjectSummary 数组 |
| POST | `/api/v2/planning/sessions/{session_id}/projects` | 关联已有项目，并切换为活动项目 | `project_id` 必填 | `201`，ProjectSummary |
| DELETE | `/api/v2/planning/sessions/{session_id}/projects/{project_id}` | 解除关联，不删除项目 | 两个路径 ID | `204` |
| POST | `/api/v2/planning/sessions/{session_id}/projects:create` | 在会话内创建并激活新项目 | `name` 必填，`description?` | `201`，ProjectSummary |

Session 关键字段：

- `id`、`actor_user_id`、`runtime_owner`（当前为 `go`）。
- `active_project_id`、`case_id`、`projects`。
- `title`、`status`、`requirements`、`plan`、`missing_slots`、`last_error_message`。
- `created_at`、`updated_at`。

ProjectSummary：`id`、`name`、`description`、`is_active`。SessionSummary 是列表用简化视图，不包含完整 requirements/plan。

行为约束：

1. 创建时不指定项目，会优先选择 actor 的默认/已有项目；没有可用项目才新建 `default-{session_id}`。
2. `clean_context` 默认 `false`，存入会话 requirements，供 Go 向 Worker 传递浏览器上下文策略。
3. PATCH 不支持直接写 requirements、TaskPlan 或 DSL；标题去空白后最长 200。
4. 会话状态可为 `collecting`、`plan_ready`、`drafts_ready`、`reviewing`、`saving`、`executing`、`completed`、`closed`、`error`，与 AgentRun 状态不是同一枚举。
5. 解绑活动项目后，服务选择剩余关联项目；没有项目时，启动 AgentRun 无法解析有效项目上下文。
6. 会话详情不是聊天事件记录；聊天和工具历史通过 Agent 事件接口读取。删除会话也不应作为取消执行的替代操作。

源码：[路由处理](../backend-go/internal/transport/http/planning.go)、[类型](../backend-go/internal/planning/types.go)、[项目解析](../backend-go/internal/planning/postgres.go)。

## 5. Agent Run 与事件（6 个）

用于发送自然语言任务、查看进度、恢复历史、接收流式消息、回答 AI 问题和批准 DSL。

| 方法 | 完整路径 | 用途 | 请求关键字段 | 成功响应 |
|---|---|---|---|---|
| POST | `/api/v2/agent/runs` | 启动一次 AI 任务 | `session_id`、`message`；兼容 `conversation_id` | `202`，AgentRun，后台继续运行 |
| GET | `/api/v2/agent/runs/{run_id}` | 查询任务当前状态 | 字符串 run ID | `200`，AgentRun |
| GET | `/api/v2/agent/runs/{run_id}/events` | 分页游标式读取已持久化事件 | `after_seq?`，默认 0 | `200`，`{"events": Event[]}` |
| GET | `/api/v2/agent/runs/{run_id}/events/stream` | 先重放历史，再实时推送事件 | `after_seq?` 或 `Last-Event-ID` | `200`，`text/event-stream` |
| POST | `/api/v2/agent/runs/{run_id}/cancel` | 取消 Agent 编排，停止活跃循环 | `reason` 必填且不能全空白 | `200`，AgentRun |
| POST | `/api/v2/agent/runs/{run_id}/tool-calls/{tool_call_id}/resume` | 回答问题/提交审批并恢复任务 | `answers` 对象、`next_step?` 字符串 | `200`，继续运行后的 AgentRun |

### 5.1 启动与恢复

推荐启动请求：

```json
{
  "session_id": 1,
  "message": "打开 https://example.com 并验证页面地址包含 example.com"
}
```

- `message` 为非空字符串；自由文本只进入 Agent 规划，不直接交给 Runner 执行。
- `session_id` 为有效正整数；未提供有效 session ID 时，可从兼容字段 `conversation_id: "1"` 解析。这里的 conversation 不是任意聊天 UUID。
- 请求结构仍有 `project_id`，但处理器不拿它决定执行项目；以会话的有效关联项目为准。
- AgentRun 返回 `id`、`conversation_id`、`project_id`、`status`、`input`、时间及可选的 `pending_tool_call_id`、`pending_step_id`、`latest_generation_id`、`approved_generation_id`。
- 完整 transcript 不在 AgentRun JSON 中返回。
- `resume` 必须匹配当前挂起 ToolCall，通常先看到 `waiting_user` / `tool.pending`。不存在通用的“任意位置恢复”接口。
- 审批使用 `{"answers":{"approve_dsl":true}}`；只有布尔值 `true` 才表示批准最新 generation，不能传字符串 `"true"`。
- `next_step` 是恢复信息，不是任意跳转或绕过审批的指令。
- `resume` 当前会继续运行 Agent 再返回，不是与启动相同的立即 `202` 模式；调用方应留足超时并持续监听 SSE。
- 取消 AgentRun 不等价于取消已入队 ExecutionBatch；需要停止正式执行时，另调批次取消接口。

AgentRun 状态：`running`、`waiting_user`、`completed`、`failed`、`cancelled`。

### 5.2 SSE 与事件协议

事件结构：

```json
{
  "seq": 1,
  "type": "run.started",
  "conversation_id": "1",
  "run_id": "run_example",
  "timestamp": "2026-09-10T08:00:00Z",
  "payload": {}
}
```

关联字段按事件类型可包含 `step_id`、`tool_call_id`、`parent_id`、`checkpoint_id`。

| 事件类型 | 用途 |
|---|---|
| `run.started`、`run.finished`、`run.failed`、`run.cancelled` | Agent 生命周期和终态 |
| `message.started`、`message.delta`、`message.finished` | 助手消息的开始、增量和完成 |
| `tool.started`、`tool.args.delta` | 工具开始及参数增量 |
| `tool.pending` | 工具挂起，等待用户回答或审批 |
| `tool.result`、`tool.finished`、`tool.failed` | 工具结果及完成/失败 |
| `artifact.published` | DSL、批次、报告、计划等产物引用 |
| `task_plan.updated` | 计划版本、SHA、状态及步骤 grounding 摘要 |
| `research.llm_call` | 模型请求、usage、延迟、重试与 request ID 审计 |
| `agent.pipeline.trace` | 模型请求上下文组成、累计 usage，以及工具签名、state epoch、attempt 和 retry lineage |

断线重连规则：

1. `seq` 在同一个 Run 内单调递增；保存最后成功处理的序号。
2. 重连携带 `after_seq=N`，仅获取 `seq > N` 的事件；必须为非负整数。
3. SSE 未提供 `after_seq` 时才使用 `Last-Event-ID`。普通 events 接口只接受查询参数。
4. SSE 的 `id` 为序号，`event` 为事件类型，`data` 为完整 Event JSON。
5. 服务约每 15 秒发送 keep-alive；Run 到终态后补发已持久化事件并关闭连接。
6. `waiting_user` 不是终态，不应仅因暂无消息就判断任务结束。
7. 使用具名事件监听，例如 `addEventListener("tool.pending", ...)`；不能只依赖默认 `onmessage`。

源码：[路由、SSE 与错误映射](../backend-go/internal/transport/http/handler.go)、[Run/Event 类型](../backend-go/internal/agentservice/types.go)、[审批与恢复](../backend-go/internal/harness/harness.go)。

## 6. 用例管理（6 个）

用于保存可复用的结构化 DSL 用例，供用例中心编辑和回归执行。自然语言任务不是用例执行格式。

| 方法 | 完整路径 | 用途 | 请求关键字段 | 成功响应 |
|---|---|---|---|---|
| GET | `/api/v2/cases` | 按项目/名称检索用例 | `project_id?`、`search?`、`page?`、`page_size?` | `200`，CasePage |
| POST | `/api/v2/cases` | 新建结构化用例 | CaseMutation | `201`，StoredCase |
| GET | `/api/v2/cases/{case_id}` | 读取用例及步骤 | 路径 ID | `200`，StoredCase |
| PUT | `/api/v2/cases/{case_id}` | 更新完整用例定义 | CaseMutation | `200`，StoredCase |
| DELETE | `/api/v2/cases/{case_id}` | 删除单个用例 | 路径 ID | `204` |
| DELETE | `/api/v2/cases/batch` | 批量删除用例 | `{"case_ids":[1,2]}`，非空 | `204`；未删到任何可访问记录时 `404` |

CaseMutation：

| 字段 | 类型 | 说明 |
|---|---|---|
| `project_id` | integer | 必填，正整数；目标项目必须可访问 |
| `name` | string | 必填，1～200，不能全空白 |
| `profile` | string，可选 | `legacy-v1` / `research-v1` / `research-v2` |
| `description` | string/null，可选 | 用例说明 |
| `base_url` | string/null，可选 | 相对导航路径的基准 URL |
| `input_contract` | array，可选 | 输入变量定义；缺省按空数组处理 |
| `output_contract` | array，可选 | 输出变量定义；缺省按空数组处理 |
| `steps` | array | 必填且非空，结构化动作列表 |

StoredCase 在上述字段基础上增加 `id`、`created_by`、`updated_by`、`created_at`、`updated_at`。

CasePage：`items`、`total`、`page`、`page_size`、`total_pages`、`has_next`、`has_prev`。默认 `page=1`、`page_size=20`；非法/非正值回退默认值，`page_size` 最大 200。

注意：

- Case PUT 与 Project PUT 不同，必须提交 `project_id`、`name`、`steps` 等完整定义。
- CRUD 成功仅说明定义已保存，不代表通过正式执行所需的全部 DSL/grounding 校验。
- 当前稳定动作集为 `goto`、`click`、`input`、`wait_for`、`assert_text`、`assert_url_contains`、`capture_text`。
- `research-v1/v2` 正式执行需要内部 canonical DSL binding；它不是公共 Batch API 可提交字段。不要把手工保存 research profile 后直接执行视为绕过 Agent 审批的入口。
- 批量删除按可访问记录实际删除，不承诺请求的每个 ID 都存在。

源码：[路由处理](../backend-go/internal/transport/http/cases.go)、[类型](../backend-go/internal/cases/types.go)、[保存校验](../backend-go/internal/cases/postgres.go)、[正式执行校验](../backend-go/internal/execution/store.go)。

## 7. 执行与报告（10 个）

### 7.1 单用例执行与批次（6 个）

用于启动正式 Playwright 执行、跟踪队列以及项目回归。Go API 持久化 Batch/Job，Go execution-worker 消费队列，Python 执行浏览器动作，再由 Go 落库。

| 方法 | 完整路径 | 用途 | 请求关键字段 | 成功响应 |
|---|---|---|---|---|
| POST | `/api/v2/cases/{case_id}/execute` | 执行单个用例并等待终态 | `input_values?`，可传 `{}` | `200`，ExecutionDetail |
| POST | `/api/v2/execution-batches` | 异步创建一批执行任务 | BatchCreateRequest | `201`，BatchDetail |
| GET | `/api/v2/execution-batches` | 查看某项目最近批次 | `project_id` 必填，`limit?` | `200`，BatchSummary 数组 |
| GET | `/api/v2/execution-batches/{batch_id}` | 查看批次和各 Job 状态 | 路径 ID | `200`，BatchDetail |
| GET | `/api/v2/execution-batches/{batch_id}/report` | 查看批次聚合报告和最新执行详情 | 路径 ID | `200`，BatchReport |
| POST | `/api/v2/execution-batches/{batch_id}/cancel` | 取消待执行任务，标记运行中任务取消 | 无请求体 | `200`，BatchDetail |

BatchCreateRequest：

```json
{
  "project_id": 1,
  "case_ids": [10, 11],
  "concurrency_limit": 2,
  "input_values": {},
  "idempotency_key": "regression-20260910-001"
}
```

| 字段 | 说明 |
|---|---|
| `project_id` | 必填，正整数 |
| `case_ids` | 必填、非空，同一项目的可执行用例 ID；服务去重 |
| `planning_session_id` | 可选；必须属于 actor 且关联该项目，同一会话已有 pending/running 批次时冲突 |
| `concurrency_limit` | 有效范围 1～16；缺省或越界时使用 1 |
| `input_values` | `map<string,string>`，缺省 `{}`；不是任意嵌套 JSON |
| `idempotency_key` | 可选字符串；同一 actor 下同 key、同项目复用已有批次，不比较新请求的完整内容 |

关键约束：

- **创建批次必须传 `case_ids`**，不存在省略后自动执行整个项目的逻辑。全项目回归先列出用例再提交 ID。
- 幂等 key 放在 JSON 请求体，而不是 Go 的 `Idempotency-Key` HTTP header。同 key 跨项目返回冲突；不同执行意图使用新 key。
- 单用例 `/execute` 内部也建批次，但会每秒读取报告直到终态；Worker 未启动可能长时间等待，交互页面优先使用批次接口。
- 批次列表 `limit` 默认 50，有效范围 1～100；越界回退 50，不是截断到 100。
- `/report` 是当前快照，未执行完也能返回 `pending/running`，不会在 HTTP 层等待终态。
- 取消不是即时终止所有浏览器动作的承诺。待执行 Job 直接取消，运行中 Job 设置 `cancel_requested`；返回批次仍可能为 `running`，继续轮询终态。
- 仅对需要停止的活动批次使用 cancel，不要把它作为删除或归档接口。

### 7.2 执行记录与统计（4 个）

用于报告列表、单次失败排查、通过率趋势及失败根因聚合。

| 方法 | 完整路径 | 用途 | 请求关键字段 | 成功响应 |
|---|---|---|---|---|
| GET | `/api/v2/executions` | 查询实际执行记录摘要 | 下表过滤条件 | `200`，ExecutionSummary 数组 |
| GET | `/api/v2/executions/{execution_id}` | 查看一次执行的完整证据与分析 | 路径 ID | `200`，ExecutionDetail |
| DELETE | `/api/v2/executions/{execution_id}` | 删除运行记录 | 路径 ID | `204` |
| GET | `/api/v2/executions/overview` | 聚合全局/项目/用例的统计数据 | `scope_type?`、`project_id?`、`case_id?`、`window_days?`、`failure_fingerprint?` | `200`，Overview |

执行列表查询字段：

| 字段 | 用途/默认值 |
|---|---|
| `project_id`、`case_id` | 可选正整数，限定项目或用例 |
| `status` | 按状态匹配 |
| `failure_category` | 按 FailureSignal 类别匹配 |
| `failure_fingerprint` | 按同一失败根因指纹过滤 |
| `window_days` | 最近 N 天；缺省/非正/非法时不限制时间 |
| `limit` | 默认 20，有效 1～100；越界回退 20 |
| `offset` | 默认 0；非法或负值回退 0 |

列表不是 CasePage，不返回 total/page。摘要去掉 `report`、`dsl_snapshot`、`analysis_status`、`analysis`，查看这些字段需要详情接口。

Overview：`scope_type` 为 `global/project/case`，省略时按 case ID、project ID、global 的优先级推断；显式 global 忽略两个 ID，project 忽略 case ID。调用方应为 project/case 提供对应 ID。`window_days` 仅 7/14/30 有效，其余回退 7。

Overview 关键输出：`total_count`、`passed_count`、`failed_count`、`running_count`、`pass_rate`、`automation_rate`、`intervention_rate`、`avg_duration_ms`、`trend_points`、`failure_categories`、`failure_step_actions`、`top_failed_cases`、`failure_root_causes`、`latest_failed_runs`、`latest_intervention_runs`、`current_window_range`、`previous_window_range`、`previous_window_stats`、`window_comparison`。

### 7.3 报告对象

| 对象 | 关键内容 |
|---|---|
| BatchSummary | `id`、项目/会话 ID、`status`、并发、各状态 Job 数、分析及时间 |
| BatchDetail | BatchSummary + `jobs` |
| Job | `id`、`case_id`、`order_index`、`status`、`attempt_count`、`max_attempts`、`cancel_requested`、heartbeat、`latest_execution` |
| BatchReport | BatchDetail + `completed_jobs`、`pass_rate`；单 Job 场景还带 canonical DSL 元信息 |
| ExecutionDetail | `id`、`case_id`、`batch_id`、`job_id`、`attempt_number`、`status`、`error_message`、`dsl_snapshot`、`dsl_sha256`、`report`、`failure_signal`、`analysis` |
| `report.steps` | 步骤动作、结果、URL、截图/DOM 证据、定位诊断等；以实际报告 Schema 为准 |

批次通过率为 `passed / (passed + failed + needs_intervention)`，分母为 0 时返回 0；取消任务不纳入该分母。

Batch/Job 常见状态：`pending`、`running`、`passed`、`failed`、`needs_intervention`、`cancelled`。不要用 AgentRun 的 `completed` 代替测试的 `passed`。

删除执行记录不是取消任务，也不保证一并删除磁盘上的 artifact 文件。

源码：[路由处理](../backend-go/internal/transport/http/execution.go)、[请求、队列与报告](../backend-go/internal/execution/store.go)、[统计口径](../backend-go/internal/execution/overview.go)、[步骤证据合同](../browser-worker/src/browser_worker/contracts/executions.py)。

## 8. 人工定位修正（1 个）

| 方法 | 完整路径 | 用途 | 请求关键字段 | 成功响应 |
|---|---|---|---|---|
| POST | `/api/v2/corrections` | 保存人工提供的定位修正，关联原执行以供后续定位流程使用 | 下列字段均必填 | `201`，StoredCorrection |

```json
{
  "page_url": "https://example.com/form",
  "target_description": "提交按钮",
  "correction_type": "test_id",
  "correction_value": "submit",
  "source_execution_id": 100
}
```

- `page_url` 必须为绝对 URL；服务归一化为 `page_url_pattern`。
- `correction_type` 为 `css` / `xpath` / `test_id`；注意这里不是 `data-testid`。
- `target_description`、`correction_value` 非空；来源 execution 必须可访问。
- 同一 URL 模式和归一化 target 已有活动修正时，更新已有记录。
- 响应包含 `id`、`page_url_pattern`、target/type/value、`verified_count`、`consecutive_failures`、`is_active`、来源和时间。
- 该请求只保存修正，不会自动重跑，也不证明修正后的 locator 已在真实页面验证。
- **当前没有修正列表、单条 GET 或删除 HTTP 路由。** POST 虽返回 `/api/v2/corrections/{id}` 的 `Location`，该地址当前不能 GET；见 BUG-178。请使用 POST 返回的完整对象。

源码：[HTTP 处理](../backend-go/internal/transport/http/corrections.go)、[请求与保存](../backend-go/internal/corrections/store.go)。

## 9. Research 实验与验收（13 个）

用于可复现地比较 Agent 运行结果、保留独立验收结论和计算模型成本/质量指标，不是普通用例 CRUD。

### 9.1 实验定义与调度（5 个）

| 方法 | 完整路径 | 用途 | 请求关键字段 | 成功响应 |
|---|---|---|---|---|
| POST | `/api/v2/research/experiments` | 注册实验定义及控制变量 | Experiment 定义 | `201`，Experiment |
| GET | `/api/v2/research/experiments` | 查询实验列表 | `project_id` 必填；`status?`、`variant?`、`limit?`、`offset?` | `200`，Experiment 数组 |
| GET | `/api/v2/research/experiments/{experiment_id}` | 查看实验配置和状态 | query `project_id` 必填 | `200`，Experiment |
| POST | `/api/v2/research/experiments/{experiment_id}/start` | 激活实验，生成预热/正式测量 schedule | `{"project_id":1}` | `200`，`{experiment,schedule}` |
| GET | `/api/v2/research/experiments/{experiment_id}/runs` | 查询实验的有序运行列表 | query `project_id` 必填 | `200`，`[{order,run}]` |

Experiment 关键输入：

| 字段组 | 字段与说明 |
|---|---|
| 标识 | `id?`（省略时由定义生成）、`project_id`、`name`、`goal` |
| 数据集 | `dataset_version` |
| 模型 | `model_provider`、`model_name`、`model_version`、`prompt_version` |
| 浏览器 | `browser_name`、`browser_version`、`viewport` 对象 |
| 实现版本 | `code_sha256`（64 位十六进制摘要，不是 40 位 Git commit）、`policy_version` |
| 协议 | `observation_profile`、`dsl_profile` |
| 实验组 | `seed`、`variant`、`repetitions`（至少 1） |
| 驱动配置 | `config` 对象，见下表 |

`policy_version` 当前为 `research.policy.v1`，`variant` 仅支持 `dsl_verification`。创建时状态强制为 `draft`；相同定义重复创建可复用，ID 相同但定义不同会冲突。

推荐 `config` 使用 `research.experiment_config.v2`：

| 字段 | 约束 |
|---|---|
| `schema_version` | `research.experiment_config.v2` |
| `acceptance_spec_id` | 非空，绑定版本化验收任务 |
| `acceptance_spec_sha256` | 64 位小写十六进制 SHA |
| `request_timeout_seconds` | 1～3600 |
| `run_timeout_seconds` | 60～3600 |
| `cancel_grace_seconds` | 0～300 |
| `warmup_repetitions` | 非负 |
| `clean_context` | 必须为 `true` |
| `schedule_version` | `research.schedule.v1` |

仍兼容 v1 config，但它不能带 acceptance spec 绑定字段。创建实验不要提交模型密钥；这里记录控制变量，不提供秘密配置管理。

列表默认 `limit=100`、`offset=0`；非法/非正 limit 或负 offset 返回 `400`。Research 请求体采用严格 JSON 解码，拒绝未知字段和多个顶层 JSON 值。

### 9.2 测量运行、关联链与验收（8 个）

| 方法 | 完整路径 | 用途 | 请求关键字段 | 成功响应 |
|---|---|---|---|---|
| GET | `/api/v2/research/runs/{run_id}` | 查看一次实验运行及 metrics/links | query `project_id` 必填 | `200`，ResearchRun |
| POST | `/api/v2/research/runs/{run_id}/start` | 标记开始测量并计算截止时间 | `{"project_id":1}` | `200`，`{run,deadline}` |
| PUT | `/api/v2/research/runs/{run_id}/links` | 关联 Agent、DSL、Batch、Execution 证据链 | `{project_id,links}` | `200`，ResearchRun |
| PUT | `/api/v2/research/runs/{run_id}/oracle` | 保存独立结果验收裁决 | `{project_id,execution_id,decision}` | `200`，OracleResult |
| GET | `/api/v2/research/runs/{run_id}/oracle` | 查询独立验收结果 | query `project_id` 必填 | `200`，OracleResult |
| POST | `/api/v2/research/runs/{run_id}/project-metrics` | 从持久化事实计算并写回运行指标 | `{"project_id":1}` | `200`，带 metrics 的 ResearchRun |
| POST | `/api/v2/research/runs/{run_id}/finish` | 完成本次测量，检查完成条件 | `{project_id,status}`；status 为 `completed/failed` | `200`，ResearchRun |
| POST | `/api/v2/research/runs/{run_id}/cancel` | 将实验运行登记为取消 | `{"project_id":1}` | `200`，ResearchRun |

links 支持 `agent_run_id`、`generation_id`、`batch_id`、`execution_id`、`dsl_sha256`。必须构成有序前缀：

```text
agent_run_id -> generation_id -> batch_id -> execution_id
                         |
                         +-> dsl_sha256
```

不能只提交 execution ID 而缺少前面的链路，也不能把别的任务的结果冒充当前结果。

Oracle `decision` 关键字段：`schema_version`、`id`、`evaluator`、`passed`、`reason_code`、`decision_facts`、`sources`、可选的 `content_sha256`。事实包括 `name`、`passed`、`actual`、`expected`、`sources`。来源需满足独立证据约束；同一结果相同内容可重放，不同裁决不能无条件覆盖。

ResearchRun 包含 `id`、`experiment_id`、`project_id`、`repetition_index`、`warmup`、`status`、`versions`、`links`、可选 metrics 和时间。指标包括 task/execution/verification success、grounding accuracy、invalid action rate、recovery rate、steps、retries、LLM calls、tokens、latency、vision calls；每项使用 `{value, unavailable_reason}`，缺失数据不直接伪装为 0。

特别说明：

- `project-metrics` 的 project 是“投影/计算”动词，计算的是本次 ResearchRun 指标，不是项目报告中心统计。
- experiment/start 生成 schedule，run/start 更新测量状态；后续创建会话、发起 Agent、审批及验收由 Driver 编排。
- Research cancel 只改变研究记录状态，不会级联调用 Agent cancel 或 Batch cancel。
- `completed` 是实验流程完成状态，不可当作业务任务通过；通过与否看 Oracle 和 metrics。
- Experiment 状态：`draft/active/completed/cancelled`；ResearchRun 状态：`pending/running/completed/failed/cancelled`。
- 当前没有公开的实验删除、实验 cancel、trajectory 下载 HTTP 路由；离线导出代码不等于可访问 URL。

源码：[路由](../backend-go/internal/transport/http/research.go)、[类型](../backend-go/internal/research/types.go)、[调度与完成约束](../backend-go/internal/research/service.go)、[Oracle 合同](../backend-go/internal/research/oracle.go)、[验收规格 Schema](../research/schemas/agentic-e2e-acceptance.v1.schema.json)。

## 10. Browser Worker（5 个自定义 HTTP 路由）

该服务是浏览器能力提供方，不是第二套平台后端；直接调用不会自动生成 Go 官方执行记录。

| 方法 | 完整路径 | 用途 | 请求 | 成功响应 |
|---|---|---|---|---|
| GET | `/` | 查询 Worker 元信息 | 无 | `200`，`{name,environment,docs_url}` |
| GET | `/api/v1/health` | Worker 健康检查 | 无 | `200`，`{status,service,environment,version}` |
| GET | `/artifacts/{artifact_path:path}` | 下载截图、DOM、Observation 等证据文件 | 相对 artifact 路径 | `200`，文件内容 |
| POST | `/api/v1/internal/browser-capabilities/{capability}` | 调用探索/校验能力 | BrowserCapabilityRequest | `200`，`{"result": ...}` |
| POST | `/api/v1/internal/browser-executions` | 同步执行已结构化的 DSL | BrowserExecutionRequest | `200`，执行结果对象 |

Go 另有一个独立探针：

| 方法 | 完整路径 | 用途 | 请求 | 成功响应 |
|---|---|---|---|---|
| GET | `/health` | Go 服务健康检查 | 无 | `200`，`{"status":"ok"}` |

两种健康检查都不是完整的数据库、模型和真实浏览器可用性验收。

FastAPI 默认另有 `/docs`、`/redoc`、`/openapi.json`、`/docs/oauth2-redirect`。这些只描述 Worker，不包含 Go 的 51 个接口；生产 Nginx 不将这些地址代理到 Worker。

### 10.1 通用能力请求与三种 capability

```json
{
  "actor_user_id": 1,
  "project_id": 1,
  "conversation_id": "1",
  "context": {
    "clean_context": true
  },
  "arguments": {
    "url": "https://example.com",
    "observation_schema_version": "v2"
  }
}
```

`actor_user_id`、`project_id` 为正整数，`conversation_id` 长度 1～100；这里的归属参数由 Go 提供，不是用户登录凭据。`context` 可包含 `clean_context`（默认 false）、`entry_url_or_page`。

| capability | 用来做什么 | arguments | result 关键字段 |
|---|---|---|---|
| `explore_page` | 打开一个已知 URL，采集页面元素和状态证据 | `url` 必填，`core_user_flow_text?`、`observation_schema_version?` | `url`、`element_count`、`observation_v2`、`context_evidence`、可选 warning |
| `explore_flow` | 在一次隔离探测中执行有限动作，采集多个页面状态 | `steps` 非空；`base_url?`、`flow_description?`、`observation_schema_version?` | `pages`、`success`、`failures`、`total_pages`、`total_elements`、`context_evidence` |
| `validate_page_elements` | 检查需求元素覆盖或 DSL 的定位/绑定结构 | 下述互斥模式 | `valid` 与校验明细，DSL 模式另含校验后的 `dsl_case` |

Worker 直接请求默认 observation version 为 `v1`；Go Agent 探索工具默认显式传 `v2`。v2 的原始 `a11y_nodes` 被清空/在模型侧移除，主要读取 `observation_v2`，不能据此误判没有元素。

explore_flow 的 `steps[]` 支持 `url?`、`description?`、`actions[]`。每个 action：

- `action` 仅 `click/input/wait_for`，可带 `plan_step_id`。
- `target` 字符串与结构化 `locator` 必须二选一。
- 结构化 locator 仅用于 `wait_for`，kind 仅 `role/label/placeholder/text`。
- `condition` 仅用于 `wait_for`，支持 `visible` 或 `value_equals`；后者要求结构化 locator 和字符串 `expected`。
- 可带 `value`、`timeout_ms`（1～60000）。
- 每次 flow 的浏览器探测上下文隔离，不应依赖上次 flow 的浏览器状态。但浏览器隔离不等于撤销目标网站的服务端副作用，仍需 Go 计划/策略控制。

validate_page_elements 模式：

1. 需求覆盖：同时传 `required_elements` 与 `a11y_nodes`。要求项含 `id`、`description`、非空 `keywords`，可选 `roles`；返回 checks、missing/ambiguous IDs 和 recommended_action。`valid=true` 只代表没有缺失，不代表不存在歧义或获得执行授权。
2. DSL preflight：同时传 `dsl_case` 与 `a11y_nodes_by_state`，返回 `validation_mode=dsl_case`、digest、confidence、warnings。
3. research-v2：提交可执行的 `dsl_case`，可不带 A11y 数组；检查 TargetBinding/DSL 结构并返回 `validation_mode=target_binding`。不是用户审批接口，也不是保证当前网页仍然匹配的实时执行结果。

### 10.2 无状态执行 RPC

```json
{
  "execution_id": 100,
  "dsl_case": {
    "name": "Example smoke",
    "base_url": "https://example.com",
    "input_contract": [],
    "output_contract": [],
    "steps": [
      {"action": "goto", "value": "/"},
      {"action": "assert_url_contains", "value": "example.com"}
    ]
  },
  "input_values": {}
}
```

请求的 `execution_id` 为正整数，`dsl_case` 必填，`base_url` 可选，`input_values` 默认空字符串映射。示例 ID 仅说明格式，正式调用使用 Go 已创建的真实 Execution ID。

响应包含 `status`、`error_message`、`report`、`failure_signal`。HTTP 200 也可能携带 `status=failed`。RPC 负责执行和采集，不直接写平台业务表。

### 10.3 证据与中间件

- artifact_path 为 artifacts 根目录下的相对路径，可包含多级子目录；不存在或越出根目录都返回 404。
- 当前文件访问没有登录保护，仅做目录范围检查；证据内容可能含页面敏感数据。
- Worker 有按 IP 的进程内限流。
- Worker 的 `Idempotency-Key` 是进程内 POST 响应缓存，默认 TTL 1 小时，不是 PostgreSQL 队列幂等，也不能保证并发请求只执行一次；不同请求不要复用 key。

源码：[路由装配](../browser-worker/src/browser_worker/server/router.py)、[应用入口](../browser-worker/src/browser_worker/server/main.py)、[能力请求合同](../browser-worker/src/browser_worker/contracts/browser_capabilities.py)、[执行合同](../browser-worker/src/browser_worker/contracts/browser_executions.py)、[能力实现](../browser-worker/src/browser_worker/capabilities/browser_capabilities.py)。

## 11. Agent 工具（9 个，不是 HTTP 接口）

模型提出 tool call，Harness 注入 run/actor/project/conversation 上下文并执行策略校验。前端通过 Run 和事件接口观察工具，不直接 POST 到工具名。

| 工具名 | 用途 | 模型输入关键字段 | 输出/限制 |
|---|---|---|---|
| `set_task_plan` | 持久化版本化计划，固定动作顺序、次数与副作用边界 | `goal`、`max_side_effect`、`forbidden_actions`、`steps` | 返回 plan ID/version/SHA/binding/steps；goal 最终采用 Run 原始输入 |
| `ask_user_question` | 请求缺失信息或 DSL 执行审批 | `questions`，1～3 项 | 挂起为 user_input，通过 resume 接口回答 |
| `explore_page` | 为待处理计划步骤采集单页证据 | `url`、`plan_step_ids`，可选 flow text、observation version | Observation 与预算/grounding 摘要；不能无限重复探测 |
| `explore_flow` | 通过有限动作采集多个页面状态并 grounding | `steps`、`plan_step_ids`，可选 base URL 等 | 隔离探测结果；只允许计划授权的动作 |
| `validate_page_elements` | 建议性检查需求元素是否覆盖 | `required_elements`、`a11y_nodes` | 不授予 DSL 生成/执行权限 |
| `generate_dsl` | 校验、编译并保存绑定当前计划的 DSL 草案 | `plan_binding`、`case`，可选 `a11y_nodes_by_state` | generation ID 与产物；research-v2 locator 由已持久化证据编译 |
| `execute_dsl` | 保存已批准版本并入正式队列 | `generation_id`、`input_values?` | case/batch 信息；必须匹配该 Run 的用户审批 |
| `get_report` | 获取批次事实报告并可等待终态 | `batch_id`、`wait_for_terminal?` | 默认等待，约每秒读取，最长约 10 分钟或受上下文截止限制 |
| `fix_and_retry` | 基于失败事实准备透明修复计划 | `batch_id` | 返回策略与重放边界，不自动绕过探索、生成及再次审批 |

计划步骤至少声明 `id`、`intent`、`action`、`expected_occurrences`、`idempotency`、`side_effect`、`preconditions`、`completion_conditions`。`idempotency` 为 `idempotent/non_idempotent`，副作用等级为 `none/browser_state/external_state/unknown`。

DSL 的 `plan_binding` 为 `{plan_id,version,sha256}`。research-v2 步骤以 `plan_step_id` 和需要时的 `target_binding_id` 引用已验证候选，模型不能自行编造 selector/candidates/confidence。计划改版会使旧 DSL 及审批失效。

三种浏览器工具中，Go 探索参数的顶层 `plan_step_ids` 属于 Harness/TaskPlan 约束，转发 Python 前会剥离；不要照搬模型工具参数作为 Worker arguments。

`generate_dsl` 名称中的“生成”包括模型构造草案和后端确定性校验/保存，不存在对应的公开 REST `/generate_dsl`。`fix_and_retry` 也不是直接再次执行失败动作。

源码：[正式工具注册](../backend-go/cmd/agentservice/main.go)、[计划工具](../backend-go/internal/tools/task_plan.go)、[提问工具](../backend-go/internal/tools/ask_user.go)、[浏览器工具](../backend-go/internal/tools/browser.go)、[DSL 工具 Schema](../backend-go/internal/tools/dsl.go)、[执行/报告/修复工具](../backend-go/internal/tools/execution.go)。

## 12. 典型调用链

### 12.1 AI 规划到正式报告

```text
创建 Planning Session
  -> POST agent/runs
  -> GET events/stream，持续保存 seq
  -> Agent: set_task_plan -> explore_page/explore_flow
  -> Agent: generate_dsl -> ask_user_question
  -> 用户审阅当前 DSL，POST tool-calls/{id}/resume
  -> Agent: execute_dsl -> ExecutionBatch/Job
  -> Go execution-worker -> Python browser-executions
  -> Go 写回 Execution/Report
  -> GET execution-batches/{id}/report
  -> GET executions/{id} -> /artifacts/...
```

元素建议性校验、再次探索和失败修复由实际任务决定，不是每个任务都必须调用每个工具。

### 12.2 已有普通用例的项目回归

```text
GET projects
  -> GET cases?project_id=...
  -> 选择 case_ids
  -> POST execution-batches
  -> GET execution-batches/{id} 或 /report，轮询
  -> GET executions/{execution_id}，查证据
```

长任务优先使用异步批次，而不是长时间占用单用例 `/execute` 请求。research profile 用例须遵循内部 canonical binding/审批路径。

### 12.3 定位失败排查

```text
GET executions/{id}
  -> 查看 failure_signal 与 report.steps 的候选/最终定位/截图
  -> POST corrections
  -> 通过适用的正式执行入口重新运行
  -> 对比新报告
```

保存 correction 不等于缺陷已修复；以重跑结果和证据为准。

### 12.4 Research 验收

```text
创建 experiment -> experiment/start -> 有序 schedule
  -> research run/start
  -> Driver 创建 Planning/AgentRun 并驱动审批、执行
  -> PUT links，绑定实际 Agent/Generation/Batch/Execution
  -> 独立 Oracle 核验任务结果，PUT oracle
  -> POST project-metrics
  -> POST finish
```

需要终止时分别处理 ResearchRun、AgentRun 与 ExecutionBatch，不能假定一个 cancel 自动停止三层。

## 13. 本地调用示例

以下命令是手工联调示例，本次文档整理没有执行写请求或付费模型调用。

### 13.1 启动服务

先配置本地 PostgreSQL 和必要环境变量，分别在对应终端执行：

```bash
# backend-go/：先迁移
go run ./cmd/migrate
```

```bash
# browser-worker/：启动 Python capability/execution API
uv run browser-worker-dev
```

```bash
# backend-go/：启动执行队列消费者
go run ./cmd/execution-worker --concurrency 2
```

```bash
# backend-go/：启动 Go HTTP/SSE
go run ./cmd/agentservice
```

只读检查：

```bash
curl -sS http://127.0.0.1:8081/health
curl -sS http://127.0.0.1:8000/api/v1/health
curl -sS http://127.0.0.1:8081/api/v2/projects
```

Worker 的在线接口文档：`http://127.0.0.1:8000/docs`。

已有 AgentRun 可通过 PostgreSQL 事件生成只读诊断摘要：

```bash
# backend-go/
go run ./cmd/pipeline-audit --run-id <agent-run-id>
```

输出包含计划版本、模型请求首末/峰值字节数、最新上下文组成、累计 token，
以及相同 state epoch 下的重复工具签名。旧 Run 没有
`agent.pipeline.trace.v1` 时对应计数为空，不会从不完整历史中猜测。

### 13.2 手工创建普通用例并异步执行

先创建项目，从响应获取真实 `id`：

```bash
curl -sS -X POST http://127.0.0.1:8081/api/v2/projects \
  -H 'Content-Type: application/json' \
  -d '{"name":"API smoke","description":"接口联调用例"}'
```

下面的 `1` 必须替换为上述 project ID：

```bash
curl -sS -X POST http://127.0.0.1:8081/api/v2/cases \
  -H 'Content-Type: application/json' \
  -d '{
    "project_id":1,
    "name":"Example smoke",
    "base_url":"https://example.com",
    "input_contract":[],
    "output_contract":[],
    "steps":[
      {"action":"goto","value":"/"},
      {"action":"assert_url_contains","value":"example.com"}
    ]
  }'
```

将 `case_ids` 换成响应中实际 case ID，再创建批次；新一轮运行使用新的幂等 key：

```bash
curl -sS -X POST http://127.0.0.1:8081/api/v2/execution-batches \
  -H 'Content-Type: application/json' \
  -d '{"project_id":1,"case_ids":[1],"concurrency_limit":1,"input_values":{},"idempotency_key":"api-smoke-001"}'
```

读取返回 batch ID 对应报告：

```bash
curl -sS http://127.0.0.1:8081/api/v2/execution-batches/1/report
```

### 13.3 Agent 入口与事件恢复

创建会话，从响应 `session.id` 获取真实 ID：

```bash
curl -sS -X POST http://127.0.0.1:8081/api/v2/planning/sessions \
  -H 'Content-Type: application/json' \
  -d '{"project_id":1,"clean_context":true}'
```

下面的启动操作会调用配置的真实模型并可能产生费用：

```bash
curl -sS -X POST http://127.0.0.1:8081/api/v2/agent/runs \
  -H 'Content-Type: application/json' \
  -d '{"session_id":1,"message":"打开 https://example.com 并验证页面地址包含 example.com"}'
```

用响应中的 Run ID 订阅事件：

```bash
curl -N 'http://127.0.0.1:8081/api/v2/agent/runs/RUN_ID/events/stream?after_seq=0'
```

实际看到 DSL 审批问题并审阅当前版本后，使用它的 ToolCall ID：

```bash
curl -sS -X POST \
  http://127.0.0.1:8081/api/v2/agent/runs/RUN_ID/tool-calls/TOOL_CALL_ID/resume \
  -H 'Content-Type: application/json' \
  -d '{"answers":{"approve_dsl":true}}'
```

## 14. 当前没有的接口与维护方式

以下能力不能从历史文档或内部函数名推断为现有 HTTP 接口：

- 登录/登出/Token/角色管理。
- 独立 Suite CRUD；当前批量回归入口是 execution-batches。
- 公开的 TaskPlan CRUD、DSL generation 查询/审批 REST、通用 Tool 调用 API。
- 独立 Job HTTP CRUD、单个 Execution cancel。
- 修正记录列表/GET/DELETE。
- 公共 WebSocket 执行协议；当前 Agent 进度使用 SSE。
- Go Swagger/OpenAPI 端点；Python `/docs` 不能代表整个平台 API。

维护时优先核对：

| 来源 | 用途 |
|---|---|
| [Go 主路由](../backend-go/internal/transport/http/handler.go) | 平台接口、健康与 SSE 的注册事实 |
| [Research 路由](../backend-go/internal/transport/http/research.go) | 实验接口与严格请求解码 |
| [Python router](../browser-worker/src/browser_worker/server/router.py) / [main](../browser-worker/src/browser_worker/server/main.py) | Worker 前缀、内部路由及文件/元信息 |
| [共享能力合同](../contracts/browser-capabilities.v1.json) | 跨语言能力参数协议 |
| [Observation](../contracts/browser-observation.v2.schema.json) / [TargetBinding](../contracts/target-binding.v1.schema.json) / [LocatorSpec](../contracts/locator-spec.v1.schema.json) | 定位与浏览器证据结构 |
| [前端 API 类型](../frontend/src/types/api.ts) | 页面消费的数据视图；有差异时以后端实际响应为准 |
| [Nginx](../deploy/nginx.conf) / [Vite](../frontend/vite.config.ts) | 哪些接口可从前端入口访问 |

验证范围：本文按路由、请求类型、处理器与相关服务源码核对，并进行静态覆盖检查；不宣称全部接口已经真实数据库、浏览器或模型联调通过。当前已知问题和后续修复以 [Bug 日志](./bug-log.md) 为准。
