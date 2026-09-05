# AI Web Testing → Adaptive Web Agent Research Roadmap

> 目标：在现有 AI_Web_Testing 工程基础上，把系统从“LLM + Web 自动化工具链”升级为一个可实验、可度量、可扩展的 **Agentic Web Testing Research Platform**。
>
> 核心约束：基础 LLM 冻结，不依赖 fine-tuning。研究重点放在 **observation、action abstraction、execution verification、feedback-driven recovery、adaptive controller**。

---

## 1. 研究定位

现有系统已经具备：

```text
User Task
   ↓
Web Observation
   ↓
Candidate / Locator
   ↓
LLM
   ↓
DSL
   ↓
Playwright
   ↓
Verification / Evidence
   ↓
Failure
   ↓
Fix & Retry
```

真正值得研究的不是继续堆更多 locator heuristic，也不是简单增加一个 VLM，而是回答：

> **冻结 LLM 在复杂结构化 Web 状态下为什么容易失败？如何通过结构化 observation、可执行 action abstraction、运行时验证和反馈驱动恢复，提高长程 Web Agent 的可靠性？**

进一步：

> **Agent 能否根据当前状态和历史反馈，动态选择 observation / grounding / recovery strategy，而不修改基础 LLM？**

建议把研究方向表述为：

**Frozen LLM + Adaptive Web Agent**

或者：

**Feedback-Driven Adaptive Web Agents**

---

# 2. 核心 Research Hypothesis

## H1：直接 Action Generation 不稳定

冻结 LLM 直接从 DOM / A11y Tree 生成 primitive browser actions：

```text
Observation → LLM → click/type/select
```

在复杂页面、候选元素相似、长轨迹任务中容易产生：

- wrong element
- ambiguous grounding
- stale element
- wrong page state
- premature stop
- invalid action
- recovery failure

---

## H2：Action Abstraction 能提升可靠性

将：

```text
Natural language → Browser primitive
```

变成：

```text
Natural language
      ↓
Semantic Candidate
      ↓
Action DSL / Intermediate Representation
      ↓
Playwright
      ↓
Browser
```

可以降低 LLM 的 action space 和执行错误。

---

## H3：Execution Verification 比单纯生成更关键

Agent 不应该只说：

> “我要点击 Add to cart。”

而应该产生：

```text
Intent
Target
Precondition
Action
Postcondition
```

执行后验证：

```text
Action
  ↓
Browser
  ↓
Postcondition
  ↓
Success / Failure
```

---

## H4：失败反馈可以驱动 Agent Strategy Adaptation

如果一次失败：

```text
A11y grounding failed
```

Agent 不应该机械地再次生成同样 DSL，而应该能够：

```text
Diagnose
  ↓
Choose recovery strategy
  ↓
Re-explore / DOM / Vision / Retry / Rollback
```

---

## H5：在冻结 LLM 条件下，优化 Controller 仍然有意义

不更新：

```text
LLM parameters
```

而优化：

```text
Agent Controller
Observation Strategy
Grounding Strategy
Recovery Policy
Action Abstraction
```

形式化为：

\[
\theta_{LLM} = frozen
\]

\[
\max_{\theta_C} E[R]
\]

其中 \(\theta_C\) 是 controller / agent strategy 参数。

---

# 3. 不要一开始直接上 PPO

推荐研究路线：

```text
Static Baseline
    ↓
Heuristic Controller
    ↓
Contextual Bandit
    ↓
Sequential Decision Process
    ↓
RL
```

原因：

如果 action 只是：

> A11y / DOM / Vision 三选一

这是 Contextual Bandit 更自然。

只有当：

```text
当前 action
  ↓
改变下一个 state
  ↓
影响后续决策
```

才真正需要 MDP / sequential RL。

---

# 4. 第一阶段：严格复现和 Ablation

先不做学习。

比较：

### A — Direct LLM

```text
A11y → LLM → browser action
```

### B — Few-shot

```text
A11y + few-shot → LLM → browser action
```

### C — Candidate Filtering

```text
A11y
 ↓
Candidate extraction
 ↓
LLM
```

### D — Candidate + DSL

```text
A11y
 ↓
Candidates
 ↓
DSL
 ↓
Playwright
```

### E — Candidate + DSL + Verification

```text
DSL
 ↓
Precondition
 ↓
Execute
 ↓
Postcondition
```

### F — Adaptive Recovery

```text
Failure
 ↓
Diagnosis
 ↓
Recovery Strategy
 ↓
Retry
```

