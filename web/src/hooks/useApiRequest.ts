// useApiRequest runs one request per key and exposes its result. A new key
// (other parameters, or a reload token after the index changed) aborts the
// previous request and drops its data at once: the view shows loading
// instead of an answer for other parameters or an older index.

import { useCallback, useEffect, useEffectEvent, useState } from "react";

/** The result of a request: nothing requested (key null), loading, the
 * answer, or the failure. */
export type RequestState<T> =
  | { kind: "idle" }
  | { kind: "loading" }
  | { kind: "ok"; data: T }
  | { kind: "failed"; error: Error };

export interface RequestResult<T> {
  state: RequestState<T>;
  /** Repeats the request with the same key. */
  retry: () => void;
}

interface Held<T> {
  key: string | null;
  attempt: number;
  state: RequestState<T>;
}

/**
 * useApiRequest calls load whenever key or the retry counter changes; key
 * null means "nothing to request". load must read everything it needs from
 * the key's inputs; it is not a dependency itself, so callers can pass an
 * inline function. waiting holds the request back (state "loading") until
 * an input it needs is known, such as the editor choice (ADR 0050).
 */
export function useApiRequest<T>(key: string | null, load: (signal: AbortSignal) => Promise<T>, waiting = false): RequestResult<T> {
  const [attempt, setAttempt] = useState(0);
  const [held, setHeld] = useState<Held<T>>({ key: null, attempt: 0, state: { kind: "idle" } });
  const run = useEffectEvent(load);

  useEffect(() => {
    if (key === null || waiting) return;
    const controller = new AbortController();
    run(controller.signal).then(
      (data) => {
        if (!controller.signal.aborted) setHeld({ key, attempt, state: { kind: "ok", data } });
      },
      (err: unknown) => {
        if (controller.signal.aborted) return;
        const error = err instanceof Error ? err : new Error(String(err));
        setHeld({ key, attempt, state: { kind: "failed", error } });
      },
    );
    return () => {
      controller.abort();
    };
  }, [key, attempt, waiting]);

  const retry = useCallback(() => {
    setAttempt((a) => a + 1);
  }, []);

  let state: RequestState<T>;
  if (key === null) state = { kind: "idle" };
  else if (waiting || held.key !== key || held.attempt !== attempt) state = { kind: "loading" };
  else state = held.state;
  return { state, retry };
}
