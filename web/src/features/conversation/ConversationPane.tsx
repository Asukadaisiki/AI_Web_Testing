import { Bot, GitBranch } from 'lucide-react';
import { useMemo } from 'react';
import { Composer } from './Composer';
import { MessageBlock, RunStateBlock } from './MessageBlock';
import type { WorkspaceState } from '../workspace/useWorkspace';

interface Props {
  workspace: WorkspaceState;
}

export function ConversationPane({ workspace }: Props): JSX.Element {
  const runIds = useMemo(() => {
    const unique: string[] = [];
    for (const message of workspace.conversation?.messages ?? []) {
      if (
        message.run_id !== undefined &&
        message.run_id !== '' &&
        !unique.includes(message.run_id)
      ) {
        unique.push(message.run_id);
      }
    }
    return unique;
  }, [workspace.conversation]);

  const title =
    workspace.conversation?.conversation.title?.trim() || 'Untitled conversation';

  return (
    <main className="pane conversation-pane" aria-label="Conversation workspace">
      <header className="pane-header conversation-header">
        <div>
          <p className="eyebrow">Conversation</p>
          <h2>{workspace.selection.conversationId === null ? 'No conversation selected' : title}</h2>
        </div>
        {workspace.run !== null ? (
          <div className="run-summary" aria-label={`Current run ${workspace.run.status}`}>
            <span className={`status-indicator status-${workspace.run.status}`} />
            <span>{workspace.run.status.replace('_', ' ')}</span>
            <code>{workspace.run.id.slice(0, 12)}</code>
          </div>
        ) : null}
      </header>

      {runIds.length > 1 ? (
        <nav className="run-tabs" aria-label="Conversation runs">
          <GitBranch size={14} aria-hidden="true" />
          {runIds.map((runId, index) => (
            <button
              key={runId}
              type="button"
              className={runId === workspace.selection.runId ? 'is-active' : ''}
              aria-pressed={runId === workspace.selection.runId}
              onClick={() => workspace.selectRun(runId)}
            >
              Run {index + 1}
            </button>
          ))}
        </nav>
      ) : null}

      <section className="message-history" aria-label="Message history" aria-live="polite">
        {workspace.selection.conversationId === null ? (
          <div className="center-empty">
            <Bot size={28} />
            <h3>Select or create a conversation</h3>
            <p>Evaluation messages and durable run states stay in this workspace.</p>
          </div>
        ) : workspace.conversation === null ? (
          <div className="center-empty">
            <span className="loading-bar" />
            <p>Loading conversation...</p>
          </div>
        ) : workspace.conversation.messages.length === 0 ? (
          <div className="center-empty">
            <Bot size={28} />
            <h3>Start an evaluation</h3>
            <p>Select an authorized agent and model, then describe the goal.</p>
          </div>
        ) : (
          workspace.conversation.messages.map((message) => (
            <div key={message.id}>
              <MessageBlock message={message} />
              {message.run_id === workspace.selection.runId && workspace.run !== null ? (
                <RunStateBlock
                  run={workspace.run}
                  events={workspace.events}
                  connected={workspace.connected}
                  busy={workspace.busy}
                  onRespond={workspace.respond}
                />
              ) : null}
            </div>
          ))
        )}
        {workspace.error !== null ? (
          <div className="inline-error" role="alert">
            {workspace.error}
          </div>
        ) : null}
        {workspace.streamError !== null ? (
          <div className="stream-warning" role="status">
            {workspace.streamError}
          </div>
        ) : null}
      </section>

      {workspace.selection.conversationId !== null ? (
        <Composer
          agents={workspace.agents}
          busy={workspace.busy}
          runStatus={workspace.run?.status ?? null}
          onSend={workspace.sendMessage}
          onCancel={workspace.requestCancel}
        />
      ) : null}
    </main>
  );
}
