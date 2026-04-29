# Streaming Status And AI Timeout Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复本地 AI 超时配置，并把 AI Planning 的同一条 WebSocket 扩展为覆盖对话、草案生成、保存执行三段流式状态，让 Planning 面板能逐块显示 AI 文本、阶段标签和执行进度，同时保留 REST fallback。

**Architecture:** 复用现有 `WS /api/v1/ai-planning/sessions/{session_id}/ws` 与 `asyncio.Queue + worker thread` 桥接模式，不再新增第二条实时通道。后端把 `run_planning_turn()` 拆成“流式生成器 + 兼容同步包装器”，服务层新增 `stream_planning_message()` / `stream_generate_planning_drafts()`，前端则把当前仅用于执行的 socket client 提升为 Planning 全链路连接，并让 UI 状态从流事件派生，而不是手工维护阶段枚举。

**Tech Stack:** FastAPI WebSocket, SQLAlchemy, httpx SSE streaming, React 18, TypeScript, Ant Design, Vitest, pytest

---

## File Structure

### Backend

| File | Change |
|------|--------|
| `backend/.env` | 把 `AI_DSL_TIMEOUT_MS`、`AI_VISUAL_TIMEOUT_MS`、`AI_PLANNING_TIMEOUT_MS` 统一改成 `600000` |
| `backend/pyproject.toml` | 将 `httpx` 从 dev 依赖移动到 runtime 依赖，避免生产运行时缺包 |
| `backend/app/ai/test_planning_agent.py` | 新增 `_stream_planning_llm()`、`stream_planning_turn()`，保留 `run_planning_turn()` 作为同步兼容包装 |
| `backend/app/services/ai_planning.py` | 新增 `stream_planning_message()`、`stream_generate_planning_drafts()`，并复用现有 `save_and_execute_selected_drafts_streaming()` |
| `backend/app/services/ai_planning_streaming.py` | 抽出通用 sync-generator → async-generator 桥接，支持 `chat`、`generate_drafts`、`execute` |
| `backend/app/api/routes/ai_planning.py` | 扩展现有 WS 路由，支持 `chat` / `generate_drafts` / `execute` / `cancel` 四类消息，并保持连接可复用 |
| `backend/tests/unit/test_planning_agent.py` | 新增 Planning agent 流式单元测试 |
| `backend/tests/unit/test_ai_planning_api.py` | 新增 Planning WebSocket 流式 API 测试 |

### Frontend

| File | Change |
|------|--------|
| `frontend/src/types/api.ts` | 扩展 Planning WS 事件 union，加入 `status` / `text_chunk` / `tool_call_start` / `tool_call_end` / `draft_generating` / `turn_complete` |
| `frontend/src/services/executionWebSocket.ts` | 从“执行专用 socket”扩展为“Planning session socket”，补 open/close/error 生命周期能力 |
| `frontend/src/services/executionWebSocket.test.ts` | 覆盖新事件透传、关闭、错误与多消息发送 |
| `frontend/src/components/AITestPlanningPanel.tsx` | 建立持久 WS 连接；发送对话、生成草案、保存执行时优先走 WS；渲染流式文本、阶段标签、打字光标与执行进度 |
| `frontend/src/components/AITestPlanningPanel.test.tsx` | 覆盖对话流式渲染、草案流式生成、保存执行沿用同一 WS、fallback 到 REST |

### Out Of Scope

- 不新增 Alembic migration。
- 不移除现有 `POST /messages`、`POST /drafts:generate`、`POST /drafts:save-and-execute`。
- 不改 AI Planning 的持久化 schema；流式状态只作为传输层与前端瞬时展示能力。

---

## Task 1: Planning Agent Streaming Primitive And Timeout Alignment

**Files:**
- Modify: `backend/.env`
- Modify: `backend/pyproject.toml`
- Modify: `backend/app/ai/test_planning_agent.py`
- Modify: `backend/tests/unit/test_planning_agent.py`

- [ ] **Step 1: 先补失败测试，锁定 SSE 文本流与 turn 流事件顺序**

```python
def test_stream_planning_llm_yields_text_chunks_and_full_response(monkeypatch) -> None:
    from app.ai import test_planning_agent as planning_agent

    class FakeStreamResponse:
        def __enter__(self): return self
        def __exit__(self, exc_type, exc, tb): return False
        def iter_lines(self):
            yield 'data: {"choices":[{"delta":{"content":"你好"}}]}'
            yield 'data: {"choices":[{"delta":{"content":"，世界"}}]}'
            yield "data: [DONE]"

    class FakeClient:
        def __enter__(self): return self
        def __exit__(self, exc_type, exc, tb): return False
        def stream(self, *args, **kwargs): return FakeStreamResponse()

    monkeypatch.setattr(planning_agent.httpx, "Client", lambda timeout: FakeClient())

    events = list(
        planning_agent._stream_planning_llm(
            messages=[{"role": "user", "content": "帮我规划登录测试"}],
            api_key="k",
            model="glm-4.7",
            base_url="https://example.com/v1",
            timeout_seconds=30,
        )
    )

    assert events == [
        {"type": "text_chunk", "text": "你好"},
        {"type": "text_chunk", "text": "，世界"},
        {"type": "raw_response", "text": "你好，世界"},
    ]


def test_stream_planning_turn_emits_status_then_turn_complete(monkeypatch) -> None:
    from app.ai import test_planning_agent as planning_agent

    monkeypatch.setattr(planning_agent, "get_settings", lambda: _planning_settings())
    monkeypatch.setattr(
        planning_agent,
        "_stream_planning_llm",
        lambda **_: iter(
            [
                {"type": "text_chunk", "text": '{"action":"generate_plan","action_input":{"summary":"登录测试方案"}}'},
                {"type": "raw_response", "text": '{"action":"generate_plan","action_input":{"summary":"登录测试方案"}}'},
            ]
        ),
    )

    stream = planning_agent.stream_planning_turn(
        transcript=[{"role": "user", "content": "帮我规划登录测试"}],
        existing_requirements=None,
        db_session=object(),
        project_id=1,
    )
    events = []
    with pytest.raises(StopIteration) as stop:
        while True:
            events.append(next(stream))

    assert events[0] == {"type": "status", "phase": "thinking", "message": "正在分析需求..."}
    assert events[-1]["type"] == "turn_complete"
    assert stop.value.value.session_status == "plan_ready"
```

