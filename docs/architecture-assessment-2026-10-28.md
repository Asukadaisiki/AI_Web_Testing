# 当前项目架构评估

评估日期：2026-10-28

标准：不保留没有当前用途的设计；所有保留组件必须实际接入并产生可验证的价值；组件通过明确契约协作，避免跨层了解具体实现。

范围：当前仓库的生产入口、Compose、Pi/Host/Harness、事件持久化、SSE、Redis 和 Langfuse Exporter。本文是代码与配置审查，未修改业务代码，未执行新的完整 Compose 验收。

## 判断

执行主链路有明确职责，但项目尚未满足“所有组件接入且低耦合”。问题主要是切换后的事件契约没有同步、浏览器业务规则泄漏到通用执行层、无效配置和没有消费方的协调机制仍被保留。

共享宿主、独立 Attempt 会话和显式资源清理值得保留；不需要为这次整改引入新的调度框架、插件市场、浏览器池或更多基础设施。

“全部接入”应落实为联合部署与端到端验收，而不是要求 Redis 或 Langfuse 故障时阻止浏览器执行。执行状态仍只有 PostgreSQL 一个权威来源。

## 组件的存在理由与现状

| 组件 | 应承担的唯一主要职责 | 当前判断 |
| --- | --- | --- |
| Go API | 任务入口、读取执行事实、SSE 和证据访问 | 保留；修正模型选择与配置归属 |
| Go Runtime | 消费命令、管理执行状态、并发、取消与宿主监督 | 保留；减少浏览器规则和无效协调写入 |
| PostgreSQL | Run、Attempt、执行证据、幂等与 Outbox 的权威记录 | 已接入，必须保留 |
| Kafka | 命令积压与可靠投递、不同事件消费者的独立进度 | 已承担命令链路；补齐进度与导出消费者以形成完整事件链路；不据此宣称高并发容量已验证 |
| Redis | 可丢失、可过期的实时通知，缩短 SSE 获取进度的等待 | 部分接入；实际进度写入缺失，租约与取消提示的消费不完整 |
| Session Host | 复用进程、按 Run/Attempt 管理会话及资源生命周期 | 已接入，保留；接受共享进程的故障影响范围 |
| Pi | 模型循环、上下文、工具调用 | SDK 已接入；标准 Compose 的模型选择存在阻断真实模型的配置问题 |
| Harness | 提示词、技能、工具、业务证据与完成规则 | 可替换模块已接入；通用协议和 Go 证据投影尚未完全解耦 |
| MCP | Pi 与工具实现之间的调用与生命周期边界 | 已接入，保留；无需再增加重复的工具网关 |
| Python Worker / Playwright | 实际浏览器操作、Case 重放与执行证据 | 已接入，隔离价值明确；三语言维护成本存在，但不足以据此立即重写 Worker |
| React | 创建任务、取消、展示浏览器执行与 Case 证据 | 已接入，作为任务入口保留 |
| Langfuse / Exporter | 模型与工具 Trace 的独立观察和分析 | Exporter 已实现但完成事件不兼容，且不在默认部署中；目前不能认为联合链路已接齐 |
| Fixture / scripted provider | 无外部凭据的确定性执行链路验收 | 有测试价值；必须明确标识，不能代替真实模型验收 |

## 优先整改的问题

### 1. Langfuse 当前会忽略 Pi Run 的完成事件（高优先级）

- `backend/internal/runtime/pi_store.go:144-151` 产生 `harness.completed`。
- `backend/internal/postgres/harness.go:404-413` 再次固定完成事件类型为 `harness.completed`，载荷包含 status/error。
- `backend/internal/export/langfuse/exporter.go:27-28,167-169` 只处理 `runtime.completed`，其余类型直接返回成功。
- 即使仅替换事件名称，Exporter 的 `decodeEvent()` 还要求完成载荷中的 RunID 等于 aggregate_id，而当前 Harness 完成载荷没有该字段。
- `compose.yaml:137-138` 中 Exporter 使用可选 profile；默认启动也没有 Langfuse 服务。

