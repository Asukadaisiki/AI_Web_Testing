import { defineConfig } from '@playwright/test';
import { tmpdir } from 'node:os';
import { resolve } from 'node:path';

const baseURL = process.env['AGENTTRACE_E2E_WEB_URL'] ?? 'http://127.0.0.1:5184';
const serverURL = new URL(baseURL);
const outputDir = process.env['AGENTTRACE_E2E_OUTPUT_DIR'] ??
  resolve(tmpdir(), 'agenttrace-native-e2e', `${Date.now()}-${process.pid}`);

export default defineConfig({
  testDir: './tests',
  testMatch: ['workspace.spec.ts', 'runtime-harness.spec.ts'],
  timeout: Number(process.env['AGENTTRACE_E2E_TIMEOUT_MS'] ?? 300_000),
  expect: { timeout: 20_000 },
  fullyParallel: false,
  workers: 1,
  reporter: 'list',
  outputDir,
  webServer: {
    command: `npm run dev -- --host ${serverURL.hostname} --port ${serverURL.port || '5184'}`,
    url: baseURL,
    reuseExistingServer: true,
    env: {
      AGENTTRACE_API_URL: process.env['AGENTTRACE_API_URL'] ?? 'http://127.0.0.1:8180',
    },
  },
  use: {
    baseURL,
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
  },
});
