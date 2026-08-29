# Demo Flow Simplification Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** 将当前“登录 -> Dashboard -> 多页面平台”重构为无认证的三步演示主链路：`/` AI 规划 -> `/cases` AI 用例中心 -> `/run/:executionId` 执行与报告一体页。

**Architecture:** 前端以现有 `AITestPlanningPanel`、`CasesPage`、`ExecutionDetailPage` 为核心，重组为面向演示的三步导航；`CaseWorkbenchPage` 保留为低优先级编辑入口。后端不删除认证模块本身，但对演示主链路相关接口改为依赖固定 demo 用户，移除运行期登录要求，同时保持现有 service 层和数据模型尽量不动。

**Tech Stack:** React, TypeScript, React Router, TanStack Query, Ant Design, FastAPI, SQLAlchemy, pytest, Vitest

---

### Task 1: 后端移除演示主链路的认证门槛

**Files:**
- Modify: `backend/app/api/auth.py`
- Modify: `backend/app/api/router.py`
- Modify: `backend/app/api/routes/ai_planning.py`
- Modify: `backend/app/api/routes/artifacts.py`
- Modify: `backend/app/api/routes/cases.py`
- Modify: `backend/app/api/routes/corrections.py`
- Modify: `backend/app/api/routes/dsl.py`
- Modify: `backend/app/api/routes/executions.py`
- Modify: `backend/app/api/routes/projects.py`
- Test: `backend/tests/unit/test_ai_planning_api.py`
- Test: `backend/tests/unit/test_case_executions_api.py`
- Test: `backend/tests/unit/test_cases_api.py`
- Test: `backend/tests/unit/test_corrections_api.py`
- Test: `backend/tests/unit/test_main.py`
- Test: `backend/tests/unit/test_projects_and_report_preferences_api.py`

- [x] **Step 1: 先写失败测试，锁定“匿名访问 demo 流接口也能成功”**

```python
def test_cases_api_allows_demo_access_without_login(client) -> None:
    response = client.get("/api/v1/cases")
    assert response.status_code == 200


def test_artifacts_are_available_without_login(client, tmp_path, monkeypatch) -> None:
    response = client.get("/artifacts/executions/sample.txt")
    assert response.status_code == 200
```

- [x] **Step 2: 运行定向后端测试，确认当前会因为 401 失败**

Run: `cd backend && uv run pytest tests/unit/test_cases_api.py tests/unit/test_case_executions_api.py tests/unit/test_ai_planning_api.py tests/unit/test_corrections_api.py tests/unit/test_projects_and_report_preferences_api.py tests/unit/test_main.py -q`

Expected: 至少出现若干 `401` 或 “未登录或登录态已失效” 相关失败。

- [x] **Step 3: 实现 demo 用户依赖，并替换主链路路由中的认证依赖**

```python
DEFAULT_DEMO_USER_ID = 1


def require_demo_user(
    session: Session = Depends(get_db_session),
) -> User:
    user = get_user_by_id(session, DEFAULT_DEMO_USER_ID)
    if user is None or not user.is_active:
        raise HTTPException(status_code=status.HTTP_500_INTERNAL_SERVER_ERROR, detail="Demo user 1 is missing.")
    return user
```

```python
def create_planning_session_route(
    payload: CreateAIPlanningSessionRequest,
    response: Response,
    session: Session = Depends(get_db_session),
    current_user: User = Depends(require_demo_user),
) -> AIPlanningSessionDetail:
    ...
```

```python
api_router.include_router(ai_planning_router)
api_router.include_router(cases_router)
api_router.include_router(corrections_router)
api_router.include_router(dsl_router)
api_router.include_router(executions_router)
api_router.include_router(projects_router)
```

Implementation notes:
- 不删除 `/api/v1/auth/*` 路由，保留为未使用兼容接口。
- 只替换演示主链路真正会访问到的接口依赖；`settings` 已经无认证，不需要动。
- `reports/preferences` 若前端不再使用，可保持现状，避免无意义改动。

- [x] **Step 4: 重新运行后端定向测试，确认 demo 流 API 已可无登录访问**

Run: `cd backend && uv run pytest tests/unit/test_cases_api.py tests/unit/test_case_executions_api.py tests/unit/test_ai_planning_api.py tests/unit/test_corrections_api.py tests/unit/test_projects_and_report_preferences_api.py tests/unit/test_main.py -q`

Expected: PASS；`test_main.py` 中 artifacts 访问断言更新为匿名 200。

