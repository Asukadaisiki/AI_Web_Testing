// @vitest-environment jsdom

import '@testing-library/jest-dom/vitest';
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import type {
  DerivedTraceNode,
  OperationEvidence,
  Span,
  SpanAttempt,
  UsageRecord,
} from '../../api';
import type { TraceNode } from './projectTrace';
import { TraceDetails } from './TraceDetails';

afterEach(cleanup);

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

function derivedNode(
  kind: DerivedTraceNode['kind'],
  name: string,
): TraceNode {
  const derived: DerivedTraceNode = {
    id: `derived:operation-1:${kind}:0`,
    parent_span_id: 'operation-1',
    source_evidence_id: `evidence-${kind}-1`,
    kind,
    name,
    status: 'succeeded',
    started_at: '2026-10-21T12:00:00Z',
    ended_at: '2026-10-21T12:00:01Z',
    data: {
      action: 'goto',
      custom_step_field: 'keep this',
    },
    evidence: {
      screenshot_path: 'artifacts/checkout.png',
      custom_evidence_field: 'also kept',
    },
  };

  return {
    id: derived.id,
    parentId: derived.parent_span_id,
    kind: derived.kind,
    name: derived.name,
    status: derived.status,
    startedAt: derived.started_at,
    endedAt: derived.ended_at,
    source: { type: 'derived', value: derived },
    children: [],
  };
}

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

describe('derived trace details', () => {
  it('renders started and ended timestamps for a completed derived node', () => {
    const step = derivedNode('step', 'Step 1: goto');
    render(
      <TraceDetails
        node={step}
        attempts={[]}
        usage={[]}
        operationEvidence={[]}
      />,
    );

    expect(screen.getByText('Started').parentElement).toHaveTextContent(
      new Date(step.startedAt).toLocaleString(),
    );
    expect(screen.getByText('Ended').parentElement).toHaveTextContent(
      new Date(step.endedAt ?? '').toLocaleString(),
    );
  });

  it('renders an in-progress end time for an unfinished derived node', () => {
    const inProgressStep = derivedNode('step', 'Step 2: click');
    inProgressStep.endedAt = null;
    if (inProgressStep.source.type === 'derived') {
      inProgressStep.source.value.ended_at = null;
    }

    render(
      <TraceDetails
        node={inProgressStep}
        attempts={[]}
        usage={[]}
        operationEvidence={[]}
      />,
    );

    expect(screen.getByText('Ended').parentElement).toHaveTextContent('In progress');
  });

  it('renders step data and evidence without canonical span panels', () => {
    const step = derivedNode('step', 'Step 1: goto');
    render(
      <TraceDetails
        node={step}
        attempts={attempts}
        usage={usage}
        operationEvidence={operationEvidence}
      />,
    );

    expect(screen.getByText(step.id)).toBeInTheDocument();
    expect(screen.getByText('evidence-step-1')).toBeInTheDocument();
    expect(screen.getByText('succeeded')).toBeInTheDocument();
    expect(screen.getByText('1.00 s')).toBeInTheDocument();
    expect(screen.getByText('Step data')).toBeInTheDocument();
    expect(screen.getByText('Evidence')).toBeInTheDocument();
    expect(screen.getByText(/artifacts\/checkout\.png/)).toBeInTheDocument();
    expect(screen.getByText(/custom_step_field/)).toBeInTheDocument();
    expect(screen.getByText(/custom_evidence_field/)).toBeInTheDocument();
    expect(screen.queryByText('Attempt')).not.toBeInTheDocument();
    expect(screen.queryByText('Usage')).not.toBeInTheDocument();
    expect(screen.queryByText('Execution evidence')).not.toBeInTheDocument();
    expect(screen.queryByText('Canonical output')).not.toBeInTheDocument();
  });

  it('renders unknown tools with their own data and evidence', () => {
    const unknownTool = derivedNode('unknown_tool', 'custom_browser_tool');
    render(
      <TraceDetails
        node={unknownTool}
        attempts={[]}
        usage={[]}
        operationEvidence={[]}
      />,
    );

    expect(screen.getByText(unknownTool.id)).toBeInTheDocument();
    expect(screen.getByText('evidence-unknown_tool-1')).toBeInTheDocument();
    expect(screen.getByText('Tool data')).toBeInTheDocument();
    expect(screen.getByText('Evidence')).toBeInTheDocument();
    expect(screen.queryByText('Attempt')).not.toBeInTheDocument();
    expect(screen.queryByText('Usage')).not.toBeInTheDocument();
    expect(screen.queryByText('Canonical output')).not.toBeInTheDocument();
  });
});
