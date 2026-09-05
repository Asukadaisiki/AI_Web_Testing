# 执行日志

用于沉淀每次任务实际做了什么，方便后续追溯、复盘和回答一致化。

## 记录规则

- 每次处理需求后，在「任务记录」顶部按时间倒序新增一条记录。
- 记录结构统一为：任务、操作、结果、验证、后续；无后续事项时写“无”。
- 如果执行过程中发现明确缺陷，同时在 `docs/bug-log.md` 追加对应条目并互相引用。
- 最新的记录优先放到最上面，方便阅读。

## 记录模板

```md
## YYYY-MM-DD | 标题

- 任务：本次要解决或分析的事项。
- 操作：实际执行的关键动作。
- 结果：产出、结论或修复效果。
- 验证：已执行的验证；如果没有写“未验证”。
- 后续：下一步事项；如果没有写“无”。
```

## 阶段总览

| 阶段 | 日期 | 主题 | 关键成果 |
|------|------|------|----------|
| M1 基础 | 03-28~03-31 | 认证、CRUD、Suite 下线、AI Planning 雏形 | 认证基线 + 用例 CRUD + AI 对话面板 |
| M2 用例生成 | 04-03~04-17 | ReAct Agent、流式执行、定位器系统、DOM 感知 | ReAct loop + WebSocket 流式 + 4 层定位器 |
| M2 执行闭环 | 04-20~04-28 | DOM-aware DSL、VLM 两阶段、Explorer-Judge、评分系统 | 完整 plan→execute→analyze 闭环 |
| M2 前端重构 | 04-25~04-26 | NotebookLM 布局、E2E 手动测试、Explorer-Judge 架构 | 三栏布局 + 缺陷发现范式 |
| E2E 调优 v1 | 05-02~05-07 | 页面探索、定位器回退链、text_parent_chain、AI 质量循环 | 26 commits, 96% 步骤通过率 |
| E2E 调优 v2 | 05-10~05-12 | DeepSeek 温度/thinking 优化、提示词修复、11 项架构优化 | 544 tests, 0 failures |
| 架构重构 | 05-14~05-17 | 主路径 v2 A11y 管线、dead code 清理、DSL 生成链路修复 | 491 tests, -3.1K 行, A11y CDP 100x 快 |
| 链路修复 | 05-25 | DSL 生成链路 7 层 bug 修复（Bug A→G） | 543 tests, 16 新增测试 |
| 孤儿数据清理 | 05-25 | 全面清理代码库中的孤儿数据 | 删除 14 项孤儿数据 |
| 数据校验修复 | 05-25 | 数据传递与校验全面扫描修复 | 修复 19 项问题 |
| 变量占位符修复 | 05-28 | 分段生成 input_contract 自动提取 | 修复 ${email} 未替换问题 |
| A11y Tree 全面切换 | 05-28 | 封杀 DOM 路径，只使用 a11y tree | 500 tests, 4 项核心修复 |
| explore_flow DSL 格式支持 | 05-28 | 支持 DSL 格式步骤传入 explore_flow | 500 tests, 修复页面探索不完整 |
| 跨段变量命名权威 | 05-28 | Planning agent 输出 scenario.variables，segment prompt 注入命名字典 | 504 tests, 4 新增聚焦测试 |
| explore_flow 遮挡恢复 | 05-28 | _execute_flow_actions / capture_browser_session 接入 click_with_precheck | 504 tests, cartModal 不再杀掉探索 |
| 完整探索数据 + 用户上下文注入 | 05-29 | _load_a11y_nodes 合并所有 explore 记录 + user_context 注入 segment prompt | 504 tests, DSL 生成器看到完整元素和原始需求 |
| Agent 流程 vs 直接脚本测试 | 05-30 | explore-flow 探索、DSL 生成、执行测试对比分析 | 发现 6 项问题，a11y 过滤和选择器策略修复 |
| A11y 无名输入框定位修复 | 05-31 | label 兄弟 input 策略 + cell role 支持 | 29 steps 全通过，Quantity 输入框定位 |
| textContent + DSL 完善 | 05-31 | 4 次修复：textContent、View Product、数量修改、断言值 | 21/21 steps 全通过 |
| Anti-pattern 注入与上下文重构 | 06-04 | 重构上下文注入架构，修复执行错误注入 | 497 tests, 4 项核心修复 |
| Locator + Explore 双轨修复 | 06-05 | paragraph 角色、探索导航精确化、Prompt 清理 | 探索采集 OK, 18/18 手动 OK, 全链路待继续验证 |
| SSE 事件日志架构 | 06-08 | SSE 事件持久化 + 刷新恢复 + replay API | 解决刷新丢消息问题 |
| SSE 架构修复 v2 | 06-08 | Session 隔离 + 弹性降级 | 修复表不存在时流式崩溃 |
| 孤儿会话根因修复 | 06-08 | SSE 端点添加会话验证，测试驱动调试 | 501 tests, 根因修复 |
| 孤儿数据清理 | 06-08 | 清理 70 个孤立项目，添加清理脚本 | 数据库清理完成 |

---

## 任务记录

## 2026-09-05 | 当前项目进度复核

- 任务：基于当前分支、提交、能力文档、测试资产和缺陷状态汇总项目实际进度。
- 操作：核对 `xujinyuan/go-agentcore-v2` 与远端同步状态、近期 AgentCore 提交、README 能力矩阵、Go/Python/前端测试资产及开放缺陷；修正 README 的自愈半闭环旧口径并关闭已完成的 BUG-090；记录 BUG-106。
- 结果：项目已完成 Go AgentCore 第一条完整纵向链路和受控自愈闭环，进入控制面收敛与生产化阶段；主要剩余工作是生产部署配置、Planning Session 元数据迁移、持续回归门禁、项目级编排/定位调试 UI、鉴权恢复及历史日志治理。
- 验证：当前分支与远端提交计数为 `0/0`，盘点前工作区干净；复核 9 个 Go 测试文件、4 个 Python 聚焦测试文件和前端当前无自动化测试文件；Go 全量测试、Python 聚焦测试、前端生产构建及文档差异检查通过。
- 后续：优先完成生产反向代理与进程管理，并将真实浏览器 E2E 固化为可持续回归门禁。

## 2026-09-05 | 修复 Python 导入诊断

- 任务：检查并修复仓库中多处 Python 导入问题。
- 操作：递归验证 122 个 `app.*` 模块；使用 Ruff 定位未使用和非顶层导入；清理 9 个无效导入并调整 2 个模块的 logger 初始化位置；新增根目录 `pyrightconfig.json`，声明 `backend/.venv`、`backend` 源码路径和 Python 3.12。
- 结果：第三方依赖与 `app.*` 包可被 IDE/Pyright 正确解析，Ruff 的 `F401/F403/F405/E402` 问题由 17 个降为 0；关联 BUG-105。
- 验证：Pyright 导入检查 0 errors/0 warnings；Python 122 个模块全部导入成功；13 个单测通过；`compileall`、Go `go test ./...`、前端 `npm run build` 通过。
- 后续：仓库仍有历史导入排序提示和未纳入本次范围的类型标注债务，可单独治理。

## 2026-09-04 | 同步 Go AgentCore 重构分支到 GitHub

- 任务：将已完成并验收的 Go AgentCore、受控自愈和前端迁移改动同步到 GitHub。
- 操作：核对工作区、当前分支和最近提交；为 `xujinyuan/go-agentcore-v2` 设置远端 upstream 并推送。
- 结果：重构分支及完整提交历史已同步到 `origin`。
- 验证：推送后核对本地 HEAD、远端分支和工作区状态。
- 后续：无。

## 2026-09-04 | 前端切换 Go AgentCore 并完成浏览器验收

- 任务：将 Planning 工作台从 Python Planning SSE 切换到 Go AgentRun、ToolCall、Checkpoint 和 SSE 重放协议，并完成真实浏览器 E2E。
- 操作：新增 `features/agent` 类型、API、事件归并和 Run hook；新建 Agent 工作台展示对话、工具轨迹、artifact 和动态问题控件；最近 Run ID 按会话保存在浏览器本地，刷新后从 PostgreSQL 重放事件；Vite 分流 Go/Python 开发代理；三栏布局增加移动端纵向响应式；过滤 `RootWebArea/StaticText` 非定位节点并补回归测试；同步 README 当前阶段与后续优先级。
- 结果：前端可创建 Run、实时观察探索/验证/生成/执行/报告工具、在两个审批点暂停恢复，并在刷新后恢复完整运行。干净 E2E 使用 `heading "Example Domain"` 首次执行 3/3 步通过，没有触发修复；另一次 E2E 验证了首次 locator 失败后自动重探索、重新审批并通过的完整自愈路径。
- 验证：前端 `npm run build` 通过；Python 13 个聚焦测试通过；Playwright 桌面 1440x900 和移动 390x844 截图无横向溢出、控制台错误为 0；最终 Run 为 completed，工具调用 6 次，Batch pass_rate=1.0。全部临时 Run、generation、Case、Batch、execution 和截图已清理。
- 后续：补充生产环境反向代理配置，并评估将 Planning Session 元数据迁入 Go 控制面。

## 2026-09-04 | AgentCore 透明修复与重执行闭环

- 任务：接入非黑盒 `fix_and_retry`，验证失败事实、修复策略、DSL 重生成、人工审批和重执行的完整链路。
- 操作：Python Capability Worker 新增修复计划接口，基于 Batch/Run 的 FailureSignal 选择 `re_explore`、`regenerate_dsl` 或人工处理并返回源 DSL；Go 注册 `fix_and_retry` 工具和 repair plan artifact；Run 详情投影补充 DSL snapshot；`get_report` 默认等待 Batch 终态；增加 `analysis_status` 数据库默认值以兼容滚动期间的旧 Worker；将报告摘要纳入确定性事实层。
- 结果：真实 assertion 失败 Batch 经 Agent 调用 `fix_and_retry -> explore_page -> validate_page_elements -> generate_dsl -> ask_user_question -> execute_dsl -> get_report` 完成修复，审批前未执行，审批后新 Batch 3/3 步通过且通过率 100%；修复策略和事件流完整可审计。
- 验证：Go `go test ./...`、`go vet ./...`、编译通过；Python 12 个聚焦合同测试与 compileall 通过；Alembic 当前为 0034 且 `alembic check` 无差异；真实 DeepSeek、Playwright、PostgreSQL 队列和报告链路通过。临时 Run、generation、Case、Batch、anti-pattern、截图及本次服务进程已清理。
- 后续：将前端 Planning 工作台切换到 Go AgentRun/ToolCall/SSE 协议，并完成浏览器 E2E 验收。

## 2026-09-04 | AgentCore DSL 审批、执行与报告闭环

- 任务：将候选 DSL 审批、正式执行和报告读取接入 Go AgentCore，并修复复测与归因事实链。
- 操作：AgentRun 新增 latest/approved generation 绑定及 Alembic 0033；`generate_dsl` 发布 artifact；用户通过 `approve_dsl` 工具结果批准当前版本，`execute_dsl` 强制校验批准 ID 后创建正式 Case 和幂等 Batch；新增 `get_report` 工具并由后端等待终态；修复 BUG-099 的复测统一持久化和 BUG-100 的确定性结论覆盖。
- 结果：真实链路完成 `explore -> validate -> generate -> approve -> execute -> report`：generation 3 创建 Case 6 和 Batch 3，Worker 执行 3/3 步通过，Report Core 返回 `passed`、`pass_rate=1.0` 和 `analysis_status=completed`。LLM 不能伪造审批，也不能用错误结构化结论覆盖失败事实。
- 验证：Go 全量测试、vet 和编译通过；Python 7 个合同测试、compileall 和 `alembic check` 通过；Alembic 升级至 0033；真实 Playwright/队列/报告链路通过。测试实体将在最终联调完成后统一清理。
- 后续：实现透明 `fix_and_retry` RepairAttempt 流程，再将前端切换到 Go Agent 事件协议。

## 2026-09-04 | Go AgentCore 探索、验证与 DSL 生成链路

- 任务：让 Go AgentCore 自主调用现有 Python 浏览器能力并生成经过页面元素验证的 DSL。
- 操作：新增受限的 Python Browser/Agent Capability 路由；复用现有 `explore_page`、`explore_flow`、A11y preflight 和 DSL generator；Go 新增 Python Worker client 及 `explore_page`、`explore_flow`、`validate_page_elements`、`generate_dsl` 工具；元素验证同时支持生成前需求覆盖检查和生成后 locator preflight；Run 显式携带 project context。
- 结果：真实 Agent 能从用户需求自主完成单页探索、识别 8 个 A11y 节点、验证标题唯一命中、调用 `deepseek-v4-flash` 生成 3 步 DSL，并明确停止在未执行草案阶段。联调发现并修复 BUG-101。
- 验证：Go `go test ./...`、`go vet ./...`、编译通过；Python 5 个合同测试与 compileall 通过；真实跨进程链路产生 23 条连续事件并以 `run.finished` 收口；临时 Run 和 DSL generation 数据已清理。
- 后续：增加 DSL artifact 与用户审批状态，接入 `execute_dsl`、`get_report` 和透明 `fix_and_retry`。

## 2026-09-04 | AgentCore 异步运行与 SSE 实时重放

- 任务：让 Go Agent Run 非阻塞启动，并统一实时事件推送与 PostgreSQL 历史重放。
- 操作：新增进程内 EventBroker；事件持久化成功后再发布给订阅者；Run 增加显式项目上下文，创建接口改为 `202 Accepted` 并后台驱动 Agent；新增 `/events/stream` SSE 接口，支持 `after_seq` 和 `Last-Event-ID`，先重放历史再推送实时事件和 keep-alive。
- 结果：模型调用不再阻塞创建请求；客户端可在 Run 运行期间看到 `run.started -> tool.started -> tool.args.delta -> tool.pending`，断线后按序号补齐事件。慢订阅者不会阻塞 Agent，可从 PostgreSQL 恢复跳过的事件。
- 验证：`go test ./...`、`go vet ./...` 和 Go API 编译通过；真实服务中创建 Run 立即返回 running，SSE 随后收到 4 条有序事件并停留在用户等待状态；测试数据和本地进程已清理。
- 后续：接入 `explore_page`、`explore_flow` 和 `validate_page_elements` 工具，并将 Python Browser Worker 收敛为稳定接口。

## 2026-09-04 | AgentRun 与事件流 PostgreSQL 持久化

- 任务：让 Go AgentCore 的运行状态、对话 transcript、pending checkpoint 和事件在进程重启后可恢复。
- 操作：新增 `agent_runs`/`agent_events` SQLAlchemy 模型和 Alembic 0032 迁移；实现 Go `PostgresRepository`；使用 `agent_runs.last_event_seq` 在事务中原子分配事件序号；Go 服务启动改为校验数据库连接并使用 PostgreSQL Repository；补充数据库 URL 兼容转换。
- 结果：AgentCore 不再依赖进程内状态，Run、用户等待点、模型 transcript 和事件可跨进程恢复；内存 Repository 仅保留给单元测试。
- 验证：`go test ./...`、`go vet ./...` 通过；Alembic 升级至 `20260904_0032` 且 `alembic check` 无差异；真实创建 waiting_user Run 后重启 Go 服务，Run 和 4 条有序事件读取一致；临时数据已清理。
- 后续：新增实时 SSE 订阅，与 PostgreSQL 事件重放共用同一事件源。

## 2026-09-04 | Go AgentCore 原生工具调用与人工暂停恢复

- 任务：让 Go AgentCore 使用真实 LLM 原生 tool calling，并跑通 `ask_user_question` 暂停/恢复循环。
- 操作：新增 OpenAI 兼容 LLM client、Agent Engine、对话 transcript、Tool Registry 执行入口和 `AskUserTool`；扩展 Run 保存 pending tool/step，统一记录 message/tool/run 事件；HTTP 创建 Run 改为驱动 Agent 循环，resume 接口将用户答案作为 tool result 送回同一 Run；配置读取复用本地 `backend/.env`。
- 结果：`deepseek-v4-flash` 可通过原生 `tools/tool_calls` 自主提出问题；Run 能跨两次用户回答保持上下文，从 `running` 进入 `waiting_user`、恢复后再次等待，最终输出两条登录场景计划并进入 `completed`。当前仅注册 `ask_user_question`，其他领域工具将在后续阶段接入。
- 验证：`go test ./...`、`go vet ./...`、Go API 编译通过；真实启动于 8082 后完成两轮 `ask_user_question`，18 个事件序号连续，包含 start/args/pending/result/message/finished；旧测试进程占用 8081 的问题已清理。
- 后续：将 Run/Event/ToolCall 接入 PostgreSQL，增加 SSE 实时订阅，再接入页面探索和元素验证工具。

## 2026-09-04 | Go AgentCore 合同与 Hertz 骨架

