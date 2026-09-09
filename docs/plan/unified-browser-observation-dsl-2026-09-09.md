# Browser Observation、TargetBinding 与 DSL 统一方案

日期：2026-09-09
状态：implemented，live Agent E2E pending

## 目标

统一 A11y Tree、DOM、DSL、locator preflight 和 Playwright Runner 的语义，
消除同一 target 在不同阶段被不同规则解释的问题，同时控制浏览器证据、
模型上下文和 PostgreSQL 事件体积。

目标链路：

```text
User Goal
  -> Intent TaskPlan
  -> Browser Observation
  -> Grounded TargetBinding
  -> Draft DSL
  -> Executable DSL Compiler
  -> Approval
  -> Playwright Runner
  -> Step Evidence / Report
```

## 非目标

- 不为某个商品、站点、URL、selector 或固定操作顺序增加分支。
- 不让 LLM 直接生成可执行 selector 或 locator candidates。
- 不以完整 DOM 代替 A11y，也不以 A11y 代替全部 DOM/runtime 事实。
- 不把 Vision 作为默认定位路径。
- 不在本阶段扩大为无约束的通用浏览器 Agent。

## 实际页面调研

调研日期：2026-09-09。数量来自浏览器工具的 compact A11y snapshot 和页面
DOM 只读统计，用于比较页面形态，不等同于 Browser Worker 最终过滤后的节点数。

| 页面类型 | URL | 实际观察 | 对统一合同的要求 |
|---|---|---|---|
| 原生表单 | `https://www.selenium.dev/selenium/web/web-form.html` | 109 个 DOM 元素、18 个表单控件、0 个显式 ARIA 元素；A11y 仍产生 21 个交互节点和完整 label name | 必须支持隐式 HTML 语义、label、placeholder、disabled、readonly、select、checkbox、radio、file、date、range |
| 政府搜索 | `https://www.gov.uk/search/all` | 605 个 DOM 元素、141 个宽口径交互元素、3 个 form；存在 cookie region、landmark 和重复导航 | page state 不能只看 URL；需要 overlay/banner 状态和 landmark scope |
| React SPA | `https://todomvc.com/examples/react/dist/` | 初始 A11y 36 节点，新增 todo 后变为 50；DOM 中存在 hover 才显示的 delete button 和 checkbox，但 compact A11y 未暴露这些控件 | 必须支持同 URL 状态 revision、动态节点失效和 DOM-only interactive supplement |
| 长文与大表格 | `https://en.wikipedia.org/wiki/List_of_countries_and_dependencies_by_population` | DOM 超过 10,000 个元素，4 个 table、259 个 row；compact A11y 主要保留首屏 landmark | 不能全量传 DOM/A11y；需要 region/table query、分页或按任务提取 |
| 动态数据表格 | `https://datatables.net/examples/core/basic_init/zero_configuration.html` | 2,401 个 DOM 元素、418 个 compact A11y 节点；表格只渲染 10 行，排序、搜索、分页改变同 URL 状态 | 需要 row/cell 关系、排序状态、分页状态和 delta observation |
| ARIA composite | `https://www.w3.org/WAI/ARIA/apg/patterns/combobox/examples/combobox-autocomplete-list/` | combobox 依赖 `aria-expanded`、`aria-controls`、`aria-activedescendant`，DOM 中有 56 个 option | ElementFact 必须保留 A11y state 和 relation，不能只保留 role/name |
| Shadow DOM | `https://mdn.github.io/web-components-examples/popup-info-box-web-component/` | light DOM 52 个元素，存在 1 个 open shadow root；shadow 内部有独立交互节点 | context path 必须表达 frame/shadow 边界；XPath 不得用于穿透 shadow root |

Playwright 官方文档的定位优先级是用户可感知语义和显式测试合同：
role、label、placeholder、text、test id；CSS/XPath 是最后手段。Playwright
locator 每次动作都会重新解析当前 DOM，open Shadow DOM 默认可由多数 locator
穿透，但 XPath 不穿透，closed Shadow DOM 不受支持。

