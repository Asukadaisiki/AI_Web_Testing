# 项目执行计划（从属于核心规划）

## 文档定位

本文件从属于 [AI 自动化测试增强项目规划](./AI%20自动化测试增强项目规划.md)，不单独定义产品方向。

- 核心规划回答“项目要做成什么”。
- 本计划回答“按什么顺序落地、当前做到哪里、下一步做什么”。
- 如果本文件与核心规划冲突，以核心规划为准。

## 与核心规划的对应关系

核心规划中的五层架构，对应到仓库的落地方向如下：

1. Planner 层：`backend/app/schemas`、`backend/app/services`、`backend/app/api/routes`、`backend/app/ai`
2. Locator 层：`backend/app/locators`
3. Executor 层：`backend/app/runners`
4. Reporter 层：`backend/app/reporters`
5. Suite Manager 层：`backend/app/models`、`backend/app/services`、`backend/app/api/routes`、`frontend/src/pages`

当前仓库中的数据库模型、Case API、DSL 校验，都是为这五层能力做基础铺垫，不应替代核心路线本身。

## 当前状态快照

### 2026-03-14 补充

- 已完成 `Suite Context v2.3` 全量闭环：运行时上下文容器、输入/输出契约、变量解析、失败重跑上下文策略，以及前端上下文证据展示都已落地。
- 已完成混合定位闭环的最小可用版本：`locator_corrections`、`needs_intervention`、统一 `resolve_with_fallback()`、执行详情人工干预面板与修正提交重跑链路已打通。
- 已完成混合定位 correctness / data-integrity 修复：修正记录唯一活动约束、大小写无关查找、分页、`PATCH` 语义、URL 泛化和前端类型对齐已经收口。
- 已进入 `v3.4 混合定位稳定化与运营入口`：本轮补齐了修正记录管理页、`page_url` 过滤、runner 与 `db_session` 解耦，以及 AI 视觉定位的超时 / 限流 / 熔断保护。
- 当前主线不再回到 `v2.3`，后续应继续围绕修正运营、混合定位稳定性和真实回归链路推进。

### 2026-03-13 补充

- 已完成 `Suite 执行历史与失败重跑 v2.2`，当前代码里已经具备 `suite_runs`、`suite_run_items`、批次详情与失败重跑链路，`Suite -> Run -> Execution Detail` 的主回看路径已打通。
- 当前真实的下一里程碑应调整为 `Suite Context 与参数传递 v2.3`，不再是 `v2.2`；后续规划应围绕“共享上下文、显式变量引用、失败重跑上下文策略”展开。
- 现有代码边界已经比较清晰：`backend/app/services/suites.py` 负责 Suite 编排，`backend/app/services/executions.py` 负责单 Case 执行，`backend/app/schemas/dsl.py` 仍只有 `name / description / base_url / steps`，尚未定义 Case 输入/输出契约。
- 因此 `v2.3` 的最小改动路径应是“先补契约与批次上下文字段，再补运行时解析与证据展示”，而不是直接改 Runner 或引入第二套执行引擎。
- `混合定位系统 v3.0-v3.3` 仍应排在 `v2.3` 之后推进；如果 `Suite Context` 还未稳定，不宜同时引入新的定位降级链路和人工干预闭环。

### 2026-03-11 补充

- 已完成 `Suite 基础闭环 v2.1`：后端已新增 `GET/POST/PUT /api/v1/suites` 与 `POST /api/v1/suites/{id}/execute`，前端已新增 `Suite 管理` 列表页与 `Suite 工作台`。
- `Suite` 当前采用“同步串行编排 + 复用现有 Case 执行记录”的最小实现：按 `order_index` 顺序执行全部 Case，单条失败后继续执行后续 Case，不单独持久化 `suite_runs`。
- 前端侧边导航已扩展为“仪表盘 / 用例列表 / Suite 管理 / 执行中心 / 报告中心”；Suite 页面支持选择已有 Case、上移/下移调整顺序、保存和直接执行，并展示子执行跳转链接。
- 当前 `v2.1` 已完成最小范围，下一步应收口到 `v2.2`：补 Suite 执行历史批次、失败重跑入口和更明确的 Suite 结果回看链路。
- `Suite Context` 已进入后续规划，但不放在 `v2.1` 当前实现中：应等 Suite 历史批次和批次标识稳定后，再在 `v2.3` 引入跨 Case 变量传递、共享上下文与参数引用，避免过早把 Case 执行和 Suite 编排耦合在一起。
- 已完成 `单 Case 平台体验打磨 v2.0`：`GET /api/v1/executions` 已支持 `window_days=7/14/30`，执行中心把 `window_days / status / case_id / failure_category / failure_fingerprint / page` 统一提升为 URL 查询状态。
- 仪表盘与报告中心中的“最近失败 / 高频失败 / 根因榜”已统一回流到带筛选参数的执行中心；执行详情页现在会优先保留来源执行中心的返回路径，没有来源信息时才回退默认 `/executions`。
- `AppRouter` 已切换为路由级懒加载，`OverviewChart` 已切换到模块化 ECharts 引入；前端构建不再输出 chunk size warning，`BUG-010` 已关闭。

