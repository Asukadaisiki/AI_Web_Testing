# CONTRACT.md — 唯一权威契约

本文件是 `v2/` 里所有跨进程数据的唯一权威描述。Go 的 `internal/contract`、Python 的 `loop_worker/contracts.py`、Web 的 `types.ts` 都是它的镜像；三者由 `fixtures/contract/*.json` 的一致性测试保证不漂移。

**改契约的唯一正确流程**：改本文件 → 改 Go 类型 → 改 Python/TS 镜像 → 补 fixture → 跑一致性测试。

**阅读顺序**：先看 §9 的**会话**——它是整个闭环的容器，case / 执行 / 产物都挂在它下面；
然后 §2（case 工件）、§3（观测）、§4（执行结果）。

**一物一名**：`session` 只有一种含义（§9，用户可见的会话）。执行器内部那个浏览器上下文
叫 **browser session**（id 前缀 `bsess_`），与 `session` 无关，任何地方都不得混用。

---

## 1. 为什么不给模型写 JSON 的机会

模型**不能**产出 case。模型只能调用工具（§5），case 由 Go 侧根据工具调用序列构建。因此：

- 不存在"模型写出的 JSON 过不了校验"这种情况 —— 校验发生在每次工具调用上。
- 不存在"模型把条件写错阶段"这种情况 —— 条件由 Go 派生，模型只表达期望。

---

## 2. Case 工件（唯一可执行形态）

```jsonc
{
  "case_version": "loop.case.v1",
  "name": "把 Blue Top 加入购物车并校验",
  "goal": "用户原始目标",
  "base_url": "https://automationexercise.com",
  "steps": [ /* Step[]，至少 1 步，且 steps[0].action 必须是 "goto" */ ]
}
```

### 2.1 Step

```jsonc
{
  "index": 0,
  "action": "goto",              // goto | click | input | assert_text | assert_url
  "intent": "打开商品列表",       // 人类可读，来自模型的自然语言
  "value": "https://...",        // goto: 绝对 URL；input: 输入值；assert_text: 期望文本；assert_url: URL 子串；click: null
  "submit": false,               // 仅 input 允许；true = 填完之后按回车提交
  "target": {                    // 仅 click / input 需要；必须已接地
    "hint": "Add to cart",
    "locator": { "kind": "role", "role": "button", "name": "Add to cart", "exact": true },
    "grounding": {
      "observation_id": "obs_...",
      "page_state_id": "ps_...",
      "candidate_id": "cand_...",
      "page_url": "https://automationexercise.com/product_details/1"
    }
  },
  "preconditions":  [ /* Condition[] */ ],
  "postconditions": [ /* Condition[] */ ],
  "timeout_ms": 20000
}
```

约束：

| action | value | target | preconditions | postconditions |
|---|---|---|---|---|
| `goto` | 必填，绝对 URL | 禁止 | **必须为空**（首步在 `about:blank` 上，任何前置条件都不可满足） | ≥1 |
| `click` | 禁止 | 必填且已接地 | ≥1（只能状态事实） | ≥1 |
| `input` | 必填（可为空串） | 必填且已接地 | ≥1（只能状态事实） | ≥1 |
| `assert_text` | 必填 | 禁止 | ≥1（只能状态事实） | ≥1 |
| `assert_url` | 必填 | 禁止 | ≥1（只能状态事实） | ≥1 |

`steps[0].action != "goto"` 一律拒绝：执行器不为首步做隐式预导航。

**`submit` 只允许出现在 `input` 上**（其他动作带 `submit` → `step_unexpected_submit`）。

存在的理由：真实站点上的搜索提交控件常常是纯图标按钮——可访问名与 `text` 都是同一个
私有区码位（例如 `"\uf002"`），**模型不可能用任何 hint 指到它**，于是"在页面里搜索"
这一步在契约层面无法表达。回车是表单的原生提交方式，不需要指到任何控件，因此把它做成
契约的显式字段，而不是让执行器"填完顺手回车"（那会在需要纯填值的场景里帮倒忙）。

作者态执行（`POST /sessions/{id}/act`）同样带 `submit`：作者态必须真的提交，
否则观测停在原页面，后续步骤的接地会锚在错的页面上。

**实测边界（automationexercise.com，2026-09-23）**：该站的搜索按钮是
`<button type="button" id="submit_search"><i class="fa fa-search"></i></button>`——
是 `type="button"`，不是 `type="submit"`。于是：

| 做法 | 结果 |
|---|---|
| `input(submit=true)`（回车） | ❌ 不提交，URL 不变 |
| 点 `#submit_search` | ✅ 提交，跳 `/products?search=Blue%20Top` |
| `form.requestSubmit()` | ❌ 不提交 |

