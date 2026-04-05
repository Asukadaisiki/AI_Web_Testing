import { Suspense, lazy } from "react";
import { Navigate, Route, Routes, useParams } from "react-router-dom";

import { ErrorBlock } from "../components/PageFeedback";
import { AppLayout } from "../layouts/AppLayout";
import { LoadingBlock } from "../components/PageFeedback";

const PlanningPage = lazy(() =>
  import("../pages/PlanningPage").then((module) => ({ default: module.PlanningPage })),
);
const CasesPage = lazy(() =>
  import("../pages/CasesPage").then((module) => ({ default: module.CasesPage })),
);
const CaseWorkbenchPage = lazy(() =>
  import("../pages/CaseWorkbenchPage").then((module) => ({ default: module.CaseWorkbenchPage })),
);
const ExecutionDetailPage = lazy(() =>
  import("../pages/ExecutionDetailPage").then((module) => ({ default: module.ExecutionDetailPage })),
);

function LegacyExecutionRedirect() {
  const { executionId } = useParams<{ executionId: string }>();
  return <Navigate to={`/run/${executionId}`} replace />;
}

export function AppRouter() {
  return (
    <Suspense fallback={<LoadingBlock />}>
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
    </Suspense>
  );
}