### 2026-03-10 补充

- 已完成 `单 Case 平台化 v1.9`：报告中心已补 `当前窗口 / 上一窗口` 对比、环比摘要与失败根因榜；执行中心已支持通过 `failure_fingerprint` 承接报告中心回流筛选。
- `GET /api/v1/executions/overview` 已支持可选 `window_days=7/14/30`，并补充 `current_window_range`、`previous_window_range`、`previous_window_stats`、`window_comparison` 与 `failure_root_causes` 聚合字段。
- 仪表盘已可展示近 7 天执行趋势、最近失败执行和失败最多用例；报告中心已可展示窗口切换、失败分类分布、失败动作分布、高频失败用例、最近失败跳转与根因回流。
- 已完成 `执行中心与工作台增强 v1.5` 的主范围落地，仍聚焦单 Case 主链路，没有启动 Suite、AI 生成 DSL 或 Vision 定位。
- 执行中心已支持按 `project_id`、`status`、`case_id` 查询，并在列表中展示 `duration_ms`、`total_steps`、`failed_step_index`、`latest_screenshot_url` 等摘要字段。
- 已完成 `单 Case 观测性增强 v1.7`：执行摘要新增 `failure_category`、`failure_step_action`、`latest_url`，并新增 `GET /api/v1/executions/overview` 以输出总数、通过率、平均耗时、最近失败与失败分类聚合。
- 前端执行中心已补总览卡片、失败分类快速筛选和最近失败区；当前执行中心可同时承担“查看明细”和“识别近期问题热点”的入口。
- 执行详情页已支持失败步骤默认展开、成功步骤折叠，以及 console / network 事件按需展开，定位证据可查看候选分数、命中规则、淘汰原因和最终选择原因。
- Locator 已从“首个命中”升级为“候选召回 -> 规则打分 -> 拒绝原因记录 -> 最高分命中”，候选上限固定为 5，便于报告排障。
- 用例工作台已从纯 JSON 编辑升级为“双模式编辑”：默认结构化步骤编辑器，保留原始 JSON 作为高级模式和回退模式，并支持模板插入、增删改、排序、保存和保存并执行。
- 已完成 `单 Case 稳定化 v1.6`：`base_url` 已下沉到用例 DSL，自此相对路径 `goto` 以后端执行请求覆盖值或用例自身 `base_url` 为准，不再以 `EXECUTION_BASE_URL` 作为正式产品默认来源。
- 前端工作台已补“返回用例列表”、执行详情已补“返回执行中心 / 返回用例”，并引入新建页自动恢复草稿、编辑页恢复/丢弃草稿、保存后清理草稿的交互闭环。
- 当前未完成项仍是 Suite 批量执行、AI 生成 DSL、Vision 辅助定位，以及登录页、Suite 管理页、环境配置等更高层平台能力；这些能力继续排在单 Case 稳定性、可观察性与平台入口建设之后。

截至 2026-03-10，当前实现状态如下。

### 已完成

