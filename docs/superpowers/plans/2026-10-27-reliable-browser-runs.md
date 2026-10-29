# 可靠浏览器任务执行与受控并发 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans. Implement tasks in order and track steps with checkboxes.

**Goal:** 补齐 Redis 实时通知并验证当前 Pi 路径的幂等、取消、消费恢复和受控并发。

**Architecture:** 使用现有 PG Outbox、Kafka、Inbox 和并发限制。API 内新增独立 Live Notifier；Runtime 增加消费监督恢复与取消前置判断。Redis 仍是可丢弃通知与心跳登记，PG 保存事实。

**Tech Stack:** Go、pgx/PostgreSQL、franz-go/Kafka、Redis、Pi subprocess、Python/Playwright、Langfuse BFF。

**Spec:** `docs/superpowers/specs/2026-10-27-thin-langfuse-browser-platform-design.md`

**Dependency:** A 的持久事件合同与 B 的产品页面/代理已完成。

## Global Constraints

- 起始执行上限为 Compose 现有 `4`，command topic 保持 `8` 个分区。
- Redis StreamTTL 24 小时、MaxStreamLength 2000、LeaseTTL 30 秒继续保留。
- 不把 Redis 租约用于分布式锁、任务接管或最终状态裁决。
- SSE ID 只用 PG 全局 run seq，不能用 Harness source_sequence 或 Kafka offset。
- 业务失败与基础设施投递失败分开；终态 Run 的命令重投不启动进程。
- 不重构 Inbox 长事务；容量扩大和 fencing 另设后续项目。
- 中断后的新 Attempt 不等于 Pi 会话续跑，不保证外部浏览器副作用 exactly-once。

## Task C1：独立 Kafka → Redis 通知及可靠 SSE

**Files**

- Create: `backend/internal/live/notifier.go`
- Create: `backend/internal/live/notifier_test.go`
- Create: `backend/internal/live/postgres_reader.go`
- Create: `backend/internal/live/postgres_reader_integration_test.go`
- Modify: `backend/cmd/agenttrace-api/main.go`
- Modify: `backend/internal/api/v3/events.go`
- Modify: `backend/internal/api/v3/server_test.go`
- Modify: `backend/internal/stream/service_test.go`
- Modify: `docs/operations.md`

**Interfaces**

```go
type DurableNotification struct {
    RunID string
    Sequence int64
    Type string
    Data json.RawMessage
}

type NotificationReader interface {
    ByEventID(context.Context, string, string) (DurableNotification, bool, error)
}
```

`Notifier.Handle(context.Context, eventbus.Message) error` 消费 `agenttrace.run.events.v1`；consumer group 固定为 `agenttrace-live-notifier`。它与 Runtime/Exporter 分组不同。

- [ ] **Step 1：先写具体通知/游标回归。**
  - source_sequence 2 对应 PG seq 17，发到 Redis 的 Sequence 必须为 17。
  - Redis Append 失败仍确认这条可丢弃通知。
  - 数据库临时失败返回错误，由 notifier 消费监督重试。
  - 已删除事件忽略，不重新创建 Run。
  - Last-Event-ID 非整数、负数在写 SSE headers 之前返回 400。
  - Redis 重复通知、旧通知不导致持久事件重复展示。
  - SSE 对公开 JSON 副本脱敏，与 Trace/evaluation 保持一致。

- [ ] **Step 2：运行 live/stream/API 新回归，验证当前没有通知实现或游标提前验证。**

```bash
cd backend
go test ./internal/live ./internal/stream ./internal/api/v3 -count=1
```

- [ ] **Step 3：实现身份查询和小型 Handler。**
  - 从 Kafka envelope 取 aggregate_id 和 event_id。
  - 回查已落库事件，以下查询的 seq 才是通知序号。

```sql
SELECT seq, event_type, payload
FROM run_events
WHERE run_id = $1 AND payload->>'event_id' = $2
ORDER BY seq
LIMIT 1;
```

```go
notifyCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
defer cancel()
err := notifier.Store.Append(notifyCtx, record.RunID, live.LiveEvent{
    Sequence: record.Sequence,
    Type: record.Type,
    Data: record.Data,
})
if err != nil { notifier.Log(err) }
return nil
```

Notifier 的 Store 使用既有 LiveStore；Log 是注入的 `func(error)`，默认由 main 提供。Envelope 格式非法时记录并确认；PG 错误才返回重试错误。无需给 live 通知新增 Inbox 表。

- [ ] **Step 4：接入 API 生命周期与重连。**
  - Redis 开启时启动后台 notifier；进程 shutdown 时取消并等待 goroutine。
  - Consume 返回临时错误时，在可取消的 1 秒 timer 后重建消费者，不使 HTTP API 退出。
  - 直接使用现有 EventBus，API outbox relay 保持独立。
  - HTTP SSE 在开始响应前验证游标；每条 Data 复用 `redactTraceJSON`。
  - 保留既有 PG 补读，不实现第二套可靠事件存储。