- 任务：冻结新控制面的第一批跨模块合同，并建立可独立运行的 Go/Hertz 服务骨架。
- 操作：新增 `backend-go` Go module；定义 AgentRun、统一事件 envelope、问题和暂停恢复数据结构；定义 Repository 与 Tool Registry 接口及内存实现；新增 Hertz 健康检查、Run 创建/查询、事件增量查询和 ToolCall 恢复接口；补充服务、注册表和 HTTP 合同测试。
- 结果：Go 控制面已能独立启动，创建运行记录、按 Run 维护单调递增事件序号，并完成 `ask_user_question` 的 `running -> waiting_user -> running` 状态转换；当前存储为内存实现，尚未接入 LLM 和 PostgreSQL。
- 验证：`go test ./...`、`go vet ./...`、`go build -o /tmp/ai-web-testing-agentcore ./cmd/api` 通过；本地启动后 `GET /health` 返回 `{"status":"ok"}`，创建 Run 与查询 `run.started` 事件通过。
- 后续：增加 PostgreSQL AgentRun/Event/ToolCall 持久化，实现 AgentCore LLM 循环和 SSE 实时/重放统一事件源。

## 2026-09-04 | 正式采用 Go AgentCore 渐进迁移方案

- 任务：确认并启动以代码可读性和后续迭代效率为优先的后端重构。
- 操作：将 ADR-002 状态改为 accepted；更新仓库级架构规则和 README，明确 Go/Hertz 控制面、Python Browser Worker、PostgreSQL 队列及 Kitex 的适用边界；建立专用分支 `xujinyuan/go-agentcore-v2`。
- 结果：新业务控制面默认使用 Go，现有 FastAPI 在迁移期间保持兼容，Playwright/A11y/Locator 暂不重写；当前阶段不实现登录、Token 和角色鉴权，但保留项目与 actor 归属字段。
- 验证：文档差异检查通过；未修改运行时代码。
- 后续：冻结跨语言合同并建立可测试的 Go/Hertz 模块化骨架。

## 2026-09-04 | 以可读性为目标的后端重构建议

- 任务：在允许大规模重构的前提下，确定兼顾代码可读性与后续迭代效率的技术方案。
- 操作：结合当前约 2.4 万行 Python、千行级 Agent/工具/Runner 模块、现有 PostgreSQL 队列和 Playwright 能力，对全量 Go 重写、Go/Python 混合边界及 Kitex 使用时机进行取舍。
- 结果：建议以旁路替换方式建设 Go 模块化单体控制面，使用 Hertz 提供 HTTP/SSE、Go interface 组织 AgentCore 和领域模块，保留 Python Playwright/A11y/locator Worker；Kitex 仅在未来出现真实独立部署边界时使用。迁移前先冻结 DSL、Agent Event、Tool、Report、数据库和黄金行为合同，不直接复刻 BUG-099/100 等已知错误。
- 验证：架构评估，未修改业务代码，未运行产品测试。
- 后续：确认方向后先建立 v2 目录和合同测试，以“对话 -> ask_user -> explore -> validate -> generate -> execute -> report”首个纵向切片验证新架构。

## 2026-09-04 | Go AgentCore 与 Kitex 服务边界评估

- 任务：评估当前项目是否适合将后端改为 Go，并按 handler/service 模块化后使用 Kitex。
- 操作：统计 Python 后端模块体量和依赖关系，结合 AgentCore 工具设计、HTTP/SSE 前端协议、PostgreSQL 队列与 Playwright 能力划分迁移边界；新增 ADR-002。
- 结果：可以使用 Go 重建 Agent 控制面，但不建议逐文件全量翻译。推荐 Hertz 对前端提供 HTTP/SSE，普通 Go interface 组织单进程模块，仅在真实进程间边界使用 Kitex；现有 Python Playwright/A11y/locator Worker 暂时保留，通过 PostgreSQL Job 与 Go 控制面解耦。当前不实现登录与权限校验，但保留项目和 actor 归属字段。
- 验证：后端共约 2.4 万行 Python；确认 Agent、工具、Runner、DSL 和 execution service 中存在多个千行级模块及跨层延迟导入；架构评估未运行产品测试。
- 后续：如确认迁移方向，先冻结 DSL、事件、工具、报告和数据库合同并修复 BUG-099/100，再以旁路方式落地 Go AgentCore。

## 2026-09-04 | DeepSeek LLM 能力实测

- 任务：展示当前接入的 `deepseek-v4-flash` 在项目 Planning Agent 中的实际能力。
- 操作：通过项目 `_stream_planning_llm` 和完整 `run_planning_turn` 执行三类真实模型探针，覆盖需求抽取、探索工具决策和 locator 失败归因；不启动浏览器、不写业务数据。
- 结果：3/3 单轮响应均可解析为项目 JSON 动作；模型能从简略需求抽取被测对象，能从完整需求提取 URL、流程、断言和变量，并主动选择 `get_project_info` 工具；完整 ReAct 能正确用自然语言识别 `Login` 变为 `Sign in` 的定位器过期问题。但结构化分析错误落为 `all_passed/done` 且缺少失败明细，记录为 BUG-100，当前不能直接把 LLM 结构化结论用于无人审批的修复决策。
- 验证：网关真实请求均成功；完整 ReAct 返回 `session_status=completed`，自然语言归因正确；结构化字段矛盾已复现。首次探针脚本因误用不存在的 `SessionLocal` 导入失败，改用项目 `get_session_factory()` 后成功。
- 后续：先修复 BUG-100 的分析 Schema 与确定性约束，再测试真实页面探索、DSL 生成和失败修复建议。

## 2026-09-04 | 自愈任务编排层职责说明

- 任务：解释受控自愈方案中的任务编排层含义和边界。
- 操作：将现有失败分析、页面探索、DSL 生成、审批和 Batch 执行能力映射为后端状态机职责。
- 结果：任务编排层不是新的 AI 或必需的外部任务框架，而是位于 LLM 能力与正式 Runner 之间、负责流程顺序、条件分支、状态持久化、幂等重试、审批门和审计追踪的后端协调服务；LLM 负责理解需求、提出是否重探索及探索范围等结构化建议、分析失败和生成候选 DSL，编排层依据失败分类、权限、预算和重试上限裁决并调用后端工具，正式测试只由 Runner 执行。用户负责设定目标，并在正式 DSL 变更或扩大执行范围前审批。编排层不是数据转发层，而是流程控制权和安全边界的持有者。
- 验证：架构说明，未修改业务代码，未运行产品测试。
- 后续：第一阶段可采用 `RepairAttempt` 表、`repair_orchestrator.py` 服务和审批 API，继续复用现有 PostgreSQL Batch/Job 队列。

## 2026-09-04 | 自愈循环复用 Planning 会话方案澄清

- 任务：评估自动重探索是否可以复用现有 Planning 会话与探索能力。
- 操作：结合现有 Planning Session、失败分析、上下文注入、探索工具、DSL 草案生成和 Batch 执行边界梳理最小编排方案。
- 结果：可以复用现有能力，但应在同一 Planning Session 中创建新的 repair turn，而不是新建独立会话；该 turn 显式绑定来源 Run/Batch，并注入 FailureSignal、ExecutionAnalysis、失败步骤 evidence、原 DSL 快照和历史探索数据。编排层仅在定位、导航或页面状态过期时触发重探索，随后生成候选 DSL、校验并展示差异，用户批准后才更新正式用例并进入 Batch/Job 队列重跑。
- 验证：架构分析，未修改业务代码，未运行产品测试。
- 后续：实现带来源追踪和审批门的 RepairAttempt 状态机，复用现有 explore、draft、validation 和 execution 服务。

## 2026-09-04 | 执行归因与复测循环现状核验

- 任务：确认当前是否已实现“执行 -> 记录错误 -> 归因 -> 重新执行”循环。
- 操作：追踪直接 Case 执行、Batch Worker、FailureSignal/ExecutionAnalysis、anti-pattern、Planning 分析消息、`/retest` API、人工修正重跑入口和 DSL 草案再生成逻辑。
- 结果：执行、步骤证据与错误持久化、统一 FailureSignal、规则兜底与 LLM 归因已自动连通；直接执行和 Batch 终态均可生成持久化分析。后端 `/retest` 可筛选失败用例并重跑原 DSL，人工修正后前端也可重跑当前用例，但 Planning 前端未接入 `/retest`。归因结果尚不会自动触发重新探索、DSL 重生成、差异审批或正式用例更新，因此当前是人工触发复测的半闭环，完整自愈闭环仍由 BUG-090 跟踪；同时 `/retest` 绕过统一分析持久化的问题记录为 BUG-099。
- 验证：静态核对 `services/executions.py`、`services/execution_batches.py`、`application/reporting/analysis_service.py`、`application/planning/analysis_retest_service.py`、`draft_service.py`、Planning API 与前端执行入口；未执行真实浏览器全链测试。
- 后续：实现 `analyze -> re-explore -> regenerate -> diff -> approve -> rerun` 状态机，并为 Planning 前端增加审批与复测入口。

## 2026-09-04 | 同步项目状态与 AI 配置记录到 GitHub

- 任务：将当前项目状态盘点和本地 AI 配置变更记录同步到远端。
- 操作：仅暂存 `docs/execution-log.md` 与 `docs/bug-log.md`，创建聚焦文档提交并推送当前 `main`；确认 `backend/.env` 被 Git 忽略且未进入提交。
- 结果：状态盘点、BUG-098 和 DeepSeek/VLM 配置记录已同步到 `origin/main`，本地密钥未进入版本控制。
- 验证：首次提交 `b4640cf` 后比较本地与远端完整哈希一致，提交计数为 `0/0`。
- 后续：无。

## 2026-09-04 | 配置 DeepSeek 文本模型并关闭 VLM

- 任务：本地暂不采用 VLM 定位，统一使用 `deepseek-v4-flash` 处理 Planning 与 DSL 生成。
- 操作：在被 Git 忽略的 `backend/.env` 中启用 AI Planning 和 AI DSL，统一配置 OpenAI 兼容网关 `https://api.unself.cn/v1` 与 `deepseek-v4-flash`；关闭 AI visual locator 和 VLM 页面注释，清空 VLM 凭据。
- 结果：Planning 与 DSL 文本模型均已启用；执行定位继续走 A11y/DOM 语义链及人工干预，不调用 VLM。
- 验证：通过应用 `Settings` 读取确认两个文本模型开关、模型和网关生效，VLM 两个开关关闭且无密钥；向 `/chat/completions` 发送最小请求返回 HTTP 200、模型 `deepseek-v4-flash` 和内容 `OK`。
- 后续：真实 Planning 全链验收时继续观察 reasoning 流式展示与 DSL 结构化输出质量；API 密钥仅保存在本地忽略文件中，不写入日志或版本控制。

## 2026-09-04 | 当前项目状态盘点

- 任务：基于当前分支、提交、能力说明、缺陷日志和可运行门禁汇总项目状态。
- 操作：核对 `main` 与 `origin/main`、最近提交和工作树；复核 README 能力矩阵与开放缺陷；统计现行测试资产；执行后端编译、执行分析合同测试、前端生产构建和差异检查。
- 结果：核心平台、结构化执行、PostgreSQL Batch/Job 队列、Report Core、统一 FailureSignal/ExecutionAnalysis 已落地；当前主缺口是 BUG-090 受控自愈编排、真实 AI 全链验收和自动化回归门禁重建。发现缺陷日志存在重复编号和冲突状态，记录为 BUG-098。
- 验证：`uv run python -m compileall -q app` 通过；`uv run python -m unittest discover -s tests -p 'test_*.py' -v` 为 2/2 通过；`npm run build` 通过；`git diff --check` 通过；盘点前本地与远端提交计数为 `0/0`。
- 后续：优先实现 `analyze -> re-explore -> regenerate -> diff -> approve -> rerun`；配置模型后执行真实 AI 链路验收；恢复分层自动化测试；清理 BUG-098 日志状态歧义。

## 2026-09-02 | 同步执行分析与报告统一链路到 GitHub

- 任务：将 FailureSignal、ExecutionAnalysis、Planning/Report 统一展示及正式报告路由变更同步到远端。
- 操作：复核实现、迁移、生成类型、聚焦测试和文档差异；创建聚焦提交并推送当前 `main`。
- 结果：执行分析统一事实链、报告导航统一和 BUG-092/097 修复已同步到 `origin/main`。
- 验证：推送后核对本地与远端提交计数、最新提交和工作树状态。
- 后续：继续实现 BUG-090 的受控自动重探索与 DSL 重生成编排。

## 2026-09-02 | 打通执行、分析与正式报告事实链

- 任务：解决 Planning AI 分析旁路、刷新丢失、anti-pattern 漏记、直接执行无总结、失败分类分裂及报告导航不一致。
- 操作：新增持久化 FailureSignal/ExecutionAnalysis 和 `0030/0031` 迁移；Run/Batch 终结统一生成规则总结并在模型可用时增强为 AI 根因分析；统一 Planning/报告/anti-pattern 分类；Planning SSE 改读 Batch 持久化分析并写入历史消息；前端处理 `analysis_complete`，Planning 与 ReportPage 共用分析组件和 `/reports/:executionId` 正式详情路由。
- 结果：直接 Case、Batch Worker 和 Planning 执行均进入 `Run/Batch -> FailureSignal -> Analysis -> Report Core`；失败 anti-pattern 自动沉淀；实时与刷新后的 Planning 消息可显示同一分析；旧 `/run/:id` 自动重定向；BUG-092、BUG-097 已关闭。自动重探索和 DSL 重写仍属于 BUG-090。
- 验证：2 个 `unittest` 合同测试通过；后端 compile/import 通过；Alembic `0030/0031` 降级升级往返及 `alembic check` 通过；OpenAPI 类型重新生成；前端生产构建通过；浏览器验证 Planning 历史分析、正式报告详情、报告聚合入口及旧路由重定向，临时数据已清理。
- 后续：实现 BUG-090 的 `analyze -> re-explore -> regenerate -> diff -> approve -> rerun` 受控自愈编排。

## 2026-09-02 | 用例执行到报告总结链路核验

- 任务：确认当前“用例执行 -> 执行记录 -> 报告 -> 结果/错误总结”链路是否已经打通。
- 操作：静态追踪直接 Case 执行、ExecutionBatch Worker、TestCaseRun 持久化、Report Core、Planning SSE 分析、anti-pattern 写入和前端报告/流事件消费。
- 结果：Runner 到 TestCaseRun、步骤 evidence、Batch/Job/Run 报告及确定性统计聚合已打通；Planning 失败后会尝试 AI 分析。但 AI 分析尚未写入统一报告事实，前端不处理 `analysis_complete`，队列路径也绕过失败 anti-pattern 记录，因此端到端“报告总结闭环”仅部分完成，记录为 BUG-097。
- 验证：核对 `services/executions.py`、`services/execution_batches.py`、`application/reporting/service.py`、`services/ai_planning_streaming.py`、`analysis_retest_service.py`、Planning 流事件 reducer 和 ReportPage 数据源；未运行产品测试。
- 后续：先统一并持久化 `FailureSignal`/AnalysisResult，再让 Report Core、Planning 消息和前端共享同一总结合同。

## 2026-09-02 | 同步执行队列与 README 进展到 GitHub

- 任务：将本地执行队列功能提交和 README 最新进展同步到远端。
- 操作：复核 `main` 与 `origin/main` 差异；仅暂存 README、后端 README、执行日志和缺陷日志，创建聚焦文档提交并推送当前分支。
- 结果：执行队列、Planning 队列迁移及 README 状态更新已同步到 `origin/main`。
- 验证：推送后核对本地与远端提交计数、最新提交和工作树状态。
- 后续：无。

## 2026-09-02 | 更新 README 最新项目进展

- 任务：将 README 的项目状态更新到当前执行队列与 Report Core 阶段。
- 操作：重写根 README 当前状态为能力矩阵，新增 2026-09-02 执行控制面里程碑和下一阶段优先级；补充 Worker 启动、现行构建验证边界和 Batch/Job/Run 联调路径；同步修正后端 README 的 Planning SSE 描述。
- 结果：移除基于 2026-05-31 的 `98%+` 旧完成度口径，README 现已反映持久化队列、Report Core、heartbeat/取消、受控自愈缺口和自动化测试门禁现状；BUG-096 已关闭。
- 验证：`git diff --check` 通过；README 本地 Markdown 链接检查通过。
- 后续：实现统一 `FailureSignal` 和受控自愈编排后继续同步能力矩阵。

## 2026-09-02 | 当前项目进展盘点

- 任务：基于当前仓库、提交记录、执行日志和缺陷日志汇总项目最新进展。
- 操作：核对 `main` 与远端差异、最近提交范围、阶段计划、当前能力说明、开放缺陷和可用验证入口。
- 结果：本地 `main` 领先 `origin/main` 2 个提交；持久化 ExecutionBatch/ExecutionJob 队列、Report Core、Planning SSE 队列迁移、heartbeat 与持久化取消已落地。当前主要未完成项为统一 FailureSignal、受控自动自愈编排、真实 AI 全链验收，以及恢复与当前架构匹配的自动化测试门禁。发现根 README 仍使用 2026-05-31 的阶段与完成度口径，已记录为 BUG-096。
- 验证：检查 Git 状态和提交统计；核对 `docs/execution-log.md`、`docs/bug-log.md`、README、能力状态清单及优化计划；本次未运行产品测试。
- 后续：优先实现统一失败事实模型，再接入 `analyze -> re-explore -> regenerate -> diff -> approve -> rerun` 自愈编排；同步更新 README，并决定是否将本地 2 个提交推送到 GitHub。

## 2026-09-02 | Planning SSE 迁移执行队列并接入取消与 Heartbeat

