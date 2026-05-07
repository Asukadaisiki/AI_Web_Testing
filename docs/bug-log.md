# Bug 日志

用于沉淀在开发、联调、测试和执行过程中发现的问题，跟踪影响、状态和修复结论。

## 记录规则

- 发现一个明确问题时新增一条记录。
- 状态建议使用：`open`、`in_progress`、`fixed`、`wont_fix`。
- 每条记录尽量包含复现条件、影响范围、定位结论和验证方式。
- 如果问题来自某次任务执行，请回链到 `docs/execution-log.md` 中的对应记录。
- 最新的记录优先放到最上面，方便阅读

## 模板

```md
## BUG-XXX | 标题

- 日期：YYYY-MM-DD
- 状态：fixed
- 来源：需求 / 自测 / 联调 / 线上反馈
- 描述：问题现象
- 复现步骤：
  1. 步骤一
  2. 步骤二
- 影响：功能、页面、模块或用户范围
- 根因：如果尚未定位，写“待定位”
- 处理：修复动作或计划
- 验证：已执行的验证；如果没有写“未验证”
- 关联记录：执行日志日期或链接
```



## BUG-082 | capture_page_session 定位器使用简化版 resolver 导致登录态不稳定

- 日期：2026-05-07
- 状态：fixed ()
- 来源：E2E 回归测试
- 描述：capture_browser_session 使用简化版 _resolve_step_locator（只尝试少量候选 + count()>0 即返回），导致 Email placeholder 匹配到 3 个元素 → strict mode 失败 → 登录 session 无法保存 → explore_flow 数据减半（73K vs 157K）→ 草案质量波动。
- 根因：_resolve_step_locator 没有使用完整的定位器链路（semantic → a11y → VLM fallback）
- 处理：改为直接调用 resolve_with_fallback（与 runner 相同链路）；添加页载等待 + 元素 tag 验证 + 2 次重试
- 验证：S161 capture_page_session 成功，page_elements 从 73K → 1.28MB

## BUG-081 | DSL 草案间质量剧烈波动 — 相同 prompt 产出 42 步和缺登录的 30 步

- 日期：2026-05-07
- 状态：fixed (, , , )
- 来源：E2E 回归测试
- 描述：相同 draft_prompt（2069 chars），S155 产出 42 步完整草案，S162 产出缺少登录导航的 30 步草案（goto / 后直接 input Email Address）。根本原因是 DSL 生成模型的随机性。
- 处理：1) 系统提示词加入【流程-页面导航映射】规则；2) 草案生成后一致性检查——prompt 提到登录页必须有导航步骤，goto / 后不能直接 input
- 验证：Draft 89 包含完整 click "Signup / Login" 导航

## BUG-080 | assert_text 的 target 字段中的运行时变量未被替换

- 日期：2026-05-07
- 状态：fixed ()
- 来源：E2E 回归测试
- 描述：assert_text target="${cart_a_total}" 中的 ${cart_a_total} 未被 _substitute_variables 替换 → 定位器按字面量去找文本 → 永远找不到 → 走 VLM 兜底 → 定位到错误元素。
- 根因：_substitute_variables 只对 step.value 调用，未对 step.target 调用
- 处理：在所有 runner 和 helper 函数中，将 step.target 在使用前统一替换为 _substitute_variables(step.target, _vars()) or step.target
- 验证：543/544 单元测试通过

## BUG-079 | 购物车测试数据污染 — 已确认定位器无问题，数据污染导致 [verified: Exec 107 100%] — 前序测试遗留商品导致数量断言失败

- 日期：2026-05-07
- 状态：verified (Exec 107 42/42=100%)
- 来源：E2E 回归测试
- 描述：Exec 106 Step 27 `assert_text '1' value='${cart_a_quantity}'` 失败。capture 抓到的数量是 31（前序测试累积），但断言期望 1。定位器本身工作正常——`button_role` 正确找到了 `<button>1</button>`，`css` 正确抓到了 `<button>31</button>`。根因是账号购物车未清空。
- 复现步骤：
  1. 多次执行 brand_filter_cart 测试
  2. 购物车中 Blue Top 数量累积到 31
  3. 新测试假设初始数量=1，实际=31
- 影响：购物车数量相关的 assert_text 不可靠
- 根因：测试间缺少购物车清理步骤
- 处理：1) 测试开始前清空购物车 2) AI 不应硬编码数量值，应 capture 后做一致性比较
- 验证：用户手动清空购物车后重新执行，Exec 107 42/42=100% 全部通过
- 关联记录：execution-log.md 2026-05-07 第 26-27 步骤分析，Exec 106

## BUG-078 | DSL 归一化器删除了合法的 click/wait_for/capture_text 步骤

- 日期：2026-05-07
- 状态：fixed (`ecbbb3a`)
- 来源：E2E 回归测试
- 描述：AI 生成的合法步骤（如 `click "Login"`, `click "Products"`, `click "(6) POLO"`, `click "Add to cart"`）被归一化器删除。根因是 AI 给 click/wait_for/capture_text 步骤添加了 `"value": null` 字段，但这些步骤模型没有 `value` 属性，Pydantic `extra_forbidden` 拒绝。
- 复现步骤：
  1. AI 生成 DSL，步骤中包含 `"action": "click", "target": "Login", "value": null`
  2. 归一化器 `_repair_step_shape` 处理字段别名
  3. `_STEP_ADAPTER.validate_python` 因 `value` 为 extra field 而失败
  4. 步骤被静默删除，warnings 显示"校验失败"
- 影响：导致关键导航步骤缺失，测试执行卡死在错误页面
- 根因：`_repair_step_shape` 未剥离 click/wait_for/capture_text 步骤的 spurious `value` 字段
- 处理：在 `_repair_step_shape` 末尾，对 click/wait_for/capture_text 类型步骤移除 `value` 键
- 验证：58 个 DSL 单元测试通过，Exec 106 证实 0 步骤被删
- 关联记录：Draft 80 8 步骤被删，Draft 81 0 步骤被删

## BUG-077 | DSL 归一化器删除了合法的 goto/assert_url_contains 步骤

- 日期：2026-05-07
- 状态：fixed (`8d05871`)
- 来源：E2E 回归测试
- 描述：AI 给 goto 和 assert_url_contains 步骤添加了 `candidates: []` 和 `postconditions: []` 字段，但 GotoStep 和 AssertUrlContainsStep 模型没有这些字段，Pydantic `extra_forbidden` 拒绝。实际影响：仅 goto 和 assert_url_contains 步骤受影响，其他步骤（click/input/wait_for/capture_text/assert_text）有这些字段。
- 复现步骤：
  1. AI 生成 `{"action": "goto", "value": "/", "candidates": [], "postconditions": []}`
  2. Pydantic 验证报 `extra_forbidden` 错误
  3. goto 步骤被丢弃 → 归一化器自动补充一个默认 goto "/"
- 影响：原始 goto 步骤被替换，可能导致导航 URL 不正确
- 根因：AI 给所有步骤统一加了 candidates/postconditions 空数组
- 处理：在 `_repair_step_shape` 中对 goto/assert_url_contains 剥离 candidates/postconditions
- 验证：58 个 DSL 单元测试通过
- 关联记录：Draft 76-80 均有"步骤 #1 校验失败（action=goto）"警告

## BUG-076 | DSL target 文本中的中文字符被 PostgreSQL JSON 序列化损坏

- 日期：2026-05-07
- 状态：fixed (`aaa3f18`)
- 来源：E2E 回归测试
- 描述：DSL 步骤 target 字段中的"附近的"被序列化为 `\udc84`（lone low surrogate），导致 `text_parent_chain` 定位器的 split regex 无法匹配。所有含中文的 target 均受影响（6 个步骤）。
- 复现步骤：
  1. AI 生成 target="Blue Top 附近的 Rs. 500"
  2. DSL JSON 经 PostgreSQL JSONB 列存储
  3. 读取时"的"字符被损坏为 `\udc84` surrogate
  4. `_PARENT_SPLIT_RE` regex 无法匹配损坏文本
  5. `_resolve_text_parent_chain` 返回 None → 候选被跳过
- 影响：text_parent_chain 定位器对所有含"附近的"的 target 完全失效，回退到 VLM/coordinate click
- 根因：PostgreSQL JSON 序列化过程中 Unicode BMP 字符被错误编码为 surrogate pair
- 处理：在 DSL 归一化入口处检测并修复 surrogate 字符（`encode('utf-8', 'surrogatepass').decode('utf-8', 'replace')`）
- 验证：Draft 81 证实 surrogate_targets=0
- 关联记录：Exec 103-104 的 Step 10/16/22 均为 surrogate 损坏

