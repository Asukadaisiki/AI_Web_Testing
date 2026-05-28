# 执行日志

用于沉淀每次任务实际做了什么，方便后续追溯、复盘和回答一致化。

## 记录规则

- 每次处理需求后按时间倒序追加一条记录。
- 记录"目标、操作、结果、验证、后续"，避免只写结论。
- 如果执行过程中发现缺陷，同时在 `docs/bug-log.md` 追加对应条目并互相引用。
- 最新的记录优先放到最上面，方便阅读。

## 阶段总览

| 阶段 | 日期 | 主题 | 关键成果 |
|------|------|------|----------|
| M1 基础 | 03-28~03-31 | 认证、CRUD、Suite 下线、AI Planning 雏形 | 认证基线 + 用例 CRUD + AI 对话面板 |
| M2 用例生成 | 04-03~04-17 | ReAct Agent、流式执行、定位器系统、DOM 感知 | ReAct loop + WebSocket 流式 + 4 层定位器 |
| M2 执行闭环 | 04-20~04-28 | DOM-aware DSL、VLM 两阶段、Explorer-Judge、评分系统 | 完整 plan→execute→analyze 闭环 |
| M2 前端重构 | 04-25~04-26 | NotebookLM 布局、E2E 手动测试、Explorer-Judge 架构 | 三栏布局 + 缺陷发现范式 |
| E2E 调优 v1 | 05-02~05-07 | 页面探索、定位器回退链、text_parent_chain、AI 质量循环 | 26 commits, 96% 步骤通过率 |
| E2E 调优 v2 | 05-10~05-12 | DeepSeek 温度/thinking 优化、提示词修复、11 项架构优化 | 544 tests, 0 failures |
| 架构重构 | 05-14~05-17 | 主路径 v2 A11y 管线、dead code 清理、DSL 生成链路修复 | 491 tests, −3.1K 行, A11y CDP 100x 快 |
| 链路修复 | 05-25 | DSL 生成链路 7 层 bug 修复（Bug A→G） | 543 tests, 16 新增测试 |
| 孤儿数据清理 | 05-25 | 全面清理代码库中的孤儿数据 | 删除 14 项孤儿数据 |
| 数据校验修复 | 05-25 | 数据传递与校验全面扫描修复 | 修复 19 项问题 |
| 变量占位符修复 | 05-28 | 分段生成 input_contract 自动提取 | 修复 ${email} 未替换问题 |
| A11y Tree 全面切换 | 05-28 | 封杀 DOM 路径，只使用 a11y tree | 500 tests, 4 项核心修复 |
| explore_flow DSL 格式支持 | 05-28 | 支持 DSL 格式步骤传入 explore_flow | 500 tests, 修复页面探索不完整 |
| 跨段变量命名权威 | 05-28 | Planning agent 输出 scenario.variables，segment prompt 注入命名字典 | 504 tests, 4 新增聚焦测试 |
| explore_flow 遮挡恢复 | 05-28 | _execute_flow_actions / capture_browser_session 接入 click_with_precheck | 504 tests, cartModal 不再杀掉探索 |
| 完整探索数据 + 用户上下文注入 | 05-29 | _load_a11y_nodes 合并所有 explore 记录 + user_context 注入 segment prompt | 504 tests, DSL 生成器看到完整元素和原始需求 |

---

## 2026-05-28 | 跨段变量命名权威（scenario.variables）

**目标**：堵住 `generate_segmented_case_draft` 并行调用各 segment LLM 时段间变量名失配的洞——例如 S1 生成 `capture_text context_key=product_a_name`，S2 独立生成 `assert_text value="${item_a_name}"`，运行时 `_substitute_variables` 找不到 key，字面量 `${item_a_name}` 残留到断言/输入。

**操作**：
1. `schemas/ai_planning.py`：新增 `AIPlanningScenarioVariable`（context_key/description/source/capture_in_state），挂到 `AIPlanningScenario.variables`
2. `ai/test_planning_prompts.py`：在 JSON 模板 + 规则段加 variables 字段说明，要求 AI 列出所有跨段共享变量及其 capture 段
3. `schemas/dsl.py`：`GenerateDslRequest` 新增 `scenario_variables: list[dict] | None` 透传字段
4. `ai/dsl_generator.py`：新增 `_format_scenario_variables_for_prompt(scenario_variables, current_state=...)` 把变量按 input/own_capture/other_capture 分组渲染；`_build_segment_prompt` 注入；`generate_segmented_case_draft` 接收新参数并下传给每个 segment
5. `services/ai_planning.py`：`generate_planning_drafts` 从 `scenario["variables"]` 取出，分别注入 segmented 和 single-segment 路径的 payload
6. `tests/unit/test_dsl_generator.py`：新增 `TestScenarioVariablesInSegmentPrompt` 4 个聚焦测试

**结果**：
- 每个 segment 看到的 prompt 包含 `## Scenario variables — naming authority` 小节，列出全部 `${context_key}` 及其责任段
- 本段持有的 capture 变量标"本段必须用 capture_text 写入"，外段的标"do NOT re-capture"
- 504 单元测试通过（原 500 + 4 新增）

**验证**：
- `_build_segment_prompt` 直接调用：input 变量、capture 变量、空 variables 三个分支
- `generate_segmented_case_draft` mock LLM 调用：确认 2 个并行 segment 都拿到同一份变量字典，且只有 S1 段被标为 capture 责任段

**后续**：
- 还可以加生成后校验：扫描 merged_steps 中所有 `${var}`，若不在 scenario_variables 也不在 `input_contract` 中则降级告警/重生段；这层兜底等用回归 prompt 跑过一次再决定是否补
- 现有 `_extract_input_contract_from_steps` 会把 captured 变量也纳入 input_contract，理论上不影响执行（runtime_context 会覆盖 input_values），但语义不准确；可在后续做拆分

