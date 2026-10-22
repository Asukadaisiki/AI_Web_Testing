import { expect, test, type Locator, type Page } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import { resolve } from 'node:path';

const canonicalGoal =
  'Filter alpha, add two items to the cart, and verify the cart summary.';
const screenshots = resolve(process.cwd(), '..', 'artifacts', 'ui');

let canonicalRoute = '';
let fixtureProjectId = '';
let fixtureProjectName = '';

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

function findFixtureProjectIds(): string[] {
  if (fixtureProjectName === '') return [];
  return executeFixtureSQL(
    `SELECT id FROM projects WHERE name = '${fixtureProjectName}';`,
  )
    .trim()
    .split('\n')
    .filter((id) => id !== '');
}

function fixtureRuntimeIsQuiescent(): boolean {
  if (fixtureProjectName === '') return true;
  const output = executeFixtureSQL(`WITH fixture_project AS (
      SELECT id FROM projects WHERE name = '${fixtureProjectName}'
    ), fixture_commands AS (
      SELECT event_id
      FROM outbox_events
      WHERE topic = 'agenttrace.run.commands.v1'
        AND key IN (SELECT id FROM runs WHERE project_id IN (SELECT id FROM fixture_project))
        AND published_at IS NOT NULL
    )
    SELECT
      (SELECT COUNT(*) FROM runs
       WHERE project_id IN (SELECT id FROM fixture_project)
         AND status NOT IN ('succeeded', 'failed', 'cancelled')) = 0,
      (SELECT COUNT(*) FROM fixture_commands AS command
       WHERE NOT EXISTS (
         SELECT 1 FROM inbox_events
         WHERE consumer = 'agenttrace-runtime'
           AND event_id = command.event_id
           AND processed_at IS NOT NULL
       )) = 0;`);
  return output.trim() === 't|t';
}