## BUG-075 | 元素视觉分组的 group label 太粗糙

- 日期：2026-05-07
- 状态：open（待优化）
- 来源：E2E 回归测试
- 描述：`_group_elements_by_visual_proximity` 按 rect 坐标分组，但 group label 逻辑简单——优先取 h1-h4 文本，否则取第一个非泛型文本。导致部分分组只有 1-2 个元素被隔离为独立组，label 无意义（如 "button"）。
- 影响：AI 看到的页面分组不够语义化，部分组没有有用的 label
- 根因：坐标聚类算法对孤立元素过敏感
- 处理：待后续优化分组算法（增大 tolerance、合并小 group）
- 验证：用户手动清空购物车后重新执行，Exec 107 42/42=100% 全部通过

## BUG-074 | text_parent_chain 定位器未在 runner 候选列表中被优先尝试

- 日期：2026-05-07
- 状态：fixed (`6922bc8`)
- 来源：E2E 回归测试
- 描述：执行层原始流程 `_resolve_with_confidence_gate` 在 `locator_confidence="low"` 时先调 VLM preverify，跳过了包含 `text_parent_chain` 的语义候选链。导致 VLM 频繁抢占、429 限频、找到错误元素。
- 复现步骤：
  1. preflight 将几乎所有步骤标记为 low confidence
  2. `_resolve_with_confidence_gate` 检测到 low → 直接调 `preverify_with_vlm`
  3. VLM 找到错误坐标 → 点击错误元素 → 后续步骤失败
- 影响：VLM 抢占导致"Continuing Shopping"弹窗从未出现，多个执行在 Step 12/14 失败
- 根因：VLM preverify 不应跳过语义定位链
- 处理：重构为统一流程——语义优先（含 text_parent_chain）、VLM 仅作最后兜底。同时添加 2.5 分钟步骤超时防止无限挂起。
- 验证：Exec 102+ 证实 `text_parent_chain` 在候选列表中排在第一位
- 关联记录：Exec 94-101 反复出现"Continue Shopping"定位失败

## BUG-073 | text_parent_chain 的正则表达式无法匹配含空格的父文本

- 日期：2026-05-07
- 状态：fixed (`aabea6a`)
- 来源：E2E 回归测试
- 描述：`_PARENT_TEXT_RE` 使用 `[^>\\s>{2,60}?`（惰性匹配 + 排除空格），导致"Blue Top 附近的 Add to cart"无法匹配——"Blue Top"含空格，`[^>\\s]` 排除了空格字符。
- 复现步骤：
  1. target = "Blue Top 附近的 Add to cart"
  2. regex `[A-Za-z][^>\\s]{1,60}?` 匹配 "Bl" 后就停止（惰性匹配 + 空格排除）
  3. 后续"附近的"分隔符匹配失败
- 影响：`_resolve_text_parent_chain` 返回 None，text_parent_chain 定位器从未生效
- 根因：1) 惰性量词 `{1,60}?` 使匹配过短 2) `[^>\\s]` 错误排除了空格
- 处理：改用 split 方式——`_PARENT_SPLIT_RE` 直接在 `>>`/`的`/`附近的` 处分隔，不再依赖复杂正则
- 验证：33 个语义单元测试通过，Exec 102 Step 11 证实 `text_parent_chain` 成功匹配
- 关联记录：Exec 96-100 Step 11 均为 VLM 而非 text_parent_chain

## BUG-072 | text_parent_chain 使用硬编码 XPath ancestor 无法适配不同页面结构

- 日期：2026-05-07
- 状态：fixed (`892889e`)
- 来源：E2E 回归测试
- 描述：`_find_in_ancestor` 最初使用 `xpath=ancestor::*[contains(@class,'product')]` 硬编码 class 名。购物车页面使用 `<table>` 结构，没有 'product' class → 返回 0 元素 → 候选被跳过。
- 复现步骤：
  1. 执行购物车页的 assert_text "Blue Top 附近的 Rs. 500"
  2. `_find_in_ancestor` 用 XPath ancestor 查找含 'product' class 的祖先
  3. 购物车 `<tr>` 不含此 class → 无匹配
- 影响：购物车页所有 text_parent_chain 定位器失效
- 根因：XPath 硬编码了特定网站的 class 名
- 处理：改为自适应深度遍历——从 parent_text 元素出发，逐层 `..` 向上（depth 2-8），每层尝试 `get_by_text(child_text)`，选最浅匹配。无网站/页面依赖。
- 验证：33 个语义单元测试通过，手动验证购物车页 depth=3(<tr>) 可找到 "Rs. 500"
- 关联记录：Exec 103-104 Step 22 失败

## BUG-071 | text_parent_chain 的 child_text 使用 exact=True 导致 substring match 失败

- 日期：2026-05-07
- 状态：fixed (`dba307c`)
- 来源：E2E 回归测试
- 描述：`_find_in_ancestor` 使用 `get_by_text(child_text, exact=True)` 进行精确匹配，但实际 DOM 中价格文本可能有前后空格/格式差异，导致"Rs. 500"精确匹配失败。
- 影响：品牌筛选页的价格 capture 和购物车页的价格 assertion 均失败
- 根因：`exact=True` 要求文本完全相等，DOM 文本格式化变化（空格、嵌套元素）导致不匹配
- 处理：改回 `exact=False`（子串匹配），并在 ancestor 遍历中添加 try/catch 防止异常吞没
- 验证：33 个语义单元测试通过
- 关联记录：Exec 103-104 Step 10 价格 capture 失败

## BUG-070 | DSL generator thinking mode 下 reasoning_content 空响应

- 日期：2026-05-06
- 状态：fixed (`9f67995`)
- 来源：E2E 回归测试
- 描述：DSL generator 使用 DeepSeek thinking mode 时，模型返回 `reasoning_content` 但 `content` 为空。`_extract_message_content` 只读 `content` 字段，忽略了 `reasoning_content` → 返回空字符串 → JSON 解析失败 → 草案状态 failed。
- 影响：所有 DSL 草案生成失败（"AI 返回了无法解析的 DSL JSON"）
- 根因：`_extract_message_content` 缺少对 `reasoning_content` 的 fallback（与 BUG-063 同模式但在 DSL generator 中）
- 处理：在 content 为空时 fallback 到 `reasoning_content`
- 验证：Draft 62 生成成功（33 步）
- 关联记录：session 132 draft 61 失败

## BUG-069 | 系统提示词引导 AI 在信息充足时仍使用 ask_user 询问确认

- 日期：2026-05-06
- 状态：fixed (`13016a6`)
- 来源：E2E 回归测试
- 描述：系统提示词第 91 行：当收集到 4+ 项信息时，通过 ask_user 询问"信息是否足够"。这导致 AI 第一轮动作为 ask_user 而非 explore_page/explore_flow。即使用户提供了完整的 core_user_flow，AI 仍然先问"要生成方案吗"。
- 影响：AI 不探索页面就直接进入收集状态，无页面数据就生成 DSL
- 根因：提示词明确鼓励 AI 在信息充足时使用 ask_user 确认
- 处理：修改规则为"信息充足时直接 generate_plan，不用 ask_user"
- 验证：Session 139 AI 第一轮动作为 call_tool（get_project_info），不再问废话
- 关联记录：session 138 AI 第一轮 ask_user

## BUG-068 | 页面探索压缩子代理丢弃登录表单元素

- 日期：2026-05-06
- 状态：fixed (`081c49e`)
- 来源：E2E 回归测试
- 描述：`run_compression_subagent` 的 `_filter_elements_for_compression` 硬编码取前 100 个元素。登录表单字段（data-qa="login-email" 等）可能在第 100+ 位置被截断。子代理 prompt 对表单强调不足，压缩结果 `forms: []` 为空。
- 影响：AI 探索登录页后收到的压缩数据只有 3 个 key_elements（全是 footer 元素），没有登录表单信息
- 根因：1) `_filter_elements_for_compression` 截取前 100 个元素 2) 子代理 prompt 未强调必须保留表单字段
- 处理：改为优先保留交互元素（input/button/select/textarea/a），非交互元素限制 80 个；重写 prompt 为强制 JSON 结构 + 9 条绝对规则
- 验证：65 个单元测试通过
- 关联记录：session 140 AI 报告"未能获取登录表单元素"