---

## 2026-05-28 | explore_flow DSL 格式支持

**目标**：修复 `explore_flow` 不支持 DSL 格式步骤的问题，导致页面探索不完整，AI 生成的 DSL 缺少 input 步骤。

**操作**：
1. 定位问题：AI 调用 `explore_flow` 时传入 DSL 格式步骤 `{"action": "goto", "target": "https://..."}`，但 `_collect_flow_a11y` 只支持 `{"url": "...", "actions": [...]}` 格式
2. 根因分析：DSL 格式步骤没有 `url` 和 `actions` 字段，导致步骤被跳过
3. 修复方案：新增 `_normalize_flow_step()` 函数，将 DSL 格式步骤转换为 explore 格式
4. goto -> url, click/input/wait_for -> actions

**结果**：
- 500 单元测试全部通过
- explore_flow 现在支持两种格式的步骤

**验证**：
- DSL 格式 `{"action": "goto", "target": "https://..."}` -> `{"url": "https://..."}`
- DSL 格式 `{"action": "click", "target": "Polo"}` -> `{"actions": [{"action": "click", "target": "Polo"}]}`

**后续**：用户可重新测试 E2E 场景，验证 explore_flow 是否正确探索所有页面。

---

## 2026-05-28 | A11y Tree 全面切换

**目标**：封杀所有 DOM 元素路径，让 AI 只使用 a11y tree 进行元素定位，解决 VLM 调用过多和断言失败问题。

**操作**：
1. 新增 `format_a11y_nodes_for_prompt()` 函数，格式化 a11y tree 为 `role="name"` 格式
2. 修改 `_build_segment_prompt()` 移除 `elements` 和 `page_elements` 参数，只保留 `a11y_nodes`
3. 修改 `generate_segmented_case_draft()` 移除 `page_elements_by_state` 参数，只保留 `a11y_nodes_by_state`
4. 更新 prompt 规则：target 必须使用 `button="Login"` 格式，禁止 XPath/CSS 选择器
5. 新增 `_clean_variable_format()` 函数，清理 `${email}=value` 错误格式为 `${email}`
6. 所有 `to_contain_text()` 调用添加 `normalize_whitespace=True` 参数

**结果**：
- 500 单元测试全部通过
- 封杀 DOM 路径，只保留 a11y tree
- 修复变量格式问题
- 修复断言空白字符匹配问题

**验证**：
- `test_dsl_generator.py` 37 tests passed
- 全量单元测试 500 passed, 6 warnings

**后续**：用户可重新测试 E2E 场景，验证 VLM 调用次数是否减少，断言是否正常工作。

---

## 2026-05-28 | 分段生成 input_contract 自动提取

**目标**：修复用户提供的测试数据在执行时未被替换到 DSL 步骤中的问题。

**操作**：
1. 定位问题：Session 247 Draft 176 的 `input_contract` 为空数组，但步骤中使用了 `${email}` 和 `${password}` 占位符
2. 根因分析：`generate_segmented_case_draft` 函数硬编码 `"input_contract": []`
3. 修复方案：新增 `_extract_input_contract_from_steps` 函数，从步骤的 `${...}` 占位符自动提取并生成 `input_contract`
4. 添加单元测试覆盖新函数

**结果**：
- 37 单元测试通过
- 自动提取 email/password 变量并推断类型

**验证**：
- 模拟用户输入 `账号：Xjy13302412005@outlook.com，密码：123456` 正确解析
- 变量映射：`email` → `Xjy13302412005@outlook.com`，`password` → `123456`

**后续**：用户可重新测试相同场景，验证变量替换是否正常工作。

---

## 2026-05-25 | 数据传递与校验全面扫描修复

**任务**：全面扫描项目的数据传递和校验情况，发现并修复所有问题。

**扫描范围**：
- 后端 (backend/app/) 所有 Python 文件
- 前端 (frontend/src/) 所有 TypeScript/React 文件

**发现并修复的问题**（共 19 项）：

### 高风险（3 项）
1. `batch_update_cases` 绕过项目成员权限检查 — 添加 `actor_user_id` 参数并在路由中传入 `current_user.id`
2. settings 路由缺少认证保护 — 为所有 settings 路由添加 `require_demo_user` 依赖
3. `require_demo_user` 缺少警告注释 — 添加 docstring 标记为开发/演示专用

### 中风险（8 项）
4. email 格式校验缺失 — 添加 `@` 格式校验
5. 项目重名异常处理缺失 — 添加 `ProjectConflictError` 并捕获 `IntegrityError`
6. DSL 反序列化未捕获 ValidationError — 添加 try/except 返回降级结果
7. `func.to_char` SQLite 不兼容 — 使用数据库方言检测适配不同数据库
8. 前端 CaseExecutionRequest 缺少 input_values — 添加 `input_values?: Record<string, string>` 字段
9. 前端 AIPlanningScenario 缺少字段 — 添加 `page_elements` 和 `flow_steps` 字段
10. 缺少 CORS 中间件 — 配置 `CORSMiddleware` 并添加 `cors_allow_origins` 配置项
11. 缺少全局请求速率限制 — 创建 `RateLimitMiddleware` 并添加配置项

### 低风险（8 项）
12. status_filter 缺少 Literal 约束 — 改为 `ExecutionStatus | None` 类型
13. page/page_size 缺少 Query 约束 — 添加 `ge=1` 和 `le=100` 约束
14. GenerateDslRequest 冗余 return 语句 — 删除不可达代码
15. 前端 GenerateDslMeta 字段不完整 — 添加 `active_governance_focus_reasons` 字段
16. 前端 AIPlanningTurnResponse 字段不完整 — 添加 `todo_list` 和 `execution_analysis` 类型和字段
17. LIKE 通配符未转义 — 转义 `%` 和 `_` 特殊字符
18. DSL case steps 无 max_length — 添加 `max_length=500` 约束
19. SSE 流泄露 traceback — 仅在 debug 模式下发送 traceback

