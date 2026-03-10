# Backend Status

后端是核心规划落地的主执行端，围绕 `uv + FastAPI + SQLAlchemy + Alembic` 推进。

## 当前已具备

- FastAPI 应用入口
- 应用创建阶段数据库连通性校验
- Alembic 初始迁移
- 第一批领域模型
- DSL 校验接口
- `cases` 创建、列表、详情、更新 API
- 单 Case 执行与执行记录查询
- `executions overview` 聚合接口，可输出通过率、平均耗时、最近失败、失败分类分布、按天趋势、失败动作分布与高频失败用例
- Playwright Runner、基础 Locator 与结构化执行报告
- 用例级 `base_url`，用于承载相对路径 `goto` 的正式执行地址

## 当前未完成

- Suite 批量执行链路
- AI 接入层
- 更完整的环境配置、Suite 编排和历史对比能力

## 本地开发约定

- 开发数据库默认使用 PostgreSQL
- 首次启动前先执行 `uv run alembic upgrade head`
- 后端启动命令：`uv run backend-dev`

## Smoke 基准用例

当前默认的真实联调基准是 `example.com` 冒烟用例：

- `base_url`：`https://example.com`
- `steps[0]`：`{"action": "goto", "value": "/"}`
- `steps[1]`：`{"action": "assert_url_contains", "value": "example.com"}`

该用例可用于验证：

- Runner 能正常执行真实页面
- 执行详情中的 `latest_url` 与步骤证据是否完整
- `GET /api/v1/executions/overview`、`GET /api/v1/executions`、`GET /api/v1/executions/{id}` 三处口径是否一致
- 仪表盘与报告中心读取 `overview` 聚合字段时，趋势、失败动作和高频失败用例是否与明细一致

## 后端落地顺序

后端执行顺序必须服从核心规划：

1. 阶段 1：DSL、Case、Runner 最小闭环
2. 阶段 2：Locator 服务
3. 阶段 3：自然语言生成 DSL
4. 阶段 4：Reporter 与报告查询
5. 阶段 5：Suite 与回归执行