## BUG-067 | explore_flow 相对 URL 未解析导致页面探索失败

- 日期：2026-05-07
- 状态：fixed (`f53807d`)
- 来源：E2E 回归测试
- 描述：AI 调用 explore_flow 时传入相对 URL（`/products`, `/brand_products/Polo`, `/view_cart`），但 `collect_multi_page_elements` 未解析相对 URL，Playwright 的 `page.goto("/products")` 失败 → 返回空元素。
- 影响：品牌筛选页和购物车页的探索数据为空（81 字符 vs 157KB）
- 根因：`collect_multi_page_elements` 缺少 base_url 拼接逻辑
- 处理：用 `urljoin` 将相对 URL 解析为绝对 URL
- 验证：S152 page_elements 从 81 字符增长到 157KB
- 关联记录：session 151 页面探索数据仅 81 字符

## BUG-066 | AI 的 core_user_flow 被序列化为 Python list repr

- 日期：2026-05-07
- 状态：fixed (`23e3bc9`)
- 来源：E2E 回归测试
- 描述：AI 在 `collected_info` 中以 list 形式返回 `core_user_flow`（如 `["打开首页...", "点击 Products..."]`）。`_merge_requirements` 对非 main_assertions 字段直接调用 `str(incoming)`，将 list 转成了 Python repr 字符串 `"['打开首页...', '点击 Products...']"`。DSL prompt 收到这个畸形的流程描述，生成的 DSL 质量极差（17 步 vs 33 步）。
- 影响：draft 质量随机波动（取决于 AI 是否用 list 格式返回 core_user_flow）
- 根因：`_merge_requirements` 只对 main_assertions 做了 list→string 特殊处理
- 处理：对 core_user_flow 和所有 list 字段统一处理——join 为编号列表
- 验证：S146 draft 72 43 步（修复后） vs S145 draft 70 17 步（修复前）
- 关联记录：session 144 good vs 145 bad core_user_flow 格式对比

## BUG-065 | DSL prompt 未要求 capture_text 后必须跟 assert_text

- 日期：2026-05-06
- 状态：fixed (`a631041`, `c5e4411`)
- 来源：E2E 回归测试
- 描述：AI 生成的 DSL 有 5 个 capture_text 但 0 个 assert_text。capture 只是提取数据不做验证，测试实际上没有检查任何预期结果。用户指出"测试用例肯定要断言，不断言怎么知道对不对？"
- 影响：测试表面上全部通过（23/23）但实际上核心断言完全缺失
- 根因：DSL prompt 只说明了 capture_text 的用法，未强制要求 capture 后必须 assert
- 处理：在系统 prompt 和用户规则中增加"capture 必须 assert"规则；新增"modify→input→assert"规则确保修改操作有前置 input 步骤
- 验证：Draft 69 有 10 个 assert_text（vs Draft 66 的 0 个）
- 关联记录：Exec 95 23/23 passed 但 0 assert_text

## BUG-064 | preflight 将所有 target 标记为 low confidence 导致 VLM 抢占

- 日期：2026-05-06
- 状态：fixed（通过 BUG-074 的流程重构规避）
- 来源：E2E 回归测试
- 描述：preflight 在已探索元素中找不到 target 文本的精确匹配 → 几乎所有步骤被标记为 `locator_confidence="low"` → 执行层 `_resolve_with_confidence_gate` 为 low 目标直接调用 VLM → VLM 抢占语义定位链。
- 影响：语义定位器（包括 text_parent_chain）从未有执行机会
- 根因：preflight 的 confidence 机制与执行层的 VLM 优先逻辑联动缺陷
- 处理：通过 BUG-074 的执行流程重构绕过（语义链优先、VLM 兜底），preflight confidence 不再影响定位器优先级
- 验证：Exec 102+ 证实语义定位链在 VLM 之前执行
- 关联记录：Exec 94-101

- 日期：2026-05-04
- 状态：fixed（2026-05-05 追加修复）
- 来源：线上反馈
- 描述：使用 `deepseek-v4-flash` 模型进行 AI planning 对话时，SSE 流式输出直接空白——没有思考标注（如"正在深度推理分析需求..."的实时更新），没有思考内容输出，前端在模型思考阶段完全看不到任何文本内容
- 复现步骤：
  1. 配置 `AI_PLANNING_MODEL=deepseek-v4-flash`、`AI_PLANNING_BASE_URL=https://api.deepseek.com`
  2. 在 AI Planning 面板中发送测试需求
  3. 观察 SSE 流式输出 — 模型思考阶段前端空白，偶尔出现一次 status tag
  4. 刷新页面 — 会话消失，需要返回 AI 测试规划列表重新选择项目再进入才能展示消息
- 影响：所有使用 DeepSeek（或启用了 thinking mode 的模型）的 AI planning 会话
- 根因：
  - `backend/app/ai/test_planning_agent.py` `_stream_planning_llm()` L769-776：`reasoning_content` 被接收后在内存累积（`reasoning_text.append(reasoning)`），仅按 ~200 字符节流发送 `status` 事件（"正在深度推理分析需求..."），**不产出 `text_chunk` 事件**。模型思考阶段前端收不到任何文本内容
  - `reasoning_text` 在函数结束前也未归入 `raw_response`，完全丢弃
  - 前端 `handleStreamEvent` 无思考内容的独立展示逻辑（即便收到也无法分隔呈现）
  - （追加）`reasoning_text` 未作为 `raw_response` 兜底：若模型仅产出 `reasoning_content` 而无 `content`，`raw_response` 为空 → 触发 `empty_response` 错误 → 前端 OPTIMISTIC 消息 content 为空 → 空白展示
  - （追加）`_call_planning_llm()` 非流式路径只提取 `message.content`，完全忽略 `message.reasoning_content`
  - （追加）`turn_complete` 后 `loadSessionDetail()` 用服务端数据替换 transcript，导致流式阶段累积的 `_thinkingContent` 丢失
  - （追加）历史消息加载后未清除 `_streaming: true` 标志
- 处理：
  - backend：`_stream_planning_llm()` 中每个 `reasoning` chunk 同步产出 `text_chunk` 事件（带 `thinking: true` 标记），保留节流 `status` 事件用于 phase label 更新
  - frontend types：`TextChunkStreamEvent` 新增 `thinking?: boolean` 可选字段
  - frontend handler：`thinking: true` 的 text_chunk 存入 `_thinkingContent` 字段（与 `content` 分开），不污染正式回复内容
  - frontend render：`_thinkingContent` 存在时渲染可折叠 `<details>` "思考过程"区块（最大高度 200px、overflow-y 滚动）
  - （2026-05-05 追加）
    - backend `_stream_planning_llm()`：`content` 为空时使用 `reasoning_text` 作为 `raw_response` 兜底，避免空响应触发 `empty_response` 错误
    - backend `_extract_message_content()`：`content` 为空时回退提取 `reasoning_content`，非流式路径获得同样保护
    - frontend `applySessionDetail()`：加载历史消息时检查并清除 `_streaming: true` 标志，防止刷新后残留的流式状态
    - frontend `handleStreamEvent` `turn_complete`：`loadSessionDetail()` 后保留流式阶段累积的 `_thinkingContent`，不因服务端重载丢失思考内容
- 验证：
  - backend：29 planning agent 单元测试 + 11 AI planning API 测试通过，505/506 全单元测试通过（1 预存失败与修改无关）
  - frontend：TypeScript 编译无错误
- 关联记录：`docs/execution-log.md` 2026-05-04、2026-05-05

## BUG-060 | AI planning 中间层三大架构断层：动作式探索、页面状态图、定位器预校验

- 日期：2026-05-03
- 状态：fixed
- 来源：架构排查 / BUG-059 延伸
- 描述：BUG-059 的 link-aware ReAct 修复解决了"页面选择"问题，但也暴露了中间层更深的三个架构断层：
  1. `explore_flow` 仍是 URL 级探索——只接受 URL 列表做 `page.goto()`，不会在页面间执行点击/输入/等待动作，无法覆盖登录后页面、弹窗后状态、动态导航等场景
  2. 页面知识是扁平的 `page_elements` 文本——所有页面元素拼接成一个字符串，无页面状态标记，LLM 需自行推断哪个元素属于哪个页面
  3. DSL 生成后无 locator preflight——定位器验证全部推迟到执行期，生成阶段不检查 target 是否唯一、可见、可操作
