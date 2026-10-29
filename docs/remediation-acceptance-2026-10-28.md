# 架构整改与本地验收

日期：2026-10-28。范围：本机原生服务，真实 `openai-compatible / glm-5.3-flash`。

## 整改结果

- PostgreSQL 保持唯一执行事实来源；Run 完成统一为 `run.completed`。
- Kafka 分别投递执行命令、Redis 通知和 Langfuse 导出，消费者独立记录进度。
- Redis 仅提供过期实时通知；SSE 使用 PostgreSQL 序号回放。删除无消费方的租约和取消提示。
- API 使用显式公开模型配置，模型凭据留在 Runtime，缺少凭据不回退为 scripted。
- 通用 Host/Pi/Harness 协议与浏览器契约分离；完成结论由所选 Harness 提供。
- 首次证据写入失败定向取消并等待浏览器释放确认；不自动重放已经执行的外部动作。
- 模型输入、调用时间、token 与缓存用量可对账；未知费用和 SDK 无法区分的全零用量省略。
- 本地启动器接入真实 Langfuse、ClickHouse、MinIO，复用 Kafka 数据目录时保留集群身份。
- 联合测试发现并修正前端遗漏 `run.completed` 事件监听。
- 问答/审批继续执行时保留原始目标与用户补充，避免只把“approved”传给模型。

## 真实联合验收

完整记录：[acceptance-2026-10-28.json](acceptance-2026-10-28.json)。

成功 Run：`run_7ac8ce4b1593b2cb0d847d8eee12acdf`。
真实模型调用 4 次，耗时 6.469–17.175 秒，10 个 Span、4 条 Usage、
3 条操作证据、20 条持久事件。API 下载并验证 4 张 PNG。
Kafka 命令处理和 Outbox 均已完成，Redis 20 条通知且设置有效过期时间。
SSE 断线续传与 PostgreSQL 内容、顺序一致。

Langfuse Trace：
`agenttrace-trace-run_7ac8ce4b1593b2cb0d847d8eee12acdf`。
10 个 Observation；Generation 模型、输入及 token 与本地记录一致。
刻意失败与执行中取消也分别导出正确终态，三种终态均只产生一个持久完成事件。

## 故障验收

完整记录：[faults-2026-10-28.json](faults-2026-10-28.json)。

验证 scoped PostgreSQL 事件写入故障、Redis 不可用时 SSE 回放、
Kafka 暂停后 Outbox 留存并恢复投递、Langfuse HTTP 不可用时异步重试。
Worker 代理仅记录请求元数据，并以健康任务的实际执行请求证明代理已接入。
允许首次动作在故障被 Runtime 收到前已经发出；要求随后停止且无第二次操作。

## 验证范围与限制

TypeScript 251 项、Worker 98 项、React 55 项测试通过，Node/React 类型检查及构建通过。
Go 全包测试与本机依赖集成通过，Runtime/PostgreSQL/Langfuse 关键包 race 检查通过；
真实 Pi/MCP 共享宿主 race 集成通过。
本地启动器 4 项、验收适配器 7 项、Langfuse 生命周期 4 项离线检查通过。
Playwright 3 项真实前端测试全部通过：桌面 Trace、窄屏切换、问答与审批恢复，
最终运行耗时约 1.2 分钟。续跑修复后的 PostgreSQL/Runtime 相关 race 测试再次通过。

独立审查发现的 cleanup 确认、unknown usage 和审计代理接入校验问题已修正；
最后增量审查未发现其余 P1/P2 问题。验收发现的 SSE 监听与审批目标丢失问题
均增加回归测试并通过真实前端复测。

Langfuse 原生重启后再次验证真实 owner 登录、HTTP 207 入库、Trace/Span 查询及
MinIO 对象持久化。其 loopback 验收环境关闭 API 限流，并使用英文 locale/UTC；
上游本地化日期 Header 会被 Node 拒绝，原生环境配置规避该问题。
固定版本上游 Worker 清理后可能保留进程句柄，操作处理见 [operations.md](operations.md)。

此验收验证本机功能、边界和恢复行为；没有证明生产容量或多机故障容错。
共享宿主清理超时可能影响同进程的其他会话；已经发生的外部副作用需要人工对账。
当前 Pi 路径没有保存与恢复会话检查点。已发生执行的旧 Attempt 不会因为续跑或
消息重投递而自动重放；需要恢复这类外部执行时，先对账再创建新的 Run。
前端问答/审批验收使用开始执行前的等待状态，验证响应入库、调度和模型目标。
