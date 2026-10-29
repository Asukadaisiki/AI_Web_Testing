# Langfuse 浏览器 Agent 薄适配设计

**日期：** 2026-10-27<br>
**状态：** 用户已选定方向；本文及配套计划仅定义实施方案，业务代码尚未修改。

## 1. 目标与范围

基于 Langfuse 做一个可执行、可验证、可重放的浏览器 Agent 任务台。首版实现：

1. Agent 操作、失败反馈、修复与客观验收证据。
2. Langfuse 任务入口、运行操作与浏览器证据面板。
3. Go/Kafka 可靠执行、受控并发、Redis 实时通知与故障验收。

首版只绑定一个配置好的 Langfuse Project、一个 AgentTrace Project 和一个任务 Conversation。每次提交仍创建独立 Run，Pi 不共享任务上下文。这使现有消息提交与幂等接口可以直接复用。多项目动态绑定、批量评测、模型 A/B、浏览器池调度、自动扩缩容和 Pi 中途会话恢复不进入这三份计划。

## 2. 版本与仓库边界

- AgentTrace 代码留在当前仓库，保留 Go、TypeScript Harness、Python Worker。
- Langfuse 使用独立 checkout：`/Users/bytedance/project/langfuse-agenttrace`。下文 `LF/` 代表该 checkout 根目录，代码改动提交在其本地二开分支。
- 实施参考版本固定为 Langfuse `v3.225.14`，tag commit 为 `6dbf1d78e44b7d7dfc7e71afc20a6f29c8c2f202`。这是已查询的 v3 tag，不表示最新主版本。
- 该 tag 要求 Node.js `24`，声明 `pnpm@11.10.0`。Harness 继续使用 Pi SDK `1.4.0`，不升级其依赖作为本次工作的前提。
- 已核对该 tag 的 `web/src/server/api/trpc.ts`、`web/src/components/trace/TracePage.tsx`、Trace 页面路由、项目权限函数和 package scripts。
- Langfuse 使用自己的数据库与上游基础设施；Go 只管理 AgentTrace 数据库，避免在 Langfuse Prisma schema 中再建 Run/Case 表。
- 联合部署使用 Docker Compose `2.24.4+` 的端口重置语法。两个独立 Compose 项目通过 `agenttrace-bridge` 网络连接；Langfuse ClickHouse 不暴露宿主 8123，避免与现有 fixture-site 冲突。

## 3. 职责与调用关系

```text
Langfuse browser-runs 页面 / 原生 Trace 页扩展面板
    → browserRuns tRPC router（上游会话和项目权限）
    → Go HTTP API（Run / Trace / Evaluation）
    → PostgreSQL：Message + Run + Outbox
    → Kafka：run.commands.v1
    → Go Runtime：固定并发上限
    → Pi Browser Harness
    → Browser MCP / CaseBuilder
    → Python Playwright Worker

Pi JSONL → Go → PostgreSQL：事件、Span、用量、操作证据、最终状态
PostgreSQL Outbox → Kafka：run.events.v1
    ├─ API 内 Live Notifier → Redis → Go SSE → Langfuse SSE 代理
    └─ Langfuse Exporter → Langfuse 原生 Trace / Generation
```

Runtime、Notifier、Exporter 使用不同 Kafka consumer group。实时通知与 Langfuse 写入故障不改变已持久化的 Run 状态。

## 4. 不变量

- `loop.case.v1` 与 `pi-browser-harness.v1` 主版本保持不变，只增加可选字段。
- 成功判定保留现有规则：最后一次有效完整重放通过，且持久取消未覆盖成功。
- CaseBuilder 的步骤提交、FailureLedger、已成功步骤恢复、失败尾部移除逻辑继续复用。
- 完整 Case 从 `browser.case.completed` 的持久事件读取；不能把模型说明、StepView 或截图推断成 executable Case。
- `run_events.seq` 是 SSE 和展示顺序；`source_sequence` 只在对应 Harness Attempt 内解释。
- 修复标记来自实际执行的恢复逻辑。改变目标或工具参数的判断来自实际工具调用，不生成隐藏思维链。
- 本版展示任务目标、已公开助手说明、工具参数与页面反馈，不把这些标成完整模型请求上下文。
- 每个已成功的步骤仍有前后置条件；Run 状态和浏览器重放 verdict 分开展示。
- 不宣称浏览器外部副作用端到端 exactly-once。

## 5. 公共读取契约

新增 `GET /api/v1/runs/{id}/evaluation`，只从 PostgreSQL 生成：

```ts
type BrowserEvaluation = {
  evaluation_version: "agenttrace.browser-evaluation.v1";
  run_id: string;
  status: string;
  goal: string;
  tool_calls: ToolEvidence[];
  case_attempts: CaseEvidence[];
  latest_case_verdict: "passed" | "failed" | "error" | null;
  repair_count: number;
  artifact_paths: string[];
  warnings: string[];
};

type ToolEvidence = {
  id: string;
  harness_attempt_id: string;
  source_sequence: number;
  tool_call_id: string;
  name: string;
  arguments: Record<string, unknown> | null;
  result: Record<string, unknown> | null;
  prior_observation_id: string | null;
  current_observation_id: string | null;
};

type CaseEvidence = {
  id: string;
  harness_attempt_id: string;
  source_sequence: number;
  case: Record<string, unknown>;
  dry_run: Record<string, unknown>;
};
```