- 影响：所有依赖跨页面、含登录、含动态交互的测试场景；定位器质量无法在规划阶段暴露；用户看到草案时不知道哪些 step 定位器有问题
- 根因：
  - `backend/app/ai/planning_tools.py` 的 `_handle_explore_flow` 只调用 `collect_multi_page_elements(urls)`，不支持 `steps` 格式的动作式探索
  - `backend/app/ai/page_explorer.py` 无 `collect_flow_elements` 函数；`collect_multi_page_elements` 只做 `page.goto()` + DOM 抓取，不做动作交互
  - `backend/app/services/ai_planning.py` DSL 生成后直接返回，无 preflight 步骤
  - `backend/app/schemas/dsl.py` 各 step 无 `page_state` 字段
- 处理（Phase 1-3 全套升级）：
  - **Phase 1: 动作式 explore_flow** — `page_explorer.py` 新增 `collect_flow_elements(steps)` 函数，支持 `{url, description, actions: [{action, target, value}]}` 格式，在页面间执行 click/input/wait_for 动作后采集 DOM；`planning_tools.py` 的 `_handle_explore_flow` 支持 `steps` 参数（保留 `urls` 向后兼容）；工具定义更新
  - **Phase 2: 页面状态标记** — `collect_flow_elements` 为每个不同 URL 分配 `page_state_id`（S0, S1, ...），每个元素记录 `page_state`；新增 `build_flow_formatted_output` 使用 `=== 页面状态 S{n}: {url} ===` 格式化输出；`dsl.py` 的 ClickStep/InputStep/WaitForStep/AssertTextStep/CaptureTextStep 新增 `page_state` 字段；`dsl_generator.py` prompt 新增页面状态归属指令
  - **Phase 3: Locator preflight** — 新建 `backend/app/ai/locator_preflight.py`，`preflight_locators()` 静态校验 DSL targets 与已采集元素的匹配度（显式选择器匹配 + 语义文本匹配），计算 per-step confidence（high/medium/low）；`ai_planning.py` 的 `generate_planning_drafts` 在 DSL 生成后自动调用 `apply_preflight_to_dsl`，将 confidence 回写到各 step，warnings 并入 draft
- 验证：485 单元测试全部通过；`_classify_target` 正确区分 7 种 target 类型；preflight 正确评估唯一/歧义/无匹配三种场景
- 关联记录：`docs/execution-log.md` 2026-05-03（企业级中间层升级）

## BUG-059 | AI planning 中间层仍是 URL 级探索而非 flow 驱动探索，导致登录链路与跨页规划失真

- 日期：2026-05-03
- 状态：fixed
- 来源：架构排查 / 用户问题分析
- 复现步骤：
  1. 在 AI planning 中仅提供 `entry_url_or_page: https://automationexercise.com/`、核心流程与少量测试数据
  2. 触发自动探索与 DSL 草案生成
  3. 观察自动探索只按入口页链接顺序抓取页面，而不是按 `core_user_flow` 优先推进登录或业务关键路径
  4. 当后续页面依赖点击进入、登录态或动态导航时，草案会停留在模糊 target 或错误页面假设上
- 影响：所有需要“从入口页逐步探索到目标页面”的规划任务，包括登录、加入购物车、订单流、跳转式表单、多页面断言等；定位器质量和草案完整性都会受影响
- 根因：
  - `backend/app/ai/test_planning_agent.py` 中 `_auto_explore_entry_url()` 只会把入口页和 `_extract_internal_links()` 抽到的前 4 个链接送入探索，逻辑与 `core_user_flow` 无绑定
  - `backend/app/ai/planning_tools.py` 的 `explore_flow` 入参仍是 `urls`，而不是动作序列或状态转移计划
  - `backend/app/services/ai_planning.py` 把 `page_elements` 作为 DSL 生成输入，但没有在生成前后做浏览器侧 locator preflight
	- 处理：
	  - _extract_internal_links() 升级为 flow 驱动：接收 core_user_flow 参数，提取中/英文流程关键词（登录/login、购物车/cart、搜索/search 等），按 URL 路径与关键词匹配度评分排序，优先探索流程相关页面而非盲取 DOM 顺序前 4 个链接
	  - explore_flow 工具新增 flow_description 可选参数，让 AI 可以描述页面间的导航意图（如"首页→登录→商品搜索→购物车"），帮助探索器理解页面上下文
	  - _auto_explore_entry_url() 将 core_user_flow 传递给链接提取，使自动探索优先覆盖用户描述的关键路径页面
	- 验证：
	  - 代码检查：backend/app/ai/test_planning_agent.py:944、:1017；backend/app/ai/planning_tools.py:653、:993
	  - 单元测试：471 全部通过
	  - 功能验证：无 core_user_flow 时链接按 DOM 顺序返回；含 login 流程时 /login 从位置 3 提升至位置 1
- 关联记录：`docs/execution-log.md` 2026-05-03（AI planning 中间层排查）

## BUG-058 | AI Test Planning 面板切换会话后仍把项目操作发送到初始 session，导致探索工具看似未生效

- 日期：2026-05-03
- 状态：fixed
- 来源：架构排查 / 用户问题分析
- 描述：`AITestPlanningPanel` 内部会根据当前选择切换 `sessionId` 状态，但渲染 `SessionProjectPanel` 时仍传入初始 `sessionIdProp`。结果是用户切换到新 planning session 后，项目创建、关联、查询仍落到旧 session；当前 session 可能没有 project，后端工具网关会拒绝 `explore_page`、`explore_flow`、`capture_page_session`，表现为 AI “不会主动调用工具”或“调用了也没探索”。
- 复现步骤：
  1. 打开 AI Test Planning 面板并创建或切换到另一个 session
  2. 在项目区域创建或关联项目
  3. 继续在当前 session 里生成 draft 或触发探索
  4. 观察项目实际被绑定到旧 session，当前 session 仍可能显示或落入“无 project”状态，探索工具不可用
- 影响：planning 阶段的自动页面探索、会话态采集、flow 探索都会被隐藏前置条件拦截，用户容易误以为问题来自 prompt 或 DOM 工具能力，实际是 session/project 绑定链路失真
- 根因：
  - `frontend/src/components/AITestPlanningPanel.tsx:183` 使用实时 `sessionId` 状态管理当前会话
  - 但 `frontend/src/components/AITestPlanningPanel.tsx:621` 传给 `SessionProjectPanel` 的仍是静态 `sessionIdProp`
  - `frontend/src/components/SessionProjectPanel.tsx:28`、`:42`、`:68` 的查询/创建/关联请求全部依赖这个错误的 `sessionId`
- 处理：`AITestPlanningPanel.tsx:621` 将 `sessionId={sessionIdProp}` 改为 `sessionId={sessionId ?? 0}`，使 `SessionProjectPanel` 始终使用当前状态中的会话 ID 而非初始 prop
- 验证：
  - 代码检查：`frontend/src/components/AITestPlanningPanel.tsx:621`
  - TypeScript 类型检查通过
  - 切换 session 后创建/关联项目的请求会使用当前 session id
- 关联记录：`docs/execution-log.md` 2026-05-03（AI planning 中间层排查）

## BUG-057 | click_with_precheck 对 hidden 元素超时不触发恢复链，force click 被跳过

- 日期：2026-05-03
- 状态：fixed
- 来源：Session 15 E2E 测试 — Automation Exercise 品牌筛选购物车，Step 点击 "Continue Shopping"
- 描述：点击 modal 中的 "Continue Shopping" 时，Playwright 报告 `Locator.wait_for: Timeout 5000ms exceeded. 15 × locator resolved to hidden`。元素在 DOM 中存在但 CSS display/visibility 使其处于 hidden 状态（Bootstrap modal 动画期间），`locator.click()` 等待 5s 后超时。但 `_is_interception_error` 只匹配 `"intercepts pointer events"`，不匹配 `"resolved to hidden"`，导致整个 5 策略恢复链（wait→dismiss→avoid→force→remove）完全被跳过，force click 没机会执行。
- 复现步骤：
  1. 执行 Automation Exercise 品牌筛选购物车测试用例
  2. 点击 "Add to cart" 后 modal 弹出
  3. 尝试点击 modal 中 "Continue Shopping" 按钮
  4. 元素存在但 Playwright 判定为 hidden，等待 5s 超时
  5. 错误类型不匹配 `INTERCEPT_PATTERN`，恢复链不触发，直接失败
