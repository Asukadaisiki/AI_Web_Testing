# v2/ — 单闭环重构

## 这是什么

从零实现**唯一一条闭环**，不掺任何其他功能：

```
输入 → 规划(agentruntime) → 执行 → 报告 → 失败回灌 → 输入
```

`v1/` 是旧项目（含 git 历史），**只作参考，不改动**。本目录是独立模块，有自己的 Go module、Python 包、前端工程与 SQLite 库。

## 为什么重做执行器与契约

上一版坏在两处，都出在数据契约：

1. **AI 生成的 DSL 过不了校验** —— 模型手写 JSON，系统再去校验；契约的合法取值散落在 Go、Python、提示词共 6 处，模型必然撞上其中某一条。
2. **AI 生成的就是错的** —— 模型凭想象写目标与条件（元素不存在、条件在运行时永远不成立），错误要到执行期才暴露。

本版的结构性对策：

| 问题 | 对策 |
|---|---|
| 模型手写 JSON 会撞契约 | **模型不写 JSON**。模型只能调用结构化工具；case 由 Go 侧根据工具调用逐条构建，构建过程本身就是校验 |
| 模型凭空写目标 | `click` / `input` 的 `target` **必须当场对最近一次真实观测解析成功**，否则工具返回候选列表让模型改口 |
| 模型把条件写错阶段 | 条件**由 Go 派生**，模型只表达期望（`expect_text` / `expect_url` / `expect_gone` / `expect_value`），阶段规则只有一张表 |
| 首步 goto 的前置条件永远不成立 | case **必须首步为 goto**；首步无前置条件，后续步骤的前置条件由 Go 从观测页 URL 派生，天然可满足 |
| 同一对象多副身子 | 只有一种形态：`cases.payload_json`（不可变工件，按内容哈希寻址）。执行、报告都读它，无回退链 |
| 模型生成的用例"就是错的" | `finish_case` **不落库，先干跑**：在全新浏览器上下文里跑通才允许进入审批。跑不通就把失败步骤结构化回给模型 |
| 定位器在观测时能中、执行时不中 | 观测阶段**就地验证**定位器，只有 `match_count == 1` 的才输出；偏好 role（可访问名）→ text → css |
| Go 与 Python 各写一份契约 | `fixtures/contract/case_contract.json` 两侧共读，必须给出相同的 accept 与错误码（`CONTRACT.md` §2.4） |

## 目录

```
v2/
  README.md       本文件
  CONTRACT.md     数据契约（唯一权威，人读版）
  plan/           重构计划与诊断（v1 结构问题的证据与分期方案，决策记录）
  backend/        Go：agentruntime + planner 工具 + executor 桥 + report + feedback + API（SQLite）
  worker/         Python：全新精简 Playwright 执行器（5 个 action）
  web/            React：4 个页面
  fixtures/       离线夹具（契约一致性用例、工具调用脚本、静态测试页）
  data/           SQLite 文件与证据产物
```

## 4 个页面（不多不少）

| 路径 | 页面 | 作用 |
|---|---|---|
| `/` | 输入/会话 | 输入目标 → SSE 时间线（工具调用、观测摘要）→ 审批 |
| `/executions/:id` | 执行详情 | 每步状态 + 证据（截图、console、network、URL 前后） |
| `/runs/:id/report` | 报告 | 汇总 + 失败信号表 |
| `/runs/:id/injection` | 错误注入 | 失败回灌候选 → 编辑 → 确认 → 开下一轮 |

## 运行

```bash
# 1) 执行器（Python，:8100）
cd v2/worker && uv sync && uv run uvicorn loop_worker.main:app --port 8100

# 2) 控制面（Go，:8101）
cd v2/backend && go run ./cmd/loopd

# 3) Web（Vite，:5174）
cd v2/web && npm install && npm run dev
```

### 离线验证（不调用模型、不花钱）

三层，从下往上：

```bash
# 契约一致性：Go 与 Python 共读同一份夹具，错误码必须逐字一致
cd v2/backend && go test ./internal/contract/
cd v2/worker && uv run python -m unittest tests.test_contract_conformance

# 闭环全链路：假执行器 + 脚本模型，确定性验证 规划/审批/执行/报告/回灌/提问/改口
cd v2/backend && go test ./internal/agentruntime/

# 真执行器（需要 Playwright 浏览器）
cd v2/worker && uv run python -m unittest discover -s tests -t .
```

### 不花钱地跑一次真实闭环（脚本模型 + 真浏览器）

`LOOP_LLM_SCRIPT` 一旦设置，控制面就用离线脚本替代真实模型：整条闭环照跑，
浏览器是真浏览器，但模型调用是零成本的固定脚本。

