# AI Web Testing

AI 增强的 Web UI 自动化测试平台。

当前仓库采用前后端分离结构：
- `backend/`：FastAPI + SQLAlchemy + Alembic + Playwright Runner
- `frontend/`：React + TypeScript + Vite 平台 UI
- `docs/`：产品规划、执行计划、设计文档、执行日志与缺陷日志

## 当前状态

当前阶段：**M2 功能增强 — A11y 管线 v2 已落地，主路径全面切换**。M1/M2 前端体验全部完成。

进度判断（基于 2026-05-15 量化结果）：
- M1 完成度：`100%`
- M2 前端体验重构完成度：`100%`
- M2 功能增强完成度：`95%`（A11y 管线 + 缓存 + Preflight 闭环已通）
- 相对核心五阶段产品路线图整体完成度：`95%+`
- 单元测试：**505 passed / 0 failed**，集成测试：**8 passed / 0 failed**

### 2026-05-15 主路径 v2 — A11y 树驱动管线

将主路径感知层从 DOM 全量抽取替换为 CDP Accessibility Tree，接通缓存与 Preflight 闭环：

| 指标 | v1 (DOM) | v2 (A11y) |
|------|----------|-----------|
| 单页探索耗时 | 4.7-7.9s | 19-61ms（**100-250x 快**） |
| 元素数据量 | 271-598 kB | 7-26 kB（**22-38x 小**） |
| 元素上限 | 300（撞 cap） | 无上限（A11y 天然 50-300） |
| ReAct safety_cap | 30 | 5 |
| 系统提示词 | 186 行 | 30 行 |
| DSL 生成入口 | `generate_case_draft` + segmented | `generate_segmented_case_draft` 唯一入口 |
| 草案质量门 | 无 | Preflight 1:N candidates + 单段重生 |
| 探索缓存 | 无 | `AIPlanningToolResult` DB 缓存，TTL=4h |
| 默认项目 | 手动创建 | session 创建自动绑定 `default-{session_id}` |

核心改动：16 commits，37 files，+3.5K / −6.6K lines（净 −3.1K）。

### 2026-05-07 AI Agent 质量量化里程碑

通过 `test_brand_filter_cart` 标准回归用例反复测试，27 个 commits 修复 16 个 bugs（BUG-064 ~ BUG-079），最终实现：

| 指标 | 修复前 | 修复后 |
|------|--------|--------|
| AI 探索行为 | ask_user 询问废话 | 自动 explore_page → capture_session → explore_flow |
| 页面探索覆盖 | 1 页 | 3 页（登录页 + 品牌筛选页 + 购物车页） |
| DSL 步骤 | 10（仅有购物车断言） | 42（完整登录→筛选→加购→验证→数量修改） |
| assert_text | 0 | 9 |
| 步骤被归一化删除 | 10-36 | 0 |
| nth-of-type 定位器 | 13 | 0（全部语义定位） |
| 执行通过率 | 0%（无法执行） | **100%（42/42）** |

### 已完成的核心能力

- 平台基础：NotebookLM 三栏浮岛布局、侧边栏导航、ReportPage、Case 管理（含编辑/删除）、执行详情、执行记录删除
- 前端体验：全局大圆角/无边框/弱阴影主题 token，全部页面统一三栏布局
- 执行主链路：DSL 校验、单 Case 执行、步骤级证据、执行详情与报告聚合
- 执行流式推送：后端 SSE 流式执行原语、AI Planning SSE worker/路由、前端实时进度气泡与取消按钮
- 混合定位闭环：Tier 0 人工修正、Tier 1 DOM 语义定位（含 text_parent_chain 消歧）、Tier 2 AI visual、Tier 3 人工干预
- AI DSL：`generate_segmented_case_draft` 唯一入口、分段并行生成、Preflight 1:N candidates + 单段重生、草案预览与导入
- AI 规划助手：对话式测试规划、A11y 树探索（CDP `Accessibility.getFullAXTree`）、关键字驱动折叠展开、DB 缓存（TTL=4h）、ReAct lite（safety_cap=5, 4 字段 schema）
- 认证基线：`/auth/login`、`/auth/logout`、`/auth/me`、前端 `/login`、受保护路由、统一 `401` 回归能力：505 单元测试 + 8 集成测试（含 3 条浏览器级 A11y 管线回归）
- 白盒测试：session 层 + 用例创建/执行/端到端全链路 API chain 测试覆盖

