# Bug 日志

用于沉淀在开发、联调、测试和执行过程中发现的问题，跟踪影响、状态和修复结论。

## 记录规则

- 发现一个明确问题时，在「问题记录」顶部按时间倒序新增一条记录。
- 记录结构统一为：日期、状态、严重度、来源、描述、复现步骤、影响、根因、处理、验证、关联记录。
- 状态建议使用：`open`、`in_progress`、`fixed`、`wont_fix`。
- 如果问题来自某次任务执行，请回链到 `docs/execution-log.md` 中的对应记录。
- 最新的记录优先放到最上面，方便阅读。

## 记录模板

```md
## BUG-XXX | 标题

- 日期：YYYY-MM-DD
- 状态：fixed
- 严重度：low / medium / high / critical
- 来源：需求 / 自测 / 联调 / 线上反馈
- 描述：问题现象。
- 复现步骤：
  1. 步骤一
  2. 步骤二
- 影响：功能、页面、模块或用户范围。
- 根因：如果尚未定位，写“待定位”。
- 处理：修复动作或计划。
- 验证：已执行的验证；如果没有写“未验证”。
- 关联记录：执行日志日期或链接。
```

## 分类索引

| 类别 | 概述 | 典型编号 |
|------|------|----------|
| A. DSL 生成与归一化 | LLM 输出→结构化 DSL 链路的格式、字段、校验问题 | Bug #A-G, BUG-077/078/083/070/085/056/048/045 |
| B. 定位器系统 | 语义/CSS/VLM/坐标定位器匹配、策略、回退问题 | BUG-084/082/080/076/075/074/073/072/071/064/057/053/050/049/046, Bug #1 |
| C. 页面探索与数据采集 | explore_page/flow、DOM/A11y 采集、缓存问题 | BUG-060/059/067/068, Bug #2, Bug #A |
| D. AI 决策与提示词 | ReAct 循环、提示词遵循、工具调用去重、角色推荐 | BUG-085/081/069/066/065/054, Bug #C, Bug #K-M |
| E. SSE 流式与前端 | 流式输出、会话管理、前端渲染 | BUG-063/064/058/044/042, Bug #D |
| F. 执行引擎 | Playwright runner、变量替换、证据采集 | BUG-079/057/053/051/047, Bug #3 |
| G. 配置与基础设施 | API 合同、权限、网络重试、DB 配置 | AUDIT-*, BUG-045/043/041, Bug #B |

> 注：同一 BUG 编号在不同日期出现不同内容时，以日期区分（如 BUG-065 (2026-05-06) vs BUG-065 (2026-05-12)）。

---

## 问题记录

## BUG-113 | Python Browser Worker 仍承载非浏览器控制面

- 日期：2026-09-05
- 状态：fixed
- 严重度：high
- 来源：Go AgentService 全面迁移完成性审计
- 描述：公开 API 虽已迁移到 Go，但 `generate_dsl`、`execute_dsl`、`get_report` 和 `fix_and_retry` 仍经 Python `/internal/agent-capabilities` 完成持久化与业务编排。
- 影响：Python 仍是隐含的第二控制面，Go 无法独立保证 DSL 所有权、审批后的执行创建和报告读取合同。
- 根因：前一阶段只迁移了外部 HTTP 路由，没有继续迁移 Agent 工具背后的内部 capability。
- 处理：新增 Go DSL Store 和结构化校验；新增 Go ControlPlane 工具适配器；将 actor 身份传入 Tool Call；删除 Python agent capability 路由、服务、schema、测试及其下游死代码；Browser Worker Client 只保留浏览器 capability。
- 验证：真实 PostgreSQL 集成测试覆盖 Go GenerateDSL → ExecuteDSL → Report → FixAndRetry；全仓检索无 Python `agent_capabilities` 引用；Go/Python/前端回归通过。
- 关联记录：`docs/execution-log.md#2026-09-05--完成内部控制面迁移与-browser-worker-重命名`

## BUG-112 | Go 控制面 SQL 与实际 PostgreSQL schema 不一致

- 日期：2026-09-05
- 状态：fixed
- 严重度：high
- 来源：Go 控制面真实 PostgreSQL 集成测试
- 描述：Go 创建 Execution Job 和 Locator Correction 时遗漏无 server default 的非空字段；Project 删除也会被 Execution 外键 `RESTRICT` 阻断。
- 复现步骤：
  1. 在 Alembic `20260905_0038` schema 上调用 Go `CreateBatch`。
  2. 创建执行记录后调用 Go `Correction.Create`。
  3. 删除含执行历史的 Project。
- 影响：Batch 创建、人工定位修正和项目删除三个核心操作在编译及 mock 测试通过后仍会在真实 PostgreSQL 上失败。
- 根因：迁移期 Go SQL 依据 ORM 默认值编写，但部分默认值仅由 SQLAlchemy 客户端注入，数据库列本身没有默认值；项目删除也未处理 `RESTRICT` 历史表。
- 处理：Job 插入显式写入 attempt/max-attempt/cancel 默认值；Correction 插入显式写入时间戳；Project 删除在同一事务中按外键顺序清理 correction、run 和 batch；同时补齐 Batch 幂等与 Planning Session 所有权校验。
- 验证：真实 PostgreSQL 16.15 纵向集成测试通过，Go 全量 test/vet/build 通过。
- 关联记录：`docs/execution-log.md#2026-09-05--补齐-go-报告聚合并验证-postgresql-控制面`

## BUG-111 | Python Planning Agent 与 Go AgentService 重复决策

- 日期：2026-09-05
- 状态：fixed
- 严重度：high
- 来源：AgentService 全面迁移
- 描述：Go AgentCore 已成为 Planning 主入口，但 Python 仍保留完整 ReAct Agent、Planning SSE/API，并被 Report Core 作为失败分析器调用，形成两套 Agent 决策源。
- 影响：工具协议、Prompt、状态机和失败处理可能产生分叉，Python 无法收缩为稳定 Browser Worker。
- 根因：Go 迁移仅替换前端主入口，没有同步切断 Report Core 和旧 API 对 Python Agent 的依赖。
- 处理：报告改为确定性分析；浏览器 capability 从旧 Planning 工具注册表中提取；删除 Python Planning API、ReAct、Prompt、SSE、草案编排和持久化模型；Go 拆分为 `agent`、`harness`、`agentservice`，并增加工具调用前 policy gate。
- 验证：全仓已无 Python Planning Agent 引用；Go/Python/前端测试及构建通过；Alembic `0037` 已应用且 schema check 无差异。
- 关联记录：`docs/execution-log.md#2026-09-05--启动-go-agentservice-全面迁移`

## BUG-110 | Go 工具实现与前端旧 Planning 链路残留

- 日期：2026-09-05
- 状态：fixed
- 严重度：medium
- 来源：架构与无引用代码复核
- 描述：`ask_user_question` 工具实现位于 `agentcore` 包，破坏 Core 与 Tool Registry 的职责边界；前端切换到 Go AgentWorkbench 后仍保留旧 Planning 面板、SSE store、兼容 barrel 和无消费者 API client；Python 还注册了两个从未读写的预留 ORM 模型。
- 复现步骤：
  1. 检查 `backend-go/internal/agentcore/ask_user_tool.go` 的包归属。
  2. 使用 Knip 扫描前端不可达文件和未使用导出。
  3. 全仓检索 `AIPlanningFlowStep` 与 `LocatorAttemptLog` 的生产读写调用。
- 影响：目录职责模糊，重复状态机和客户端增加维护成本；空 ORM 表制造已经具备数据闭环的错误认知。
- 根因：Go AgentCore 切换完成后，旧 Python Planning 前端和早期数据闭环预留项未同步清理。
- 处理：将 `AskUserTool` 移入 `internal/tools`；删除旧前端 Planning/SSE 链路、无消费者客户端和未接入的 OpenAPI 生成产物；删除两个未使用 ORM 模型并通过 Alembic `0036` 下线对应表；清理无引用兼容函数和常量。
- 验证：Go test/vet/build、Python 22 项 unittest、前端 8 项 Vitest 和生产构建通过；Knip 无未使用项；Vulture 高置信度扫描无结果；Alembic 升级及 schema check 通过。
- 关联记录：`docs/execution-log.md#2026-09-05--清理非主链代码并修正-go-工具边界`

## BUG-109 | 请求日志记录登录明文密码

- 日期：2026-09-05
- 状态：fixed
- 严重度：critical
- 来源：恢复鉴权后的真实浏览器验收
- 描述：`RequestLoggingMiddleware` 会原样序列化 JSON 请求体，登录请求中的 `password` 被写入服务日志。
- 复现步骤：
  1. 调用 `POST /api/v1/auth/login`。
  2. 查看 Python API 日志。
  3. 可见邮箱和明文密码。
- 影响：任何可读取应用日志的人员或系统都可能获得用户凭据。
- 根因：请求/响应日志仅做长度截断，没有按字段递归脱敏。
- 处理：对 password、secret、token、API key、Cookie 和 Session 等敏感字段递归替换为 `***`，请求和响应 JSON 共用同一处理。
- 验证：新增两项脱敏测试；完整 Python 测试通过，明文测试密码不再出现在格式化日志内容中。
- 关联记录：`docs/execution-log.md#2026-09-05--生产化控制面鉴权与工作台完善`

## BUG-108 | 根级忽略规则吞掉新增前端源码和测试

- 日期：2026-09-05
- 状态：fixed
- 严重度：high
- 来源：前端回归编排与定位调试实施
- 描述：根 `.gitignore` 存在 `frontend` 整目录规则，历史已跟踪文件可继续修改，但新页面、组件和测试不会出现在正常 Git 暂存范围。
- 复现步骤：
  1. 在 `frontend/src` 新增文件。
  2. 执行 `git check-ignore -v <file>`。
  3. 文件命中根 `.gitignore` 的 `frontend` 规则。
- 影响：本地实现和测试可能通过，但新增文件不会被同步到远端，提交后构建缺失模块。
- 根因：历史忽略规则把整个前端目录当作生成物。
- 处理：移除整目录忽略，保留 `node_modules/dist` 等通用规则，并显式忽略 Playwright 浏览器缓存、报告和测试结果。
- 验证：所有新增页面、组件、Vitest 和 Playwright 文件均出现在 `git status`；前端测试和构建通过。
- 关联记录：`docs/execution-log.md#2026-09-05--生产化控制面鉴权与工作台完善`

## BUG-107 | 恢复登录后部分业务接口仍可匿名访问

- 日期：2026-09-05
- 状态：fixed
- 严重度：high
- 来源：鉴权边界复核
- 描述：仅部分路由显式依赖 `require_authenticated_user`，旧的 execution、correction、DSL 查询/删除接口和 artifact 下载没有统一认证门禁。
- 复现步骤：
  1. 清除登录 Cookie。
  2. 调用未声明用户依赖的业务接口或 artifact 路由。
  3. 请求不会在路由入口统一返回 401。
- 影响：匿名调用者可能读取执行证据、定位修正和 DSL generation，或触发删除操作。
- 根因：认证依赖随功能逐个添加，API 组装层没有默认保护策略。
- 处理：在 API router 中将除 health/auth 外的业务路由统一挂到认证分组；artifact 路由单独增加认证依赖；Go API 同时通过 Python `/auth/me` 内省 Cookie。
- 验证：新增路由门禁测试，覆盖全部 Python 业务路由和 artifact；Go 测试覆盖匿名拒绝与跨用户 Run 拒绝。
- 关联记录：`docs/execution-log.md#2026-09-05--生产化控制面鉴权与工作台完善`

## BUG-106 | 项目状态文档仍保留自愈半闭环旧口径

- 日期：2026-09-05
- 状态：fixed
- 严重度：low
- 来源：当前项目进度盘点
- 描述：根 README 下半部分仍写“自动自愈尚无统一状态机和前端审批入口”“真实全链待验收”，BUG-090 也保持 `open`，与 2026-09-04 已完成的 Go AgentCore 受控自愈和真实浏览器 E2E 结论冲突。
- 复现步骤：
  1. 阅读 README 的“当前状态”和“与计划相比的主要差距”。
  2. 对照 2026-09-04 AgentCore、自愈及前端 E2E 执行记录。
  3. 检查 BUG-090 状态。
- 影响：项目进度会被误判为尚未完成受控自愈和端到端验收。
- 根因：新闭环完成后只更新了 README 顶部状态和执行日志，旧阶段说明及原缺陷状态未同步收口。
- 处理：更新 README 的进行中事项和实际差距；将 BUG-090 标记为已修复并补充实现与验证结果。
- 验证：交叉核对当前分支提交、README、AgentCore 文档和 2026-09-04 执行记录；`git diff --check` 通过。
- 关联记录：`docs/execution-log.md#2026-09-05--当前项目进度复核`

## BUG-105 | Python 导入解析配置缺失且存在无效导入

- 日期：2026-09-05
- 状态：fixed
- 严重度：low
- 来源：自测
- 描述：从仓库根目录打开工程时，IDE 无法解析 `backend` 下的第三方依赖和 `app.*` 包；静态扫描同时发现 9 个未使用导入和 2 个模块的非顶层导入。
- 复现步骤：
  1. 从仓库根目录打开任一 `backend/app` Python 文件。
  2. 查看 `fastapi`、`sqlalchemy`、`pydantic` 或 `app.*` 导入诊断。
  3. 使用 Ruff 扫描 `F401/F403/F405/E402`。