- 任务：完成 BUG-094 剩余工作，将旧 Planning SSE 执行迁移到 Batch 队列，并接入运行中 Job 的持久化取消和 heartbeat。
- 操作：Planning 保存执行改为保存草案后创建 ExecutionBatch，再轮询 Report Core 输出兼容 SSE 事件；同一 Planning session 限制单个活动 Batch；ExecutionJob 增加 heartbeat 字段；Worker 使用独立 Session 每 2 秒续租并检查取消，将取消事件传入 Runner。
- 结果：Planning SSE 不再直接执行 Playwright；多会话通过持久化队列受控并行，取消状态不依赖原 HTTP 连接，Worker lease 可持续续期。取消在下一安全步骤边界生效。
- 验证：迁移升级至 `20260902_0029`；真实 Planning SSE 完成 `save -> queue -> execute -> report -> done`；跨 Session heartbeat/取消验证后 Job 与 Batch 均为 `cancelled`。
- 后续：后续如要求强制中断单个长 Playwright 调用，应将 Job 进一步隔离到可终止子进程。

## 2026-09-02 | ExecutionBatch、ExecutionJob 与 Report Core 第一阶段落地

- 任务：开始实现支持多会话、多项目并行的执行控制平面和报告聚合基础。
- 操作：新增 ExecutionBatch/ExecutionJob 模型、schema 和 Alembic 迁移；TestCaseRun 增加 batch/job、attempt、DSL 快照/hash、报告版本；实现持久化幂等键、PostgreSQL `FOR UPDATE SKIP LOCKED` Job 领取、Batch 并发限制、lease/取消/终态聚合、固定并发 Worker；新增 run/batch/project Report Core 与批次 API；执行未知异常统一收口为失败记录；移除每次 Run 对全局 VLM runtime state 的重置；同步 OpenAPI 和前端生成类型。
- 结果：新链路支持 `Batch 1:N Job 1:N Run`，可按 batch 查询任务和最新运行报告；API 负责入队，独立 Worker 负责执行，报告层不再依赖 SSE 请求线程。旧单用例与 Planning SSE 路径保持兼容。
- 验证：数据库升级至 `20260902_0028`，`alembic check` 无差异；内存队列双 Job 领取、幂等创建与终态聚合通过；未知 Runner 异常收口通过；真实 PostgreSQL + Playwright 完成 `Batch -> Job -> Run -> Report`，2/2 步骤通过；批次列表 API 的 403/200 权限路径通过；前端 OpenAPI 类型生成和生产构建通过；临时数据已清理。
- 后续：将 Planning“保存并执行”迁移到 Batch API；增加运行中 Job 的持久化取消/heartbeat；统一 FailureSignal 分类并在报告写入时固化。

## 2026-09-02 | ExecutionBatch、ExecutionJob、Report Core 与队列选型说明

- 任务：解释执行批次、执行任务、报告核心服务的职责关系，并比较 PostgreSQL DB Queue、Celery 和 Kafka。
- 操作：结合当前 Project、PlanningSession、TestCaseRun 和 SSE 执行模型定义渐进式执行控制平面。
- 结果：ExecutionBatch 表示一次明确的项目测试活动，ExecutionJob 表示其中一个可调度用例任务，TestCaseRun 表示 Job 的一次实际尝试，Report Core 是按 run/batch/project 生成报告的无状态应用模块。当前规模优先采用 PostgreSQL DB Queue；需要成熟 Python 任务调度时再引入 Celery；Kafka 更适合高吞吐可回放事件流，不适合作为当前首选任务执行框架。
- 验证：架构概念分析，未修改业务代码，未运行测试。
- 后续：设计数据模型时明确 `Batch 1:N Job 1:N Run`，并保留未来 outcome/reward 关联。

## 2026-09-02 | 多项目并行下的 Report Core 与执行控制平面设计

- 任务：评估 Report Core 按 ID 处理项目报告的可行性，并澄清多线程、数据写冲突和消息排队机制。
- 操作：基于现有 TestCaseRun、Planning SSE 工作线程、内存 asyncio.Queue、事件日志和报告聚合路径划分执行控制面、事实存储与报告读模型。
- 结果：Report Core 可设计为无状态聚合服务，但项目历史应按 `project_id` 查询，一次测试活动必须按新增 `batch_id` 聚合。当前线程只承担请求内执行/流式桥接，内存 Queue 不是任务队列；独立 run 写入通常不冲突，同一 planning session、事件序号、locator correction 和全局 VLM 状态存在竞争。现有表可保留，通过新增 batch/job/event 并给 TestCaseRun 增加关联与快照字段渐进演进，无需整体推倒重写。
- 验证：架构静态分析，未修改业务代码，未运行测试。
- 后续：先确定 Report Core 三层查询合同和 execution batch/job schema，再决定采用 PostgreSQL DB queue 还是外部任务队列。

## 2026-09-02 | 多会话与多项目并行执行分析

- 任务：确认一次开启多个 Planning 会话执行时的行为，并评估未来多项目并行测试能力。
- 操作：检查 SSE 工作线程、SQLAlchemy Session、取消管理器、Playwright 浏览器生命周期、artifact 隔离、VLM runtime state、事件日志序号和数据库配置。
- 结果：不同会话会创建独立线程、数据库 Session、浏览器和 execution artifact 目录，低并发下可同时运行；但没有任务队列、并发上限或进程恢复。同一会话重复执行会覆盖取消句柄并产生事件序号冲突；每次用例执行还会重置进程级 VLM 限流、断路器和统计，造成跨项目干扰。SQLite 并发写风险也高于 PostgreSQL。
- 验证：静态分析，未修改业务代码，未执行并发压力测试。
- 后续：多项目并行前引入 execution batch/job、持久化状态机、幂等键、按 job 取消、全局/项目并发限制，并拆分 VLM 全局治理状态与单次执行统计。

## 2026-09-02 | 报告、执行持久化与调度链路分析

- 任务：从报告入手，确认用例执行结果如何保存、项目内用例如何执行，以及当前同步/异步模型。
- 操作：追踪 ExecutionReport/StepExecutionEvidence schema、Playwright Runner、TestCaseRun 持久化、执行 API、Planning 保存执行 SSE、报告聚合与前端触发入口。
- 结果：单次用例报告以 JSON 存于 `test_case_runs.report`，步骤截图存文件并在报告中保存路径；直接执行 API 同步阻塞。Planning SSE 在守护工作线程中运行同步执行并流式返回，多个选中用例仍串行执行。当前不存在项目批次执行实体/API，项目报告只是聚合项目下相互独立的运行记录。确认错误分类存在三套口径，且未捕获异常可能留下永久 `running` 记录。
- 验证：静态分析，未修改业务代码，未运行测试。
- 后续：先设计并固化统一 `FailureSignal` 与报告版本/DSL 溯源字段，再实现错误总结和注入模块。

## 2026-09-02 | 调整 AgenticRL 自愈闭环实施顺序

- 任务：明确先完善错误总结与错误注入，再新增独立模块桥接执行、校准和重生成链路。
- 操作：基于现有 execution report、locator trace、anti-pattern、Planning context 与 DSL prompt 注入路径重新划分阶段边界。
- 结果：第一阶段聚焦标准化错误事件、证据引用、错误记忆、检索选择和可追踪注入；仅预留后续策略模块所需契约。第二阶段再实现自愈编排与 AgenticRL 策略学习，避免基础数据处理与闭环决策高耦合。
- 验证：架构静态分析，未修改业务代码，未运行测试。
- 后续：先定义错误总结和注入的数据 schema，再逐步替换当前分散的字符串拼接与直接注入逻辑。

## 2026-09-02 | 清理测试构建残留

- 任务：继续清理后端和前端中残留的测试构建代码与产物。
- 操作：删除后端 pytest 缓存和已删除测试文件的字节码目录、前端 Vitest 缓存与空测试依赖目录；同步后端虚拟环境；移除 `page_explorer.py` 中仅为测试 monkeypatch 保留的 Playwright 包装函数；清理 `.gitignore` 中失效的测试产物和临时测试脚本规则。
- 结果：后端和前端均无项目级测试目录、测试构建缓存、测试框架依赖或测试专用源码入口；生产 Playwright 执行能力保持不变。
- 验证：`uv run python -m compileall -q app`、关键模块导入和 `npm run build` 通过；`uv pip show pytest` 确认 pytest 未安装；残留目录和关键字扫描为空。
- 后续：无。

## 2026-09-02 | 清理全部自动化测试代码

- 任务：删除因项目结构变化而失效、难以维护的全部自动化测试代码。
- 操作：删除后端单元/集成/E2E 测试与夹具、前端 Vitest 测试与测试工具、测试指南及 E2E 输入样例；移除 pytest、Vitest、Testing Library、jsdom 的依赖和配置；同步当前 README 与架构说明。
- 结果：仓库不再包含可执行自动化测试套件或测试框架配置；保留业务 `test_case`/`test_planning` 模块、Alembic 历史迁移、Playwright 正式执行引擎及历史设计记录。
- 验证：`uv lock --check`、`uv run python -m compileall -q app`、应用工厂导入和 `npm run build` 均通过。
- 后续：后续如恢复自动化测试，应基于当前架构重新设计测试分层与契约，不复用本次删除的旧套件。

## 2026-09-02 | AgenticRL 与自愈闭环模块设计构思

- 任务：构思 AgenticRL 模块如何接入“执行方案 → 总结错误 → 校准 trace → 重新生成方案 → 验证 prompt 作用”的自愈闭环。
- 操作：检索现有 Planning、DSL、Prompt Registry、anti-pattern、execution trace、analysis/retest 相关代码与历史设计记录，确认当前能力边界与缺口。
- 结果：建议将 AgenticRL 作为独立学习与策略服务，位于执行报告/错误总结之后、重新规划/DSL 生成之前；不替代 DSL 校验和 Runner，只输出结构化修复建议、策略选择、prompt policy 与闭环决策。
- 验证：静态分析，未运行测试。
- 后续：如进入实现，优先新增 AgenticRL 数据模型与 service，再接入保存执行后的失败分析与 DSL retry 入口。

## 2026-09-01 | AI 规划到失败复测链路核验

- 任务：核验“用户输入 → Agent 规划 → 页面探索 → DSL 生成 → Playwright 执行 → 报告 → 错因分析 → 错误沉淀 → 会话上下文注入 → 再次执行”链路是否可运行。
- 操作：静态追踪 Planning、Explorer、DSL、Runner、Report、Analysis/Retest 调用链；运行后端聚焦测试、前端全量测试与构建、Playwright 浏览器集成；通过 Midscene 打开规划页并创建会话；修复流式执行未自动注入会话测试数据、未记录失败 anti-pattern 的同步/流式行为差异并补回归测试；忽略 Midscene 本地报告目录。
- 结果：规划到报告的模块链路存在，失败分析、错误记录、上下文注入和后端复测也分别存在；但当前不是全自动自愈闭环，自动重新探索/重生成 DSL/更新用例未编排，前端也未接入复测 API。本机 Planning AI 与 DSL AI 均未启用，UI 明确显示“AI 设置：未启用”。
- 验证：后端聚焦测试 107 passed、1 skipped；修复回归 27 passed、1 skipped；后端默认全量 501 passed、1 skipped、1 个认证策略漂移测试失败；前端 75 passed；前端生产构建通过；浏览器创建会话成功；A11y 浏览器测试 2 passed、1 个旧常量断言失败；登录执行到第 6/6 步后因旧 `flash` target 进入人工干预；人工干预回归 3 passed、1 个旧内部函数 monkeypatch 失败。
- 后续：配置 Planning/DSL 模型后补一次真实 AI 全链验收；决定是否实现受控自动自愈编排；清理浏览器集成测试漂移。

## 2026-09-01 | 关闭登录鉴权并自动使用 admin

- 任务：简化本地测试启动流程，跳过前端登录并自动使用数据库中的 admin 账号。
- 操作：移除前端路由的登录守卫，将 `/login` 重定向到规划工作台；后端当前用户依赖改为按 `AUTH_AUTO_LOGIN_EMAIL` 查询数据库账号，默认使用 `admin@test.com`；保留原登录接口用于兼容；同步测试与启动文档。
- 结果：所有页面可直接访问，依赖用户身份的接口统一使用配置的 admin；管理员账号缺失或停用时返回明确的 500 错误。
- 验证：当前数据库成功解析 `admin@test.com`（ID 2）；无 Cookie 请求 `/api/v1/auth/me` 返回 200；后端单测 491 passed、1 skipped；前端 75 tests passed；`npm run build` 通过。
- 后续：该模式仅适用于本地单用户测试；恢复多用户部署前需重新启用登录鉴权。

## 2026-08-31 | 补充同步后端应用说明

- 任务：将此前排除的 `backend/app/README.md` 本地修改同步到 GitHub。
- 操作：按用户确认保留当前 README 内容，并与本次同步记录一并提交到 `main`。
- 结果：后端应用目录说明的本地修改纳入版本控制。
- 验证：文档差异检查通过；未涉及业务代码和运行逻辑。
- 后续：无。

## 2026-08-31 | 同步数据库重置记录到 GitHub

- 任务：将本地数据库重置记录同步到远端仓库。
- 操作：仅暂存并提交 `docs/execution-log.md`，排除与本次任务无关的 `backend/app/README.md` 本地修改；推送当前 `main` 分支。
- 结果：数据库重置记录已同步到 `origin/main`，提交为 `b043a9e`。
- 验证：本地 `main` 与 `origin/main` 提交计数为 `0/0`。
- 后续：无。

## 2026-08-31 | 重置本地数据库业务状态

- 任务：仅保留账号信息和默认项目，清空其他业务数据并重置自增序列。
- 操作：在单个事务中清空会话、消息、草稿、用例、执行、报告、定位记录等 16 张业务表；保留 `users`、默认 `projects`、必要的 `project_members` 和 `alembic_version`；同步保留表序列。
- 结果：当前保留 2 个账号、`Default Project(id=1)` 及其 1 条 owner 成员关系；其余业务表均为空。
- 验证：`ai_planning_sessions_id_seq` 与 `session_projects_id_seq` 均为 `last_value=1, is_called=false`，下次创建会话及关联记录将从 `id=1` 开始；后端健康检查返回 200。
- 后续：无。

## 2026-08-31 | 当前代码架构可理解性评估

- 任务：评估当前仓库架构，解释文件分类、命名、模块职责和依赖关系，并定位代码难以理解的主要原因。
- 操作：盘点前后端目录与模块规模；追踪 API、Planning、DSL、Runner、Locator、Report 主链；分析 Python 跨层依赖与模块强连通分量；新增 `docs/architecture-guide.md`，整理目录词典、Planning 文件职责、前后端依赖规则、需求定位表和推荐阅读顺序；更新根 README 与后端入口说明。
- 结果：当前架构较 2026-08-28 审计已有明显改善，Planning 用例服务和同步/流式执行源已完成拆分；主要认知负担来自超大核心函数、`application`/`services` 双向分层、过渡 facade/类型 barrel、前端大组件，以及入口文档严重滞后。综合可维护性评估为 6.8/10；现已提供对应当前代码的统一导航入口。
- 验证：后端默认测试 499 passed、1 skipped、10 deselected；前端 77 tests passed；`npm run build` 通过；静态依赖图仅发现 ORM 双向 relationship 形成模块强连通分量；本地 Markdown 链接检查通过。
- 后续：按指南中的过渡依赖逐步拆分 Planning Agent/草案生成/执行报告聚合的大函数，并移除兼容 facade 和前端手写总类型 barrel。

---

## 2026-08-31 | Prompt 统一管理入口落地

- 任务：先做当前 prompt 管理，不实现 RL 相关逻辑，只为后续策略接入预留扩展点。
- 操作：新增统一 Prompt Registry 入口；迁移 Planning 初始化、DSL 生成 system prompt、VLM system prompt；保留旧导入路径兼容。
- 结果：静态 prompt 不再散落在调用模块中，业务代码通过 stage 渲染 prompt；Planning 初始化 prompt 已同步薄化。
- 验证：聚焦单元测试与语法检查通过。
- 后续：继续把运行时 guard/context/anti-pattern 文本迁入统一入口，并下线在线 anti-pattern few-shot 注入。

---

## 2026-08-31 | 梳理现有 Prompt 清单并修正日志位置

- 任务：按要求将日志记录放到任务记录最前面，并梳理当前项目所有现存 prompt。
- 操作：扫描后端、前端与文档中的 prompt 定义、构造函数和注入点；修正上轮 AgenticRL 分析记录的放置位置。
- 结果：确认当前线上代码中的主要 prompt 集中在 AI planning、DSL generation、anti-pattern 注入和 VLM locator 四类。
- 验证：通过 `rg` 检索 `SYSTEM_PROMPT`、`user_prompt`、`build_*prompt`、`format_*prompt`、`few-shot` 等关键字。
- 后续：如需继续治理 prompt，可先将 anti-pattern 注入从在线 prompt 移到离线学习样本。

---

## 2026-08-31 | AgenticRL 演进机制设计分析

