# AI Web Testing

AI 增强的 Web UI 自动化测试平台。

当前仓库采用前后端分离结构：
- `backend/`：FastAPI + SQLAlchemy + Alembic + Playwright Runner
- `frontend/`：React + TypeScript + Vite 平台 UI
- `docs/`：产品规划、执行计划、设计文档、执行日志与缺陷日志

## 当前状态

当前阶段：**M1 收口**，重点是“AI DSL 治理主线收尾 + 平台基础认证入口落地”。

进度判断（估算）：
- 按当前 M1 目标看：约 `85% - 90%`
- 按核心五阶段产品目标看：约 `75% - 80%`

已完成的核心能力：
- 平台基础：Dashboard、Case 管理、Suite 管理、执行中心、报告中心、修正记录、AI 设置、登录鉴权入口
- 执行主链路：DSL 校验、单 Case 执行、Suite 批量执行与失败重跑、步骤级证据、执行详情与批次详情
- Suite Context：输入/输出契约、跨 Case 上下文传递、上下文快照、失败重跑上下文复用、前端证据展示
- 混合定位闭环：Tier 0 人工修正、Tier 1 DOM 语义定位、Tier 2 AI visual、Tier 3 人工干预，以及 P0-P4 精度/缓存优化
- AI DSL：自然语言生成、草案预览与导入、反馈闭环、治理页观测、`2026-03-24.governance-v3.3` 数据驱动收敛
- 认证基线：`/auth/login`、`/auth/logout`、`/auth/me`、前端 `/login`、受保护路由、统一 `401` 回退
- 回归能力：后端/前端自动化测试链路已建立，浏览器级 3 条固定主回归 + 2 条扩展回归已固化

当前仍在收口的事项：
- 继续基于 `top_rejection_reasons`、`rejection_reason_by_variant`、`retry_acceptance_by_reason` 收敛 AI DSL 高频拒绝原因
- AI visual 仍默认关闭；虽然 3 条固定主回归已通过，但本地样本量不足，尚未进入默认开启评估
- 继续巩固认证改造后的主回归与本地运行口径

与计划相比的主要差距：
- 报告系统已经可用，但离“更完整的 AI 失败分析 / 报告扩面”还有差距
- AI visual 还没有达到默认开启条件，目前仍处于受控灰度验证阶段
- 认证只做到“本地账号密码 + Cookie Session”的最小可用形态，尚未进入角色权限、账号管理、密码重置等下一阶段
- corrections 运维视角的跨目标分析与更细粒度状态反馈尚未开始

AI visual 灰度验收口径见 [`docs/ai-visual-gray-acceptance-baseline.md`](./docs/ai-visual-gray-acceptance-baseline.md)，本轮结论见 [`docs/ai-visual-gray-acceptance-2026-03-24.md`](./docs/ai-visual-gray-acceptance-2026-03-24.md)。

## Suite Context 使用路径

建议按下面的顺序验证 `Suite Context v2.3`：
1. 在 `Case 工作台` 为 Case A 配置 `output_contract`
2. 在 `Case 工作台` 为 Case B 配置 `input_contract`，并在步骤里使用 `${context_key}`
3. 将两个 Case 放入同一个 Suite，执行后在 `Suite 批次详情` 查看上下文快照和变量读写证据
4. 打开子执行详情页，确认 `Suite Context` 读写证据、解析失败原因和来源批次展示正常

## 人工干预路径

当定位链路无法命中目标时：
1. 执行状态会落为 `needs_intervention`
2. 在 `执行详情页` 的失败步骤中查看失败截图、DOM 快照和建议 selector
3. 可直接跳到 `修正记录` 页面查看历史修正，或在当前页面提交新修正
4. 提交修正记录后，直接从页面重跑当前 Case
5. 后续同页面同目标会优先命中人工修正记录

## 浏览器级回归

仓库现在提供本地可控的浏览器级回归链路，用于验证：

- 3 条固定主回归：
  - 单 Case smoke 可稳定执行成功
  - 首次执行落为 `needs_intervention`，提交 correction 后可重跑通过，再次执行由 Tier 0 命中
  - Suite Context 失败重跑可复用原始上下文快照
- 2 条扩展回归：
  - 错误 correction 连续失败 3 次后会自动停用
  - DOM candidates 被 VLM rerank 时可稳定选中更优目标

运行方式：

```powershell
cd backend
uv run pytest tests/integration -m browser_integration
```

前置条件：

- 已执行 `uv sync`
- 已执行 `uv run playwright install chromium`
- 测试会自动启动 `backend/tests/fixtures/` 下的本地静态页，不依赖外部站点

## AI 视觉保护配置

后端当前支持以下保护性配置，默认都以“不中断主链路”为原则：
- `ENABLE_AI_DSL_GENERATE=false`
- `AI_DSL_TIMEOUT_MS=15000`
- `AI_DSL_API_KEY=`
- `AI_DSL_BASE_URL=https://api.openai.com/v1`
- `AI_DSL_MODEL=`
- `AI_DSL_STRICT_MODE=false`
- `AI_DSL_ALLOW_AUTO_REPAIR=true`
- `ENABLE_AI_VISUAL_LOCATE=false`
- `AI_VISUAL_TIMEOUT_MS=10000`
- `AI_VISUAL_FAILURE_THRESHOLD=3`
- `AI_VISUAL_COOLDOWN_SECONDS=60`
- `AI_VISUAL_RATE_LIMIT_PER_MINUTE=10`

当 VLM 请求连续失败、超时或超过速率预算时，系统会直接跳过 Tier 2，继续走现有降级链路或进入人工干预。

AI DSL 生成现在还会输出最小治理信息：
- `warnings`：风险提示或需人工注意的问题
- `normalization_notes`：自动修正动作
- `generation_meta`：使用模型、Base URL 来源、删除非法 steps / contracts 数量等

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

### 后端测试

```powershell
cd backend
uv run pytest
```

### 后端浏览器级回归

```powershell
cd backend
uv run pytest tests/integration -m browser_integration
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

建议先用最小 smoke case 验证主链路：
- `base_url`: `https://example.com`
- Step 1: `goto /`
- Step 2: `assert_url_contains example.com`

建议按下面顺序检查：
1. 在 Case 工作台创建并保存用例
2. 直接执行单 Case，确认执行详情、截图和证据正常
3. 创建包含 2 个 Case 的 Suite
4. 执行 Suite，确认生成批次详情
5. 制造 1 个失败 Case，验证失败重跑与历史回看

## 文档索引

- `docs/AI 自动化测试增强项目规划.md`：核心产品规划，优先级最高
- `docs/project-plan.md`：当前执行计划与阶段状态
- `docs/frontend-design.md`：前端设计说明
- `docs/ai-visual-gray-acceptance-baseline.md`：AI visual 灰度验收口径与门槛
- `docs/ai-visual-gray-acceptance-2026-03-24.md`：2026-03-24 本地受控灰度验收结论
- `docs/execution-log.md`：任务执行记录
- `docs/bug-log.md`：缺陷记录

如果文档之间有冲突，以 `docs/AI 自动化测试增强项目规划.md` 为准。

## 开发约束

- 正式执行结果以 `backend` Runner 为准
- 前端只负责平台交互、工作台编辑和结果展示
- AI 能力不能绕过 DSL 校验直接驱动执行
- 新增功能默认需要补测试，并更新 `docs/execution-log.md`
