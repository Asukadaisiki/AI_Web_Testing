// @vitest-environment jsdom

import '@testing-library/jest-dom/vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import type { ConversationDetail, Run } from '../../api';
import type { WorkspaceState } from '../workspace/useWorkspace';
import { ConversationPane } from './ConversationPane';

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
});
