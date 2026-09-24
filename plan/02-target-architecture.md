# 02 目标架构

## 1. 目标形态一句话

**自然语言输入，AI 自动规划路径，但核心不绑定任何业务域。**

```
目标(NL)
  → 页面世界模型(Observation)
  → 候选动作图(ActionCandidate Graph)
  → Blocker 检测与目标可达性检查
  → 通用动作代数(Action/Condition)
  → CaseArtifact(不可变、已接地)
  → 干跑证明
  → 审批执行
  → 报告与失败回灌
```

v2 的产品目标不是“跑通电商”，而是“用电商暴露真实 Web 的通用问题”。电商、SaaS 后台、
内容站、表单系统都必须落在同一套浏览器能力上：页面理解、目标接地、动作执行、断言、失败修复。

## 2. 核心原则

| 原则 | 含义 |
|---|---|
| 领域不进核心 | 核心不出现商品、订单、CRM、文章等业务词；这些只存在于用户目标和页面文本里 |
| 模型不写 DSL | 模型只能调用工具，case 由 Go 侧构建、校验、归一化 |
| 目标必须接地 | 每个可交互目标必须来自最近一次真实观测，且执行前已验证唯一命中 |
| 页面模型优先 | 通用性来自更好的页面世界模型，而不是给每个行业写脚本 |
| 失败必须结构化 | 失败信号要能驱动下一轮规划，而不是只给人看一段错误字符串 |
| 每阶段可运行 | 任一阶段结束都必须保留当前单闭环可运行，不做大爆炸重写 |

## 3. 目标架构分层

### 3.1 控制面（Go）

| 模块 | 职责 |
|---|---|
| `agentruntime` | 管理模型对话、工具调用循环、用量熔断、run 状态推进 |
| `planner` | 把工具调用翻译为 case 步骤；负责目标解析、步骤派生、干跑前构建 |
| `contract` | Go 侧契约、条件阶段表、动作派生规则、错误码 |
| `worker` | 浏览器执行器 RPC 客户端 |
| `report` / `feedback` | 从执行结果生成失败信号与下一轮输入候选 |
| `store` | SQLite 持久化：session/run/case/execution/report/usage |
| `api` | Web/API/SSE 出口 |

控制面的长期边界是：**规划与领域目标在 Go，浏览器事实在 worker，二者只通过契约交换数据。**

### 3.2 浏览器执行器（Python）

| 模块 | 职责 |
|---|---|
| `observer` | 采集页面世界模型，包括元素、文本、结构、候选定位器、截图 |
| `locators` | 把契约定位器转换为 Playwright locator，并验证唯一命中 |
| `blockers` | 识别弹层、广告、遮挡、登录墙、验证码、loading 等通用阻塞，并给出安全恢复建议 |
| `actions` | 执行通用浏览器动作；执行前做目标可达性检查，必要时调用安全恢复 |
| `conditions` | 评估条件，区分 pre/post 阶段 |
| `runner` | 在全新 browser context 内顺序执行 case 并产出证据 |
| `sessions` / `main` | 作者态浏览器会话与 HTTP API |

执行器不理解业务目标，只提供浏览器事实与动作结果。

### 3.3 数据契约

契约仍然是唯一跨进程边界。当前 v2 已有 `case`、`observation`、`execution` 三类核心形态，
后续应扩展而不是旁路：

| 契约 | 当前状态 | 目标状态 |
|---|---|---|
| `CaseArtifact` | 5 个动作，扁平 target | 增加结构化 `TargetSpec`、通用动作集、更多断言 |
| `Observation` | 扁平元素表，已验证 locator | 页面世界模型：结构、区域、表单、列表项、弹层、frame、候选属性、blocker 摘要、action candidates |
| `ExecutionResult` | 步骤结果、截图、console、network | 增加 blocker 分类、目标可达性、恢复动作记录、重试/修复上下文 |
| `Feedback` | 按失败 kind 生成候选 | 按结构化失败原因生成可执行的重规划提示 |

## 4. 页面世界模型（Observation v2）

当前观测最大限制是“扁平元素表”。通用 Web E2E 需要让模型和解析器理解页面结构：