- 影响：IDE 产生误报警告，真实无效导入混在噪声中，降低静态检查可信度。
- 根因：仓库未声明 Pyright 的虚拟环境和 Python 源码根路径；部分文件还残留未使用导入或在 logger 初始化后继续导入。
- 处理：新增导入解析专用 `pyrightconfig.json`，指向 `backend/.venv` 和 `backend`；移除无效导入并调整导入位置。
- 验证：Pyright 0 errors/0 warnings；Ruff 目标规则全部通过；122 个应用模块均可导入；13 个单测通过。
- 关联记录：`docs/execution-log.md#2026-09-05--修复-python-导入诊断`

## BUG-104 | DSL 生成器可选择不可定位的文档结构节点

- 日期：2026-09-04
- 状态：fixed
- 严重度：high
- 来源：AgentCore 前端真实浏览器 E2E
- 描述：页面探索同时向 DSL 生成器暴露 `RootWebArea`、`StaticText` 和已验证的 `heading`。模型首次生成时选择 `RootWebArea "Example Domain"` 作为 `wait_for/assert_text` 目标，运行时没有 locator candidate，进入 `needs_intervention`。
- 复现步骤：
  1. 探索 `https://example.com` 并要求断言首页标题。
  2. 生成器选择与 h1 同名的 `RootWebArea`。
  3. 执行 `wait_for RootWebArea "Example Domain"`，所有定位层失败。
- 影响：元素验证已经找到唯一可用 heading，但首轮执行仍可能因不可定位节点失败，产生无意义的修复和再次审批。
- 根因：A11y 黑名单未排除仅表示文档根和文本叶子的角色，生成提示中仍将其展示为可用 target。
- 处理：在页面探索边界过滤 `RootWebArea` 和 `StaticText`，只将可稳定定位的语义节点送入验证与 DSL 生成。
- 验证：新增 A11y 过滤回归测试；真实重跑生成 `heading "Example Domain"`，首次执行 3/3 步通过且未调用 `fix_and_retry`。
- 关联记录：`docs/execution-log.md#2026-09-04--前端切换-go-agentcore-并完成浏览器验收`

## BUG-103 | AI 报告摘要可与确定性失败结论矛盾

- 日期：2026-09-04
- 状态：fixed
- 严重度：high
- 来源：AgentCore 真实失败修复联调
- 描述：失败 Batch 的 `conclusion`、`case_results`、`FailureSignal` 和 `recommended_action` 已由确定性分析覆盖，但 `summary` 仍直接采用模型回复，出现结构化结论为 `all_failed`、摘要却写“全部通过”的矛盾。
- 复现步骤：
  1. 执行一个定位成功但断言值错误的用例。
  2. 让模型分析失败结果并返回错误的通过摘要。
  3. 查看 Batch 报告，可见 `conclusion=all_failed`，但 `summary` 表示全部通过。
- 影响：Agent 修复策略仍正确，但用户界面和下游文本消费者会收到与事实冲突的结论。
- 根因：BUG-100 修复仅锁定结构化结果字段，遗漏了同样承载执行结论的摘要字段。
- 处理：`summary` 改为确定性分析输出；AI 仅增强失败明细、根因、影响范围和建议范围。
- 验证：新增冲突摘要回归断言；Python 执行分析合同测试通过。
- 关联记录：`docs/execution-log.md#2026-09-04--agentcore-透明修复与重执行闭环`

## BUG-102 | 新分析字段导致滚动期间旧 Worker 无法写入执行记录

- 日期：2026-09-04
- 状态：fixed
- 严重度：high
- 来源：AgentCore 真实失败用例联调
- 描述：数据库新增非空 `analysis_status` 后，仍在运行的旧 Worker ORM 不会在 INSERT 中携带该字段，创建 `test_case_runs` 时触发 NOT NULL 约束错误。
- 复现步骤：
  1. 在数据库升级分析字段迁移后保留旧版本 Worker 进程。
  2. 创建并执行一个新 Batch。
  3. 旧 Worker 插入 Run 时因缺少 `analysis_status` 失败。
- 影响：滚动升级窗口内新执行无法落库，Batch 会失败且缺少正常步骤证据。
- 根因：新列仅配置 Python 侧 default，没有数据库 server default，旧进程无法感知新字段。
- 处理：为 `test_case_runs.analysis_status` 和 `execution_batches.analysis_status` 增加数据库默认值 `pending`，并新增 Alembic 0034。
- 验证：旧 Worker 成功创建失败 Run 并持久化 assertion FailureSignal；Alembic 0034 已应用且 `alembic check` 无差异。
- 关联记录：`docs/execution-log.md#2026-09-04--agentcore-透明修复与重执行闭环`

## BUG-101 | Agent 工具失败前缺少审计事件且 DSL 工具允许无效上下文

- 日期：2026-09-04
- 状态：fixed
- 严重度：high
- 来源：Go AgentCore 真实探索到 DSL 生成联调
- 描述：首轮 `explore_page -> validate_page_elements -> generate_dsl` 中，模型因工具 Schema 过宽而传入缺少 `steps` 的 `current_case`，Python DSL 请求返回 422；同时 Engine 只在工具成功后记录 started/args/result，失败调用仅留下 `run.failed`，无法还原实际参数和失败工具。
- 复现步骤：
  1. 让 Agent 为 `https://example.com` 探索并生成首页标题断言 DSL。
  2. 前两个工具成功后，模型调用 `generate_dsl` 并传入不完整 `current_case`。
  3. Python 返回 `current_case.steps Field required`；事件流缺少该工具的 started/args。
- 影响：合法生成链路被可选但无效的上下文字段阻断，且失败审计无法定位模型实际调用参数。
- 根因：`generate_dsl` 工具暴露了没有完整 JSON Schema 的 `current_case`；Engine 将工具生命周期事件延迟到执行成功后写入。
- 处理：从首阶段工具 Schema 移除 `current_case/preserve_contracts`；所有工具统一在执行前持久化 `tool.started` 和 `tool.args.delta`，失败时增加 `tool.failed`，成功时记录 result/finished。
- 验证：同一需求重跑后依次完成 explore、validate、generate，生成 3 步 DSL 并以 23 条事件正常收口。
- 关联记录：`docs/execution-log.md#2026-09-04--go-agentcore-探索验证与-dsl-生成链路`

## BUG-100 | LLM 失败归因可产生与失败事实矛盾的结构化结论

- 日期：2026-09-04
- 状态：fixed
- 严重度：high
- 来源：DeepSeek LLM 能力实测
- 描述：向完整 Planning ReAct Agent 输入明确的 locator 失败信号后，模型自然语言正确识别 `Login` 文案变为 `Sign in`，但最终 `ExecutionAnalysis` 为 `conclusion=all_passed`、`recommended_action=done`，且 `failure_details` 为空。
- 复现步骤：
  1. 使用当前 `deepseek-v4-flash` 配置调用 `run_planning_turn()`。
  2. 输入 `category=locator`、`no candidates matched` 及按钮文案变化证据，并要求使用 `analyze_results`。
  3. 检查返回的 `assistant_message` 与 `execution_analysis`。
- 影响：自然语言总结看似正确，但自动编排若消费结构化字段会把失败误判为全部通过并停止修复；报告标签也可能与 FailureSignal 冲突。
- 根因：`analyze_results` 解析允许缺失或不完整的 `analysis` 对象通过 `ExecutionAnalysis` 默认值补全，且没有根据已知 FailureSignal 对 `conclusion`、`recommended_action` 和失败明细做一致性校验。
- 处理：持久化前由确定性分析覆盖 conclusion、case results 和 FailureSignal；LLM 仅增强摘要、根因、影响范围和修复建议。存在失败信号且 LLM 返回 `done` 时保留确定性的 `targeted_retest`。
- 验证：新增回归测试注入 `all_passed/done` 的错误 AI 结果，最终持久化仍为 `all_failed/targeted_retest`，并保留 locator FailureSignal 和失败明细。
- 关联记录：`docs/execution-log.md#2026-09-04--deepseek-llm-能力实测`

## BUG-099 | Planning 复测绕过统一执行分析持久化

- 日期：2026-09-04
- 状态：fixed
- 严重度：high
- 来源：执行归因与复测循环现状核验
- 描述：`POST /ai-planning/sessions/{session_id}/retest` 通过 `execute_case()` 重跑原用例，之后直接调用 `run_analysis_turn()` 并把结果写入 Planning 消息，但没有调用统一的 `analyze_run()`，也没有把分析写入复测产生的 `TestCaseRun.analysis_json`。
- 复现步骤：
  1. 对包含失败用例的 Planning session 调用 `/retest`。
  2. 复测完成后读取返回的 Planning 消息，可见本轮分析。
  3. 查询本次新建的 Run 报告，分析仍可能保持 `pending` 且缺少同一份持久化 `ExecutionAnalysis`。
- 影响：复测会产生执行结果和会话内分析，但 Report Core、执行详情与 Planning 消息不再共享同一分析事实；anti-pattern 统一沉淀也可能被绕过。
- 根因：复测服务保留了统一分析服务落地前的“执行后直接调用 Planning Agent”旧路径。
- 处理：新增 `analyze_runs()` 聚合入口；Planning 复测完成后对本轮全部 execution ID 生成一次统一分析，将同一事实持久化到每个 Run，并从该持久化结果构造 Planning 消息和 anti-pattern。
- 验证：新增双 Run 回归测试，确认聚合分析为 `all_failed`、两个 Run 均为 `analysis_status=completed` 且 `analysis_json` 一致。
- 关联记录：`docs/execution-log.md#2026-09-04--执行归因与复测循环现状核验`

## BUG-098 | 缺陷日志重复编号导致开放状态统计失真

- 日期：2026-09-04
- 状态：fixed
- 严重度：low
- 来源：当前项目状态盘点
- 描述：`docs/bug-log.md` 中存在同一编号的多条历史记录且状态互相冲突，例如 BUG-M 同时存在 `fixed` 与 `open` 记录，BUG-086 也有重复记录。按编号或状态自动汇总时会把已修复问题再次计为开放问题。
- 复现步骤：
  1. 按 `## BUG-*` 标题解析缺陷日志。
  2. 汇总状态为 `open` 或 `in_progress` 的记录。
  3. 可见同一编号同时出现在已修复与未关闭结果中。
- 影响：项目状态报告和开放缺陷数量不可靠，需要人工结合记录日期与上下文去重。
- 根因：历史日志合并时保留了重复编号记录，后续状态更新没有归并到单一权威条目。
- 处理：将纯交叉引用降为三级索引标题，为两个不同缺陷使用的 065/066 冲突编号增加唯一后缀；新增 `scripts/check_bug_log.py` 并接入 CI。
- 验证：校验脚本确认 101 条历史缺陷记录 ID 唯一。
- 关联记录：`docs/execution-log.md#2026-09-04--当前项目状态盘点`

## BUG-097 | 队列执行后的 AI 分析未进入统一报告事实链

- 日期：2026-09-02
- 状态：fixed
- 严重度：high
- 来源：用例执行到报告总结链路核验
- 描述：Planning 队列执行完成后会构造 `execution_summary`，失败时也会调用 `run_analysis_turn()` 并发出 `analysis_complete`；但分析结果没有写入 TestCaseRun、ExecutionBatch Report Core 或 AIPlanningMessage，前端 `ExecutionStreamEvent` 和 reducer 也未声明/处理 `analysis_complete`。此外，新队列路径通过 `save_and_execute_selected_drafts(execute=False)` 保存用例，绕过了旧执行分支中的 `_record_execution_anti_patterns()`。
- 复现步骤：
  1. 从 Planning 会话保存并执行一个会失败的草案。
  2. Worker 完成 Job，确认 TestCaseRun 和 Batch 报告已生成。
  3. 后端执行 `run_analysis_turn()` 并发出 `analysis_complete`。
  4. 刷新 Planning 页面或查询 Run/Batch 报告，无法从统一报告事实中读取本次 AI 分析；前端实时 reducer 也忽略该事件，失败 anti-pattern 未由新队列路径沉淀。
- 影响：执行记录和结构化报告可用，但“失败总结 -> 报告展示 -> 历史回放 -> 后续生成复用”的链路不闭合；直接 Case/Batch API 执行也不会触发统一 AI 总结。
- 根因：Report Core 当前仅投影 Batch/Job/Run 状态和步骤证据；Planning AI 分析仍是旁路事件，迁移到队列时未同步设计持久化合同、前端事件合同和 anti-pattern 写入。
- 处理：新增持久化 `FailureSignal` 和 `ExecutionAnalysis`，由直接 Run 或 Batch 终结统一触发；Report Core、Planning 消息和执行详情读取同一分析；补齐 `analysis_complete` 前端合同、历史消息恢复和执行 anti-pattern 记录；正式报告详情统一到 `/reports/:executionId`，旧 `/run/:executionId` 保留重定向。
- 验证：2 个执行分析合同测试通过；Alembic `0030/0031` 降级升级往返及 `alembic check` 通过；前端生产构建通过；浏览器验证 Planning 历史分析、正式报告详情、报告聚合入口和旧路由重定向通过。
- 关联记录：`docs/execution-log.md#2026-09-02--用例执行到报告总结链路核验`

## BUG-096 | 根 README 的项目阶段与完成度口径再次漂移

- 日期：2026-09-02
- 状态：fixed
- 严重度：medium
- 来源：当前项目进展盘点
- 描述：根 README 仍将当前阶段描述为 2026-05-31 的“M2 功能增强”，并沿用基于当时结果的 `98%+` 完成度；未体现 8 月架构治理、9 月执行队列、Report Core、Planning 队列迁移、自动化测试套件移除及当前开放缺陷。
- 复现步骤：
  1. 阅读 README 的“当前状态”和“已完成的核心能力”。
  2. 对照 2026-08-28 优化计划、2026-09-02 执行日志及最近两个提交。
  3. 可见 README 的阶段、验证基线和待办均不是当前状态。
