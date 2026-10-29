import { expect, test, type Locator, type Page } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import { resolve } from 'node:path';
import { tmpdir } from 'node:os';

import {
  fixtureCleanupDiagnostics,
  fixtureCleanupIsSafe,
  type FixtureCleanupState,
} from '../test-utils/fixture-cleanup';

const goalURL = process.env['AGENTTRACE_E2E_GOAL_URL'] ?? 'http://127.0.0.1:8124/index.html';
let canonicalGoal = process.env['AGENTTRACE_E2E_GOAL'] ??
  `Open ${goalURL}, verify Alpha is visible using assert_text, then use finish_case to create exactly one minimal case and pass its fresh-context dry run.`;
const seedRunId = process.env['AGENTTRACE_E2E_SEED_RUN_ID'];
const screenshots = process.env['AGENTTRACE_E2E_SCREENSHOT_DIR'] ??
  resolve(tmpdir(), 'agenttrace-native-e2e-ui', `${Date.now()}-${process.pid}`);
const cleanupAPIRoot = process.env['AGENTTRACE_API_URL'] ?? 'http://127.0.0.1:8180';
const runTimeoutMS = Number(process.env['AGENTTRACE_E2E_TIMEOUT_MS'] ?? 300_000);
const cleanupTimeoutMS = 20_000;
const cleanupPollIntervalMS = 250;

let canonicalRoute = '';
let fixtureProjectId = '';
let fixtureProjectName = '';
let fixtureConversationId = '';
let operationName = '';
let modelLabel = '';

interface WaitingRunFixture {
  runId: string;
  messageId: string;
}