- 前后端单仓目录骨架：`backend/`、`frontend/`、`docs/`
- FastAPI 应用入口、数据库连通性校验、Alembic 初始化链路
- 第一批领域模型与迁移：`users`、`projects`、`test_cases`、`test_suites`、`suite_cases`
- DSL Schema 与校验接口
- Case 持久化接口：创建、列表、详情
- 单 Case 同步执行闭环：`POST /api/v1/cases/{id}/execute`、执行记录查询、Playwright Runner v0
- 执行报告持久化与步骤级基础证据：状态、URL、错误信息、截图路径
- Artifact 只读访问与前端可访问截图 URL
- 前端平台入口：Dashboard、Case 列表页、执行中心、报告中心、报告详情页、Case 工作台
- `executions overview` 聚合：支持通过率、平均耗时、最近失败、失败分类、按天趋势、失败动作分布、高频失败用例、上一窗口对比与失败根因聚合
- 执行入口深链联动：Dashboard / 报告中心 / 执行中心 / 执行详情之间已支持基于 URL query 与路由 state 的筛选回流
- 前端平台体验打磨 v2.0：执行中心 URL 状态闭环、详情回跳、路由懒加载与构建拆包优化
- Suite 基础闭环 v2.1：Suite CRUD、Suite 工作台、批量执行入口与执行摘要跳转
- 后端与前端最小测试链路

### 进行中

- `v3.4` 收尾：本地夹具页真实回归闭环、README / 测试文档与当前代码状态的同步
- 混合定位稳定性回归：围绕 `needs_intervention -> correction -> rerun -> Tier 0 hit` 的主链路补更多浏览器级联调验证
- 报告聚合与平台联动体验的小幅细化

### 未开始或未落地

- AI 生成 DSL
- 登录页、环境配置页
- 默认开启的真实 AI 视觉定位接入与模型配置管理
- 更完整的 corrections 运营能力：批量治理、历史分析、命中趋势
- 浏览器级端到端回归基建
- `v3.4` 延后加固项：
  - `ai_visual.py` 的 `RUNTIME_STATE` 线程安全与并发执行隔离
  - LLM 返回值 JSON 提取健壮性（替代当前简单大括号截取）
  - `schemas/corrections.py` 中按 `correction_type` 区分的 `correction_value` 格式校验
  - corrections 创建/状态更新日志级别从 `WARNING` 收敛到业务操作级别
  - `ai_visual.py` 中未使用的 `deep_locate` 参数清理或正式接入

## 下一里程碑

下一里程碑调整为“混合定位稳定化与运营入口 v3.4”。

- 目标：在既有混合定位闭环上补齐本地真实回归验证，把“能跑”收口为“可重复验证、可持续维护”。
- 范围：本地夹具页、浏览器级 `needs_intervention -> correction -> rerun -> Tier 0 hit` 集成回归、测试组织收口，以及相关 README / 计划文档同步。
- 展示原则：继续复用现有 `Execution Detail -> InterventionPanel -> Corrections` 链路；不在本阶段默认打开真实 AI 模型，不引入新的执行引擎；本轮完成后先进入 corrections 运营增强，再处理 `ai_visual` 线程安全、JSON 提取与 correction schema 校验等延后加固项。

### v2.3 建议执行顺序（2026-03-13）

#### v2.3a：契约与数据层收口

- 先在 `backend/app/schemas/dsl.py` 为 `DSLCase` 增加可选输入/输出契约，而不是直接在自由文本中声明变量。
- 在 `suite_runs` 或等价批次上下文模型中补“上下文快照 / 上下文来源 / 重跑上下文模式”字段，保证变量传递有可追溯的运行容器。
- 扩展执行与批次返回 schema，为前端展示“变量读取、变量写入、解析失败原因”预留结构化字段。
- 验收重点：仅完成 schema、模型、迁移和 API 返回结构，不要求这一阶段就打通真实变量传递。

#### v2.3b：运行时变量解析与失败策略

- 在 `backend/app/services/suites.py` 的批次编排层增加上下文解析，而不是把变量替换逻辑塞进 `playwright_runner.py`。
- 执行顺序保持不变：进入 Suite 批次后，先解析当前 Case 的输入变量，生成运行时 DSL，再复用 `execute_case()`。
- 单个 Case 完成后，把显式声明的输出写回当前批次上下文；若变量缺失、类型不匹配或解析失败，在服务层直接给出明确失败结果。
- 失败重跑需要明确两种策略：`reuse_source_context` 与 `empty_context`；默认策略应可配置但必须落库可追溯。
- 验收重点：至少支持“Case A 写变量，Case B 读变量”的单链路 happy path，以及缺失变量的 fail-fast。