## 当前问题

### 1. A11y 与 DOM 字段混合

当前采集先读取 CDP Full AX Tree，再按 `backendDOMNodeId` 读取 DOM 属性。
当 DOM `textContent` 与 accessible name 不同时，代码会用 `textContent` 覆盖
`name`。这会使：

- preflight 的 `role + name` 不再等于 Playwright 的 `get_by_role` 语义；
- 父节点重复携带大量后代文本；
- 文本验证事实和交互元素身份混在一起。

### 2. 自定义 target 字符串存在多套解析器

Runtime 支持：

```text
role="name"
role="name" inside "scope"
role="name" inside product "scope"
```

Preflight 只识别：

```text
... inside product "scope"
```

这已经形成 BUG-168：相同 DSL target 在预检和执行阶段可能产生不同候选。

### 3. TaskPlan target 与执行 target 耦合

TaskPlan 在探索前定义业务 target；`generate_dsl` 又要求 DSL target 与 PlanStep
完全相同。业务语义如“结果列表中对应记录的编辑入口”不一定是合法的
Playwright locator 表达式。探索后得到的 locator facts 没有独立归属层。

### 4. Preflight 和 Runner 重复实现

- Python preflight 根据 snapshot 构造 candidates。
- Python Runner 再根据 strategy 构造 Playwright locator。
- Go 和 Python 分别维护 candidate strategy 白名单。
- semantic parser、scope parser 和 role normalization 也存在重复。

### 5. 证据与上下文仍会增长

最近 Run `run_a85adb2d33a4dcf463ba5e70`：

| 指标 | 数值 |
|---|---:|
| 3 条探索 raw result | 664,631 bytes |
| 最大单条 raw result | 259,481 bytes |
| 3 条模型摘要累计 | 95,641 bytes |
| 最后一次模型请求 | 390,352 bytes |
| 最后一次 input tokens | 95,865 |

当前 32 KiB 单摘要、64 KiB 硬上限、160 KiB 累计预算阻止了原始结果直接灌入
模型，但 Plan、tool arguments、历史消息和多个摘要仍会持续累积。

## 设计原则

1. 事实分源：A11y、DOM、runtime 和 visual evidence 不互相覆盖。
2. 语义与定位分离：PlanStep 保存业务语义，TargetBinding 保存执行绑定。
3. 单一编译器：preflight 与 Runner 使用同一个 LocatorSpec 编译入口。
4. LLM 不写 selector：AI 选择意图和 binding，不构造 candidates。
5. 按需观察：默认只采交互元素、关系节点和当前 PlanStep 相关事实。
6. 不信任快照：正式执行前仍在当前页面重新解析并验证 locator。
7. 只执行一次：locator fallback 发生在动作派发前；动作派发后不得盲目重放。
8. 可审计：每个 candidate 都能回溯 observation、元素和验证结果。

## 统一领域模型

### BrowserObservation

```json
{
  "schema_version": "browser.observation.v2",
  "observation_id": "obs_xxx",
  "page_state": {
    "state_id": "state_xxx",
    "revision": 3,
    "url": "https://example.test/items",
    "title": "Items",
    "frame_path": [],
    "state_sha256": "...",
    "previous_state_sha256": "..."
  },
  "elements": [],
  "relations": [],
  "artifacts": {
    "snapshot_uri": "artifact://browser-observation/obs_xxx",
    "sha256": "...",
    "content_bytes": 12345
  }
}
```

`state_id` 不能只由 URL 产生。至少包含：

- normalized URL；
- document/frame identity；
- 可见 dialog/overlay；
- 关键 landmark；
- 当前 route/history state；
- 交互元素集合摘要。

同 URL 的 SPA 更新、combobox 展开和表格翻页必须产生新 revision。

### ElementFact

