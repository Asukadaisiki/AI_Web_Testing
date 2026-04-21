# 执行日志

用于沉淀每次任务实际做了什么，方便后续追溯、复盘和回答一致化。

## 记录规则

- 每次处理需求后按时间倒序追加一条记录。
- 记录”目标、操作、结果、验证、后续”，避免只写结论。
- 如果执行过程中发现缺陷，同时在 `docs/bug-log.md` 追加对应条目并互相引用。
- 最新的记录优先放到最上面，方便阅读。

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