当前仍在推进的事项：
- `generate_case_draft` 已删除，但 `services/dsl.py` 的 `/api/v1/dsl/generate` 路由已切换到 `generate_segmented_case_draft`
- AI visual 仍默认关闭，灰度验证样本积累中

与计划相比的主要差距：
- AI visual 还没有达到默认开启条件，仍处于受控灰度验证阶段
- 认证只做到”本地账号密码 + Cookie Session”最小可用形态，尚未进入角色权限、账号管理、密码重置

AI visual 灰度验收口径见 [`docs/ai-visual-gray-acceptance-baseline.md`](./docs/ai-visual-gray-acceptance-baseline.md)，本轮结论见 [`docs/ai-visual-gray-acceptance-2026-03-24.md`](./docs/ai-visual-gray-acceptance-2026-03-24.md)。

## 演示流

当前前端采用三步闭环演示流（无需登录）：

1. **AI 规划**（PlanningPage）：通过对话式 AI 助手生成测试方案，支持会话历史恢复与会话管理
2. **用例中心**（CasesPage）：审阅 DSL 草案、保存为正式用例、编辑/删除已有用例、触发 Playwright 执行（支持实时流式进度与取消）
3. **报告**（ReportPage）：查看执行结果、概览统计、步骤证据与截图，支持删除执行记录

全部页面采用 NotebookLM 风格三栏浮岛布局，侧边栏底部导航。

## 人工干预路径

当定位链路无法命中目标时：
1. 执行状态会落为 `needs_intervention`
2. 在 `执行详情页` 的失败步骤中查看失败截图、DOM 快照和建议 selector
3. 可直接跳到 `修正记录` 页面查看历史修正，或在当前页面提交新修正
4. 提交修正记录后，直接从页面重跑当前 Case
5. 后续同页面同目标会优先命中人工修正记录

## 浏览器级回归

仓库提供本地可控的浏览器级回归链路，用于验证：

- 3 条 A11y 管线回归（`test_main_path_v2_e2e.py`，`@pytest.mark.browser_integration`）：
  - A11y 节点结构验证（node_id/role/name/focusable/disabled/page_state）
  - 所有 role ∈ `USEFUL_A11Y_ROLES`（24 种）
  - Preflight 与真实 a11y_nodes 的 candidates 命中
- 5 条 API 级集成测试（`test_main_path_v2_e2e.py`）：
  - 默认项目 auto-create、Preflight candidates、DB 缓存命中/过期
- 6 条 Platform API Chain 白盒集成测试：
  - Session 层：登录、登出、未授权访问 3 条
  - 用例创建 + 执行链路：有效 DSL 创建用例、登录 Case 执行验证、端到端全链路 3 条

运行方式：

```powershell
cd backend
# 全量单测（505 tests）
uv run pytest tests/unit -q

# 浏览器级回归（需 Playwright）
uv run pytest tests/integration -m browser_integration

# API Chain 白盒测试
uv run pytest tests/integration/test_platform_api_chain.py -v
```

前置条件：

- 已执行 `uv sync`
- 已执行 `uv run playwright install chromium`
- 浏览器级测试使用 `the-internet.herokuapp.com` 作为目标站点

## AI 视觉保护配置

后端当前支持以下保护性配置，默认都以”不中断主链路”为原则：
- `ENABLE_AI_DSL_GENERATE=false`
- `AI_DSL_TIMEOUT_MS=15000`
- `AI_DSL_API_KEY=`
- `AI_DSL_BASE_URL=https://api.openai.com/v1`
- `AI_DSL_MODEL=`：用于自然语言生成 DSL 草案的文本模型
- `AI_DSL_STRICT_MODE=false`
- `AI_DSL_ALLOW_AUTO_REPAIR=true`
- `ENABLE_AI_VISUAL_LOCATE=false`
- `AI_VISUAL_TIMEOUT_MS=10000`
- `AI_VISUAL_FAILURE_THRESHOLD=3`
- `AI_VISUAL_COOLDOWN_SECONDS=60`
- `AI_VISUAL_RATE_LIMIT_PER_MINUTE=10`
- `VLM_BASE_URL=https://api.openai.com/v1`
- `VLM_MODEL=`：用于 Tier 2 AI visual 的视觉模型
- `VLM_MODEL_FAMILY=gpt-4o`
- `VLM_API_KEY=`

