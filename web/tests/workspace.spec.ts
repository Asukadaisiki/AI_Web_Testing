import { expect, test, type Locator, type Page } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import { resolve } from 'node:path';

const canonicalGoal =
  'Filter alpha, add two items to the cart, and verify the cart summary.';
const screenshots = resolve(process.cwd(), '..', 'artifacts', 'ui');

let canonicalRoute = '';
let fixtureProjectId = '';

interface WaitingRunFixture {
  runId: string;
  messageId: string;
}

function executeFixtureSQL(sql: string): string {
  return execFileSync(
    'docker',
    [
      'compose',
      '-f',
      resolve(process.cwd(), '..', 'compose.yaml'),
      'exec',
      '-T',
      'postgres',
      'psql',
      '-U',
      'agenttrace',
      '-d',
      'agenttrace',
      '-At',
      '-q',
      '-v',
      'ON_ERROR_STOP=1',
      '-c',
      sql,
    ],
    { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] },
  );
}

function createWaitingRun(
  sourceRunId: string,
  status: 'waiting_input' | 'waiting_approval',
): WaitingRunFixture {
  expect(sourceRunId).toMatch(/^run_[a-zA-Z0-9]+$/);
  const runId = `run_e2e${crypto.randomUUID().replaceAll('-', '')}`;
  const messageId = `msg_e2e${crypto.randomUUID().replaceAll('-', '')}`;
  const stateLabel = status === 'waiting_input' ? 'input' : 'approval';
  const output = executeFixtureSQL(`BEGIN;
    WITH source AS (
      SELECT project_id, conversation_id, specification
      FROM runs
      WHERE id = '${sourceRunId}' AND status = 'succeeded'
    ), next_ordinal AS (
      SELECT COALESCE(MAX(ordinal), 0) + 1 AS ordinal
      FROM messages
      WHERE conversation_id = (SELECT conversation_id FROM source)
    )
    INSERT INTO runs (id, project_id, conversation_id, status, specification)
    SELECT '${runId}', source.project_id, source.conversation_id, '${status}',
      jsonb_set(
        jsonb_set(source.specification, '{command,run_id}', to_jsonb('${runId}'::text)),
        '{command,messages}',
        jsonb_build_array(jsonb_build_object(
          'id', '${messageId}',
          'conversation_id', source.conversation_id,
          'run_id', '${runId}',
          'ordinal', next_ordinal.ordinal,
          'role', 'user',
          'content', 'E2E fixture waiting for ${stateLabel}.',
          'metadata', jsonb_build_object('fixture', 'task-13'),
          'created_at', now()
        ))
      )
    FROM source, next_ordinal;
    INSERT INTO messages (id, conversation_id, run_id, ordinal, role, content, metadata)
    SELECT '${messageId}', conversation_id, id,
      (specification #>> '{command,messages,0,ordinal}')::bigint,
      'user', 'E2E fixture waiting for ${stateLabel}.', '{"fixture":"task-13"}'::jsonb
    FROM runs
    WHERE id = '${runId}';
    SELECT fixture.id, fixture.specification #>> '{command,run_id}',
      fixture.specification #>> '{command,messages,0,id}'
    FROM runs AS fixture
    WHERE fixture.id = '${runId}';
  COMMIT;`);
  expect(output.split('\n')).toContain(`${runId}|${runId}|${messageId}`);
  return { runId, messageId };
}

function routeForRun(runId: string): string {
  const url = new URL(`http://agenttrace.local${canonicalRoute}`);
  url.searchParams.set('run', runId);
  url.searchParams.delete('span');
  return `${url.pathname}${url.search}`;
}

function readRunStatus(runId: string): string {
  return executeFixtureSQL(
    `SELECT status FROM runs WHERE id = '${runId}';`,
  ).trim();
}

function cleanupFixtureProject(): void {
  if (fixtureProjectId === '') return;
  const output = executeFixtureSQL(`BEGIN;
    DELETE FROM outbox_events
    WHERE key IN (SELECT id FROM runs WHERE project_id = '${fixtureProjectId}');
    DELETE FROM consumer_retry_counts AS retries
    WHERE EXISTS (
      SELECT 1 FROM runs
      WHERE project_id = '${fixtureProjectId}'
        AND retries.payload::text LIKE '%' || runs.id || '%'
    );
    DELETE FROM inbox_events AS inbox
    WHERE EXISTS (
      SELECT 1 FROM runs
      WHERE project_id = '${fixtureProjectId}'
        AND inbox.payload::text LIKE '%' || runs.id || '%'
    );
    DELETE FROM runs WHERE project_id = '${fixtureProjectId}';
    DELETE FROM projects WHERE id = '${fixtureProjectId}';
    SELECT COUNT(*) FROM projects WHERE id = '${fixtureProjectId}';
  COMMIT;`);
  expect(output.trim()).toBe('0');
  fixtureProjectId = '';
}

