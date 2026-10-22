// @vitest-environment jsdom

import { act, renderHook, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
  getConversation,
  getRun,
  getTrace,
  listAgents,
  listConversations,
  listProjects,
  getHealth,
  type ConversationDetail,
  type Run,
  type Span,
  type Trace,
} from '../../api';
import { useWorkspace } from './useWorkspace';

vi.mock('../../api', async () => {
  const actual = await vi.importActual<typeof import('../../api')>('../../api');
  return {
    ...actual,
    getConversation: vi.fn(),
    getRun: vi.fn(),
    getTrace: vi.fn(),
    listAgents: vi.fn(),
    listConversations: vi.fn(),
    listProjects: vi.fn(),
    getHealth: vi.fn(),
  };
});

interface Deferred<T> {
  promise: Promise<T>;
  resolve: (value: T) => void;
  reject: (reason: unknown) => void;
}

function deferred<T>(): Deferred<T> {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((accept, fail) => {
    resolve = accept;
    reject = fail;
  });
  return { promise, resolve, reject };
}

function conversation(id: string, runIds: string[] = []): ConversationDetail {
  return {
    conversation: {
      id,
      project_id: 'project-1',
      title: id,
      metadata: {},
      created_at: '2026-10-21T12:00:00Z',
      updated_at: '2026-10-21T12:00:00Z',
    },
    messages: runIds.map((runId, index) => ({
      id: `message-${runId}`,
      conversation_id: id,
      run_id: runId,
      ordinal: index + 1,
      role: 'assistant',
      content: runId,
      created_at: '2026-10-21T12:00:00Z',
    })),
  };
}

function run(id: string): Run {
  return {
    id,
    project_id: 'project-1',
    conversation_id: 'conversation-1',
    status: 'running',
    checkpoint_version: 1,
    created_at: '2026-10-21T12:00:00Z',
    updated_at: '2026-10-21T12:00:00Z',
  };
}

function span(runId: string): Span {
  return {
    id: `row-${runId}`,
    run_id: runId,
    trace_id: `trace-${runId}`,
    span_id: `span-${runId}`,
    kind: 'model',
    name: runId,
    status: 'succeeded',
    started_at: '2026-10-21T12:00:00Z',
    ended_at: '2026-10-21T12:00:01Z',
    created_at: '2026-10-21T12:00:00Z',
    updated_at: '2026-10-21T12:00:01Z',
  };
}

class FakeEventSource {
  onmessage: ((event: MessageEvent<string>) => void) | null = null;
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;

  addEventListener(): void {}
  removeEventListener(): void {}
  close(): void {}
}

beforeEach(() => {
  vi.clearAllMocks();
  vi.stubGlobal('EventSource', FakeEventSource);
  vi.mocked(listProjects).mockResolvedValue([
    {
      id: 'project-1',
      name: 'Project',
      metadata: {},
      created_at: '2026-10-21T12:00:00Z',
      updated_at: '2026-10-21T12:00:00Z',
    },
  ]);
  vi.mocked(listAgents).mockResolvedValue([]);
  vi.mocked(getHealth).mockResolvedValue({
    status: 'ok',
    dependencies: {},
  });
  vi.mocked(listConversations).mockResolvedValue([
    conversation('conversation-1').conversation,
    conversation('conversation-2').conversation,
  ]);
});

