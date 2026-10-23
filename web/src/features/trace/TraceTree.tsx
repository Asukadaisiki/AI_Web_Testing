import {
  Bot,
  BrainCircuit,
  ChevronDown,
  ChevronRight,
  FileCheck2,
  Globe2,
  ListChecks,
  Wrench,
  Workflow,
} from 'lucide-react';
import { useState, type CSSProperties, type KeyboardEvent } from 'react';
import { flattenTrace, type TraceNode } from './projectTrace';

interface Props {
  nodes: TraceNode[];
  selectedNodeId: string | null;
  onSelect: (nodeId: string) => void;
}

interface FlatRow {
  node: TraceNode;
  depth: number;
}

function rows(
  nodes: readonly TraceNode[],
  collapsedNodeIds: ReadonlySet<string>,
  depth = 1,
): FlatRow[] {
  return nodes.flatMap((node) => {
    const isCollapsed =
      node.kind === 'operation' && collapsedNodeIds.has(node.id);
    return [
      { node, depth },
      ...(isCollapsed ? [] : rows(node.children, collapsedNodeIds, depth + 1)),
    ];
  });
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
      return <ListChecks size={14} />;
    case 'unknown_tool':
      return <Wrench size={14} />;
  }
}

export function TraceTree({ nodes, selectedNodeId, onSelect }: Props): JSX.Element {
  const [collapsedNodeIds, setCollapsedNodeIds] = useState<Set<string>>(
    () => new Set(),
  );
  const flattened = rows(nodes, collapsedNodeIds);
  const logicalNodes = flattenTrace(nodes);

  function move(nextIndex: number): void {
    const next = flattened[nextIndex];
    if (next === undefined) return;
    onSelect(next.node.id);
    document.getElementById(`trace-node-${next.node.id}`)?.focus();
  }

  function toggle(nodeId: string): void {
    setCollapsedNodeIds((current) => {
      const next = new Set(current);
      if (next.has(nodeId)) {
        next.delete(nodeId);
      } else {
        next.add(nodeId);
      }
      return next;
    });
  }

  function handleKey(event: KeyboardEvent<HTMLDivElement>, index: number): void {
    const current = flattened[index]?.node;
    if (current === undefined) return;
    const canToggle = current.kind === 'operation' && current.children.length > 0;
    const isCollapsed = collapsedNodeIds.has(current.id);
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
        if (canToggle && isCollapsed) {
          event.preventDefault();
          toggle(current.id);
          break;
        }
        const child = current.children[0];
        if (child === undefined) break;
        event.preventDefault();
        const childIndex = flattened.findIndex((row) => row.node.id === child.id);
        move(childIndex);
        break;
      }
      case 'ArrowLeft': {
        if (canToggle && !isCollapsed) {
          event.preventDefault();
          toggle(current.id);
          break;
        }
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
        <div
          id={`trace-node-${node.id}`}
          key={node.id}
          role="treeitem"
          tabIndex={node.id === selectedNodeId ? 0 : -1}
          aria-level={depth}
          aria-selected={node.id === selectedNodeId}
          aria-expanded={
            node.kind === 'operation' && node.children.length > 0
              ? !collapsedNodeIds.has(node.id)
              : undefined
          }
          aria-label={node.name}
          className={`trace-node${node.id === selectedNodeId ? ' is-selected' : ''}`}
          style={{ '--trace-depth': depth } as CSSProperties}
          onKeyDown={(event) => handleKey(event, index)}
        >
          {node.kind === 'operation' && node.children.length > 0 ? (
            <button
              type="button"
              className="trace-node-toggle"
              aria-label={`${collapsedNodeIds.has(node.id) ? 'Expand' : 'Collapse'} ${node.name}`}
              onClick={() => toggle(node.id)}
            >
              {collapsedNodeIds.has(node.id) ? (
                <ChevronRight size={14} />
              ) : (
                <ChevronDown size={14} />
              )}
            </button>
          ) : (
            <span className="trace-node-toggle-spacer" aria-hidden="true" />
          )}
          <button
            type="button"
            className="trace-node-select"
            aria-label={`Select ${node.name}`}
            onClick={() => onSelect(node.id)}
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
        </div>
      ))}
    </div>
  );
}
