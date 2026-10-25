// @vitest-environment jsdom

import '@testing-library/jest-dom/vitest';
import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import type { OperationEvidence, Run, UsageRecord } from '../../api';
import { TraceOverview } from './TraceOverview';

const run: Run = {
  id: 'run-1',
  project_id: 'project-1',
  conversation_id: 'conversation-1',
  source_message_id: 'message-1',
  status: 'failed',
  checkpoint_version: 4,
  created_at: '2026-10-21T12:00:00Z',
  updated_at: '2026-10-21T12:00:03Z',
  finished_at: '2026-10-21T12:00:03Z',
};

const usage: UsageRecord[] = [
  {
    id: 'usage-1',
    span_id: 'model-1',
    span_attempt_id: 'attempt-1',
    input_tokens: 10,
    output_tokens: 5,
    total_tokens: 15,
    cost_micros: 40,
    details: {},
    created_at: '2026-10-21T12:00:01Z',
  },
  {
    id: 'usage-2',
    span_id: 'model-2',
    span_attempt_id: 'attempt-2',
    input_tokens: 10,
    output_tokens: 5,
    total_tokens: 15,
    cost_micros: 60,
    details: {},
    created_at: '2026-10-21T12:00:02Z',
  },
];

const operationEvidence: OperationEvidence[] = [
  {
    id: 'operation-1',
    span_id: 'operation-span-1',
    operation_id: 'call-1',
    status: 'succeeded',
    artifacts: [],
    created_at: '2026-10-21T12:00:01Z',
    updated_at: '2026-10-21T12:00:01Z',
  },
  {
    id: 'operation-2',
    span_id: 'operation-span-2',
    operation_id: 'call-2',
    status: 'failed',
    artifacts: [],
    error: { message: 'failed assertion' },
    created_at: '2026-10-21T12:00:02Z',
    updated_at: '2026-10-21T12:00:02Z',
  },
];

describe('run trace overview', () => {
  it('projects report and final usage from canonical trace records', () => {
    render(
      <TraceOverview
        run={run}
        usage={usage}
        operationEvidence={operationEvidence}
      />,
    );

    expect(screen.getByRole('region', { name: 'Run report' })).toHaveTextContent(
      'failed',
    );
    expect(screen.getByText('2 operations')).toBeInTheDocument();
    expect(screen.getByText('1 passed · 1 failed')).toBeInTheDocument();
    expect(screen.getByText('30 tokens')).toBeInTheDocument();
    expect(screen.getByText('$0.000100')).toBeInTheDocument();
  });
});