- [ ] **Step 2: 跑后端单测，确认当前实现确实不支持这些流事件**

Run: `cd backend && uv run pytest tests/unit/test_planning_agent.py -q`
Expected: FAIL，因为 `_stream_planning_llm()`、`stream_planning_turn()` 还不存在，且当前仅支持一次性 `urllib.request` 非流式返回。

- [ ] **Step 3: 实现 agent 层流式调用，同时修正超时配置与 runtime 依赖**

```python
# backend/app/ai/test_planning_agent.py
import httpx

def _stream_planning_llm(
    *,
    messages: list[dict[str, Any]],
    api_key: str,
    model: str,
    base_url: str,
    timeout_seconds: float,
) -> Generator[dict[str, str], None, None]:
    payload = {
        "model": model,
        "messages": messages,
        "response_format": {"type": "json_object"},
        "stream": True,
    }
    endpoint = f"{base_url.rstrip('/')}/chat/completions"
    full_text: list[str] = []
    with httpx.Client(timeout=timeout_seconds) as client:
        with client.stream(
            "POST",
            endpoint,
            json=payload,
            headers={
                "Authorization": f"Bearer {api_key}",
                "Content-Type": "application/json",
            },
        ) as response:
            response.raise_for_status()
            for raw_line in response.iter_lines():
                if not raw_line or not raw_line.startswith("data: "):
                    continue
                data = raw_line.removeprefix("data: ").strip()
                if data == "[DONE]":
                    break
                payload = json.loads(data)
                chunk = payload["choices"][0].get("delta", {}).get("content")
                if chunk:
                    full_text.append(chunk)
                    yield {"type": "text_chunk", "text": chunk}
    yield {"type": "raw_response", "text": "".join(full_text)}


def stream_planning_turn(
    *,
    transcript: list[dict[str, str]],
    existing_requirements: AIPlanningRequirements | None,
    db_session: Session,
    project_id: int,
) -> Generator[dict[str, Any], None, AIPlanningTurnResponse]:
    requirements = existing_requirements.model_copy(deep=True) if existing_requirements else AIPlanningRequirements()
    settings = get_settings()
    tool_calls: list[AIPlanningToolCall] = []
    transcript_messages, force_generate = _prepare_transcript_for_llm(transcript)
    conversation: list[dict[str, str]] = [{"role": "system", "content": build_system_prompt()}, *transcript_messages]
    yield {"type": "status", "phase": "thinking", "message": "正在分析需求..."}
    raw_response = ""
    for event in _stream_planning_llm(
        messages=conversation,
        api_key=settings.ai_planning_api_key or "",
        model=settings.ai_planning_model or "",
        base_url=settings.ai_planning_base_url,
        timeout_seconds=max(1.0, settings.ai_planning_timeout_ms / 1000),
    ):
        if event["type"] == "text_chunk":
            yield event
        elif event["type"] == "raw_response":
            raw_response = event["text"]
    parsed = _parse_llm_response(raw_response)
    if parsed is None:
        response = _run_fallback_turn(
            transcript=transcript,
            requirements=requirements,
            assistant_message="遇到了解析问题，我先按已有信息给你整理一个测试方案。",
            force_generate=force_generate,
            tool_calls=tool_calls,
        )
        yield {
            "type": "turn_complete",
            "session_status": response.session_status,
            "payload": {
                "assistant_message": response.assistant_message,
                "missing_slots": response.missing_slots,
                "suggested_questions": response.suggested_questions,
                "plan": response.plan.model_dump(mode="json") if response.plan else None,
                "tool_calls": [item.model_dump(mode="json") for item in response.tool_calls],
            },
        }
        return response
    _merge_requirements(requirements, parsed.get("collected_info"))
    action_input = parsed.get("action_input") if isinstance(parsed.get("action_input"), dict) else {}
    action = str(parsed.get("action") or "").strip()
    if action == "call_tool":
        tool_name = str(action_input.get("tool") or "").strip()
        params = action_input.get("params") if isinstance(action_input.get("params"), dict) else {}
        yield {"type": "tool_call_start", "tool": tool_name, "params": params}
        tool_result_text = execute_tool(
            tool_name=tool_name,
            params=params,
            db_session=db_session,
            project_id=project_id,
        )
        yield {"type": "tool_call_end", "tool": tool_name, "result": _safe_parse_json(tool_result_text)}
        response = AIPlanningTurnResponse(
            assistant_message="我先根据工具结果继续整理测试方案。",
            session_status="collecting",
            requirements=requirements,
            missing_slots=_collect_missing_slots(requirements),
            suggested_questions=[],
            plan=None,
            drafts=[],
            next_action="ask_followup",
            tool_calls=tool_calls,
        )
    elif action == "generate_plan" or force_generate:
        response = _plan_response(
            requirements=requirements,
            plan_payload=action_input,
            assistant_message="信息已经足够，我先给出结构化测试方案。",
            tool_calls=tool_calls,
        )
    else:
        response = AIPlanningTurnResponse(
            assistant_message=str(action_input.get("message") or _default_followup_question(requirements)),
            session_status="collecting",
            requirements=requirements,
            missing_slots=_collect_missing_slots(requirements),
            suggested_questions=[str(action_input.get("message") or _default_followup_question(requirements))],
            plan=None,
            drafts=[],
            next_action="ask_followup",
            tool_calls=tool_calls,
        )
    yield {
        "type": "turn_complete",
        "session_status": response.session_status,
        "payload": {
            "assistant_message": response.assistant_message,
            "missing_slots": response.missing_slots,
            "suggested_questions": response.suggested_questions,
            "plan": response.plan.model_dump(mode="json") if response.plan else None,
            "tool_calls": [item.model_dump(mode="json") for item in response.tool_calls],
        },
    }
    return response


def run_planning_turn(
    *,
    transcript: list[dict[str, str]],
    existing_requirements: AIPlanningRequirements | None,
    db_session: Session,
    project_id: int,
) -> AIPlanningTurnResponse:
    stream = stream_planning_turn(
        transcript=transcript,
        existing_requirements=existing_requirements,
        db_session=db_session,
        project_id=project_id,
    )
    while True:
        try:
            next(stream)
        except StopIteration as stop:
            return stop.value
```

