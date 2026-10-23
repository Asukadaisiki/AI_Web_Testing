import { Radio, Rows3 } from 'lucide-react';
import type { WorkspaceState } from '../workspace/useWorkspace';
import { TraceDetails } from './TraceDetails';
import { TraceOverview } from './TraceOverview';
import { TraceTree } from './TraceTree';

interface Props {
  workspace: WorkspaceState;
}

export function TracePane({ workspace }: Props): JSX.Element {
  const selectedSpanId =
    workspace.selectedNode?.source.type === 'span'
      ? workspace.selectedNode.source.value.span_id
      : undefined;
  const attempts = workspace.attempts.filter(
    (attempt) => attempt.span_id === selectedSpanId,
  );
  const usage = workspace.usage.filter(
    (record) => record.span_id === selectedSpanId,
  );
  const operationEvidence = workspace.operationEvidence.filter(
    (evidence) => evidence.span_id === selectedSpanId,
  );

  return (
    <aside className="pane trace-pane" aria-label="Run trace and details">
      <header className="pane-header trace-header">
        <div>
          <p className="eyebrow">Current turn</p>
          <h2>Trace</h2>
        </div>
        <div className={`live-badge${workspace.connected ? ' is-live' : ''}`}>
          <Radio size={13} />
          {workspace.connected ? 'Live' : 'Offline'}
        </div>
      </header>

      {workspace.selection.runId === null ? (
        <div className="trace-placeholder">
          <Rows3 size={24} />
          <h3>No run selected</h3>
          <p>Send a message or select a previous run to inspect its Trace.</p>
        </div>
      ) : (
        <>
          <TraceOverview
            run={workspace.run}
            usage={workspace.usage}
            operationEvidence={workspace.operationEvidence}
          />
          <section className="trace-tree-region" aria-labelledby="trace-tree-heading">
            <div className="trace-region-heading">
              <h3 id="trace-tree-heading">Execution tree</h3>
              <span>{workspace.spans.length} nodes</span>
            </div>
            <TraceTree
              nodes={workspace.trace}
              selectedSpanId={workspace.selection.spanId}
              onSelect={workspace.selectSpan}
            />
          </section>
          <section className="trace-detail-region" aria-label="Selected trace node details">
            <TraceDetails
              node={workspace.selectedNode}
              attempts={attempts}
              usage={usage}
              operationEvidence={operationEvidence}
            />
          </section>
        </>
      )}
    </aside>
  );
}
