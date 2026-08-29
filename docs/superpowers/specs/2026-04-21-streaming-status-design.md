# 流式状态感知 & AI 超时修复设计

日期: 2026-04-21

## 概述

解决两个问题:
1. AI 响应超时 — `.env` 覆盖了代码默认的 600s 超时，导致频繁超时
2. 用户状态感知弱 — AI 对话和 DSL 生成阶段无流式反馈，用户不知道系统在做什么

## 问题1: 超时修复

### 现状

代码默认值已为 600000ms (10分钟)，但 `.env` 覆盖为更短值:

| 变量 | 当前值 | 目标值 |
|------|--------|--------|
| AI_DSL_TIMEOUT_MS | 120000 (2min) | 600000 (10min) |
| AI_VISUAL_TIMEOUT_MS | 10000 (10s) | 600000 (10min) |
| AI_PLANNING_TIMEOUT_MS | 60000 (1min) | 600000 (10min) |

### 修改

更新 `backend/.env` 三个超时值为 600000。无代码改动。

## 问题2: 流式状态感知

### 从 Claude Code 学到的核心模式

1. **一切皆流**: `query()` 是 `async function*` 生成器，API 响应、工具执行、状态更新全部通过 yield 立即推送
2. **Stream<T> 异步队列**: Producer-Consumer 模式，enqueue 立即交付或缓冲
3. **状态是派生的**: UI 状态从实际工作状态计算，不手动设置
4. **进度与结果分离**: progress 消息立即 yield，不等最终 results
5. **生成器组合**: 多个 generator 通过 `yield*` 链式委托

### 设计目标

- 一个 WebSocket 连接贯穿 "对话 → 生成DSL → 执行" 全流程
- AI 回复逐字流式显示在对话气泡中
- 每个阶段有明确的状态标签（正在思考/正在生成/正在执行）
- 状态从实际工作阶段派生，不是手动标记

### 架构

#### 核心模式: Python Generator + asyncio.Queue

参照 Claude Code 的 `async function*` + `Stream<T>` 模式，用 Python 的 generator + asyncio.Queue 实现等价机制：

```
LLM API (stream=True)
  → generator yield text_chunk
    → asyncio.Queue
      → WebSocket send_json
        → 前端 onEvent callback
          → React setState (追加文本)
```

#### WebSocket 消息协议扩展

**客户端 → 服务端 (新增):**

```json
{"type": "chat", "content": "帮我测试登录功能"}
{"type": "generate_drafts", "scenario_keys": ["login_success"]}
```

**服务端 → 客户端 (新增事件):**

```json
{"type": "status", "phase": "thinking", "message": "正在分析需求..."}
{"type": "text_chunk", "text": "好的，我来分析一下..."}
{"type": "tool_call_start", "tool": "explore_page", "params": {...}}
{"type": "tool_call_end", "tool": "explore_page", "result": "..."}
{"type": "draft_generating", "scenario_key": "login_success", "message": "生成DSL中..."}
{"type": "turn_complete", "session_status": "plan_ready", "payload": {...}}
```

**已有事件保持不变:** `save_progress`, `case_start`, `step_start`, `step_complete`, `done`, `cancelled`, `error`

#### 后端改动

**1. `test_planning_agent.py` — 流式 LLM 调用**

核心改造: 将 `_call_planning_llm` 改为 generator，使用 LLM API 的 `stream=True`。

```python
def _stream_planning_llm(...) -> Generator[dict, None, None]:
    """Yield events from streaming LLM API call.

    Yields:
        {"type": "text_chunk", "text": "..."} — each text chunk from API
        {"type": "raw_response", "text": "..."} — complete response at end
    """
    payload = {
        "model": model,
        "messages": messages,
        "response_format": {"type": "json_object"},
        "stream": True,  # 关键: 启用流式
    }
    # 使用 httpx 替代 urllib.request，支持 SSE 流式读取
    with httpx.stream("POST", endpoint, json=payload, ...) as response:
        full_text = []
        for line in response.iter_lines():
            if line.startswith("data: "):
                chunk = parse_sse_chunk(line)
                if chunk text:
                    full_text.append(chunk)
                    yield {"type": "text_chunk", "text": chunk}
        yield {"type": "raw_response", "text": "".join(full_text)}
```

`run_planning_turn` 改为 `stream_planning_turn` generator:

```python
def stream_planning_turn(...) -> Generator[dict, None, AIPlanningTurnResponse]:
    """Streaming ReAct planning turn.

    Yields status/text_chunk/tool_call events during processing.
    Returns AIPlanningTurnResponse via generator return value.
    """
    for round_index in range(max_rounds):
        yield {"type": "status", "phase": "thinking", "message": "正在分析需求..."}

        full_text = ""
        for event in _stream_planning_llm(...):
            if event["type"] == "text_chunk":
                yield {"type": "text_chunk", "text": event["text"]}
            elif event["type"] == "raw_response":
                full_text = event["text"]

        parsed = _parse_llm_response(full_text)
        # ... ReAct loop logic ...

        if action == "call_tool":
            yield {"type": "tool_call_start", "tool": tool_name, ...}
            result = execute_tool(...)
            yield {"type": "tool_call_end", "tool": tool_name, "result": ...}

    yield {"type": "turn_complete", "session_status": ..., "payload": ...}
    return response
```

