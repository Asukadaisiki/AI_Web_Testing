# Backend Skeleton

后端采用 `uv + FastAPI + SQLAlchemy + Alembic` 方向搭建。

当前已具备：

- FastAPI 应用入口
- 应用创建阶段数据库连通性校验
- Alembic 迁移初始化
- 阶段 1 领域模型骨架与最小 `cases` API

本地开发约定：

- 开发数据库默认使用 PostgreSQL
- 首次启动前先执行 `uv run alembic upgrade head`
- 后端启动命令为 `uv run backend-dev`

后续按以下顺序逐步实现：

1. 配置与基础工程
2. 数据库模型与迁移
3. 鉴权与项目空间
4. DSL 与执行任务
5. Runner、Locator、Reporter
