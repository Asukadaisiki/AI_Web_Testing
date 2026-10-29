import { StatusBadge } from "@/src/components/layouts/status-badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/src/components/ui/table";
import type { AgentTraceRun } from "@/src/features/browser-runs/contracts";

export function RunList({
  runs,
  selectedRunId,
  onSelect,
}: {
  runs: AgentTraceRun[];
  selectedRunId: string | undefined;
  onSelect: (runId: string) => void;
}) {
  return (
    <div
      role="region"
      aria-label="Run table"
      className="w-full overflow-x-auto"
    >
      <Table className="min-w-[56rem]">
        <TableHeader>
          <TableRow>
            <TableHead>Run</TableHead>
            <TableHead>Status</TableHead>
            <TableHead>Runtime</TableHead>
            <TableHead>Harness Snapshot</TableHead>
            <TableHead>Updated</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {runs.map((run) => (
            <TableRow
              key={run.id}
              data-state={selectedRunId === run.id ? "selected" : undefined}
              className="cursor-pointer"
              onClick={() => onSelect(run.id)}
            >
              <TableCell className="font-mono text-xs break-all">
                {run.id}
              </TableCell>
              <TableCell>
                <StatusBadge type={run.status} />
              </TableCell>
              <TableCell>{run.runtime_id ?? "Not recorded"}</TableCell>
              <TableCell className="font-mono text-xs break-all">
                {run.harness_id
                  ? `${run.harness_id} (revision ${run.harness_revision ?? "unknown"})`
                  : "Not recorded"}
              </TableCell>
              <TableCell>{new Date(run.updated_at).toLocaleString()}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}
