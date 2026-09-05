# 代码库优化执行计划

日期：2026-08-28
依据：`docs/codebase-orphan-architecture-audit-2026-08-28.md`

## 一屏结论

- 优化顺序固定为：**安全止血与迁移恢复 -> 测试门禁可信 -> 确定孤儿清理 -> Planning 解耦 -> 执行核心统一 -> 前端按域收口**。
- 在凭据、迁移链、调试接口和测试基线恢复前，不启动大规模删除或架构拆分。
- 第一里程碑以“可信”为目标：Critical/High 缺陷关闭、前后端默认测试全绿、空库迁移可重复执行。
- 第二里程碑以“减负”为目标：确定孤儿清零，休眠能力全部有 owner、状态和去留期限。
- 第三里程碑以“解耦”为目标：消除 Planning 循环依赖，统一执行事件源，再拆前端业务域。
- 计划按单人维护节奏估算，共约 **25-38 个工程日**；其中前 7-10 个工程日应优先完成可信门禁和低风险清理。

## 目标与非目标

### 目标

1. 消除审计中的 D-01 至 D-11，并为修复建立自动化回归门禁。
2. 删除 O-01 至 O-19 中已确认的孤儿代码，不破坏框架隐式入口和历史迁移。
3. 让 Planning、Runner、Locator、Reporting 的职责和依赖方向可验证。
4. 保持结构化 DSL 是唯一正式执行输入，后端 Runner 是执行结果唯一事实源。
5. 让文档、测试、迁移和配置默认可被 Git 发现并进入 CI。

### 非目标

- 不在本轮引入分布式执行、微服务或新的 Agent 框架。
- 不扩大 DSL 动作集合，不允许自然语言绕过 DSL 校验直接驱动浏览器。
- 不以 VLM 替代 DOM/A11y 主定位路径；VLM 保持受控 fallback。
- 不机械删除“休眠能力”，必须先确认业务 owner、仓外消费者和下线迁移。

## 执行原则

1. 每个任务包独立提交，修复、清理、重构不混在同一提交。
2. 先补回归测试，再改生产逻辑；删除任务至少通过引用扫描和相关测试。
3. 每阶段有明确入口门禁和退出门禁，未满足时不得进入下一阶段。
4. 数据库变更必须包含 Alembic 验证；安全问题必须按已泄露/可被滥用处理。
5. 架构迁移采用绞杀式替换：先定义边界和适配器，再迁调用方，最后删除旧入口。

## 阶段总览

| 阶段 | 目标 | 审计映射 | 预计工期 | 退出门禁 |
|---|---|---|---:|---|
| P0 | 安全止血与仓库可恢复 | D-01、D-02、D-03、D-11 | 2-4 日 | 无明文凭据；空库可升级；调试接口不可匿名访问；关键文件不再被忽略 |
| P1 | 恢复可信测试基线 | D-04 至 D-10 | 3-5 日 | 前后端默认测试全绿；integration 被默认收集；build/lint 通过 |
| P2 | 低风险减负 | O-01 至 O-19 | 2-4 日 | 确定孤儿清零；休眠项有决策记录；主链回归通过 |
| P3 | Planning 域解耦 | 架构问题 1、2、4、7 | 8-12 日 | 无 Planning 循环依赖；应用服务按用例拆分；合同测试通过 |
| P4 | 执行与报告统一 | 架构问题 3 | 5-8 日 | 同步/流式共用状态机与事件源；报告只读结构化结果 |
| P5 | 前端按业务域收口 | 架构问题 5、6 | 5-8 日 | Planning 面板和 API/types 完成按域拆分；路由与认证边界一致 |

工期是单人顺序执行估算，不含外部凭据轮换、历史清理审批和产品决策等待时间。

## P0：安全止血与仓库可恢复

### P0-1 凭据事件处理

- 映射：D-01。
- 动作：
  - 立即轮换 `.claude/settings.local.json` 中出现的凭据，并检查其调用日志和权限范围。
  - 使用 `git rm --cached` 停止跟踪本地设置，保留不含秘密的示例配置。
  - 根据仓库传播范围决定是否使用历史重写工具；历史重写必须单独审批和通知协作者。
  - 增加 secret scanning，至少覆盖提交前检查和 CI。
