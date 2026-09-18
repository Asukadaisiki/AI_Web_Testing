# 上下文预算设计：在 DeepSeek thinking+tools 硬约束下控制上下文增长

- 日期：2026-09-18
- 状态：**P0/P2/P3/P4 已实施**（P1 度量随 P3 落地；P5 未开始）
- 关联：BUG-201（intent 全等比对，已修）、BUG-202（配置阻断）、BUG-203（推理回放约束）、BUG-199（grounding 撞墙钟）、BUG-200（根因已推翻）、`docs/plan/2026-09-13-grounding-plan-collapse.md`
- 实测数据来源：`ai_web_testing` 库 `agent_events` 的 `prompt_spec.request_budget` 与 `reasoning.content_bytes`

---

## 实施进展（2026-09-18）

| 阶段 | 状态 | 落地内容 |
|---|---|---|
| **P0** | ✅ 已实施 | `browser-worker/.env`：`AI_PLANNING_BASE_URL` → `https://api.deepseek.com`，`AI_PLANNING_MODEL` → `deepseek-flash` |
| **P1** | ✅ 已具备 | `prompt_cache_hit_tokens`/`miss`/`cached_tokens` 由 BUG-155 聚合，实测可观测 |
| **P2** | ✅ 已实施 | `compiler.go` 放宽 `intent`（仅非空）+ 门禁错误指明字段；`service.go` 顺序/越界门禁错误附「当前 pending 步骤序列」 |
| **P3** | ✅ 已实施 | 轮内改写改为 `AGENTSERVICE_PER_TURN_TRANSCRIPT_COMPACTION` 显式开启，默认 append-only（缓存优先） |
| **P4** | ✅ 已实施 | `agent.Message.SegmentBoundary` + `agent.ModelContext` 投影；harness 在 plan 转为 `ready_for_generation` 时注入 grounded-plan 交接并开启新 segment（每修订一次） |
| **P5** | ⬜ 未开始 | `get_observation` 只读取回工具、下调 exploration 预算、精简工具定义/system prompt |

---

## 0. 相对上一轮口头分析的更正（重要）

上一轮我提出 P1「只回放最近一轮推理，可立减 54%」。**该方案在 DeepSeek 官方 API 下会导致 HTTP 400，必须废弃。**