| 能力 | 目的 |
|---|---|
| 元素层级 | 知道按钮属于哪个卡片、表单、表格行、弹层 |
| 邻近文本 | 表达“Blue Top 旁边的 View Product” |
| 容器节点 | 给 `within` / `scope` 提供可接地的锚点 |
| 表单模型 | 识别 label、input、submit 控件、按钮类型、回车是否可提交 |
| 列表/表格模型 | 支持“第 N 行”“包含某文本的行”“某列对应按钮” |
| 弹层/遮罩模型 | 识别当前是否被 modal、cookie banner、广告插屏阻塞 |
| frame/tab 模型 | 明确当前可操作上下文，必要时切换 frame 或 tab |
| 候选属性 | `aria-label`、`title`、`placeholder`、`id`、`name`、`data-testid` 等 |
| 目标可达性 | 通过 hit-test 判断目标点击点是否被其它元素、iframe、固定层或遮罩覆盖 |
| 候选动作 | 把“可点击/可提交/可关闭/可选择”的 DOM 事实整理成模型可选的 action candidate |

关键约束：这些字段是**观测事实**，不是模型猜测。所有能落进 case 的定位器仍必须在观测期验证唯一命中。

## 5. 通用定位语义（TargetSpec v2）

当前 `hint -> 单元素` 只能处理“页面上唯一可见文本/可访问名”。目标形态应升级为：

```jsonc
{
  "intent": "打开 Blue Top 的详情页",
  "object": { "text": "View Product", "role": "link" },
  "scope": { "kind": "container", "contains_text": "Blue Top" },
  "relation": "within",
  "locator": { "...": "观测期已验证的最终 locator" },
  "grounding": { "...": "observation/page_state/candidate lineage" }
}
```

必要能力：

| 能力 | 解决的问题 |
|---|---|
| `within` / `scope` | 同名按钮、卡片内按钮、表格行操作 |
| alias matching | 纯图标按钮、只有 `id/title/aria-label` 的控件 |
| role + relation | “某个 label 对应的输入框”“某行的删除按钮” |
| stable candidate id | 让模型选择候选时不会依赖临时排序 |
| explicit ambiguity | 多个候选同分时继续拒绝，不能退回 v1 的“默认点第一个” |

### 5.1 Candidate 机制：把 selector 编译成可验证候选

P5 真实站点 canary 暴露出一个关键事实：让模型在自然语言里重新描述“搜索框旁边的纯图标按钮”，
比让它直接写 `#submit_search` 更难。但直接写 selector 又会破坏核心约束：不可审计、不可追溯、
不保证来自最近一次真实观测。因此新的中间层目标不是“让模型更会猜 selector”，而是：

**系统从页面事实编译出候选，模型只选择候选；最终 locator 仍由系统验证并写入 case。**

Observation 应增加稳定的候选视图：

```jsonc
{
  "action_candidates": [
    {
      "candidate_id": "act_18",
      "action": "click",
      "target_ref": "e18",
      "role": "button",
      "name": "\uf002",
      "text": "",
      "aliases": ["search submit", "submit_search", "form submit"],
      "attributes": { "id": "submit_search", "type": "button" },
      "relations": [
        { "type": "form_submit_candidate", "ref": "s12" },
        { "type": "near_control", "ref": "e3", "label": "Search Product" }
      ],
      "locator": { "...": "观测期已验证的最终 locator" },
      "confidence": "high"
    }
  ]
}
```

候选类型按通用 Web 行为组织，而不是按业务域组织：

| candidate kind | 来源事实 | 解决的问题 |
|---|---|---|
| `element_candidate` | 可交互元素 + verified locator | 普通 click/input/select/check |
| `form_submit_candidate` | form 内按钮、input 关系、`type`、监听器迹象、邻近关系 | `type=button` + JS 搜索/提交 |
| `dialog_dismiss_candidate` | dialog/blocker 内 close/cancel/accept/reject 控件 | 弹层、cookie banner、插屏 |
| `navigation_candidate` | link/button 导航迹象、href、URL 变化探测 | 图标导航、菜单入口 |
| `assertion_candidate` | 页面文本、标题、URL、元素状态 | 让断言来自当前观测事实 |

模型可使用两种入口：

1. **语义解析入口**：继续传 `TargetSpec`，由 resolver 在完整 Observation 上解析。
2. **候选选择入口**：当 PageView 已给出明确 candidate 时，模型传 `candidate_id` 或 `target_ref`。

