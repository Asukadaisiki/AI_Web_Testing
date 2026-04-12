# Bug 日志

用于沉淀在开发、联调、测试和执行过程中发现的问题，跟踪影响、状态和修复结论。

## 记录规则

- 发现一个明确问题时新增一条记录。
- 状态建议使用：`open`、`in_progress`、`fixed`、`wont_fix`。
- 每条记录尽量包含复现条件、影响范围、定位结论和验证方式。
- 如果问题来自某次任务执行，请回链到 `docs/execution-log.md` 中的对应记录。
- 最新的记录优先放到最上面，方便阅读

## 模板

```md
## BUG-XXX | 标题

- 日期：YYYY-MM-DD
- 状态：fixed
- 来源：需求 / 自测 / 联调 / 线上反馈
- 描述：问题现象
- 复现步骤：
  1. 步骤一
  2. 步骤二
- 影响：功能、页面、模块或用户范围
- 根因：如果尚未定位，写“待定位”
- 处理：修复动作或计划
- 验证：已执行的验证；如果没有写“未验证”
- 关联记录：执行日志日期或链接
```

## BUG-044 | AI Planning 面板缓存失效会话时不会回退创建新会话

- 日期：2026-04-12
- 状态：fixed
- 来源：需求实现 / 静态检查
- 描述：当 `localStorage.ai_planning_last_session` 指向一个已删除或不存在的 session 时，`AITestPlanningPanel` 初始化会先尝试恢复该 session；恢复失败后，因为当前逻辑仍通过“localStorage 是否存在 key”判断是否需要创建新会话，导致页面停留在无活跃 session 状态。
- 复现步骤：
  1. 在浏览器本地存储中写入一个不存在的 `ai_planning_last_session`
  2. 打开 Planning 页面
  3. 观察恢复请求失败后，没有自动切换到其他会话，也没有自动创建新会话
- 影响：删除当前会话或服务端清理历史会话后，用户再次进入 Planning 页面可能无法继续对话，且会话删除功能难以稳定收口
- 根因：初始化分支依赖 localStorage key 是否存在，而不是“恢复是否成功”
- 处理：在会话删除实现计划中引入 `loadSessionDetail()` / `createAndSelectSession()` helper，恢复失败时清理本地缓存并自动创建或切换到可用会话
- 验证：
  - `cd backend && uv run pytest tests/unit/test_ai_planning_api.py -q`
  - `cd frontend && npm run test -- src/services/api.test.ts src/components/AITestPlanningPanel.test.tsx`
  - `cd frontend && npx tsc --noEmit`
- 关联记录：`docs/execution-log.md` 2026-04-12

## BUG-042 | AI 测试规划面板初始化首条消息可能丢失，且新后端回归测试默认不会被跟踪

- 日期：2026-03-30
- 状态：fixed
- 来源：自测
- 描述：实现 AI 测试规划对话助手时发现两个实际缺陷。其一，AITestPlanningPanel 在 planning session 尚未创建完成前允许点击“发送消息”，会导致首条输入被直接忽略；其二，仓库 .gitignore 中存在 tests/ 规则，会让新建的 backend/tests/unit/test_ai_planning_api.py 默认处于未跟踪状态，后续同步时容易遗漏关键回归测试。
- 复现步骤：
  1. 打开工作台后立即在 AI 测试助手输入内容并点击“发送消息”
  2. 观察前端未报错，但首条消息没有进入 transcript，也没有触发后端 planning turn
  3. 新增 backend/tests/unit/test_ai_planning_api.py 后执行 git status --short --untracked-files=all
  4. 观察测试文件最初不会出现在待跟踪列表中
- 影响：AI 测试规划首轮交互不稳定，且新增后端回归测试存在被遗漏进版本控制的风险
- 根因：发送按钮缺少对 session bootstrap 完成状态的约束；仓库忽略规则对任意 tests/ 目录一刀切，未给 backend/tests 留出白名单
- 处理：发送按钮增加 isBootstrapping、sessionId 和空输入约束，导入草案后使用服务端返回结果刷新状态；.gitignore 新增 backend/tests/unit/test_ai_planning_api.py 的定向白名单规则，确保新测试可被跟踪
- 验证：
  - cd frontend && npm run test -- src/components/AITestPlanningPanel.test.tsx
  - git status --short --untracked-files=all | Select-String "test_ai_planning_api.py"
- 关联记录：docs/execution-log.md 2026-03-30 23:15

## BUG-041 | 最新 CRUD 提交存在权限绕过、统计接口运行时失败与删除路径不闭合

