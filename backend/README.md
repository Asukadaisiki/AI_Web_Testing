# Backend Status

后端是核心规划落地的主执行端，围绕 `uv + FastAPI + SQLAlchemy + Alembic` 推进。

## 当前已具备

- FastAPI 应用入口
- 应用创建阶段数据库连通性校验
- Alembic 初始迁移
- 第一批领域模型
- DSL 校验接口
- `cases` 基础持久化 API

## 当前未完成

- Playwright Runner
- Step 级真实执行
- Locator 服务
- Reporter 服务
- 任务与报告查询
- Suite 批量执行链路
- AI 接入层

## 本地开发约定

- 开发数据库默认使用 PostgreSQL
- 首次启动前先执行 `uv run alembic upgrade head`
- 后端启动命令：`uv run backend-dev`

## 后端落地顺序

后端执行顺序必须服从核心规划：

1. 阶段 1：DSL、Case、Runner 最小闭环
2. 阶段 2：Locator 服务
3. 阶段 3：自然语言生成 DSL
4. 阶段 4：Reporter 与报告查询
5. 阶段 5：Suite 与回归执行
