import { Send, Square } from 'lucide-react';
import { useEffect, useMemo, useRef, useState } from 'react';
import type { Agent, ModelRef, RunStatus } from '../../api';

interface Props {
  agents: Agent[];
  busy: boolean;
  runStatus: RunStatus | null;
  onSend: (
    content: string,
    agentId: string,
    model: ModelRef,
    idempotencyKey: string,
  ) => Promise<boolean>;
  onCancel: () => Promise<void>;
}

function newIdempotencyKey(): string {
  return crypto.randomUUID();
}

function modelKey(model: ModelRef): string {
  return `${model.provider}\u0000${model.name}`;
}

function modelLabel(model: ModelRef): string {
  if (model.provider === 'scripted' && model.name === 'browser-eval-fixture') {
    return 'Browser evaluation';
  }
  return `${model.provider} / ${model.name}`;
}

export function Composer({
  agents,
  busy,
  runStatus,
  onSend,
  onCancel,
}: Props): JSX.Element {
  const [draft, setDraft] = useState('');
  const [agentId, setAgentId] = useState('');
  const [selectedModelKey, setSelectedModelKey] = useState('');
  const idempotencyKey = useRef(newIdempotencyKey());
  const sending = useRef(false);

  const models = useMemo(() => {
    const unique = new Map<string, ModelRef>();
    for (const agent of agents) {
      unique.set(modelKey(agent.model), {
        provider: agent.model.provider,
        name: agent.model.name,
      });
    }
    return [...unique.values()];
  }, [agents]);

  useEffect(() => {
    if (agents.length === 0) return;
    const selected = agents.find((agent) => agent.id === agentId) ?? agents[0];
    if (selected === undefined) return;
    setAgentId(selected.id);
    setSelectedModelKey(modelKey(selected.model));
  }, [agentId, agents]);

  const activeModel =
    models.find((model) => modelKey(model) === selectedModelKey) ?? null;
  const cancellable =
    runStatus !== null &&
    !['succeeded', 'failed', 'cancelled', 'cancelling'].includes(runStatus);

  async function submit(): Promise<void> {
    if (
      sending.current ||
      draft.trim() === '' ||
      agentId === '' ||
      activeModel === null
    ) {
      return;
    }
    sending.current = true;
    const sent = await onSend(
      draft.trim(),
      agentId,
      activeModel,
      idempotencyKey.current,
    );
    sending.current = false;
    if (sent) {
      setDraft('');
      idempotencyKey.current = newIdempotencyKey();
    }
  }

  return (
    <form
      className="composer"
      aria-label="Send evaluation message"
      onSubmit={(event) => {
        event.preventDefault();
        void submit();
      }}
    >
      <label className="composer-input">
        <span className="sr-only">Evaluation message</span>
        <textarea
          rows={3}
          value={draft}
          placeholder="Describe the evaluation goal..."
          onChange={(event) => {
            setDraft(event.target.value);
            idempotencyKey.current = newIdempotencyKey();
          }}
          onKeyDown={(event) => {
            if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) {
              event.preventDefault();
              void submit();
            }
          }}
        />
      </label>
      <div className="composer-controls">
        <label>
          <span>Agent</span>
          <select
            aria-label="Agent"
            value={agentId}
            disabled={agents.length === 0}
            onChange={(event) => {
              const next = agents.find((agent) => agent.id === event.target.value);
              setAgentId(event.target.value);
              if (next !== undefined) setSelectedModelKey(modelKey(next.model));
            }}
          >
            {agents.length === 0 ? <option value="">Unavailable</option> : null}
            {agents.map((agent) => (
              <option key={agent.id} value={agent.id}>
                {agent.product_name}
              </option>
            ))}
          </select>
        </label>
        <label>
          <span>Model</span>
          <select
            aria-label="Model"
            value={selectedModelKey}
            disabled={models.length === 0}
            onChange={(event) => setSelectedModelKey(event.target.value)}
          >
            {models.map((model) => (
              <option key={modelKey(model)} value={modelKey(model)}>
                {modelLabel(model)}
              </option>
            ))}
          </select>
        </label>
        <span className="composer-hint">Ctrl/⌘ + Enter</span>
        {cancellable ? (
          <button
            className="button"
            type="button"
            aria-label="Cancel current run"
            disabled={busy}
            onClick={() => void onCancel()}
          >
            <Square size={14} />
            Cancel
          </button>
        ) : null}
        <button
          className="button button-primary send-button"
          type="submit"
          aria-label="Send message"
          disabled={busy || draft.trim() === '' || activeModel === null || agentId === ''}
        >
          <Send size={15} />
          {busy ? 'Sending' : 'Send'}
        </button>
      </div>
    </form>
  );
}
