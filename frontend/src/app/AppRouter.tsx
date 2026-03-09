import { Navigate, Route, Routes } from "react-router-dom";

import { AppLayout } from "../layouts/AppLayout";
import { CasesPage } from "../pages/CasesPage";
import { ExecutionDetailPage } from "../pages/ExecutionDetailPage";
import { ExecutionsPage } from "../pages/ExecutionsPage";

export function AppRouter() {
  return (
    <Routes>
      <Route element={<AppLayout />}>
        <Route path="/" element={<Navigate to="/cases" replace />} />
        <Route path="/cases" element={<CasesPage />} />
        <Route path="/executions" element={<ExecutionsPage />} />
        <Route path="/executions/:executionId" element={<ExecutionDetailPage />} />
      </Route>
    </Routes>
  );
}
