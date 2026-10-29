import { useEffect, useRef, useState } from "react";
import type { AgentTraceRun } from "../contracts";

const terminalStatuses = ["succeeded", "failed", "cancelled"];
const stateEvents = [
  "run.requested",
  "run.started",
  "run.cancel.requested",
  "run.continuation.requested",
  "run.response.received",
  "run.completed",
  "harness.started",
  "harness.settled",
  "harness.failed",
  "browser.case.completed",
  "runtime.step.completed",
];

export function useRunEvents({
  projectId,
  runId,
  status,
  onRefresh,
}: {
  projectId: string;
  runId: string | undefined;
  status: AgentTraceRun["status"] | undefined;
  onRefresh: () => void;
}) {
  const refreshRef = useRef(onRefresh);
  refreshRef.current = onRefresh;
  const [attempt, setAttempt] = useState(0);
  const [exhaustedSubscription, setExhaustedSubscription] = useState<string>();
  const subscription = JSON.stringify([projectId, runId, attempt]);

  const terminal = Boolean(status && terminalStatuses.includes(status));
  // Synchronize the external SSE connection and its bounded fallback timer.
  useEffect(() => {
    if (
      !projectId ||
      !runId ||
      terminal ||
      exhaustedSubscription === subscription
    )
      return;

    let lastSequence = 0;
    let lastEventAt = Date.now();
    let polls = 0;
    let unavailable = false;
    let closed = false;
    const url = new URL(
      `/api/agenttrace/events/${encodeURIComponent(runId)}`,
      window.location.origin,
    );
    url.searchParams.set("projectId", projectId);
    let eventSource: EventSource | undefined;
    try {
      eventSource = new EventSource(url.toString());
    } catch {
      unavailable = true;
    }
    const timer = window.setInterval(() => {
      if (unavailable || Date.now() - lastEventAt >= 5000) {
        refreshRef.current();
        polls += 1;
        if (polls >= 60) {
          window.clearInterval(timer);
          eventSource?.close();
          closed = true;
          setExhaustedSubscription(subscription);
        }
      }
    }, 5000);

    const acceptDurableEvent = (event: MessageEvent, type: string) => {
      if (closed) return;
      if (event.lastEventId) {
        const sequence = Number(event.lastEventId);
        if (!Number.isSafeInteger(sequence) || sequence <= lastSequence) {
          return false;
        }
        lastSequence = sequence;
      }
      lastEventAt = Date.now();
      unavailable = false;
      refreshRef.current();
      if (type === "run.completed") {
        window.clearInterval(timer);
        eventSource?.close();
        closed = true;
      }
    };

    stateEvents.forEach((type) => {
      eventSource?.addEventListener(type, (event) =>
        acceptDurableEvent(event as MessageEvent, type),
      );
    });

    if (eventSource) {
      eventSource.onerror = () => {
        unavailable = true;
      };
      eventSource.onmessage = (event) => {
        try {
          const payload = JSON.parse(event.data) as { event_type?: string };
          if (payload.event_type && stateEvents.includes(payload.event_type)) {
            acceptDurableEvent(event, payload.event_type);
          }
        } catch {
          // Ignore malformed upstream event data; the next valid durable event resumes the stream.
        }
      };
    }

    return () => {
      window.clearInterval(timer);
      if (!closed) eventSource?.close();
      closed = true;
    };
  }, [projectId, runId, terminal, subscription, exhaustedSubscription]);

  return {
    exhausted: !terminal && exhaustedSubscription === subscription,
    reconnect: () => {
      if (!projectId || !runId || terminal) return;
      setAttempt((current) => current + 1);
      refreshRef.current();
    },
  };
}