候选选择不是 selector 绕路。控制面必须校验：

- candidate 来自最近一次 Observation；
- candidate 的 locator 已在观测期唯一验证；
- candidate 的 action 与工具动作兼容；
- candidate 的 target 仍满足可见、可用、可达性检查；
- candidate 过期、歧义或跨页面复用时拒绝，并要求重新 `open_page`。

这把“模型裸写 Playwright 的灵活性”变成了“模型选择系统已验证候选”的受控能力。

## 6. 通用动作代数（Action/Condition v2）

当前动作集只有 `goto`、`click`、`input`、`assert_text`、`assert_url`。这能证明闭环，
但不足以覆盖普通 Web 产品。目标动作集按优先级扩展：

| 层级 | 动作/断言 | 场景 |
|---|---|---|
| A | `select`、`check`、`uncheck`、`scroll_into_view` | 表单、筛选、长列表 |
| B | `hover`、`dismiss_dialog`、`switch_frame`、`switch_tab` | 菜单、弹层、iframe、弹窗 |
| C | `upload_file`、`download_expect` | 文件流 |
| D | `assert_element`、`assert_attribute`、`assert_count`、`assert_network` | 更精确的验收 oracle |

动作扩展原则：

- 每个动作必须有明确的 postcondition 语义。
- 每个动作都必须能在作者态执行，也能在干跑/正式执行中复现。
- 新动作必须同步更新 Go 契约、Python 执行器、工具 schema、契约一致性 fixture。
- 不为某个行业增加动作名，例如不加 `add_to_cart`、`create_ticket`，这些都由通用动作组合表达。

## 7. 规划循环

规划器目标从“线性生成步骤”升级为“探索、压缩、执行、修复”：

1. `open_page` 获取完整观测事实。
2. 控制面生成与目标相关的 `PageView`，而不是把所有元素都塞给模型。
3. Observation 生成 `action_candidates`：元素候选、表单提交候选、关闭候选、导航候选、断言候选。
4. 模型调用工具；工具在完整 Observation 上解析 `TargetSpec`，或校验模型选择的 `candidate_id`。
5. 执行动作前，执行器对目标做可达性检查：locator 唯一命中、目标可见/可用、点击点未被遮挡。
6. 若发现 blocker，先进入安全恢复；恢复成功后重试原动作并记录恢复过程。
7. 动作成功后更新观测；动作失败返回候选、blocker、hit-test 结果与结构化原因。
8. `finish_case` 干跑失败时返回失败步骤、未满足条件、blocker、当前页面摘要和已尝试 candidate。
9. 模型用 `drop_last_step`、重新 `open_page`、选择 candidate、缩小 scope、换 alias 或请求用户输入修复尾部步骤。

成本控制的核心不是换模型，而是控制 PageView：目标相关性裁剪、结构摘要、增量观测、候选按需展开。

## 8. 失败与恢复模型

失败信号应从现在的通用 kind 继续细化，但保持可跨行业复用：

| 类别 | 示例 |
|---|---|
| 定位失败 | `target_not_found`、`target_ambiguous`、`scope_not_found` |
| 条件失败 | `condition_unmet`、`assertion_timeout` |
| 页面阻塞 | `blocked_by_dialog`、`blocked_by_overlay`、`blocked_by_interstitial`、`blocked_by_cookie_banner`、`blocked_by_auth`、`blocked_by_captcha`、`blocked_by_loading` |
| 环境失败 | `worker_error`、`network_unreachable`、`navigation_timeout` |
| 预算失败 | `model_budget_exceeded`、`observation_too_large` |

恢复策略分两层：

- 执行器可安全处理的通用恢复：关闭 cookie banner、等待 loading 消失、滚动到目标、切回主 frame。
- 需要重新规划的恢复：登录、验证码、目标不存在、业务流程变更、目标歧义无法消除。

## 9. Blocker + Grounding Recovery

P5 的目标是把真实 Web 的干扰建模成通用浏览器问题，而不是给某个行业写特判脚本。它解决两类
经常混在一起的失败：

1. **目标已接地但不可达**：locator 唯一、元素存在，但点击点被弹层、广告 iframe、sticky header、
   loading mask 或全屏 interstitial 覆盖。
