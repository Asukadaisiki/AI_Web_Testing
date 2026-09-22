import { useCallback, useEffect, useRef, useState } from 'react';
import { errorMessage } from '../api';

export interface AsyncState<T> {
  data: T | null;
  error: string | null;
  loading: boolean;
  reload: () => void;
}

/**
 * 极简 fetch hook：状态一律来自后端响应，不在前端推断业务状态。
 * deps 变化或调用 reload() 时重新拉取；卸载后丢弃迟到的响应。
 */
export function useApi<T>(load: () => Promise<T>, deps: readonly unknown[]): AsyncState<T> {
  const loadRef = useRef(load);
  loadRef.current = load;

  const [tick, setTick] = useState(0);
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    loadRef.current().then(
      (value: T) => {
        if (cancelled) return;
        setData(value);
        setError(null);
        setLoading(false);
      },
      (err: unknown) => {
        if (cancelled) return;
        setError(errorMessage(err));
        setLoading(false);
      },
    );
    return () => {
      cancelled = true;
    };
    // eslint 不存在于本工程；依赖由调用方显式给出。
  }, [...deps, tick]);

  const reload = useCallback(() => {
    setTick((value) => value + 1);
  }, []);

  return { data, error, loading, reload };
}
