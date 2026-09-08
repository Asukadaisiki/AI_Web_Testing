# Browser Worker

Python 进程是浏览器执行与证据采集 Worker。Browser capability API 只提供
Playwright/A11y 能力，不再在 HTTP 请求链路中读取业务数据库；用户业务控制面、
Agent 决策、项目/会话归属校验和官方报告查询均由 Go AgentService 提供。

当前主路径由 Go `cmd/execution-worker` 消费 `execution_jobs`，通过无状态
`/api/v1/internal/browser-executions` 调用 Python Playwright runner，并由 Go
写回 `test_case_runs`。Python 旧 execution worker 已删除。

## 当前已具备

- FastAPI 应用入口
- 内网 Browser capability API
- 内网 stateless Browser execution API
- Playwright Runner、基础 Locator 与结构化执行报告
- 接收 Go 已裁决的 Browser context（如 `clean_context`、`entry_url_or_page`）

## 目录结构

```text
src/
  browser_worker/
    server/        FastAPI 入口、路由和 HTTP 适配
    capabilities/  Go 调用的浏览器能力与无状态执行 RPC
    exploration/   页面探索、A11y 采集、locator preflight、VLM prompt
    runners/       Playwright DSL 执行器
    locators/      元素定位和修正协议
    reporting/     执行报告和 failure signal
    contracts/     Pydantic 请求、响应和运行时数据合同
    runtime/       配置、日志和中间件
```

## 当前未完成

- 更完整的 AI 接入层治理（模型管理、prompt 调优、审计与回放）
- 多节点 artifact/storage state 对象存储
- Go 侧模型治理与评测闭环

## 本地开发约定

- Browser capability API：`uv run browser-worker-dev`
- 执行队列 Worker：`cd ../backend-go && go run ./cmd/execution-worker --concurrency 2`
- 数据库 schema：`cd ../backend-go && go run ./cmd/migrate`
- 当前不启用登录、Cookie、Token 或角色鉴权
- AI visual 默认关闭；如需启用，额外设置 `ENABLE_AI_VISUAL_LOCATE=true`、`VLM_API_KEY`、`VLM_BASE_URL`、`VLM_MODEL` 与 `VLM_MODEL_FAMILY`

## 本地生成物

- `.venv/`：`uv sync` 创建的 Python 环境，可删除后重建。
- `artifacts/`：执行截图和 DOM evidence，可能被历史报告引用，不应自动清空。
- `storage_states/`：浏览器会话状态，运行时按需创建。
- `.ruff_cache/`、`__pycache__/`：纯缓存，可直接删除。
- `.env`：本机配置，保留且不提交。

Browser Worker 不再创建本地 SQLite 数据库或项目根日志文件；日志统一写到 stdout。

## Smoke 基准用例

当前默认的真实联调基准是 `example.com` 冒烟用例：

- `base_url`：`https://example.com`
- `steps[0]`：`{"action": "goto", "value": "/"}`
- `steps[1]`：`{"action": "assert_url_contains", "value": "example.com"}`

该用例可用于验证：

- Runner 能正常执行真实页面
- 执行详情中的 `latest_url` 与步骤证据是否完整
- `GET /api/v2/executions/overview`、`GET /api/v2/executions`、`GET /api/v2/executions/{id}` 三处口径是否一致
- 仪表盘与报告中心读取 `overview` 聚合字段时，趋势、失败动作、高频失败用例、上一窗口对比和失败根因是否与明细一致
- `GET /api/v2/executions?failure_fingerprint=...` 是否能承接报告中心根因榜回流筛选

## 后端落地顺序

后端执行顺序必须服从核心规划：

1. 阶段 1：DSL、Case、Runner 最小闭环
2. 阶段 2：Locator 服务
3. 阶段 3：自然语言生成 DSL
4. 阶段 4：Reporter 与报告查询
5. 阶段 5：项目级回归执行与资产管理

## 项目级执行队列

创建批次后，Go API 只持久化 Batch 和待执行 Job；Go execution worker 负责领取，
并调用 Python Browser execution RPC 执行：

```bash
cd ../backend-go
go run ./cmd/execution-worker --concurrency 2
```

核心接口：

- `POST /api/v2/execution-batches`：为项目全部或指定用例创建执行批次
- `GET /api/v2/execution-batches?project_id={id}`：查询项目批次
- `GET /api/v2/execution-batches/{id}`：查询批次与任务状态
- `GET /api/v2/execution-batches/{id}/report`：查询批次报告
- `POST /api/v2/execution-batches/{id}/cancel`：取消待执行任务并标记运行中任务

当前 Go execution worker 使用 PostgreSQL 行锁领取任务，调用 Browser execution API
执行 Playwright，并持久化运行报告。Planning SSE 已迁移为创建 Batch，并轮询
Go Report Core 输出兼容进度事件，不再在请求线程中直接执行 Playwright。

## Agentic E2E

统一驱动器从版本化 acceptance spec 读取自然语言 Goal 和独立结果事实。发送给
Agent 的只有 Goal；Oracle 合同不会进入模型上下文。Driver 跟踪事件、读取 DSL
artifact、提交显式审批、等待正式报告，再用通用 Oracle 解释声明式结果断言：

```bash
uv run python scripts/run_agentic_e2e.py \
  --acceptance-spec ../research/acceptance/automationexercise-blue-top-cart.v1.json
```

需要打开 DeepSeek thinking mode 时，在启动 Go AgentService 前设置：

```bash
AI_PLANNING_THINK_MODE=true
AI_PLANNING_REASONING_EFFORT=max
```

新增任务只允许增加 acceptance JSON，不得修改 Driver/Runner/Oracle 代码。结果使用
`agentic-e2e.result.v1` JSON。
运行中 Job 通过 heartbeat 续租并读取持久化取消标记，取消会在 Runner 的下一安全步骤边界生效。
