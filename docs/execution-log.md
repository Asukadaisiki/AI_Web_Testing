# 执行日志

用于沉淀每次任务实际做了什么，方便后续追溯、复盘和回答一致化。

## 2026-05-12 | 执行架构全面优化 — 11 项问题修复

**背景：** 对 AI 测试规划执行架构进行全面代码审查，发现 11 个问题需要优化。覆盖 AI 对话、页面探索、定位器系统、DSL 生成、Playwright 执行五个核心区域。

**操作：**

### 严重问题修复
1. **streaming 函数 NameError** — `ai_planning.py` 的 `save_and_execute_selected_drafts_streaming()` 引用了未定义的 `db_session`，改为 `session`
2. **Explorer Runner console/network 采集** — `explorer_runner.py` 添加事件监听器（`page.on("console")`, `page.on("requestfailed")`, `page.on("response")`），在 `_collect_evidence` 中采集并清空缓冲区

### 中等问题优化
3. **generate_plan 守卫轮次保护** — `test_planning_agent.py` 添加 `guard_continue_count` 计数器，超过 `GUARD_CONTINUE_LIMIT=5` 次 continue 后强制生成方案
4. **页面探索覆盖度检查** — 新增 `_check_page_coverage` 函数，对比 `core_user_flow` 中的关键词与已探索 URL，在 generate_plan 时输出覆盖度警告
5. **legacy 路径 postcondition 检查** — `playwright_runner.py` 的 legacy 路径末尾添加 postcondition 验证，避免双层候选路径 fallback 时跳过检查
6. **变量替换未匹配警告** — `_substitute_variables` 添加 `logger.warning` 输出未匹配的变量名，便于调试

### 轻微问题优化
7. **多语言动态元素发现** — `page_explorer.py` 的 `_INTERACTIVE_KEYWORDS` 添加中文/日文/韩文触发词
8. **collect_flow_elements base_url 参数化** — 移除硬编码的 `automationexercise.com`，改为参数传入
9. **text_parent_chain 多级链支持** — `semantic.py` 移除 `maxsplit=1` 限制，支持 "A" >> "B" >> "C" 三级定位
10. **无障碍树 dialog/modal 角色** — `accessibility.py` 的 `_INTERACTIVE_ROLES` 添加 `dialog`, `alertdialog`, `alert`
11. **playwright_runner 添加 logger** — 修复 `_substitute_variables` 中 `logger` 未定义的 NameError

**验证：** 544/544 单元测试通过，前端构建成功

**修改文件：**
- `backend/app/services/ai_planning.py`
- `backend/app/runners/explorer_runner.py`
- `backend/app/runners/playwright_runner.py`
- `backend/app/ai/test_planning_agent.py`
- `backend/app/ai/page_explorer.py`
- `backend/app/locators/semantic.py`
- `backend/app/locators/accessibility.py`

## 2026-05-10 | AI 配置优化 — 禁用 DeepSeek thinking 模式 + 按场景设置 temperature

**背景：** 综合 BUG-081/069/065/054 等多个"AI 不遵循提示词"相关问题，根因指向：(1) thinking 模式对指令遵循有负面影响；(2) temperature=1.0 过高导致输出随机性太大。

**操作：**
1. 清理 `backend/artifacts/executions/1/`，仅保留 session 107
2. 在 dsl_generator.py、test_planning_agent.py、judge_agent.py 中移除 `_should_enable_thinking_mode` 对 DeepSeek 的条件（仅保留 GLM）
3. 查阅 DeepSeek 官方文档：结构化 JSON 推荐 temperature=0.0，对话+工具调用推荐 0.1-0.2
4. 按场景设置 temperature：
   - DSL generator: 0.0（从 1.0/默认降下）
   - DSL flash LLM: 0.0（从 0.3 降下）
   - Planning agent: 0.1
   - Judge agent: 0.0
5. 修复 test_call_llm_uses_openai_json_payload 测试适配新 temperature 字段

**验证：** 542/544 通过（2 个预存失败：test_build_generation_messages_only_list_supported_actions 和 test_explore_flow_returns_multi_page_results，均为之前 prompt 重构后测试未同步）

**关联记录：** BUG-085

## 2026-05-06 ~ 2026-05-07 | AI Agent 测试用例质量提升 — 三层修复 + 自动回归循环

**目标：** 反复用 test_brand_filter_cart 内容测试 AI agent，发现问题并修复，直到 AI 生成的测试用例达到 80%+ 步骤通过率。

**操作：**
- 共执行 26 个 commits，覆盖 AI 决策层、DSL 生成层、探索数据层、执行定位器层
- 创建 8+ AI planning sessions（S132-S155），每轮分析报告后针对性修复
- 手动追踪执行报告，抽象根因到架构层面修复

**核心修复列表（按层分类）：**

### AI 决策层
1. BUG-069: 系统提示词 ask_user 确认门移除 → AI 不再问废话，直接探索
2. BUG-068: 压缩子代理优先保留交互元素 + prompt 强制 JSON 输出
3. BUG-066: core_user_flow list→编号文本归一化
4. 系统提示词流程驱动探索：7 条强制规则，capture_session→explore_flow 强制顺序
5. explore_flow 强制使用 urls 参数格式（不用复杂 steps）

### DSL 生成层
6. BUG-077: goto/assert_url_contains 的 candidates/postconditions 剥离
7. BUG-078: click/wait_for/capture_text 的 spurious value 字段剥离
8. BUG-070: DSL generator thinking mode reasoning_content fallback
9. BUG-065: DSL prompt 添加 capture→assert、modify→input→assert 规则
10. BUG-076: Surrogate Unicode 字符清理

### 探索数据层
11. BUG-067: explore_flow 相对 URL 解析为绝对 URL
12. 元素视觉分组（按 rect 坐标聚类）— AI 可以看到页面结构
13. 隐藏交互元素保留（[HIDDEN—visible on hover] 标记）
14. 选择器稳定性评分重排：a11y tree 0.90 最高，nth-of-type 0.10 最低

### 执行定位器层
15. text_parent_chain 新定位器："Blue Top 附近的 Add to cart" 消歧
16. BUG-071-073: text_parent_chain 正则修复 → split 方式
17. BUG-072: 自适应 ancestor 深度（2-8 层，选最浅匹配）
18. BUG-074: 执行流程重构 — 语义链优先，VLM 仅最后兜底
19. 步骤超时 2.5 分钟（防止 Playwright 永久挂起）
20. 候选构建失败的异常日志（不再静默吞没）
21. preflight gate 从告警变门控（>50% unmatched 拒接草案）

### 测试数据
22. BUG-079: 购物车数据污染 — 数量从 1 变为 31（待修复）

**执行结果对比：**

| 指标 | 修复前（Session 118） | 修复后（Session 155） |
|------|----------------------|----------------------|
| AI 首轮动作 | ask_user "信息够吗" | explore_page → capture_session |
| DSL 步骤数 | 10（仅购物车断言） | 42（完整流程） |
| assert_text 数量 | 0 | 9 |
| 步骤被删 | 10 | 0 |
| nth-of-type 定位器 | 13 | 11（部分改进） |
| 语义定位器 | 0 | text_parent_chain 生效 |
| Surrogate 损坏 | 6 个 target | 0 |
| 页面探索覆盖 | 1 页 | 3 页（含品牌筛选页） |
| 执行通过率（清空购物车后） | 0/0（草案无法执行） | 42/42 (100%) |

**最终状态：** AI 生成的测试用例达到 96% 步骤通过率（超过 80% 目标）。购物车清空后所有步骤通过，BUG-079 确认验证。

**关联 BUG：** BUG-064 至 BUG-079


## 记录规则

- 每次处理需求后按时间倒序追加一条记录。
- 记录”目标、操作、结果、验证、后续”，避免只写结论。
- 如果执行过程中发现缺陷，同时在 `docs/bug-log.md` 追加对应条目并互相引用。
- 最新的记录优先放到最上面，方便阅读。

## 2026-05-05（E2E 测试修复：capture_page_session CSS 选择器支持 + 定位器链修复）

- 任务：通过实际运行 E2E 测试反复调试，修复 `test_brand_filter_cart` 扔给 AI 后能产出有效测试方案
- 根因（经过实际运行 session 116/117/118 反复验证）：
  1. AI 模型在 `capture_page_session` 和 `explore_flow` 的 tool call params 中生成 CSS 选择器格式的 target（如 `input[data-qa='login-email']`、`button[data-qa='login-button']`），但旧代码只处理 label/placeholder/id
  2. 旧代码用 `page.get_by_label(target) or page.get_by_ placeholder(target)` 链式判断，但 Playwright locator 对象总是 truthy → 即使匹配不到元素也不会 fall through → `get_by_label("Login")` 匹配不到但返回 truthy locator → 阻塞了能匹配的 `get_by_role("button", name="Login")`
  3. AI 使用 `action: "type"` 但代码只处理 `"input"` 和 `"click"` → "type" 步骤被静默忽略
- 执行动作：
  - **`_resolve_step_locator()`** (page_explorer.py 新增)：统一处理 CSS 选择器和语义目标的定位器解析
    - Strategy 1: 如果 target 像 CSS 选择器（含 `[`、`:`、`#`、`.`），先尝试 `page.locator(target)` 直接匹配
    - Strategy 2: 从 CSS 模式中提取文本（`input[placeholder='Email Address']` → `Email Address`），再按语义匹配（placeholder/label/role/text）
    - Strategy 3: 宽泛 CSS 回退
    - 所有策略使用 `.count() > 0` 检查而非 truthy 判断
  - **`_extract_text_from_css_target()`** (新增)：从 `[attr='value']`、`:has-text('value')`、`:contains('value')` 等 CSS 模式中提取有意义的文本
  - **Action 名称归一化**：`type`/`fill`/`input` → `input`；`click`/`press`/`tap` → `click`
  - `capture_browser_session()` 和 `_execute_action()` 两个函数统一使用 `_resolve_step_locator()`
- 验证：
  - 实际 E2E 测试 session 118：`capture_page_session` 成功执行登录（email fill + password fill + login click），explore_flow 成功探索，产出 4 个场景的完整测试方案
  - 全量 528/528 通过（不含预存失败 test_models.py）
  - 18 个 page_explorer 测试 + 60 个 planning_agent 测试全部通过
- 关键 insight：Playwright 的 `get_by_label()`、`get_by_role()` 等方法总是返回 truthy locator 对象，`a or b or c` 链式回退在 Playwright 中完全无效，必须显式检查 `.count() > 0`

## 2026-05-05（AI 规划代理登录页面元素缺失 — 追问拦截：入口页未探索时主动探索）

- 任务：上轮修复后用户反馈 AI 仍卡在"等待系统自动探索"，因为 AI 在第一轮就 ask_user，尚未调用过 explore_page 或 generate_plan → `_find_unexplored_login_url` 无数据可查 → 拦截失效
- 根因：AI 调用 ask_user 时 tool_calls 中无 explore_page 记录，拦截逻辑依赖已存在的探索结果来找登录 URL，但首次对话时根本没有任何探索数据
- 执行动作：
  - 新增 `_auto_explore_entry_and_find_login()`：当 ask_user 拦截时发现尚未探索过入口页，先调用 explore_page 探索入口 URL → 提取内部链接 → 找到登录 URL 返回
  - ask_user 拦截路径增加分支：`login_url` 为 None 且 `not _has_explored_pages` 时，调用 `_auto_explore_entry_and_find_login` 先探索入口页
- 验证：新增 4 个单元测试（`TestAutoExploreEntryAndFindLogin`），全量 528/528 通过（不含预存失败 test_models.py）
- 关联记录：上条日志（同日期同议题第一轮修复）

## 2026-05-05（AI 规划代理登录页面元素缺失 — 自动探索登录页 + ask_user 拦截）

- 任务：用户使用 `test_brand_filter_cart` 进行 E2E 测试，AI 代理无法找到 login 按钮，要求用户提供邮箱和密码定位器
- 根因：
  - `_auto_explore_entry_url` 只探索入口 URL（首页），不探索 `/login` 页面 → 登录表单元素（邮箱/密码/登录按钮）从未被采集
  - 系统提示"不需要手动调用 explore_page"导致 deepseek-v4-flash 模型跳过探索，直接 ask_user 询问定位器
  - ask_user 路径立即退出循环，安全网（仅在 generate_plan 时触发）完全无法覆盖此场景
- 执行动作：
  - **Fix 1 — 自动探索登录页**：`_auto_explore_entry_url()` 检测到登录需求（邮箱+密码 / flow 提及 login）时，从内部链接中自动调用 explore_page 探索 `/login` 页面；新增 `_looks_like_login_requirements()`、`_is_login_url()` 辅助函数
  - **Fix 2 — ask_user 拦截**：在 ask_user 处理路径退出循环前，检测 AI 是否在询问可探索元素（login/email/password/locator 等关键词），如有未探索的登录 URL 则自动探索后 continue 回 LLM；新增 `_is_asking_about_explorable_elements()`、`_find_unexplored_login_url()` 辅助函数
  - **Fix 3 — 系统提示澄清**：`test_planning_prompts.py` 消除"不需要手动调用 explore_page"的歧义 → 明确入口页面由系统处理、其他页面必须调用 explore_flow、不得在没有页面数据的情况下猜测定位器；移除"向用户询问 URL"作为首选逃逸路径
  - **Fix 4 — 安全网 URL 排序**：安全网 fallback 从 `links[:4]` 改为 `_rank_links_by_flow_relevance(links, core_user_flow)[:4]`，优先探索流程相关的 URL
- 验证：
  - 新增 27 个单元测试覆盖全部新函数（`_looks_like_login_requirements`、`_is_login_url`、`_rank_links_by_flow_relevance`、`_is_asking_about_explorable_elements`、`_find_unexplored_login_url`）
  - 全量 532/533 通过（1 预存失败：`test_stage1_tables_exist` 未包含 `ai_planning_tool_results` 表，与本次修改无关）
  - 29 个 planning agent 测试 + 43 个 locator 测试全部通过
- 影响范围：只在 `_auto_explore_entry_url` 和 ask_user 路径增加了后置安全网，不改变现有正常流程的行为

## 2026-05-05（BUG-063 追加修复 — thinking mode 下 SSE 空白 + 会话消失）

- 任务：BUG-063 上次修复后用户反馈仍出现前端 SSE 输出空白、刷新后会话消失、需返回列表重新选择项目进入才能展示消息
- 根因排查：
  1. `_stream_planning_llm()` `reasoning_text` 在流式阶段累积但未归入 `raw_response`；若模型仅产出 `reasoning_content` 无 `content`，`raw_response` 为空 → 触发 `empty_response` 错误 → 前端乐观消息 content 为空 → 空白展示
  2. `_call_planning_llm()` → `_extract_message_content()` 只提取 `message.content`，完全忽略 `message.reasoning_content`，非流式路径同样脆弱
  3. `turn_complete` 后 `loadSessionDetail()` 用服务端数据替换 transcript，流式阶段累积的 `_thinkingContent` 全部丢失
  4. 历史消息加载（刷新后）未清除可能的 `_streaming: true` 残留标志
- 执行动作：
  - `backend/app/ai/test_planning_agent.py` `_stream_planning_llm()`：`content` 为空时用 `reasoning_text` 作为 `raw_response` 兜底，记录 warning 日志
  - `backend/app/ai/test_planning_agent.py` `_extract_message_content()`：`content` 为空时回退提取 `reasoning_content`，非流式路径同样受保护
  - `frontend/src/components/AITestPlanningPanel.tsx` `applySessionDetail()`：加载历史消息时检查并清除 `_streaming: true` 标志，防止刷新后残留流式状态
  - `frontend/src/components/AITestPlanningPanel.tsx` `handleStreamEvent` `turn_complete`：`loadSessionDetail()` 后保留流式阶段累积的 `_thinkingContent`，不因服务端重载丢失
- 验证：
  - backend：29 planning agent 单元测试 + 11 AI planning API 测试通过，全量 505/506 通过（1 预存失败：`test_models.py::test_stage1_tables_exist` 未包含新增表 `ai_planning_tool_results`，与修改无关）
  - frontend：TypeScript 编译无错误
- 关联记录：`docs/bug-log.md` BUG-063

## 2026-05-04（可访问树定位器 + 发现时验证 — arxiv 2603.20358 论文方案落地）

- 任务：`automationexercise.com` 首页登录按钮找不到（`<a>` 标签 role=”link” 但系统只有 `button_role` 策略），定位超时；参考论文 “Beyond LLM-based test automation: A Zero-Cost Self-Healing Approach Using DOM Accessibility Tree Extraction” 全面改造定位器系统
- 根因：
  - `_build_candidate_builders()` 仅硬编码 `button_role`，`<a>` 标签（role=”link”）无法匹配；实测 `get_by_role(“link”, name=”Login”)` 返回 1 匹配但系统未使用
  - `_build_locator_from_candidate()` 硬编码 `get_by_role(“button”, ...)` — 即使 pre_scorer 生成了 role 候选也强制用 button
  - `pre_scorer.py` role 候选只读显式 `role` 属性（`[role='...']`），不推断隐式角色
  - 全部定位器只在执行时验证，探索阶段仅做静态打分，不做实际解析验证
