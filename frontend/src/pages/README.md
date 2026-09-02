# Pages

演示流三步闭环页面。

## 主链路页面

| 页面 | 路由 | 说明 |
|------|------|------|
| PlanningPage | `/` | AI 测试规划入口，生成测试方案与用例 |
| CasesPage | `/cases` | AI 用例中心，管理和执行测试用例 |
| ExecutionDetailPage | `/reports/:executionId` | 执行与报告一体页，包含步骤证据、定位策略和报告总览 |

## 辅助页面

| 页面 | 路由 | 说明 |
|------|------|------|
| ReportPage | `/reports` | 项目报告页，展示执行概览和用例执行详情 |

## 旧路由重定向

| 旧路径 | 目标 |
|--------|------|
| `/dashboard` | `/` |
| `/executions` | `/cases` |
| `/login` | `/` |
| `/run/:id`、`/executions/:id` | `/reports/:id` |
