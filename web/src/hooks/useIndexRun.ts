// useIndexRun polls GET /api/v0/index/run, starts a run with POST and
// drives the run switch (T016j section 4).
//
// Rules:
// - request on mount, every 30 s while the page is visible, every 1 s while
//   the answer reports a held lock (held_here or held_elsewhere) or a free
//   lock next to a job that has not ended, at once
//   when the page becomes visible again, and right after a POST answers;
// - never two requests at once;
// - when a job reaches "committed", or the lock goes from held to free, ask
//   useStatus for an immediate check (the sections' run switch follows the
//   status answer);
// - while a committed job's run is newer than the status answer and the
//   lock is free, each such status answer triggers one more check (a status
//   request that raced the commit), at most MAX_RECHECKS times per job;
// - a POST refused with index_locked is not an error to show: the next run
//   answer shows the holder. Any other refusal is kept until dismissed, the
//   next POST, or a run answer showing a held lock or another job.

import { useCallback, useEffect, useRef, useState } from "react";

import { ApiError } from "@/api/client";
import { httpIndexRunClient, type IndexRun, type IndexRunClient } from "@/api/indexRun";
import { POLL_INTERVAL_MS, type StatusView } from "@/hooks/useStatus";
import { compareRuns } from "@/sections/api/ApiSection";
import { unsettled, type PostError, type RunView } from "@/shell/reindexModel";

/** How often the run state is requested while a run holds the lock. */
export const RUN_POLL_INTERVAL_MS = 1_000;

/** At most this many extra status checks per committed job. */
export const MAX_RECHECKS = 5;

export interface IndexRunResult {
  run: RunView;
  pending: boolean;
  postError: PostError | null;
  startedJobId: string | null;
  dismissedJobId: string | null;
  /** start POSTs a new run (ignored while one is pending). */
  start: () => void;
  /** dismiss hides a failed or cancelled job, a POST refusal, or the
   * "outcome unknown" of a job a restarted server forgot. */
  dismiss: () => void;
  /** poll asks for the run state now: another view of the same index (the
   * workspace list, T038 WS-52) saw its job change. */
  poll: () => void;
}

function held(doc: IndexRun | null): boolean {
  const s = doc?.lock.state;
  return s === "held_here" || s === "held_elsewhere";
}

/** fast is true while the answer can change within seconds: a held lock,
 * or a free lock next to a job that has not ended. */
function fast(doc: IndexRun | null): boolean {
  return held(doc) || (doc !== null && unsettled(doc));
}

/** useIndexRun follows the rules at the top of this file. check is
 * useStatus's immediate check; status is its current view. */