```toml
# backend/pyproject.toml
dependencies = [
    "fastapi>=0.116,<1.0",
    "sqlalchemy>=2.0,<3.0",
    "alembic>=1.16,<2.0",
    "itsdangerous>=2.2,<3.0",
    "playwright>=1.52,<2.0",
    "pillow>=11.0,<12.0",
    "psycopg[binary]>=3.2,<4.0",
    "uvicorn>=0.35,<1.0",
    "httpx>=0.28,<1.0",
]
```

```env
# backend/.env
AI_VISUAL_TIMEOUT_MS=600000
AI_DSL_TIMEOUT_MS=600000
AI_PLANNING_TIMEOUT_MS=600000
```

- [ ] **Step 4: 回归 agent 级测试，确认同步兼容包装仍可工作**

Run: `cd backend && uv run pytest tests/unit/test_planning_agent.py tests/unit/test_ai_planning_api.py -q`
Expected: PASS，且现有 `send_planning_message()`/`generate_planning_drafts()` 的同步 REST 路径不需要改调用方式。

- [ ] **Step 5: Commit**

```bash
git add backend/.env backend/pyproject.toml backend/app/ai/test_planning_agent.py backend/tests/unit/test_planning_agent.py backend/tests/unit/test_ai_planning_api.py
git commit -m "feat: stream ai planning agent responses"
```

## Task 2: Service Generator And WebSocket Route Expansion

**Files:**
- Modify: `backend/app/services/ai_planning.py`
- Modify: `backend/app/services/ai_planning_streaming.py`
- Modify: `backend/app/api/routes/ai_planning.py`
- Modify: `backend/tests/unit/test_ai_planning_api.py`

- [ ] **Step 1: 先补失败测试，覆盖 `chat` / `generate_drafts` / `execute` 复用同一 WS**

```python
def test_ai_planning_ws_handles_chat_then_generate_drafts_then_execute(client, monkeypatch) -> None:
    from app.services import ai_planning_streaming as streaming_service

    async def fake_chat_stream(**kwargs):
        yield {"type": "status", "phase": "thinking", "message": "正在分析需求..."}
        yield {"type": "text_chunk", "text": "好的，我先整理登录测试场景。"}
        yield {"type": "turn_complete", "session_status": "plan_ready", "payload": {"assistant_message": "测试方案已整理"}}

    async def fake_draft_stream(**kwargs):
        yield {"type": "draft_generating", "scenario_key": "login_success", "message": "正在生成登录成功 DSL..."}
        yield {"type": "turn_complete", "session_status": "drafts_ready", "payload": {"assistant_message": "草案已生成"}}

    async def fake_execute_stream(**kwargs):
        yield {"type": "save_progress", "saved_count": 1, "total": 1, "case_name": "登录成功"}
        yield {"type": "done"}

    monkeypatch.setattr(streaming_service, "stream_planning_chat", fake_chat_stream)
    monkeypatch.setattr(streaming_service, "stream_planning_drafts", fake_draft_stream)
    monkeypatch.setattr(streaming_service, "stream_save_and_execute", fake_execute_stream)

    with client.websocket_connect("/api/v1/ai-planning/sessions/1/ws?user_id=1") as ws:
        ws.send_json({"type": "chat", "content": "帮我规划登录测试"})
        assert ws.receive_json()["type"] == "status"
        assert ws.receive_json()["type"] == "text_chunk"
        assert ws.receive_json()["type"] == "turn_complete"

        ws.send_json({"type": "generate_drafts", "scenario_keys": ["login_success"]})
        assert ws.receive_json()["type"] == "draft_generating"
        assert ws.receive_json()["type"] == "turn_complete"

        ws.send_json({"type": "execute", "draft_ids": [11]})
        assert ws.receive_json()["type"] == "save_progress"
        assert ws.receive_json()["type"] == "done"
```

