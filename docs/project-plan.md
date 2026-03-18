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

#### 混合定位精度优化（Locator 层增强）

以下五项按依赖关系和投入产出比排序，建议按序推进：

**P0 — elementFromPoint 遮挡穿透**
- 问题：`elementFromPoint` 只返回 z-index 最顶层元素，toast / loading overlay / cookie banner 会遮挡目标
- 方案：检测返回元素是否为常见遮挡物（`role="alert"`, `class*="overlay"`, `class*="toast"` 等），命中时改用 `elementsFromPoint` 取整个 z-stack 逐个匹配
- 涉及文件：`backend/app/locators/fallback.py` (`_snapshot_dom_element_at_point`)
- 投入：小 | 收益：高（消除一类系统性误判）

**P1 — DOM 交叉验证增强**
- 问题：当前 `_dom_snapshot_matches_target` 做严格 token 子集检查，过于刚性（"删除"能匹配"删除账户"，但"提交订单"无法匹配 aria-label="提交"）
- 方案：引入 Jaccard 相似度阈值（如 ≥ 0.5）作为模糊匹配兜底；对中文 target 增加分词粒度（单字 token 回退）
- 涉及文件：`backend/app/locators/fallback.py` (`_dom_snapshot_matches_target`, `_tokenize`)
- 投入：小 | 收益：中（减少 AI 定位正确但验证误拒的情况）

**P2 — AI 视觉定位 `deep_locate` 两阶段精度优化**
- 设计文档 2.3 节已完整描述，当前 `ai_visual.py` 未实现
- 阶段 1：VLM 粗定位区域（section）→ 扩展搜索区域（至少 400×400）→ 裁剪放大 2× → 阶段 2 精确定位 → 坐标回算
- 需实现：`_locate_section`、`_expand_search_area`、`_crop_and_scale`（依赖 Pillow）
- 涉及文件：`backend/app/locators/ai_visual.py`
- 投入：中 | 收益：高（密集页面定位精度显著提升，参考 Midscene 核心优化策略）

**P3 — Tier 1 + Tier 2 融合（DOM 候选检索 + VLM 排序）**
- 问题：当前 Tier 1 和 Tier 2 完全串行独立，Tier 1 多候选分数接近时直接放弃进入纯视觉定位
- 方案：当 Tier 1 找到多个候选且最高分与次高分差距 < 阈值时，在截图上标注候选位置（画框/编号），发送给 VLM 做排序选择，直接复用 DOM locator
- 优势：候选已是合法 DOM 元素，不存在 `elementFromPoint` 反查失败的问题；比纯 bbox 定位更可靠
- 涉及文件：`backend/app/locators/fallback.py` (新增 `_try_vlm_rank_candidates`)、`backend/app/locators/ai_visual.py` (新增排序 prompt)
- 投入：中 | 收益：高（本质上是方式一"DOM 候选 + VLM 排序"与方式二的融合）

**P4 — AI 定位结果缓存**
- 问题：同一页面同一 target 重复出现时（循环操作场景），每次都调用 VLM
- 方案：在 `resolve_with_fallback` 中增加 `(page_url_pattern, target) → selector` 的内存缓存（LRU），AI 定位成功后写入缓存，后续命中时直接用缓存的 selector；失效时清除
- 与 Tier 0 修正记录的区别：缓存是会话级自动产生、无需人工介入；修正记录是跨会话持久化、人工提交
- 涉及文件：`backend/app/locators/fallback.py` (缓存层)、`backend/app/locators/ai_visual.py`
- 投入：小 | 收益：中（减少重复 VLM 调用，降低延迟和成本）

#### 其他未开始项

- 默认开启的真实 AI 视觉定位接入与 VLM 模型配置管理页
- 登录页与平台认证体系
- 更完整的浏览器级端到端回归矩阵

## 下一里程碑

当前主线为 **AI 生成 DSL 持续深化**，基于已有治理数据（`top_rejection_reasons` / `model_outcome_breakdown` / `generation_mode_breakdown`）驱动 prompt 优化。

DSL 深化告一段落后，建议进入 **混合定位精度优化** 阶段，按 P0 → P4 顺序推进。其中 P0（遮挡穿透）和 P1（验证增强）改动小、收益确定，可作为过渡期穿插完成；P2（deepLocate）和 P3（Tier 1+2 融合）是核心投入项，建议各自独立验证。

后续可选方向：

- **混合定位精度优化 P0–P4**：遮挡穿透 → 验证增强 → deepLocate → DOM+VLM 融合 → 缓存层。
- **真实 AI 视觉定位接入**：VLM 配置页、默认开启策略与模型管理（P2/P3 的前置条件）。
- **登录页与认证体系**：平台登录入口、用户会话与权限边界。
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