- 影响：所有因 CSS animation/transition 期间元素 visibility 判定为 hidden 的点击场景（Bootstrap modal 弹出、fade-in 动画、tab 切换等）
- 根因：`_is_interception_error` 的设计范围只覆盖了"元素被其他元素遮挡"的场景，未覆盖"元素自身 hidden（CSS 动画过渡期）"的场景。前者是需要清除遮挡物，后者只需要 force click 绕过可见性检查
- 处理：在 `click_with_precheck` 中新增 `_HIDDEN_ELEMENT_PATTERN` 正则匹配 `"resolved to hidden"`，检测到 hidden 元素超时时直接走 `_try_force`（`force=True` + JS `el.click()` 兜底），不进入完整的 interception 恢复链（dismiss/remove 策略对 hidden 元素有害）
- 验证：471 单元测试通过；点击预处理相关测试通过
- 关联记录：execution-log 2026-05-03

## BUG-056 | DSL draft prompt 超 50000 字符导致 Pydantic 校验失败

- 日期：2026-05-03
- 状态：fixed
- 来源：Session 15 E2E 测试 — 生成 DSL 草案时 `1 validation error for GenerateDslRequest prompt String should have at most 50000 characters`
- 描述：`_build_draft_prompt` 将完整的 `page_elements`（格式化 DOM 元素清单，可达 80000+ 字符）直接嵌入 `draft_prompt` 字符串中。该 `draft_prompt` 作为 `GenerateDslRequest.prompt` 传入 Pydantic 校验，触发 `max_length=50000` 限制。实际上 `page_elements` 已通过 `GenerateDslRequest.page_elements` 单独字段传递，DSL 生成器在 `_build_user_prompt_lines` 中单独注入——嵌入到 `prompt` 里是完全冗余的。
- 复现步骤：
  1. AI 规划会话中 `explore_page`/`explore_flow` 采集了 300+ 页面元素
  2. `_build_draft_prompt` 将 80K+ 字符的元素清单拼入 draft_prompt
  3. 生成 DSL 草案时 `GenerateDslRequest(prompt=draft_prompt)` 校验失败
  4. 所有 1 个草案均生成失败
- 影响：所有页面元素较多（>200 个可交互元素）的测试场景都无法生成 DSL 草案
- 根因：`page_elements` 数据在两个渠道重复传递——嵌入 `prompt` 字段 + 独立 `page_elements` 字段。嵌入 `prompt` 是历史遗留（`page_elements` 字段是后来加的），未做清理
- 处理：`_build_draft_prompt` 中将嵌入式 DOM section 替换为简短提示"页面可交互元素清单已通过 page_elements 字段单独提供"，实际数据仍通过 `GenerateDslRequest.page_elements` 传递
- 验证：471 单元测试通过
- 关联记录：execution-log 2026-05-03

## BUG-055 | create_project 成功后 project_id 局部变量未更新，同一 turn 内后续工具调用被拦截

- 日期：2026-05-03
- 状态：fixed
- 来源：Session 15 E2E 测试 — 无项目→创建会话→AI 调用 create_project→后续 explore_page/capture_page_session 全部失败
- 描述：当会话无关联项目时，`project_id=0` 传入 `stream_planning_turn`。AI 调用 `create_project` 成功后，DB 中项目已创建且会话已关联，但内存中 `project_id` 局部变量从未被更新，仍为 0。同一 turn 内 AI 再调用 `explore_page`/`capture_page_session`/`explore_flow` 时，`execute_tool` 检查 `not project_id` 为 True，返回 `"当前会话未关联项目"`。虽然每个新 turn 开始时 `ai_planning.py` 会从 DB 重新读取 `project_ids`，但同一 turn 内 AI 必须先建项目才能探索页面的流程完全不可用。
- 复现步骤：
  1. 新建项目、创建会话（无项目关联）
  2. 发送测试需求
  3. AI 在 ReAct 循环中调用 `create_project` → 成功，返回 `{"id": N, "auto_linked_to_session": true}`
  4. AI 继续调用 `explore_page` → `execute_tool` 检查 `project_id==0` → 返回错误
  5. 或 AI 调用 `generate_plan` → `_auto_explore_entry_url` 使用 `project_id=0` → 探索全部被拦截
- 影响：无项目时创建会话的完整 AI 规划流程完全不可用——AI 必须分两个 turn 才能完成"先建项目→再探索页面"的基本操作
- 根因：`stream_planning_turn` 中 `project_id` 是局部变量，在 `create_project` 工具调用成功后未从返回结果中提取新 ID 更新
- 处理：在 ReAct 循环 `call_tool` 分支中，`create_project` 成功后从 `parsed_result["id"]` 提取新项目 ID 并更新局部 `project_id`；同时更新 `_extract_exploration_error` 检测 `"info"` 类型响应（no-project 消息）作为错误
- 验证：471 单元测试通过；79 个 planning 相关测试通过
- 关联记录：execution-log 2026-05-03

## BUG-054 | AI 忽略用户描述的弹层交互步骤，用导航栏元素替代弹层元素

- 日期：2026-04-25
- 状态：fixed
- 来源：Session 52 端到端链路测试 — Exec 67 Step 12 失败
- 描述：用户需求明确写了「在弹层中点击 View Cart，跳转购物车页」，但 AI 生成的 DSL 使用导航栏 "Cart" 而非弹层 "View Cart"。点击 "Add to cart" 后出现确认弹层遮挡了导航栏 "Cart"，导致 click 超时失败。Exec 67 login_error 场景 11/12 通过，仅 Step 12 失败。
- 复现步骤：
  1. Session 52，Draft 25 login_error 场景
  2. Step 10 click "Add to cart" → 弹层出现
  3. Step 11 wait_for "Cart" → passed（导航栏 Cart 可见）
  4. Step 12 click "Cart" → 超时，弹层遮挡了导航栏 Cart
- 影响：所有涉及动态弹层交互的测试场景（如加购确认弹层、删除确认弹层等）
- 根因：(1) 静态 explore_flow 无法采集点击后弹出的动态元素，AI 在 DOM 中只看到导航栏 "Cart"；(2) AI 未严格遵循用户描述的弹层交互流程
- 处理：三重修复：(1) 新增 `_discover_interactive_elements()` 点击关键按钮捕获弹层元素；(2) Prompt 追加动态交互规则强制 AI 保留用户描述的弹层步骤；(3) `format_elements_for_prompt` 添加 `[dynamic]` 标记区分动态元素
- 验证：Session 53 Draft 26/27 正确使用 "View Cart"；Exec 69/70 各 13/13 全部通过
- 关联记录：execution-log 2026-04-25

## BUG-053 | VLM bbox 坐标在 DOM 选择器提取失败时被丢弃

- 日期：2026-04-25
- 状态：fixed
- 来源：BUG-054 根因分析 — VLM 能看到弹层元素但无法点击
- 描述：VLM 视觉定位返回了准确的 bbox 坐标 `(center_x, center_y)`，但 `_build_locator_from_ai_point()` 将坐标转为 DOM 选择器时失败（弹层元素无法通过 `document.elementsFromPoint` 正确解析），此时整个 `AILocateResult` 被丢弃，系统直接抛出 `InterventionNeededError`。Playwright 原生支持 `page.mouse.click(x, y)` 坐标点击但从未使用。
- 复现步骤：
  1. 执行含弹层交互的用例
  2. Tier 1 语义定位失败
  3. Tier 2 VLM 返回有效 bbox
  4. `_build_locator_from_ai_point` DOM 快照提取失败 → 返回 None
  5. bbox 坐标丢失 → 抛出 InterventionNeededError
- 影响：所有 VLM 能看到但 DOM 无法正确解析的元素（弹层、shadow DOM、iframe 内容等）都无法自动定位
- 根因：fallback 链缺少 bbox → 坐标点击的回退路径
- 处理：(1) `ResolvedLocator` 新增 `click_coordinates` 字段；(2) 新增 `_try_coordinate_click_fallback()` Tier 2.5 回退；(3) Runner click/input 步骤支持 `page.mouse.click(x,y)` + `page.keyboard.type()`
- 验证：单元测试覆盖坐标回退、VLM None 回退、非法坐标过滤；端到端 Exec 69/70 全部通过
- 关联记录：execution-log 2026-04-25

## BUG-052 | AI DSL 生成在无 DOM 快照时仍猜测不存在的 CSS 选择器

