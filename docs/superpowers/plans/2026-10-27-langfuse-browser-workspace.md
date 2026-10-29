# Langfuse 任务入口与浏览器证据面板 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans. Implement tasks in order and track steps with checkboxes.

**Goal:** 在 Langfuse 创建、取消、重跑浏览器任务，并关联原生 Trace 与真实 Case 证据。

**Architecture:** 独立 fork 中新增 browser-runs feature，复用上游会话、权限、Page、Trace 和用量展示。BFF 调用现有 Go API，Go/PG 仍管理 Run；Exporter 负责异步原生观测映射。

**Tech Stack:** Langfuse v3.225.14、Node.js 24、pnpm 11.10.0、Next.js/tRPC/Zod；Go HTTP API 与既有 Langfuse ingestion client。

**Spec:** `docs/superpowers/specs/2026-10-27-thin-langfuse-browser-platform-design.md`

**Dependency:** 功能一 A1–A3 已通过；`LF/` 是 `/Users/bytedance/project/langfuse-agenttrace`。

## Global Constraints

- 固定 Langfuse v3.225.14；不修改其 Prisma schema 或观测存储架构。
- 单 Project / Conversation 配置绑定；不新增项目动态映射表。
- Run、Case 和最终状态只读写 Go API。
- UI 不获得 Go bridge token、模型 API key、Langfuse secret key。
- 创建/取消/重跑使用上游 playground:execute 权限，读取使用项目成员权限。
- 实时 UI 读 Go；原生 Langfuse Trace 在完成后异步出现，延迟期间不显示“执行失败”。

## Task B1：固定 fork、实现服务端 Go 客户端和项目权限边界

**Files**

- Create: `LF/web/src/features/browser-runs/contracts.ts`
- Create: `LF/web/src/features/browser-runs/server/config.ts`
- Create: `LF/web/src/features/browser-runs/server/agenttrace-client.ts`
- Create: `LF/web/src/features/browser-runs/server/router.ts`
- Create: `LF/web/src/__tests__/server/unit/browser-runs/agenttrace-client.servertest.ts`
- Create: `LF/web/src/__tests__/server/unit/browser-runs/router.servertest.ts`
- Modify: `LF/web/src/server/api/root.ts`
- Modify: `LF/web/src/env.mjs`
- Create: `backend/internal/api/v3/bridge_auth.go`
- Create: `backend/internal/api/v3/bridge_auth_test.go`
- Modify: `backend/internal/api/v3/server.go`, `backend/internal/config/config.go`
- Modify: `backend/cmd/agenttrace-api/main.go`
- Modify: `compose.yaml` 的 API port 为 `127.0.0.1:8080:8080`
- Create: `compose.langfuse-bridge.yaml`
- Create: `LF/docker-compose.agenttrace.yml`
- Modify: `integration/v3/canonical_scenario.py`, `pi_harness_scenario.py`, `failure_drills.py` 的请求 helper

**Interfaces**

```ts
type BridgeConfig = {
  apiUrl: string;
  bridgeToken: string;
  langfuseProjectId: string;
  agenttraceProjectId: string;
  conversationId: string;
};

type CreateRunInput = {
  projectId: string;
  url: string;
  goal: string;
  idempotencyKey: string;
};
```

Router 提供 `list`、`create`、`get`、`evaluation`、`cancel`、`rerun`、`authorizeEvents`。除 create/list 外接收 `{projectId,runId}`；rerun 增加 idempotencyKey。

- [ ] **Step 1：建立固定上游 checkout，并保留独立提交历史。**

```bash
git clone --branch v3.225.14 --single-branch https://github.com/langfuse/langfuse.git /Users/bytedance/project/langfuse-agenttrace
git -C /Users/bytedance/project/langfuse-agenttrace switch -c feat/browser-agent-workspace
git -C /Users/bytedance/project/langfuse-agenttrace rev-parse HEAD
```

最后结果须为 Spec 中的 commit。实施时使用 Node 24 与 pnpm 11.10.0 执行 `pnpm install --frozen-lockfile`。按上游 self-hosting 文档启动其必需服务和迁移；不运行含数据清空行为的 `dx`/`infra:dev:prune`。

