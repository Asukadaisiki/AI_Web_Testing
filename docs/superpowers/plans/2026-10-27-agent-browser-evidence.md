# Agent 浏览器失败修复与客观验收 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans. Implement tasks in order and track steps with checkboxes.

**Goal:** 用真实持久证据呈现目标、工具参数、失败、修复与完整重放。

**Architecture:** 保留现有执行闭环，在工具事件中添加少量可选信息；Go 从同一事务读取持久事件并生成 evaluation.v1。先在现有工作台验收，再由 Langfuse 复用相同读取接口。

**Tech Stack:** Pi SDK 1.4.0、TypeScript/Vitest、Go/pgx、PostgreSQL、React、Python/Playwright。

**Spec:** `docs/superpowers/specs/2026-10-27-thin-langfuse-browser-platform-design.md`

## Global Constraints

- 保持 `loop.case.v1` 和 `pi-browser-harness.v1` 主版本。
- CaseBuilder/Replay/FailureLedger/Worker 的执行规则继续复用。
- 成功由实际完整重放和持久 Run 状态决定；公开说明不替代验收。
- 新字段可选；旧事件可读；不生成隐藏思维链或虚构模型输入。
- 所有最终执行证据来自 PostgreSQL；读取模型不写回原事件。

## Task A1：补齐工具参数、观测身份与实际修复标记

**Files**

- Modify: `pi-browser-harness/src/trace-events.ts`
- Modify: `pi-browser-harness/src/run-protocol.ts`
- Modify: `pi-browser-harness/src/browser/observation-view.ts`
- Modify: `pi-browser-harness/src/browser/case-builder.ts`
- Modify: `pi-browser-harness/src/browser/replay.ts`
- Modify: `pi-browser-harness/src/mcp/tool-result.ts`
- Modify: `backend/internal/piharness/protocol.go`
- Test: `pi-browser-harness/test/trace-events.test.ts`, `observation-view.test.ts`, `replay.test.ts`, `case-builder.test.ts`
- Test: `backend/internal/piharness/protocol_test.go`

**Interfaces**

Consumes: Pi 的 `tool_execution_start`、CaseBuilder 当前 Observation、已成功步骤与 Replay 执行结果。<br>
Produces: `pi.tool.started.payload.arguments?`、PageView 的观测身份、ToolResult 的可选 RepairInfo。

```ts
export interface RepairInfo {
  kind: "authoring_restore" | "dry_run_tail_removed";
  committed_prefix_steps: number;
  removed_steps: number;
  from_observation_id: string | null;
  to_observation_id: string | null;
}
```

- [ ] **Step 1：先写字段与语义回归。**
  - tool start 的实际 `args` 到 JSONL 后仍为对象。
  - 页面视图保留 observation_id/page_state_id。
  - 操作执行失败、成功恢复后才产生 `authoring_restore`。
  - dry-run 第 3 步失败时，保存前 2 步，移除第 3 步及后续步骤；计数按数组长度计算。
  - Grounding 歧义或恢复本身失败不标为“修复成功”。

```ts
import { expect, it } from "vitest";
import { buildPageView } from "../src/browser/observation-view.js";

it("preserves observation identity for evidence correlation", () => {
  const view = buildPageView({
    observation_id: "obs_current", page_state_id: "page_current",
    browser_session_id: "browser_current", url: "http://fixture.test/",
    title: "Fixture", elements: [], structures: [], blockers: [],
    action_candidates: [], truncated: false,
    truncation_reason: null, screenshot_path: null,
  }, "Verify quantity", null);
  expect(view.observation_id).toBe("obs_current");
  expect(view.page_state_id).toBe("page_current");
});
```

- [ ] **Step 2：验证新增回归先失败。**

```bash
cd pi-browser-harness
npm test -- --run test/trace-events.test.ts test/observation-view.test.ts test/replay.test.ts test/case-builder.test.ts
```

- [ ] **Step 3：添加最小字段，不修改 Pi SDK。**
  - `mapPiEvent` 在已有 tool start 分支复制 `event.args`；用 JSON 可序列化对象边界，拒绝非对象。
  - `run-protocol.ts` 中 arguments 为可选 Record，Go `ToolStartedPayload` 中为可选 RawMessage；存在时检查为 JSON object。
  - `buildPageView` 直接复制 Worker 已生成的两个 ID。
  - `restoreAfterFailedAction` 在 replay 成功后记录恢复前后 ID，removed_steps 为 0。
  - `prepareDryRunRepair` 在成功 replay 后记录 removed_steps，不能在 replay 前报告成功。
  - `encodeToolOutcome` 把 repair 传入 structuredContent，不把 JSON 塞入助手说明。