- [x] **Step 5: 提交这一任务**

```bash
git add backend/app/api/auth.py backend/app/api/router.py backend/app/api/routes/ai_planning.py backend/app/api/routes/artifacts.py backend/app/api/routes/cases.py backend/app/api/routes/corrections.py backend/app/api/routes/dsl.py backend/app/api/routes/executions.py backend/app/api/routes/projects.py backend/tests/unit/test_ai_planning_api.py backend/tests/unit/test_case_executions_api.py backend/tests/unit/test_cases_api.py backend/tests/unit/test_corrections_api.py backend/tests/unit/test_main.py backend/tests/unit/test_projects_and_report_preferences_api.py
git commit -m "refactor: remove auth requirement from demo flow APIs"
```

### Task 2: 重做前端应用壳与路由，只保留三步演示流

**Files:**
- Modify: `frontend/src/app/App.tsx`
- Modify: `frontend/src/app/AppRouter.tsx`
- Modify: `frontend/src/layouts/AppLayout.tsx`
- Delete: `frontend/src/auth/AuthContext.tsx`
- Delete: `frontend/src/auth/AuthContext.test.tsx`
- Delete: `frontend/src/pages/AISettingsPage.tsx`
- Delete: `frontend/src/pages/AISettingsPage.test.tsx`
- Delete: `frontend/src/pages/CorrectionsPage.tsx`
- Delete: `frontend/src/pages/CorrectionsPage.test.tsx`
- Delete: `frontend/src/pages/DashboardPage.tsx`
- Delete: `frontend/src/pages/DashboardPage.test.tsx`
- Delete: `frontend/src/pages/ExecutionsPage.tsx`
- Delete: `frontend/src/pages/ExecutionsPage.test.tsx`
- Delete: `frontend/src/pages/LoginPage.tsx`
- Delete: `frontend/src/pages/LoginPage.test.tsx`
- Delete: `frontend/src/pages/ReportCenterPage.tsx`
- Delete: `frontend/src/pages/ReportCenterPage.test.tsx`
- Test: `frontend/src/app/AppRouter.test.tsx`

- [x] **Step 1: 先把路由测试改成无认证三步流，制造失败**

```tsx
test("root route renders planning page without auth guard", async () => {
  renderRouter(["/"]);
  expect(await screen.findByText("Planning Mock")).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "步骤 1 AI 规划" })).toHaveAttribute("href", "/");
});

test("legacy execution detail path redirects to /run/:id", async () => {
  renderRouter(["/executions/12"]);
  expect(await screen.findByText("Execution Detail Mock")).toBeInTheDocument();
});
```

- [x] **Step 2: 运行前端路由测试，确认当前仍依赖 AuthProvider 和旧页面**

Run: `cd frontend && npm run test -- src/app/AppRouter.test.tsx`

Expected: FAIL，错误点包括 `useAuth` mock 不再匹配后续目标结构，或仍然命中 `/login`、`/dashboard`。

- [x] **Step 3: 实现新的应用壳、路由和步骤导航**

```tsx
export function AppRoot() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <AppRouter />
      </BrowserRouter>
    </QueryClientProvider>
  );
}
```

```tsx
function LegacyExecutionRedirect() {
  const { executionId } = useParams<{ executionId: string }>();
  return <Navigate to={`/run/${executionId}`} replace />;
}

<Routes>
  <Route element={<AppLayout />}>
    <Route path="/" element={<PlanningPage />} />
    <Route path="/cases" element={<CasesPage />} />
    <Route path="/cases/new" element={<CaseWorkbenchPage />} />
    <Route path="/cases/:caseId/edit" element={<CaseWorkbenchPage />} />
    <Route path="/run/:executionId" element={<ExecutionDetailPage />} />
    <Route path="/executions/:executionId" element={<LegacyExecutionRedirect />} />
    <Route path="/dashboard" element={<Navigate to="/" replace />} />
    <Route path="/executions" element={<Navigate to="/cases" replace />} />
    <Route path="/login" element={<Navigate to="/" replace />} />
  </Route>
</Routes>
```

```tsx
const demoSteps = [
  { key: "/", title: "步骤 1", description: "AI 规划" },
  { key: "/cases", title: "步骤 2", description: "AI 用例" },
  { key: "/run", title: "步骤 3", description: "执行与报告" },
];
```

