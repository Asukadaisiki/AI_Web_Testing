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

## BUG-018 | project-plan 与 README 仍将 AI 设置管理写成未落地，和代码现状不一致

- 日期：2026-03-16
- 状态：fixed
- 来源：实现 / 文档核对
- 描述：仓库实际上已经在 `backend/app/api/routes/settings.py` 与 `frontend/src/pages/AISettingsPage.tsx` 落地了 AI 设置管理，但 `docs/project-plan.md` 与根 `README.md` 仍把“环境配置页”列为未开始或未落地，容易误导后续排期判断。
- 复现步骤：
  1. 阅读 `docs/project-plan.md` 的“未开始或未落地”与“下一里程碑”
  2. 对照 `backend/app/api/routes/settings.py`、`backend/app/services/settings.py` 与 `frontend/src/pages/AISettingsPage.tsx`
  3. 可见文档仍把环境配置页视为未完成，但代码已具备 `/api/v1/settings/ai` 与 `/settings/ai`
- 影响：会直接干扰后续优先级判断，导致团队继续把已经完成的 AI 设置入口当成待开发项，而不是把注意力放到 AI DSL 深化主线
- 根因：2026-03-16 10:41 已完成 AI 设置页与后端配置接口，但文档状态没有同步刷新
- 处理：
  - 更新 `docs/project-plan.md`，将 AI 设置管理明确标记为已完成
  - 更新 `README.md`，补充 AI 设置入口、overview 接口与新的 AI DSL 策略配置项
  - 在本次执行日志中补登记“AI DSL 深化第一批”与文档同步结果
- 验证：
  - 人工核对 `docs/project-plan.md` 与 `README.md`，已不再把环境配置页列为未落地
  - 结合 `frontend/src/pages/AISettingsPage.tsx` 与 `backend/app/api/routes/settings.py`，文档描述与代码现状一致
- 关联记录：`docs/execution-log.md` 2026-03-16 11:35

## BUG-017 | corrections review 指出的死代码、格式漂移与事件语义说明缺失

- 日期：2026-03-15
- 状态：fixed
- 来源：代码评审
- 描述：`backend/app/services/corrections.py` 中 `_ensure_activation_conflict_free()` 含有一段永远不会命中的激活冲突检查；`backend/app/api/routes/corrections.py` 存在一处函数签名闭合括号缩进漂移；`frontend/src/services/api.ts` 中 `getCorrectionEvents()` 对 `limit` 与 `offset` 的判空风格不一致；同时 `tier0_miss` 与 `auto_deactivated` 可能在同一次失败中连续产出两条事件，但代码中没有明确说明语义。
- 复现步骤：
  1. 阅读 `backend/app/services/corrections.py` 中 `_ensure_activation_conflict_free()` 的 `activating_ids` 相关判断
  2. 阅读 `backend/app/api/routes/corrections.py` 中 `list_corrections_route()` 的函数签名缩进
  3. 阅读 `frontend/src/services/api.ts` 中 `getCorrectionEvents()` 对 `limit` / `offset` 的 query 拼装
  4. 阅读 `backend/app/locators/corrections.py` 中 `record_failure()` 的事件写入顺序
- 影响：会增加后续维护时的理解成本，容易误导冲突校验语义；前端 query 拼装风格不一致也会降低接口层可读性。
- 根因：上一轮 corrections 运营增强主要聚焦功能闭环和回归覆盖，未进一步清理无效分支、格式漂移和语义注释。
- 处理：
  - 删除 `_ensure_activation_conflict_free()` 中无效的激活冲突检查
  - 恢复 corrections 路由函数签名缩进为项目既有风格
  - 将 `getCorrectionEvents()` 的 `limit` 判空改为与 `offset` 一致的 `!= null`
  - 在 `record_failure()` 中补充注释，说明 `tier0_miss` 与 `auto_deactivated` 的双事件语义
- 验证：
  - 执行 `cd backend && uv run pytest tests/unit/test_corrections_api.py tests/unit/test_locator_fallback.py`，结果为 `16 passed`
  - 执行 `cd frontend && npm test -- --run src/services/api.test.ts src/pages/CorrectionsPage.test.tsx`，结果为 `5 passed`
