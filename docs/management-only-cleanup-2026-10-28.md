# 旧兼容代码清理

状态：清理完成，独立审查和本地验收通过；源码与报告随本次清理提交保存。
清理基线为 `c7772f5`，验证日期为 2026-10-29。

目标架构只有一条生产路径：

`Langfuse UI/BFF → 已认证 Go Control API → 项目 Registry 快照 → 带租约和 fencing 的 SDK Host / Browser Worker`

本次删除旧产品入口和执行回退。保留 Pi、Claude Code、Codex 官方
SDK、当前 native/flat provider 适配、确定性测试和 `pi.*` 事件协议。
保留已应用数据库迁移、历史 Run、浏览器 profile、artifact 和历史验收记录。

## 清理范围

| 范围 | 目标 | 当前状态 |
| --- | --- | --- |
| Go 控制层 | 强制认证与项目绑定，删除全局可变 Harness、旧提交/response 和无租约执行路径 | 独立审查通过；真实 Kafka fixture 补测完成 |
| SDK Host | 仅共享会话协议，显式 Runtime 和不可变 Harness 快照，删除默认 Pi、启动模块与磁盘内容回退 | 独立审查通过；测试清理修复及 36 项相关回归通过 |
| Browser Worker | HTTP 会话与 replay 必须携带完整管理身份和内部认证 | 150 项全量通过；独立审查通过 |
| 产品与部署 | 删除旧 `web/`、Compose/v3 编排，提取管理验收公共辅助代码并更新原生启动 | 两轮修复及独立复审通过 |

## 新验证

| 检查 | 结果 |
| --- | --- |
| Langfuse 管理前端，Node 24.18.0 | 87 项通过 |
| Langfuse 管理 BFF，Node 24.18.0 | 86 项通过 |
| Langfuse 类型检查，Node 24.18.0 | 通过 |
| Go 全量 race、隔离 PostgreSQL | 最终集成通过；六类专用测试库均已安全移除 |
| 真实 Kafka 分区交接与容量释放 | 四次 race 通过；私有测试 topic 已删除 |
| SDK Host 全量、类型检查、构建 | 最终集成 500 项通过、2 个 opt-in 默认跳过；类型检查和构建通过 |
| SDK Host 真实共享进程及 Chromium 密码回归 | 两项单独启用，均通过 |
| Worker 全量 | 最终集成 150 项通过、无跳过 |
| 原生启动/管理/故障辅助与部署契约 | 最后修复后 Python 85、Node 11、部署契约 4 项通过 |
| 更新后的三 SDK 浏览器执行与只读 UI | Pi、Claude Code、Codex 全部通过；只读 UI 3 项及 3 张截图通过 |
| 独立分任务审查与最终集成审查 | 四个分任务、整体审查后的修复与限定复审均通过；无未关闭问题 |

Langfuse 上游现有 `automaticVercelMonitors` 弃用警告仍存在；本次没有修改
Langfuse sibling 的源码或推送上游。新日志单独保存，未覆盖旧验收证据。

重启前只读取本地验收栈：已验证原 supervisor PID `22046`，未结束 Run、
活动租约、打开控制会话均为零；已有一个 `needs_review` 隔离环境。
这只证明本地部署已静止，不代表外部部署的旧消息已清空。

部署审查发现新验收 helper 的 usage 使用了物理而非逻辑 Span ID、
SSE 原始存储比较未考虑公开脱敏，以及心跳流缺少总超时。第一轮修复
及独立复审确认这三项、outbox 正向存在断言和 libpq 转义解析均已修正。
复审新发现从密码字段推断的短字符串可能撞上 SSE 协议文本，例如
`data` 被误判为 `data:` 的泄漏。第二轮修复已通过独立复审，保留
递归秘密字段拒绝与显式 canary 的原始行/解码事件检查。
旧 `web` 中四份历史测试证据已经移至保留归档，主 agent 核对的
SHA-256 与迁移前一致；十九份旧 v3 忽略文件也保留在归档中。
源码审计已确认旧路径不存在、117 份基线迁移/历史/Langfuse 证据文件
字节一致，`git diff --check` 通过。当前删除 86 个已跟踪文件，
最终已跟踪 diff 为新增 2839 行、删除 28032 行；
新源文件、计划/报告及截图另计。删除分布为旧 `web` 45、integration 27、
backend 9、Host 2、根目录 Compose/runner 3 个文件。
重启前已记录原有 profile、artifact、私有配置和 key 的 146 份文件指纹，
验收和最终重启后两次比较均为零差异。