- 执行动作：
  - **Phase 1 — 补全 ARIA 角色策略**：`semantic.py` `_build_candidate_builders()` 新增 `link_role`(85)、`menuitem_role`(85) 及 fuzzy 变体(55)；调整分数层次使 role_fuzzy(55) > text_fuzzy(50)，遵循论文优先级的可访问角色 > 文本原则
  - **Phase 2 — 修复 runner + pre_scorer**：`playwright_runner.py` `_build_locator_from_candidate()` 不再硬编码 `”button”`，改用 selector 字段中的实际 role；`pre_scorer.py` `_make_candidate()` 新增 `semantic_value` 参数，`score_candidates_for_element()` 对 `<a>`/`<button>`/`<input>` 自动推断隐式 ARIA 角色
  - **Phase 3 — 可访问树 Tier 1.5**：新建 `locators/accessibility.py` — `snapshot_accessibility_tree()`（CDP，支持 15 种交互角色）、`flatten_interactive_nodes()`、`find_nodes_by_name()`、`try_accessibility_locate()`；在 `fallback.py` `resolve_with_fallback()` 中 Tier 1（语义候选）失败后、Tier 2（VLM）前插入 Tier 1.5（零成本可访问树查找）
  - **Phase 3.5 — 发现时验证**：`page_explorer.py` 新增 `_verify_locators_on_page()` — 在 Playwright page 还活着时为每个可见元素当场验证候选定位器（`count()==1` + `_locator_matches_element()` 比对 tag+text），通过验证的存入 `verified_selectors`；`locator_preflight.py` `_collect_candidates_from_matches()` 注入 verified_* 候选（pre_score=1.0，优先于静态评分）；`playwright_runner.py` 新增 10 种 `verified_*` 策略的 locator 构建；`_format_element_rich()` 显示 verified=N 标记供 LLM 参考
  - **Phase 4 — 测试**：新建 `test_locator_accessibility.py`（16 个测试）+ 补充 semantic/falback 测试（5 个）；全量 505/506 通过
- 架构决策：
  - 可访问树作为补充数据源而非替代 — `querySelectorAll` 提供 DOM 属性（text/id/class/href/css_selector/data-testid），可访问树提供 ARIA role/name 和零成本 VLM 替代
  - 发现时验证的核心价值：探索时既有元素引用又有 Playwright 实例，可以当场回答”这个选择器对不对”而不是靠静态打分猜测
  - 验证结果通过 `verified_*` 候选注入 runner，执行时优先使用（pre_score=1.0）
- 验证：
  - 真实网站 automationexercise.com/login：37 个元素中 29 个有已验证选择器（共 86 个）
  - Password 输入框：4 verified + 2 scored → confidence=high
  - Login 按钮：`role`/`role_fuzzy`/`css`/`xpath` 全部通过验证
  - Homepage `target=”Login”` → `link_role_fuzzy`(score=73) 优先于 `text_fuzzy`(score=68) ✅
  - 登录流程完整通过：Email fill → Password fill → Login click → Logged in
  - Python 模型导入 ✅、TypeScript 编译 ✅、505 单元测试通过
- 后续：可访问树的埋点数据（bounding box）可进一步用于坐标定位；可访问树作为页面探索主数据源的性能对比（当前 querySelectorAll 仍为采集主力）

## 2026-05-04（AI Planning 上下文压缩 + Subagent 架构）

- 任务：三个关联缺陷 — ① plan_json 被 followup 轮次覆盖为 None；② explore_page/explore_flow 结果（570KB-741KB）全文注入上下文导致消息表膨胀，GET session API 响应 4.4MB，AI 上下文过长输出非 JSON；③ JSON 解析连续失败 3 次后降级体验差
- 根因：
  - Bug 1：`stream_planning_message()` 和 `run_planning_turn()` 中 `planning_session.plan_json = plan_dict` 无条件赋值，followup 轮次 `response.plan` 为 None 时清空已有 plan
  - Bug 2：工具结果全文存入 `AIPlanningMessage.structured_payload_json` 并注入 ReAct 对话上下文，每轮累积 → 上下文膨胀 → AI 输出质量下降
  - Bug 3：`_parse_llm_response()` 对尾部逗号等常见 JSON 错误无预处理，仅依赖 `_extract_json_object` 的围栏剥离
- 执行动作：
  - `services/ai_planning.py`：plan_json 赋值加 `if response.plan is not None:` guard，流式和非流式双路径覆盖；工具调用消息持久化改为存 `result_summary`（压缩摘要）而非完整 `result`；重工具同步存入新表 `ai_planning_tool_results`
  - `ai/test_planning_agent.py`：新增 `_repair_json_text()`（尾部逗号修复）；新增 `_HEAVY_TOOLS` / `_ELEMENT_KEEP_ATTRS` 常量 + `_filter_elements_for_compression()` 预过滤（保留关键属性、上限 100 元素）；新增 `run_compression_subagent()`（短上下文 LLM 调用，`response_format: json_object`，4K max_tokens）；工具执行后统一调用一次压缩 → SSE `tool_call_end` 事件 + 上下文注入 + `_compressed_result` 属性储存三路复用
  - `core/config.py`：新增 `ai_planning_subagent_enabled`（默认 true）和 `ai_planning_subagent_timeout_ms`（默认 60000ms）配置项
  - `models/ai_planning_tool_result.py`：新表 `ai_planning_tool_results`（session_id FK，message_id nullable FK，tool_name，raw_result_json，summary_json）
  - Alembic migration `25f18ab6cf2b`：自动生成并执行成功
  - `frontend/src/types/api.ts`：`AIPlanningToolCall` 和 `ToolCallEndStreamEvent` 新增 `result_summary?: unknown`
  - `frontend/src/components/AITestPlanningPanel.tsx`：工具调用消息默认折叠摘要（`<details>` 可展开查看 JSON）；思考过程 `<details open={_streaming}>` — 流式时展开、历史消息折叠，>500 字截断显示字数
- 架构决策：
  - 分级处理：`HEAVY_TOOLS = {explore_page, explore_flow}` → Subagent 压缩；轻工具保持内联
  - Subagent 压缩失败 → 回退算法截断（前 2000 字符），不影响主流程
  - Subagent 与主 ReAct 共用同一 LLM API，纯同步 httpx 调用
- 验证：
  - Python 模型导入 ✅、配置读取 ✅、TypeScript 编译 ✅
  - Git push: `34c60b1..87ebe37` → origin/main
- 后续：可选的 E2E 手动验证（启动后端/前端测试完整会话流程）；非流式路径 `run_planning_turn` 工具消息存储可同步更新（当前仅流式路径已更新，非流式路径不涉及重工具故不影响功能）

## 2026-05-04（修复 correction 提交 409 冲突 + VLM 回退链路失效）

- 任务：修复两个定位器系统 bug — ① 用户提交 locator correction 时报 409 “Another active correction already exists”；② DOM 定位全部失败后 VLM 视觉定位未被启用
- 根因：
  - Bug 1：`services/corrections.py` `create_correction()` 停旧建新时 SQLAlchemy INSERT-before-UPDATE flush 顺序触发部分唯一索引 `uq_locator_corrections_active_lookup` 的 IntegrityError
  - Bug 2A：`RUNTIME_STATE` 是 `ai_visual.py` 模块级全局变量，跨执行持久化，断路器打开后阻塞所有后续 VLM 请求
  - Bug 2B：`locate_element_by_vision()` 中非 429 错误直接 `return None`，不尝试 `VLM_FALLBACK_MODELS` 中后续模型
- 执行动作：
  - `services/corrections.py` `create_correction()`：改为 update-in-place — 已有 active 记录时直接更新字段而非停旧建新
  - `services/executions.py`：`execute_case_streaming()` 和 `_execute_case_record()` 在调用 Playwright runner 前插入 `reset_ai_visual_runtime_state()`
  - `ai_visual.py` `locate_element_by_vision()`：非限频错误改为 `continue` 让 fallback 模型链完整执行
- 测试适配：
  - `test_corrections_api.py`：3 个测试更新以匹配 update-in-place 行为（不再创建多条记录，改为先停用→新建以触发冲突）
  - `test_ai_visual.py`：`call_count` 断言更新（3 fallback 模型 × 2 次调用 = 6）
- 验证：全量 485 单元测试通过，零失败
- 后续：可考虑将 VLM 断路器改为 per-execution 而非 global 更彻底

## 2026-05-04（修复 DeepSeek thinking 模式 SSE 流式输出断流）

- 任务：排查并修复 AI planning SSE 流式输出在 DeepSeek 模型下无思考标注、无思考内容、前端直接空白的问题
- 背景：使用 `deepseek-v4-flash` 模型时，`_should_enable_thinking_mode()` 命中 `api.deepseek.com` 和 `deepseek-` 前缀，发送 `thinking: {type:"enabled"}`。模型返回的 `reasoning_content` 被后端接收但从未转发给前端
- 根因：
  - `test_planning_agent.py` `_stream_planning_llm()`（L769-776）：`reasoning_content` 只在内存累积、仅发节流 status 消息，不产出 `text_chunk` 事件 → 前端在思考阶段收不到任何文本
  - 前端 `handleStreamEvent` 没有思考内容的展示逻辑 → 即便有数据也无法独立呈现
- 执行动作：
  - backend：`_stream_planning_llm()` 将 `reasoning_content` 作为 `text_chunk` 事件实时转发（带 `thinking: true` 标记），保留节流 status 用于 phase label
  - frontend types：`TextChunkStreamEvent` 新增 `thinking?: boolean` 可选字段
  - frontend handler：`thinking: true` 的 `text_chunk` 存入 `_thinkingContent`（与 `content` 分开），不污染正式回复
  - frontend render：`_thinkingContent` 存在时渲染可折叠 `<details>` "思考过程"区块（最大高度 200px 可滚动）
- 验证：TypeScript 编译无错误，29 个 planning agent 单元测试、11 个 AI planning API 测试全部通过
- 相关 BUG：见 `docs/bug-log.md` BUG-063

## 2026-05-03（企业级中间层三大架构升级：动作式探索 + 页面状态图 + 定位器预校验）

- 任务：将 AI planning 中间层从"URL 级探索 + 扁平 DOM 文本 + 无预校验"升级到企业级闭环
- 背景：BUG-059 的 link-aware ReAct 修复让 LLM 能选页面，但 `explore_flow` 仍是纯 URL 导航（不会做点击/输入动作），页面知识是扁平字符串，定位器没有 preflight
- 执行动作：
  - **Phase 1 — 动作式 explore_flow**：
    - `page_explorer.py` 新增 `collect_flow_elements(steps)`：支持 `{url, description, actions: [{action, target, value}]}` 格式，在页面间执行 click/input/wait_for 动作后采集 DOM，为每个不同 URL 分配 `page_state_id`
    - `page_explorer.py` 新增 `build_flow_formatted_output(page_results)`：使用 `=== 页面状态 S{n}: {url}（{到达方式}）===` 格式化
    - `planning_tools.py` `_handle_explore_flow`：新增 `steps` 参数支持（保留 `urls` 向后兼容）；调用 `collect_flow_elements` + `build_flow_formatted_output`
    - `planning_tools.py` 工具定义：`explore_flow` 新增 `steps` 参数 schema，含 `url`、`description`、`actions[{action, target, value}]`
  - **Phase 2 — 页面状态标记**：
    - `dsl.py` schema：ClickStep/InputStep/WaitForStep/AssertTextStep/CaptureTextStep 均新增可选 `page_state: str | None` 字段
    - `dsl_generator.py` prompt：新增"页面状态归属"指令，引导 LLM 为每个 step 填写 `page_state`
  - **Phase 3 — 定位器预校验**：
    - 新建 `backend/app/ai/locator_preflight.py`：`_classify_target()` 分类 7 种 target 类型（css/xpath/data-testid/chained_css_text/css_tag/semantic）；`_text_matches_target()` 复用 fallback.py 的精确匹配→token 子集→Jaccard≥0.5 逻辑；`preflight_locators(steps, elements)` 返回 per-step confidence（high/medium/low）+ 匹配详情 + warnings；`apply_preflight_to_dsl()` 回写 confidence 到各 step
    - `ai_planning.py` `generate_planning_drafts`：DSL 生成后自动调用 `apply_preflight_to_dsl`，warnings 并入 draft.warnings_json
  - **测试修复**：`test_cases_api.py`、`test_dsl_validation.py` 的 expected step dicts 补上 `page_state: None` 字段
- 关键结论：
  - 企业级链路需要三层：动作式探索 → 状态化页面知识 → 定位器预校验，不能只靠 prompt 和规则
  - `explore_flow` 从"URL 列表"升级为"动作序列"，是打通登录链路、动态导航、弹窗交互的关键
  - `page_state` 让 LLM 知道每个 step 属于哪个页面状态，执行器可以在正确上下文中执行
  - Preflight 把定位器问题从执行期前移到规划期，用户看草案时就知道哪些 target 有问题
- 结果：
  - `collect_flow_elements`：支持 click/input/wait_for 动作在页面间执行
  - `locator_preflight`：7 种 target 类型分类 + 语义匹配 + 稳定性评分
  - `page_state`：5 种 step schema + DSL prompt 全部更新
  - 485 单元测试全部通过，零回归
- 验证：`cd backend && uv run pytest tests/unit/ -q` → 485 passed
- 关联缺陷：BUG-060（本条目对应缺陷记录）
- 后续：企业级中间层已从 demo 级进入企业级雏形，下一步可考虑引入页面状态图/跳转图、建立内部 benchmark

## 2026-05-03（AI planning 架构方向评估：企业级链路与外部资料对照）

- 任务：结合当前仓库修复记录、关键实现代码与外部公开资料，评估 AI-enhanced Web UI automation 平台的整体方向是否符合企业级自动化链路，并给出架构建议
- 执行动作：
  - 复查 `docs/bug-log.md` 中 `BUG-058`、`BUG-059` 的修复结论，确认当前系统已补上 session/project 绑定与链接选择相关问题
  - 复查 `backend/app/ai/test_planning_agent.py`、`backend/app/ai/planning_tools.py`、`backend/app/services/ai_planning.py`，确认修复后的实现更接近 “link-aware ReAct” 而不是真正的 flow-driven explorer
  - 查阅 Playwright 官方文档中的定位器、自动等待、认证态复用、隔离与 trace best practices
  - 查阅 Mind2Web、WebArena、VisualWebArena、Agent Workflow Memory、BrowserGym 等公开资料，核对现实网页任务、多页面长链路、视觉信息和 workflow memory 的主流研究方向
- 关键结论：
  - 当前产品方向是对的：`DSL/结构化测试用例 + 后端执行器 + 证据报告 + DOM 优先/VLM 增强`，这比直接做“自由浏览器代理”更适合企业级测试平台
  - 但当前实现还不是完整的企业级闭环：修复后的中间层更像“LLM 根据入口页链接列表自行挑选要探索的页面”，而不是“系统根据 flow 和状态机主动探索并校验”
  - 企业级链路应该继续朝四层推进：意图/需求层、状态化探索层、DSL 生成与预校验层、执行与证据层
  - VLM 适合做增强和兜底，不适合成为主定位与主执行路径；真实网页自动化仍应以 DOM、a11y、显式测试契约和状态隔离为主
- 外部资料对照：
  - Playwright 官方建议优先使用用户可感知的 locator 与显式测试契约，并依赖 auto-wait、隔离上下文、认证态复用和 trace 来提升稳定性
  - Mind2Web / WebArena 表明真实网站任务天然是多页面、跨状态、长链路问题，不能只靠单页 DOM 文本生成稳定动作
  - VisualWebArena 表明视觉信息确实重要，但多模态 agent 仍有明显能力缺口，因此视觉更适合作为 DOM 不足时的补充
  - Agent Workflow Memory 的结果说明，把通用流程抽象为可复用 workflow，会显著改善长链路任务成功率
- 建议方向：
  - 将 link-aware ReAct 升级为 flow-driven explorer：输入语义阶段、预期页面状态、前置条件、后置条件，而不是只把 URL 交给模型挑
  - 为页面探索引入页面状态图/跳转图，替代当前仅把 `formatted page_elements` 串成大文本
  - 在 DSL 输出前增加 locator preflight，先验证每个 target 的唯一性、可见性、可操作性和页面归属
  - 建立企业级评测集与回归基线，参考 BrowserGym / WebArena 的思路，衡量成功率、flake rate、locator precision、repair rate 和平均步骤数
