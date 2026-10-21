import type { Span } from '../../api';

export interface TraceNode extends Span {
  children: TraceNode[];
}

function compareNodes(left: TraceNode, right: TraceNode): number {
  const time = left.started_at.localeCompare(right.started_at);
  return time === 0 ? left.span_id.localeCompare(right.span_id) : time;
}

function sortTree(nodes: TraceNode[]): void {
  nodes.sort(compareNodes);
  for (const node of nodes) sortTree(node.children);
}

export function projectTrace(spans: readonly Span[]): TraceNode[] {
  const nodes = new Map<string, TraceNode>();
  for (const span of spans) {
    nodes.set(span.span_id, { ...span, children: [] });
  }

  const roots: TraceNode[] = [];
  for (const node of nodes.values()) {
    const parent =
      node.parent_span_id === undefined || node.parent_span_id === ''
        ? undefined
        : nodes.get(node.parent_span_id);
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