### G — Multimodal Adaptive

```text
A11y
DOM
Vision
 ↓
Adaptive selection
```

---

# 5. 第一篇研究实验应该测什么

不要只有 success rate。

记录：

```text
Task Success
Grounding Accuracy
Invalid Action Rate
Execution Success
Verification Success
Recovery Rate
Average Steps
Average Retries
LLM Calls
Input Tokens
Output Tokens
Latency
Vision Calls
```

建议核心指标：

\[
SuccessRate
\]

\[
RecoveryRate =
\frac{RecoveredFailures}{Failures}
\]

\[
ActionEfficiency =
\frac{SuccessfulTasks}{Steps}
\]

\[
CostEfficiency =
\frac{SuccessfulTasks}{TokenCost + \lambda VisionCost}
\]

---

# 6. 把 DSL 升级为 Intermediate Action Representation

当前 DSL 不要只被视为 Playwright 的脚本格式。

建议定义：

\[
A_t =
(Intent,
Target,
Preconditions,
Action,
Postconditions)
\]

示例：

```json
{
  "action": "click",
  "target": {
    "role": "button",
    "name": "Add to cart"
  },
  "precondition": {
    "visible": true,
    "enabled": true
  },
  "postcondition": {
    "text_contains": "Added"
  }
}
```

这样 DSL 变成：

**可执行 + 可验证 + 可修复的 action representation。**

---

# 7. 研究级 Agent Architecture

建议最终架构：

```text
                         User Task
                             │
                             ▼
                    ┌────────────────┐
                    │ Agent Controller│
                    └───────┬────────┘
                            │
          ┌─────────────────┼─────────────────┐
          ▼                 ▼                 ▼
       A11y/DOM          Vision           History
          │                 │                 │
          └─────────────────┼─────────────────┘
                            ▼
                   Observation Builder
                            │
                            ▼
                   Candidate Builder
                            │
                            ▼
                      Frozen LLM
                            │
                            ▼
                     Action DSL / IR
                            │
                            ▼
                        Playwright
                            │
                            ▼
                         Browser
                            │
                            ▼
                    Execution Evidence
                            │
                            ▼
                    Postcondition Check
                            │
                   ┌────────┴────────┐
                   ▼                 ▼
                 Success           Failure
                   │                 │
                   ▼                 ▼
                  Done           Diagnosis
                                     │
                                     ▼
                                  Recovery
                                     │
                                     └──────→ Controller
```

核心新增的是：

> **Controller + Trajectory + Feedback + Recovery**

而不是重新设计 Browser Worker。

---

# 8. Controller 应该负责什么

Controller 不回答用户，也不直接生成 selector。

它只负责：

```text
OBSERVE
GROUND
VALIDATE
PLAN
EXECUTE
VERIFY
RE_EXPLORE
RECOVER
USE_DOM
USE_A11Y
USE_VISION
ROLLBACK
STOP
```

也就是说：

> Controller = strategy-level policy

LLM = frozen reasoning / language model

Playwright = deterministic executor

---

# 9. 这里才开始进入你关心的 RL

你的 LLM：

\[
\theta_{LLM} = frozen
\]

你的 Controller：

\[
\pi_\phi
\]

于是：

\[
s_t
\xrightarrow{\pi_\phi}
a_t
\xrightarrow{Environment}s_{t+1}
\]

其中：

### State

不要直接把整棵 DOM 当 state。

建议抽象成：

```json
{
  "task": "...",
  "page_url": "...",
  "page_state": "...",

  "observation_type": "a11y",
  "candidate_count": 8,
  "grounding_confidence": 0.61,

  "last_action": "...",
  "last_result": "element_not_found",

  "step": 7,

  "failure_history": [
    "AMBIGUOUS_ELEMENT"
  ],

  "available_modalities": [
    "a11y",
    "dom",
    "vision"
  ]
}
```

不要把整个 DOM 当作 controller state。

Controller 需要的是：

> **决定策略所需的状态摘要。**

---

# 10. Action 怎么定义

第一版：

```text
USE_A11Y
USE_DOM
USE_VISION
EXPLORE
VALIDATE
GENERATE_DSL
EXECUTE
VERIFY
RE_EXPLORE
RECOVER
ROLLBACK
STOP
```

之后可以细化：

```text
EXPLORE_A11Y(depth=2)
EXPLORE_DOM(scope=form)
USE_VISION(region=...)
VALIDATE(candidates=[...])
RECOVER(strategy=...)
```

注意：

> Controller action 是“策略动作”，不是浏览器 primitive。

