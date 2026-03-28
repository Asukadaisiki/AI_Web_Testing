# AI Web Testing

AI 增强的 Web UI 自动化测试平台。

当前仓库采用前后端分离结构：
- `backend/`：FastAPI + SQLAlchemy + Alembic + Playwright Runner
- `frontend/`：React + TypeScript + Vite 平台 UI
- `docs/`：产品规划、执行计划、设计文档、执行日志与缺陷日志

## 当前状态

当前已完成的主链路：
- Case 管理：创建、列表、详情、编辑
- DSL 校验：结构化 DSL 校验与保存
- 单 Case 执行：后端 Runner 执行、证据生成、执行详情查看
- 执行中心：列表、筛选、窗口统计、失败分类、根因回流
- 仪表盘 / 报告中心：趋势、失败聚合、根因榜
- Suite 管理：Suite CRUD、工作台排序、批量执行
- Suite Context v2.3：Case 输入/输出契约、跨 Case 上下文传递、上下文快照、失败重跑上下文策略、前端上下文证据展示
- 混合定位闭环第一阶段：`locator_corrections` 修正记录、`needs_intervention` 执行状态、统一降级定位入口、执行详情页人工干预面板
- 修正记录管理：前端已提供 `/corrections` 页面，可按目标描述、页面 URL 和状态筛选修正记录，并支持 overview 卡片、命中趋势、批量启用/停用与事件时间线
- AI 生成 DSL 最小闭环：后端已提供 `POST /api/v1/dsl/generate`，前端工作台可输入自然语言生成草案，并选择“替换当前 DSL”或“仅导入步骤”
- AI 生成 DSL 深化：支持 `generation_mode / import_mode / current_case / current_steps / preserve_contracts`，后端会输出 `normalization_notes` 与 `generation_meta`，前端工作台可展示自动修正项、风险 warning 与三种导入方式
- AI DSL 第二轮治理能力：支持重试上下文、按拒绝原因重试生成、重试版 `prompt_version`，治理页可查看重试成效与 prompt 版本效果
- AI DSL 数据驱动治理第二轮第三批收敛：当前基线已升级到 `2026-03-24.governance-v3.3`；治理焦点选择已改为综合参考 `top_rejection_reasons`、`rejection_reason_by_variant` 与 `retry_acceptance_by_reason`，并围绕 `context_mismatch / bad_contracts` 继续滚动收敛
- AI 设置管理：前端已提供 `/settings/ai` 页面，支持管理 AI DSL / VLM 运行时配置；`GET /api/v1/settings/ai/overview` 可查看 DSL 生成最小观测指标
- 平台基础认证入口：后端已提供 `POST /api/v1/auth/login`、`POST /api/v1/auth/logout`、`GET /api/v1/auth/me`；前端已新增 `/login`、受保护路由、登录态恢复、Header 当前用户与统一 `401` 回退
- AI 设置治理概览：overview 现在会额外返回“当前治理焦点”“当前 Prompt 版本”“Prompt 版本观测口径”“治理焦点选择口径”与“当前治理焦点明细”，便于对齐当前治理批次与收敛结果
- 混合定位精度优化 P0-P3：已落地 overlay 穿透、DOM 严格匹配 + Jaccard + 中文单字回退、`deep_locate` 两阶段定位、DOM 候选 + VLM 排序
- Locator P4 sidecar：`resolve_with_fallback` 已增加会话级 AI 定位结果缓存，命中前会重新校验 selector，失效后自动清除
- AI 视觉保护：Tier 2 仍默认关闭，但已补超时、限流和熔断保护，避免不稳定模型拖垮主执行链路

当前未完成的重点方向：
- AI 生成 DSL 数据驱动第二轮优化继续滚动（以 `2026-03-24.governance-v3.3` 为 M1 治理基线，继续收敛后续高频拒绝原因）
- AI visual 继续默认关闭；当前已完成一轮本地受控灰度验收，3 条固定浏览器主回归通过，但本地 `ai_visual_stats` 仍为零样本，暂不进入默认开启评估
- M1 本轮不包含：AI visual 默认开启、角色权限细分、自助注册/找回密码、第三方登录、报告系统新扩面

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

默认本地种子账号（从零执行迁移创建数据库时）：
- 邮箱：`seed-owner@example.com`
- 密码：`password123`

如果你的本地数据库已经在本次认证改造前升级过 `20260324_0015`，需要重建本地库后重新执行迁移，或手动重置 `users.password_hash`。

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
