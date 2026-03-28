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

## 当前状态快照（截至 2026-03-28）

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
- AI DSL 第二轮可用率收敛首批：重试上下文、按拒绝原因重试生成、重试版 `prompt_version`、治理页重试成效概览
- AI DSL 数据驱动优化第二轮首批：`2026-03-22.governance-v3`、基于高频拒绝原因的固定治理规则、contract alias 自动修正与默认治理焦点回退
- AI DSL 数据驱动优化第二轮第二批：`2026-03-23.governance-v3.2`、排除已收敛的 `wrong_actions / invalid_structure` 后滚动聚焦 `context_mismatch / bad_contracts`，并补齐上下文名称/描述对齐、`context_key` snake_case 修正与无稳定 `source` 输出契约过滤
- AI DSL 数据驱动优化第二轮第三批：`2026-03-24.governance-v3.3`、治理焦点选择改为综合参考 rejected 数量、retry 未收敛量与受影响 prompt variant 覆盖；同时对 `bad_contracts` 增加基于当前契约的保守回填与稳定化修复
- 混合定位精度优化 P0-P3：overlay 遮挡穿透、DOM 严格匹配 + Jaccard + 中文单字回退、`deep_locate` 两阶段定位、DOM 候选 + VLM 排序
- Locator follow-up：`deep_locate` 总超时预算、`semantic` 公共候选接口、Pillow lazy import、错误与日志收口
- Locator sidecar P4：`resolve_with_fallback` 已增加会话级 AI 定位结果缓存、缓存命中校验与失效清理日志
- 浏览器级固定主回归：单 Case smoke、`needs_intervention -> correction -> rerun -> Tier0 hit`、`Suite Context + rerun_failed` 三条主链路已固化，本地夹具页同时保留 2 条扩展回归
- AI visual 灰度验收口径：已新增 [`docs/ai-visual-gray-acceptance-baseline.md`](./ai-visual-gray-acceptance-baseline.md)，明确观测窗口、通过阈值与 3 条浏览器主回归门槛
- AI visual 灰度验收结论（2026-03-24）：已完成 1 个本地受控观察窗口，3 条固定浏览器主回归全部通过；但当前本地 `ai_visual_stats` 仍为零样本，尚未达到 `>= 30 locate_requests` 门槛，结论为“继续默认关闭，样本不足，不进入默认开启评估”，详见 [`docs/ai-visual-gray-acceptance-2026-03-24.md`](./ai-visual-gray-acceptance-2026-03-24.md)
- 平台基础认证入口：`users` 已扩展为可登录实体，后端已提供 `POST /api/v1/auth/login`、`POST /api/v1/auth/logout`、`GET /api/v1/auth/me`，业务 API 默认要求登录；前端已新增 `/login`、登录态恢复、受保护路由、Header 用户信息与统一 `401` 回退

### 进行中

- M1 收口主线：在认证入口已落地的前提下，继续收尾 `governance-v3.3` 主线与固定主回归验收，不再扩张到报告增强或新的定位主线
- AI 生成 DSL 数据驱动优化第二轮：在 `governance-v3.3` 基线上继续按 `top_rejection_reasons`、`rejection_reason_by_variant`、`retry_acceptance_by_reason` 滚动收敛后续高频原因
- AI visual 灰度验收：继续在默认关闭前提下补足手动开启窗口样本，只有累计达到 `>= 30 locate_requests` 或保留连续 3 天观察记录后，才重新评估是否进入默认开启讨论

### 未开始

#### 其他未开始项

- AI visual 默认开启策略仍未开始，当前保持“可配置、可调用、默认关闭”；2026-03-24 已完成首个受控窗口，但结论仍为样本不足
- corrections 运维视角的跨目标分析与更细状态反馈仍未开始

## 下一里程碑

当前主线为 **M1 收口：治理主线收尾 + 平台基础认证入口**。

AI DSL 方向当前已切到 `2026-03-24.governance-v3.3`：不再重做 `wrong_actions / invalid_structure`，而是继续基于现有治理数据（`top_rejection_reasons`、`rejection_reason_by_variant`、`retry_acceptance_by_reason`）滚动收敛剩余高频拒绝原因，目标仍是降低“初次失败且按原因重试后仍失败”的占比；治理页现在也会直接展示当前焦点的 rejected / variant / retry / retry accepted 明细。本轮还补齐了 integration 级回归，验证非法 retry 请求不会留下错误审计记录。

认证方向本轮已经收口到“本地账号密码 + Cookie Session”的最小可用形态：登录、登出、当前用户、受保护路由与受保护 API 已落地；本期不做角色分层、自助注册、找回密码、第三方登录或更深的安全加固。对全新从零迁移的本地数据库，默认种子账号为 `seed-owner@example.com / password123`。

Locator 方向不再重开新主线，P4 会话级缓存已作为 sidecar 落地；后续只围绕 AI visual 灰度验收补样本，重点验证“重复目标场景减少调用、命中率不回退、延迟可控”。当前首个本地受控窗口只确认了 3 条固定主回归稳定通过，还不能支持默认开启讨论。具体验收口径见 [`docs/ai-visual-gray-acceptance-baseline.md`](./ai-visual-gray-acceptance-baseline.md)，当前结论见 [`docs/ai-visual-gray-acceptance-2026-03-24.md`](./ai-visual-gray-acceptance-2026-03-24.md)。

M1 明确不包含：

- AI visual 默认开启
- 角色权限细分
- 自助注册 / 找回密码
- 第三方登录
- 新一轮报告系统扩面

回归方向已切换为“固定主回归长期保留”，而不是继续补入口数量。建议持续验证以下三条：

1. 单 Case smoke。
2. `needs_intervention -> correction -> rerun -> Tier0 hit`。
3. Suite Context 串联 + 失败重跑。

后续可选方向：

- **AI DSL 治理**：继续按高频拒绝原因滚动更新 prompt / normalization，并观察 `prompt_version_breakdown`。
- **AI visual 灰度验收**：继续补齐手动开启窗口下的样本量，暂不改默认关闭策略。
- **认证体系下一阶段**：角色权限边界、账号管理、密码重置与更完整的安全治理。
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
- 浏览器级回归：固定保留 `单 Case smoke`、`intervention -> rerun -> Tier0 hit`、`Suite Context + rerun_failed` 三条主回归；AI DSL 维持单元/API 级验证，不把外部模型稳定性引入主 CI

## 风险与兜底

1. **自然语言生成不稳定**：AI 输出只进入校验链路，不直接进入执行器
2. **SQLite 与 PostgreSQL 行为差异**：模型设计贴近 PostgreSQL，关键 SQL 行为尽早验证
3. **定位复杂度过高**：先完成 DOM 增强定位，Vision 先保留接口或原型验证
4. **前端过早追求视觉完整度**：优先保证工作台、执行结果、报告主链路
