import {
  createContext,
  useContext,
  useRef,
  useSyncExternalStore,
  type ReactNode,
} from "react";

import { callSSE } from "../../shared/api/sseClient";
import {
  createPlanningSession,
  deletePlanningSession,
  getPlanningSession,
  getSessionEvents,
  listPlanningSessions,
} from "./api";
import {
  DEFAULT_REQUIREMENTS,
  reduceTranscriptEvent,
} from "./planningStreamEvents";
import type {
  AIPlanningDraft,
  AIPlanningMessage,
  AIPlanningPlan,
  AIPlanningRequirements,
  AIPlanningSessionDetail,
  AIPlanningSessionSummary,
  ExecutionStreamEvent,
} from "../../types/api";

export type PlanningStreamKind = "chat" | "drafts" | "execute";

export interface ActivePlanningStream {
  id: number;
  kind: PlanningStreamKind;
  messageId: number;
  controller: AbortController;
}

export interface PlanningSessionState {
  sessionId: number;
  transcript: AIPlanningMessage[];
  requirements: AIPlanningRequirements;
  missingSlots: string[];
  suggestedQuestions: string[];
  plan: AIPlanningPlan | null;
  drafts: AIPlanningDraft[];
  activeStream: ActivePlanningStream | null;
}

export interface PlanningWorkspaceSnapshot {
  currentSessionId: number | null;
  sessions: Record<number, PlanningSessionState>;
  sessionList: AIPlanningSessionSummary[];
  isBootstrapping: boolean;
  isLoadingHistory: boolean;
}

type Listener = () => void;

function emptySessionState(sessionId: number): PlanningSessionState {
  return {
    sessionId,
    transcript: [],
    requirements: DEFAULT_REQUIREMENTS,
    missingSlots: [],
    suggestedQuestions: [],
    plan: null,
    drafts: [],
    activeStream: null,
  };
}

function messagesFromDetail(
  detail: AIPlanningSessionDetail,
): AIPlanningMessage[] {
  return detail.messages.map((item) => {
    const payload = item.structured_payload as Record<string, unknown> | null;
    if (item.turn_type === "streaming") {
      return {
        ...item,
        structured_payload: {
          ...(payload ?? {}),
          _streaming: true,
          _interrupted: true,
        },
      };
    }
    if (payload?._streaming) {
      return {
        ...item,
        structured_payload: { ...payload, _streaming: false },
      };
    }
    return item;
  });
}

export class PlanningWorkspaceStore {
  private state: PlanningWorkspaceSnapshot;
  private listeners = new Set<Listener>();
  private nextStreamId = 1;

  constructor() {
    this.state = {
      currentSessionId: null,
      sessions: {},
      sessionList: [],
      isBootstrapping: false,
      isLoadingHistory: false,
    };
  }

  subscribe = (listener: Listener): (() => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };

  getSnapshot = (): PlanningWorkspaceSnapshot => this.state;

  private emit() {
    for (const listener of this.listeners) {
      listener();
    }
  }

  private setState(
    updater: (prev: PlanningWorkspaceSnapshot) => PlanningWorkspaceSnapshot,
  ) {
    this.state = updater(this.state);
    this.emit();
  }

  private updateSession(
    sessionId: number,
    updater: (prev: PlanningSessionState) => PlanningSessionState,
  ) {
    this.setState((prev) => ({
      ...prev,
      sessions: {
        ...prev.sessions,
        [sessionId]: updater(
          prev.sessions[sessionId] ?? emptySessionState(sessionId),
        ),
      },
    }));
  }

  get currentSession(): PlanningSessionState | null {
    const id = this.state.currentSessionId;
    return id == null ? null : this.state.sessions[id] ?? null;
  }

  selectSession(sessionId: number | null) {
    if (this.state.currentSessionId === sessionId) {
      return;
    }
    this.setState((prev) => ({ ...prev, currentSessionId: sessionId }));
  }

  setBootstrapping(value: boolean) {
    this.setState((prev) => ({ ...prev, isBootstrapping: value }));
  }

  setLoadingHistory(value: boolean) {
    this.setState((prev) => ({ ...prev, isLoadingHistory: value }));
  }

  setCurrentTranscript(
    updater: (transcript: AIPlanningMessage[]) => AIPlanningMessage[],
  ) {
    const id = this.state.currentSessionId;
    if (id == null) {
      return;
    }
    this.updateSession(id, (session) => ({
      ...session,
      transcript: updater(session.transcript),
    }));
  }

  setSessionTranscript(
    sessionId: number,
    transcript: AIPlanningMessage[],
  ) {
    this.updateSession(sessionId, (session) => ({ ...session, transcript }));
  }