---

# 11. Reward

最简单版本：

\[
R =
\begin{cases}
1 & success \\
0 & failure
\end{cases}
\]

研究版本：

\[
R =
\alpha Success
-\beta Steps
-\gamma InvalidActions
-\delta TokenCost
-\eta VisionCost
\]

例如：

```text
success             +10
step                 -0.1
invalid action       -0.5
failed verification -0.5
vision call          -0.2
```

不要一开始设计特别复杂的 reward。

先保证 reward reliable。

---

# 12. Failure Taxonomy

建议统一：

```text
OBSERVATION_FAILURE
GROUNDING_FAILURE
AMBIGUOUS_ELEMENT
WRONG_ELEMENT
STALE_ELEMENT
PLANNING_FAILURE
ACTION_GENERATION_FAILURE
EXECUTION_FAILURE
WRONG_PAGE_STATE
POSTCONDITION_FAILURE
PREMATURE_STOP
LOOP
RECOVERY_FAILURE
```

你的 failure taxonomy 会成为非常重要的 research asset。

最终可以得到：

```text
10000 trajectories
       ↓
Failure distribution
```

例如：

```text
Planning            31%
Grounding           18%
Wrong page state    17%
Recovery            14%
Action generation   12%
Verification         8%
```

这比单纯 benchmark score 更有研究价值。

---

# 13. Recovery 应该从“retry”升级为“diagnosis → strategy selection”

当前：

```text
failure
 ↓
fix_and_retry
```

建议改成：

```text
Failure Signal
      ↓
Failure Classifier
      ↓
Root Cause Hypothesis
      ↓
Recovery Policy
      ↓
New Strategy
      ↓
Execute
      ↓
Verify
```

例如：

```text
ELEMENT_NOT_FOUND
        ↓
Grounding failure?
        ↓
Re-explore A11y
        ↓
still fail?
        ↓
DOM
        ↓
still ambiguous?
        ↓
Vision
        ↓
verify
```

这会比简单 retry “agentic” 很多。

---

# 14. Active Perception

不要让 Vision 永远成为：

> A11y failed → screenshot

而是：

> Agent 主动判断什么时候需要额外信息。

例如：

```text
confidence > 0.85
    → A11y enough

0.60 < confidence < 0.85
    → add DOM context

confidence < 0.60
    → Vision

repeated failure
    → re-explore / recovery
```

研究问题：

> **Can a frozen LLM agent learn when additional visual or structural information is worth acquiring?**

这是比 “DOM vs Vision” 更强的问题。

---

# 15. Candidate Set 应该成为独立研究变量

你现在已经有 candidate selection。

建议实验：

```text
Full DOM
↓
1000 elements

Filter
↓
100 elements

Semantic filter
↓
20 elements

Task-aware filter
↓
5 elements
```

研究：

\[
CandidateCount \rightarrow Success
\]

以及：

\[
CandidateCount \rightarrow TokenCost
\]

可能出现一个 trade-off：

```text
候选太多 → LLM reasoning burden 高
候选太少 → target 被误删
```

因此可以研究：

> **Optimal task-aware candidate budget**

---

# 16. Observation Compression

研究：

```text
Full A11y
vs
Pruned A11y
vs
Task-aware A11y
vs
A11y + DOM
vs
Adaptive observation
```

定义：

\[
O_t = f(Task, PageState, History)
\]

目标：

> 在不损失必要信息的前提下，减少 observation complexity。

这是一个非常适合 frozen LLM 的研究方向。

---

# 17. Contextual Bandit 第一版

定义策略：

```text
A = A11y
B = DOM
C = A11y + DOM
D = Vision
E = A11y + Vision
```

Context：

```text
task type
page type
candidate count
ambiguity
previous failure
grounding confidence
```

Reward：

```text
success / failure
```

先比较：

```text
Fixed A11y
Fixed DOM
Fixed Vision
Rule-based routing
Contextual bandit routing
```

这是非常干净的实验。

---

# 18. Sequential RL 第二版

只有当你需要：

```text
observe
 ↓
act
 ↓
new state
 ↓
observe again
 ↓
new action
```

才进入 MDP。

形式化：

\[
s_t \rightarrow a_t \rightarrow s_{t+1}
\]

例如：

```text
s0:
A11y ambiguous

a0:
USE_DOM

s1:
DOM still ambiguous

a1:
USE_VISION

s2:
candidate confirmed

a2:
EXECUTE

s3:
success
```

这里 RL 才真正有必要。

---