- [ ] **Step 2: 跑 WebSocket 相关测试，确认当前路由仍只接受 `execute` / `cancel`**

Run: `cd backend && uv run pytest tests/unit/test_ai_planning_api.py -q`
Expected: FAIL，因为当前 `ai_planning_session_ws()` 收到 `execute` 后会 `break`，且不存在 `stream_planning_chat()` / `stream_planning_drafts()`。

- [ ] **Step 3: 扩展服务层生成器与通用桥接，保持 REST fallback 不变**

```python
# backend/app/services/ai_planning.py
def stream_planning_message(
    session: Session,
    planning_session_id: int,
    *,
    actor_user_id: int,
    content: str,
) -> Generator[dict[str, object], None, AIPlanningTurnResponse]:
    planning_session = _get_session(session, planning_session_id, actor_user_id=actor_user_id)
    session.add(AIPlanningMessage(session_id=planning_session.id, role="user", turn_type="user", content=content))
    session.flush()
    transcript_records = session.scalars(
        select(AIPlanningMessage)
        .where(AIPlanningMessage.session_id == planning_session.id)
        .order_by(AIPlanningMessage.id.asc())
    ).all()
    stream = stream_planning_turn(
        transcript=[{"role": item.role, "content": item.content} for item in transcript_records if item.turn_type != "tool_call"],
        existing_requirements=AIPlanningRequirements.model_validate(planning_session.requirements_json or {}),
        db_session=session,
        project_id=planning_session.project_id,
    )
    while True:
        try:
            event = next(stream)
            yield event
        except StopIteration as stop:
            response = stop.value
            break
    planning_session.status = response.session_status
    planning_session.requirements_json = response.requirements.model_dump(mode="json")
    planning_session.plan_json = response.plan.model_dump(mode="json") if response.plan else None
    planning_session.missing_slots_json = response.missing_slots
    turn_type = "plan" if response.plan is not None else ("system_error" if response.session_status == "error" else "followup")
    session.add(
        AIPlanningMessage(
            session_id=planning_session.id,
            role="assistant",
            turn_type=turn_type,
            content=response.assistant_message,
            structured_payload_json={
                "missing_slots": response.missing_slots,
                "suggested_questions": response.suggested_questions,
                "plan": response.plan.model_dump(mode="json") if response.plan else None,
                "tool_calls": [item.model_dump(mode="json") for item in response.tool_calls],
            },
        )
    )
    session.commit()
    return response


def stream_generate_planning_drafts(
    session: Session,
    planning_session_id: int,
    payload: GenerateAIPlanningDraftsRequest,
    *,
    actor_user_id: int,
) -> Generator[dict[str, object], None, AIPlanningTurnResponse]:
    for scenario_key in payload.scenario_keys:
        yield {
            "type": "draft_generating",
            "scenario_key": scenario_key,
            "message": f"正在生成 {scenario_key} 的 DSL...",
        }
    result = generate_planning_drafts(
        session,
        planning_session_id,
        payload,
        actor_user_id=actor_user_id,
    )
    yield {
        "type": "turn_complete",
        "session_status": result.session_status,
        "payload": {
            "assistant_message": result.assistant_message,
            "drafts": [item.model_dump(mode='json') for item in result.drafts],
            "plan": result.plan.model_dump(mode='json') if result.plan else None,
        },
    }
    return result
```

```python
# backend/app/services/ai_planning_streaming.py
def _run_sync_generator(
    *,
    generator_factory: Callable[[Session], Generator[dict[str, object], None, object]],
    session_factory: sessionmaker,
    queue: asyncio.Queue,
    loop: asyncio.AbstractEventLoop,
) -> None:
    try:
        with session_factory() as session:
            stream = generator_factory(session)
            while True:
                try:
                    event = next(stream)
                except StopIteration:
                    break
                loop.call_soon_threadsafe(queue.put_nowait, _serialize_event(event))
    except Exception as exc:
        logger.exception("Planning stream worker error")
        loop.call_soon_threadsafe(queue.put_nowait, {"type": "error", "message": str(exc)})
    finally:
        loop.call_soon_threadsafe(queue.put_nowait, _TerminalSignal())


async def stream_planning_chat(
    *,
    session_factory: sessionmaker,
    planning_session_id: int,
    content: str,
    actor_user_id: int,
) -> AsyncGenerator[dict, None]:
    async for event in _bridge_sync_generator(
        session_factory=session_factory,
        generator_factory=lambda db: stream_planning_message(db, planning_session_id, actor_user_id=actor_user_id, content=content),
    ):
        yield event


async def stream_planning_drafts(
    *,
    session_factory: sessionmaker,
    planning_session_id: int,
    payload: GenerateAIPlanningDraftsRequest,
    actor_user_id: int,
) -> AsyncGenerator[dict, None]:
    async for event in _bridge_sync_generator(
        session_factory=session_factory,
        generator_factory=lambda db: stream_generate_planning_drafts(db, planning_session_id, payload, actor_user_id=actor_user_id),
    ):
        yield event
```

