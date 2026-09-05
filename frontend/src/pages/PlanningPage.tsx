import { Navigate, useParams } from "react-router-dom";
import { AgentWorkbench } from "../components/AgentWorkbench";

export function PlanningPage() {
  const { sessionId } = useParams<{ sessionId: string }>();

  if (!sessionId) {
    return <Navigate to="/planning" replace />;
  }

  return (
    <AgentWorkbench sessionId={Number(sessionId)} />
  );
}