- 影响：维护者会误判项目成熟度、当前主线和质量门禁，`98%+` 也缺少可复核的现行计算口径。
- 根因：架构与执行链路继续演进后，README 未随任务日志同步更新。
- 处理：按当前能力矩阵重写“当前状态”，取消无稳定口径的总百分比；补充 ExecutionBatch/ExecutionJob、Report Core、Planning 队列迁移、受控自愈缺口、测试门禁现状和 Worker 启动方式；同步修正后端 README 中旧 SSE 路径描述。
- 验证：核对 README、后端 README、Git 提交、执行日志和开放缺陷；Markdown 差异检查通过。
- 关联记录：`docs/execution-log.md#2026-09-02--当前项目进展盘点`

## BUG-095 | PostgreSQL 队列候选批次 DISTINCT 排序不合法

- 日期：2026-09-02
- 状态：fixed
- 严重度：high
- 来源：ExecutionBatch 真实 PostgreSQL 冒烟测试
- 描述：首版 Job 领取查询使用 `SELECT DISTINCT batch.id` 并按未出现在选择列表中的 `batch.created_at` 排序，PostgreSQL 抛出 `InvalidColumnReference`。
- 复现步骤：
  1. 创建包含待执行 Job 的 Batch。
  2. 调用 `claim_next_execution_job()`。
  3. PostgreSQL 在候选批次查询阶段拒绝 SQL。
- 影响：Worker 无法领取任何任务。
- 根因：通过 JOIN 去重候选批次时错误组合了 DISTINCT 与额外排序字段。
- 处理：改为相关 `EXISTS` 子查询筛选含可领取 Job 的 Batch，不再需要 DISTINCT。
- 验证：真实 PostgreSQL 完成 `Batch -> Job -> Playwright Run -> Batch Report`，状态 `passed`、2/2 步骤通过。
- 关联记录：`docs/execution-log.md#2026-09-02--executionbatchexecutionjob-与-report-core-第一阶段落地`

## BUG-094 | 多会话并行执行缺少任务隔离与并发治理

- 日期：2026-09-02
- 状态：fixed
- 严重度：high
- 来源：多项目并行执行架构分析
- 描述：每个 Planning SSE 执行请求直接创建守护线程和 Chromium，无全局/项目级并发上限或持久化任务队列；取消句柄只按 `session_id` 保存，同一会话重复启动会覆盖旧句柄；VLM 限流、断路器和统计为进程级共享状态，但每个用例启动时都会全局重置；同一会话的多个 EventLogWriter 还会各自从 `seq=1` 写入。
- 复现步骤：
  1. 同时从多个 Planning 会话发起保存并执行，或对同一会话连续发起两次执行。
  2. 观察每个请求创建独立守护线程和浏览器进程。
  3. 检查取消管理器、VLM runtime state 和 SSE event seq。
- 影响：低并发下报告通常可独立保存，但高并发可能耗尽浏览器、数据库连接和外部模型额度；跨项目 VLM 状态会互相清空，同一会话取消和事件回放不可靠，进程退出后任务不可恢复。
- 根因：SSE 传输线程同时承担任务执行职责，缺少独立 execution job/batch、持久化状态机、幂等控制和资源调度层。
- 处理：新增持久化 ExecutionBatch/ExecutionJob、数据库幂等键、PostgreSQL 行锁领取、Batch 并发限制、独立 Worker 和 batch/job/run 报告关联；Planning SSE 已改为创建 Batch 并订阅 Report Core；Worker 每 2 秒 heartbeat/续租并读取持久化取消标记；同一 Planning session 禁止重复活动 Batch；停止每次 Run 前重置全局 VLM 状态。
- 验证：PostgreSQL 迁移与真实 Playwright 队列闭环通过；同 Batch 两个 Job 可分别领取并正确聚合终态；同一幂等键重复创建返回相同 Batch；Planning SSE 队列事件闭环通过；跨 Session 取消测试确认 Job/Batch 均收口为 `cancelled`。
- 备注：取消在约 2 秒内传播，并在 Runner 下一安全步骤边界生效；不强杀正在执行中的单个同步 Playwright 调用。
- 关联记录：`docs/execution-log.md#2026-09-02--多会话与多项目并行执行分析`

## BUG-093 | 未捕获执行异常会遗留永久 running 记录

- 日期：2026-09-02
- 状态：fixed
- 严重度：high
- 来源：报告与执行持久化静态分析
- 描述：`execute_case_streaming()` 创建并提交 `running` 记录后，只处理人工干预、RunnerExecutionError 和主动取消；浏览器启动、证据序列化或其他未归一化异常可直接逸出，无法写入终态、结束时间和报告。
- 复现步骤：
  1. 调用单用例执行接口，令 Playwright 浏览器启动或 Runner 外围逻辑抛出未包装异常。
  2. 查询对应 `test_case_runs` 记录。
  3. 记录仍为 `running`，`finished_at` 和 `report` 为空。
- 影响：报告中心出现永久运行记录，统计失真，错误总结模块也拿不到失败证据。
- 根因：执行记录先独立提交，但终态持久化没有统一 `finalize`/兜底异常路径。
- 处理：执行入口增加兜底异常收口；未知异常发生后回滚当前事务，重新读取已创建 Run，写入失败报告、错误信息与 `finished_at` 后再抛出。新 Job lease 支持过期任务重新领取。
- 验证：注入未知 RuntimeError 后，TestCaseRun 正确落为 `failed`，报告、结束时间和 DSL hash 均已持久化。
- 关联记录：`docs/execution-log.md#2026-09-02--报告执行持久化与调度链路分析`

## BUG-092 | 报告、Planning 与 anti-pattern 使用三套错误分类

- 日期：2026-09-02
- 状态：fixed
- 严重度：high
- 来源：AgenticRL 前置错误分类分析
- 描述：报告中心使用 `configuration/locator/assertion/navigation/network/runner`，Planning 上下文使用 `locator_stale/assertion_mismatch/timeout/network_error/unknown`，anti-pattern 使用 `missing_navigation/missing_wait_for/...`。报告分类还在读取时从首个失败步骤临时推导，没有随执行结果固化。
- 复现步骤：
  1. 检查 `schemas/executions.py` 的 `FailureCategory`。
  2. 检查 `application/planning/context_service.py::categorize_error()`。
  3. 检查 `services/anti_patterns.py` 的错误类别常量。
- 影响：同一失败在报告、上下文注入和学习样本中得到不同标签；历史记录会随分类代码变化而改变，不适合作为 AgenticRL 训练和评估数据。
- 根因：报告展示、会话分析和 DSL 负例分别独立演进，没有统一失败事实模型。
- 处理：新增统一 `FailureSignal`，在 Run 终结时按 `configuration/locator/assertion/navigation/network/runner` 固化；Planning 分类改用同一分类器；anti-pattern 保留 DSL 修复模式 `error_category`，同时新增统一 `failure_category` 并迁移回填历史记录。
- 验证：执行分析合同测试覆盖 locator 分类持久化和 anti-pattern 映射；Alembic `0030/0031` 升降级及 schema 差异检查通过。
- 关联记录：`docs/execution-log.md#2026-09-02--报告执行持久化与调度链路分析`

## BUG-091 | 浏览器集成测试与当前认证及定位实现漂移

- 日期：2026-09-01
- 状态：open
- 严重度：medium
- 来源：主链路浏览器回归
- 描述：浏览器集成中存在三类旧断言：匿名访问仍期望 401，但本地模式已自动登录；A11y role 测试引用已变为 `None` 的兼容常量；VLM 重排测试 monkeypatch 已删除的内部函数。登录样例还使用裸 `flash` target，实际执行到最后一步后进入人工干预。
- 影响：浏览器回归无法作为当前主链健康门禁，容易把测试资产漂移误判为生产链整体故障。
- 根因：认证策略、A11y 过滤与 locator 内部实现演进后，集成 fixture 和 monkeypatch 未同步更新。
- 处理：待按当前公开合同重写相关断言和 fixture，避免依赖内部函数。
- 验证：`test_main_path_v2_e2e.py` 为 2 passed、1 failed；登录执行为 5 步通过、第 6 步定位失败；`test_intervention_regression.py` 为 3 passed、1 failed。
- 关联记录：`docs/execution-log.md#2026-09-01--ai-规划到失败复测链路核验`

## BUG-090 | 失败后自动重探索和 DSL 重写尚未编排

- 日期：2026-09-01
- 状态：fixed
- 严重度：high
- 来源：主链路静态核验
- 描述：后端分别具备失败分析、anti-pattern 记录、执行错误上下文注入和 `/retest`，但复测只重跑原 DSL；没有自动重新探索、重生成 DSL、更新正式用例的编排，前端也没有复测 API 客户端或操作入口。
- 影响：用户描述的“分析错因 → 注入当前会话 → 重新组织上下文 → 再次执行”不能自动闭环，需要用户再次对话、生成草案并手动执行。
- 根因：现有实现采用半自动治理，能力模块已存在但缺少受控自愈状态机和 UI 确认点。
- 处理：已实现透明 `fix_and_retry` 工具和 repair plan artifact；Agent 按失败事实执行重探索、元素验证、DSL 重生成，并在前端审批后通过 Batch/Job 队列重执行。
- 验证：真实失败用例完成 `fix_and_retry -> explore_page -> validate_page_elements -> generate_dsl -> approve -> execute_dsl -> get_report`，重执行 3/3 步通过；Go、Python 聚焦测试、前端构建及桌面/移动浏览器 E2E 通过。
- 关联记录：`docs/execution-log.md#2026-09-01--ai-规划到失败复测链路核验`

## BUG-089 | 流式保存执行遗漏测试数据注入和失败沉淀

- 日期：2026-09-01
- 状态：fixed
- 严重度：high
- 来源：主链路静态核验
- 描述：前端“保存并执行”使用 `/execute` 流式路径；该路径未执行同步路径已有的 `build_input_values_from_session` 和 `_record_execution_anti_patterns`。
- 影响：含 `${context_key}` 的用例可能因输入为空执行失败，且失败不会沉淀为下一轮 DSL 生成可用的 anti-pattern。
- 根因：同步保存执行逻辑增强后，流式实现未同步更新。
- 处理：在流式路径执行前自动解析会话测试数据，执行后按保存用例记录失败 anti-pattern；同时将旧式 `Query.get()` 改为 SQLAlchemy 2.x `Session.get()`。
- 验证：新增流式路径回归，确认 `input_values={"username": "contract-user"}` 传入 Runner 且失败记录函数被调用；相关测试 27 passed、1 skipped。
- 关联记录：`docs/execution-log.md#2026-09-01--ai-规划到失败复测链路核验`

## BUG-088 | 架构入口文档与当前实现严重漂移

- 日期：2026-08-31
- 状态：fixed
- 严重度：medium
- 来源：架构评估
- 描述：根 `README.md` 仍以 2026-05 的功能修复流水为主体，状态、测试数量和“无需登录”等描述已过期；`backend/app/*/README.md` 仍是初始化占位文案；根 README 文档索引引用 4 个不存在的文件。
- 复现步骤：
  1. 阅读根 `README.md` 的“当前状态”“演示流”和“文档索引”。
  2. 对照当前认证路由、499 项后端测试、77 项前端测试及 `backend/app/application/planning/` 结构。
  3. 检查索引中的 `docs/AI 自动化测试增强项目规划.md`、`docs/project-plan.md` 和两份 AI visual 文档，均不存在。
- 影响：新维护者无法从入口文档建立可信的模块边界和主调用链，需要依赖提交日志和源码反向推断架构，显著增加理解与变更成本。
- 根因：功能和架构连续演进后，README 与模块级说明未纳入同步更新门禁；执行日志承担了事实记录，但没有可替代的当前态架构导航。
- 处理：新增 `docs/architecture-guide.md`，覆盖前后端目录职责、Planning 文件词典、主调用链、依赖规则、过渡目录、需求定位表和阅读顺序；根 README 增加当前架构入口并清除 4 个失效文档链接；更新 `backend/app/README.md` 的占位说明。
- 验证：已逐项核对文件路径、当前模块结构和测试基线；README 与新增架构指南的本地 Markdown 链接检查通过。
- 关联记录：`docs/execution-log.md#2026-08-31--当前代码架构可理解性评估`

---

## BUG-087 | 日志记录顺序和结构不一致

- 日期：2026-08-31
- 状态：fixed
- 严重度：low
- 来源：用户反馈
- 描述：`docs/execution-log.md` 中部分 2026-08-28 至 2026-08-30 记录被追加到历史记录尾部，破坏按时间倒序展示；`docs/bug-log.md` 同时存在顶部记录、模板规则、分类历史混排，且记录标题层级不统一。
- 复现步骤：
  1. 打开 `docs/execution-log.md`，可见 2026-08-28 至 2026-08-30 记录出现在 2026-03-28 之后。
  2. 打开 `docs/bug-log.md`，可见最新记录、模板、规则、分类索引和历史记录混排，记录标题同时使用 `##` 与 `###`。
- 影响：阅读日志时无法可靠按时间追溯任务和缺陷，新增记录也容易继续写入错误位置。
- 根因：历史追加规则执行不一致，且两个日志文件缺少一致的顶部骨架和独立记录区。
- 处理：统一两个日志文件结构为说明、记录规则、记录模板、索引/总览、记录区；将记录区整理为日期倒序；统一 bug 记录标题层级为 `##`。
- 验证：已扫描两个日志的记录标题，确认最新日期在前，旧的 2026-08-28 至 2026-08-30 记录已归位。
- 关联记录：`docs/execution-log.md#2026-08-31--统一日志结构与时间倒序展示`

