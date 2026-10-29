import { useCallback, useEffect, useRef, useState } from 'react';
import {
  createHarness, deleteHarness, errorMessage, listHarnesses, listRuntimes, updateHarness,
  type HarnessDefinition, type HarnessSnapshot, type Runtime,
} from '../../api';

export interface ExecutionCatalogState {
  runtimes: Runtime[];
  harnesses: HarnessSnapshot[];
  harnessId: string;
  loading: boolean;
  error: string | null;
  selectHarness: (id: string) => void;
  refresh: () => Promise<void>;
  saveHarness: (id: string | null, definition: HarnessDefinition) => Promise<HarnessSnapshot>;
  removeHarness: (id: string) => Promise<void>;
}

export function useExecutionCatalog(): ExecutionCatalogState {
  const [runtimes, setRuntimes] = useState<Runtime[]>([]);
  const [harnesses, setHarnesses] = useState<HarnessSnapshot[]>([]);
  const [selectedId, selectHarness] = useState('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const request = useRef(0);
  const harnessId = harnesses.find((item) => item.id === selectedId)?.id ?? harnesses[0]?.id ?? '';

  const refresh = useCallback(async (): Promise<void> => {
    const version = ++request.current;
    setLoading(true);
    const [runtimeResult, harnessResult] = await Promise.allSettled([listRuntimes(), listHarnesses()]);
    if (version !== request.current) return;
    setRuntimes(runtimeResult.status === 'fulfilled' ? runtimeResult.value : []);
    setHarnesses(harnessResult.status === 'fulfilled' ? harnessResult.value : []);
    const failures = [runtimeResult, harnessResult].flatMap((result) =>
      result.status === 'rejected' ? [errorMessage(result.reason)] : [],
    );
    setError(failures.length === 0 ? null : failures.join(' '));
    setLoading(false);
  }, []);

  useEffect(() => {
    void refresh();
    return () => { request.current += 1; };
  }, [refresh]);

  const saveHarness = useCallback(async (
    id: string | null, definition: HarnessDefinition,
  ): Promise<HarnessSnapshot> => {
    const snapshot = id === null
      ? await createHarness(definition)
      : await updateHarness(id, definition);
    request.current += 1;
    setLoading(false);
    setHarnesses((items) => items.some((item) => item.id === snapshot.id)
      ? items.map((item) => item.id === snapshot.id ? snapshot : item)
      : [...items, snapshot]);
    selectHarness(snapshot.id);
    return snapshot;
  }, []);

  const removeHarness = useCallback(async (id: string): Promise<void> => {
    await deleteHarness(id);
    request.current += 1;
    setLoading(false);
    setHarnesses((items) => items.filter((item) => item.id !== id));
  }, []);

  return { runtimes, harnesses, harnessId, loading, error, selectHarness, refresh, saveHarness, removeHarness };
}