**`submit` 只解决"表单能被回车提交"这一类站点。** 当站点的提交控件是 `type="button"`
+ JS 时，回车不是出路，唯一出路是能指到那个纯图标按钮本身——那是另一个能力缺口
（别名匹配面），尚未实现。不要因为本字段存在就假定搜索一定能跑通。

**缺省超时**：条件 `10000ms`、步骤 `20000ms`。这两个数是按真实站点实测定的：
冷启动（干跑用的是全新 context，没有任何缓存）打开 automationexercise.com 的
domcontentloaded 实测 5.0–6.1s，站内跳转 `/products` 实测 6.3s。原来的 5000/3000
正好卡在真实耗时上，导致**干跑永远不可能通过**，而干跑不过 case 就永远建不出来。

### 2.2 Condition

```jsonc
{ "type": "text_visible", "value": "Added!", "timeout_ms": 10000 }
```

**条件阶段表（唯一权威，全仓库只有这一处）**：

| type | 可作 pre | 可作 post | 语义 |
|---|---|---|---|
| `url_contains` | ✅ | ✅ | 当前 URL 包含 `value` |
| `text_visible` | ✅ | ✅ | 页面上存在可见的、文本包含 `value` 的元素 |
| `text_gone` | ✅ | ✅ | 页面上不存在可见的、文本包含 `value` 的元素 |
| `url_changes` | ❌ | ✅ | 当前 URL 与执行本步前不同 |
| `value_equals` | ❌ | ✅ | 本步 target 元素的 `value` 等于 `value` |

规则：

- **pre 只能是状态事实**（`url_contains` / `text_visible` / `text_gone`）。`url_changes` / `value_equals` 是变化事实，动作前没有真值，作为 pre 一律拒绝。
- 不存在 `element_visible` / `element_gone` / `network_request`：v1 不支持，未在表中即为非法。

### 2.3 条件派生规则（模型不写条件，Go 派生）

| 工具调用 | 派生的 preconditions | 派生的 postconditions |
|---|---|---|
| `open_page(url)` | `[]` | `[{url_contains, path(url)}]` |
| `click(hint, expect_*)` | `[{url_contains, path(最近观测页 URL)}]` | 由 `expect_*` 派生，见下 |
| `input(hint, value, expect_*)` | `[{url_contains, path(最近观测页 URL)}]` | 同上 |
| `assert_text(text)` | `[{url_contains, path(最近观测页 URL)}]` | `[{text_visible, text}]` |
| `assert_url(contains)` | `[{url_contains, path(最近观测页 URL)}]` | `[{url_contains, contains}]` |

`expect_*` → postcondition 的映射：

| 模型表达 | 派生条件 |
|---|---|
| `expect_text: "Added!"` | `{text_visible, "Added!"}` |
| `expect_gone: "Loading"` | `{text_gone, "Loading"}` |
| `expect_url: "/view_cart"` | `{url_contains, "/view_cart"}` |
| `expect_value: "1"` | `{value_equals, "1"}` |
| 都没给 | 工具报错：动作步骤必须声明至少一个期望 |

因为 pre 由"最近观测页 URL"派生，而前一步的 postconditions 已保证到达该页，**pre 天然可满足**，不会出现"条件永远判不过"。

### 2.4 校验错误码（Go 与 Python 必须逐字一致）

`fixtures/contract/case_contract.json` 是两侧共读的一致性夹具：同一份 case，Go 的
`contract.Validate` 与 Python 的 `contracts.validate_case` 必须给出相同的 accept 与错误码。

| 错误码 | 触发条件 |
|---|---|
| `case_invalid_json` | 不是 JSON、类型不符、出现未知字段 |
| `case_version_mismatch` | `case_version` 非空且不等于 `loop.case.v1` |
| `case_name_required` | `name` 为空 |
| `case_empty_steps` | `steps` 为空 |
| `case_not_goto_first` | `steps[0].action != "goto"` |
| `step_index_mismatch` | `index` 与下标不符（归一化后不应出现） |
| `step_intent_required` | `intent` 为空 |
| `step_unknown_action` | `action` 不在 5 个动作内 |
| `step_missing_value` | `goto` / `input` / `assert_*` 缺 `value` |
| `step_unexpected_value` | `click` 带了 `value` |
| `step_unexpected_submit` | 非 `input` 动作带了 `submit` |
| `step_missing_target` | `click` / `input` 缺 `target` 或 `target.hint` 为空 |
| `step_unexpected_target` | `goto` / `assert_*` 带了 `target` |
| `step_target_ungrounded` | `target.grounding` 缺 `observation_id` / `candidate_id` / `page_url` |
| `locator_invalid` | 定位器 kind 非法或缺该 kind 的必填字段 |
| `step_missing_precondition` | 非 goto 步骤没有 preconditions |
| `step_missing_postcondition` | 任何步骤没有 postconditions |
| `goto_precondition_forbidden` | `goto` 声明了 preconditions |
| `condition_unknown_type` | 条件 type 不在阶段表中 |
| `condition_phase` | 变化事实（`url_changes` / `value_equals`）被用作 pre |
| `condition_missing_value` | 条件 `value` 为空 |
| `condition_missing_timeout` | 条件 `timeout_ms <= 0`（归一化后不应出现） |
| `goto_value_not_absolute` | `goto` 的 `value` 不是绝对 http(s) URL |