#### v2.3c：前端工作台与批次可观测性

- 在 Case 工作台补输入/输出契约编辑能力，但仍与 DSL 共用同一套校验，不新增旁路配置入口。
- 在 Suite 批次详情页和执行详情页展示变量读取/写入证据、上下文快照摘要和重跑上下文模式。
- 把变量相关失败纳入现有执行详情与报告视图，避免用户只能看后端原始 JSON。
- 验收重点：前端能清楚回答“这个变量从哪里来、在哪里被消费、为什么失败”。

#### v3.0 启动前置条件

- `v2.3` 需要先完成最小自动化测试覆盖，至少包含 schema 校验、Suite 编排、失败重跑上下文策略和前端展示契约。
- `docs/project-plan.md`、`README.md` 与执行日志状态同步后，再切入混合定位系统 `v3.0`，避免两个大主题并行导致文档和实现再次失配。

### 后续规划：Suite Context 与参数传递 v2.3

- 安排位置：放在 `v2.2` 之后，而不是并入当前 `v2.1` 或抢在 `v2.2` 之前。
- 原因：`Suite Context` 本质上依赖稳定的 Suite 批次身份与运行上下文；如果在还没有 `suite_runs` 或等价批次模型时提前加入变量传递，后续历史回看、失败重跑和上下文追踪都会变得混乱。
- 目标：支持在同一次 Suite 运行内，把前一个 Case 的结构化输出写入共享上下文，并在后续 Case 中显式引用。
- 最小范围：
  - 定义 `Suite Run Context` 或等价运行时共享变量容器
  - 为 Case 增加“可选输入参数”与“可选结构化输出”契约
  - 为 DSL 增加受控变量引用能力，例如 `${order_id}` 一类的占位符解析
  - 在执行报告中记录变量写入、读取来源和解析失败原因
- 非目标：
  - 不允许 Case 之间隐式共享浏览器内存状态来替代结构化上下文
  - 不允许 AI 或自由文本直接修改运行时上下文而绕过 DSL 校验
  - 不在第一版 `Suite Context` 中引入复杂条件分支、循环或脚本执行
- 验收标准：
  - 同一 Suite 中可由 Case A 输出变量、Case B 显式引用
  - 变量缺失或类型不匹配时，可在执行前或执行中输出明确错误
  - 失败重跑时能够区分“沿用原批次上下文”与“从空上下文重跑”的策略

## 执行原则

- 所有执行阶段必须围绕核心规划中的五层架构推进。
- 优先打通 `DSL -> Executor -> Evidence -> Report` 主链路，再扩展平台管理能力。
- 平台化管理、调试工作台、AI 增强都不能绕过结构化 DSL 校验。
- 前端只负责平台交互、工作台预览和结果展示，正式执行始终以后端 Runner 为准。
- 前端展示默认基于截图、结构化报告与执行回放信息，而不是远程浏览器窗口串流。
- 先做最小闭环，再做增强能力；先做 DOM 增强，再考虑 Vision。

## 分阶段执行计划

### 阶段 1：基础执行能力

本阶段直接对应核心规划中的“基础执行能力”。

目标：

- 建立可运行的前后端基础工程
- 打通最小执行闭环
- 为后续 Locator、Reporter、Suite 能力提供支撑结构

范围：

- FastAPI 基础工程
- 后端服务启动阶段数据库连通性校验
- 首次运行前执行数据库迁移，确保表结构先于业务接口生效
- React 前端基础工程与页面骨架
- DSL Schema 定义
- Case 持久化模型与基础接口
- Playwright 执行框架
- 基础动作执行：`goto`、`click`、`input`、`wait_for`、`assert_text`、`assert_url_contains`
- 单个 Case 执行
- 截图、基本执行日志、JSON/HTML 初版报告
- 一条登录冒烟链路

交付物：

- 可运行的前后端工程
- 已完成迁移的后端服务
- 可被执行的结构化 DSL 用例
- 最小执行结果页或结果视图
- 初版结构化报告数据

验收标准：

- 启动后端服务时能够在应用创建阶段连通数据库，连库失败时启动直接失败
- 执行迁移后数据库表结构真实生效，再开放依赖数据库的接口
- 能创建或导入一份 DSL 用例
- 能触发单个 Case 执行
- 每一步至少产出截图、状态和失败信息等基础证据
- 非法 DSL 能在执行前被拦截

