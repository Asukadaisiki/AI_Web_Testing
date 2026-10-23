import type {
  DerivedTraceKind,
  DerivedTraceNode,
  Span,
  SpanKind,
} from '../../api';

export interface TraceNode {
  id: string;
  parentId?: string;
  kind: SpanKind | DerivedTraceKind;
  name: string;
  status: string;
  startedAt: string;
  endedAt?: string | null;
  source:
    | { type: 'span'; value: Span }
    | { type: 'derived'; value: DerivedTraceNode };
  children: TraceNode[];
}

function compareNodes(left: TraceNode, right: TraceNode): number {
  const time = left.startedAt.localeCompare(right.startedAt);
  return time === 0 ? left.id.localeCompare(right.id) : time;
}

function sortTree(nodes: TraceNode[]): void {
  nodes.sort(compareNodes);
  for (const node of nodes) sortTree(node.children);
}

export function projectTrace(
  spans: readonly Span[],
  derivedNodes: readonly DerivedTraceNode[],
): TraceNode[] {
  const nodes = new Map<string, TraceNode>();
  for (const span of spans) {
    nodes.set(span.span_id, {
      id: span.span_id,
      parentId:
        span.parent_span_id === undefined || span.parent_span_id === ''
          ? undefined
          : span.parent_span_id,
      kind: span.kind,
      name: span.name,
      status: span.status,
      startedAt: span.started_at,
      endedAt: span.ended_at,
      source: { type: 'span', value: span },
      children: [],
    });
  }
  for (const derived of derivedNodes) {
    if (!nodes.has(derived.parent_span_id)) continue;
    nodes.set(derived.id, {
      id: derived.id,
      parentId: derived.parent_span_id,
      kind: derived.kind,
      name: derived.name,
      status: derived.status,
      startedAt: derived.started_at,
      endedAt: derived.ended_at,
      source: { type: 'derived', value: derived },
      children: [],
    });
  }

  const roots: TraceNode[] = [];
  for (const node of nodes.values()) {
    const parent = node.parentId === undefined ? undefined : nodes.get(node.parentId);
    if (parent === undefined || parent === node) {
      roots.push(node);
      continue;
    }
    parent.children.push(node);
  }
  sortTree(roots);
  return roots;
}

export function flattenTrace(nodes: readonly TraceNode[]): TraceNode[] {
  const flattened: TraceNode[] = [];
  const visit = (items: readonly TraceNode[]): void => {
    for (const item of items) {
      flattened.push(item);
      visit(item.children);
    }
  };
  visit(nodes);
  return flattened;
}
