import { useEffect, useRef, useState } from 'react';
import { parseRunEvent, runEventsUrl, type RunEvent } from '../api';

export interface RunEventsState {
  events: RunEvent[];
  connected: boolean;
  error: string | null;
}

const BASE_RETRY_MS = 1000;
const MAX_RETRY_MS = 5000;

/**
 * 订阅 `GET /api/runs/{id}/events?from=<seq>`。
 *
 * - 断线后自行重连，重连 URL 带上 `from=<已收到的最大 seq>`，由后端重放。
 * - 收到的 seq 小于等于已记录值时丢弃（防止重放重复）。
 * - runId 变化或组件卸载时关闭 EventSource 并清理重连定时器。
 */
export function useRunEvents(runId: string | null): RunEventsState {
  const [events, setEvents] = useState<RunEvent[]>([]);
  const [connected, setConnected] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const lastSeqRef = useRef(0);

  useEffect(() => {
    lastSeqRef.current = 0;
    setEvents([]);
    setConnected(false);
    setError(null);
    if (runId === null) return;

    let cancelled = false;
    let source: EventSource | null = null;
    let timer: number | null = null;
    let attempt = 0;

    const connect = (): void => {
      if (cancelled) return;
      const url = runEventsUrl(runId, lastSeqRef.current);
      const es = new EventSource(url);
      source = es;

      es.onopen = () => {
        if (cancelled) return;
        attempt = 0;
        setConnected(true);
        setError(null);
      };

      es.onmessage = (message: MessageEvent) => {
        if (cancelled) return;
        const raw: unknown = message.data;
        if (typeof raw !== 'string') return;
        const event = parseRunEvent(raw);
        if (event === null) return;
        if (event.seq <= lastSeqRef.current) return;
        lastSeqRef.current = event.seq;
        setEvents((previous) => [...previous, event]);
      };

      es.onerror = () => {
        if (cancelled) return;
        setConnected(false);
        es.close();
        source = null;
        attempt += 1;
        const delay = Math.min(BASE_RETRY_MS * attempt, MAX_RETRY_MS);
        setError(`事件流断开，${String(delay)} ms 后从 seq=${String(lastSeqRef.current)} 重连`);
        timer = window.setTimeout(connect, delay);
      };
    };

    connect();

    return () => {
      cancelled = true;
      if (timer !== null) window.clearTimeout(timer);
      if (source !== null) source.close();
      setConnected(false);
    };
  }, [runId]);

  return { events, connected, error };
}