```json
{
  "element_ref": "el_xxx",
  "context_path": {
    "frames": [],
    "shadow_hosts": []
  },
  "a11y": {
    "role": "combobox",
    "name": "State",
    "description": null,
    "value": "",
    "states": {
      "expanded": false,
      "disabled": false,
      "readonly": false,
      "checked": null,
      "selected": null
    },
    "relations": {
      "controls": ["el_listbox"],
      "active_descendant": null,
      "labelled_by": ["el_label"]
    }
  },
  "dom": {
    "backend_node_id": 101,
    "tag": "input",
    "attrs": {
      "id": "state",
      "type": "text"
    },
    "text": ""
  },
  "runtime": {
    "connected": true,
    "visible": true,
    "enabled": true,
    "editable": true
  }
}
```

约束：

- `a11y.name` 永远是浏览器计算的 accessible name。
- `dom.text` 单独保存且有长度上限，不得覆盖 `a11y.name`。
- 无 AX 映射的 DOM 控件允许 `a11y=null`。
- 无 DOM 映射的虚拟 AX 节点允许 `dom=null`。
- `backendDOMNodeId` 只用于同一 document snapshot 内关联，不作为跨导航稳定 ID。
- password、secret 和用户敏感输入不得写入 observation。

### LocatorSpec

Locator 不再使用自定义字符串语法，使用封闭联合类型：

```json
{
  "kind": "role",
  "role": "button",
  "name": "Save",
  "exact": true
}
```

```json
{
  "kind": "scoped",
  "scope": {
    "kind": "role",
    "role": "row",
    "name": "Alice"
  },
  "target": {
    "kind": "role",
    "role": "button",
    "name": "Edit",
    "exact": true
  }
}
```

首阶段允许的 kind：

- `role`
- `label`
- `placeholder`
- `text`
- `test_id`
- `css`
- `xpath`
- `scoped`

后续可显式增加：

- `frame`
- `alt_text`
- `title`

规则：

- `xpath` 不允许声明 shadow piercing。
- closed Shadow DOM 无可执行 candidate 时返回 `unsupported_context`。
- `scoped` 直接映射 Playwright locator chaining/filter，不向上猜测固定 DOM 层级。
- 不允许任意 JavaScript 或字符串拼接 locator。

### TargetBinding

```json
{
  "schema_version": "grounding.target-binding.v1",
  "binding_id": "binding_xxx",
  "plan_id": "plan_xxx",
  "plan_version": 2,
  "plan_step_id": "step_4",
  "semantic_target": "指定记录的编辑入口",
  "action": "click",
  "page_state_id": "state_xxx",
  "observation_id": "obs_xxx",
  "element_refs": ["el_12"],
  "candidates": [
    {
      "candidate_id": "candidate_1",
      "element_ref": "el_12",
      "context_path": {
        "frames": [],
        "shadow_hosts": []
      },
      "locator": {
        "kind": "scoped",
        "scope": {
          "kind": "role",
          "role": "row",
          "name": "Alice"
        },
        "target": {
          "kind": "role",
          "role": "button",
          "name": "Edit",
          "exact": true
        }
      },
      "provenance": "a11y_exact",
      "observed_count": 1,
      "visible": true,
      "enabled": true,
      "score": 0.95
    }
  ],
  "selected_candidate_id": "candidate_1",
  "binding_sha256": "..."
}
```

TargetBinding 是 Grounding 的结果，不是 TaskPlan 的输入。它绑定：

- PlanStep；
- page state；
- observation；
- element facts；
- locator candidates；
- actionability；
- provenance。

### Draft DSL

LLM 只提交业务字段和 binding 引用：

```json
{
  "schema_version": "dsl.draft.v2",
  "profile": "research-v2",
  "steps": [
    {
      "plan_step_id": "step_4",
      "action": "click",
      "intent": "打开指定记录",
      "target_binding_id": "binding_xxx",
      "preconditions": [],
      "postconditions": [
        {"type": "url_contains", "value": "/details/"}
      ],
      "idempotency": "idempotent",
      "side_effect": "browser_state"
    }
  ]
}
```

Draft DSL 不允许：