Implementation notes:
- `AppLayout` 不再显示用户信息、登出按钮和旧平台菜单。
- 布局采用顶部品牌区 + `Steps` 导航，执行页将第三步高亮。
- `CaseWorkbenchPage` 继续保留，但不进入主导航。

- [x] **Step 4: 重新运行路由测试，确认旧认证壳已拆掉**

Run: `cd frontend && npm run test -- src/app/AppRouter.test.tsx`

Expected: PASS。

- [x] **Step 5: 提交这一任务**

```bash
git add frontend/src/app/App.tsx frontend/src/app/AppRouter.tsx frontend/src/layouts/AppLayout.tsx frontend/src/app/AppRouter.test.tsx
git rm frontend/src/auth/AuthContext.tsx frontend/src/auth/AuthContext.test.tsx frontend/src/pages/AISettingsPage.tsx frontend/src/pages/AISettingsPage.test.tsx frontend/src/pages/CorrectionsPage.tsx frontend/src/pages/CorrectionsPage.test.tsx frontend/src/pages/DashboardPage.tsx frontend/src/pages/DashboardPage.test.tsx frontend/src/pages/ExecutionsPage.tsx frontend/src/pages/ExecutionsPage.test.tsx frontend/src/pages/LoginPage.tsx frontend/src/pages/LoginPage.test.tsx frontend/src/pages/ReportCenterPage.tsx frontend/src/pages/ReportCenterPage.test.tsx
git commit -m "refactor: simplify frontend shell to demo flow"
```

### Task 3: 新增 PlanningPage，把 AI 规划页改成完整入口

**Files:**
- Create: `frontend/src/pages/PlanningPage.tsx`
- Create: `frontend/src/pages/PlanningPage.test.tsx`
- Modify: `frontend/src/components/AITestPlanningPanel.tsx`
- Modify: `frontend/src/components/AITestPlanningPanel.test.tsx`
- Modify: `frontend/src/app/AppRouter.tsx`
- Modify: `frontend/src/services/api.ts`

- [x] **Step 1: 先写页面测试，锁定“导入草案时直接创建用例并跳到 cases”**

```tsx
test("planning page creates a case from imported draft and navigates to cases", async () => {
  vi.mocked(api.getProjects).mockResolvedValue([{ id: 1, name: "Demo Project", description: null }]);
  vi.mocked(api.getAISettings).mockResolvedValue({ enable_ai_planning: true } as never);
  vi.mocked(api.createCase).mockResolvedValue({ id: 9, project_id: 1, name: "AI 登录用例" } as never);

  renderWithProviders(<PlanningPage />, { route: "/", path: "/" });
  await userEvent.click(await screen.findByRole("button", { name: "创建用例并进入用例中心" }));

  await waitFor(() => {
    expect(api.createCase).toHaveBeenCalledWith(expect.objectContaining({ project_id: 1, actor_user_id: 1 }));
  });
});
```

- [x] **Step 2: 运行规划页和 planning panel 测试，确认当前没有独立入口页**

Run: `cd frontend && npm run test -- src/pages/PlanningPage.test.tsx src/components/AITestPlanningPanel.test.tsx`

Expected: FAIL，`PlanningPage` 不存在，且 panel 按钮文案仍是“导入到当前编辑器”。

- [x] **Step 3: 实现 PlanningPage，并给 AITestPlanningPanel 加上下文化导入文案**

```tsx
type AITestPlanningPanelProps = {
  ...
  draftImportLabel?: string;
};
```

```tsx
<Button
  type="primary"
  onClick={() => void handleImportDraft(draft)}
  disabled={draft.status !== "generated"}
>
  {draftImportLabel ?? "导入到当前编辑器"}
</Button>
```

```tsx
export function PlanningPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [selectedProjectId, setSelectedProjectId] = useState<number | null>(null);
  const projectsQuery = useQuery({ queryKey: ["projects"], queryFn: getProjects });
  const aiSettingsQuery = useQuery({ queryKey: ["ai-settings"], queryFn: getAISettings });

  async function handleImportDraft(draft: AIPlanningDraft) {
    if (!draft.dsl_case) {
      throw new Error("规划草案没有可创建的 DSL 内容。");
    }
    const projectId = selectedProjectId ?? projectsQuery.data?.[0]?.id;
    if (!projectId) {
      throw new Error("当前没有可用项目，无法创建用例。");
    }
    const createdCase = await createCase({
      project_id: projectId,
      actor_user_id: 1,
      ...draft.dsl_case,
    });
    await queryClient.invalidateQueries({ queryKey: ["cases"] });
    navigate(`/cases?created=${createdCase.id}`);
  }

  return <AITestPlanningPanel ... draftImportLabel="创建用例并进入用例中心" onImportDraft={handleImportDraft} />;
}
```

