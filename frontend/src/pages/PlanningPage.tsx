import { Navigate, useParams } from "react-router-dom";
import { AgentWorkbench } from "../components/AgentWorkbench";
import { useQuery } from "@tanstack/react-query";
import { getAISettings } from "../features/planning/api";

export function PlanningPage() {
  const { sessionId } = useParams<{ sessionId: string }>();
  const aiSettingsQuery = useQuery({ queryKey: ["ai-settings"], queryFn: getAISettings });

  if (!sessionId) {
    return <Navigate to="/planning" replace />;
  }

  return (
    <AgentWorkbench
      aiSettings={aiSettingsQuery.data ?? null}
      sessionId={Number(sessionId)}
    />
  );
}
