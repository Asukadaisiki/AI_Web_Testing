# App Layout

这里放 Browser Worker 代码。

目标边界：

- `api/`：内部 Browser capability HTTP 适配层，不承载用户侧业务 API。
- `application/browser/`：Go AgentService 调用的浏览器能力和无状态执行边界。
- `ai/page_explorer.py` 与 `ai/locator_preflight.py`：页面探索和 preflight 事实生成。
- `runners/`、`locators/`：Playwright 执行和定位能力。
- `services/`、`reporters/`：无状态执行 RPC 仍复用的纯报告和失败信号逻辑。
