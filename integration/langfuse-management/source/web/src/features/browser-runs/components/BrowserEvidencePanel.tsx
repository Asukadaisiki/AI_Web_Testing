import { Alert, AlertDescription, AlertTitle } from "@/src/components/ui/alert";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/src/components/ui/card";
import { JSONView } from "@/src/components/ui/CodeJsonViewer";
import { ResizableImage } from "@/src/components/ui/resizable-image";
import type { AgentTrace } from "../contracts";
import { readBrowserEvidence, screenshotPathsForRun } from "../trace-evidence";

export function BrowserEvidencePanel({
  projectId,
  runId,
  trace,
  isLoading,
  error,
}: {
  projectId: string;
  runId: string;
  trace: AgentTrace | undefined;
  isLoading?: boolean;
  error?: string;
}) {
  if (!trace)
    return (
      <Alert variant={error ? "destructive" : "default"}>
        <AlertTitle>
          {error
            ? "Canonical evidence unavailable"
            : isLoading
              ? "Loading canonical evidence..."
              : "Canonical evidence not available"}
        </AlertTitle>
        <AlertDescription>
          {error ??
            "Browser evidence is optional. This does not indicate native Langfuse Trace export status."}
        </AlertDescription>
      </Alert>
    );
  const evidence = readBrowserEvidence(trace);
  const screenshots = screenshotPathsForRun(trace, runId);
  return (
    <div className="flex flex-col gap-4">
      {error ? (
        <p role="alert" className="text-destructive text-sm">
          {error}
        </p>
      ) : null}
      <Card>
        <CardHeader>
          <CardTitle>Case evidence</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          {!evidence.cases.length ? (
            <p className="text-muted-foreground text-sm">
              Case evidence not available.
            </p>
          ) : null}
          {evidence.cases.map((item) => (
            <div key={item.id} data-testid="canonical-browser-case">
              <JSONView title={`Case ${item.id}`} json={item.case} />
            </div>
          ))}
          {!evidence.dryRuns.length ? (
            <p className="text-muted-foreground text-sm">
              Dry-run evidence not available.
            </p>
          ) : null}
          {evidence.dryRuns.map((item) => (
            <JSONView
              key={item.id}
              title={`Dry run ${item.dryRun.execution_id}`}
              json={item.dryRun}
            />
          ))}
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>Operation evidence</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          {!evidence.operations.length ? (
            <p className="text-muted-foreground text-sm">
              No operation evidence recorded.
            </p>
          ) : null}
          {evidence.operations.map((span) => (
            <div key={span.id} className="flex flex-col gap-3">
              <JSONView
                title={`${span.name} input`}
                json={span.input ?? null}
              />
              <JSONView
                title={`${span.name} output`}
                json={span.output ?? null}
              />
              {trace.operation_evidence
                .filter((operation) => operation.span_id === span.id)
                .map((operation) => (
                  <JSONView
                    key={operation.id}
                    title={`${span.name} result ${operation.id}`}
                    json={operation.result ?? null}
                  />
                ))}
            </div>
          ))}
        </CardContent>
      </Card>
      {screenshots.length ? (
        <Card>
          <CardHeader>
            <CardTitle>Screenshots</CardTitle>
          </CardHeader>
          <CardContent className="grid gap-3">
            {screenshots.map((path) => (
              <ResizableImage
                key={path}
                src={`/api/agenttrace/artifacts/${encodeURIComponent(runId)}?projectId=${encodeURIComponent(projectId)}&path=${encodeURIComponent(path)}`}
                alt={`Browser evidence ${path}`}
                isDefaultVisible
                shouldValidateImageSource={false}
              />
            ))}
          </CardContent>
        </Card>
      ) : null}
    </div>
  );
}
