import { Suspense, lazy } from "react";
import { Navigate, Route, Routes, useParams } from "react-router-dom";

import { LoadingBlock } from "../components/PageFeedback";

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

function LegacyExecutionRedirect() {
  const { executionId } = useParams<{ executionId: string }>();
  return <Navigate to={`/run/${executionId}`} replace />;
}

export function AppRouter() {
  return (
    <Suspense fallback={<LoadingBlock />}>
      <Routes>
        <Route path="/" element={<PlanningPage />} />
        <Route path="/cases" element={<CasesPage />} />
        <Route path="/cases/:caseId/edit" element={<CaseEditPage />} />
        <Route path="/reports" element={<ReportPage />} />
        <Route path="/run/:executionId" element={<ExecutionDetailPage />} />
        <Route path="/executions/:executionId" element={<LegacyExecutionRedirect />} />
        <Route path="/dashboard" element={<Navigate to="/" replace />} />
        <Route path="/executions" element={<Navigate to="/cases" replace />} />
        <Route path="/login" element={<Navigate to="/" replace />} />
      </Routes>
    </Suspense>
  );
}
