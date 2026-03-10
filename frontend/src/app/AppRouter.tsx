import { Navigate, Route, Routes } from "react-router-dom";

import { AppLayout } from "../layouts/AppLayout";
import { CaseWorkbenchPage } from "../pages/CaseWorkbenchPage";
import { CasesPage } from "../pages/CasesPage";
import { DashboardPage } from "../pages/DashboardPage";
import { ExecutionDetailPage } from "../pages/ExecutionDetailPage";
import { ExecutionsPage } from "../pages/ExecutionsPage";
import { ReportCenterPage } from "../pages/ReportCenterPage";

export function AppRouter() {
  return (
    <Routes>
      <Route element={<AppLayout />}>
        <Route path="/" element={<Navigate to="/dashboard" replace />} />
        <Route path="/dashboard" element={<DashboardPage />} />
        <Route path="/cases" element={<CasesPage />} />
        <Route path="/cases/new" element={<CaseWorkbenchPage />} />
        <Route path="/cases/:caseId/edit" element={<CaseWorkbenchPage />} />
        <Route path="/executions" element={<ExecutionsPage />} />
        <Route path="/executions/:executionId" element={<ExecutionDetailPage />} />
        <Route path="/reports" element={<ReportCenterPage />} />
      </Route>
    </Routes>
  );
}
