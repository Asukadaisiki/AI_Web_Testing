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
- 单 Case 执行与执行记录查询
- `executions overview` 聚合接口，可输出通过率、平均耗时、最近失败、失败分类分布、按天趋势、失败动作分布、高频失败用例、上一窗口对比与失败根因聚合
- `corrections overview / events / bulk` 运营接口，可输出命中趋势、事件时间线并支持批量启停
- Playwright Runner、基础 Locator 与结构化执行报告
- 用例级 `base_url`，用于承载相对路径 `goto` 的正式执行地址

## 当前未完成

- 更完整的 AI 接入层治理（模型管理、prompt 调优、审计与回放）
- 更完整的环境配置、项目级回归编排与历史对比能力

## 本地开发约定

- 开发数据库默认使用 PostgreSQL
- 首次启动前先执行 `uv run alembic upgrade head`
- 后端启动命令：`uv run backend-dev`
- 当前本地单用户模式关闭登录鉴权，所有请求自动使用 `AUTH_AUTO_LOGIN_EMAIL` 指定的数据库账号（默认 `admin@test.com`）；该账号缺失或停用时接口会返回 500
- AI DSL 生成默认关闭；如需启用，额外设置 `ENABLE_AI_DSL_GENERATE=true`、`AI_DSL_API_KEY` 与 `AI_DSL_MODEL`
- AI visual 默认关闭；如需启用，额外设置 `ENABLE_AI_VISUAL_LOCATE=true`、`VLM_API_KEY`、`VLM_BASE_URL`、`VLM_MODEL` 与 `VLM_MODEL_FAMILY`

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

## 后端落地顺序

后端执行顺序必须服从核心规划：

1. 阶段 1：DSL、Case、Runner 最小闭环
2. 阶段 2：Locator 服务
3. 阶段 3：自然语言生成 DSL
4. 阶段 4：Reporter 与报告查询
5. 阶段 5：项目级回归执行与资产管理
