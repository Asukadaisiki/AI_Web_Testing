# Browser Worker Package

这个包只承载浏览器执行器职责，不承载用户侧业务控制面。

- `server/`：FastAPI 入口、路由和 HTTP 适配层。
- `capabilities/`：Go 控制面调用的浏览器能力和无状态执行 RPC。
- `exploration/`：页面 A11y 探索、locator preflight 和 VLM prompt。
- `runners/`：解释已校验 DSL 并驱动 Playwright。
- `locators/`：元素定位、人工修正协议和 fallback。
- `reporting/`：结构化报告构建和 failure signal 分类。
- `contracts/`：Pydantic 请求、响应和运行时数据合同。
- `runtime/`：配置、日志和中间件。
