import { useCallback, useEffect } from "react";

import type {
  AIPlanningDraft,
  AIPlanningMessage,
  AIPlanningPlan,
  AIPlanningRequirements,
  AIPlanningSessionDetail,
} from "../../types/api";
import {
  DEFAULT_REQUIREMENTS,
} from "./planningStreamEvents";
import {
  usePlanningWorkspace,
  usePlanningWorkspaceSnapshot,
} from "./planningWorkspaceStore";

type PlanningSessionStateOptions = {
  initialSessionId: number;
  onError: (message: string) => void;
};

export function usePlanningSessionState({
  initialSessionId,
  onError,
}: PlanningSessionStateOptions) {
  const workspace = usePlanningWorkspace();
  const snapshot = usePlanningWorkspaceSnapshot();

  const sessionId = snapshot.currentSessionId;
  const currentSession =
    sessionId == null ? null : snapshot.sessions[sessionId] ?? null;

  const loadSessionList = useCallback(
    () => workspace.loadSessionList(),
    [workspace],
  );

  const loadSessionDetail = useCallback(
    (targetSessionId: number) => workspace.loadSessionDetail(targetSessionId),
    [workspace],
  );

  const createAndSelectSession = useCallback(
    () => workspace.createAndSelectSession(),
    [workspace],
  );

  const deleteAndSelectSession = useCallback(
    (deletedSessionId: number) =>
      workspace.deleteAndSelectSession(deletedSessionId),
    [workspace],
  );

  const setTranscript = useCallback(
    (updater: (transcript: AIPlanningMessage[]) => AIPlanningMessage[]) =>
      workspace.setCurrentTranscript(updater),
    [workspace],
  );

  const setDrafts = useCallback(
    (updater: (drafts: AIPlanningDraft[]) => AIPlanningDraft[]) =>
      workspace.setCurrentDrafts(updater),
    [workspace],
  );

  const setRequirements = useCallback(
    (requirements: AIPlanningRequirements) =>
      workspace.setCurrentRequirements(requirements),
    [workspace],
  );

  const setMissingSlots = useCallback(
    (missingSlots: string[]) => workspace.setCurrentMissingSlots(missingSlots),
    [workspace],
  );

  const setPlan = useCallback(
    (plan: AIPlanningPlan | null) => workspace.setCurrentPlan(plan),
    [workspace],
  );

  const setSessionId = useCallback(
    (id: number | null) => workspace.selectSession(id),
    [workspace],
  );

  const setIsBootstrapping = useCallback(
    (value: boolean) => workspace.setBootstrapping(value),
    [workspace],
  );

  const setSuggestedQuestions = useCallback(
    (questions: string[]) => workspace.setCurrentSuggestedQuestions(questions),
    [workspace],
  );

  useEffect(() => {
    let cancelled = false;

    async function initialize() {
      workspace.selectSession(initialSessionId);
      workspace.setBootstrapping(true);
      try {
        await workspace.loadSessionDetail(initialSessionId);
        if (!cancelled) {
          await workspace.loadSessionList();
        }
      } catch (error) {
        if (!cancelled) {
          onError(error instanceof Error ? error.message : String(error));
        }
      } finally {
        if (!cancelled) {
          workspace.setBootstrapping(false);
        }
      }
    }

    void initialize();
    return () => {
      cancelled = true;
    };
  }, [initialSessionId, workspace]);

  return {
    activeStreamKind: currentSession?.activeStream?.kind ?? null,
    createAndSelectSession,
    deleteAndSelectSession,
    drafts: currentSession?.drafts ?? [],
    isBootstrapping: snapshot.isBootstrapping,
    isLoadingHistory: snapshot.isLoadingHistory,
    loadSessionDetail,
    loadSessionList,
    missingSlots: currentSession?.missingSlots ?? [],
    plan: currentSession?.plan ?? null,
    requirements: currentSession?.requirements ?? DEFAULT_REQUIREMENTS,
    sessionId,
    sessionList: snapshot.sessionList,
    setDrafts,
    setIsBootstrapping,
    setMissingSlots,
    setPlan,
    setRequirements,
    setSessionId,
    setSuggestedQuestions,
    setTranscript,
    suggestedQuestions: currentSession?.suggestedQuestions ?? [],
    transcript: currentSession?.transcript ?? [],
  };
}
