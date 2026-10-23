// @vitest-environment jsdom

import '@testing-library/jest-dom/vitest';
import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import type {
  OperationEvidence,
  Span,
  SpanAttempt,
  UsageRecord,
} from '../../api';
import type { TraceNode } from './projectTrace';
import { TraceDetails } from './TraceDetails';

const span: Span = {
  id: 'span-row-1',
  run_id: 'run-1',
  trace_id: 'trace-1',
  span_id: 'operation-1',
  kind: 'operation',
  name: 'browser_execute_case',
  status: 'failed',
  input: { case: { name: 'checkout flow', steps: [{ action: 'click' }] } },
  output: null,
  started_at: '2026-10-21T12:00:00Z',
  ended_at: '2026-10-21T12:00:01Z',
  created_at: '2026-10-21T12:00:00Z',
  updated_at: '2026-10-21T12:00:01Z',
};

const node: TraceNode = {
  id: span.span_id,
  kind: span.kind,
  name: span.name,
  status: span.status,
  startedAt: span.started_at,
  endedAt: span.ended_at,
  source: { type: 'span', value: span },
  children: [],
};

const attempts: SpanAttempt[] = [{
  id: 'attempt-1',
  span_id: 'operation-1',
  attempt_number: 2,
  operation_id: 'operation-call-1',
  status: 'failed',
  error: { message: 'browser failed' },
  started_at: '2026-10-21T12:00:00Z',
  ended_at: '2026-10-21T12:00:01Z',
}];

const usage: UsageRecord[] = [{
  id: 'usage-1',
  span_id: 'operation-1',
  span_attempt_id: 'attempt-1',
  provider: 'openai-compatible',
  model: 'fixture',
  input_tokens: 12,
  output_tokens: 8,
  total_tokens: 20,
  cost_micros: 44,
  details: { cached_tokens: 2 },
  created_at: '2026-10-21T12:00:01Z',
}];

const operationEvidence: OperationEvidence[] = [{
  id: 'evidence-1',
  span_id: 'operation-1',
  span_attempt_id: 'attempt-1',
  operation_id: 'operation-call-1',
  status: 'failed',
  result: { steps: [{ index: 0, status: 'failed' }] },
  artifacts: [{ kind: 'screenshot', path: 'failure.png' }],
  error: { message: 'browser failed' },
  created_at: '2026-10-21T12:00:01Z',
  updated_at: '2026-10-21T12:00:01Z',
}];

describe('canonical trace details', () => {
  it('projects attempts usage execution evidence planner case and feedback', () => {
    render(
      <TraceDetails
        node={node}
        attempts={attempts}
        usage={usage}
        operationEvidence={operationEvidence}
      />,
    );

    expect(screen.getByText('Attempt 2')).toBeInTheDocument();
    expect(screen.getByText(/20 total tokens/)).toBeInTheDocument();
    expect(screen.getByText(/failure\.png/)).toBeInTheDocument();
    expect(screen.getByText(/checkout flow/)).toBeInTheDocument();
    expect(screen.getAllByText(/browser failed/).length).toBeGreaterThan(0);
    expect(screen.queryByText(/not exposed/i)).not.toBeInTheDocument();
  });
});