- 日期：2026-04-24
- 状态：fixed
- 来源：BUG-050 修复验证 — 端到端 AI Planning 全流程执行
- 描述：AI Planning 会话 session_id=41 中，草案 Draft 17 的 Step 3 target 为 `#input-email`，但实际登录页面中 email 输入框无 id 属性（实际 placeholder="Email Address"）。AI 在没有通过 `explore_page` 获取 DOM 快照的情况下，仍然"猜测"了不存在的 `#input-email`。DOM 证据注入功能（BUG-050 修复）虽然已实现，但本次执行中 `page_elements` 为 null，说明 planning agent 未执行 `explore_page` 工具调用。
- 复现步骤：
  1. 创建 AI Planning 会话（session_id=41）
  2. 发送 Automation Exercise 测试需求
  3. AI 直接生成 plan（status=plan_ready），未调用 explore_page 工具
  4. 生成 DSL 草案，Draft 17 使用 `#input-email`（不存在）
  5. 执行失败，Step 3 所有定位层级无法匹配
- 影响：AI 在未访问目标页面的情况下生成 DSL，选择器准确性依赖 AI 训练知识而非实际 DOM，与 BUG-050 修复目标（基于 DOM 证据生成选择器）矛盾
- 根因：planning agent 的 ReAct 流程中，`explore_page` 调用是可选的而非强制的。当用户一次提供完整需求时，agent 直接跳到 plan 生成，未触发页面探索
- 修复：在 ReAct 循环的 `generate_plan` 分支前插入强制检查——若无 explore_page/flow 调用则自动用 entry_url 触发 explore_page，结果注入 tool_calls 后 continue 让 LLM 基于真实 DOM 重新生成；同时新增 `explore_flow` 工具支持多页面探索 + VLM 页面布局注解
- 验证：303 单元测试全部通过；待端到端链路测试验证
- 关联记录：execution-log 2026-04-24 Session 2、BUG-050

## BUG-051 | input_contract 变量占位符在执行时未被替换

- 日期：2026-04-24
- 状态：fixed
- 来源：BUG-050 修复验证 — 端到端 AI Planning 全流程执行
- 描述：AI Planning 生成的 DSL 草案包含 `input_contract`（如 `${login_email}`、`${login_password}`、`${search_keyword}`），但 save-and-execute 执行时，这些变量占位符未被替换为实际值。Execution 54 的 Step 8 URL 显示 `?search=${search_keyword}`，说明 `${search_keyword}` 被直接作为字符串输入到搜索框
- 复现步骤：
  1. AI Planning 生成包含 input_contract 的 DSL 草案
  2. save-and-execute 执行草案
  3. 查看执行结果，value 字段中的 `${context_key}` 占位符未被替换
- 影响：所有使用 input_contract 变量的 DSL 用例都无法正确执行，输入的是占位符字符串而非实际测试数据
- 根因：变量替换功能完全未实现——runner 直接使用 `step.value` 原始字符串，`CaseExecutionRequest` 无 `input_values` 参数，整条链路缺失
- 修复：`playwright_runner.py` 新增 `_substitute_variables` 函数（正则 `\$\{([A-Za-z_][A-Za-z0-9_]*)\}` 替换），`CaseExecutionRequest` 增加 `input_values` 字段，runner 的 goto/input/assert_text/assert_url_contains 四处 step.value 使用处全部替换，`executions.py` 透传参数
- 验证：303 单元测试全部通过；待端到端链路测试验证
- 关联记录：execution-log 2026-04-24 Session 2

## BUG-050 | AI DSL 生成定位策略不匹配 DOM 结构（选择器容器错误）

- 日期：2026-04-23
- 状态：fixed
- 来源：白盒测试验证（Automation Exercise 场景）
- 描述：AI 生成 DSL 时产出链式选择器 `.productinfo text='View Product'`，但实际 DOM 结构中 `.productinfo` 内部不包含 "View Product" 文本——二者是兄弟关系（`.product-image-wrapper > (.productinfo + a[text=View Product])`），导致链式定位 `page.locator(".productinfo").get_by_text("View Product")` 匹配 0 个元素。类似地，`button:has-text('Add to cart')` 和 `u:has-text('View Cart')` 等非标准复合选择器也会导致定位失败。本质是 AI 在没有 DOM 快照的情况下"猜测"选择器，CSS 容器和内部文本的对应关系容易出错。
- 复现步骤：
  1. 通过 AI Planning 会话描述"测试 automationexercise.com 登录到购物车流程"
  2. AI 生成 DSL 草案，Case 9 Step 8 target 为 `.productinfo text='View Product'`
  3. 执行用例，Step 8 定位器返回 0 candidates，触发 needs_intervention
  4. 手动验证 `page.locator('.productinfo').get_by_text('View Product').count() == 0`，而 `page.locator('.product-image-wrapper').get_by_text('View Product').count() == 14`
- 影响：AI 生成的旧 DSL 用例（Case 8/9）执行失败，需要人工干预或重新生成
- 根因：AI 无 DOM 快照时凭语义猜测 CSS 容器与文本的包含关系，容易选错父级容器。同时定位器系统此前不支持链式选择器解析（BUG-049 的延伸），即使 DOM 正确也无法处理 `.class text=value` 格式
- 处理：五重修复：
  1. **定位器侧**（`semantic.py`）：新增 `_resolve_chained_selector` 函数，解析 `.class text=value`、`.class >> text=value` 等 Playwright 链式选择器格式为 `page.locator(css).get_by_text(value)`，策略 `chained_css_text` 评分 110
  2. **Prompt 侧**（`dsl_generator.py`）：v2026-04-22.target-strategy-v1 prompt 禁止生成无效复合格式，引导 AI 使用语义文本（如直接写 `View Product`）或带 `target_strategy` 字段的显式定位
  3. **Schema 侧**（`dsl.py`）：新增 `target_strategy` 字段允许显式声明定位策略
  4. **数据库侧**：`test_case_runs.error_message` 从 VARCHAR(2000) 改为 Text，解决长错误信息存储溢出
  5. **DOM 证据注入**：将 planning agent 的 `explore_page` DOM 数据传递到 DSL 生成 prompt（`_extract_page_elements → _build_draft_prompt → GenerateDslRequest → dsl_generator`），AI 基于 DOM 元素清单生成 target，不再猜测
  6. **target_strategy 偏好提示**：`resolve_semantic_locator` 将 `target_strategy` 从锁死改为偏好提示，hint 失败后 fallback 到全量语义扫描
- 验证：
  - 单元测试 `test_locator_semantic.py::TestChainedSelector` 7/7 passed
  - Playwright 实际验证：`.productinfo >> text=Add to cart` → 14 matches，`.product-image-wrapper >> text=View Product` → 14 matches
  - 旧 Case 9 执行：链式选择器解析正确（正则匹配成功，构建了正确的 Playwright 链式调用），但 DOM 结构不符导致 0 candidates——属 AI DSL 策略错误而非解析器 bug
- 关联记录：execution-log 2026-04-23

## BUG-048 | AI DSL 规划阶段完整性校验缺失

- 日期：2026-04-21
- 状态：fixed
- 来源：白盒测试执行（The Internet Login Page 场景）
- 描述：AI 生成 DSL 时遗漏 goto 步骤，将 base_url 设为完整登录页 URL 但不生成 goto 步骤，导致执行器在 about:blank 上操作。根因是规划阶段缺少完整性校验，AI 不知道"入口"在哪。
- 复现步骤：
  1. 通过 AI Planning 会话描述"测试 The Internet 登录页"
  2. AI 生成草案，base_url 设为完整登录 URL，steps 中无 goto 步骤
  3. 执行 DSL 用例，所有步骤在 about:blank 上失败
- 影响：AI 生成的 DSL 可能缺少关键导航步骤，用户需要手动补齐
- 根因：Prompt 未引导 AI 评估测试完整性（前置条件、入口、步骤、预期），后处理不检查逻辑完整性
- 处理：已修复。(1) Prompt 增加测试五要素完整性引导和 base_url 规范说明（`_BASE_USER_RULE_LINES`）；(2) 后处理新增 `_check_dsl_completeness` 函数，检测 base_url 含页面路径或无 goto 步骤时发出 warning/normalization_note，不阻断生成，保持灵活性
- 验证：`cd backend && uv run pytest tests/unit/test_dsl_validation.py::TestDslCompletenessCheck -v`，5 passed
- 关联记录：execution-log 2026-04-21

## BUG-049 | 语义定位器不支持标签名开头的复合 CSS 选择器