检查顺序也是契约的一部分：先整包解码（未知字段 / 类型），再 case 级，再逐步骤，
步骤内按上表自上而下。两侧必须一致，否则同一个坏 case 会得到不同错误码。

---

## 3. 观测（接地用的证据）

`POST /sessions/{id}/navigate` 与 `act` 返回（这里的 `{id}` 是**执行器的 browser session**，
见 §9 的命名约定）：

```jsonc
{
  "observation_id": "obs_7f3a",
  "page_state_id": "ps_2b91",
  "browser_session_id": "bsess_9c02",
  "url": "https://automationexercise.com/products",
  "title": "Automation Exercise - All Products",
  "elements": [
    {
      "ref": "e12",
      "tag": "button",
      "role": "button",
      "name": "Add to cart",
      "text": "Add to cart",
      "value": null,
      "visible": true,
      "enabled": true,
      "locators": [
        { "kind": "role", "role": "button", "name": "Add to cart", "exact": true, "match_count": 1 },
        { "kind": "text", "text": "Add to cart", "exact": true, "match_count": 1 },
        { "kind": "css", "css": "#cart-12 > button", "match_count": 1 }
      ]
    }
  ],
  "screenshot_path": "sess_4d1a/obs_7f3a.png"
}
```

`elements` 只保留可交互或有文本的元素，上限 200 条（超出时优先保留可见且 enabled 的）。`name` 的可访问名计算规则见 §6。

### 3.1 定位器必须在观测时就地验证（关键约束）

`locators` 是**按偏好排序、且已在当前页面上验证过**的定位器列表：

- 每条都必须是**当场真实解析过**的，`match_count` 为实际命中数；
- 只有 `match_count == 1` 的定位器才允许出现（保证"观测时能解析、执行时能命中"）；
- 偏好顺序：`role`（用浏览器计算的可访问名）→ `text` → `css`（结构路径，最后手段）；
- 若某元素的 `role` 定位器命中数 ≠ 1（例如可访问名含图标字体的私有区字形、或存在同名元素），**不得**输出该定位器，改用下一种；
- 一个元素一条定位器都验证不出来时，该元素不进入 `elements`。

执行期只使用 case 里已经记录的那一条定位器，不再重新推导。`candidate_id` 形如 `"<element ref>:<locators 下标>"`。

---

## 4. 执行结果

`POST /execute {"session_id": "sess_...", "case": {...}}` 返回
（`session_id` 必填：产物必须能落到会话目录里，见 §9）：

```jsonc
{
  "execution_id": "exec_...",
  "status": "passed" | "failed" | "error",
  "started_at": "2026-09-21T10:00:00Z",
  "finished_at": "2026-09-21T10:00:42Z",
  "final_url": "https://automationexercise.com/view_cart",
  "steps": [
    {
      "index": 0,
      "action": "goto",
      "status": "passed" | "failed",
      "started_at": "...",
      "duration_ms": 1830,
      "url_before": "about:blank",
      "url_after": "https://automationexercise.com/products",
      "conditions": [
        { "phase": "post", "type": "url_contains", "value": "/products", "satisfied": true, "detail": null }
      ],
      "evidence": {
        "screenshot_path": "sess_4d1a/exec_..._0.png",
        "console": [ { "level": "error", "text": "..." } ],
        "network": [ { "method": "GET", "url": "...", "status": 200 } ]
      },
      "error": null
    }
  ]
}
```

`status` 取值：`passed`（全部步骤与条件通过）、`failed`（有步骤或条件未通过）、`error`（执行器自身故障，例如浏览器启动失败、case 非法）。

`screenshot_path` 是**相对产物根目录**的 POSIX 路径，形如 `<session_id>/<文件名>`；
控制面把它直接拼成 `/artifacts/<screenshot_path>` 提供下载（§9.2）。不给出仓库绝对路径，
避免"换了工作目录就 404"。