- 日期：2026-03-30
- 状态：fixed
- 来源：代码评审 / 最新提交 `7eb71ae`
- 描述：审查最新 CRUD 提交时发现 4 类问题。其一，`cases` 路由和 service 的新增读写接口仅校验”已登录”，没有校验当前用户是否属于目标项目，导致任意已登录用户都能读取、更新、删除其他项目的用例；其二，`GET /api/v1/cases/stats/{project_id}` 声明返回 `ProjectTestCaseStats`，但 service 返回值缺少必填字段 `created_by_user`，路由层会在构造响应模型时直接触发校验错误；其三，`delete_project()` 直接删除项目，但 `test_cases.project_id` 的外键是 `ondelete=”RESTRICT”`，已有用例的项目无法被删除并会在提交时抛出数据库完整性错误；其四，原有 `GET /api/v1/cases` 与 `GET /api/v1/projects` 的响应合同已经变化，但对应单测没有更新，现有测试已失败
- 复现步骤：
  1. 以任意已登录用户访问 `/api/v1/cases`、`/api/v1/cases/project/{project_id}`、`/api/v1/cases/{case_id}`、`PUT /api/v1/cases/{case_id}` 或 `DELETE /api/v1/cases/{case_id}`，观察代码路径中没有项目成员校验
  2. 调用 `/api/v1/cases/stats/{project_id}`，观察 `backend/app/api/routes/cases.py` 会执行 `ProjectTestCaseStats(**stats_data)`，而 `backend/app/services/cases.py` 返回值缺少 `created_by_user`
  3. 创建带 `test_cases` 的项目后调用 `DELETE /api/v1/projects/{project_id}`，观察 `backend/app/services/project_management.py` 直接删除项目，而 `backend/app/models/test_case.py` 将外键定义为 `ForeignKey(“projects.id”, ondelete=”RESTRICT”)`
  4. 执行 `uv run pytest backend/tests/unit/test_cases_api.py -q` 与 `uv run pytest backend/tests/unit/test_projects_and_report_preferences_api.py -q`，观察列表接口断言失败
- 影响：当前提交标称”complete CRUD”，但实际存在越权访问风险、统计接口 500 风险、项目删除不可用风险，以及已存在 API 消费方/测试的兼容性回归
- 根因：新增 CRUD 与分页/统计逻辑时，只补了路由和 service 主干，没有沿项目成员边界、响应 schema、一对多删除约束和历史接口合同做完整联动校验
- 处理：已在 `082ae22` 中全部修复——补齐项目成员权限校验、修正 stats 返回结构、处理外键约束下的项目删除语义、更新测试断言匹配新的分页响应
- 验证：
  - `uv run pytest backend/tests/unit/test_cases_api.py -q`，全部通过
  - `uv run pytest backend/tests/unit/test_projects_and_report_preferences_api.py -q`，全部通过
  - 静态核对 `backend/app/api/routes/cases.py`、`backend/app/services/cases.py`、`backend/app/services/project_management.py`、`backend/app/schemas/cases.py`、`backend/app/models/test_case.py`
- 关联记录：`docs/execution-log.md` 2026-03-30 21:31、2026-03-30 22:00

## BUG-043 | 新增 AI planning 配置字段后，settings API 更新合同未同步，导致现有 PUT /settings/ai 测试与调用方 422

- 日期：2026-04-03
- 状态：fixed
- 来源：任务实现 / 回归测试
- 描述：在为 AI planning 新增 `enable_ai_planning`、`ai_planning_model`、`ai_planning_base_url`、`ai_planning_timeout_ms`、`ai_planning_max_react_rounds` 与密钥字段后，`AISettingsUpdateRequest` 已要求这些字段必填，但原有 `backend/tests/unit/test_ai_settings_api.py` 和若干前端保存配置路径仍沿用旧 payload，未补 planning 字段，触发 `422 Unprocessable Entity`。
- 复现步骤：
  1. 保持新增 planning 字段后的后端 schema 不变
  2. 使用旧版 payload 调用 `PUT /api/v1/settings/ai`
  3. 观察接口返回 422，`test_update_ai_settings_persists_to_env_file_and_allows_clearing_keys` 与 `test_update_ai_settings_accepts_glm_model_family` 失败
- 影响：AI settings 保存链路在 contract 层不一致，新增 planning 配置后会阻断原有 settings 更新回归测试，也容易让前端保存逻辑出现兼容性回退
- 根因：配置 schema 已扩展，但测试样例和部分前端表单/类型没有同步补齐新增字段，形成请求合同漂移
- 处理：补齐后端测试中的 planning 字段；前端 `AISettings` / `AISettingsUpdatePayload`、`AISettingsPage` 表单初始化与保存请求一并纳入 planning 字段，消除 settings 合同漂移
- 验证：
  - `cd backend && uv run pytest tests/unit/test_ai_settings_api.py -q`
  - `cd frontend && npm run test -- src/pages/AISettingsPage.test.tsx src/services/api.test.ts`
- 关联记录：`docs/execution-log.md` 2026-04-03 23:02