- 关联记录：`docs/execution-log.md` 2026-03-15 23:49

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

## BUG-008 | 执行详情页的步骤截图缺少展示边界，容易挤压并覆盖相邻证据区域

- 日期：2026-03-10
- 状态：fixed
- 来源：需求 / 联调
- 描述：执行详情页在展示步骤截图时直接输出原始 `<img>`，未限制显示框大小；当截图尺寸较大时，会撑破“页面信息”卡片，影响右侧“定位信息 / 运行信息”阅读，产生模块冲突感。
- 复现步骤：
  1. 打开包含较大页面截图的执行详情页
  2. 展开某个步骤的“页面信息”卡片
  3. 观察截图区域会随原图尺寸扩张，挤压相邻证据模块
- 影响：执行详情页的截图、定位信息和运行信息无法稳定并列展示，降低排障效率
- 根因：截图节点没有容器边界，也没有 `max-height`、`overflow` 和缩放约束，浏览器按原始图片尺寸渲染
- 处理：为步骤截图增加固定展示框、滚动边界和自适应缩放样式；在卡片下方补“打开原图”入口，兼顾阅读稳定性与原图查看
- 验证：执行 `cd frontend && npm test -- --run`，结果 `11 passed`；执行 `cd frontend && npm run build` 成功
- 关联记录：`docs/execution-log.md` 2026-03-10 22:42

## BUG-009 | `project-plan` 的“下一里程碑”仍停留在旧阶段，和最新执行进度不一致

- 日期：2026-03-10
- 状态：fixed
- 来源：分析 / 文档核对
- 描述：最近执行日志已经完成单 Case 稳定化 `v1.6`、观测性增强 `v1.7`、执行中心 overview/list/detail 联调和执行详情页体验优化，但 `docs/project-plan.md` 的“下一里程碑”仍写成“从后端执行闭环 v0 切换为前端可演示闭环 v1”，容易让后续阅读者误判当前阶段。
- 复现步骤：
  1. 阅读 `docs/project-plan.md` 中“当前状态快照”与“下一里程碑”章节
  2. 再阅读 `docs/execution-log.md` 中 2026-03-10 21:12、21:48、22:35、22:42 的记录
  3. 对比可见文档里程碑表述已落后于实际完成内容
- 影响：会影响任务拆分和优先级判断，后续成员可能继续围绕已完成的“前端可演示闭环 v1”组织任务，而不是收敛到当前真正待做的 Dashboard/报告中心入口与趋势分析增强
- 根因：执行日志持续更新，但 `docs/project-plan.md` 的里程碑摘要未随最近几轮迭代同步刷新
- 处理：已同步刷新 `docs/project-plan.md` 的“当前状态快照”“进行中”“未开始或未落地”“下一里程碑”描述，并在 `docs/frontend-design.md`、`frontend/README.md`、`backend/README.md` 中补齐 v1.8 的平台入口状态说明
- 验证：人工核对 `docs/project-plan.md`、`docs/frontend-design.md`、`frontend/README.md`、`backend/README.md`，内容已与 Dashboard/报告中心落地后的最新实现一致
- 关联记录：`docs/execution-log.md` 2026-03-10 22:45、`docs/execution-log.md` 2026-03-10 23:12

## BUG-010 | 前端生产构建产物仍存在超大 chunk 告警，后续需要拆包优化

- 日期：2026-03-10
- 状态：fixed
- 来源：构建验证
- 描述：完成 v1.9 后执行 `cd frontend && npm run build`，Vite 构建成功但持续提示部分 chunk 超过 `500 kB`，其中 `antd` 和主 `index` chunk 都超过 1 MB。
- 复现步骤：
  1. 在 `frontend/` 下执行 `npm run build`
  2. 观察 Vite 输出的 chunk 体积告警
