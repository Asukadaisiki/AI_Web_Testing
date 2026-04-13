# WebSocket 执行流式推送设计

## 背景

BUG-045 剩余项：AI planning "保存并执行草案" 链路当前是同步阻塞的——用户点击后需等待所有用例执行完毕才能看到结果。如果执行 3 个用例各需 30 秒，用户会面对 90 秒的空白等待。

目标：在对话框内实时展示执行进度，包括用例级和步骤级两级事件推送，并支持中途取消。

## 方案选择

选择 WebSocket 而非 SSE，原因：
- 支持双向通信，客户端可发送 `cancel` 指令中途取消执行
- 一次连接可复用于多次执行请求
- 与未来需要双向通信的场景（如实时定位干预）方向一致

## WebSocket 消息协议

### 客户端 → 服务器

```json
{"type": "execute", "draft_ids": [1, 2, 3]}
{"type": "cancel"}
```

### 服务器 → 客户端

```jsonc
// 保存阶段
{"type": "save_progress", "saved_count": 2, "total": 3, "case_name": "登录测试"}
{"type": "save_complete", "saved_cases": [{"case_id": 42, "case_name": "登录测试"}]}

// 用例级事件
{"type": "case_start", "case_id": 42, "case_name": "登录测试", "total_steps": 5}
{"type": "case_complete", "case_id": 42, "case_name": "登录测试", "status": "passed", "passed_steps": 5, "total_steps": 5, "duration_ms": 3200, "execution_id": 15}

// 步骤级事件
{"type": "step_start", "case_id": 42, "step_index": 0, "action": "navigate", "target": "https://example.com/login"}
{"type": "step_complete", "case_id": 42, "step_index": 0, "action": "navigate", "status": "passed", "duration_ms": 1200}

// 最终摘要
{"type": "execution_summary", "message": "...", "structured_payload": {...}, "saved_cases": [...], "execution_summaries": [...]}

// 控制
{"type": "error", "message": "连接超时"}
{"type": "cancelled"}
{"type": "done"}
```

## 后端改动

### 1. 新增 WebSocket 路由

文件：`backend/app/api/routes/ai_planning.py`

- 端点：`WS /api/v1/ai-planning/sessions/{session_id}/ws`
- 认证：通过 query param `token` 传递（FastAPI WebSocket 不支持 header cookie 认证，改用首次消息或 query param），复用 `require_demo_user` 逻辑校验用户身份
- 接收 `execute` / `cancel` 指令
- 调用 `stream_save_and_execute()` 推送流式事件
- 维护 `CancellationManager` 字典追踪可取消的执行任务

### 2. 改造 Playwright Runner

文件：`backend/app/runners/playwright_runner.py`

将 `execute_case_with_playwright()` 改为生成器模式：

- 新增 `execute_case_with_playwright_streaming()` 生成器函数
- 每完成一步 yield `StepStreamEvent(action, step_index, status, duration_ms)`
- 保持原 `execute_case_with_playwright()` 不变（收集生成器全部输出，返回 list）
- 生成器在每步开始前检查 `cancelled` 事件，如已取消则抛出 `RunnerCancelledError`

### 3. 新增流式 Service

文件：`backend/app/services/ai_planning.py`

新增 `stream_save_and_execute()` 异步生成器：

```python
async def stream_save_and_execute(
    session: Session,
    planning_session_id: int,
    draft_ids: list[int],
    actor_user_id: int,
    cancel_event: threading.Event,
) -> AsyncGenerator[dict, None]:
    # 1. 保存阶段：逐个保存用例，yield save_progress
    # 2. 执行阶段：逐个执行用例
    #    - yield case_start
    #    - 消费 runner 生成器，yield step_start / step_complete
    #    - yield case_complete
    # 3. 持久化 execution_summary 到 AIPlanningMessage
    # 4. yield execution_summary + done
```

### 4. 取消机制

- 每个 WebSocket 连接维护一个 `threading.Event` 作为取消信号
- 客户端发送 `cancel` 时设置 event
- Runner 在每个步骤前检查 event，若已设置则停止执行
- 已完成的用例保留结果，未开始的用例跳过

## 前端改动

### 1. 新增 WebSocket 服务

文件：`frontend/src/services/executionWebSocket.ts`

```typescript
interface ExecutionStreamEvent {
  type: string;
  [key: string]: unknown;
}

function connectExecutionStream(
  sessionId: number,
  onEvent: (event: ExecutionStreamEvent) => void,
  onError: (error: Error) => void,
): { send: (msg: object) => void; close: () => void }
```

- 管理 WebSocket 连接生命周期
- 自动重连（3 次指数退避）
- 消息 JSON 解析和事件分发

### 2. 改造 AITestPlanningPanel