Implementation notes:
- 页面顶部增加一段演示说明，明确这是“AI 生成测试方案 -> AI 生成测试用例”的第一步。
- 如果项目列表为空，直接显示 `Alert`，不额外引入项目创建流程。
- `services/api.ts` 无需新增接口，只复用已有 `getProjects/getAISettings/createCase`。

- [x] **Step 4: 重新运行规划页相关测试**

Run: `cd frontend && npm run test -- src/pages/PlanningPage.test.tsx src/components/AITestPlanningPanel.test.tsx`

Expected: PASS。

- [x] **Step 5: 提交这一任务**

```bash
git add frontend/src/pages/PlanningPage.tsx frontend/src/pages/PlanningPage.test.tsx frontend/src/components/AITestPlanningPanel.tsx frontend/src/components/AITestPlanningPanel.test.tsx frontend/src/app/AppRouter.tsx frontend/src/services/api.ts
git commit -m "feat: add full-page planning entry for demo flow"
```

### Task 4: 收拢 Cases Hub、Workbench 回流和干预面板的路径

**Files:**
- Modify: `frontend/src/components/InterventionPanel.tsx`
- Modify: `frontend/src/components/executionPresentation.tsx`
- Modify: `frontend/src/pages/CaseWorkbenchPage.tsx`
- Modify: `frontend/src/pages/CaseWorkbenchPage.test.tsx`
- Modify: `frontend/src/pages/CasesPage.tsx`
- Modify: `frontend/src/pages/CasesPage.test.tsx`
- Modify: `frontend/src/pages/ExecutionDetailPage.test.tsx`

- [x] **Step 1: 先改测试，锁定所有 active flow 都应跳到 `/run/:id`，且不再依赖 CorrectionsPage**

```tsx
expect(api.executeCase).toHaveBeenCalledWith(1, { actor_user_id: 1 });
expect(await screen.findByText("detail-view")).toBeInTheDocument();
```

```tsx
expect(screen.queryByRole("link", { name: "查看同目标修正记录" })).not.toBeInTheDocument();
expect(screen.getByText("修正记录已保存到定位库")).toBeInTheDocument();
```

- [x] **Step 2: 运行 cases/workbench/execution 相关测试，确认当前仍引用旧执行中心和 corrections 页面**

Run: `cd frontend && npm run test -- src/pages/CasesPage.test.tsx src/pages/CaseWorkbenchPage.test.tsx src/pages/ExecutionDetailPage.test.tsx`

Expected: FAIL，失败点包括 `/executions/:id`、`/corrections?...` 旧链接断言。

- [x] **Step 3: 实现 Cases Hub 精简和主链路路径统一**

```tsx
onSuccess: (execution) => {
  void navigate(`/run/${execution.id}`);
}
```

```tsx
<Space>
  <Button type="primary">
    <Link to="/">返回 AI 规划</Link>
  </Button>
  <Button>
    <Link to="/cases/new">手动补充/编辑</Link>
  </Button>
</Space>
```

```tsx
const rerunMutation = useMutation({
  mutationFn: () => executeCase(caseId, { actor_user_id: triggeredBy }),
  onSuccess: (execution) => {
    navigate(`/run/${execution.id}`);
  },
});
```

```tsx
<Alert
  type="info"
  showIcon
  message="修正记录已保存到定位库"
  description="当前演示流不再提供独立修正列表页，请直接在本页完成提交后重跑验证。"
/>
```

