// @vitest-environment jsdom

import '@testing-library/jest-dom/vitest';
import { render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import type {
  DerivedTraceNode,
  OperationEvidence,
  Span,
  SpanAttempt,
  UsageRecord,
} from '../../api';
import type { WorkspaceState } from '../workspace/useWorkspace';
import { projectTrace } from './projectTrace';

vi.mock('./TraceDetails', () => ({
  TraceDetails: ({
    node,
    attempts,
    usage,
    operationEvidence,
  }: {
    node: { id: string } | null;
    attempts: SpanAttempt[];
    usage: UsageRecord[];
    operationEvidence: OperationEvidence[];
  }) => (
    <output aria-label="Trace detail evidence">
      {node?.id}: attempts {attempts.length}, usage {usage.length}, operation evidence {operationEvidence.length}
    </output>
  ),
}));

import { TracePane } from './TracePane';

const span: Span = {
  id: 'span-row-1',
  run_id: 'run-1',
  trace_id: 'trace-1',
  span_id: 'operation-1',
  kind: 'operation',
  name: 'browser_execute_case',
  status: 'succeeded',
  input: null,
  output: null,
  started_at: '2026-10-21T12:00:00Z',
  ended_at: '2026-10-21T12:00:03Z',
  created_at: '2026-10-21T12:00:00Z',
  updated_at: '2026-10-21T12:00:03Z',
};

const derived: DerivedTraceNode = {
  id: 'derived:operation-1:step:0',
  parent_span_id: span.span_id,
  source_evidence_id: 'evidence-1',
  kind: 'step',
  name: 'Step 1: goto',
  status: 'succeeded',
  started_at: '2026-10-21T12:00:01Z',
  ended_at: '2026-10-21T12:00:02Z',
  data: {},
  evidence: {},
};

const attempts: SpanAttempt[] = [{
  id: 'attempt-1',
  span_id: span.span_id,
  attempt_number: 1,
  status: 'succeeded',
  started_at: span.started_at,
  ended_at: span.ended_at,
}];

const usage: UsageRecord[] = [{
  id: 'usage-1',
  span_id: span.span_id,
  input_tokens: 10,
  output_tokens: 5,
  total_tokens: 15,
  cost_micros: 40,
  details: {},
  created_at: span.ended_at ?? span.started_at,
}];

const operationEvidence: OperationEvidence[] = [{
  id: 'evidence-1',
  span_id: span.span_id,
  operation_id: 'call-1',
  status: 'succeeded',
  artifacts: [],
  created_at: span.ended_at ?? span.started_at,
  updated_at: span.ended_at ?? span.started_at,
}];

function workspaceWithDerivedSelection(): WorkspaceState {
  const trace = projectTrace([span], [derived]);
  const selectedNode = trace[0]?.children[0] ?? null;

  const workspace = {
    selection: {
      projectId: 'project-1',
      conversationId: 'conversation-1',
      runId: 'run-1',
      spanId: derived.id,
    },
    run: null,
    spans: [span],
    derivedNodes: [derived],
    attempts,
    usage,
    operationEvidence,
    trace,
    selectedNode,
    connected: false,
    selectSpan: vi.fn(),
  };

  return workspace as unknown as WorkspaceState;
}

describe('TracePane', () => {
  it('withholds canonical evidence from derived selections and counts derived nodes', () => {
    render(<TracePane workspace={workspaceWithDerivedSelection()} />);

    expect(screen.getByLabelText('Trace detail evidence')).toHaveTextContent(
      `${derived.id}: attempts 0, usage 0, operation evidence 0`,
    );
    expect(screen.getByText('2 nodes')).toBeInTheDocument();
  });
});
