import { useCallback, useEffect, useRef, useState } from "react";

import {
  getAgentRun,
  listAgentEvents,
  resumeAgentToolCall,
  startAgentRun,
  subscribeAgentEvents,
} from "./api";
import { mergeAgentEvents } from "./events";
import type { AgentEvent, AgentRun } from "./types";

function storageKey(conversationId: string): string {
  return `agentservice:last-run:${conversationId}`;
}

export function useAgentRun(conversationId: string) {
  const [run, setRun] = useState<AgentRun | null>(null);
  const [events, setEvents] = useState<AgentEvent[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const closeStreamRef = useRef<(() => void) | null>(null);
  const eventsRef = useRef<AgentEvent[]>([]);

  const applyEvents = useCallback((incoming: AgentEvent[]) => {
    setEvents((current) => {
      const merged = mergeAgentEvents(current, incoming);
      eventsRef.current = merged;
      return merged;
    });
  }, []);

  const refresh = useCallback(async (runId?: string) => {
    const targetRunId =
      runId ?? localStorage.getItem(storageKey(conversationId)) ?? "";
    if (!targetRunId) {
      setLoading(false);
      return null;
    }
    try {
      const [nextRun, nextEvents] = await Promise.all([
        getAgentRun(targetRunId),
        listAgentEvents(targetRunId),
      ]);
      setRun(nextRun);
      applyEvents(nextEvents);
      setError(null);
      return nextRun;
    } catch (cause) {
      localStorage.removeItem(storageKey(conversationId));
      setRun(null);
      setEvents([]);
      eventsRef.current = [];
      setError(cause instanceof Error ? cause.message : String(cause));
      return null;
    } finally {
      setLoading(false);
    }
  }, [applyEvents, conversationId]);

  const connect = useCallback((runId: string) => {
    closeStreamRef.current?.();
    const afterSeq = eventsRef.current.at(-1)?.seq ?? 0;
    const close = subscribeAgentEvents(
      runId,
      afterSeq,
      (event) => {
        applyEvents([event]);
        if (
          event.type === "run.finished" ||
          event.type === "run.failed" ||
          event.type === "run.cancelled" ||
          event.type === "tool.pending"
        ) {
          closeStreamRef.current?.();
          closeStreamRef.current = null;
          void refresh(runId);
        }
      },
      () => {
        closeStreamRef.current = null;
        void refresh(runId);
      },
    );
    closeStreamRef.current = close;
  }, [applyEvents, refresh]);

  useEffect(() => {
    setRun(null);
    setEvents([]);
    eventsRef.current = [];
    setError(null);
    setLoading(true);
    void refresh().then((storedRun) => {
      if (storedRun?.status === "running") {
        connect(storedRun.id);
      }
    });
    return () => {
      closeStreamRef.current?.();
      closeStreamRef.current = null;
    };
  }, [connect, refresh]);

  const start = useCallback(async (projectId: number, message: string) => {
    closeStreamRef.current?.();
    setError(null);
    setEvents([]);
    eventsRef.current = [];
    const nextRun = await startAgentRun({
      conversation_id: conversationId,
      project_id: projectId,
      message,
    });
    localStorage.setItem(storageKey(conversationId), nextRun.id);
    setRun(nextRun);
    connect(nextRun.id);
    return nextRun;
  }, [connect, conversationId]);

  const resume = useCallback(async (
    answers: Record<string, unknown>,
    nextStep?: string,
  ) => {
    if (!run?.pending_tool_call_id) {
      throw new Error("当前 Run 没有等待中的工具调用");
    }
    setError(null);
    setRun({ ...run, status: "running" });
    connect(run.id);
    try {
      const nextRun = await resumeAgentToolCall(
        run.id,
        run.pending_tool_call_id,
        answers,
        nextStep,
      );
      setRun(nextRun);
      await refresh(run.id);
      return nextRun;
    } catch (cause) {
      closeStreamRef.current?.();
      closeStreamRef.current = null;
      throw cause;
    }
  }, [connect, refresh, run]);

  const reconnect = useCallback(async () => {
    if (!run) return;
    const nextRun = await refresh(run.id);
    if (nextRun?.status === "running") {
      connect(nextRun.id);
    }
  }, [connect, refresh, run]);

  return {
    error,
    events,
    loading,
    reconnect,
    resume,
    run,
    start,
  };
}
