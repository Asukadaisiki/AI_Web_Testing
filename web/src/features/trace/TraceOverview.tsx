import type { OperationEvidence, Run, UsageRecord } from '../../api';

interface Props {
  run: Run | null;
  usage: UsageRecord[];
  operationEvidence: OperationEvidence[];
}

const passedStatuses = new Set(['succeeded', 'passed', 'completed']);
const failedStatuses = new Set(['failed', 'error', 'cancelled']);

export function TraceOverview({
  run,
  usage,
  operationEvidence,
}: Props): JSX.Element | null {
  if (run === null) return null;

  const passed = operationEvidence.filter((operation) =>
    passedStatuses.has(operation.status),
  ).length;
  const failed = operationEvidence.filter((operation) =>
    failedStatuses.has(operation.status),
  ).length;
  const tokens = usage.reduce((total, record) => total + record.total_tokens, 0);
  const costMicros = usage.reduce((total, record) => total + record.cost_micros, 0);

  return (
    <section className="trace-overview" aria-label="Run report">
      <div>
        <span>Status</span>
        <strong>{run.status.replace('_', ' ')}</strong>
      </div>
      <div>
        <span>Execution</span>
        <strong>{operationEvidence.length} operations</strong>
        <small>{passed} passed · {failed} failed</small>
      </div>
      <div>
        <span>Usage</span>
        <strong>{tokens.toLocaleString()} tokens</strong>
        <small>${(costMicros / 1_000_000).toFixed(6)}</small>
      </div>
    </section>
  );
}
