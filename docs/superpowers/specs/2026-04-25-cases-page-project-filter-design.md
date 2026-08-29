# CasesPage 项目级分类设计

## 背景

当前 CasesPage 展示所有项目的用例，没有项目维度的组织。后端已支持 `project_id` 过滤，`StoredCaseSummary` 已有 `project_id` 字段，但前端未使用。ReportPage 已有成熟的项目选择器可复用。

## 目标

用例分类层级：**项目 → 状态**。先选项目，再按状态过滤。

## 改动范围

### 1. API 层 (`frontend/src/services/api.ts`)

- `getCases()` 增加 `project_id` 可选参数，构建 query string
- 无需新增接口，`GET /api/v1/cases?project_id=X` 已存在

### 2. CasesPage (`frontend/src/pages/CasesPage.tsx`)

**左侧面板改造**（参考 ReportPage 模式）：

```
项目列表（带增删改）
─────────────
搜索框
状态过滤 tags（全部/待执行/已通过/已失败）
─────────────
新建用例按钮
```

- 新增 `selectedProjectId` state，默认选第一个项目
- fetch projects query（复用 `getProjects`）
- 查询 cases 时传入 `project_id`
- 无项目选中时显示 Empty 提示
- 项目 CRUD：新建/编辑/删除 Modal（复用 ReportPage 逻辑）

**状态过滤**：

- 保持现有 statusFilter 逻辑不变
- 但只对当前项目下的用例生效

**用例卡片**：

- 无需额外改动，已显示足够信息

### 3. 不改动的部分

- 后端 API：已完整支持，无需改动
- 数据库模型：已有 `project_id` 外键
- 类型定义：`StoredCaseSummary` 已有 `project_id` 字段

## 数据流

```
用户选择项目 → setSelectedProjectId
  → useQuery(["cases", projectId]) → getCases({ project_id: projectId })
  → filteredCases（searchText + statusFilter 过滤）
  → 渲染卡片
```

## 项目删除行为

- 项目删除后 CASCADE 删除关联用例和执行记录
- 删除后自动切到下一个项目或空状态
- 与 ReportPage 行为一致
