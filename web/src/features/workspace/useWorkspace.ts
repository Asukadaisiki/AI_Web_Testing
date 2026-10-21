import {
  useCallback,
  useEffect,
  useMemo,
  useReducer,
  useState,
} from 'react';
import {
  cancelRun,
  createConversation,
  createMessage,
  createProject,
  errorMessage,
  getConversation,
  getHealth,
  getRun,
  getTrace,
  listAgents,
  listConversations,
  listProjects,
  parseRunEvent,
  runEventsURL,
  submitRunResponse,
  type Agent,
  type Conversation,
  type ConversationDetail,
  type Health,
  type ModelRef,
  type Project,
  type Run,
  type RunEvent,
  type Span,
} from '../../api';
import { flattenTrace, projectTrace, type TraceNode } from '../trace/projectTrace';

export interface WorkspaceSelection {
  projectId: string | null;
  conversationId: string | null;
  runId: string | null;
  spanId: string | null;
}

export type WorkspaceSelectionAction =
  | { type: 'restore'; selection: WorkspaceSelection }
  | { type: 'project'; projectId: string | null }
  | { type: 'conversation'; conversationId: string | null }
  | { type: 'run'; runId: string | null }
  | { type: 'span'; spanId: string | null };

export function workspaceSelectionReducer(
  state: WorkspaceSelection,
  action: WorkspaceSelectionAction,
): WorkspaceSelection {
  switch (action.type) {
    case 'restore':
      return action.selection;
    case 'project':
      return {
        projectId: action.projectId,
        conversationId: null,
        runId: null,
        spanId: null,
      };
    case 'conversation':
      return {
        ...state,
        conversationId: action.conversationId,
        runId: null,
        spanId: null,
      };
    case 'run':
      return { ...state, runId: action.runId, spanId: null };
    case 'span':
      return { ...state, spanId: action.spanId };
  }
}

export function selectionFromSearch(search: string): WorkspaceSelection {
  const query = new URLSearchParams(search);
  const read = (key: string): string | null => {
    const value = query.get(key);
    return value === null || value === '' ? null : value;
  };
  return {
    projectId: read('project'),
    conversationId: read('conversation'),
    runId: read('run'),
    spanId: read('span'),
  };
}

export function mergeRunEvents(
  current: readonly RunEvent[],
  incoming: readonly RunEvent[],
): RunEvent[] {
  const seen = new Set(
    current.flatMap((event) => (event.sequence === null ? [] : [event.sequence])),
  );
  const merged = [...current];
  for (const event of incoming) {
    if (event.sequence !== null) {
      if (seen.has(event.sequence)) continue;
      seen.add(event.sequence);
    }
    merged.push(event);
  }
  return merged;
}

export function keepSelectedSpan(
  selectedSpanId: string | null,
  nodes: readonly TraceNode[],
): string | null {
  const flattened = flattenTrace(nodes);
  if (
    selectedSpanId !== null &&
    flattened.some((node) => node.span_id === selectedSpanId)
  ) {
    return selectedSpanId;
  }
  return flattened[0]?.span_id ?? null;
}

const namedRunEvents = [
  'run.started',
  'run.cancel.requested',
  'run.response.received',
  'runtime.step.completed',
  'runtime.completed',
  'span.completed',
  'token.delta',
  'error',
] as const;

const terminalStatuses = new Set(['succeeded', 'failed', 'cancelled']);

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function isTerminalEvent(event: RunEvent): boolean {
  if (
    event.type === 'runtime.completed' ||
    event.type === 'run.completed' ||
    event.type === 'run.failed' ||
    event.type === 'run.cancelled'
  ) {
    return true;
  }
  if (!isRecord(event.data)) return false;
  const status = event.data['status'];
  return typeof status === 'string' && terminalStatuses.has(status);
}

