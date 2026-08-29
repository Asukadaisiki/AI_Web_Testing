import { useEffect, useState } from "react";

import type {
  AIPlanningDraft,
  AIPlanningMessage,
  AIPlanningPlan,
  AIPlanningRequirements,
  AIPlanningSessionDetail,
  AIPlanningSessionSummary,
} from "../../types/api";
import {
  createPlanningSession,
  deletePlanningSession,
  getPlanningSession,
  getSessionEvents,
  listPlanningSessions,
} from "./api";

const DEFAULT_REQUIREMENTS: AIPlanningRequirements = {
  app_under_test: null,
  business_goal: null,
  entry_url_or_page: null,
  core_user_flow: null,
  main_assertions: [],
  test_data_or_account: null,
  scope_limits: null,
};

type PlanningSessionStateOptions = {
  initialSessionId: number;
  onError: (message: string) => void;
};

export function usePlanningSessionState({
  initialSessionId,
  onError,
}: PlanningSessionStateOptions) {
  const [sessionId, setSessionId] = useState<number | null>(null);
  const [transcript, setTranscript] = useState<AIPlanningMessage[]>([]);
  const [requirements, setRequirements] = useState<AIPlanningRequirements>(
    DEFAULT_REQUIREMENTS,
  );
  const [missingSlots, setMissingSlots] = useState<string[]>([]);
  const [suggestedQuestions, setSuggestedQuestions] = useState<string[]>([]);
  const [plan, setPlan] = useState<AIPlanningPlan | null>(null);
  const [drafts, setDrafts] = useState<AIPlanningDraft[]>([]);
  const [sessionList, setSessionList] = useState<AIPlanningSessionSummary[]>([]);
  const [isBootstrapping, setIsBootstrapping] = useState(false);
  const [isLoadingHistory, setIsLoadingHistory] = useState(false);

  async function loadSessionList() {
    setIsLoadingHistory(true);
    try {
      const list = await listPlanningSessions();
      setSessionList(list);
      return list;
    } finally {
      setIsLoadingHistory(false);
    }
  }

  function applySessionDetail(detail: AIPlanningSessionDetail) {
    setSessionId(detail.session.id);
    setRequirements(detail.session.requirements);
    setMissingSlots(detail.session.missing_slots);
    setSuggestedQuestions([]);
    setPlan(detail.session.plan ?? null);
    setTranscript(
      detail.messages.map((item) => {
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
      }),
    );
    setDrafts(detail.drafts);
  }

  async function applySessionDetailWithRecovery(
    detail: AIPlanningSessionDetail,
  ) {
    applySessionDetail(detail);
    const interruptedMessage = detail.messages.find(
      (item) => item.turn_type === "streaming",
    );
    if (!interruptedMessage) {
      return;
    }

    try {
      const events = await getSessionEvents(detail.session.id, 0);
      let recoveredContent = interruptedMessage.content;
      let recoveredPhase: string | null = null;
      let recoveredPhaseMessage: string | null = null;
      let hasCompleted = false;
      let thinkingContent = "";

      for (const event of events) {
        if (
          event.message_id !== interruptedMessage.id
          && event.message_id !== null
        ) {
          continue;
        }
        const data = event.event_data as Record<string, unknown>;
        if (event.event_type === "text_chunk") {
          if (data.thinking) {
            thinkingContent += (data.text as string) || "";
          } else {
            recoveredContent += (data.text as string) || "";
          }
        } else if (event.event_type === "status") {
          recoveredPhase = (data.phase as string) || recoveredPhase;
          recoveredPhaseMessage =
            (data.message as string) || recoveredPhaseMessage;
        } else if (event.event_type === "tool_call_start") {
          recoveredPhase = "tool_calling";
          recoveredPhaseMessage = `正在调用工具: ${data.tool || ""}`;
        } else if (event.event_type === "tool_call_end") {
          recoveredPhase = "thinking";
          recoveredPhaseMessage = "正在分析需求...";
        } else if (event.event_type === "turn_complete") {
          hasCompleted = true;
          const payload = data.payload as Record<string, unknown> | undefined;
          if (payload?.assistant_message) {
            recoveredContent = payload.assistant_message as string;
          }
        }
      }

      setTranscript((current) =>
        current.map((item) => {
          if (item.id !== interruptedMessage.id) {
            return item;
          }
          const payload = item.structured_payload ?? {};
          return {
            ...item,
            turn_type: hasCompleted ? "followup" : item.turn_type,
            content: recoveredContent || item.content,
            structured_payload: {
              ...payload,
              _streaming: !hasCompleted,
              _interrupted: false,
              _recovered: true,
              ...(!hasCompleted
                ? {
                    _phase: recoveredPhase || "thinking",
                    _phaseMessage: recoveredPhaseMessage || "正在恢复...",
                  }
                : {}),
              ...(thinkingContent
                ? { _thinkingContent: thinkingContent }
                : {}),
            },
          };
        }),
      );
    } catch {
      // Keep the interrupted marker when replay is unavailable.
    }
  }

  async function loadSessionDetail(targetSessionId: number) {
    try {
      const detail = await getPlanningSession(targetSessionId);
      await applySessionDetailWithRecovery(detail);
      return detail;
    } catch (error) {
      await loadSessionList().catch(() => undefined);
      throw error;
    }
  }

  async function createAndSelectSession() {
    const detail = await createPlanningSession({});
    applySessionDetail(detail);
    return detail;
  }

  async function deleteAndSelectSession(deletedSessionId: number) {
    await deletePlanningSession(deletedSessionId);
    const nextList = await loadSessionList();
    if (deletedSessionId !== sessionId) {
      return;
    }
    const nextSession = nextList[0];
    if (nextSession) {
      await loadSessionDetail(nextSession.id);
    } else {
      await createAndSelectSession();
    }
  }

  useEffect(() => {
    let cancelled = false;

    async function initialize() {
      setIsBootstrapping(true);
      try {
        await loadSessionDetail(initialSessionId);
        if (!cancelled) {
          await loadSessionList();
        }
      } catch (error) {
        if (!cancelled) {
          onError(error instanceof Error ? error.message : String(error));
        }
      } finally {
        if (!cancelled) {
          setIsBootstrapping(false);
        }
      }
    }

    void initialize();
    return () => {
      cancelled = true;
    };
  }, [initialSessionId]);

  return {
    applySessionDetail,
    createAndSelectSession,
    deleteAndSelectSession,
    drafts,
    isBootstrapping,
    isLoadingHistory,
    loadSessionDetail,
    loadSessionList,
    missingSlots,
    plan,
    requirements,
    sessionId,
    sessionList,
    setDrafts,
    setIsBootstrapping,
    setMissingSlots,
    setPlan,
    setRequirements,
    setSessionId,
    setSessionList,
    setSuggestedQuestions,
    setTranscript,
    suggestedQuestions,
    transcript,
  };
}
