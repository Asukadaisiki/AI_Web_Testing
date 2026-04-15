import type { ExecutionStreamEvent } from "../types/api";

/**
 * Build the WebSocket URL for an AI planning execution stream.
 */
function buildWsUrl(sessionId: number): string {
  const base = window.location.host;
  const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
  return `${protocol}//${base}/api/v1/ai-planning/sessions/${sessionId}/ws?user_id=1`;
}

export interface ExecutionStreamClient {
  send: (data: Record<string, unknown>) => void;
  close: () => void;
}

/**
 * Open a WebSocket connection to the planning execution stream.
 *
 * @param sessionId - The AI planning session ID.
 * @param onEvent - Callback invoked for each parsed stream event.
 * @param onError - Callback invoked when the socket errors.
 * @returns A client handle with `send` and `close` methods.
 */
export function connectExecutionStream(
  sessionId: number,
  onEvent: (event: ExecutionStreamEvent) => void,
  onError: (error: Error) => void,
): ExecutionStreamClient {
  const url = buildWsUrl(sessionId);
  const ws = new WebSocket(url);

  ws.onmessage = (ev: MessageEvent) => {
    try {
      const parsed: ExecutionStreamEvent = JSON.parse(ev.data);
      onEvent(parsed);
    } catch {
      // Ignore malformed messages
    }
  };

  ws.onerror = () => {
    onError(new Error("WebSocket connection error"));
  };

  return {
    send(data: Record<string, unknown>) {
      ws.send(JSON.stringify(data));
    },
    close() {
      ws.close();
    },
  };
}