function writeSelection(selection: WorkspaceSelection): void {
  const query = new URLSearchParams();
  if (selection.projectId !== null) query.set('project', selection.projectId);
  if (selection.conversationId !== null) {
    query.set('conversation', selection.conversationId);
  }
  if (selection.runId !== null) query.set('run', selection.runId);
  if (selection.spanId !== null) query.set('span', selection.spanId);
  const suffix = query.toString();
  window.history.replaceState(null, '', `${window.location.pathname}${suffix === '' ? '' : `?${suffix}`}`);
}

function latestRunId(detail: ConversationDetail): string | null {
  for (let index = detail.messages.length - 1; index >= 0; index -= 1) {
    const runId = detail.messages[index]?.run_id;
    if (runId !== undefined && runId !== '') return runId;
  }
  return null;
}

export interface WorkspaceState {
  selection: WorkspaceSelection;
  projects: Project[];
  conversations: Conversation[];
  conversation: ConversationDetail | null;
  run: Run | null;
  spans: Span[];
  trace: TraceNode[];
  selectedSpan: TraceNode | null;
  events: RunEvent[];
  agents: Agent[];
  health: Health | null;
  loading: boolean;
  busy: boolean;
  connected: boolean;
  error: string | null;
  streamError: string | null;
  selectProject: (projectId: string) => void;
  selectConversation: (conversationId: string) => void;
  selectRun: (runId: string) => void;
  selectSpan: (spanId: string) => void;
  addProject: (name: string) => Promise<void>;
  addConversation: (title: string) => Promise<void>;
  sendMessage: (
    content: string,
    agentId: string,
    model: ModelRef,
    idempotencyKey: string,
  ) => Promise<boolean>;
  respond: (
    kind: 'question' | 'approval',
    content: string,
    idempotencyKey: string,
  ) => Promise<boolean>;
  requestCancel: () => Promise<void>;
  refresh: () => Promise<void>;
}

