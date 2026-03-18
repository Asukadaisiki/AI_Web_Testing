# 项目执行计划（从属于核心规划）

## 文档定位

本文件从属于 [AI 自动化测试增强项目规划](./AI%20自动化测试增强项目规划.md)，不单独定义产品方向。

- 核心规划回答"项目要做成什么"。
- 本计划回答"按什么顺序落地、当前做到哪里、下一步做什么"。
- 如果本文件与核心规划冲突，以核心规划为准。

## 与核心规划的对应关系

核心规划中的五层架构，对应到仓库的落地方向如下：

1. Planner 层：`backend/app/schemas`、`backend/app/services`、`backend/app/api/routes`、`backend/app/ai`
2. Locator 层：`backend/app/locators`
3. Executor 层：`backend/app/runners`
4. Reporter 层：`backend/app/reporters`
5. Suite Manager 层：`backend/app/models`、`backend/app/services`、`backend/app/api/routes`、`frontend/src/pages`

## 当前状态快照（截至 2026-03-18）

### 已完成

- 前后端单仓目录骨架、FastAPI 入口、数据库连通性校验、Alembic 初始化
- 领域模型与迁移：`users`、`projects`、`test_cases`、`test_suites`、`suite_cases`
- DSL Schema 与校验接口、Case 持久化接口（创建、列表、详情）
- 单 Case 同步执行闭环、Playwright Runner v0、执行报告持久化与步骤级证据
- Artifact 只读访问与前端可访问截图 URL
- 前端平台入口：Dashboard、Case 列表/工作台、Suite 列表/工作台/批次详情、执行中心、执行详情、报告中心、修正记录、AI 配置
- `executions overview` 聚合（通过率、平均耗时、失败分类、按天趋势等）
- 执行入口深链联动（Dashboard / 报告 / 执行中心之间的 URL 筛选回流）
- 前端体验打磨 v2.0：执行中心 URL 状态闭环、路由懒加载与构建拆包
- Suite 基础闭环 v2.1：Suite CRUD、工作台、批量执行、批次详情与失败项重跑
- 后端与前端最小测试链路
- 混合定位系统 v3.0–v3.3 最小闭环：修正记录模型/API、四层降级定位 `resolve_with_fallback`、AI 视觉定位模块、前端干预面板、corrections 管理页
- v3.4 延后加固全部完成：`RUNTIME_STATE` 线程安全、JSON 提取健壮化、`correction_value` 格式校验、日志收敛、`deep_locate` 死参数清理
- corrections 运营增强：事件模型、overview/events/bulk API、前端命中趋势图与批量启停、并发安全修复
- AI 生成 DSL 最小闭环：`POST /api/v1/dsl/generate`、工作台自然语言生成入口、草案预览与导入
- AI 设置管理入口：`GET/PUT /api/v1/settings/ai`、`AISettingsPage`
- AI 生成 DSL 深化第一批：生成模式/上下文/导入控制、自动修正与 `normalization_notes`、生成观测概览
- AI DSL 反馈闭环：`feedback_status`（pending/accepted/rejected）、采纳/放弃上报、概览聚合
- AI DSL 治理与观测闭环：生成记录多维筛选、详情查询、结构化拒绝原因、prompt 版本审计、前端治理表格与详情抽屉

### 进行中

- AI 生成 DSL 持续深化：prompt 调优、草案修正策略、生成约束迭代（基于治理数据驱动）

### 未开始

- AI 视觉定位 `_deep_locate` 两阶段精度优化（设计文档 2.3 节）：先粗定位区域 → 裁剪放大 → 精确定位 → 坐标回算，含 `_locate_section`、`_expand_search_area`、`_crop_and_scale`
- 默认开启的真实 AI 视觉定位接入与 VLM 模型配置管理页
- 登录页与平台认证体系
- 更完整的浏览器级端到端回归矩阵

## 下一里程碑

当前主线为 **AI 生成 DSL 持续深化**，基于已有治理数据（`top_rejection_reasons` / `model_outcome_breakdown` / `generation_mode_breakdown`）驱动 prompt 优化。

后续可选方向：

- **登录页与认证体系**：平台登录入口、用户会话与权限边界。
- **真实 AI 视觉定位接入**：VLM 配置页、默认开启策略与模型管理。
- **corrections 运维继续细化**：跨修正目标分析、更明确的状态反馈。

混合定位系统技术设计参见 [`docs/hybrid-locate-and-intervention-design.md`](./hybrid-locate-and-intervention-design.md)。

## 执行原则

- 所有执行阶段必须围绕核心规划中的五层架构推进。
- 优先打通 `DSL -> Executor -> Evidence -> Report` 主链路，再扩展平台管理能力。
- 平台化管理、调试工作台、AI 增强都不能绕过结构化 DSL 校验。
- 前端只负责平台交互、工作台预览和结果展示，正式执行始终以后端 Runner 为准。
- 先做最小闭环，再做增强能力；先做 DOM 增强，再考虑 Vision。

## 验证策略

- 后端：PostgreSQL（端口 5432），单元测试覆盖 Schema/服务层/定位逻辑，集成测试覆盖任务创建/执行/报告
- 前端：组件测试覆盖表单/列表/状态展示，页面测试覆盖用例创建/执行/报告
- 端到端：至少保留一条登录冒烟用例作为主回归链路

## 风险与兜底

1. **自然语言生成不稳定**：AI 输出只进入校验链路，不直接进入执行器
2. **SQLite 与 PostgreSQL 行为差异**：模型设计贴近 PostgreSQL，关键 SQL 行为尽早验证
3. **定位复杂度过高**：先完成 DOM 增强定位，Vision 先保留接口或原型验证
4. **前端过早追求视觉完整度**：优先保证工作台、执行结果、报告主链路
