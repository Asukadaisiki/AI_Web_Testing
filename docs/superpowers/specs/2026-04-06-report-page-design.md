# Report Page Design

Replace CaseWorkbenchPage (工作台) with a Report page that shows test execution results organized by project. Remove DSL generation from frontend display.

## Layout

Two-panel layout using existing NotebookLM card style:
- **Left panel**: Project list (selectable)
- **Center panel**: Project overview stats + case execution results with step details
- **Right panel**: Removed

## Left Panel - Project List

- Fetch projects via `GET /api/v1/projects`
- Display project name as selectable list items
- Active item highlighted with current theme
- Persist user selection via `localStorage` (or ReportPreference API)

## Center Panel - Overview Statistics

- Fetch via `GET /api/v1/executions/overview?project_id={id}`
- Display 4 stat cards: pass rate, failure count, total cases, average duration
- Auto-refresh when project selection changes

## Center Panel - Execution Results

- Fetch via `GET /api/v1/executions?project_id={id}` for the list
- Each result row shows: case name, execution status (passed/failed/running), timestamp
- Expandable to reveal step-by-step details:
  - Step index, action type, status icon
  - Screenshot thumbnail (clickable to enlarge)
  - Failed steps: red highlight, error message, failure category (locator/assertion/navigation/network)
- Fetch detailed report via `GET /api/v1/executions/{id}` when expanding a row

## Route Changes

| Old Route | New Route | Notes |
|-----------|-----------|-------|
| `/cases/new` → CaseWorkbenchPage | Remove | Delete page |
| `/cases/:caseId/edit` → CaseWorkbenchPage | Remove | Delete page |
| None | `/reports` → ReportPage | New page |

## Navigation Changes

- Sidebar item: `工作台` (wrench icon, `/cases/new`) → `报告` (chart icon, `/reports`)
- Remove CaseWorkbenchPage from lazy-loaded routes

## Files to Modify

1. `frontend/src/pages/CaseWorkbenchPage.tsx` → Delete
2. `frontend/src/pages/ReportPage.tsx` → New (report page component)
3. `frontend/src/app/AppRouter.tsx` → Update routes
4. `frontend/src/components/NotebookNav.tsx` → Update navigation item
5. `frontend/src/app/App.tsx` → Remove CaseWorkbenchPage lazy import if present

## Files NOT Changed

- Backend API: No changes needed, all required endpoints exist
- DSL generation backend: Keep as-is, just remove from frontend
- Other pages (PlanningPage, CasesPage, ExecutionDetailPage): Unchanged

## Success Criteria

- Report page loads with project list in left panel
- Selecting a project shows overview stats and execution results
- Expanding an execution shows step details with screenshots and failure info
- CaseWorkbenchPage and DSL generation UI are no longer accessible from frontend
- Navigation works correctly with new route
