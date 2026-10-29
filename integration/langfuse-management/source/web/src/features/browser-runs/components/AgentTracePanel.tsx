import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/src/components/ui/card";
import { BrowserEvidencePanel } from "./BrowserEvidencePanel";
import { api } from "@/src/utils/api";

export function AgentTracePanel({
  projectId,
  runId,
}: {
  projectId: string;
  runId: string;
}) {
  const run = api.browserRuns.get.useQuery(
    { projectId, runId },
    { retry: false },
  );
  const trace = api.browserRuns.trace.useQuery(
    { projectId, runId },
    { enabled: run.isSuccess, retry: false },
  );

  if (run.isError) return null;

  return (
    <aside className="w-full max-w-md overflow-y-auto border-l p-4">
      <Card>
        <CardHeader>
          <CardTitle>AgentTrace evidence</CardTitle>
        </CardHeader>
        <CardContent>
          {run.data ? (
            <BrowserEvidencePanel
              projectId={projectId}
              runId={runId}
              trace={trace.data}
              isLoading={trace.isPending}
              error={
                trace.isError
                  ? "Unable to load canonical evidence. Try again later."
                  : undefined
              }
            />
          ) : (
            <p className="text-muted-foreground text-sm">
              Loading browser evidence...
            </p>
          )}
        </CardContent>
      </Card>
    </aside>
  );
}