```tsx
export function buildExecutionLink(record: Pick<StoredCaseExecutionSummary, "id" | "failed_step_index">) {
  if (record.failed_step_index === null || record.failed_step_index === undefined) {
    return `/run/${record.id}`;
  }
  return `/run/${record.id}#step-${record.failed_step_index + 1}`;
}
```

Implementation notes:
- `CasesPage` 文案改成“AI 用例中心”，弱化“平台列表页”语义。
- `CaseWorkbenchPage` 继续保留 AI planning 面板和自然语言生成，但执行成功回流必须改到 `/run/:id`。
- `InterventionPanel` 只保留“提交修正 -> 立即重跑”的闭环，不再跳转到已删除页面。

- [x] **Step 4: 重新运行这组前端测试**

Run: `cd frontend && npm run test -- src/pages/CasesPage.test.tsx src/pages/CaseWorkbenchPage.test.tsx src/pages/ExecutionDetailPage.test.tsx`

Expected: PASS。

- [x] **Step 5: 提交这一任务**

```bash
git add frontend/src/components/InterventionPanel.tsx frontend/src/components/executionPresentation.tsx frontend/src/pages/CaseWorkbenchPage.tsx frontend/src/pages/CaseWorkbenchPage.test.tsx frontend/src/pages/CasesPage.tsx frontend/src/pages/CasesPage.test.tsx frontend/src/pages/ExecutionDetailPage.test.tsx
git commit -m "refactor: align cases and intervention flow with run route"
```

### Task 5: 强化 ExecutionDetailPage，把报告展示和定位策略分析融合进来

**Files:**
- Create: `frontend/src/components/executionMetrics.ts`
- Create: `frontend/src/components/executionMetrics.test.ts`
- Modify: `frontend/src/pages/ExecutionDetailPage.tsx`
- Modify: `frontend/src/pages/ExecutionDetailPage.test.tsx`
- Modify: `frontend/src/services/api.ts`

- [x] **Step 1: 先写失败测试，锁定“报告总览 + 定位策略可视化”**

```tsx
vi.mocked(api.getExecutionOverview).mockResolvedValue({
  scope_type: "case",
  scope_case_id: 1,
  scope_project_id: 1,
  total_count: 4,
  passed_count: 3,
  failed_count: 1,
  running_count: 0,
  auto_completed_count: 4,
  intervention_count: 1,
  pass_rate: 0.75,
  automation_rate: 1,
  intervention_rate: 0.25,
  avg_duration_ms: 2100,
  previous_window_stats: { total_count: 2, passed_count: 1, failed_count: 1, running_count: 0, pass_rate: 0.5, avg_duration_ms: 2600 },
  window_comparison: { total_count_delta: 2, passed_count_delta: 2, failed_count_delta: 0, running_count_delta: 0, pass_rate_delta: 0.25, avg_duration_ms_delta: -500 },
  latest_failed_runs: [],
  latest_intervention_runs: [],
  failure_categories: [],
  trend_points: [],
  failure_step_actions: [],
  top_failed_cases: [],
  failure_root_causes: [],
} as never);

expect(await screen.findByText("执行报告总览")).toBeInTheDocument();
expect(screen.getByText("定位策略总览")).toBeInTheDocument();
expect(screen.getByText("DOM 语义")).toBeInTheDocument();
```

- [x] **Step 2: 运行执行详情测试，确认当前页面还只有步骤证据，没有总览层**

Run: `cd frontend && npm run test -- src/pages/ExecutionDetailPage.test.tsx src/components/executionMetrics.test.ts`

Expected: FAIL，`getExecutionOverview` 尚未被调用，且 `executionMetrics.ts` 不存在。

- [x] **Step 3: 实现定位策略归类和报告总览**

```ts
export type LocatorStrategyBucket = "dom" | "vlm" | "correction" | "manual" | "not_applicable";

export function classifyLocatorStrategy(step: StepExecutionEvidence): LocatorStrategyBucket {
  const raw = `${step.resolved_by ?? ""} ${step.locator_trace?.match_strategy ?? ""}`.toLowerCase();
  if (step.intervention_request) return "manual";
  if (raw.includes("correction") || raw.includes("tier0") || raw.includes("test_id") || raw.includes("xpath")) return "correction";
  if (raw.includes("visual") || raw.includes("vlm") || raw.includes("ai")) return "vlm";
  if (step.target || step.locator_trace) return "dom";
  return "not_applicable";
}
```

```tsx
const overviewQuery = useQuery({
  queryKey: ["execution-overview", "case", detail.case_id],
  queryFn: () =>
    getExecutionOverview({
      scope_type: "case",
      project_id: detail.project_id,
      case_id: detail.case_id,
      window_days: 14,
    }),
  enabled: Boolean(detail.case_id),
});
```

```tsx
<Card title="执行报告总览">
  <div className="summary-strip">
    <div className="summary-tile">
      <div className="summary-label">通过率</div>
      <div className="summary-value">{formatPassRate(overview.pass_rate)}</div>
    </div>
    <div className="summary-tile">
      <div className="summary-label">平均耗时</div>
      <div className="summary-value">{formatDuration(overview.avg_duration_ms)}</div>
    </div>
    <div className="summary-tile">
      <div className="summary-label">人工介入率</div>
      <div className="summary-value">{formatPassRate(overview.intervention_rate)}</div>
    </div>
  </div>
