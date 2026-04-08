# AI 对话历史恢复 + 完整测试流程设计

> 日期: 2026-04-08
> 状态: Draft

## 背景

当前 AI 对话页面存在两个问题：
1. 切换页面后聊天记录丢失（前端 state 随组件卸载销毁）
2. AI 只能生成 DSL 草案，无法完成"保存用例 → 执行测试 → 展示结果"的完整链路

用户提供测试数据（The Internet Login Page）用于验证完整流程。

---

## 一、顶部会话切换器 + 历史恢复

### 前端改动

1. **AITestPlanningPanel 顶部添加会话选择器**
   - 组件挂载时调用 `GET /api/v1/ai-planning/sessions` 加载会话列表
   - Select 下拉框，选项显示 `app_under_test`（无名称时显示创建时间）
   - 右侧"新建会话"按钮
   - 选中会话后调用 `GET /api/v1/ai-planning/sessions/{id}` 加载完整状态（transcript + requirements + plan + drafts）

2. **状态持久化策略**
   - 当前页面内：React state 始终保留
   - 切走再切回来：`useEffect` 在 mount 时自动恢复上次选中的会话（sessionId 存 localStorage）
   - 每次发消息/收到响应后，transcript 自动同步到后端（已有逻辑）

### 后端改动

3. **新增列表接口**（如不存在）
   - `GET /api/v1/ai-planning/sessions` — 返回当前用户的会话列表（id、名称、状态、创建时间）
   - 现有 `GET /api/v1/ai-planning/sessions/{id}` 已返回完整数据，无需改动

---

## 二、AI 完整测试流程 — 状态机扩展

### 当前状态机

```
collecting → plan_ready → drafts_ready
```

### 扩展后状态机

```
collecting → plan_ready → drafts_ready → reviewing → saving → executing → completed
```

### 每个阶段的对话行为

| 状态 | AI 行为 | 用户操作 |
|------|---------|---------|
| `collecting` | 收集需求，追问缺失 slot | 回答问题 |
| `plan_ready` | 展示测试计划，问"是否确认？" | 确认/修改 |
| `drafts_ready` | 展示 DSL 草案，问"请审阅，确认后我将保存" | 审阅/确认 |
| `reviewing` | 用户已确认草案，问"是否保存为测试用例？" | 勾选用例，确认保存 |
| `saving` | 调用 `POST /api/v1/cases` 保存，汇报结果 | 等待 |
| `executing` | 问"是否立即执行？" | 确认执行 |
| `completed` | 调用 `POST /api/v1/cases/{id}/execute`，展示摘要+报告链接 | 查看报告 |

**关键原则：每一步都由 AI 主动询问、用户确认后才继续。**

### 后端改动

- `test_planning_agent.py`：扩展 ReAct agent 对话阶段，新增 `reviewing` / `saving` / `executing` / `completed` 状态处理
- `saving` 阶段：调用已有的 `services/cases.py` 的 `create_case()` 函数
- `completed` 阶段：调用已有的 `services/executions.py` 的 `execute_case()` 函数

### 前端改动

- DSL 草案区域增加勾选框
- `saving` / `executing` 阶段消息增加加载状态
- `completed` 阶段展示执行摘要 + 报告页链接

---

## 三、前端消息展示与交互

### 对话消息类型扩展

当前消息类型为 `user` / `assistant` 纯文本。新增以下富消息类型，通过 `message_type` 字段区分：

### 1. DSL 草案审阅消息（`reviewing` 阶段）

```
┌─────────────────────────────────┐
│  以下测试用例已生成，请审阅：       │
│                                 │
│  ☑ 正向登录成功                   │
│    goto → input → input → click  │
│    → assert_text → assert_text   │
│                                 │
│  ☑ 验证页面元素可见               │
│    goto → wait_for → assert_text │
│                                 │
│  [确认保存]  [修改]              │
└─────────────────────────────────┘
```

### 2. 保存结果消息（`saving` 阶段）

```
┌─────────────────────────────────┐
│  ✅ 已保存 2 个测试用例            │
│    - 正向登录成功 (case #12)      │
│    - 验证页面元素可见 (case #13)   │
│                                 │
│  是否立即执行这些用例？ [执行] [稍后]│
└─────────────────────────────────┘
```

### 3. 执行结果摘要消息（`completed` 阶段）

```
┌─────────────────────────────────┐
│  测试执行完成                     │
│                                 │
│  正向登录成功 — ✅ 通过 (6/6步)    │
│    耗时 3.2s                     │
│    [截图缩略图]                   │
│                                 │
│  查看完整报告 → [前往报告页]       │
└─────────────────────────────────┘
```

### 实现方式

- 后端在 `assistant_message` 中返回结构化 JSON（`message_type` 字段区分类型）
- 前端根据 `message_type` 渲染不同的消息卡片组件
- 按钮点击回调触发对应的 API 调用（保存/执行），发送对应消息给后端

---

## 四、对接已有 API 的具体调用方式

### 保存用例（`saving` 阶段）

```python
# 后端 test_planning_agent.py 中
# 遍历用户勾选的 drafts，逐个调用已有的 case 创建服务
for draft in selected_drafts:
    case = await create_case(
        db=db,
        project_id=session.project_id,
        name=draft.name,
        description=draft.description,
        base_url=draft.dsl.base_url,
        input_contract=draft.dsl.input_contract,
        output_contract=draft.dsl.output_contract,
        steps=draft.dsl.steps,
        actor_user_id=current_user_id
    )
    saved_cases.append(case)
```

### 执行用例（`completed` 阶段）

```python
# 遍历已保存的 case，逐个调用已有的执行服务
for case in saved_cases:
    run = await execute_case(
        db=db,
        case_id=case.id,
        base_url=case.dsl.get("base_url"),
        actor_user_id=current_user_id
    )
    execution_ids.append(run.id)
```

### 返回执行摘要

```python
# 等待执行完成后查询结果
for run_id in execution_ids:
    run = await get_execution_detail(db, run_id)
    # 构建 summary：status、passed/failed steps、duration、screenshot_url
```

### 关键原则

- 不新建任何执行层代码，纯粹调用 `services/cases.py` 和 `services/executions.py`
- 后端 AI agent 内部直接调用 service 层函数（不走 HTTP，直接函数调用）
- 前端只负责展示，不直接调 cases/executions API

---

## 验证数据

使用 The Internet Login Page 测试数据验证完整流程：

```
app_under_test: The Internet - Login Page
entry_url_or_page: https://the-internet.herokuapp.com/login
core_user_flow:
  - 打开登录页
  - 输入用户名 tomsmith
  - 输入密码 SuperSecretPassword!
  - 点击 Login 按钮
  - 跳转到 secure 页面
  - 校验登录成功提示和 Logout 按钮
test_data_or_account:
  username: tomsmith
  password: SuperSecretPassword!
scope_limits:
  - 仅测试正向登录成功链路
```

预期 DSL 输出：
```json
{
  "steps": [
    {"action": "goto", "value": "https://the-internet.herokuapp.com/login"},
    {"action": "input", "target": "username field", "value": "tomsmith"},
    {"action": "input", "target": "password field", "value": "SuperSecretPassword!"},
    {"action": "click", "target": "Login button"},
    {"action": "assert_url_contains", "value": "/secure"},
    {"action": "assert_text", "target": "success message", "value": "You logged into a secure area!"},
    {"action": "assert_text", "target": "Logout button", "value": "Logout"}
  ]
}
```