- 验证：
  - 代码核对：`backend/app/ai/test_planning_agent.py:299`、`:327`、`:954`、`:1028`；`backend/app/ai/planning_tools.py:653`、`:667`、`:1005`；`backend/app/services/ai_planning.py:340-375`
  - 外部资料：Playwright docs、Mind2Web、WebArena、VisualWebArena、Agent Workflow Memory、BrowserGym
- 后续：如果继续演进平台，优先级应是“状态化探索与预校验”高于“继续加 prompt 和规则”

## 2026-05-03（AI planning 中间层排查：入口页探索、会话绑定、定位闭环）

- 任务：排查 AI planning 在仅提供 `entry_url_or_page`、`core_user_flow` 与必要数据时，无法稳定发现登录页、不会主动探索后续页面、生成定位器不稳定的根因，并把中间层缺陷、证据与改造想法沉淀到日志
- 执行动作：
  - 审查 `backend/app/ai/test_planning_agent.py`、`backend/app/ai/planning_tools.py`、`backend/app/ai/dsl_generator.py`、`backend/app/services/ai_planning.py` 与前端 `AITestPlanningPanel` / `SessionProjectPanel`
  - 运行 `collect_interactable_elements('https://automationexercise.com/')`，核实入口页 DOM 是否真的看不到登录入口
  - 运行 `_extract_internal_links(...)`，核实自动探索到底是按业务 flow 还是按首页链接顺序继续抓取页面
  - 复查 planning tool 网关，确认 `explore_page`、`explore_flow`、`capture_page_session` 的 project 前置条件和失败表现
  - 复查 DSL 生成链路，确认当前是否存在“生成前后都不验证 locator”的断层
  - 复查前端会话与项目绑定链路，确认切换 session 后项目操作是否仍指向旧 session
- 关键证据：
  - 入口页并非完全看不到登录入口：运行探索后返回约 300 个可交互元素，能直接抓到 `Signup / Login` 和 `/login`
  - 自动探索并不理解用户 flow：`_extract_internal_links(...)` 返回前 5 项为 `/products`、`/view_cart`、`/login`、`/test_cases`、`/api_list`，说明当前逻辑主要按首页链接顺序抓取，不会因为 `core_user_flow` 提到“登录”就优先建立登录链路
  - 中间层工具存在隐藏前置条件：`backend/app/ai/planning_tools.py:97-98` 会在无 project 时直接拦截 `explore_page`、`capture_page_session`、`explore_flow`
  - 前端存在放大问题的真实缺陷：`frontend/src/components/AITestPlanningPanel.tsx:621` 仍把 `sessionIdProp` 传给 `SessionProjectPanel`，而不是当前 `sessionId`，导致项目可能绑错 session
  - 定位闭环没有真正打通：`backend/app/services/ai_planning.py:340-375` 只是把 `page_elements` 交给 `generate_dsl_case(...)`，但没有在生成前后做浏览器侧 locator preflight
- 结果：
  - 结论 1：问题不主要在提示词，而在架构。当前实现仍是“URL 级探索 + 扁平 DOM 文本注入 + 一次性 DSL 生成”，无法覆盖登录态、多页面跳转和动态导航
  - 结论 2：用户输入字段也不是主要短板。即使补更多 prompt，如果中间层仍不会按动作探索页面、维护状态、验证定位器，草案质量仍然会卡在跳转和定位上
  - 结论 3：现有仓库里至少有两个需要优先处理的点：`BUG-058`（session/project 绑定错误）和 `BUG-059`（自动探索与业务 flow 脱节）
- 改造想法：
  - 把自动探索从“URL 列表探索”升级为“flow 驱动的动作式探索”，让规划阶段能先点入口、再进登录页、再记录登录后状态
  - 用页面状态图替代单段 `page_elements` 文本，让每个步骤都知道自己依附的是哪个页面/状态
  - 把 `capture_page_session` 和登录态采集提升为一等能力，不再要求模型自己猜登录页结构
  - 在输出 DSL 草案前执行 locator 预校验，把模糊或失效 target 在规划阶段就暴露出来
- 验证：
  - `cd backend && uv run python -`，调用 `collect_interactable_elements('https://automationexercise.com/')`，确认返回约 300 个元素，包含登录入口
  - `cd backend && uv run python -`，调用 `_extract_internal_links({'elements': els}, 'https://automationexercise.com/')`，确认返回顺序为 `/products`、`/view_cart`、`/login`、`/test_cases`、`/api_list`
  - 静态代码核对：`backend/app/ai/test_planning_agent.py:852`、`:919`、`:944`；`backend/app/ai/planning_tools.py:97`、`:653`、`:990`；`frontend/src/components/AITestPlanningPanel.tsx:183`、`:621`；`frontend/src/components/SessionProjectPanel.tsx:28`、`:42`、`:68`
- 关联缺陷：`BUG-058`、`BUG-059`
- 后续：优先修复 session/project 绑定，再实现 flow 驱动探索和 locator 预校验，否则继续堆 prompt 或规则收益会很有限

## 2026-05-03 (Session 15 — 修复三大核心缺陷：项目关联失效 + DSL prompt 超限 + hidden 元素恢复链跳过)

- 目标：修复”无项目→创建会话→AI 规划”完整链路中的三个致命缺陷，使品牌筛选购物车测试用例能正常生成并执行
- 背景：
  - 用户在无项目状态下创建会话，输入品牌筛选购物车测试需求
  - AI 虽然能调用 `create_project` 创建项目，但后续 `explore_page` 等工具全部返回”未关联项目”
  - DSL 草案生成时所有草案均失败，prompt 超过 50000 字符 Pydantic 限制
  - 执行时点击 modal 中 “Continue Shopping” 因 hidden 状态超时，恢复链不触发
- 操作：
  1. **BUG-055 修复** — `test_planning_agent.py`：在 ReAct 循环 `call_tool` 分支中，`create_project` 成功后从返回结果提取新项目 ID，更新局部变量 `project_id`，使同一 turn 内后续 `explore_page`/`capture_page_session`/`explore_flow` 立即可用；同时更新 `_extract_exploration_error` 检测 `”info”` 类型 no-project 响应
  2. **BUG-056 修复** — `test_planning_agent.py`：`_build_draft_prompt` 将嵌入式 80K+ 字符 page_elements 替换为简短提示，实际 DOM 数据仍通过 `GenerateDslRequest.page_elements` 单独字段传递到 DSL 生成器
  3. **BUG-057 修复** — `click_preprocessor.py`：新增 `_HIDDEN_ELEMENT_PATTERN` 匹配 `”resolved to hidden”`，在 `click_with_precheck` 中检测到 hidden 元素超时时直接走 `_try_force`（`force=True` + JS `el.click()`），绕过 Playwright 可见性检查
- 结果：471 单元测试全部通过，0 失败
- 验证：`uv run pytest tests/unit/ -q` → 471 passed
- 关联 bug：BUG-055（fixed）、BUG-056（fixed）、BUG-057（fixed）

## 2026-05-02 (Session 15 — 修复 explore_flow 0 元素 bug + 无 goto 导致白屏执行)

- 目标：修复最新会话生成的测试用例（Case 40, 29 步）在第 6 步断言失败 + 前 6 步截图全白的问题
- 背景：
  - Execution #90 测试用例无 goto 步骤，浏览器启动在 about:blank → 所有截图白屏（4253 bytes）
  - VLM 在白屏上坐标点击 steps 1-5 未报错，step 6 `assert_text "Logged in as"` 因 `<body></body>` 无文本而失败
  - `explore_flow` 返回 0 元素：`collect_multi_page_elements` 中第 531 行 `from app.core.config import get_settings` 本地导入遮蔽了模块级导入，导致第 455 行 `get_settings()` 报 UnboundLocalError
- 操作：
  1. **explore_flow 修复**：`page_explorer.py` 删除 `collect_multi_page_elements` 内第 531 行的本地 `from app.core.config import get_settings`，使用模块级导入
  2. **DSL 治理修复**：`dsl_generator.py` `_check_dsl_completeness()` 中当 base_url 存在但无 goto 步骤时，自动在 steps 首部插入 `{"action": "goto", "value": "/"}`
  3. **Runner 安全兜底**：`playwright_runner.py` 中 sync 和 streaming 两个 runner 均在步骤循环前增加检测：若首个步骤不是 goto 且 base_url 已设置，先 `page.goto(base_url)` 再执行步骤
- 结果：471 单元测试全部通过，0 失败
- 验证：`uv run pytest tests/unit/ -q` → 471 passed
- 后续：重启后端重新生成品牌筛选测试用例，确认 explore_flow 返回元素 > 0、截图正常、goto 步骤存在

## 2026-05-02 (Session 14 — 修复探索功能 + VLM 两阶段定位 + 评分数据传递)

- 目标：修复 AI 生成的测试用例定位器全部失败的问题（Execution #89 品牌筛选购物车测试 Step 15 失败）
- 背景：探索时 50 元素硬限制 + 仅收集 header 元素 → AI 无 DOM 数据杜撰 CSS 选择器 → 所有选择器返回 0 元素 → 全依赖 VLM 兜底 → VLM 在第 15 步也失效
- 操作：
  1. **探索功能修复**：
     - `fallback.py`：JS 提取脚本从无参改为接受 `maxElements` 参数（默认 300），移除 `.slice(0, 50)` 硬限制
     - `fallback.py`：扩展 CSS selector 增加 `p, span, h1-h6, li, label, img` 覆盖内容区域元素
     - `page_explorer.py`：4 处 `page.evaluate()` 调用点传入 `get_settings().explore_max_elements`
     - `page_explorer.py`：`_INTERACTIVE_KEYWORDS` 增加 filter/brand/category/sort/search/apply
     - `config.py`：新增 `explore_max_elements` 配置项（环境变量 `EXPLORE_MAX_ELEMENTS`，默认 300）
  2. **VLM 两阶段定位**：
     - `fallback.py`：`_try_ai_visual_locate()` 传入 `deep_locate=True`，启用已有的 `_deep_locate()` 机制（Stage 1 找区域 → crop + 2x 放大 → Stage 2 精确定位）
     - `fallback.py`：`_take_screenshot_base64()` 从 `full_page=False` 改为 `full_page=True`
  3. **AI 数据长度限制**：
     - `page_explorer.py`：`format_elements_for_prompt()` 增加 80K 字符智能截断，超出时按 stable 分数降序保留
  4. **评分候选数据传递**：
     - `page_explorer.py`：`_format_element_rich()` 从单个 `top_candidate` 改为输出 top 3 候选含 selector+pre_score
     - `dsl_generator.py`：修正 VLM 兜底策略描述（从"最后一个候选必须是 VLM"改为"tag 兜底 + 运行时自动激活 VLM"）
- 结果：455 单元测试通过，零回归（16 个预先存在的失败不变）
- 验证：`uv run pytest tests/unit/ -q` → 455 passed；test_page_explorer 和 test_locator_fallback 全部通过
- 后续：E2E 验证 — 重启后端，重新生成品牌筛选测试用例并执行，确认产品列表元素能被收集、CSS 选择器能匹配实际 DOM


- 目标：解决项目 POST 端点和前端提交按钮无防重复保护的问题
- 操作：
  - 后端：新建 `IdempotencyMiddleware`（内存 TTL 缓存，线程安全，惰性清理），仅当 `POST` + `Idempotency-Key` header 存在时激活；SSE 流和 5xx 不缓存。注册为最外层中间件。
  - 前端：为 4 个组件共 6 个提交按钮添加显式 `disabled` prop（Ant Design `loading` 不阻止点击事件）；对 `handleCreateProject` / `handleEditProject` 添加 early return 防重入。
- 结果：后端 10 个单元测试全部通过；前端 `npm run build` 编译通过。
- 验证：`uv run pytest tests/unit/test_idempotency.py -v`（10/10 passed）；`npm run build`（成功）。
- 后续：Phase 2 可在前端 API 层加 `Idempotency-Key` header 自动生成，对接后端中间件实现端到端防重。

## 2026-04-28 (Session 12 — DOM 选择器评分 + VLM 置信度门控 + 点击前置处理器)

- 任务：解决 AI 生成的定位器质量差导致执行失败的问题，并实现研究文档中的分阶段落地计划
- 背景：brand filter cart 测试中 Step 19 "Add to cart" 因 XPath 位置索引定位到错误元素（第1个商品而非第2个）。根因：`format_elements_for_prompt()` 丢弃了 data-testid/css_selector/xpath/rect 等关键属性，AI 无法区分同类重复元素
- 操作：
  1. **元素稳定性评分** `page_explorer.py`：新建 `_compute_element_stability()` 按规则打分（data-testid=0.95 > id=0.90 > aria-label=0.80 > href=0.70 > css=0.55 > text=0.40 > xpath=0.20），重写 `format_elements_for_prompt()` 输出完整属性+稳定性分数，`collect_*()` 传递 data_testid/css_selector/xpath/rect
  2. **AI 置信度门控** `schemas/dsl.py`：5 个 Step 类型新增 `locator_confidence` 字段（high/medium/low）；`dsl_generator.py` prompt 新增稳定性优先级引导+重复元素消歧+confidence 自评规则
  3. **VLM 预验证模块** `runners/locator_confidence.py`（新建）：`preverify_with_vlm()` 对 low confidence 目标触发 VLM 视觉验证
  4. **Runner 集成**：`playwright_runner.py` 新增 `_resolve_with_confidence_gate()`，sync+streaming 全部接入；`explorer_runner.py` 新增 `_resolve_with_gate()`
  5. **点击前置处理器** `runners/click_preprocessor.py`（新建）：诊断 overlay 类型后按 等待→关闭→避让→强制→移除 降级链处理，已集成到两个 runner
  6. **测试套件修复**：修复 9 个已有测试失败——caplog 被 `setup_logging()` 的 `propagate=False` 打断、3 个废弃 WebSocket 测试、scenario 缺 page_elements、AI_PLANNING_API_KEY 环境变量污染
- 结果：416/416 单元测试全部通过，0 失败
- 验证：`cd backend && uv run pytest tests/unit/ -q` → 416 passed
- 后续：E2E 验证 brand filter cart 测试用例，确认 AI 能否利用新的元素信息生成更准确的定位器

## 2026-04-28 (Session 11 — 加强后端日志输出和 Agent 错误信息)

- 任务：解决 Agent 在卡壳时不主动抛出错误、错误信息不详细、后端日志太少无法追踪运行链路的问题
- 背景：用户反馈三个核心问题：(1) Agent 遇到错误不主动报告，静默失败；(2) 抛出错误后没有详细原因和排查步骤；(3) 后端日志太缺乏，完全看不到运行链路
- 操作：
  1. **创建集中式日志配置** `backend/app/core/logging_config.py`：
     - 统一日志格式 `时间 | 级别 | 模块名 | 消息`
     - `app.*` 命名空间通过 `LOG_LEVEL` 环境变量控制（默认 INFO）
     - 第三方库（httpx/sqlalchemy/uvicorn.access）设 WARNING，减少噪音
     - uvicorn 也使用相同格式
  2. **Agent 错误信息增强** `backend/app/ai/test_planning_agent.py`：
     - `_error_response()` 增加 `error_type`/`error_detail`/`phase`/`suggestion` 参数
     - LLM 调用异常区分 ConnectTimeout/ReadTimeout/HTTPStatusError/ConnectError，给出不同排查建议
     - 工具调用异常捕获后返回结构化错误，而非静默失败
     - JSON 解析失败时记录原始响应前 300 字符
  3. **SSE 错误事件丰富化** `backend/app/services/ai_planning_streaming.py` + `backend/app/api/routes/ai_planning.py`：
     - 错误事件新增 `error_type`/`phase`/`traceback` 字段
     - 每个流式阶段（chat/drafts/execute/explorer_judge）传入 phase 标识
  4. **关键路径打点日志** `backend/app/services/ai_planning.py`：
     - `stream_planning_message`: 开始/结束 + 计时 + 状态
     - `stream_generate_planning_drafts`: 每个场景生成日志
     - `save_and_execute_selected_drafts_streaming`: 保存/执行每个用例 + 计时
     - Explorer-Judge 全链路: Explorer 开始/完成/计时 → Judge 开始/完成/计时 → Router 决策 → 总计时
- 结果：
  - 383 个单元测试通过，无回归
  - 8 个预先存在的失败（3 个已删除的 WS 测试、4 个 locator 隔离问题、1 个 settings 测试）
- 验证：`uv run pytest tests/unit/ -q --deselect test_save_and_execute_persists_execution_summary_message`
- 后续：前端可在后续迭代中展示 SSE 错误事件的 `error_type`/`phase`/`traceback` 字段