`case` 和 `dry_run` 的内容分别遵循既有 Case、ExecutionResult 契约。历史工具事件缺少 arguments 时返回 `null` 和 warning；没有完整 Case 时 verdict 为 `null`。格式损坏的 Case/ExecutionResult 返回服务端证据错误，不输出“通过”。

读取 Run、目标消息和事件采用同一个 `REPEATABLE READ READ ONLY` 事务。原始证据不改写，API 对公开副本复用既有字段脱敏规则。

## 6. 最薄 Langfuse 适配

- 新建 `features/browser-runs`，集中放合同、Go 客户端、tRPC router、页面和组件。
- 新增 `/project/[projectId]/browser-runs` 页面。
- 在已验证存在的 `components/trace/TracePage.tsx` 中插入一个扩展组件；只有当前项目成员且 Trace metadata 的 `agenttrace_run_id` 存在时才加载。
- 使用 `protectedProjectProcedure`；创建、取消、重跑复用上游 `playground:execute` 权限检查，读取复用项目成员检查。
- 原生 Trace 与 Run 的关联继续使用现有 Exporter 生成的 `agenttrace-trace-<run_id>`。不重命名历史 Trace ID。
- 任务入口先通过 Go `/agents` 获取服务端允许的模型，然后向固定 Conversation 的 `/messages` 提交。
- 绑定的 AgentTrace Project/Conversation 在部署初始化时通过现有 API 创建，ID 写入服务端配置；列表接口仍沿用 Conversation 内 Run 列表，不新增任务创建事务。
- 同一次用户操作保留同一个幂等键；更改 URL 或目标后才生成新键。
- 失败重跑创建新 Run，继续调用已有 `/reruns`，不篡改原 Run。
- SSE 和图片通过经过项目授权的 Next.js 服务端路由代理，浏览器不获得 Go bridge token 或 Langfuse secret key。

## 7. 配置

Langfuse 服务端新增：

```text
AGENTTRACE_API_URL
AGENTTRACE_BRIDGE_TOKEN
AGENTTRACE_LANGFUSE_PROJECT_ID
AGENTTRACE_PROJECT_ID
AGENTTRACE_CONVERSATION_ID
```

Go API 新增可选 `AGENTTRACE_BRIDGE_TOKEN`，对应 `config.Config.BridgeToken` 和 `api/v3.ServerConfig.BridgeToken`。启用时要求 Bearer token，健康检查除外；本地测试可不启用。Compose 的 API 宿主端口收敛到 `127.0.0.1`。BFF 只提供列出的固定操作，不接受任意上游 URL 或任意代理路径。

Exporter 新增可选 `AGENTTRACE_EXPORT_PROJECT_ID`，对应 `config.Exporter.ExportProjectID` 和 `langfuse.Exporter.ExportProjectID`：配置后只导出该 AgentTrace Project，必须与用于 Langfuse ingestion 的 Project API key 对应。首版不做动态凭证管理。

## 8. 最小可靠执行

- 初始执行并发使用 Compose 现有上限 `4`，Kafka 分区维持 `8`。
- Redis 流保留现有 24 小时 TTL、约 2000 条记录上限；租约保留 30 秒 TTL、约 15 秒续期，仅用于心跳信息。
- Kafka live notifier 根据 `event_id + run_id` 读回 PostgreSQL 的全局 seq，再发送 Redis 通知；不能拿 Kafka offset 或 Harness source_sequence 作 SSE ID。
- Redis Append 超时或失败只记录告警并确认这条可丢弃通知；PostgreSQL 读取失败则由 notifier 自己重试消费连接。
- Runtime 的消费错误增加可取消的监督重启；业务失败已持久化后仍确认命令，不作为基础设施失败反复执行。
- 启动时已有持久取消请求的 Run 不启动 Pi 子进程，完成取消记录。
- Runtime 中断后的执行是新 Harness Attempt；先前证据保留，不声称从原 Pi 内存会话续跑。
- 本版不重构 Inbox 长事务，只记录资源占用并限制并发；后续扩大容量时另行设计任务认领和失效 fencing。

## 9. 验收

1. 本地 fixture 的完整执行、业务失败、失败修复、最终重放都能读取真实证据。
2. Langfuse 创建任务、取消、重跑、查看 Case/截图/模型 Trace 可用；无权限访问被拒绝。
3. Redis 停机仍能读取持久进度，SSE 重连不会重复展示持久事件。
4. 重复提交同一幂等键返回同一 Run；同键不同内容冲突。
5. 已完成命令重投不增加 Attempt、Span 或操作结果。
6. 排队取消不启动 Pi；运行中取消后 Authoring Session 回到测试前基线。
7. 以当前 Pi 路径批量提交 12 个任务，执行峰值不超过 4；记录实际耗时、成功数、队列等待和资源使用，不沿用旧 Go Engine 压测报告。

## 10. 实施文件

- 总入口：`docs/superpowers/plans/2026-10-27-thin-langfuse-adaptation.md`
- 功能一：`docs/superpowers/plans/2026-10-27-agent-browser-evidence.md`
- 功能二：`docs/superpowers/plans/2026-10-27-langfuse-browser-workspace.md`
- 功能三：`docs/superpowers/plans/2026-10-27-reliable-browser-runs.md`

上游参考：[v3.225.14](https://github.com/langfuse/langfuse/tree/v3.225.14)、[项目权限过程](https://github.com/langfuse/langfuse/blob/v3.225.14/web/src/server/api/trpc.ts)、[TracePage](https://github.com/langfuse/langfuse/blob/v3.225.14/web/src/components/trace/TracePage.tsx)。
