import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  ApiError,
  getConversation,
  getTrace,
  listAgents,
  listProjects,
  parseRunEvent,
  rerunRun,
} from './api';

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

  it('rejects malformed successful JSON before it reaches workspace state', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response('{"projects":null}', {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    );

    await expect(listProjects()).rejects.toEqual(
      new ApiError(
        200,
        'invalid_response',
        'Invalid API response at projects: expected an array.',
      ),
    );
  });

  it('parses conversation runs with their public lineage', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            conversation: {
              id: 'conversation-1',
              project_id: 'project-1',
              title: 'Checkout',
              metadata: {},
              created_at: '2026-10-25T12:00:00Z',
              updated_at: '2026-10-25T12:05:00Z',
            },
            messages: [{
              id: 'message-1',
              conversation_id: 'conversation-1',
              run_id: 'run-original',
              ordinal: 1,
              role: 'user',
              content: 'Check checkout',
              created_at: '2026-10-25T12:00:00Z',
            }],
            runs: [{
              id: 'run-original',
              project_id: 'project-1',
              conversation_id: 'conversation-1',
              source_message_id: 'message-1',
              status: 'failed',
              checkpoint_version: 1,
              created_at: '2026-10-25T12:00:01Z',
              updated_at: '2026-10-25T12:01:00Z',
            }, {
              id: 'run-rerun',
              project_id: 'project-1',
              conversation_id: 'conversation-1',
              source_message_id: 'message-1',
              retry_of_run_id: 'run-original',
              status: 'queued',
              checkpoint_version: 0,
              created_at: '2026-10-25T12:05:00Z',
              updated_at: '2026-10-25T12:05:00Z',
            }],
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        ),
      ),
    );

    const detail = await getConversation('conversation-1');

    expect(detail.runs).toHaveLength(2);
    expect(detail.runs[0]).toMatchObject({
      id: 'run-original',
      source_message_id: 'message-1',
    });
    expect(detail.runs[0]?.retry_of_run_id).toBeUndefined();
    expect(detail.runs[1]).toMatchObject({
      id: 'run-rerun',
      source_message_id: 'message-1',
      retry_of_run_id: 'run-original',
    });
  });

  it('parses observable decision context on a model span', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            spans: [{
              id: 'span-row-1',
              run_id: 'run-1',
              trace_id: 'trace-1',
              span_id: 'model-1',
              kind: 'model',
              name: 'plan checkout',
              status: 'succeeded',
              decision_context: {
                objective: { role: 'user', content: 'Check checkout' },
                prior_observation: { role: 'tool', content: 'Login succeeded' },
                selected_operation: {
                  name: 'browser_execute_case',
                  arguments: null,
                },
                visible_explanation: 'I will validate checkout.',
              },
              started_at: '2026-10-25T12:00:00Z',
              created_at: '2026-10-25T12:00:00Z',
              updated_at: '2026-10-25T12:00:01Z',
            }],
            attempts: [],
            usage: [],
            operation_evidence: [],
            derived_nodes: [],
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        ),
      ),
    );

    const trace = await getTrace('run-1');

    expect(trace.spans[0]?.decision_context).toEqual({
      objective: { role: 'user', content: 'Check checkout' },
      prior_observation: { role: 'tool', content: 'Login succeeded' },
      selected_operation: {
        name: 'browser_execute_case',
        arguments: null,
      },
      visible_explanation: 'I will validate checkout.',
    });
  });

  it('posts a rerun request and parses the accepted run', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            run_id: 'run-new',
            status: 'queued',
            retry_of_run_id: 'run-failed',
          }),
          { status: 202, headers: { 'Content-Type': 'application/json' } },
        ),
      ),
    );

    await expect(rerunRun('run-failed', {
      idempotency_key: 'rerun-1',
    })).resolves.toEqual({
      run_id: 'run-new',
      status: 'queued',
      retry_of_run_id: 'run-failed',
    });
  });

  it('parses canonical attempts usage and operation evidence with the trace', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            spans: [{
              id: 'span-row-1',
              run_id: 'run-1',
              trace_id: 'trace-1',
              span_id: 'operation-1',
              kind: 'operation',
              name: 'browser_execute_case',
              status: 'failed',
              input: { case: { name: 'checkout' } },
              started_at: '2026-10-21T12:00:00Z',
              created_at: '2026-10-21T12:00:00Z',
              updated_at: '2026-10-21T12:00:01Z',
            }],
            attempts: [{
              id: 'attempt-1',
              span_id: 'operation-1',
              attempt_number: 1,
              operation_id: 'operation-call-1',
              status: 'failed',
              error: { message: 'browser failed' },
              started_at: '2026-10-21T12:00:00Z',
            }],
            usage: [{
              id: 'usage-1',
              span_id: 'operation-1',
              span_attempt_id: 'attempt-1',
              provider: 'openai-compatible',
              model: 'fixture',
              input_tokens: 12,
              output_tokens: 8,
              total_tokens: 20,
              cost_micros: 44,
              details: { cached_tokens: 2 },
              created_at: '2026-10-21T12:00:01Z',
            }],
            operation_evidence: [{
              id: 'evidence-1',
              span_id: 'operation-1',
              span_attempt_id: 'attempt-1',
              operation_id: 'operation-call-1',
              status: 'failed',
              result: { steps: [{ status: 'failed' }] },
              artifacts: [{ kind: 'screenshot', path: 'failure.png' }],
              error: { message: 'browser failed' },
              created_at: '2026-10-21T12:00:01Z',
              updated_at: '2026-10-21T12:00:01Z',
            }],
            derived_nodes: [{
              id: 'derived:operation-1:step:0',
              parent_span_id: 'operation-1',
              source_evidence_id: 'evidence-1',
              kind: 'step',
              name: 'Open checkout',
              status: 'passed',
              started_at: '2026-10-21T12:00:00Z',
              ended_at: '2026-10-21T12:00:01Z',
              data: { index: 0 },
              evidence: { screenshot: 'failure.png' },
            }],
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        ),
      ),
    );

    const trace = await getTrace('run-1');

    expect(trace.attempts[0]).toMatchObject({
      span_id: 'operation-1',
      attempt_number: 1,
      status: 'failed',
    });
    expect(trace.usage[0]).toMatchObject({ total_tokens: 20, cost_micros: 44 });
    expect(trace.operation_evidence[0]?.artifacts).toEqual([
      { kind: 'screenshot', path: 'failure.png' },
    ]);
    expect(trace.derived_nodes[0]).toMatchObject({
      id: 'derived:operation-1:step:0',
      parent_span_id: 'operation-1',
      kind: 'step',
      status: 'passed',
    });
  });

  it('rejects a derived trace node with an unknown kind', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            spans: [],
            attempts: [],
            usage: [],
            operation_evidence: [],
            derived_nodes: [{
              id: 'derived:operation-1:other:0',
              parent_span_id: 'operation-1',
              source_evidence_id: 'evidence-1',
              kind: 'other',
              name: 'Other node',
              status: 'passed',
              started_at: '2026-10-21T12:00:00Z',
              data: {},
              evidence: {},
            }],
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        ),
      ),
    );

    await expect(getTrace('run-1')).rejects.toEqual(
      new ApiError(
        200,
        'invalid_response',
        'Invalid API response at derived_nodes[0].kind: expected one of step, unknown_tool.',
      ),
    );
  });

  it('rejects successful agent payloads missing required JSON fields', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            agents: [{
              id: 'agent-1',
              product_name: 'Agent',
              model: {
                provider: 'openai-compatible',
                name: 'fixture',
                reasoning_effort: 'medium',
              },
            }],
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        ),
      ),
    );

    await expect(listAgents()).rejects.toEqual(
      new ApiError(
        200,
        'invalid_response',
        'Invalid API response at agents[0].safety: expected a JSON value.',
      ),
    );
  });
});