```ts
const priorCount = context.getSteps().length;
const fromId = context.getObservation()?.observation_id ?? null;
await replayCommitted(context, failedIndex, signal);
result.repair = {
  kind: "dry_run_tail_removed",
  committed_prefix_steps: failedIndex,
  removed_steps: priorCount - failedIndex,
  from_observation_id: fromId,
  to_observation_id: context.getObservation()?.observation_id ?? null,
};
```

- [ ] **Step 4：验证协议的有效/无效字段和旧事件。**
  - arguments 缺省可接受；null、数组、标量不接受。
  - 不改变 source_sequence 校验和已有 nested evidence 校验。

```bash
cd backend
go test ./internal/piharness -count=1
```

- [ ] **Step 5：运行 Harness 全套测试、typecheck/build，提交此 Task。**

```bash
cd pi-browser-harness
npm test -- --run
npm run typecheck
npm run build
```

提交建议：`feat: preserve browser tool arguments and repair evidence`。

## Task A2：新增一致的浏览器证据读取模型

**Files**

- Create: `backend/internal/evaluation/types.go`
- Create: `backend/internal/evaluation/project.go`
- Create: `backend/internal/evaluation/project_test.go`
- Create: `backend/internal/postgres/evaluation.go`
- Create: `backend/internal/postgres/evaluation_integration_test.go`
- Create: `fixtures/trace/browser-evaluation-v1.json`

**Interfaces**

Consumes: Run、源用户消息、按全局 seq 排序的持久 run_events。<br>
Produces: `evaluation.Project(Input) (Bundle, error)`、`Store.GetBrowserEvaluation(context.Context, string) (evaluation.Bundle, error)`。

```go
type Record struct {
    ID string
    Sequence int64
    HarnessAttemptID string
    SourceSequence int64
    Type string
    Payload json.RawMessage
}

type Input struct {
    Run domain.Run
    Goal string
    Events []Record
}
```

Bundle 的 JSON 字段与 Spec 第 5 节一致。CaseEvidence、ToolEvidence 保留所在 Attempt；不能用 source_sequence 在不同 Attempt 间关联。

- [ ] **Step 1：写纯投影回归及共享 JSON 合同 fixture。**
  - 包含失败工具、恢复标记、修改参数后的调用、失败 dry-run、最终通过 dry-run。
  - 两个 Attempt 中使用相同 tool_call_id，验证不会串联。
  - 缺少 arguments 的历史事件返回 null。
  - succeeded Run 没有 Case 时 verdict 为 null，不能伪造通过。

```go
func TestNoCaseDoesNotFabricatePassingVerdict(t *testing.T) {
    bundle, err := Project(Input{
        Run: domain.Run{ID: "run_fixture", Status: domain.RunSucceeded},
        Goal: "Verify cart quantity",
    })
    if err != nil { t.Fatal(err) }
    if bundle.LatestCaseVerdict != nil {
        t.Fatalf("unexpected verdict: %v", *bundle.LatestCaseVerdict)
    }
}
```

- [ ] **Step 2：运行 `go test ./internal/evaluation -count=1`，观察缺少类型/方法或错误 verdict 的失败。**

- [ ] **Step 3：实现纯投影，定义明确关联规则。**
  1. 外层解析为现有 `event.Envelope[json.RawMessage]`，再解析其 Payload。
  2. tool start 与 tool completed 用 `(harness_attempt_id, tool_call_id)` 配对。
  3. prior_observation 是同 Attempt 中该 start 之前最新 browser.observation。
  4. current_observation 优先使用该 tool_call_id 的 observation 事件。
  5. Case 从每条 browser.case.completed 收集；使用既有 Go Case/ExecutionResult 验证器验证。
  6. latest_case_verdict 按全局 seq 取最后一个有效完整执行，Run 状态原样保留。
  7. repair_count 只计 completed result 中实际存在的 RepairInfo。
  8. artifact_paths 只从持久观察/执行证据中的截图路径收集、去重；不接受客户端补入。

```go
func callKey(attemptID, callID string) string {
    return attemptID + "\x00" + callID
}
```

- [ ] **Step 4：实现 PostgreSQL 快照读取，先确认 Run 存在。**
  - 事务隔离使用 `pgx.RepeatableRead`、`pgx.ReadOnly`。
  - Run 与 source_message_id 对应的用户消息在同一事务中读取。
  - 事件查询如下；读取后的纯投影不再查询数据库。

```sql
SELECT id, seq, COALESCE(harness_attempt_id, ''),
       COALESCE(source_sequence, 0), event_type, payload
FROM run_events
WHERE run_id = $1
ORDER BY seq ASC;
```

- [ ] **Step 5：用隔离测试库验证中途读取、终态读取与跨 Attempt 关联。**

```bash
cd backend
go test ./internal/evaluation ./internal/postgres -run 'Test.*Evaluation|TestNoCase' -count=1
```

测试数据库必须使用现有 `testpostgres` 安全辅助，不指向业务默认库。

