# Go AgentCore

`backend-go/` 是 AI Web Testing 的新控制面。它负责：

- AgentRun、Step、ToolCall 和事件协议
- OpenAI 兼容的原生 tool calling AgentCore 循环
- `ask_user_question` 暂停和恢复
- HTTP/SSE API
- DSL、执行队列和报告的应用层编排

Python `backend/` 在迁移期间继续提供现有 API，并长期保留 Playwright、A11y、Locator 和 Evidence 浏览器执行能力。

## 目录

```text
cmd/api/                    Hertz 服务入口
internal/agentcore/         AgentRun 状态与事件
internal/config/            进程配置
internal/tools/             工具合同与注册表
internal/transport/http/    HTTP 协议适配
```

## 本地运行

```bash
go test ./...
go run ./cmd/api
```

默认监听 `127.0.0.1:8081`，可通过 `AGENTCORE_HTTP_ADDR` 修改。

当前可用接口：

- `GET /health`
- `POST /api/v2/agent/runs`
- `GET /api/v2/agent/runs/{run_id}`
- `GET /api/v2/agent/runs/{run_id}/events?after_seq={seq}`
- `POST /api/v2/agent/runs/{run_id}/tool-calls/{tool_call_id}/resume`

模型配置复用 `backend/.env` 中的：

- `AI_PLANNING_BASE_URL`
- `AI_PLANNING_API_KEY`
- `AI_PLANNING_MODEL`

正式服务使用 PostgreSQL 保存 AgentRun、完整 transcript、pending tool/step 和事件流。事件序号通过 `agent_runs.last_event_seq` 在数据库中原子分配，保证同一 Run 内单调递增。

内存 Repository 仅用于快速单元测试。
