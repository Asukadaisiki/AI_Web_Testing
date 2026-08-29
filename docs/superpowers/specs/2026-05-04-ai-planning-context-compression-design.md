# AI Planning Context Compression & Subagent Architecture

Date: 2026-05-04
Status: draft

## Problem Statement

3 interconnected issues in the AI planning flow:

1. **plan_json 被覆盖** — `stream_planning_message()` 在每轮对话结束时无条件写入 `plan_json`，followup 轮次将已有 plan 覆盖为 None
2. **消息体膨胀** — `explore_page`（~570KB）和 `explore_flow`（~741KB）的完整结果存入 `structured_payload_json`，并被全文注入 ReAct 对话上下文，导致单条消息 payload 1.78MB，GET session API 响应 4.4MB
3. **JSON 解析连续失败** — 上下文过长时 AI 输出非 JSON 文本，`_parse_llm_response` 重试 3 次后 fallback，降级体验差

Root cause chain: 重工具结果未压缩 → 上下文膨胀 → AI 输出质量下降 → JSON 解析失败 → 降级体验

## Solution Overview

**分级处理架构**：重工具（explore_page / explore_flow / capture_page_session）走 Subagent 压缩通道；轻工具（get_project_info / create_project）保持内联。

- Plan 覆盖 → 一行 guard 修复
- 消息体膨胀 → Subagent 压缩 + 消息表存摘要不存原始
- JSON 解析失败 → JSON repair 预处理 + 上下文压缩根治

## Data Model

### New table: `ai_planning_tool_results`

```sql
CREATE TABLE ai_planning_tool_results (
    id              SERIAL PRIMARY KEY,
    session_id      INTEGER NOT NULL REFERENCES ai_planning_sessions(id),
    message_id      INTEGER REFERENCES ai_planning_messages(id),  -- nullable: 消息入库后回填
    tool_name       VARCHAR(100) NOT NULL,
    raw_result_json JSON,          -- 原始结果（调试用，可配置关闭）
    summary_json    JSON NOT NULL, -- 压缩后的结构化摘要
    created_at      TIMESTAMP DEFAULT NOW()
);
```

### `ai_planning_messages.structured_payload_json` 改动

工具调用消息的 result 从完整数据改为摘要引用：

```json
// 之前（~570KB）
{ "type": "tool_call", "tool": "explore_page", "params": {...},
  "result": { "url": "...", "elements": [/* 数百条完整 DOM 元素 */] } }

// 之后（~2KB）
{ "type": "tool_call", "tool": "explore_page", "params": {...},
  "result_summary": {
    "url": "https://...",
    "page_title": "Automation Exercise",
    "element_counts": {"buttons": 12, "inputs": 8, "links": 45},
    "key_elements": [/* 仅保留有稳定 selector 的关键元素，≤30 条 */]
  },
  "tool_result_id": 42 }
```

### 工具结果生命周期

```
stream_planning_turn() 中:
  tool 执行 → raw_result (内存) → Subagent 压缩 (内存) → 摘要注入上下文
                                            ↓
                                     raw + summary 暂存于 AIPlanningToolCall.result

stream_planning_message() 中 (stream 结束后):
  遍历 response.tool_calls:
    1. 创建 AIPlanningMessage (flush → 获得 message_id)
    2. 对 HEAVY_TOOLS: 创建 AIPlanningToolResult (raw + summary, 回填 message_id)
    3. AIPlanningMessage.structured_payload_json 存摘要（不存 raw）
```

Subagent 调用使用同步 `httpx.Client`，与 `_stream_planning_llm` 相同的模式，在 `stream_planning_turn` 生成器内直接调用。

## Backend Architecture

### Subagent Runner

重工具执行后 → raw_result 存入 `ai_planning_tool_results` → Subagent LLM 调用压缩 → 摘要注入对话上下文。

| | 主 ReAct | Subagent |
|---|---|---|
| 任务 | 理解需求 → 决策 → 生成方案 | 压缩 DOM 数据为测试关键信息 |
| 输入 | 对话历史 + 工具摘要 | 单个工具的 raw_result |
| 输出 | ReAct JSON（action/plan） | 结构化摘要 JSON |
| 上下文长度 | 可能很长 | 极短 |

Subagent 模型：与主 ReAct 相同模型（可通过配置覆盖），短上下文、聚焦 prompt。

### Heavy vs Light tools

```python
HEAVY_TOOLS = {"explore_page", "explore_flow"}  # ~570KB / ~741KB per call
LIGHT_TOOLS = {"get_project_info", "create_project", "capture_page_session"}  # <1KB per call
```

### Subagent output schema

**explore_page → 页面摘要**
```json
{
  "page_title": "Automation Exercise",
  "url": "https://automationexercise.com/",
  "navigation": ["Products", "Cart", "Login"],
  "element_counts": {"buttons": 12, "inputs": 3, "links": 45},
  "key_elements": [
    {"role": "link", "text": "Products", "selector": "a[href='/products']"},
    {"role": "button", "text": "Login", "selector": "a[href='/login']"}
  ],
  "forms": [
    {"purpose": "login", "fields": [
      {"label": "Email", "selector": "input[data-qa='login-email']"},
      {"label": "Password", "selector": "input[data-qa='login-password']"}
    ]}
  ]
}
```