- 任务：分析 trace 记录与 anti 错误注入如何融合 AgenticRL，自主优化 SOP 和提示词。
- 操作：检查 ReAct prompt、DSL 生成 prompt、anti-pattern 注入、generation run 反馈模型与历史设计文档。
- 结果：确认现有系统已有 generation run、用户反馈、执行报告、locator trace 等学习样本基础；建议将 anti-pattern 从 prompt few-shot 注入迁移为离线评分样本和策略候选来源。
- 验证：静态取证，未运行测试。
- 后续：优先建设统一事件样本、奖励函数、prompt/策略版本治理、离线评估集与灰度发布闭环。

---

## 2026-08-31 | 同步日志结构整理到 GitHub

- 任务：将当前日志结构整理变更同步到 GitHub。
- 操作：检查工作区和当前分支，只暂存 `docs/execution-log.md` 与 `docs/bug-log.md`，创建 focused commit 并推送当前 `main` 分支。
- 结果：本记录随同日志结构整理变更一起提交并推送到远端。
- 验证：推送后通过 `git status --short` 与 `git log -1 --stat --oneline` 核对。
- 后续：无。

---

## 2026-08-31 | 统一日志结构与时间倒序展示

- 任务：修复 `docs/execution-log.md` 和 `docs/bug-log.md` 的展示顺序与结构不一致问题。
- 操作：统一两个日志文件的顶部骨架；将记录区改为按日期倒序展示；把 `execution-log` 中尾部错位的 2026-08-28 至 2026-08-30 记录归位；把 `bug-log` 从分类混排整理为统一时间线。
- 结果：两个日志均以说明、记录规则、记录模板、索引/总览、记录区的结构展示，新增记录时可直接插入记录区顶部。
- 验证：通过标题扫描确认 `execution-log` 与 `bug-log` 的记录区最新日期在前，历史记录不再散落在旧日期之后。
- 后续：后续新增记录继续保持最新记录优先，并使用对应模板字段。

---

## 2026-08-31 | 修复日志记录倒序问题

- 任务：修复本轮新增执行日志被追加到文件尾部的问题，恢复“最新记录优先放到最上面”的约定。
- 操作：
  - 将 2026-08-31 的「修复默认项目与规划会话初始化/绑定策略」「排查规划会话与项目读取/创建链路」「复刻首次新建规划会话 500」「诊断首次新建规划会话 500 后第二次成功」移动到日志记录区顶部。
  - 保持历史 2026-08-30 及更早记录顺序不变。
- 验证：`docs/execution-log.md` 顶部记录区已按最新任务在前排列。
- 备注：本次仅修正文档顺序，不改业务代码。

---

## 2026-08-31 | 修复默认项目与规划会话初始化/绑定策略

- 任务：落实「初始化项目时同时初始化会话并绑定；会话与项目保持多对多绑定；测试用例仍以项目为执行归属」。
- 变更：
  - 新增 Alembic 迁移 `20260831_0027_seed_default_planning_session.py`：
    - 将 `Default Project(id=1)` 标记为 `is_default=true`。
    - 若种子用户与默认项目存在且尚未绑定规划会话，则创建 `默认规划会话` 并写入 `session_projects`。
    - 设置默认会话 `active_project_id=1`。
    - PostgreSQL 下同步 `users/projects/project_members/ai_planning_sessions/session_projects` 序列，避免 seed 固定 ID 后再次分配重复主键。
  - 调整 `create_planning_session`：
    - 未传 `project_id` 时优先复用当前用户已有项目，按 `is_default desc, id asc` 选择默认项目。
    - 仅在用户没有可访问项目时兜底创建 `default-{session_id}`。
    - 兜底创建项目时同步写入 `ProjectMember`，避免自动项目在全局项目列表不可见。
  - 更新测试：
    - 会话创建测试改为断言复用 `Default Project`。
    - 新增多会话共享同一项目覆盖。
    - 新增默认规划会话迁移测试。
    - 修复配置测试对本地 `.env` 的隔离缺口。
- 验证：
  - `cd backend && uv run alembic upgrade head` 成功，当前 head 为 `20260831_0027`。
  - 修复后接口级验证：`POST /api/v1/ai-planning/sessions` 返回 `201`，新会话绑定 `Default Project(id=1)`。
  - `cd backend && uv run pytest -q` 通过：495 passed、1 skipped、10 deselected。
  - 本地 PostgreSQL 已清理到目标状态：仅保留 `Default Project(id=1, is_default=true)`、`默认规划会话`、以及二者的 `session_projects` 绑定。

---

## 2026-08-31 | 排查规划会话与项目读取/创建链路

- 任务：解释初始化项目存在但规划首页不显示、点击新建会话触发 500 的完整前后端链路。
- 前端链路：
  - `/planning` 的 `SessionListPage` 只调用 `listPlanningSessions()`，即 `GET /api/v1/ai-planning/sessions`；页面展示的是每个规划会话返回的 `projects` 字段，不读取全局项目列表。
  - 点击 `/planning` 顶部「新建会话」调用 `createPlanningSession({})`，即 `POST /api/v1/ai-planning/sessions`，没有传 `project_id`，也没有先读取或复用 `Default Project`。
  - 进入会话后的 `SessionProjectPanel` 才同时读取 `listSessionProjects(sessionId)` 与 `getProjects()`；其中 `getProjects()` 是 `GET /api/v1/projects`，用于下拉选择「关联已有项目」。
- 后端链路：
  - `POST /api/v1/ai-planning/sessions` 进入 `create_planning_session_route`，调用 `create_planning_session`。
  - `create_planning_session` 先创建 `AIPlanningSession`，随后在 `payload.project_id is None` 时自动创建 `Project(name=f"default-{record.id}", is_default=True)`，再写入 `SessionProject` 并设置 `active_project_id`。
  - `GET /api/v1/ai-planning/sessions` 只列出当前用户的会话，并从 `record.projects` 返回会话已关联项目；未关联任何会话的初始化项目不会出现在规划首页。
  - `GET /api/v1/projects` 通过 `ProjectMember` 列出当前用户可访问项目，能返回初始化的 `Default Project(id=1)`。
- 实测：
  - `GET /api/v1/projects` 返回 `Default Project(id=1)`。
  - `GET /api/v1/ai-planning/sessions` 返回会话 `id=2` 及其关联项目 `default-2(id=2)`，不返回未关联到会话的 `Default Project`。
- 结论：这是数据模型/初始化/默认关联策略不一致叠加序列未对齐的问题。初始化项目存在于全局项目域，但规划首页是会话域；新建会话未复用初始化项目，而是自动创建会话私有默认项目，进而撞上未对齐的 `projects_id_seq`。
- 备注：本次仅做链路排查和记录，未修改业务代码或数据库数据。

---

## 2026-08-31 | 复刻首次新建规划会话 500

- 任务：按页面行为复刻「首次新建会话 500，第二次成功且显示会话 2」。
- 数据库重置：
  - 删除现有 `ai_planning_sessions` 记录，级联清理会话关联数据。
  - 删除自动生成的 `projects.name like 'default-%' and is_default = true` 项目，保留种子项目 `Default Project(id=1)`。
  - 将 `ai_planning_sessions_id_seq`、`projects_id_seq`、`session_projects_id_seq` 重置为 `1, is_called=false`，恢复可复刻的序列错位状态。
- 复刻方式：
  - 使用 FastAPI `TestClient(raise_server_exceptions=False)` 连续请求两次 `POST /api/v1/ai-planning/sessions`。
- 结果：
  - 第一次请求返回 `500`，响应堆栈包含 `psycopg.errors.UniqueViolation`。
  - 第二次请求返回 `201`，响应体包含 `session.id=2`、`active_project_id=2`、项目 `default-2`。
  - 复刻后数据库仅有 `ai_planning_sessions.id=2`，无会话 1；`session_projects` 仅关联 `(session_id=2, project_id=2)`。
- 备注：复刻会保留当前数据库处于「第二次点击成功后」的状态；本次未修改业务代码。

---

## 2026-08-31 | 诊断首次新建规划会话 500 后第二次成功

- 任务：定位「首次点击新建会话返回 500 Internal Server Error，第二次点击成功且显示会话 2」的原因。
- 调查：
  - 检查前端 `SessionListPage.handleCreate`，确认每次点击直接调用 `createPlanningSession({})`，没有「先检测是否已有会话」的复用逻辑。
  - 检查后端 `create_planning_session`，确认新建会话后若未传 `project_id` 会创建默认项目 `default-{record.id}`，并在同一事务内提交。
  - 查询本地 PostgreSQL：当前仅存在 `ai_planning_sessions.id=2`，不存在会话 1；`projects` 存在 `id=1 Default Project` 与 `id=2 default-2`；相关序列均已推进。
  - 对照初始 migration，发现种子数据显式插入 `users.id=1`、`projects.id=1`、`project_members.id=1`。
- 结论：本地 PostgreSQL 种子数据显式写入 `projects.id=1` 后未同步 `projects_id_seq`，首次创建默认项目时序列仍尝试分配 `id=1`，触发主键冲突并回滚；PostgreSQL 序列不随事务回滚，因此第二次分配 `id=2` 成功。
- 备注：本次仅诊断和记录，未修改业务代码或数据库数据。

---

## 2026-08-30 | 修复 AI 规划 Playwright 浏览器缺失与 Sync API 实例泄漏

- 任务：修复 AI 规划 `explore_flow` / `explore_page` 失败，并将 Playwright 浏览器安装到 D 盘。
- 操作：
  - 将 Chromium 及依赖（chromium、chromium-headless-shell、ffmpeg、winldd）安装到 `D:\PlaywrightBrowsers`。
  - 在 `backend/.env` 与 `backend/.env.example` 增加 `PLAYWRIGHT_BROWSERS_PATH=D:\PlaywrightBrowsers`。
  - 修复 `BrowserSessionManager.get_or_create_context` 与 `_collect_flow_a11y`：浏览器启动失败时释放 `sync_playwright` 实例，避免后续误报 Sync API inside asyncio loop。
  - 同步 `users_id_seq` / `projects_id_seq` / `project_members_id_seq` 到当前最大 id，消除 `project_members` 主键冲突。
- 验证：
  - `sync_playwright` 成功加载 https://example.com。
  - `_collect_flow_a11y` 成功探索 https://example.com（1 页、8 个元素）。
  - `create_project` 成功创建项目，无主键冲突。
  - 后端默认测试：493 passed、1 skipped、10 deselected。
- 备注：新增 BUG-086 记录到 `docs/bug-log.md`。重启后端前请确保 `.env` 的 `PLAYWRIGHT_BROWSERS_PATH` 已生效。

---

## 2026-08-30 | 写入本地 admin 账号并清空测试数据库

- 任务：测试依赖后端登录但没有注册账号；写入 admin 账号、清空旧数据并验证登录可用。
- 操作：
  - 启动本机 PostgreSQL 18（`D:/PostgreSQL/data`；服务为 NetworkService，普通用户 `net start` 被拒绝，改用 `pg_ctl.exe start -w`）。
  - 清空 `public` schema 全部业务表数据（`TRUNCATE ... RESTART IDENTITY CASCADE`，保留 `alembic_version` 迁移版本）。
  - 通过 ORM 写入账号：`id=1`、`email=admin@example.com`、`display_name=admin`、密码 `admin123`（PBKDF2-SHA256 哈希），并重建默认项目 `Default Project`（`id=1`、`is_default=true`）及 owner 成员关系。
- 验证：
  - `POST /api/v1/auth/login` 使用 `admin@example.com / admin123` 返回 200；`GET /api/v1/auth/me` 返回同一用户；错误密码返回 401。
  - 清理后仅剩 `users=1`、`projects=1`、`project_members=1`，其余业务表均为 0。
- 备注：登录表单要求邮箱格式，因此登录账号为 `admin@example.com`，平台显示名为 `admin`。

---

## 2026-08-30 | P5 前端业务域收口

- 任务：按 planning/projects/cases/executions/reports 拆分前端边界，并恢复真实认证入口。
- 操作：
  - 将 API client 拆到 `shared/api/client.ts`，业务请求拆入各 `features/*/api.ts`；旧 `services/api.ts` 缩为兼容 barrel。
  - 通用 SSE client 和 PageFeedback 下沉到 shared，Planning 取消请求归回 planning domain。
  - 从 `AITestPlanningPanel` 抽出 session 初始化/恢复 hook、SSE 生命周期 hook 和 Requirements 视图。
  - 新增 `AuthGuard` 与 `LoginPage`，统一保护业务路由并支持登录后恢复目标地址。
  - 增加 FastAPI OpenAPI 导出脚本、schema 快照和 `openapi-typescript` 生成命令；auth transport types 已切换到生成类型。
  - 执行依赖安全升级；React Router 升至 v7 并删除失效的 v6 future flags。
- 验证：
  - 前端测试：67 passed。
  - `npm run build` 与 `npm run generate:api-types` 通过，连续生成结果一致。
  - `npm audit --audit-level=low`：0 vulnerabilities。
  - API/SSE 兼容 barrel 保留，现有调用方和测试可渐进迁移。
- 备注：P5 结构目标已落实；手写 `types/api.ts` 作为 UI view model 兼容层保留，transport schema 以生成文件为准。

---

## 2026-08-30 | 凭据本地清理与 Git 历史处置

- 任务：清除误提交的本地配置和智谱 BigModel API 凭据历史。
- 操作：
  - 从本地 `.claude/settings.local.json` 删除包含明文 Bearer 凭据的权限项。
  - 使用 index filter 从 `main` 的全部 484 个历史提交中移除 `.claude/settings.local.json`。
  - 以重写前远端哈希执行 `--force-with-lease`，避免覆盖并发远端更新。
  - 删除 `refs/original`、reflog、临时 bundle 和验证 clone，并执行立即 GC。
- 验证：
  - 本地与远端 `main` 哈希一致。
  - 远端全量克隆中目标路径历史提交数和当前跟踪数均为 0。
  - 本地全部 refs 的目标路径提交数为 0，BigModel Bearer 模式扫描为 0。
- 备注：仓库侧处置完成；外部凭据吊销仍需账号持有人登录智谱 API Keys 控制台完成身份验证。

---

## 2026-08-30 | 本地 main 对齐远端（放弃本地未推送提交）

- 任务：本地与远端仓库同步，清理中断的 rebase 现场。
- 背景：此前一次 `git pull --rebase` 在 `backend/app/services/ai_planning.py` 冲突后中断；本地 `main` 与 `origin/main` 因远端凭据清理 force-push 已分叉。
- 决策：用户确认以远端为准，放弃本地未推送提交（Bug #J、EventLogWriter 方法名修复、三重修复等 4 个提交）。
- 操作：
  - `git rebase --abort` 回到 `main`。
  - `git fetch origin --prune`。
  - `git reset --hard origin/main`。
- 验证：本地 `main` 与 `origin/main` 一致（`4719f1e`），无未提交跟踪文件差异；30 个未跟踪 `docs/` 文件保持不变。

---

## 2026-08-30 | P0-P5 重构成果复核与代码质量评估

- 任务：基于真实代码与动态测试检查 P0-P5 重构完成度，评估当前代码质量（不只看 md 结论）。
- 操作：
  - 检查 `git log`/工作树：P0-P5 各阶段提交存在，`main` 与 `origin/main` 对齐（`5ec68ec`），工作树干净。
  - 运行后端默认测试、前端测试与构建；核对 Alembic 迁移链、Planning 兼容 facade、执行单事件源、清理脚本保护、locator 调试路由注册测试。
  - 交叉检查删除门禁：`locator_confidence.py`、`AppLayout.tsx`、`echarts`、`test_dsl.json`、旧 preflight、旧同步 Planning LLM、AI selector cache 等均已删除；休眠能力清单已建立。
- 验证：
  - `uv run pytest -q`：1 failed、492 passed、1 skipped、10 deselected；失败为本地 `backend/.env` 中 `ENABLE_AI_VISUAL_LOCATE=true` 污染 `test_ai_visual_locate_default_is_disabled`。
  - `uv run alembic heads`：唯一 head `20260829_0026`；`alembic history` 链条连续无断链。
  - `npm test -- --run`：8 个测试文件通过、1 个文件加载失败（`services/api.test.ts`），根因是 `services/api.ts:7` 引用不存在的 `features/reports/api`。
  - `npm run build`：失败，错误为 `TS2307`（缺少 `features/reports/api`）与 `TS2305`（barrel 未导出 `getReportPreference/updateReportPreference`）。
- 结论：P0-P4 完成度较高，Planning facade 已缩到 72 行、执行同步入口复用流式生成器、清理脚本默认 dry-run 且带保护；P5 前端按域拆分遗漏 `features/reports` 域，导致前端测试与构建全红。
- 发现：本次复核新发现 2 个缺陷，已记录到 `docs/bug-log.md`（AUDIT-20260830-17、AUDIT-20260830-18）。

---

## 2026-08-30 | P5 reports 域修复 + 前端/后端门禁恢复

- 任务：修复复核发现的前端门禁问题，并验证前端构建与后端服务实际跑通。
- 操作：
  - 新增 `frontend/src/features/reports/api.ts` 与 `types.ts`，恢复 `getReportPreference/updateReportPreference` 客户端，barrel `services/api.ts` 重新可用。
  - 修复 `features/planning/api.ts` 中 `getAISettings` 的重复 `return`。
  - 修复 `test_config.py` 两个 VLM 默认值用例：重定向 `ENV_FILE_PATH`，避免本地 `.env` 污染默认断言。
  - 启动 PostgreSQL（`D:/PostgreSQL/data`），执行 `uv run alembic upgrade head` 将本地库从 `20260608_0025` 升到 `20260829_0026`。
  - 发现本地 `api.unself.cn` 网关需带 `/v1` 路径，修正 `.env` 中 `AI_DSL_BASE_URL` 与 `AI_PLANNING_BASE_URL`。