新增 AgentTrace Compose override：

```yaml
services:
  api:
    networks: [default, bridge]
    environment:
      AGENTTRACE_BRIDGE_TOKEN: ${AGENTTRACE_BRIDGE_TOKEN}
  exporter:
    networks: [default, bridge]
    environment:
      AGENTTRACE_LANGFUSE_BASE_URL: http://langfuse-web:3000
      AGENTTRACE_EXPORT_PROJECT_ID: ${AGENTTRACE_PROJECT_ID}
networks:
  bridge:
    external: true
    name: agenttrace-bridge
```

新增 Langfuse Compose override：从同一固定源码构建 Web/Worker，避免浮动 `:3` 镜像；复用上游数据库配置。

```yaml
services:
  langfuse-web:
    image: langfuse-agenttrace-web:v3.225.14
    build:
      context: .
      dockerfile: web/Dockerfile
    networks: [default, bridge]
    environment:
      AGENTTRACE_API_URL: http://api:8080
      AGENTTRACE_BRIDGE_TOKEN: ${AGENTTRACE_BRIDGE_TOKEN}
      AGENTTRACE_LANGFUSE_PROJECT_ID: ${AGENTTRACE_LANGFUSE_PROJECT_ID}
      AGENTTRACE_PROJECT_ID: ${AGENTTRACE_PROJECT_ID}
      AGENTTRACE_CONVERSATION_ID: ${AGENTTRACE_CONVERSATION_ID}
  langfuse-worker:
    image: langfuse-agenttrace-worker:v3.225.14
    build:
      context: .
      dockerfile: worker/Dockerfile
  clickhouse:
    ports: !reset []
networks:
  bridge:
    external: true
    name: agenttrace-bridge
```

首次部署创建 bridge 网络；已存在时直接复用。两个 checkout 的 `.env` 记录同一 token 和绑定 ID，凭证不提交。

env.mjs 的五个 bridge 字段声明为服务端可选字段；config.ts 的 `readBridgeConfig(): BridgeConfig | null` 只在五项全部非空时返回配置。未配置时 router 返回 PRECONDITION_FAILED、扩展组件隐藏，原生 Langfuse 仍可启动。这样可以先启动原生服务并创建项目，再填入绑定 ID；不把初始化变成循环依赖。

```bash
docker network create agenttrace-bridge
docker compose -f compose.yaml -f compose.langfuse-bridge.yaml up -d --build
```

在 LF checkout：

```bash
docker compose -p langfuse-agenttrace -f docker-compose.yml -f docker-compose.agenttrace.yml up -d --build
```

先在 Langfuse 创建 Project/key；通过 Go 原有 POST projects、POST conversations 创建对应任务空间，再写入绑定 ID 并重启 Web/Exporter。不在运行一次任务时自动创建新 Conversation。

- [ ] **Step 2：先写客户端与权限回归。**
  - URL/goal 为空拒绝；URL 必须 HTTP(S)。
  - 所有 Go 请求仅发往服务器配置的 apiUrl，携带服务器 token。
  - create 同键重试不换键；409 映射 CONFLICT，404 映射 NOT_FOUND，服务错误映射 INTERNAL_SERVER_ERROR。
  - 非配置 Langfuse Project 拒绝；外部 Run 的 Project 不匹配时拒绝。
  - VIEWER 可读，缺少 playground:execute 时不能创建/取消/重跑。
  - Go token 开启时无 token/错误 token 为 401，health 仍可探测。
  - 场景请求 helper 从 `AGENTTRACE_BRIDGE_TOKEN` 注入 Authorization；测试 token 未配置时保持现有调用行为。

- [ ] **Step 3：将测试跑到具体缺方法/缺认证的失败，再实现固定客户端。**
  - 读取 Go `/agents`，取服务端允许的 browser-eval 模型。
  - 任务内容格式固定为 `Open {url}. {goal}`。
  - list 读取固定 Conversation；create 向同一 Conversation 的 /messages 提交。
  - get/evaluation/cancel/rerun 在代理前查 Run.project_id。
  - 不写第二套任务创建事务，不增加一个通用 HTTP proxy。