- 验收：
  - 当前工作树、Git 可达历史扫描和 CI 日志均不再暴露有效凭据。
  - 旧凭据已失效；应用可通过环境变量启动。
- 回滚：代码变更可回滚，凭据轮换不可回滚，只能签发新凭据。

### P0-2 恢复 Alembic 迁移链

- 映射：D-02。
- 依赖：先修复 `.gitignore` 对 migration 的定向忽略。
- 动作：
  - 优先从可信历史恢复 revision `45061d8892d7`，不得仅修改 `down_revision` 绕过缺失迁移。
  - 对恢复迁移的 schema 结果与当前 ORM metadata 做差异检查。
  - 增加 PostgreSQL 空库 `alembic upgrade head` 和升级后 schema smoke test。
- 验收：
  - revision 图只有一个预期 head，不存在断链。
  - 连续两次新建空库升级结果一致；已有开发库可从当前 revision 正常升级。
- 回滚：保留数据库备份；若恢复迁移与线上历史不一致，停止发布并重新核对原 revision。

### P0-3 收紧 locator 调试接口

- 映射：D-03。
- 决策：默认从生产 router 移除；如开发环境确需保留，必须同时满足认证、环境开关、URL allowlist、禁止私网/本机地址、超时与并发限制。
- 验收：
  - 生产配置下接口为 404 或不可注册。
  - 开发配置下匿名请求、私网 URL、重定向到私网和超时场景均被测试拒绝。

### P0-4 修正跟踪策略

- 映射：D-11。
- 动作：
  - 删除 `docs/*`、根 `tests/` 和具体 migration 的宽泛忽略规则。
  - 仅忽略可再生成的报告、截图、缓存和本地秘密。
  - 对审计文档、优化计划、测试和 migration 执行 `git check-ignore` 负向校验。
- 验收：新增文档、测试和 migration 会出现在 `git status --short`；生成物仍保持忽略。

## P1：恢复可信测试基线

### P1-1 修复运行时确定缺陷

| 任务 | 映射 | 实施要点 | 必要测试 |
|---|---|---|---|
| VLM ranker 参数闭环 | D-04 | 将 `model_family` 作为显式参数从调用方传入，禁止依赖未定义局部变量 | candidate ranking 单测，覆盖不同 model family 和失败返回 |
| 新建用例路由 | D-05 | 增加显式 `/cases/new` 路由或 create mode，避免复用数值 ID 编辑入口 | 路由、提交成功、取消返回、非法 ID 测试 |
| AI selector cache 闭环 | D-06 | 先确认 cache key、隔离范围和失效语义；成功定位后写入，否则删除整套伪缓存与指标 | miss/write/hit/invalidate 及跨项目隔离测试 |
| 清理脚本安全边界 | D-07 | 默认 dry-run；保护 default、有 member、有 case 的项目；删除需显式确认 | SQLite 边界测试和“不得删除”用例 |
| DSL 公开符号表 | D-09 | 修正 `__all__`，加入公开 import surface 测试 | 模块导入及导出一致性测试 |

### P1-2 修复测试发现与前端漂移

- 映射：D-08、D-10。
- 动作：
  - 将不依赖真实浏览器/外部服务的 integration 纳入默认 pytest；浏览器与外部 API 测试按 marker 分层。
  - 对 7 个前端失败逐项判定“实现缺陷”或“测试合同过期”，禁止只更新快照掩盖行为变化。
  - 消除 render-time navigate 和 NaN height warning，将其设为测试失败条件。
  - 固化最小 CI：Ruff/Pyflakes、后端 unit+integration、前端 Vitest、TypeScript build、Knip、空库 migration。
- 验收命令：

```bash
cd browser-worker
uv run ruff check .
uv run pytest
uv run alembic upgrade head

cd ../frontend
npm test -- --run
npm run build
npx knip
```

- 验收：默认测试零失败、零未解释 warning；测试收集清单明确包含 integration。

## P2：低风险减负

删除任务按风险拆为四批，每批独立提交并执行对应回归。