### 阶段 2：混合定位系统

本阶段直接对应核心规划中的”混合元素定位系统”，对应里程碑 v3.0–v3.3。

目标：

- 建立四层降级定位体系（人工修正 → DOM 语义 → AI 视觉 → 人工干预）
- 提升定位稳定性，建立可解释的定位过程
- 形成修正闭环：失败 → 干预 → 修正 → 重跑命中

范围：

- 人工修正记录数据模型与 CRUD API（v3.0）
- 四层降级定位链路 `resolve_with_fallback`（v3.1）
- AI 视觉定位模块：VLM API、bbox 归一化、deepLocate、DOM 交叉验证（v3.2）
- 前端干预面板与修正提交闭环（v3.3）
- `InterventionNeededError` 异常链路与 `needs_intervention` 状态
- URL 泛化匹配与修正记录置信度追踪

交付物：

- 四层降级定位服务
- AI 视觉定位模块
- 修正记录管理 API
- 前端干预面板
- 定位候选与命中证据

验收标准：

- 有修正记录时 Tier 0 优先命中，跳过后续层
- 语义 target 能命中简单页面元素（Tier 1 现有能力）
- AI 视觉定位对截图中的目标元素能返回正确坐标（Tier 2）
- 全部失败时标记 `needs_intervention`，前端可提交修正
- 修正提交后重跑能命中，`verified_count` 递增
- 报告中可查看候选、评分、命中结果和失败原因

### 阶段 3：测试 DSL 与自然语言生成

本阶段直接对应核心规划中的“测试 DSL 与自然语言生成”。

目标：

- 打通自然语言到结构化 DSL 的编排能力
- 保持 AI 只做辅助，不绕过执行约束

范围：

- 完善 DSL 结构定义
- 建立 `DSL -> Executor` 映射
- 接入 LLM 生成 DSL
- 为手动编辑与 AI 生成共用同一套校验链路

交付物：

- 结构化 DSL 编辑与生成入口
- 统一 DSL 校验与转换服务

验收标准：

- AI 生成 DSL 必须通过 schema 校验
- 同一份 DSL 可被手动编辑、保存、执行
- AI 输出不能绕过核心执行链

### 阶段 4：测试报告系统

本阶段直接对应核心规划中的“执行证据与报告系统”。

目标：

- 提升排障效率
- 为 AI 失败分析提供结构化输入

范围：

- 步骤级证据记录
- 执行轨迹展示
- 报告详情页
- 失败分类
- AI 失败总结

交付物：

- 人类可读报告
- AI 可分析的结构化报告
- 报告列表与详情页

验收标准：

- 每一步均能查看截图、URL、日志、断言结果和失败原因
- 报告支持按任务或执行记录查询
- 结构化报告可供 AI 分析使用

### 阶段 5：测试套件与回归执行

本阶段直接对应核心规划中的“测试套件管理”。

目标：

- 形成 Case/Suite 组织与批量执行能力
- 支撑冒烟与回归测试场景

范围：

- Suite 管理
- 批量执行
- 失败重跑
- 历史结果追踪
- Suite Context / 共享变量 / 跨 Case 参数传递
- 执行中心与回归入口

交付物：

- Suite 管理能力
- 冒烟与回归执行能力
- Suite 运行上下文与参数传递能力
- 历史执行对比

验收标准：

- 能批量执行 Suite
- 能只重跑失败 Case
- 能在同一次 Suite 运行内显式传递结构化变量
- 能追踪历史结果并进行基础对比

## 配套基础事项

以下内容是核心路线的支撑项，不单独作为产品主线：

- 用户与认证
- 项目与空间模型
- 权限边界
- 前端平台壳与路由
- 基础可观测性与日志

这些事项的优先级由核心路线决定，不能喧宾夺主。

## 风险与兜底方案

### 风险 1：自然语言生成不稳定

兜底：

- 早期以手写 DSL 和模板辅助为主
- AI 输出只进入校验链路，不直接进入执行器

### 风险 2：SQLite 与 PostgreSQL 行为差异

兜底：

- 模型设计尽量贴近 PostgreSQL
- 关键 SQL 行为尽早在 PostgreSQL 验证

