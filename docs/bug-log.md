# Bug 日志

用于沉淀在开发、联调、测试和执行过程中发现的问题，跟踪影响、状态和修复结论。

## 记录规则

- 发现一个明确问题时新增一条记录。
- 状态建议使用：`open`、`in_progress`、`fixed`、`wont_fix`。
- 每条记录尽量包含复现条件、影响范围、定位结论和验证方式。
- 如果问题来自某次任务执行，请回链到 `docs/execution-log.md` 中的对应记录。

## 模板

```md
## BUG-XXX | 标题

- 日期：YYYY-MM-DD
- 状态：open
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

## BUG-001 | SQLite 测试种子数据插入顺序触发外键失败

- 日期：2026-03-09
- 状态：fixed
- 来源：自测
- 描述：新增项目/用户/成员关系模型后，`pytest` 在测试夹具初始化阶段插入 `project_members` 时报 `FOREIGN KEY constraint failed`。
- 复现步骤：
  1. 使用临时 SQLite 数据库执行测试夹具建表与种子插入
  2. 在同一批次 flush 中插入 `users`、`projects`、`project_members`
- 影响：`backend` API 测试、模型测试与健康检查测试全部无法启动
- 根因：启用 SQLite 外键校验后，测试夹具未先持久化父记录，导致成员关系记录在约束检查时找不到对应的用户和项目
- 处理：在 `tests/conftest.py` 中先对 `users` 与 `projects` 执行 `flush()`，再插入 `project_members`
- 验证：执行 `cd backend && uv run pytest`，结果 `14 passed`
- 关联记录：`docs/execution-log.md` 2026-03-09 00:22

## BUG-002 | 配置的 PostgreSQL 数据库名不存在导致启动与迁移失败

- 日期：2026-03-09
- 状态：fixed
- 来源：自测
- 描述：后端当前 `DATABASE_URL` 指向 `ai_web_testing`，但本地 PostgreSQL 中不存在该数据库，导致应用创建阶段连库失败，Alembic 迁移也无法执行。
- 复现步骤：
  1. 在 `backend/.env` 中使用 `postgresql+psycopg://postgres:123456@127.0.0.1:5432/ai_web_testing`
  2. 执行 `cd backend && uv run python -c "from app.main import create_app; create_app()"`
  3. 或执行 `cd backend && uv run alembic upgrade head`
- 影响：后端服务无法启动；数据库迁移无法初始化；依赖真实开发库的联调被阻塞
- 根因：本地 PostgreSQL 服务可达，账号密码有效，但目标库 `ai_web_testing` 尚未创建；现有实例中仅探测到 `easytest_dev`、`postgres`、`template0`、`template1`
- 处理：已连接 `postgres` 默认库并创建 `ai_web_testing` 数据库，随后重新执行连库校验、Alembic 初始化和服务启动验证
- 验证：`verify_database_connection()` 成功；`alembic upgrade head` 成功；`/api/v1/health` 返回 `200`
- 关联记录：`docs/execution-log.md` 2026-03-09 00:52、`docs/execution-log.md` 2026-03-09 00:56
## BUG-003 | 规划文档与当前实现状态缺少“已完成/规划中”区分，易造成结构认知偏差

- 日期：2026-03-09
- 状态：fixed
- 来源：需求
- 描述：规划文档和前端设计文档描述的是目标态平台能力，但仓库当前仅实现了后端阶段 1 的部分内容、前端仍以骨架目录为主；由于缺少“当前状态”说明，阅读仓库时容易误判为项目结构已偏离规划
- 复现步骤：
  1. 阅读 `docs/AI 自动化测试增强项目规划.md`、`docs/project-plan.md`、`docs/frontend-design.md`
  2. 对照 `frontend/package.json`、`frontend/src/` 和 `backend/app/api/router.py`
  3. 观察到文档中已描述登录、仪表盘、执行中心、报告中心、React Router、TanStack Query、Ant Design、ECharts 等内容，但实际仓库未落地对应前端实现与依赖
- 影响：新加入开发者难以快速判断哪些是目标规划、哪些是当前已完成内容，容易在需求评审、任务拆分和排期时形成误判
- 根因：当前文档偏“目标蓝图”，缺少“当前里程碑状态”或“完成度标注”；README 也仍使用 skeleton 口径，和部分后端已实现内容存在信息滞后
- 处理：已将 `docs/AI 自动化测试增强项目规划.md` 明确为核心规划文档；重写 `docs/project-plan.md` 作为从属执行计划；更新 `docs/frontend-design.md`、`README.md`、`backend/README.md`、`frontend/README.md`，统一“核心规划优先、当前状态单独说明”的口径
- 验证：人工核对更新后的文档，已明确区分核心规划、执行计划、前端目标设计和当前实现状态
- 关联记录：`docs/execution-log.md` 2026-03-09 01:19、`docs/execution-log.md` 2026-03-09 01:31

## BUG-004 | Ant Design 组件在 Vitest/jsdom 下缺少浏览器 API 模拟导致前端测试无法运行

- 日期：2026-03-09
- 状态：fixed
- 来源：自测
- 描述：引入 Ant Design 表格、描述列表和栅格后，前端页面测试在 Vitest/jsdom 环境下报 `window.matchMedia is not a function` 与 `window.getComputedStyle` 相关异常，导致页面无法渲染。
- 复现步骤：
  1. 在 `frontend/` 下执行 `npm test`
  2. 渲染包含 `Table`、`Descriptions`、`Row/Col` 的页面组件
