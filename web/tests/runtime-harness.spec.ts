import { expect, test, type Page } from '@playwright/test';
import type { HarnessSnapshot } from '../src/api';

const browser: HarnessSnapshot = {
  id: 'browser-harness', name: 'Browser Harness', revision: 1,
  system_prompt: 'Verify browser behavior.', skills: [],
  mcp: { browser: { command: 'node', args: ['server.js'], env: { API_KEY: 'env:BROWSER_API_KEY' } } },
};
const generic: HarnessSnapshot = {
  id: 'generic', name: 'Generic', revision: 1, system_prompt: 'Answer carefully.', skills: [], mcp: {},
};
const runtimes = [
  { id: 'pi', name: 'Pi', available: true, model: { provider: 'openai-compatible', name: 'pi-model' } },
  { id: 'claude-code', name: 'Claude Code', available: false, unavailable_reason: 'Provider not configured', model: { provider: 'anthropic-compatible', name: 'claude-model' } },
  { id: 'codex', name: 'Codex', available: true, model: { provider: 'openai-responses', name: 'codex-model', reasoning_effort: 'high' } },
];

async function mockCatalogWorkspace(page: Page, deleteStatus: 200 | 204 = 204) {
  let harnesses = structuredClone([browser, generic]);
  let run: Record<string, unknown> | null = null;
  let messages: Record<string, unknown>[] = [];
  const conversation = {
    id: 'conversation', project_id: 'project', title: 'Checkout', metadata: {},
    created_at: '2026-10-28T09:00:00Z', updated_at: '2026-10-28T09:00:00Z',
  };
  await page.route('**/api/v1/**', async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    const method = request.method();
    let payload: unknown;
    let status = 200;
    if (path === '/api/v1/runtimes') payload = { runtimes };
    else if (path === '/api/v1/harnesses' && method === 'GET') payload = { harnesses };
    else if (path.startsWith('/api/v1/harnesses') && method !== 'GET') {
      const id = path.split('/')[4] ?? 'new-harness';
      const previous = harnesses.find((item) => item.id === id);
      if (method === 'DELETE') {
        harnesses = harnesses.filter((item) => item.id !== id);
        status = deleteStatus;
        payload = previous;
      } else {
        const snapshot = {
          ...request.postDataJSON(), id, revision: (previous?.revision ?? 0) + 1,
        } as HarnessSnapshot;
        harnesses = [...harnesses.filter((item) => item.id !== id), snapshot];
        status = method === 'POST' ? 201 : 200;
        payload = snapshot;
      }
    } else if (path === '/api/v1/projects') {
      payload = { projects: [{ ...conversation, id: 'project', name: 'Project' }] };
    } else if (path === '/api/v1/projects/project/conversations') payload = { conversations: [conversation] };
    else if (path === '/api/v1/conversations/conversation') payload = { conversation, messages, runs: run ? [run] : [] };
    else if (path === '/api/v1/conversations/conversation/messages' && method === 'POST') {
      const body = request.postDataJSON();
      run = {
        id: 'run', project_id: 'project', conversation_id: 'conversation', source_message_id: 'message',
        status: 'succeeded', checkpoint_version: 0,
        created_at: conversation.created_at, updated_at: conversation.updated_at,
        runtime_id: body.runtime_id, harness_id: body.harness_id,
        harness_revision: harnesses.find((item) => item.id === body.harness_id)?.revision,
      };
      messages = [{
        id: 'message', conversation_id: 'conversation', run_id: 'run', ordinal: 1,
        role: 'user', content: body.content, metadata: {}, created_at: conversation.created_at,
      }];
      payload = { message_id: 'message', run_id: 'run', status: 'queued' };
      status = 202;
    } else if (path === '/api/v1/runs/run') payload = run;
    else if (path === '/api/v1/runs/run/trace') {
      payload = { spans: [], attempts: [], usage: [], operation_evidence: [], derived_nodes: [] };
    } else if (path === '/api/v1/health') payload = { status: 'ok', dependencies: {} };
    else if (path.endsWith('/events')) {
      await route.fulfill({ status: 200, contentType: 'text/event-stream', body: '' });
      return;
    } else {
      status = 404;
      payload = { error: { message: `Unexpected ${method} ${path}` } };
    }
    await route.fulfill({
      status, contentType: 'application/json', body: status === 204 ? undefined : JSON.stringify(payload),
    });
  });
}