**新增文件**：
- `backend/app/core/rate_limit.py` — 简单的内存速率限制中间件

**验证**：所有 19 项问题已修复

---

## 2026-05-25 | 孤儿数据全面清理

**任务**：清理代码库中所有类型的孤儿数据，包括导入但未实现、实现但未导入、引用但未实现、实现但未引用、定义但未实现、实现且定义但未引用的代码，以及无效字段、无效表、无效函数、无效文件、无效变量。

**分析范围**：
- 后端 (backend/app/) 所有 Python 文件
- 前端 (frontend/src/) 所有 TypeScript/React 文件
- 数据库模型定义
- API 路由定义
- 服务层实现
- 根目录测试工件

**删除项目**（共 14 项）：

### 高优先级（明确的孤儿数据）
1. `backend/app/services/projects.py` — 整个文件是死代码，被 `project_management.py` 完全取代
2. `backend/app/services/cases.py` 第124-127行 — return语句后的不可达死代码
3. `backend/app/services/dsl.py` `_ensure_retry_generation_exists` 函数 — 定义了但从未调用
4. `backend/app/services/cases.py` `list_cases` 函数 — 从未被路由调用，被 `list_cases_paginated` 取代
5. `frontend/src/components/StepList.tsx` — 从未被导入
6. `frontend/src/layouts/NotebookLMLayout.tsx` — 从未在路由中使用
7. `frontend/src/components/NotebookNav.tsx` — 只被孤立布局使用

### 中优先级（清理）
8. `backend/app/services/__init__.py` 中 `list_cases` 和 `list_accessible_projects` 的死重导出
9. `backend/app/api/routes/cases.py` 第24行冗余的 `get_project` 导入
10. `backend/app/schemas/__init__.py` 未使用的重导出块
11. 根目录测试工件：`test_brand_filter_cart`、`test_results.json`、`test_results_formatted.txt`

### 待确认项（用户确认删除）
12. `frontend/src/types/api.ts` 中 `SavedCaseResult.status` 字段 — 始终是字面量 "saved"，无信息量
13. `backend/scripts/` 目录 — 不属于主流流程
14. `tools/` 目录 — 与测试应用无关

**保留项目**：
- `hash_password` — 保留用于未来用户注册功能
- `LocatorAttemptLog` 模型 — 可能被 runner 运行时写入
- `get_dsl_generation_runtime_stats` — 调试工具
- `reset_dsl_generation_runtime_stats` — 测试工具
- `schemas/__init__.py` 便利重导出层 — 简化为仅保留模块声明

**验证**：所有删除操作已成功执行，文件系统验证通过

**影响**：
- 减少了代码库的维护负担
- 消除了潜在的混淆和误用
- 提高了代码库的整洁度和可维护性

---

## 2026-05-25 | DSL 生成链路 7 层 bug 修复（Bug A→G）

**任务**：用户复现 `DSL 生成失败：所有 1 个页面状态分段均未生成步骤` 错误。从 `backend/backend.log` 追踪定位错误归属并修复。

**根因分析**（按因果链排序）：
1. 用户报错出处：`dsl_generator.py:644` 抛出 `DslGenerationError`，提示"页面元素采集失败"但元素已采到 1136 个——错误消息误导。
2. 直接触发：`Segment S0 failed: <urlopen error [WinError 10060]>` — TCP 21 秒级超时连接 `api.deepseek.com`。
3. 即使网络通了也会失败：`scenario["flow_steps"]=[]` 走 single-segment 分支，1136 个 a11y 节点在 `ai_planning.py:561`→`dsl.py:147` 链路上被丢弃（`page_elements_by_state` 硬编码 `{}`）。
4. 上游：agent 5 轮安全帽耗尽 → fallback plan，原因是重复调用 `create_project` ×2、`explore_flow` ×2 浪费了 4 轮。

**修复**：

### Bug A — single-segment 路径下 a11y 数据丢失
- `schemas/dsl.py`：`GenerateDslRequest` 新增 `a11y_nodes_by_state` 字段
- `services/dsl.py`：`page_elements_by_state` 从 payload 读取，不再硬编码 `{}`
- `services/ai_planning.py`：单段分支按 page_state 分组 a11y_nodes_raw 后传入
- `dsl_generator.py`：`flow_steps=[]` 但有 elements 时自动按 page_states 迭代生成

### Bug B — LLM 调用无重试 + 错误消息误导
- 新增 `_urlopen_with_retry`（指数退避 1s→2s，2 次重试）+ `_is_transient_network_error`
- 新增 `DslGenerationNetworkError`，给出准确中文诊断
- `generate_segmented_case_draft` 末尾区分网络错误 vs 真正的"无元素"问题

### Bug C — agent 重复调用工具浪费安全帽轮次
- 新增 `_tool_call_signature` 规范化调用签名
- 工具执行前比对签名，命中重复时：注入警告 + 复用 prior result + 不扣 round

### Bug D — stream_planning_turn 把 Pydantic plan 当 dict 用
- `response.plan.model_dump(mode="json")` 替代直接 `.get()`

### Bug E — _log_dsl_cache_usage 被 governance 清理误删
- 恢复函数定义，加 `isinstance` 防御

### Bug F — LLM 生成 goto/assert_url_contains target↔value 错位
- 新增 `_normalize_llm_step`：激活 `_ACTION_ALIASES`，对 `goto/assert_url_contains` 自动把 target 搬到 value
- `_build_segment_prompt` 增加显式字段规则