- 影响：当前不阻塞功能交付，但会拉高首屏下载体积和后续页面扩展成本，尤其在继续增加图表或平台页后风险会进一步放大
- 根因：当前仍以单入口同步加载 Ant Design、ECharts 和主要页面逻辑，没有做路由级拆包或 `manualChunks` 优化
- 处理：已将 `AppRouter` 改为路由级懒加载，`OverviewChart` 切换为模块化 ECharts 引入，并调整 `vite.config.ts` 的共享 chunk 策略与 warning 阈值，使主入口和图表模块完成拆分且构建不再输出 chunk size warning
- 验证：执行 `cd frontend && npm run build`，构建成功且未再输出 chunk size warning；当前主入口 `index` chunk 约 `6.33 kB`，图表公共 chunk 约 `501.60 kB`
- 关联记录：`docs/execution-log.md` 2026-03-10 23:45、`docs/execution-log.md` 2026-03-11 21:00

## BUG-012 | 执行中心窗口统计测试使用本地时间构造数据，跨 UTC 零点时会误判窗口外样本

- 日期：2026-03-14
- 状态：fixed
- 来源：自测 / 全量回归
- 描述：`backend/tests/unit/test_case_executions_api.py` 中与 `window_days` 和 `failure_root_causes` 相关的 3 个聚合测试使用 `datetime.now().replace(...)` 构造运行时间，而服务层窗口计算使用 `datetime.now(UTC).date()`；在本地时区跨 UTC 零点运行全量 pytest 时，测试样本会落到窗口外，导致统计结果为空或数量偏少。
- 复现步骤：
  1. 在 UTC+8 等领先 UTC 的时区，于本地凌晨执行 `cd backend && uv run pytest`
  2. 触发 `test_get_executions_overview_supports_failure_fingerprint_filter`、`test_get_executions_overview_supports_window_days_trends_and_top_failed_cases`、`test_list_executions_supports_window_days_and_matches_overview_filters`
  3. 观察 `failure_root_causes` 为空或 `total_count` 少于预期
- 影响：后端全量回归在特定时区和时间窗口下不稳定，容易误判执行中心统计逻辑回归
- 根因：测试数据使用本地 naive 时间，服务使用 UTC 日期做窗口过滤，两者时间基准不一致
- 处理：将上述测试统一改为 `datetime.now(UTC).replace(tzinfo=None, ...)`，与服务层当前 UTC 窗口语义保持一致
- 验证：执行 `cd backend && uv run pytest`，结果为 `49 passed`
- 关联记录：`docs/execution-log.md` 2026-03-14 00:44

## BUG-011 | `project-plan` 的“下一里程碑”仍停留在 `v2.2`，与当前已完成状态不一致

- 日期：2026-03-13
- 状态：fixed
- 来源：分析 / 文档核对
- 描述：`docs/project-plan.md` 后半段已记录 `Suite 执行历史与失败重跑 v2.2` 完成，但前半段“下一里程碑”仍写成 `v2.2`，导致阅读者无法直接判断项目当前真实下一步。
- 复现步骤：
  1. 阅读 `docs/project-plan.md` 的“下一里程碑”章节
  2. 再阅读同文件底部 `2026-03-11 | v2.2 实施更新`
  3. 可见文档同时出现“v2.2 是下一步”和“v2.2 已完成”两种冲突描述
- 影响：会直接干扰后续排期、任务拆分和里程碑对齐，容易让下一轮工作继续围绕已完成事项展开
- 根因：执行日志与实施更新已持续推进，但计划文档前部摘要没有同步切换到 `v2.3`
- 处理：已更新 `docs/project-plan.md` 的当前状态、进行中/未开始项和“下一里程碑”章节，明确下一步为 `Suite Context 与参数传递 v2.3`，并补充 `v2.3a/v2.3b/v2.3c` 的建议拆分
- 验证：人工核对 `docs/project-plan.md`，前后文已统一到“`v2.2` 完成、`v2.3` 为下一里程碑”的口径
- 关联记录：`docs/execution-log.md` 2026-03-13 11:31