文件：`frontend/src/components/AITestPlanningPanel.tsx`

"保存并执行"按钮点击后：

1. 建立 WebSocket 连接
2. 发送 `{"type": "execute", "draft_ids": [...]}`
3. 实时接收事件，更新对话气泡内容：
   - `save_progress`：显示"正在保存用例..."
   - `case_start`：显示用例名和步骤总数
   - `step_start` / `step_complete`：在当前用例卡片内追加步骤行（图标 + action + status）
   - `case_complete`：标记用例完成状态（✅/❌）
   - `execution_summary`：替换临时内容为最终的执行摘要卡片（复用现有 `execution_summary` 渲染逻辑）
4. 收到 `done` 后关闭连接
5. 新增"取消执行"按钮，发送 `cancel` 指令

### 3. 前端消息状态

在对话 transcript 中新增临时消息类型 `execution_progress`：

```typescript
interface ExecutionProgressMessage {
  role: "assistant";
  content: string;
  structured_payload: {
    type: "execution_progress";
    cases: {
      case_id: number;
      case_name: string;
      status: "running" | "passed" | "failed" | "cancelled";
      steps: {
        action: string;
        status: "running" | "passed" | "failed";
        duration_ms?: number;
      }[];
    }[];
  };
}
```

前端用此结构维护实时进度，执行完成后替换为 `execution_summary` 类型消息。

## 端点策略

新增 WebSocket 端点作为主路径，原同步 `save-and-execute` 端点保留不变（供 API 直接调用、脚本等非 WS 场景使用）。前端"保存并执行"按钮切换为走 WS 端点。

## 认证方案

FastAPI WebSocket 不方便使用 cookie/session 认证。采用 query param 方案：

```
ws://localhost:8000/api/v1/ai-planning/sessions/{session_id}/ws?token=<session_token>
```

当前项目使用 `require_demo_user` 做 demo 认证。WebSocket 端点在连接时校验 token 参数与 demo 用户匹配。token 值可复用现有的 session cookie 值，或新增一个轻量 token 机制。

由于当前是 demo 模式（单用户），最简方案是：WebSocket 连接时在 query param 传 `user_id`，后端校验该用户存在即可。后续如需正式认证可替换为 JWT token。

## 错误处理

| 场景 | 处理 |
|------|------|
| WS 连接断开 | 前端自动重连（3 次退避）；重连后通过 GET session 详情恢复已完成的执行结果 |
| 执行中途取消 | 后端停止 runner，标记未完用例为 cancelled，推送 `cancelled` 事件 |
| 单步超时/失败 | 作为 `step_complete(status=failed)` 推送，不中断整个流（与当前行为一致） |
| 全部用例执行失败 | 仍然推送 `execution_summary` 和 `done`，摘要中体现失败状态 |

## 数据持久化

执行完成后，`stream_save_and_execute()` 负责将最终 execution_summary 持久化为 `AIPlanningMessage`（与当前 `save_and_execute_selected_drafts()` 逻辑一致）。

流式过程中的中间事件不持久化——它们是瞬态的 UI 状态。刷新页面后通过 GET session 详情恢复最终结果。

## 测试策略

### 后端

1. **WebSocket 端点测试**：使用 FastAPI TestClient 的 WebSocket 上下文管理器，验证 execute → 事件序列 → done 完整流程
2. **取消测试**：发送 execute 后立即发送 cancel，验证收到 `cancelled` 事件且未执行完的用例被跳过
3. **Runner 生成器测试**：验证 `execute_case_with_playwright_streaming()` 在每步完成后 yield 正确的 StepStreamEvent
4. **认证测试**：验证无 token 或错误 token 时 WS 连接被拒绝

### 前端

1. **WebSocket hook 测试**：验证连接、消息解析、重连、关闭行为
2. **面板集成测试**：验证"保存并执行"按钮触发 WS 连接、事件实时追加到对话、完成后替换为摘要卡片
3. **取消按钮测试**：验证点击取消后发送 cancel 指令、UI 更新为取消状态

## 改动范围总结

| 文件 | 改动类型 |
|------|----------|
| `backend/app/api/routes/ai_planning.py` | 新增 WS 端点 |
| `backend/app/runners/playwright_runner.py` | 新增流式生成器函数 |
| `backend/app/services/ai_planning.py` | 新增 `stream_save_and_execute()` |
| `frontend/src/services/executionWebSocket.ts` | 新建 WS 服务 |
| `frontend/src/components/AITestPlanningPanel.tsx` | 改造执行流程为 WS 驱动 |
| 后端测试文件 | 新增 WS / runner 流式 / 取消相关测试 |
| 前端测试文件 | 新增 WS hook / 面板集成测试 |
