# SSE 流式传输测试设计指南

> 以 AI 测试规划平台为例，讲解异步测试、单元测试、集成测试、E2E 测试的设计思路。

---

## 目录

1. [测试金字塔](#测试金字塔)
2. [异步测试基础](#异步测试基础)
3. [单元测试](#单元测试)
4. [集成测试](#集成测试)
5. [E2E 测试](#e2e-测试)
6. [测试维度：正常流程、边界条件、异常流程](#测试维度)
7. [实际案例：SSE 流式传输测试](#实际案例)

---

## 测试金字塔

```
                    ┌─────────────┐
                    │   E2E 测试   │  ← 模拟真实用户操作，验证完整链路
                    ├─────────────┤
                    │  集成测试    │  ← 测试组件交互，验证 API 行为
                    ├─────────────┤
                    │  单元测试    │  ← 测试独立函数，验证业务逻辑
                    └─────────────┘

数量：单元测试 > 集成测试 > E2E 测试
速度：单元测试 > 集成测试 > E2E 测试
成本：单元测试 < 集成测试 < E2E 测试
```

---

## 异步测试基础

### 为什么需要异步测试？

SSE 流式传输是异步的：客户端发送请求 → 服务端流式返回事件 → 客户端处理事件。

**同步测试**：调用 → 等待 → 返回 → 断言
**异步测试**：调用 → 继续执行 → 事件到达 → 断言

### 异步测试的三种方式

#### 方式 1：同步包装器（最简单）

```python
import asyncio

def test_async_function():
    """用 asyncio.run() 包装异步函数"""
    async def async_operation():
        await asyncio.sleep(0.1)
        return "result"
    
    # 同步调用异步函数
    result = asyncio.run(async_operation())
    assert result == "result"
```

**适用场景**：简单的异步函数测试

#### 方式 2：pytest-asyncio（推荐）

```python
import pytest

@pytest.mark.asyncio
async def test_async_function():
    """直接标记为异步测试"""
    result = await async_operation()
    assert result == "result"
```

**适用场景**：大多数异步测试

#### 方式 3：httpx.AsyncClient（FastAPI 专用）

```python
import pytest
from httpx import AsyncClient

@pytest.mark.asyncio
async def test_fastapi_endpoint():
    """测试 FastAPI 异步端点"""
    async with AsyncClient(app=app, base_url="http://test") as client:
        response = await client.get("/api/v1/health")
        assert response.status_code == 200
```

**适用场景**：FastAPI API 测试

### 异步 Mock

```python
from unittest.mock import AsyncMock

@pytest.mark.asyncio
async def test_async_mock():
    """测试异步 Mock"""
    mock_service = AsyncMock()
    mock_service.fetch_data.return_value = {"key": "value"}
    
    result = await mock_service.fetch_data()
    assert result == {"key": "value"}
    mock_service.fetch_data.assert_called_once()
```

---

## 单元测试

### 定义

单元测试是测试**独立函数或方法**的测试，不依赖外部资源（数据库、网络、文件系统）。

### 特点

- **速度快**：毫秒级
- **隔离性**：不依赖外部资源
- **确定性**：相同输入 always 相同输出
- **覆盖面高**：覆盖所有分支和边界

### 本项目示例

#### 示例 1：测试 AI 规划 Agent

```python
# backend/tests/unit/test_planning_agent.py

def test_run_planning_turn_calls_tool_then_asks_user(monkeypatch):
    """测试 AI 规划 Agent 的 ReAct 循环"""
    from app.ai import test_planning_agent as planning_agent

    # Mock LLM 响应
    llm_responses = iter([
        """
        {
          "thought": "先看下项目里有没有现成登录用例",
          "action": "call_tool",
          "action_input": {
            "tool": "list_test_cases",
            "params": { "search": "登录", "limit": 1 }
          }
        }
        """,
        """
        {
          "thought": "已有业务目标，但还缺入口信息",
          "action": "ask_user",
          "action_input": {
            "message": "请补充登录入口页面或 URL。"
          }
        }
        """,
    ])

    def _fake_stream_llm(**_kwargs):
        text = next(llm_responses)
        yield {"type": "text_chunk", "text": text}

    # Mock 外部依赖
    monkeypatch.setattr(planning_agent, "_stream_planning_llm", _fake_stream_llm)
    monkeypatch.setattr(
        planning_agent,
        "execute_tool",
        lambda **kwargs: '{"cases": [{"id": 11, "name": "后台登录成功"}], "total": 1}',
    )

    # 执行测试
    result = planning_agent.run_planning_turn(
        transcript=[{"role": "user", "content": "帮我规划商城后台登录测试"}],
        existing_requirements=None,
        db_session=object(),
        project_id=9,
    )

    # 断言
    assert result.next_action == "ask_followup"
    assert result.session_status == "collecting"
    assert result.assistant_message == "请补充登录入口页面或 URL。"
    assert result.requirements.app_under_test == "商城后台"
    assert result.requirements.business_goal == "商城后台登录"
```

**关键点**：
- Mock 了 LLM 调用和数据库操作
- 测试的是纯业务逻辑
- 不依赖外部资源

#### 示例 2：测试事件日志写入

```python
# backend/tests/unit/test_sse_event_log.py

def test_event_log_writer_writes_events():
    """测试 EventLogWriter 写入事件"""
    from app.services.sse_event_log import EventLogWriter
    
    # Mock session factory
    mock_session = MagicMock()
    mock_session_factory = MagicMock(return_value=mock_session)
    
    # 创建 writer
    writer = EventLogWriter(
        session_factory=mock_session_factory,
        session_id=1,
        message_id=100,
        flush_interval=3,
    )
    
    # 写入事件
    writer.write("status", {"phase": "thinking"})
    writer.write("text_chunk", {"text": "hello"})
    writer.write("text_chunk", {"text": "world"})
    
    # 验证写入了 3 个事件（达到 flush_interval）
    assert mock_session.add.call_count == 3
    assert mock_session.commit.call_count == 1  # 自动 flush
```

**关键点**：
- Mock 了数据库 session
- 测试的是 EventLogWriter 的逻辑
- 验证了自动 flush 行为

---

## 集成测试

### 定义

集成测试是测试**组件之间交互**的测试，涉及真实或模拟的外部资源（数据库、API）。

### 特点

- **速度中等**：秒级
- **真实性**：使用真实数据库
- **覆盖面中等**：覆盖主要路径
- **依赖环境**：需要数据库等外部资源

### 本项目示例

#### 示例 1：测试会话 CRUD

```python
# backend/tests/unit/test_ai_planning_api.py

def test_create_planning_session_and_restore_detail(client):
    """测试创建会话并获取详情"""
    # 创建会话
    create_response = client.post(
        "/api/v1/ai-planning/sessions",
        json={},
    )
    assert create_response.status_code == 201
    session_id = create_response.json()["session"]["id"]

    # 获取详情
    detail_response = client.get(f"/api/v1/ai-planning/sessions/{session_id}")
    assert detail_response.status_code == 200
    payload = detail_response.json()
    assert payload["messages"] == []
    assert payload["drafts"] == []
```

**关键点**：
- 使用真实的数据库（SQLite 内存）
- 测试完整的 HTTP 请求/响应
- 验证数据持久化

#### 示例 2：测试会话删除

```python
def test_delete_planning_session_removes_session_and_returns_204(client):
    """测试删除会话"""
    # 创建会话
    create_response = client.post("/api/v1/ai-planning/sessions", json={})
    session_id = create_response.json()["session"]["id"]

    # 删除会话
    delete_response = client.delete(f"/api/v1/ai-planning/sessions/{session_id}")
    assert delete_response.status_code == 204

    # 验证会话已删除
    detail_response = client.get(f"/api/v1/ai-planning/sessions/{session_id}")
    assert detail_response.status_code == 404
```

#### 示例 3：测试孤儿会话检测

```python
# backend/tests/unit/test_orphan_session.py

def test_send_message_to_deleted_session_returns_404(client, db_session):
    """测试向已删除的会话发送消息返回 404"""
    # 创建会话
    create_resp = client.post("/api/v1/ai-planning/sessions", json={})
    session_id = create_resp.json()["session"]["id"]

    # 删除会话
    delete_resp = client.delete(f"/api/v1/ai-planning/sessions/{session_id}")
    assert delete_resp.status_code == 204

    # 尝试发送消息
    msg_resp = client.post(
        f"/api/v1/ai-planning/sessions/{session_id}/messages",
        json={"content": "test message"},
    )
    assert msg_resp.status_code == 404  # 不是 200！
```

**关键点**：
- 测试了组件之间的交互
- 使用真实的数据库
- 验证了错误处理

---

## E2E 测试

### 定义

E2E（End-to-End）测试是模拟**真实用户操作**的测试，验证完整链路。

### 特点

- **速度慢**：秒级到分钟级
- **真实性**：使用真实浏览器
- **覆盖面低**：只覆盖关键路径
- **成本高**：需要浏览器环境

### 本项目示例

#### 示例 1：测试 AI 规划完整流程

```python
# backend/tests/integration/test_e2e_planning.py

@pytest.mark.browser_integration
def test_ai_planning_full_flow(page):
    """测试 AI 规划完整流程"""
    # 1. 打开规划页面
    page.goto("http://localhost:5173/planning")
    
    # 2. 创建新会话
    page.click("button:has-text('新建会话')")
    page.wait_for_selector("text=AI Planning")
    
    # 3. 发送消息
    page.fill("textarea", "帮我规划商城后台登录测试")
    page.click("button:has-text('发送')")
    
    # 4. 等待 AI 响应
    page.wait_for_selector("text=请补充", timeout=30000)
    
    # 5. 验证响应内容
    response_text = page.locator(".assistant-message").text_content()
    assert "请补充" in response_text
    
    # 6. 验证会话状态
    page.click("button:has-text('会话详情')")
    status = page.locator(".session-status").text_content()
    assert status == "collecting"
```

**关键点**：
- 使用真实浏览器（Playwright）
- 模拟真实用户操作
- 验证完整链路

#### 示例 2：测试页面刷新恢复

```python
@pytest.mark.browser_integration
def test_page_refresh_recovery(page):
    """测试页面刷新后恢复数据"""
    # 1. 创建会话并发送消息
    page.goto("http://localhost:5173/planning")
    page.click("button:has-text('新建会话')")
    page.fill("textarea", "测试消息")
    page.click("button:has-text('发送')")
    
    # 2. 等待部分响应
    page.wait_for_selector(".assistant-message", timeout=10000)
    
    # 3. 刷新页面
    page.reload()
    
    # 4. 验证数据恢复
    page.wait_for_selector(".assistant-message", timeout=10000)
    messages = page.locator(".message-item").count()
    assert messages >= 2  # 用户消息 + AI 回复（或中断标记）
```

---

## 测试维度

### 1. 正常流程（Happy Path）

**定义**：用户按照预期方式使用系统，所有操作都成功。

**测试重点**：
- 完整的业务流程
- 数据正确性
- 状态一致性

**示例**：

```python
def test_normal_chat_flow(client, db_session):
    """测试正常聊天流程"""
    # 创建会话
    session = create_session(client)
    
    # 发送消息
    response = send_message(client, session.id, "帮我规划登录测试")
    
    # 验证响应
    assert response.status_code == 200
    assert "assistant_message" in response.json()
    
    # 验证数据库
    messages = get_messages(db_session, session.id)
    assert len(messages) >= 2
```

### 2. 边界条件（Edge Cases）

**定义**：系统在边界状态下的行为，如空值、最大值、最小值。

**测试重点**：
- 空输入
- 最大长度
- 特殊字符
- 并发操作

**示例**：

```python
def test_empty_message(client, db_session):
    """测试空消息"""
    session = create_session(client)
    
    response = client.post(
        f"/api/v1/ai-planning/sessions/{session.id}/messages",
        json={"content": ""},  # 空消息
    )
    
    # 应该返回 422 或 400
    assert response.status_code in [400, 422]


def test_long_message(client, db_session):
    """测试超长消息"""
    session = create_session(client)
    long_message = "测试" * 10000  # 20000 字符
    
    response = client.post(
        f"/api/v1/ai-planning/sessions/{session.id}/messages",
        json={"content": long_message},
    )
    
    # 应该能处理或返回合适的错误
    assert response.status_code in [200, 400, 413]


def test_special_characters(client, db_session):
    """测试特殊字符"""
    session = create_session(client)
    special_message = "'; DROP TABLE sessions; --"
    
    response = client.post(
        f"/api/v1/ai-planning/sessions/{session.id}/messages",
        json={"content": special_message},
    )
    
    # 应该正常处理，不会执行 SQL
    assert response.status_code == 200
    
    # 验证表还在
    assert session_exists(db_session, session.id)
```

### 3. 异常流程（Error Cases）

**定义**：系统在异常状态下的行为，如网络错误、服务重启、数据不一致。

**测试重点**：
- 资源不存在
- 权限不足
- 并发冲突
- 超时处理

**示例**：

```python
def test_session_not_found(client):
    """测试会话不存在"""
    response = client.get("/api/v1/ai-planning/sessions/999")
    assert response.status_code == 404


def test_session_deleted_during_operation(client, db_session):
    """测试会话在操作过程中被删除"""
    session = create_session(client)
    
    # 模拟会话被删除
    delete_session(db_session, session.id)
    
    # 尝试发送消息
    response = client.post(
        f"/api/v1/ai-planning/sessions/{session.id}/messages",
        json={"content": "测试"},
    )
    
    assert response.status_code == 404


def test_concurrent_deletion(client, db_session):
    """测试并发删除"""
    session = create_session(client)
    
    # 并发删除
    import concurrent.futures
    with concurrent.futures.ThreadPoolExecutor(max_workers=10) as executor:
        futures = [
            executor.submit(
                client.delete,
                f"/api/v1/ai-planning/sessions/{session.id}"
            )
            for _ in range(10)
        ]
        results = [f.result() for f in futures]
    
    # 只有一个成功，其他返回 404
    success_count = sum(1 for r in results if r.status_code == 204)
    assert success_count == 1
```

---

## 实际案例：SSE 流式传输测试

### 背景

我们的项目中发现了以下 SSE 相关的 Bug：

1. **孤儿会话**：前端显示的会话在数据库中不存在
2. **刷新丢消息**：页面刷新后消息丢失
3. **Session 腐败**：事件日志写入失败导致主流程崩溃

### 测试设计

#### 1. 单元测试：EventLogWriter

```python
def test_event_log_writer_resilient_to_missing_table():
    """测试 EventLogWriter 在表不存在时的弹性降级"""
    from app.services.sse_event_log import EventLogWriter
    
    # Mock session factory，模拟表不存在
    mock_session = MagicMock()
    mock_session.add.side_effect = Exception("Table does not exist")
    mock_session_factory = MagicMock(return_value=mock_session)
    
    # 创建 writer
    writer = EventLogWriter(
        session_factory=mock_session_factory,
        session_id=1,
    )
    
    # 写入事件（应该静默失败）
    writer.write("status", {"phase": "thinking"})
    
    # 验证 writer 被禁用
    assert writer.enabled == False
    
    # 后续写入应该是 no-op
    writer.write("text_chunk", {"text": "hello"})
    assert mock_session.add.call_count == 1  # 只调用了一次
```

**测试点**：
- EventLogWriter 在表不存在时不会崩溃
- 第一次写入失败后，后续写入被跳过
- 主流程不受影响

#### 2. 集成测试：SSE 端点验证

```python
def test_chat_sse_validates_session_exists(client, db_session):
    """测试 chat SSE 端点验证会话存在"""
    response = client.post(
        "/api/v1/ai-planning/sessions/999/chat",
        json={"content": "测试"},
    )
    
    # 应该返回 404，而不是 200
    assert response.status_code == 404


def test_drafts_sse_validates_session_exists(client, db_session):
    """测试 drafts SSE 端点验证会话存在"""
    response = client.post(
        "/api/v1/ai-planning/sessions/999/drafts",
        json={"scenario_keys": ["test"]},
    )
    
    # 应该返回 404，而不是 200
    assert response.status_code == 404


def test_execute_sse_validates_session_exists(client, db_session):
    """测试 execute SSE 端点验证会话存在"""
    response = client.post(
        "/api/v1/ai-planning/sessions/999/execute",
        json={"draft_ids": [1]},
    )
    
    # 应该返回 404，而不是 200
    assert response.status_code == 404
```

**测试点**：
- SSE 端点在启动流之前验证会话存在
- 会话不存在时返回 404，而不是 200
- 前端能正确处理 404 错误

#### 3. 集成测试：事件日志持久化

```python
def test_event_log_persisted_during_streaming(client, db_session):
    """测试事件日志在流式传输过程中持久化"""
    # 创建会话
    session = create_session(client)
    
    # 流式传输
    events = []
    response = client.post(
        f"/api/v1/ai-planning/sessions/{session.id}/chat",
        json={"content": "测试"},
    )
    
    # 验证事件日志
    event_logs = get_event_logs(db_session, session.id)
    assert len(event_logs) > 0
    
    # 验证事件类型
    event_types = [log.event_type for log in event_logs]
    assert "status" in event_types
    assert "text_chunk" in event_types
```

**测试点**：
- 事件日志在流式传输过程中正确写入
- 事件类型完整
- 事件顺序正确

#### 4. E2E 测试：页面刷新恢复

```python
@pytest.mark.browser_integration
def test_page_refresh_recovers_streaming_state(page):
    """测试页面刷新后恢复流式状态"""
    # 1. 创建会话并开始流式传输
    page.goto("http://localhost:5173/planning")
    page.click("button:has-text('新建会话')")
    page.fill("textarea", "帮我规划登录测试")
    page.click("button:has-text('发送')")
    
    # 2. 等待部分响应
    page.wait_for_selector(".assistant-message", timeout=10000)
    
    # 3. 刷新页面
    page.reload()
    
    # 4. 验证恢复
    page.wait_for_selector(".assistant-message", timeout=10000)
    
    # 5. 检查是否有恢复标记
    recovered = page.locator("text=已恢复").count()
    interrupted = page.locator("text=回复中断").count()
    
    # 应该有恢复标记或中断标记
    assert recovered > 0 or interrupted > 0
```

**测试点**：
- 页面刷新后能恢复流式状态
- 显示恢复标记或中断标记
- 用户能看到最新内容

---

## 测试设计总结

### 测试金字塔应用

| 层次 | 测试类型 | 测试重点 | 数量 |
|------|----------|----------|------|
| 底层 | 单元测试 | 独立函数、业务逻辑 | 最多 |
| 中层 | 集成测试 | API 端点、组件交互 | 中等 |
| 顶层 | E2E 测试 | 完整链路、用户场景 | 最少 |

### 测试维度应用

| 维度 | 测试重点 | 示例 |
|------|----------|------|
| 正常流程 | 完整业务流程 | 创建会话 → 发送消息 → 接收响应 |
| 边界条件 | 空值、最大值、特殊字符 | 空消息、超长消息、SQL 注入 |
| 异常流程 | 资源不存在、并发冲突 | 会话删除、并发操作 |

### 异步测试应用

| 场景 | 测试方式 | 示例 |
|------|----------|------|
| 异步函数 | pytest-asyncio | 测试 EventLogWriter |
| API 端点 | httpx.AsyncClient | 测试 SSE 端点 |
| 流式响应 | client.stream() | 测试 SSE 事件流 |
| 并发操作 | asyncio.gather() | 测试并发删除 |

### 实际经验

1. **测试驱动调试**：先写测试复现问题，再修复根因
2. **边界优先**：优先测试边界情况和异常流程
3. **数据一致性**：验证前端和后端数据一致
4. **防御性编程**：在入口处验证，而不是在内部报错

---

## 附录：测试工具速查

### pytest 常用装饰器

```python
@pytest.mark.asyncio          # 异步测试
@pytest.mark.browser_integration  # 浏览器集成测试
@pytest.fixture               # 测试夹具
@pytest.mark.parametrize       # 参数化测试
```

### pytest 帮助函数

```python
pytest.raises(Exception)      # 测试异常
pytest.warns(Warning)         # 测试警告
pytest.approx(value)          # 浮点数近似比较
```

### Mock 工具

```python
from unittest.mock import MagicMock, AsyncMock, patch

mock = MagicMock()            # 同步 Mock
async_mock = AsyncMock()      # 异步 Mock
with patch("module.func") as m:  # 上下文管理器 Mock
    m.return_value = "mocked"
```