export function useIndexRun(status: StatusView, check: () => void, client: IndexRunClient = httpIndexRunClient): IndexRunResult {
  const [run, setRun] = useState<RunView>({ kind: "loading" });
  const [pending, setPending] = useState(false);
  const [postError, setPostError] = useState<PostError | null>(null);
  const [startedJobId, setStartedJobId] = useState<string | null>(null);
  const [dismissedJobId, setDismissedJobId] = useState<string | null>(null);

  const checkRef = useRef(check);
  useEffect(() => {
    checkRef.current = check;
  }, [check]);
  // pollNow asks the polling loop for a request now; set by the effect.
  const pollNow = useRef<() => void>(() => undefined);
  // The answer the transitions compare against.
  const lastDoc = useRef<IndexRun | null>(null);
  // The job id (null: none) of the run answer when the POST was refused;
  // undefined while no refusal is shown.
  const refusedWith = useRef<string | null | undefined>(undefined);

  // accept records an answer (from GET or a 202 POST) and fires the run
  // switch's immediate status check on the transitions.
  const accept = useCallback((doc: IndexRun) => {
    const prev = lastDoc.current;
    lastDoc.current = doc;
    const job = doc.job;
    const newlyCommitted =
      job?.state === "committed" && !(prev?.job?.id === job.id && prev.job.state === "committed");
    const released = held(prev) && doc.lock.state === "free";
    if (prev !== null && (newlyCommitted || released)) checkRef.current();
    // A held lock or another job is newer than a POST refusal.
    const refusal = refusedWith.current;
    if (refusal !== undefined && (held(doc) || (doc.job?.id ?? null) !== refusal)) {
      refusedWith.current = undefined;
      setPostError(null);
    }
    setRun({ kind: "ok", doc });
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    const signal = controller.signal;
    let inFlight = false;
    let queued = false;
    let timer: ReturnType<typeof setTimeout> | undefined;

    const schedule = () => {
      clearTimeout(timer);
      timer = setTimeout(tick, fast(lastDoc.current) ? RUN_POLL_INTERVAL_MS : POLL_INTERVAL_MS);
    };
    const request = () => {
      inFlight = true;
      clearTimeout(timer);
      client
        .fetch({ signal })
        .then(
          (doc) => {
            if (!signal.aborted) accept(doc);
          },
          (err: unknown) => {
            if (signal.aborted) return;
            lastDoc.current = null;
            setRun({ kind: "failed", error: err instanceof Error ? err : new Error(String(err)) });
          },
        )
        .finally(() => {
          inFlight = false;
          if (signal.aborted) return;
          if (queued) {
            queued = false;
            request();
          } else {
            schedule();
          }
        });
    };
    function tick() {
      if (inFlight) return;
      if (document.visibilityState === "visible") request();
      else schedule();
    }
    const onVisible = () => {
      if (document.visibilityState === "visible") tick();
    };
    pollNow.current = () => {
      if (inFlight) queued = true;
      else request();
    };
    request();
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      controller.abort();
      clearTimeout(timer);
      document.removeEventListener("visibilitychange", onVisible);
      pollNow.current = () => undefined;
    };
  }, [client, accept]);

  // Step 4: a status answer older than the committed run asks again.
  const rechecks = useRef<{ job: string; count: number }>({ job: "", count: 0 });
  useEffect(() => {
    if (status.kind !== "ok" || run.kind !== "ok") return;
    const { job, lock } = run.doc;
    if (job?.state !== "committed" || job.run === null || lock.state !== "free") return;
    const shown = status.status.index.last_run?.finished_at ?? "none";
    if (compareRuns(shown, job.run.finished_at) >= 0) return;
    if (rechecks.current.job !== job.id) rechecks.current = { job: job.id, count: 0 };
    if (rechecks.current.count >= MAX_RECHECKS) return;
    rechecks.current.count++;
    checkRef.current();
  }, [status, run]);

  const pendingRef = useRef(false);
  const start = useCallback(() => {
    if (pendingRef.current) return;
    pendingRef.current = true;
    setPending(true);
    refusedWith.current = undefined;
    setPostError(null);
    // A new run replaces the shown failure (D6: "until the next run").
    const current = lastDoc.current?.job;
    if (current) setDismissedJobId(current.id);
    client
      .start()
      .then(
        (doc) => {
          if (doc.job !== null) setStartedJobId(doc.job.id);
          accept(doc);
        },
        (err: unknown) => {
          if (err instanceof ApiError && err.code === "index_locked") return;
          const code = err instanceof ApiError ? (err.code ?? err.kind) : "error";
          let message = err instanceof Error ? err.message : String(err);
          // ApiError messages start with "code: "; the code is shown apart.
          if (message.startsWith(`${code}: `)) message = message.slice(code.length + 2);
          refusedWith.current = lastDoc.current?.job?.id ?? null;
          setPostError({ code, message });
        },
      )
      .finally(() => {
        pendingRef.current = false;
        setPending(false);
        pollNow.current();
      });
  }, [client, accept]);

  const dismiss = useCallback(() => {
    refusedWith.current = undefined;
    setPostError(null);
    setStartedJobId(null);
    const job = lastDoc.current?.job;
    if (job) setDismissedJobId(job.id);
  }, []);

  const poll = useCallback(() => {
    pollNow.current();
  }, []);

  return { run, pending, postError, startedJobId, dismissedJobId, start, dismiss, poll };
}
