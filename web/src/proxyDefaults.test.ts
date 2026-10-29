import { afterEach, describe, expect, it, vi } from 'vitest';

const apiURL = 'AGENTTRACE_API_URL';

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllEnvs();
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

  it('lets the Playwright web server inherit the configured API address', async () => {
    vi.stubEnv(apiURL, 'http://127.0.0.1:8280');
    vi.stubEnv('AGENTTRACE_E2E_WEB_URL', 'http://127.0.0.1:5284');
    const config = (await import('../playwright.config')).default;

    expect(config.webServer).toMatchObject({
      command: 'npm run dev -- --host 127.0.0.1 --port 5284',
      url: 'http://127.0.0.1:5284',
      reuseExistingServer: true,
      env: { AGENTTRACE_API_URL: 'http://127.0.0.1:8280' },
    });
    expect(config.use?.baseURL).toBe('http://127.0.0.1:5284');
  });

  it('uses the native API and web defaults without overrides', async () => {
    vi.stubEnv(apiURL, undefined);
    vi.stubEnv('AGENTTRACE_E2E_WEB_URL', undefined);
    const config = (await import('../playwright.config')).default;

    expect(config.webServer).toMatchObject({
      url: 'http://127.0.0.1:5184',
      env: { AGENTTRACE_API_URL: 'http://127.0.0.1:8180' },
    });
    expect(config.use?.baseURL).toBe('http://127.0.0.1:5184');
  });
});