2. **目标语义不足但可通过上下文接地**：按钮无可读文本、只有图标字体或 `type=button`，
   需要借助表单 scope、邻近 label、placeholder、`id/name/title/aria-label/data-testid` 等事实生成 alias。

### 9.1 Blocker 观测

`observer` 应在完整 Observation 中增加 `blockers` 摘要：

| 字段 | 含义 |
|---|---|
| `kind` | `dialog` / `overlay` / `interstitial` / `cookie_banner` / `auth_wall` / `captcha` / `loading` |
| `ref` | blocker 对应结构节点或元素引用 |
| `confidence` | `high` / `medium` / `low`，决定执行器是否可自动恢复 |
| `covers_target_ref` | 若 hit-test 发现遮挡目标，记录被遮挡目标 |
| `dismiss_candidates` | 可安全尝试的关闭控件候选，只能来自已验证 locator |
| `reason` | 命中规则摘要，供报告与模型修复使用 |

识别信号必须来自页面事实：role/dialog、`aria-modal`、fixed 全屏层、iframe 覆盖、可见 loading 文本、
cookie/consent 文案、auth/login 文案、captcha/recaptcha 标记。不要用业务域词，例如商品、订单、CRM。

### 9.2 目标可达性检查

执行器在 `click`、`hover`、`check`、`uncheck`、`select`、`upload_file` 前必须做 grounding recovery：

1. 用 case 中已记录的 locator 解析目标，仍要求唯一命中。
2. 滚动目标到稳定可点击区域，避免 sticky header 遮挡。
3. 取目标中心点或可点击候选点，调用 `document.elementFromPoint` 做 hit-test。
4. 若命中的不是目标或其后代，判断遮挡元素是否属于已识别 blocker。
5. 可恢复则执行安全恢复并重试一次；不可恢复则返回结构化 blocker 失败。

这一步不能退化成坐标点击。坐标只用于判断“谁挡住了目标”，真正动作仍必须作用在已验证 locator 上。

### 9.3 安全恢复策略

自动恢复只允许处理高置信、低风险、可逆的 UI：

| blocker | 可自动恢复 | 策略 |
|---|---|---|
| loading | 是 | 等待消失，再重观测 |
| cookie banner | 是 | 点击已验证的 accept/close/reject 控件，记录选择 |
| modal/dialog | 有条件 | 仅点击明确 close/cancel/×/No thanks，或先尝试 Escape |
| sticky overlay/header | 是 | 滚动目标到无遮挡区域，再 hit-test |
| interstitial/ad iframe | 有条件 | 只能关闭明确关闭控件；不能点击广告内容 |
| auth wall | 否 | 返回 `blocked_by_auth`，进入 `awaiting_input` 或让模型请求用户 |
| captcha/recaptcha | 否 | 返回 `blocked_by_captcha`，不得绕过 |

每次恢复都必须写入 `ExecutionResult.steps[].recovery`（或等价结构）：blocker kind、尝试动作、
是否成功、恢复前后截图/URL、是否重试原动作。恢复失败不能吞掉原始 blocker。

### 9.4 表单与图标控件恢复

图标按钮和 JS 驱动搜索不是电商专用问题，通用解法是表单/邻近关系：

- `form` 结构记录 submit 候选、`enter_submittable`、控件 label、placeholder、name/id。
- `Observation.action_candidates` 直接输出表单级动作，例如 `form_submit_candidate`，并把候选与输入框、
  form、按钮属性、邻近关系绑定。
- `TargetSpec.scope` 可指向包含某输入框/label 的 form；`object.aliases` 可使用 `submit`、`search submit`、
  `id/name/title/aria-label/data-testid` 等候选属性。
- 对 `type=submit` 或 Enter 可提交的表单，优先用 `input(submit=true)`。
- 对 `type=button` + JS 的图标按钮，优先让模型选择系统给出的 `form_submit_candidate`；
  其次才走 scope + alias 接地到按钮，再用已验证 locator 点击。
- 如果 alias 或 candidate 仍不唯一，必须返回 `target_ambiguous`，不能默认点第一个。

