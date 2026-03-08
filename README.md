# AI Web Testing

## 文档体系

- `docs/AI 自动化测试增强项目规划.md`：核心规划文档，定义产品目标、五层架构和主路线
- `docs/project-plan.md`：执行计划文档，必须围绕核心规划展开
- `docs/frontend-design.md`：前端目标设计文档，从属于核心规划
- `docs/execution-log.md`：任务执行记录
- `docs/bug-log.md`：缺陷与偏差记录

如果文档之间出现冲突，以 `docs/AI 自动化测试增强项目规划.md` 为准。

## 当前状态

当前仓库处于“后端阶段 1 局部完成、前端仍为骨架”的状态。

- `backend/`：已具备 FastAPI 入口、数据库连通性校验、Alembic 初始化、领域模型、DSL 校验、Case 基础 API
- `frontend/`：当前以目录骨架和模块边界为主，尚未完成页面与依赖落地
- `docs/`：包含核心规划、执行计划、前端设计和过程日志

## 仓库结构

- `backend/`：Python 后端，围绕 `uv + FastAPI + SQLAlchemy + Alembic`
- `frontend/`：React 前端，目标技术栈为 `React + TypeScript + Vite`
- `docs/`：产品规划、执行计划、前端设计和任务日志

## 开发约束

- 项目主线必须围绕核心规划推进，不把平台支撑工作误当成主产品路线
- 正式测试执行以后端 Runner 为准，前端只做触发、工作台调试和结果展示
- AI 能力不能绕过结构化 DSL 校验和执行链路
