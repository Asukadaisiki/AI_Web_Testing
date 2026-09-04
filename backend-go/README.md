# Go AgentCore

`backend-go/` 是 AI Web Testing 的新控制面。它负责：

- AgentRun、Step、ToolCall 和事件协议
- AgentCore 对话与工具循环
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

当前 Repository 为进程内实现，只用于合同和 API 骨架验证。进入正式联调前会替换为 PostgreSQL 实现。