| 批次 | 范围 | 审计映射 | 验证 |
|---|---|---|---|
| C1 独立文件与资产 | 后端孤儿模块、前端孤儿 layout、未用 CSS/依赖、临时结果与冗余占位 | O-01、O-13、O-16、O-17、O-18、O-19 | 全仓引用扫描、前端 build/test、后端 import/test |
| C2 DSL/Preflight 残留 | 旧常量、helper、旧 preflight 与未接入 collector | O-02、O-03、O-04、O-05 | DSL validation、generation、preflight 单测 |
| C3 Explorer 旧子图 | prompt formatter、过滤器、旧 flow action 链 | O-06、O-07、O-08 | 删除旧合同测试；保留并覆盖 `_collect_flow_a11y` 动态入口 |
| C4 零调用符号 | Planning 同步 LLM、日志包装、locator/schema/access helper、前端旧 exports | O-09、O-10、O-11、O-12、O-14、O-15 | import surface、Planning SSE、locator、前端交互测试 |

### 删除门禁

1. `rg`、Vulture/Knip 均无生产引用。
2. 明确检查 FastAPI、SQLAlchemy、Alembic、pytest、React lazy import 等隐式入口。
3. 删除测试时记录它验证的是旧实现细节还是仍有效的业务合同；有效合同必须迁移到当前入口。
4. 每批删除后主链 smoke test 通过：创建项目 -> 生成结构化 DSL -> 保存 Case -> 执行 -> 查看报告。

### 休眠能力治理

建立 `capability-status` 清单，每项包含 `owner`、`status`、`external_consumer`、`decision_date`、`remove_after`：

- ORM 休眠表先决定补齐 CRUD 还是通过新 migration 下线。
- 未消费 API 先确认 UI 路线图和仓外消费者，再删除 client/backend endpoint。
- 公开 VLM API 保留一个版本的弃用周期。
- 测试钩子改为私有符号或明确 documented public API。
- `cleanup_orphan_data.py` 修复前禁止执行删除模式。

## P3：Planning 域解耦

### 目标边界

```text
api/routes
  -> application/planning/
       session_service
       conversation_service
       draft_service
       save_execute_service
       analysis_retest_service
  -> ports/
       explorer
       dsl_generator
       case_gateway
       execution_gateway
       event_log
       cache
  -> adapters/
       playwright_explorer
       sqlalchemy_*
       sse_event_log
```

### 迁移顺序

1. 写 ADR，冻结三个语义：
   - M1 的 Planning Session 采用单 active project，还是完整支持多项目。
   - VLM 默认关闭，仅在 DOM/A11y/candidate 路径失败后启用。
   - 同步与流式 API 的支持范围及兼容期限。
2. 定义 ports 的类型和错误合同，先不移动业务逻辑。
3. 抽出 session/conversation CRUD，路由只依赖 application service。
4. 抽出 draft generation 与 preflight，保持结构化 DSL 校验顺序不变。
5. 抽出 save-and-execute、analysis/retest。
6. 将 cache/EventLog 改为 adapter，移除 agent/tool 对 service 私有函数的调用。
7. 删除延迟 import 和旧 facade；增加依赖方向检查。

### 验收

- `services.ai_planning` 与 `ai.test_planning_agent` 不再互相导入。
- application service 不导入 FastAPI route，也不调用其他模块私有符号。
- 每个用例服务有合同测试，SSE 重连/重放和 DSL 校验顺序不回归。
- 原 `ai_planning.py` 仅保留短期兼容 facade，随后删除；不得长期形成第二套入口。

## P4：统一执行核心与报告读模型

### 实施步骤

1. 定义执行状态机与事件 schema：queued、running、step_started、step_finished、intervention、completed、failed、cancelled。
2. Runner 只负责解释已校验 DSL、驱动 Playwright、产生步骤事件和 evidence。
3. Execution Service 统一事务、状态迁移、取消、超时和持久化。
4. 同步 API 等待同一事件流完成；SSE API 转发同一事件流，不再各自实现执行流程。
5. Reporting 从持久化 JSON 构建 read model，不反向控制执行。

### 验收