**explore_flow → 流程摘要**
```json
{
  "flow_title": "登录 → 商品筛选 → 加购",
  "steps": [
    {"url": "...", "page_title": "首页", "key_action": "click Login link"},
    {"url": "...", "page_title": "登录", "key_elements": [...]}
  ],
  "critical_selectors": ["input[data-qa='login-email']", "button[data-qa='login-button']"]
}
```

### Context injection logic change

`test_planning_agent.py` 行 449-452：

```python
if tool_name in HEAVY_TOOLS:
    compressed = run_compression_subagent(tool_name, parsed_result, settings)
    conversation.append({
        "role": "system",
        "content": f"工具 {tool_name} 返回摘要：{json.dumps(compressed, ensure_ascii=False)}"
    })
else:
    conversation.append({
        "role": "system",
        "content": f"工具 {tool_name} 返回结果：{tool_result_text}"
    })
```

### SSE 事件调整

`tool_call_end` 事件（行 443）对重工具仅推送 `result_summary`，不推送 `result`。

### plan_json 覆盖修复

`ai_planning.py` 行 244-248：

```python
# 之前：无条件覆盖
plan_dict = response.plan.model_dump(...) if response.plan is not None else None
if plan_dict is not None:
    plan_dict["_page_results"] = ...
planning_session.plan_json = plan_dict  # 可能为 None

# 之后：仅在有新 plan 时更新
if response.plan is not None:
    plan_dict = response.plan.model_dump(mode="json")
    plan_dict["_page_results"] = _extract_raw_page_results(response.tool_calls)
    planning_session.plan_json = plan_dict
```

### JSON 解析强化

`_extract_json_object()` 增强：

1. **JSON repair 预处理** — 修复尾部逗号、补全未闭合括号、移除 markdown 前缀后缀
2. **更宽松的提取** — 多级 fallback：先尝试匹配 JSON 对象 `{}`，失败再尝试匹配代码围栏，再失败用启发式修复
3. **根因缓解** — 上下文压缩后，AI 输出非 JSON 的概率显著降低

```python
def _repair_json_text(text: str) -> str:
    """Repair common AI JSON output errors before parsing."""
    # 移除 markdown 围栏
    text = _strip_markdown_fences(text)
    # 移除尾部逗号
    text = re.sub(r',\s*}', '}', text)
    text = re.sub(r',\s*]', ']', text)
    # 补全未闭合的括号
    text = _balance_brackets(text)
    return text
```

## Frontend Changes

### 思考过程按需展开

`AITestPlanningPanel.tsx` 中 `_thinkingContent` 渲染：

- 默认折叠为一行提示：`💭 思考中…（展开）`
- 用户点击展开完整内容
- 超过 500 字自动截断，显示 `展开全部（{N} 字）`

### 工具结果摘要化

`turn_type: "tool_call"` 消息：
- 默认显示摘要行：`🔧 explore_page — 已采集 45 个可交互元素`
- 对重工具，`_phaseMessage` 包含摘要信息而非原始数据量
- 点击可展开查看完整结果

### 兼容性

SSE 事件格式不变，仅数据量缩小。前端 `handleStreamEvent` 逻辑无需改动，折叠逻辑在渲染层处理。

## Files Changed

| 层 | 文件 | 改动 |
|----|------|------|
| DB | `backend/app/models/ai_planning_tool_result.py` | 新模型 |
| DB | Alembic migration | 新表 |
| 后端 | `backend/app/services/ai_planning.py` | plan 覆盖修复 + 工具结果存摘要 |
| 后端 | `backend/app/ai/test_planning_agent.py` | Subagent 压缩通道 + JSON repair + 上下文注入改造 |
| 前端 | `frontend/src/components/AITestPlanningPanel.tsx` | 思考折叠 + 工具结果摘要展示 |
| 前端 | `frontend/src/types/api.ts` | `result_summary` 字段类型 |

## Risks & Mitigations

1. **Subagent 增加延迟** — 每次重工具调用多一次 LLM 往返。缓解：Subagent 上下文极短（~2KB prompt），预计 2-5s 额外延迟；可配置开关，调试时可关闭压缩
2. **摘要丢失细节** — 关键 selector 可能在压缩中丢失。缓解：Subagent prompt 明确要求保留 `data-qa` / `id` / `name` 等测试属性；原始数据保留在 `ai_planning_tool_results` 表中可追溯
3. **兼容性** — 已有消息的 `result` 字段仍然是大 JSON。缓解：不做数据迁移，前端向后兼容处理

## Configuration

```env
# Subagent 开关（调试时可关闭，直接走算法截断）
AI_PLANNING_SUBAGENT_ENABLED=true
# Subagent 超时
AI_PLANNING_SUBAGENT_TIMEOUT_MS=60000
```