- [ ] **Step 5：验证 Redis 停机、恢复、SSE 断开和重连。**
  - 完成状态在 Redis 停机期间仍可从 PG/SSE 读取。
  - notifier 重复投递时按持久事件 ID 验证展示唯一。
  - Langfuse 代理收到的持久 ID 与 Go 输出完全一致。

- [ ] **Step 6：提交此 Task。** 建议：`feat: bridge durable run notifications to Redis`。

## Task C2：排队取消、消费监督恢复与资源释放

**Files**

- Modify: `backend/internal/runtime/pi_runner.go`, `pi_runner_test.go`
- Modify: `backend/cmd/agenttrace-runtime/main.go`, `main_test.go`
- Modify: `backend/internal/runtime/consumer_test.go`
- Modify: `worker/loop_worker/sessions.py`
- Modify: `worker/loop_worker/main.py`
- Modify: `worker/tests/test_api.py`
- Modify: `integration/v3/failure_drills.py`
- Modify: `integration/v3/test_runtime_restart.py`, `test_redis_outage.py`, `test_duplicate_delivery.py`

**Interfaces**

Consumes: StartRun 的 HarnessAttemptRef.CancelRequested、现有 terminalRunError、现有 CancellationRequested/CompleteHarnessRun。<br>
Produces: 取消请求前置短路；`runConsumersWithRetry(ctx, consume, onError) error`；Worker health 的 `active_authoring_sessions` 诊断字段。

- [ ] **Step 1：写不启动进程和错误分类回归。**
  - StartRun 返回 CancelRequested=true：Process.Run 调用数为 0，CompleteHarnessRun 写 cancelled。
  - 已终态 Run 重投：无新 Attempt/Span/操作结果。
  - failed browser case 已持久化：Kafka 命令确认，不递增基础设施重试。
  - 短暂 Kafka 错误后消费者重建；ctx 取消立即结束重试。
  - 取消与完成竞争保留现有 PG 行锁回归，不换成 Redis 判断。

- [ ] **Step 2：跑到具体失败，然后添加启动前短路。**

```go
if ref.CancelRequested {
    status, err := r.Store.CompleteHarnessRun(ctx, ref, PiRunOutcome{
        Status: domain.RunCancelled,
        Error: "run cancelled before harness start",
    })
    if err != nil { return err }
    return &terminalRunError{
        cause: fmt.Errorf("run terminated with durable status %s", status),
    }
}
```

放在 StartRun 成功后、构建和启动 Harness 之前。queued 取消可先进入 cancelling，消费时完成 cancelled，但必须不启动 Pi。并发 Run 原有槽位机制不重写。

- [ ] **Step 3：Runtime 外层新增可取消监督循环。**
  - 每轮运行已有 consumeWorkers。
  - 非 context 错误通过 onError 记录，等待 1 秒后重建 consumer group 连接。
  - RetryStore 的计数继续跨连接保留；达到已有阈值交给 DLQ。
  - consumeWorkers 本轮的 goroutine 全部退出后才启动下一轮，避免叠加执行槽位。
  - 不对 terminalRunError 增加重新执行。

```go
timer := time.NewTimer(time.Second)
select {
case <-ctx.Done():
    timer.Stop()
    return ctx.Err()
case <-timer.C:
}
```

`runConsumersWithRetry` 的 consume 参数为 `func(context.Context) error`，onError 为 `func(error)`。测试注入一次临时失败、随后正常消费的函数，避免依赖不可控固定 sleep。

- [ ] **Step 4：新增 Worker authoring session 计数并验证取消释放。**

```python
def active_authoring_sessions(self) -> int:
    return sum(1 for session in self._sessions.values() if not session.closed)
```

health 只增加该诊断计数，不暴露页面内容。场景对比本测试开始与取消完成后的基线，不要求清空其他用户的会话。dry-run Context 的 finally-close 使用既有 Worker 测试验证。

- [ ] **Step 5：修正故障脚本里的旧路径假设。**
  - 当前 Pi 一次 Run 会有多个工具结果，不再断言 operation_results 恰好为 1。
  - 已完成命令重投前后保存 Attempt/Span/operation 的 ID 集合与摘要，要求完全不变。
  - 中途进程 kill 的记录分为旧 Attempt 和恢复后的新 Attempt，旧证据必须保留；不把新 Attempt 宣称为无副作用的原会话恢复。
  - 故障演练只运行本地、可丢弃 fixture，必须在 finally 恢复服务。

```bash
cd backend
go test ./internal/runtime ./cmd/agenttrace-runtime -count=1
cd ../worker
uv run python -m unittest tests.test_api
```

- [ ] **Step 6：提交此 Task。** 建议：`fix: recover runtime consumers and skip cancelled harness starts`。