- `selector`
- `target_strategy`
- `candidates`
- 自定义 `role="..." inside ...` 字符串
- 未绑定的自由文本执行 target
- 未绑定的 `element_visible` / `element_gone` 条件；元素条件必须改为
  TargetBinding，兼容期可使用 URL、text、value 条件

### Executable DSL

Go DSL Compiler 校验 TaskPlan、Draft DSL 和 TargetBinding 后生成不可变执行快照：

```json
{
  "schema_version": "dsl.executable.v3",
  "canonical_version": "dsl.canonical.v3",
  "plan_binding": {
    "plan_id": "plan_xxx",
    "version": 2,
    "sha256": "..."
  },
  "observation_bindings": [
    {
      "binding_id": "binding_xxx",
      "binding_sha256": "...",
      "observation_sha256": "..."
    }
  ],
  "steps": [
    {
      "plan_step_id": "step_4",
      "action": "click",
      "intent": "打开指定记录",
      "semantic_target": "指定记录的编辑入口",
      "locator_candidates": [],
      "preconditions": [],
      "postconditions": [],
      "idempotency": "idempotent",
      "side_effect": "browser_state"
    }
  ]
}
```

Executable DSL 中的 candidates 只能由编译器从 TargetBinding 注入。

## 单一 Capability Manifest

新增版本化 `browser.capabilities.v1`，作为 Go、Python 和前端展示的唯一事实源：

```json
{
  "actions": {
    "click": {
      "requires_target": true,
      "requires_unique_match": true,
      "requires_visible": true,
      "requires_enabled": true
    },
    "input": {
      "requires_target": true,
      "requires_editable": true
    },
    "wait_for": {
      "requires_target": false
    }
  },
  "locator_kinds": [
    "role",
    "label",
    "placeholder",
    "text",
    "test_id",
    "css",
    "xpath",
    "scoped"
  ]
}
```

约束：

- JSON Schema/manifest 是枚举唯一来源。
- Go 负责 schema、TaskPlan、hash 和授权校验。
- Python 负责 `LocatorSpec -> Playwright Locator` 的唯一编译实现。
- Preflight 和 Runner 必须调用同一个 compiler，不得各自维护正则。
- 合同测试必须逐个 locator kind 验证 Go 接受、Python 可解析、Runner 可执行。

## Observation 采集策略

### 默认采集

只保留：

- 可交互 AX 节点；
- 当前 PlanStep 关键词命中的验证事实；
- 上述节点的必要祖先和关系节点；
- DOM-only interactive controls；
- dialog、alert、status、table/grid、form 和 landmark 摘要；
- 每个候选的实时 count/visible/enabled/editable。

### 按需扩展

- `query_by_role`
- `query_by_text`
- `query_region`
- `query_table`
- `query_frame`
- `query_shadow_root`
- `observe_state_delta`

禁止默认读取完整 DOM 或完整长表格。Wikipedia/DataTables 类页面必须先定位 region、
table 或分页，再返回目标行列。

### 动态状态

对 SPA、dialog、combobox、table 和 virtual list：

- 每次结构变化产生 observation revision；
- action 记录 before/after state SHA；
- 相同 element 的新 revision 生成新的 runtime facts；
- 旧 locator candidate 仅作为 provenance，不自动视为当前仍有效。

## Preflight

Preflight 分为两层：

### Binding Preflight

在 disposable BrowserContext 中完成：

1. 编译 LocatorSpec；
2. 检查 count；
3. 检查 visible/enabled/editable；
4. 验证 frame/shadow context；
5. 记录 candidate 和 observation hash；
6. 不触发目标业务动作。

### Execution Preflight

Runner 在每个动作派发前重新执行：

1. 根据 LocatorSpec 解析当前 locator；
2. 要求唯一匹配；
3. 检查 actionability；
4. 核对关键 element fingerprint；
5. 失败则停止并输出 `locator_stale`，不得改用未验证自由文本定位。

Preflight 只验证“能定位”，不修改 TaskPlan 语义，也不判断业务目标是否完成。

## Runner

