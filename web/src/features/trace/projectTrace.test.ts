import { describe, expect, it } from 'vitest';
import type { DerivedTraceNode, Span } from '../../api';
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

function derivedNode(
  id: string,
  parentSpanId: string,
  kind: DerivedTraceNode['kind'],
  startedAt: string,
): DerivedTraceNode {
  return {
    id,
    parent_span_id: parentSpanId,
    source_evidence_id: `evidence-${id}`,
    kind,
    name: id,
    status: 'succeeded',
    started_at: startedAt,
    ended_at: '2026-10-21T12:00:03Z',
    data: {},
    evidence: {},
  };
}

describe('projectTrace', () => {
  it('projects an out-of-order span list into a stable parent-child tree', () => {
    const projected = projectTrace([
      span('child-late', 'root', '2026-10-21T12:00:02Z'),
      span('root', '', '2026-10-21T12:00:00Z'),
      span('child-early', 'root', '2026-10-21T12:00:01Z'),
    ], []);

    expect(projected.map((node) => node.id)).toEqual(['root']);
    expect(projected[0]?.children.map((node) => node.id)).toEqual([
      'child-early',
      'child-late',
    ]);
  });

  it('merges derived nodes under their canonical parent', () => {
    const operation = span('operation-1', '', '2026-10-21T12:00:00Z');
    const step = derivedNode(
      'derived:operation-1:step:0',
      'operation-1',
      'step',
      '2026-10-21T12:00:01Z',
    );

    const projected = projectTrace([operation], [step]);

    expect(projected[0]?.children[0]).toMatchObject({
      id: 'derived:operation-1:step:0',
      parentId: 'operation-1',
      kind: 'step',
      source: { type: 'derived' },
    });
  });

  it('projects the same tree regardless of input order', () => {
    const root = span('root', '', '2026-10-21T12:00:00Z');
    const operation = span('operation-1', 'root', '2026-10-21T12:00:01Z');
    const firstStep = derivedNode(
      'derived:operation-1:step:0',
      'operation-1',
      'step',
      '2026-10-21T12:00:02Z',
    );
    const secondStep = derivedNode(
      'derived:operation-1:step:1',
      'operation-1',
      'step',
      '2026-10-21T12:00:03Z',
    );

    expect(
      projectTrace([operation, root], [secondStep, firstStep]),
    ).toEqual(projectTrace([root, operation], [firstStep, secondStep]));
  });

  it('omits derived nodes that do not have a canonical parent', () => {
    const projected = projectTrace([], [
      derivedNode(
        'derived:missing:step:0',
        'missing',
        'step',
        '2026-10-21T12:00:00Z',
      ),
    ]);

    expect(projected).toEqual([]);
  });

  it('retains a canonical orphan as a root', () => {
    const projected = projectTrace([
      span('orphan', 'missing-parent', '2026-10-21T12:00:00Z'),
    ], []);

    expect(projected).toMatchObject([{ id: 'orphan', parentId: 'missing-parent' }]);
  });
});
