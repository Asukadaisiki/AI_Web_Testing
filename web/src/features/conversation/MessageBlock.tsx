import {
  AlertTriangle,
  CheckCircle2,
  CircleDot,
  Clock3,
  LoaderCircle,
  PauseCircle,
  XCircle,
} from 'lucide-react';
import { useMemo, useRef, useState } from 'react';
import type { Message, Run, RunEvent } from '../../api';

interface MessageProps {
  message: Message;
}

interface RunStateProps {
  run: Run;
  events: RunEvent[];
  connected: boolean;
  busy: boolean;
  onRespond: (
    kind: 'question' | 'approval',
    content: string,
    idempotencyKey: string,
  ) => Promise<boolean>;
}

function record(value: unknown): Record<string, unknown> | null {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;
}

function eventPayload(event: RunEvent): Record<string, unknown> | null {
  const data = record(event.data);
  if (data === null) return null;
  return record(data['payload']) ?? data;
}

function stringField(value: Record<string, unknown> | null, ...keys: string[]): string {
  for (const key of keys) {
    const candidate = value?.[key];
    if (typeof candidate === 'string') return candidate;
  }
  return '';
}

export function MessageBlock({ message }: MessageProps): JSX.Element {
  const user = message.role === 'user';
  return (
    <article className={`message-block message-${user ? 'user' : 'assistant'}`}>
      <header>
        <strong>{user ? 'You' : 'Agent'}</strong>
        <time dateTime={message.created_at}>
          {new Date(message.created_at).toLocaleTimeString([], {
            hour: '2-digit',
            minute: '2-digit',
          })}
        </time>
      </header>
      <p>{message.content}</p>
    </article>
  );
}

export function RunStateBlock({
  run,
  events,
  connected,
  busy,
  onRespond,
}: RunStateProps): JSX.Element {
  const [answer, setAnswer] = useState('');
  const responseKey = useRef(crypto.randomUUID());
  const completion = [...events]
    .reverse()
    .find((event) => event.type === 'runtime.completed');
  const completionPayload = completion === undefined ? null : eventPayload(completion);
  const assistant = stringField(completionPayload, 'Assistant', 'assistant');
  const completionError = stringField(completionPayload, 'Error', 'error');

  const streamed = useMemo(
    () =>
      events
        .filter((event) => event.type === 'token.delta')
        .map((event) => stringField(eventPayload(event), 'delta', 'text'))
        .join(''),
    [events],
  );

  async function respond(kind: 'question' | 'approval', content: string): Promise<void> {
    const accepted = await onRespond(kind, content, responseKey.current);
    if (accepted) {
      setAnswer('');
      responseKey.current = crypto.randomUUID();
    }
  }

  if (run.status === 'queued') {
    return (
      <section className="run-state state-queued" aria-label="Run queued">
        <Clock3 size={17} />
        <div>
          <strong>Queued</strong>
          <p>Accepted durably and waiting for runtime delivery.</p>
        </div>
      </section>
    );
  }

  if (run.status === 'running') {
    return (
      <section className="run-state state-running" aria-label="Run streaming">
        <LoaderCircle className="spin" size={17} />
        <div>
          <div className="run-state-heading">
            <strong>Streaming</strong>
            <span className={`connection-state${connected ? ' is-live' : ''}`}>
              {connected ? 'Live' : 'Reconnecting'}
            </span>
          </div>
          <p className={streamed === '' ? 'muted' : 'stream-output'}>
            {streamed === '' ? 'The agent is working. Durable trace updates appear at right.' : streamed}
          </p>
        </div>
      </section>
    );
  }

  if (run.status === 'waiting_input') {
    return (
      <section className="run-state state-waiting" aria-label="Run waiting for input">
        <PauseCircle size={17} />
        <div>
          <strong>Input required</strong>
          <p>The agent needs a response before it can continue.</p>
          <label>
            <span className="sr-only">Response to agent question</span>
            <textarea
              rows={2}
              value={answer}
              placeholder="Enter response"
              onChange={(event) => {
                setAnswer(event.target.value);
                responseKey.current = crypto.randomUUID();
              }}
            />
          </label>
          <button
            type="button"
            className="button button-primary"
            disabled={busy || answer.trim() === ''}
            onClick={() => void respond('question', answer.trim())}
          >
            Continue
          </button>
        </div>
      </section>
    );
  }

  if (run.status === 'waiting_approval') {
    return (
      <section className="run-state state-waiting" aria-label="Run waiting for approval">
        <CircleDot size={17} />
        <div>
          <strong>Approval required</strong>
          <p>Review the current Trace before allowing the agent to continue.</p>
          <div className="button-row">
            <button
              type="button"
              className="button button-primary"
              disabled={busy}
              onClick={() => void respond('approval', 'approved')}
            >
              Approve
            </button>
            <button
              type="button"
              className="button button-danger"
              disabled={busy}
              onClick={() => void respond('approval', 'rejected')}
            >
              Reject
            </button>
          </div>
        </div>
      </section>
    );
  }

  if (run.status === 'cancelling') {
    return (
      <section className="run-state state-cancelled" aria-label="Run cancelling">
        <LoaderCircle className="spin" size={17} />
        <div>
          <strong>Cancelling</strong>
          <p>The durable cancellation request is waiting for a checkpoint boundary.</p>
        </div>
      </section>
    );
  }

  if (run.status === 'failed') {
    return (
      <section className="run-state state-failed" aria-label="Run failed">
        <AlertTriangle size={17} />
        <div>
          <strong>Failed</strong>
          <p>{completionError || 'The run failed. Select a Trace node to inspect the recorded cause.'}</p>
        </div>
      </section>
    );
  }

  if (run.status === 'cancelled') {
    return (
      <section className="run-state state-cancelled" aria-label="Run cancelled">
        <XCircle size={17} />
        <div>
          <strong>Cancelled</strong>
          <p>The run stopped at a durable checkpoint.</p>
        </div>
      </section>
    );
  }

  return (
    <section className="run-state state-completed" aria-label="Run completed">
      <CheckCircle2 size={17} />
      <div>
        <strong>Completed</strong>
        <p>{assistant || 'The run completed successfully. The canonical Trace is available at right.'}</p>
      </div>
    </section>
  );
}
