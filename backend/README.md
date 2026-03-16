# Backend Status

后端是核心规划落地的主执行端，围绕 `uv + FastAPI + SQLAlchemy + Alembic` 推进。

## 当前已具备

- FastAPI 应用入口
- 应用创建阶段数据库连通性校验
- Alembic 初始迁移
- 第一批领域模型
- DSL 校验接口
- DSL 生成草案接口（`POST /api/v1/dsl/generate`）
- `cases` 创建、列表、详情、更新 API
- `suites` 创建、列表、详情、更新 API
- 单 Case 执行与执行记录查询
- Suite 同步串行批量执行入口，复用现有 `test_case_runs`
- `executions overview` 聚合接口，可输出通过率、平均耗时、最近失败、失败分类分布、按天趋势、失败动作分布、高频失败用例、上一窗口对比与失败根因聚合
- `corrections overview / events / bulk` 运营接口，可输出命中趋势、事件时间线并支持批量启停
- Playwright Runner、基础 Locator 与结构化执行报告
- 用例级 `base_url`，用于承载相对路径 `goto` 的正式执行地址

## 当前未完成

- 更完整的 AI 接入层治理（模型管理、prompt 调优、审计与回放）
- 更完整的环境配置、Suite 历史批次、失败重跑和历史对比能力

## 本地开发约定

- 开发数据库默认使用 PostgreSQL
- 首次启动前先执行 `uv run alembic upgrade head`
- 后端启动命令：`uv run backend-dev`
- AI DSL 生成默认关闭；如需启用，额外设置 `ENABLE_AI_DSL_GENERATE=true`、`AI_DSL_API_KEY` 与 `AI_DSL_MODEL`

## Smoke 基准用例

当前默认的真实联调基准是 `example.com` 冒烟用例：

- `base_url`：`https://example.com`
- `steps[0]`：`{"action": "goto", "value": "/"}`
- `steps[1]`：`{"action": "assert_url_contains", "value": "example.com"}`

该用例可用于验证：

- Runner 能正常执行真实页面
- 执行详情中的 `latest_url` 与步骤证据是否完整
- `GET /api/v1/executions/overview`、`GET /api/v1/executions`、`GET /api/v1/executions/{id}` 三处口径是否一致
- 仪表盘与报告中心读取 `overview` 聚合字段时，趋势、失败动作、高频失败用例、上一窗口对比和失败根因是否与明细一致
- `GET /api/v1/executions?failure_fingerprint=...` 是否能承接报告中心根因榜回流筛选

## 本地人工干预回归

除 `example.com` smoke 外，后端现在还支持一条本地夹具页真实回归链路，用于验证：

- 首次执行进入 `needs_intervention`
- 通过 `POST /api/v1/corrections` 提交修正
- 重跑后由 Tier 0 命中并执行通过
- 错误修正连续失败 3 次后会自动停用

运行命令：

```powershell
cd backend
uv run pytest tests/integration -m browser_integration
```

若本机未安装 Chromium，先执行：

```powershell
cd backend
uv run playwright install chromium
```

## 后端落地顺序

后端执行顺序必须服从核心规划：

1. 阶段 1：DSL、Case、Runner 最小闭环
2. 阶段 2：Locator 服务
3. 阶段 3：自然语言生成 DSL
4. 阶段 4：Reporter 与报告查询
5. 阶段 5：Suite 与回归执行