async function cleanupFixtureProject(): Promise<void> {
  if (fixtureProjectName === '') return;
  const fixtureIds = findFixtureProjectIds();
  expect(fixtureIds).toHaveLength(1);
  const fixtureProjectID = fixtureIds[0] ?? '';

  executeFixtureSQL(`BEGIN;
    DELETE FROM outbox_events
    WHERE topic = 'agenttrace.run.commands.v1'
      AND published_at IS NULL
      AND key IN (SELECT id FROM runs WHERE project_id = '${fixtureProjectID}');
    UPDATE runs
    SET status = 'cancelled',
        cancel_requested_at = COALESCE(cancel_requested_at, now()),
        finished_at = COALESCE(finished_at, now()),
        updated_at = now()
    WHERE project_id = '${fixtureProjectID}'
      AND status NOT IN ('succeeded', 'failed', 'cancelled');
  COMMIT;`);

  await expect.poll(fixtureRuntimeIsQuiescent, { timeout: 20_000 }).toBe(true);

  const output = executeFixtureSQL(`BEGIN;
    DELETE FROM consumer_retry_counts
    WHERE event_id IN (
      SELECT event_id FROM outbox_events
      WHERE key IN (SELECT id FROM runs WHERE project_id = '${fixtureProjectID}')
    );
    DELETE FROM inbox_events
    WHERE event_id IN (
      SELECT event_id FROM outbox_events
      WHERE key IN (SELECT id FROM runs WHERE project_id = '${fixtureProjectID}')
    );
    DELETE FROM outbox_events
    WHERE key IN (SELECT id FROM runs WHERE project_id = '${fixtureProjectID}');
    DELETE FROM idempotency_keys
    WHERE scope IN (
      SELECT 'run-response:' || id FROM runs WHERE project_id = '${fixtureProjectID}'
    ) OR response #>> '{run_id}' IN (
      SELECT id FROM runs WHERE project_id = '${fixtureProjectID}'
    );
    DELETE FROM runs WHERE project_id = '${fixtureProjectID}';
    DELETE FROM projects WHERE id = '${fixtureProjectID}';
    SELECT COUNT(*) FROM projects WHERE name = '${fixtureProjectName}';
  COMMIT;`);
  expect(output.trim()).toBe('0');
  fixtureProjectId = '';
  fixtureProjectName = '';
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

async function expectNoBoxOverlap(first: Locator, second: Locator): Promise<void> {
  const [firstBox, secondBox] = await Promise.all([visibleBox(first), visibleBox(second)]);
  expect(firstBox).not.toBeNull();
  expect(secondBox).not.toBeNull();
  if (firstBox === null || secondBox === null) return;
  const overlaps =
    firstBox.x < secondBox.x + secondBox.width &&
    secondBox.x < firstBox.x + firstBox.width &&
    firstBox.y < secondBox.y + secondBox.height &&
    secondBox.y < firstBox.y + firstBox.height;
  expect(overlaps).toBe(false);
}

async function expectSelectedModelIsFullyVisible(select: Locator): Promise<void> {
  const selectedText = await select.locator('option:checked').textContent();
  expect(selectedText).toBe('Browser evaluation');
  await expect(select).toHaveValue(await select.inputValue());
  await expect
    .poll(() =>
      select.evaluate((element, expectedText) => {
        const selected = element.selectedOptions.item(0);
        return (
          selected?.text === expectedText &&
          element.scrollWidth <= element.clientWidth
        );
      }, selectedText),
    )
    .toBe(true);
}

async function expectPaneBodyFitsViewport(page: Page, pane: Locator): Promise<void> {
  await expect
    .poll(() =>
      page.locator('body').evaluate((body) => body.scrollWidth <= window.innerWidth),
    )
    .toBe(true);
  await expect
    .poll(() => pane.evaluate((element) => element.scrollWidth <= element.clientWidth))
    .toBe(true);
}

async function createCanonicalWorkspace(page: Page): Promise<void> {
  const suffix = `${Date.now()}-${Math.floor(Math.random() * 10_000)}`;
  fixtureProjectName = `E2E canonical ${suffix}`;
  const conversationTitle = `Browser verification ${suffix}`;

  await page.getByRole('button', { name: 'Create project' }).click();
  await page.getByLabel('Project name').fill(fixtureProjectName);
  await page.getByRole('button', { name: 'Create', exact: true }).click();
  const projectSelect = page.getByRole('combobox', { name: 'Select project' });
  const fixtureProjectIds = findFixtureProjectIds();
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
  test.afterAll(async () => {
    await cleanupFixtureProject();
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
    await expectSelectedModelIsFullyVisible(page.getByRole('combobox', { name: 'Model' }));

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
    await page.screenshot({
      path: resolve(screenshots, 'narrow-390x844.png'),
      fullPage: false,
    });
    await page.getByRole('button', { name: 'Show projects and conversations' }).click();
    const projects = page.getByRole('complementary', {
      name: 'Projects and conversations',
    });
    await expect(projects).toBeVisible();
    await expectPaneBodyFitsViewport(page, projects);
    expect(page.url()).toBe(route);
    await page.getByRole('button', { name: 'Show conversation' }).click();
    const conversation = page.getByRole('main', { name: 'Conversation workspace' });
    await expect(conversation).toBeVisible();
    await expectPaneBodyFitsViewport(page, conversation);
    const agent = page.getByRole('combobox', { name: 'Agent' }).locator('xpath=..');
    const model = page.getByRole('combobox', { name: 'Model' }).locator('xpath=..');
    const send = page.getByRole('button', { name: 'Send message' });
    await expectSelectedModelIsFullyVisible(page.getByRole('combobox', { name: 'Model' }));
    await expectNoBoxOverlap(agent, model);
    await expectNoBoxOverlap(agent, send);
    await expectNoBoxOverlap(model, send);
    await page.getByRole('button', { name: 'Show trace' }).click();
    const trace = page.getByRole('complementary', { name: 'Run trace and details' });
    await expect(trace).toBeVisible();
    await expectPaneBodyFitsViewport(page, trace);
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
    await cleanupFixtureProject();
  });
});
