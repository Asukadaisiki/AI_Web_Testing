export interface SSEClientOptions {
  url: string;
  body: Record<string, unknown>;
  onEvent: (eventType: string, data: unknown) => void;
  onDone?: () => void;
  signal?: AbortSignal;
  timeoutMs?: number;
}

const DEFAULT_SSE_TIMEOUT_MS = 10 * 60 * 1000;

export async function callSSE(options: SSEClientOptions): Promise<void> {
  const timeout = AbortSignal.timeout(
    options.timeoutMs ?? DEFAULT_SSE_TIMEOUT_MS,
  );
  const signal = options.signal
    ? AbortSignal.any([options.signal, timeout])
    : timeout;

  const response = await fetch(options.url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(options.body),
    signal,
  });
  if (!response.ok) {
    throw new Error(`HTTP ${response.status}: ${response.statusText}`);
  }

  const reader = response.body!.getReader();
  const decoder = new TextDecoder();
  let buffer = "";

  while (true) {
    const { done, value } = await reader.read();
    if (done) {
      break;
    }
    buffer += decoder.decode(value, { stream: true });
    const parts = buffer.split("\n\n");
    buffer = parts.pop() ?? "";

    for (const part of parts) {
      let eventType = "message";
      let eventData = "{}";
      for (const line of part.split("\n")) {
        if (line.startsWith("event: ")) {
          eventType = line.slice(7);
        } else if (line.startsWith("data: ")) {
          eventData = line.slice(6);
        }
      }
      try {
        options.onEvent(eventType, JSON.parse(eventData));
      } catch {
        // Ignore malformed JSON events and continue reading the stream.
      }
    }
  }

  options.onDone?.();
}