`error` 字段（`steps[].error` 与顶层 `error`）是**对象**，不是字符串：

```jsonc
{ "kind": "target_not_found", "message": "locator role=button name='Add to cart' matched 0 elements" }
```

`kind` 取值即 §4.1 的信号种类；`message` 是给人看的原因。Go 侧直接把它当作失败信号，
不再从字符串里猜 kind。

### 4.1 失败信号

Go 侧从执行结果派生，落 `report_signals` 表：

| kind | 触发条件 |
|---|---|
| `target_not_found` | 元素定位在超时内未命中 |
| `condition_unmet` | 前置或后置条件未满足 |
| `step_timeout` | 单步超时 |
| `worker_error` | 执行器返回 error 或不可达 |
| `case_invalid` | case 未通过契约校验（正常情况下不应出现） |

---

## 5. 模型可用的工具（模型唯一的输出形式）

| 工具 | 参数 | 行为 |
|---|---|---|
| `open_page` | `url`, `intent` | 真实导航并观测；记录 goto 步骤 |
| `click` | `hint`, `intent`, `expect_text?`, `expect_gone?`, `expect_url?`, `expect_value?` | 在最近观测中解析 `hint`；**必须唯一命中**；记录 click 步骤 |
| `input` | `hint`, `value`, `intent`, `expect_*`, `submit?` | 同上，另填 `value`；`submit: true` 表示填完按回车提交 |
| `assert_text` | `text`, `intent` | 记录页面级文本断言 |
| `assert_url` | `contains`, `intent` | 记录页面级 URL 断言 |
| `finish_case` | `name` | 全量校验并落库为工件；run 进入 `awaiting_approval` |
| `ask_user` | `question` | run 进入 `awaiting_input`，等人回答后继续 |

工具失败时返回结构化错误（例如 `{"error":"target_not_found","hint":"...","candidates":[...]}`），模型必须据此改口重试，**不允许**把未接地的目标写进 case。

`click` / `input` 只在**最近一次观测所在的页面**上解析目标。模型若想点下一页的元素，必须先 `click`（带 `expect_url`）再继续用新观测。

---

## 6. 可访问名（a11y name）的唯一实现

`name` 一律取浏览器计算的可访问名（Playwright `get_by_role` 的匹配语义），**不允许**用 DOM 文本拼接代替。若元素的可访问名含私有区字形（图标字体），执行期必须使用同一条计算路径，保证"观测时能解析、执行时能命中"。

Python 侧只有一处实现：`loop_worker/observer.py::accessible_name`。

---

## 7. 失败回灌（报告 → 输入）

半自动：

1. Go 侧在报告生成时，对每个 run 的失败信号生成候选输入，落 `feedback_candidates`。
2. 候选生成规则（通用，不含任何任务专有名词）：
   - 按 `kind` 去重，最多 3 条；
   - 每条候选输入 = 原始 `input` + 结构化失败摘要块：

     ```
     原目标：<input>
     上一轮失败：
     - 第 <n> 步（<intent>）：<kind 的中文说明>
     请在重新规划时避免上述失败。
     ```
3. 人在错误注入页编辑/确认后，创建新 run：`input = 候选文本`，`parent_run_id = 原 run`。
   候选是**一次性的**：确认后即标记 `used`，同一条候选再确认返回 409 `candidate_already_used`——
   一个失败只允许回灌一轮，要再试就编辑输入后走“在同一会话里再开一轮”（页面 1）。

---

## 8. 数据表（SQLite）

```sql
sessions(id, goal, created_at, updated_at)                           -- 闭环的容器（§9）
runs(id, session_id, input, status, parent_run_id, error, created_at, updated_at)
run_events(id, run_id, seq, type, payload_json, created_at)          -- SSE 重放
cases(id, session_id, run_id, content_hash, payload_json, created_at) -- 不可变工件
case_approvals(id, case_id, approved_by, created_at)
executions(id, run_id, case_id, status, result_json, started_at, finished_at)
execution_steps(id, execution_id, step_index, action, status, evidence_json, error)
report_signals(id, run_id, execution_id, step_index, kind, message, created_at)
feedback_candidates(id, run_id, signal_kind, proposed_input, status, created_at)
model_usage(run_id, model_calls, prompt_tokens, completion_tokens, total_tokens, reasoning_tokens, cached_tokens, updated_at)
```

`runs.session_id` 与 `cases.session_id` 都指向 §9 的会话；`cases.run_id` 仍保留，
用于回答"这个 case 是哪一轮产出的"。`runs.parent_run_id` 指出回灌链条上的上一轮。

