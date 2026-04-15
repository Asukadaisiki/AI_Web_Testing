import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";

// Mock WebSocket before importing the module
class MockWebSocket {
  static instances: MockWebSocket[] = [];
  url: string;
  onopen: ((ev: Event) => void) | null = null;
  onmessage: ((ev: MessageEvent) => void) | null = null;
  onerror: ((ev: Event) => void) | null = null;
  onclose: ((ev: CloseEvent) => void) | null = null;
  readyState: number = WebSocket.CONNECTING;
  sentMessages: unknown[] = [];

  constructor(url: string) {
    this.url = url;
    MockWebSocket.instances.push(this);
    // Simulate async open
    setTimeout(() => {
      this.readyState = WebSocket.OPEN;
      this.onopen?.(new Event("open"));
    }, 0);
  }

  send(data: string) {
    this.sentMessages.push(JSON.parse(data));
  }

  close() {
    this.readyState = WebSocket.CLOSED;
    this.onclose?.(new CloseEvent("close"));
  }

  // Helper to simulate receiving a message
  _receive(data: unknown) {
    this.onmessage?.(new MessageEvent("message", { data: JSON.stringify(data) }));
  }

  // Helper to simulate an error
  _error(message: string) {
    this.onerror?.(new Event("error"));
  }
}

// Store original WebSocket
const OriginalWebSocket = globalThis.WebSocket;

beforeEach(() => {
  MockWebSocket.instances = [];
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  (globalThis as any).WebSocket = MockWebSocket;
});

afterEach(() => {
  globalThis.WebSocket = OriginalWebSocket;
});

describe("connectExecutionStream", () => {
  test("builds the planning websocket URL and forwards parsed events", async () => {
    const { connectExecutionStream } = await import("./executionWebSocket");
    const onEvent = vi.fn();
    const onError = vi.fn();

    connectExecutionStream(5, onEvent, onError);

    // Wait for the async constructor callback
    await vi.waitFor(() => {
      expect(MockWebSocket.instances.length).toBe(1);
    });

    const ws = MockWebSocket.instances[0];
    expect(ws.url).toContain("/api/v1/ai-planning/sessions/5/ws?user_id=1");

    // Simulate receiving an event
    ws._receive({ type: "save_progress", saved_count: 1, total: 2, case_name: "登录" });
    expect(onEvent).toHaveBeenCalledWith({
      type: "save_progress",
      saved_count: 1,
      total: 2,
      case_name: "登录",
    });
  });

  test("send method sends JSON message through the socket", async () => {
    const { connectExecutionStream } = await import("./executionWebSocket");
    const onEvent = vi.fn();
    const onError = vi.fn();

    const client = connectExecutionStream(3, onEvent, onError);

    await vi.waitFor(() => {
      expect(MockWebSocket.instances.length).toBe(1);
    });

    const ws = MockWebSocket.instances[0];
    client.send({ type: "execute", draft_ids: [11] });

    expect(ws.sentMessages).toEqual([{ type: "execute", draft_ids: [11] }]);
  });

  test("calls onError when WebSocket errors", async () => {
    const { connectExecutionStream } = await import("./executionWebSocket");
    const onEvent = vi.fn();
    const onError = vi.fn();

    connectExecutionStream(1, onEvent, onError);

    await vi.waitFor(() => {
      expect(MockWebSocket.instances.length).toBe(1);
    });

    MockWebSocket.instances[0]._error("connection failed");

    expect(onError).toHaveBeenCalled();
  });
});