# 19. Engineering 改造原则

不要推倒现有 repo。

当前：

```text
AgentCore
Harness
Tools
Browser Worker
Locators
Runner
Reporter
Failure Signals
Fix & Retry
```

都保留。

新增：

```text
controller/
trajectory/
feedback/
recovery/
```

---

# 20. 推荐的新增目录

```text
backend-go/internal/
├── agent/
├── harness/
├── tools/
├── controller/
│   ├── state.go
│   ├── policy.go
│   ├── decision.go
│   ├── strategy.go
│   └── context.go
│
├── trajectory/
│   ├── trajectory.go
│   ├── transition.go
│   ├── recorder.go
│   └── storage.go
│
├── feedback/
│   ├── reward.go
│   ├── failure.go
│   ├── diagnosis.go
│   └── verifier.go
│
└── recovery/
    ├── strategy.go
    ├── planner.go
    └── rollback.go
```

Browser Worker：

```text
browser-worker/app/
├── ai/
│   ├── observation/
│   │   ├── a11y.py
│   │   ├── dom.py
│   │   └── vision.py
│   │
│   ├── grounding/
│   │   ├── candidate.py
│   │   └── scorer.py
│   │
│   └── verification/
│       └── postcondition.py
```

---

# 21. Trajectory 数据模型

最重要的是不要只存最终报告。

建立：

```text
AgentTrajectory
```

每一步：

```text
trajectory_id
run_id
step_id

state_before

observation
observation_type

candidate_set
candidate_scores

controller_action

llm_input
llm_output

dsl_action

browser_action

execution_result

postcondition_result

failure_type

recovery_strategy

reward
cost
timestamp
```

这实际上直接对应：

\[
(s_t,a_t,r_t,s_{t+1},done)
\]

以后可以直接导出成 research dataset。

---

# 22. Event Model

在现有 AgentRun / ToolCall / Checkpoint / Transcript 基础上增加：

```text
ObservationEvent
DecisionEvent
ActionEvent
ExecutionEvent
VerificationEvent
FailureEvent
RecoveryEvent
RewardEvent
```

示例：

```json
{
  "event": "controller_decision",
  "state_id": "s_087",
  "strategy": "A11Y_PLUS_DOM",
  "confidence": 0.42,
  "reason_code": "AMBIGUOUS_CANDIDATES"
}
```

Reward：

```json
{
  "event": "reward",
  "step": 8,
  "reward": -0.2,
  "success": false,
  "failure_type": "ELEMENT_NOT_FOUND"
}
```

---

# 23. Research Dataset

最终你应该能从生产/benchmark run 自动生成：

```text
trajectory.jsonl
```

例如：

```json
{
  "task": "...",
  "steps": [
    {
      "state": {...},
      "action": "USE_A11Y",
      "reward": 0
    },
    {
      "state": {...},
      "action": "USE_DOM",
      "reward": -0.2
    },
    {
      "state": {...},
      "action": "USE_VISION",
      "reward": 1
    }
  ],
  "success": true
}
```

这个数据未来可以：

```text
offline analysis
bandit training
RL training
failure analysis
policy evaluation
```

---

# 24. 实验矩阵

建议第一轮：

| Axis | Variants |
|---|---|
| Observation | A11y / DOM / Vision / Hybrid |
| Candidate | None / Fixed / Semantic / Task-aware |
| Action | Primitive / Candidate / DSL |
| Verification | None / Postcondition |
| Recovery | None / Retry / Diagnosis-aware |
| Routing | Static / Rule / Bandit |
| Memory | None / Episode / Historical |

不要一次全部组合。

按层逐步 ablation。

---

# 25. 第一阶段论文级实验

先回答三个问题：

### Q1

> Does structured candidate selection improve frozen LLM web agents?

### Q2

> Does executable action abstraction improve reliability?

### Q3

> Does execution feedback enable more effective recovery?

如果三个问题都有明确答案，你已经有一个非常完整的 empirical story。

---

# 26. 第二阶段论文级实验

研究：

> **Adaptive Observation Selection**

比较：

```text
Static A11y
Static Vision
Static Hybrid
Rule-based adaptive
Bandit adaptive
```

指标：

```text
Success
Steps
Token Cost
Vision Calls
Recovery Rate
Latency
```

核心结论可能是：

> **The optimal observation modality is task- and state-dependent.**

---

# 27. 第三阶段论文级实验

研究：

> **Feedback-Driven Recovery**

测试人为注入失败：

```text
wrong locator
stale DOM
ambiguous element
wrong page state
missing modal
```