```python
# backend/app/api/routes/ai_planning.py
while True:
    data = await websocket.receive_json()
    msg_type = data.get("type")

    if msg_type == "chat":
        async for event in stream_planning_chat(
            session_factory=session_factory,
            planning_session_id=session_id,
            content=str(data.get("content") or ""),
            actor_user_id=current_user.id,
        ):
            await websocket.send_json(event)
        continue

    if msg_type == "generate_drafts":
        payload = GenerateAIPlanningDraftsRequest.model_validate(
            {
                "scenario_keys": data.get("scenario_keys", []),
                "current_case": data.get("current_case"),
                "current_steps": data.get("current_steps"),
                "current_input_contract": data.get("current_input_contract"),
                "current_output_contract": data.get("current_output_contract"),
                "preserve_contracts": data.get("preserve_contracts", True),
            }
        )
        async for event in stream_planning_drafts(
            session_factory=session_factory,
            planning_session_id=session_id,
            payload=payload,
            actor_user_id=current_user.id,
        ):
            await websocket.send_json(event)
        continue

    if msg_type == "execute":
        async for event in stream_save_and_execute(
            session_factory=session_factory,
            planning_session_id=session_id,
            draft_ids=data.get("draft_ids", []),
            actor_user_id=current_user.id,
            cancel_event=cancel_event,
        ):
            await websocket.send_json(event)
        continue

    if msg_type == "cancel":
        cancel_event.set()
        await websocket.send_json({"type": "cancelled"})
        continue

    await websocket.send_json({"type": "error", "message": f"Unsupported message type: {msg_type}"})
```

- [ ] **Step 4: 跑后端 AI Planning API 回归，确认 REST 与 WS 两条路径都仍可用**

Run: `cd backend && uv run pytest tests/unit/test_ai_planning_api.py tests/unit/test_planning_agent.py -q`
Expected: PASS，已有 REST API 测试继续通过，新增 WS 流式测试覆盖新事件序列。

- [ ] **Step 5: Commit**

```bash
git add backend/app/services/ai_planning.py backend/app/services/ai_planning_streaming.py backend/app/api/routes/ai_planning.py backend/tests/unit/test_ai_planning_api.py
git commit -m "feat: extend planning websocket for chat and draft streaming"
```

## Task 3: Frontend Planning WebSocket Event Model

**Files:**
- Modify: `frontend/src/types/api.ts`
- Modify: `frontend/src/services/executionWebSocket.ts`
- Modify: `frontend/src/services/executionWebSocket.test.ts`

- [ ] **Step 1: 先补失败测试，定义新事件透传与连接关闭行为**

```typescript
it("forwards planning stream events and supports explicit close", async () => {
  const { connectExecutionStream } = await import("./executionWebSocket");
  const onEvent = vi.fn();
  const onError = vi.fn();

  const client = connectExecutionStream(5, onEvent, onError);
  await vi.waitFor(() => expect(MockWebSocket.instances.length).toBe(1));

  const ws = MockWebSocket.instances[0];
  ws._receive({ type: "status", phase: "thinking", message: "正在分析需求..." });
  ws._receive({ type: "text_chunk", text: "好的，我来分析一下。" });

  expect(onEvent).toHaveBeenNthCalledWith(1, {
    type: "status",
    phase: "thinking",
    message: "正在分析需求...",
  });
  expect(onEvent).toHaveBeenNthCalledWith(2, {
    type: "text_chunk",
    text: "好的，我来分析一下。",
  });

  client.close();
  expect(ws.readyState).toBe(WebSocket.CLOSED);
});
```

- [ ] **Step 2: 跑前端 socket 测试，确认当前类型系统不包含这些事件**

Run: `cd frontend && npm run test -- src/services/executionWebSocket.test.ts`
Expected: FAIL，因为 `ExecutionStreamEvent` 还没有 `status` / `text_chunk` / `turn_complete` 等 Planning 事件。

- [ ] **Step 3: 扩展事件 union 与 socket 生命周期能力**

```typescript
// frontend/src/types/api.ts
export interface StatusStreamEvent {
  type: "status";
  phase: "thinking" | "generating" | "tool_calling" | "executing";
  message: string;
}

export interface TextChunkStreamEvent {
  type: "text_chunk";
  text: string;
}

export interface ToolCallStartStreamEvent {
  type: "tool_call_start";
  tool: string;
  params?: Record<string, unknown>;
}

export interface ToolCallEndStreamEvent {
  type: "tool_call_end";
  tool: string;
  result?: unknown;
}

export interface DraftGeneratingStreamEvent {
  type: "draft_generating";
  scenario_key: string;
  message: string;
}

export interface TurnCompleteStreamEvent {
  type: "turn_complete";
  session_status: string;
  payload: Record<string, unknown>;
}

export type ExecutionStreamEvent =
  | StatusStreamEvent
  | TextChunkStreamEvent
  | ToolCallStartStreamEvent
  | ToolCallEndStreamEvent
  | DraftGeneratingStreamEvent
  | TurnCompleteStreamEvent
  | SaveProgressEvent
  | CaseStartEvent
  | StepStartEvent
  | StepCompleteEvent
  | ExecutionSummaryStreamEvent
  | CancelledEvent
  | DoneEvent
  | ErrorEvent;
```