  setCurrentDrafts(updater: (drafts: AIPlanningDraft[]) => AIPlanningDraft[]) {
    const id = this.state.currentSessionId;
    if (id == null) {
      return;
    }
    this.updateSession(id, (session) => ({
      ...session,
      drafts: updater(session.drafts),
    }));
  }

  setCurrentRequirements(requirements: AIPlanningRequirements) {
    const id = this.state.currentSessionId;
    if (id == null) {
      return;
    }
    this.updateSession(id, (session) => ({ ...session, requirements }));
  }

  setCurrentMissingSlots(missingSlots: string[]) {
    const id = this.state.currentSessionId;
    if (id == null) {
      return;
    }
    this.updateSession(id, (session) => ({ ...session, missingSlots }));
  }

  setCurrentPlan(plan: AIPlanningPlan | null) {
    const id = this.state.currentSessionId;
    if (id == null) {
      return;
    }
    this.updateSession(id, (session) => ({ ...session, plan }));
  }

  setCurrentSuggestedQuestions(questions: string[]) {
    const id = this.state.currentSessionId;
    if (id == null) {
      return;
    }
    this.updateSession(id, (session) => ({
      ...session,
      suggestedQuestions: questions,
    }));
  }

  async loadSessionList(): Promise<AIPlanningSessionSummary[]> {
    this.setLoadingHistory(true);
    try {
      const list = await listPlanningSessions();
      this.setState((prev) => ({ ...prev, sessionList: list }));
      return list;
    } finally {
      this.setLoadingHistory(false);
    }
  }

  /**
   * Apply a session detail snapshot. When the session has an active in-memory
   * stream, the transcript is preserved and only session-level metadata is
   * updated. Otherwise the transcript is rebuilt from persisted messages and
   * then patched by replaying persisted SSE events.
   */
  async loadSessionDetail(sessionId: number): Promise<AIPlanningSessionDetail> {
    const detail = await getPlanningSession(sessionId);
    const activeStream = this.state.sessions[sessionId]?.activeStream ?? null;

    if (activeStream) {
      this.updateSession(sessionId, (session) => ({
        ...session,
        sessionId: detail.session.id,
        requirements: detail.session.requirements,
        missingSlots: detail.session.missing_slots,
        plan: detail.session.plan ?? null,
        drafts: detail.drafts,
      }));
      this.setState((prev) => ({
        ...prev,
        currentSessionId: detail.session.id,
      }));
      return detail;
    }

    await this.applySessionDetailWithRecovery(detail);
    return detail;
  }

  private async applySessionDetailWithRecovery(
    detail: AIPlanningSessionDetail,
  ): Promise<void> {
    let messages = messagesFromDetail(detail);

    const streamingIds = messages
      .filter((item) => item.turn_type === "streaming")
      .map((item) => item.id);

    if (streamingIds.length > 0) {
      try {
        const events = await getSessionEvents(detail.session.id, 0);
        for (const streamingId of streamingIds) {
          const targetEvents = events.filter(
            (event) =>
              event.message_id === streamingId || event.message_id === null,
          );
          if (targetEvents.length === 0) {
            continue;
          }

          // Replay from an empty streaming message so content blocks are
          // rebuilt exactly once from the persisted event log.
          messages = messages.map((item) =>
            item.id === streamingId
              ? {
                  ...item,
                  content: "",
                  structured_payload: {
                    _streaming: true,
                    _interrupted: true,
                  },
                }
              : item,
          );

          for (const event of targetEvents) {
            messages = reduceTranscriptEvent(
              messages,
              event.event_data as unknown as ExecutionStreamEvent,
              streamingId,
            );
          }

          const hasCompleted = targetEvents.some(
            (event) => event.event_type === "turn_complete",
          );
          messages = messages.map((item) => {
            if (item.id !== streamingId) {
              return item;
            }
            const payload =
              (item.structured_payload as Record<string, unknown>) ?? {};
            return {
              ...item,
              turn_type: hasCompleted ? "followup" : item.turn_type,
              structured_payload: {
                ...payload,
                _streaming: !hasCompleted,
                _interrupted: false,
                _recovered: true,
              },
            };
          });
        }
      } catch {
        // Keep the interrupted marker when event replay is unavailable.
      }
    }

    this.setState((prev) => ({
      ...prev,
      currentSessionId: detail.session.id,
      sessions: {
        ...prev.sessions,
        [detail.session.id]: {
          ...(prev.sessions[detail.session.id] ?? emptySessionState(detail.session.id)),
          sessionId: detail.session.id,
          transcript: messages,
          requirements: detail.session.requirements,
          missingSlots: detail.session.missing_slots,
          suggestedQuestions: [],
          plan: detail.session.plan ?? null,
          drafts: detail.drafts,
        },
      },
    }));
  }