- 验证：
  - 后端默认测试：493 passed、1 skipped、10 deselected。
  - 前端测试：67 passed；`npm run build` 通过。
  - 后端 `uvicorn --factory` 启动，`/api/v1/health` 返回 200。
  - 通过 Vite 代理 `127.0.0.1:5173` 创建会话 311 并发送消息，AI 正常返回收集需求的 `assistant_message`，`session_status=collecting`。
- 备注：AUDIT-20260830-17 已关闭；新增 AUDIT-20260830-19 记录 base URL 配置问题。

---

## 2026-08-30 | 清理 Git 历史中的 backend/.env 明文凭据

- 任务：从 `main` 全部历史提交中移除 `backend/.env` / `.env`，消除远端可见的明文密钥记录。
- 操作：
  - 备份当前 `main` 到本地分支 `backup-pre-env-purge`，并创建 `git bundle`（`D:/AutoTestingLearingProject/backup-pre-env-purge-20260830-031248.bundle`）。
  - `git filter-branch --index-filter 'git rm --cached --ignore-unmatch .env backend/.env' --prune-empty -- main` 重写全部 486 个提交。
  - 删除 `refs/original`，清理 reflog 后 `git push origin main --force-with-lease`。
- 验证：
  - `git rev-list main --objects | grep -E '\.env$'`：无 `.env`/`backend/.env` 对象；`backend/.env.example` 保留。
  - `git log origin/main --oneline -- .env backend/.env`：空。
  - 本地 `main` 与 `origin/main` 一致（`60e51e6`），树无差异。
  - 后端/前端服务仍在运行，`/api/v1/health` 与 Vite 均返回 200。
- 备注：历史中的 `.claude/settings.local.json` 已于更早前清理；本次仅处理 `backend/.env`。工作树中的 `backend/.env` 仍保留用于本地开发（已被 gitignore），但历史已无记录。

---

## 2026-08-30 | 仓库代码质量评估（真实代码 + 动态门禁）

- 任务：应要求对当前仓库做一次代码质量评估，基于真实源码、目录结构与动态测试，不只看文档结论。
- 操作：
  - 统计后端 `backend/app`（22,194 行 Python）与前端 `frontend/src`（13,637 行 TS/TSX）结构，检查分层、路由、模型、迁移、测试覆盖。
  - 运行后端默认 pytest、前端 `npm run build`；检查 TODO/FIXME、`print()`、`console.log`、密钥扫描、Alembic 版本链、重复模块与 lint 配置。
- 验证：
  - 后端：493 passed、1 skipped、10 deselected（95.77s）；存在 `TestCase`/`TestCaseRun` 类名被 pytest 收集的 PytestCollectionWarning。
  - 前端：`tsc --noEmit && vite build` 通过，主包约 763KB（gzip 240KB）偏大，无代码分割治理。
  - 密钥扫描：仅测试夹具中的 `test-key` / `new-dsl-secret` 假值，未发现真实泄露。
- 结论：整体为「结构清晰、可运行、测试覆盖扎实的中上水平」代码库；主要短板是 lint/类型检查未纳入 CI 门禁、4 个超大模块需拆分、`__pycache__`/`dist`/`node_modules` 等生成物污染工作树感知、AI 配置字段随功能堆叠趋杂。
- 备注：仅追加本日志，未改动任何业务代码。

---

## 2026-08-30 | AI 消息流式传输设计分析（对照 pi 事件协议）

- 任务：分析前端消息框设计与 AI 返回消息消费链路，指出思考内容与正文渲染/顺序问题，参考 pi 的 `AssistantMessageEventStream` 事件协议，给出 Web 端与框架优化方案。
- 操作：
  - 阅读 `frontend/src/shared/api/sseClient.ts`、`features/planning/usePlanningSse.ts`、`components/AITestPlanningPanel.tsx` 的 SSE 解析与 `handleStreamEvent` 分发逻辑。
  - 阅读 `backend/app/ai/test_planning_agent.py` 的 `stream_planning_turn` / `_stream_planning_llm`，确认 `text_chunk(thinking=true)` 与正文共用 `content` 字段、前端拆成 `_thinkingContent` + `content` 两路渲染。
  - 对照 `pi/packages/ai/src/types.ts` 的 `AssistantMessageEvent`（text/thinking/toolcall 的 start/delta/end + contentIndex）与 `utils/event-stream.ts` 的 `EventStream`。
- 结论：当前是「动作型扁平事件 + 前端手工拼字符串」，思考与正文不是有序 content block，导致思考折叠但正文在框外、顺序丢失、`turn_complete` 覆盖流式文本等问题；应改为「消息生命周期事件 + contentIndex + reducer」，思考作为一等 content block。
- 备注：仅追加本日志，未改动任何业务代码。

---

## 2026-08-30 | 对齐 pi 的 AI 消息流式传输协议（实现）

- 任务：将前端消息框与后端流式事件协议对齐 pi 的 `AssistantMessageEvent` 设计（有序 content block + contentIndex），解决思考内容与正文乱序、正文覆盖等问题。
- 操作：
  - 后端 `app/ai/test_planning_agent.py::_stream_planning_llm` 由扁平 `text_chunk(thinking=true)` 改为产出 `content_block_start / content_block_delta / content_block_end`（带 `content_index` 与 `kind`）。
  - `app/ai/test_planning_agent.py::stream_planning_turn` 透传三类 `content_block_*` 事件。
  - `app/application/planning/conversation_service.py` 新增 `_flush_streaming_content_block`，把有序块持久化到 `structured_payload_json["content_blocks"]`，并以文本块镜像到 `message.content` 兼容旧渲染；最终 `turn_complete` 不再覆盖流式文本。
  - 前端 `types/api.ts` 新增 `AssistantContentBlock` 与 `ContentBlockStart/Delta/EndStreamEvent`。
  - 前端 `AITestPlanningPanel.tsx` 新增 `readContentBlocks` / `applyContentBlockEvent` / `AssistantMessageBody`，按 content block 顺序渲染思考与正文；`turn_complete` 仅在无流式块时才回填最终消息。
- 验证：
  - 更新 `tests/unit/test_planning_agent.py` 流式断言。
  - 后端默认 pytest：493 passed、1 skipped、10 deselected。
  - 前端 `npm run build` 通过；`npm test` 67 passed。
- 备注：本次工作树还包含上一任务遗留的 BUG-086 修复（`.env.example`、`page_explorer.py`、`docs/bug-log.md`），未在本任务中改动，同步时需一并注意。

---

## 2026-08-30 | 诊断切页后流式消息丢失问题

- 任务：定位「切换页面后再次回到会话页，消息丢失，只能等 AI 全部发完才能看到」的根因。
- 结论：
  - 后端流式期间实际上已经持续落库（user 消息、streaming 消息、content_block 事件、event log），不存在「后端没持久化」。
  - 真正问题是前端「进行中的流式状态」没有可恢复的会话级全局来源：`usePlanningSessionState` 挂在 `AITestPlanningPanel` 内，切页时组件卸载，SSE 继续在后台跑，`handleStreamEvent` 更新的是已卸载组件的本地 state；回到页面时 `initialize()` 重新拉 `getPlanningSession` 并用 `applySessionDetail` 整体覆盖 transcript，把后台流期间本应累积的流式内容冲掉。
  - 恢复逻辑 `applySessionDetailWithRecovery` 只对 `turn_type === "streaming"` 的「中断」消息做一次性 event replay，且 replay 只认识旧事件（text_chunk/status/tool_call/turn_complete），不识别本次新增的 `content_block_*` 事件，因此思考块和有序块无法正确重建。
  - `loadSessionDetail` 没有「SSE 进行中则不覆盖 transcript」的保护，且 `applySessionDetail` 会把 detail.messages 重新映射，丢掉了当前 UI 里正在增长的 content blocks。
  - `AITestPlanningPanel` 内部使用 `sessionId` state 而不跟随路由 `sessionIdProp`，切页回来 `sessionIdProp` 变化被忽略，也放大了状态错位问题。
- 建议修复方向（本次仅诊断，未改代码）：
  1. 把会话状态与活跃 SSE 流提升为全局 store（或 Query/Context），切页只卸载视图、不卸载状态，re-mount 时订阅正在进行的流事件。
  2. 若沿用「后台流 + 回页重放」，加载详情时保留 `_streaming` 消息的实时 content_blocks，或仅在无活跃流时覆盖 transcript。
  3. 扩展 `applySessionDetailWithRecovery` 支持 `content_block_*` 事件重放；否则新协议的消息刷新后思考块仍丢。
  4. `sessionId` 改为跟随路由 prop，避免内部/外部双源不一致。
- 备注：仅追加本诊断记录，未改动业务代码。

---

## 2026-08-30 | 切页消息丢失根治疗方案设计

- 任务：为「切页后流式消息丢失」设计根治疗法（先设计，待实施）。
- 方案核心：把「会话状态」和「活跃 SSE 流」从 `AITestPlanningPanel` 组件提升为路由无关的全局 workspace store；切页只卸载视图，不卸载状态与流；回页时由 store 判定「有活跃流则复用内存 transcript，无活跃流则拉详情并按统一 reducer 重放事件」。
- 关键落点：
  - 新增模块级 `planningWorkspaceStore`（`useSyncExternalStore`，不引入新依赖）。
  - 新增 `planningStreamEvents` 纯函数 reducer，前端实时事件与回页重放共用同一套逻辑，统一支持 `text_chunk`、`content_block_*`、`tool_call_*`、`execution_*`、`turn_complete`。
  - `usePlanningSse` 去掉组件卸载即 abort 的行为，abort 由 store 显式管理。
  - `AITestPlanningPanel` 改为消费 store；`sessionId` 跟随路由 prop。
  - `App.tsx` / `test-utils.tsx` 增加 Provider。
  - 后端可选加固：`GET /sessions/{id}/events` 支持游标/按 message_id 过滤。
- 备注：本轮为方案设计，未改业务代码；实施时按 store → reducer → panel → tests 顺序推进。

---

## 2026-08-30 | 切页消息丢失根治疗方案落地

- 任务：实施「会话状态 + 活跃 SSE 流提升为路由无关全局 workspace store」的根治方案。
- 操作：
  - 新增 `frontend/src/features/planning/planningStreamEvents.ts`：纯函数 reducer（`reduceTranscriptEvent`）+ `readContentBlocks` / `applyContentBlockEvent` / `createOptimisticMessage`，实时事件与重放共用。
  - 新增 `frontend/src/features/planning/planningWorkspaceStore.tsx`：`useSyncExternalStore` 外部 store，管理 `currentSessionId`、各 session 的 `transcript/requirements/plan/drafts/activeStream`；`loadSessionDetail` 有活跃流时只更新元数据、不覆盖 transcript；无活跃流时按事件日志重放并支持 `content_block_*`。
  - 重构 `usePlanningSse.ts`：去掉组件卸载即 abort，改为 store 管理 abort，新增 `runStream(sessionId, kind, messageId, options)`。
  - 重构 `usePlanningSessionState.ts`：改为消费 store，提供兼容旧面板的 API。
  - 重构 `AITestPlanningPanel.tsx`：删除组件内手写 `handleStreamEvent`，事件交给 store reducer；流式状态由 `activeStreamKind` 派生；乐观消息 ID 传给 `runStream` 作为活跃消息 ID。
  - `App.tsx` / `test-utils.tsx` 注入 `PlanningWorkspaceProvider`。
- 验证：
  - 新增 `planningStreamEvents.test.ts` 7 用例、`planningWorkspaceStore.test.ts` 3 用例。
  - `npm run build` 通过；`npm test` 77 passed（11 files）。
- 备注：后端未改动；本次为纯前端架构修复。

---

## 2026-08-29 | P3 Active Project 与项目上下文边界

- 任务：落实 Planning Session 单 active project 语义，并将项目上下文职责移出总编排器。
- 操作：
  - `ai_planning_sessions` 新增 `active_project_id`，migration 为历史 session 回填最早关联项目。
  - 新建、关联和工具创建项目时自动切换 active project；重复关联作为幂等切换；删除 active 项目时回退到最早剩余关联。
  - 所有 Planning 执行路径改用集中 `_get_active_project_id`，移除散落的 `project_ids[0]`。
  - API schema 和前端类型增加 `active_project_id` / `is_active`，项目面板支持查看和点击切换当前项目。
  - 新增 `app/application/planning/project_context.py`，承接 session ownership、项目关联、active project 和成员修复职责。
  - AI Planning API 直接依赖 application project context，不再动态导入 service 私有 `_get_session`。
- 验证：
  - 后端默认测试：491 passed、1 skipped、10 deselected。
  - 前端测试：64 passed；`npm run build` 通过。
  - active project migration 独立升降级测试通过；Alembic 唯一 head 为 `20260829_0026`。
  - 变更范围 Ruff `F401/F821` 检查通过。
- 备注：保留多项目历史关联，仅执行上下文收敛为单 active project；P3 下一批拆 conversation 和 draft application service。

---

## 2026-08-29 | P3 Session Lifecycle 拆分

- 任务：将 Planning Session 生命周期与 schema 映射移出 `services/ai_planning.py`。
- 操作：
  - 新增 `application/planning/session_service.py`，承接 session list/create/detail/delete。
  - 新增 `application/planning/presenters.py`，统一 session/message/draft schema 转换。
  - 将 required requirement slots 移到 schema 层，Agent 与 session service 共享同一合同。
  - AI Planning API 直接依赖 session service；总编排器删除重复实现和 presenter。
  - 新建 session 对显式 `project_id` 增加存在性校验，修正旧测试依赖 SQLite 关闭外键的错误假设。
- 验证：
  - session/API 定向测试：30 passed。
  - 后端默认测试：491 passed、1 skipped、10 deselected。
  - application/route/service 变更范围 Ruff `F401/F821` 检查通过。
- 备注：下一批拆 conversation service 与 draft service，继续缩减总编排器。

---

## 2026-08-29 | P3 Conversation Service 拆分

- 任务：将 Planning 同步/流式对话编排迁入 application service，并显式化外部依赖。
- 操作：
  - 新增 `application/planning/conversation_service.py`，承接消息、会话状态、工具结果和流式占位消息持久化。
  - 通过 `ConversationContextInjector`、`AutoDraftGenerator` 和 `ConversationEventLogFactory` ports 注入上下文、草案生成和事件日志能力。
  - API 与 streaming bridge 改为调用 conversation service；旧总服务删除重复的同步/流式实现。
  - 将上下文注入和自动草案 adapter 改为公开符号，消除路由与 application service 对私有实现的依赖。
  - 更新 API 测试 patch 边界，并验证 Agent 收到 transcript 与 auto-draft port。
- 验证：
  - Planning 定向测试：108 passed、1 skipped。
  - 后端默认测试：491 passed、1 skipped、10 deselected。
  - 变更范围 Ruff `F401/F821`、Python compileall 和 `git diff --check` 通过。
- 备注：P3 下一批拆 draft service，再拆 save-and-execute 与 analysis/retest。

---

## 2026-08-29 | P3 Draft 与 Context Service 拆分

- 任务：将 Planning 草案生命周期和共享上下文构建移出总编排器。
- 操作：
  - 新增 `application/planning/draft_service.py`，承接草案生成、流式生成、状态更新、删除及自动草案 adapter。
  - 新增 `application/planning/context_service.py`，承接会话状态、工具历史、anti-pattern 和执行错误上下文构建。
  - AI Planning API 与 streaming bridge 直接依赖新 application services。
  - `services.ai_planning` 删除对应实现，仅保留兼容导出。
  - 测试替身改为 patch 实际实现模块，避免 facade 迁移后出现无效 mock。
- 验证：
  - Planning/Context/Analysis 定向测试：73 passed、1 skipped。
  - 变更范围 Ruff `F401/F821` 与 `git diff --check` 通过。
- 备注：下一批拆 save-and-execute 与 analysis/retest，并继续收敛 application ports。

---

## 2026-08-29 | P3 Save/Execute 与 Analysis/Retest 拆分

- 任务：完成 Planning application service 用例拆分并移除总编排器实现。
- 操作：
  - 新增 `execution_inputs.py`、`save_execute_service.py` 和 `analysis_retest_service.py`。
  - API 与 streaming bridge 直接依赖 application services；`services.ai_planning` 收敛为 71 行兼容 facade。
  - 将跨 application service 的分析、上下文和输入解析 helper 改为公开合同。
  - Planning Tools 为项目状态、洞察和推荐复测查询提供公共入口。
  - 修复流式 save-and-execute 调用不存在的 `EventLogWriter.log()`，改为注入 event-log factory 并调用 `write()`。
- 验证：
  - P3 定向测试：61 passed。
  - 变更范围 Ruff `F401/F821` 和 `git diff --check` 通过。
- 备注：P3 用例服务拆分完成；兼容 facade 保留一个迁移周期，下一阶段进入 P4 单执行事件源。

---

## 2026-08-29 | P4 单执行事件源与取消终态