### 风险 3：定位复杂度过高

兜底：

- 先完成 DOM 增强定位
- Vision 先保留接口或原型验证

### 风险 4：前端过早追求平台视觉完整度

兜底：

- 优先保证工作台、执行结果、报告主链路
- 页面视觉和图表在主链路稳定后再增强

## 验证策略

后端：

- 开发数据库默认连接 PostgreSQL，本地端口使用 `5432`
- 单元测试覆盖 Schema、服务层、基础定位逻辑
- 集成测试覆盖任务创建、执行、报告查询

前端：

- 组件测试覆盖表单、列表、状态展示
- 页面测试覆盖用例创建、执行查看、报告查看

端到端：

- 至少保留一条登录冒烟用例作为主回归链路
## 2026-03-11 | v2.2 实施更新

- 已完成 `Suite 执行历史与失败重跑 v2.2`。
- 后端已新增 `suite_runs`、`suite_run_items` 两张批次表，并通过 Alembic 迁移落库。
- `POST /api/v1/suites/{id}/execute` 已改为先创建 Suite 批次、逐条执行 Case、逐项落库，再回写聚合状态。
- 已新增 `GET /api/v1/suites/{id}/runs`、`GET /api/v1/suites/{id}/runs/{run_id}` 与 `POST /api/v1/suites/{id}/runs/{run_id}/rerun-failed`。
- `失败重跑` 当前默认重跑“当前 Case 最新 DSL”，不会回放历史 DSL 快照；该策略是 v2.2 的最小实现。
- 前端已新增 `SuiteRunDetailPage`，`SuitesPage` 会展示最近批次摘要，`SuiteWorkbenchPage` 会展示最近批次列表并在执行后跳转到批次详情页。
- `ExecutionDetailPage` 已支持优先返回来源 Suite 批次；无来源时仍回到执行中心。
- `v2.3` 继续保持为 `Suite Context 与参数传递`，不在本轮引入上下文共享、变量占位符解析或跨 Case 状态复用。

## 后续规划：混合定位系统 v3.0–v3.3

本系列里程碑直接对应核心规划"阶段二：混合定位系统"，安排在 `v2.3` 之后。

详细技术设计参见 [`docs/hybrid-locate-and-intervention-design.md`](./hybrid-locate-and-intervention-design.md)。

### v3.0: 数据模型与修正记录 API

- 目标：为四层降级定位建立数据基础和修正记录管理能力。
- 范围：
  - 新建 `LocatorCorrection` 数据库模型（`locator_corrections` 表）+ Alembic 迁移
  - `ExecutionStatus` 扩展：新增 `"needs_intervention"` 状态
  - 新增 schema：`InterventionRequest`、`DOMElementSnapshot`、`AILocateCandidate`
  - `StepExecutionEvidence` 扩展：新增 `intervention_request` 字段
  - 修正记录 CRUD API：`POST/GET /api/v1/corrections`、`PUT /{id}/deactivate`
  - URL 泛化工具 `url_pattern.py`：动态路径段（数字 ID、UUID、长随机串）自动替换为 `*`
  - `find_active_correction()` 查找函数：按 `page_url_pattern + target_description` 匹配活跃修正
- 交付物：
  - 可通过 API 创建、查询、停用修正记录
  - 数据库迁移脚本可独立执行
- 验收标准：
  - `locator_corrections` 表可通过 Alembic 迁移创建
  - 修正记录 API 可正常增删查
  - URL 泛化覆盖数字 ID、UUID、长随机串
- 文件变更：
  - 新建：`backend/app/models/locator_correction.py`、`backend/app/locators/url_pattern.py`、`backend/app/locators/corrections.py`、`backend/app/api/routes/corrections.py`、Alembic 迁移
  - 修改：`backend/app/schemas/executions.py`、`backend/app/models/__init__.py`、`backend/app/api/routes/__init__.py`

### v3.1: 四层降级定位链路 (Tier 0 + Tier 3)