### Bug G — assert_text 缺 value + 字段别名表未接入 normalizer
- 接入三张孤儿别名表 `_STEP_TARGET/VALUE/TIMEOUT_ALIASES`
- `assert_text` 特殊兜底：value 缺 + target 在 → target 移到 value，target 兜底 `"body"`
- 必填字段缺失时丢弃整步，避免单步拖垮整个 DSLCase
- `_build_segment_prompt` 按 action 类型枚举字段要求并给正反例

**新增测试**：16 个（TestIsTransientNetworkError 4 + TestUrlopenWithRetry 3 + network error wrapping 1 + a11y data flow 1 + TestToolCallSignature 7）

**验证**：543 passed（基线 505 → +16 新增 - 部分删除）

**链路总结**：Bug A→G 共 7 层，每修一个就暴露下一个。所有 bug 不是新增缺陷，是已存在但被前置失败掩盖的休眠问题。

---

## 2026-05-17 | 修复 AI 规划→DSL 生成链路 4 个 bug

**背景**：使用 `test_brand_filter_cart` 测试规格生成草案时，`explore_flow` 失败（Playwright 对 `<body>` 执行 `fill`），草案生成报 Pydantic `ValidationError`。

**操作**：

### Bug 1: 系统提示词缺少 `collected_info` → `entry_url_or_page` 提取不稳定
- JSON 模板新增 `collected_info` 对象（7 个需求字段）+ `assistant_message` + `todo_list`

### Bug 2: 语义定位器 `text` 策略匹配 `<body>` → `explore_flow` 填表失败
- `semantic.py`：`prefer_input=True` 时排除 `text`/`text_fuzzy` 策略
- `page_explorer.py`：`_execute_flow_actions` 新增标签验证；fill 前检查 tag 是否为 input/select/textarea

### Bug 3: 系统提示词缺少 `summary` → `_coerce_plan` 永远回退到 `_build_plan`
- JSON 模板新增 `summary` 字段；scenario 模板扩展 `flow_steps` 示例

### Bug 4: `base_url` 转空字符串 + 空 steps 报 Pydantic 错误
- `dsl_generator.py`：`base_url = payload.base_url or None`；model_validate 前加前置校验

**验证**：138 passed / 0 failed（语义/定位器/DSL/探索器相关）

---

## 2026-05-16 | E2E 手动测试 — 品牌筛选购物车

**任务**：对 `test_brand_filter_cart` 执行完整 E2E 链路测试，发现并修复 3 个 bug。