function visibleBox(locator: Locator): Promise<ReturnType<Locator['boundingBox']>> {
  return locator.boundingBox();
}

async function expectNoOverlap(first: Locator, second: Locator): Promise<void> {
  const [firstBox, secondBox] = await Promise.all([visibleBox(first), visibleBox(second)]);
  expect(firstBox).not.toBeNull();
  expect(secondBox).not.toBeNull();
  if (firstBox === null || secondBox === null) return;
  expect(firstBox.x + firstBox.width).toBeLessThanOrEqual(secondBox.x);
}

async function createCanonicalWorkspace(page: Page): Promise<void> {
  const suffix = `${Date.now()}-${Math.floor(Math.random() * 10_000)}`;
  const projectName = `E2E canonical ${suffix}`;
  const conversationTitle = `Browser verification ${suffix}`;

  await page.getByRole('button', { name: 'Create project' }).click();
  await page.getByLabel('Project name').fill(projectName);
  await page.getByRole('button', { name: 'Create', exact: true }).click();
  const projectSelect = page.getByRole('combobox', { name: 'Select project' });
  const fixtureProjectIds = executeFixtureSQL(
    `SELECT id FROM projects WHERE name = '${projectName}';`,
  )
    .trim()
    .split('\n')
    .filter((id) => id !== '');
  expect(fixtureProjectIds).toHaveLength(1);
  fixtureProjectId = fixtureProjectIds[0] ?? '';
  expect(fixtureProjectId).toMatch(/^prj_[a-zA-Z0-9]+$/);
  await expect(projectSelect).toHaveValue(fixtureProjectId);
  await expect
    .poll(() => new URL(page.url()).searchParams.get('project'))
    .toBe(fixtureProjectId);

  await page.getByRole('button', { name: 'Create conversation' }).click();
  await page.getByLabel('Conversation title').fill(conversationTitle);
  await page.getByRole('button', { name: 'Create', exact: true }).click();
  await expect(page.getByRole('main', { name: 'Conversation workspace' })).toContainText(
    conversationTitle,
  );
}

