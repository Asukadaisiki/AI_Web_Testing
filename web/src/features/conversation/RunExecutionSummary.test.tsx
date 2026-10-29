// @vitest-environment jsdom
import '@testing-library/jest-dom/vitest';
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, expect, it } from 'vitest';
import type { Run, Span } from '../../api';
import { RunExecutionSummary } from './RunExecutionSummary';

const run: Run = {
  id: 'run', project_id: 'project', conversation_id: 'conversation', source_message_id: 'message',
  status: 'queued', checkpoint_version: 0, created_at: 'now', updated_at: 'now',
};
const span: Span = {
  id: 'span', run_id: 'run', trace_id: 'trace', span_id: 'root', kind: 'run',
  name: 'run', status: 'running', started_at: 'now', created_at: 'now', updated_at: 'now',
  input: { execution: { runtime_id: 'pi', harness: { id: 'browser', name: 'Browser', revision: 2 } } },
};
afterEach(cleanup);

it('shows durable Run identity and revision instead of the current editor selection', () => {
  render(<RunExecutionSummary run={{ ...run, runtime_id: 'codex', harness_id: 'deleted-harness', harness_revision: 3 }} spans={[span]} />);
  expect(screen.getByLabelText('Run composition')).toHaveTextContent('codex');
  expect(screen.getByLabelText('Run composition')).toHaveTextContent('deleted-harness (r3)');
  expect(screen.getByLabelText('Run composition')).not.toHaveTextContent('Browser');
});

it('falls back to a same-Run trace execution snapshot when Run metadata is absent', () => {
  render(<RunExecutionSummary run={run} spans={[span]} />);
  expect(screen.getByLabelText('Run composition')).toHaveTextContent('pi');
  expect(screen.getByLabelText('Run composition')).toHaveTextContent('Browser (r2)');
});

it('does not invent composition on legacy Runs or use another Run trace', () => {
  const { rerender } = render(<RunExecutionSummary run={run} spans={[]} />);
  expect(screen.queryByLabelText('Run composition')).toBeNull();
  rerender(<RunExecutionSummary run={run} spans={[{ ...span, run_id: 'different-run' }]} />);
  expect(screen.queryByLabelText('Run composition')).toBeNull();
});
