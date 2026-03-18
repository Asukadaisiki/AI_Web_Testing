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

### 2026-03-16 补充

- 已完成 `AI 生成 DSL 最小闭环`：后端新增 `POST /api/v1/dsl/generate`，基于 OpenAI 兼容接口生成 DSL 草案，统一走现有 `DSLCase` schema 强校验，不直接保存、不直接执行。
- 前端 `CaseWorkbenchPage` 已新增“自然语言生成”区域，支持输入需求、生成草案、预览 JSON，并在“替换当前 DSL”与“仅导入步骤”两种导入方式之间选择。
- 新增回归覆盖：后端验证 prompt 只暴露现有 6 个 action、未配置返回 503、非法 JSON / 非法 DSL 返回 502；前端验证生成预览、步骤导入、整单替换与生成失败不污染当前编辑态。
- 文档状态同步收口：此前“浏览器级端到端回归基建未落地”的表述已过时；当前仓库至少已具备本地夹具页浏览器级回归基线，后续重点应转向 AI DSL 深化、prompt 调优与模型治理，而不是重复建设回归基建。
- 已完成 `AI 设置管理入口`：后端已提供 `GET/PUT /api/v1/settings/ai`，前端已提供 `AISettingsPage`，可管理 AI DSL / VLM 运行时配置；后续无需再把“环境配置页”作为未开始事项。
- 已进入 `AI 生成 DSL 深化` 第一批：生成请求支持 `generation_mode / import_mode / current_case / current_steps / preserve_contracts`，后端会输出 `normalization_notes` 与 `generation_meta`，前端工作台可展示自动修正、风险 warning、不可导入错误与三种导入动作；同时 `GET /api/v1/settings/ai/overview` 已提供最小生成观测指标。


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
- AI 生成 DSL 最小闭环：`POST /api/v1/dsl/generate`、工作台自然语言生成入口、草案预览与导入
- `executions overview` 聚合：支持通过率、平均耗时、最近失败、失败分类、按天趋势、失败动作分布、高频失败用例、上一窗口对比与失败根因聚合
- 执行入口深链联动：Dashboard / 报告中心 / 执行中心 / 执行详情之间已支持基于 URL query 与路由 state 的筛选回流
- 前端平台体验打磨 v2.0：执行中心 URL 状态闭环、详情回跳、路由懒加载与构建拆包优化
- Suite 基础闭环 v2.1：Suite CRUD、Suite 工作台、批量执行入口与执行摘要跳转
- 后端与前端最小测试链路

### 进行中

- corrections 运维细化：事件聚合之后的跨修正目标分析、批量治理体验打磨与更明确的状态反馈
- 报告聚合与平台联动体验的小幅细化
- AI 生成 DSL 深化：继续调优 prompt、草案修正策略、生成约束与模型治理

### 未开始或未落地

- 登录页与更完整的平台认证体系
- 默认开启的真实 AI 视觉定位接入与模型配置管理
- 更完整的浏览器级端到端回归矩阵

## 下一里程碑

`v3.4` 延后加固已全部完成（线程安全、JSON 提取健壮化、格式校验、日志收敛、死参数清理）。

下一里程碑当前建议直接收敛为 `AI 生成 DSL 深化`，优先级高于登录体系或默认开启 AI 视觉定位。
详细技术设计参见 [`docs/hybrid-locate-and-intervention-design.md`](./hybrid-locate-and-intervention-design.md)。

下一里程碑建议从以下方向中选取：

- **corrections 运维细化**：跨修正目标分析、批量治理体验打磨、更明确的状态反馈。
- **AI 生成 DSL 深化**：prompt 调优、模型治理、草案修正与更多可控约束。
- **登录页与认证体系**：平台登录入口、用户会话与权限边界。
- **真实 AI 视觉定位接入**：VLM 配置页、默认开启策略与模型管理。


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