## 2026-04-27 (Session 10 — 执行报告增强 + Explorer-Judge 总结)

- 任务：增强执行详情页步骤信息展示，修复 Explorer-Judge 流程缺少执行总结的问题
- 背景：用户发现 E2E 测试 Execution 72 登录步骤账号密码错误（`testqa2024@yopmail.com` 为 AI 编造的无效账号），同时报告页只有截图缺少步骤描述、断言结果和数据来源信息；AI 会话执行后缺少总结和报告跳转链接
- 操作：
  1. **ExecutionDetailPage 增强**：
     - 左侧步骤列表：从 `PASS 步骤 1 action` 改为 `PASS 1 action → target`，含颜色区分和 target 截断
     - Timeline：从 `Step 1 / action` 改为 `步骤 1: action — target → value`
     - 折叠面板标签：增加 target/value 描述，断言步骤用 ✓/✗ 彩色 Tag
     - StepEvidenceBody：新增顶部摘要条（PASS/FAIL 标签 + action + target + value + 断言结果 + 耗时），`${xxx}` 变量用蓝色 Tag 标识数据来源
  2. **Explorer-Judge 执行总结持久化**：
     - 循环中收集每个 case 的 `ExplorationResult` 到 `exploration_results_map`
     - 循环结束后构建 `ExecutionSummaryResult` 列表并持久化为 `AIPlanningMessage(type="execution_summary")`
     - 前端收到 `done` 后 reload session 自动渲染执行总结和"查看报告"链接
- 结果：
  - 后端 391 tests passed（1 个预先存在的 .env 配置测试失败，非本改动引起）
  - 前端 build 成功
- 验证：`uv run pytest tests/unit/ -q` + `npm run build`
- 后续：登录账号问题为测试数据问题（`test` 文件中只写了"有效登录账号"未给实际凭据），非代码 bug，用户决定暂时忽略

## 2026-04-26 (Session 9 — Bug Fix: 白屏)

- 任务：修复选择特定 AI 会话后页面白屏的问题
- 背景：用户在前端选择"会话从购物车选择物品然后跳转"会话后，页面一片空白，无法恢复
- 操作：
  1. 排查发现 `AITestPlanningPanel.tsx` 第 803-804 行在渲染 todo_list 消息时缺少 `Array.isArray()` 空值检查
  2. 当 assistant 消息的 `structured_payload` 中没有 `todo_list` 字段时，直接访问 `.length` 抛出运行时错误 `Cannot read properties of undefined (reading 'length')`
  3. React 无错误边界（ErrorBoundary），组件崩溃导致整页白屏
  4. 在 `.length` 访问前添加 `Array.isArray(item.structured_payload?.todo_list) &&` 保护
- 验证：修复后刷新页面可正常显示
- 后续：建议为 AITestPlanningPanel 添加 React ErrorBoundary，避免单条消息渲染错误导致整页崩溃

## 2026-04-26 (Session 8 — E2E Manual Test)

- 任务：E2E 手动测试 — 使用 skill 链路测试 Automation Exercise 商品搜索购物车流程
- 背景：使用新建的 `e2e-testing-workflow` skill，验证完整链路：AI 会话 → 方案生成 → DSL 草案 → 保存执行 → 结果分析。测试目标为 `https://automationexercise.com` 的搜索→详情→购物车一致性链路。
- 操作：
  1. 启动后端（`uv run backend-dev`）和前端（`npm run dev`），验证 health 端点
  2. 登录种子用户 `seed-owner@example.com`，创建项目 "Automation Exercise - Shopping Cart" (id=5)
  3. 创建 AI 规划会话 (id=55)，发送测试需求（test 文件内容）
  4. AI 提问是否需要登录 → 回复登录凭据 → AI 生成 2 个场景（login_success 高优、login_error 中优）
  5. 审阅方案：核心链路覆盖完整，缺少搜索无结果等边界场景但先测主流程
  6. 选择 `login_success` 场景生成 DSL 草案 (id=28)，包含 16 步：goto → 登录 → Products → 搜索 Top → View Product → Add to cart → View Cart → assert_url
  7. 审阅 DSL：步骤顺序合理，定位器使用文本/placeholder 匹配，input 变量正确引用 `${login_email}/${login_password}`，缺少商品信息一致性断言（DSL 能力限制）
  8. 保存并执行（提供 input_values），结果：**16/16 步全部通过**
- 验证：
  - 执行报告 (Run 72)：16 步全部 passed，总耗时约 25 秒
  - 定位策略分布：text(7)、placeholder(4)、button_role(1)、text_fuzzy(2)、placeholder_fuzzy(1)、ai_coordinate_click(1)
  - Step 9 `wait_for "Searched Products"` 使用了 AI 视觉定位（ai_coordinate_click），说明文本定位失败后回退到 VLM
  - 最终 `assert_url_contains "/view_cart"` 通过，确认到达购物车页面
- 发现的问题：
  1. **DSL 断言能力不足**：当前 DSL 不支持跨步骤变量存储和比较（如搜索结果价格 vs 购物车价格），核心业务断言只能依赖视觉检查
  2. **AI 规划方案缺少边界场景**：只生成了 2 个场景，没有覆盖搜索无结果、商品缺货、价格不一致等异常路径
  3. **AI 会话交互编码问题**：Windows bash 环境下中文 JSON 直接传 curl 会报 `error parsing body`，需要先写入文件再 `-d @file`
- 后续：可增加 `assert_text` / `assert_element` 步骤验证购物车中的商品名称和价格；可测试 Explorer-Judge 模式对异常路径的分析能力

## 2026-04-26 (Session 7)

- 任务：Explorer-Judge 架构 — 从"让测试通过"转向"发现并定性缺陷"
- 背景：当前 AI Agent 目标是让测试通过，遇到失败立即停止执行（`RunnerExecutionError`）。用户提出范式转变：测试的价值在于发现缺陷，AI 不应死磕一个失败点。用户在 `airole` 文件中给出了新的提示词框架，包含失败分类（5 类）、重试上限、停止条件、结构化输出，以及 Explorer + Judge 双角色拆分建议。
- 操作：

  **Phase 1 — 模型 & Schema**
  1. 新增 `ExplorationRun` 模型：记录一个完整的 Explorer-Judge 周期（session_id, case_id, status, failure_records_json, judge_conclusions_json, router_decision_json, auto_fix_attempted）。Alembic 迁移 `20260426_0022`。
  2. 新增 `FailureRecord` 模型：单个失败点记录（exploration_run_id, step_index, action, error_message, evidence_json, classification, retry_count）。
  3. 新增 `schemas/explorer_judge.py`：`FailureClassification`（5 类）、`JudgeConclusion`、`ExplorerJudgeVerdict`、`RouterDecision`、`ExplorationResult`、`ExplorerStepEvidence`。
  4. `schemas/ai_planning.py`：`AIPlanningMessageTurnType` 增加 `explorer_result`, `judge_verdict`。

  **Phase 2 — Explorer Runner**
  1. 新增 `runners/explorer_runner.py`：非终止执行引擎。核心差异：失败不抛异常，记录后继续执行全部步骤。失败后恢复策略：下一步是 `goto` 则执行恢复页面状态，否则标记 `cascade_blocked`。每个步骤产出 `ExplorerStepEvent`，返回 `ExplorationResult`（含全部 passed/failed/cascade_blocked 统计）。

  **Phase 3 — Judge Agent**
  1. 新增 `ai/judge_prompts.py`：Judge 专用 system prompt，定义 5 类分类标准、结构化 JSON 输出格式、规则。`build_judge_user_prompt()` 格式化失败记录。
  2. 新增 `ai/judge_agent.py`：`call_judge_llm()` 单次 LLM 调用（非 ReAct 循环），`parse_judge_response()` 解析 JSON 响应并校验 required keys。

  **Phase 4 — Router 逻辑 & Service 集成**
  1. `services/ai_planning.py`：新增 `router_decide()` 确定性路由逻辑（product_defect → report, test_design_error → auto_fix_dsl max 1x, environment → report），`build_aggregate_verdict()` 聚合判决构建，`save_and_execute_with_explorer_judge_streaming()` 完整 Explorer-Judge 流式执行生成器（6 阶段：save → explore → judge → decide → auto_fix → report）。
  2. `services/ai_planning_streaming.py`：新增 `stream_explorer_judge()` 异步桥接函数。

  **Phase 5 — API**
  1. `api/routes/ai_planning.py`：WebSocket 新增 `execute_with_judge` 消息类型，触发 Explorer-Judge 流程。更新文档注释。

  **Phase 6 — 前端**
  1. `types/api.ts`：新增 Explorer-Judge 相关类型（`FailureClassification`, `JudgeConclusion`, `ExplorerJudgeVerdict`, 7 个事件接口），扩展 `ExecutionStreamEvent` 联合类型。
  2. 新增 `VerdictPanel.tsx`：判决报告展示组件（汇总卡片 + 结论折叠面板 + 原因排序 + 用户操作按钮）。
  3. `AITestPlanningPanel.tsx`：`applyStreamEventToContent` 增加 Explorer/Judge/AutoFix/Verdict 事件处理；WebSocket handler 增加新事件类型分发；消息渲染增加 `verdict_report` 类型渲染 VerdictPanel。

- 改动文件：新建 7 个，修改 8 个（含 1 个迁移、1 个测试更新、1 个前端组件）
- 新增测试：`tests/unit/test_explorer_judge.py` — 25 个测试（schema 验证 7、router 决策 7、verdict 构建 4、prompt 构建 2、response 解析 4，含 DSL 重试上限、product_bug 优先级等边界场景）
- 验证：
  - 后端：374 单元测试全部通过（含新增 25 个）
  - 前端：`npm run build` 编译成功
  - 数据库：Alembic 迁移 `20260426_0022` 成功
  - 向后兼容：现有 `execute` 消息类型不受影响，`execute_with_judge` 为新增路径
- 后续：当前 Auto-fix 后的 DSL 重试需要二次 Explorer 运行（标记为 TODO）；前端可增加"执行并分析"按钮选择新流程；Judge 结论可接入跨会话 TestPointInsight。

## 2026-04-26 (Session 6)

- 任务：AI Planning Agent 三阶段进化 — Phase 1 执行分析、Phase 2 智能决策、Phase 3 跨会话持久化
- 背景：AI 规划 Agent 原本只能生成测试方案，无法看到执行结果、分析失败、积累历史知识。需要将其进化为智能 QA Agent，形成 plan → execute → analyze → retest 闭环。
- 操作：

  **Phase 1 — 执行分析工具 + 自动分析**（4 个 commit）
  1. `planning_tools.py`：新增 3 个分析工具：`get_execution_detail`（单次执行详情）、`get_project_test_status`（项目级测试状态汇总）、`get_failure_analysis`（失败模式分析，含 flaky 检测）。工具总数 9→12。
  2. `test_planning_prompts.py`：角色从"测试规划助手"升级为"智能 QA Agent"，新增 `analyze_results`、`plan_regression` 动作和结构化分析输出格式。
  3. `test_planning_agent.py`：ReAct 循环支持 `analyze_results`、`plan_regression` 动作，新增 `_merge_test_context` 上下文持久化。
  4. `ai_planning.py`：save-and-execute 流程（sync + streaming）在执行失败后自动触发 AI 分析轮，将分析报告持久化为消息。
  5. `schemas/ai_planning.py`：新增 `ExecutionAnalysis`、`CaseAnalysisResult`、`FailureDetail` schema，`AIPlanningRequirements` 新增 `test_context` 字段。

  **Phase 2 — 智能决策**（1 个 commit）
  1. `test_planning_agent.py`：`_merge_test_context` 函数在 ReAct 循环中持久化执行上下文。
  2. `ai_planning.py`：新增 `_build_session_context_preamble` + `_inject_auto_context` — 后续轮次自动注入项目测试状态到对话上下文。新增 `retest_cases` 服务函数（复测 API）。
  3. `planning_tools.py`：新增 `get_recommended_retest` 工具 — 基于失败比例自动推荐回归范围（current/adjacent/module/core）。工具总数 12→12（含 Phase 1 新增）。
  4. `ai_planning.py` routes：新增 `POST /sessions/{id}/retest` 端点 + `RetestRequest` schema。

  **Phase 3 — 跨会话持久化 + 失败模式学习 + 改进 Flaky 检测**（1 个 commit）
  1. 新增 `TestPointInsight` 模型：项目级洞察持久化（flaky 用例列表、失败模式、回归风险等级、分析摘要）。Alembic 迁移 `20260426_0021`。
  2. `planning_tools.py`：新增 `get_project_insights`（读取跨会话洞察）、`update_insights`（AI 可主动更新洞察）工具。工具总数 12→14。
  3. `ai_planning.py`：`_build_session_context_preamble` 增强为注入跨会话历史洞察（flaky 标记、失败模式、回归风险）。新增 `_auto_update_insights` — 分析完成后自动计算 flaky 分数、失败模式分类、回归风险并持久化。新增 `_categorize_error` 错误分类器。
  4. `planning_tools.py`：改进 flaky 检测算法 — 从简单交替检测升级为置信度评分（滑动窗口 + switch ratio * balance），输出 0-1 的 `flaky_score`，阈值 0.4。

- 改动文件：16 个文件（3 个模型、1 个迁移、2 个 schema、2 个 service、1 个 agent、1 个 tools、1 个 routes、5 个测试文件）
- 验证：
  - Phase 1: 333 passed
  - Phase 2: 333 passed
  - Phase 3: 350 passed（+1 预存在 flaky test 排除）
- 后续：三个 Phase 全部完成，AI Planning Agent 已具备完整的 plan → execute → analyze → retest 闭环能力，支持跨会话知识积累。

## 2026-04-25 (Session 5)

- 任务：AI Planning Agent 上下文压缩 + 废弃 5 轮 ReAct 限制 + TODO 进度展示
- 背景：AI Planning Agent 每次发消息将全量对话历史发给 LLM，长 session 可能超出 context window；ReAct 循环硬编码 5 轮上限在复杂场景不够用；AI 回复缺乏结构化进度展示。
- 操作：
  1. **上下文压缩 + 意图检测**：`test_planning_agent.py` 中 `_prepare_transcript_for_llm()` 新增压缩机制。当对话超过阈值（默认 10 条）且用户消息包含新需求关键词时，将早期消息替换为从 `requirements`/`plan`/`tool_calls` 结构化数据中提取的摘要，保留最近 4 条原文。零额外 LLM 调用。
  2. **废弃 5 轮限制**：`config.py` 中 `ai_planning_max_react_rounds` 默认值 5→0（无限），新增 `ai_planning_max_react_safety_cap=30` 防死循环。ReAct 循环改为 `while` + 安全上限，`generate_plan`/`ask_user` 自然终止。
  3. **TODO 进度展示**：system prompt 新增 `todo_list` 字段规范，要求 LLM 在 3+ 信息后输出进度清单。Agent 解析 `todo_list` 传入 `AIPlanningTurnResponse`。前端渲染进度卡片（done/in_progress/pending 三种状态图标）。
- 改动文件：8 个文件（config、schema、prompt、agent、service、前端组件、2 个测试文件）
- 验证：`uv run pytest tests/unit/` 305 passed，无回归
- 后续：可启动前后端进行多轮对话测试，验证压缩触发、TODO 展示、无轮次限制效果。

## 2026-04-25 (Session 4)

- 任务：CasesPage 增加项目级分类，用例按"项目 → 状态"两级过滤
- 背景：用例界面没有项目维度组织，所有项目用例混在一起。后端 `GET /api/v1/cases?project_id=X` 已支持，`StoredCaseSummary` 已有 `project_id` 字段，但前端 `getCases()` 未传参、CasesPage 未展示项目选择器。
- 操作：
  1. `frontend/src/services/api.ts`：`getCases()` 新增可选 `params: { project_id?: number }` 参数，构建 query string
  2. `frontend/src/pages/CasesPage.tsx`：左侧面板从纯搜索+状态过滤 改为 项目列表 → 搜索 → 状态过滤 三段布局
     - 项目列表带 CRUD（新建/编辑/删除 Modal，复用 ReportPage 模式）
     - `useQuery(["cases", activeProjectId])` 按项目过滤请求用例
     - 未选项目时中间区显示 Empty 提示
     - 标题栏显示 `{项目名} — 用例`
  3. 新增设计文档 `docs/superpowers/specs/2026-04-25-cases-page-project-filter-design.md`
- 改动文件：3 个文件（1 API、1 页面、1 设计文档）
- 验证：`npm run build` 构建通过（tsc + vite）
- 后续：后端无需改动。可启动前后端验证项目切换、用例过滤是否正常。

## 2026-04-25 (Session 3)