## 实施取舍

以下记录实施时自主决定的事项和判断错误的代价：

- 在已有 `v3` 功能 checkout 内清理；错误时可从基线恢复或迁移改动。
- 保留已应用迁移和历史证据；代价是历史 schema 仍占空间，但不恢复旧执行入口。
- 保留当前 SDK/provider 适配、确定性 fixture 和 `pi.*` 协议；以后退役需另行迁移。
- 用户已选择清理，继续进行可逆源码修改；范围判断错误时可撤销源码删除。
- 实施 agent 不提交或操作运行中的服务，由主 agent 统一集成；代价是不形成逐任务独立提交。
- 删除旧公开 continuation/response 入口；未来 wait/resume 功能需要设计明确的租约准入。
- 保留管理请求的内部哈希序列化；代价是旧字段名称仅作为内部历史来源继续存在。
- 根目录 v3 测试编排和旧审计代理与其旧调用方一并退役；以后审计工具需实现经过审查的管理认证路径。
- 保留当前已认证、按项目限定的 `/artifacts` 地址；以后重命名需协调 BFF 与验收调用方。
- SDK Host 任务同时迁移 Go 的真实 Pi 并发/清理测试，因为它仍使用待删除的启动模块和无快照请求；错误时需调整测试侧可信组合，不恢复生产回退。
- 在部署任务的只读审查期间执行互不重叠的隔离集成检查；若审查导致修改，代价是重跑覆盖改动的检查。
- 保留此次忽略的计划工作区，其中含历史 web 证据、v3 忽略文件和旧目录元数据的唯一归档副本；代价是本地归档和审查快照继续占用磁盘，之后可明确迁移。

## 部署与源码状态

仅向已验证的原 supervisor 发出 SIGTERM，确认其应用端口关闭后启动
当前源码，三 SDK 验收时 supervisor 为 `57215`、Runtime 为 `60418`。
健康检查记录了当前源文件及 Host dist 的 SHA-256，并确认项目绑定、
Runtime、relay/exporter 心跳和原有隔离环境。第一次检查早于 Runtime
登记，单独保留失败记录；有界等待的新检查通过。
最终配置修复及复审后再次安全重启，当前 supervisor 为 `77335`、
Runtime 为 `80707`。检查实际新 Runtime 的进程命令和专属登记 ID、
项目绑定与 heartbeat 均通过；早期尚在存活窗口的旧登记未作为最终
Runtime 身份依据。最终源文件/Host dist hash 已另存。
UI 首次使用的系统 Python 缺少 Playwright，尚未操作浏览器即失败；
使用现有 Worker venv 后通过。两次记录分别保留，未安装新依赖。

UI 证据：[只读检查报告](../integration/langfuse-management/live-cleanup-evidence-retry1/report.json)。
验收栈为 API `8180`、Worker `8110`、Kafka `9192`、Redis DB `4`、
Langfuse `3300`（保留 build `.next-management-20261028-05`）。
这不证明外部部署或容器实际运行；Kafka 的四个当前 topic 均只读核对为
八分区，未对共享服务重置或注入故障。

三种 SDK 都完成真实 fixture 登录，执行中观测到已完成的 grounded
browser tool；实时和终态公开 trace 均没有唯一密码 canary。
实际已认证 SSE 全量及 Last-Event-ID 重连与公开投影一致。
PostgreSQL 持久 span/attempt/usage/operation 数量与规范 trace 对齐，
每 Run 恰有一个 `run.completed`，outbox 已发布且无待发布行。
三个 Run 的实际 Langfuse 终态 trace、观察与 generation usage 也已校验。

