# 项目待办

更新日期：2026-10-28

- [x] Kafka 独立 Live Notifier 写入 Redis 唤醒通知，SSE 按 PostgreSQL 序号回放。
- [x] 统一应用完成事件 `run.completed`，成功、失败、取消均进入异步 Langfuse 导出。
- [x] 模型模式显式配置，Runtime 持有凭据，默认真实 `glm-5.3-flash`。
- [x] 第一次证据写入失败立即定向取消并等待清理。
- [x] 公共 Harness 协议与浏览器扩展分开，Go 浏览器投影独立，完成 verdict 必填。
- [x] 删除无消费方的 Redis 租约、取消提示与未使用的单次进程 Runner。
- [x] 记录模型实际输入、调用时间和缓存 token，导出未知成本时不伪造零值。
- [x] 原生 Langfuse、ClickHouse、MinIO 已接入，提供本地运行入口。

- [x] 最终联合验收与独立代码审查；见 [整改验收报告](docs/remediation-acceptance-2026-10-28.md)。

- [x] Adapter + Strategy 接入 Pi、Claude Code、Codex 官方 SDK。
- [x] 独立 Harness CRUD：system prompt、skills、MCP，Run 保存不可变版本快照。
- [x] Runtime/Harness 选择和配置管理页面，支持更新、删除后的失败 Run 快照重跑。
- [x] Codex 可选 flat-functions provider 兼容；SDK、MCP 真实执行和用量保持原链路。
- [x] SDK/MCP 关闭确认、未确认时保留资源；Pi 直接关闭等待活跃 prompt。
- [x] 三 SDK 的 15 场景原生联合验收、独立 Langfuse 导出和最终 Pi 浏览器复测；见 [Runtime/Harness 验收报告](docs/runtime-harness-acceptance-2026-10-28.md)。