test.describe('Runtime/Harness UI with controlled API', () => {
  test('sends runtime-specific models and keeps the persisted Run composition after catalog edits', async ({ page }) => {
    await mockCatalogWorkspace(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto('/');
    await expect(page.locator('.workspace > .pane-slot')).toHaveCount(3);
    const runtime = page.getByRole('combobox', { name: 'Runtime', exact: true });
    await expect(runtime.locator('option[value="claude-code"]')).toHaveAttribute('disabled', '');
    await expect(runtime.locator('option[value="claude-code"]')).toContainText('Provider not configured');
    await runtime.selectOption('codex');
    const model = page.getByRole('combobox', { name: 'Model', exact: true });
    await expect(model).toBeDisabled();
    await expect(model.locator('option')).toHaveCount(1);
    await expect(model).toContainText('openai-responses / codex-model');
    await page.getByRole('combobox', { name: 'Harness', exact: true }).selectOption('generic');
    await page.getByRole('textbox', { name: 'Evaluation message', exact: true }).fill('Check checkout');
    const submitted = page.waitForRequest((request) => request.method() === 'POST' && request.url().endsWith('/messages'));
    await page.getByRole('button', { name: 'Send message' }).click();
    expect((await submitted).postDataJSON()).toEqual({
      content: 'Check checkout', agent_id: 'codex', runtime_id: 'codex', harness_id: 'generic',
      model: { provider: 'openai-responses', name: 'codex-model', reasoning_effort: 'high' },
      idempotency_key: expect.any(String),
    });
    const composition = page.getByLabel('Run composition');
    await expect(composition).toContainText('Runtime codex');
    await expect(composition).toContainText('Harness generic (r1)');
    await page.getByRole('button', { name: 'Manage Harnesses' }).click();
    await page.getByLabel('Harness name').fill('Renamed Generic');
    await page.getByRole('button', { name: 'Save Harness', exact: true }).click();
    await expect(page.getByRole('combobox', { name: 'Harness', exact: true })).toContainText('Renamed Generic (r2)');
    await expect(composition).toContainText('Harness generic (r1)');
    await page.getByRole('button', { name: 'Delete Harness', exact: true }).click();
    await page.getByRole('button', { name: 'Confirm delete Harness' }).click();
    await expect(page.getByRole('combobox', { name: 'Harness', exact: true })).toHaveValue('browser-harness');
    await expect(composition).toContainText('Harness generic (r1)');
    await page.reload();
    await expect(composition).toContainText('Runtime codex');
    await expect(composition).toContainText('Harness generic (r1)');
  });

  for (const deleteStatus of [200, 204] as const) {
    test(`creates, validates, saves and deletes Harnesses with DELETE ${deleteStatus}`, async ({ page }) => {
      await mockCatalogWorkspace(page, deleteStatus);
      await page.goto('/');
      await page.getByRole('button', { name: 'Manage Harnesses' }).click();
      const manager = page.getByLabel('Harness manager');
      await expect(manager).toBeVisible();
      await page.getByRole('button', { name: 'New Harness' }).click();
      await page.getByLabel('Harness name').fill('Custom checks');
      await page.getByLabel('System prompt').fill('Check the selected site.');
      await page.getByLabel('Skills JSON').fill('{');
      await expect(manager.getByRole('alert')).toContainText('valid JSON');
      await expect(page.getByRole('button', { name: 'Create Harness' })).toBeDisabled();
      await page.getByLabel('Skills JSON').fill('[{"name":"checks","content":"Check checkout","files":[{"path":"refs/check.md","content":"Check title"}]}]');
      await page.getByLabel('MCP JSON').fill('{"checks":{"command":"node","args":["checks.js"],"env":{"API_KEY":"env:CHECKS_API_KEY"},"host_only_tools":["release"]}}');
      const created = page.waitForRequest((request) => request.method() === 'POST' && request.url().endsWith('/harnesses'));
      await page.getByRole('button', { name: 'Create Harness' }).click();
      expect((await created).postDataJSON()).toMatchObject({
        name: 'Custom checks', system_prompt: 'Check the selected site.',
        skills: [{ name: 'checks', content: 'Check checkout', files: [{ path: 'refs/check.md', content: 'Check title' }] }],
        mcp: { checks: { command: 'node', args: ['checks.js'], env: { API_KEY: 'env:CHECKS_API_KEY' }, host_only_tools: ['release'] } },
      });
      await expect(page.getByRole('combobox', { name: 'Harness', exact: true })).toHaveValue('new-harness');
      await page.getByLabel('System prompt').fill('Updated prompt.');
      await page.getByLabel('Skills JSON').fill('[]');
      await page.getByLabel('MCP JSON').fill('{}');
      const saved = page.waitForRequest((request) => request.method() === 'PUT' && request.url().endsWith('/new-harness'));
      await page.getByRole('button', { name: 'Save Harness', exact: true }).click();
      expect((await saved).postDataJSON()).toEqual({
        name: 'Custom checks', system_prompt: 'Updated prompt.', skills: [], mcp: {},
      });
      await expect(manager.getByRole('status')).toContainText('(r2)');
      await page.getByRole('button', { name: 'Refresh Harnesses' }).click();
      await expect(page.getByLabel('System prompt')).toHaveValue('Updated prompt.');
      await page.getByRole('button', { name: 'Delete Harness', exact: true }).click();
      await expect(manager.getByRole('combobox')).toContainText('Custom checks');
      await page.getByRole('button', { name: 'Keep Harness' }).click();
      await expect(manager.getByRole('combobox')).toContainText('Custom checks');
      await page.getByRole('button', { name: 'Delete Harness', exact: true }).click();
      await page.getByRole('button', { name: 'Confirm delete Harness' }).click();
      await expect(manager.getByRole('combobox')).not.toContainText('Custom checks');
      await page.reload();
      await page.getByRole('button', { name: 'Manage Harnesses' }).click();
      await expect(manager.getByRole('combobox')).not.toContainText('Custom checks');
    });
  }

  test('keeps the compact manager and composer within a narrow conversation pane', async ({ page }) => {
    await mockCatalogWorkspace(page);
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto('/');
    await page.getByRole('button', { name: 'Manage Harnesses' }).click();
    const conversation = page.getByRole('main', { name: 'Conversation workspace' });
    await expect(page.getByLabel('Harness manager')).toBeVisible();
    await expect(page.getByRole('combobox', { name: 'Runtime', exact: true })).toBeVisible();
    expect(await conversation.evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true);
    expect(await page.locator('body').evaluate((element) => element.scrollWidth <= window.innerWidth)).toBe(true);
    await page.getByRole('button', { name: 'Manage Harnesses' }).click();
    await expect(page.getByLabel('Harness manager')).toBeHidden();
  });
});

test('native Harness manager CRUD persists revisions and refreshes the catalog', async ({ page }) => {
  test.skip(process.env['AGENTTRACE_E2E_LIVE_CATALOG'] !== '1', 'Requires the native API catalog');
  const apiRoot = process.env['AGENTTRACE_API_URL'] ?? 'http://127.0.0.1:8180';
  const name = `E2E Harness ${crypto.randomUUID()}`;
  let id: string | undefined;
  try {
    await page.goto('/');
    await page.getByRole('button', { name: 'Manage Harnesses' }).click();
    await page.getByRole('button', { name: 'New Harness' }).click();
    await page.getByLabel('Harness name').fill(name);
    await page.getByLabel('System prompt').fill('Answer with a short summary.');
    const created = page.waitForResponse((response) => response.request().method() === 'POST' && response.url().endsWith('/harnesses'));
    await page.getByRole('button', { name: 'Create Harness' }).click();
    const response = await created;
    expect(response.ok()).toBe(true);
    const snapshot = await response.json() as HarnessSnapshot;
    id = snapshot.id;
    expect(snapshot).toMatchObject({ name, revision: 1, skills: [], mcp: {} });
    await page.getByLabel('System prompt').fill('Revised short summary.');
    await page.getByRole('button', { name: 'Save Harness', exact: true }).click();
    await expect(page.getByLabel('Harness manager').getByRole('status')).toContainText('(r2)');
    await page.reload();
    await page.getByRole('button', { name: 'Manage Harnesses' }).click();
    await page.getByRole('combobox', { name: 'Manage Harness', exact: true }).selectOption(id);
    await expect(page.getByLabel('System prompt')).toHaveValue('Revised short summary.');
    await page.getByRole('button', { name: 'Refresh Harnesses' }).click();
    await expect(page.getByRole('combobox', { name: 'Manage Harness', exact: true })).toContainText(`${name} (r2)`);
    await page.getByRole('button', { name: 'Delete Harness', exact: true }).click();
    await page.getByRole('button', { name: 'Confirm delete Harness' }).click();
    await expect(page.getByRole('combobox', { name: 'Manage Harness', exact: true })).not.toContainText(name);
  } finally {
    if (id !== undefined) {
      const response = await page.request.delete(`${apiRoot}/api/v1/harnesses/${encodeURIComponent(id)}`);
      expect([200, 204, 404]).toContain(response.status());
    }
  }
});
