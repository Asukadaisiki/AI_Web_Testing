# 会话与项目解耦设计

日期: 2026-04-29

## 背景

当前 AI 规划功能强制依赖项目——前端禁用面板直到项目存在，后端服务层有硬编码回退（`project_id or 1`）。这导致用户必须先有项目才能开始 AI 规划，但实际使用中应该是「会话优先」：用户先进入会话，在对话过程中按需创建/关联项目。

## 目标

- 会话创建不需要项目
- 会话与项目为多对多关系（一个会话可关联多个项目，一个项目可被多个会话引用）
- 对话阶段可以无项目运行，DSL 生成前必须关联至少一个项目
- UI 入口从「选项目 → 开始规划」改为「会话列表 → 进入会话 → 按需关联项目」

## 数据模型

### 新增关联表 `session_projects`

```
session_projects
  id          INTEGER PK AUTOINCREMENT
  session_id  INTEGER FK → ai_planning_sessions.id ON DELETE CASCADE
  project_id  INTEGER FK → projects.id ON DELETE CASCADE
  created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
  UNIQUE(session_id, project_id)
```

### 修改 `ai_planning_sessions` 表

- 移除 `project_id` 列（通过 Alembic migration）

### ORM 变更

- `AIPlanningSession` 模型：去掉 `project_id`，新增 `projects = relationship("Project", secondary="session_projects", back_populates="sessions")`
- `Project` 模型：新增 `sessions = relationship("AIPlanningSession", secondary="session_projects", back_populates="projects")`
- 新建 `SessionProject` 关联模型（含 `created_at`）

### 迁移策略

1. 创建 `session_projects` 表
2. 将现有 `ai_planning_sessions.project_id` 数据迁移到关联表（WHERE project_id IS NOT NULL）
3. 删除 `ai_planning_sessions.project_id` 列

## API 路由

### 现有路由修改

| 端点 | 变更 |
|------|------|
| `POST /sessions` | 移除 `project_id` 字段 |
| `GET /sessions` | 移除 `project_id` 查询参数，改为通用列表（支持分页） |
| `POST /sessions/{id}/drafts:generate` | 前置校验：会话必须至少关联一个项目，否则 400 |
| `POST /sessions/{id}/execute` | 同上 |
| 其余端点 | 不变 |

### 新增路由

| 端点 | 说明 |
|------|------|
| `POST /sessions/{id}/projects` | 关联项目，body: `{ project_id: int }` |
| `DELETE /sessions/{id}/projects/{project_id}` | 取消关联 |
| `GET /sessions/{id}/projects` | 列出关联项目 |
| `POST /sessions/{id}/projects:create` | 在会话内创建项目并自动关联，body: `{ name, description? }` |

### Schema 变更

- `CreateAIPlanningSessionRequest`：移除 `project_id`
- `AIPlanningSession`：移除 `project_id`，新增 `projects: list[ProjectSummary]`
- 新增 `LinkProjectRequest(project_id: int)`
- 新增 `CreateProjectInSessionRequest(name: str, description: str | None)`

## 服务层

### 核心原则

会话可无项目运行，直到 DSL 生成或执行时才校验项目关联。

### `ai_planning.py` 函数变更

| 函数 | 变更 |
|------|------|
| `create_planning_session()` | 移除 `project_id` 逻辑 |
| `list_planning_sessions()` | 移除 `project_id` 过滤 |
| `send/stream_planning_message()` | 从关联项目获取 `project_ids` 列表传入 agent，无项目传空列表 |
| `generate_planning_drafts()` | 前置校验至少一个关联项目 |
| `save_and_execute_selected_drafts()` | 从 draft 上下文获取 project_id |
| `retest_cases()` | 从 case 记录获取 project_id |
| `_build_session_context_preamble()` | 无项目时返回基础 preamble，有项目时合并所有关联项目上下文 |
| `_auto_update_insights()` | 遍历所有关联项目分别更新 |

### 移除硬编码

- `planning_session.project_id or 1` → 从关联项目获取
- `planning_session.project_id or 0` → 同上

### 新增函数

- `link_project_to_session(session_id, project_id, user_id)` — 校验权限后关联
- `unlink_project_from_session(session_id, project_id)` — 取消关联
- `list_session_projects(session_id)` — 返回关联项目列表
- `create_project_in_session(session_id, name, description, user_id)` — 创建并关联

### `test_planning_agent.py` / `planning_tools.py` 变更

- `project_id: int` 参数改为 `project_ids: list[int]`
- 无项目时，项目相关工具返回「暂无项目关联」提示

## 前端

### 新增 `SessionListPage`

- 显示所有会话卡片：标题、关联项目标签、最近活动时间
- 「新建会话」按钮，点击直接创建
- 点击卡片进入 `SessionDetailPage`

### 改造 `PlanningPage` → `SessionDetailPage`

- 路由改为 `/planning/sessions/:sessionId`
- 移除项目选择器
- 新增项目区域：标签显示关联项目，可取消关联
- 「关联项目」下拉 + 「新建项目」按钮
- DSL 生成按钮：无项目时禁用并提示

### 组件变更

- `AITestPlanningPanel`：`projectId` prop → `projectIds: number[]`
- 移除 `!projectId` 禁用逻辑
- `createPlanningSession()` 不传 `project_id`

### 类型变更

```typescript
// Before
AIPlanningSession { project_id: number }
CreatePlanningSessionPayload { project_id: number }

// After
AIPlanningSession { projects: ProjectSummary[] }
CreatePlanningSessionPayload { }

// New types
LinkProjectPayload { project_id: number }
CreateProjectInSessionPayload { name: string; description?: string }
```

### 新增 API 调用

- `linkProjectToSession(sessionId, payload)`
- `unlinkProjectFromSession(sessionId, projectId)`
- `listSessionProjects(sessionId)`
- `createProjectInSession(sessionId, payload)`

## 错误处理

| 场景 | 行为 |
|------|------|
| DSL 生成时无关联项目 | 400：「请先关联至少一个项目」 |
| 关联不存在的项目 | 404 |
| 重复关联 | 409 Conflict |
| 取消关联后 draft 仍引用该项目 | draft 保留 project_id 快照，不失效 |
| 无项目时 AI 使用项目工具 | 返回「当前会话未关联项目，请先创建或关联项目」 |

## 测试

### 单元测试

- 关联/取消关联 CRUD
- DSL 生成前的项目校验
- 无项目时 agent 工具返回值
- 多项目上下文聚合

### 集成测试

- 完整流程：创建会话 → 对话 → 创建项目 → 关联 → DSL 生成 → 执行
- 多会话共享同一项目
- 删除项目后会话状态（级联取消关联，会话不删除）