处理：定义一个应用层 Run 完成契约，生产者和消费者按同一契约处理。需要同时检查事件类型与载荷，不能仅替换字符串。旧类型只留有明确使用方和退出条件的兼容入口。提供实际连接 Langfuse 的联合启动与验收路径。

验收：成功、失败和取消都能触发导出；重复投递不重复创建远端记录；导出失败可重试，并且不改写 Run 状态。

### 2. Redis 没有收到当前执行路径的进度事件（高优先级）

- API 在 `backend/cmd/agenttrace-api/main.go:97-104` 注入了 Redis LiveStore。
- `backend/internal/stream/service.go` 能读取 Redis，也会轮询 PostgreSQL。
- Runtime 把 Pi 事件写入 PostgreSQL，但生产代码没有调用 `LiveStore.Append()` 写入执行进度。
- 当前 API 启动了 Outbox Relay，没有实现设计文档中的 Run 事件 Live Notifier 消费者。

处理：通过现有 Kafka Run 事件流接入一个 Live Notifier，再写 Redis，保留独立消费进度。首先让 Redis 通知唤醒 SSE 读取持久事件；没有明确 token 流需求时，不新增一条临时 token 管道。

验收：真实 Run 会生成 Redis 进度记录；正常通知减少等待；Redis 故障与重连仍从 PostgreSQL 补齐，持久事件不重复展示。

### 3. 标准 Compose 配置下，设置模型密钥仍可能一直运行 scripted provider（高优先级）

- `backend/cmd/agenttrace-api/main.go:141-146` 按 API 进程是否读取到 APIKey 决定 scripted 或真实 provider。
- `compose.yaml` 的 API 服务只传模型名，模型密钥只传给 Runtime。
- API 创建命令时因此选择 scripted；`pi-browser-harness/src/model-provider.ts` 信任 request.model.provider，并不会因为 Runtime 有密钥自动改成真实模型。

处理：用明确的 provider / model / execution mode 描述请求，凭据留在执行侧。API 不应为了决定执行模式而依赖秘密。统一 profile、环境配置和请求快照的优先级，避免配置存在但不生效。

补充：`AGENTTRACE_MODEL_REASONING_EFFORT` 已被配置层读取，但 API/Runtime 仍采用嵌入的 browser profile 默认值；`AGENTTRACE_MODEL_SCRIPT` / ScriptPath 没有当前生产使用方。

验收：标准联合部署中，切换 execution mode 后实际模型请求与声明一致；缺失真实模型凭据明确报错，不能静默以 fixture 结果替代。

### 4. Harness 具备代码替换能力，但协议与证据仍有业务耦合（中优先级）

- `pi-browser-harness/src/run-protocol.ts:18-25,159-164` 要求所有执行请求提供 worker_url 和 artifact_scope；不需要浏览器的 Harness 也必须提供这些字段。
- 该模块直接导入浏览器 Case/ExecutionResult 校验，生命周期协议与浏览器证据契约同文件维护。
- `backend/internal/runtime/pi_store.go:19-22,224-231` 根据浏览器工具名称前缀与 finish_case 生成操作证据。
- `backend/internal/runtime/pi_runner.go:170-196` 新路径消费 Harness verdict，但仍保留浏览器 Case 判定作为兼容回退。

处理：把执行标识、模型引用和生命周期保留在公共契约；浏览器配置与证据使用浏览器扩展契约。Go 的通用持久化流程只处理标准事件，浏览器特有投影放在单独模块。无需引入动态注册框架；固定的通用投影加浏览器投影已足够。

保留协议版本兼容时应写清迁移路径。改掉文件名或 pi.* 事件名本身不会解决耦合，不为命名做大规模重构。

验收：替换 Harness 不需要无意义的浏览器地址；工具名变化不要求修改通用 Runtime；旧浏览器证据仍可读取；业务完成规则仍由所选 Harness 定义。

### 5. Trace 写入失败后仍继续执行浏览器操作（高优先级）