**2. `ai_planning.py` — 流式消息服务**

新增 `stream_planning_message()` generator:

```python
def stream_planning_message(session, planning_session_id, *, actor_user_id, content):
    """Generator: save user msg → stream AI turn → save AI msg → yield events."""
    # 保存用户消息
    session.add(AIPlanningMessage(role="user", content=content, ...))
    session.flush()

    # 调用流式 planning turn 并转发事件
    stream = stream_planning_turn(...)
    response = None
    try:
        while True:
            event = next(stream)
            yield event  # 逐事件转发给 WebSocket
    except StopIteration as stop:
        response = stop.value

    # 保存 AI 回复消息
    session.add(AIPlanningMessage(role="assistant", content=response.assistant_message, ...))
    session.commit()
```

新增 `stream_generate_planning_drafts()` generator:

```python
def stream_generate_planning_drafts(session, planning_session_id, scenario_keys, *, actor_user_id):
    """Generator: for each scenario, yield status events during DSL generation."""
    for scenario_key in scenario_keys:
        yield {"type": "draft_generating", "scenario_key": scenario_key, "message": f"正在生成 {scenario_key} 的 DSL..."}
        # ... 生成 DSL ...
        yield {"type": "text_chunk", "text": f"✅ {scenario_key} 生成完成\n"}
    yield {"type": "turn_complete", ...}
```

**3. `ai_planning_streaming.py` — 扩展流式桥接**

复用现有的 asyncio.Queue + worker thread 模式，增加对 chat/generate_drafts 的支持:

```python
def stream_planning_chat(session_factory, planning_session_id, content, actor_user_id):
    """Async generator bridge: sync generator → async WebSocket events."""
    # 与现有 stream_save_and_execute 同构
    queue = asyncio.Queue()

    def _worker():
        with session_factory() as db:
            for event in stream_planning_message(db, planning_session_id, ...):
                asyncio.run_coroutine_threadsafe(queue.put(event), loop)

    thread = threading.Thread(target=_worker, daemon=True)
    thread.start()

    while True:
        event = await queue.get()
        yield event
        if event.get("type") in ("turn_complete", "error"):
            break
```

**4. `ai_planning.py` (路由) — WebSocket 扩展**

在 `ai_planning_session_ws` 的消息循环中增加处理:

```python
if msg_type == "chat":
    async for event in stream_planning_chat(session_factory, session_id, data.get("content", ""), current_user.id):
        await websocket.send_json(event)

elif msg_type == "generate_drafts":
    async for event in stream_planning_drafts(session_factory, session_id, data.get("scenario_keys", []), current_user.id):
        await websocket.send_json(event)
```

REST 端点 (`POST /messages`, `POST /drafts:generate`) 保留不动，供非 WS 客户端使用。

#### 前端改动

**1. `executionWebSocket.ts` — 事件类型扩展**

扩展 `ExecutionStreamEvent` union type，新增:

```typescript
type StatusStreamEvent = {
  type: "status";
  phase: "thinking" | "generating" | "tool_calling" | "executing";
  message: string;
};

type TextChunkStreamEvent = {
  type: "text_chunk";
  text: string;
};

type ToolCallStartStreamEvent = {
  type: "tool_call_start";
  tool: string;
  params?: Record<string, unknown>;
};

type ToolCallEndStreamEvent = {
  type: "tool_call_end";
  tool: string;
  result?: string;
};

type DraftGeneratingStreamEvent = {
  type: "draft_generating";
  scenario_key: string;
  message: string;
};

type TurnCompleteStreamEvent = {
  type: "turn_complete";
  session_status: string;
  payload: Record<string, unknown>;
};
```

**2. `AITestPlanningPanel.tsx` — 流式对话渲染**

参照 Claude Code 的状态驱动 UI 模式:

