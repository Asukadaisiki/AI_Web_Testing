// @vitest-environment jsdom

import '@testing-library/jest-dom/vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import type { DerivedTraceNode, Span } from '../../api';
import { projectTrace } from './projectTrace';
import { TraceTree } from './TraceTree';

const operation: Span = {
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

function derivedStep(id: string, name: string): DerivedTraceNode {
  return {
    id,
    parent_span_id: operation.span_id,
    source_evidence_id: `evidence-${id}`,
    kind: 'step',
    name,
    status: 'succeeded',
    started_at: '2026-10-21T12:00:01Z',
    ended_at: '2026-10-21T12:00:02Z',
    data: {},
    evidence: {},
  };
}

describe('TraceTree', () => {
  it('collapses derived children and navigates only visible rows', () => {
    const onSelect = vi.fn();
    render(
      <TraceTree
        nodes={projectTrace(
          [operation],
          [
            derivedStep('derived:operation-1:step:0', 'Step 1: goto'),
            derivedStep('derived:operation-1:step:1', 'Step 2: click'),
          ],
        )}
        selectedNodeId={null}
        onSelect={onSelect}
      />,
    );

    const operationRow = screen.getByRole('treeitem', {
      name: /browser_execute_case/i,
    });
    expect(operationRow).toHaveAttribute('aria-expanded', 'true');
    fireEvent.click(
      screen.getByRole('button', { name: /collapse browser_execute_case/i }),
    );
    expect(screen.queryByText('Step 1: goto')).not.toBeInTheDocument();
    fireEvent.click(
      screen.getByRole('button', { name: /expand browser_execute_case/i }),
    );
    expect(screen.getByText('Step 1: goto')).toBeInTheDocument();
    const firstStep = screen.getByRole('treeitem', { name: /step 1: goto/i });
    const secondStep = screen.getByRole('treeitem', { name: /step 2: click/i });

    operationRow.focus();
    fireEvent.keyDown(operationRow, { key: 'ArrowRight' });
    expect(firstStep).toHaveFocus();
    expect(onSelect).toHaveBeenLastCalledWith('derived:operation-1:step:0');

    fireEvent.keyDown(firstStep, { key: 'ArrowDown' });
    expect(secondStep).toHaveFocus();
    expect(onSelect).toHaveBeenLastCalledWith('derived:operation-1:step:1');

    fireEvent.keyDown(secondStep, { key: 'ArrowUp' });
    expect(firstStep).toHaveFocus();

    fireEvent.keyDown(firstStep, { key: 'End' });
    expect(secondStep).toHaveFocus();

    fireEvent.keyDown(secondStep, { key: 'Home' });
    expect(operationRow).toHaveFocus();

    fireEvent.keyDown(operationRow, { key: 'ArrowLeft' });
    expect(operationRow).toHaveAttribute('aria-expanded', 'false');
    expect(screen.queryByText('Step 1: goto')).not.toBeInTheDocument();

    fireEvent.keyDown(operationRow, { key: 'ArrowRight' });
    expect(operationRow).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByText('Step 1: goto')).toBeInTheDocument();
  });
});