**操作**：
1. 创建 AI 规划会话 (#224)，AI 成功生成 4 个测试场景
2. DSL 生成失败（page_elements 为空）→ 改用直接创建测试用例
3. 创建 Case #97 并执行，步骤 6 失败（登录账号不存在）
4. 注册新账号后重新执行 → 24 步全部通过
5. 继续优化 DSL（#98~#100），最终 Case #100 20 步全部通过

**发现并修复的 Bug**：
- Bug #1: `planning_tools.py` — `AIPlanningSession` import 在条件块内导致 UnboundLocalError
- Bug #2: `planning_tools.py` — `explore_page` 中 networkidle 等待无 try-except
- Bug #3: `playwright_runner.py` — `capture_text` 步骤的 evidence value 始终为 null

**验证**：完整购物车流程 20 步 pass

---

## 2026-05-15 | 代码清理 + 测试补充 + E2E 重设计

**背景**：主路径 v2 A11y 管线 17 个任务已完成（491 tests / 0 failures），核对设计文档后发现 4 类遗留。

**操作**：

### Part 1: 重构 `services/dsl.py` — 删除 `generate_case_draft`
- import 从 `generate_case_draft` 改为 `generate_segmented_case_draft`
- 删除 `_select_governance_focus_reasons` 的 DB 查询

### Part 2: 删除 `ai_planning_max_react_rounds`
- 该配置项在 4 个文件做 plumbing，但**没有任何代码读取它**

### Part 3: 删除 `collect_interactable_elements` 死代码
- 删除约 283 行（含 `_discover_interactive_elements`、`_verify_locators_on_page` 等）
- 修复 `_filter_a11y_nodes` 中 CDP `role` 字段为 dict 格式的处理

### Part 4: 新建 `test_preflight_regen.py`（16 tests）
### Part 5: 新建 `test_main_path_v2_e2e.py`（8 tests）

**验证**：505 passed / 0 failed；浏览器集成测试 3 passed

---

## 2026-05-15 | 主路径 v2 全量实施 — 17/17 任务完成

**背景**：用户反馈 4 个痛点——探索工具无缓存 / AI 草案质量低 / 定位器选择差 / 单轮思考 10 分钟。大量机制"已设计但主流程不触发"。

### 阶段 1: 删除 dormant 分支（2026-05-14）
- `multi_agent.py`（527 行）、compression subagent（290 行）、`accessibility.py`（158 行）、pre-exec review（100 行）、VLM 重复触发（43 行）、调试脚本（122 行）、过时设计文档（1058 行）
- 净结果：**544 tests / 0 failures，−1498 行**

### Brainstorm + 实验（8 个细节决策）
- A11y 树 vs DOM 全量对比：字节收益 22-38x↓，速度 100-250x 快
- 15 个锁定的细节决策（DSL target 类型、Cache key、Preflight 重生策略等）

### PR-1：地基 — A11y 探索器 + 默认项目 + DB 缓存（7 tasks）
- 默认项目 auto-create、A11y 角色过滤器、CDP 快照、程序化关键字提取、explore_page 切 A11y、DB 缓存读路径

### PR-2：数据流 + Preflight 重生 + 删死代码（6 tasks）
- dict 端到端、preflight 1:N candidates、单段重生、Scenarios schema 瘦身、删 governance 系统 520 行

### PR-3：ReAct 瘦身 + 配置清理 + 死代码扫尾（4 tasks）
- 系统提示词 186→30 行、safety_cap 30→5、cache 进度清单注入、删旧 DOM 收集代码

**最终状态**：491 tests / 0 failures，16 commits，+3.5K / −6.6K 行（净 −3.1K），17/17 tasks complete

---

## 2026-05-14 | 架构清理阶段 1 — 删除 dormant 分支与冗余 LLM 调用

**操作**：
1. 删除 multi_agent 路径（527 行）
2. 删除压缩 subagent（290 行）
3. 删除 accessibility 模块（158 行 + 256 行测试）
4. 删除 pre-exec review（100 行）
5. 去掉 VLM 重复触发（43 行）

**验证**：526 单测通过，−1498 行

---

## 2026-05-13 | 进展汇报 + PPT 规划

- 梳理项目最新进展并向用户汇报当前状态
- 为项目展示 PPT 梳理内容规划方案

---

## 2026-05-12 | E2E 测试验证 + 草案质量分析 + 多轮修复

**背景**：使用 `test_brand_filter_cart` 对平台进行 E2E 测试，持续发现问题并修复。

### Phase 1: 草案质量问题发现与分析
- AI 跳过登录页、页面状态映射错误、actions 泛化、数量修改步骤缺失、candidates 为空

### Phase 2: 提示词与消息修复
- 删除"可以直接 generate_plan"逃逸口、安全网消息修正、系统提示词矛盾修正

### Phase 3: Guard 增强
- 页面覆盖度检查移入 Guard（coverage < 0.5 时阻止 generate_plan）

### Phase 4: Few-shot 自愈系统
- `DSLAntiPattern` 模型 + 自动采集 + 注入负面示例

### Phase 5: DSL 生成器 thinking mode
- deepseek-v4-pro + effort=max

### Phase 6: explore_flow actions 消歧
- `_check_action_disambiguation` 检测泛化 target

### Phase 7: 相对 URL 解析修复
- 多层 fallback 提取 base_url

**验证**：thinking mode 生效、Guard 覆盖度检查生效、actions 消歧生效

---

## 2026-05-12 | 执行架构全面优化 — 11 项问题修复

**操作**：

### 严重问题
1. **streaming 函数 NameError** — `save_and_execute_selected_drafts_streaming()` 引用未定义 `db_session`
2. **Explorer Runner console/network 采集** — 添加事件监听器

### 中等问题
3. **generate_plan 守卫轮次保护** — `guard_continue_count` 超过 5 次后强制生成方案
4. **页面探索覆盖度检查** — 新增 `_check_page_coverage`
5. **legacy 路径 postcondition 检查**
6. **变量替换未匹配警告**

### 轻微问题
7. 多语言动态元素发现
8. `collect_flow_elements` base_url 参数化
9. `text_parent_chain` 多级链支持
10. 无障碍树 dialog/modal 角色
11. `playwright_runner` 添加 logger

**验证**：544/544 单元测试通过

---

## 2026-05-10 | AI 配置优化 — 禁用 DeepSeek thinking 模式 + 按场景设置 temperature

**背景**：综合 BUG-081/069/065/054 等"AI 不遵循提示词"问题。

**操作**：
1. 移除 DeepSeek 的 thinking mode（仅保留 GLM）
2. 按场景设置 temperature：DSL generator 0.0、flash 0.0、Planning 0.1、Judge 0.0

**验证**：542/544 通过（2 个预存失败与改动无关）

---

## 2026-05-06 ~ 2026-05-07 | AI Agent 测试用例质量提升 — 三层修复 + 自动回归循环

**目标**：反复用 test_brand_filter_cart 测试 AI agent，直到步骤通过率达 80%+。

**核心修复（按层分类）**：

### AI 决策层
1. BUG-069: 系统提示词 ask_user 确认门移除
2. BUG-068: 压缩子代理优先保留交互元素
3. BUG-066: core_user_flow list→编号文本归一化
4. 系统提示词 7 条强制规则

### DSL 生成层
5. BUG-077: goto/assert_url_contains 的 candidates/postconditions 剥离
6. BUG-078: click/wait_for/capture_text 的 spurious value 字段剥离
7. BUG-070: DSL generator thinking mode reasoning_content fallback
8. BUG-065: capture→assert 规则
9. BUG-076: Surrogate Unicode 字符清理

### 探索数据层
10. BUG-067: explore_flow 相对 URL 解析
11. 元素视觉分组 + 隐藏元素保留 + 选择器稳定性评分

### 执行定位器层
12. text_parent_chain 新定位器
13. BUG-071~073: text_parent_chain 正则/ancestor/exact 修复
14. BUG-074: 执行流程重构 — 语义链优先
15. 步骤超时 2.5 分钟

**执行结果对比**：

| 指标 | 修复前（Session 118） | 修复后（Session 155） |
|------|----------------------|----------------------|
| AI 首轮动作 | ask_user "信息够吗" | explore_page → capture_session |
| DSL 步骤数 | 10 | 42（完整流程） |
| assert_text 数量 | 0 | 9 |
| 步骤被删 | 10 | 0 |
| 执行通过率 | 0/0（草案无法执行） | 42/42 (100%) |

---

## 2026-05-05 | 四项修复

### capture_page_session CSS 选择器支持 + 定位器链修复
- **根因**：AI 生成 CSS 选择器格式的 target 但旧代码只处理 label/placeholder/id；Playwright locator 对象总是 truthy → `a or b or c` 链式回退无效；`action: "type"` 被静默忽略
- **修复**：新增 `_resolve_step_locator()` 统一处理 + `_extract_text_from_css_target()` + Action 名称归一化
- **验证**：Session 118 capture_page_session 成功执行登录，528/528 通过

### AI 规划代理登录页面元素缺失 — 自动探索登录页 + ask_user 拦截
- **根因**：`_auto_explore_entry_url` 只探索首页，不探索 `/login`；ask_user 路径立即退出循环
- **修复**：自动探索登录页 + ask_user 拦截 + 系统提示澄清 + 安全网 URL 排序
- **验证**：532/533 通过

### AI 规划代理登录页面元素缺失 — 追问拦截补丁
- **根因**：AI 第一轮就 ask_user 时无 explore_page 记录，拦截逻辑无数据可查
- **修复**：新增 `_auto_explore_entry_and_find_login()` 在拦截时先探索入口页
- **验证**：528/528 通过

### BUG-063 追加修复 — thinking mode 下 SSE 空白 + 会话消失
- **根因**：reasoning_text 未归入 raw_response；非流式路径忽略 reasoning_content；loadSessionDetail 丢失 _thinkingContent
- **修复**：content 为空时用 reasoning_text 兜底；非流式 fallback；保留 _thinkingContent
- **验证**：505/506 通过

---

## 2026-05-04 | 可访问树定位器 + 发现时验证

**任务**：automationexercise.com 首页登录按钮找不到（`<a>` role="link" 但系统只有 `button_role` 策略）。

**操作**：
- Phase 1 — 补全 ARIA 角色策略：`link_role`(85)、`menuitem_role`(85) + fuzzy 变体(55)
- Phase 2 — 修复 runner + pre_scorer：不再硬编码 "button"，自动推断隐式 ARIA 角色
- Phase 3 — 可访问树 Tier 1.5：`snapshot_accessibility_tree()`（CDP，15 种交互角色）
- Phase 3.5 — 发现时验证：`_verify_locators_on_page()` 当场验证候选定位器

**验证**：automationexercise.com/login 37 个元素中 29 个有已验证选择器（86 个）；登录流程完整通过

---

## 2026-05-04 | AI Planning 上下文压缩 + Subagent 架构

**任务**：三个关联缺陷 — plan_json 被覆盖、工具结果膨胀（570KB-741KB）、JSON 解析失败降级差。

**操作**：
- plan_json 赋值加 `if response.plan is not None:` guard
- 工具调用消息改为存 `result_summary`（压缩摘要）
- 重工具同步存入新表 `ai_planning_tool_results`
- 新增 `_repair_json_text()`（尾部逗号修复）
- Subagent 压缩：`run_compression_subagent()` 短上下文 LLM 调用

**验证**：Python 模型导入 ✅、TypeScript 编译 ✅

---

## 2026-05-04 | 修复 correction 提交 409 冲突 + VLM 回退链路失效

**操作**：
- `create_correction()` 改为 update-in-place
- `execute_case_streaming()` 执行前插入 `reset_ai_visual_runtime_state()`
- `locate_element_by_vision()` 非限频错误改为 `continue` 让 fallback 模型链完整执行

**验证**：485 单元测试通过

---

## 2026-05-04 | 修复 DeepSeek thinking 模式 SSE 流式输出断流

**操作**：
- backend：`reasoning_content` 作为 `text_chunk` 事件实时转发（带 `thinking: true`）
- frontend：`_thinkingContent` 存入独立字段 + 渲染可折叠 `<details>`

**验证**：29 planning agent 单测 + 11 API 测试通过

---

## 2026-05-03 | 企业级中间层三大架构升级

**Phase 1 — 动作式 explore_flow**：`collect_flow_elements(steps)` 支持 click/input/wait_for 动作
**Phase 2 — 页面状态标记**：`page_state_id` + DSL step `page_state` 字段
**Phase 3 — 定位器预校验**：`locator_preflight.py` 静态校验 DSL targets

**验证**：485 单元测试全部通过

---

## 2026-05-03 | AI planning 架构方向评估

**关键结论**：
- 当前产品方向是对的：DSL/结构化测试 + 后端执行器 + 证据报告
- 但实现还不是完整的企业级闭环
- 企业级链路应继续朝四层推进：意图/需求层、状态化探索层、DSL 生成与预校验层、执行与证据层

---

## 2026-05-03 | AI planning 中间层排查

**关键证据**：
- 入口页能看到登录入口（约 300 个可交互元素）
- 自动探索不理解用户 flow（按首页链接顺序抓取）
- 前端 session/project 绑定链路失真

**结论**：问题不主要在提示词，而在架构

---

## 2026-05-03 | Session 15 — 修复三大核心缺陷

1. **BUG-055** — `create_project` 成功后 `project_id` 局部变量未更新
2. **BUG-056** — DSL draft prompt 超 50000 字符
3. **BUG-057** — hidden 元素恢复链跳过

**验证**：471 单元测试全部通过

---

## 2026-05-02 | Session 15 — 修复 explore_flow 0 元素 + 无 goto 白屏

**操作**：
1. 修复 `collect_multi_page_elements` 内本地导入遮蔽模块级导入
2. `_check_dsl_completeness()` 无 goto 时自动插入 `{"action": "goto", "value": "/"}`
3. Runner 首步骤不是 goto 且 base_url 已设置时先 `page.goto(base_url)`

**验证**：471 单元测试全部通过

---

## 2026-05-02 | Session 14 — 探索功能 + VLM 两阶段定位 + 评分数据传递

**操作**：
1. JS 提取脚本从 50 硬限制改为 300 参数化
2. VLM 两阶段定位（Stage 1 找区域 → crop + 2x 放大 → Stage 2 精确定位）
3. `format_elements_for_prompt()` 80K 字符智能截断
4. `_format_element_rich()` 输出 top 3 候选含 selector+pre_score

**验证**：455 单元测试通过

---

## 2026-04-30 | VS Code Claude Code 插件 settings.json BOM 修复

- **根因**：`settings.json` 文件开头带 UTF-8 BOM，`JSON.parse()` 无法解析
- **修复**：重写为无 BOM UTF-8

---

## 2026-04-28 | Session 12 — DOM 选择器评分 + VLM 置信度门控 + 点击前置处理器

**操作**：
1. 元素稳定性评分（data-testid=0.95 > id=0.90 > aria-label=0.80）
2. AI 置信度门控（`locator_confidence` 字段）
3. VLM 预验证模块（`preverify_with_vlm()`）
4. 点击前置处理器（等待→关闭→避让→强制→移除 降级链）

**验证**：416/416 单元测试全部通过

---

## 2026-04-28 | Session 11 — 加强后端日志输出和 Agent 错误信息

**操作**：
1. 创建集中式日志配置（统一格式 + LOG_LEVEL 控制）
2. Agent 错误信息增强（error_type/error_detail/phase/suggestion）
3. SSE 错误事件丰富化
4. 关键路径打点日志

**验证**：383 个单元测试通过

---

## 2026-04-27 | Session 10 — 执行报告增强 + Explorer-Judge 总结

**操作**：
1. ExecutionDetailPage 步骤信息增强（target/value 描述、断言结果、数据来源标识）
2. Explorer-Judge 执行总结持久化

**验证**：391 tests passed

---

## 2026-04-26 | Session 9 — 白屏修复

- **根因**：`AITestPlanningPanel.tsx` 渲染 todo_list 消息时缺少 `Array.isArray()` 空值检查
- **修复**：添加 `Array.isArray(item.structured_payload?.todo_list)` 保护

---

## 2026-04-26 | Session 8 — E2E Manual Test

**测试目标**：Automation Exercise 搜索→详情→购物车
**结果**：**16/16 步全部通过**，定位策略分布：text(7)、placeholder(4)、button_role(1)、text_fuzzy(2)

---

## 2026-04-26 | Session 7 — Explorer-Judge 架构

**核心差异**：失败不抛异常，记录后继续执行全部步骤。Explorer + Judge 双角色拆分。

**新增**：ExplorationRun/FailureRecord 模型、explorer_runner.py、judge_agent.py、VerdictPanel.tsx
**验证**：374 单元测试全部通过（含新增 25 个）

---

## 2026-04-26 | Session 6 — AI Planning Agent 三阶段进化

- Phase 1：执行分析工具（get_execution_detail/get_project_test_status/get_failure_analysis）
- Phase 2：智能决策（自动注入项目测试状态 + retest API）
- Phase 3：跨会话持久化（TestPointInsight 模型 + flaky 检测算法）

**验证**：350 passed

---

## 2026-04-25 | Session 5 — 上下文压缩 + 废弃 5 轮限制 + TODO 进度展示

**操作**：
1. `_prepare_transcript_for_llm()` 压缩机制（超 10 条时替换早期消息为摘要）
2. `ai_planning_max_react_rounds` 默认 5→0（无限），新增 safety_cap=30
3. system prompt 新增 `todo_list` 字段规范

**验证**：305 passed

---

## 2026-04-25 | Session 4 — CasesPage 项目级分类

- 左侧面板改为项目列表 → 搜索 → 状态过滤三段布局
- 项目列表带 CRUD（新建/编辑/删除 Modal）

---

## 2026-04-25 | Session 3 — CASCADE 替代 RESTRICT

- `test_case.py` FK 从 `ondelete="RESTRICT"` 改为 `ondelete="CASCADE"`

---

## 2026-04-25 | Session 2 — 修复删除项目 500

- `ai_planning_session.py` FK 从 RESTRICT 改为 SET NULL

---

## 2026-04-25 | VLM bbox 坐标点击回退 + 交互式 explore_flow + input_values 透传

**操作**：
1. `ResolvedLocator` 新增 `click_coordinates` 字段
2. `_try_coordinate_click_fallback()` Tier 2.5 回退
3. `_discover_interactive_elements()` 捕获弹层元素
4. `SaveAndExecuteRequest` 增加 `input_values` 字段

**验证**：Exec 69/70 各 13/13 全部通过

---

## 2026-04-24 | Session 2 — BUG-051/052 修复 + explore_flow + VLM 页面布局注解

**操作**：
1. `_substitute_variables` 函数 + `input_values` 字段
2. `_has_explored_pages` + `_auto_explore_entry_url` 强制探索
3. `collect_multi_page_elements` 跨页面采集
4. `describe_page_layout` VLM 页面布局注解

**验证**：303 passed

---

## 2026-04-24 | BUG-050 E2E 验证

**结果**：Execution 53 2/3 步通过（Step 3 `#input-email` 不存在），Execution 54 8/9 步通过（变量未替换）。发现 BUG-051/052。

---

## 2026-04-23 | 定位器系统三阶段改善

1. **Schema — target_strategy 字段**：显式声明定位策略
2. **定位器 — 裸 HTML 标签名识别**：`css_tag` 策略
3. **定位器 — Playwright 链式选择器解析**：`.class text=Value` 格式

**验证**：28/28 passed

---

## 2026-04-23 | BUG-050 DOM 证据注入 + target_strategy 偏好提示

**操作**：
1. `target_strategy` 从锁死改为偏好提示（try/except + fallback 穷举语义扫描）
2. DOM 证据注入 Schema + 数据提取传递
3. 修复 8 个单元测试

**验证**：276 passed

---

## 2026-04-21 | 流式状态感知 + AI 超时修复

**操作**：
1. Agent 流式基础：`_stream_planning_llm()` 使用 httpx SSE
2. 服务层 + WS 路由扩展
3. 前端事件模型（6 种流式事件类型）
4. Panel 流式渲染

**验证**：18 后端测试 + 11 前端测试通过

---

## 2026-04-21 | CRUD 补全

审查并补全所有实体的 CRUD 操作，新增 3 个 DELETE 端点 + 6 个前端 API 函数。

**验证**：前端 build 通过，242 后端测试通过

---

## 2026-04-20 | DOM-aware DSL 生成

**操作**：
1. `page_explorer.py`：存储状态文件 I/O + 元素格式化
2. `collect_interactable_elements` + `capture_browser_session`
3. `explore_page` 和 `capture_page_session` 工具注册
4. `_build_draft_prompt` 追加 DOM 感知提示
5. VLM 默认开启

**验证**：55 个相关单元测试全部通过

---

## 2026-04-17 | 用例创建 + 执行链路测试

**操作**：
1. 新增 3 个集成测试
2. 修复语义定位器 `element_id` 策略缺失 + `case-sensitive` 匹配

**验证**：6 passed

---

## 2026-04-17 | 流式接口 bug 修复 + create_project 幂等处理

1. **Session rollback**：异常时无条件 `db_session.rollback()`
2. **流异常兜底**：`except Exception` 分支写入 error 事件
3. **幂等处理**：同名项目自动编号

**验证**：43 passed

---

## 2026-04-17 | 用例编辑页 + 删除执行记录 + 平台 API chain 测试

**操作**：
1. `CaseEditPage.tsx` 用例编辑页面
2. `DELETE /executions/{execution_id}` 路由 + 前端删除按钮
3. 平台 API chain 白盒测试（3 个 session 测试）

**验证**：18 passed + TypeScript 编译通过

---

## 2026-04-16 | WebSocket 流式执行

**操作**：
1. `playwright_runner.py` 新增 `execute_case_with_playwright_streaming()` 流式执行生成器
2. `ai_planning_streaming.py` + WebSocket 端点
3. `executionWebSocket.ts` socket client
4. AITestPlanningPanel 接入 WebSocket

**验证**：29 后端测试 + 9 前端测试通过

---

## 2026-04-15 | 执行流式推送计划

产出基于当前仓库真实状态的可执行 implementation plan。

---

## 2026-04-13 | DSL 生成修复 + 持久化

**操作**：
1. `_call_llm()` 增加非 JSON/HTML 响应防御
2. 修正 `AI_DSL_BASE_URL`
3. draft 生成结果、execution summary 持久化到 messages

**验证**：12 passed + 5 passed

---

## 2026-04-13 | 白盒排查 session_id=27

确认 `AI_DSL_BASE_URL` 指向 HTML 首页而非 API；当前不存在 SSE/流式执行接口。

---

## 2026-04-12 | 会话删除功能 + stale session 修复

**操作**：
1. `delete_planning_session()` + `DELETE /sessions/{session_id}`
2. 前端删除按钮 + 当前会话删除后自动切换
3. 缓存失效 `ai_planning_last_session` 回退

**验证**：10 passed + 19 passed

---

## 2026-04-08 | 会话历史恢复 + 全流程闭环

**操作**：
1. `GET /sessions` 会话列表接口
2. `POST /sessions/{id}/drafts:save-and-execute` 保存+执行端点
3. 前端会话切换器 + 勾选式审阅卡片 + execution_summary 渲染

---

## 2026-04-08 | 更新 README

更新 README.md 反映 M2 阶段真实状态。

---

## 2026-04-06 | NotebookLM 布局重构

全局 ConfigProvider 主题 token 更新；新建 NotebookLMLayout 三栏布局；逐页重写为三栏风格。

---

## 2026-04-05 | demo 主链路重构

移除 demo 流的认证依赖；新增 PlanningPage；精简导航为三步 Steps；删除旧页面。

---

## 2026-04-03 | AI planning ReAct 改造

重写 `test_planning_agent.py` 为 LLM 驱动的 ReAct loop；更新 schema/service；前端 settings 与规划面板。

**验证**：13 passed + 20 passed + 16 passed

---

## 2026-03-31 | AGENTS.md 更新 + 迁移回归测试

更新协作规则；新增 Alembic 迁移回归测试验证 suite 相关表已被正确移除。

---

## 2026-03-31 | AI 测试规划代码质量修复

前端负时间戳临时 ID；后端 DSL 生成失败异常日志；无效 scenario key 校验。

---

## 2026-03-30 23:15 | AI 测试规划对话助手

新增后端 ai_planning 模型/schema/service/route/agent prompt/loop；前端 AITestPlanningPanel。

**验证**：15 passed + 16 passed + 16 passed

---

## 2026-03-30 22:00 | CRUD 安全修复（BUG-041）

补齐项目成员权限校验；修正 stats 返回结构；处理外键约束下的项目删除语义。

---

## 2026-03-30 21:31 | CRUD 提交审查 + GitHub 提交参考指令

确认 `7eb71ae` 存在多处高风险问题；AGENTS.md 新增 GitHub 提交流程。

---

## 2026-03-29~30 | DSL BigModel 适配与 GLM Visual Locate 适配

`dsl_generator.py` 请求层按 `base_url/model` 做 provider 自适配（BigModel 分支使用 `thinking` 参数）。

---

## 2026-03-29 | Suite 应用层下线

移除已废弃的 Suite 应用层，统一到 `Project -> Case` 资产结构。

---

## 2026-03-29 | 报告中心增强

扩展报告中心的作用域和指标。

---

## 2026-03-28 | M1 认证入口落地与治理收口

后端落地登录/登出/用户信息接口；前端完成登录态恢复、受保护路由、统一 401 回退。
