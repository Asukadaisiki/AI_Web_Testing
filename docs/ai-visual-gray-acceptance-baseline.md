# AI visual 灰度验收基线

用于回答“AI visual 是否值得进入默认开启评估”，而不是直接推动默认开启。

## 目标

- 在不改变 `ENABLE_AI_VISUAL_LOCATE=false` 默认策略的前提下，建立可重复的观测与验收口径。
- 验证三件事：
  - 重复目标场景是否减少重复调用
  - 命中率是否不回退
  - 延迟是否可控

## 采集方式

- 观测入口：`GET /api/v1/settings/ai/overview` 中的 `ai_visual_stats`
- 观测指标：
  - `locate_requests`
  - `locate_success_count`
  - `locate_failure_count`
  - `cache_hit_count`
  - `cache_miss_count`
  - `cache_invalidated_count`
  - `breaker_skip_count`
  - `rate_limited_skip_count`
  - `disabled_skip_count`
  - `avg_locate_latency_ms`
  - `max_locate_latency_ms`

## 观察窗口

- 灰度环境或本地受控环境中，至少完成 1 个连续观察窗口。
- 每个窗口至少满足以下两个条件：
  - 完整执行 3 条固定浏览器主回归各 1 次
  - 若手动开启 AI visual，则累计不少于 30 次 `locate_requests`
- 如果短期内达不到 30 次请求，至少保留 3 天观测记录后再做结论。

## 通过阈值

- 3 条固定主回归必须全部通过：
  - 单 Case smoke
  - `needs_intervention -> correction -> rerun -> Tier0 hit`
  - Suite Context + 失败重跑
- 开启 AI visual 的观察窗口中，建议满足：
  - 命中率：`locate_success_count / locate_requests >= 0.60`
  - 缓存复用率：`cache_hit_count / (cache_hit_count + cache_miss_count) >= 0.20`
  - 平均延迟：`avg_locate_latency_ms <= 2000`
  - 最大延迟：`max_locate_latency_ms <= 10000`
  - 跳过率：`(breaker_skip_count + rate_limited_skip_count) / locate_requests <= 0.20`

## 不通过判定

- 任一浏览器主回归失败
- 命中率明显低于 60%
- 平均延迟或最大延迟持续超过阈值
- 熔断或限流跳过占比持续偏高
- 开启 AI visual 后，重复目标场景没有体现出 cache 复用收益

## 本轮明确不做

- 不把 runtime 统计持久化到数据库
- 不引入新的 rollout 开关或默认开启策略
- 不新增外部站点依赖；验收仍以本地夹具页和固定回归为准
