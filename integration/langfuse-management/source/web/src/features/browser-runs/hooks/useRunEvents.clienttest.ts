import { act, renderHook } from "@testing-library/react";
import { useRunEvents } from "./useRunEvents";

class EventSourceStub {
  static instances: EventSourceStub[] = [];
  readonly close = vi.fn();
  onmessage: ((event: MessageEvent) => void) | null = null;
  onerror: (() => void) | null = null;
  readonly listeners = new Map<string, (event: MessageEvent) => void>();

  constructor(readonly url: string) {
    EventSourceStub.instances.push(this);
  }

  addEventListener(type: string, listener: (event: MessageEvent) => void) {
    this.listeners.set(type, listener);
  }
}

describe("useRunEvents", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    EventSourceStub.instances = [];
    vi.stubGlobal("EventSource", EventSourceStub);
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it("refreshes the actual durable cancellation event without waiting for polling", () => {
    const onRefresh = vi.fn();
    const { unmount } = renderHook(() =>
      useRunEvents({
        projectId: "p",
        runId: "run_1",
        status: "running",
        onRefresh,
      }),
    );
    act(() => {
      EventSourceStub.instances[0]!.listeners.get("run.cancel.requested")?.(
        new MessageEvent("run.cancel.requested", {
          lastEventId: "8",
          data: "{}",
        }),
      );
    });
    expect(onRefresh).toHaveBeenCalledTimes(1);
    unmount();
  });

  it("keeps one canonical stream, deduplicates sequence and refreshes intermediate and terminal state", () => {
    const onRefresh = vi.fn();
    const { rerender, unmount } = renderHook(
      ({ runId }) =>
        useRunEvents({
          projectId: "project-1",
          runId,
          onRefresh,
          status: "running",
        }),
      { initialProps: { runId: "run-1" } },
    );

    expect(EventSourceStub.instances).toHaveLength(1);
    const initial = EventSourceStub.instances[0]!;
    expect(initial.url).toContain("/api/agenttrace/events/run-1");

    act(() => {
      initial.listeners.get("run.started")?.(
        new MessageEvent("message", {
          data: JSON.stringify({ event_type: "run.started" }),
          lastEventId: "4",
        }),
      );
      initial.listeners.get("run.started")?.(
        new MessageEvent("message", {
          data: JSON.stringify({ event_type: "run.started" }),
          lastEventId: "4",
        }),
      );
      initial.onmessage?.(
        new MessageEvent("message", {
          data: JSON.stringify({ event_type: "run.completed" }),
          lastEventId: "5",
        }),
      );
    });
    expect(onRefresh).toHaveBeenCalledTimes(2);

    rerender({ runId: "run-2" });
    expect(initial.close).toHaveBeenCalledTimes(1);
    expect(EventSourceStub.instances).toHaveLength(2);

    unmount();
    expect(EventSourceStub.instances[1]!.close).toHaveBeenCalledTimes(1);
  });

  it("polls an unavailable or stale stream at a bounded cadence and stops at terminal state", () => {
    const onRefresh = vi.fn();
    const { rerender, unmount } = renderHook(
      ({ status }: { status: "running" | "failed" }) =>
        useRunEvents({ projectId: "p", runId: "run_1", status, onRefresh }),
      { initialProps: { status: "running" as "running" | "failed" } },
    );
    act(() => {
      EventSourceStub.instances[0]!.onerror?.();
      vi.advanceTimersByTime(15000);
    });
    expect(onRefresh).toHaveBeenCalledTimes(3);
    act(() => vi.advanceTimersByTime(60 * 60 * 1000));
    expect(onRefresh.mock.calls.length).toBeLessThanOrEqual(60);
    rerender({ status: "failed" });
    const calls = onRefresh.mock.calls.length;
    act(() => vi.advanceTimersByTime(60000));
    expect(onRefresh).toHaveBeenCalledTimes(calls);
    unmount();
  });
  it.each(["unavailable", "stale", "missing"] as const)(
    "exposes exhausted %s polling and re-arms recovery without a page reload",
    (transport) => {
      if (transport === "missing") vi.stubGlobal("EventSource", undefined);
      const onRefresh = vi.fn();
      const { result, rerender, unmount } = renderHook(
        ({ status }: { status: "running" | "failed" }) =>
          useRunEvents({ projectId: "p", runId: "run_1", status, onRefresh }),
        { initialProps: { status: "running" as "running" | "failed" } },
      );
      act(() => {
        if (transport === "unavailable")
          EventSourceStub.instances[0]!.onerror?.();
        vi.advanceTimersByTime(300000);
      });
      expect(onRefresh).toHaveBeenCalledTimes(60);
      expect(result.current.exhausted).toBe(true);
      act(() => vi.advanceTimersByTime(60000));
      expect(onRefresh).toHaveBeenCalledTimes(60);

      act(() => result.current.reconnect());
      expect(result.current.exhausted).toBe(false);
      expect(onRefresh).toHaveBeenCalledTimes(61);
      act(() => vi.advanceTimersByTime(5000));
      expect(onRefresh).toHaveBeenCalledTimes(62);
      if (transport !== "missing") {
        expect(EventSourceStub.instances).toHaveLength(2);
        expect(EventSourceStub.instances[0]!.close).toHaveBeenCalledTimes(1);
        act(() =>
          EventSourceStub.instances[1]!.listeners.get("run.completed")?.(
            new MessageEvent("run.completed", { lastEventId: "9", data: "{}" }),
          ),
        );
        expect(onRefresh).toHaveBeenCalledTimes(63);
      }
      rerender({ status: "failed" });
      expect(result.current.exhausted).toBe(false);
      const calls = onRefresh.mock.calls.length;
      act(() => vi.advanceTimersByTime(60000));
      expect(onRefresh).toHaveBeenCalledTimes(calls);
      unmount();
    },
  );
  it("does not carry exhaustion into another Run's subscription", () => {
    const onRefresh = vi.fn();
    const { result, rerender, unmount } = renderHook(
      ({ runId }) =>
        useRunEvents({ projectId: "p", runId, status: "running", onRefresh }),
      { initialProps: { runId: "run_1" } },
    );
    act(() => vi.advanceTimersByTime(300000));
    expect(result.current.exhausted).toBe(true);
    rerender({ runId: "run_2" });
    expect(result.current.exhausted).toBe(false);
    act(() => vi.advanceTimersByTime(5000));
    expect(onRefresh).toHaveBeenCalledTimes(61);
    rerender({ runId: "run_1" });
    expect(result.current.exhausted).toBe(true);
    act(() => vi.advanceTimersByTime(5000));
    expect(onRefresh).toHaveBeenCalledTimes(61);
    act(() => result.current.reconnect());
    expect(result.current.exhausted).toBe(false);
    act(() => vi.advanceTimersByTime(5000));
    expect(onRefresh).toHaveBeenCalledTimes(63);
    unmount();
  });
  it("uses bounded polling for a stale stream even without an error event and cleans up on unmount", () => {
    const onRefresh = vi.fn();
    const { unmount } = renderHook(() =>
      useRunEvents({
        projectId: "p",
        runId: "run_1",
        status: "queued",
        onRefresh,
      }),
    );
    act(() => vi.advanceTimersByTime(5000));
    expect(onRefresh).toHaveBeenCalledTimes(1);
    unmount();
    act(() => vi.advanceTimersByTime(15000));
    expect(onRefresh).toHaveBeenCalledTimes(1);
  });
  it("does not subscribe for terminal Runs and falls back when EventSource is unavailable", () => {
    const onRefresh = vi.fn();
    const { unmount } = renderHook(() =>
      useRunEvents({
        projectId: "p",
        runId: "run_1",
        status: "cancelled",
        onRefresh,
      }),
    );
    expect(EventSourceStub.instances).toHaveLength(0);
    unmount();
    vi.stubGlobal("EventSource", undefined);
    const next = renderHook(() =>
      useRunEvents({
        projectId: "p",
        runId: "run_2",
        status: "running",
        onRefresh,
      }),
    );
    act(() => vi.advanceTimersByTime(5000));
    expect(onRefresh).toHaveBeenCalledTimes(1);
    next.unmount();
  });
});