```typescript
// frontend/src/services/executionWebSocket.ts
export interface ExecutionStreamClient {
  send: (data: Record<string, unknown>) => void;
  close: () => void;
  isOpen: () => boolean;
}

export function connectExecutionStream(
  sessionId: number,
  onEvent: (event: ExecutionStreamEvent) => void,
  onError: (error: Error) => void,
): ExecutionStreamClient {
  const ws = new WebSocket(buildWsUrl(sessionId));

  ws.onmessage = (ev) => {
    try {
      onEvent(JSON.parse(ev.data) as ExecutionStreamEvent);
    } catch {
      onError(new Error("Invalid WebSocket payload"));
    }
  };
  ws.onerror = () => onError(new Error("WebSocket connection error"));

  return {
    send(data) {
      ws.send(JSON.stringify(data));
    },
    close() {
      ws.close();
    },
    isOpen() {
      return ws.readyState === WebSocket.OPEN;
    },
  };
}
```

- [ ] **Step 4: 跑 socket 单测**

Run: `cd frontend && npm run test -- src/services/executionWebSocket.test.ts`
Expected: PASS，既有 `execute` 发送行为仍然成立，新 Planning 事件也能透传。

- [ ] **Step 5: Commit**

```bash
git add frontend/src/types/api.ts frontend/src/services/executionWebSocket.ts frontend/src/services/executionWebSocket.test.ts
git commit -m "feat: extend planning websocket event model"
```

## Task 4: Planning Panel Chat And Draft Streaming UI

**Files:**
- Modify: `frontend/src/components/AITestPlanningPanel.tsx`
- Modify: `frontend/src/components/AITestPlanningPanel.test.tsx`

- [ ] **Step 1: 先补失败测试，覆盖“发送消息后出现流式 AI 气泡”和“生成草案显示阶段标签”**

```typescript
test("发送消息后通过同一条 WS 流式渲染 AI 回复", async () => {
  const send = vi.fn();
  let onEventRef: ((event: ExecutionStreamEvent) => void) | null = null;
  vi.mocked(wsModule.connectExecutionStream).mockImplementation((_sessionId, onEvent) => {
    onEventRef = onEvent as (event: ExecutionStreamEvent) => void;
    return { send, close: vi.fn(), isOpen: () => true };
  });

  renderWithProviders(<AITestPlanningPanel aiSettings={aiSettings} projectId={1} onImportDraft={vi.fn()} />);
  await screen.findByText("AI Planning");
  await userEvent.type(screen.getByLabelText("测试规划对话输入"), "帮我规划登录测试{enter}");

  expect(send).toHaveBeenCalledWith({ type: "chat", content: "帮我规划登录测试" });

  act(() => {
    onEventRef?.({ type: "status", phase: "thinking", message: "正在分析需求..." });
    onEventRef?.({ type: "text_chunk", text: "好的，我先整理一下。" });
  });

  expect(screen.getByText("正在分析需求...")).toBeInTheDocument();
  expect(screen.getByText(/好的，我先整理一下/)).toBeInTheDocument();
});


test("生成草案时通过 WS 显示 draft_generating 状态", async () => {
  const send = vi.fn();
  vi.mocked(wsModule.connectExecutionStream).mockImplementation((_sessionId, onEvent) => {
    setTimeout(() => {
      onEvent({
        type: "draft_generating",
        scenario_key: "login_success",
        message: "正在生成 login_success 的 DSL...",
      });
    }, 0);
    return { send, close: vi.fn(), isOpen: () => true };
  });

  renderWithProviders(<AITestPlanningPanel aiSettings={aiSettings} projectId={1} onImportDraft={vi.fn()} />);
  await screen.findByText("AI Planning");
  await userEvent.click(await screen.findByRole("checkbox", { name: "选择场景 登录成功" }));
  await userEvent.click(screen.getByRole("button", { name: "生成选中草案" }));
  expect(send).toHaveBeenCalledWith(
    expect.objectContaining({
      type: "generate_drafts",
      scenario_keys: ["login_success"],
    }),
  );
});
```

- [ ] **Step 2: 跑组件测试，确认当前实现仍然只走 REST 对话与草案生成**

Run: `cd frontend && npm run test -- src/components/AITestPlanningPanel.test.tsx`
Expected: FAIL，因为当前 `handleSendMessage()` 调 `sendPlanningMessage()`，`handleGenerateDrafts()` 调 `generatePlanningDrafts()`，不会创建流式对话气泡。

- [ ] **Step 3: 让 Panel 使用“会话级持久 WS”，并在不可用时回退到 REST**

