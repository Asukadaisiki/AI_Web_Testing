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
5. 资产管理层：`backend/app/models`、`backend/app/services`、`backend/app/api/routes`、`frontend/src/pages`，当前以 `Project -> Case` 为主

## 当前状态快照（截至 2026-03-31）

### 进度评估

- 当前阶段：**M1 收口已完成，进入 M2 规划前期**
- M1 完成度：`100%`（认证入口、治理主线收口、Suite 下线、AI 规划助手均已落地）
- 相对核心五阶段路线图的整体完成度估算：`80% - 85%`

阶段对齐判断：
- 阶段一 基础执行能力：已完成
- 阶段二 混合定位系统：主链路完成，已适配智谱 GLM 视觉模型，AI visual 默认开启评估未收口
- 阶段三 DSL 与自然语言生成：主链路完成，已适配智谱 BigModel，AI 测试规划对话助手已落地
- 阶段四 报告系统：报告中心已完成作用域和指标增强，暂未进入新一轮扩面
- 阶段五 项目级回归执行：Suite 已下线，统一为 `Project -> Case` 资产结构，CRUD 完整且权限已加固

### 已完成

- 平台基础：前后端单仓骨架、FastAPI 入口、Alembic 迁移、`users / projects / test_cases / test_case_runs` 等当前核心模型已落地
- 平台页面：Dashboard、Case 列表/工作台、执行中心、执行详情、报告中心、修正记录、AI 配置、登录页均已可用
- 执行主链路：DSL Schema 与校验、Case 持久化、单 Case 执行、步骤级证据、Artifact 访问、执行详情、`executions overview` 聚合已打通
- 资产结构：统一为 `Project -> Case`，Suite 应用层已下线并完成数据库清理；CRUD 完整且项目成员权限校验已加固
- 混合定位闭环：修正记录模型/API、`resolve_with_fallback` 四层降级链路、AI visual、前端人工干预面板、corrections 管理页已完成
- 定位精度与稳定性：P0-P4 优化、`deep_locate` 总超时预算、公共候选接口、缓存命中校验、运行保护与相关加固均已落地
- AI DSL 闭环：自然语言生成、草案预览与导入、`generation_mode / import_mode / preserve_contracts`、`normalization_notes`、`generation_meta`、反馈闭环、治理与观测页已完成；已适配智谱 BigModel (`glm-4.7-flash`)，支持 thinking 模式
- AI 视觉定位适配：已适配智谱 GLM 视觉模型，保持非智谱 provider 不回归
- AI DSL 治理基线：已推进到 `2026-03-24.governance-v3.3`，围绕 `context_mismatch / bad_contracts` 按 rejected、variant、retry 指标持续收敛
- AI 测试规划助手：工作台内嵌对话 UI、后端 planning session 持久化、agent loop 与 DSL 草案生成复用链路已落地
- 认证基线：后端 `POST /api/v1/auth/login`、`POST /api/v1/auth/logout`、`GET /api/v1/auth/me` 已落地，业务 API 默认要求登录；前端已完成 `/login`、登录态恢复、受保护路由、统一 `401` 回退；auth session 和 artifact 访问已加固
- 报告中心增强：作用域和指标已扩展
- 回归体系：后端与前端自动化测试链路已建立，2 条浏览器级固定主回归已固化

### 进行中

- AI 生成 DSL 数据驱动优化：在 `governance-v3.3` 基线上继续按 `top_rejection_reasons`、`rejection_reason_by_variant`、`retry_acceptance_by_reason` 滚动收敛后续高频原因
- AI visual 灰度验收：继续在默认关闭前提下补足手动开启窗口样本，只有累计达到 `>= 30 locate_requests` 或保留连续 3 天观察记录后，才重新评估是否进入默认开启讨论
- AI 测试规划助手打磨：基于实际使用反馈优化对话体验和场景生成质量

### 未开始

- AI visual 默认开启策略：当前保持”可配置、可调用、默认关闭”；已完成首个受控窗口，但结论仍为样本不足
- corrections 运维视角的跨目标分析与更细状态反馈
- 角色权限细分、账号管理、密码重置
- 新一轮报告系统扩面（AI 失败分析等）
- 回归编排：基于 `Project -> Case` 结构的批量回归执行

## 下一里程碑

M1 已完成。当前进入 **M2 规划前期**，待确认优先级后启动。

M1 已完成的交付项：
- 认证入口（本地账号密码 + Cookie Session 最小可用形态）
- AI DSL 治理 v3.3 收口
- Suite 应用层下线，统一为 `Project -> Case` 资产结构
- Case CRUD 完整且项目成员权限已加固
- DSL 生成和 AI 视觉定位已适配智谱 GLM 系列模型
- AI 测试规划对话助手（工作台内嵌）
- 报告中心作用域和指标增强
- 2 条浏览器级固定主回归长期保留

相对核心规划的主要差距：
- 报告系统虽已具备结果展示和聚合，但还没有进入“更完整 AI 失败分析 / 新一轮报告扩面”
- 认证还停留在“本地账号密码 + Cookie Session”的最小可用形态，尚未进入角色分层、账号管理、密码重置等下一阶段
- corrections 运维视角的跨目标分析与更细粒度反馈还没有启动

M1 明确不包含（已确认）：

- AI visual 默认开启
- 角色权限细分
- 自助注册 / 找回密码
- 第三方登录
- 新一轮报告系统扩面

M2 候选方向（待确认优先级）：

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
- 浏览器级回归：固定保留 `单 Case smoke`、`intervention -> rerun -> Tier0 hit` 两条主回归；AI DSL 维持单元/API 级验证，不把外部模型稳定性引入主 CI

## 风险与兜底

1. **自然语言生成不稳定**：AI 输出只进入校验链路，不直接进入执行器
2. **SQLite 与 PostgreSQL 行为差异**：模型设计贴近 PostgreSQL，关键 SQL 行为尽早验证
3. **定位复杂度过高**：先完成 DOM 增强定位，Vision 先保留接口或原型验证
4. **前端过早追求视觉完整度**：优先保证工作台、执行结果、报告主链路
