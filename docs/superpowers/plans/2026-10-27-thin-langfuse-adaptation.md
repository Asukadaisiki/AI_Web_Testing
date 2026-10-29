# Langfuse 浏览器 Agent 薄适配 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement the linked plans task-by-task. Steps use checkbox syntax for tracking.

**Goal:** 完成可验证浏览器 Agent、Langfuse 产品入口和可靠任务执行三项功能。

**Architecture:** Langfuse 负责会话权限、任务页面与原生观测；Go 管理 Run 与持久证据；Pi 和 Harness 负责 Agent 决策与浏览器验证。新增代码集中在证据读取模型、Langfuse browser-runs feature、实时通知桥接。

**Tech Stack:** Go 1.24、PostgreSQL、Kafka、Redis、Pi SDK 1.4.0、TypeScript、Python/Playwright；Langfuse v3.225.14 / Node.js 24 / pnpm 11.10.0。

**Spec:** `docs/superpowers/specs/2026-10-27-thin-langfuse-browser-platform-design.md`

**Status:** 实现计划已整理；业务代码未开始修改，本文中的新增目录均为目标结构。

## Global Constraints

- 首版只绑定一个配置好的 Langfuse Project、AgentTrace Project、任务 Conversation。
- Go PostgreSQL 是 Run、Case、事件和最终状态的权威来源；不修改 Langfuse Prisma schema。
- Case 与 Harness 协议主版本不变；原始 Case/ExecutionResult 必须来自持久事件。
- 上游 Langfuse tag 固定到 v3.225.14；不基于浮动 main 编写路径。
- 不新增 Pi 内存会话恢复、浏览器池、分布式租约调度、批量评测或自动扩缩容。
- 任何修改都遵循具体回归失败 → 最小实现 → 通过验证的顺序。
- 文档中的测试命令是实施时应运行的命令，不代表本次已经执行。

## 1. 实施顺序

```text
功能一：A1 工具/修复证据 → A2 证据读取模型 → A3 API 与现有工作台验证
功能二：B1 Langfuse 客户端与权限 → B2 原生 Trace 导出 → B3 任务页与证据面板
功能三：C1 Redis/SSE → C2 取消与消费恢复 → C3 新 Pi 路径容量及演示验收
```

功能一可以独立在现有工作台验收；功能二依赖功能一的读取契约；功能三补齐完整产品链路的可靠性。

| 计划 | 新增内容 | 主要复用 |
|---|---|---|
| [功能一：Agent 证据](2026-10-27-agent-browser-evidence.md) | 工具参数、修复标记、evaluation 读取与展示 | CaseBuilder、Replay、FailureLedger、Worker、Run 事件 |
| [功能二：Langfuse 适配](2026-10-27-langfuse-browser-workspace.md) | browser-runs feature、SSE/图片代理、Exporter 关联 | 上游 Trace/权限、既有 Go HTTP 接口 |
| [功能三：可靠执行](2026-10-27-reliable-browser-runs.md) | Live Notifier、消费监督恢复、新 Pi 路径验收 | Outbox、Inbox、Run 状态机、并发上限 |

## 2. AgentTrace 目标代码结构

`+` 表示新增，`~` 表示局部修改，其余为继续复用。