```typescript
// frontend/src/components/AITestPlanningPanel.tsx
const wsClientRef = useRef<ExecutionStreamClient | null>(null);
const activeAssistantMessageIdRef = useRef<number | null>(null);

useEffect(() => {
  if (!sessionId) return;
  const client = connectExecutionStream(
    sessionId,
    async (event) => {
      if (event.type === "status" || event.type === "text_chunk" || event.type === "tool_call_start" || event.type === "tool_call_end") {
        setTranscript((current) =>
          current.map((msg) =>
            msg.id === activeAssistantMessageIdRef.current
              ? applyPlanningStreamEvent(msg, event)
              : msg,
          ),
        );
        return;
      }

      if (event.type === "draft_generating") {
        setTranscript((current) =>
          current.map((msg) =>
            msg.id === activeAssistantMessageIdRef.current
              ? {
                  ...msg,
                  structured_payload: {
                    ...(msg.structured_payload ?? {}),
                    _phase: "draft_generating",
                    _phaseMessage: event.message,
                  },
                }
              : msg,
          ),
        );
        return;
      }

      if (event.type === "turn_complete") {
        await loadSessionDetail(sessionId);
        await loadSessionList();
      }
    },
    (error) => {
      void messageApi.error(error.message);
    },
  );
  wsClientRef.current = client;
  return () => {
    client.close();
    wsClientRef.current = null;
  };
}, [sessionId]);

async function handleSendMessage() {
  const optimisticUser = createOptimisticMessage(sessionId, "user", "user", trimmed);
  const optimisticAssistant = createOptimisticMessage(sessionId, "assistant", "followup", "", {
    type: "streaming_response",
    _phase: "thinking",
    _phaseMessage: "正在分析需求...",
  });
  activeAssistantMessageIdRef.current = optimisticAssistant.id;
  setTranscript((current) => [...current, optimisticUser, optimisticAssistant]);

  if (wsClientRef.current?.isOpen()) {
    wsClientRef.current.send({ type: "chat", content: trimmed });
    setInputValue("");
    return;
  }

  const response = await sendPlanningMessage(sessionId, { content: trimmed });
  setTranscript((current) =>
    current
      .filter((msg) => msg.id !== optimisticAssistant.id)
      .concat(
        buildToolMessages(sessionId, response.tool_calls ?? []),
        createOptimisticMessage(
          sessionId,
          "assistant",
          response.plan ? "plan" : response.session_status === "error" ? "system_error" : "followup",
          response.assistant_message,
        ),
      ),
  );
}

async function handleGenerateDrafts() {
  setIsGenerating(true);
  if (wsClientRef.current?.isOpen()) {
    const optimisticAssistant = createOptimisticMessage(sessionId, "assistant", "plan", "", {
      type: "streaming_response",
      _phase: "generating",
      _phaseMessage: "正在生成 DSL...",
    });
    activeAssistantMessageIdRef.current = optimisticAssistant.id;
    setTranscript((current) => [...current, optimisticAssistant]);
    wsClientRef.current.send({
      type: "generate_drafts",
      scenario_keys: selectedScenarioKeys,
      current_case: currentCase ?? null,
      current_steps: currentSteps ?? null,
      current_input_contract: currentInputContract ?? null,
      current_output_contract: currentOutputContract ?? null,
      preserve_contracts: true,
    });
    return;
  }

  const response = await generatePlanningDrafts(sessionId, {
    scenario_keys: selectedScenarioKeys,
    current_case: currentCase ?? null,
    current_steps: currentSteps ?? null,
    current_input_contract: currentInputContract ?? null,
    current_output_contract: currentOutputContract ?? null,
    preserve_contracts: true,
  });
  setDrafts(response.drafts);
  setPlan(response.plan ?? null);
  setTranscript((current) => [
    ...current,
    createOptimisticMessage(sessionId, "assistant", "plan", response.assistant_message),
  ]);
}
```

```tsx
{item.structured_payload?._phase ? (
  <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
    <Tag color={phaseColorMap[item.structured_payload._phase as string] ?? "processing"}>
      {String(item.structured_payload._phaseMessage ?? "处理中...")}
    </Tag>
    <div style={{ whiteSpace: "pre-wrap" }}>
      {item.content}
      {item.structured_payload?._streaming ? <span className="typing-cursor">▊</span> : null}
    </div>
  </div>
) : (
  item.content
)}
```

- [ ] **Step 4: 跑组件测试，确认对话与草案都能流式展示，且 fallback 仍可走 REST**