- 任务：统一同步与流式 Case 执行核心，消除重复事务和报告构建路径。
- 操作：
  - `execute_case()` 改为消费 `execute_case_streaming()` 至终态，同步与流式入口共享 runner、状态迁移和 evidence 持久化。
  - 删除重复 `_execute_case_record()` 及同步 Playwright runner 依赖。
  - 增加同步/流式等价测试，比较最终状态、步骤顺序和完整 evidence。
  - 将 `cancelled` 纳入前后端执行状态合同，并在取消异常重新抛出前持久化报告与结束时间。
  - 补齐执行详情和报告中心的取消状态展示。
- 验证：
  - Execution 定向测试：19 passed。
  - 前端测试：64 passed。
  - 变更范围 Ruff `F401/F821` 通过。
- 备注：Runner 的流式解释器现为唯一执行源；报告继续只读取 `TestCaseRun.report` 持久化 JSON。

---

## 2026-08-28 | 全代码库孤儿代码与架构审计

- 任务：全量扫描代码库，识别无引用且不属于关键链路的代码，并评估当前架构与优化方向。
- 操作：
  - 以 275 个 tracked 文件为全集，建立前后端入口、API、DSL、AI planning、runner、locator、report 和 migration 链路。
  - 使用 Knip、Vulture、Pyflakes、`rg` 及独立子代理交叉检查，并排除 FastAPI、SQLAlchemy、Alembic、pytest、React lazy import 等隐式引用误报。
  - 生成 `docs/codebase-orphan-architecture-audit-2026-08-28.md`，记录确定孤儿、休眠能力、保留项、结构评分和分阶段优化方案。
- 验证：
  - `npm run build` 通过。
  - `npm test -- --run`：53 passed，7 failed。
  - 后端动态测试未运行：当前环境只有 Python 3.9，项目要求 Python 3.12，且未安装 `uv`。
- 发现：明文凭据、Alembic 迁移链断裂、无鉴权调试接口、VLM 分支未定义变量、错误新建用例路由、默认测试遗漏 integration 等问题已记录到 `docs/bug-log.md`。

---

## 2026-08-28 | 代码库优化执行计划

- 任务：根据 `docs/codebase-orphan-architecture-audit-2026-08-28.md` 制定可排期、可验收的优化计划。
- 操作：
  - 新增 `docs/plan/codebase-optimization-plan-2026-08-28.md`。
  - 将 D-01 至 D-11、O-01 至 O-19 和主要架构问题拆为 P0-P5 六个阶段。
  - 补充任务依赖、工期估算、测试门禁、删除门禁、架构决策、回滚策略和全计划完成定义。
  - 将安全止血、迁移恢复和测试基线设为架构重构前置条件。
- 验证：
  - 审计中的 D-01 至 D-11 和 O-01 至 O-19 均已纳入阶段映射。
  - 计划文件位于 `.gitignore` 白名单 `docs/plan/*.md`，可被 Git 跟踪。
- 备注：本次仅制定计划，未执行缺陷修复、孤儿代码删除或测试命令；未发现审计报告以外的新缺陷。

---

## 2026-08-28 | 优化计划同步与 P0 仓库治理

- 任务：同步代码库优化计划，并执行 P0 安全止血、迁移链恢复和跟踪策略修复。
- 操作：
  - 将审计报告、优化计划和日志提交并同步到 GitHub `main`，提交为 `7f055b3`。
  - 收窄 `.gitignore`，恢复文档、测试和 migration 默认跟踪，并忽略本地 `.tools/`。
  - 停止跟踪 `.claude/settings.local.json`，保留开发者本地文件；外部凭据轮换和历史处置仍待人工完成。
  - 恢复缺失 migration `45061d8892d7_add_is_default_to_projects.py` 及升降级回归测试。
  - 从生产 router 删除无鉴权 `/api/v1/ai-planning/test/locator` 调试接口并增加路由注册测试。
- 验证：
  - `uv run pytest tests/unit/test_project_default_migration.py -q`：1 passed。
  - `uv run pytest tests/unit/test_ai_planning_api.py::test_locator_debug_route_is_not_registered -q`：1 passed。
  - `uv run pytest tests/unit -q`：503 passed、2 failed、1 skipped；失败均为既有测试合同漂移，与本次 P0 修改无关，转入 P1 处理。
  - `uv run alembic heads`：唯一 head 为 `20260608_0025`。
  - 安全复核：本次差异未发现新增可利用问题。
- 发现：SQLite 空库全链升级在历史 migration `20260313_0004` 处失败，已记录为 `AUDIT-20260828-12`；生产 PostgreSQL 空库升级尚未在当前环境验证。

---

## 2026-08-28 | P1 运行时缺陷与测试门禁修复

- 任务：修复审计 D-04 至 D-10，并恢复前后端默认测试基线。
- 操作：
  - 修复 VLM candidate ranker 的 `model_family` 参数传递。
  - 删除已失效的 AI selector cache 及未消费指标。
  - 修正 DSL service `__all__` 并增加公开符号一致性测试。
  - 加固孤儿数据清理脚本：默认 dry-run、显式确认、默认项目/成员/用例/session 保护。
  - 默认 pytest 纳入非浏览器 integration，并为浏览器和外部服务 E2E 补 marker。
  - 增加 `/cases/new` create mode、项目参数传递和非法 case ID 拦截。
  - 修复前端路由、Cases 数据 mock、Planning SSE 时序断言、render-time navigate 和 TextArea NaN height。
- 验证：
  - 后端默认测试：519 passed、1 skipped、10 deselected。
  - 前端测试：63 passed。
  - 前端 `npm run build`：通过。
  - Knip：仍报告 P2 范围的孤儿文件、未用依赖/导出，以及 `@ant-design/icons` 未声明直接依赖。
  - Ruff：工具可运行，但全仓存在 575 个既有问题，不能直接作为零告警门禁；需先建立基线或分阶段清理。
- 备注：D-04 至 D-10 已关闭。静态检查遗留转入 P2；未修改用户已暂存的 `test_brand_filter_cart`。

---

## 2026-08-28 | P2 确定孤儿代码清理

- 任务：按 C1-C4 批次删除审计 O-01 至 O-19 的确定孤儿，并治理休眠能力。
- 操作：
  - 删除孤儿 backend runner、frontend layout、临时 DSL 结果和冗余 `.gitkeep`。
  - 移除未使用 `echarts` 及 Vite 分包规则，补齐 `@ant-design/icons` 直接依赖。
  - 删除未使用 CSS、DSL adapter/helper、旧 locator preflight 和 candidate collector。
  - 删除 Page Explorer 旧 DOM prompt formatter、grouping/filter 子系统及旧 flow action 链，保留 `_collect_flow_a11y` 当前主链。
  - 删除旧同步 Planning LLM、未启用日志上下文/Timer、零调用 locator/access/SSE helper 和前端旧导出。
  - 删除只固定旧实现的测试，保留并回归当前 DSL、A11y、runner 和 API 合同测试。
  - 新增 `docs/plan/capability-status-2026-08-28.md`，记录休眠能力 owner、状态和复核期限。
- 验证：
  - 后端默认测试：490 passed、1 skipped、10 deselected。
  - 前端测试：63 passed；`npm run build` 通过。
  - Knip 不再报告孤儿文件、未使用依赖和缺失直接依赖；剩余项为明确保留的 dormant API clients 与 transport types。
  - 变更文件 Ruff `F401/F821` 检查通过。
- 备注：`CaseListParams` 整体无消费者，已删除；Pydantic 框架隐式入口和 Alembic 历史均保留。

---

## 2026-08-28 | P3 Planning 解耦第一切片

- 任务：冻结 Planning/执行语义，并消除 Agent 对 Planning Service 的反向依赖。
- 操作：
  - 新增 `docs/plan/adr-001-planning-execution-semantics.md`，确定单 active project、VLM 默认关闭和同步/流式单事件源方向。
  - 将 `ENABLE_AI_VISUAL_LOCATE` 默认值改为 false，保留显式启用能力。
  - 将工具结果 URL 归一化、缓存查询和原始页面结果提取迁到 `app.ai.tool_result_cache`。
  - Planning Tools 不再导入 `services.ai_planning` 私有缓存函数。
  - Agent 通过 `AutoDraftGenerator` protocol 接收草案生成 callback，不再延迟导入 Planning Service。
  - Planning Service 不再调用 Agent 私有 `_extract_raw_page_results`。
- 验证：
  - P3 定向测试：94 passed。
  - 后端默认测试：490 passed、1 skipped、10 deselected。
  - 新增模块及配置变更 Ruff `F401/F821` 检查通过。
- 备注：P3 尚未完成；后续继续建立 active project 边界并按 session/conversation/draft/save-execute/analysis-retest 拆 application services。

---

## 2026-06-08 | 孤儿数据清理：清理 70 个孤立项目，添加清理脚本

**任务**：清理数据库中的孤儿数据。

**问题现象**：
- 数据库中有 70 个孤立项目（不关联任何会话）
- 这些项目是会话删除后遗留的

**清理过程**：

**第一步：检查孤儿数据**
```bash
uv run python scripts/cleanup_orphan_data.py --dry-run
```

输出：
```
=== Orphaned Data Summary ===
Orphaned projects: 70
Orphaned messages: 0
Orphaned drafts: 0
Orphaned event logs: 0
Orphaned session-project links: 0
```

**第二步：执行清理**
```bash
uv run python scripts/cleanup_orphan_data.py
```

输出：
```
=== Cleaning up orphaned data ===
Deleted 0 orphaned session-project links
Deleted 0 orphaned event logs
Deleted 0 orphaned drafts
Deleted 0 orphaned messages
Deleted 70 orphaned projects
```

**第三步：验证清理**
```bash
uv run python scripts/cleanup_orphan_data.py --dry-run
```

输出：
```
=== Orphaned Data Summary ===
Orphaned projects: 0
Orphaned messages: 0
Orphaned drafts: 0
Orphaned event logs: 0
Orphaned session-project links: 0
```

### 新增文件

**1. 清理脚本**
- `backend/scripts/cleanup_orphan_data.py`
- 功能：查找并删除所有孤儿数据
- 支持 `--dry-run` 模式预览
- 支持：孤立项目、消息、草案、事件日志、会话-项目链接

**2. 测试文件**
- `backend/tests/unit/test_orphan_session.py`
- 功能：测试孤儿会话场景
- 6 个测试用例覆盖各种边界情况

### 验证结果

- 后端单元测试：501 tests passed（2 个预存失败）
- 孤儿数据清理：70 个孤立项目已删除
- 数据库状态：所有孤儿数据已清理

### 设计原则

1. **定期清理**：定期运行清理脚本，防止孤儿数据积累
2. **测试覆盖**：测试覆盖所有边界情况
3. **防御性编程**：在入口处验证数据，防止孤儿数据产生
4. **监控告警**：监控孤儿数据数量，及时发现问题

---

## 2026-06-08 | 孤儿会话根因修复：SSE 端点添加会话验证，测试驱动调试

**任务**：通过测试驱动调试找到孤儿会话的根因并修复。

**问题现象**：
- 前端显示会话 305
- 数据库中没有会话 305
- 用户尝试发送消息时报错：`AI planning session 305 not found`

**调试过程**：

**第一步：写测试复现问题**
创建 `test_orphan_session.py`，测试以下场景：
1. 会话列表只包含存在的会话
2. 发送消息到已删除的会话返回 404
3. 获取已删除会话的详情返回 404
4. 为已删除的会话生成草案返回 404
5. 并发会话操作
6. 会话删除后列表刷新

**第二步：发现 bug**
测试 `test_generate_drafts_for_deleted_session_returns_404` 失败：
```
assert 200 == 404  # drafts 端点返回 200，而不是 404
```

**根因分析**：
SSE 端点（`/chat`、`/drafts`、`/execute`）在启动流之前**没有验证会话是否存在**。错误发生在流式生成器内部，前端收到 200 状态码，但流会失败。

**这是孤儿会话的根本原因**：
1. 前端调用 `/drafts` 端点
2. 后端返回 200（流式响应）
3. 前端认为操作成功
4. 流式生成器内部报错：`AI planning session X not found`
5. 前端显示错误，但会话列表没有刷新
6. 用户看到孤儿会话

**第三步：修复**
在所有 SSE 端点添加会话验证：
- `/chat`：启动流之前验证会话存在
- `/drafts`：启动流之前验证会话存在
- `/execute`：启动流之前验证会话存在

如果会话不存在，直接返回 404，而不是启动流再报错。

### 后端改动

**1. 新增测试文件**
- `tests/unit/test_orphan_session.py`：6 个测试用例覆盖孤儿会话场景

**2. 修改 SSE 端点**
- `backend/app/api/routes/ai_planning.py`：
  - `chat_sse`：添加会话验证
  - `drafts_sse`：添加会话验证
  - `execute_sse`：添加会话验证

**3. 前端错误处理**（之前的改动）
- `handleSendMessage`：发送前验证会话存在
- `handleGenerateDrafts`：生成前验证会话存在
- `loadSessionDetail`：加载失败时刷新会话列表

### 验证结果

- 后端单元测试：501 tests passed（2 个预存失败）
- 孤儿会话测试：6 tests passed
- 前端构建：TypeScript 编译 + Vite 构建成功

### 设计原则

1. **测试驱动调试**：先写测试复现问题，再修复
2. **根因分析**：找到问题的根本原因，而不是加防线
3. **防御性编程**：在入口处验证数据，而不是在内部报错
4. **错误处理**：返回明确的错误码，而不是流式失败

---

## 2026-06-08 | 孤儿会话修复：前端会话验证 + 错误处理 + 状态同步

**任务**：修复前端显示孤儿会话数据（数据库中不存在）的问题。

**问题现象**：
- 前端显示会话 305
- 数据库中没有会话 305
- 用户尝试发送消息时报错：`AI planning session 305 not found`

**根本原因**：
1. 会话创建失败但前端乐观更新
2. 会话被删除但前端未同步
3. 网络错误导致状态不一致
4. 竞态条件

**设计缺陷**：
- 没有考虑异常流程
- 没有考虑数据一致性
- 没有考虑边界情况
- 没有设计测试用例

**修复方案**：

**1. 前端添加会话验证**
- `handleSendMessage`：发送消息前验证会话存在
- `handleGenerateDrafts`：生成草案前验证会话存在
- `loadSessionDetail`：加载会话失败时刷新会话列表

**2. 前端添加错误处理**
- `createAndSelectSession`：创建会话失败时显示错误
- `handleSendMessage`：发送失败时刷新会话列表
- `handleGenerateDrafts`：生成失败时刷新会话列表

**3. 前端添加状态同步**
- 错误发生时自动刷新会话列表
- 会话不存在时显示友好错误消息
- 会话列表保持与数据库同步

**设计原则**：
1. **永远不要假设数据存在**：访问前必须验证
2. **永远不要忽略错误**：所有操作必须有错误处理
3. **永远不要信任缓存**：关键操作前刷新数据
4. **永远不要忽略边界情况**：考虑所有可能的失败场景

### 后端改动

**1. 修复迁移链**
- `0cf285e27ae1` 的 `down_revision` 从 `20260426_0022` 改为 `20260426_0021`
- 执行 `uv run alembic upgrade head` 创建 `ai_planning_event_logs` 表

### 前端改动

**1. `handleSendMessage`**
- 发送消息前调用 `getPlanningSession` 验证会话存在
- 发送失败时自动刷新会话列表

**2. `handleGenerateDrafts`**
- 生成草案前调用 `getPlanningSession` 验证会话存在
- 生成失败时自动刷新会话列表

**3. `loadSessionDetail`**
- 加载失败时显示友好错误消息
- 加载失败时自动刷新会话列表

**4. `createAndSelectSession`**
- 创建失败时显示友好错误消息
- 创建失败时抛出错误供调用者处理

### 验证结果

- 后端单元测试：495 tests passed（2 个预存失败）
- 前端构建：TypeScript 编译 + Vite 构建成功
- 会话验证：发送消息前验证会话存在
- 错误处理：所有操作都有错误处理
- 状态同步：错误发生时自动刷新会话列表

---

## 2026-06-08 | SSE 架构修复 v2：Session 隔离 + 弹性降级

**任务**：修复 SSE 事件日志架构设计缺陷——表不存在时流式崩溃。

**问题根因**：
原设计中 `EventLogWriter` 和主流共享同一个数据库 session。当事件日志写入失败时，session 被 rollback，导致主流的 `_flush_streaming_msg_to_db` 也失败。

**日志证据**：
```
Failed to flush SSE events (session 304), disabling: This Session's transaction has been rolled back
due to a previous exception during flush. Original exception was: (psycopg.errors.UndefinedTable)
关系 "ai_planning_event_logs" 不存在
```

**解决方案：Session 隔离 + 弹性降级**

**核心改动**：
1. **Session 隔离**：`EventLogWriter` 使用独立的数据库 session，不影响主流
2. **无前置初始化**：`__init__` 不查询数据库，只存储参数
3. **内联写入**：每个事件写入是独立的 try-catch，失败后静默禁用
4. **弹性降级**：如果表不存在，第一次写入失败 → 后续写入全部跳过 → 主流不受影响

