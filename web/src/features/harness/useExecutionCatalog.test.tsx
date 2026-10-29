// @vitest-environment jsdom
import { act, cleanup, renderHook, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { HarnessSnapshot } from '../../api';
import { useExecutionCatalog } from './useExecutionCatalog';

const harness: HarnessSnapshot = {
  id: 'browser', name: 'Browser', revision: 1, system_prompt: '', skills: [], mcp: {},
};
const runtime = { id: 'pi', name: 'Pi', available: true, model: { provider: 'provider', name: 'model' } };

function deferredResponse() {
  let resolve!: (value: Response) => void;
  const promise = new Promise<Response>((done) => { resolve = done; });
  return { promise, resolve };
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('execution catalog freshness', () => {
  it('does not restore an old Harness revision when a refresh resolves after save', async () => {
    const pending = deferredResponse();
    let refreshing = false;
    vi.stubGlobal('fetch', vi.fn(async (path: string, init?: RequestInit) => {
      if (init?.method === 'PUT') return new Response(JSON.stringify({ ...harness, revision: 2 }));
      if (path.endsWith('/runtimes')) return new Response(JSON.stringify({ runtimes: [runtime] }));
      return refreshing ? pending.promise : new Response(JSON.stringify({ harnesses: [harness] }));
    }));
    const { result } = renderHook(useExecutionCatalog);
    await waitFor(() => expect(result.current.loading).toBe(false));
    refreshing = true;
    let refresh!: Promise<void>;
    act(() => { refresh = result.current.refresh(); });
    await act(async () => { await result.current.saveHarness('browser', harness); });
    await act(async () => {
      pending.resolve(new Response(JSON.stringify({ harnesses: [harness] })));
      await refresh;
    });
    expect(result.current.harnesses[0]?.revision).toBe(2);
    expect(result.current.harnessId).toBe('browser');
    expect(result.current.loading).toBe(false);
  });

  it('removes stale runtime readiness when a refresh fails without discarding Harnesses', async () => {
    let failing = false;
    vi.stubGlobal('fetch', vi.fn(async (path: string) => (
      path.endsWith('/runtimes')
        ? failing
          ? new Response('{"error":{"message":"Runtime catalog offline"}}', { status: 503 })
          : new Response(JSON.stringify({ runtimes: [runtime] }))
        : new Response(JSON.stringify({ harnesses: [harness] }))
    )));
    const { result } = renderHook(useExecutionCatalog);
    await waitFor(() => expect(result.current.runtimes).toHaveLength(1));
    failing = true;
    await act(async () => { await result.current.refresh(); });
    expect(result.current.runtimes).toEqual([]);
    expect(result.current.harnessId).toBe('browser');
    expect(result.current.error).toContain('Runtime catalog offline');
    expect(result.current.loading).toBe(false);
  });
});