```ts
const content = `Open ${input.url}. ${input.goal}`;
const requestBody = {
  content,
  agent_id: agent.id,
  model: agent.model,
  idempotency_key: input.idempotencyKey,
};
```

```ts
import { TRPCError } from "@trpc/server";

export function assertRunProject(
  run: { project_id: string },
  expectedProjectId: string,
): void {
  if (run.project_id !== expectedProjectId) {
    throw new TRPCError({ code: "NOT_FOUND" });
  }
}
```

- [ ] **Step 4：在新 router 复用上游权限，不重写角色表。**

```ts
import { throwIfNoProjectAccess } from "@/src/features/rbac";

throwIfNoProjectAccess({
  session: ctx.session,
  projectId: input.projectId,
  scope: "playground:execute",
});
```

所有 procedure 基于上游 `protectedProjectProcedure`；额外验证 input.projectId 等于配置。root.ts 只注册 `browserRuns: browserRunsRouter`。

- [ ] **Step 5：复用 A 的共享 JSON fixture 做客户端合同测试。**
  - contracts.ts 验证 evaluation_version、顶层数组、nullable verdict/arguments 和 Case/ExecutionResult 字段。
  - Go 与 fork 都读取同一 fixture 的复制版本，并用 checksum 测试或脚本确保副本一致。

```bash
cd /Users/bytedance/project/langfuse-agenttrace
pnpm --filter web exec vitest run --project server-unit src/__tests__/server/unit/browser-runs
pnpm --filter web typecheck
```

该 tag 的 `web/vitest.config.mts` 明确将 server-unit 限定到 `src/__tests__/server/unit/**/*.servertest.{ts,tsx}`。上面的路径与文件名遵守该规则；零测试不得当作通过。

- [ ] **Step 6：AgentTrace 与 fork 分别提交本 Task。**
  - AgentTrace：`feat: add optional browser workspace bridge authentication`
  - Langfuse fork：`feat: add authorized AgentTrace browser run client`

## Task B2：对齐完成事件与 Langfuse 原生模型用量映射

**Files**

- Modify: `backend/internal/export/langfuse/exporter.go`
- Create: `backend/internal/export/langfuse/projection.go`
- Create: `backend/internal/export/langfuse/projection_test.go`
- Modify: `backend/internal/export/langfuse/client.go`, `client_test.go`, `exporter_test.go`
- Modify: `backend/internal/export/langfuse/postgres_integration_test.go`
- Modify: `backend/internal/config/config.go`, `config_test.go`
- Modify: `backend/cmd/agenttrace-exporter/main.go`
- Create: `integration/v3/thin_langfuse_scenario.py`
- Create: `integration/v3/test_thin_langfuse_scenario.py`

**Interfaces**

Consumes: 现有 Store.GetRun/GetConversation/ListTrace，新增复用 Store.GetTraceDetails。<br>
Produces: 相同 Trace ID、原生 Generation 的 input/output usage、model；Trace metadata 关联 run_id / retry_of_run_id。

- [ ] **Step 1：写完成事件与用量映射回归。**
  - harness.completed 触发导出；pi.message.completed 不触发。
  - runtime.completed 只作历史兼容。
  - 配置 export project 时，其他 Project 不导出。
  - 同一事件重投没有新增远端身份；取消/失败保留真实状态。
  - Usage 按 Span/Attempt 关联，不把整 Run Token 数重复写到每个 Generation。
  - 不存在实际价格数据时不伪造成本。

- [ ] **Step 2：运行新增回归，确认当前 harness.completed 被忽略及 usage 缺失。**

```bash
cd backend
go test ./internal/export/langfuse -count=1
```

- [ ] **Step 3：先完成最小事件修复和项目过滤。**

```go
func isCompletionEvent(eventType string) bool {
    return eventType == "harness.completed" || eventType == "runtime.completed"
}
```

保留已有 Inbox、DeliveryGate、RetryStore/DLQ，不创建新队列。项目过滤在获取 Run 后、读取和发送 Trace 前完成；不匹配的事件确认消费并记录原因。

