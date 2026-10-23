import {
  Bot,
  BrainCircuit,
  FileCheck2,
  Globe2,
  Wrench,
  Workflow,
} from 'lucide-react';
import type { KeyboardEvent } from 'react';
import { flattenTrace, type TraceNode } from './projectTrace';

interface Props {
  nodes: TraceNode[];
  selectedSpanId: string | null;
  onSelect: (spanId: string) => void;
}

interface FlatRow {
  node: TraceNode;
  depth: number;
}

function rows(nodes: readonly TraceNode[], depth = 1): FlatRow[] {
  return nodes.flatMap((node) => [
    { node, depth },
    ...rows(node.children, depth + 1),
  ]);
}

function KindIcon({ kind }: { kind: TraceNode['kind'] }): JSX.Element {
  switch (kind) {
    case 'model':
      return <Bot size={14} />;
    case 'reasoning':
      return <BrainCircuit size={14} />;
    case 'tool':
      return <Wrench size={14} />;
    case 'operation':
      return <Globe2 size={14} />;
    case 'report':
      return <FileCheck2 size={14} />;
    case 'run':
      return <Workflow size={14} />;
    case 'step':
      return <Workflow size={14} />;
    case 'unknown_tool':
      return <Wrench size={14} />;
  }
}

export function TraceTree({ nodes, selectedSpanId, onSelect }: Props): JSX.Element {
  const flattened = rows(nodes);
  const logicalNodes = flattenTrace(nodes);

  function move(nextIndex: number): void {
    const next = flattened[nextIndex];
    if (next === undefined) return;
    onSelect(next.node.id);
    document.getElementById(`trace-node-${String(nextIndex)}`)?.focus();
  }

  function handleKey(event: KeyboardEvent<HTMLButtonElement>, index: number): void {
    const current = flattened[index]?.node;
    if (current === undefined) return;
    switch (event.key) {
      case 'ArrowDown':
        event.preventDefault();
        move(Math.min(index + 1, flattened.length - 1));
        break;
      case 'ArrowUp':
        event.preventDefault();
        move(Math.max(index - 1, 0));
        break;
      case 'Home':
        event.preventDefault();
        move(0);
        break;
      case 'End':
        event.preventDefault();
        move(flattened.length - 1);
        break;
      case 'ArrowRight': {
        const child = current.children[0];
        if (child === undefined) break;
        event.preventDefault();
        const childIndex = flattened.findIndex((row) => row.node.id === child.id);
        move(childIndex);
        break;
      }
      case 'ArrowLeft': {
        if (current.parentId === undefined) break;
        event.preventDefault();
        const parentIndex = flattened.findIndex(
          (row) => row.node.id === current.parentId,
        );
        move(parentIndex);
        break;
      }
    }
  }

  if (logicalNodes.length === 0) {
    return <p className="trace-empty">No trace spans recorded for this run.</p>;
  }

  return (
    <div className="trace-tree" role="tree" aria-label="Run trace">
      {flattened.map(({ node, depth }, index) => (
        <button
          id={`trace-node-${String(index)}`}
          key={node.id}
          type="button"
          role="treeitem"
          aria-level={depth}
          aria-selected={node.id === selectedSpanId}
          aria-label={`Trace node: ${node.name}`}
          className={`trace-node${node.id === selectedSpanId ? ' is-selected' : ''}`}
          style={{ '--trace-depth': depth } as React.CSSProperties}
          onClick={() => onSelect(node.id)}
          onKeyDown={(event) => handleKey(event, index)}
        >
          <span className={`trace-kind trace-kind-${node.kind}`}>
            <KindIcon kind={node.kind} />
          </span>
          <span className="trace-node-copy">
            <strong>{node.name}</strong>
            <span>{node.kind}</span>
          </span>
          <span className={`trace-status trace-status-${node.status}`} title={node.status} />
        </button>
      ))}
    </div>
  );
}
