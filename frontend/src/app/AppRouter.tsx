import { Suspense, lazy } from "react";
import { Navigate, Outlet, Route, Routes } from "react-router-dom";

import { useAuth } from "../auth/AuthContext";
import { ErrorBlock } from "../components/PageFeedback";
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
const CorrectionsPage = lazy(() =>
  import("../pages/CorrectionsPage").then((module) => ({ default: module.CorrectionsPage })),
);
const AISettingsPage = lazy(() =>
  import("../pages/AISettingsPage").then((module) => ({ default: module.AISettingsPage })),
);
const ExecutionDetailPage = lazy(() =>
  import("../pages/ExecutionDetailPage").then((module) => ({ default: module.ExecutionDetailPage })),
);
const ReportCenterPage = lazy(() =>
  import("../pages/ReportCenterPage").then((module) => ({ default: module.ReportCenterPage })),
);
const LoginPage = lazy(() =>
  import("../pages/LoginPage").then((module) => ({ default: module.LoginPage })),
);

function ProtectedRoute() {
  const { authErrorMessage, isAuthResolved, isAuthenticated } = useAuth();

  if (!isAuthResolved) {
    return <LoadingBlock />;
  }
  if (authErrorMessage) {
    return <ErrorBlock message={authErrorMessage} />;
  }
  if (!isAuthenticated) {
    return <Navigate to="/login" replace />;
  }
  return <Outlet />;
}

export function AppRouter() {
  return (
    <Suspense fallback={<LoadingBlock />}>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route element={<ProtectedRoute />}>
          <Route element={<AppLayout />}>
            <Route path="/" element={<Navigate to="/dashboard" replace />} />
            <Route path="/dashboard" element={<DashboardPage />} />
            <Route path="/cases" element={<CasesPage />} />
            <Route path="/cases/new" element={<CaseWorkbenchPage />} />
            <Route path="/cases/:caseId/edit" element={<CaseWorkbenchPage />} />
            <Route path="/executions" element={<ExecutionsPage />} />
            <Route path="/corrections" element={<CorrectionsPage />} />
            <Route path="/settings/ai" element={<AISettingsPage />} />
            <Route path="/executions/:executionId" element={<ExecutionDetailPage />} />
            <Route path="/reports" element={<ReportCenterPage />} />
          </Route>
        </Route>
      </Routes>
    </Suspense>
  );
}