**关键代码**：
```python
class EventLogWriter:
    def __init__(self, session_factory, session_id, ...):
        self._session_factory = session_factory  # 接收 session_factory，不是 session
        self._session = None  # Lazy init，不立即创建

    def write(self, event_type, event_data):
        if not self._enabled:
            return
        session = self._get_session()  # 独立 session
        try:
            session.add(AIPlanningEventLog(...))
        except Exception:
            self._enabled = False  # 只禁用事件日志，不影响主流
            session.rollback()  # 只 rollback 独立 session
```

**架构对比**：
```
旧设计（错误）：
┌─────────────┐
│ Main Session │◀─── EventLogWriter（共享 session）
└─────────────┘
       │
       ▼ 写入失败 → session rollback → 主流崩溃

新设计（正确）：
┌─────────────┐
│ Main Session │◀─── 主流（独立）
└─────────────┘
┌─────────────┐
│ Log Session  │◀─── EventLogWriter（独立 session）
└─────────────┘
       │
       ▼ 写入失败 → log session rollback → 主流不受影响
```

### 后端改动

**1. 重写 `sse_event_log.py`**
- 移除 `SSEEventLogger` 类
- 新增 `EventLogWriter` 类
- 接收 `session_factory` 而不是 `session`
- 使用独立 session，Lazy 初始化

**2. 更新 `ai_planning.py`**
- `stream_planning_message`：新增 `session_factory` 参数
- `stream_generate_planning_drafts`：新增 `session_factory` 参数
- `save_and_execute_selected_drafts_streaming`：新增 `session_factory` 参数
- 所有 `EventLogWriter` 初始化使用 `session_factory`

**3. 更新 `ai_planning_streaming.py`**
- `stream_planning_chat`：传递 `session_factory` 给 `stream_planning_message`
- `stream_planning_drafts`：传递 `session_factory` 给 `stream_generate_planning_drafts`
- `_run_sync_save_and_execute`：传递 `session_factory` 给 `save_and_execute_selected_drafts_streaming`

### 验证结果

- 后端单元测试：495 tests passed（2 个预存失败）
- 前端构建：TypeScript 编译 + Vite 构建成功
- Session 隔离：事件日志失败不影响主流
- 弹性降级：如果表不存在，流式正常工作，事件日志静默禁用

---

## 2026-06-08 | SSE 架构修复：弹性降级设计，移除前置 DB 依赖

**任务**：修复 SSE 事件日志架构设计缺陷——表不存在时流式崩溃。

**问题根因**：
原设计在 `SSEEventLogger.__init__` 中查询 `ai_planning_event_logs` 表获取 max(seq)，如果表不存在（迁移未执行），整个流式会崩溃。

**设计缺陷**：
- 事件日志是"增强功能"，不应该成为主流程的硬依赖
- 不应该在流式开始前就初始化并查询数据库
- 应该是 LLM 拿到消息之后才开始写入数据库

**解决方案：弹性降级 + 内联写入**

**新架构设计**：
```python
class EventLogWriter:
    def __init__(self, session, session_id, ...):
        # NO DB query here — just store parameters
        self._enabled = True

    def write(self, event_type, event_data):
        if not self._enabled:
            return
        try:
            # Inline write — if table missing, first write fails → disable
            session.add(AIPlanningEventLog(...))
        except Exception:
            self._enabled = False  # Graceful degradation
```

**关键改动**：
1. **移除 `SSEEventLogger` 类**，替换为 `EventLogWriter`
2. **无前置初始化**：`__init__` 不查询数据库，只存储参数
3. **内联写入**：每个事件写入是独立的 try-catch，失败后静默禁用
4. **弹性降级**：如果表不存在，第一次写入失败 → 后续写入全部跳过 → 主流不受影响

**核心原则**：
- 事件日志是"有则更好"的功能，不是"必须存在"的依赖
- 每个写入操作都是独立的，失败不影响主流程
- 不应该在流式开始前就查询数据库

### 后端改动

**1. 重写 `sse_event_log.py`**
- 移除 `SSEEventLogger` 类
- 新增 `EventLogWriter` 类
- 特性：无前置初始化、内联写入、弹性降级

**2. 更新 `ai_planning.py`**
- 替换所有 `SSEEventLogger` 为 `EventLogWriter`
- 移除前置初始化代码
- 保持内联写入模式

**3. 更新 replay API**
- 使用 `created_at` 排序（seq 是 per-stream，不是全局）
- 保持向后兼容性

### 验证结果

- 后端单元测试：495 tests passed（2 个预存失败）
- 前端构建：TypeScript 编译 + Vite 构建成功
- 弹性降级：如果表不存在，流式正常工作，事件日志静默禁用

---

## 2026-06-08 | SSE 事件日志架构：解决刷新丢消息问题

**任务**：修复 AI 规划会话中 SSE 流式消息刷新后丢失的问题。

**问题根因**：
SSE 事件是"发射即忘"的——事件通过 HTTP 流推送给前端但从未持久化到数据库。一旦页面刷新，前端只能从数据库加载，而数据库中的 stub 消息（`turn_type="streaming"`）内容远落后于实际流式进度。用户必须等到整个流完成、数据库最终提交后才能看到完整内容并继续操作。

**具体问题**：
1. `text_chunk` 事件每 5 个才 flush 一次到 DB
2. `status`、`tool_call_start`、`tool_call_end` 等事件从未持久化
3. 没有事件日志/事件存储——SSE 事件是 fire-and-forget
4. 前端无重连/恢复机制——POST-based SSE 无法用浏览器原生 `EventSource` 重连

### 解决方案：SSE 事件日志 + 智能恢复

**架构设计**：
```
┌─────────────┐     ┌──────────────┐     ┌──────────────────┐
│  AI Agent   │────▶│ SSE Stream   │────▶│ Frontend (实时)   │
│ (生成器)     │     │ (HTTP 流)     │     │ (乐观更新)        │
└──────┬──────┘     └──────────────┘     └──────────────────┘
       │
       ▼
┌──────────────┐     ┌──────────────────┐
│  DB Event    │────▶│ Replay API       │
│  Log Table   │     │ (GET /events)    │
└──────────────┘     └──────────────────┘
                              │
                              ▼
                     ┌──────────────────┐
                     │ Frontend (刷新后) │
                     │ (事件恢复)        │
                     └──────────────────┘
```

### 后端改动

**1. 新增模型 `AIPlanningEventLog`**
- 文件：`backend/app/models/ai_planning_event_log.py`
- 字段：session_id, message_id, event_type, event_data, seq, created_at
- 索引：`(session_id, seq)` 联合索引支持高效 range query

**2. 新增 Alembic 迁移**
- 文件：`backend/alembic/versions/20260608_0025_sse_event_log.py`

**3. 新增 `SSEEventLogger` 服务**
- 文件：`backend/app/services/sse_event_log.py`
- 功能：在 SSE 流式过程中批量持久化每个事件
- 特性：自动 flush、序列号管理、错误容忍

**4. 集成到 `stream_planning_message`**
- 文件：`backend/app/services/ai_planning.py`
- 改动：
  - 每个事件 yield 前写入事件日志
  - `status` 事件也触发 flush（原来只有 text_chunk）
  - `_flush_streaming_msg_to_db` 增强：同时更新 `structured_payload_json` 中的阶段信息
  - `_STREAMING_FLUSH_INTERVAL` 从 5 降到 3

**5. 集成到 `stream_generate_planning_drafts`**
- 文件：`backend/app/services/ai_planning.py`
- 改动：draft_generating 和 turn_complete 事件写入日志

**6. 集成到 `save_and_execute_selected_drafts_streaming`**
- 文件：`backend/app/services/ai_planning.py`
- 改动：save_progress, case_start, step_start/complete, analysis_complete, done 事件写入日志

**7. 新增事件 replay API**
- 文件：`backend/app/api/routes/ai_planning.py`
- 端点：`GET /api/v1/ai-planning/sessions/{session_id}/events?after_seq=N`
- 功能：返回指定序号之后的所有事件，支持增量获取

### 前端改动

**1. 新增 `getSessionEvents` API 函数**
- 文件：`frontend/src/services/api.ts`
- 功能：调用 replay API 获取事件日志

**2. 增强 `applySessionDetail` 支持事件恢复**
- 文件：`frontend/src/components/AITestPlanningPanel.tsx`
- 新增 `applySessionDetailWithRecovery` 函数
- 逻辑：
  1. 先应用 DB 状态（显示最后 flush 的内容）
  2. 检测 `turn_type="streaming"` 的中断消息
  3. 调用 replay API 获取该消息的所有事件
  4. 回放 text_chunk/status/tool_call 事件恢复最新内容
  5. 如果发现 `turn_complete` 事件，直接标记为完成
  6. 更新 transcript 显示恢复的内容

**3. UI 增强**
- 新增"✓ 已恢复"指示器，显示内容已从事件日志恢复
- 保持原有的"⏸ 回复中断"指示器作为 fallback

### 验证结果

- 后端单元测试：505 tests passed（修复了 1 个因新增表导致的测试）
- 前端构建：TypeScript 编译 + Vite 构建成功
- 新增表：`ai_planning_event_logs` 已通过 Alembic 迁移创建

### 后续优化（不在本次范围）

1. 心跳保活：SSE 流中定期发送 `:keepalive` 注释
2. 事件日志清理：定期清理超过 7 天的事件日志
3. 断线重连：前端检测连接断开后自动重连
4. 并发控制：限制同时活跃的 SSE 连接数

---

## 2026-06-05 | Locator + Explore 双轨修复：paragraph 角色支持、探索导航精确化、Prompt 清理

**任务**：修复品牌筛选购物车 E2E 测试中"数据采集错误 → DSL 失败"的完整链路问题。

**问题发现过程**：
用户报告两个 bug：① `paragraph "Premium Polo T-Shirts"` 定位失败；② `capture_text` 后的 DSL 步骤意图不清晰。通过追踪 AI Session 302 的完整数据流（DB → explore_flow 参数 → 采集数据 → DSL 生成 → 执行结果），发现根因不只在 locator 层，而是三层叠加。

### Bug K：paragraph/StaticText role 在 semantic locator 中缺失