---

## AUDIT-20260831-01 | 本地 PostgreSQL 种子项目序列未对齐导致首次新建会话 500

- 日期：2026-08-31
- 状态：fixed
- 严重度：medium
- 位置：`backend/alembic/versions/20260309_0001_stage1_domain_models.py:175-204`、`backend/app/application/planning/session_service.py:84-92`
- 来源：用户反馈 / 本地复现
- 描述：初始 migration 显式插入 `projects.id=1`，但 PostgreSQL 序列未同步到 `max(id)`。首次新建规划会话且未传 `project_id` 时，服务会创建默认项目；若 `projects_id_seq` 仍从 1 开始，`Project` 插入触发主键冲突，接口返回 500。该事务回滚会话记录，但 PostgreSQL sequence 不回滚，因此第二次请求使用 `id=2` 后成功，界面显示会话 2。
- 链路补充：规划首页读取的是 `GET /api/v1/ai-planning/sessions`，只展示已关联到会话的项目；初始化的 `Default Project(id=1)` 虽可通过 `GET /api/v1/projects` 读取，但没有与任何规划会话关联，因此不会出现在规划首页。新建会话接口在未传 `project_id` 时也不会复用该初始化项目，而是自动创建 `default-{session_id}`。
- 影响：本地空库/重建库首次使用新建规划会话时体验异常，并产生编号跳号，容易误判为隐藏会话或前端状态错乱。
- 处理：新增迁移 `20260831_0027`，将 `Default Project(id=1)` 标记为默认项目，创建并绑定默认规划会话，且在 PostgreSQL 下校准 `users/projects/project_members/ai_planning_sessions/session_projects` 序列；同时调整新建会话逻辑，未传 `project_id` 时优先复用当前用户已有项目，只有无可访问项目时才创建兜底默认项目并补 `ProjectMember`。
- 验证：已重置本地 PostgreSQL 会话数据并复刻。修复前第一次 `POST /api/v1/ai-planning/sessions` 返回 `500`，响应堆栈包含 `psycopg.errors.UniqueViolation`；修复后同接口返回 `201`，新会话绑定 `Default Project(id=1)`。后端完整测试 `uv run pytest -q` 通过，结果为 495 passed、1 skipped、10 deselected。

---

## BUG-086 | AI 规划探索失败：Playwright 浏览器未安装且 Sync API 实例泄漏

- 日期：2026-08-30
- 状态：fixed
- 来源：AI 规划工具调用 `explore_flow` / `explore_page` 失败（session 2）
- 描述：AI 规划进入 `tool_call` 阶段时，`explore_flow` 报 `BrowserType.launch: Executable doesn't exist at C:\Users\30521\AppData\Local\ms-playwright\...`；随后 `explore_page` 报 `It looks like you are using Playwright Sync API inside the asyncio loop.`。两种错误叠加导致探索失败，最终规划报 `exploration_failed`。
- 复现步骤：
  1. 本机未在默认路径安装 Playwright 浏览器，且未配置 `PLAYWRIGHT_BROWSERS_PATH`。
  2. AI 调用 `explore_flow` → `BrowserSessionManager.get_or_create_context` 启动 Chromium 失败。
  3. 启动失败后 `pw.__exit__` 未被调用，Playwright 事件循环残留。
  4. 后续 `explore_page` 再次进入 `sync_playwright().__enter__` 时检测到运行中的 asyncio loop，抛 Sync API 错误。
- 影响：AI 规划无法采集页面元素，无法生成有效 DSL；同时暴露项目/成员自增序列不同步的隐患。
- 根因：① Playwright 浏览器未安装到默认路径且无环境变量指向自定义路径；② `BrowserSessionManager.get_or_create_context` 与 `_collect_flow_a11y` 在 `pw.__enter__` / `chromium.launch` 失败时未清理 `pw`，导致 loop 泄漏；③ 手工重建种子数据时显式写入 `id=1` 但未同步自增序列，`project_members_id_seq` 仍从 1 开始，触发 `pk_project_members` 主键冲突。
- 处理：
  1. 将 Chromium 及依赖下载到 `D:\PlaywrightBrowsers`。
  2. 在 `backend/.env` 与 `backend/.env.example` 增加 `PLAYWRIGHT_BROWSERS_PATH=D:\PlaywrightBrowsers`。
  3. 修复 `BrowserSessionManager.get_or_create_context` 和 `_collect_flow_a11y` 中启动失败时释放 Playwright 实例。
  4. 用 `setval` 同步 `users_id_seq` / `projects_id_seq` / `project_members_id_seq` 到当前最大 id。
- 验证：
  - `sync_playwright` 加载 https://example.com 成功。
  - `_collect_flow_a11y` 探索 https://example.com 成功（1 页、8 个元素）。
  - `create_project` 成功创建项目并写入成员记录，无主键冲突。
  - 后端默认测试 493 passed、1 skipped、10 deselected。
- 关联记录：`docs/execution-log.md#2026-08-30`

---

## AUDIT-20260830-16 | 登录后恢复路径可接受协议相对地址

- 日期：2026-08-30
- 状态：fixed
- 严重度：medium
- 位置：`frontend/src/features/auth/LoginPage.tsx`
- 描述：认证 guard 将当前路径写入 location state；未经校验直接传给 `navigate()` 时，`//host` 或反斜杠路径可能触发客户端开放重定向。
- 建议：只接受单斜杠开头且不含反斜杠的站内路径，其余回退到固定工作台。
- 处理：新增 `getSafeDestination()` 并在登录前统一归一化恢复目标。
- 验证：测试覆盖协议相对路径、反斜杠路径和合法站内路径。

---

## AUDIT-20260830-17 | P5 拆分后 `services/api.ts` 引用不存在的 `features/reports/api`

- 日期：2026-08-30
- 状态：fixed
- 严重度：high
- 位置：`frontend/src/services/api.ts:7`、`frontend/src/services/api.test.ts`
- 描述：P5 提交 `7cd1fd9` 将业务 API 拆到 `features/*` 后，`services/api.ts` 保留兼容 barrel 并 `export * from "../features/reports/api"`，但 `features/reports/` 从未创建；同时 `api.test.ts` 仍从 barrel 导入 `getReportPreference/updateReportPreference`（旧 `services/api.ts` 中的函数），拆分时遗漏迁移。
- 复现：`cd frontend && npm test -- --run`（`api.test.ts` 加载失败）；`cd frontend && npm run build`（`TS2307` / `TS2305`）。
- 影响：前端测试与构建均失败，无法通过 P1 门禁；report preference API 在前端无域归属。
- 建议：创建 `features/reports/api.ts` 并迁移 `getReportPreference/updateReportPreference`，或从 barrel 移除 reports 导出并把测试改为从正确模块导入；同时补 reports domain 测试。

---

## AUDIT-20260830-18 | 本地 `backend/.env` 含明文 API 凭据并破坏默认测试门禁

- 日期：2026-08-30
- 状态：open
- 严重度：medium
- 位置：`backend/.env`
- 描述：本地 `.env` 仍含 `VLM_API_KEY`、`AI_DSL_API_KEY`、`AI_PLANNING_API_KEY` 明文凭据，且 `ENABLE_AI_VISUAL_LOCATE=true`。`test_ai_visual_locate_default_is_disabled` 通过 `_load_env_file` 读取 `.env` 时被污染，默认测试 1 失败。
- 复现：`cd backend && uv run pytest -q` 观察 `test_config.py::test_ai_visual_locate_default_is_disabled` 失败；`cat backend/.env` 可见明文密钥。
- 影响：本地测试门禁不稳定；明文凭据留在工作树（虽被 gitignore，但存在本地泄露风险）。
- 处理：测试隔离已修复——`test_config.py` 两个 VLM 默认值用例重定向 `ENV_FILE_PATH`，默认测试恢复 493 passed；凭据轮换与 `.env` 去密钥仍待人工处理。
- 验证：`uv run pytest -q` 通过，`test_ai_visual_locate_default_is_disabled` 不再受 `.env` 污染。

---

## AUDIT-20260830-19 | 本地中转 LLM 网关 base URL 缺少 `/v1`

- 日期：2026-08-30
- 状态：fixed
- 严重度：medium
- 位置：`backend/.env`
- 描述：`AI_DSL_BASE_URL`/`AI_PLANNING_BASE_URL` 配置为 `https://api.unself.cn`，流式调用会请求 `https://api.unself.cn/chat/completions`，该网关在此路径返回 HTML 首页，导致解析为空响应、会话报 `empty_response`。
- 处理：将两个 base URL 修正为 `https://api.unself.cn/v1`。
- 验证：经 Vite 代理发送消息，AI 正常返回 `assistant_message` 与 `session_status=collecting`。

---

## AUDIT-20260829-13 | Planning 流式执行调用不存在的事件日志方法

- 日期：2026-08-29
- 状态：fixed
- 严重度：high
- 位置：`backend/app/application/planning/save_execute_service.py`
- 描述：save-and-execute 流式路径调用 `EventLogWriter.log()`，但 writer 仅提供 `write()`；运行到首个事件时会抛出 `AttributeError` 并中断流。
- 建议：统一使用公开 `write()` 合同，并通过注入的 event-log port 增加流式错误路径测试。
- 处理：将事件日志改为 `PlanningEventLogFactory` 注入，所有事件统一调用 `write()`。
- 验证：新增无有效草案的流式事件日志合同测试，确认 error 事件写入并 flush。

---

## AUDIT-20260829-14 | 取消执行状态未持久化

- 日期：2026-08-29
- 状态：fixed
- 严重度：high
- 位置：`backend/app/services/executions.py`
- 描述：流式执行捕获 `RunnerCancelledError` 后仅修改内存对象并立即重新抛出，未提交结束状态、时间和已有 evidence，刷新后记录仍可能显示 `running`。
- 建议：在重新抛出取消异常前持久化 `cancelled` 终态和已有步骤报告。
- 处理：扩展执行状态合同，取消时保存 report、error、finished_at 并提交事务。
- 验证：取消执行测试断言数据库状态为 `cancelled`、结束时间存在且报告可读取。

---

## AUDIT-20260829-15 | 前端 `/login` 路由绕过认证流程

- 日期：2026-08-29
- 状态：fixed
- 严重度：high
- 位置：`frontend/src/app/AppRouter.tsx`
- 描述：`/login` 直接跳转到 Planning，且业务路由没有统一认证 guard，失效登录态只能依赖后端逐请求返回 401。
- 建议：提供真实登录页面，并在路由层统一校验 `/auth/me`。
- 处理：新增 `AuthGuard` 与 `LoginPage`；业务路由统一受保护，登录成功后恢复原目标地址。
- 验证：路由测试覆盖已登录访问、未登录跳转、登录页渲染及登录成功返回工作台。

---

## AUDIT-20260828-12 | SQLite 空库无法执行完整 Alembic 升级

- 日期：2026-08-28
- 状态：open
- 严重度：medium
- 位置：`backend/alembic/versions/20260313_0004_suite_context_contracts.py:23`
- 描述：SQLite 执行 `alembic upgrade head` 时，在 0004 直接新增外键约束处抛出 `NotImplementedError`，无法到达后续 migration；生产 PostgreSQL 不受该方言限制，但本地 SQLite 升级路径不可用。
- 建议：将该 migration 的约束变更改为 Alembic batch mode，并增加 SQLite 空库全链升级测试；PostgreSQL CI 继续作为生产迁移门禁。

---

## AUDIT-20260828-01 | tracked 本地配置包含明文 API 凭据

- 日期：2026-08-28
- 状态：in_progress
- 严重度：critical
- 位置：`.claude/settings.local.json:28`
- 描述：本地设置文件已被 Git 跟踪，其中命令白名单包含明文 API 凭据；后续加入 `.gitignore` 不能移除历史泄露。
- 建议：立即轮换凭据、停止跟踪该文件，并按仓库传播范围决定是否清理历史。
- 处理：已停止跟踪该文件、删除本地明文权限项，并从 `main` 全部历史提交中清除该路径后强制更新远端；外部智谱密钥仍待账号持有人登录控制台删除并新建。
- 验证：本地全部 refs 与远端全量克隆中该路径的历史提交数均为 0；BigModel Bearer 模式扫描为 0。

---

## AUDIT-20260828-02 | Alembic 迁移链引用缺失 revision

- 日期：2026-08-28
- 状态：fixed
- 严重度：critical
- 位置：`backend/alembic/versions/20260608_0025_sse_event_log.py:3-4`
- 描述：`down_revision = "45061d8892d7"`，但仓库不存在该 revision，且 `.gitignore` 明确忽略预期文件。
- 建议：找回原迁移或创建等价迁移并正确衔接，增加空库 `alembic upgrade head` CI。
- 处理：恢复 `45061d8892d7_add_is_default_to_projects.py`，父 revision 指向 `1c65d6ff37db`，并增加独立升降级测试。
- 验证：`alembic heads` 返回唯一 head `20260608_0025`；迁移回归测试通过。SQLite 全链升级受 AUDIT-20260828-12 阻断。

---

## AUDIT-20260828-03 | 生产 router 暴露无鉴权浏览器调试接口

- 日期：2026-08-28
- 状态：fixed
- 严重度：high
- 位置：`backend/app/api/routes/ai_planning.py:497-561`
- 描述：`POST /ai-planning/test/locator` 可访问调用者提供的 URL 并启动 Playwright，没有鉴权和 URL 限制。
- 建议：移出生产 router；如需保留，增加鉴权、allowlist、并发和超时限制。
- 处理：从生产 AI Planning router 删除该调试接口及其浏览器调用链。
- 验证：新增路由注册回归测试，确认 `/api/v1/ai-planning/test/locator` 不存在。