export function useWorkspace(): WorkspaceState {
  const [selection, dispatchSelection] = useReducer(
    workspaceSelectionReducer,
    window.location.search,
    selectionFromSearch,
  );
  const [projects, setProjects] = useState<Project[]>([]);
  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [conversation, setConversation] = useState<ConversationDetail | null>(null);
  const [run, setRun] = useState<Run | null>(null);
  const [spans, setSpans] = useState<Span[]>([]);
  const [events, setEvents] = useState<RunEvent[]>([]);
  const [agents, setAgents] = useState<Agent[]>([]);
  const [health, setHealth] = useState<Health | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [connected, setConnected] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [streamError, setStreamError] = useState<string | null>(null);

  const trace = useMemo(() => projectTrace(spans), [spans]);
  const flattenedTrace = useMemo(() => flattenTrace(trace), [trace]);
  const selectedSpan =
    flattenedTrace.find((node) => node.span_id === selection.spanId) ?? null;

  useEffect(() => {
    writeSelection(selection);
  }, [selection]);

  const loadProjects = useCallback(async (): Promise<Project[]> => {
    const value = await listProjects();
    setProjects(value);
    return value;
  }, []);

  const loadConversations = useCallback(async (projectId: string): Promise<Conversation[]> => {
    const value = await listConversations(projectId);
    setConversations(value);
    return value;
  }, []);

  const loadConversation = useCallback(async (conversationId: string): Promise<ConversationDetail> => {
    const value = await getConversation(conversationId);
    setConversation(value);
    return value;
  }, []);

  const loadRunData = useCallback(async (runId: string): Promise<void> => {
    const [nextRun, nextSpans] = await Promise.all([getRun(runId), getTrace(runId)]);
    setRun(nextRun);
    setSpans(nextSpans);
  }, []);

  const loadSystemData = useCallback(async (): Promise<void> => {
    const [nextProjects, nextAgents, nextHealth] = await Promise.all([
      loadProjects(),
      listAgents(),
      getHealth(),
    ]);
    setAgents(nextAgents);
    setHealth(nextHealth);
    if (
      nextProjects.length > 0 &&
      (selection.projectId === null ||
        !nextProjects.some((project) => project.id === selection.projectId))
    ) {
      dispatchSelection({ type: 'project', projectId: nextProjects[0]?.id ?? null });
    }
  }, [loadProjects, selection.projectId]);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    void loadSystemData().then(
      () => {
        if (!cancelled) {
          setLoading(false);
          setError(null);
        }
      },
      (failure: unknown) => {
        if (!cancelled) {
          setLoading(false);
          setError(errorMessage(failure));
        }
      },
    );
    return () => {
      cancelled = true;
    };
  }, [loadSystemData]);

  useEffect(() => {
    const projectId = selection.projectId;
    if (projectId === null) {
      setConversations([]);
      return;
    }
    let cancelled = false;
    void loadConversations(projectId).then(
      (items) => {
        if (cancelled) return;
        if (
          items.length > 0 &&
          (selection.conversationId === null ||
            !items.some((item) => item.id === selection.conversationId))
        ) {
          dispatchSelection({
            type: 'conversation',
            conversationId: items[items.length - 1]?.id ?? null,
          });
        }
        if (items.length === 0) {
          dispatchSelection({ type: 'conversation', conversationId: null });
          setConversation(null);
        }
        setError(null);
      },
      (failure: unknown) => {
        if (!cancelled) setError(errorMessage(failure));
      },
    );
    return () => {
      cancelled = true;
    };
  }, [loadConversations, selection.conversationId, selection.projectId]);

  useEffect(() => {
    const conversationId = selection.conversationId;
    if (conversationId === null) {
      setConversation(null);
      return;
    }
    let cancelled = false;
    void loadConversation(conversationId).then(
      (detail) => {
        if (cancelled) return;
        const runIds = new Set(
          detail.messages.flatMap((message) =>
            message.run_id === undefined || message.run_id === '' ? [] : [message.run_id],
          ),
        );
        if (
          selection.runId === null ||
          (runIds.size > 0 && !runIds.has(selection.runId))
        ) {
          dispatchSelection({ type: 'run', runId: latestRunId(detail) });
        }
        setError(null);
      },
      (failure: unknown) => {
        if (!cancelled) setError(errorMessage(failure));
      },
    );
    return () => {
      cancelled = true;
    };
  }, [loadConversation, selection.conversationId, selection.runId]);

  useEffect(() => {
    const runId = selection.runId;
    setEvents([]);
    setConnected(false);
    setStreamError(null);
    if (runId === null) {
      setRun(null);
      setSpans([]);
      return;
    }
    let cancelled = false;
    void loadRunData(runId).catch((failure: unknown) => {
      if (!cancelled) setError(errorMessage(failure));
    });
    return () => {
      cancelled = true;
    };
  }, [loadRunData, selection.runId]);

  useEffect(() => {
    const retained = keepSelectedSpan(selection.spanId, trace);
    if (retained !== selection.spanId) {
      dispatchSelection({ type: 'span', spanId: retained });
    }
  }, [selection.spanId, trace]);

  useEffect(() => {
    const runId = selection.runId;
    if (runId === null) return;

    const source = new EventSource(runEventsURL(runId));
    let disposed = false;
    const receive = (type: string, raw: Event): void => {
      if (disposed || !(raw instanceof MessageEvent)) return;
      const event = parseRunEvent(type, raw as MessageEvent<string>);
      setEvents((current) => mergeRunEvents(current, [event]));
      if (event.sequence !== null) {
        void loadRunData(runId).catch((failure: unknown) => {
          if (!disposed) setStreamError(errorMessage(failure));
        });
      }
      if (isTerminalEvent(event) && selection.conversationId !== null) {
        void loadConversation(selection.conversationId).catch((failure: unknown) => {
          if (!disposed) setStreamError(errorMessage(failure));
        });
      }
    };
    const listeners = namedRunEvents.map((type) => {
      const listener = (raw: Event): void => receive(type, raw);
      source.addEventListener(type, listener);
      return { type, listener };
    });

    source.onmessage = (message) => receive('message', message);
    source.onopen = () => {
      if (disposed) return;
      setConnected(true);
      setStreamError(null);
    };
    source.onerror = () => {
      if (disposed) return;
      setConnected(false);
      setStreamError('Live updates disconnected. EventSource is retrying from the last durable event.');
    };

    return () => {
      disposed = true;
      for (const { type, listener } of listeners) {
        source.removeEventListener(type, listener);
      }
      source.close();
      setConnected(false);
    };
  }, [loadConversation, loadRunData, selection.conversationId, selection.runId]);

  const perform = useCallback(async (action: () => Promise<void>): Promise<boolean> => {
    setBusy(true);
    setError(null);
    try {
      await action();
      return true;
    } catch (failure: unknown) {
      setError(errorMessage(failure));
      return false;
    } finally {
      setBusy(false);
    }
  }, []);

  const addProject = useCallback(
    async (name: string): Promise<void> => {
      await perform(async () => {
        const created = await createProject(name);
        await loadProjects();
        dispatchSelection({ type: 'project', projectId: created.id });
      });
    },
    [loadProjects, perform],
  );

  const addConversation = useCallback(
    async (title: string): Promise<void> => {
      if (selection.projectId === null) return;
      await perform(async () => {
        const created = await createConversation(selection.projectId as string, title);
        await loadConversations(selection.projectId as string);
        dispatchSelection({ type: 'conversation', conversationId: created.id });
      });
    },
    [loadConversations, perform, selection.projectId],
  );

  const sendMessage = useCallback(
    async (
      content: string,
      agentId: string,
      model: ModelRef,
      idempotencyKey: string,
    ): Promise<boolean> => {
      if (selection.conversationId === null) return false;
      return perform(async () => {
        const created = await createMessage(selection.conversationId as string, {
          content,
          agent_id: agentId,
          model,
          idempotency_key: idempotencyKey,
        });
        await loadConversation(selection.conversationId as string);
        dispatchSelection({ type: 'run', runId: created.run_id });
      });
    },
    [loadConversation, perform, selection.conversationId],
  );

  const respond = useCallback(
    async (
      kind: 'question' | 'approval',
      content: string,
      idempotencyKey: string,
    ): Promise<boolean> => {
      if (selection.runId === null) return false;
      return perform(async () => {
        await submitRunResponse(selection.runId as string, {
          kind,
          content,
          idempotency_key: idempotencyKey,
        });
        await loadRunData(selection.runId as string);
      });
    },
    [loadRunData, perform, selection.runId],
  );

  const requestCancel = useCallback(async (): Promise<void> => {
    if (selection.runId === null) return;
    await perform(async () => {
      await cancelRun(selection.runId as string);
      await loadRunData(selection.runId as string);
    });
  }, [loadRunData, perform, selection.runId]);

  const refresh = useCallback(async (): Promise<void> => {
    await perform(async () => {
      await loadSystemData();
      if (selection.projectId !== null) await loadConversations(selection.projectId);
      if (selection.conversationId !== null) await loadConversation(selection.conversationId);
      if (selection.runId !== null) await loadRunData(selection.runId);
    });
  }, [
    loadConversation,
    loadConversations,
    loadRunData,
    loadSystemData,
    perform,
    selection.conversationId,
    selection.projectId,
    selection.runId,
  ]);

  return {
    selection,
    projects,
    conversations,
    conversation,
    run,
    spans,
    trace,
    selectedSpan,
    events,
    agents,
    health,
    loading,
    busy,
    connected,
    error,
    streamError,
    selectProject: (projectId) =>
      dispatchSelection({ type: 'project', projectId }),
    selectConversation: (conversationId) =>
      dispatchSelection({ type: 'conversation', conversationId }),
    selectRun: (runId) => dispatchSelection({ type: 'run', runId }),
    selectSpan: (spanId) => dispatchSelection({ type: 'span', spanId }),
    addProject,
    addConversation,
    sendMessage,
    respond,
    requestCancel,
    refresh,
  };
}
