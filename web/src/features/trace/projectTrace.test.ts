import { describe, expect, it } from 'vitest';
import type { Span } from '../../api';
import { projectTrace } from './projectTrace';

function span(
  spanId: string,
  parentSpanId: string,
  startedAt: string,
): Span {
  return {
    id: `row-${spanId}`,
    run_id: 'run-1',
    trace_id: 'trace-1',
    span_id: spanId,
    parent_span_id: parentSpanId,
    kind: 'model',
    name: spanId,
    status: 'succeeded',
    input: null,
    output: null,
    started_at: startedAt,
    ended_at: '2026-10-21T12:00:03Z',
    created_at: startedAt,
    updated_at: '2026-10-21T12:00:03Z',
  };
}

describe('projectTrace', () => {
  it('projects an out-of-order span list into a stable parent-child tree', () => {
    const projected = projectTrace([
      span('child-late', 'root', '2026-10-21T12:00:02Z'),
      span('root', '', '2026-10-21T12:00:00Z'),
      span('child-early', 'root', '2026-10-21T12:00:01Z'),
    ]);

    expect(projected.map((node) => node.span_id)).toEqual(['root']);
    expect(projected[0]?.children.map((node) => node.span_id)).toEqual([
      'child-early',
      'child-late',
    ]);
  });
});
