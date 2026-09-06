# Browser Worker

Python 进程是浏览器执行与证据采集 Worker，使用 `uv + FastAPI + SQLAlchemy +
Alembic + Playwright`。用户业务控制面和 Agent 决策均由 Go AgentService 提供。

## 当前已具备

- FastAPI 应用入口
- 应用创建阶段数据库连通性校验
- Alembic 初始迁移
- Alembic schema 管理
- 内网 Browser capability API
- Playwright Runner、基础 Locator 与结构化执行报告
- PostgreSQL Job polling、lease、heartbeat 与取消检测
- 确定性 `FailureSignal`、执行分析和 evidence 持久化
- 固定 actor 上下文与 capability 项目归属校验

## 当前未完成

- 更完整的 AI 接入层治理（模型管理、prompt 调优、审计与回放）
- 多节点 artifact/storage state 对象存储
- Go 侧模型治理与评测闭环

## 本地开发约定

- 开发数据库默认使用 PostgreSQL
- 首次启动前先执行 `uv run alembic upgrade head`
- Browser capability API：`uv run browser-worker-dev`
- 执行队列 Worker：`uv run python -m app.workers.execution_worker --concurrency 2`
- 当前不启用登录、Cookie、Token 或角色鉴权
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

## 项目级执行队列

创建批次后，API 只持久化 Batch 和待执行 Job；独立 Worker 负责领取并执行：

```bash
uv run python -m app.workers.execution_worker --concurrency 2
```

核心接口：

- `POST /api/v1/execution-batches`：为项目全部或指定用例创建执行批次
- `GET /api/v1/execution-batches?project_id={id}`：查询项目批次
- `GET /api/v1/execution-batches/{id}`：查询批次与任务状态
- `GET /api/v1/execution-batches/{id}/report`：查询批次报告
- `POST /api/v1/execution-batches/{id}/cancel`：取消待执行任务并标记运行中任务

当前 Worker 使用 PostgreSQL 行锁领取任务。Planning SSE 已迁移为创建 Batch，
并轮询 Report Core 输出兼容进度事件，不再在请求线程中直接执行 Playwright。

## Agentic E2E

统一驱动器只接受自然语言 Goal，通过 Go AgentService 跟踪事件、读取 DSL artifact、
提交显式审批、等待正式报告，再从终态 DOM evidence 独立校验购物车：

```bash
uv run python scripts/run_agentic_e2e.py \
  '匿名访问 Automation Exercise，从 Products 页面搜索 Blue Top，确认搜索结果，进入商品详情，将数量保持为 1，加入购物车，通过加购弹层打开 View Cart，并验证购物车中商品名为 Blue Top、单价和总价均为 Rs. 500、数量为 1。不得注册、登录、结账或填写个人信息；默认不使用 Vision。'
```

可用 `--oracle-mutation wrong-price` 或 `--oracle-mutation wrong-product`
执行独立 Oracle 负向变异。结果使用 `agentic-e2e.result.v1` JSON。
运行中 Job 通过 heartbeat 续租并读取持久化取消标记，取消会在 Runner 的下一安全步骤边界生效。
