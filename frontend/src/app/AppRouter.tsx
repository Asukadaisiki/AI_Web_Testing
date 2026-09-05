import { Suspense, lazy } from "react";
import { Navigate, Route, Routes, useParams } from "react-router-dom";

import { LoadingBlock } from "../shared/ui/PageFeedback";
import { AuthGuard } from "../features/auth/AuthGuard";

const SessionListPage = lazy(() =>
  import("../pages/SessionListPage").then((m) => ({ default: m.SessionListPage })),
);
const PlanningPage = lazy(() =>
  import("../pages/PlanningPage").then((m) => ({ default: m.PlanningPage })),
);
const CasesPage = lazy(() =>
  import("../pages/CasesPage").then((m) => ({ default: m.CasesPage })),
);
const ReportPage = lazy(() =>
  import("../pages/ReportPage").then((m) => ({ default: m.ReportPage })),
);
const ExecutionDetailPage = lazy(() =>
  import("../pages/ExecutionDetailPage").then((m) => ({ default: m.ExecutionDetailPage })),
);
const CaseEditPage = lazy(() =>
  import("../pages/CaseEditPage").then((m) => ({ default: m.CaseEditPage })),
);
const LoginPage = lazy(() =>
  import("../features/auth/LoginPage").then((m) => ({ default: m.LoginPage })),
);
const RegressionPage = lazy(() =>
  import("../pages/RegressionPage").then((m) => ({ default: m.RegressionPage })),
);
const LocatorDebugPage = lazy(() =>
  import("../pages/LocatorDebugPage").then((m) => ({ default: m.LocatorDebugPage })),
);
function LegacyExecutionRedirect() {
  const { executionId } = useParams<{ executionId: string }>();
  return <Navigate to={`/reports/${executionId}`} replace />;
}

export function AppRouter() {
  return (
    <Suspense fallback={<LoadingBlock />}>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route element={<AuthGuard />}>
          <Route path="/planning" element={<SessionListPage />} />
          <Route path="/planning/sessions/:sessionId" element={<PlanningPage />} />
          <Route path="/" element={<Navigate to="/planning" replace />} />
          <Route path="/cases" element={<CasesPage />} />
          <Route path="/cases/new" element={<CaseEditPage />} />
          <Route path="/cases/:caseId/edit" element={<CaseEditPage />} />
          <Route path="/regression" element={<RegressionPage />} />
          <Route path="/locator-debug" element={<LocatorDebugPage />} />
          <Route path="/reports" element={<ReportPage />} />
          <Route path="/reports/:executionId" element={<ExecutionDetailPage />} />
          <Route path="/run/:executionId" element={<LegacyExecutionRedirect />} />
          <Route path="/executions/:executionId" element={<LegacyExecutionRedirect />} />
          <Route path="/dashboard" element={<Navigate to="/planning" replace />} />
          <Route path="/executions" element={<Navigate to="/cases" replace />} />
        </Route>
      </Routes>
    </Suspense>
  );
}
