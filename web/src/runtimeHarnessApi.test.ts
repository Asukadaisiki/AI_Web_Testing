import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  ApiError,
  createHarness,
  createMessage,
  deleteHarness,
  getRun,
  listHarnesses,
  listRuntimes,
  updateHarness,
} from './api';

const definition = {
  name: 'Browser',
  system_prompt: 'Check the goal.',
  skills: [{
    name: 'browser',
    content: 'Use browser tools.',
    files: [{ path: 'examples/case.json', content: '{}' }],
  }],
  mcp: {
    browser: {
      command: 'node',
      args: ['server.js'],
      env: { TOKEN: 'env:BROWSER_TOKEN', GOAL: '{{goal}}' },
      host_only_tools: ['release'],
    },
  },
};
const snapshot = { ...definition, id: 'browser', revision: 2 };

function respond(body: unknown, status = 200): ReturnType<typeof vi.fn> {
  const fetch = vi.fn().mockResolvedValue(
    new Response(status === 204 ? null : JSON.stringify(body), { status }),
  );
  vi.stubGlobal('fetch', fetch);
  return fetch;
}

afterEach(() => vi.unstubAllGlobals());

describe('Runtime and Harness API', () => {
  it('parses public runtime readiness and model effort without leaking extra configuration', async () => {
    respond({
      runtimes: [{
        id: 'codex', name: 'Codex', available: false,
        unavailable_reason: 'Provider not configured',
        model: { provider: 'openai-responses', name: 'gpt-5', reasoning_effort: 'high' },
        credentials: 'must-not-reach-ui',
      }],
    });
    expect(await listRuntimes()).toEqual([{
      id: 'codex', name: 'Codex', available: false,
      unavailable_reason: 'Provider not configured',
      model: { provider: 'openai-responses', name: 'gpt-5', reasoning_effort: 'high' },
    }]);
  });

  it.each([
    { id: 'other', available: true },
    { id: 'pi', available: 'true' },
  ])('rejects invalid runtime identity or readiness: %j', async (fields) => {
    respond({ runtimes: [{
      ...fields, name: 'Pi', model: { provider: 'provider', name: 'model' },
    }] });
    await expect(listRuntimes()).rejects.toMatchObject({ code: 'invalid_response' });
  });

  it('round-trips all three Harness parts and its revision', async () => {
    respond({ harnesses: [snapshot] });
    expect(await listHarnesses()).toEqual([snapshot]);
  });

  it('rejects malformed nested Harness fields', async () => {
    respond({ harnesses: [{ ...snapshot, mcp: { browser: { command: 'node', args: [2] } } }] });
    await expect(listHarnesses()).rejects.toMatchObject({ code: 'invalid_response' });
    respond({ harnesses: [{ ...snapshot, revision: 1.5 }] });
    await expect(listHarnesses()).rejects.toMatchObject({ code: 'invalid_response' });
  });

  it('creates and replaces snapshots using only the editable definition', async () => {
    const fetch = respond(snapshot, 201);
    expect(await createHarness(definition)).toEqual(snapshot);
    expect(fetch).toHaveBeenCalledWith('/api/v1/harnesses', expect.objectContaining({
      method: 'POST', body: JSON.stringify(definition),
    }));
    const put = respond({ ...snapshot, revision: 3 });
    expect((await updateHarness('browser/one', definition)).revision).toBe(3);
    expect(put).toHaveBeenCalledWith('/api/v1/harnesses/browser%2Fone', expect.objectContaining({
      method: 'PUT', body: JSON.stringify(definition),
    }));
  });

  it('handles empty DELETE 204 and preserves HTTP failures', async () => {
    const fetch = respond(null, 204);
    await expect(deleteHarness('browser/one')).resolves.toBeUndefined();
    expect(fetch).toHaveBeenCalledWith('/api/v1/harnesses/browser%2Fone', { method: 'DELETE' });
    respond({ error: { code: 'in_use', message: 'Cannot delete' } }, 409);
    await expect(deleteHarness('browser')).rejects.toEqual(new ApiError(409, 'in_use', 'Cannot delete'));
  });

  it('also accepts the current backend DELETE 200 snapshot without accepting malformed success', async () => {
    respond(snapshot, 200);
    await expect(deleteHarness('browser')).resolves.toBeUndefined();
    respond({ id: 'browser' }, 200);
    await expect(deleteHarness('browser')).rejects.toMatchObject({ code: 'invalid_response' });
  });

  it('posts composition fields and preserves reasoning effort', async () => {
    const fetch = respond({ message_id: 'message', run_id: 'run', status: 'queued' }, 202);
    const body = {
      content: 'Check checkout', agent_id: 'codex',
      model: { provider: 'openai-responses', name: 'gpt-5', reasoning_effort: 'high' },
      runtime_id: 'codex' as const, harness_id: 'browser', idempotency_key: 'send-1',
    };
    await createMessage('conversation', body);
    expect(fetch).toHaveBeenCalledWith('/api/v1/conversations/conversation/messages', expect.objectContaining({
      body: JSON.stringify(body),
    }));
  });

  it('retains optional execution identity on a Run without requiring it on legacy Runs', async () => {
    const run = {
      id: 'run', project_id: 'project', conversation_id: 'conversation',
      source_message_id: 'message', status: 'queued', checkpoint_version: 0,
      created_at: 'now', updated_at: 'now',
    };
    respond({ ...run, runtime_id: 'pi', harness_id: 'browser', harness_revision: 2 });
    expect(await getRun('run')).toMatchObject({
      runtime_id: 'pi', harness_id: 'browser', harness_revision: 2,
    });
    respond(run);
    expect((await getRun('run')).runtime_id).toBeUndefined();
  });
});
