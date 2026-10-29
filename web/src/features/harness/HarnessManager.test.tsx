// @vitest-environment jsdom
import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { HarnessDefinition, HarnessSnapshot } from '../../api';
import { HarnessManager } from './HarnessManager';
import { useExecutionCatalog } from './useExecutionCatalog';

const browser: HarnessSnapshot = {
  id: 'browser', name: 'Browser', revision: 1,
  system_prompt: 'Check the browser.', skills: [],
  mcp: { browser: { command: 'node', args: ['server.js'], env: { TOKEN: 'env:BROWSER_TOKEN' } } },
};
let stored: HarnessSnapshot[];
let requests: { method: string; path: string; body?: HarnessDefinition }[];
let failure: string | null;

function Host() {
  const catalog = useExecutionCatalog();
  return <>
    <span aria-label="Selected Harness">{catalog.harnessId}</span>
    <HarnessManager catalog={catalog} />
  </>;
}

beforeEach(() => {
  stored = [structuredClone(browser)];
  requests = [];
  failure = null;
  vi.stubGlobal('fetch', vi.fn(async (path: string, init?: RequestInit) => {
    const method = init?.method ?? 'GET';
    const body = init?.body ? JSON.parse(String(init.body)) as HarnessDefinition : undefined;
    requests.push({ method, path, body });
    if (failure && method !== 'GET') {
      return new Response(JSON.stringify({ error: { message: failure } }), { status: 500 });
    }
    if (path === '/api/v1/runtimes') return new Response('{"runtimes":[]}');
    if (method === 'GET') return new Response(JSON.stringify({ harnesses: stored }));
    if (method === 'DELETE') {
      stored = stored.filter((item) => path !== `/api/v1/harnesses/${item.id}`);
      return new Response(null, { status: 204 });
    }
    const id = method === 'POST' ? 'generic' : 'browser';
    const snapshot = { ...body!, id, revision: method === 'POST' ? 1 : 2 };
    stored = [...stored.filter((item) => item.id !== id), snapshot];
    return new Response(JSON.stringify(snapshot), { status: method === 'POST' ? 201 : 200 });
  }));
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

async function open() {
  render(<Host />);
  await waitFor(() => expect(screen.getByLabelText('Harness name')).toHaveValue('Browser'));
}

describe('Harness manager', () => {
  it('creates, selects, replaces all three parts, and deletes after confirmation', async () => {
    await open();
    fireEvent.change(screen.getByLabelText('System prompt'), { target: { value: 'Updated prompt' } });
    fireEvent.change(screen.getByLabelText('Skills JSON'), {
      target: { value: '[{"name":"checks","content":"Check checkout","files":[{"path":"cases/example.json","content":"{}"}]}]' },
    });
    fireEvent.change(screen.getByLabelText('MCP JSON'), {
      target: { value: '{"browser":{"command":"node","args":["new.js"],"env":{"TOKEN":"env:NEW_TOKEN"},"host_only_tools":["release"]}}' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Save Harness' }));
    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('Saved'));
    expect(requests.find((item) => item.method === 'PUT')).toEqual({
      method: 'PUT', path: '/api/v1/harnesses/browser',
      body: {
        name: 'Browser', system_prompt: 'Updated prompt',
        skills: [{ name: 'checks', content: 'Check checkout', files: [{ path: 'cases/example.json', content: '{}' }] }],
        mcp: { browser: { command: 'node', args: ['new.js'], env: { TOKEN: 'env:NEW_TOKEN' }, host_only_tools: ['release'] } },
      },
    });
    expect(screen.getByLabelText('Manage Harness')).toHaveTextContent('r2');

    fireEvent.click(screen.getByRole('button', { name: 'New Harness' }));
    fireEvent.change(screen.getByLabelText('Harness name'), { target: { value: 'Generic' } });
    fireEvent.change(screen.getByLabelText('System prompt'), { target: { value: 'Summarize' } });
    fireEvent.click(screen.getByRole('button', { name: 'Create Harness' }));
    await waitFor(() => expect(screen.getByLabelText('Selected Harness')).toHaveTextContent('generic'));
    expect(requests.find((item) => item.method === 'POST')?.body).toEqual({
      name: 'Generic', system_prompt: 'Summarize', skills: [], mcp: {},
    });
    fireEvent.click(screen.getByRole('button', { name: 'Delete Harness' }));
    expect(requests.some((item) => item.method === 'DELETE')).toBe(false);
    fireEvent.click(screen.getByRole('button', { name: 'Confirm delete Harness' }));
    await waitFor(() => expect(screen.getByLabelText('Selected Harness')).toHaveTextContent('browser'));
    expect(screen.getByLabelText('Manage Harness')).not.toHaveTextContent('Generic');
  });

  it.each([
    ['Skills JSON', '{', /valid JSON/],
    ['Skills JSON', '{}', /array/],
    ['Skills JSON', '[{"name":"same","content":""},{"name":"same","content":""}]', /unique/],
    ['Skills JSON', '[{"name":"check","content":"","files":[{"path":"../outside","content":""}]}]', /relative/],
    ['MCP JSON', '{"browser":{"command":"node","args":[1]}}', /string/],
    ['MCP JSON', '{"browser":{"command":""}}', /command/],
    ['MCP JSON', '{"browser":{"command":"node","env":{"API_KEY":"literal-secret"}}}', /env:VARIABLE_NAME/],
  ])('blocks invalid %s without sending it', async (label, json, message) => {
    await open();
    fireEvent.change(screen.getByLabelText(label), { target: { value: json } });
    expect(screen.getByRole('alert')).toHaveTextContent(message);
    expect(screen.getByRole('button', { name: 'Save Harness' })).toBeDisabled();
    fireEvent.submit(screen.getByRole('form', { name: 'Harness configuration' }));
    expect(requests.every((item) => item.method === 'GET')).toBe(true);
  });

  it('retains edits after an API error and refreshes server revisions explicitly', async () => {
    await open();
    failure = 'Save failed';
    fireEvent.change(screen.getByLabelText('Harness name'), { target: { value: 'Edited' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save Harness' }));
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('Save failed'));
    expect(screen.getByLabelText('Harness name')).toHaveValue('Edited');
    failure = null;
    stored = [{ ...browser, name: 'Remote update', revision: 5 }];
    fireEvent.click(screen.getByRole('button', { name: 'Refresh Harnesses' }));
    await waitFor(() => expect(screen.getByLabelText('Harness name')).toHaveValue('Remote update'));
    expect(screen.getByLabelText('Manage Harness')).toHaveTextContent('r5');
  });

  it('does not render literal credentials returned in MCP environment fields', async () => {
    stored = [{ ...browser, mcp: { browser: { command: 'node', env: { API_KEY: 'literal-secret' } } } }];
    await open();
    expect(screen.getByLabelText('MCP JSON')).not.toHaveValue(expect.stringContaining('literal-secret'));
    expect(screen.getByRole('alert')).toHaveTextContent('env:VARIABLE_NAME');
    expect(screen.getByRole('button', { name: 'Save Harness' })).toBeDisabled();
  });
});
