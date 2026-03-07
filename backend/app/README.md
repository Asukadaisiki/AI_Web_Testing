# App Layout

这里放后端应用代码。

建议后续按以下模块逐步实现：

- `api/`：路由与接口层
- `core/`：配置、日志、通用基础设施
- `db/`：数据库会话、基础定义
- `models/`：SQLAlchemy 模型
- `schemas/`：Pydantic 模型
- `services/`：业务服务层
- `runners/`：执行器与任务运行
- `locators/`：定位策略
- `reporters/`：报告与证据输出
- `ai/`：AI 接入与编排
