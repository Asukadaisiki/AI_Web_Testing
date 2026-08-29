# Report Page Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace CaseWorkbenchPage with a ReportPage that shows project-scoped execution results.

**Architecture:** Two-panel layout — left sidebar lists projects, center shows overview stats + expandable execution results with step details. All data from existing backend APIs, no backend changes.

**Tech Stack:** React, Ant Design, @tanstack/react-query, existing NotebookLMLayout + NotebookNav

---

## File Structure

| Action | File | Responsibility |
|--------|------|---------------|
| Create | `frontend/src/pages/ReportPage.tsx` | Main report page with project list + execution results |
| Delete | `frontend/src/pages/CaseWorkbenchPage.tsx` | Remove old workbench page |
| Modify | `frontend/src/app/AppRouter.tsx` | Replace workbench routes with `/reports` |
| Modify | `frontend/src/components/NotebookNav.tsx` | Change nav item from 工作台 to 报告 |

---

### Task 1: Update navigation and routes

**Files:**
- Modify: `frontend/src/components/NotebookNav.tsx`
- Modify: `frontend/src/app/AppRouter.tsx`

- [x] **Step 1: Update NotebookNav.tsx** — Change the third nav item from 工作台 to 报告

Change line 6 from:
```ts
{ key: "/cases/new", label: "工作台", icon: "🔧" },
```
to:
```ts
{ key: "/reports", label: "报告", icon: "📊" },
```

- [x] **Step 2: Update AppRouter.tsx** — Remove CaseWorkbenchPage, add ReportPage route

Remove the CaseWorkbenchPage lazy import (lines 12-14). Add ReportPage import:
```ts
const ReportPage = lazy(() =>
  import("../pages/ReportPage").then((m) => ({ default: m.ReportPage })),
);
```

Remove routes `/cases/new` and `/cases/:caseId/edit`. Add:
```tsx
<Route path="/reports" element={<ReportPage />} />
```

