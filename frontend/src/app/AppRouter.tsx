import { Suspense, lazy } from "react";
import { Navigate, Route, Routes } from "react-router-dom";

import { AppLayout } from "../layouts/AppLayout";
import { LoadingBlock } from "../components/PageFeedback";

const DashboardPage = lazy(() =>
  import("../pages/DashboardPage").then((module) => ({ default: module.DashboardPage })),
);
const CasesPage = lazy(() =>
  import("../pages/CasesPage").then((module) => ({ default: module.CasesPage })),
);
const CaseWorkbenchPage = lazy(() =>
  import("../pages/CaseWorkbenchPage").then((module) => ({ default: module.CaseWorkbenchPage })),
);
const ExecutionsPage = lazy(() =>
  import("../pages/ExecutionsPage").then((module) => ({ default: module.ExecutionsPage })),
);
const ExecutionDetailPage = lazy(() =>
  import("../pages/ExecutionDetailPage").then((module) => ({ default: module.ExecutionDetailPage })),
);
const ReportCenterPage = lazy(() =>
  import("../pages/ReportCenterPage").then((module) => ({ default: module.ReportCenterPage })),
);

export function AppRouter() {
  return (
    <Suspense fallback={<LoadingBlock />}>
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
    </Suspense>
  );
}
