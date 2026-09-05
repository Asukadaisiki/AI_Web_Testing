# AgentCore 工具化闭环设计

日期：2026-09-04  
状态：proposed

## 目标

AgentCore 作为唯一的对话与认知编排入口：

```text
用户 <-> AgentCore <-> 工具 <-> 后端领域服务
```

- 用户提供目标、补充信息并批准高影响变更。
- AgentCore 理解上下文、规划任务、选择工具并解释结果。
- 工具暴露稳定、结构化、可审计的领域能力。
- 后端负责校验、持久化、幂等、队列、执行和证据，不参与开放式对话决策。
- Runner 只执行通过校验和授权的结构化 DSL。

不新增与 AgentCore 竞争决策权的独立业务编排器。后端保留执行控制面，用于约束和可靠地落实 AgentCore 的工具调用。
当前本地单用户阶段不实现登录、Token 和角色鉴权，仅保留数据归属字段以兼容后续接入。

## 交互原则

1. 首次消息创建一个 Run，后续所有事件携带 `conversationId`、`runId`、`stepId`、`toolCallId`、`parentId`、`checkpointId` 和 `traceId`。
2. 工具生命周期显式拆分为 `TOOL_CALL_START`、增量 `TOOL_CALL_ARGS`、`TOOL_CALL_RESULT` 和 `TOOL_CALL_END`。
3. 文本同样使用 `TEXT_MESSAGE_START`、`TEXT_MESSAGE_CONTENT` 和 `TEXT_MESSAGE_END` 增量输出。
4. `ask_user_question` 产生 `TOOL_CALL_PENDING`。用户回答通过第二次 `SendMsg` 回传同一个 `toolCallId`，结果包含 `userAnswer` 和 `nextStep`，Agent 从 checkpoint 继续。
5. 最终结果通过 `ARTIFACT` 发布，Run 通过 `DONE`/`RUN_FINISHED` 收口。
6. 多个无状态工具可以并行调用；有页面状态依赖的浏览器工具必须按同一 browser session 串行。
7. 本项目继续使用已有 SSE 事件持久化和重放，不增加轮询协议。

## 两类编排

### AgentCore：认知编排

AgentCore 决定：

- 信息是否足够，是否调用 `ask_user_question`
- 先探索单页还是沿流程探索
- 是否需要再次验证页面元素
- 何时生成 DSL
- 执行失败后是读取报告、修复 DSL、重新探索还是请求人工处理
- 如何向用户解释进度和结果

### 后端：执行治理

后端决定：

- URL、参数和 DSL 是否合法
- 工具是否允许在当前状态调用
- 是否超过并发、预算和重试上限
- 如何持久化 Run、Step、ToolCall、Artifact 和 Evidence
- 如何创建 Batch/Job、续租、取消和恢复任务
- 正式 DSL 是否已获批准

实际动作应满足：

```text
AgentCore 工具选择 ∩ 后端策略允许 ∩ 用户确认范围
```

## 工具集

### 1. `ask_user_question`

用于缺失信息或需要用户决策时暂停 Agent。

输入：

```json
{
  "questions": [
    {
      "id": "login_mode",
      "question": "本次使用哪种登录方式？",
      "type": "single_select",
      "options": [
        {"value": "account", "label": "账号密码"},
        {"value": "cookie", "label": "复用登录态"}
      ]
    }
  ]
}
```

输出：

```json
{
  "answers": {"login_mode": "account"},
  "next_step": "continue"
}
```

该工具必须支持 `pending -> resumed`，用户回答绑定原 `tool_call_id`，不创建新 Planning Session。

### 2. `explore_page`

输入一个已知 URL，采集单页 A11y Tree、可交互元素、页面状态和可继续探索的链接。

输出必须返回 `exploration_id`、`page_state_id`、规范化 URL、元素集合、候选链接和采集告警。

### 3. `explore_flow`

根据用户流程在同一浏览器会话中执行有限的导航、点击、输入和等待，采集多个页面状态。

输入应包含 `base_url`、受限动作列表、最大页面数和最大动作数。输出按页面状态组织，不直接生成 DSL。

### 4. `validate_page_elements`

原设想中的 `autherzation_page` 建议改名为 `validate_page_elements`，避免与身份授权混淆。