- 同一 DSL 经同步/流式入口产生相同最终状态、step 顺序和 evidence schema。
- 每个已执行步骤都有 target、candidates、final match 或 failure reason。
- 重试、取消、进程异常和 SSE 断线重连有集成测试。
- 报告页面只读取持久化结果，刷新后与实时结束状态一致。

## P5：前端按业务域收口

### 目标结构

```text
frontend/src/
  app/
  features/
    planning/
    projects/
    cases/
    executions/
    reports/
  shared/
    api/
    ui/
```

### 迁移顺序

1. 从 `AITestPlanningPanel` 抽出纯状态 hook：SSE 生命周期、会话恢复、draft、execution。
2. 将视图按会话、草案、执行拆分，保持页面编排层只组合状态与视图。
3. 按域拆分 `services/api.ts` 和 `types/api.ts`。
4. 从 FastAPI OpenAPI 生成 transport types；手写类型仅保留 UI view model。
5. 抽取共享 project query/mutation，统一 Cases/Reports 的缓存失效和错误处理。
6. 统一认证保护路由，移除 `/login` 的临时跳转语义。

### 验收

- `AITestPlanningPanel` 不再直接实现网络状态机和全部业务视图。
- 页面组件不包含正式执行逻辑，只调用后端执行 API。
- transport types 可由 OpenAPI 重生成且无手工漂移。
- Cases、Planning、Execution、Reports 的主流程路由测试全绿，无渲染期导航和布局 warning。

## 决策清单

以下决策应在 P3/P4 开始前形成 ADR：

| 决策 | 推荐默认值 | 原因 |
|---|---|---|
| Session 项目语义 | M1 强制单 active project | 当前实现大量使用 `project_ids[0]`，先让模型与行为一致 |
| VLM 策略 | 默认关闭，受控 fallback | 符合 DOM-enhanced-first 产品目标并降低不确定性 |
| 调试 locator API | 仅开发环境注册 | 最小化 SSRF 和浏览器资源滥用面 |
| 同步/流式执行 | 单事件源，两个消费者 | 防止状态迁移和 evidence 分叉 |
| AI selector cache | 满足隔离/失效合同后接入，否则删除 | 避免保留不可验证的伪能力 |
| API 类型 | OpenAPI 生成 transport types | 降低前后端合同漂移 |

## 发布与回滚策略

- P0/P1 每项可单独发布；涉及安全和 migration 的任务禁止与功能重构合并。
- P2 每批先记录删除前基线，出现主链回归立即回滚该批，不跨批排查。
- P3/P4 使用兼容 facade 或 feature flag 灰度迁移调用方；新旧路径不得同时写入不同事实源。
- 数据库迁移先在空库和最近生产快照副本验证；生产发布前备份并记录 downgrade 限制。
- 前端域拆分保持 URL 和 API 合同稳定，结构迁移不夹带交互重设计。

## 度量与完成定义

### 每周度量

- Critical/High open 数量。
- 默认 CI 通过率和平均耗时。
- 前端 warning 数、后端未收集测试数。
- Planning 循环依赖数量和私有跨模块调用数量。
- 孤儿代码/未用 export 数量。
- 执行步骤 evidence 完整率。

### 全计划完成定义

1. D-01 至 D-11 均有修复提交、自动化验证和 bug-log 关闭记录。
2. O-01 至 O-19 已删除，或有经过确认的保留理由和到期日期。
3. 空库 migration、后端默认测试、前端测试/build、静态检查全部在 CI 中通过。
4. Planning 无循环依赖；同步/流式执行共享一个状态机和事件源。
5. 前端按业务域组织，OpenAPI transport types 成为前后端合同来源。
6. 主业务闭环 E2E 通过，并为每个执行步骤保留结构化 evidence。

## 建议提交序列

```text
security: rotate leaked credential and restrict locator debug route
fix: restore alembic revision chain
chore: track source docs tests and migrations by default
fix: close audited runtime defects
test: restore backend and frontend quality gates
refactor: remove confirmed orphan modules and exports
refactor: split planning application services
refactor: unify execution lifecycle and event stream
refactor: organize frontend by business domain
docs: close audit findings and record architecture decisions
```
