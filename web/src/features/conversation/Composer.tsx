import { Send, Square } from 'lucide-react';
import { useEffect, useMemo, useRef, useState } from 'react';
import type { Agent, ExecutionSelection, HarnessSnapshot, ModelRef, Runtime, RunStatus } from '../../api';

interface Props {
  agents: Agent[];
  runtimes?: Runtime[];
  harnesses?: HarnessSnapshot[];
  harnessId?: string;
  onSelectHarness?: (id: string) => void;
  busy: boolean;
  runStatus: RunStatus | null;
  onSend: (
    content: string,
    agentId: string,
    model: ModelRef,
    idempotencyKey: string,
    execution?: ExecutionSelection,
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
  runtimes,
  harnesses = [],
  harnessId = '',
  onSelectHarness,
  busy,
  runStatus,
  onSend,
  onCancel,
}: Props): JSX.Element {
  const [draft, setDraft] = useState('');
  const [agentId, setAgentId] = useState('');
  const [selectedModelKey, setSelectedModelKey] = useState('');
  const [runtimeId, setRuntimeId] = useState('');
  const idempotencyKey = useRef(newIdempotencyKey());
  const sending = useRef(false);

  const models = useMemo(() => {
    const unique = new Map<string, ModelRef>();
    for (const agent of agents) {
      unique.set(modelKey(agent.model), agent.model);
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

  const runtime = runtimes?.find((item) => item.id === runtimeId)
    ?? runtimes?.find((item) => item.available)
    ?? runtimes?.[0];
  const runtimeMode = runtimes !== undefined;
  const activeModel = runtimeMode
    ? runtime?.model ?? null
    : models.find((model) => modelKey(model) === selectedModelKey) ?? null;
  const activeId = runtimeMode ? runtime?.id ?? '' : agentId;
  const harness = harnesses.find((item) => item.id === harnessId);
  const validSelection = activeId !== '' && activeModel !== null && (
    !runtimeMode || (runtime?.available === true && harness !== undefined)
  );
  const selectionKey = `${activeId}\u0000${modelKey(activeModel ?? { provider: '', name: '' })}\u0000${activeModel?.reasoning_effort ?? ''}\u0000${runtimeMode ? `${harnessId}\u0000${String(harness?.revision ?? '')}` : ''}`;
  useEffect(() => {
    idempotencyKey.current = newIdempotencyKey();
  }, [selectionKey]);
  const cancellable =
    runStatus !== null &&
    !['succeeded', 'failed', 'cancelled', 'cancelling'].includes(runStatus);

  async function submit(): Promise<void> {
    if (
      sending.current ||
      busy ||
      draft.trim() === '' ||
      !validSelection ||
      activeModel === null
    ) {
      return;
    }
    sending.current = true;
    try {
      const args: [string, string, ModelRef, string] = [
        draft.trim(), activeId, activeModel, idempotencyKey.current,
      ];
      const sent = runtimeMode && runtime !== undefined
        ? await onSend(...args, { runtime_id: runtime.id, harness_id: harnessId })
        : await onSend(...args);
      if (sent) {
        setDraft('');
        idempotencyKey.current = newIdempotencyKey();
      }
    } finally {
      sending.current = false;
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
        {runtimeMode ? (
          <label>
            <span>Runtime</span>
            <select
              aria-label="Runtime"
              value={runtime?.id ?? ''}
              disabled={busy || runtimes.length === 0}
              onChange={(event) => setRuntimeId(event.target.value)}
            >
              {runtimes.length === 0 ? <option value="">Unavailable</option> : null}
              {runtimes.map((item) => (
                <option key={item.id} value={item.id} disabled={!item.available}>
                  {item.name}{!item.available ? ` (Unavailable: ${item.unavailable_reason ?? 'not ready'})` : ''}
                </option>
              ))}
            </select>
          </label>
        ) : <label>
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
        </label>}
        <label>
          <span>Model</span>
          <select
            aria-label="Model"
            value={activeModel === null ? '' : modelKey(activeModel)}
            disabled={runtimeMode || busy || models.length === 0}
            onChange={(event) => setSelectedModelKey(event.target.value)}
          >
            {(runtimeMode ? (activeModel === null ? [] : [activeModel]) : models).map((model) => (
              <option key={modelKey(model)} value={modelKey(model)}>
                {modelLabel(model)}
              </option>
            ))}
          </select>
        </label>
        {runtimeMode ? (
          <label>
            <span>Harness</span>
            <select
              aria-label="Harness"
              value={harnessId}
              disabled={busy || harnesses.length === 0}
              onChange={(event) => onSelectHarness?.(event.target.value)}
            >
              {!harnesses.some((item) => item.id === harnessId) ? <option value="">Select Harness</option> : null}
              {harnesses.map((item) => (
                <option key={item.id} value={item.id}>{item.name} (r{item.revision})</option>
              ))}
            </select>
          </label>
        ) : null}
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
          disabled={busy || draft.trim() === '' || !validSelection}
        >
          <Send size={15} />
          {busy ? 'Sending' : 'Send'}
        </button>
      </div>
    </form>
  );
}