职责：

- 将用户流程中的动作和断言映射到已探索元素
- 输出每个目标的候选、唯一性、置信度和缺失原因
- 判断是否需要补充探索
- 为 DSL 生成提供经过验证的元素引用

它是探索与 DSL 生成之间的关键质量门。

### 5. `generate_dsl`

基于需求、流程、变量字典和已验证元素生成候选 DSL。

后端必须执行 Schema 校验、变量闭包检查、动作白名单检查和 locator preflight。输出是候选版本，不直接覆盖正式用例。

### 6. `execute_dsl`

接收已校验的 DSL 版本或已批准的 Case ID，创建 `ExecutionBatch -> ExecutionJob -> TestCaseRun`。

该工具应快速返回 `batch_id`/`job_id`，执行进度通过事件流发布，禁止让 Agent 请求线程直接运行 Playwright。

### 7. `get_report`

建议使用 `get_report` 而不是 `show_report`：

- 工具返回结构化 Run/Batch Report、FailureSignal、ExecutionAnalysis 和 artifact 引用。
- AgentCore 负责总结。
- 前端根据 artifact 类型渲染报告，不让 LLM 决定 UI 展示实现。

### 8. `fix_and_retry`

不应实现为不可见的黑盒工具。它可以作为复合工具，但必须展开为子步骤：

```text
get_report
-> 判断修复策略
-> 可选 explore_page/explore_flow
-> validate_page_elements
-> generate_dsl
-> 生成 DSL diff
-> ask_user_question（批准/拒绝）
-> execute_dsl
-> get_report
```

输出必须包含来源执行、旧/新 DSL 版本、变更原因、审批记录、重试次数和新执行 ID。

## 事件合同

建议统一为：

```json
{
  "seq": 42,
  "type": "tool.started",
  "conversation_id": "planning-session-id",
  "run_id": "agent-run-id",
  "step_id": "agent-step-id",
  "tool_call_id": "tool-call-id",
  "parent_id": "parent-event-id",
  "checkpoint_id": "checkpoint-id",
  "timestamp": "ISO-8601",
  "payload": {}
}
```

核心事件：

- `run.started`
- `message.started`
- `message.delta`
- `message.finished`
- `tool.started`
- `tool.args.delta`
- `tool.pending`
- `tool.result`
- `tool.finished`
- `artifact.published`
- `run.finished`
- `run.failed`

所有事件持久化后再推送 SSE；刷新页面按 `seq` 重放。

## 用户交互

前端只需要呈现三类信息：

1. Agent 文本：当前理解、下一步和最终结论。
2. 工具步骤：排队、运行、等待用户、成功或失败，可展开查看输入摘要和结果。
3. Artifact：探索快照、DSL Diff、执行进度和报告。

`ask_user_question` 是唯一通用暂停协议。审批 DSL、补充账号、选择测试范围都使用同一协议，通过问题类型和 payload 区分。

## 与当前项目的映射

可直接复用：

- `AIPlanningSession`、消息历史和 SSE EventLog
- `explore_page`、`explore_flow`
- A11y Tree 与 locator preflight
- DSL 生成、Schema 校验和 anti-pattern
- `ExecutionBatch`、`ExecutionJob`、Runner 和 Report Core
- FailureSignal 与 ExecutionAnalysis

需要补齐：

- 通用 AgentRun/Step/ToolCall 事件合同
- `ask_user_question` 的 pending/resume checkpoint
- `validate_page_elements` 独立工具合同
- `generate_dsl`、`execute_dsl`、`get_report` 的 Agent 工具封装
- 可展开且带审批门的 `fix_and_retry`
- BUG-099 的复测分析事实链
- BUG-100 的结构化归因一致性约束

## 实施顺序

1. 先统一 Agent 事件 envelope 和 `ask_user_question` 暂停/恢复。
2. 收敛工具注册表，只向 LLM 暴露稳定的用户任务工具。
3. 提取 `validate_page_elements`，形成探索到 DSL 的明确质量门。
4. 将 DSL 生成、队列执行和报告读取包装为 Agent 工具。
5. 最后实现透明的 `fix_and_retry` 复合流程，并接入 DSL Diff 审批。
