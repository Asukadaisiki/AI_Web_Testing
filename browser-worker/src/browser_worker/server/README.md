# Server

内部 HTTP 适配层。

只暴露：

- `/api/v1/health`
- `/api/v1/internal/browser-capabilities/*`
- `/api/v1/internal/browser-executions`
- `/artifacts/*`

项目、用例、执行、报告、Agent 事件和权限校验都属于 Go AgentService。