Run: `cd frontend && npm run test -- src/components/AITestPlanningPanel.test.tsx`
Expected: PASS，新增测试验证 WS 文本块、阶段标签、草案生成消息，已有 REST mock 用例继续成立。

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/AITestPlanningPanel.tsx frontend/src/components/AITestPlanningPanel.test.tsx
git commit -m "feat: stream planning chat and draft generation in panel"
```

## Task 5: Unify Execute Flow On The Same WebSocket And Final Validation

**Files:**
- Modify: `frontend/src/components/AITestPlanningPanel.tsx`
- Modify: `frontend/src/components/AITestPlanningPanel.test.tsx`
- Modify: `backend/tests/unit/test_ai_planning_api.py`

- [ ] **Step 1: 先补失败测试，锁定“保存并执行沿用已建立的 session WS，而不是额外临时连接”**

```typescript
test("保存并执行沿用已建立的 planning socket", async () => {
  const send = vi.fn();
  vi.mocked(wsModule.connectExecutionStream).mockImplementation(() => ({
    send,
    close: vi.fn(),
    isOpen: () => true,
  }));

  renderWithProviders(<AITestPlanningPanel aiSettings={aiSettings} projectId={1} onImportDraft={vi.fn()} />);
  await screen.findByText("AI Planning");
  await userEvent.click(screen.getByRole("checkbox", { name: "选择场景 登录成功" }));
  await userEvent.click(screen.getByRole("button", { name: "保存并执行" }));

  expect(send).toHaveBeenCalledWith({ type: "execute", draft_ids: [11] });
  expect(wsModule.connectExecutionStream).toHaveBeenCalledTimes(1);
});
```

- [ ] **Step 2: 跑前端测试，确认当前实现保存执行仍然是点击时临时创建一条 socket**

Run: `cd frontend && npm run test -- src/components/AITestPlanningPanel.test.tsx`
Expected: FAIL，因为当前 `保存并执行` 内部调用 `connectExecutionStream()`，会产生第二条连接。

- [ ] **Step 3: 复用 session socket 发送 `execute` / `cancel`，并保持 HTTP fallback**

```typescript
async function handleExecuteSelectedDrafts() {
  if (!sessionId || selectedScenarioKeys.length === 0) return;
  const draftIds = drafts.filter((d) => selectedScenarioKeys.includes(d.scenario_key)).map((d) => d.id);

  setIsExecuting(true);
  const progressMessage = createOptimisticMessage(sessionId, "assistant", "followup", "正在保存并执行已选草案…", {
    type: "execution_progress",
    saved_count: 0,
    total: 0,
    cases: [],
  });
  activeAssistantMessageIdRef.current = progressMessage.id;
  setTranscript((current) => [...current, progressMessage]);

  if (wsClientRef.current?.isOpen()) {
    wsClientRef.current.send({ type: "execute", draft_ids });
    return;
  }

  const resp = await saveAndExecuteDrafts(sessionId, draftIds, true);
  setTranscript((current) => [
    ...current,
    createOptimisticMessage(sessionId, "assistant", "plan", resp.assistant_message, {
      type: "execution_summary",
      saved_cases: resp.saved_cases ?? [],
      execution_summaries: resp.execution_summaries ?? [],
    }),
  ]);
}

function handleSocketEvent(event: ExecutionStreamEvent) {
  if (event.type === "save_progress" || event.type === "case_start" || event.type === "step_start" || event.type === "step_complete") {
    setTranscript((current) =>
      current.map((msg) =>
        msg.id === activeAssistantMessageIdRef.current
          ? {
              ...msg,
              content: applyStreamEventToContent(msg.content, event),
              structured_payload: applyStreamEventToPayload(msg.structured_payload as Record<string, unknown> | null, event),
            }
          : msg,
      ),
    );
    return;
  }

  if (event.type === "done" || event.type === "cancelled" || event.type === "error") {
    void loadSessionDetail(sessionId!);
    void loadSessionList();
    setIsExecuting(false);
  }
}
```

- [ ] **Step 4: 跑前后端核心回归，并做一次手工 smoke**

Run: `cd backend && uv run pytest tests/unit/test_planning_agent.py tests/unit/test_ai_planning_api.py -q`
Expected: PASS

Run: `cd frontend && npm run test -- src/services/executionWebSocket.test.ts src/components/AITestPlanningPanel.test.tsx`
Expected: PASS

Run: `cd frontend && npx tsc --noEmit`
Expected: PASS

Manual smoke:
1. `cd backend && uv run uvicorn app.main:create_app --factory --reload`
2. `cd frontend && npm run dev`
3. 打开 Planning 页面，连续执行“发送对话 → 生成草案 → 保存并执行”
4. 观察同一会话中出现阶段标签、逐块文本、DSL 生成提示、步骤级执行进度
5. 人为断开 WS 或停掉后端后，再次发送消息，确认 REST fallback 仍能得到完整响应

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/AITestPlanningPanel.tsx frontend/src/components/AITestPlanningPanel.test.tsx backend/tests/unit/test_ai_planning_api.py
git commit -m "feat: unify ai planning streaming workflow"
```

---

## Validation

1. `cd backend && uv run pytest tests/unit/test_planning_agent.py tests/unit/test_ai_planning_api.py -q`
2. `cd frontend && npm run test -- src/services/executionWebSocket.test.ts src/components/AITestPlanningPanel.test.tsx`
3. `cd frontend && npx tsc --noEmit`
4. 手工验证 Planning 页面：同一会话内依次完成对话、草案生成、保存执行，确认 WS 流事件顺序正确，UI 标签与最终持久化消息一致。

## Implementation Notes

- 当前仓库已经有 `save_and_execute_selected_drafts_streaming()` 与 `WS /sessions/{session_id}/ws`，因此本计划是“扩展现有流式基础设施”，不是另起一套 SSE 或第二条 WebSocket。
- `httpx` 目前只在 `backend` 的 dev 依赖中，若按 spec 在运行时代码中引入 `httpx.Client`，必须同步移动到 runtime 依赖，否则生产环境启动会因缺包失败。
- 前端 `ExecutionSummaryStreamEvent` 当前已在类型中声明，但后端 WS 实际发的是持久化后 `done` + 重新拉详情；本次保持这个模型，避免同时维护两套最终结果来源。
- `turn_complete` 的 payload 以“足够更新当前气泡 + 最终仍回读 session detail”为原则，不试图把所有数据库持久化字段都塞进实时事件里。
- REST fallback 只在 WS 不可用、连接异常或流式超时未收到任何事件时触发，避免同一操作重复提交。
