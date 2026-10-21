import { Braces, Clock3, Database, Gauge, Paperclip } from 'lucide-react';
import type { TraceNode } from './projectTrace';

interface Props {
  node: TraceNode | null;
}

function asRecord(value: unknown): Record<string, unknown> | null {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;
}

function findField(value: unknown, names: readonly string[]): unknown {
  const source = asRecord(value);
  if (source === null) return null;
  for (const [key, candidate] of Object.entries(source)) {
    if (names.some((name) => name.toLocaleLowerCase() === key.toLocaleLowerCase())) {
      return candidate;
    }
  }
  return null;
}

function formatJSON(value: unknown): string {
  if (value === null || value === undefined) return 'Not recorded';
  if (typeof value === 'string') return value;
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}

function duration(node: TraceNode): string {
  if (node.ended_at === null || node.ended_at === undefined) return 'In progress';
  const milliseconds =
    new Date(node.ended_at).getTime() - new Date(node.started_at).getTime();
  return milliseconds >= 1000
    ? `${(milliseconds / 1000).toFixed(2)} s`
    : `${String(Math.max(milliseconds, 0))} ms`;
}

export function TraceDetails({ node }: Props): JSX.Element {
  if (node === null) {
    return (
      <div className="trace-detail-empty">
        Select a Trace node to inspect its canonical data.
      </div>
    );
  }

  const usage =
    findField(node.output, ['usage']) ?? findField(node.input, ['usage']);
  const attempts =
    findField(node.output, ['attempt', 'attempts']) ??
    findField(node.input, ['attempt', 'attempts']);
  const evidence =
    findField(node.output, ['evidence', 'artifacts']) ??
    findField(node.input, ['evidence', 'artifacts']);

  return (
    <article className="trace-details" aria-label={`Trace details for ${node.name}`}>
      <header>
        <p className="eyebrow">{node.kind}</p>
        <h3>{node.name}</h3>
        <code>{node.span_id}</code>
      </header>

      <dl className="detail-grid">
        <div>
          <dt>Status</dt>
          <dd>{node.status}</dd>
        </div>
        <div>
          <dt>Duration</dt>
          <dd>{duration(node)}</dd>
        </div>
        <div>
          <dt>Started</dt>
          <dd>{new Date(node.started_at).toLocaleString()}</dd>
        </div>
        <div>
          <dt>Ended</dt>
          <dd>
            {node.ended_at === null || node.ended_at === undefined
              ? 'In progress'
              : new Date(node.ended_at).toLocaleString()}
          </dd>
        </div>
      </dl>

      <details open>
        <summary>
          <Database size={14} />
          Attempt
        </summary>
        {attempts === null ? (
          <p className="detail-note">
            Attempt records are not exposed by the current trace API.
          </p>
        ) : (
          <pre>{formatJSON(attempts)}</pre>
        )}
      </details>

      <details>
        <summary>
          <Gauge size={14} />
          Usage
        </summary>
        <pre>{formatJSON(usage)}</pre>
      </details>

      <details>
        <summary>
          <Paperclip size={14} />
          Evidence
        </summary>
        <pre>{formatJSON(evidence)}</pre>
      </details>

      <details>
        <summary>
          <Braces size={14} />
          Input
        </summary>
        <pre>{formatJSON(node.input)}</pre>
      </details>

      <details>
        <summary>
          <Braces size={14} />
          Output
        </summary>
        <pre>{formatJSON(node.output)}</pre>
      </details>

      <div className="detail-updated">
        <Clock3 size={13} />
        Updated {new Date(node.updated_at).toLocaleString()}
      </div>
    </article>
  );
}
