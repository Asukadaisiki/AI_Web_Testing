import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiError, listProjects, parseRunEvent } from './api';

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('typed API client', () => {
  it('preserves the HTTP failure when an error response is not JSON', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response('not found', {
          status: 404,
          headers: { 'Content-Type': 'text/plain' },
        }),
      ),
    );

    await expect(listProjects()).rejects.toEqual(
      new ApiError(404, 'request_failed', 'Request failed with HTTP 404.'),
    );
  });

  it('does not assign an inherited durable ID to an ephemeral token event', () => {
    const message = {
      data: '{"delta":"hello"}',
      lastEventId: '2',
    } as MessageEvent<string>;

    expect(parseRunEvent('token.delta', message)).toEqual({
      id: null,
      sequence: null,
      type: 'token.delta',
      data: { delta: 'hello' },
    });
  });
});
