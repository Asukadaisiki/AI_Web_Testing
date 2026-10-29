import { useRef, useState } from "react";
import Link from "next/link";
import { Alert, AlertDescription, AlertTitle } from "@/src/components/ui/alert";
import { Button } from "@/src/components/ui/button";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/src/components/ui/card";
import { NewRunDialog } from "../components/NewRunDialog";
import { ManagementShell } from "./ManagementShell";
import { RunActions } from "../components/RunActions";
import { RunList } from "../components/RunList";
import { BrowserEvidencePanel } from "../components/BrowserEvidencePanel";
import { RunSnapshotPanel } from "../components/RunSnapshotPanel";
import { useRunEvents } from "../hooks/useRunEvents";
import { useHasProjectAccess } from "@/src/features/rbac/utils/checkProjectAccess";
import { api } from "@/src/utils/api";
import { safeRandomUUID } from "@/src/utils/safe-random-uuid";

const isTerminal = (status: string) =>
  ["succeeded", "failed", "cancelled"].includes(status);

export function BrowserRunsPage({ projectId }: { projectId: string }) {
  return <ManagementShell key={projectId} projectId={projectId} />;
}

export function RunWorkspace({
  projectId,
  conversationId,
  conversationActive,
  environmentReady,
}: {
  projectId: string;
  conversationId: string;
  conversationActive: boolean;
  environmentReady: boolean;
}) {
  const [selectedRunId, setSelectedRunId] = useState<string>();
  const unresolvedReruns = useRef(
    new Map<
      string,
      { projectId: string; runId: string; idempotencyKey: string }
    >(),
  );
  const canExecute = useHasProjectAccess({
    projectId,
    scope: "playground:execute",
  });
  const utils = api.useUtils();
  const enabled = Boolean(projectId);
  const runs = api.browserRuns.list.useQuery(
    { projectId, conversationId },
    { enabled, retry: false },
  );
  const runtimes = api.browserRuns.runtimes.useQuery(
    { projectId },
    { enabled, retry: false },
  );
  const harnesses = api.browserRuns.harnesses.useQuery(
    { projectId },
    { enabled, retry: false },
  );
  const run = api.browserRuns.get.useQuery(
    { projectId, runId: selectedRunId ?? "" },
    { enabled: enabled && Boolean(selectedRunId), retry: false },
  );
  const selectedRun =
    run.data ?? runs.data?.find((item) => item.id === selectedRunId);
  const trace = api.browserRuns.trace.useQuery(
    { projectId, runId: selectedRunId ?? "" },
    { enabled: enabled && Boolean(selectedRunId), retry: false },
  );
  const refresh = () =>
    Promise.all([
      utils.browserRuns.list.invalidate({ projectId, conversationId }),
      ...(selectedRunId
        ? [
            utils.browserRuns.get.invalidate({
              projectId,
              runId: selectedRunId,
            }),
            utils.browserRuns.trace.invalidate({
              projectId,
              runId: selectedRunId,
            }),
          ]
        : []),
    ]);
  const activeRun =
    selectedRun && !isTerminal(selectedRun.status)
      ? selectedRun
      : runs.data?.find((item) => !isTerminal(item.status));
  const runEvents = useRunEvents({
    projectId,
    runId: activeRun?.id,
    status: activeRun?.status,
    onRefresh: refresh,
  });

  const create = api.browserRuns.create.useMutation({
    onSuccess: (result) => {
      setSelectedRunId(result.run_id);
      refresh();
    },
  });
  const cancel = api.browserRuns.cancel.useMutation({ onSuccess: refresh });
  const rerun = api.browserRuns.rerun.useMutation({
    onSuccess: (result, input) => {
      // Only confirmed acceptance retires an intent, even after selection changes.
      for (const [identity, unresolved] of unresolvedReruns.current) {
        if (
          unresolved.projectId === input.projectId &&
          unresolved.runId === input.runId &&
          unresolved.idempotencyKey === input.idempotencyKey
        ) {
          unresolvedReruns.current.delete(identity);
        }
      }
      setSelectedRunId(result.run_id);
      refresh();
    },
  });
  const catalogError = runtimes.isError || harnesses.isError;
  const selectRun = (id: string) => {
    cancel.reset();
    rerun.reset();
    setSelectedRunId(id);
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <h3 className="text-base font-bold">Conversation runs</h3>
          <p className="text-muted-foreground text-sm">
            Select an independent Runtime and a published Harness version.
            {!conversationActive
              ? " This archived conversation is read-only."
              : !environmentReady
                ? " Waiting for a ready browser environment."
                : ""}
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <NewRunDialog
            conversationId={conversationId}
            isLoading={create.isPending}
            runtimes={runtimes.data}
            harnesses={harnesses.data}
            catalogLoading={runtimes.isPending || harnesses.isPending}
            catalogError={
              catalogError
                ? "Runtime or Harness catalog unavailable. Check the configured bridge and try again."
                : undefined
            }
            canExecute={
              canExecute &&
              runs.isSuccess &&
              conversationActive &&
              environmentReady
            }
            error={
              create.isError
                ? "Run submission failed. Check catalog readiness and retry the unchanged draft."
                : undefined
            }
            onCreate={(input, onSuccess) =>
              create.mutate({ projectId, ...input }, { onSuccess })
            }
          />
        </div>
      </div>
      {runs.isError || catalogError ? (
        <Alert variant="destructive">
          <AlertTitle>Agent runs are unavailable</AlertTitle>
          <AlertDescription>
            Check the configured project bridge and try again.
            <Button
              variant="outline"
              size="sm"
              onClick={() =>
                Promise.all([
                  runs.refetch(),
                  runtimes.refetch(),
                  harnesses.refetch(),
                ])
              }
            >
              Retry
            </Button>
          </AlertDescription>
        </Alert>
      ) : null}
      <Card>
        <CardHeader>
          <CardTitle>Runs</CardTitle>
        </CardHeader>
        <CardContent>
          {runs.isPending ? (
            <p role="status">Loading Runs...</p>
          ) : runs.data?.length ? (
            <RunList
              runs={runs.data}
              selectedRunId={selectedRunId}
              onSelect={selectRun}
            />
          ) : !runs.isError ? (
            <p className="text-muted-foreground text-sm">No agent runs yet.</p>
          ) : null}
        </CardContent>
      </Card>
      {selectedRunId && run.isError ? (
        <Alert variant="destructive">
          <AlertTitle>Run refresh unavailable</AlertTitle>
          <AlertDescription>
            Unable to refresh durable state. Try again.
            <Button variant="outline" size="sm" onClick={refresh}>
              Refresh Run
            </Button>
          </AlertDescription>
        </Alert>
      ) : null}
      {runEvents.exhausted ? (
        <Alert>
          <AlertTitle>Live updates paused</AlertTitle>
          <AlertDescription>
            Automatic recovery for Run {activeRun?.id} is exhausted. Reconnect
            to refresh durable state and resume bounded updates.
            <Button variant="outline" size="sm" onClick={runEvents.reconnect}>
              Reconnect updates
            </Button>
          </AlertDescription>
        </Alert>
      ) : null}
      {selectedRun ? (
        <Card>
          <CardHeader className="flex-row flex-wrap items-center justify-between gap-3">
            <div className="flex flex-col gap-1">
              <CardTitle>Run {selectedRun.id}</CardTitle>
              <p className="text-sm">Durable status: {selectedRun.status}</p>
              <p className="text-muted-foreground text-xs break-all">
                {selectedRun.runtime_id ?? "Runtime not recorded"} /{" "}
                {selectedRun.harness_id ?? "Harness not recorded"} / revision{" "}
                {selectedRun.harness_revision ?? "not recorded"}
              </p>
            </div>
            <RunActions
              key={`${selectedRun.id}:${selectedRun.harness_revision ?? "historical"}`}
              status={selectedRun.status}
              canExecute={canExecute && !run.isError}
              canRerun={conversationActive && environmentReady}
              cancelPending={cancel.isPending}
              rerunPending={rerun.isPending}
              error={
                cancel.isError
                  ? "Cancellation failed. Refresh state and try again."
                  : rerun.isError
                    ? "Rerun failed. Retry to reuse the same request key."
                    : undefined
              }
              onCancel={() =>
                cancel.mutate({ projectId, runId: selectedRun.id })
              }
              onRerun={() => {
                const identity = JSON.stringify([
                  projectId,
                  selectedRun.id,
                  selectedRun.harness_revision ?? null,
                ]);
                const input = unresolvedReruns.current.get(identity) ?? {
                  projectId,
                  runId: selectedRun.id,
                  idempotencyKey: safeRandomUUID(),
                };
                unresolvedReruns.current.set(identity, input);
                rerun.mutate(input);
              }}
            />
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            {selectedRun.retry_of_run_id ? (
              <p className="text-sm">Retry of {selectedRun.retry_of_run_id}</p>
            ) : null}
            <RunSnapshotPanel run={selectedRun} />
            <Button asChild variant="outline" className="self-start">
              <Link
                href={`/project/${projectId}/sessions/${encodeURIComponent(`agenttrace-session-${selectedRun.conversation_id}`)}`}
              >
                View Langfuse Session
              </Link>
            </Button>
            <NativeTraceLink
              key={selectedRun.id}
              projectId={projectId}
              runId={selectedRun.id}
              terminal={isTerminal(selectedRun.status)}
            />
            <BrowserEvidencePanel
              projectId={projectId}
              runId={selectedRun.id}
              trace={trace.data}
              isLoading={trace.isPending}
              error={
                trace.isError
                  ? "Unable to load canonical evidence. Refresh and try again."
                  : undefined
              }
            />
          </CardContent>
        </Card>
      ) : selectedRunId && run.isPending ? (
        <p role="status">Loading selected Run...</p>
      ) : null}
    </div>
  );
}

function NativeTraceLink({
  projectId,
  runId,
  terminal,
}: {
  projectId: string;
  runId: string;
  terminal: boolean;
}) {
  const traceId = `agenttrace-trace-${runId}`;
  const exported = api.traces.byId.useQuery(
    { projectId, traceId, verbosity: "compact" },
    {
      enabled: terminal,
      retry: false,
      refetchInterval: (query) =>
        query.state.data || query.state.errorUpdateCount >= 60 ? false : 5000,
    },
  );
  const missing = exported.error?.data?.code === "NOT_FOUND";
  return (
    <div className="flex flex-col items-start gap-2">
      <Button asChild variant="outline">
        <Link
          href={`/project/${projectId}/traces/${encodeURIComponent(traceId)}`}
        >
          View Langfuse Trace
        </Link>
      </Button>
      <p className="text-muted-foreground text-sm">
        {exported.data
          ? "Native Trace exported."
          : !terminal
            ? "Native Trace export follows Run completion."
            : exported.isError && !missing
              ? "Native Trace availability could not be checked."
              : "Native Trace export is syncing or unavailable; durable Run status is unchanged."}
      </p>
    </div>
  );
}