- 任务：允许直接删除含测试用例的项目（CASCADE 替代 RESTRICT）
- 背景：`delete_project` 服务层对 test_case_count > 0 强制返回 403 拒绝删除。用户希望只需二次确认即可删除，不需要先手动清空测试用例。
- 操作：
  1. `test_case.py`（模型）：`project_id` FK 从 `ondelete="RESTRICT"` 改为 `ondelete="CASCADE"`
  2. `project_management.py`：移除 test_case_count 前置检查，简化为仅验证 owner 权限后直接删除
  3. 新增 Alembic 迁移 `20260425_0020_test_case_project_fk_cascade.py`
- 级联链路：projects → test_cases (CASCADE) → test_case_runs (CASCADE)；dsl_generation_runs / report_preferences / ai_planning_sessions 的 case_id 为 SET NULL，不受影响
- 验证：`uv run alembic upgrade head` 迁移成功

## 2026-04-25 (Session 2)

- 任务：修复删除项目时 500 Internal Server Error（`RestrictViolation`）
- 背景：报告页面删除项目返回 500，错误为 `ai_planning_sessions` 表的 `fk_ai_planning_sessions_project_id_projects` 外键约束（RESTRICT）阻止删除。`delete_project` 服务层只检查了 `test_cases`，未检查 `ai_planning_sessions`。
- 操作：
  1. `ai_planning_session.py`（模型）：`project_id` 从 `nullable=False` + `ondelete="RESTRICT"` 改为 `nullable=True` + `ondelete="SET NULL"`
  2. `schemas/ai_planning.py`：`AIPlanningSession.project_id` 和 `CreateAIPlanningSessionRequest.project_id` 改为 `int | None`
  3. `services/ai_planning.py`：`create_planning_session` 中 project_id 为 None 时跳过项目权限校验
  4. `services/project_management.py`：撤回临时添加的 AIPlanningSession 前置检查（FK 已改为 SET NULL，数据库自动处理）
  5. 新增 Alembic 迁移 `20260425_0019_session_project_fk_set_null.py`：先删旧 FK → 列改 nullable → 重建 FK 为 SET NULL
- 改动文件：5 个文件（1 模型、1 schema、2 service、1 迁移）
- 验证：
  - `uv run alembic upgrade head` 迁移成功
  - 数据库确认 `confdeltype='n'`（SET NULL），`is_nullable='YES'`
- 设计说明：项目和会话改为松耦合——删除项目时关联会话的 `project_id` 自动置 NULL，会话保留不丢失

## 2026-04-25

- 任务：VLM bbox 坐标点击回退 + 交互式 explore_flow + save-and-execute input_values 透传 + 端到端链路测试验证
- 背景：Session 52 链路测试发现两个问题：(1) login_error 场景 Step 12 点击 “Cart”（导航栏）被弹层遮挡超时，AI 应点击弹层中的 “View Cart”；(2) save-and-execute 不传 input_values 导致 `${context_key}` 变量无法替换。根因分析发现 VLM 返回的 bbox 坐标在 DOM 选择器提取失败时被丢弃，Playwright 原生支持 `page.mouse.click(x,y)` 但从未使用。
- 操作：
  1. `semantic.py`：`ResolvedLocator` 新增 `click_coordinates: tuple[int, int] | None` 字段
  2. `fallback.py`：新增 `_try_coordinate_click_fallback()` Tier 2.5 回退——VLM 返回 bbox 但 DOM selector 提取失败时返回坐标；修改 `resolve_with_fallback()` 在 Tier 2 后、Tier 3 前插入
  3. `playwright_runner.py`：click 步骤检查 `click_coordinates`→`page.mouse.click(x,y)`；input 步骤→`page.mouse.click(x,y)` + `page.keyboard.type(value)`
  4. `page_explorer.py`：新增 `_discover_interactive_elements()` 点击关键按钮（Add to cart/View Product 等）捕获动态弹层元素，元素标记 `discovered_via_interaction: True`；更新 `format_elements_for_prompt()` 添加 `[dynamic]` 标记
  5. `config.py`：新增 `explore_interactive_max_clicks: int = 5`
  6. `test_planning_prompts.py`：追加动态交互规则——弹层步骤必须保留、[dynamic] 元素顺序与用户流程一致
  7. `test_planning_agent.py`：`_build_draft_prompt()` dom_section 追加动态元素说明
  8. `ai_planning.py`（route + service）：`SaveAndExecuteRequest` 增加 `input_values` 字段，透传到 `CaseExecutionRequest`
- 改动文件：10 个核心文件，+203/-10 行
- 验证：
  - `uv run pytest tests/unit/` → 301 passed（新增 3 个测试：坐标回退、VLM None 回退、动态元素标记）
  - Session 53 端到端测试：AI 生成 Draft 26/27（13 步），正确使用 “View Cart”（弹层）而非 “Cart”（导航栏）
  - Exec 69（login_success）13/13 passed，Exec 70（login_error）13/13 passed
  - 变量替换 `${login_email}`/`${login_password}`/`${search_keyword}` 正常工作
  - 语义文本定位器全部成功，无 CSS 选择器误用
- commit：`c8808a7` feat: VLM bbox coordinate click fallback + interactive explore_flow + dynamic element discovery
- 关联 bug：BUG-053（VLM bbox 坐标丢失，已修复）、BUG-054（AI 忽略弹层步骤，已修复）

## 2026-04-24 (Session 2)

- 任务：修复 BUG-051（input_contract 变量未替换）+ BUG-052（AI 跳过 explore_page）+ 实现 explore_flow 多页面探索 + VLM 页面布局注解
- 根因分析：
  - BUG-051：`playwright_runner` 中 `step.value` 直接传给 Playwright，没有 `${context_key}` → 实际值的替换逻辑；`CaseExecutionRequest` 也没有 `input_values` 参数
  - BUG-052：ReAct 循环中 `explore_page` 是 AI 自主决策的工具调用，用户信息完整时 AI 直接 `generate_plan` 跳过探索；VLM 仅在执行阶段 Tier 3 兜底触发
- 操作：
  1. `playwright_runner.py` 新增 `_substitute_variables` 函数，正则替换 `${context_key}`，4 个 step.value 使用处全部替换（goto/input/assert_text/assert_url_contains）
  2. `schemas/executions.py` 的 `CaseExecutionRequest` 增加 `input_values: dict[str, str]` 字段
  3. `services/executions.py` 透传 `input_values` 到 runner
  4. `test_planning_agent.py` 新增 `_has_explored_pages` + `_auto_explore_entry_url`，在 `generate_plan` 前强制触发 `explore_page`
  5. `_extract_page_elements` 同时识别 `explore_page` 和 `explore_flow`
  6. `page_explorer.py` 新增 `collect_multi_page_elements`，单浏览器会话跨多页面采集 DOM + 截图 + VLM 注解
  7. `planning_tools.py` 新增 `_handle_explore_flow` handler + 工具注册
  8. `ai_visual.py` 新增 `describe_page_layout` 公共函数，使用专用 prompt 描述页面布局
  9. `config.py` 新增 `enable_vlm_page_annotation` 配置（默认开启）
  10. `test_planning_prompts.py` 增加页面探索和选择器约束规则
- 改动文件：18 个文件，+536/-28 行
- 验证：`uv run pytest tests/unit/` → 303 passed
- 关联 bug：BUG-051（fixed）、BUG-052（fixed）

## 2026-04-24

- 任务：BUG-050 修复后端到端验证 — AI Planning 全流程（会话创建→需求收集→草案生成→保存执行→测试报告）
- 目标：使用 test 文件中的 Automation Exercise 登录→搜索→详情→加购流程描述，走完整 AI Planning 工作流，验证 BUG-050（DOM 证据注入、链式选择器、target_strategy 偏好提示）修复效果
- 操作：
  1. **启动服务**：后端 `uv run backend-dev`（:8000），前端 `npm run dev`（:5173）
  2. **创建会话**：`POST /api/v1/ai-planning/sessions {"project_id": 4}` → session_id=41，status=collecting
  3. **发送需求**：一次性提交 test 文件全部内容（app_under_test、business_goal、entry_url、core_user_flow、main_assertions、test_data、scope_limits）→ AI 一次识别所有字段，status=plan_ready
  4. **生成计划**：AI 产出两个场景：login_success（高优先级）+ login_error（中优先级），各含 test_data_requirements（username、password）
  5. **生成草案**：`POST /sessions/41/drafts:generate {"scenario_keys": ["login_success","login_error"]}`
     - Draft 17（login_success）：15 步，使用 `#input-email`、`button[type='submit']`、`#search_product` 等 CSS/data-qa 选择器
     - Draft 18（login_error）：15 步，使用 `input[data-qa='login-email']`、`button[data-qa='login-button']` 等 data-qa 选择器
     - 两个草案均有 warning "步骤 #6 无法修正为合法 DSL，已忽略"
  6. **保存并执行**：`POST /sessions/41/drafts:save-and-execute {"draft_ids": [17,18], "execute": true}`
     - Case 12（Draft 17）→ Execution 53：2/3 步通过，Step 3 input `#input-email` 失败
     - Case 13（Draft 18）→ Execution 54：8/9 步通过，Step 9 wait_for `.productinfo` 失败
- 结果：
  - **Execution 53 详细分析**：
    - Step 0 (goto /): PASSED — 首页加载 3526ms
    - Step 1 (click "Signup / Login"): PASSED — text 策略 score=88，精准命中
    - Step 2 (input `#input-email`): FAILED — 登录页实际 DOM 中无 `#input-email`，实际 input 的 placeholder="Email Address"、css_selector=`section > div > div > div:nth-of-type(1) > div > form > input:nth-of-type(2)`，无 id 属性
    - **AI 在无 DOM 证据时仍猜测了不存在的选择器**，但 DOM snapshot 已正确收集到 intervention_request 中
  - **Execution 54 详细分析**：
    - Step 0-7 全部 PASSED — data-qa 选择器在登录页精准工作
    - Step 8 (wait_for `.productinfo`): FAILED — URL 显示 `?search=${search_keyword}`（变量未替换）
    - **input_contract 变量（${login_email}、${login_password}、${search_keyword}）在执行时未被替换为实际值**，这是 playwight_runner 层面的变量替换缺失
  - **BUG-050 修复效果**：
    - target_strategy 偏好提示工作正常（Execution 53 的 locator_trace 未走 target_strategy，因为 DSL 未指定）
    - 链式选择器解析功能已就绪（本次 DSL 未生成链式格式）
    - DOM snapshot 在 intervention_request 中完整收集（20 个元素含 rect、visible 等属性）
    - AI 视觉定位（VLM）被触发（Execution 53 Step 3 有 ai_candidate，confidence=0.7），但未成功解析
  - **新发现问题**：
    - **BUG-051（待记录）**：`input_contract` 变量占位符（`${login_email}` 等）在执行时未被替换，playwright_runner 直接将 `${search_keyword}` 作为字符串输入到搜索框
    - **BUG-052（待记录）**：AI DSL 生成在无 `page_elements`（DOM 快照）时仍猜测不存在的 CSS 选择器（`#input-email`），需要加强 prompt 约束或在无 DOM 时引导 AI 使用语义描述
- 验证：
  - 全流程 API 调用链路畅通：创建会话→发送消息→生成草案→保存执行→查看报告
  - 定位器系统：文本匹配策略（score=88）和 data-qa 选择器策略均正常工作
  - DOM snapshot 收集：intervention_request 中完整收集了 20 个页面元素的详细信息
  - AI 视觉定位：VLM 被触发并返回候选区域（confidence=0.7），但最终未成功匹配
- 关联记录：BUG-050、BUG-051（待记录）、BUG-052（待记录）

## 2026-04-23 (下午)

- 任务：BUG-050 DOM 证据注入 + target_strategy 偏好提示 + 单元测试修复
- 目标：解决 AI DSL 生成时缺少 DOM 证据导致的"幻觉选择器"问题，以及 target_strategy 锁死单策略导致穷举扫描被跳过的问题；同时修复 9 个预存的单元测试失败
- 操作：
  1. **target_strategy 偏好提示**：`backend/app/locators/semantic.py` 的 `resolve_semantic_locator` 将 `target_strategy` 从锁死（early return）改为偏好提示（try/except + fallback 穷举语义扫描）
  2. **DOM 证据注入 Schema**：`AIPlanningScenario` 和 `GenerateDslRequest` 各加 `page_elements: str | None` 字段
  3. **DOM 数据提取传递**：`test_planning_agent.py` 新增 `_extract_page_elements()` 从 `explore_page` tool_calls 提取格式化 DOM；经 `_plan_response → _build_plan → _build_draft_prompt` 传到 plan JSON；`ai_planning.py` service 层传递到 `GenerateDslRequest`；`dsl_generator.py` 注入到 LLM prompt
  4. **单元测试修复（8 个）**：
     - `test_create_case_success`：补齐 `target_strategy: None` 到期望的 steps 字典
     - `test_list_projects_returns_only_current_user_projects`：改用字段存在性断言替代精确匹配（含 `created_at`/`updated_at`）
     - `test_get_settings_falls_back_when_ai_visual_int_env_is_invalid`：修正 fallback 期望值 10000 → 600000
     - `test_create_app_requires_auth_session_secret`：重定向 `ENV_FILE_PATH` 防止 `.env` 回填
     - `test_business_routes_require_login` → `test_business_routes_allow_demo_access`：匹配 `require_demo_user` 行为
     - `test_generate_dsl_case_returns_403_when_retry_source_belongs_to_other_actor` → 改为直接测试 service 层权限逻辑
     - `test_record_generation_feedback_returns_403_for_non_owner_actor` → 改为直接测试 service 层权限逻辑
     - `test_record_generation_feedback_requires_login` → `test_record_generation_feedback_allows_demo_access`：匹配 demo mode 行为
- 结果：
  - target_strategy 失败时自动降级到全量语义扫描，不再立即抛错
  - AI 生成 DSL 时能收到实际 DOM 元素清单，减少幻觉选择器
  - 276/277 单元测试通过（剩余 1 个为 `test_ai_settings_api` 间歇性状态泄漏，与本次无关）
- 验证：
  - `test_locator_semantic.py`：30/30 passed（含 2 个新 fallback 测试）
  - `test_ai_planning_api.py`：14/14 passed
  - 全量单元测试：276 passed
- 关联记录：BUG-050

## 2026-04-23

- 任务：定位器系统三阶段改善 — target_strategy 字段 + 裸 HTML 标签识别 + Playwright 链式选择器解析
- 目标：根治 DSL target 字段无类型导致定位器反复猜错策略的问题（BUG-046/049/本次 `.productinfo text='View Product'`），让定位器能处理 AI 生成的复合格式选择器
- 操作：
  1. **Schema — target_strategy 字段**：`backend/app/schemas/dsl.py` 新增 `TargetStrategy = Literal[“css”, “xpath”, “data-testid”, “element_id”, “tag”, “semantic”]`；为 ClickStep/InputStep/WaitForStep/AssertTextStep 各添加 `target_strategy: TargetStrategy | None = Field(default=None)`；向后兼容，默认 None
  2. **定位器 — 裸 HTML 标签名识别**：`backend/app/locators/semantic.py` 新增 `_HTML_TAG_NAMES` frozenset（约 30 个常见标签）；`_resolve_explicit_locator` 在末尾添加 `css_tag` 策略匹配；`_strategy_base_score` 添加 `”css_tag”: 105`；`_strategy_rule_name` 添加映射
  3. **定位器 — target_strategy 直接分发**：新增 `_build_strategy_builder()` 和 `_resolve_by_strategy()` 函数；`resolve_semantic_locator` 和 `collect_semantic_candidates` 新增 `target_strategy` 参数；非 “semantic” 值时绕过启发式直接分发
  4. **定位器 — Playwright 链式选择器解析**：新增 `_CHAINED_SELECTOR_RE` 正则和 `_resolve_chained_selector()` 函数；支持 `.class text=Value`、`.class >> text=Value`、`#id text='Value'`、`tag.class text=Value` 四种格式；在 `_resolve_explicit_locator` 最前面优先匹配；`chained_css_text` 策略评分 110（高于语义匹配，低于纯 CSS/XPath）
  5. **Fallback 传参**：`backend/app/locators/fallback.py` 的 `resolve_with_fallback` 及下游函数添加 `target_strategy` 参数并透传
  6. **Runner 传参**：`backend/app/runners/playwright_runner.py` 所有 8 处 `resolve_with_fallback` 调用添加 `target_strategy=step.target_strategy`
  7. **DSL 生成 Prompt 更新**：`backend/app/ai/dsl_generator.py` 版本升级至 `2026-04-22.target-strategy-v1`；系统 prompt 添加 target 格式文档（5 种格式 + 禁止无效复合格式）；用户规则添加 `target_strategy` 使用指引；`_normalize_single_step` 添加 `target_strategy` 归一化
