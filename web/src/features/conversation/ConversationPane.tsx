import { Bot, Settings2 } from 'lucide-react';
import { useId, useMemo, useState } from 'react';
import type { Message, Run } from '../../api';
import { Composer } from './Composer';
import { RunExecutionSummary } from './RunExecutionSummary';
import { HarnessManager } from '../harness/HarnessManager';
import type { ExecutionCatalogState } from '../harness/useExecutionCatalog';
import {
  formatMessageDate,
  formatMessageTime,
  MessageBlock,
  messageDateKey,
  RunStateBlock,
} from './MessageBlock';
import type { WorkspaceState } from '../workspace/useWorkspace';

interface Props {
  workspace: WorkspaceState;
  catalog?: ExecutionCatalogState;
}

interface TaskTimelineEntry {
  kind: 'task';
  source: Message;
  runs: Run[];
  selectedRun: Run;
  responses: Message[];
}

interface MessageTimelineEntry {
  kind: 'message';
  message: Message;
}

type TimelineEntry = TaskTimelineEntry | MessageTimelineEntry;

function displayStatus(status: Run['status']): string {
  return status
    .split('_')
    .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
    .join(' ');
}

function formatAttemptDuration(run: Run): string {
  if (run.started_at === undefined || run.finished_at === undefined) {
    return 'Not available';
  }
  const milliseconds =
    new Date(run.finished_at).getTime() - new Date(run.started_at).getTime();
  if (!Number.isFinite(milliseconds) || milliseconds < 0) return 'Not available';
  const seconds = Math.floor(milliseconds / 1000);
  const minutes = Math.floor(seconds / 60);
  return minutes === 0 ? `${String(seconds)}s` : `${String(minutes)}m ${String(seconds % 60)}s`;
}

