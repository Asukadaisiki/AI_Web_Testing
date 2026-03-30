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

## 当前状态


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
- 状态：open
- 来源：代码评审 / 最新提交 `7eb71ae`
- 描述：审查最新 CRUD 提交时发现 4 类问题。其一，`cases` 路由和 service 的新增读写接口仅校验“已登录”，没有校验当前用户是否属于目标项目，导致任意已登录用户都能读取、更新、删除其他项目的用例；其二，`GET /api/v1/cases/stats/{project_id}` 声明返回 `ProjectTestCaseStats`，但 service 返回值缺少必填字段 `created_by_user`，路由层会在构造响应模型时直接触发校验错误；其三，`delete_project()` 直接删除项目，但 `test_cases.project_id` 的外键是 `ondelete="RESTRICT"`，已有用例的项目无法被删除并会在提交时抛出数据库完整性错误；其四，原有 `GET /api/v1/cases` 与 `GET /api/v1/projects` 的响应合同已经变化，但对应单测没有更新，现有测试已失败
- 复现步骤：
  1. 以任意已登录用户访问 `/api/v1/cases`、`/api/v1/cases/project/{project_id}`、`/api/v1/cases/{case_id}`、`PUT /api/v1/cases/{case_id}` 或 `DELETE /api/v1/cases/{case_id}`，观察代码路径中没有项目成员校验
  2. 调用 `/api/v1/cases/stats/{project_id}`，观察 `backend/app/api/routes/cases.py` 会执行 `ProjectTestCaseStats(**stats_data)`，而 `backend/app/services/cases.py` 返回值缺少 `created_by_user`
  3. 创建带 `test_cases` 的项目后调用 `DELETE /api/v1/projects/{project_id}`，观察 `backend/app/services/project_management.py` 直接删除项目，而 `backend/app/models/test_case.py` 将外键定义为 `ForeignKey("projects.id", ondelete="RESTRICT")`
  4. 执行 `uv run pytest backend/tests/unit/test_cases_api.py -q` 与 `uv run pytest backend/tests/unit/test_projects_and_report_preferences_api.py -q`，观察列表接口断言失败
- 影响：当前提交标称“complete CRUD”，但实际存在越权访问风险、统计接口 500 风险、项目删除不可用风险，以及已存在 API 消费方/测试的兼容性回归
- 根因：新增 CRUD 与分页/统计逻辑时，只补了路由和 service 主干，没有沿项目成员边界、响应 schema、一对多删除约束和历史接口合同做完整联动校验
- 建议处理：
  - 为 case 的创建、查询、列表、统计、更新、删除与批量接口补齐项目成员/所有权校验
  - 修正 `ProjectTestCaseStats` 与 `get_project_test_case_stats()` 的字段契约，保证响应模型可正常构造
  - 明确项目删除策略：禁止删除含用例项目并返回显式业务错误，或先处理关联用例，再提交事务
  - 更新或补充 `backend/tests/unit/test_cases_api.py`、`backend/tests/unit/test_projects_and_report_preferences_api.py`，并新增针对 stats、权限与删除失败语义的测试
- 验证：
  - `uv run pytest backend/tests/unit/test_cases_api.py -q`
  - `uv run pytest backend/tests/unit/test_projects_and_report_preferences_api.py -q`
  - 静态核对 `backend/app/api/routes/cases.py`、`backend/app/services/cases.py`、`backend/app/services/project_management.py`、`backend/app/schemas/cases.py`、`backend/app/models/test_case.py`
- 关联记录：`docs/execution-log.md` 2026-03-30 21:31


