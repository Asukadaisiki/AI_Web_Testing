import type { JSONValue, Run, Span } from '../../api';

interface Props {
  run: Run;
  spans?: Span[];
}

function record(value: JSONValue | undefined): Record<string, JSONValue> | null {
  return typeof value === 'object' && value !== null && !Array.isArray(value) ? value : null;
}

export function RunExecutionSummary({ run, spans = [] }: Props): JSX.Element | null {
  const input = record(spans.find((span) => span.run_id === run.id && span.kind === 'run')?.input);
  const snapshot = record(input?.['execution']);
  const harness = record(snapshot?.['harness']);
  const hasRunIdentity = run.runtime_id !== undefined || run.harness_id !== undefined;
  const runtimeId = hasRunIdentity ? run.runtime_id : snapshot?.['runtime_id'];
  const harnessLabel = hasRunIdentity ? run.harness_id : harness?.['name'] ?? harness?.['id'];
  const revision = hasRunIdentity ? run.harness_revision : harness?.['revision'];
  if (typeof runtimeId !== 'string' && typeof harnessLabel !== 'string') return null;

  return (
    <div className="run-execution-summary" aria-label="Run composition">
      {typeof runtimeId === 'string' ? <span>Runtime <code>{runtimeId}</code></span> : null}
      {typeof harnessLabel === 'string' ? (
        <span>Harness <code>{harnessLabel}</code>{typeof revision === 'number' ? ` (r${String(revision)})` : ''}</span>
      ) : null}
    </div>
  );
}