</Card>
```

```tsx
<Descriptions.Item label="定位策略">
  <Tag color="blue">DOM 语义</Tag>
</Descriptions.Item>
```

Implementation notes:
- 报告总览来自 `getExecutionOverview()` 的 case scope 聚合数据。
- 当前执行内的定位策略分布来自 `detail.report.steps` 的本地聚合，不额外改后端 schema。
- 归类规则是基于 `resolved_by` 和 `locator_trace.match_strategy` 的前端推断；如果后续要做更精确统计，再补后端枚举字段。

- [x] **Step 4: 重新运行执行详情相关测试**

Run: `cd frontend && npm run test -- src/pages/ExecutionDetailPage.test.tsx src/components/executionMetrics.test.ts`

Expected: PASS。

- [x] **Step 5: 提交这一任务**

```bash
git add frontend/src/components/executionMetrics.ts frontend/src/components/executionMetrics.test.ts frontend/src/pages/ExecutionDetailPage.tsx frontend/src/pages/ExecutionDetailPage.test.tsx frontend/src/services/api.ts
git commit -m "feat: fuse execution report summary into detail page"
```

### Task 6: 做最后清理、全链路验证和任务日志

**Files:**
- Modify: `docs/execution-log.md`
- Modify: `docs/bug-log.md`（仅在实施过程中发现明确缺陷时）
- Modify: `frontend/src/pages/README.md`

- [x] **Step 1: 跑前端主链路测试**

Run: `cd frontend && npm run test -- src/app/AppRouter.test.tsx src/pages/PlanningPage.test.tsx src/components/AITestPlanningPanel.test.tsx src/pages/CasesPage.test.tsx src/pages/CaseWorkbenchPage.test.tsx src/pages/ExecutionDetailPage.test.tsx src/components/executionMetrics.test.ts`

Expected: PASS。

- [x] **Step 2: 跑后端主链路测试**

Run: `cd backend && uv run pytest tests/unit/test_ai_planning_api.py tests/unit/test_cases_api.py tests/unit/test_case_executions_api.py tests/unit/test_corrections_api.py tests/unit/test_projects_and_report_preferences_api.py tests/unit/test_main.py -q`

Expected: PASS。

- [x] **Step 3: 追加任务日志**

```md
## 2026-04-05

- 任务：将前端主链路重构为 AI 规划 -> AI 用例 -> 执行与报告，无需登录
- 执行动作：移除 demo 流的认证依赖；新增 PlanningPage；精简导航与页面；融合执行详情页与报告总览；删除旧平台页
- 结果：演示流从平台式多页面收敛为三步闭环
- 验证：前端 Vitest 与后端 pytest 定向通过
- 后续：如需彻底清除 auth 模块和报告偏好接口，可单开一次清理任务
```

- [x] **Step 4: 只在发现明确问题时追加 bug-log**

Run: `git diff -- docs/bug-log.md`

Expected: 若实施过程中没有新增清晰缺陷，不修改 `docs/bug-log.md`。

- [x] **Step 5: 复查最终 diff**

Run: `git diff --stat`

Expected: 仅包含 demo flow 重构相关文件，无遗留旧页面 import 或 `/corrections`、`/dashboard`、`/login` 活跃链接。

- [x] **Step 6: 提交最终收口**

```bash
git add docs/execution-log.md frontend/src/pages/README.md
git commit -m "docs: record demo flow simplification work"
```

## Self-Review

- Spec coverage:
  - 去认证：Task 1
  - 新三步路由和导航：Task 2
  - Planning 作为第一页并直接建用例：Task 3
  - Cases 作为 AI 用例中心：Task 4
  - Execute + Report 融合、定位策略展示：Task 5
  - 验证、日志、收口：Task 6
- Placeholder scan: 无 `TODO/TBD/implement later` 占位语。
- Type consistency:
  - 统一使用 `/run/:executionId` 作为执行详情主路径。
  - 前端仍使用 `actor_user_id: 1`，与后端 `demo user 1` 约定一致。
  - 定位策略桶统一为 `dom | vlm | correction | manual | not_applicable`。

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-04-05-demo-flow-simplification.md`. Two execution options:

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

**Which approach?**