describe('workspace request freshness', () => {
  it('suppresses a stale conversation response after selection changes', async () => {
    window.history.replaceState(
      null,
      '',
      '/?project=project-1&conversation=conversation-1',
    );
    const first = deferred<ConversationDetail>();
    const second = deferred<ConversationDetail>();
    vi.mocked(getConversation).mockImplementation((id) =>
      id === 'conversation-1' ? first.promise : second.promise,
    );

    const { result } = renderHook(() => useWorkspace());
    await waitFor(() => expect(getConversation).toHaveBeenCalledWith('conversation-1'));

    act(() => result.current.selectConversation('conversation-2'));
    await waitFor(() => expect(getConversation).toHaveBeenCalledWith('conversation-2'));
    act(() => second.resolve(conversation('conversation-2')));
    await waitFor(() =>
      expect(result.current.conversation?.conversation.id).toBe('conversation-2'),
    );

    await act(async () => {
      first.resolve(conversation('conversation-1'));
      await first.promise;
    });
    expect(result.current.conversation?.conversation.id).toBe('conversation-2');
  });

  it('suppresses stale run and trace responses after selection changes', async () => {
    window.history.replaceState(
      null,
      '',
      '/?project=project-1&conversation=conversation-1&run=run-1',
    );
    vi.mocked(getConversation).mockResolvedValue(
      conversation('conversation-1', ['run-1', 'run-2']),
    );
    const firstRun = deferred<Run>();
    const firstTrace = deferred<Trace>();
    const secondRun = deferred<Run>();
    const secondTrace = deferred<Trace>();
    vi.mocked(getRun).mockImplementation((id) =>
      id === 'run-1' ? firstRun.promise : secondRun.promise,
    );
    vi.mocked(getTrace).mockImplementation((id) =>
      id === 'run-1' ? firstTrace.promise : secondTrace.promise,
    );

    const { result } = renderHook(() => useWorkspace());
    await waitFor(() => expect(getRun).toHaveBeenCalledWith('run-1'));

    act(() => result.current.selectRun('run-2'));
    await waitFor(() => expect(getRun).toHaveBeenCalledWith('run-2'));
    act(() => {
      secondRun.resolve(run('run-2'));
      secondTrace.resolve({
        spans: [span('run-2')],
        attempts: [],
        usage: [],
        operation_evidence: [],
      });
    });
    await waitFor(() => expect(result.current.run?.id).toBe('run-2'));

    await act(async () => {
      firstRun.resolve(run('run-1'));
      firstTrace.resolve({
        spans: [span('run-1')],
        attempts: [],
        usage: [],
        operation_evidence: [],
      });
      await Promise.all([firstRun.promise, firstTrace.promise]);
    });
    expect(result.current.run?.id).toBe('run-2');
    expect(result.current.spans[0]?.run_id).toBe('run-2');
  });

  it('suppresses an older overlapping run refetch failure', async () => {
    window.history.replaceState(
      null,
      '',
      '/?project=project-1&conversation=conversation-1&run=run-1',
    );
    vi.mocked(getConversation).mockResolvedValue(conversation('conversation-1', ['run-1']));
    vi.mocked(getRun).mockResolvedValue(run('run-1'));
    vi.mocked(getTrace).mockResolvedValue({
      spans: [span('run-1')],
      attempts: [],
      usage: [],
      operation_evidence: [],
    });

    const { result } = renderHook(() => useWorkspace());
    await waitFor(() => expect(result.current.run?.id).toBe('run-1'));

    const staleRun = deferred<Run>();
    const staleTrace = deferred<Trace>();
    const currentRun = deferred<Run>();
    const currentTrace = deferred<Trace>();
    vi.mocked(getRun)
      .mockImplementationOnce(() => staleRun.promise)
      .mockImplementationOnce(() => currentRun.promise);
    vi.mocked(getTrace)
      .mockImplementationOnce(() => staleTrace.promise)
      .mockImplementationOnce(() => currentTrace.promise);

    act(() => {
      void result.current.refresh();
    });
    await waitFor(() => expect(getRun).toHaveBeenCalledTimes(2));
    act(() => {
      void result.current.refresh();
    });
    await waitFor(() => expect(getRun).toHaveBeenCalledTimes(3));

    act(() => {
      currentRun.resolve({ ...run('run-1'), checkpoint_version: 2 });
      currentTrace.resolve({
        spans: [span('run-1')],
        attempts: [],
        usage: [],
        operation_evidence: [],
      });
    });
    await waitFor(() => expect(result.current.run?.checkpoint_version).toBe(2));

    await act(async () => {
      staleRun.reject(new Error('stale run failure'));
      staleTrace.reject(new Error('stale trace failure'));
      await Promise.resolve();
    });

    expect(result.current.error).toBeNull();
  });

  it('shows the newest run refetch failure', async () => {
    window.history.replaceState(
      null,
      '',
      '/?project=project-1&conversation=conversation-1&run=run-1',
    );
    vi.mocked(getConversation).mockResolvedValue(conversation('conversation-1', ['run-1']));
    vi.mocked(getRun).mockResolvedValue(run('run-1'));
    vi.mocked(getTrace).mockResolvedValue({
      spans: [span('run-1')],
      attempts: [],
      usage: [],
      operation_evidence: [],
    });

    const { result } = renderHook(() => useWorkspace());
    await waitFor(() => expect(result.current.run?.id).toBe('run-1'));

    vi.mocked(getRun).mockRejectedValueOnce(new Error('current run failure'));
    await act(async () => {
      await result.current.refresh();
    });

    expect(result.current.error).toBe('current run failure');
  });
});