```typescript
// 状态从实际事件派生 (Claude Code 模式: status is derivative)
const [streamingState, setStreamingState] = useState<{
  phase: string | null;      // 从 status 事件派生
  isStreaming: boolean;       // 收到 text_chunk 即 true，turn_complete 即 false
  currentText: string;        // text_chunk 累积
}>({ phase: null, isStreaming: false, currentText: "" });

// 发送消息: 优先 WS，fallback REST
const sendMessage = async (content: string) => {
  if (wsClient) {
    // 先创建一个空的 AI 消息气泡
    const optimisticMsg = createOptimisticMessage(sessionId, "assistant", "streaming", "", null);
    setTranscript(prev => [...prev, optimisticMsg]);
    wsClient.send({ type: "chat", content });
  } else {
    // Fallback: REST API
    const resp = await sendPlanningMessage(sessionId, content);
    // ... 更新 transcript
  }
};

// 流式事件处理
const handleStreamEvent = (event: ExecutionStreamEvent, targetMsgId: string) => {
  setTransscript(prev => prev.map(msg => {
    if (msg.id !== targetMsgId) return msg;

    switch (event.type) {
      case "status":
        return { ...msg, structured_payload: { ...msg.structured_payload, _phase: event.phase, _phaseMessage: event.message } };
      case "text_chunk":
        return { ...msg, content: msg.content + event.text };
      case "turn_complete":
        return { ...msg, structured_payload: event.payload, content: event.payload.assistant_message || msg.content };
      default:
        return msg;
    }
  }));
};
```

渲染时在气泡顶部显示状态标签:

```tsx
{msg.structured_payload?._phase && msg.structured_payload?._phase !== "complete" && (
  <Tag color={phaseColors[msg.structured_payload._phase]}>
    {msg.structured_payload._phaseMessage}
  </Tag>
)}
{/* 打字光标: 流式进行中显示闪烁 | */}
{msg.structured_payload?._phase && msg.structured_payload._phase !== "complete" && (
  <span className="typing-cursor">▊</span>
)}
```

### 状态标签设计

状态从事件自动派生（Claude Code 模式: 不手动管理状态）:

| 触发事件 | phase | 标签文字 | 颜色 |
|----------|-------|----------|------|
| status(thinking) | thinking | 正在思考... | 蓝色 |
| tool_call_start | tool_calling | 正在调用工具: {tool} | 橙色 |
| status(generating) | generating | 正在生成测试方案... | 黄色 |
| draft_generating | draft_generating | 正在生成 DSL: {scenario} | 黄色 |
| case_start/step_start | executing | 正在执行... | 绿色 |
| turn_complete | (移除标签) | — | — |

### 完整数据流

```
用户输入 "帮我测试登录"
  → 前端 WS.send({type:"chat", content:"..."})
  → 前端创建空 AI 气泡 + 蓝色标签 "正在思考..."

后端 stream_planning_message()
  → yield {type:"status", phase:"thinking", message:"正在分析需求..."}
    → 前端: 气泡显示 "正在分析需求..."
  → yield {type:"text_chunk", text:"好的，我来分析一下"}
    → 前端: 气泡逐字显示 "好的，我来分析一下▊"
  → yield {type:"text_chunk", text:"你的测试需求..."}
    → 前端: 气泡追加 "你的测试需求...▊"
  → yield {type:"tool_call_start", tool:"explore_page"}
    → 前端: 标签变橙色 "正在调用工具: explore_page"
  → yield {type:"tool_call_end", tool:"explore_page"}
    → 前端: 标签恢复蓝色
  → yield {type:"status", phase:"generating", message:"正在生成测试方案..."}
    → 前端: 标签变黄色
  → yield {type:"text_chunk", text:"我为你设计了3个测试场景..."}
    → 前端: 继续追加文本
  → yield {type:"turn_complete", session_status:"plan_ready", payload:{...}}
    → 前端: 移除光标 + 标签，用完整 payload 更新 structured_payload
    → 前端: 刷新 session detail
```

### 降级策略

- WebSocket 断连时，前端自动 fallback 到 REST API（等待完整响应）
- REST 端点保留不动，确保 API 兼容性
- 流式 LLM 调用失败时，fallback 到非流式完整调用
- 前端检测: 如果发送 chat 后一定时间内没收到任何事件，自动切 REST

### 文件改动清单

| 文件 | 改动类型 | 说明 |
|------|----------|------|
| `backend/.env` | 修改 | 超时值改为 600000 |
| `backend/app/ai/test_planning_agent.py` | 重构 | 流式 LLM 调用 (httpx stream)，generator 版 ReAct 循环 |
| `backend/app/services/ai_planning.py` | 扩展 | 新增 stream_planning_message, stream_generate_planning_drafts generators |
| `backend/app/services/ai_planning_streaming.py` | 扩展 | 增加流式对话桥接 (复用 asyncio.Queue 模式) |
| `backend/app/api/routes/ai_planning.py` | 扩展 | WS 处理 chat 和 generate_drafts 消息 |
| `frontend/src/services/executionWebSocket.ts` | 扩展 | 新事件类型 |
| `frontend/src/components/AITestPlanningPanel.tsx` | 重构 | 流式渲染 + 状态标签 + 打字光标 |
| `frontend/src/types/api.ts` | 扩展 | 新增流式事件类型定义 |

### 测试

- 后端单元测试: `_stream_planning_llm` mock 测试，验证事件类型和顺序
- 后端集成测试: WebSocket 端到端测试，发送 chat 消息验证收到正确事件流
- 前端测试: 组件测试验证流式渲染和状态标签显示