DeepSeek 官方《Thinking Mode》文档原文（[api-docs.deepseek.com/guides/thinking_mode](https://api-docs.deepseek.com/guides/thinking_mode/)）：

> for requests carrying the `tools` parameter, the `reasoning_content` must be **fully passed back** to the API in all subsequent requests — even for turns where the model did not perform a tool call. If your code does not correctly pass back `reasoning_content`, the API will return a **400 error**.

而本项目的 Agent 循环**每一轮都携带 `tools`**（`agent/loop.go:90` 恒定传入 `l.definitions`）。因此：

> **推理通道不可裁剪、不可丢弃、不可只保留最近一轮。**

现有代码 `platform/llm/openai.go:395-397` 的做法（thinking 开启时为每条 assistant 消息回填 `ReasoningContent`）**是与官方要求一致的、正确的**。这一点必须在方案里固化为不变量，避免后续被当成"低垂果实"误优化。

---

## 1. 问题陈述（实测基线）

三轮 live E2E（2026-09-18）均在 900s 墙钟取消。以 round3（`run_4a7be4be7f8be3c34e275503`）末次请求 seq 163 为例，共 252,918 字节：

| 通道 | 字节 | 占比 | 可裁剪性 |
|---|---|---|---|
| **assistant 推理回放** | **137,896** | **54.5%** | ❌ 官方禁止裁剪 |
| tool 结果（含 exploration 摘要 44,402） | 46,238 | 18.3% | ✅ 已是摘要且有预算 |
| tool 定义（每轮全量重发） | 22,212 | 8.8% | ⚠️ 可精简 |
| assistant 工具入参 | 16,339 | 6.5% | ⚠️ 随轮次增长 |
| system prompt | 13,081 | 5.2% | ⚠️ 可精简 |
| assistant 正文 | 2,711 | 1.1% | — |
| 可恢复错误 | 236 | 0.1% | — |

增长轨迹（round3，13 次调用）：

- 逐轮**新增**推理：11,973 → 117 → 22,905 → 14,632 → 5,236 → 6,925 → 10,070 → 12,913 → 18,009 → 14,893 → 18,670 → 1,553 → 39,789 字节。
- 末次**回放**推理 137,896 = 前 12 轮之和（逐项验算精确吻合）。
- input tokens：10,059 → 63,694（**6.3 倍**）。
- 时间：LLM 累计 651s / 900s（72%）；round2 为 629s / 900s（70%），单次峰值 251s。

**结论：上下文增长 = O(轮次 × 每轮推理)，且推理项不可回收。**

---

## 2. 外部 API 约束（设计的前提）

### 2.1 推理必须全量回传（硬约束）

- 触发条件：请求携带 `tools`。
- 后果：缺失即 400，run 直接失败。
- 适用范围：**所有后续轮次**，包括模型未发起工具调用的轮次。
- 跨"用户轮次"（我们语境下即跨阶段）同样要求回传。

**✅ 已实测确认（2026-09-18，DeepSeek 官方端点）**：构造一个携带 `tools` 的请求，assistant 消息带 `tool_calls` 但省略 `reasoning_content`：

```
HTTP 400  {"error":{"message":"The `reasoning_content` in the thinking mode must be passed back to the API."}}
```

同请求补回 `reasoning_content` 后返回 200（`finish_reason=stop`）。**该约束为运行时硬约束，非文档理论要求。**

→ 设计含义：**只要还在同一个带 tools 的对话里，推理就只能增长。**

### 2.2 上下文缓存默认开启（可利用的杠杆）

DeepSeek 官方《Context Caching》文档（[api-docs.deepseek.com/guides/kv_cache](https://api-docs.deepseek.com/guides/kv_cache/)）：

- 默认对所有用户开启，无需改代码。
- 命中规则：后续请求必须**完整匹配**某个**缓存前缀单元**；前缀单元在「用户输入结束位置」「模型输出结束位置」持久化，长文本还会按固定 token 间隔切分。
- 命中情况通过 `usage.prompt_cache_hit_tokens` / `prompt_cache_miss_tokens` 返回。

**对本项目极其有利**：我们的 transcript 是**只追加**的（`loop.go:95`），每轮请求都是上一轮的严格前缀延伸 —— 这正是缓存命中的理想形态。理论上推理回放虽大，却应当大量命中缓存（更便宜、更快）。

**但存在一个直接冲突**：`agent/tool_result.go:643` 的 `CompactExplorationTranscript` 会**改写历史消息内容**（`transcript[item.index].Content = string(encoded)`），每改写一次就改变一次前缀，导致该点之后的缓存全部失效。调用点在 `harness/harness.go:822`，**每一轮工具结果后都会尝试压缩**。

> 即：「压缩上下文体积」与「保住前缀缓存」在实现上互相拆台。这是本方案必须显式权衡的核心张力。

### 2.3 已就位的度量能力

BUG-155 已实现 provider cache hit/miss 累计聚合（`PipelineCumulativeUsage`）与 run 级成本熔断（`config.go:65-67`：`MaxModelCalls=40`、`MaxTotalTokens=3_000_000`、`MaxTranscriptBytes=1_000_000`）。度量与刹车都在，缺的是**策略**。

---

## 3. 可压缩性边界（本篇的核心判断）

| 通道 | 能否压缩 | 依据 |
|---|---|---|
| assistant 推理 | **不能** | 官方硬约束；裁剪 = 400 |
| tool 结果 | 能 | 已是确定性摘要；48KB 聚合预算已生效（实测触顶后稳定） |
| tool 定义 | 能（有限） | 静态，可精简描述与 schema |
| system prompt | 能（有限） | 13KB，其中 PHASE 2.5 Web 知识段可评估 |
| assistant 正文/入参 | 弱 | 入参随轮次增长，但不可丢（否则工具调用悬空） |
| **轮次数量** | **能，且是唯一能压制推理项的手段** | 推理 ∝ 轮次 |

**因此设计重心必须从"压缩内容"转向"控制轮次 + 分段重置"。**

---

## 4. 设计目标与不变量

**不变量（不可违反）**

1. 凡发送给带 `tools` 请求的 assistant 消息，其 `reasoning_content` 必须原样保留。
2. `taskplan` 是 grounding 结果的唯一权威；跨阶段交接只依赖它，不依赖对话历史。
3. 不得为单一 E2E 任务硬编码（AGENTS.md「No Task-Specific Hardcoding」）。
4. 不得重新引入影子状态机（`groundingplan` 的失败模式，见 BUG-196）。

**目标**

- 把上下文从「随轮次无界累积」改为「按阶段有界」，并让已占用的上下文尽量落在缓存里。
- 900s 墙钟内收敛到 `ready_for_generation` + `generate_dsl`。

---

## 5. 方案总览：四个杠杆

| 杠杆 | 作用对象 | 预期收益 |
|---|---|---|
| **L1 轮次最小化** | 推理项（唯一手段） | 直接线性削减推理总量 |
| **L2 阶段边界上下文重置** | 推理项累积上限 | 把无界累积切成有界段 |
| **L3 缓存友好化** | 已占用上下文的代价 | 降本降延迟，不减体积 |
| **L4 非推理通道收紧** | tool/system 通道 | 体积再降 20-30KB |

L1、L2 是治本；L3 是让"治不好的大上下文"变便宜；L4 是收尾。

---

## 6. 详细设计

### L1 轮次最小化

推理总量 ∝ 轮次，故这是唯一能真正削减推理的手腕。

1. **消除门禁空转**（收益最大、成本最低）
   实测 round2/round3 的 `tool.failed` 中，纯空转占比很高，每一次都永久增加一份推理：
   - `DSL step 1 does not preserve plan step "open_products" semantics`（seq 215/225，BUG-201：`intent` 全等比对，模型两次都因此被拒且猜错原因，白烧 2 轮 + 300s）。
   - `expected next plan step "open_products", got "search_blue_top"`（round3 seq 54）。
   - `explore_flow action references unbound plan step "add_to_cart"`（round3 seq 148）。
   - `explore_flow candidate_ref requires grounding.query.v2`（round2 seq 97）。
   措施：① 修 BUG-201（`intent` 降级为非空/语义片段比对）；② 让门禁错误**指明具体字段与允许集合**（当前 `compileDraftStep` 把 8 个字段的 OR 合成为一条无信息量消息，模型只能瞎猜）；③ 在工具描述/摘要里显式给出「下一步允许的 plan_step_id 列表」。

2. **保留并强化批量探索**：`explore_flow` 已支持单次多动作，应继续引导"一次覆盖一个页面转换"，避免每步一次调用。

3. **失败信息可行动**（已部分落地）：`_flow_failure_candidate_hints` 已返回候选语义 locator，减少"盲猜重试"轮次。

4. **推理力度分档**：`AI_PLANNING_REASONING_EFFORT` 目前固定 `high`（`browser-work/.env`）。官方映射表显示 `low`→low、`high`→high、`max`→max，即 effort 直接影响 CoT 长度。机械性轮次（如已 grounded 后的 DSL 生成）可降到 `low`，预计每轮推理显著缩短，进而压低累积量。

### L2 阶段边界上下文重置（治本）

**动机**：既然推理不能裁剪，唯一的上限控制就是「不让一个对话无限长」。而在阶段边界重置恰好是**安全**的 —— 因为 grounding 的成果已经全部落在 `taskplan`（plan + `target_bindings` + evidence 引用）里，它是可序列化的权威载体，不需要靠对话历史承载。

**阶段划分**

| 阶段 | 工具集 | 出口产物 |
|---|---|---|
| A. Grounding | `set_task_plan`、`explore_page`、`explore_flow` | `taskplan` 全步 grounded + target bindings |
| B. Generation | `generate_dsl` | DSL draft（纯函数式：plan → DSL） |
| C. Repair/Approve | `ask_user_question`、`execute_dsl`、`fix_and_retry`、`get_report` | 执行与修复 |

**交接设计**

- 从 A 进入 B 时，**以新的 transcript 开始**：system prompt + 「grounded plan 的结构化导出」（每步 id/action/intent/value/target_binding/candidate/observed_count）+ 验收规格，**不带** A 阶段的探索历史与推理。
- 这不违反 §2.1：官方约束针对的是"同一 messages 数组中已存在的 assistant 消息必须带上其推理"。开启新对话即不携带旧消息，属于合法重置（模型会丢失探索直觉，由结构化交接补偿）。
- 实现位置：`harness` 层引入 run 内 **segment** 概念 —— 一个 run 可包含多个 segment，每个 segment 一个独立 transcript，但共享同一个 `taskplan`/`agent_events`。事件与审计不受影响。
- 风险与缓解：B 阶段若发现 plan 有缺陷需回到 A，属于"跨段回退"，应允许（新开 A 段），但要防止抖动 —— 用既有的一次性约束（`CreateVersion` 相同 PlanSHA256 为 no-op，BUG-181）抑制。

**这是本方案收益最大的一项**：round2 在走到 `generate_dsl` 时已累积 137KB 推理，而 B 阶段真正需要的只是那份 13 步的结构化 plan（约数 KB）。

### L3 缓存友好化

1. **停止每轮改写历史**：`CompactExplorationTranscript` 当前在每个工具结果后都可能改写旧消息，破坏前缀缓存。建议改为：
   - 默认**不在轮内压缩**（让 append-only 形态保持，吃满缓存）；
   - 仅在**阶段边界**（L2 的重置点）做一次整理，因为那里本来就要重建上下文；
   - 若必须在轮内压缩，则**只压缩最新的尾部消息**，绝不改写已发出的早期消息。
2. **保持前缀字节稳定**：system prompt 内不得插入时间戳、随机 ID、可变路径；`tool_definition_bytes` 排序稳定（现有 `definitions` 顺序固定，需回归保证）。
3. **度量闭环**：把 `prompt_cache_hit_tokens` / `prompt_cache_miss_tokens` 纳入 E2E 结果断言（BUG-155 的聚合已存在），把"缓存命中率"变成可观测指标。
4. **需要实测确认**：DeepSeek 官方端点上，revision 每次追加是否稳定命中。这是本方案**必须先验证的假设**。

**✅ 已实测（2026-09-18）**：对同一请求体连续发送 3 次，`usage` 稳定返回

```
prompt=369  prompt_cache_hit_tokens=128  prompt_cache_miss_tokens=241  cached_tokens=128
```

即**前缀缓存确实生效且可观测**（约 35% 命中，命中部分为工具定义等稳定前缀）。同时确认官方响应带 `prompt_cache_hit_tokens` / `prompt_cache_miss_tokens` / `prompt_tokens_details.cached_tokens` 三个字段，L3 的度量闭环可直接落地。

### L4 非推理通道收紧 + 按需加载闭环

1. **重建只读取回工具**（补齐"按需加载"）
   - 新增 `get_observation(ref)`：接受 `observation_id` / `candidate_id` / `element_ref`，从 `agent_events` 读取已持久化的观察切片返回**有界**结果。
   - 严格**无状态机**：不写 plan、不推进状态、不做授权门禁（规避 BUG-196 失败模式）。它只是"读已落库的事实"。
   - 价值：`CompactExplorationTranscript` 已会把被取代页面降级为 `reference_only`（保留 id 供再取），但 `query_observation` 在 `d6d3aca` 被整体删除后，**降级数据目前无取回路径**。补上它，压缩才真正无损。
2. **下调 exploration 预算**：`ModelExplorationBudgetBytes` 现为 48KB（`tool_result.go:19`）。有了取回工具后可降至 16–24KB，直接省 24–32KB/轮。
3. **精简 tool 定义**（22KB/轮）：压缩描述文本与 schema，去掉冗余示例。
4. **评估 system prompt**（13KB）：PHASE 2.5 `webPlatformKnowledgePrompt` 六段知识可评估按需注入（仅在相关工具被调用时给对应段落）。

---

## 7. 实施阶段（每步独立可验证）

| 阶段 | 内容 | 验证方式 |
|---|---|---|
| **P0** | **修复配置缺陷（已实测确认，两处阻断）**：① `AI_PLANNING_BASE_URL=https://api.deepseek.com/chat/completions` 被 `openai.go:134` 再追加一次，实测该 URL 返回 **404**；应改为 `https://api.deepseek.com`。② `AI_PLANNING_MODEL=deepseek-v4.1-flash` 无效，实测返回 **400**：`The supported API model names are deepseek-flash, deepseek-v4-pro, but you passed deepseek-v4.1-flash`；应改为 `deepseek-flash`（或 `deepseek-v4-pro`） | 已实测通过：正确路径 + `deepseek-flash` 返回 200 |
| **P1** | **建立基线度量**：在官方端点上记录 reasoning 占比、cache hit 率、每阶段轮次 | 从 `agent_events` 出数（已有 `jq`/psql 路径） |
| **P2** | **L1 轮次最小化**：修 BUG-201 + 门禁错误可行动化 + effort 分档 | Go 单测 + E2E 轮次对比 |
| **P3** | **L3 缓存友好化**：停止轮内改写历史 + 缓存命中断言 | cache hit tokens 显著上升 |
| **P4** | **L2 阶段边界重置**：harness 引入 segment | 断点：A→B 时请求字节应回落至低位 |
| **P5** | **L4 收紧**：`get_observation` + 降预算 + 精简定义/system | 体积下降 + 压缩后仍可取回 |

**顺序理由**：P2/P3 不改变架构、风险低、可立即降本；P4 收益最大但动 harness 结构，应在前两步把度量和门禁噪声清干净之后再动。

---

## 8. 验证标准（可量化）

- [ ] 单次请求 `assistant_reasoning_bytes` 占比：当前 54.5% → 目标「不增长超过阶段上限」（阶段内仍会涨，但阶段重置后回落）。
- [ ] 每次 run 的 LLM 逻辑调用数：round2 17 次 / round3 13 次 → 目标 ≤ 10（消除空转后）。
- [ ] `prompt_cache_hit_tokens` 占比：基线待测 → 目标 > 70%。
- [ ] 900s 内达到 `ready_for_generation` 且 `generate_dsl` 成功。
- [ ] 门禁空转类 `tool.failed`：目标 0（BUG-201 修复后）。
- [ ] 不违反不变量 1（任何带 tools 的请求，历史 assistant 推理完整）。

---

## 9. 风险

1. **P4 阶段重置可能损失探索直觉**：模型在 B 阶段可能"不知道"某些页面事实。缓解：交接导出必须包含每个 grounded 步的 target_binding 与候选观测计数（这些正是 DSL 生成所需）。
2. **缓存命中假设未验证**：若官方端点在多轮 append 下命中率仍低，则 L3 收益落空、整体只能靠 L1/L2 硬压体积。
3. **effort 降到 low 可能损害 grounding 质量**：需 A/B 对比，不可一刀切。
4. **改 harness 引入 segment 属于结构性变更**：必须保证 `agent_events` 审计链与现有 run 语义不破坏（前端 SSE 回放、`research_runs` 关联）。
5. **`get_observation` 可能被误用成新的影子状态机**：必须用测试锁死"只读、不写 plan、不推进状态"。

---

## 10. 待确认问题（需用户决策或实测）

1. P0 的 base URL / 模型名修正是否现在执行？（这是跑通任何 E2E 的前提）
2. 是否接受 P4 的「run 内多 segment」结构性变更，还是先只做 P2/P3 看能推进到哪一步？
3. `reasoning_effort` 是否允许分阶段分档（当前固定 `high`）？
4. 是否保留 `CompactExplorationTranscript` 的轮内压缩（体积优先）还是改为缓存优先（成本/延迟优先）？两者不可兼得，需要取舍。
