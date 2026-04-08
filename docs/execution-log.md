# 执行日志

用于沉淀每次任务实际做了什么，方便后续追溯、复盘和回答一致化。

## 记录规则

- 每次处理需求后按时间倒序追加一条记录。
- 记录”目标、操作、结果、验证、后续”，避免只写结论。
- 如果执行过程中发现缺陷，同时在 `docs/bug-log.md` 追加对应条目并互相引用。
- 最新的记录优先放到最上面，方便阅读。

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