`runs.status`：`planning` → `awaiting_approval` → `executing` → `reporting` → `completed` / `failed`；分支状态 `awaiting_input`。

`completed` 与 `failed` 的区别是**闭环有没有走完**，不是用例有没有通过：

| 情况 | runs.status | 报告里体现 |
|---|---|---|
| 用例执行完，全部步骤通过 | `completed` | `steps_failed = 0`，无 signals |
| 用例执行完，有步骤失败 | `completed` | `steps_failed > 0`，signals 非空，自动生成回灌候选 |
| 执行器返回 `status = "error"` | `failed` | signals 含 `worker_error` |
| 执行器不可达 / case 过不了校验 | `failed` | signals 含 `worker_error` / `case_invalid`，仍有回灌候选 |
| 规划期出错（模型失败、超上限、干跑一直不过） | `failed` | 没有 case 工件，没有执行 |

只有"执行跑完"才可能产出报告；`failed` 时报告页仍会给出 signals 与回灌候选，避免死路。

`cases` 只有一个形态，执行与报告都按 `case_id` 读 `payload_json`，**没有第二数据源，没有回退链**。

### 8.1 规划阶段的干跑（写进契约的硬约束）

`finish_case` 不是"落库"，而是"**校验 + 在全新浏览器上下文里干跑一遍**"：

- 干跑通过 → 落 `cases`，run 进入 `awaiting_approval`，人看到的是**已经被证明能跑通的**工件；
- 干跑失败 → **不落库**，把失败步骤与未满足的条件结构化回给模型，让它改口重来。

因此 `awaiting_approval` 状态下的 case 天然满足两条：过得了契约校验（构建即校验）、
在当前站点上确实跑得通（干跑证明）。真实执行仍可能失败（站点变了、抖动），
那一类失败由 §7 的回灌闭环处理。

---

## 9. 会话（session）：闭环的容器

### 9.1 一个会话 = 一个目标 + 它的全部轮次

```
session (sess_...)
├── run 1  planning → awaiting_approval → executing → reporting → completed
│     └── case 1 (payload_json)   executions / signals / feedback
└── run 2  （由 run 1 的失败回灌产生，parent_run_id = run 1）
      └── case 2 ...
```

- **会话是唯一的产品级容器**。人输入的"目标"属于会话，不属于某一轮；
  回灌产生的下一轮**属于同一个会话**，不是新会话。
- 一个会话至少有一轮 run。第一轮在会话创建时同时产生。
- `runs.parent_run_id` 记录轮次链条（第 1 轮为 `null`），`runs.session_id` 记录归属。
- **case / DSL 绑定会话**：`cases.session_id`。`cases.run_id` 保留，回答"哪一轮产出的"。
- **产物绑定会话**：证据文件落在 `<产物根>/<session_id>/`，见 §9.2。
- **成本按会话汇总**：会话的 token 用量 = 它全部轮次的 `model_usage` 之和。

### 9.2 产物路径（唯一约定）

产物根目录由 `LOOP_ARTIFACTS_DIR` 指定，默认 `<仓库根>/data/sessions`。布局：

```
data/sessions/
└── sess_4d1a/                      ← 一个会话一个目录
    ├── obs_7f3a.png                ← 规划期观测截图
    ├── exec_b118d79f_0.png         ← 执行期每步截图
    └── exec_b118d79f_1.png
```

- 执行器**只**往 `<产物根>/<session_id>/` 写，文件名自定。
- 控制面**只**从同一个根目录读，HTTP 路径为 `/artifacts/<session_id>/<文件名>`。
- 契约里出现的 `screenshot_path` 一律是 `<session_id>/<文件名>` 这种**相对产物根**的形式，
  两边都不写绝对路径。

### 9.3 命名（一物一名，不得混用）

| 名字 | 含义 | id 前缀 | 谁能看见 |
|---|---|---|---|
| `session` / 会话 | 目标 + 全部轮次 | `sess_` | 用户 |
| `run` | 会话里的一轮闭环 | `run_` | 用户 |
| `browser session` | 执行器里的一个浏览器上下文，用完即弃 | `bsess_` | 仅执行器内部 |

`browser session` **不是**会话：它没有目标、不跨轮次、不进数据库。契约里凡出现
`browser_session_id` 的地方都只表示这个临时句柄。

### 9.4 会话状态

会话自身**没有状态机**——状态是每轮 run 的事（§8）。会话列表里显示的"状态"
是**它最新一轮**的状态，仅用于列表展示，不参与任何判断。
