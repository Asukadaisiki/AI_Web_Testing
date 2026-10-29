# Runtime / Harness 联合验收

日期：2026-10-28。原生 PostgreSQL、Kafka、Redis、Playwright Worker 和三个官方 SDK；实际模型为 `glm-5.3-flash`。

## 已实现的边界

- Go 负责持久化、投递、并发、取消和证据，不运行模型循环。
- Adapter 适配 Pi、Claude Code、Codex 官方 SDK，Strategy 按 `runtime_id` 选择。
- Harness 独立存储为 `system_prompt + skills + mcp`，提供 PostgreSQL CRUD 和版本管理。
- Run 原子保存完整 Harness 快照；修改或删除目录项不改变已有 Run 和重跑规格。
- Langfuse 是独立异步观察者；核心应用健康检查显示 Langfuse disabled，独立 exporter 仍可导出 trace。

## 真实执行结果

15 个场景全部达到预期。负向场景和快照重跑的 `failed` 是预期结果，原始失败证据保留。

| 场景 | Pi | Claude Code | Codex |
| --- | --- | --- | --- |
| 通用 Harness：系统提示和 skill | succeeded · 227 tokens | succeeded · 365 tokens | succeeded · 1,528 tokens |
| 同一浏览器 Harness：实际工具和 fresh-context dry run | succeeded · 34,824 tokens | succeeded · 22,784 tokens | succeeded · 42,571 tokens |
| 不存在文本的负向断言 | failed · 26,513 tokens | failed · 20,077 tokens | failed · 30,943 tokens |
| 实际 open_page 后主动取消 | cancelled | cancelled | cancelled |
| Harness 修改、删除后的失败 Run 快照重跑 | failed · 26,500 tokens | failed · 29,687 tokens | failed · 30,736 tokens |

每个 Run 均核验唯一持久化完成事件、已结束的 span 和 SSE 重连回放。12 个浏览器 Run 均核验 Worker 的实际 DELETE 关闭确认；三个成功浏览器 Run 各下载验证 4 张 PNG 证据。

三种 SDK 的浏览器 trace 均成功导出 Langfuse。Claude 的累计 query/model 用量关联 Run 根节点，并保存到 Trace/root 元数据，不分摊为单条 GENERATION 用量。Langfuse 查询接口为未知用量生成的零值兼容字段不被当作实际 token 证据。

Codex 原始浏览器失败 Run `run_7871ee00f5be65cfc572d7860ce324c7` 保留；通过真实 `/reruns` 接口创建的 `run_3b39b543930527e95d8d5e086b79b0ad` 成功执行 `open_page`、`assert_text` 和 `finish_case`。

## 回归与审查

Go 全量测试、独立测试库中的 PostgreSQL/Runtime/Langfuse 集成测试、共享 host 的真实 Pi SDK race 检查、前端 89 项单元测试和生产构建通过。Worker 98 项测试、浏览器界面 8 项测试通过。

独立审查验证 Codex provider 适配和 Pi transport 关闭确认，并发现 Pi 直接 `close()` 未等待活跃 prompt 的问题。修复后直接关闭会主动取消并等待 SDK 和 prompt，再执行 MCP 清理；真实 SDK 回归测试验证文件保留和幂等关闭。最终 SDK 全量测试和构建结果见配套 JSON。

最终 SDK 套件 422 项通过、1 项显式启用的 live smoke 测试跳过；真实 provider 已由上述联合验收覆盖。类型检查和构建通过。最终构建另执行 Pi 浏览器 Run `run_1673695159bde1683a122878a7433cd7`：succeeded、34,798 tokens、4 张 PNG、唯一完成事件和实际关闭确认。

## 实际限制

- 当前 GLM Responses 服务忽略 namespace 工具声明，需要显式设置 `AGENTTRACE_CODEX_TOOL_FORMAT=flat-functions`。默认 `native` 保留原始协议。转换仅发生在 provider 声明和 callable name 边界，真实 SDK、CLI、MCP、参数、返回值和用量保持原执行链路。
- Pi 记录模型调用粒度；Claude 提供 assistant message 和累计 query/model 用量；Codex 提供 turn 粒度。未提供的逐次模型输入、调用时间和账单成本保持未知；取消时尚未收到 SDK 用量的零记录不代表实际未消费 token。
- 直接 SDK 子进程和 MCP transport 等待关闭确认。任意自行脱离进程组的后代进程没有独立 join 确认。关闭未确认时抛出 `SessionCleanupError`，保留 Attempt 文件，不宣称资源释放成功。
- 验收使用隔离 Harness 和 `8112` 审计代理证明浏览器关闭。验收结束后代理停止；正常应用 Worker 仍在 `8110`。重跑这些审计快照需先按 README 启动代理；正常内置 Harness 使用 `8110`。
- catalog 的 available 表示本机配置就绪；外部服务是否可用仍由实际 SDK 执行验证。

## 查看结果

工作台：<http://127.0.0.1:5184>。

- [Pi 浏览器 Run](http://127.0.0.1:5184/?project=prj_6ae7f1de2d558fdfbf9e0ce8e8c1323f&conversation=conv_ad3eba41295bc083cc99a907ae90d055&run=run_bcae1bf8fef34932d4b62bac23a97283)
- [Claude Code 浏览器 Run](http://127.0.0.1:5184/?project=prj_6ae7f1de2d558fdfbf9e0ce8e8c1323f&conversation=conv_dc4dc049c18a0da26f8f57d4f247404c&run=run_4ede99802878a1d2a7e78e915a762010)
- [Codex 浏览器 Run](http://127.0.0.1:5184/?project=prj_6ae7f1de2d558fdfbf9e0ce8e8c1323f&conversation=conv_15c65e4590aa3f68e9b3951dfd2ff332&run=run_3b39b543930527e95d8d5e086b79b0ad)

完整 Run ID、用量、关闭确认、审查修复及最终验证记录：[配套 JSON](runtime-harness-acceptance-2026-10-28.json)。