这类能力必须由中间层提供，而不是要求模型“更聪明”。模型能理解“提交搜索”这个意图，
但它不应该负责推导 `#submit_search`、判断按钮是否属于某个 form、或猜测某个私有区图标字形代表搜索。
这些都应由 `observer/resolver` 通过 DOM、a11y、几何、表单关系和可选探测来完成。

### 9.5 Candidate-first 工具协议

P5 之后工具协议应允许模型在两条路径之间选择：

| 路径 | 适用场景 | 约束 |
|---|---|---|
| `TargetSpec` 语义解析 | 目标有清晰文本、role、scope 或 alias | resolver 必须唯一接地 |
| `candidate_id` 选择 | 目标低语义但已被 Observation 编译成候选 | candidate 必须来自最近一次观测且 locator 已验证 |

候选选择工具示例：

```jsonc
{
  "tool": "click",
  "candidate_id": "act_18",
  "intent": "提交搜索表单",
  "expect_url": "search=Blue"
}
```

控制面收到 `candidate_id` 后不信任模型描述，只做四件事：

1. 在最近 Observation 中查找 candidate。
2. 校验 candidate 支持当前 action。
3. 取 candidate 自带的 verified locator 与 grounding，派生 `Target`。
4. 作者态执行一次；成功后才把 step 追加进 case。

这样模型不需要也不允许写 `#submit_search`，但仍能使用页面里已经观察到的 `id=submit_search`
这一事实。最终进入 case 的仍是系统验证过的 locator，而不是模型生成的 selector。

### 9.6 与规划器的交互

planner 看到 blocker 或 grounding recovery 失败时，不能重复同一个失败 hint 超过一次。下一轮必须改变策略：

- `blocked_by_*`：先 `open_page` 重观测；若仍存在，调用 `dismiss_dialog` 或请求用户输入。
- `target_not_found`：换 alias、展开候选、改用 `candidate_id`，或请求用户补充目标描述。
- `target_ambiguous`：缩小 scope，例如 card/table row/form/dialog/frame；若 PageView 已有候选，改选 candidate。
- `scope_not_found`：改用后代文本、邻近 label、form 关系、candidate 关系，或重新导航到更相关页面。
- `condition_unmet`：`drop_last_step` 后重建尾部，避免保留已失败的断言。

修复上下文最少包含：失败步骤、当前 URL、失败 locator/target spec、blocker、hit-test 结果、
未满足条件、候选替代目标、已经尝试过的 recovery 与 candidate。

规划器还必须维护一次 run 内的失败签名，避免无限烧 token：

| 失败签名 | 下一次必须改变 |
|---|---|
| 同一 `hint + action + page_state` 再次失败 | 改用 candidate、scope 或请求用户 |
| 同一 `candidate_id` 执行失败 | 不再重试该 candidate，除非页面状态已变化 |
| `input(submit=true)` 后 URL/结果不变 | 改用 `form_submit_candidate` 或已验证提交按钮 |
| `scope_not_found` 且 scope.ref 指向元素 ref | 改用元素所属 form/container 的 structure ref，或让 resolver 支持 element-ref relation |
| 干跑失败两次来自同一根因 | 停止自动修复，返回结构化失败而不是继续消耗预算 |

## 10. 跨场景验收矩阵

通用能力必须用多场景证明，不再只看电商：

| 场景 | 覆盖能力 |
|---|---|
| 电商 | 搜索、重复卡片、详情、加购、购物车断言 |
| SaaS CRUD | 表单填写、下拉、创建、编辑、删除确认 |
| 内容站 | 搜索、分页、详情页文本断言 |
| 后台表格 | 表格行 scope、筛选、行内操作、弹窗确认 |
| 文件流 | 上传、下载触发、状态提示 |
| iframe/弹窗 | frame 切换、新 tab、遮罩关闭 |

每类至少要有一个离线 fixture；真实站点 canary 可以少，但必须覆盖最容易失真的能力边界。

## 11. 非目标

- 不做行业专用 DSL。
- 不让模型手写 CSS/XPath 作为常规路径。
- 不引入微服务、消息队列、多租户、鉴权体系。
- 不在核心里维护“电商知识库”“SaaS 知识库”。
- 不以视觉定位替代 DOM/a11y 接地；视觉可以作为未来 fallback，但不是当前主路径。
- 不绕过验证码、登录墙、支付确认或其它需要真实用户授权的流程。