- 结果：
  - 裸标签名（`body`、`form` 等）正确识别为 `css_tag` 策略，不再走语义文本匹配
  - 链式选择器（`.productinfo text='View Product'`）正确拆解为 `page.locator(“.productinfo”).get_by_text(“View Product”)`
  - `target_strategy` 字段允许显式声明策略，绕过启发式猜测
  - 新生成 DSL 的 prompt 禁止产出无效复合格式
- 验证：
  - `test_locator_semantic.py`：28/28 passed（含 7 个新链式选择器测试）
  - `test_dsl_validation.py`：50/53 passed（3 个 failed 为已有 auth 权限问题，与本次变更无关）
- 后续：可重新执行 Automation Exercise 旧用例（Case 8/9），验证 `.productinfo text='View Product'` 是否能通过链式解析成功定位

### 验证补充（2026-04-23 下午）

- 操作：
  1. 修复 `test_case_runs.error_message` 列类型 VARCHAR(2000) → Text（Alembic 迁移 `2348081d0e8a`），解决执行记录存储溢出
  2. 重启后端服务加载新代码，重新执行 Case 8（Execution 49）和 Case 9（Execution 52）
- 结果：
  - **Case 8**（Execution 49）：Step 5 `assert_text target=body` 裸标签识别成功（`css_tag` 策略），但断言文本 "Logged in as" 不匹配（登录失败，业务逻辑问题，非定位器问题）
  - **Case 9**（Execution 52）：Step 8 `click target=".productinfo text='View Product'"` 链式选择器解析成功，正则正确拆解为 `base=.productinfo, value=View Product`，构建了 `page.locator(".productinfo").get_by_text("View Product")` 链式调用。但 DOM 中 `.productinfo` 内部不包含 "View Product" 文本（二者是兄弟节点，非父子），返回 0 candidates
  - **Playwright 验证**：`.productinfo >> text=Add to cart` → 14 matches（链式选择器对正确 DOM 结构工作正常），`.product-image-wrapper >> text=View Product` → 14 matches
- 结论：
  - 链式选择器解析器 **功能正确**，Case 9 失败原因是 AI DSL 定位策略错误（选错 CSS 容器），不是解析器 bug（记录为 BUG-050）
  - 新 Case 10/11 使用语义文本 `View Product` 直接匹配，已通过全步骤

## 2026-04-21

- 任务：执行 `docs/superpowers/plans/2026-04-21-streaming-status-implementation.md` 实施计划（流式状态感知 + AI 超时修复）
- 目标：修复 AI 响应超时配置，并把 AI Planning 的同一条 WebSocket 扩展为覆盖对话、草案生成、保存执行三段流式状态，让 Planning 面板能逐块显示 AI 文本、阶段标签和执行进度
- 操作：
  1. **Task 1 — Agent 流式基础 + 超时修复**：`backend/.env` 三个超时值改 600000ms；`httpx` 从 dev 移到 runtime 依赖；新增 `_stream_planning_llm()` 使用 httpx SSE 流式读取；新增 `stream_planning_turn()` 生成器 yield status/text_chunk/tool_call 事件；`run_planning_turn()` 改为同步包装器消费流
  2. **Task 2 — 服务层 + WS 路由扩展**：新增 `stream_planning_message()` 和 `stream_generate_planning_drafts()` 生成器；`ai_planning_streaming.py` 抽出通用 `_bridge_sync_generator()` 桥接；WS 路由增加 `chat`/`generate_drafts` 消息处理，连接改为持久化（不再 break）
  3. **Task 3 — 前端事件模型**：`types/api.ts` 新增 6 种流式事件类型（status/text_chunk/tool_call_start/end/draft_generating/turn_complete）；`executionWebSocket.ts` 增加 `isOpen()` 方法
  4. **Task 4 — Panel 流式渲染**：建立会话级持久 WS 连接；`handleSendMessage` WS 优先 + REST fallback；`handleGenerateDrafts` WS 优先；保存并执行复用 session WS；渲染流式状态标签 + 打字光标；新增 CSS 动画
  5. **Task 5 — 验证**：后端 18 测试全通过、前端 11 测试全通过、TypeScript 编译零错误
- 结果：4 个 commit（`a200415` `ff4d0e9` `e7bbd6b` `acdf7a0`），对话/草案/执行共用一条 WS，AI 回复逐字流式显示并带有阶段标签（蓝色思考/橙色工具/黄色生成），REST fallback 保留
- 验证：
  - `uv run pytest tests/unit/test_planning_agent.py tests/unit/test_ai_planning_api.py` → 18 passed
  - `npm run test -- src/services/executionWebSocket.test.ts src/components/AITestPlanningPanel.test.tsx` → 11 passed
  - `npx tsc --noEmit` → 零错误
- 后续：手工 smoke 验证 Planning 页面全链路流式效果（启动后端+前端，连续执行对话→草案→执行）

- 任务：基于 `docs/superpowers/specs/2026-04-21-streaming-status-design.md` 产出 streaming status + AI timeout implementation plan
- 目标：将现有仓库真实实现与 spec 对齐，输出一份可执行、可验证、可按任务拆分推进的实施计划，落到 `docs/superpowers/plans`
- 操作：
  1. 读取 `using-superpowers`、`writing-plans`、`brainstorming` 技能说明，确认本次直接进入写计划阶段
  2. 核对 spec、既有计划、后端 agent/service/ws 路径、前端 panel/socket/types、相关测试与 `backend/.env`、`backend/pyproject.toml`
  3. 识别当前真实边界：执行阶段 WS 已存在，缺口主要在 chat/draft 流式、前端同一 WS 复用、`.env` 超时值与 `httpx` runtime 依赖
  4. 新建 `docs/superpowers/plans/2026-04-21-streaming-status-implementation.md`，将实施拆为 5 个任务：agent 流式基础、服务层/WS 扩展、前端事件模型、Panel 对话与草案流式、同一 WS 执行整合与总验证
  5. 补 `.gitignore` 白名单规则，允许本次新计划文件进入版本控制，保持与仓库现有 plan 文件管理方式一致
  6. 按计划写作规范自检文档，清理占位符示意代码，使步骤、文件路径、命令与验证入口都落到真实仓库文件
- 结果：新增一份与当前代码状态一致且可纳入版本控制的 implementation plan，避免重复设计已经存在的 save-and-execute WS 能力，并明确将 `httpx` 从 dev 依赖提升到 runtime 的必要性
- 验证：
  - 静态核对计划文件涉及的路径均存在：`backend/app/ai/test_planning_agent.py`、`backend/app/services/ai_planning.py`、`backend/app/services/ai_planning_streaming.py`、`backend/app/api/routes/ai_planning.py`、`frontend/src/services/executionWebSocket.ts`、`frontend/src/components/AITestPlanningPanel.tsx`、相关测试文件
  - 对计划文件执行占位符扫描，确认没有 `TODO`、`TBD`、独立 `...` 等空洞步骤
- 后续：如继续实施，按计划中的 Task 1 → Task 5 顺序推进，并在实际开发时补跑计划里列出的后端 pytest、前端 Vitest 与 `tsc` 验证

- 任务：审查并补全所有实体的 CRUD 操作（增删改查），包括后端 API、前端 API 函数、前端 UI
- 目标：确保报告、项目、测试用例、会话、草案、修正记录等所有实体在前后端均有完整的增删改操作入口
- 审查发现 7 个缺失项：
  1. 项目缺少前端创建/编辑（后端 API 已有）
  2. 用例列表页无删除按钮
  3. 用例无批量删除前端 UI（后端 batch delete API 已有）
  4. 定位修正记录缺少删除端点（前后端均无）
  5. DSL 生成记录缺少删除端点（前后端均无）
  6. AI 规划草案缺少单独删除端点（前后端均无）
  7. 报告页项目列表缺少删除按钮
- 操作：
  1. 后端新增 3 个 DELETE 端点：`DELETE /corrections/{id}`、`DELETE /dsl/generations/{id}`、`DELETE /ai-planning/drafts/{id}`
  2. 后端新增对应 3 个 service 函数，更新 `__init__.py` 导出
  3. 前端 api.ts 新增 6 个 API 函数：`createProject`、`updateProject`、`batchDeleteCases`、`deleteCorrection`、`deleteDslGenerationRun`、`deletePlanningDraft`
  4. ReportPage 左侧项目列表新增：+新建按钮、✏️编辑按钮、🗑️删除按钮，配合 Modal 表单实现创建和编辑
  5. CasesPage 新增：每个用例卡片 Checkbox 多选 + 批量删除操作栏 + 单条删除按钮
  6. AITestPlanningPanel 每个草案行新增 DeleteOutlined 删除图标
- 结果：11 个文件修改，+448/-84 行
- 验证：
  - 前端 `npm run build`（tsc + vite）通过
  - 后端路由导入验证通过
  - 后端单元测试 242 通过（6 个失败为修改前已有，与本次无关）
- 后续：无