Runner 只接受 Executable DSL：

- 不解析自定义 target 字符串；
- 不从 semantic target 临时创造 locator；
- 按预排序 candidates 做动作前 fallback；
- candidate 全部失效时停止并请求 re-grounding；
- 动作只派发一次；
- side effect 已提交或未知时禁止自动重放；
- 每步记录 candidate attempts、selected candidate、action outcome、条件结果和证据。

## 数据预算

### Raw Evidence

- 每个 page state 保存一个基线 snapshot。
- 后续 action 保存 delta、target evidence 和新 snapshot ref。
- snapshot 使用内容寻址和压缩 artifact。
- event 只保存 URI、SHA、原始/压缩字节数和 schema version。
- 单 snapshot 超过 512 KiB 时按 frame/region/table 分片。
- DOM text 每节点默认最多 256 UTF-8 字符；超出部分单独 artifact 化。

### Agent Context

- 单次 observation delta 目标上限 16 KiB。
- 活跃 observation 窗口上限 48 KiB。
- 每个 PlanStep 最多保留 5 个候选、20 个相关 ElementFact。
- 已 grounded step 只保留 binding ref 和完成事实。
- 旧 revision 只保留 state SHA 和 artifact ref。
- 超预算时由确定性裁剪器处理，不调用额外 LLM 压缩。

### Run Budget

- 最大 LLM logical calls；
- 最大 input/output tokens；
- 每个 TaskPlan ID/version 的 `explore_page=5`、`explore_flow=4`；
- 整个 AgentRun 的 `explore_page=10`、`explore_flow=8` 硬上限，计划改版不得重置；
- 同一计划版本内，规范化 page/flow 签名只能完成一次；description、timeout
  和 observation version 不参与签名，不能用于绕过；
- 最大 raw evidence bytes；
- 最大 wall-clock deadline。

任一预算耗尽后进入明确的 `blocked` 或 `failed`，不得继续隐式重试。
`set_task_plan`、每次探索摘要和 gate failure 都必须向模型暴露 used、limit、
remaining、重复签名限制和当前签名 fingerprint。

### Exploration Action Contract

- 用于 grounding 的 flow action 必须显式携带 `plan_step_id`，服务端优先按 ID
  校验动作、值、副作用和结果归属，不依赖相似文本或 action 顺序猜测。
- 支持性观察可以省略 `plan_step_id`，但不能推进 PlanStep。
- `wait_for` 的元素目标只能使用 role/label/placeholder/text 语义 LocatorSpec。
- 控件值使用 `{"type":"value_equals","expected":"..."}`，不得将 label 当作值文本。
- click/input 候选先过滤动作兼容的交互元素，再判断 exact name 和现场唯一性。
- external/unknown side effect 只能通过 wait_for 观察控件，不得在探索中执行。

## 跨站验收矩阵

实现不得以单一命名业务任务验收。至少覆盖：

| 场景 | 验收重点 |
|---|---|
| Selenium Web Form | implicit label、disabled/readonly、select、checkbox、radio、file、date、range |
| GOV.UK Search | landmark、cookie overlay、表单提交、重复导航 |
| TodoMVC | 同 URL SPA revision、动态列表、hover 控件、checked state |
| DataTables | table/row/cell、排序、搜索、分页、同 URL 数据变化 |
| WAI ARIA Combobox | expanded、controls、activedescendant、option selection、keyboard |
| MDN Web Component | open Shadow DOM、shadow context、XPath 限制 |
| Wikipedia Table | 大 DOM、按 region/table 查询、数据提取、上下文预算 |

每类场景验证：

1. Observation 不丢失任务所需事实。
2. Agent context 不超过预算。
3. TargetBinding 可追溯到 observation。
4. Preflight 与 Runner 使用同一 LocatorSpec 编译结果。
5. 正式执行在新 BrowserContext 中通过。
6. Report 能还原 locator attempts 和状态变化。
7. 新增场景只增加 fixture，不修改通用 runtime。

## 迁移顺序

