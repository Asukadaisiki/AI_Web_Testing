import {
  CaseSchema,
  ExecutionResultSchema,
  type AgentTrace,
} from "./contracts";

export function screenshotPathsForRun(trace: AgentTrace, runId: string) {
  return [
    ...new Set(
      trace.operation_evidence.flatMap(
        (operation) => operation.artifacts?.screenshot_paths ?? [],
      ),
    ),
  ].filter(
    (path) =>
      path.startsWith(`${runId}/`) &&
      !/[\x00-\x1f\\?#%]/.test(path) &&
      path
        .split("/")
        .every((part) => part !== "" && part !== "." && part !== ".."),
  );
}

export function readBrowserEvidence(trace: AgentTrace) {
  const operations = trace.spans.filter((span) => span.kind === "operation");
  const cases = operations.flatMap((span) => {
    const input = span.input;
    const candidate =
      input && typeof input === "object" && "case" in input
        ? input.case
        : input;
    const parsed = CaseSchema.safeParse(candidate);
    return parsed.success ? [{ id: span.id, case: parsed.data }] : [];
  });
  const dryRuns = trace.operation_evidence.flatMap((operation) => {
    const parsed = ExecutionResultSchema.safeParse(operation.result);
    return parsed.success ? [{ id: operation.id, dryRun: parsed.data }] : [];
  });
  return { cases, dryRuns, operations };
}
