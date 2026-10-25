import { describe, expect, it } from 'vitest';
import type { ConversationDetail, RunEvent } from '../../api';
import {
  keepSelectedSpan,
  latestRunId,
  mergeRunEvents,
  workspaceSelectionReducer,
  type WorkspaceSelection,
} from './useWorkspace';
import type { TraceNode } from '../trace/projectTrace';

const emptySelection: WorkspaceSelection = {
  projectId: null,
  conversationId: null,
  runId: null,
  spanId: null,
};

function event(sequence: number | null, type: string): RunEvent {
  return {
    id: sequence === null ? null : String(sequence),
    sequence,
    type,
    data: { type },
  };
}

function node(spanId: string): TraceNode {
  const span = {
    id: `row-${spanId}`,
    run_id: 'run-1',
    trace_id: 'trace-1',
    span_id: spanId,
    parent_span_id: '',
    kind: 'model' as const,
    name: spanId,
    status: 'succeeded',
    input: null,
    output: null,
    started_at: '2026-10-21T12:00:00Z',
    ended_at: '2026-10-21T12:00:01Z',
    created_at: '2026-10-21T12:00:00Z',
    updated_at: '2026-10-21T12:00:01Z',
  };
  return {
    id: spanId,
    kind: span.kind,
    name: span.name,
    status: span.status,
    startedAt: span.started_at,
    endedAt: span.ended_at,
    source: { type: 'span', value: span },
    children: [],
  };
}

describe('workspace state', () => {
  it('gets the latest Run only from the canonical conversation runs', () => {
    const detail: ConversationDetail = {
      conversation: {
        id: 'conversation-1',
        project_id: 'project-1',
        metadata: {},
        created_at: '2026-10-25T12:00:00Z',
        updated_at: '2026-10-25T12:05:00Z',
      },
      messages: [{
        id: 'message-1',
        conversation_id: 'conversation-1',
        run_id: 'run-from-message',
        ordinal: 1,
        role: 'assistant',
        content: 'Old message association',
        created_at: '2026-10-25T12:00:00Z',
      }],
      runs: ['run-original', 'run-rerun'].map((id) => ({
        id,
        project_id: 'project-1',
        conversation_id: 'conversation-1',
        source_message_id: 'message-1',
        status: 'failed',
        checkpoint_version: 1,
        created_at: '2026-10-25T12:00:00Z',
        updated_at: '2026-10-25T12:05:00Z',
      })),
    };

    expect(latestRunId(detail)).toBe('run-rerun');
  });

  it('keeps selected project conversation run and span in one workspace state', () => {
    const selected = workspaceSelectionReducer(emptySelection, {
      type: 'restore',
      selection: {
        projectId: 'project-1',
        conversationId: 'conversation-1',
        runId: 'run-1',
        spanId: 'span-1',
      },
    });

    expect(selected).toEqual({
      projectId: 'project-1',
      conversationId: 'conversation-1',
      runId: 'run-1',
      spanId: 'span-1',
    });
  });

  it('merges replayed SSE events without duplicating a sequence', () => {
    const merged = mergeRunEvents(
      [event(1, 'run.started'), event(2, 'runtime.step.completed')],
      [
        event(2, 'runtime.step.completed'),
        event(null, 'token.delta'),
        event(3, 'runtime.completed'),
      ],
    );

    expect(merged.map(({ sequence, type }) => ({ sequence, type }))).toEqual([
      { sequence: 1, type: 'run.started' },
      { sequence: 2, type: 'runtime.step.completed' },
      { sequence: null, type: 'token.delta' },
      { sequence: 3, type: 'runtime.completed' },
    ]);
  });

  it('keeps the selected node when a sibling live update arrives', () => {
    const before = [node('model-1')];
    const after = [node('model-1'), node('operation-1')];

    expect(keepSelectedSpan('model-1', before)).toBe('model-1');
    expect(keepSelectedSpan('model-1', after)).toBe('model-1');
  });

  it('retains a restored node while its trace is loading', () => {
    expect(keepSelectedSpan('operation-1', [])).toBe('operation-1');
  });
});