### Phase 0：冻结与基线

- 固定现有 `research-v1` 为只读兼容。
- 为当前 A11y/DOM/raw/model bytes 建立指标。
- 新增上述跨站 acceptance fixtures。

### Phase 1：先定义合同

- 定义 BrowserObservation、ElementFact、LocatorSpec、TargetBinding。
- 定义 capability manifest 和 JSON Schema。
- Go/Python 增加共享 golden contract tests。

### Phase 2：Observation v2

- 保留 accessible name，不再被 DOM text 覆盖。
- 增加完整 AX states/relations。
- 增加 frame/shadow context。
- raw snapshot 改为 artifact + event ref。
- 模型只接收 step-scoped delta。

### Phase 3：GroundedPlan

- PlanStep 继续保存业务 semantic target。
- Grounding 持久化 TargetBinding。
- TaskPlan ready 条件改为每个可执行 step 都有有效 binding。

### Phase 4：DSL Compiler

- AI 输出 Draft DSL。
- Go 校验业务语义和 plan occurrence。
- 服务端注入 TargetBinding candidates。
- 生成 `dsl.canonical.v3` 与完整 hash 链。

### Phase 5：统一 Preflight 与 Runner

- 删除 preflight/runtime 独立正则。
- 只保留一个 Python LocatorSpec compiler。
- Runner 禁止自由文本 locator fallback。
- 失败统一返回结构化 re-grounding signal。

### Phase 6：删除旧路径

- 删除自定义 `role="..." inside ...` parser。
- 删除 Go/Python 重复 candidate strategy 列表。
- 删除 research-v2 不再引用的 `target_strategy` 和旧 candidate 分支。
- 通过跨站矩阵后再停止写入 research-v1。

## 完成标准

- 同一 LocatorSpec 在 preflight 和 Runner 中生成相同 Playwright API 调用。
- A11y accessible name 与 DOM text 全程不互相覆盖。
- TaskPlan 不包含 selector，Executable DSL 不包含未绑定 target。
- 所有 click/input 类步骤都绑定可追溯、现场验证过的 candidate。
- 单次模型 observation 不超过 16 KiB，活跃 observation 窗口不超过 48 KiB。
- 大表格、SPA、ARIA composite、Shadow DOM 和原生表单全部通过声明式验收。
- 无任务专用产品、价格、URL、selector 或步骤分支进入通用代码。

## 实施结果

| Phase | 状态 | 结果 |
|---|---|---|
| 0 | completed | 新增 7 类跨站 fixture，并保留 research-v1 兼容路径 |
| 1 | completed | 新增共享 manifest/JSON Schema、Go browsercontract 和 Python Pydantic contract |
| 2 | completed | 双写 Observation v2；A11y/DOM/runtime 分源；open shadow/frame context；gzip 内容寻址 artifact 和大快照分片；模型窗口降为 16/32/48 KiB |
| 3 | completed | PlanStep 持久化 TargetBinding；locator-bearing step 无 binding 时不能完成 grounding；Plan revision 会重绑定并重算 hash |
| 4 | completed | 新增 research-v2 Draft DSL、Go 编译、canonical v3、plan/observation binding 和共享 golden |
| 5 | completed | research-v2 preflight 与 Runner 使用同一结构化 LocatorSpec compiler；执行前重新检查唯一、可见、可用 |
| 6 | completed with live-agent gate | research-v2 不再解析自定义 locator 字符串；legacy parser 已统一；跨站只读观察完成，Selenium research-v2 正式 Runner smoke 3/3 通过 |

尚未完成的是需要真实模型成本的 live Agent E2E。它不是本实现的静态门禁，
但在该验收通过前不能把 research-v2 标记为生产默认稳定版本。

2026-09-10 补充：首次 v4-pro live E2E 暴露的计划改版预算、重复探测、
容器候选、控件值 wait_for 和取消终态问题已经修复。真实浏览器 smoke 已验证
Products 导航和 spinbutton value_equals；尚未付费重跑完整 Agent E2E。
