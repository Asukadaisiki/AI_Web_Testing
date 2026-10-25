// @vitest-environment jsdom

import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { ConversationDetail, Run } from '../../api';
import type { WorkspaceState } from '../workspace/useWorkspace';
import { ConversationPane } from './ConversationPane';

afterEach(cleanup);

const originalRun: Run = {
  id: 'run-original',
  project_id: 'project-1',
  conversation_id: 'conversation-1',
  source_message_id: 'message-task',
  status: 'succeeded',
  checkpoint_version: 2,
  created_at: '2026-10-21T12:00:01Z',
  updated_at: '2026-10-21T12:03:00Z',
  finished_at: '2026-10-21T12:03:00Z',
};

const rerun: Run = {
  ...originalRun,
  id: 'run-rerun',
  retry_of_run_id: originalRun.id,
  status: 'failed',
  checkpoint_version: 1,
  created_at: '2026-10-22T12:00:01Z',
  updated_at: '2026-10-22T12:03:00Z',
  finished_at: '2026-10-22T12:03:00Z',
};

const detail: ConversationDetail = {
  conversation: {
    id: 'conversation-1',
    project_id: 'project-1',
    title: 'Checkout investigation',
    metadata: {},
    created_at: '2026-10-21T12:00:00Z',
    updated_at: '2026-10-22T12:04:00Z',
  },
  messages: [
    {
      id: 'message-task',
      conversation_id: 'conversation-1',
      ordinal: 1,
      role: 'user',
      content: 'Check checkout',
      created_at: '2026-10-21T12:00:00Z',
    },
    {
      id: 'message-original-result',
      conversation_id: 'conversation-1',
      run_id: originalRun.id,
      ordinal: 2,
      role: 'assistant',
      content: 'The first attempt passed login.',
      created_at: '2026-10-21T12:03:00Z',
    },
    {
      id: 'message-response',
      conversation_id: 'conversation-1',
      run_id: originalRun.id,
      ordinal: 3,
      role: 'user',
      content: 'Continue after the login check.',
      created_at: '2026-10-22T12:00:00Z',
    },
    {
      id: 'message-rerun-result',
      conversation_id: 'conversation-1',
      run_id: rerun.id,
      ordinal: 4,
      role: 'assistant',
      content: 'The rerun failed during checkout.',
      created_at: '2026-10-22T12:03:00Z',
    },
  ],
  runs: [originalRun, rerun],
};

function workspaceWithRerun(): WorkspaceState {
  return {
    selection: {
      projectId: 'project-1',
      conversationId: 'conversation-1',
      runId: rerun.id,
      spanId: null,
    },
    conversation: detail,
    run: rerun,
    events: [],
    connected: false,
    busy: false,
    agents: [],
    error: null,
    streamError: null,
    selectRun: vi.fn(),
    respond: vi.fn().mockResolvedValue(true),
    rerunFailed: vi.fn().mockResolvedValue(true),
    requestCancel: vi.fn().mockResolvedValue(undefined),
    sendMessage: vi.fn().mockResolvedValue(true),
  } as unknown as WorkspaceState;
}

describe('ConversationPane', () => {
  it('groups source Runs as ordered attempts while keeping response messages in ordinal order', () => {
    const workspace = workspaceWithRerun();
    render(<ConversationPane workspace={workspace} />);

    expect(screen.getByRole('button', { name: 'Attempt 1' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Attempt 2' })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
    expect(screen.getByRole('button', { name: 'Rerun failed task' })).toBeInTheDocument();
    expect(screen.getAllByText(/\d{2}:\d{2}:\d{2}/)).not.toHaveLength(0);
    expect(screen.queryByRole('navigation', { name: 'Conversation runs' })).toBeNull();
    expect(screen.getAllByRole('region', { name: 'Run failed' })).toHaveLength(1);
    expect(screen.getByText('Continue after the login check.')).toBeInTheDocument();
    expect(screen.getAllByText('Continue after the login check.')).toHaveLength(1);

    fireEvent.click(screen.getByRole('button', { name: 'Attempt 1' }));

    expect(workspace.selectRun).toHaveBeenCalledWith(originalRun.id);
  });

  it('renders messages by ascending ordinal when API input order and timestamps disagree', () => {
    const workspace = workspaceWithRerun();
    workspace.conversation = {
      ...detail,
      messages: [
        {
          ...detail.messages[3],
          ordinal: 3,
          created_at: '2026-10-19T23:59:59Z',
        },
        {
          ...detail.messages[0],
          ordinal: 1,
          created_at: '2026-10-21T12:00:00Z',
        },
        {
          ...detail.messages[1],
          ordinal: 2,
          created_at: '2026-10-20T00:00:00Z',
        },
        {
          ...detail.messages[2],
          ordinal: 4,
          created_at: '2026-10-18T00:00:00Z',
        },
      ],
    };

    render(<ConversationPane workspace={workspace} />);

    const contents = Array.from(
      screen.getByLabelText('Message history').querySelectorAll('.message-block p'),
      (element) => element.textContent,
    );

    expect(contents).toEqual([
      'Check checkout',
      'The first attempt passed login.',
      'The rerun failed during checkout.',
      'Continue after the login check.',
    ]);
    expect(screen.getByLabelText('Message history').querySelectorAll('.message-date-separator'))
      .toHaveLength(3);
  });

  it('does not render state for a loaded Run that differs from the selected attempt', () => {
    const workspace = workspaceWithRerun();
    workspace.run = originalRun;

    render(<ConversationPane workspace={workspace} />);

    expect(screen.getByRole('button', { name: 'Attempt 2' })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
    expect(screen.queryByRole('region', { name: 'Run completed' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Rerun failed task' })).toBeNull();
  });
});
