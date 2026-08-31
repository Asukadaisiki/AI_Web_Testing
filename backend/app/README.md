# App Layout

后端采用 HTTP 适配、业务用例编排、领域服务和执行基础设施分层。

- `api/`：HTTP 路由、认证依赖和协议转换
- `application/`：跨能力的业务用例编排，目前主要承载 Planning
- `services/`：Case、Execution、DSL、Project 等可复用业务能力
- `ai/`：LLM、Agent、Prompt、页面探索和 AI 工具
- `runners/`：解释 DSL 并驱动 Playwright
- `locators/`：元素定位、人工修正和 fallback
- `reporters/`：将步骤证据组装为结构化报告
- `schemas/`：Pydantic 请求、响应和运行时数据合同
- `models/`：SQLAlchemy 持久化模型
- `db/`：数据库 Session 和 Base
- `core/`：配置、日志、通用基础设施

完整文件导航、主调用链和依赖规则见
[`docs/architecture-guide.md`](../../docs/architecture-guide.md)。