## Task C3：当前 Pi 路径容量、完整演示与验收文档

**Files**

- Create: `integration/v3/pi_capacity_scenario.py`
- Create: `integration/v3/test_pi_capacity_scenario.py`
- Modify: `integration/v3/agent_repair_scenario.py`
- Modify: `integration/v3/thin_langfuse_scenario.py`
- Modify: `run_v3_tests.py`, `run_v3_tests_test.py`
- Modify: `docs/operations.md`
- Update at implementation completion: `TODO.md` 中对应 Redis/Exporter 项

**Interfaces**

Consumes: A/B/C1/C2 完整系统。<br>
Produces: 当前版本、模型类型、配置和实际测量结果明确的容量/故障报告。

- [ ] **Step 1：先写场景统计回归。**
  - 创建时间、实际 started_at、finished_at 分开，queue_wait=started-created。
  - 只统计确有 Harness Attempt 的 Run，不能用 HTTP 并发数代替执行并发。
  - 从开始/结束区间计算 peak_active；重投前后比较持久 ID 集合。
  - 若任务失败/超时则报告失败，不能只统计提交成功。

```python
def peak_active(intervals: list[tuple[float, float]]) -> int:
    points = [(start, 1) for start, _ in intervals]
    points += [(end, -1) for _, end in intervals]
    active = peak = 0
    for _, delta in sorted(points, key=lambda point: (point[0], point[1])):
        active += delta
        peak = max(peak, active)
    return peak
```

相同时间先处理结束，区间按半开解释。时间值来自数据库事实，不用等待轮询结果推测。

- [ ] **Step 2：实现受控容量场景。**
  - 配置 4 个 Runtime 槽位、8 个分区，批量提交 12 个独立幂等键的任务。
  - 等待所有 Run 终态并验证 Attempt 独立、成功任务存在完整 Case/执行证据。
  - 硬验收 peak_active <= 4；期望能占用多个槽位，观察不到并发时记录为容量验证不足。
  - 报告中记录输入任务数、success/failed/cancelled/timeout、queue wait、duration、peak_active、PG 连接数、Worker authoring session 峰值。
  - PG 长事务占用与 Worker 共享 Chromium 是已知容量边界，不用旧 Go Engine 数据填充。

- [ ] **Step 3：把故障和用户操作串成完整演示。**
  1. 从 Langfuse 创建任务，查看工具参数与修复。
  2. finish_case 在新 Context 中通过后，查看 Case 条件与截图。
  3. 同键重复提交返回同 Run，同键改内容返回冲突。
  4. 对运行中和排队中的任务分别取消。
  5. Redis 停机时仍读到持久终态；恢复后 SSE 不重复展示。
  6. 重投已完成命令，所有持久执行身份保持不变。
  7. Langfuse 导出故障不改变 Run；恢复后按稳定 Trace ID 检查同步。

- [ ] **Step 4：运行必要验收，记录实际结果。**

```bash
cd backend
go test -p 1 ./... -count=1
cd ../pi-browser-harness
npm test -- --run
npm run typecheck
npm run build
cd ../worker
uv run python -m unittest discover -s tests -t .
cd ../web
npm test -- --run
npm run build
```

Go 数据库集成测试必须使用隔离测试库。服务健康后在根目录执行：

```bash
uv run --project worker --with 'psycopg[binary]>=3.2,<4' python integration/v3/pi_harness_scenario.py
uv run --project worker --with 'psycopg[binary]>=3.2,<4' python integration/v3/agent_repair_scenario.py
uv run --project worker --with 'psycopg[binary]>=3.2,<4' python integration/v3/thin_langfuse_scenario.py
uv run --project worker --with 'psycopg[binary]>=3.2,<4' python integration/v3/pi_capacity_scenario.py
```

Langfuse UI E2E 使用 B3 的命令。scripted 容量结果与真实模型结果分开，外部 provider 未配置时明确标为未运行。

- [ ] **Step 5：输出实际报告并更新操作说明。**
  - 最终报告放 `artifacts/load-test/pi-capacity-report.json`；仅在此 Task 实际运行后生成。
  - 中间日志、调试文件和缓存放 `/tmp`，不把示例数据作为实测报告提交。
  - docs/operations.md 写明两个仓库启动方式、固定项目配置、三个进程组、恢复与数据保护边界。
  - 对应场景全部通过后才勾选 TODO.md；失败项保持未完成并写明证据。

- [ ] **Step 6：分别提交最后验收与文档。** 建议：`test: verify Pi browser workspace reliability and bounded capacity`。

## 阶段 C 完成标准

以新版 Pi 路径证明产品操作、持久证据、实时降级、幂等、取消与受控容量。最终宣传范围为“可靠任务执行与受控并发”，更大规模并发和多实例接管需要后续容量与所有权设计。
