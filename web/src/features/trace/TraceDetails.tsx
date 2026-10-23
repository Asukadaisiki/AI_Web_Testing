import {
  Braces,
  Clock3,
  Database,
  FileText,
  Gauge,
  MessageSquareWarning,
  Paperclip,
} from 'lucide-react';
import type {
  OperationEvidence,
  SpanAttempt,
  UsageRecord,
} from '../../api';
import type { TraceNode } from './projectTrace';

interface Props {
  node: TraceNode | null;
  attempts: SpanAttempt[];
  usage: UsageRecord[];
  operationEvidence: OperationEvidence[];
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
  if (node.endedAt === null || node.endedAt === undefined) return 'In progress';
  const milliseconds =
    new Date(node.endedAt).getTime() - new Date(node.startedAt).getTime();
  return milliseconds >= 1000
    ? `${(milliseconds / 1000).toFixed(2)} s`
    : `${String(Math.max(milliseconds, 0))} ms`;
}

function formatUsage(record: UsageRecord): string {
  const cost = (record.cost_micros / 1_000_000).toFixed(6);
  const model = [record.provider, record.model].filter(Boolean).join(' / ');
  return `${record.total_tokens.toLocaleString()} total tokens (${record.input_tokens.toLocaleString()} input, ${record.output_tokens.toLocaleString()} output) · $${cost}${model === '' ? '' : ` · ${model}`}`;
}

export function TraceDetails({
  node,
  attempts,
  usage,
  operationEvidence,
}: Props): JSX.Element {
  if (node === null) {
    return (
      <div className="trace-detail-empty">
        Select a Trace node to inspect its details.
      </div>
    );
  }

  if (node.source.type === 'derived') {
    const derived = node.source.value;
    const dataLabel = node.kind === 'step' ? 'Step data' : 'Tool data';

    return (
      <article className="trace-details" aria-label={`Trace details for ${node.name}`}>
        <header>
          <p className="eyebrow">{node.kind}</p>
          <h3>{node.name}</h3>
          <code>{node.id}</code>
        </header>

        <dl className="detail-grid">
          <div>
            <dt>Source evidence ID</dt>
            <dd>{derived.source_evidence_id}</dd>
          </div>
          <div>
            <dt>Status</dt>
            <dd>{node.status}</dd>
          </div>
          <div>
            <dt>Duration</dt>
            <dd>{duration(node)}</dd>
          </div>
          <div>
            <dt>Canonical parent</dt>
            <dd>{derived.parent_span_id}</dd>
          </div>
        </dl>

        <details open>
          <summary>
            <Braces size={14} />
            {dataLabel}
          </summary>
          <pre>{formatJSON(derived.data)}</pre>
        </details>

        <details open>
          <summary>
            <Paperclip size={14} />
            Evidence
          </summary>
          <pre>{formatJSON(derived.evidence)}</pre>
        </details>
      </article>
    );
  }

  const span = node.source.value;

  return (
    <article className="trace-details" aria-label={`Trace details for ${node.name}`}>
      <header>
        <p className="eyebrow">{node.kind}</p>
        <h3>{node.name}</h3>
        <code>{node.id}</code>
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
          <dd>{new Date(node.startedAt).toLocaleString()}</dd>
        </div>
        <div>
          <dt>Ended</dt>
          <dd>
            {node.endedAt === null || node.endedAt === undefined
              ? 'In progress'
              : new Date(node.endedAt).toLocaleString()}
          </dd>
        </div>
      </dl>

      <details open>
        <summary>
          <Database size={14} />
          Attempt
        </summary>
        {attempts.length === 0 ? (
          <p className="detail-note">No attempt recorded for this span.</p>
        ) : (
          <div className="detail-records">
            {attempts.map((attempt) => (
              <div key={attempt.id}>
                <strong>Attempt {attempt.attempt_number}</strong>
                <pre>{formatJSON({
                  status: attempt.status,
                  provider: attempt.provider,
                  operation_id: attempt.operation_id,
                  started_at: attempt.started_at,
                  ended_at: attempt.ended_at,
                  error: attempt.error,
                })}</pre>
              </div>
            ))}
          </div>
        )}
      </details>

      <details>
        <summary>
          <Gauge size={14} />
          Usage
        </summary>
        {usage.length === 0 ? (
          <p className="detail-note">No usage attributed to this span.</p>
        ) : (
          <div className="detail-records">
            {usage.map((record) => (
              <div key={record.id}>
                <strong>{formatUsage(record)}</strong>
                <pre>{formatJSON(record.details)}</pre>
              </div>
            ))}
          </div>
        )}
      </details>

      <details>
        <summary>
          <Paperclip size={14} />
          Execution evidence
        </summary>
        {operationEvidence.length === 0 ? (
          <p className="detail-note">No operation evidence recorded for this span.</p>
        ) : (
          <div className="detail-records">
            {operationEvidence.map((evidence) => (
              <div key={evidence.id}>
                <strong>{evidence.operation_id} · {evidence.status}</strong>
                <pre>{formatJSON({
                  result: evidence.result,
                  artifacts: evidence.artifacts,
                })}</pre>
              </div>
            ))}
          </div>
        )}
      </details>

      <details>
        <summary>
          <FileText size={14} />
          Planner / case input
        </summary>
        <pre>{formatJSON(span.input)}</pre>
      </details>

      <details>
        <summary>
          <MessageSquareWarning size={14} />
          Failure feedback
        </summary>
        {operationEvidence.every((evidence) => evidence.error === undefined) &&
        attempts.every((attempt) => attempt.error === undefined) ? (
          <p className="detail-note">No failure feedback recorded for this span.</p>
        ) : (
          <pre>{formatJSON({
            operation_errors: operationEvidence.flatMap((evidence) =>
              evidence.error === undefined ? [] : [evidence.error],
            ),
            attempt_errors: attempts.flatMap((attempt) =>
              attempt.error === undefined ? [] : [attempt.error],
            ),
          })}</pre>
        )}
      </details>

      <details>
        <summary>
          <Braces size={14} />
          Canonical output
        </summary>
        <pre>{formatJSON(span.output)}</pre>
      </details>

      <div className="detail-updated">
        <Clock3 size={13} />
        Updated {new Date(span.updated_at).toLocaleString()}
      </div>
    </article>
  );
}