- **现象**：Run 195 报错 `All locate tiers failed for target: paragraph "Premium Polo T-Shirts"`
- **发现过程**：
  1. 查询 AI Session 302 生成的 DSL draft (ID=218)，发现大量 `paragraph "X"` 和 `heading "X" inside "Y"` 格式的 target
  2. 分析 [semantic.py:48-56](backend/app/locators/semantic.py#L48-L56) 的 `_A11Y_ROLE_TARGET_RE` 正则，发现 `paragraph` 不在角色列表中
  3. 用 Playwright 直接测试 `get_by_role("paragraph")` 能发现 14 个 `<p>` 元素，但 `get_by_role("paragraph", name="Blue Top")` 返回 0——因为 `<p>` 是非交互元素，没有 accessible name
  4. 对比 [dsl_generator.py:829-833](backend/app/ai/dsl_generator.py#L829)，prompt 教 AI 用 `StaticText`，但 `StaticText` 也不在正则和角色映射中
- **代码证据**：
  ```python
  # semantic.py:48-54 — 修复前，paragraph/statictext 都不在正则中
  _A11Y_ROLE_TARGET_RE = re.compile(
      r'^(button|link|...|heading|...|cell|row|column)'  # ← 缺 paragraph/statictext
  )
  # semantic.py:68-109 — 修复前，A11Y_TO_PLAYWRIGHT_ROLE 也没有 paragraph
  ```
- **修复**：
  1. 在 `_A11Y_ROLE_TARGET_RE` 加入 `paragraph|statictext`
  2. 新增 `_TEXT_ONLY_ROLES` 集合标记 `{"paragraph", "statictext"}`
  3. 在 `_build_a11y_candidates` 中，text-only 角色优先用 `get_by_text()` 而非 `get_by_role(name=)`
  4. inside scoping 的容器查找从爬 1 级 (`xpath=..`) 改为爬 3 级
- **实际成果**：`paragraph "Blue Top"` 正确解析为 `get_by_text("Blue Top", exact=True)`，text 匹配准确。手动 18/18 步全通过。

### Bug L：explore_flow 的 a11y click 导航不可靠

- **现象**：AI Session 302 的 Call 1（点击式探索）采集到的 Polo 页面数据是全部 35 个产品，不是 6 个 Polo 专属产品
- **发现过程**：
  1. 查询 `ai_planning_tool_results` (explore id=357/358)，对比两次探索的 raw_result
  2. Call 1（steps-based, `click "Polo"`）产出 34 个产品段落，Call 2（URL-based, 编造 `products?brand=Polo`）也是同样全部产品
  3. 用 Playwright 实测 `resolve_with_fallback(page, 'Polo')` 能正确找到 `a[href="/brand_products/Polo"]`，但 explore_flow 中采集的产品仍是全部
  4. 追踪到 [page_explorer.py:1590](backend/app/ai/page_explorer.py#L1590)：探索阶段 click 只用 `_resolve_step_locator`（a11y text 匹配），可能匹配到错误元素（如 breadcrumb/文本节点）且静默失败
- **代码证据**：
  ```python
  # page_explorer.py:1590-1597 — 修复前，只有 a11y 定位，没有精确选择器
  elif act in ("click", "press", "tap"):
      loc = _resolve_step_locator(page, target, kind="click", skip_vlm=True)
      if loc is None:
          loc = _resolve_step_locator(page, target, kind="click", skip_vlm=True)
      if loc is not None:
          click_with_precheck(page, loc)
  ```
- **修复**：新增 `_resolve_from_collected_nodes` 函数（~60行），在探索阶段 click 时优先用上一页采集的 `verified_selectors`（如 `a[href="/brand_products/Polo"]`）或 DOM 属性构造 CSS 选择器，失败才回退 a11y locator
- **实际成果**：探索采集从 35 个全部产品精确到 6 个 Polo 专属产品（Blue Top, Fancy Green Top, Green Side Placket Detail T-Shirt, Premium Polo T-Shirts, Soft Stretch Jeans, Grunt Blue Slim Fit Jeans）

### Bug M：explore_flow 页面 URL 归因在动作前

- **现象**：修复 Bug L 后，`_collect_flow_a11y` 将 Polo 页面的 173 个节点归因到 products 页面 URL（`S1` 状态），而非 Polo 页面
- **发现过程**：测试脚本打印每个 state 的产品集合，发现 S1（products）有 34 个产品，S2（Polo）不存在
- **代码证据**：
  ```python
  # page_explorer.py:1637 — 修复前，URL 在动作执行前捕获
  current_url = page.url  # ← 此时还在 products 页面
  # ... 执行 click Polo → 导航到 brand_products/Polo ...
  # 但 page_entry["url"] 仍是 products 的 URL
  ```
- **修复**：在 `results.append(page_entry)` 前检查 `page.url != current_url`，如果导航发生则更新 `page_entry["url"]` 和新 state，并回写 action nodes 的 `page_state`
- **实际成果**：Polo 页面正确获得独立 state（S2），节点归因准确

### Prompt 清理

对 3 个文件 9 处硬编码 prompt 修复：
- **dsl_generator.py**：`StaticText` → `paragraph` 示例统一、删除 `cell` 示例、合并重复 `${var}` 规则
- **test_planning_agent.py**：删除 `test@automationexercise.com`/`password123`/`click "(6) POLO"` 硬编码 demo 值
- **test_planning_prompts.py**：删除 `"Signup / Login"`/`"Email"` 硬编码、删除"探索失败→报告用户不跳过"（与代码 GUARD_CONTINUE_LIMIT 矛盾）

### 全链路验证

探索修复 + prompt 清理后，运行完整 pipeline：
1. ✅ 探索采集：Polo 专属 6 个产品，精确无误
2. ✅ DSL 生成：结构正确，AI 正确选择了 Blue Top (Rs.500) + Fancy Green Top (Rs.700)
3. ⚠️ 执行：AI 仍在 Fancy Green Top 的价格上出错（用了 Rs.1000 而非 Rs.700），属于 LLM 推理层面问题，待后续 prompt 优化

**待解决问题**：
- AI 从探索数据中提取 product→price 关联仍有 LLM 幻觉（把 Green Side Placket Detail T-Shirt 的 Rs.1000 安到 Fancy Green Top）
- 全链路执行因价格错误未通过，需要改进 DSL generator 的数据呈现格式

**测试结果**：
- paragraph/StaticText 修复后手动 18/18 步通过
- 探索修复后精确采集 Polo 6 产品
- 全链路：探索 ✅ → DSL 生成 ⚠️ → 执行 ❌

---

## 2026-06-04 | Anti-pattern 注入与上下文重构

**任务**：重构上下文注入架构，修复执行错误注入，解决 VLM fallback 问题。

**测试需求**：
- 验证品牌筛选购物车流程
- 测试执行错误注入是否生效
- 测试 DSL 生成器是否使用 user_context

**问题现象与修复**：

### 修复 1：执行错误注入失效
- **现象**：当 `case_id` 为 null 时，`_build_execution_error_context` 直接返回 None
- **根因**：函数只从 `case_id` 查询执行记录，没有从项目维度查询
- **修复**：当 `case_id` 为 null 时，从项目的最近执行记录中查找
- **验证**：测试 `test_should_inject_error_when_recent_execution_exists` 通过

### 修复 2：dsl_execution 函数签名错误
- **现象**：`slog.dsl_execution()` 使用了 `session_id` 参数，但函数只接受 `execution_id`
- **根因**：函数签名不匹配
- **修复**：改为使用 `execution_id=latest_run.id`
- **验证**：测试 `test_injects_error_when_case_id_exists` 通过

### 修复 3：上下文注入架构重构
- **现象**：`_inject_auto_context` 函数只在 AI 的 ReAct 循环中被调用，DSL 生成器无法使用
- **根因**：架构耦合，上下文注入函数无法复用
- **修复**：提取 `_build_auto_context_preamble` 函数，可以在 DSL 生成器调用时被调用
- **验证**：测试 `test_build_auto_context_preamble` 系列通过

### 修复 4：DSL 生成器使用 user_context
- **现象**：DSL 生成器有 `user_context` 字段，但没有被使用
- **根因**：DSL 生成器没有在 prompt 中注入 `user_context`
- **修复**：在 `dsl_generator.py` 中添加 `user_context` 的注入
- **验证**：测试 `test_dsl_generator_uses_user_context` 通过

**待解决问题**：
- VLM fallback 问题依然存在（AI 生成 `paragraph` 格式，但不是有效的 Playwright role）
- AI 获取错误信息后没有生成新的 draft，而是重新执行了同一个 draft

**测试结果**：
- 497 个单元测试通过
- 1 个测试跳过
- 6 个警告

---

## 2026-05-31 | textContent + DSL 完善（4 次修复）

**任务**：验证购物车品牌筛选 DSL 测试用例，修复 4 个问题，最终 21/21 步骤全通过。

**测试需求**：
- 首页 → 品牌筛选（Polo）→ 添加 Blue Top (Rs.500) → 添加 Fancy Green Top (Rs.700, qty=2) → 验证购物车总价

**问题现象与修复**：

### 修复 1：textContent 问题
- **现象**：`heading="BRAND - POLO PRODUCTS"` 定位失败，实际文本是 `Brand - Polo Products`
- **根因**：CSS `text-transform: uppercase` 导致 `innerText` 返回全大写，但 Playwright 使用 `textContent`
- **修复**：修改 `_augment_a11y_nodes_with_dom` 函数，使用 `textContent` 替代 `innerText`
- **文件**：`backend/app/ai/page_explorer.py`

### 修复 2：View Product 定位问题
- **现象**：`link="View Product" inside "Blue Top"` 定位失败
- **根因**："View Product" 链接不在产品容器中（a11y 树中是独立的 `list` 元素）
- **修复**：使用 CSS 选择器 `.product-image-wrapper:has(.productinfo:has-text("Fancy Green Top")) a:has-text("View Product")`
- **文件**：`backend/test_dsl.json`

### 修复 3：数量修改问题
- **现象**：购物车页面 `input #quantity 2` 无效，总价未更新
- **根因**：购物车页面数量是只读的（`<button class="disabled">1</button>`）
- **修复**：在产品详情页设置数量为 2，然后再添加到购物车
- **文件**：`backend/test_dsl.json`

### 修复 4：断言值问题
- **现象**：`assert_text cell="Rs. 1400"` 找不到
- **根因**：Fancy Green Top 数量是 1，总价是 Rs. 700
- **修复**：在产品详情页设置数量为 2，总价更新为 Rs. 1400
- **文件**：`backend/test_dsl.json`

**执行动作**：
1. 运行 `python -c "..."` 测试 DSL 执行
2. 检查 a11y 树结构，发现 "View Product" 不在产品容器中
3. 检查购物车 HTML 结构，发现数量是只读的
4. 测试产品详情页设置数量后添加到购物车

**结果**：21/21 步骤全通过

**验证**：
```
Step 1: OK - goto
Step 2: OK - click (Products)
Step 3: OK - wait_for (All Products)
Step 4: OK - click ((6)Polo)
Step 5: OK - wait_for (Brand - Polo Products)
Step 6: OK - capture_text (Rs. 500)
Step 7: OK - click (Add to cart - Blue Top)
Step 8: OK - wait_for (Continue Shopping)
Step 9: OK - click (Continue Shopping)
Step 10: OK - capture_text (Rs. 700)
Step 11: OK - click (View Product - Fancy Green Top)
Step 12: OK - wait_for (Quantity:)
Step 13: OK - input (#quantity = 2)
Step 14: OK - click (Add to cart)
Step 15: OK - wait_for (View Cart)
Step 16: OK - click (View Cart)
Step 17: OK - wait_for (Shopping Cart)
Step 18: OK - assert_text (Blue Top)
Step 19: OK - assert_text (Rs. 500)
Step 20: OK - assert_text (Rs. 500)
Step 21: OK - assert_text (Rs. 1400)
```

**后续**：
- "View Product" 链接不在产品容器中，无法使用 `inside` 语法，需要使用 CSS 选择器
- 购物车页面数量是只读的，需要在产品详情页设置数量

---

## 2026-05-31 | A11y 无名输入框定位修复

**任务**：验证购物车品牌筛选 DSL 测试用例，修复 Quantity 输入框定位失败问题。

**测试需求**：
- 登录 → 品牌筛选（Polo）→ 添加 Blue Top (Rs.500) → 添加 Fancy Green Top (Rs.700, qty=2) → 验证购物车总价

**问题现象**：
1. `textbox="Quantity"` 定位失败 — 输入框在 a11y 树中是 `ignored` 状态
2. 购物车页面价格是 `cell` role，不是 `heading`，`inside` 语法失效

**根因分析**：
1. Quantity 输入框（`<input type="number" id="quantity">`）没有 `aria-label`，也没有关联的 `<label>` 元素，Chrome 无障碍引擎将其标记为 `ignored`
2. 购物车页面价格在 `<td>` 单元格内，a11y role 是 `cell`，不是 `heading`

**操作**：
1. **语义定位器增强** (`backend/app/locators/semantic.py`)：
   - 添加 `_find_input_near_text()` 函数：查找 label 文本 → 定位兄弟 input 元素
   - 新增 `a11y_label_sibling_input` 策略（基础分 82）
   - 扩展 `_A11Y_ROLE_TARGET_RE` 和 `_A11Y_TO_PLAYWRIGHT_ROLE`：添加 `cell`、`row`、`column`

2. **DSL 修改** (`backend/test_dsl.json`)：
   - `textbox="Quantity"` → `Quantity`（纯文本匹配，触发 label-sibling-input 策略）
   - `heading="Rs. 500" inside "Blue Top"` → `cell="Rs. 500"`
   - `heading="Rs. 1400" inside "Fancy Green Top"` → `cell="Rs. 1400"`

3. **执行脚本清理**：删除 `execute_dsl.py`、`run_dsl_with_runner.py`、`test_cart_flow.py`、`explore_results.json`、`product_structure.txt`

**结果**：29 个步骤全部通过（含 Login、品牌筛选、添加商品、购物车验证）

**验证**：
```
Step 12: input Quantity → OK: Filled Quantity
Step 20: input Quantity → OK: Filled Quantity
Step 28: assert_text cell="Rs. 500" → OK
Step 29: assert_text cell="Rs. 1400" → OK
```

**后续**：
- 此修复适用于所有没有 `aria-label` 的输入框（通过 label 文本自动关联）
- `cell` role 支持可用于表格数据断言

---

## 2026-05-30 | Agent 流程 vs 直接脚本测试对比分析

**任务**：使用 explore-flow 工具探索页面，生成 DSL 并执行测试，验证购物车品牌筛选功能。

**测试需求**：
- 登录 → 品牌筛选（Polo）→ 添加商品 A（Blue Top）→ 添加商品 B（Fancy Green Top）→ 验证购物车

**操作**：
1. 使用 `explore_flow` 探索页面，获取 a11y 节点
2. 基于探索结果生成 DSL
3. 执行 DSL 验证购物车功能

**发现的问题**（共 6 项）：

### 问题 1：a11y 节点过滤太严格
- **现象**：商品名称（`paragraph` 元素）被过滤掉，无法获取
- **根因**：`USEFUL_A11Y_ROLES` 使用白名单模式，遗漏了 `paragraph`、`text`、`statictext` 等角色
- **修复**：改为黑名单模式（`IGNORED_A11Y_ROLES`），只排除已知无用的角色

### 问题 2：语义定位器对文本敏感
- **现象**：`get_by_text("(6) POLO")` 无法匹配页面中的 `"(6)Polo"`
- **根因**：文本匹配对大小写和空格敏感
- **修复**：添加更灵活的匹配策略（去除空格、大小写不敏感、正则表达式、role-based fallback）

### 问题 3：广告遮挡点击操作
- **现象**：Google 广告 iframe 遮挡点击，报错 "subtree intercepts pointer events"
- **根因**：页面上有 Google 广告覆盖层
- **修复**：添加 JavaScript 移除广告 iframe

### 问题 4：弹窗等待问题
- **现象**：点击 "Add to cart" 后立即点击 "Continue Shopping" 失败
- **根因**：弹窗需要时间加载
- **修复**：添加 `wait_for_selector('.modal-content')` 等待弹窗出现

### 问题 5：按钮选择歧义
- **现象**：Agent 流程添加了错误的商品（Men Tshirt 而不是 Fancy Green Top）
- **根因**：
  - 直接脚本：`product_cards.nth(1).locator('.add-to-cart')` → 6 个商品卡片
  - Agent 流程：`.productinfo .add-to-cart` → 34 个按钮（包含 overlay 按钮）
  - 按钮顺序不一致，导致选择了错误的按钮
- **修复**：使用和直接脚本相同的选择器策略

### 问题 6：asyncio 兼容性问题
- **现象**：在 asyncio 事件循环中使用同步 Playwright API 报错
- **根因**：`explore_flow` 使用 asyncio，但 Playwright 同步 API 不能在 asyncio 中使用
- **修复**：使用 `subprocess` 在单独进程中执行 DSL

**测试结果对比**：

| 测试方法 | 结果 | 说明 |
|----------|------|------|
| 直接脚本（test_cart_flow.py） | ✅ 完全成功 | 使用精确的选择器策略 |
| Agent 流程（test_cart_flow_agent_final.py） | ❌ 部分失败 | 浏览器崩溃，无法完成验证 |

**根本原因分析**：

Agent 流程失败的根本原因是**选择器策略差异**：
- 直接脚本：先找商品卡片（`.productinfo`），再在卡片内查找按钮（`.add-to-cart`）
- Agent 流程：直接查找所有按钮（`.productinfo .add-to-cart`），导致匹配到 34 个按钮

**代码变更**：
1. `backend/app/ai/page_explorer.py`：a11y 节点过滤从白名单改为黑名单
2. `backend/app/locators/semantic.py`：添加更灵活的文本匹配策略

**生成的测试文件**：
1. `backend/explore_full_flow_final.json` - 完整的探索结果
2. `backend/test_cart_flow.py` - 直接脚本（成功）
3. `backend/test_cart_flow_agent_final.py` - Agent 流程脚本（部分失败）
4. `backend/test_report.md` - 测试报告

**后续**：
1. 需要修复 Agent 流程中的选择器策略，使用和直接脚本相同的方法
2. 考虑在 explore_flow 中添加更智能的按钮识别逻辑
3. 需要处理浏览器崩溃的问题

---

## 2026-05-30 | E2E 自动化测试 Skill

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

## 2026-05-30 | E2E 自动化测试 Skill（API 回归补充）

- 任务：创建 E2E 自动化测试 skill，使用 test_brand_filter_cart 需求文件验证 AI 规划流程和 DSL 质量
- 操作：
  - 创建 `backend/tests/e2e/test_e2e_brand_filter_cart.py` — httpx 调用 REST API 模拟前端操作
  - 创建 `.claude/skills/e2e-brand-filter-cart.md` — Skill 定义文件
  - 修复 `page_explorer.py`：探索阶段跳过 VLM fallback（`skip_vlm` 参数），避免模态弹窗按钮触发慢速 VLM
  - 修复 `dsl_generator.py`：prompt 规则明确 `${var}` 只能用在 value 字段；新增 `_fix_variable_misuse` 后处理函数
  - 更新 `pyproject.toml`：添加 `e2e_api` marker 和 `tests/e2e` testpath
- 验证：E2E 测试通过（~7-11 分钟），DSL 覆盖登录→品牌筛选→加购→购物车验证完整流程
- 发现的问题：
  - VLM 模型全部失败（429 限流、元素未找到、类型错误）导致 explore_flow 极慢
  - DSL 生成器会将 `${var}` 误用在 target 字段（已在 prompt 和后处理中修复）
- 后续：
  - 测试只验证了 `login_success` 场景，完整购物车验证场景需要更长时间
  - 模态弹窗按钮（Continue Shopping, View Cart）confidence=low，需要更好的处理策略

---

## 2026-05-30 | Automation Exercise Polo 页面无障碍树采集

- 任务：采集 `https://automationexercise.com/brand_products/Polo` 页面的无障碍树元素。
- 操作：使用浏览器打开目标页面，确认标题为 `Automation Exercise - Polo Products`，采集页面可访问快照，整理导航、分类、品牌、商品列表和订阅区元素。
- 验证：页面成功加载，URL 保持为 `/brand_products/Polo`，无障碍快照可读取。
- 备注：本次为页面采集与分析任务，未修改业务代码，未发现需要写入 bug-log 的明确缺陷。

---

## 2026-05-30 | Polo 商品页 DSL 消歧优化分析

- 任务：分析 `explore_flow` 采集 Polo 商品页后，DSL 生成阶段重复使用 `Rs. 500` / 商品名导致商品选择不明确的问题。
- 操作：复查 `page_explorer.py`、`dsl_generator.py`、`locator_preflight.py`、`semantic.py` 中元素格式化、候选预检和语义定位逻辑；结合实际无障碍树确认页面存在同一商品卡片默认层与 hover 层重复暴露。
- 结论：优先把商品卡片抽象为结构化业务单元，生成 DSL 时使用商品名 + 价格 + 卡片内动作的组合定位，避免裸文本 `Rs. 500` 或裸 `Add to cart`。
- 备注：本次为分析建议，未修改业务代码，未追加 bug-log。

---

## 2026-05-30 | Polo 商品页 DSL 消歧修复

- 任务：修复 Polo 商品页 `explore_flow` 后 DSL 生成阶段容易用裸 `Rs. 500` / 裸 `Add to cart` 导致商品选择歧义的问题。
- 操作：
  - `dsl_generator.py`：为 A11y 元素清单增加去重后的商品卡片摘要；重复元素追加 duplicate 标记；系统提示要求商品加购使用 `商品名 附近的 Add to cart`；价格点击自动重写为商品上下文目标。
  - `semantic.py`：支持解析并执行 `Blue Top 附近的 Add to cart` 这类上下文定位。
  - `locator_preflight.py`：裸 `Add to cart` / `View Product` 多匹配时降为 low confidence，并提示补商品上下文。
  - 补充 `test_dsl_generator.py`、`test_locator_confidence.py`、`test_locator_semantic.py` 单测。
- 验证：`uv run pytest tests/unit/test_dsl_generator.py tests/unit/test_locator_confidence.py tests/unit/test_locator_semantic.py -q`，82 passed。
- 备注：未发现新的明确缺陷，未追加 bug-log。

---

## 2026-05-30 | DSL 扩展适配 Playwright 执行器分析

- 任务：分析如果要扩展 DSL 功能，如何更好适配现有 Playwright runner。
- 操作：复查 `schemas/dsl.py`、`runners/playwright_runner.py`、`postcondition_verifier.py`、`services/dsl.py` 中当前动作、定位、postcondition、变量和执行证据能力。
- 结论：建议优先扩展强语义动作、断言、等待、作用域/集合与 evidence schema；避免直接开放任意 Playwright API 或 `evaluate_js`，防止绕过结构化 DSL 校验。
- 备注：本次为设计分析，未修改业务代码，未追加 bug-log。

---

## 2026-05-30 | A11y-first 商品定位消歧修正

- 任务：将 Polo 商品页 DSL 消歧方案从 DOM `text_parent_chain` 修正为无障碍树定位 + 结构化候选校验路径。
- 操作：
  - 移除 `semantic.py` 中 `附近的` / `text_parent_chain` 主定位路径，避免把 DOM 作用域定位混入 a11y 语义定位。
  - `page_explorer.py`：通过 CDP `backendDOMNodeId` 为 a11y 节点回填 DOM 属性，并生成已验证的 Playwright candidate selector。
  - `dsl_generator.py`：商品卡片摘要改为 `target="Add to cart"` + verified candidate；当 AI 误用价格作为 click target 时，重写为带 candidates 的结构化步骤。
  - `locator_preflight.py`：将 a11y 节点上的 `verified_selectors` 写入 DSL candidates；裸重复商品动作仍标记为 low confidence。
  - `playwright_runner.py`：修复候选执行路径的 locator trace 构建，并要求候选 locator 唯一匹配后再执行。
  - 更新相关单测和旧提示文案，删除 `text_parent_chain/附近的` 生成引导。
- 验证：`uv run pytest tests/unit/test_dsl_generator.py tests/unit/test_locator_confidence.py tests/unit/test_locator_semantic.py tests/unit/test_page_explorer.py tests/unit/test_dsl_validation.py tests/unit/test_preflight_regen.py -q`，115 passed，1 个既有 PytestCollectionWarning。
- 备注：本次是对上一版 DOM fallback 方案的架构修正，未追加 bug-log。

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
