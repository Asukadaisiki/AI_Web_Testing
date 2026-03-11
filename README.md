# AI Web Testing

AI 增强的 Web UI 自动化测试平台。

当前仓库采用前后端分离结构：
- `backend/`：FastAPI + SQLAlchemy + Alembic + Playwright Runner
- `frontend/`：React + TypeScript + Vite 平台 UI
- `docs/`：产品规划、执行计划、设计文档、执行日志与缺陷日志

## 当前状态

当前已完成的主链路：
- Case 管理：创建、列表、详情、编辑
- DSL 校验：结构化 DSL 校验与保存
- 单 Case 执行：后端 Runner 执行、证据生成、执行详情查看
- 执行中心：列表、筛选、窗口统计、失败分类、根因回流
- 仪表盘 / 报告中心：趋势、失败聚合、根因榜
- Suite 管理：Suite CRUD、工作台排序、批量执行
- Suite v2.2：批次历史持久化、批次详情、失败重跑、从子执行详情返回来源 Suite 批次

当前未完成的重点方向：
- Suite Context / 跨 Case 参数传递
- AI 生成 DSL
- Vision 辅助定位
- 更完整的环境配置与登录体系

## 快速开始

### 1. 后端

```powershell
cd backend
uv sync
uv run alembic upgrade head
uv run backend-dev
```

默认后端地址：
- `http://127.0.0.1:8000`

### 2. 前端

```powershell
cd frontend
npm install
npm run dev
```

默认前端地址：
- `http://127.0.0.1:5173`

## 测试与构建

### 后端测试

```powershell
cd backend
uv run pytest
```

### 前端测试

```powershell
cd frontend
npm test -- --run
```

### 前端构建

```powershell
cd frontend
npm run build
```

## 推荐联调路径

建议先用最小 smoke case 验证主链路：
- `base_url`: `https://example.com`
- Step 1: `goto /`
- Step 2: `assert_url_contains example.com`

建议按下面顺序检查：
1. 在 Case 工作台创建并保存用例
2. 直接执行单 Case，确认执行详情、截图和证据正常
3. 创建包含 2 个 Case 的 Suite
4. 执行 Suite，确认生成批次详情
5. 制造 1 个失败 Case，验证失败重跑与历史回看

## 文档索引

- `docs/AI 自动化测试增强项目规划.md`：核心产品规划，优先级最高
- `docs/project-plan.md`：当前执行计划与阶段状态
- `docs/frontend-design.md`：前端设计说明
- `docs/execution-log.md`：任务执行记录
- `docs/bug-log.md`：缺陷记录

如果文档之间有冲突，以 `docs/AI 自动化测试增强项目规划.md` 为准。

## 开发约束

- 正式执行结果以 `backend` Runner 为准
- 前端只负责平台交互、工作台编辑和结果展示
- AI 能力不能绕过 DSL 校验直接驱动执行
- 新增功能默认需要补测试，并更新 `docs/execution-log.md`