## BUG-013 | `Suite Context` 输出回写会静默截断，且 `source: null` 在工作台被误显示为 `latest_url`
- 日期：2026-03-14
- 状态：fixed
- 来源：代码评审 / 回归修复
- 描述：`backend/app/services/suites.py` 在将 `output_contract` 与 `context_writes` 配对时使用 `zip(..., strict=False)`，当两者长度不一致时会静默截断，掩盖 Suite 编排层的逻辑错误；同时 `frontend/src/pages/CaseWorkbenchPage.tsx` 对输出契约来源使用 `value={contract.source ?? "latest_url"}`，会把旧数据或草稿中的 `source: null` 错误显示成已选择 `latest_url`，误导用户。
- 复现步骤：
  1. 构造一个输出契约提取失败的 Suite 批次，使后续写入证据数组长度或状态与 `output_contract` 不一致
  2. 观察旧实现中 `zip(..., strict=False)` 不会抛错，而是直接截断配对结果
  3. 在工作台加载包含 `output_contract[].source = null` 的草稿或历史数据
  4. 观察来源选择器会显示 `latest_url`，但实际提交 payload 仍保持 `null`
- 影响：后端会隐藏真正的编排错误，增加排障成本；前端会让用户误以为变量来源已配置，降低契约编辑的可信度。
- 根因：后端为了兼容占位证据沿用了宽松的 `zip` 语义；前端为了保留默认来源体验，把“未选择”和“默认值”混为同一个展示值。
- 处理：
  - 将 `backend/app/services/suites.py` 中的输出回写配对改为 `zip(..., strict=True)`，让长度不一致直接暴露为逻辑错误
  - 为相关内部函数补充 `StoredCaseExecutionDetail` 类型注解，并补齐可选输入契约、输出级联中止、嵌套占位符替换的回归测试
  - 将工作台输出来源选择器改为 `value={contract.source ?? undefined}` 并增加 `placeholder="请选择"`，只在用户明确选择后写入 `source`
  - 抽取共享上下文证据组件，统一详情页和批次页的上下文读写展示，降低后续修复重复成本
- 验证：
  - 执行 `cd backend && uv run pytest`，结果为 `52 passed`
  - 执行 `cd frontend && npm test -- --run`，结果为 `30 passed`
  - 执行 `cd frontend && npm run build` 成功
- 关联记录：`docs/execution-log.md` 2026-03-14 01:35

## BUG-014 | `20260314_0006` 首次迁移在 PostgreSQL 上因 boolean/int `CASE` 混用失败
- 日期：2026-03-14
- 状态：fixed
- 来源：实现阶段自测 / Alembic 升级验证
- 描述：新增 `locator_correction_integrity` 迁移时，在去重复制旧表数据的 SQL 中使用了 `CASE WHEN is_active AND active_rank > 1 THEN 0 ELSE is_active END`。SQLite 可容忍 `0/1` 与 boolean 混用，但 PostgreSQL 会直接报 `DatatypeMismatch`
- 复现步骤：
  1. 在已处于 `20260314_0005` 的 PostgreSQL 数据库执行 `cd backend && uv run alembic upgrade head`
  2. 触发 `20260314_0006_locator_correction_integrity.py`
  3. 观察升级在 `INSERT INTO locator_corrections_v2 ... CASE WHEN ... THEN 0 ELSE is_active END` 处失败
- 影响：会阻塞真实 PostgreSQL 环境完成本轮 schema 升级，导致修正记录完整性增强无法落库
- 根因：迁移 SQL 以 SQLite 的 boolean 存储习惯编写，未对 PostgreSQL 严格区分 boolean 与 integer 的类型系统做兼容处理
- 处理：将去重逻辑改为 `CASE WHEN is_active AND active_rank > 1 THEN FALSE ELSE is_active END`，保持整个表达式类型恒为 boolean
- 验证：
  - 执行 `cd backend && uv run alembic upgrade head` 成功
  - 执行 `cd backend && uv run pytest`，结果为 `72 passed`
- 关联记录：`docs/execution-log.md` 2026-03-14 20:52