- [ ] **Step 6：提交此 Task。** 建议：`feat: project durable browser evaluation evidence`。

## Task A3：证据 API、现有工作台展示与真实修复 fixture

**Files**

- Create: `backend/internal/api/v3/evaluation.go`
- Create: `backend/internal/api/v3/evaluation_test.go`
- Modify: `backend/internal/api/v3/server.go`
- Modify: `backend/internal/api/v3/server_test.go` 中 Repository fake
- Modify: `web/src/api.ts`
- Create: `web/src/features/trace/BrowserEvaluation.tsx`
- Create: `web/src/features/trace/BrowserEvaluation.test.tsx`
- Modify: `web/src/features/trace/TracePane.tsx`
- Create: `worker/fixtures/site/repair.html`
- Modify: `pi-browser-harness/src/pi-session.ts` 中测试用 scripted provider 分支
- Create: `integration/v3/agent_repair_scenario.py`
- Create: `integration/v3/test_agent_repair_scenario.py`

**Interfaces**

Consumes: A2 `GetBrowserEvaluation`。<br>
Produces: `GET /api/v1/runs/{id}/evaluation`、`getBrowserEvaluation(runId)`、`BrowserEvaluation({evaluation})`。

- [ ] **Step 1：先写 API 和组件回归。**
  - 不存在的 Run 返回 404。
  - Case 内嵌 token/password 字段被既有脱敏规则移除；canonical JSON 不变。
  - cancelled Run 即使最新 dry-run passed，也显示“已取消”并保留浏览器通过事实。
  - 历史缺参数显示“未记录”，没有 Case 显示“尚无完整重放证据”。

```tsx
import { render, screen } from "@testing-library/react";
import { expect, it } from "vitest";
import { BrowserEvaluation } from "./BrowserEvaluation";

it("does not invent a passing case", () => {
  render(<BrowserEvaluation evaluation={{
    evaluation_version: "agenttrace.browser-evaluation.v1",
    run_id: "run_fixture", status: "succeeded", goal: "Verify cart",
    tool_calls: [], case_attempts: [], latest_case_verdict: null,
    repair_count: 0, artifact_paths: [], warnings: [],
  }} />);
  expect(screen.getByText("尚无完整重放证据")).toBeInTheDocument();
});
```

- [ ] **Step 2：运行新增 API/组件回归，观察缺路由和缺组件导致的失败。**

- [ ] **Step 3：接入只读路由和展示。**
  - Server Repository 增加 A2 方法；注册新的 GET。
  - 响应副本复用 `redactTraceJSON`，损坏证据返回服务端错误。
  - 面板只呈现目标、工具参数/result、repair、Case 与 dry_run；真实工具结果允许展开。
  - 截图引用 `artifact_paths`，不使用外部占位图。

```go
mux.HandleFunc("GET /api/v1/runs/{id}/evaluation", server.handleGetEvaluation)
```

- [ ] **Step 4：增加可重复修复 fixture 和 scripted 合同测试路径。**
  - repair.html 有同名 Submit 按钮、两个有标识的容器和结果区。
  - 一个错误目标会真实改变错误结果区，使后置条件失败；正确目标满足原始期望。
  - scripted 分支仅在明确的 fixture 目标标记 `AgentRepairFixture` 下使用：open_page → 错误目标 → 检查失败 → 正确 scope → 最终断言 → finish_case。
  - 用该分支验证协议与修复代码；报告必须标明 scripted，不能当作真实模型的自主修复结果。
  - 原 scripted 默认和 DoesNotExist 分支不变。

```html
<section aria-label="Wrong form"><button type="button">Submit</button></section>
<section aria-label="Expected form"><button type="button">Submit</button></section>
<p role="status" id="result">Pending</p>
```

页面事件处理用外部或页面内确定性代码分别写入 `Wrong target`、`Expected result`；不改验收预期。

- [ ] **Step 5：场景断言真实失败、恢复标记、参数变化与通过重放共同存在。**
  - 查询 PostgreSQL / evaluation API，不向数据库插入伪造“成功”。
  - 使用真实模型时把 model/provider、Harness 版本和结果另行记录；模型未修复就记录失败。

```bash
cd backend
go test ./internal/api/v3 -count=1
cd ../web
npm test -- --run
npm run build
```

服务健康后，在根目录运行：

```bash
uv run --project worker --with 'psycopg[binary]>=3.2,<4' python integration/v3/agent_repair_scenario.py
```

- [ ] **Step 6：更新场景单元测试，提交此 Task。** 建议：`feat: expose browser repair and replay evidence`。

## 阶段 A 完成标准

API 与现有工作台展示同一份真实证据；工具参数、失败、恢复与最终完整重放可追溯到 Attempt/事件。scripted 与真实模型证明分开记录，后续 Langfuse 只复用该契约。