| SDK | SSE 事件 | Span / Attempt / Usage / Operation | Langfuse observation |
| --- | ---: | --- | ---: |
| Pi | 33 | 18 / 13 / 6 / 6 | 18 |
| Claude Code | 38 | 28 / 23 / 1 / 6 | 28 |
| Codex | 23 | 13 / 8 / 1 / 6 | 13 |

只删除本轮创建的 `conv_53b7850b50b6d8081cc51316e0c2b969`；
其删除后 message/environment 数量均为零，tracked cleanup 无未完成项。
验收后再次确认未结束 Run/活动租约/打开控制会话均为零，原有
`needs_review` 数量仍为一；146 份原文件 hash、内容和权限全部一致。

本地私有新证据保存于
`~/Library/Caches/agenttrace-management-acceptance-20261028/`：
`management-cleanup-{host,chromium,worker-native}-final1.json`、
`management-cleanup-native-finalfix.json`、`management-cleanup-postgres-checks.json`、
`management-cleanup-health-final-runtime.json`、`management-cleanup-version-smoke.json`、
`management-cleanup-source-audit-final4.json`、
`management-cleanup-final-config-injected.json` 和
`management-cleanup-state-finalfix-after.json`。较早的失败和通过记录一并保留。
这些文件含部署标识，保持私有权限；没有把私有环境文件内容加入仓库。

覆盖限制：移除旧 scenario 后并未宣称其所有历史断言都等价迁移，
例如详细 inbox/message/command 对齐、Redis 排序/TTL、旧固定模型的
timing、负向/取消 SSE-export 矩阵及通用 Harness composition。
现有 fixture、管理成功/拒绝/幂等性、租约/control/fault 覆盖及本轮真实
三 SDK 登录均有新证据。数据库中未知的 per-call input/timing 和
query usage 保持未知，没有拼造 generation 数据。

最终整体审查未发现 Critical 问题；发现一项 Important：新共享 dotenv
解析器用于启动后，会剥掉未加外层引号的 JSON 内部引号，影响非空
Codex 请求头配置。同时发现四项 Minor：Go 测试可读性、child-exit
覆盖表述、此次只读 UI
继承的历史覆盖说明，以及 contract 注释中的旧 `web` 路径。
已有 Worker 慢任务诊断未显示本次引入的缺陷，保留其性能证据限制。
上述五项已完成修复：未外引号 JSON 内部字节得以保留；新增七项
helper/launcher 反例，原生 Python 85 与 Node 11 通过；24 项相关 Go
API 测试、Pi fixture 编译及 Go literal/tag AST 等价检查通过。
最终限定复审确认五项全部修正，没有新增问题。原有 Pi witness 只证明
场景结束前的 child exit；报告已明确它不证明每次 release 前的退出顺序。
保留九项既有 Worker 慢任务诊断，不据此宣称延迟或吞吐已经验证。

私有配置检查仅输出数量/布尔值：直接文件读取修正了一项原有绑定
JSON（旧解析非法、新解析有效）；按实际启动 wrapper 注入环境后，
168 个有效配置键与修复前一致，绑定 JSON 有效，未改写私有文件。
只读 UI 报告仅修正继承的覆盖说明，原始报告已按 hash/权限归档；
三个实际检查、GET 记录、截图、checkpoint 和此前失败记录均保留。
最后配置改动只影响解析器，实际启动环境一致、SDK 执行代码未变；
因此三 SDK 验收沿用已记录结果，额外完成最终启动与当前 Runtime
健康验证。最终静止状态为未结束 Run/活动租约/打开控制会话均零，
原有 `needs_review` 一；146 份原文件最终比较零差异。
当前没有删除原有数据库、私有状态、profile 或历史 artifact。
仅清理本次专门创建、已确认所有连接关闭的隔离测试数据库。
本次清理提交基于 `c7772f5`，分支为 `v3`。提交包含源码、回归测试、
计划、报告和本轮 UI 证据；私有状态与忽略的历史归档保留在本地。
