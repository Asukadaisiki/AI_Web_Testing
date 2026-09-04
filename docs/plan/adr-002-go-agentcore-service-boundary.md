# ADR-002：Go AgentCore 与 Python Runner 边界

日期：2026-09-04  
状态：accepted

## 背景

当前 Python 后端约 2.4 万行，复杂度主要集中在：

- `ai/test_planning_agent.py`：2620 行
- `runners/playwright_runner.py`：1277 行
- `ai/planning_tools.py`：1252 行
- `ai/page_explorer.py`：1168 行
- `services/dsl.py`：1115 行
- `ai/dsl_generator.py`：1020 行

Agent 循环、工具注册、数据库访问、页面探索、消息持久化和 SSE 事件存在交叉依赖。仅将现有文件逐个翻译为 Go 不会消除这些边界问题。

当前里程碑按本地单用户模式运行，不实现登录、Token 和角色鉴权。仍保留 `project_id`、`actor_id` 等归属字段，避免未来增加身份接口时重做数据模型。

## 决策建议

采用 Go 重建 Agent 控制面，保留 Python Playwright 执行面：

```text
React Frontend
    |
    | HTTP + SSE
    v
Go API / AgentCore (Hertz)
    |
    +-- Conversation / Agent Run
    +-- Tool Registry
    +-- DSL / Report / Execution application services
    +-- PostgreSQL state and queue
    |
    | PostgreSQL ExecutionJob
    v
Python Worker
    |
    +-- Playwright Runner
    +-- A11y exploration
    +-- locator and evidence capture
```

### 为什么不是只用 Kitex

Kitex 适合内部 RPC，不直接替代面向浏览器的 HTTP、SSE 和文件下载接口。

- 对前端：使用 Hertz 提供 REST/SSE。
- 单进程模块调用：使用普通 Go interface，不引入 RPC。
- 未来 AgentCore、Explorer 或 Worker 需要独立部署时：在真实进程边界使用 Kitex。
- 当前异步执行继续使用 PostgreSQL Batch/Job 队列，不为排队任务额外引入同步 RPC。

## Go 目录建议

```text
backend-go/
  cmd/
    api/
      main.go
    worker/
      main.go
  internal/
    agentcore/
      domain.go
      service.go
      repository.go
      events.go
    conversation/
      handler.go
      service.go
      repository.go
    tools/
      registry.go
      ask_user_question.go
      explore_page.go
      explore_flow.go
      validate_page_elements.go
      generate_dsl.go
      execute_dsl.go
      get_report.go
      fix_and_retry.go
    execution/
      handler.go
      service.go
      repository.go
    reporting/
      handler.go
      service.go
      repository.go
    transport/
      http/
        router.go
        sse.go
      rpc/
        kitex/
    platform/
      database/
      llm/
      queue/
      observability/
  api/
    openapi/
    thrift/
```

约束：

- `handler` 只做协议转换和错误映射。
- `service` 实现业务用例，不依赖 Hertz 或 Kitex 请求类型。
- `repository` 隔离 PostgreSQL。
- `tools` 只依赖 application service interface，不直接拼 SQL。
- AgentCore 只通过 Tool Registry 使用业务能力。
- Runner 只解释已校验 DSL。

## Kitex 服务边界

若后续拆进程，只暴露粗粒度 RPC：

```text
AgentCoreService
  StartRun
  ResumeToolCall
  GetRun
  CancelRun

BrowserCapabilityService
  ExplorePage
  ExploreFlow
  ValidatePageElements
```

不要把每一个数据库 CRUD 或 DSL step 暴露成 RPC。工具名是 Agent 能力合同，不等于一个 Kitex 微服务。

## 鉴权策略

当前阶段：

- 不实现登录页、Token 校验、角色和权限中间件。
- 使用单一 system actor 运行本地流程。
- API 请求不要求用户身份字段。

必须保留：

- `project_id`
- `created_by`/`actor_id` 可空或 system 值
- Artifact、Run、Case 的项目归属

未来增加鉴权时，只在 Hertz middleware 和 actor resolver 中接入身份，不改变 Agent、工具和 Runner 合同。

## 迁移策略

不进行一次性全量重写。

### 阶段 1：冻结合同

- 固化 DSL Schema、OpenAPI、Agent Event、Tool Call、Report 和数据库字段语义。
- 修复 BUG-099、BUG-100，避免把已知错误迁入 Go。

### 阶段 2：Go AgentCore

- Go 实现 Conversation、AgentRun、ToolCall、checkpoint 和 SSE。
- 工具先通过内部 HTTP 调用现有 FastAPI 能力。
- 前端切换到 Go API，但 Python Runner 保持不变。

### 阶段 3：迁移非浏览器领域服务

- 依次迁移 Case、DSL validation、Batch/Job、Report Core。
- 每迁移一个模块，保持 OpenAPI 和数据库兼容。

### 阶段 4：稳定执行边界

- Python 仅保留 Playwright、A11y exploration、locator 和 evidence。
- 通过 PostgreSQL Job 或明确的 Kitex/gRPC 边界与 Go 控制面通信。

## 不建议

- 不建议一次性把全部 Python 代码改写成 Go。
- 不建议仅为“模块化”引入多个 Kitex 微服务。
- 不建议当前重写 Playwright Runner；浏览器执行是已有能力中风险最高、验证成本最大的部分。
- 不建议认为鉴权只影响接口；可以延后鉴权行为，但应保留数据归属字段。

## 验收

1. AgentCore 不直接访问 ORM 或 Playwright。
2. Handler 不包含业务状态机。
3. 每个工具有稳定输入、输出和错误 Schema。
4. `ask_user_question` 可暂停并从 checkpoint 恢复。
5. 前端刷新后可按事件序号重放同一 Run。
6. Go 与 Python 对同一 DSL、执行状态和报告使用兼容合同。