当 VLM 请求连续失败、超时或超过速率预算时，系统会直接跳过 Tier 2，继续走现有降级链路或进入人工干预。

AI DSL 生成会输出最小治理信息：
- `warnings`：风险提示或需人工注意的问题
- `normalization_notes`：自动修正动作
- `generation_meta`：使用模型、Base URL 来源、删除非法 steps / contracts 数量等

## 定位系统

混合定位采用多层降级链路，A11y 优先、VLM 兜底：

| Tier | 策略 | 说明 |
|------|------|------|
| Tier 0 | 人工修正 | 优先命中已保存的 corrections 记录 |
| Tier 1 | A11y candidates | Preflight 1:N 映射：`role`(0.90) / `role_fuzzy`(0.75) / `text`(0.55)，runtime_scorer 排序 |
| Tier 2 | DOM 语义定位 | text_parent_chain 消歧、element_id、CSS/XPath、text/role/placeholder 匹配 |
| Tier 3 | AI visual | 视觉模型定位，默认关闭，需手动开启 |
| Tier 4 | 人工干预 | 定位失败后进入 `needs_intervention`，提交修正后可重跑 |

## 快速开始

### 1. 后端

```powershell
cd backend
uv sync
uv run alembic upgrade head
uv run backend-dev
```

默认后端地址：
- `http://127.0.0.1:8000`

### 2. 前端

```powershell
cd frontend
npm install
npm run dev
```

默认前端地址：
- `http://127.0.0.1:5173`

认证相关本地配置：
- `AUTH_SESSION_SECRET` 现在必须显式配置，建议先复制 `backend/.env.example` 到 `backend/.env` 后填写自己的 session secret
- 本地若仍通过 `http://127.0.0.1` 调试，需要显式设置 `AUTH_SESSION_HTTPS_ONLY=false`；在 HTTPS 环境下应保持 `true`
- 从零迁移创建数据库时不再提供公开默认密码；如需继续使用种子账号 `seed-owner@example.com`，请在本地手动初始化或重置 `users.password_hash`

如果你的本地数据库已经在本次认证改造前升级过 `20260324_0015`，建议重建本地库后重新执行迁移，或手动重置 `users.password_hash`。

## 测试与构建

### 后端测试（505 单元测试）

```powershell
cd backend
uv run pytest tests/unit -q
```

### 后端集成测试（8 tests: 5 API + 3 browser）

```powershell
cd backend
# API 级集成测试（无浏览器）
uv run pytest tests/integration/test_main_path_v2_e2e.py -v -k "not browser_integration"

# 浏览器级回归（需 Playwright）
uv run pytest tests/integration/test_main_path_v2_e2e.py -v -m browser_integration
```

### 前端测试

```powershell
cd frontend
npm test -- --run
```

### 前端构建

```powershell
cd frontend
npm run build
```

## 推荐联调路径

建议先通过 AI 规划页生成一个 smoke case 验证完整闭环：
- `base_url`: `https://example.com`
- Step 1: `goto /`
- Step 2: `assert_url_contains example.com`

建议按下面顺序检查：
1. 在 AI 规划页生成测试方案并审阅 DSL 草案
2. 保存为正式用例并触发执行
3. 在报告页查看执行结果、步骤证据与截图

## 文档索引

- `docs/AI 自动化测试增强项目规划.md`：核心产品规划，优先级最高
- `docs/project-plan.md`：当前执行计划与阶段状态
- `docs/frontend-design.md`：前端设计说明
- `docs/execution-log.md`：任务执行记录
- `docs/bug-log.md`：缺陷记录
- `docs/ai-visual-gray-acceptance-baseline.md`：AI visual 灰度验收口径与门槛
- `docs/superpowers/specs/2026-05-14-main-path-v2-a11y-pipeline-design.md`：A11y 管线 v2 设计文档
- `docs/superpowers/plans/2026-05-15-main-path-v2-a11y-pipeline.md`：A11y 管线 v2 实施计划
- `docs/superpowers/specs/`：功能设计规格文档
- `docs/superpowers/plans/`：实施计划文档

如果文档之间有冲突，以 `docs/AI 自动化测试增强项目规划.md` 为准。

## 开发约束

- 正式执行结果以 `backend` Runner 为准
- 前端只负责平台交互、工作台编辑和结果展示
- AI 能力不能绕过 DSL 校验直接驱动执行
- 新增功能默认需要补测试，并更新 `docs/execution-log.md`
