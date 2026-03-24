# AI visual 灰度验收结论（2026-03-24）

## 结论

- 当前结论：**不进入默认开启评估，继续保持 `ENABLE_AI_VISUAL_LOCATE=false`。**
- 原因：虽然本地受控窗口下 3 条固定浏览器主回归全部通过，但本轮没有积累到手动开启 AI visual 所要求的有效样本量，当前 `ai_visual_stats` 仍为零样本。

## 本轮执行

- 执行日期：2026-03-24
- 环境：本地受控环境
- 浏览器级固定主回归：
  - `test_local_single_case_smoke_executes_successfully`
  - `test_local_intervention_flow_rerun_hits_tier_zero`
  - `test_suite_context_rerun_failed_reuses_context_snapshot_after_manual_correction`
- 结果：3/3 通过

## ai_visual_stats 快照

本轮本地检查结果：

```text
locate_requests=0
locate_success_count=0
locate_failure_count=0
cache_hit_count=0
cache_miss_count=0
cache_invalidated_count=0
breaker_skip_count=0
rate_limited_skip_count=0
disabled_skip_count=0
avg_locate_latency_ms=0.0
max_locate_latency_ms=0.0
```

这说明本轮只能证明“默认关闭前提下，主回归未被 AI visual 相关改动破坏”，不能证明“开启 AI visual 后具备足够收益”。

## 判定

- 已满足：
  - 3 条固定浏览器主回归全部通过
  - 默认关闭策略未被修改
- 未满足：
  - 手动开启 AI visual 后累计 `>= 30 locate_requests`
  - 或连续 3 天观察记录

## 后续要求

- 继续按 [`docs/ai-visual-gray-acceptance-baseline.md`](./ai-visual-gray-acceptance-baseline.md) 采样。
- 在手动开启 AI visual 的受控窗口内，补足以下任一条件后，再重新做默认开启评估：
  - 单窗口累计不少于 30 次 `locate_requests`
  - 或保留连续 3 天观测记录
- 在那之前：
  - 不新增 rollout 开关
  - 不持久化 runtime 统计
  - 不修改默认关闭策略