export function ConversationPane({ workspace, catalog }: Props): JSX.Element {
  const [managingHarnesses, setManagingHarnesses] = useState(false);
  const managerId = useId();
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
  const messages = useMemo(
    () =>
      [...(workspace.conversation?.messages ?? [])].sort(
        (left, right) => left.ordinal - right.ordinal,
      ),
    [workspace.conversation],
  );
  const timeline = useMemo(() => {
    const taskSourceIds = new Set(runsBySource.keys());
    const knownRunIDs = new Set(
      (workspace.conversation?.runs ?? []).map((run) => run.id),
    );
    const responsesByRun = new Map<string, Message[]>();
    for (const message of messages) {
      if (
        taskSourceIds.has(message.id) ||
        message.run_id === undefined ||
        !knownRunIDs.has(message.run_id)
      ) {
        continue;
      }
      const responses = responsesByRun.get(message.run_id) ?? [];
      responses.push(message);
      responsesByRun.set(message.run_id, responses);
    }

    const entries: TimelineEntry[] = [];
    for (const message of messages) {
      const runs = runsBySource.get(message.id);
      if (runs !== undefined) {
        const selectedRun =
          message.id === selectedTaskSourceId
            ? runs.find((run) => run.id === workspace.selection.runId) ?? runs[runs.length - 1]
            : runs[runs.length - 1];
        if (selectedRun !== undefined) {
          entries.push({
            kind: 'task',
            source: message,
            runs,
            selectedRun,
            responses: responsesByRun.get(selectedRun.id) ?? [],
          });
        }
        continue;
      }
      if (message.run_id !== undefined && knownRunIDs.has(message.run_id)) {
        continue;
      }
      entries.push({ kind: 'message', message });
    }

    const dateStarts = new Map<string, boolean>();
    let previousDate: string | undefined;
    for (const entry of entries) {
      const displayedMessages =
        entry.kind === 'task' ? [entry.source, ...entry.responses] : [entry.message];
      for (const message of displayedMessages) {
        const date = messageDateKey(message.created_at);
        dateStarts.set(message.id, date !== previousDate);
        previousDate = date;
      }
    }
    return { entries, dateStarts };
  }, [
    messages,
    runsBySource,
    selectedTaskSourceId,
    workspace.conversation,
    workspace.selection.runId,
  ]);

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
        {catalog !== undefined ? (
          <button
            type="button"
            className="button harness-manager-toggle"
            aria-label="Manage Harnesses"
            aria-expanded={managingHarnesses}
            aria-controls={managerId}
            onClick={() => setManagingHarnesses((open) => !open)}
          >
            <Settings2 size={14} />
            Harnesses
          </button>
        ) : null}
      </header>
      {workspace.run !== null ? <RunExecutionSummary run={workspace.run} spans={workspace.spans} /> : null}
      {catalog !== undefined && managingHarnesses ? (
        <div id={managerId} className="harness-manager-region">
          <HarnessManager catalog={catalog} />
        </div>
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
        ) : messages.length === 0 ? (
          <div className="center-empty">
            <Bot size={28} />
            <h3>Start an evaluation</h3>
            <p>{catalog === undefined
              ? 'Select an authorized agent and model, then describe the goal.'
              : 'Select a Runtime and Harness, then describe the goal.'}</p>
          </div>
        ) : (
          timeline.entries.map((entry) => {
            if (entry.kind === 'message') {
              return (
                <div key={entry.message.id} className="timeline-entry">
                  {timeline.dateStarts.get(entry.message.id) ? (
                    <div className="message-date-separator" role="separator">
                      <span>{formatMessageDate(entry.message.created_at)}</span>
                    </div>
                  ) : null}
                  <MessageBlock message={entry.message} />
                </div>
              );
            }

            const selectedTask =
              entry.source.id === selectedTaskSourceId &&
              workspace.run?.id === entry.selectedRun.id;
            const selectedAttemptIndex = entry.runs.indexOf(entry.selectedRun) + 1;

            return (
              <div key={entry.source.id} className="timeline-entry task-timeline-entry">
                {timeline.dateStarts.get(entry.source.id) ? (
                  <div className="message-date-separator" role="separator">
                    <span>{formatMessageDate(entry.source.created_at)}</span>
                  </div>
                ) : null}
                <MessageBlock message={entry.source} />
                <nav className="task-attempts" aria-label="Task attempts">
                  {entry.runs.map((run, attemptIndex) => {
                    const metadataID = `attempt-${run.id}-metadata`;
                    return (
                      <div key={run.id} className="task-attempt">
                        <button
                          type="button"
                          className={
                            run.id === entry.selectedRun.id
                              ? 'attempt-selector is-active'
                              : 'attempt-selector'
                          }
                          aria-pressed={run.id === entry.selectedRun.id}
                          aria-describedby={metadataID}
                          onClick={() => workspace.selectRun(run.id)}
                        >
                          Attempt {attemptIndex + 1}
                        </button>
                        <dl id={metadataID} className="attempt-metadata">
                          <div>
                            <dt>Status</dt>
                            <dd>{displayStatus(run.status)}</dd>
                          </div>
                          <div>
                            <dt>Queued</dt>
                            <dd>
                              <time dateTime={run.created_at}>
                                {formatMessageTime(run.created_at)}
                              </time>
                            </dd>
                          </div>
                          <div>
                            <dt>Started</dt>
                            <dd>
                              {run.started_at === undefined ? 'Not started' : (
                                <time dateTime={run.started_at}>
                                  {formatMessageTime(run.started_at)}
                                </time>
                              )}
                            </dd>
                          </div>
                          <div>
                            <dt>Finished</dt>
                            <dd>
                              {run.finished_at === undefined ? 'In progress' : (
                                <time dateTime={run.finished_at}>
                                  {formatMessageTime(run.finished_at)}
                                </time>
                              )}
                            </dd>
                          </div>
                          <div>
                            <dt>Duration</dt>
                            <dd>{formatAttemptDuration(run)}</dd>
                          </div>
                        </dl>
                      </div>
                    );
                  })}
                </nav>
                {entry.responses.length > 0 ? (
                  <section
                    className="attempt-responses"
                    aria-label={`Attempt ${String(selectedAttemptIndex)} responses`}
                  >
                    <p>Attempt {selectedAttemptIndex} responses</p>
                    {entry.responses.map((message) => (
                      <div key={message.id}>
                        {timeline.dateStarts.get(message.id) ? (
                          <div className="message-date-separator" role="separator">
                            <span>{formatMessageDate(message.created_at)}</span>
                          </div>
                        ) : null}
                        <MessageBlock message={message} />
                      </div>
                    ))}
                  </section>
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
        {catalog?.error != null && !managingHarnesses ? (
          <div className="inline-error" role="alert">{catalog.error}</div>
        ) : null}
      </section>

      {workspace.selection.conversationId !== null ? (
        <Composer
          agents={workspace.agents}
          runtimes={catalog?.runtimes}
          harnesses={catalog?.harnesses}
          harnessId={catalog?.harnessId}
          onSelectHarness={catalog?.selectHarness}
          busy={workspace.busy || catalog?.loading === true}
          runStatus={workspace.run?.status ?? null}
          onSend={workspace.sendMessage}
          onCancel={workspace.requestCancel}
        />
      ) : null}
    </main>
  );
}
