import { Button } from "@/src/components/ui/button";
import type { AgentTraceRun } from "@/src/features/browser-runs/contracts";

const terminalStatuses: AgentTraceRun["status"][] = [
  "succeeded",
  "failed",
  "cancelled",
];

export function RunActions({
  status,
  onCancel,
  onRerun,
  cancelPending = false,
  rerunPending = false,
  error,
  canExecute = true,
  canRerun = true,
}: {
  status: AgentTraceRun["status"];
  onCancel: () => void;
  onRerun: () => void;
  cancelPending?: boolean;
  rerunPending?: boolean;
  error?: string;
  canExecute?: boolean;
  canRerun?: boolean;
}) {
  const pending = cancelPending || rerunPending;
  return (
    <div className="flex flex-wrap items-center gap-2">
      {!terminalStatuses.includes(status) ? (
        <Button
          aria-label="Cancel run"
          size="sm"
          variant="outline"
          onClick={onCancel}
          disabled={!canExecute || pending || status === "cancelling"}
          loading={cancelPending}
        >
          Cancel run
        </Button>
      ) : null}
      {status === "failed" ? (
        <Button
          aria-label="Rerun"
          size="sm"
          variant="outline"
          disabled={!canExecute || !canRerun || pending}
          loading={rerunPending}
          onClick={() => onRerun()}
        >
          Rerun
        </Button>
      ) : null}
      {error ? (
        <p role="alert" className="text-destructive text-sm">
          {error}
        </p>
      ) : null}
    </div>
  );
}