- `backend/internal/runtime/pi_runner.go:67-71` 在事件追加失败时只保存 appendErr，不停止 session。
- 后续动作仍可发生，直到 Process.Run 返回才把持久化错误用于最终判定。
- PostgreSQL 对 source_sequence 要求连续；一次丢失后，后续证据也可能不能提交。

处理：首次持久化失败立即停止该 Attempt，等待清理，阻止更多无法记录的动作。已经发生的外部副作用不能通过取消撤回，恢复策略需要显式对账，不自动重放整个任务。

验收：执行中模拟 PostgreSQL 写入失败，目标 Attempt 不继续发起后续操作，其他健康任务不受该 session 的定向停止影响。

### 6. 没有消费方的 Redis 协调机制增加了复杂度（中优先级）

- `backend/internal/runtime/consumer.go:229-252` 定时 SetLease；当前生产代码没有读取该租约来做所有权判定或故障恢复。
- API 在取消时写 Redis hint，但 `PiRunner.watchCancellation()` 只查询 PostgreSQL，不使用 IsCancelRequested。
- `backend/internal/runtime/pi_runner.go:17` 采用固定 25ms 取消轮询，单个活动 Run 约产生 40 次取消查询/秒，尚未看到该频率的延迟指标依据。

处理：删除无使用方的租约写入，除非存在明确的恢复流程。取消提示要么接入以减少查询等待，要么删除重复写入；始终保留 PostgreSQL 的最终取消判定。根据取消延迟要求和测量结果设置轮询频率。

这些是可以直接精简的机制，不需要为了“接入所有组件”再构造一个租约恢复系统。

### 7. 观察端有界面，但证据不足以支持完整模型分析（中优先级）

- `backend/internal/runtime/pi_store.go:303-333` 把模型 Span 的开始与结束都设为消息完成时刻，并把模型请求记录为 `{}`。
- Langfuse Observation / Batch 当前没有使用持久 Usage 字段构造明确的 token/cost 数据。
- `exporter.go` 的 buildBatch 使用当前 Conversation 的所有消息作为 Trace 输入，而 Pi 请求仅从 Run command 取最后一个 user goal；两者不等价。

处理：记录可公开、可脱敏的实际输入快照与真实调用时间、用量。明确区分缺失数据与零值。导出当前 Run 实际输入，不能把完整 Conversation 伪装成该次 Pi 调用上下文。

验收：Langfuse 显示的数据可与本地 Run/Attempt 对账；没有数据时显示未知，不以零耗时、空请求或其他 Run 的消息替代。

## 应保留的边界

- PostgreSQL 管业务事实；Redis 管可丢弃通知；Kafka 管投递和消费进度。
- Host 管会话生命周期；Pi 管模型循环；Harness 管工具与完成规则。
- Worker 不依赖 Pi 或 Langfuse；Langfuse 不管理浏览器 session，不决定任务成功。
- React 作为任务入口；Langfuse 作为观察端。当前没有证据证明需要另建一套 Langfuse 任务页面，暂停双入口扩展。
- Go 与 Node 通过版本化事件契约通信；Python 与 Harness 通过执行契约通信。不要把三种语言改造成共同持有模型状态或共同决定 Case verdict。

## 实施顺序与结束条件

1. 修正真实模型选择、完成事件契约和持久化失败停止行为。
2. 以现有事件流接齐 Redis Live Notifier 与 Langfuse Exporter，提供全部组件联合运行入口。
3. 拆出浏览器专属请求/证据投影，消除死配置与无消费方机制。
4. 完善实际模型输入、时序与 Usage 证据，并完成真实模型验收。
5. 用一条真实浏览器任务验证全部组件：请求进入 → Kafka 调度 → Pi 执行 → MCP/Worker 操作 → PostgreSQL 留证 → Redis/SSE 更新 → Langfuse 导出。
6. 分别注入 Redis、Langfuse、Kafka、持久化和 session 取消故障，验证各自的降级与停止边界。

不把单元测试通过、模块可 import、容器健康或 scripted fixture 通过，等同于全部组件的联合业务验收完成。