- [ ] **Step 4：从 GetTraceDetails 构建真实映射。**
  - 继续使用 `remoteID("trace", run.ID)` 和现有 Span ID 映射。
  - `Observation` 增加 `Model string` 和 `UsageDetails map[string]int64`。
  - `client.go` 对 generation-create body 输出 `model` 和 `usageDetails`；普通 Span 不附加模型 usage。
  - Case/截图仍由 A 接口按需读取，不把所有截图 bytes 写入 ingestion。
  - Trace metadata 增加 `agenttrace_run_id`、`agenttrace_retry_of_run_id`；状态仍来自 PG。

```go
observation.UsageDetails = map[string]int64{
    "input": usageRecord.InputTokens,
    "output": usageRecord.OutputTokens,
    "total": usageRecord.TotalTokens,
}
```

使用 Run.specification.command.model.name 填 model；现有 usage provider 的字面值 `"model"` 不是模型名。

- [ ] **Step 5：执行固定 Langfuse tag 的真实 ingestion smoke。**
  - 用该 Project 的 key 调现有 `/api/public/ingestion`。
  - 通过 Langfuse Public API 读取对应 Trace，断言 run_id metadata、Generation model/usage。
  - 原生 Trace 出现有异步延迟，使用有截止时间的读取；截止后记录失败，不修改 Run。
  - 相同输入重投后按稳定 ID 校验记录数量/内容。

```bash
cd backend
go test ./internal/export/langfuse ./cmd/agenttrace-exporter -count=1
```

服务健康且项目 key 配置完成后，在根目录运行：

```bash
uv run --project worker --with 'psycopg[binary]>=3.2,<4' python integration/v3/thin_langfuse_scenario.py
```

- [ ] **Step 6：提交此 Task。** 建议：`feat: export Pi completion and generation usage to Langfuse`。

## Task B3：任务页、原生 Trace 扩展面板、SSE 与图片代理

**Files**

- Create: `LF/web/src/features/browser-runs/pages/BrowserRunsPage.tsx`
- Create: `LF/web/src/features/browser-runs/components/NewRunDialog.tsx`
- Create: `LF/web/src/features/browser-runs/components/RunActions.tsx`
- Create: `LF/web/src/features/browser-runs/components/RunList.tsx`
- Create: `LF/web/src/features/browser-runs/components/BrowserEvidencePanel.tsx`
- Create: `LF/web/src/features/browser-runs/components/AgentTracePanel.tsx`
- Create: `LF/web/src/features/browser-runs/hooks/useRunEvents.ts`
- Create: `LF/web/src/pages/project/[projectId]/browser-runs/index.tsx`
- Create: `LF/web/src/pages/api/agenttrace/events/[runId].ts`
- Create: `LF/web/src/pages/api/agenttrace/artifacts/[runId].ts`
- Modify: `LF/web/src/components/trace/TracePage.tsx`
- Create: `LF/web/src/features/browser-runs/components/RunActions.clienttest.tsx`
- Create: `LF/web/src/features/browser-runs/hooks/useRunEvents.clienttest.ts`
- Create: `LF/web/src/__tests__/e2e/browser-runs.spec.ts`

**Interfaces**

Consumes: B1 router、A evaluation API、B2 稳定 Trace 关联。<br>
Produces: Langfuse 内可操作的完整浏览器任务体验。

- [ ] **Step 1：先写用户行为和代理边界回归。**
  - 双击和网络重试使用同一 idempotencyKey；更改目标生成新键。
  - 只对非终态显示取消，只对 failed 显示重跑。
  - 选择 Run 后建立一个 SSE；切换 Run/卸载后关闭旧连接。
  - 公共共享 Trace 的非项目成员不加载浏览器扩展内容。
  - 图片路径必须出现在该 Run 的 artifact_paths 内，禁止任意文件代理。

- [ ] **Step 2：实现任务页，首版不修改复杂侧栏。**
  - 新 URL 使用上游 Page 和 UI 组件，任务列表来自固定 Conversation。
  - 表单提交固定 URL/goal → B1 create；UI 保存 draft 的幂等键。
  - 选择 Run 展示 Go 状态、证据和“查看 Langfuse Trace”链接。
  - export 尚未出现时显示“观测记录正在同步”，仍能查看 Go 证据。

