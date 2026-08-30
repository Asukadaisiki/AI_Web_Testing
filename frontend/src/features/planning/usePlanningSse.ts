import { useCallback } from "react";

import { usePlanningWorkspace } from "./planningWorkspaceStore";
import type { PlanningStreamKind } from "./planningWorkspaceStore";
import type { ExecutionStreamEvent } from "../../types/api";

export function usePlanningSse() {
  const workspace = usePlanningWorkspace();

  const abort = useCallback(() => {
    workspace.abortStream();
  }, [workspace]);

  const run = useCallback(
    (sessionId: number, kind: PlanningStreamKind, messageId: number, options: {
      url: string;
      body: Record<string, unknown>;
      onEvent?: (event: ExecutionStreamEvent) => void;
    }) => workspace.runStream(sessionId, kind, messageId, options),
    [workspace],
  );

  return { abort, run };
}