- 目标：在现有 DOM 语义定位前后接入 Tier 0（修正记录优先查找）和 Tier 3（人工干预标记），形成完整的降级链路骨架。
- 范围：
  - 实现 `resolve_with_fallback()` 定位入口，替换 Runner 中现有 `resolve_semantic_locator()` 直接调用
  - Tier 0：在定位最前端查询 `locator_corrections`，命中则直接使用修正的 selector，并更新 `verified_count`；连续失败 3 次自动停用
  - Tier 3：当所有层都失败时，采集完整上下文（截图、URL、DOM 快照、AI 候选、Tier 1 trace），抛出 `InterventionNeededError`
  - 定义 `InterventionNeededError` 与 `RunnerInterventionError`
  - 服务层捕获 `RunnerInterventionError`，将 execution 状态设为 `needs_intervention`
  - DOM 快照采集：提取页面所有可交互元素的 tag、text、role、aria-label、rect 等信息
- 交付物：
  - Runner 定位调用统一走 `resolve_with_fallback`
  - 有修正记录时自动命中；全部失败时标记 `needs_intervention`
- 验收标准：
  - 有活跃修正记录时 Tier 0 命中，跳过后续层
  - 修正 selector 失效时 `consecutive_failures` 递增，3 次后自动停用
  - 全部失败时 execution 状态为 `needs_intervention`，step evidence 包含完整 `intervention_request`
- 文件变更：
  - 修改：`backend/app/runners/playwright_runner.py`、`backend/app/services/executions.py`、`backend/app/locators/__init__.py`

### v3.2: AI 视觉定位 (Tier 2)

- 目标：实现基于 VLM 截图的视觉元素定位，作为 DOM 语义定位的补充层。
- 范围：
  - 新建 `ai_visual.py`：核心 AI 视觉定位模块
  - VLM API 调用：基于 OpenAI 兼容接口，支持 qwen-vl、gemini、gpt-4o、qwen2.5-vl
  - bbox 归一化适配：不同模型返回不同坐标格式（归一化 0–1000 / 像素坐标 / xy 交换），统一转为像素坐标
  - deepLocate 两阶段定位（参考 Midscene）：先粗定位区域 → 扩展搜索区域（至少 400×400）→ 裁剪放大 2× → 精确定位 → 坐标回算
  - AI 定位结果与 DOM 交叉验证：`elementFromPoint` 获取 DOM 元素，验证语义匹配
  - 将 Tier 2 接入 `resolve_with_fallback`，在 Tier 1 失败后调用
- 交付物：
  - AI 视觉定位模块可独立调用
  - Runner 降级链路 Tier 1 失败后自动尝试 Tier 2
- 验收标准：
  - 对截图 + 目标描述能返回 bbox 和中心坐标
  - deepLocate 模式下精度优于单阶段
  - 不同模型族的 bbox 格式均能正确归一化
  - Tier 2 命中后能生成可复用的 Playwright locator
- 文件变更：
  - 新建：`backend/app/locators/ai_visual.py`
  - 修改：`backend/app/runners/playwright_runner.py`（Tier 2 接入）
- 依赖：需安装 `openai` SDK 和 `Pillow`（图片裁剪缩放）

### v3.3: 前端干预闭环

- 目标：在前端为 `needs_intervention` 状态的失败步骤提供可视化干预面板，完成修正提交 → 重跑 → Tier 0 命中的完整闭环。
- 范围：
  - 执行详情页：当 step 有 `intervention_request` 时显示干预面板
  - 干预面板交互：
    - 展示失败时的页面截图（用户可点击指定位置）
    - 手动输入选择器（css / xpath / test_id）
    - 展示 DOM 快照中的候选可交互元素列表（tag、text、role、visible、enabled）
    - 提交修正按钮 → 调用 `POST /api/v1/corrections`
  - 提交修正后引导用户重跑 → Tier 0 命中 → 验证闭环
  - （可选，后续迭代）独立的修正记录管理页：查看所有修正、筛选、手动启用/停用
- 交付物：
  - 执行详情页干预面板组件
  - 修正提交到重跑成功的完整闭环
- 验收标准：
  - `needs_intervention` 状态的步骤在详情页展示干预面板
  - 用户可通过面板提交修正记录
  - 重跑后 Tier 0 命中修正的 selector，步骤执行成功
  - 修正记录的 `verified_count` 正确递增
- 文件变更：
  - 修改：`frontend/src/pages/ExecutionDetailPage.tsx`（新增干预面板组件）
  - 可选新建：`frontend/src/components/InterventionPanel.tsx`、`frontend/src/pages/CorrectionsPage.tsx`
