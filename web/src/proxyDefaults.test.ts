import { afterEach, describe, expect, it, vi } from 'vitest';

const apiURL = 'AGENTTRACE_API_URL';

afterEach(() => {
  vi.restoreAllMocks();
  vi.resetModules();
});

describe('Vite API proxy', () => {
  it('uses the host-reachable API address when no container override is supplied', async () => {
    const original = process.env[apiURL];
    delete process.env[apiURL];

    try {
      const config = (await import('../vite.config')).default;
      const proxy = config.server?.proxy as Record<string, { target?: string }>;

      expect(proxy['/api']?.target).toBe('http://127.0.0.1:8080');
      expect(proxy['/artifacts']?.target).toBe('http://127.0.0.1:8080');
    } finally {
      if (original === undefined) {
        delete process.env[apiURL];
      } else {
        process.env[apiURL] = original;
      }
    }
  });
});