- 影响：前端页面测试全部失败，无法验证“Case 列表 / 执行列表 / 报告详情”的最小演示链路
- 根因：Ant Design 的响应式与表格内部逻辑依赖浏览器环境中的 `matchMedia` 与完整 `getComputedStyle` 行为，而 jsdom 默认未提供这些能力
- 处理：在 `frontend/src/setupTests.ts` 中补充 `matchMedia` 与 `getComputedStyle` 的测试环境兼容桩，恢复 Vitest 下的组件渲染能力
- 验证：执行 `cd frontend && npm test`，结果 `3 passed`
- 关联记录：`docs/execution-log.md` 2026-03-09 21:35

## BUG-005 | Vite 开发服务器在当前 Windows 环境下仅监听 IPv6 回环地址，导致浏览器访问 localhost 被拒绝

- 日期：2026-03-09
- 状态：fixed
- 来源：联调
- 描述：前端执行 `npm run dev` 后，浏览器访问 `localhost` 提示拒绝连接，本机 `Invoke-WebRequest` 访问 `127.0.0.1:5173` 也失败。
- 复现步骤：
  1. 在 `frontend/` 下执行 `npm run dev`
  2. 访问 `http://localhost:5173` 或 `http://127.0.0.1:5173`
  3. 观察开发服务未正常响应
- 影响：前端开发环境无法直接通过浏览器访问，阻塞本地联调
- 根因：Vite 在当前 Windows 环境下默认仅监听 `::1:5173`，没有同时绑定 `127.0.0.1:5173`，浏览器或命令行按 IPv4 路径访问时被拒绝
- 处理：在 `frontend/vite.config.ts` 中显式设置 `server.host = "127.0.0.1"`，并同步设置 `preview.host`
- 验证：以 `vite --host 127.0.0.1` 启动后，监听地址变为 `127.0.0.1:5173`，HTTP 访问恢复正常
- 关联记录：`docs/execution-log.md` 2026-03-09 21:47

## BUG-006 | 工作台模式切换控件在 Vitest/jsdom 下交互不稳定，导致 JSON/结构化编辑切换测试失败

- 日期：2026-03-10
- 状态：fixed
- 来源：自测
- 描述：`CaseWorkbenchPage` 初版使用 `Radio.Group` 风格的模式切换控件后，Vitest/jsdom 中切换到“原始 JSON”或切回“结构化编辑”不稳定，导致工作台主链路测试持续失败。
- 复现步骤：
  1. 在 `frontend/` 下执行 `npm test`
  2. 运行 `src/pages/CaseWorkbenchPage.test.tsx`
  3. 观察“结构化步骤编辑支持应用模板、切换 JSON 并保存执行”用例在模式切换后断言失败
- 影响：前端测试无法稳定覆盖工作台双模式编辑主链路，影响保存并执行链路的发布前验证
- 根因：该类 Ant Design 切换控件在 jsdom 中依赖隐藏 input、label 和 pointer events 的事件传递，测试环境下稳定性不足；同时原测试对整段 JSON 精确值过于敏感
- 处理：将工作台模式切换改为显式按钮切换，保留原有状态切换逻辑；同时将测试断言收敛为对 JSON 编辑器出现及关键 DSL 片段的校验
- 验证：执行 `cd frontend && npm test`，结果 `6 passed`
- 关联记录：`docs/execution-log.md` 2026-03-10 21:12

## BUG-007 | 相对 URL 的 `goto` 未绑定用例级地址时会在首步全部失败

- 日期：2026-03-10
- 状态：fixed
- 来源：联调 / 自测
- 描述：当前真实执行记录中的用例使用相对路径 `goto`（如 `/login`、`/`），但旧实现把地址来源放在执行请求 `base_url` 或后端 `EXECUTION_BASE_URL`，而不是用例本身，导致未显式传参时所有这类用例都在第 1 步失败。
- 复现步骤：
  1. 创建未配置用例级 `base_url`、但包含 `{"action": "goto", "value": "/login"}` 或 `{"action": "goto", "value": "/"}` 的用例
  2. 不在执行请求中传入 `base_url`
  3. 查看执行记录与截图
- 影响：现有相对 URL 用例无法作为可维护测试资产长期复用，执行报告会全部在首步失败；用户也容易误判为 Runner、截图或报告展示故障
- 根因：URL 归属设计不对，执行地址被放在请求期或后端环境变量，而不是 DSLCase 本身；相对路径 `goto` 因此无法稳定复用
- 处理：已将 `base_url` 下沉到 DSLCase / Case 表单；执行优先级改为“执行请求 `base_url` 覆盖值 > 用例自身 `base_url`”；若两者都缺失则在 service 层直接返回明确失败信息，不再依赖 `EXECUTION_BASE_URL`
- 验证：执行 `cd backend && uv run pytest`，结果 `31 passed`；执行 `cd frontend && npm test`，结果 `10 passed`
- 关联记录：`docs/execution-log.md` 2026-03-10 21:20、`docs/execution-log.md` 2026-03-10 21:48