然后比较：

```text
No Recovery
Naive Retry
LLM Retry
Rule Recovery
Adaptive Recovery
```

核心指标：

\[
RecoveryRate
\]

以及：

\[
AdditionalCostPerRecovery
\]

---

# 28. 最终的 Agentic 版本

最终目标不是：

```text
LLM → tool → tool → tool
```

而是：

```text
Goal
 ↓
Observe
 ↓
Form hypothesis
 ↓
Choose information source
 ↓
Ground candidates
 ↓
Construct action
 ↓
Execute
 ↓
Verify
 ↓
Update belief
 ↓
Recover / continue
 ↓
Observe again
```

这是一个真正的：

> **closed-loop agent**

---

# 29. 研究主线建议

最推荐的主线：

## Frozen LLM + Adaptive Perception + Executable Action Abstraction + Feedback-Driven Recovery

四个 component：

```text
1. Adaptive Perception
2. Candidate Grounding
3. Executable Action DSL
4. Feedback-driven Recovery
```

其中：

```text
LLM = frozen
Controller = adaptive
Browser = environment
DSL = action representation
Verifier = feedback
Reward = task outcome + cost
```

---

# 30. 不建议做的事情

## 不建议 1

为了“RL”直接上 PPO / GRPO。

除非已经证明你的 controller 是真正 sequential decision problem。

---

## 不建议 2

继续无止境堆 locator heuristic。

容易变成：

> benchmark-specific engineering

---

## 不建议 3

简单做 DOM vs VLM。

结果很容易停留在：

```text
A 80%
B 83%
```

缺少更深的 agentic insight。

---

## 不建议 4

只看 final success。

必须看：

```text
trajectory
failure
recovery
cost
```

---

# 31. 推荐的 12 周 SOP

## Week 1–2

整理现有工程：

```text
trajectory logging
failure taxonomy
baseline evaluation
```

目标：

> 可以完整回放一次 AgentRun。

---

## Week 3–4

DSL 升级：

```text
Intent
Target
Precondition
Action
Postcondition
```

目标：

> DSL 成为 executable action representation。

---

## Week 5–6

做 baseline / ablation：

```text
Direct
Candidate
DSL
DSL + Verification
```

目标：

> 找到真正有效的 component。

---

## Week 7–8

加入：

```text
Failure Diagnosis
Recovery Strategy
```

目标：

> Agent 能根据不同 failure 采用不同修复策略。

---

## Week 9

加入：

```text
Observation Routing
```

目标：

> A11y / DOM / Vision 不再只是固定 pipeline。

---

## Week 10

实现：

```text
Contextual Bandit
```

目标：

> 学习选择 observation / recovery strategy。

---

## Week 11–12

如果 sequential state dependency 明确：

```text
MDP
Sequential Policy
RL
```

否则保留 Bandit。

---

# 32. 最终论文结构

## Introduction

问题：

> Frozen LLMs struggle with complex structured Web observations and reliable long-horizon interaction.

---

## Observation

发现：

> Direct grounding is not the only bottleneck; action abstraction, verification and recovery are critical.

---

## Method

提出：

> Adaptive Web Agent with executable action abstraction and feedback-driven controller.

---

## Experiments

```text
Representation
Action abstraction
Verification
Recovery
Adaptive routing
```

---

## Analysis

重点展示：

```text
Where agents fail
Why they fail
How controller changes behavior
When Vision is actually useful
```

---

# 33. 最终研究目标

\[
\boxed{
Frozen\ LLM
+
Adaptive\ Controller
+
Structured\ Observation
+
Executable\ Action\ Abstraction
+
Runtime\ Verification
+
Feedback\ Driven\ Recovery
}
\]

而不是：

\[
\boxed{
Prompt
+
More\ Tools
+
More\ Locators
}
\]

这两者的 research value 差别非常大。

---

# 34. 最终判断

以你现在的工程为基础，最值得做的不是“把 Web Testing 做得更自动化”，而是：

> **把 Web Testing 变成一个研究 Agent 如何在不修改基础模型的情况下，通过环境反馈进行策略适应的实验平台。**

你的 DSL、Playwright executor、A11y locator、VLM fallback、postcondition verifier、failure signal、fix-and-retry 都已经可以作为这个平台的基础设施。

真正缺的是：

```text
State
Trajectory
Controller
Feedback
Reward
Recovery
Evaluation
```

一旦这几个东西统一起来，你就拥有了：

**一个可以研究 Agentic behavior 的 Web environment。**