  async createAndSelectSession(): Promise<AIPlanningSessionDetail> {
    const detail = await createPlanningSession({});
    this.setState((prev) => ({
      ...prev,
      currentSessionId: detail.session.id,
      sessions: {
        ...prev.sessions,
        [detail.session.id]: {
          ...(prev.sessions[detail.session.id] ?? emptySessionState(detail.session.id)),
          sessionId: detail.session.id,
          transcript: messagesFromDetail(detail),
          requirements: detail.session.requirements,
          missingSlots: detail.session.missing_slots,
          suggestedQuestions: [],
          plan: detail.session.plan ?? null,
          drafts: detail.drafts,
        },
      },
    }));
    return detail;
  }

  async deleteAndSelectSession(deletedSessionId: number): Promise<void> {
    await deletePlanningSession(deletedSessionId);
    const nextList = await this.loadSessionList();
    const previousSessionId = this.state.currentSessionId;

    if (deletedSessionId !== previousSessionId) {
      return;
    }
    const nextSession = nextList[0];
    if (nextSession) {
      await this.loadSessionDetail(nextSession.id);
    } else {
      await this.createAndSelectSession();
    }
  }

  beginStream(
    sessionId: number,
    kind: PlanningStreamKind,
    messageId: number,
  ): number {
    const previous = this.state.sessions[sessionId]?.activeStream ?? null;
    if (previous) {
      previous.controller.abort();
    }
    const id = this.nextStreamId++;
    const stream: ActivePlanningStream = {
      id,
      kind,
      messageId,
      controller: new AbortController(),
    };
    this.updateSession(sessionId, (session) => ({
      ...session,
      activeStream: stream,
    }));
    return id;
  }

  endStream(sessionId: number, streamId: number) {
    const current = this.state.sessions[sessionId]?.activeStream ?? null;
    if (!current || current.id !== streamId) {
      return;
    }
    this.updateSession(sessionId, (session) => ({
      ...session,
      activeStream: null,
    }));
  }

  abortStream(sessionId?: number) {
    const targetId = sessionId ?? this.state.currentSessionId;
    if (targetId == null) {
      return;
    }
    const current = this.state.sessions[targetId]?.activeStream ?? null;
    if (!current) {
      return;
    }
    current.controller.abort();
    this.updateSession(targetId, (session) => ({
      ...session,
      activeStream: null,
    }));
  }

  async runStream(
    sessionId: number,
    kind: PlanningStreamKind,
    messageId: number,
    options: {
      url: string;
      body: Record<string, unknown>;
      onEvent?: (event: ExecutionStreamEvent) => void;
    },
  ): Promise<void> {
    const streamId = this.beginStream(sessionId, kind, messageId);
    const activeStream = this.state.sessions[sessionId]?.activeStream;
    try {
      await callSSE({
        url: options.url,
        body: options.body,
        signal: activeStream?.controller.signal,
        onEvent: (_type, data) => {
          const event = data as ExecutionStreamEvent;
          this.dispatchStreamEvent(sessionId, event);
          options.onEvent?.(event);
        },
      });
    } finally {
      this.endStream(sessionId, streamId);
    }
  }

  dispatchStreamEvent(sessionId: number, event: ExecutionStreamEvent) {
    this.updateSession(sessionId, (session) => {
      const targetId = session.activeStream?.messageId ?? null;
      if (targetId == null) {
        return session;
      }
      return {
        ...session,
        transcript: reduceTranscriptEvent(
          session.transcript,
          event,
          targetId,
        ),
      };
    });
  }
}

const PlanningWorkspaceContext = createContext<PlanningWorkspaceStore | null>(
  null,
);

export function PlanningWorkspaceProvider({
  children,
}: {
  children: ReactNode;
}) {
  const storeRef = useRef<PlanningWorkspaceStore | null>(null);
  if (storeRef.current === null) {
    storeRef.current = new PlanningWorkspaceStore();
  }
  const store = storeRef.current;

  return (
    <PlanningWorkspaceContext.Provider value={store}>
      {children}
    </PlanningWorkspaceContext.Provider>
  );
}

export function usePlanningWorkspace(): PlanningWorkspaceStore {
  const store = useContext(PlanningWorkspaceContext);
  if (store == null) {
    throw new Error(
      "usePlanningWorkspace must be used within PlanningWorkspaceProvider",
    );
  }
  return store;
}

export function usePlanningWorkspaceSnapshot(): PlanningWorkspaceSnapshot {
  const store = usePlanningWorkspace();
  return useSyncExternalStore(store.subscribe, store.getSnapshot);
}