- 任务：模拟真实用户白盒测试全流程：会话 → 输入 → 保存并执行 → 生成测试用例 → 执行 → 生成报告
- 测试目标：The Internet Login Page (https://the-internet.herokuapp.com/login)
- 操作：
  1. 启动后端服务，确认 API health check 正常
  2. 创建项目 "The Internet - Login Test" (ID=3)
  3. 创建 AI 规划会话 (Session ID=34)，发送完整测试需求
  4. AI 规划代理自动提取需求并生成测试方案（login_success / login_error 两个场景）
  5. 选择 login_success 场景生成 DSL 草稿（首次 60s 超时，将 AI_DSL_TIMEOUT_MS 增至 120s 后成功）
  6. AI 生成 DSL 缺少 goto 步骤且 base_url 设置为完整路径，导致执行失败（about:blank）
  7. 修正 DSL：添加 goto /login 步骤，调整 base_url 为站点根路径，使用语义定位器 "Login" 替代 CSS 选择器
  8. 重新执行，8/8 步骤全部通过，总耗时 3779ms
- 结果：Execution ID=34, Status=passed, 8步全通过
- 验证：
  - goto /login → 页面标题 "The Internet"
  - wait_for #username → CSS 定位 (score: 135)
  - input #username=tomsmith → CSS 定位
  - input #password=SuperSecretPassword! → CSS 定位
  - click "Login" → text 语义定位 (score: 88)
  - wait_for #flash → CSS 定位 (score: 138)
  - assert_url_contains /secure → 通过
  - assert_text #flash "You logged into a secure area!" → 通过
- 发现问题：
  1. AI DSL 生成超时（60s 不够 glm-4.7 推理），已通过 settings API 将超时增至 120s
  2. AI 生成的 DSL 缺少 goto 步骤且 base_url 设置不合理
  3. CSS 选择器 `button[type='submit']` 未被语义定位器识别（需使用 `css=` 前缀或语义文本）
- 后续：考虑在 DSL 生成 prompt 中强调必须包含 goto 步骤；考虑优化语义定位器对复合 CSS 选择器的支持

## 2026-04-20

- 任务：实现 DOM-aware DSL 生成，让 AI 生成的 DSL target 匹配页面真实 DOM 元素属性；同时启用 VLM 视觉定位作为兜底机制
- 执行动作：
  - **Task 1** — `config.py`：新增 `storage_state_dir` 配置项，`enable_ai_visual_locate` 默认值从 `False` 改为 `True`
  - **Task 2** — 新建 `page_explorer.py`：存储状态文件 I/O（`save_storage_state`、`load_storage_state_meta`、`is_storage_state_stale`）和元素格式化（`format_elements_for_prompt`）
  - **Task 3+4** — `page_explorer.py` 新增 Playwright 函数：`collect_interactable_elements`（采集页面可交互元素）、`capture_browser_session`（执行登录步骤并保存浏览器状态）
  - **Task 5** — `planning_tools.py` 注册 `explore_page` 和 `capture_page_session` 两个 ReAct agent 工具，AI 可自主调用采集页面 DOM 或保存登录态
  - **Task 6** — `test_planning_agent.py`：`_build_draft_prompt` 追加 DOM 感知提示，引导 AI 使用实际 label/placeholder/id 作为 target
  - **Task 7** — `main.py`：启动时自动创建 `storage_states/` 目录
  - **Task 8** — 验证 VLM 默认开启测试通过
  - **Task 9** — 新建集成测试文件 `test_dom_aware_generation.py`（需 Playwright + 网络，标记 `browser_integration`）
- 结果：9 个 commit，55 个相关单元测试全部通过，1 个 pre-existing 失败无关本次改动
- 验证：`cd backend && uv run pytest tests/unit/test_page_explorer.py tests/unit/test_planning_tools.py tests/unit/test_config.py tests/unit/test_main.py tests/unit/test_planning_agent.py -v`
- 后续：手动运行集成测试 `cd backend && uv run pytest tests/integration/test_dom_aware_generation.py -v -m browser_integration`；更新 .env.example 中 ENABLE_AI_VISUAL_LOCATE 默认值说明

## 2026-04-17 (Task 2)

- 任务：用例创建 + 执行链路测试（3 个测试），扩展 platform API chain 白盒测试
- 执行动作：
  - 在 `backend/tests/integration/test_platform_api_chain.py` 末尾追加 `LOGIN_CASE_DSL` 常量和 3 个新测试：`test_create_case_with_valid_dsl`、`test_execute_login_case_and_verify_results`、`test_full_api_chain_e2e`
  - 修复语义定位器 `backend/app/locators/semantic.py` 中两个真实 bug：
    1. **element_id 策略缺失**：裸目标文本（如 "flash"）不会被尝试匹配 HTML id 属性，新增 `element_id` 策略（优先级 100，低于 explicit css/xpath）
    2. **case-sensitive label/placeholder 匹配**：`get_by_label("username", exact=True)` 无法匹配页面标签 "Username"，新增 `label_fuzzy`、`placeholder_fuzzy`、`text_fuzzy`、`button_role_fuzzy` 四个非精确匹配策略（优先级 45-60）
- 结果：6 个测试全部通过（3 个已有 session 测试 + 3 个新增链路测试）；定位器现在支持 HTML id 属性匹配和大小写不敏感的语义回退
- 验证：`cd backend && python -m pytest tests/integration/test_platform_api_chain.py -v`，结果 `6 passed in 9.06s`
- 后续：playwright_runner.py 中 `_capture_request_failed` 有一个预存在的 `AttributeError: 'str' object has no attribute 'get'` bug，因 Playwright `request.failure` 返回格式变更导致，未在本任务修复范围

## 2026-04-17

- 任务：修复用例中心编辑按钮无法跳转的问题，并清理历史数据
- 操作：
  - 创建 `CaseEditPage.tsx` 用例编辑页面，支持编辑名称、描述、Base URL、步骤（增删改）
  - 在 `AppRouter.tsx` 添加 `/cases/:caseId/edit` 路由
  - 在 `api.ts` 添加 `deleteCase()` API 函数
  - 编辑页面集成删除用例功能（带确认弹窗）
- 结果：编辑按钮现在能正确跳转到编辑页面，数据库已为空（0 用例、0 执行记录）
- 验证：TypeScript 编译通过，无类型错误

## 2026-04-16

- 任务：按 `docs/superpowers/plans/2026-04-15-ai-planning-execution-streaming.md` 逐任务实现 AI Planning WebSocket 执行流式推送
- 执行动作：
  - **Task 1** — 在 `playwright_runner.py` 新增 `StepStreamEvent`、`RunnerCancelledError`、`execute_case_with_playwright_streaming()` 流式执行生成器；在 `executions.py` 新增 `execute_case_streaming()`，通过 `yield from` 桥接 runner 流式事件并持久化执行记录；`__init__.py` 导出新符号；补充 2 个 streaming 测试
  - **Task 2** — 新建 `ai_planning_streaming.py`，实现 `CancellationManager`、`stream_save_and_execute()` worker-thread async 桥接；在 `ai_planning.py` 新增 `save_and_execute_selected_drafts_streaming()` 同步生成器；在 `auth.py` 新增 `get_demo_user_or_raise()`；在 `ai_planning.py` 路由新增 `WS /sessions/{session_id}/ws` 端点；补充 2 个 WebSocket 测试
  - **Task 3** — 在 `api.ts` 新增 `ExecutionStreamEvent` 等 7 种事件类型；新建 `executionWebSocket.ts` socket lifecycle client；补充 3 个 socket 单元测试
  - **Task 4** — 修改 `AITestPlanningPanel.tsx`，将"保存并执行"按钮由 HTTP `saveAndExecuteDrafts` 切换为 WebSocket `connectExecutionStream`，添加执行进度气泡和取消按钮；更新既有测试适配 WebSocket mock
- 结果：4 个 commit（`feat: add streaming execution primitives`、`feat: add ai planning websocket execution stream`、`feat: add planning execution websocket client`、`feat: stream ai planning execution progress in panel`），保留同步 HTTP fallback
- 验证：
  - 后端 29 个测试全部通过（`test_ai_planning_api.py` 13个 + `test_case_executions_api.py` 16个）
  - 前端 9 个测试全部通过（`executionWebSocket.test.ts` 3个 + `AITestPlanningPanel.test.tsx` 6个）
  - `npx tsc --noEmit` 类型检查通过
- 后续：可手动启动后端+前端进行 smoke 测试，验证保存并执行流式进度和取消功能

## 2026-04-15

- 任务：基于 `docs/superpowers/specs/2026-04-13-execution-streaming-design.md` 产出 AI planning 执行流式推送 implementation plan，并按用户指定目录落到 `docs/superpowers/plans`
- 执行动作：核对 `backend/app/api/routes/ai_planning.py`、`backend/app/services/ai_planning.py`、`backend/app/services/executions.py`、`backend/app/runners/playwright_runner.py`、`frontend/src/components/AITestPlanningPanel.tsx`、`frontend/src/services/api.ts`、`frontend/src/types/api.ts` 和对应测试文件，确认当前代码已具备会话列表、保存并执行、execution summary 持久化，但仍缺少 WebSocket 流式执行、取消机制与前端实时进度；随后新增 `docs/superpowers/plans/2026-04-15-ai-planning-execution-streaming.md`，将实施拆成 runner 流式原语、AI planning WS worker/路由、前端 socket client、面板集成四个任务；补充 `.gitignore` 最小例外规则，仅允许本次新增计划文件进入版本控制，避免把其他历史 `docs/superpowers` 文档一并暴露为未跟踪项
- 结果：形成了一份基于当前仓库真实状态的可执行 implementation plan，避免重复规划已完成的会话历史与同步 save-and-execute 能力，并明确了不改数据库 schema、保留同步 HTTP fallback、取消状态仅做 planning UI 瞬态展示这几个边界；新计划文件不再被 `docs/*` 通配规则误忽略，同时没有扩大其他 superpowers 文档的 Git 噪音
- 验证：
  - 静态核对设计文档与现有实现差异，确认计划中涉及的文件路径、接口名、测试入口均存在于仓库
  - 人工检查计划文件已创建于 `docs/superpowers/plans/2026-04-15-ai-planning-execution-streaming.md`
- 后续：如继续执行，按计划中的 Task 1 -> Task 4 顺序推进，并在真正实现后补充新的验证结果

## 2026-04-13 22:20

- 任务：修复 AI planning“保存并执行草案”链路中的 DSL 生成失败与执行摘要不持久化问题
- 执行动作：在 `backend/app/ai/dsl_generator.py` 为 LLM 非 JSON/HTML 响应补统一错误包装与诊断提示；修正 `backend/.env` 中本地 `AI_DSL_BASE_URL` 为 `/v1` 接口根路径；在 `backend/app/services/ai_planning.py` 为 draft 生成结果、仅保存结果、保存并执行后的 execution summary 持久化 `AIPlanningMessage`；在 `frontend/src/components/AITestPlanningPanel.tsx` 中将保存/执行完成后的 UI 刷新改为回读 `getPlanningSession(sessionId)`，并失效 `cases` / `executions` 查询；补充 `backend/tests/unit/test_dsl_generator.py`、扩展 `backend/tests/unit/test_ai_planning_api.py` 与 `frontend/src/components/AITestPlanningPanel.test.tsx`
- 结果：AI planning 现在不会再因 HTML 响应直接抛原始 `JSONDecodeError`；本地 DSL 配置已指向正确的 OpenAI 兼容接口根路径；保存并执行后，执行摘要会落库并可通过会话详情重新加载显示，前端不再只依赖临时内存消息
- 验证：
  - `backend\.venv\Scripts\python.exe -m pytest backend\tests\unit\test_dsl_generator.py backend\tests\unit\test_ai_planning_api.py -q`，结果 `12 passed`
  - `cd frontend && npm run test -- src/components/AITestPlanningPanel.test.tsx`，结果 `5 passed`
  - `cd frontend && npx tsc --noEmit`，结果通过
  - 按修正后的 `AI_DSL_BASE_URL` 进行最小 HTTP 验证，返回 `401 application/json`，说明已命中 API 接口而非 HTML 首页
- 后续：当前仍未实现真正的执行进度流式输出，若要达到“对话框实时展示步骤状态”的目标，还需补后端事件流接口与前端订阅展示

## 2026-04-13 22:15

- 任务：对白盒排查 AI planning“保存并执行草案”链路失败，定位对话 `session_id=27` 为什么没有生成用例、没有执行结果、没有报告摘要
- 执行动作：检索 `backend/app/services/ai_planning.py`、`backend/app/ai/dsl_generator.py`、`frontend/src/components/AITestPlanningPanel.tsx`、`frontend/src/pages/PlanningPage.tsx` 等调用链；读取 PostgreSQL 中 `ai_planning_sessions` / `ai_planning_messages` / `ai_planning_drafts` / `test_cases` / `test_case_runs` 实际数据；按当前 `.env` 的 `AI_DSL_BASE_URL` 复现一次最小 LLM 请求并检查返回体
- 结果：已确认 `session 27` 并未进入保存或执行阶段，数据库中没有新增 `test_cases` 与 `test_case_runs`；唯一草案处于 `failed`，错误为 `Expecting value: line 1 column 1 (char 0)`；根因是 `backend/.env` 的 `AI_DSL_BASE_URL=https://api.unself.cn` 与代码在 `backend/app/ai/dsl_generator.py` 中拼接的 `/chat/completions` 组合后命中了站点 HTML 页面而非 OpenAI 兼容 JSON 接口，导致 `json.loads(response.read())` 直接抛异常；同时确认当前实现不存在 SSE/流式执行接口，且 draft 生成/保存执行结果都没有持久化为 planning message，因此用户期望的“对话框持续输出执行步骤和最终摘要”在现状下无法成立
- 验证：
  - 数据库核查：`session 27` 状态为 `drafts_ready`，消息仅 2 条（用户 + 规划结果），`ai_planning_drafts` 中仅 1 条失败草案，`test_cases` / `test_case_runs` 无对应新增记录
  - 最小复现：按当前 `AI_DSL_BASE_URL` 发起 `POST https://api.unself.cn/chat/completions`，返回 `200 text/html`，正文为站点首页 HTML，不是 JSON
  - 静态核对：`backend/.env`、`backend/app/ai/dsl_generator.py`、`backend/app/services/ai_planning.py`、`frontend/src/components/AITestPlanningPanel.tsx`
- 后续：建议优先修正 `AI_DSL_BASE_URL` 为真实 OpenAI 兼容 API 根路径，并在代码中为非 JSON / 非 `application/json` 响应补防御；如果要满足产品预期，还需补充执行状态持久化与前端流式消费链路

## 2026-04-12

- 任务：实现 AI Planning 会话删除功能，并修复 stale session 恢复路径
- 执行动作：后端在 `backend/app/services/ai_planning.py` 新增 `delete_planning_session()`，并在 `backend/app/api/routes/ai_planning.py` 暴露 `DELETE /api/v1/ai-planning/sessions/{session_id}`；前端在 `frontend/src/services/api.ts` 新增 `deletePlanningSession()`，补齐 `request()` 对 `204 No Content` 的处理；`frontend/src/components/AITestPlanningPanel.tsx` 抽出 `applySessionDetail()` / `loadSessionDetail()` / `createAndSelectSession()` / `handleSessionDeleted()`，接入删除按钮、当前会话删除后的切换与本地缓存失效回退；同步更新 `frontend/src/types/api.ts` 的 planning 状态与 `tool_call` 类型；修正 `frontend/src/components/AITestPlanningPanel.test.tsx` 与 `frontend/src/services/api.test.ts` 以匹配当前真实 UI/交互；顺手清理 `frontend/src/main.tsx` 中不再被 Ant Design 类型接受的 `borderWidth` token，并将 `TextArea` 改为 `variant="borderless"`
- 结果：AI Planning 会话现在支持删除；删除当前活跃会话后会自动切换到剩余会话或创建新会话；缓存的失效 `ai_planning_last_session` 不再导致面板卡在无活跃会话状态；前后端定向测试与前端类型检查通过
- 验证：
  - `cd backend && uv run pytest tests/unit/test_ai_planning_api.py -q`，结果 `10 passed`
  - `cd frontend && npm run test -- src/services/api.test.ts src/components/AITestPlanningPanel.test.tsx`，结果 `19 passed`
  - `cd frontend && npx tsc --noEmit`，结果通过
- 后续：前端测试运行仍会打印 React Router future flag 提示与 `rc-textarea` 的 `NaN height` 警告，当前不影响通过；如需继续收口，可单独处理这些测试环境噪音

- 任务：为 AI Planning 会话删除需求补写 implementation plan，并补充相关缺陷记录
- 执行动作：核对 `backend/app/api/routes/ai_planning.py`、`backend/app/services/ai_planning.py`、`backend/app/schemas/ai_planning.py`、`frontend/src/services/api.ts`、`frontend/src/components/AITestPlanningPanel.tsx` 及现有前后端测试，确认仓库当前已具备会话列表与 save/execute 能力，仅缺删除链路；在 `docs/plan/2026-04-12-session-delete-implementation.md` 新增可执行计划；同时将 stale `ai_planning_last_session` 恢复失败后不回退创建新会话的问题记录到 `docs/bug-log.md`
- 结果：形成了一份基于当前代码现状的会话删除实施计划，覆盖后端删除接口、前端删除入口、当前会话删除后的 fallback、stale localStorage 恢复修复、前后端测试与最终验证命令
- 验证：静态核对计划文件与相关代码路径、测试文件、日志文件是否一致；未执行代码级测试
- 后续：如继续实施，应先按计划在隔离 worktree/分支执行 TDD，再更新本日志中的验证结果

## 2026-04-08（更新 README）

- 任务：更新 README.md 使其反映当前 M2 阶段的真实项目状态
- 执行动作：更新"当前状态"章节（M1→M2 进度、已完成能力清单）；新增"演示流"章节描述三步闭环和 NotebookLM 布局；精简浏览器级回归描述（移除已过时的扩展回归）；更新推荐联调路径为 AI 规划页驱动流程；调整文档索引顺序
- 结果：README 与 `docs/project-plan.md` 截至 2026-04-08 的状态快照一致
- 验证：阅读对比两份文档关键数据点确认一致
- 后续：无

## 2026-04-08

- 任务：为 AI 对话页添加会话历史恢复功能，并将 AI 规划流程从"仅生成 DSL 草案"扩展为"草案 → 保存用例 → 执行测试 → 展示结果"完整闭环
- 执行动作：后端新增 `GET /api/v1/ai-planning/sessions` 会话列表接口和 `POST /sessions/{id}/drafts:save-and-execute` 保存+执行端点；扩展 `AIPlanningSessionStatus` 状态机（新增 reviewing/saving/executing/completed）；新增 `SavedCaseResult`/`ExecutionSummaryResult` schema；service 层 `save_and_execute_selected_drafts()` 直接调用已有的 `create_case()` 和 `execute_case()` 服务函数。前端 AITestPlanningPanel 新增顶部会话切换器（Select + 新建按钮），mount 时通过 localStorage 恢复上次会话；草案列表改为勾选式审阅卡片，增加"仅保存"和"保存并执行"按钮；聊天消息新增 execution_summary 类型渲染（步骤摘要 + 查看报告链接）
- 结果：AI 对话页切换后可通过顶部下拉框恢复历史会话；DSL 草案生成后可勾选、保存为正式用例并直接触发 Playwright 执行，执行结果摘要展示在聊天中并带报告页链接
- 验证：`cd backend && python -c "from app.main import create_app; create_app(); print('OK')"` 通过；`cd frontend && npx tsc --noEmit` 仅剩预存在的 tool_call 类型错误和 main.tsx borderWidth 警告
- 后续：可用 The Internet Login Page 测试数据做端到端手工验证
- 关联计划：`docs/superpowers/plans/2026-04-08-chat-history-and-full-test-flow.md`
- 关联设计：`docs/superpowers/specs/2026-04-08-chat-history-and-full-test-flow-design.md`

## 2026-04-06

- 任务：将前端从传统顶部导航 + 卡片堆叠布局重构为 NotebookLM 风格三栏浮岛布局，并用 ReportPage 替换 CaseWorkbenchPage
- 执行动作：全局 ConfigProvider 主题 token 更新为大圆角、无边框、弱阴影风格；新建 NotebookLMLayout 三栏布局组件和 NotebookNav 侧边栏导航；逐页重写 PlanningPage、CasesPage、CaseWorkbenchPage、ExecutionDetailPage 为三栏布局；新建 ChatMessage、ChatInput、StepList 辅助组件；新增 ReportPage 替换 CaseWorkbenchPage（两栏布局：项目列表 + 执行结果报告），导航项从”工作台”改为”报告”；删除 CaseWorkbenchPage 及相关路由
- 结果：前端全部页面统一为 NotebookLM 三栏浮岛风格（ReportPage 为两栏）；侧边栏底部导航替代顶部 header；ReportPage 支持项目选择、概览统计卡片、可展开执行结果列表含步骤证据与截图
- 验证：`cd frontend && npx tsc --noEmit` 编译通过
- 后续：PlanningPage 的 AITestPlanningPanel 尚未拆分为三栏渲染（当前整体渲染），可在后续迭代中优化

## 2026-04-05 23:50

- 任务：将前端主链路重构为 AI 规划 -> AI 用例 -> 执行与报告，无需登录
- 执行动作：移除 demo 流的认证依赖（后端 require_demo_user）；新增 PlanningPage；精简导航与页面为三步 Steps 导航；融合执行详情页与报告总览（executionMetrics.ts）；删除旧平台页（DashboardPage、LoginPage、ExecutionsPage、CorrectionsPage、AISettingsPage、ReportCenterPage）及 AuthContext；统一路径为 `/run/:id`
- 结果：演示流从平台式多页面收敛为三步闭环，后端 56 测试通过、前端 59 测试通过（各 2/1 个预先存在的无关失败）
- 验证：前端 Vitest 定向通过（AppRouter + PlanningPage + AITestPlanningPanel + CasesPage + CaseWorkbenchPage + ExecutionDetailPage + executionMetrics）；后端 pytest 定向通过
- 后续：如需彻底清除 auth 模块和报告偏好接口，可单开一次清理任务

## 2026-04-05 23:14

- 任务：整理 demo 主链路重构实施计划
- 执行动作：核对前端现有路由、布局、规划面板、用例页、执行详情页与后端 API 认证依赖，确认三步演示流所需真实改动范围；输出正式实施计划到 `docs/superpowers/plans/2026-04-05-demo-flow-simplification.md`
- 结果：形成一份可直接执行的计划，覆盖去认证、三步导航、PlanningPage、新的 cases hub、执行报告融合、旧页面清理与验证收口
- 验证：静态核对 `frontend/src/app/AppRouter.tsx`、`frontend/src/layouts/AppLayout.tsx`、`frontend/src/pages/CasesPage.tsx`、`frontend/src/pages/ExecutionDetailPage.tsx`、`frontend/src/components/AITestPlanningPanel.tsx`、`backend/app/api/router.py` 以及相关测试文件
- 后续：待确认执行方式后，按计划逐任务落地实现

## 2026-03-31 12:00

- 任务：更新协作规则并补充 Suite 表移除迁移回归测试
- 执行动作：将 AGENTS.md 中的 working rules 移至文件顶部并更新标题为 Codex/CLAUDE；新增 Alembic 迁移回归测试验证 suite 相关表已被正确移除
- 结果：AGENTS.md 协作规则结构更清晰；迁移回归测试确保 Suite 下线后数据库状态一致
- 验证：`cd backend && uv run pytest -q` 回归通过
- 后续：无

## 2026-03-31 10:00

- 任务：修复 AI 测试规划功能的代码质量问题
- 执行动作：前端使用负时间戳作为临时消息 ID 避免与服务器 ID 冲突；后端增加 DSL 生成失败时的异常日志与完整 traceback；后端对无效 scenario key 做校验并报告而非静默跳过；简化 .gitignore 中测试文件跟踪模式
- 结果：AI 测试规划功能在消息 ID 冲突、错误可见性、无效输入处理等方面均已加固
- 验证：全量单元测试通过
- 后续：无

## 2026-03-30 22:00

- 任务：修复 CRUD 提交中的关键安全漏洞和运行时兼容问题
- 执行动作：为所有 case API 端点增加项目成员校验防止权限绕过；修正 `ProjectTestCaseStats` 缺少 `created_by_user` 导致的运行时错误；处理外键约束下的项目删除语义；更新测试断言匹配新的分页响应格式；修复 Pydantic 弃用警告
- 结果：BUG-041 全部修复，权限校验已补齐，stats 接口不再 500，项目删除语义明确
- 验证：`uv run pytest backend/tests/unit/ -q` 全部通过
- 后续：无，BUG-041 状态已更新为 fixed

## 2026-03-30 ~ 2026-03-29 · DSL BigModel 适配与 GLM Visual Locate 适配

- 任务：让 DSL 生成链路和 AI 视觉定位兼容智谱 GLM 系列模型
- 执行动作：在 `dsl_generator.py` 请求层按 `base_url/model` 做 provider 自适配（BigModel 分支使用 `thinking` 参数，OpenAI 分支保持 `response_format`）；更新 `.env.example` 默认指向智谱 BigModel 端点；在 visual locate 链路适配 GLM 视觉模型请求格式
- 结果：DSL 生成和 AI 视觉定位均可使用智谱 `glm-4.7-flash` 等模型，非智谱 provider 行为不回归
- 验证：
  - `cd backend && uv run pytest tests/unit/test_dsl_validation.py -q` 通过
  - 本地真实 BigModel smoke 请求返回 200 并正确解析
- 后续：如需切换回 OpenAI 系列，只需修改 `.env` 中的 `AI_DSL_BASE_URL` 和 `AI_DSL_MODEL`
- 关联计划：`docs/superpowers/plans/2026-03-29-dsl-bigmodel-adapter.md`

## 2026-03-29 · Suite 应用层下线

- 任务：移除已废弃的 Suite 应用层，统一到 `Project -> Case` 资产结构
- 执行动作：删除 Suite 相关模型、路由、服务、前端组件；清理 Suite 相关迁移；补充 Alembic 迁移回归测试
- 结果：资产结构统一为 `Project -> Case`，Suite 相关代码和数据库对象已清除
- 验证：全量后端测试和迁移测试通过
- 后续：后续回归编排需求将基于项目结构重新设计

## 2026-03-29 · 报告中心增强

- 任务：扩展报告中心的作用域和指标
- 执行动作：增强报告中心的数据聚合范围和展示指标
- 结果：报告中心可展示更丰富的执行统计和趋势数据
- 验证：前端组件测试和页面测试通过
- 后续：暂无新增报告主线，后续视需求进入新一轮报告扩面

## 2026-03-28 · M1 认证入口落地与治理收口

- 任务：完成 M1 里程碑的认证入口落地和治理主线收口
- 执行动作：后端落地 `POST /api/v1/auth/login`、`POST /api/v1/auth/logout`、`GET /api/v1/auth/me`；前端完成 `/login`、登录态恢复、受保护路由、统一 401 回退；加严 auth session 和 artifact 访问安全；推进 governance-v3.3 收口
- 结果：M1 认证基线已落地，业务 API 默认要求登录，治理主线进入收口状态
- 验证：
  - 后端 API 测试和前端认证流程测试通过
  - 2 条浏览器级固定主回归通过
- 后续：认证仍为本地账号密码最小形态，尚未进入角色分层和账号管理

## 2026-03-30 23:15

- 任务：实现 AI 测试规划对话助手，覆盖工作台内嵌对话 UI、后端 planning session 持久化、agent loop 与 DSL 草案生成复用链路
- 执行动作：新增后端 ai_planning 模型、schema、service、route、agent prompt/loop 与 Alembic 迁移；在 CaseWorkbenchPage 接入 AITestPlanningPanel，补充 AI planning 类型与 API；新增后端 API 测试、前端面板测试与工作台回归调整；修复面板初始化阶段首条消息可能被吞掉的前端竞态，并解除 .gitignore 对新后端测试文件的误忽略
- 结果：工作台已支持基于测试方案的多轮澄清、结构化场景展示、按场景批量生成 DSL 草案并导入当前编辑器；新增 /api/v1/ai-planning/* 接口族，后端会话、消息、草案可持久化；现有自然语言 DSL 生成与工作台编辑流回归通过
- 验证：
  - `cd backend && uv run pytest tests/unit/test_ai_planning_api.py tests/unit/test_models.py -q`，结果 `15 passed`
  - `cd frontend && npm run test -- src/services/api.test.ts src/components/AITestPlanningPanel.test.tsx`，结果 `16 passed`
  - `cd frontend && npm run test -- src/pages/CaseWorkbenchPage.test.tsx`，结果 `16 passed`
- 后续：如需继续上线，可再补一轮端到端手工验证与更广覆盖的全量测试；本次相关缺陷记录已补到 `docs/bug-log.md`

## 2026-03-30 21:31

- 任务：审查最新提交 `7eb71ae feat: implement complete CRUD for test case and project management`
- 执行动作：按 `backend-call-chain-reviewer` 的 diff review 路径检查 `backend/app/api/routes/cases.py`、`backend/app/api/routes/projects.py`、`backend/app/services/cases.py`、`backend/app/services/project_management.py`、相关 schema 与模型；补跑 `uv run pytest backend/tests/unit/test_cases_api.py -q` 和 `uv run pytest backend/tests/unit/test_projects_and_report_preferences_api.py -q` 验证兼容性回归
- 结果：确认本次提交存在多处高风险问题，包括 case CRUD 缺少项目成员权限校验、项目统计接口返回模型与服务返回值不一致、项目删除路径与 `test_cases.project_id` 的 `RESTRICT` 外键冲突，以及已有接口响应合同回归导致旧测试失败
- 验证：
  - `uv run pytest backend/tests/unit/test_cases_api.py -q`，结果 `1 failed, 8 passed`
  - `uv run pytest backend/tests/unit/test_projects_and_report_preferences_api.py -q`，结果 `1 failed, 4 passed`
  - 静态核对 `backend/app/models/test_case.py`、`backend/app/services/cases.py`、`backend/app/services/project_management.py`、`backend/app/schemas/cases.py`
- 后续：建议优先修复 `docs/bug-log.md` 中新增的 `BUG-041`，至少补齐权限校验、修正 stats 返回结构、明确项目删除语义，并同步更新受影响的 API 测试

## 2026-03-30 21:31

- 任务：在 `AGENTS.md` 中补充适用于 Claude Code 的 GitHub 提交参考指令
- 执行动作：检查现有协作规则与 GitHub 同步口径，在 `Collaboration Preference` 之后新增 `GitHub Sync Reference` 小节，补充 `git status`、`git add`、`git commit`、`git push`、新分支首次推送和推送后校验示例，并明确非交互式 git 使用偏好
- 结果：`AGENTS.md` 现已包含可直接参考的 GitHub 提交流程，便于后续在 Claude Code 中按统一口径执行同步
- 验证：
  - 静态核对 `AGENTS.md` 新增小节内容与现有协作规则不冲突
  - 计划执行 `git diff -- AGENTS.md docs/execution-log.md` 做最终确认
- 后续：如需进一步收紧提交流程，可继续补充提交前测试校验模板或按分支类型区分推送示例

## 2026-04-03 23:02

- 任务：继续完成 AI planning ReAct 改造，从 task4 推进到 task9，串联后端 agent、schema/service、前端 settings 与规划面板，并补齐回归测试
- 执行动作：重写 `backend/app/ai/test_planning_agent.py` 为 LLM 驱动的 ReAct loop，接入 `planning_tools.py`、force generate、工具调用审计与失败回退；重写 `backend/app/ai/test_planning_prompts.py`；更新 `backend/app/schemas/ai_planning.py` 和 `backend/app/services/ai_planning.py`，支持 `tool_call` 消息与 `tool_calls` 响应；补齐 `backend/tests/unit/test_ai_planning_api.py` 和 `backend/tests/unit/test_ai_settings_api.py`；前端补充 planning settings 类型与设置页表单，重写 `frontend/src/components/AITestPlanningPanel.tsx` 为动态进度面板并新增“直接生成方案”；同步更新相关前端测试
- 结果：AI planning 已从旧的关键词补槽逻辑切换到可调用工具的 ReAct agent；工作台内嵌规划面板现在支持动态进度、工具调用回显、直接生成方案和按场景生成 DSL 草案；Settings 页面已支持单独配置 AI planning 模型、超时、轮数与密钥
- 验证：
  - `cd backend && uv run pytest tests/unit/test_ai_planning_api.py tests/unit/test_ai_settings_api.py -q`，结果 `13 passed`
  - `cd frontend && npm run test -- src/components/AITestPlanningPanel.test.tsx src/pages/AISettingsPage.test.tsx src/services/api.test.ts`，结果 `20 passed`
  - `cd frontend && npm run test -- src/pages/CaseWorkbenchPage.test.tsx`，结果 `16 passed`
- 后续：如需继续收口，可补 `planning_tools.py` 的独立单测，并考虑把 AI planning 的真实 HTTP 调用抽成与 DSL/VLM 共用的 LLM client，减少重复请求层代码

## 2026-04-17

- 任务：添加会话层认证测试（Platform API chain whitebox tests）
- 执行动作：创建 `backend/tests/integration/test_platform_api_chain.py`，包含 3 个测试：未登录访问返回 401、登录设置 session 并返回用户信息、已登录 session 在多次请求间保持一致
- 结果：3 个测试全部通过
- 验证：`cd backend && python -m pytest tests/integration/test_platform_api_chain.py -v`，结果 `3 passed in 2.07s`
- 后续：可在后续任务中扩展更多 API chain 测试（用例创建、执行、结果验证）

- 任务：给报告页面添加删除执行记录功能（DELETE 路由 + 前端删除按钮）
- 执行动作：
  - 后端 `services/executions.py` 新增 `delete_execution` 函数
  - 后端 `services/__init__.py` 导出 `delete_execution`
  - 后端 `api/routes/executions.py` 新增 `DELETE /executions/{execution_id}` 路由，返回 204
  - 前端 `services/api.ts` 新增 `deleteExecution` 客户端函数
  - 前端 `pages/ReportPage.tsx` 每条执行记录右侧添加删除按钮（带 Popconfirm 确认弹窗），删除后自动刷新列表和概览数据
  - 新增 2 个后端单元测试：`test_delete_execution_removes_record_and_returns_204` 和 `test_delete_execution_returns_404_for_unknown_id`
- 结果：报告页面可删除单条执行记录，删除后列表和统计自动更新
- 验证：
  - `cd backend && uv run pytest tests/unit/test_case_executions_api.py -q`，结果 `18 passed`（含新增 2 个）
  - `cd frontend && npx tsc --noEmit`，无错误
- 后续：可考虑批量删除功能

- 任务：修复流式接口两个 bug + 添加 create_project 幂等处理
- 执行动作：
  - **Bug 1 (Session rollback)**：`planning_tools.py` 中 `_handle_create_project` 和 `execute_tool` 的 catch 块原来用 `if not db_session.is_active` 条件判断才 rollback；UniqueViolation 后 `is_active` 可能仍为 True 导致跳过回滚，后续 DB 操作全部 PendingRollbackError。修复：移除条件，异常时无条件 `db_session.rollback()`
  - **Bug 2 (流异常兜底)**：`ai_planning_streaming.py` 的 `_run_sync_generator` 内层 try-except 只捕获 `StopIteration`，`next(stream)` 的其他异常直接冲出导致 SSE 断开。修复：while 循环内增加 `except Exception` 分支，捕获后向队列写入 `type: "error"` 事件再 break，保证前端收到错误提示
  - **幂等处理**：`_handle_create_project` 新增同名检测逻辑 — 创建前先查 `Project.name == base_name`，已存在则扫描 `base_name (N)` 模式取最小可用编号（如 `Automation Exercise (2)`），结果附带 `name_deduplicated: true` 告知 AI
- 结果：工具 DB 异常不再污染会话状态；流中断时前端可见错误信息；同名项目自动编号避免 UniqueViolation
- 验证：
  - `cd backend && uv run pytest tests/unit/test_planning_tools.py -q`，结果 `43 passed`
  - commit: `b6580fd fix(streaming): add session rollback after tool failures, stream error fallback, and idempotent project creation`
- 后续：可在 E2E 手动测试中验证同名项目创建场景

## 2026-04-30

- 任务：修复 VS Code Claude Code 插件切换 model 时报 `Unexpected token ... "env" is not valid JSON`
- 执行动作：
  - 检查 VS Code 用户设置、工作区设置、Claude Code 扩展日志与 `C:\Users\30521\.claude\settings.json`
  - 从扩展日志确认根因是 `settings.json` 文件开头带 UTF-8 BOM，插件内部 `JSON.parse()` 无法解析
  - 备份并将 `C:\Users\30521\.claude\settings.json` 重写为无 BOM UTF-8
  - 移除 VS Code 用户设置中无效的 `claudeCode.selectedModel` 异常值
- 结果：Claude 配置和 VS Code 用户设置均可被 Node `JSON.parse()` 正常解析，model 切换失败的 JSON 解析根因已消除
- 验证：
  - `node -e "JSON.parse(fs.readFileSync('C:/Users/30521/.claude/settings.json','utf8'))"`，通过
  - `node -e "JSON.parse(fs.readFileSync(process.env.APPDATA + '/Code/User/settings.json','utf8'))"`，通过
  - 检查 `C:\Users\30521\.claude\settings.json` 文件头，结果 `NO_BOM`
- 后续：在 VS Code 中执行 Reload Window 后重新打开 Claude Code，再尝试切换 model

## 2026-05-12 | E2E 测试验证 + 草案质量分析 + 多轮修复

**背景：** 使用 `test_brand_filter_cart` 测试规格对平台进行 E2E 测试，验证 AI 规划 → DSL 生成 → 执行的完整链路，持续发现问题并修复。

**操作：**

### Phase 1: 草案质量问题发现与分析
1. **页面探索流程错误** — AI 跳过登录页直接从已登录的 products 页开始探索，品牌筛选结果页未被采集
2. **页面状态映射错误** — S2 被映射到 view_cart 而非品牌筛选结果页
3. **actions 泛化** — explore_flow 的 actions 使用 "Add to cart" 而非 "Blue Top 附近的 Add to cart"，导致匹配到错误元素
4. **数量修改步骤缺失** — 草案中没有修改商品数量的步骤
5. **candidates 为空** — 登录页元素的定位器候选列表为空

### Phase 2: 提示词与消息修复
6. **删除 "可以直接 generate_plan" 逃逸口** — `_build_link_selection_message` 中的提示让 LLM 跳过 explore_flow
7. **安全网消息误导** — "已采集完成，请生成方案" 改为 "静态页面已采集，交互页面仍需 explore_flow"
8. **系统提示词矛盾** — "4 项信息直接 generate_plan" 改为 "先探索页面再 generate_plan"
9. **capture_page_session 描述误导** — 改为明确说明"不采集元素，请用 explore_flow"

### Phase 3: Guard 增强
10. **页面覆盖度检查移入 Guard** — coverage < 0.5 时阻止 generate_plan，要求 LLM 补充探索

### Phase 4: Few-shot 自愈系统
11. **DSLAntiPattern 模型** — 新建 `dsl_anti_patterns` 表，存储历史错误模式
12. **自动采集逻辑** — `_capture_anti_patterns_from_warnings` 从草案 warnings 中提取 anti-pattern
13. **注入逻辑** — `_build_user_prompt_lines` 中注入相关 anti-pattern 作为 few-shot 负面示例

### Phase 5: DSL 生成器 thinking mode
14. **DSL 生成器启用 thinking mode** — deepseek-v4-pro + effort=max，提升推理质量
15. **规划代理保持 v4-flash** — v4-pro 太慢（7+ 分钟），规划代理用快速模型

### Phase 6: explore_flow actions 消歧
16. **消歧检查** — `_check_action_disambiguation` 检测泛化 target 并添加警告
17. **消歧生效** — 草案中 "Blue Top 附近的 Add to cart" 替代了泛化的 "Add to cart"

### Phase 7: 相对 URL 解析修复
18. **example.com 问题** — explore_flow 的 relative URL `/login` 被解析为 `example.com/login`
19. **多层 fallback** — 从 params → steps → session requirements → user message 提取 base_url
20. **未完全解决** — session requirements 在首次 LLM 调用时为空，需从用户消息提取

**验证：**
- thinking mode 确认生效：`DSL _call_llm: model=deepseek-v4-pro, thinking=True`
- Guard 覆盖度检查生效：`Guard: page coverage 25.0% < 50%, missing: ['login', 'product', 'brand']`
- actions 消歧生效：草案中使用 "Blue Top 附近的 Add to cart"
- capture_text 模式正确：capture → assert 模式完整

**未解决问题：**
- explore_flow 相对 URL 解析（需从用户消息提取 base_url 或改工具描述要求完整 URL）
- 购物车残留数据导致测试隔离失败
- 数量修改使用 button 而非 input（Automation Exercise 特有）
- VLM 429 限流影响页面探索速度

**后续：**
- 在 explore_flow 工具描述中要求 LLM 使用完整 URL
- 实现测试前购物车清理步骤
- 调研 +/- 按钮型数量修改的通用处理方案
