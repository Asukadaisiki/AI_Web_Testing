import { Bot } from 'lucide-react';
import { useMemo } from 'react';
import type { Run } from '../../api';
import { Composer } from './Composer';
import {
  formatMessageDate,
  MessageBlock,
  messageDateKey,
  RunStateBlock,
} from './MessageBlock';
import type { WorkspaceState } from '../workspace/useWorkspace';

interface Props {
  workspace: WorkspaceState;
}

export function ConversationPane({ workspace }: Props): JSX.Element {
  const runsBySource = useMemo(() => {
    const grouped = new Map<string, Run[]>();
    for (const run of workspace.conversation?.runs ?? []) {
      const runs = grouped.get(run.source_message_id) ?? [];
      runs.push(run);
      grouped.set(run.source_message_id, runs);
    }
    return grouped;
  }, [workspace.conversation]);

  const selectedTaskSourceId = useMemo(
    () =>
      workspace.conversation?.runs.find(
        (run) => run.id === workspace.selection.runId,
      )?.source_message_id ?? null,
    [workspace.conversation, workspace.selection.runId],
  );

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
          workspace.conversation.messages.map((message, index) => {
            const runs = runsBySource.get(message.id);
            const priorMessage = workspace.conversation?.messages[index - 1];
            const startsDate =
              priorMessage === undefined ||
              messageDateKey(priorMessage.created_at) !== messageDateKey(message.created_at);
            const selectedTask =
              message.id === selectedTaskSourceId && workspace.run !== null;

            return (
              <div key={message.id} className="timeline-entry">
                {startsDate ? (
                  <div className="message-date-separator" role="separator">
                    <span>{formatMessageDate(message.created_at)}</span>
                  </div>
                ) : null}
                <MessageBlock message={message} />
                {runs !== undefined ? (
                  <nav className="task-attempts" aria-label="Task attempts">
                    {runs.map((run, attemptIndex) => (
                      <button
                        key={run.id}
                        type="button"
                        className={
                          run.id === workspace.selection.runId
                            ? 'attempt-selector is-active'
                            : 'attempt-selector'
                        }
                        aria-pressed={run.id === workspace.selection.runId}
                        title={`Attempt ${String(attemptIndex + 1)}: ${run.status.replace('_', ' ')}`}
                        onClick={() => workspace.selectRun(run.id)}
                      >
                        Attempt {attemptIndex + 1}
                      </button>
                    ))}
                  </nav>
                ) : null}
                {selectedTask && workspace.run !== null ? (
                  <RunStateBlock
                    run={workspace.run}
                    events={workspace.events}
                    connected={workspace.connected}
                    busy={workspace.busy}
                    onRespond={workspace.respond}
                    onRerun={workspace.rerunFailed}
                  />
                ) : null}
              </div>
            );
          })
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