---

## AUDIT-20260828-04 | VLM candidate ranker 使用未定义变量

- 日期：2026-08-28
- 状态：fixed
- 严重度：high
- 位置：`backend/app/locators/ai_visual.py:513-553`
- 描述：`_call_candidate_ranker` 没有 `model_family` 参数，却在调用 `_call_chat_completion` 时读取该名称。
- 建议：补齐参数传递并增加候选排序分支测试。
- 处理：从 `rank_candidates_by_vision` 向 `_call_candidate_ranker` 显式传递最终解析的 model family。
- 验证：AI visual 单测覆盖 GLM candidate ranking 分支并通过。

---

## AUDIT-20260828-05 | 新建用例链接落入编辑路由

- 日期：2026-08-28
- 状态：fixed
- 严重度：high
- 位置：`frontend/src/pages/CasesPage.tsx:286-300,490-497`
- 描述：UI 跳转 `/cases/new`，路由只有 `/cases/:caseId/edit`，最终会尝试请求数值为 `NaN` 的 case。
- 建议：增加明确的新建路由/模式，并覆盖路由测试。
- 处理：增加 `/cases/new` 路由和 create mode，通过 query 参数传递当前项目；非法编辑 ID 在请求前拒绝。
- 验证：新增路由、新建提交和非法 ID 测试，前端全量测试通过。

---

## AUDIT-20260828-06 | AI selector cache 只有读取和失效，没有写入

- 日期：2026-08-28
- 状态：fixed
- 严重度：high
- 位置：`backend/app/locators/fallback.py:247-303`
- 描述：唯一写函数 `_store_cached_ai_selector` 零调用，缓存无法产生新条目。
- 建议：在成功定位后写入，或删除整套无效缓存及指标。
- 处理：确认 DOM selector 提取已在历史重构中删除后，移除永远 miss 的缓存读取、存储、失效逻辑及未消费指标。
- 验证：locator fallback、AI visual、settings API 单测通过，前端类型检查和构建通过。

---

## AUDIT-20260828-07 | 孤儿数据清理脚本可能误删合法项目

- 日期：2026-08-28
- 状态：fixed
- 严重度：high
- 位置：`backend/scripts/cleanup_orphan_data.py:31-39,130-136`
- 描述：脚本把“未关联 planning session”的项目都判为孤儿，删除项目可能级联删除仍有效的用例和成员关系。
- 建议：增加 member、case、默认项目保护条件及二次确认，并补数据库边界测试。
- 处理：默认改为 dry-run；执行删除必须同时提供 `--execute --confirm DELETE_ORPHANED_DATA`；候选排除默认项目、有成员、有用例和有关联 session 的项目。
- 验证：新增保护边界及确认词测试并通过。

---

## AUDIT-20260828-08 | 默认测试发现遗漏 integration

- 日期：2026-08-28
- 状态：fixed
- 严重度：medium
- 位置：`backend/pyproject.toml:33-38`
- 描述：默认 `pytest` 的 `testpaths` 只有 `tests/unit`、`tests/e2e`，四个 integration 文件不会默认运行。
- 建议：默认 CI 纳入非浏览器 integration，浏览器测试用 marker 单独控制。
- 处理：默认发现范围包含 unit、integration、e2e，并默认排除 `browser_integration`、`e2e_api`；补齐漏标的真实浏览器测试。
- 验证：默认收集 522 个测试，其中 10 个外部依赖测试明确 deselected；默认执行 519 passed、1 skipped。

---

## AUDIT-20260828-09 | 前端测试与当前实现漂移

- 日期：2026-08-28
- 状态：fixed
- 严重度：medium
- 描述：2026-08-28 执行 Vitest，结果为 53 passed、7 failed；失败集中于 Cases 和 AI planning panel，并出现 render-time navigate、NaN height 警告。
- 建议：先判断测试合同还是实现行为为准，再修复数据 mock、交互断言和渲染副作用。
- 处理：更新路由、项目查询和 SSE 时序 mock；PlanningPage 改用声明式重定向；规划输入框改用固定 rows，消除 jsdom NaN height。
- 验证：Vitest 63 passed，前述 render-time navigate 与 NaN height warning 均不再出现。

---

## AUDIT-20260828-10 | DSL service 公开符号表与实现不一致

- 日期：2026-08-28
- 状态：fixed
- 严重度：medium
- 位置：`backend/app/services/dsl.py:1099-1115`
- 描述：`__all__` 导出不存在的 `get_dsl_generation_runtime_stats`，同时遗漏已实现的 `delete_dsl_generation_run`。
- 建议：修正公开符号表，并增加 import surface 静态检查。
- 处理：删除不存在的导出并加入 `delete_dsl_generation_run`。
- 验证：新增公开符号一致性测试并通过。

---

## AUDIT-20260828-11 | `.gitignore` 会静默遗漏新文档、测试和关键迁移

- 日期：2026-08-28
- 状态：fixed
- 严重度：medium
- 位置：`.gitignore:50-79`
- 描述：规则默认忽略 `docs/*`、根 `tests/` 及指定 migration；本次新审计文档也被直接忽略。
- 建议：改为只忽略生成物，对源码、测试、迁移和审计文档采用默认跟踪策略。
- 处理：移除文档、测试和指定 migration 的忽略规则，仅保留生成物、本地配置与外部测试项目规则。
- 验证：`git check-ignore` 确认新增文档、测试和 migration 默认可跟踪，本地设置与报告生成物仍被忽略。

---

## BUG-K | paragraph/StaticText role 在 semantic locator 正则和映射中缺失

- 日期：2026-06-05
- 状态：fixed
- 来源：E2E 测试执行失败（Run 195-198）
- 描述：AI 生成的 DSL 中大量使用 `paragraph "X"` 格式 target，但 semantic locator 的 `_A11Y_ROLE_TARGET_RE` 正则不包含 `paragraph`，`_A11Y_TO_PLAYWRIGHT_ROLE` 映射也没有 `paragraph`。同时 prompt 推荐 AI 使用的 `StaticText` 角色也不在正则和映射中。
- 复现步骤：
  1. AI 通过 explore_flow 采集页面，产品名称在 a11y tree 中角色为 `paragraph`
  2. AI 生成 DSL target=`paragraph "Blue Top"`
  3. semantic locator 解析时正则不匹配，退化到纯文本搜索 `get_by_text('paragraph "Blue Top"')`，找不到元素
  4. 所有 tier 失败 → needs_intervention
- 影响：所有使用 `paragraph` 角色作为 target 的 DSL 步骤全部失败；prompt 推荐 `StaticText` 但 parser 不支持，AI 被误导
- 根因：① 正则和映射只包含交互式 ARIA 角色，遗漏了 `paragraph`；② `paragraph` 是有效 ARIA role，`get_by_role("paragraph")` 能找到元素，但 `get_by_role("paragraph", name=...)` 因 `<p>` 元素无 accessible name 而失败
- 处理：
  1. `_A11Y_ROLE_TARGET_RE` 加入 `paragraph|statictext`
  2. `_A11Y_TO_PLAYWRIGHT_ROLE` 加入 `"paragraph": "paragraph"`
  3. 新增 `_TEXT_ONLY_ROLES` 集合，text-only 角色优先用 `get_by_text()` 而非 `get_by_role(name=)`
  4. inside scoping 容器查找改为爬 3 级父元素
- 验证：手动 18/18 步全部通过；`paragraph "Blue Top"` 正确解析为 `get_by_text("Blue Top", exact=True)`
- 关联记录：`docs/execution-log.md#2026-06-05`

---

## BUG-L | explore_flow 的 click 动作用 a11y text 匹配不可靠，导致探索数据错误

- 日期：2026-06-05
- 状态：fixed
- 来源：AI Session 302 探索数据追踪
- 描述：AI 通过 `explore_flow` 执行 `click "Polo"` 时，`_resolve_step_locator` 使用 a11y text 匹配（`get_by_text`），可能匹配到错误元素（如 breadcrumb、文本节点），且点击失败时静默继续，不报错。导致探索采集到的数据来自错误页面（全部产品页而非 Polo 筛选页）。
- 复现步骤：
  1. AI Session 302 调用 explore_flow，step 为 `{"action": "click", "target": "Polo"}`
  2. a11y locator 找到元素并点击 → 但没有验证是否成功导航
  3. 探索采集的全部产品（34 个），而非 Polo 专属产品（6 个）
  4. AI 基于错误数据生成 DSL，使用了全产品页中不存在的价格和产品名
- 影响：所有依赖 explore_flow click 导航的探索都可能采集到错误页面的数据，导致后续 DSL 生成失败
- 根因：探索阶段 click 只用 a11y text 匹配（`get_by_text`），没有利用已采集节点的精确选择器（`verified_selectors` 如 `a[href="/brand_products/Polo"]`），也没有验证导航是否成功
- 处理：新增 `_resolve_from_collected_nodes` 函数，优先用上一页采集的 `verified_selectors` 或 DOM 属性（`data-product-id`、`href`）构造 CSS 选择器精确定位，失败才回退 a11y locator
- 验证：修复后探索精确采集 6 个 Polo 专属产品（vs 修复前 35 个全部产品）
- 关联记录：`docs/execution-log.md#2026-06-05`

---

## BUG-M | explore_flow 页面 URL 在动作执行前捕获，导航后节点归因错误

- 日期：2026-06-05
- 状态：fixed
- 来源：修复 BUG-L 后的测试验证
- 描述：`_collect_flow_a11y` 在动作执行前捕获 `current_url`，如果动作导致页面导航（如 click 链接），采集的节点属于新页面但被归因到旧页面的 URL 和 state。导致 `_deduplicate_explore_results` 按 URL 合并时删除正确数据。
- 复现步骤：
  1. 在 products 页面执行 `click "Polo"` → 导航到 `brand_products/Polo`
  2. `current_url` 在 click 前捕获为 products URL
  3. click 后采集的 173 个 Polo 页面节点被归因到 products URL state
  4. dedup 按 URL 合并，Polo 页面数据被 products 页面覆盖
- 影响：通过 click 导航后采集的节点 URL 与实际页面不匹配，数据聚合时丢失
- 根因：`current_url = page.url` 在动作循环之前执行，导航后的 URL 变化未被反映
- 处理：在 `results.append(page_entry)` 前检查 `page.url != current_url`，若导航发生则更新 URL、分配新 state、回写节点 state
- 验证：修复后 Polo 页面获得独立 S2 state，节点归因正确
- 关联记录：`docs/execution-log.md#2026-06-05`

---

## Bug #I | AI 生成 paragraph 格式导致 VLM fallback

- 日期：2026-06-04
- 状态：open
- 来源：E2E 测试
- 描述：AI 生成了 `paragraph "Premium Polo T-Shirts"` 格式，但 `paragraph` 不是有效的 Playwright role，导致语义定位失败，回退到 VLM。
- 复现步骤：
  1. 用户输入测试需求
  2. AI 生成 DSL 草案
  3. 执行 DSL 时，`paragraph` 格式导致语义定位失败
  4. 回退到 VLM，但 VLM 也失败（429 限流、返回 None、TypeError）
- 影响：所有使用 `paragraph` 格式的步骤都会失败
- 根因：`_format_elements_flat` 函数显示容器时使用了 `paragraph="Blue Top"` 格式，AI 混淆了容器格式和子元素格式
- 处理：
  1. 修改 `_format_elements_flat` 函数，使用 `[container]` 前缀区分容器和子元素
  2. 更新 DSL 生成器的 prompt，添加更明确的指导
  3. 添加 WRONG examples 告诉 AI 不要使用 `paragraph` 格式
- 验证：测试 `test_format_elements_flat_uses_container_prefix` 通过，但 E2E 测试仍失败
- 关联记录：Session 302, Execution 197/198

**可能产生的原因**：

1. **AI 模型理解能力不足**
   - AI 没有理解 `[container]` 前缀的含义
   - AI 混淆了容器格式和子元素格式
   - AI 没有遵循 prompt 中的指导

2. **Prompt 设计问题**
   - Prompt 中的示例不够清晰
   - AI 没有看到正确的示例
   - Prompt 中的规则太多，AI 无法全部遵循

3. **DSL 生成器与 Playwright 不适配**
   - DSL 生成器生成的是 a11y role 格式（如 `paragraph "Blue Top"`）
   - 但 Playwright 的语义定位器不支持 `paragraph` 格式
   - 这是一个架构问题：DSL 生成器和 Playwright 之间存在格式不匹配

4. **与其他项目的区别**
   - testbrand(mimov2.5pro) 使用 Selenium + CSS 选择器/XPath
   - AI_Web_Testing 使用 Playwright + a11y role 格式
   - Selenium 支持多种定位方式，容错性强
   - Playwright 依赖 a11y role 格式，容错性弱

**为什么与 DSL 不适配**：

1. **格式不匹配**
   - DSL 生成器生成的是 a11y role 格式（如 `paragraph "Blue Top"`）
   - 但 Playwright 的语义定位器不支持 `paragraph` 格式
   - 这导致语义定位失败，回退到 VLM

2. **容错性不足**
   - Playwright 的语义定位器只支持 a11y role 格式
   - 没有其他定位方式作为备选
   - 当 a11y role 格式失败时，只能回退到 VLM

3. **VLM 模型不稳定**
   - VLM 模型遇到问题（429 限流、返回 None、TypeError）
   - 当 VLM 失败时，整个步骤就会失败

---

## Bug #J | AI 获取错误信息后没有生成新的 draft