```bash
# 站点夹具
cd v2/worker && uv run python -m http.server 8123 --directory fixtures/site

# 执行器（另开一个终端）
cd v2/worker && uv run uvicorn loop_worker.main:app --port 8100

# 控制面（另开一个终端；脚本见 fixtures/scripts/catalog_alpha.json）
cd v2/backend && LOOP_LLM_SCRIPT=../fixtures/scripts/catalog_alpha.json go run ./cmd/loopd
```

然后 POST `/api/runs`、轮询、`POST /api/runs/{id}/approve`、看 `/api/runs/{id}/report`。
`fixtures/scripts/catalog_alpha.json` 里的 URL 指向 `127.0.0.1:8123`，与上面第 1 步对应。

## 配置

| 变量 | 说明 |
|---|---|
| `LOOP_LLM_SCRIPT` | 指向脚本 JSON；设置后走离线脚本模型（零成本），优先于下面的真实模型配置 |
| `LOOP_LLM_PROVIDER` | `lark`（火山方舟/Ark）、`deepseek` 或 `openai`（任意 OpenAI 兼容端点） |
| `LOOP_LLM_API_KEY_ENV` | 给出**变量名**，从该变量读密钥（对应配置里的 `apiKeyEnv`）。推荐用这个，密钥不进命令行 |
| `LOOP_LLM_API_KEY` | 直接给密钥值；与上面二选一，同时设置时以它为准 |
| `LOOP_LLM_MODEL` / `LOOP_LLM_BASE_URL` | 覆盖模型与端点；不设则用提供方默认值 |
| `LOOP_WORKER_URL` | 执行器地址，默认 `http://127.0.0.1:8100` |
| `LOOP_ADDR` | 控制面监听地址，默认 `127.0.0.1:8101` |
| `LOOP_DATA_DIR` | 数据目录，默认 `<v2 根>/data`（从工作目录向上找 `CONTRACT.md` + `backend/`） |
| `LOOP_DB_PATH` / `LOOP_ARTIFACTS_DIR` | 覆盖 SQLite 路径 / 证据目录。证据目录必须与执行器一致，否则截图 404 |
| `LOOP_MAX_MODEL_CALLS` | 调用次数上限，默认 40 |
| `LOOP_MAX_TOTAL_TOKENS` | **成本熔断**：整个 run 累计 token 超过它就中止，默认 1500000；设 `0` 关闭 |
| `PLAYWRIGHT_BROWSERS_PATH` | 执行器需要；本机浏览器在 `D:\PlaywrightBrowsers` |

缺密钥会直接启动失败，不会静默降级到别的模型。`/api/health` 会回报当前模型、端点，
以及证据目录是否与执行器一致。

### 真实模型：火山方舟（Ark）

provider 默认值就是 `lark` 的正式配置，所以只需要提供方 + 密钥来源两项：

```powershell
cd v2\backend
$env:LOOP_LLM_PROVIDER   = 'lark'
$env:LOOP_LLM_API_KEY_ENV = 'LARK_API_KEY'   # 密钥从同名环境变量读，不写进命令行
$env:LARK_API_KEY        = '<your key>'
go run ./cmd/loopd
```

对应的默认值是 `baseURL = https://ark.cn-beijing.volces.com/api/plan/v3`、
`model = deepseek-v4.1-flash[1m]`。端点**不要**带 `/chat/completions`：客户端会自己追加。
换模型只改 `LOOP_LLM_MODEL`（例如 `deepseek-v4-pro[1m]`）。

### 成本

每次模型调用的 `usage` 都会增量累加到 `model_usage` 表（一个 run 一行），并同时发一条
`model_usage` SSE 事件，所以**规划过程中就能看着成本涨**。`GET /api/runs` 与
`GET /api/runs/{id}` 都带 `usage` 对象：

```json
{"model_calls":7,"prompt_tokens":41230,"completion_tokens":1180,
 "total_tokens":42410,"reasoning_tokens":640,"cached_tokens":0}
```

`reasoning_tokens` 已含在 `completion_tokens` 内、`cached_tokens` 已含在 `prompt_tokens` 内，
只是把成本构成摊开，**不能再加一遍**。前端只显示 token，不做金额换算（单价随模型与时段变，
前端算钱只会算错）。

超预算时 run 直接失败（`status=failed`，不是 case 失败），错误信息给出已用/上限与调法；
超预算那一次调用的用量同样记账。离线脚本模型不报用量，因此熔断对它天然不生效——
这正是离线验证可以反复跑的前提。

## 明确不做

用例库、套件、回归、定位调试页、research 试点、多 profile、契约生成器、微服务、鉴权、视觉定位。**一概不做。**
