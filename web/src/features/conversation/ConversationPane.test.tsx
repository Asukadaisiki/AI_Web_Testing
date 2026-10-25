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
  started_at: '2026-10-21T12:00:05Z',
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
  started_at: '2026-10-22T12:00:06Z',
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
  it('groups selected attempt responses beneath their source task and exposes lifecycle metadata', () => {
    const workspace = workspaceWithRerun();
    const rendered = render(<ConversationPane workspace={workspace} />);

    expect(screen.getByRole('button', { name: 'Attempt 1' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Attempt 2' })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
    expect(screen.getByText('Succeeded')).toBeInTheDocument();
    expect(screen.getAllByText('Failed')).toHaveLength(2);
    expect(screen.getAllByText('Queued')).toHaveLength(2);
    expect(screen.getAllByText('Started')).toHaveLength(2);
    expect(screen.getAllByText('Finished')).toHaveLength(2);
    expect(screen.getByText('2m 55s')).toBeInTheDocument();
    const attempts = screen.getByRole('navigation', { name: 'Task attempts' });
    expect(attempts.querySelector(`time[datetime="${rerun.created_at}"]`)).not.toBeNull();
    expect(attempts.querySelector(`time[datetime="${rerun.started_at ?? ''}"]`)).not.toBeNull();
    expect(attempts.querySelector(`time[datetime="${rerun.finished_at ?? ''}"]`)).not.toBeNull();
    expect(screen.getByRole('button', { name: 'Rerun failed task' })).toBeInTheDocument();
    expect(screen.getAllByText(/\d{2}:\d{2}:\d{2}/)).not.toHaveLength(0);
    expect(screen.queryByRole('navigation', { name: 'Conversation runs' })).toBeNull();
    expect(screen.getAllByRole('region', { name: 'Run failed' })).toHaveLength(1);
    expect(screen.getByText('The rerun failed during checkout.')).toBeInTheDocument();
    expect(screen.queryByText('The first attempt passed login.')).toBeNull();
    expect(screen.queryByText('Continue after the login check.')).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Attempt 1' }));

    expect(workspace.selectRun).toHaveBeenCalledWith(originalRun.id);

    workspace.selection.runId = originalRun.id;
    workspace.run = originalRun;
    rendered.rerender(<ConversationPane workspace={workspace} />);

    expect(screen.getByRole('button', { name: 'Attempt 1' })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
    expect(screen.getByText('The first attempt passed login.')).toBeInTheDocument();
    expect(screen.getByText('Continue after the login check.')).toBeInTheDocument();
    expect(screen.getAllByText('The first attempt passed login.')).toHaveLength(1);
    expect(screen.getAllByText('Continue after the login check.')).toHaveLength(1);
    expect(screen.queryByText('The rerun failed during checkout.')).toBeNull();
    expect(
      Array.from(
        screen.getByLabelText('Attempt 1 responses').querySelectorAll('.message-block p'),
        (element) => element.textContent,
      ),
    ).toEqual([
      'The first attempt passed login.',
      'Continue after the login check.',
    ]);
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

    expect(contents).toEqual(['Check checkout', 'The rerun failed during checkout.']);
    expect(screen.getByLabelText('Message history').querySelectorAll('.message-date-separator'))
      .toHaveLength(2);
  });

  it('uses the latest API-order attempt for a task that is not globally selected', () => {
    const workspace = workspaceWithRerun();
    const secondOriginal: Run = {
      ...originalRun,
      id: 'run-second-original',
      source_message_id: 'message-second-task',
      created_at: '2026-10-23T12:00:01Z',
    };
    const secondRerun: Run = {
      ...secondOriginal,
      id: 'run-second-rerun',
      retry_of_run_id: secondOriginal.id,
      created_at: '2026-10-23T12:01:01Z',
    };
    workspace.conversation = {
      ...detail,
      messages: [
        ...detail.messages,
        {
          id: 'message-second-task',
          conversation_id: detail.conversation.id,
          ordinal: 5,
          role: 'user',
          content: 'Check the receipt',
          created_at: '2026-10-23T12:00:00Z',
        },
        {
          id: 'message-second-original-output',
          conversation_id: detail.conversation.id,
          run_id: secondOriginal.id,
          ordinal: 6,
          role: 'assistant',
          content: 'Earlier receipt output.',
          created_at: '2026-10-23T12:00:30Z',
        },
        {
          id: 'message-second-rerun-output',
          conversation_id: detail.conversation.id,
          run_id: secondRerun.id,
          ordinal: 7,
          role: 'assistant',
          content: 'Latest receipt output.',
          created_at: '2026-10-23T12:01:30Z',
        },
      ],
      runs: [...detail.runs, secondOriginal, secondRerun],
    };

    render(<ConversationPane workspace={workspace} />);

    const attemptGroups = screen.getAllByRole('navigation', { name: 'Task attempts' });
    expect(attemptGroups).toHaveLength(2);
    expect(attemptGroups[1]?.querySelectorAll('button')[1]).toHaveAttribute(
      'aria-pressed',
      'true',
    );
    expect(screen.getByText('Latest receipt output.')).toBeInTheDocument();
    expect(screen.queryByText('Earlier receipt output.')).toBeNull();
  });

  it('disables and invokes the failed rerun action with workspace busy state', () => {
    const workspace = workspaceWithRerun();
    workspace.busy = true;
    const rendered = render(<ConversationPane workspace={workspace} />);

    const rerunButton = screen.getByRole('button', { name: 'Rerun failed task' });
    expect(rerunButton).toBeDisabled();

    workspace.busy = false;
    rendered.rerender(<ConversationPane workspace={workspace} />);
    fireEvent.click(screen.getByRole('button', { name: 'Rerun failed task' }));

    expect(workspace.rerunFailed).toHaveBeenCalledTimes(1);
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
