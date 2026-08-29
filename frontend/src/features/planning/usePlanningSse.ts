import { useCallback, useEffect, useRef } from "react";

import {
  callSSE,
  type SSEClientOptions,
} from "../../shared/api/sseClient";

type PlanningSseRequest = Omit<SSEClientOptions, "signal">;

export function usePlanningSse() {
  const abortRef = useRef<AbortController | null>(null);

  const abort = useCallback(() => {
    abortRef.current?.abort();
    abortRef.current = null;
  }, []);

  const run = useCallback(async (options: PlanningSseRequest) => {
    abort();
    const controller = new AbortController();
    abortRef.current = controller;
    try {
      await callSSE({
        ...options,
        signal: controller.signal,
      });
    } finally {
      if (abortRef.current === controller) {
        abortRef.current = null;
      }
    }
  }, [abort]);

  useEffect(() => abort, [abort]);

  return { abort, run };
}
