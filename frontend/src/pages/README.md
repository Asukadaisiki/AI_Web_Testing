# Pages

演示流三步闭环页面。

## 主链路页面

| 页面 | 路由 | 说明 |
|------|------|------|
| PlanningPage | `/` | AI 测试规划入口，生成测试方案与用例 |
| CasesPage | `/cases` | AI 用例中心，管理和执行测试用例 |
| ExecutionDetailPage | `/run/:executionId` | 执行与报告一体页，包含步骤证据、定位策略和报告总览 |

## 辅助页面

| 页面 | 路由 | 说明 |
|------|------|------|
| CaseWorkbenchPage | `/cases/new`, `/cases/:caseId/edit` | 用例编辑工作台（不进入主导航） |

## 旧路由重定向

| 旧路径 | 目标 |
|--------|------|
| `/dashboard` | `/` |
| `/executions` | `/cases` |
| `/login` | `/` |
| `/executions/:id` | `/run/:id` |