- 日期：2026-06-04
- 状态：open
- 来源：E2E 测试
- 描述：AI 调用 `get_execution_detail` 获取错误信息后，没有生成新的 draft，而是重新执行了同一个 draft。
- 复现步骤：
  1. 用户输入测试需求
  2. AI 生成 DSL 草案（draft_id=218）
  3. 执行 DSL 失败
  4. AI 调用 `get_execution_detail` 获取错误信息
  5. AI 重新执行同一个 draft_id=218，而不是生成新的 draft
- 影响：错误信息注入没有生效，AI 无法从错误中学习
- 根因：AI 没有理解 `user_context` 中的错误信息，或者没有生成新的 draft 的逻辑
- 处理：需要修改 AI 的 ReAct 循环，让它在获取错误信息后生成新的 draft
- 验证：从日志验证，AI 确实获取了错误信息，但没有生成新的 draft
- 关联记录：Session 302, Execution 197/198

**可能产生的原因**：

1. **AI 没有理解错误信息**
   - AI 获取了错误信息，但没有理解如何使用它
   - AI 没有将错误信息与 DSL 步骤关联起来
   - AI 没有生成新的 draft 的逻辑

2. **ReAct 循环设计问题**
   - ReAct 循环中没有"生成新的 draft"的工具
   - AI 只能调用现有的工具（如 `get_execution_detail`）
   - 没有工具让 AI 生成新的 draft

3. **错误信息注入位置问题**
   - 错误信息被注入到 `user_context` 中
   - 但 `user_context` 只在 DSL 生成器的 prompt 中使用
   - AI 的 ReAct 循环没有使用 `user_context`

4. **AI 模型能力不足**
   - AI 没有理解"重新生成"的含义
   - AI 没有从错误中学习的能力
   - AI 只是重新执行了同一个 draft

**为什么与 DSL 不适配**：

1. **错误信息没有传递给 DSL 生成器**
   - 错误信息被注入到 `user_context` 中
   - 但 AI 的 ReAct 循环没有使用 `user_context`
   - DSL 生成器没有看到错误信息

2. **没有重新生成机制**
   - AI 的 ReAct 循环中没有"生成新的 draft"的工具
   - AI 只能调用现有的工具
   - 没有机制让 AI 重新生成 draft

3. **架构问题**
   - 错误信息注入和 DSL 生成是分离的
   - 没有将错误信息传递给 DSL 生成器的机制
   - 需要重构架构，让错误信息能够传递给 DSL 生成器

---

## Bug #H | 分段生成模式 input_contract 为空导致变量占位符未替换

- 日期：2026-05-28
- 状态：fixed
- 来源：E2E 测试
- 描述：用户提供测试数据 `账号：Xjy13302412005@outlook.com，密码：123456`，但执行时 input 步骤直接输入 `${email}` 而非实际邮箱。
- 根因：`generate_segmented_case_draft` 硬编码 `"input_contract": []`，导致步骤中的 `${email}` 等占位符无法被 `_build_input_values_from_session` 解析为实际变量值。
- 处理：新增 `_extract_input_contract_from_steps` 函数，从步骤的 `${...}` 占位符自动提取并生成 `input_contract`，支持变量类型推断和去重。
- 验证：37 单元测试通过
- 关联记录：Session 247 Draft 176

---

## Bug #G | LLM 生成 assert_text 缺 value；字段别名表定义但未接入 normalizer

- 日期：2026-05-25
- 状态：fixed (`2d3161a`)
- 来源：E2E 回归测试
- 描述：5 个 segment 全部成功生成共 16 步骤，但 `DSLCase.model_validate` 抛 `steps.15.assert_text.value Field required`。LLM 把期望文本放进 `target` 字段，漏填 `value`。
- 根因：
  - `_normalize_llm_step` 此前仅处理 `goto/assert_url_contains` 的 target→value 移动，未覆盖 `assert_text`
  - `_STEP_TARGET_ALIASES` / `_STEP_VALUE_ALIASES` / `_STEP_TIMEOUT_ALIASES` 三张别名表已定义但全文无调用（孤儿数据）
- 处理：
  - `_normalize_llm_step` 接入三张别名表，用 `_promote_first_alias()` 转换
  - `assert_text` 特殊修复：value 缺 + target 在 → target 移到 value，target 兜底为 `"body"`
  - `input`/`click`/`wait_for`/`capture_text` 必填字段缺失时返回 None 让 normalizer 丢弃
  - `_build_segment_prompt` 分别枚举每类 action 的字段要求，给正反例
- 验证：542/544 单元测试通过
- 关联记录：execution-log.md 2026-05-25（Bug A→F 链路收尾）

---

## Bug #F | LLM 生成 goto/assert_url_contains 步骤时 target↔value 字段错位

- 日期：2026-05-25
- 状态：fixed
- 来源：E2E 回归测试
- 描述：LLM 调用成功返回，但 `DSLCase.model_validate` 抛 `steps.0.goto.value Field required`，LLM 把 URL 错填到 `target` 字段。
- 根因：`goto` 和 `assert_url_contains` 用 `value` 存 URL，但 segment prompt 没显式区分字段；`_ACTION_ALIASES` 字典已定义但全文未使用（governance 清理遗留废代码）。
- 处理：
  - 新增 `_normalize_llm_step(step)`：激活 `_ACTION_ALIASES`（open/navigate/visit → goto 等），对 `_URL_VALUE_ACTIONS = {"goto", "assert_url_contains"}` 自动把 target 搬到 value
  - `_build_segment_prompt` 增加显式规则：`goto/assert_url_contains 使用 'value' 存放 URL`
- 验证：5 个 segment 全部成功
- 关联记录：execution-log.md 2026-05-25

---

## Bug #E | _log_dsl_cache_usage 函数被 governance 清理误删但调用方残留

- 日期：2026-05-25
- 状态：fixed (`f22ccb8`)
- 来源：E2E 回归测试
- 描述：LLM 调用成功返回，但 segment 报 `name '_log_dsl_cache_usage' is not defined`，最终又抛出"所有分段均未生成步骤"。
- 根因：commit `8d92654`（refactor: delete governance system）把函数定义一起删掉，但 `_call_llm:402` 和 `_call_dsl_flash_llm:510` 仍保留调用。Bug A 修复让 LLM 调用真正成功后，这个潜伏代码 rot 才暴露。
- 处理：恢复 `_log_dsl_cache_usage` 函数定义（参照 commit `6372a8f`），加 `isinstance` 防御
- 验证：segment 正常生成步骤
- 关联记录：execution-log.md 2026-05-25

---

## Bug #B | LLM 调用无重试 + 网络错误消息误导

- 日期：2026-05-25
- 状态：fixed
- 来源：E2E 回归测试
- 描述：`urlopen WinError 10060`（TCP 超时连接 `api.deepseek.com`）单次失败即终止整段 DSL 生成，且最终错误为"所有分段均未生成步骤"——误导用户以为是元素问题。
- 根因：`_call_dsl_flash_llm` 直接调 `request.urlopen` 无重试、无退避；错误消息未区分网络失败 vs 真正的"无元素"问题。
- 处理：
  - 新增 `_urlopen_with_retry`（指数退避 1s→2s，2 次重试）+ `_is_transient_network_error`
  - 新增 `DslGenerationNetworkError` 异常，给出准确诊断（含 host、错误类型、排查建议）
  - `generate_segmented_case_draft` 末尾判断 warnings 是否全为网络错误关键字，命中则抛 `DslGenerationNetworkError`
- 验证：网络错误时给出准确诊断而非误导信息
- 关联记录：execution-log.md 2026-05-25

---

## Bug #D | stream_planning_turn 把 Pydantic plan 当 dict 用导致 AttributeError

- 日期：2026-05-25
- 状态：fixed (`67f021a`)
- 来源：E2E 回归测试
- 描述：日志反复出现 `Auto DSL generation failed: 'AIPlanningPlan' object has no attribute 'get'`，导致计划生成后跳过自动 DSL 草案。
- 根因：`response.plan` 是 `AIPlanningPlan` Pydantic 模型，代码写成 `plan_json.get("scenarios", [])`。
- 处理：改为 `plan_data = response.plan.model_dump(mode="json") if response.plan else {}`
- 验证：自动 DSL 草案正常生成
- 关联记录：execution-log.md 2026-05-25

---

## Bug #A | single-segment 路径下 a11y_nodes 数据丢失

- 日期：2026-05-25
- 状态：fixed
- 来源：E2E 回归测试
- 描述：AI 规划 fallback plan 时 `scenario["flow_steps"]=[]`，走 single-segment 分支。后端日志显示 `a11y_nodes=1136` 但 `has_page_elements=False`，LLM 拿到的 prompt 中 `Available elements: (no elements)`。
- 根因：`ai_planning.py:561` 调 `generate_dsl_case` 时未把 `a11y_nodes_raw` 传过去；`dsl.py:147` 中 `page_elements_by_state` 硬编码为 `{}`。
- 处理：
  - `GenerateDslRequest` 新增 `a11y_nodes_by_state: dict[str, list[dict]] | None` 字段
  - `dsl.py` 从 `payload.a11y_nodes_by_state` 读取
  - `ai_planning.py` 单段分支按 `page_state` 分组 `a11y_nodes_raw` 后通过 payload 传入
  - `dsl_generator.py` 在 `flow_steps=[]` 但 `page_elements_by_state` 有数据时按 page_states keys 迭代
- 验证：single-segment 路径正常生成步骤
- 关联记录：execution-log.md 2026-05-25

---

### 交叉索引：Bug #A（2026-05-25）single-segment 路径下 a11y_nodes 数据丢失

