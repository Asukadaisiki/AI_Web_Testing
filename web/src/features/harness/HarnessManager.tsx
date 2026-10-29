import { useEffect, useRef, useState } from 'react';
import { errorMessage } from '../../api';
import { harnessDraft, validateHarnessDraft } from './harnessDraft';
import type { ExecutionCatalogState } from './useExecutionCatalog';

interface Props {
  catalog: ExecutionCatalogState;
}

export function HarnessManager({ catalog }: Props): JSX.Element {
  const selected = catalog.harnesses.find((item) => item.id === catalog.harnessId);
  const [editingId, setEditingId] = useState<string | null>(selected?.id ?? null);
  const [draft, setDraft] = useState(() => harnessDraft(selected));
  const [busy, setBusy] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [feedback, setFeedback] = useState<string | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const working = useRef(false);
  const validation = validateHarnessDraft(draft);
  const locked = busy || catalog.loading;

  useEffect(() => {
    setEditingId(selected?.id ?? null);
    setDraft(harnessDraft(selected));
    setConfirmDelete(false);
  }, [selected?.id, selected?.revision]); // Snapshot revisions, not local edits, reset the form.

  function edit(field: keyof typeof draft, value: string): void {
    setDraft((current) => ({ ...current, [field]: value }));
    setFeedback(null);
    setFailure(null);
    setConfirmDelete(false);
  }

  async function perform(action: () => Promise<void>): Promise<void> {
    if (working.current || catalog.loading) return;
    working.current = true;
    setBusy(true);
    setFailure(null);
    setFeedback(null);
    try {
      await action();
    } catch (error) {
      setFailure(errorMessage(error));
    } finally {
      working.current = false;
      setBusy(false);
    }
  }

  async function save(): Promise<void> {
    if (validation.definition === null) return;
    const definition = validation.definition;
    await perform(async () => {
      const snapshot = await catalog.saveHarness(editingId, definition);
      setEditingId(snapshot.id);
      setDraft(harnessDraft(snapshot));
      setFeedback(`Saved ${snapshot.name} (r${String(snapshot.revision)}).`);
    });
  }

  return (
    <section className="harness-manager" aria-label="Harness manager">
      <div className="harness-toolbar">
        <label>
          <span className="sr-only">Manage Harness</span>
          <select
            aria-label="Manage Harness"
            value={editingId ?? ''}
            disabled={locked}
            onChange={(event) => {
              const snapshot = catalog.harnesses.find((item) => item.id === event.target.value);
              if (snapshot === undefined) return;
              catalog.selectHarness(snapshot.id);
              setEditingId(snapshot.id);
              setDraft(harnessDraft(snapshot));
              setFeedback(null);
              setFailure(null);
              setConfirmDelete(false);
            }}
          >
            {editingId === null ? <option value="">New Harness</option> : null}
            {catalog.harnesses.map((item) => (
              <option key={item.id} value={item.id}>{item.name} (r{item.revision})</option>
            ))}
          </select>
        </label>
        <button type="button" className="button" disabled={locked} aria-label="New Harness" onClick={() => {
          setEditingId(null);
          setDraft(harnessDraft());
          setFailure(null);
          setFeedback(null);
          setConfirmDelete(false);
        }}>New</button>
        <button type="button" className="button" disabled={locked} aria-label="Refresh Harnesses" onClick={() => {
          void perform(async () => {
            await catalog.refresh();
            setFeedback('Refreshed Harnesses.');
          });
        }}>Refresh</button>
      </div>
      <form aria-label="Harness configuration" onSubmit={(event) => {
        event.preventDefault();
        void save();
      }}>
        <fieldset disabled={locked}>
          <label>
            <span>Name</span>
            <input aria-label="Harness name" value={draft.name} onChange={(event) => edit('name', event.target.value)} />
          </label>
          <label>
            <span>System prompt</span>
            <textarea aria-label="System prompt" rows={3} value={draft.prompt} onChange={(event) => edit('prompt', event.target.value)} />
          </label>
          <div className="harness-json-fields">
            <label>
              <span>Skills JSON</span>
              <textarea aria-label="Skills JSON" spellCheck={false} rows={5} value={draft.skillsJSON} onChange={(event) => edit('skillsJSON', event.target.value)} />
            </label>
            <label>
              <span>MCP JSON</span>
              <textarea aria-label="MCP JSON" aria-describedby="harness-env-note" spellCheck={false} rows={5} value={draft.mcpJSON} onChange={(event) => edit('mcpJSON', event.target.value)} />
            </label>
          </div>
          <p id="harness-env-note" className="harness-note">Use env:VARIABLE_NAME for secrets; credentials stay on the host.</p>
          <div className="inline-actions">
            <button type="submit" className="button button-primary" disabled={validation.definition === null} aria-label={editingId === null ? 'Create Harness' : 'Save Harness'}>
              {editingId === null ? 'Create' : 'Save'}
            </button>
            {editingId !== null ? (
              <button type="button" className="button button-danger" aria-label={confirmDelete ? 'Confirm delete Harness' : 'Delete Harness'} onClick={() => {
                if (!confirmDelete) { setConfirmDelete(true); return; }
                const id = editingId;
                void perform(async () => {
                  await catalog.removeHarness(id);
                  setConfirmDelete(false);
                  setFeedback('Deleted Harness. Existing Runs keep their snapshots.');
                });
              }}>{confirmDelete ? 'Confirm delete' : 'Delete'}</button>
            ) : null}
            {confirmDelete ? <button type="button" className="button" onClick={() => setConfirmDelete(false)}>Keep Harness</button> : null}
          </div>
        </fieldset>
      </form>
      {failure ?? catalog.error ?? validation.error ? (
        <p className="inline-error" role="alert">{failure ?? catalog.error ?? validation.error}</p>
      ) : feedback !== null ? <p className="harness-note" role="status">{feedback}</p> : null}
    </section>
  );
}
