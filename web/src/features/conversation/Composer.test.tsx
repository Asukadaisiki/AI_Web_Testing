// @vitest-environment jsdom
import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { HarnessSnapshot, Runtime } from '../../api';
import { Composer } from './Composer';

const runtimes: Runtime[] = [
  { id: 'pi', name: 'Pi', available: true, model: { provider: 'openai-compatible', name: 'pi-model', reasoning_effort: 'medium' } },
  { id: 'claude-code', name: 'Claude Code', available: false, unavailable_reason: 'Provider not configured', model: { provider: 'anthropic-compatible', name: 'claude-model' } },
  { id: 'codex', name: 'Codex', available: true, model: { provider: 'openai-responses', name: 'codex-model', reasoning_effort: 'high' } },
];
const harnesses: HarnessSnapshot[] = [
  { id: 'browser', name: 'Browser', revision: 1, system_prompt: '', skills: [], mcp: {} },
  { id: 'generic', name: 'Generic', revision: 2, system_prompt: '', skills: [], mcp: {} },
];

afterEach(cleanup);

function setup(overrides: Partial<Parameters<typeof Composer>[0]> = {}) {
  const onSend = vi.fn().mockResolvedValue(false);
  const props = {
    agents: [], runtimes, harnesses, harnessId: 'browser',
    onSelectHarness: vi.fn(), busy: false, runStatus: null,
    onSend, onCancel: vi.fn(), ...overrides,
  };
  return { ...render(<Composer {...props} />), props, onSend };
}

describe('Runtime composer', () => {
  it('offers only the selected runtime model and submits its full identity with Harness', async () => {
    const { onSend } = setup();
    fireEvent.change(screen.getByLabelText('Runtime'), { target: { value: 'codex' } });
    const model = screen.getByLabelText('Model');
    expect(model.querySelectorAll('option')).toHaveLength(1);
    expect(model).toHaveTextContent('openai-responses / codex-model');
    expect(model).not.toHaveTextContent('pi-model');
    fireEvent.change(screen.getByLabelText('Evaluation message'), { target: { value: ' Check checkout ' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }));
    await waitFor(() => expect(onSend).toHaveBeenCalledWith(
      'Check checkout', 'codex',
      { provider: 'openai-responses', name: 'codex-model', reasoning_effort: 'high' },
      expect.any(String), { runtime_id: 'codex', harness_id: 'browser' },
    ));
  });

  it('disables unavailable runtime options and sending when selection loses readiness', () => {
    const { rerender, props } = setup();
    expect(screen.getByRole('option', { name: /Claude Code/ })).toBeDisabled();
    expect(screen.getByRole('option', { name: /Claude Code/ })).toHaveTextContent('Provider not configured');
    fireEvent.change(screen.getByLabelText('Evaluation message'), { target: { value: 'Check' } });
    rerender(<Composer {...props} runtimes={runtimes.map((item) => ({ ...item, available: false }))} />);
    expect(screen.getByRole('button', { name: 'Send message' })).toBeDisabled();
    fireEvent.keyDown(screen.getByLabelText('Evaluation message'), { key: 'Enter', ctrlKey: true });
    expect(props.onSend).not.toHaveBeenCalled();
  });

  it('uses controlled Harness selection and prevents sending without a valid snapshot', async () => {
    const { props, rerender, onSend } = setup();
    fireEvent.change(screen.getByLabelText('Harness'), { target: { value: 'generic' } });
    expect(props.onSelectHarness).toHaveBeenCalledWith('generic');
    rerender(<Composer {...props} harnessId="generic" />);
    fireEvent.change(screen.getByLabelText('Evaluation message'), { target: { value: 'Check' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }));
    await waitFor(() => expect(onSend.mock.calls[0]?.[4]).toEqual({ runtime_id: 'pi', harness_id: 'generic' }));
    rerender(<Composer {...props} harnesses={[]} />);
    expect(screen.getByRole('button', { name: 'Send message' })).toBeDisabled();
  });

  it('retains a failed send key but changes it when composition changes', async () => {
    const { onSend } = setup();
    fireEvent.change(screen.getByLabelText('Evaluation message'), { target: { value: 'Check' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }));
    await waitFor(() => expect(onSend).toHaveBeenCalledTimes(1));
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }));
    await waitFor(() => expect(onSend).toHaveBeenCalledTimes(2));
    expect(onSend.mock.calls[1]?.[3]).toBe(onSend.mock.calls[0]?.[3]);
    fireEvent.change(screen.getByLabelText('Runtime'), { target: { value: 'codex' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }));
    await waitFor(() => expect(onSend).toHaveBeenCalledTimes(3));
    expect(onSend.mock.calls[2]?.[3]).not.toBe(onSend.mock.calls[0]?.[3]);
  });

  it('uses a new retry key after the selected Harness revision changes', async () => {
    const { onSend, props, rerender } = setup();
    fireEvent.change(screen.getByLabelText('Evaluation message'), { target: { value: 'Check' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }));
    await waitFor(() => expect(onSend).toHaveBeenCalledTimes(1));
    rerender(<Composer {...props} harnesses={harnesses.map((item) => (
      item.id === 'browser' ? { ...item, revision: 2 } : item
    ))} />);
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }));
    await waitFor(() => expect(onSend).toHaveBeenCalledTimes(2));
    expect(onSend.mock.calls[1]?.[3]).not.toBe(onSend.mock.calls[0]?.[3]);
  });

  it('keeps legacy Agent and Model callers working while preserving model effort', async () => {
    const { onSend } = setup({
      runtimes: undefined,
      agents: [{ id: 'browser-eval', product_name: 'Browser evaluation', model: { provider: 'provider', name: 'model', reasoning_effort: 'medium' }, safety: {} }],
    });
    expect(screen.getByLabelText('Agent')).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText('Evaluation message'), { target: { value: 'Check' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }));
    await waitFor(() => expect(onSend).toHaveBeenCalledWith(
      'Check', 'browser-eval', { provider: 'provider', name: 'model', reasoning_effort: 'medium' }, expect.any(String),
    ));
  });
});