```ts
const traceId = `agenttrace-trace-${run.id}`;
const traceUrl = `/project/${projectId}/traces/${encodeURIComponent(traceId)}`;
```

- [ ] **Step 3：原生 Trace 页只插入扩展组件。**
  - 从其已加载 Trace.metadata 中取 agenttrace_run_id。
  - AgentTracePanel 在组件内部检查绑定项目和项目成员；不修改上游 TraceDetailBody。
  - Panel 展示 Case、各次 dry-run、工具参数、repair 和截图；不复制上游模型 Trace 树。

```tsx
<AgentTracePanel projectId={projectIdForAccessCheck} runId={agenttraceRunId} />
```

在 contracts.ts 增加 `readAgentTraceRunId(metadata: unknown): string | null`。字符串先 JSON.parse；对象检查 agenttrace_run_id 为非空字符串；解析失败返回 null，不直接强转，也不加载扩展面板。

```ts
export function readAgentTraceRunId(metadata: unknown): string | null {
  try {
    const value = typeof metadata === "string" ? JSON.parse(metadata) : metadata;
    if (!value || typeof value !== "object" || Array.isArray(value)) return null;
    const id = (value as Record<string, unknown>).agenttrace_run_id;
    return typeof id === "string" && id.startsWith("run_") ? id : null;
  } catch {
    return null;
  }
}
```

- [ ] **Step 4：实现授权 SSE 代理。**
  - Next handler 通过 `createTRPCContext` 和 `appRouter.createCaller(context).browserRuns.authorizeEvents` 复用项目/Run 权限。
  - URL 为 `/api/agenttrace/events/{runId}?projectId={projectId}&after={lastSequence}`；projectId 不能省略，必须走配置与上游项目成员检查。
  - 把浏览器 Last-Event-ID 转发到 Go；初次重建连接可接受已验证的 after 查询参数。
  - Go fetch 使用服务端 Bearer token。
  - 代理保留 text/event-stream、no-cache，关闭 buffering；浏览器断开时 AbortController 取消上游读取。
  - hook 对全局持久 seq 去重，监听 `harness.completed`；随后刷新 Run/evaluation/list，而不是沿用旧 runtime.completed 名称。

```ts
let lastSequence = 0;
function acceptDurableEvent(event: MessageEvent): boolean {
  if (!event.lastEventId) return true;
  const sequence = Number(event.lastEventId);
  if (!Number.isSafeInteger(sequence) || sequence <= lastSequence) return false;
  lastSequence = sequence;
  return true;
}
```

lastSequence 按 runId 生命周期管理；新连接从最近 ID 恢复，不无限保存所有历史 ID。

- [ ] **Step 5：实现图片代理，并运行页面验收。**
  - 同一权限过程先确认 Run 项目，再读取 evaluation.artifact_paths。
  - URL 为 `/api/agenttrace/artifacts/{runId}?projectId={projectId}&path={encodedRelativePath}`；代理通过 B1 evaluation procedure 获取允许列表。
  - path 必须精确匹配允许列表；禁止把用户输入拼成任意 URL。
  - 只返回 Worker 已生成的截图，限制响应大小并取消断开的上游请求。
  - 使用 tag 的 client Vitest 和 E2E 配置，测试成功、失败、取消、重跑和共享 Trace 权限。

```bash
cd /Users/bytedance/project/langfuse-agenttrace
pnpm --filter web test-client
pnpm --filter web typecheck
pnpm --filter web build
pnpm --filter web test:e2e browser-runs.spec.ts
```

- [ ] **Step 6：提交此 Task。** 建议：`feat: add browser run workspace and Trace evidence panel`。

## 阶段 B 完成标准

用户在 Langfuse 完成任务操作，Go 的 Run 和证据保持权威；原生观测关联稳定，截图和 SSE 经同一项目边界授权。尚无 Redis 写入时仍允许 PG polling；完整实时体验在 C1 补齐。
