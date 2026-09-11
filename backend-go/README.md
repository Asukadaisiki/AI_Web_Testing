# Go AgentService

`backend-go/` 是 AI Web Testing 的新控制面。它负责：

- AgentRun、Step、ToolCall 和事件协议
- OpenAI 兼容的原生 tool calling AgentCore 循环
- `ask_user_question` 暂停和恢复
- HTTP/SSE API
- DSL、执行队列和报告的应用层编排
- Planning Session 元数据与项目关联
- 使用服务端固定 actor 维护数据归属字段，不启用登录鉴权

当前 Agent 工具：

- `ask_user_question`
- `explore_page`
- `explore_flow`
- `validate_page_elements`
- `generate_dsl`
- `execute_dsl`
- `get_report`
- `fix_and_retry`

Python Browser Worker 仅保留 Playwright、A11y、Locator、Evidence 和 locator preflight 能力；DSL、Case、Batch 与 Report 编排均在 Go 内完成。

## 目录

```text
cmd/agentservice/           Hertz 服务入口
cmd/pipeline-audit/         AgentRun 管线诊断汇总
internal/agent/             纯 Agent loop 与消息合同
internal/harness/           Prompt、工具和运行编排
internal/agentservice/      AgentRun、Checkpoint、事件与持久化
internal/config/            进程配置
internal/planning/          Planning Session 元数据控制面
internal/platform/          LLM 与 Python Worker 适配器
internal/tools/             工具合同与注册表
internal/transport/http/    HTTP 协议适配
```

## 本地运行

```bash
go test ./...
go run ./cmd/agentservice
go run ./cmd/pipeline-audit --run-id <agent-run-id>
```

默认监听 `127.0.0.1:8081`，可通过 `AGENTSERVICE_HTTP_ADDR` 修改。

当前可用接口：

- `GET /health`
- `POST /api/v2/agent/runs`
- `GET /api/v2/agent/runs/{run_id}`
- `GET /api/v2/agent/runs/{run_id}/events?after_seq={seq}`
- `GET /api/v2/agent/runs/{run_id}/events/stream?after_seq={seq}`
- `POST /api/v2/agent/runs/{run_id}/tool-calls/{tool_call_id}/resume`
- `POST/GET /api/v2/planning/sessions`
- `GET/PATCH/DELETE /api/v2/planning/sessions/{session_id}`
- `GET/POST/DELETE /api/v2/planning/sessions/{session_id}/projects...`

所有接口均无需登录。AgentService 使用 `DEFAULT_ACTOR_USER_ID`（默认 `1`）
写入 `actor_user_id` 并维持 Project membership 数据边界，为未来身份适配器保留字段，
但当前开发和生产环境都不校验 Cookie、Token 或角色。

模型配置复用 `browser-worker/.env` 中的：

- `AI_PLANNING_PROVIDER`
- `AI_PLANNING_BASE_URL`
- `AI_PLANNING_API_KEY`
- `AI_PLANNING_MODEL`
- `AI_PLANNING_THINK_MODE`：设为 `true` / `enabled` 时发送 `thinking: {"type":"enabled"}`
- `AI_PLANNING_REASONING_EFFORT`：可选，`low` / `high` / `max`，默认按 `max` 归一化

正式服务使用 PostgreSQL 保存 AgentRun、完整 transcript、pending tool/step 和事件流。事件序号通过 `agent_runs.last_event_seq` 在数据库中原子分配，保证同一 Run 内单调递增。
开启 thinking 后，Agent transcript 会保留 provider 返回的 `reasoning_content` 以满足 DeepSeek 多轮工具调用回传要求；`research.llm_call` 事件只记录 reasoning 的开关、effort、字节数和 SHA，不记录原始推理正文。
`agent.pipeline.trace` 事件记录模型请求体积及累计 usage，以及工具调用的
state epoch、规范化签名、状态、attempt 和 retry lineage；`pipeline-audit`
可从 PostgreSQL 事件离线汇总上下文增长、计划版本、重复调用和
PlanStep 到 probe/observation/element/candidate/binding/execution/report 的 lineage。

内存 Repository 仅用于快速单元测试。

正式 Agent 主链要求模型先调用 `set_task_plan`。TaskPlan/PlanStep 在 PostgreSQL
中按版本持久化，拥有目标语义、动作顺序和次数、禁止动作、幂等性及副作用边界。
探索工具只能为连续的待处理 PlanStep 补充 evidence，不能改写计划；DSL generation
必须绑定当前 plan ID、version 和 SHA。计划状态变化通过 `task_plan.updated` 事件发布。

创建 Run 返回 `202 Accepted` 后，Agent 在后台运行。客户端通过 SSE 订阅进度；断线或刷新后带 `after_seq` 或 `Last-Event-ID` 恢复，服务会先重放 PostgreSQL 事件，再推送实时事件。

`execute_dsl` 只接受当前 Run 已由用户批准的 generation ID。执行通过现有 PostgreSQL Batch/Job 队列交给 Python Worker；`get_report` 在 Go 后端等待 Batch 终态，避免 LLM 高频轮询。失败后 `fix_and_retry` 返回失败事实、源 DSL 和修复策略，Agent 仍需显式完成探索、验证、重新生成、用户审批和重执行。