test.describe.serial('AgentTrace workspace', () => {
  test.afterAll(() => {
    cleanupFixtureProject();
  });

  test('sends the canonical goal and preserves a populated desktop trace selection', async ({
    page,
  }) => {
    const consoleErrors: string[] = [];
    page.on('console', (message) => {
      if (message.type() === 'error') consoleErrors.push(message.text());
    });
    page.on('pageerror', (error) => consoleErrors.push(error.message));

    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto('/');
    await expect(page.getByRole('status', { name: 'Loading workspace...' })).toBeHidden();
    await createCanonicalWorkspace(page);

    const projects = page.getByRole('complementary', { name: 'Projects and conversations' });
    const conversation = page.getByRole('main', { name: 'Conversation workspace' });
    const trace = page.getByRole('complementary', { name: 'Run trace and details' });
    await expect(projects).toBeVisible();
    await expect(conversation).toBeVisible();
    await expect(trace).toBeVisible();
    await expectNoOverlap(projects, conversation);
    await expectNoOverlap(conversation, trace);

    await page.getByRole('textbox', { name: 'Evaluation message' }).fill(canonicalGoal);
    await page.getByRole('button', { name: 'Send message' }).click();
    await expect(page.getByLabel(/Current run (queued|running|succeeded)/)).toBeVisible();
    await expect(trace.getByText('Live', { exact: true })).toBeVisible();
    await expect(page.getByRole('region', { name: 'Run completed' })).toBeVisible({
      timeout: 80_000,
    });

    const operation = page.getByRole('treeitem', {
      name: 'Trace node: operation',
    });
    await expect(operation).toBeVisible();
    await operation.click();
    const details = page.getByRole('article', {
      name: 'Trace details for operation',
    });
    await expect(details).toBeVisible();
    await details.getByText('Execution evidence', { exact: true }).click();
    await details.getByText('Planner / case input', { exact: true }).click();
    await details.getByText('Canonical output', { exact: true }).click();
    await expect(details).toContainText('Attempt 1');
    await expect(details).toContainText('Filter alpha');
    await expect(details).toContainText('Alpha x 2');
    await expect(details).toContainText('artifacts');

    await page.screenshot({
      path: resolve(screenshots, 'desktop-1440x900.png'),
      fullPage: false,
    });

    await expect
      .poll(() => page.url())
      .toContain('project=');
    canonicalRoute = new URL(page.url()).pathname + new URL(page.url()).search;
    expect(canonicalRoute).toContain('conversation=');
    expect(canonicalRoute).toContain('run=');
    expect(canonicalRoute).toContain('span=');

    await page.reload();
    await expect(page.getByRole('main', { name: 'Conversation workspace' })).toContainText(
      canonicalGoal,
    );
    await expect(page.getByRole('article', {
      name: 'Trace details for operation',
    })).toBeVisible();
    expect(consoleErrors).toEqual([]);
  });

  test('switches populated narrow panes without changing the workspace route', async ({
    page,
  }) => {
    expect(canonicalRoute).not.toBe('');
    const consoleErrors: string[] = [];
    page.on('console', (message) => {
      if (message.type() === 'error') consoleErrors.push(message.text());
    });
    page.on('pageerror', (error) => consoleErrors.push(error.message));

    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto(canonicalRoute);
    const route = page.url();
    await expect(page.getByRole('main', { name: 'Conversation workspace' })).toContainText(
      canonicalGoal,
    );
    await page.getByRole('button', { name: 'Show trace' }).click();
    await expect(page.getByRole('complementary', { name: 'Run trace and details' })).toBeVisible();
    expect(page.url()).toBe(route);
    await expect
      .poll(() =>
        page.locator('body').evaluate((body) => body.scrollWidth <= window.innerWidth),
      )
      .toBe(true);
    await page.screenshot({
      path: resolve(screenshots, 'narrow-390x844.png'),
      fullPage: false,
    });
    await page.getByRole('button', { name: 'Show projects and conversations' }).click();
    await expect(page.getByRole('complementary', {
      name: 'Projects and conversations',
    })).toBeVisible();
    expect(page.url()).toBe(route);
    expect(consoleErrors).toEqual([]);
  });

  test('submits question and approval blocks through the live backend', async ({ page }) => {
    expect(canonicalRoute).not.toBe('');
    const consoleErrors: string[] = [];
    page.on('console', (message) => {
      if (message.type() === 'error') consoleErrors.push(message.text());
    });
    page.on('pageerror', (error) => consoleErrors.push(error.message));

    const sourceRunId = new URL(`http://agenttrace.local${canonicalRoute}`).searchParams.get('run');
    expect(sourceRunId).not.toBeNull();
    if (sourceRunId === null) return;

    const inputFixture = createWaitingRun(sourceRunId, 'waiting_input');
    expect(readRunStatus(sourceRunId)).toBe('succeeded');
    expect(readRunStatus(inputFixture.runId)).toBe('waiting_input');
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto(routeForRun(inputFixture.runId));
    const inputWait = page.getByRole('region', { name: 'Run waiting for input' });
    await expect(inputWait).toBeVisible();
    await page.getByRole('textbox', { name: 'Response to agent question' }).fill(
      'Use the alpha item quantity of two.',
    );
    await inputWait.getByRole('button', { name: 'Continue' }).click();
    await expect(inputWait).toBeHidden();
    await expect(page.getByRole('region', { name: 'Run completed' })).toBeVisible({
      timeout: 80_000,
    });
    await page.reload();
    await expect(page.getByRole('main', { name: 'Conversation workspace' })).toContainText(
      'Use the alpha item quantity of two.',
    );

    const approvalFixture = createWaitingRun(sourceRunId, 'waiting_approval');
    expect(readRunStatus(sourceRunId)).toBe('succeeded');
    expect(readRunStatus(approvalFixture.runId)).toBe('waiting_approval');
    await page.goto(routeForRun(approvalFixture.runId));
    const approvalWait = page.getByRole('region', {
      name: 'Run waiting for approval',
    });
    await expect(approvalWait).toBeVisible();
    await approvalWait.getByRole('button', { name: 'Approve' }).click();
    await expect(approvalWait).toBeHidden();
    await expect(page.getByRole('region', { name: 'Run completed' })).toBeVisible({
      timeout: 80_000,
    });
    await page.reload();
    await expect(page.getByRole('main', { name: 'Conversation workspace' })).toContainText(
      'approved',
    );
    expect(consoleErrors).toEqual([]);
    cleanupFixtureProject();
  });
});