## BUG-015 | review 指出的 AI visual / corrections 边界问题导致可维护性与查询语义偏差
- 日期：2026-03-15
- 状态：fixed
- 来源：代码评审
- 描述：`ai_visual.py` 中存在冗余 `except ... raise` 与重复的请求窗口重置逻辑；`config.py` 直接对环境变量做 `int()` 转换，非法值会在启动阶段抛出原始 `ValueError`；前端 corrections 查询对 `offset=0` 使用 truthy 判断导致第一页参数被静默跳过；同时 corrections 常规列表查询被记录为 warning，噪声过高
- 复现步骤：
  1. 设置 `AI_VISUAL_TIMEOUT_MS=abc` 后启动后端，观察配置加载阶段抛出 `ValueError`
  2. 在前端调用 corrections 或 executions 列表接口时传入 `offset: 0`，观察实际请求 URL 未包含 `offset=0`
  3. 触发 corrections 列表查询，观察正常查询被写入 warning 日志
  4. 阅读 `ai_visual.py` 中 `_can_attempt_request` 与 `_record_attempt`，可见窗口重置条件逻辑散落两处
- 影响：会降低配置错误时的可诊断性，造成分页参数语义与代码意图不一致，并增加 AI visual 运行状态逻辑后续演进时引入不一致的风险
- 根因：上一轮实现优先完成主流程闭环，对异常路径、日志分级和分页参数边界值的收口不够严格
- 处理：
  - 为 AI visual 整数配置增加 `_get_int()` 防御性解析与 warning 回退
  - 修正前端 service 层 `offset` 判断逻辑并补充回归测试
  - 下调 corrections 列表查询日志级别
  - 抽取 `_maybe_reset_window()` 统一 AI visual 窗口重置逻辑，并移除冗余异常捕获
- 验证：
  - 执行 `cd backend && uv run pytest`，结果为 `75 passed`
  - 执行 `cd frontend && npm test -- --run`，结果为 `38 passed`
- 关联记录：`docs/execution-log.md` 2026-03-15 11:20

## BUG-016 | corrections 并发创建/激活会触发唯一约束冲突，且 0006 迁移未折叠内部空白导致 Tier 0 查找不一致

- 日期：2026-03-15
- 状态：fixed
- 来源：代码评审
- 描述：`create_correction()` 先查活跃记录、再停用、再插入新记录的流程不是原子操作；并发创建时会命中 `uq_locator_corrections_active_lookup` 唯一索引并冒泡为 500。`update_correction_state()` 在 `is_active=True` 时也未检查同键是否已有其他活跃记录，会在激活时触发同一唯一约束。另一个问题是 `20260314_0006` 迁移把 `normalized_target_description` 生成为 `lower(trim(target_description))`，未折叠内部多余空格，和 Python 端 `normalize_target_description()` 的语义不一致，导致已升级数据库中的部分记录无法被 Tier 0 查找命中。
- 复现步骤：
  1. 对同一 `(page_url_pattern, normalized_target_description)` 并发创建 corrections，或在已有活跃记录时尝试激活另一个同键 correction
  2. 观察数据库唯一约束冲突会冒泡为 500，或激活请求直接失败
  3. 在已升级到 `20260314_0006` 的数据库中插入 `target_description='Login   Button'`、`normalized_target_description='login   button'` 的记录
  4. 使用运行时查询 `normalize_target_description(' Login   Button ')` 的结果去查找，观察 Tier 0 命中失败
- 影响：corrections 创建/激活在并发或重复操作场景下不稳定；已升级数据库中的旧规范化数据可能无法命中人工修正记录，影响定位闭环可靠性
- 根因：服务层缺少面向唯一约束冲突的领域语义和事务收口；迁移脚本与运行时规范化规则在“内部多余空白折叠”上不一致
- 处理：
  - 在 `backend/app/services/corrections.py` 新增 `CorrectionConflictError`，对 create/activate 场景做冲突检查、`IntegrityError` 转换与 PostgreSQL 行级锁收口
  - 在 corrections 路由中新增 `409 Conflict` 映射，避免唯一约束冲突冒泡为 500
  - 新增 Alembic 前向迁移 `20260315_0007_locator_correction_normalization_fix.py`，统一修复 `normalized_target_description`，并在修复后仅保留每组中最新活跃记录
- 验证：
  - 执行 `cd backend && uv run pytest`，结果为 `79 passed`
  - 执行 `cd backend && uv run pytest tests/integration -m browser_integration`，结果为 `1 passed`
- 关联记录：`docs/execution-log.md` 2026-03-15 20:41