- 日期：2026-05-25
> 详见 [A. DSL 生成与归一化](#bug-a--single-segment-路径下-a11y_nodes-数据丢失)，该 bug 同时影响页面探索数据传递链路。

---

## Bug #C | agent 重复调用工具浪费安全帽轮次

- 日期：2026-05-25
- 状态：fixed
- 来源：E2E 回归测试
- 描述：agent 在 5 轮内调用 `create_project` ×2（轮 2&3）、`explore_flow` ×2（轮 4&5），耗尽 5 轮安全帽 → fallback plan → flow_steps 为空。
- 根因：ReAct loop 对工具调用无去重判断。
- 处理：
  - 新增 `_tool_call_signature(tool_name, params)` 生成调用签名
  - 工具执行前比对已有签名，命中重复时：yield 重复事件 + 注入警告系统消息 + `round_index -= 1` 不扣 round
- 关联记录：execution-log.md 2026-05-25

---

### 交叉索引：Bug #B（2026-05-25）LLM 调用无重试 + 网络错误消息误导

- 日期：2026-05-25
> 详见 [A. DSL 生成与归一化](#bug-b--llm-调用无重试--网络错误消息误导)，该 bug 同时影响基础设施层面的网络健壮性。

---

## Bug #1 | AIPlanningSession UnboundLocalError in explore_flow

- 日期：2026-05-16
- 状态：fixed
- 来源：E2E 测试
- 描述：`explore_flow` 工具调用时报 `cannot access local variable 'AIPlanningSession'`。import 被放在条件块内，当 `base_url` 已通过 params 提供时被跳过。
- 处理：将 import 移到条件块之前。
- 关联记录：execution-log.md 2026-05-16

---

## Bug #2 | explore_page networkidle 超时导致异常

- 日期：2026-05-16
- 状态：fixed
- 来源：E2E 测试
- 描述：`explore_page` 在 automationexercise.com 上反复超时 `Timeout 30000ms exceeded`。部分网站持续发送跟踪请求，networkidle 永远达不到。
- 处理：用 try-except 包装 `wait_for_load_state("networkidle")`。
- 关联记录：execution-log.md 2026-05-16

---

## Bug #3 | capture_text 步骤的 value 在报告中始终为 null

- 日期：2026-05-16
- 状态：fixed
- 来源：E2E 测试
- 描述：`capture_text` 成功执行但报告中 `value` 字段始终为 `null`。捕获的文本存到了 `runtime_context` 但 `StepExecutionEvidence.value` 读取的是 `getattr(step, "value", None)`。
- 处理：引入 `step_value` 局部变量，capture_text 分支更新为实际捕获值。修复覆盖 4 个代码路径。
- 关联记录：execution-log.md 2026-05-16

---

## BUG-066 | AI 不遵循 explore_flow 提示词，跳过页面探索直接生成方案

- 日期：2026-05-12
- 状态：fixed
- 来源：E2E 测试
- 描述：DSL 草案基于不完整的页面数据生成，缺少步骤、target 泛化。
- 根因：`_build_link_selection_message` 中有"如果信息足够，也可以直接 generate_plan"逃逸口；安全网消息误导 LLM。
- 处理：删除逃逸口；安全网消息改为"静态页面已采集，交互页面仍需 explore_flow"。
- 验证：AI 不再跳过探索

---

## BUG-065B | explore_flow 相对 URL 被解析为 example.com

- 日期：2026-05-12
- 状态：fixed
- 来源：E2E 测试
- 描述：`page_explorer.py:1594` 硬编码 `base_url or "https://example.com/"` 兜底；`planning_tools.py` explore_flow 工具定义缺少 `base_url` 参数。
- 处理：移除硬编码默认值；explore_flow 工具定义添加 `base_url` 参数；base_url 提取逻辑重构到函数开头共享。
- 验证：542/544 单元测试通过

---

## BUG-085 | DeepSeek thinking 模式 + 高温导致 AI 不遵循提示词指令

- 日期：2026-05-10
- 状态：fixed
- 来源：BUG 日志聚合分析
- 描述：综合 BUG-081/069/065/054 等高频问题，根因指向两点：(1) thinking 模式对指令遵循有负面影响；(2) DeepSeek API 默认 temperature=1.0 过高。
- 处理：
  1. 在 dsl_generator.py、test_planning_agent.py、judge_agent.py 中移除 DeepSeek 的 thinking mode（仅保留 GLM）
  2. 按场景设置 temperature：DSL generator 0.0、DSL flash 0.0、Planning agent 0.1、Judge 0.0
- 验证：542/544 单元测试通过

---

### 交叉索引：BUG-085（2026-05-10）DeepSeek thinking 模式 + 高温导致 AI 不遵循提示词

- 日期：2026-05-10
> 详见 [A. DSL 生成与归一化](#bug-085--deepseek-thinking-模式--高温导致-ai-不遵循提示词指令)，该 bug 同时影响 AI 决策层。

---

## BUG-083 | AI 将 assert_text 的 ${var} 放在 target 而非 value 导致断言被删除

- 日期：2026-05-07
- 状态：fixed (7958d3b, 未验证)
- 来源：E2E 回归测试
- 描述：AI 生成 `assert_text target='${product_a_name}' value=''`，Pydantic `min_length=1` 拒绝空 value → 8 个断言步骤被归一化器删除。
- 根因：AI 模型混淆 assert_text 的 target（元素定位器）和 value（期望值）字段。
- 处理：prompt 添加"target 是页面文本，value 是 ${var}"规则 + 归一化器自动补全
- 验证：未验证（需新 session 生成 draft 确认）

---

## BUG-078 | DSL 归一化器删除了合法的 click/wait_for/capture_text 步骤

- 日期：2026-05-07
- 状态：fixed (`ecbbb3a`)
- 来源：E2E 回归测试
- 描述：AI 给 click/wait_for/capture_text 步骤添加了 `"value": null` 字段，Pydantic `extra_forbidden` 拒绝，步骤被静默删除。
- 根因：`_repair_step_shape` 未剥离 click/wait_for/capture_text 的 spurious `value` 字段。
- 处理：在 `_repair_step_shape` 末尾对这些步骤类型移除 `value` 键。
- 验证：58 个 DSL 单元测试通过，Exec 106 证实 0 步骤被删
- 关联记录：Draft 80 8 步骤被删，Draft 81 0 步骤被删

---

## BUG-077 | DSL 归一化器删除了合法的 goto/assert_url_contains 步骤

- 日期：2026-05-07
- 状态：fixed (`8d05871`)
- 来源：E2E 回归测试
- 描述：AI 给 goto 和 assert_url_contains 添加了 `candidates: []` 和 `postconditions: []` 字段，Pydantic `extra_forbidden` 拒绝，goto 步骤被丢弃。
- 根因：AI 给所有步骤统一加了 candidates/postconditions 空数组，但 GotoStep 和 AssertUrlContainsStep 模型没有这些字段。
- 处理：在 `_repair_step_shape` 中对 goto/assert_url_contains 剥离 candidates/postconditions。
- 验证：58 个 DSL 单元测试通过

---

## BUG-084 | text_parent_chain 在品牌页第二个产品上回退到 VLM

- 日期：2026-05-07
- 状态：fixed
- 来源：E2E 回归测试
- 描述：Step 15 "Blue Top 附近的 Add to cart" 用 text_parent_chain 成功，但 Step 21 "Fancy Green Top 附近的" 回退到 ai_coordinate_click。`_find_in_ancestor` 始终用 `.first` 获取第一个匹配。
- 根因：`_find_in_ancestor` 和 `_resolve_text_parent_chain` 始终用 `.first`，不尝试其他 nth 候选。
- 处理：改为迭代 `.nth(0..4)` 多个候选，找到第一个成功匹配的返回。
- 验证：33/33 语义单元测试通过

---

## BUG-082 | capture_page_session 使用简化版 resolver 导致登录态不稳定

- 日期：2026-05-07
- 状态：fixed
- 来源：E2E 回归测试
- 描述：`capture_browser_session` 使用简化版 `_resolve_step_locator`（只尝试少量候选 + count()>0 即返回），Email placeholder 匹配到 3 个元素 → strict mode 失败。
- 根因：没有使用完整的定位器链路（semantic → a11y → VLM fallback）。
- 处理：改为直接调用 `resolve_with_fallback`；添加页载等待 + 元素 tag 验证 + 2 次重试。
- 验证：S161 capture_page_session 成功，page_elements 从 73K → 1.28MB

---

## BUG-080 | assert_text 的 target 字段中的运行时变量未被替换

- 日期：2026-05-07
- 状态：fixed
- 来源：E2E 回归测试
- 描述：`assert_text target="${cart_a_total}"` 中的 `${cart_a_total}` 未被 `_substitute_variables` 替换 → 定位器按字面量查找 → 永远找不到 → 走 VLM 兜底。
- 根因：`_substitute_variables` 只对 `step.value` 调用，未对 `step.target` 调用。
- 处理：所有 runner 和 helper 中，`step.target` 使用前统一替换。
- 验证：543/544 单元测试通过

---

## BUG-076 | DSL target 文本中的中文字符被 PostgreSQL JSON 序列化损坏

- 日期：2026-05-07
- 状态：fixed (`aaa3f18`)
- 来源：E2E 回归测试
- 描述：target 字段中"附近的"被序列化为 `\udc84`（lone low surrogate），`text_parent_chain` 的 regex 无法匹配。所有含中文的 target 均受影响（6 个步骤）。
- 根因：PostgreSQL JSONB 序列化过程中 Unicode BMP 字符被错误编码为 surrogate pair。
- 处理：DSL 归一化入口处检测并修复 surrogate 字符。
- 验证：Draft 81 证实 surrogate_targets=0

---

## BUG-075 | 元素视觉分组的 group label 太粗糙

- 日期：2026-05-07
- 状态：fixed
- 来源：E2E 回归测试
- 描述：`_group_label` 的 if-elif 链太刚性，价格检测遗漏纯数字价格，1-2 个元素的分组 label 无意义。
- 处理：价格检测增强 + 新增 8 种块类型 + 渐进阈值回退 + aria_label 命名。
- 验证：542/544 单元测试通过

---

## BUG-074 | text_parent_chain 定位器未在 runner 候选列表中被优先尝试

- 日期：2026-05-07
- 状态：fixed (`6922bc8`)
- 来源：E2E 回归测试
- 描述：`_resolve_with_confidence_gate` 在 `locator_confidence="low"` 时先调 VLM preverify，跳过了 text_parent_chain。
- 根因：VLM preverify 不应跳过语义定位链。
- 处理：重构为统一流程——语义优先、VLM 仅作最后兜底。添加 2.5 分钟步骤超时。
- 验证：Exec 102+ 证实 text_parent_chain 在候选列表中排在第一位

---

## BUG-073 | text_parent_chain 的正则表达式无法匹配含空格的父文本

- 日期：2026-05-07
- 状态：fixed (`aabea6a`)
- 来源：E2E 回归测试
- 描述：`_PARENT_TEXT_RE` 使用 `[^>\\s>{2,60}?`（惰性匹配 + 排除空格），"Blue Top 附近的 Add to cart" 无法匹配。
- 根因：惰性量词使匹配过短 + `[^>\\s]` 错误排除了空格。
- 处理：改用 split 方式——`_PARENT_SPLIT_RE` 直接在 `>>`/`的`/`附近的` 处分隔。
- 验证：33 个语义单元测试通过，Exec 102 Step 11 证实成功匹配

---

## BUG-072 | text_parent_chain 使用硬编码 XPath ancestor 无法适配不同页面结构

- 日期：2026-05-07
- 状态：fixed (`892889e`)
- 来源：E2E 回归测试
- 描述：`_find_in_ancestor` 使用 `xpath=ancestor::*[contains(@class,'product')]` 硬编码 class 名。购物车页 `<tr>` 不含此 class → 返回 0 元素。
- 处理：改为自适应深度遍历——从 parent_text 元素出发，逐层 `..` 向上（depth 2-8），每层尝试 `get_by_text(child_text)`。
- 验证：33 个语义单元测试通过，手动验证购物车页 depth=3 可找到 "Rs. 500"

---

## BUG-071 | text_parent_chain 的 child_text 使用 exact=True 导致 substring match 失败

- 日期：2026-05-07
- 状态：fixed (`dba307c`)
- 来源：E2E 回归测试
- 描述：`_find_in_ancestor` 使用 `get_by_text(child_text, exact=True)`，DOM 中价格文本有前后空格/格式差异导致不匹配。
- 处理：改回 `exact=False`（子串匹配），添加 try/catch 防止异常吞没。
- 验证：33 个语义单元测试通过

---

## BUG-067 | explore_flow 相对 URL 未解析导致页面探索失败

- 日期：2026-05-07
- 状态：fixed (`f53807d`)
- 来源：E2E 回归测试
- 描述：AI 传入相对 URL（`/products`, `/brand_products/Polo`），`collect_multi_page_elements` 未解析，Playwright `page.goto("/products")` 失败 → 返回空元素。
- 处理：用 `urljoin` 将相对 URL 解析为绝对 URL。
- 验证：S152 page_elements 从 81 字符增长到 157KB

---

## BUG-081 | DSL 草案间质量剧烈波动 — 相同 prompt 产出 42 步和缺登录的 30 步

- 日期：2026-05-07
- 状态：fixed
- 来源：E2E 回归测试
- 描述：相同 draft_prompt（2069 chars），S155 产出 42 步完整草案，S162 产出缺少登录导航的 30 步草案。根因是 DSL 生成模型的随机性。
- 处理：(1) 系统提示词加入【流程-页面导航映射】规则；(2) 草案生成后一致性检查。
- 验证：Draft 89 包含完整 click "Signup / Login" 导航

---

## BUG-066B | AI 的 core_user_flow 被序列化为 Python list repr

- 日期：2026-05-07
- 状态：fixed (`23e3bc9`)
- 来源：E2E 回归测试
- 描述：AI 以 list 形式返回 `core_user_flow`，`_merge_requirements` 调用 `str(incoming)` 转成 Python repr 字符串 `"['打开首页...', '点击 Products...']"`。DSL prompt 收到畸形流程描述，质量极差。
- 处理：对 core_user_flow 和所有 list 字段统一 join 为编号列表。
- 验证：S146 draft 72 43 步（修复后） vs S145 draft 70 17 步（修复前）

---

## BUG-079 | 购物车测试数据污染 — 前序测试遗留商品导致数量断言失败

- 日期：2026-05-07
- 状态：verified (Exec 107 42/42=100%)
- 来源：E2E 回归测试
- 描述：Exec 106 Step 27 `assert_text '1' value='${cart_a_quantity}'` 失败。capture 抓到数量 31（前序测试累积），断言期望 1。定位器本身工作正常。
- 根因：测试间缺少购物车清理步骤。
- 处理：(1) 测试开始前清空购物车；(2) AI 不应硬编码数量值，应 capture 后做一致性比较。
- 验证：用户手动清空购物车后 Exec 107 42/42=100%

---

## BUG-070 | DSL generator thinking mode 下 reasoning_content 空响应

- 日期：2026-05-06
- 状态：fixed (`9f67995`)
- 来源：E2E 回归测试
- 描述：DSL generator 使用 DeepSeek thinking mode 时，模型返回 `reasoning_content` 但 `content` 为空 → JSON 解析失败 → 草案状态 failed。
- 根因：`_extract_message_content` 只读 `content` 字段，忽略了 `reasoning_content`。
- 处理：在 content 为空时 fallback 到 `reasoning_content`
- 验证：Draft 62 生成成功（33 步）

---

## BUG-065 | DSL prompt 未要求 capture_text 后必须跟 assert_text

- 日期：2026-05-06
- 状态：fixed (`a631041`, `c5e4411`)
- 来源：E2E 回归测试
- 描述：AI 生成 5 个 capture_text 但 0 个 assert_text，测试表面全部通过但核心断言完全缺失。
- 根因：DSL prompt 只说明了 capture_text 用法，未强制要求 capture 后必须 assert。
- 处理：在系统 prompt 和用户规则中增加"capture 必须 assert"规则；新增"modify→input→assert"规则。
- 验证：Draft 69 有 10 个 assert_text（vs Draft 66 的 0 个）

---

## BUG-064 | preflight 将所有 target 标记为 low confidence 导致 VLM 抢占

- 日期：2026-05-06
- 状态：fixed（通过 BUG-074 的流程重构规避）
- 来源：E2E 回归测试
- 描述：preflight 在已探索元素中找不到精确匹配 → 几乎所有步骤标记为 `locator_confidence="low"` → VLM 抢占语义定位链。
- 处理：通过 BUG-074 的执行流程重构绕过（语义链优先、VLM 兜底）。
- 验证：Exec 102+ 证实语义定位链在 VLM 之前执行

---

## BUG-068 | 页面探索压缩子代理丢弃登录表单元素

- 日期：2026-05-06
- 状态：fixed (`081c49e`)
- 来源：E2E 回归测试
- 描述：`_filter_elements_for_compression` 硬编码取前 100 个元素，登录表单字段可能在第 100+ 位置被截断。子代理 prompt 对表单强调不足，压缩结果 `forms: []` 为空。
- 处理：改为优先保留交互元素（input/button/select/textarea/a），非交互元素限制 80 个；重写 prompt 强制 JSON 结构。
- 验证：65 个单元测试通过

---

## BUG-069 | 系统提示词引导 AI 在信息充足时仍使用 ask_user 询问确认

- 日期：2026-05-06
- 状态：fixed (`13016a6`)
- 来源：E2E 回归测试
- 描述：系统提示词第 91 行：当收集到 4+ 项信息时，通过 ask_user 询问"信息是否足够"。AI 第一轮动作为 ask_user 而非 explore_page。
- 处理：修改规则为"信息充足时直接 generate_plan，不用 ask_user"。
- 验证：Session 139 AI 第一轮动作为 call_tool（get_project_info），不再问废话

---

## BUG-063 | DeepSeek thinking 模式下 SSE 流式输出空白 + 会话消失

- 日期：2026-05-04（含 2026-05-05 追加修复）
- 状态：fixed
- 来源：线上反馈
- 描述：使用 `deepseek-v4-flash` 模型时，SSE 流式输出直接空白——思考阶段前端完全看不到任何文本内容。刷新后会话消失。
- 根因（多层）：
  1. `reasoning_content` 只在内存累积、仅发节流 status 消息，不产出 `text_chunk` 事件
  2. `reasoning_text` 未归入 `raw_response`，content 为空时触发 `empty_response` 错误
  3. `_call_planning_llm()` 非流式路径只提取 `message.content`，忽略 `reasoning_content`
  4. `turn_complete` 后 `loadSessionDetail()` 用服务端数据替换 transcript，`_thinkingContent` 丢失
  5. 历史消息加载后未清除 `_streaming: true` 标志
- 处理：
  - backend：每个 `reasoning` chunk 同步产出 `text_chunk` 事件（带 `thinking: true`）；content 为空时用 `reasoning_text` 兜底；非流式路径 fallback 到 `reasoning_content`
  - frontend：`_thinkingContent` 存入独立字段 + 渲染可折叠 `<details>` "思考过程"区块；加载历史消息时清除 `_streaming` 标志；`turn_complete` 后保留 `_thinkingContent`
- 验证：29 planning agent 单测 + 11 API 测试通过；TypeScript 编译无错误
- 关联记录：execution-log.md 2026-05-04、2026-05-05

---

## BUG-056 | DSL draft prompt 超 50000 字符导致 Pydantic 校验失败

- 日期：2026-05-03
- 状态：fixed
- 来源：Session 15 E2E 测试
- 描述：`_build_draft_prompt` 将 80K+ 字符的 page_elements 直接嵌入 `draft_prompt`，触发 `max_length=50000` 限制。
- 根因：`page_elements` 数据在两个渠道重复传递——嵌入 prompt + 独立字段。
- 处理：嵌入式 DOM section 替换为简短提示，实际数据通过 `GenerateDslRequest.page_elements` 单独传递。
- 验证：471 单元测试通过

---

## BUG-057 | click_with_precheck 对 hidden 元素超时不触发恢复链

- 日期：2026-05-03
- 状态：fixed
- 来源：Session 15 E2E 测试
- 描述：点击 modal 中 "Continue Shopping" 时，Playwright 报 `resolved to hidden`，但 `_is_interception_error` 只匹配 `"intercepts pointer events"`，5 策略恢复链完全被跳过。
- 处理：新增 `_HIDDEN_ELEMENT_PATTERN` 匹配 `"resolved to hidden"`，直接走 `_try_force`。
- 验证：471 单元测试通过

---

## BUG-060 | AI planning 中间层三大架构断层

- 日期：2026-05-03
- 状态：fixed
- 来源：架构排查 / BUG-059 延伸
- 描述：三个架构断层：
  1. `explore_flow` 仍是 URL 级探索——不会在页面间执行点击/输入/等待动作
  2. 页面知识是扁平 `page_elements` 文本——无页面状态标记
  3. DSL 生成后无 locator preflight——定位器验证全部推迟到执行期
- 处理（Phase 1-3 全套升级）：
  - Phase 1：`collect_flow_elements(steps)` 支持动作式探索
  - Phase 2：`page_state_id` 页面状态标记 + DSL step `page_state` 字段
  - Phase 3：`locator_preflight.py` 静态校验 DSL targets 与已采集元素的匹配度
- 验证：485 单元测试全部通过

---

## BUG-059 | AI planning 中间层仍是 URL 级探索而非 flow 驱动探索

- 日期：2026-05-03
- 状态：fixed
- 来源：架构排查
- 描述：`_auto_explore_entry_url()` 只按入口页链接顺序抓取前 4 个链接，逻辑与 `core_user_flow` 无绑定。
- 处理：`_extract_internal_links()` 升级为 flow 驱动——按 URL 路径与关键词匹配度评分排序，优先探索流程相关页面。
- 验证：471 全部通过；含 login 流程时 /login 从位置 3 提升至位置 1

---

## BUG-058 | AI Test Planning 面板切换会话后仍把项目操作发送到初始 session

- 日期：2026-05-03
- 状态：fixed
- 来源：架构排查
- 描述：`AITestPlanningPanel` 内部根据选择切换 `sessionId` 状态，但渲染 `SessionProjectPanel` 时仍传入初始 `sessionIdProp`。切换 session 后项目操作仍落到旧 session。
- 处理：`AITestPlanningPanel.tsx:621` 将 `sessionId={sessionIdProp}` 改为 `sessionId={sessionId ?? 0}`。
- 验证：TypeScript 类型检查通过；切换 session 后请求使用当前 session id

---

### 交叉索引：BUG-057（2026-05-03）click_with_precheck 对 hidden 元素超时不触发恢复链

- 日期：2026-05-03
> 详见 [B. 定位器系统](#bug-057--click_with_precheck-对-hidden-元素超时不触发恢复链)，该 bug 同时影响执行引擎的点击预处理。

---

## BUG-053 | VLM bbox 坐标在 DOM 选择器提取失败时被丢弃

- 日期：2026-04-25
- 状态：fixed
- 来源：BUG-054 根因分析
- 描述：VLM 返回准确 bbox 坐标，但 `_build_locator_from_ai_point()` DOM 选择器提取失败时，整个 `AILocateResult` 被丢弃。Playwright 原生支持 `page.mouse.click(x,y)` 但从未使用。
- 处理：`ResolvedLocator` 新增 `click_coordinates` 字段；新增 `_try_coordinate_click_fallback()` Tier 2.5 回退。
- 验证：Exec 69/70 全部通过

---

## BUG-054 | AI 忽略用户描述的弹层交互步骤，用导航栏元素替代弹层元素

- 日期：2026-04-25
- 状态：fixed
- 来源：Session 52 E2E 测试
- 描述：用户明确写了"在弹层中点击 View Cart"，但 AI 使用导航栏 "Cart"。点击 "Add to cart" 后弹层遮挡了导航栏 "Cart"，导致 click 超时。
- 根因：(1) 静态 explore_flow 无法采集动态弹层元素；(2) AI 未严格遵循用户描述。
- 处理：三重修复——`_discover_interactive_elements()` 捕获弹层元素 + Prompt 追加动态交互规则 + `[dynamic]` 标记。
- 验证：Session 53 Draft 26/27 正确使用 "View Cart"；Exec 69/70 各 13/13 全部通过

---

### 交叉索引：BUG-053（2026-04-25）VLM bbox 坐标在 DOM 选择器提取失败时被丢弃

- 日期：2026-04-25
> 详见 [B. 定位器系统](#bug-053--vlm-bbox-坐标在-dom-选择器提取失败时被丢弃)，该 bug 同时影响执行引擎的坐标点击回退。

---

## BUG-051 | input_contract 变量占位符在执行时未被替换

- 日期：2026-04-24
- 状态：fixed
- 来源：BUG-050 修复验证
- 描述：AI 生成的 DSL 包含 `${login_email}`、`${search_keyword}` 等变量占位符，save-and-execute 时未替换为实际值。`${search_keyword}` 被直接作为字符串输入到搜索框。
- 根因：变量替换功能完全未实现——runner 直接使用 `step.value` 原始字符串。
- 处理：`playwright_runner.py` 新增 `_substitute_variables` 函数；`CaseExecutionRequest` 增加 `input_values` 字段；4 处 step.value 使用处全部替换。
- 验证：303 单元测试全部通过

---

## BUG-050 | AI DSL 生成定位策略不匹配 DOM 结构

- 日期：2026-04-23
- 状态：fixed
- 来源：白盒测试（Automation Exercise）
- 描述：AI 生成 `.productinfo text='View Product'` 链式选择器，但 `.productinfo` 和 "View Product" 是兄弟关系非父子，匹配 0 元素。
- 处理：五重修复——链式选择器解析 + prompt 禁止无效复合格式 + target_strategy 字段 + error_message 改 Text + DOM 证据注入。
- 验证：7/7 链式选择器测试通过

---

## BUG-048 | AI DSL 规划阶段完整性校验缺失

- 日期：2026-04-21
- 状态：fixed
- 来源：白盒测试（The Internet Login Page）
- 描述：AI 生成 DSL 时遗漏 goto 步骤，base_url 设为完整登录页 URL 但不生成 goto 步骤，执行器在 about:blank 上操作。
- 处理：(1) Prompt 增加测试五要素完整性引导；(2) 后处理新增 `_check_dsl_completeness` 函数。
- 验证：5 passed

---

## BUG-049 | 语义定位器不支持标签名开头的复合 CSS 选择器

- 日期：2026-04-21
- 状态：fixed
- 来源：白盒测试（The Internet Login Page）
- 描述：`_resolve_explicit_locator` 只识别以 `css=`、`#`、`.` 等开头的目标，`button[type='submit']` 以字母开头落入文本匹配。
- 处理：新增 `_COMPOUND_CSS_RE` 启发式正则识别 `tag[attr]`、`tag.class`、`tag > child` 等复合模式。
- 验证：6 passed

---

## BUG-046 | 语义定位器缺少 element_id 和 case-insensitive 匹配策略

- 日期：2026-04-17
- 状态：fixed
- 来源：集成测试自测
- 描述：无法定位以 HTML id 属性命名的目标（如 "flash"），`get_by_label("username", exact=True)` 无法匹配 "Username"。
- 处理：新增 `element_id` 策略（优先级 100）+ `label_fuzzy`/`placeholder_fuzzy`/`text_fuzzy`/`button_role_fuzzy` 四个非精确匹配策略。
- 验证：6 passed

---

## BUG-047 | playwright_runner _capture_request_failed 对 request.failure 返回格式处理错误

- 日期：2026-04-17
- 状态：fixed (d73558e)
- 来源：集成测试执行日志
- 描述：新版 Playwright 的 `request.failure` 返回类型为 `str` 而非 `dict`，`failure.get("errorText")` 抛出 `AttributeError`。
- 处理：改为 `isinstance(failure, str)` 兼容两种类型。
- 验证：集成测试不再报 AttributeError

---

## BUG-045 | AI planning "保存并执行草案"链路被 DSL 生成配置阻断

- 日期：2026-04-13
- 状态：in_progress
- 来源：白盒排查 / session_id=27
- 描述：`AI_DSL_BASE_URL=https://api.unself.cn` 返回 `200 text/html` 站点首页而非 OpenAI 兼容 JSON → `JSONDecodeError`；且 draft 生成/执行结果未持久化到 `ai_planning_messages`。
- 处理进展：已修正 `AI_DSL_BASE_URL`；已为 `_call_llm()` 增加非 JSON 响应防御；已将结果持久化到 messages。剩余：执行中流式事件推送。
- 验证：数据库实查 + 最小 HTTP 复现

---

### 交叉索引：BUG-045（2026-04-13）AI planning "保存并执行草案"链路被 DSL 生成配置阻断

- 日期：2026-04-13
> 详见 [A. DSL 生成与归一化](#bug-045--ai-planning-保存并执行草案链路被-dsl-生成配置阻断)，该 bug 同时涉及配置问题（`AI_DSL_BASE_URL` 指向错误端点）。

---

## BUG-044 | AI Planning 面板缓存失效会话时不会回退创建新会话

- 日期：2026-04-12
- 状态：fixed
- 来源：需求实现 / 静态检查
- 描述：`localStorage.ai_planning_last_session` 指向已删除 session 时，恢复失败后不会自动创建新会话，页面卡在无活跃 session 状态。
- 处理：引入 `loadSessionDetail()` / `createAndSelectSession()` helper，恢复失败时清理缓存并自动创建。
- 验证：前后端测试通过

---

## BUG-043 | 新增 AI planning 配置字段后，settings API 更新合同未同步

- 日期：2026-04-03
- 状态：fixed
- 来源：任务实现 / 回归测试
- 描述：新增 `enable_ai_planning` 等 planning 字段后，`AISettingsUpdateRequest` 已要求必填，但旧测试和前端仍用旧 payload，触发 422。
- 处理：补齐后端测试中的 planning 字段；前端 `AISettings`/`AISettingsPage` 一并纳入。
- 验证：前后端测试通过

---

## BUG-042 | AI 测试规划面板初始化首条消息可能丢失

- 日期：2026-03-30
- 状态：fixed
- 来源：自测
- 描述：session 尚未创建完成前允许点击"发送消息"，首条输入被忽略；`.gitignore` 中 `tests/` 规则导致新测试文件默认未跟踪。
- 处理：发送按钮增加 `isBootstrapping`/`sessionId`/空输入约束；`.gitignore` 新增白名单。
- 验证：前后端测试通过

---

## BUG-041 | 最新 CRUD 提交存在权限绕过、统计接口运行时失败与删除路径不闭合

- 日期：2026-03-30
- 状态：fixed (082ae22)
- 来源：代码评审 / commit 7eb71ae
- 描述：4 类问题——(1) 任意已登录用户能读取/更新/删除其他项目用例（权限绕过）；(2) `GET /stats/{project_id}` 缺少必填字段导致 500；(3) 有 test_cases 的项目删除时触发 RESTRICT 约束；(4) 历史接口响应合同变化但测试未更新。
- 处理：补齐项目成员权限校验、修正 stats 返回结构、处理外键约束下的项目删除语义、更新测试断言。
- 验证：全量测试通过