```text
backend/
  cmd/agenttrace-api/main.go                         ~ 接入 Live Notifier 与 bridge token
  cmd/agenttrace-runtime/main.go                     ~ 消费监督恢复
  internal/
    api/v3/evaluation.go                            + 证据只读端点
    api/v3/bridge_auth.go                           + 可选服务间认证
    api/v3/events.go                                ~ SSE 脱敏与游标验证
    api/v3/server.go                                ~ 路由及依赖注入
    evaluation/types.go                             + evaluation.v1 类型
    evaluation/project.go                           + 持久事件 → 证据读取模型
    postgres/evaluation.go                          + 一致性读取 Run/目标/事件
    live/notifier.go                                + Kafka 事件 → Redis 通知
    live/postgres_reader.go                         + 按事件身份读回全局 seq
    export/langfuse/exporter.go                     ~ 完成事件、项目过滤
    export/langfuse/projection.go                    + 用量与模型信息映射
    export/langfuse/client.go                       ~ Generation usage 编码
    runtime/pi_runner.go                           ~ 取消后不启动子进程
    config/config.go                               ~ bridge / export project 配置
    piharness/protocol.go                          ~ 可选工具参数校验
pi-browser-harness/
  src/trace-events.ts                              ~ 保留工具参数
  src/run-protocol.ts                              ~ 可选 arguments
  src/browser/observation-view.ts                  ~ 保留观测身份
  src/browser/case-builder.ts                      ~ 实际恢复标记
  src/browser/replay.ts                            ~ 失败尾部移除标记
  src/mcp/tool-result.ts                           ~ 传递 repair
worker/
  loop_worker/sessions.py                          ~ authoring session 诊断计数
  loop_worker/main.py                              ~ 健康诊断字段
  fixtures/site/repair.html                        + 可重复的修复 fixture
web/
  src/api.ts                                      ~ evaluation 客户端
  src/features/trace/BrowserEvaluation.tsx          + 阶段一证据展示
  src/features/trace/TracePane.tsx                  ~ 加载扩展面板
fixtures/trace/browser-evaluation-v1.json            + 跨端合同 fixture
integration/v3/
  agent_repair_scenario.py                          + 修复与证据验收
  thin_langfuse_scenario.py                         + 项目桥接验收
  pi_capacity_scenario.py                          + 当前 Pi 受控容量验收
  failure_drills.py                               ~ 修正旧单 operation 假设
compose.langfuse-bridge.yaml                        + 跨 Compose 网络与服务端配置
docs/operations.md                                ~ 配置、恢复和演示说明
```

## 3. Langfuse fork 目标代码结构

下面路径相对 `/Users/bytedance/project/langfuse-agenttrace`，不是现有 AgentTrace `web/`。

```text
web/src/
  features/browser-runs/                            + 全部定制逻辑集中于此
    contracts.ts
    server/config.ts
    server/agenttrace-client.ts
    server/router.ts
    components/NewRunDialog.tsx
    components/RunActions.tsx
    components/RunList.tsx
    components/BrowserEvidencePanel.tsx
    components/AgentTracePanel.tsx
    hooks/useRunEvents.ts
    pages/BrowserRunsPage.tsx
  pages/project/[projectId]/browser-runs/index.tsx    + 任务页面入口
  pages/api/agenttrace/events/[runId].ts              + 授权 SSE 代理
  pages/api/agenttrace/artifacts/[runId].ts           + 授权图片代理
  components/trace/TracePage.tsx                     ~ 插入一个扩展组件
  server/api/root.ts                               ~ 注册 browserRuns router
  env.mjs                                          ~ 服务端配置字段
docker-compose.agenttrace.yml                       + 自定义镜像与 bridge 网络
```

不复制 Langfuse 的模型 Trace、用量仪表盘或权限实现。入口页使用其现有 Page 和 UI 组件；只扩展浏览器任务业务。

Langfuse 测试遵循固定 tag 的命名：server-unit 放 `web/src/__tests__/server/unit/browser-runs/*.servertest.ts`，组件/Hook 测试使用 `.clienttest.ts(x)`。不修改上游 Vitest 配置来迁就测试位置。

## 4. 每阶段完成标准

- [ ] A：现有工作台能展示同一次真实执行中的工具参数、失败、恢复和完整重放；历史证据缺失明确标注。
- [ ] B：Langfuse 内可创建/取消/重跑任务，并从原生 Trace 跳转到可信的 Case/截图；权限与项目边界有效。
- [ ] C：Redis 停机、重复提交、已完成命令重投、排队取消、运行中取消、消费连接失败均有当前 Pi 路径证据；12 个任务在 4 个槽位下完成受控容量验收。

## 5. 建议本地提交边界

每个 A/B/C Task 独立提交。AgentTrace 改动提交在当前仓库，Langfuse 改动提交在 fork checkout。阶段一通过后才进入阶段二；阶段三通过后才将演示标为完整。

## 6. 本次计划审查

- 已核对代码路径、现有 Go API、Pi 事件、Case 产物来源和 Langfuse tag 页面/权限接口。
- 已明确历史数据、项目绑定、SSE seq、外部副作用和新 Attempt 的边界。
- 已区分计划中的测试 fixture、建议容量目标与实际测量结果。
- 业务测试与压测未在这次文档编写中运行。
