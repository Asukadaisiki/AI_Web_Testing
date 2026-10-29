// @vitest-environment jsdom
import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { Workspace } from './Workspace';

let paths: string[];
let sent: unknown;
let catalogFails: boolean;
const conversation = {
  id: 'conversation', project_id: 'project', title: 'Checkout', metadata: {},
  created_at: 'now', updated_at: 'now',
};
const harness = { id: 'browser', name: 'Browser', revision: 1, system_prompt: '', skills: [], mcp: {} };

beforeEach(() => {
  paths = [];
  sent = null;
  catalogFails = false;
  window.history.replaceState(null, '', '/');
  vi.stubGlobal('EventSource', class {
    addEventListener() {}
    removeEventListener() {}
    close() {}
  });
  vi.stubGlobal('fetch', vi.fn(async (path: string, init?: RequestInit) => {
    paths.push(path);
    let response: unknown;
    if (catalogFails && path === '/api/v1/runtimes') {
      return new Response('{"error":{"message":"Runtime catalog unavailable"}}', { status: 503 });
    }
    switch (path) {
      case '/api/v1/projects':
        response = { projects: [{ id: 'project', name: 'Project', metadata: {}, created_at: 'now', updated_at: 'now' }] }; break;
      case '/api/v1/projects/project/conversations':
        response = { conversations: [conversation] }; break;
      case '/api/v1/conversations/conversation':
        response = { conversation, messages: [], runs: [] }; break;
      case '/api/v1/health':
        response = { status: 'ok', dependencies: {} }; break;
      case '/api/v1/runtimes':
        response = { runtimes: [
          { id: 'pi', name: 'Pi', available: true, model: { provider: 'provider', name: 'pi-model' } },
          { id: 'codex', name: 'Codex', available: true, model: { provider: 'openai-responses', name: 'codex-model', reasoning_effort: 'high' } },
        ] }; break;
      case '/api/v1/harnesses':
        response = { harnesses: [harness] }; break;
      case '/api/v1/conversations/conversation/messages':
        sent = JSON.parse(String(init?.body)) as unknown;
        response = { message_id: 'message', run_id: 'run', status: 'queued' }; break;
      case '/api/v1/runs/run':
        response = {
          id: 'run', project_id: 'project', conversation_id: 'conversation', source_message_id: 'message',
          status: 'queued', checkpoint_version: 0, created_at: 'now', updated_at: 'now',
          runtime_id: 'codex', harness_id: 'browser', harness_revision: 1,
        }; break;
      case '/api/v1/runs/run/trace':
        response = { spans: [], attempts: [], usage: [], operation_evidence: [], derived_nodes: [] }; break;
      default:
        return new Response('{"error":{"message":"Unexpected route"}}', { status: 404 });
    }
    return new Response(JSON.stringify(response), { status: 200 });
  }));
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('workspace composition', () => {
  it('keeps the three panes, exposes a compact manager, and sends using the Runtime catalog', async () => {
    const { container } = render(<Workspace />);
    await screen.findByLabelText('Runtime');
    expect(container.querySelectorAll('.workspace > .pane-slot')).toHaveLength(3);
    expect(screen.queryByLabelText('Harness manager')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Manage Harnesses' }));
    expect(screen.getByLabelText('Harness manager')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Manage Harnesses' })).toHaveAttribute('aria-expanded', 'true');
    fireEvent.click(screen.getByRole('button', { name: 'Manage Harnesses' }));
    fireEvent.change(screen.getByLabelText('Runtime'), { target: { value: 'codex' } });
    fireEvent.change(screen.getByLabelText('Evaluation message'), { target: { value: 'Check checkout' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }));
    await waitFor(() => expect(sent).toEqual({
      content: 'Check checkout', agent_id: 'codex',
      runtime_id: 'codex', harness_id: 'browser',
      model: { provider: 'openai-responses', name: 'codex-model', reasoning_effort: 'high' },
      idempotency_key: expect.any(String),
    }));
    expect(paths).not.toContain('/api/v1/agents');
  });

  it('leaves conversation history usable but sending disabled after a catalog error', async () => {
    catalogFails = true;
    render(<Workspace />);
    await screen.findByLabelText('Runtime');
    expect(await screen.findByRole('alert')).toHaveTextContent('Runtime catalog unavailable');
    fireEvent.change(screen.getByLabelText('Evaluation message'), { target: { value: 'Check checkout' } });
    expect(screen.getByRole('button', { name: 'Send message' })).toBeDisabled();
    expect(screen.getByRole('heading', { name: 'Checkout' })).toBeInTheDocument();
  });
});