- 日期：2026-04-21
- 状态：fixed
- 来源：白盒测试执行（The Internet Login Page 场景）
- 描述：语义定位器 `_resolve_explicit_locator` 只识别以 `css=`、`xpath=`、`//`、`#`、`.`、`[`、`data-testid=` 开头的目标，复合 CSS 选择器如 `button[type='submit']` 以字母开头落入文本匹配，永远无法定位。
- 复现步骤：
  1. 创建 DSL 用例，click 步骤 target 设为 `button[type='submit']` 或 `form button`
  2. 执行用例，定位器将 target 当作文本匹配，无法找到元素
- 影响：标签名开头的复合 CSS 选择器无法工作，影响 AI 生成和手动编写 DSL
- 根因：设计限制，显式 CSS 简写仅覆盖 `#`、`.`、`[` 三种
- 处理：已修复，双重保底。(1) 定位器侧：`_resolve_explicit_locator` 新增 `_COMPOUND_CSS_RE` 启发式正则（`^[a-zA-Z][a-zA-Z0-9]*[\.\#\[\s\>:,~\+]`），识别 `tag[attr]`、`tag.class`、`tag > child`、`tag child` 等复合模式；`_build_candidate_builders` 在已有 explicit locator 时跳过 element_id 策略；(2) AI Prompt 侧：`_BASE_USER_RULE_LINES` 增加复合 CSS 选择器使用指引
- 验证：`cd backend && uv run pytest tests/unit/test_locator_semantic.py::TestCompoundCssSelector -v`，6 passed
- 关联记录：execution-log 2026-04-21

## BUG-046 | 语义定位器缺少 element_id 和 case-insensitive 匹配策略

- 日期：2026-04-17
- 状态：fixed
- 来源：集成测试自测
- 描述：语义定位器（`semantic.py`）无法定位以 HTML id 属性命名的目标（如 “flash”），且 `get_by_label(“username”, exact=True)` 无法匹配大小写不同的标签文本 “Username”，导致 the-internet 登录流程全部步骤失败（status=needs_intervention）
- 复现步骤：
  1. 创建包含 `{“action”: “input”, “target”: “username”}` 或 `{“action”: “assert_text”, “target”: “flash”}` 的 DSL 用例
  2. 执行该用例
  3. 定位器抛出 `LocatorResolutionError(“No locator candidates matched target.”)` 或 `InterventionNeededError`
- 影响：所有使用小写目标描述或 HTML id 作为 target 的 DSL 用例均无法通过语义层定位
- 根因：`_build_candidate_builders` 未尝试将裸目标字符串匹配为 `#id` CSS 选择器；所有 label/placeholder/text 策略均使用 `exact=True`，不支持大小写不敏感回退
- 处理：新增 `element_id` 策略（`page.locator(f”#{target}”)`，优先级 100）；新增 `label_fuzzy`、`placeholder_fuzzy`、`text_fuzzy`、`button_role_fuzzy` 四个非精确匹配策略（优先级 45-60），在精确匹配失败后作为回退
- 验证：`python -m pytest tests/integration/test_platform_api_chain.py -v`，6 passed
- 关联记录：execution-log 2026-04-17 (Task 2)

## BUG-047 | playwright_runner _capture_request_failed 对 request.failure 返回格式处理错误

- 日期：2026-04-17
- 状态：fixed
- 来源：集成测试执行日志
- 描述：Playwright `requestfailure` 事件回调中 `_capture_request_failed` 调用 `failure.get(“errorText”)`，但新版 Playwright 的 `request.failure` 返回类型为 `str` 而非 `dict`，导致每次网络请求失败时抛出 `AttributeError: 'str' object has no attribute 'get'`
- 复现步骤：
  1. 执行任何包含外部网络请求的用例（如 the-internet 登录）
  2. 页面加载时部分请求失败（如 optimizely analytics）
  3. 控制台输出 `AttributeError: 'str' object has no attribute 'get'` 堆栈
- 影响：修复前导致失败请求的 `failure_text` 丢失，network_events 不完整；严重时可导致 browser.close 崩溃
- 根因：Playwright 版本更新后 `request.failure` 从 `dict` 变为 `str`，代码未适配
- 处理：已修复于 commit d73558e，改为 `isinstance(failure, str)` 兼容两种类型
- 验证：集成测试不再报 AttributeError
- 关联记录：execution-log 2026-04-17 (Task 2)

## BUG-045 | AI planning”保存并执行草案”链路被 DSL 生成配置阻断，且当前实现不支持持久化执行进度/摘要

- 日期：2026-04-13
- 状态：in_progress
- 来源：白盒排查 / 对话 `session_id=27`
- 描述：AI planning 场景中，对话规划成功后进入 DSL 草案生成阶段，但 `session 27` 的唯一草案直接落为 `failed`，错误为 `Expecting value: line 1 column 1 (char 0)`，导致后续“保存并执行”无法真正创建用例或执行。白盒复现确认 `backend/.env` 中 `AI_DSL_BASE_URL=https://api.unself.cn`，而 `backend/app/ai/dsl_generator.py` 会拼接为 `https://api.unself.cn/chat/completions`；该地址当前返回 `200 text/html` 站点首页，而不是 OpenAI 兼容 JSON，因此 `json.loads(response.read().decode("utf-8"))` 在 `_call_llm()` 内直接抛出 `JSONDecodeError`。此外，即便 DSL 生成成功，当前实现也没有 SSE/流式执行接口，`generate_planning_drafts()` 与 `save_and_execute_selected_drafts()` 只返回即时响应，不把生成结果、执行阶段或执行摘要持久化到 `ai_planning_messages`，刷新后无法从会话详情恢复这些信息。
- 复现步骤：
  1. 使用当前 `.env` 配置触发 AI planning 生成草案
  2. 查看 PostgreSQL 中 `ai_planning_drafts`，观察 `session_id=27` 的草案状态为 `failed`，错误为 `Expecting value: line 1 column 1 (char 0)`
  3. 查看 `test_cases` / `test_case_runs`，观察没有对应新增记录
  4. 按当前配置向 `POST https://api.unself.cn/chat/completions` 发起最小请求，观察响应为 `200 text/html`，正文为站点首页 HTML
  5. 检查 `backend/app/services/ai_planning.py`，观察 draft 生成与 save-and-execute 都没有写入 `AIPlanningMessage`，也不存在流式推送接口
- 影响：当前产品承诺的“勾选保存并执行后自动创建用例、在对话框流式展示执行过程、输出简洁报告并附详细链接”在现状下不能成立；一旦 DSL 生成配置错误，用户只会停留在失败草案状态，且错误信息不会以完整执行报告方式呈现
- 根因：
  - 配置层：`AI_DSL_BASE_URL` 指向站点根路径而非 OpenAI 兼容 API 根路径
  - 健壮性层：`_call_llm()` 未校验 `Content-Type` 和非 JSON 响应，`JSONDecodeError` 会在持久化失败记录前直接冒泡
  - 产品实现层：planning 会话消息模型未覆盖“draft generation result / execution progress / execution summary”，前端也没有消费 SSE 的链路
- 建议处理：
  - 先修正 `AI_DSL_BASE_URL`，并为 `_call_llm()` 增加状态码、`Content-Type`、响应体截断日志与统一 `DslGenerationError` 包装
  - 在 `generate_planning_drafts()` / `save_and_execute_selected_drafts()` 中持久化 assistant message，至少保存草案生成结果和 execution summary
  - 如要满足预期体验，再补充后端执行事件流接口与前端流式订阅/展示
- 处理进展：
  - 已修正本地 `AI_DSL_BASE_URL` 为 `/v1` 接口根路径
  - 已为 `_call_llm()` 增加非 JSON/HTML 响应防御，避免再次直接抛原始 `JSONDecodeError`
  - 已将 draft 生成结果、save result、execution summary 持久化到 `ai_planning_messages`，并让前端保存/执行后回读会话详情
  - 剩余未完成项：真正的执行中流式事件推送与逐步展示
- 验证：
  - PostgreSQL 实查 `ai_planning_sessions` / `ai_planning_messages` / `ai_planning_drafts` / `test_cases` / `test_case_runs`
  - 最小 HTTP 复现 `POST https://api.unself.cn/chat/completions`
  - 静态核对 `backend/.env`、`backend/app/ai/dsl_generator.py`、`backend/app/services/ai_planning.py`、`frontend/src/components/AITestPlanningPanel.tsx`

