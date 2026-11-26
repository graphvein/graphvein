// useStatus polls GET /api/v0/status and exposes the latest result.
//
// Rules (task T016c D8):
// - request on mount, every 30 s while the page is visible, when the page
//   becomes visible again or the window gets focus, and on "Check";
// - never two requests at once: a timer, visibility or focus tick is skipped
//   while one is in flight, and "Check" waits for it and then sends its own;
// - a failed request replaces the previous result, so an old "Fresh" is
//   never shown after a check failed;
// - when the page becomes visible or gets focus again and the last answer
//   is older than the poll interval, that answer is withdrawn ("checking")
//   until a new one arrives: a tab left in the background for an hour must
//   not keep showing the hour-old "Fresh" while the new request runs;
// - an answer to a request started before the latest withdrawal is ignored
//   (it may describe the state before the absence), and a withdrawal while
//   a request is in flight queues a new one, so a current answer follows.

import { useCallback, useEffect, useRef, useState } from "react";

import { fetchStatus } from "@/api/client";
import type { Status } from "@/api/status";

/** How often the status is requested while the page is visible. */
export const POLL_INTERVAL_MS = 30_000;

/** The result shown by the shell: still loading the first answer, the last
 * successful answer, a withdrawn answer that was too old when the page came
 * back into view (a new one is on its way), or the last request's failure. */
export type StatusView =
  | { kind: "loading" }
  | { kind: "checking" }
  | { kind: "ok"; status: Status }
  | { kind: "failed"; error: Error; at: Date };

/** StatusFetcher requests the status once; tests pass their own. */
export type StatusFetcher = (options: { signal: AbortSignal }) => Promise<Status>;

/** StatusResult is what useStatus returns to the shell. */
export interface StatusResult {
  view: StatusView;
  /** True while a request is in flight. */
  checking: boolean;
  /** The repository name from the last successful answer. It identifies the
   * page and is kept after a failure; freshness is not. */
  repository: string | undefined;
  /** Requests the status now, after any request already in flight. */
  check: () => void;
}

interface State {
  view: StatusView;
  checking: boolean;
  repository: string | undefined;
}

/**
 * useStatus keeps the server status current for the shell, following the
 * rules at the top of this file. It owns the polling timer and the page
 * visibility and focus listeners, so components only render the view; the
 * fetcher is injectable so tests can answer requests by hand.
 */
export function useStatus(fetcher: StatusFetcher = fetchStatus): StatusResult {
  const [state, setState] = useState<State>({
    view: { kind: "loading" },
    checking: true,
    repository: undefined,
  });
  const checkRef = useRef<() => void>(() => undefined);

  useEffect(() => {
    const controller = new AbortController();
    const signal = controller.signal;
    let inFlight = false;
    let checkQueued = false;
    // When the last successful answer arrived (Date.now()), or undefined.
    let lastSuccess: number | undefined;
    // Incremented by each withdrawal; a request remembers the value it
    // started with, and its answer is used only if no withdrawal came since.
    let epoch = 0;

    const request = () => {
      const startedIn = epoch;
      inFlight = true;
      setState((s) => (s.checking ? s : { ...s, checking: true }));
      fetcher({ signal })
        .then(
          (status) => {
            if (signal.aborted || startedIn !== epoch) return;
            lastSuccess = Date.now();
            setState({ view: { kind: "ok", status }, checking: false, repository: status.repository });
          },
          (err: unknown) => {
            if (signal.aborted || startedIn !== epoch) return;
            lastSuccess = undefined;
            const error = err instanceof Error ? err : new Error(String(err));
            setState((s) => ({ ...s, view: { kind: "failed", error, at: new Date() }, checking: false }));
          },
        )
        .finally(() => {
          inFlight = false;
          if (checkQueued && !signal.aborted) {
            checkQueued = false;
            request();
          }
        });
    };

    const tick = () => {
      if (!inFlight && document.visibilityState === "visible") request();
    };
    // A return to the page (visible again, or focus) first withdraws an
    // answer older than the poll interval, even when a request is already
    // in flight, then requests as a tick would. A request in flight at the
    // withdrawal started before it, so its answer is ignored and a new
    // request is queued behind it.
    const onReturn = () => {
      if (document.visibilityState !== "visible") return;
      if (lastSuccess !== undefined && Date.now() - lastSuccess > POLL_INTERVAL_MS) {
        lastSuccess = undefined;
        epoch++;
        if (inFlight) checkQueued = true;
        setState((s) => (s.view.kind === "ok" ? { ...s, view: { kind: "checking" } } : s));
      }
      tick();
    };
    checkRef.current = () => {
      if (inFlight) checkQueued = true;
      else request();
    };
    request();
    const timer = setInterval(tick, POLL_INTERVAL_MS);
    document.addEventListener("visibilitychange", onReturn);
    window.addEventListener("focus", onReturn);
    return () => {
      controller.abort();
      clearInterval(timer);
      document.removeEventListener("visibilitychange", onReturn);
      window.removeEventListener("focus", onReturn);
      checkRef.current = () => undefined;
    };
  }, [fetcher]);

  const check = useCallback(() => {
    checkRef.current();
  }, []);

  return { ...state, check };
}