- [x] **Step 3: Verify app compiles** — Run dev server, expect compile error about missing ReportPage (that's ok, Task 2 will create it)

Run: `cd frontend && npx tsc --noEmit 2>&1 | head -20`
Expected: Error about missing `../pages/ReportPage` module

- [x] **Step 4: Commit**

```bash
git add frontend/src/components/NotebookNav.tsx frontend/src/app/AppRouter.tsx
git commit -m "refactor: replace workbench nav with report page route"
```

---

### Task 2: Create ReportPage skeleton with project list

**Files:**
- Create: `frontend/src/pages/ReportPage.tsx`

This task creates the page shell: two-panel layout using NotebookLMLayout (no right panel), with a working project list in the left panel and a placeholder center panel.

- [x] **Step 1: Create ReportPage.tsx with project list**

```tsx
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Spin, Empty, Typography } from "antd";

import { NotebookLMLayout } from "../layouts/NotebookLMLayout";
import { getProjects, getExecutions, getExecutionOverview } from "../services/api";
import type { ProjectSummary } from "../types/api";

const { Text, Title } = Typography;

export function ReportPage() {
  const [selectedProjectId, setSelectedProjectId] = useState<number | null>(null);

  const { data: projects = [], isLoading: projectsLoading } = useQuery<ProjectSummary[]>({
    queryKey: ["projects"],
    queryFn: getProjects,
  });

  // Auto-select first project
  const activeProjectId = selectedProjectId ?? projects[0]?.id ?? null;

  const leftPanel = (
    <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
      <Title level={5} style={{ margin: 0, marginBottom: 12 }}>
        项目
      </Title>
      {projectsLoading ? (
        <Spin />
      ) : (
        projects.map((p) => (
          <div
            key={p.id}
            onClick={() => setSelectedProjectId(p.id)}
            style={{
              padding: "8px 12px",
              borderRadius: 8,
              cursor: "pointer",
              fontSize: 13,
              background: p.id === activeProjectId ? "#1a1a2e" : "transparent",
              color: p.id === activeProjectId ? "#fff" : "#666",
              transition: "background 0.15s",
            }}
          >
            {p.name}
          </div>
        ))
      )}
    </div>
  );

  const centerPanel = activeProjectId ? (
    <div style={{ padding: 20 }}>
      <Title level={4} style={{ margin: 0, marginBottom: 16 }}>
        {projects.find((p) => p.id === activeProjectId)?.name} — 报告
      </Title>
      <Text type="secondary">执行结果将在这里展示</Text>
    </div>
  ) : (
    <div style={{ display: "flex", alignItems: "center", justifyContent: "center", height: "100%" }}>
      <Empty description="请选择一个项目" />
    </div>
  );

  return <NotebookLMLayout leftPanel={leftPanel} centerPanel={centerPanel} navBottom />;
}
```

- [x] **Step 2: Verify app compiles and renders**

Run: `cd frontend && npx tsc --noEmit`
Expected: No errors. Navigate to `/reports` in browser, should see project list.

- [x] **Step 3: Commit**

```bash
git add frontend/src/pages/ReportPage.tsx
git commit -m "feat: add ReportPage skeleton with project list"
```

---

### Task 3: Add overview statistics to center panel

**Files:**
- Modify: `frontend/src/pages/ReportPage.tsx`

Add a `useQuery` for `getExecutionOverview` scoped to the selected project. Render 4 stat cards at the top of the center panel.

- [x] **Step 1: Add overview query and stat cards**

Inside `ReportPage`, add this query (after the projects query):

```tsx
const { data: overview } = useQuery({
  queryKey: ["execution-overview", activeProjectId],
  queryFn: () =>
    getExecutionOverview({ scope_type: "project", project_id: activeProjectId!, window_days: 30 }),
  enabled: activeProjectId != null,
});
```

Replace the center panel placeholder content with:

```tsx
const centerPanel = activeProjectId ? (
  <div style={{ padding: 20, overflowY: "auto", height: "100%" }} className="panel-scroll">
    <Title level={4} style={{ margin: 0, marginBottom: 16 }}>
      {projects.find((p) => p.id === activeProjectId)?.name} — 报告
    </Title>

    {overview && (
      <div style={{ display: "grid", gridTemplateColumns: "repeat(4, 1fr)", gap: 12, marginBottom: 24 }}>
        <StatCard label="通过率" value={`${(overview.pass_rate * 100).toFixed(1)}%`} />
        <StatCard label="失败数" value={String(overview.failed_count)} />
        <StatCard label="总执行数" value={String(overview.total_count)} />
        <StatCard label="平均耗时" value={overview.avg_duration_ms ? `${(overview.avg_duration_ms / 1000).toFixed(1)}s` : "-"} />
      </div>
    )}

    <Title level={5} style={{ margin: 0, marginBottom: 12 }}>执行结果</Title>
    <Text type="secondary">执行结果列表将在下一步添加</Text>
  </div>
) : (
  <div style={{ display: "flex", alignItems: "center", justifyContent: "center", height: "100%" }}>
    <Empty description="请选择一个项目" />
  </div>
);
```

Add the `StatCard` helper above the component:

```tsx
function StatCard({ label, value }: { label: string; value: string }) {
  return (
    <div
      className="nb-card"
      style={{ padding: 16, display: "flex", flexDirection: "column", gap: 4 }}
    >
      <Text type="secondary" style={{ fontSize: 12 }}>{label}</Text>
      <Text strong style={{ fontSize: 20 }}>{value}</Text>
    </div>
  );
}
```

- [x] **Step 2: Verify stat cards render with real data**

Run: `cd frontend && npx tsc --noEmit`
Expected: No errors. Navigate to `/reports`, select a project with execution data.

- [x] **Step 3: Commit**

```bash
git add frontend/src/pages/ReportPage.tsx
git commit -m "feat: add overview statistics cards to report page"
```

---

### Task 4: Add execution results list with expandable step details

**Files:**
- Modify: `frontend/src/pages/ReportPage.tsx`

This is the main feature task. Add execution list fetched via `getExecutions`, each row expandable to show step evidence.

- [x] **Step 1: Add executions query and expanded detail query**

Inside `ReportPage`, add after the overview query:

```tsx
const { data: executions = [] } = useQuery({
  queryKey: ["executions", activeProjectId],
  queryFn: () =>
    getExecutions({ project_id: activeProjectId!, limit: 50 }),
  enabled: activeProjectId != null,
});

const [expandedId, setExpandedId] = useState<number | null>(null);

const { data: executionDetail } = useQuery({
  queryKey: ["execution-detail", expandedId],
  queryFn: () => getExecutionDetail(expandedId!),
  enabled: expandedId != null,
});
```

- [x] **Step 2: Add ExecutionRow component**

Add above the `ReportPage` component:

```tsx
import type { StoredCaseExecutionSummary, StepExecutionEvidence, ExecutionStatus } from "../types/api";

const STATUS_ICON: Record<ExecutionStatus, string> = {
  passed: "✅",
  failed: "❌",
  running: "⏳",
  needs_intervention: "⚠️",
};

function formatTime(iso: string | null) {
  if (!iso) return "-";
  return new Date(iso).toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
}

function ExecutionRow({
  exec,
  expanded,
  onToggle,
  steps,
}: {
  exec: StoredCaseExecutionSummary;
  expanded: boolean;
  onToggle: () => void;
  steps: StepExecutionEvidence[] | undefined;
}) {
  return (
    <div className="nb-card" style={{ padding: 0, marginBottom: 8 }}>
      <div
        onClick={onToggle}
        style={{
          padding: "12px 16px",
          cursor: "pointer",
          display: "flex",
          alignItems: "center",
          gap: 10,
          borderRadius: expanded ? "12px 12px 0 0" : 12,
        }}
      >
        <span>{STATUS_ICON[exec.status]}</span>
        <Text strong style={{ flex: 1 }}>{exec.case_name}</Text>
        {exec.failure_category && (
          <Tag color="red" style={{ marginRight: 4 }}>{exec.failure_category}</Tag>
        )}
        <Text type="secondary" style={{ fontSize: 12 }}>{formatTime(exec.started_at)}</Text>
      </div>

      {expanded && steps && (
        <div style={{ borderTop: "1px solid #f0f0f0", padding: "8px 16px 16px" }}>
          {steps.map((step, i) => (
            <StepRow key={i} step={step} />
          ))}
        </div>
      )}
    </div>
  );
}
```

- [x] **Step 3: Add StepRow component**

```tsx
function StepRow({ step }: { step: StepExecutionEvidence }) {
  const [showScreenshot, setShowScreenshot] = useState(false);
  const isFailed = step.status === "failed";

  return (
    <div
      style={{
        display: "flex",
        alignItems: "flex-start",
        gap: 8,
        padding: "6px 0",
        borderLeft: `3px solid ${isFailed ? "#ff4d4f" : "#52c41a"}`,
        paddingLeft: 8,
        marginBottom: 4,
      }}
    >
      <span style={{ fontSize: 12 }}>{isFailed ? "✗" : "✓"}</span>
      <div style={{ flex: 1, minWidth: 0 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
          <Text style={{ fontSize: 12 }}>
            Step {step.step_index + 1}: <Text code style={{ fontSize: 11 }}>{step.action}</Text>
            {step.target && <Text type="secondary" style={{ fontSize: 11 }}> {step.target}</Text>}
          </Text>
          {step.duration_ms != null && (
            <Text type="secondary" style={{ fontSize: 11 }}>({step.duration_ms}ms)</Text>
          )}
        </div>

        {isFailed && step.error_message && (
          <div
            style={{
              marginTop: 4,
              padding: "4px 8px",
              background: "#fff2f0",
              borderRadius: 6,
              fontSize: 12,
              color: "#cf1322",
            }}
          >
            {step.error_message}
          </div>
        )}

        {step.locator_trace?.failure_reason && (
          <Text type="secondary" style={{ fontSize: 11, display: "block", marginTop: 2 }}>
            定位失败: {step.locator_trace.failure_reason}
          </Text>
        )}

        {step.screenshot_url && (
          <div style={{ marginTop: 4 }}>
            <a
              onClick={(e) => { e.stopPropagation(); setShowScreenshot(!showScreenshot); }}
              style={{ fontSize: 11, cursor: "pointer" }}
            >
              {showScreenshot ? "收起截图" : "查看截图"}
            </a>
            {showScreenshot && (
              <img
                src={step.screenshot_url}
                alt={`Step ${step.step_index + 1} screenshot`}
                style={{
                  maxWidth: "100%",
                  maxHeight: 300,
                  borderRadius: 8,
                  marginTop: 4,
                  border: "1px solid #f0f0f0",
                }}
              />
            )}
          </div>
        )}
      </div>
    </div>
  );
}
```

- [x] **Step 4: Wire up the execution list in centerPanel**

Replace the "执行结果列表将在下一步添加" placeholder in centerPanel with:

```tsx
<Title level={5} style={{ margin: 0, marginBottom: 12 }}>执行结果</Title>
{executions.length === 0 ? (
  <Empty description="暂无执行记录" />
) : (
  executions.map((exec) => (
    <ExecutionRow
      key={exec.id}
      exec={exec}
      expanded={expandedId === exec.id}
      onToggle={() => setExpandedId(expandedId === exec.id ? null : exec.id)}
      steps={
        expandedId === exec.id && executionDetail?.report
          ? executionDetail.report.steps
          : undefined
      }
    />
  ))
)}
```

Also add the `Tag` import at the top: `import { Spin, Empty, Typography, Tag } from "antd";`

- [x] **Step 5: Verify full report page works**

Run: `cd frontend && npx tsc --noEmit`
Expected: No errors. Navigate to `/reports`, select project, see stats + execution list with expandable steps.

- [x] **Step 6: Commit**

```bash
git add frontend/src/pages/ReportPage.tsx
git commit -m "feat: add execution results list with step details to report page"
```

---

### Task 5: Delete CaseWorkbenchPage and clean up

**Files:**
- Delete: `frontend/src/pages/CaseWorkbenchPage.tsx`

- [x] **Step 1: Delete the old workbench page**

```bash
rm frontend/src/pages/CaseWorkbenchPage.tsx
```

- [x] **Step 2: Verify no remaining references**

Run: `cd frontend && grep -r "CaseWorkbenchPage" src/ || echo "Clean"`
Expected: "Clean" — no remaining imports or references.

- [x] **Step 3: Verify app still compiles**

Run: `cd frontend && npx tsc --noEmit`
Expected: No errors.

- [x] **Step 4: Commit**

```bash
git add -A
git commit -m "chore: remove CaseWorkbenchPage (replaced by ReportPage)"
```
