import { Navigate, Route, Routes } from "react-router-dom";

import { AppLayout } from "../layouts/AppLayout";
import { CaseWorkbenchPage } from "../pages/CaseWorkbenchPage";
import { CasesPage } from "../pages/CasesPage";
import { ExecutionDetailPage } from "../pages/ExecutionDetailPage";
import { ExecutionsPage } from "../pages/ExecutionsPage";

export function AppRouter() {
  return (
    <Routes>
      <Route element={<AppLayout />}>
        <Route path="/" element={<Navigate to="/cases" replace />} />
        <Route path="/cases" element={<CasesPage />} />
        <Route path="/cases/new" element={<CaseWorkbenchPage />} />
        <Route path="/cases/:caseId/edit" element={<CaseWorkbenchPage />} />
        <Route path="/executions" element={<ExecutionsPage />} />
        <Route path="/executions/:executionId" element={<ExecutionDetailPage />} />
      </Route>
    </Routes>
  );
}