## BUG-044 | AI Planning 面板缓存失效会话时不会回退创建新会话

- 日期：2026-04-12
- 状态：fixed
- 来源：需求实现 / 静态检查
- 描述：当 `localStorage.ai_planning_last_session` 指向一个已删除或不存在的 session 时，`AITestPlanningPanel` 初始化会先尝试恢复该 session；恢复失败后，因为当前逻辑仍通过“localStorage 是否存在 key”判断是否需要创建新会话，导致页面停留在无活跃 session 状态。
- 复现步骤：
  1. 在浏览器本地存储中写入一个不存在的 `ai_planning_last_session`
  2. 打开 Planning 页面
  3. 观察恢复请求失败后，没有自动切换到其他会话，也没有自动创建新会话
- 影响：删除当前会话或服务端清理历史会话后，用户再次进入 Planning 页面可能无法继续对话，且会话删除功能难以稳定收口
- 根因：初始化分支依赖 localStorage key 是否存在，而不是“恢复是否成功”
- 处理：在会话删除实现计划中引入 `loadSessionDetail()` / `createAndSelectSession()` helper，恢复失败时清理本地缓存并自动创建或切换到可用会话
- 验证：
  - `cd backend && uv run pytest tests/unit/test_ai_planning_api.py -q`
  - `cd frontend && npm run test -- src/services/api.test.ts src/components/AITestPlanningPanel.test.tsx`
  - `cd frontend && npx tsc --noEmit`
- 关联记录：`docs/execution-log.md` 2026-04-12

## BUG-042 | AI 测试规划面板初始化首条消息可能丢失，且新后端回归测试默认不会被跟踪

- 日期：2026-03-30
- 状态：fixed
- 来源：自测
- 描述：实现 AI 测试规划对话助手时发现两个实际缺陷。其一，AITestPlanningPanel 在 planning session 尚未创建完成前允许点击“发送消息”，会导致首条输入被直接忽略；其二，仓库 .gitignore 中存在 tests/ 规则，会让新建的 backend/tests/unit/test_ai_planning_api.py 默认处于未跟踪状态，后续同步时容易遗漏关键回归测试。
- 复现步骤：
  1. 打开工作台后立即在 AI 测试助手输入内容并点击“发送消息”
  2. 观察前端未报错，但首条消息没有进入 transcript，也没有触发后端 planning turn
  3. 新增 backend/tests/unit/test_ai_planning_api.py 后执行 git status --short --untracked-files=all
  4. 观察测试文件最初不会出现在待跟踪列表中
- 影响：AI 测试规划首轮交互不稳定，且新增后端回归测试存在被遗漏进版本控制的风险
- 根因：发送按钮缺少对 session bootstrap 完成状态的约束；仓库忽略规则对任意 tests/ 目录一刀切，未给 backend/tests 留出白名单
- 处理：发送按钮增加 isBootstrapping、sessionId 和空输入约束，导入草案后使用服务端返回结果刷新状态；.gitignore 新增 backend/tests/unit/test_ai_planning_api.py 的定向白名单规则，确保新测试可被跟踪
- 验证：
  - cd frontend && npm run test -- src/components/AITestPlanningPanel.test.tsx
  - git status --short --untracked-files=all | Select-String "test_ai_planning_api.py"
- 关联记录：docs/execution-log.md 2026-03-30 23:15

## BUG-041 | 最新 CRUD 提交存在权限绕过、统计接口运行时失败与删除路径不闭合

- 日期：2026-03-30
- 状态：fixed
- 来源：代码评审 / 最新提交 `7eb71ae`
- 描述：审查最新 CRUD 提交时发现 4 类问题。其一，`cases` 路由和 service 的新增读写接口仅校验”已登录”，没有校验当前用户是否属于目标项目，导致任意已登录用户都能读取、更新、删除其他项目的用例；其二，`GET /api/v1/cases/stats/{project_id}` 声明返回 `ProjectTestCaseStats`，但 service 返回值缺少必填字段 `created_by_user`，路由层会在构造响应模型时直接触发校验错误；其三，`delete_project()` 直接删除项目，但 `test_cases.project_id` 的外键是 `ondelete=”RESTRICT”`，已有用例的项目无法被删除并会在提交时抛出数据库完整性错误；其四，原有 `GET /api/v1/cases` 与 `GET /api/v1/projects` 的响应合同已经变化，但对应单测没有更新，现有测试已失败
- 复现步骤：
  1. 以任意已登录用户访问 `/api/v1/cases`、`/api/v1/cases/project/{project_id}`、`/api/v1/cases/{case_id}`、`PUT /api/v1/cases/{case_id}` 或 `DELETE /api/v1/cases/{case_id}`，观察代码路径中没有项目成员校验
  2. 调用 `/api/v1/cases/stats/{project_id}`，观察 `backend/app/api/routes/cases.py` 会执行 `ProjectTestCaseStats(**stats_data)`，而 `backend/app/services/cases.py` 返回值缺少 `created_by_user`
  3. 创建带 `test_cases` 的项目后调用 `DELETE /api/v1/projects/{project_id}`，观察 `backend/app/services/project_management.py` 直接删除项目，而 `backend/app/models/test_case.py` 将外键定义为 `ForeignKey(“projects.id”, ondelete=”RESTRICT”)`
  4. 执行 `uv run pytest backend/tests/unit/test_cases_api.py -q` 与 `uv run pytest backend/tests/unit/test_projects_and_report_preferences_api.py -q`，观察列表接口断言失败
- 影响：当前提交标称”complete CRUD”，但实际存在越权访问风险、统计接口 500 风险、项目删除不可用风险，以及已存在 API 消费方/测试的兼容性回归
- 根因：新增 CRUD 与分页/统计逻辑时，只补了路由和 service 主干，没有沿项目成员边界、响应 schema、一对多删除约束和历史接口合同做完整联动校验
- 处理：已在 `082ae22` 中全部修复——补齐项目成员权限校验、修正 stats 返回结构、处理外键约束下的项目删除语义、更新测试断言匹配新的分页响应
- 验证：
  - `uv run pytest backend/tests/unit/test_cases_api.py -q`，全部通过
  - `uv run pytest backend/tests/unit/test_projects_and_report_preferences_api.py -q`，全部通过
  - 静态核对 `backend/app/api/routes/cases.py`、`backend/app/services/cases.py`、`backend/app/services/project_management.py`、`backend/app/schemas/cases.py`、`backend/app/models/test_case.py`
- 关联记录：`docs/execution-log.md` 2026-03-30 21:31、2026-03-30 22:00

## BUG-043 | 新增 AI planning 配置字段后，settings API 更新合同未同步，导致现有 PUT /settings/ai 测试与调用方 422

- 日期：2026-04-03
- 状态：fixed
- 来源：任务实现 / 回归测试
- 描述：在为 AI planning 新增 `enable_ai_planning`、`ai_planning_model`、`ai_planning_base_url`、`ai_planning_timeout_ms`、`ai_planning_max_react_rounds` 与密钥字段后，`AISettingsUpdateRequest` 已要求这些字段必填，但原有 `backend/tests/unit/test_ai_settings_api.py` 和若干前端保存配置路径仍沿用旧 payload，未补 planning 字段，触发 `422 Unprocessable Entity`。
- 复现步骤：
  1. 保持新增 planning 字段后的后端 schema 不变
  2. 使用旧版 payload 调用 `PUT /api/v1/settings/ai`
  3. 观察接口返回 422，`test_update_ai_settings_persists_to_env_file_and_allows_clearing_keys` 与 `test_update_ai_settings_accepts_glm_model_family` 失败
- 影响：AI settings 保存链路在 contract 层不一致，新增 planning 配置后会阻断原有 settings 更新回归测试，也容易让前端保存逻辑出现兼容性回退
- 根因：配置 schema 已扩展，但测试样例和部分前端表单/类型没有同步补齐新增字段，形成请求合同漂移
- 处理：补齐后端测试中的 planning 字段；前端 `AISettings` / `AISettingsUpdatePayload`、`AISettingsPage` 表单初始化与保存请求一并纳入 planning 字段，消除 settings 合同漂移
- 验证：
  - `cd backend && uv run pytest tests/unit/test_ai_settings_api.py -q`
  - `cd frontend && npm run test -- src/pages/AISettingsPage.test.tsx src/services/api.test.ts`
- 关联记录：`docs/execution-log.md` 2026-04-03 23:02