function postgresEnvironment(dsn: string): NodeJS.ProcessEnv {
  const url = new URL(dsn);
  if (!['postgres:', 'postgresql:'].includes(url.protocol) || url.hash) {
    throw new Error('Native E2E PostgreSQL DSN must be a PostgreSQL URI without a fragment');
  }
  const parameters: Record<string, string> = {};
  for (const [key, value] of [
    ['host', url.hostname.replace(/^\[|\]$/g, '')],
    ['port', url.port],
    ['user', url.username],
    ['password', url.password],
    ['dbname', url.pathname.replace(/^\//, '')],
  ]) {
    if (key && value) parameters[key] = decodeURIComponent(value);
  }
  for (const [key, value] of url.searchParams) parameters[key] = value;
  const variables: Record<string, string> = {
    host: 'PGHOST', hostaddr: 'PGHOSTADDR', port: 'PGPORT', user: 'PGUSER',
    password: 'PGPASSWORD', dbname: 'PGDATABASE', sslmode: 'PGSSLMODE',
    sslcert: 'PGSSLCERT', sslkey: 'PGSSLKEY', sslrootcert: 'PGSSLROOTCERT',
    sslcrl: 'PGSSLCRL', sslcrldir: 'PGSSLCRLDIR',
    ssl_min_protocol_version: 'PGSSLMINPROTOCOLVERSION',
    ssl_max_protocol_version: 'PGSSLMAXPROTOCOLVERSION',
    connect_timeout: 'PGCONNECT_TIMEOUT', options: 'PGOPTIONS',
    application_name: 'PGAPPNAME', client_encoding: 'PGCLIENTENCODING',
    target_session_attrs: 'PGTARGETSESSIONATTRS', channel_binding: 'PGCHANNELBINDING',
    gssencmode: 'PGGSSENCMODE', krbsrvname: 'PGKRBSRVNAME',
    passfile: 'PGPASSFILE', service: 'PGSERVICE',
  };
  const env = { ...process.env };
  for (const [key, value] of Object.entries(parameters)) {
    const variable = variables[key];
    if (!variable) throw new Error('Unsupported native E2E PostgreSQL connection option');
    env[variable] = value;
  }
  return env;
}

function executeFixtureSQL(sql: string): string {
  const dsn = process.env['AGENTTRACE_E2E_POSTGRES_DSN'];
  if (!dsn) throw new Error('AGENTTRACE_E2E_POSTGRES_DSN is required for native E2E fixtures');
  try {
    return execFileSync(
      'psql',
      [
        '--no-psqlrc',
        '-At',
        '-q',
        '-v',
        'ON_ERROR_STOP=1',
        '-c',
        sql,
      ],
      {
        encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'], timeout: 10_000,
        env: postgresEnvironment(dsn),
      },
    );
  } catch {
    throw new Error('Native fixture SQL failed; connection details and SQL diagnostics suppressed');
  }
}

function createWaitingRun(
  sourceRunId: string,
  status: 'waiting_input' | 'waiting_approval',
): WaitingRunFixture {
  expect(sourceRunId).toMatch(/^run_[a-zA-Z0-9]+$/);
  expect(fixtureProjectId).toMatch(/^prj_[a-zA-Z0-9]+$/);
  expect(fixtureConversationId).toMatch(/^conv_[a-zA-Z0-9]+$/);
  const runId = `run_e2e${crypto.randomUUID().replaceAll('-', '')}`;
  const messageId = `msg_e2e${crypto.randomUUID().replaceAll('-', '')}`;
  const output = executeFixtureSQL(`BEGIN;
    WITH source AS (
      SELECT '${fixtureProjectId}'::text AS project_id,
        '${fixtureConversationId}'::text AS conversation_id,
        jsonb_set(specification, '{command,conversation_id}',
          to_jsonb('${fixtureConversationId}'::text)) AS specification
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
          'content', ${sqlText(canonicalGoal)},
          'metadata', jsonb_build_object('fixture', 'task-13'),
          'created_at', now()
        ))
      )
    FROM source, next_ordinal;
    INSERT INTO messages (id, conversation_id, run_id, ordinal, role, content, metadata)
    SELECT '${messageId}', conversation_id, id,
      (specification #>> '{command,messages,0,ordinal}')::bigint,
      'user', ${sqlText(canonicalGoal)}, '{"fixture":"task-13"}'::jsonb
    FROM runs
    WHERE id = '${runId}';
    UPDATE runs SET source_message_id = '${messageId}' WHERE id = '${runId}';
    SELECT fixture.id, fixture.specification #>> '{command,run_id}',
      fixture.specification #>> '{command,messages,0,id}'
    FROM runs AS fixture
    WHERE fixture.id = '${runId}';
  COMMIT;`);
  expect(output.split('\n')).toContain(`${runId}|${runId}|${messageId}`);
  return { runId, messageId };
}

function sqlText(value: string): string {
  return `'${value.replaceAll("'", "''")}'`;
}

function routeForRun(runId: string): string {
  const url = new URL(`http://agenttrace.local${canonicalRoute}`);
  url.searchParams.set('project', fixtureProjectId);
  url.searchParams.set('conversation', fixtureConversationId);
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

function quoteFixtureIDs(ids: string[]): string {
  for (const id of ids) expect(id).toMatch(/^run_[a-zA-Z0-9]+$/);
  return ids.map((id) => `'${id}'`).join(', ');
}

function quoteFixtureEventIDs(ids: string[]): string {
  for (const id of ids) expect(id).toMatch(/^evt_[a-zA-Z0-9]+$/);
  return ids.map((id) => `'${id}'`).join(', ');
}

function findFixtureRunIds(fixtureProjectID: string): string[] {
  expect(fixtureProjectID).toMatch(/^prj_[a-zA-Z0-9]+$/);
  return executeFixtureSQL(
    `SELECT id FROM runs WHERE project_id = '${fixtureProjectID}' ORDER BY id;`,
  )
    .trim()
    .split('\n')
    .filter((id) => id !== '');
}

function findFixtureCommandEventIDs(fixtureRunIds: string[]): string[] {
  const runIDs = quoteFixtureIDs(fixtureRunIds);
  return executeFixtureSQL(
    `SELECT event_id FROM outbox_events
     WHERE topic = 'agenttrace.run.commands.v1'
       AND key IN (${fixtureRunIds.length === 0 ? "''" : runIDs})
     ORDER BY event_id;`,
  )
    .trim()
    .split('\n')
    .filter((id) => id !== '');
}

function readFixtureCleanupState(
  fixtureProjectID: string,
  fixtureRunIds: string[],
): FixtureCleanupState {
  const runIDs = quoteFixtureIDs(fixtureRunIds);
  const scopedRuns = fixtureRunIds.length === 0
    ? `SELECT id, status FROM runs WHERE false`
    : `SELECT id, status FROM runs WHERE id IN (${runIDs})`;
  const scopedOutbox = fixtureRunIds.length === 0
    ? `SELECT event_id, topic, published_at, locked_by, claim_token FROM outbox_events WHERE false`
    : `SELECT event_id, topic, published_at, locked_by, claim_token
       FROM outbox_events WHERE key IN (${runIDs})`;
  const output = executeFixtureSQL(`WITH fixture_runs AS (
      ${scopedRuns}
    ), fixture_outbox AS (
      ${scopedOutbox}
    ), fixture_commands AS (
      SELECT event_id, published_at, locked_by, claim_token
      FROM fixture_outbox
      WHERE topic = 'agenttrace.run.commands.v1'
    )
    SELECT
      ${fixtureRunIds.length},
      (SELECT COUNT(*) FROM fixture_runs),
      (SELECT COUNT(*) FROM fixture_runs
       WHERE status IN ('succeeded', 'failed', 'cancelled')),
      (SELECT COUNT(*) FROM runs
       WHERE project_id = '${fixtureProjectID}'
         AND id NOT IN (${fixtureRunIds.length === 0 ? "''" : runIDs})),
      (SELECT COUNT(*) FROM fixture_commands),
      (SELECT COUNT(*) FROM fixture_commands WHERE published_at IS NOT NULL),
      (SELECT COUNT(*) FROM fixture_commands AS command
       WHERE command.published_at IS NOT NULL AND EXISTS (
         SELECT 1 FROM inbox_events
         WHERE consumer = 'agenttrace-runtime'
           AND event_id = command.event_id
           AND processed_at IS NOT NULL
       )),
      (SELECT COUNT(*) FROM fixture_outbox WHERE published_at IS NULL);`);
  const counts = output.trim().split('|').map((value) => Number.parseInt(value, 10));
  expect(counts).toHaveLength(8);
  expect(counts.every(Number.isFinite)).toBe(true);
  return {
    expectedRunCount: counts[0] ?? 0,
    runCount: counts[1] ?? 0,
    terminalRunCount: counts[2] ?? 0,
    unexpectedRunCount: counts[3] ?? 0,
    commandCount: counts[4] ?? 0,
    publishedCommandCount: counts[5] ?? 0,
    processedCommandCount: counts[6] ?? 0,
    unpublishedOutboxCount: counts[7] ?? 0,
  };
}

async function requestFixtureCancellation(runId: string): Promise<void> {
  const response = await fetch(
    `${cleanupAPIRoot}/api/v1/runs/${encodeURIComponent(runId)}/cancel`,
    {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: '{}',
    },
  );
  if (response.status !== 202) {
    const body = (await response.text()).slice(0, 256);
    throw new Error(`fixture cleanup could not cancel ${runId}: ${response.status} ${body}`);
  }
}

async function waitForFixtureCleanupSafety(
  fixtureProjectID: string,
  fixtureRunIds: string[],
): Promise<void> {
  const deadline = Date.now() + cleanupTimeoutMS;
  let state = readFixtureCleanupState(fixtureProjectID, fixtureRunIds);
  while (!fixtureCleanupIsSafe(state)) {
    if (Date.now() >= deadline) {
      throw new Error(
        `fixture cleanup timed out without deleting data: ${fixtureCleanupDiagnostics(state)}`,
      );
    }
    await new Promise<void>((resolve) => {
      setTimeout(resolve, cleanupPollIntervalMS);
    });
    state = readFixtureCleanupState(fixtureProjectID, fixtureRunIds);
  }
}

async function cleanupFixtureProject(): Promise<void> {
  if (fixtureProjectName === '') return;
  const fixtureIds = findFixtureProjectIds();
  expect(fixtureIds).toHaveLength(1);
  const fixtureProjectID = fixtureIds[0] ?? '';
  expect(fixtureProjectID).toBe(fixtureProjectId);
  const fixtureRunIds = findFixtureRunIds(fixtureProjectID);
  const activeRunIds = executeFixtureSQL(`SELECT id FROM runs
    WHERE project_id = '${fixtureProjectID}'
      AND status NOT IN ('succeeded', 'failed', 'cancelled')
    ORDER BY id;`)
    .trim()
    .split('\n')
    .filter((id) => id !== '');
  expect(activeRunIds.every((id) => fixtureRunIds.includes(id))).toBe(true);
  await Promise.all(activeRunIds.map(requestFixtureCancellation));

  await waitForFixtureCleanupSafety(fixtureProjectID, fixtureRunIds);

  const runIDs = quoteFixtureIDs(fixtureRunIds);
  const commandEventIDs = findFixtureCommandEventIDs(fixtureRunIds);
  const quotedCommandEventIDs = quoteFixtureEventIDs(commandEventIDs);
  const output = executeFixtureSQL(`BEGIN;
    DELETE FROM consumer_retry_counts
    WHERE event_id IN (
      SELECT event_id FROM outbox_events
      WHERE key IN (${fixtureRunIds.length === 0 ? "''" : runIDs})
    );
    DELETE FROM inbox_events
    WHERE event_id IN (
      SELECT event_id FROM outbox_events
      WHERE key IN (${fixtureRunIds.length === 0 ? "''" : runIDs})
    )
      AND NOT (
        consumer = 'agenttrace-runtime'
        AND processed_at IS NOT NULL
        AND event_id IN (${commandEventIDs.length === 0 ? "''" : quotedCommandEventIDs})
      );
    DELETE FROM outbox_events
    WHERE key IN (${fixtureRunIds.length === 0 ? "''" : runIDs});
    DELETE FROM idempotency_keys
    WHERE scope IN (
      SELECT 'run-response:' || id FROM runs
      WHERE project_id = '${fixtureProjectID}' AND id IN (${fixtureRunIds.length === 0 ? "''" : runIDs})
    ) OR response #>> '{run_id}' IN (
      SELECT id FROM runs
      WHERE project_id = '${fixtureProjectID}' AND id IN (${fixtureRunIds.length === 0 ? "''" : runIDs})
    );
    DELETE FROM runs
    WHERE project_id = '${fixtureProjectID}' AND id IN (${fixtureRunIds.length === 0 ? "''" : runIDs});
    DELETE FROM projects
    WHERE id = '${fixtureProjectID}' AND name = '${fixtureProjectName}';
    SELECT
      (SELECT COUNT(*) FROM projects
       WHERE id = '${fixtureProjectID}' AND name = '${fixtureProjectName}'),
      (SELECT COUNT(*) FROM runs WHERE project_id = '${fixtureProjectID}'),
      (SELECT COUNT(*) FROM inbox_events
       WHERE consumer = 'agenttrace-runtime'
         AND event_id IN (${commandEventIDs.length === 0 ? "''" : quotedCommandEventIDs})
         AND processed_at IS NOT NULL);
  COMMIT;`);
  expect(output.trim()).toBe(`0|0|${commandEventIDs.length}`);
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
  expect(selectedText).toBe(modelLabel);
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
  fixtureConversationId = new URL(page.url()).searchParams.get('conversation') ?? '';
  expect(fixtureConversationId).toMatch(/^conv_[a-zA-Z0-9]+$/);
}

test.describe.serial('AgentTrace workspace', () => {
  test.beforeAll(() => {
    expect(executeFixtureSQL('SELECT 1;').trim()).toBe('1');
  });

  test.afterAll(async () => {
    await cleanupFixtureProject();
  });

  test('conversation attempts preserve a populated desktop trace selection', async ({
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
    const runtimesResponse = await page.request.get(`${cleanupAPIRoot}/api/v1/runtimes`);
    expect(runtimesResponse.ok()).toBe(true);
    const { runtimes } = await runtimesResponse.json();
    const runtime = runtimes.find((item: { id: string; model: { provider: string; name: string } }) =>
      item.id === 'pi' && item.model.provider === 'openai-compatible' &&
      item.model.name === 'glm-5.3-flash');
    expect(runtime, 'GET /runtimes must advertise the real Pi GLM model').toBeDefined();
    await createCanonicalWorkspace(page);
    await page.getByRole('combobox', { name: 'Runtime', exact: true }).selectOption(runtime.id);
    await page.getByRole('combobox', { name: 'Harness', exact: true }).selectOption('browser-harness');
    modelLabel = await page.getByRole('combobox', { name: 'Model' })
      .locator('option:checked').innerText();
    expect(modelLabel).toContain(runtime.model.name);

    const projects = page.getByRole('complementary', { name: 'Projects and conversations' });
    const conversation = page.getByRole('main', { name: 'Conversation workspace' });
    const trace = page.getByRole('complementary', { name: 'Run trace and details' });
    await expect(projects).toBeVisible();
    await expect(conversation).toBeVisible();
    await expect(trace).toBeVisible();
    await expectNoOverlap(projects, conversation);
    await expectNoOverlap(conversation, trace);
    await expectSelectedModelIsFullyVisible(page.getByRole('combobox', { name: 'Model' }));

    if (seedRunId) {
      expect(seedRunId).toMatch(/^run_[a-zA-Z0-9]+$/);
      const response = await page.request.get(`${cleanupAPIRoot}/api/v1/runs/${seedRunId}`);
      expect(response.ok()).toBe(true);
      const seed = await response.json();
      expect(seed.status).toBe('succeeded');
      canonicalGoal = executeFixtureSQL(
        `SELECT content FROM messages WHERE id = (
          SELECT source_message_id FROM runs WHERE id = '${seedRunId}'
        );`,
      ).trim();
      expect(canonicalGoal).not.toBe('');
      await page.goto(`/?project=${seed.project_id}&conversation=${seed.conversation_id}&run=${seedRunId}`);
    } else {
      await page.getByRole('textbox', { name: 'Evaluation message' }).fill(canonicalGoal);
      const submitted = page.waitForRequest((request) =>
        request.method() === 'POST' && request.url().endsWith('/messages'));
      await page.getByRole('button', { name: 'Send message' }).click();
      expect((await submitted).postDataJSON()).toMatchObject({
        runtime_id: 'pi', harness_id: 'browser-harness',
        model: { provider: 'openai-compatible', name: 'glm-5.3-flash' },
      });
    }
    await expect(trace.getByText('Live', { exact: true })).toBeVisible();
    await expect(page.getByRole('region', { name: 'Run completed' }).last()).toBeVisible({
      timeout: runTimeoutMS,
    });
    const runId = new URL(page.url()).searchParams.get('run');
    const runResponse = await page.request.get(`${cleanupAPIRoot}/api/v1/runs/${runId}`);
    expect(runResponse.ok()).toBe(true);
    const selectedRun = await runResponse.json();
    const composition = page.getByLabel('Run composition');
    await expect(composition).toContainText(`Runtime ${selectedRun.runtime_id}`);
    await expect(composition).toContainText(`Harness ${selectedRun.harness_id} (r${selectedRun.harness_revision})`);
    await expect(conversation.locator(`[aria-describedby="attempt-${runId}-metadata"]`))
      .toHaveAttribute('aria-pressed', 'true');
    await expect(conversation.locator('.message-block time').first()).toHaveText(
      /\d{2}:\d{2}:\d{2}/,
    );

    const traceResponse = await page.request.get(`${cleanupAPIRoot}/api/v1/runs/${runId}/trace`);
    expect(traceResponse.ok()).toBe(true);
    const nativeTrace = await traceResponse.json();
    const model = nativeTrace.spans.find((span: { kind: string; input?: unknown }) =>
      span.kind === 'model' && span.input);
    expect(model).toBeDefined();
    await trace.getByRole('button', { name: `Select ${model.name}`, exact: true }).first().click();
    const planDetails = page.getByRole('article', { name: `Trace details for ${model.name}` });
    await planDetails.getByText('Planner / case input', { exact: true }).click();
    await expect(planDetails).toContainText(canonicalGoal);
    await planDetails.getByText('Usage', { exact: true }).click();
    await expect(planDetails).toContainText('input_tokens');

    const evidenceOperation = nativeTrace.spans.find((span: { id: string; kind: string; name: string }) =>
      span.kind === 'operation' && span.name.includes('finish_case') &&
      nativeTrace.operation_evidence.some((evidence: { span_id: string }) => evidence.span_id === span.id));
    expect(evidenceOperation, 'real run must have finish_case execution evidence').toBeDefined();
    operationName = evidenceOperation.name;
    const operation = page.getByRole('treeitem', {
      name: operationName,
      exact: true,
    });
    await expect(operation).toBeVisible();
    await operation.click();
    const details = page.getByRole('article', {
      name: `Trace details for ${operationName}`,
    });
    await expect(details).toBeVisible();
    await details.getByText('Execution evidence', { exact: true }).click();
    await details.getByText('Planner / case input', { exact: true }).click();
    await details.getByText('Canonical output', { exact: true }).click();
    await expect(details).toContainText('Attempt 1');
    await expect(details).toContainText('succeeded');
    await expect(details).toContainText('case');
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
      name: `Trace details for ${operationName}`,
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
    const runtime = page.getByRole('combobox', { name: 'Runtime', exact: true }).locator('xpath=..');
    const model = page.getByRole('combobox', { name: 'Model' }).locator('xpath=..');
    const harness = page.getByRole('combobox', { name: 'Harness', exact: true }).locator('xpath=..');
    const send = page.getByRole('button', { name: 'Send message' });
    await expectSelectedModelIsFullyVisible(page.getByRole('combobox', { name: 'Model' }));
    await expectNoBoxOverlap(runtime, model);
    await expectNoBoxOverlap(runtime, harness);
    await expectNoBoxOverlap(runtime, send);
    await expectNoBoxOverlap(model, harness);
    await expectNoBoxOverlap(model, send);
    await expectNoBoxOverlap(harness, send);
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
      canonicalGoal,
    );
    await inputWait.getByRole('button', { name: 'Continue' }).click();
    await expect(inputWait).toBeHidden();
    await expect.poll(() => readRunStatus(inputFixture.runId), { timeout: runTimeoutMS })
      .toBe('succeeded');
    await expect(page.getByRole('region', { name: 'Run completed' }).last()).toBeVisible({
      timeout: runTimeoutMS,
    });
    await page.reload();
    await expect(page.getByRole('main', { name: 'Conversation workspace' })).toContainText(
      canonicalGoal,
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
    await expect.poll(() => readRunStatus(approvalFixture.runId), { timeout: runTimeoutMS })
      .toBe('succeeded');
    await expect(page.getByRole('region', { name: 'Run completed' }).last()).toBeVisible({
      timeout: runTimeoutMS,
    });
    await page.reload();
    await expect(page.getByRole('main', { name: 'Conversation workspace' })).toContainText(
      'approved',
    );
    expect(consoleErrors).toEqual([]);
    await cleanupFixtureProject();
  });
});
