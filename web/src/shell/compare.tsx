// The global compare state (T019 Revision 2, V3 "Global compare"): one base
// for the Changes section and for the "changed since" markers of every
// view, chosen with the compare chip in the top bar, remembered per
// browser, with a one-click off. The shell owns the one summary request
// (the chip's count, the sidebar badge and group list, and the Changes
// section read the same answer), the bases list, and the branch-indexing
// job (POST/GET /api/v0/changes/refs).
//
// An expired or unknown base answers the summary with a refusal; every
// consumer reads that one answer, so markers (T019b part 2) drop together
// and the chip shows the refusal.

import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";

import { httpChangesClient, type ChangesBases, type ChangesClient, type ChangesHeader, type ChangesSummary, type RefJob, type SideInfo } from "@/api/changes";
import { useApiRequest, type RequestResult, type RequestState } from "@/hooks/useApiRequest";
import type { StatusView } from "@/hooks/useStatus";
import { runOf } from "@/hooks/useSchemaSummary";

/** COMPARE_KEY stores {base, on} in localStorage. */
export const COMPARE_KEY = "graphvein.compare.v1";

/** DEFAULT_BASE is the server's default: the run before the latest. */
export const DEFAULT_BASE = "previous";

const BASE_RE = /^(previous|run:\S+|pin:.+|ref:.+)$/;

/** CompareChoice is the stored base and whether comparing is on. */
export interface CompareChoice {
  base: string;
  on: boolean;
}

/** validBase says whether a stored or typed base has a form the server
 * accepts (the server still decides whether it names anything). */
export function validBase(base: string): boolean {
  return base.length <= 512 && BASE_RE.test(base) && !base.includes("...");
}

/** readCompare reads the stored choice; anything unreadable is the
 * default (previous run, on). Storage may be blocked. */
export function readCompare(): CompareChoice {
  try {
    const raw = window.localStorage.getItem(COMPARE_KEY);
    if (raw === null) return { base: DEFAULT_BASE, on: true };
    const v: unknown = JSON.parse(raw);
    if (typeof v === "object" && v !== null && "base" in v && "on" in v && typeof v.base === "string" && typeof v.on === "boolean" && validBase(v.base)) {
      return { base: v.base, on: v.on };
    }
  } catch {
    // blocked storage or a damaged value: the default
  }
  return { base: DEFAULT_BASE, on: true };
}

function writeCompare(c: CompareChoice): void {
  try {
    window.localStorage.setItem(COMPARE_KEY, JSON.stringify(c));
  } catch {
    // blocked storage: the choice lasts for this page
  }
}

/** RefProgress is a branch comparison being indexed, or its failure. */
export interface RefProgress {
  base: string;
  head: string | null;
  job: RefJob | null;
  error: Error | null;
}

/** refBaseOf is the base parameter of a branch comparison. */
export function refBaseOf(base: string, head: string | null): string {
  return head === null || head === "" ? `ref:${base}` : `ref:${base}..${head}`;
}

/** baseIdentity is what the bases list says the chosen base resolves to:
 * a pin's id and run (a pin deleted and pinned again under the same name,
 * or re-pointed, is another comparison), the ref snapshots for a branch
 * comparison (indexing a ref again adds a snapshot). Runs and "previous"
 * are fixed by the status's last run. "" without a bases answer. */
export function baseIdentity(base: string, bases: ChangesBases | null): string {
  if (bases === null) return "";
  if (base.startsWith("pin:")) {
    const p = bases.pins.find((x) => x.name === base.slice(4));
    return p === undefined ? "pin:none" : `pin:${p.id}@${p.run.run_id}`;
  }
  if (base.startsWith("ref:")) return `refs:${bases.refs.map((r) => `${r.namespace}@${r.commit}@${r.run.run_id}`).join(",")}`;
  return "";
}

function sideKey(x: SideInfo): string {
  return [x.kind, x.pin ?? "", x.run, x.finished_at, x.commit ?? ""].join("@");
}

/** comparisonKey names the comparison a summary resolved: the index run
 * it was read from, both sides (run, pin, commit) and the base's identity
 * in the bases list. Every items, impact and marker request key includes
 * it (T019 Revision 5, R5-3), so a re-run branch comparison or a re-pointed
 * pin reloads them instead of keeping an older comparison's answers. */
export function comparisonKey(s: ChangesSummary, identity: string): string {
  return [s.repository, s.base, s.index?.finished_at ?? "none", sideKey(s.summary.base), sideKey(s.summary.head), identity].join("|");
}

/** answerMatches says whether an items or impact answer was read from the
 * same index run as the summary (R5-2): an index run committed between
 * them makes the answer part of another comparison. */
export function answerMatches(summary: ChangesSummary, answer: ChangesHeader): boolean {
  return answer.repository === summary.repository && (answer.index?.finished_at ?? null) === (summary.index?.finished_at ?? null);
}

/** MARKERS_OFF_HEAD is the chip's note when the comparison's head is not
 * the current index (R5-1): the views show the current index, so a marker
 * would describe another commit's facts. */
export const MARKERS_OFF_HEAD = "markers off: this comparison's head is not the current index";

/** markersApply says whether the views may show markers for a summary:
 * only when its head is the current index. */
export function markersApply(s: ChangesSummary): boolean {
  return s.summary.head.kind === "current";
}

export interface CompareState {
  choice: CompareChoice;
  /** Sets the base and turns comparing on. */
  setBase: (base: string) => void;
  setOn: (on: boolean) => void;
  /** The summary for the chosen base (idle while off or not indexed). */
  summary: RequestResult<ChangesSummary>;
  bases: RequestResult<ChangesBases>;
  ref: RefProgress | null;
  /** Starts indexing a branch comparison; the base switches when the job
   * has committed. */
  startRef: (base: string, head: string | null) => void;
  dismissRef: () => void;
  pin: (name: string, run: string | null) => Promise<void>;
  unpin: (name: string) => Promise<void>;
  menuOpen: boolean;
  setMenuOpen: (open: boolean) => void;
  client: ChangesClient;
  /** The status's last run ("none" without one). */
  run: string | null;
  /** The resolved comparison of the summary answer (comparisonKey), null
   * until the summary answers. Request keys include it. */
  comparison: string | null;
  /** reportStale is called with an answer that does not match the summary
   * (answerMatches false): the status is checked and the summary and
   * bases reload, once per pair of index runs. */
  reportStale: (answer: ChangesHeader) => void;
}

const CompareContext = createContext<CompareState | null>(null);

/** useCompare reads the compare state; outside the provider (component
 * tests) it throws, so a missing provider is not mistaken for "off". */
export function useCompare(): CompareState {
  const c = useContext(CompareContext);
  if (c === null) throw new Error("useCompare outside CompareProvider");
  return c;
}

/** useOptionalCompare is useCompare for parts that also render without
 * the provider (the top bar in older tests). */
export function useOptionalCompare(): CompareState | null {
  return useContext(CompareContext);
}

const POLL_MS = 1000;

/**
 * CompareProvider owns the compare state. onStale asks the shell's status
 * for a check (an answer from a newer index run than the status knows).
 */
export function CompareProvider({ status, client = httpChangesClient, onStale, children }: { status: StatusView; client?: ChangesClient; onStale?: () => void; children: ReactNode }) {
  const [choice, setChoice] = useState<CompareChoice>(readCompare);
  const [gen, setGen] = useState(0);
  const [ref, setRef] = useState<RefProgress | null>(null);
  const [menuOpen, setMenuOpen] = useState(false);
  const run = status.kind === "ok" ? runOf(status.status.index.last_run) : null;
  const indexed = run !== null && run !== "none";

  // The bases list reloads with every status answer (R5-3: a pin deleted
  // and pinned again from the CLI changes no index run). The last answer
  // stays shown while the next one loads, so the menu does not flicker.
  const [poll, setPoll] = useState<{ seen: StatusView; n: number }>({ seen: status, n: 0 });
  if (poll.seen !== status) setPoll({ seen: status, n: status.kind === "ok" ? poll.n + 1 : poll.n });
  const basesReq = useApiRequest(indexed ? `bases#${run}#${String(gen)}#${String(poll.n)}` : null, (signal) => client.bases({ signal }));
  const [lastBases, setLastBases] = useState<ChangesBases | null>(null);
  if (basesReq.state.kind === "ok" && basesReq.state.data !== lastBases) setLastBases(basesReq.state.data);
  const basesState = basesReq.state;
  const basesRetry = basesReq.retry;
  const bases = useMemo<RequestResult<ChangesBases>>(
    () => (basesState.kind === "loading" && lastBases !== null ? { state: { kind: "ok", data: lastBases }, retry: basesRetry } : { state: basesState, retry: basesRetry }),
    [lastBases, basesState, basesRetry],
  );
  const basesData = bases.state.kind === "ok" ? bases.state.data : null;
  const identity = baseIdentity(choice.base, basesData);
  // A pin or branch base waits for the first bases answer, so its summary
  // is requested once, with its identity.
  const needsBases = choice.base.startsWith("pin:") || choice.base.startsWith("ref:");
  const waiting = needsBases && basesData === null && basesReq.state.kind !== "failed";
  const summary = useApiRequest(indexed && choice.on ? `summary#${choice.base}#${run}#${String(gen)}#${identity}` : null, (signal) => client.summary(choice.base, { signal }), waiting);
  const summaryData = summary.state.kind === "ok" ? summary.state.data : null;
  const comparison = summaryData === null ? null : comparisonKey(summaryData, identity);

  // Answers that do not match the summary: check the status (the badge and
  // the run) and reload the summary and the bases, once per pair of runs.
  const seenStale = useRef(new Set<string>());
  const summaryRun = summaryData === null ? null : (summaryData.index?.finished_at ?? "none");
  const reportStale = useCallback(
    (answer: ChangesHeader) => {
      const k = `${summaryRun ?? ""}>${answer.index?.finished_at ?? "none"}`;
      if (seenStale.current.has(k)) return;
      seenStale.current.add(k);
      onStale?.();
      setGen((g) => g + 1);
    },
    [summaryRun, onStale],
  );
  // A pin whose run in the bases list is not the summary's base run: the
  // bases answer is older than the summary; reload both once.
  const pinRun = choice.base.startsWith("pin:") ? (basesData?.pins.find((p) => p.name === choice.base.slice(4))?.run.run_id ?? null) : null;
  const pinMismatch = summaryData !== null && summaryData.summary.base.kind === "pin" && pinRun !== null && pinRun !== summaryData.summary.base.run ? `${pinRun}>${summaryData.summary.base.run}` : null;
  useEffect(() => {
    if (pinMismatch === null || seenStale.current.has(`pin:${pinMismatch}`)) return;
    seenStale.current.add(`pin:${pinMismatch}`);
    setGen((g) => g + 1);
  }, [pinMismatch]);

  const update = useCallback((c: CompareChoice) => {
    setChoice(c);
    writeCompare(c);
  }, []);
  const setBase = useCallback(
    (base: string) => {
      setRef(null);
      update({ base, on: true });
    },
    [update],
  );
  const setOn = useCallback((on: boolean) => {
    setChoice((c) => {
      const next = { ...c, on };
      writeCompare(next);
      return next;
    });
  }, []);

  // Polling the job: one GET at a time while it runs; tick schedules the
  // next poll after each answer.
  const [tick, setTick] = useState(0);
  const jobId = ref?.job?.state === "running" ? ref.job.id : null;
  useEffect(() => {
    if (jobId === null) return;
    const controller = new AbortController();
    const timer = setTimeout(() => {
      client.refs({ signal: controller.signal }).then(
        (job) => {
          if (controller.signal.aborted) return;
          if (job !== null && job.state === "committed" && job.id === jobId) {
            setRef(null);
            update({ base: job.base, on: true });
            setGen((g) => g + 1);
            return;
          }
          setRef((cur) => (cur === null ? null : { ...cur, job }));
          setTick((t) => t + 1);
        },
        (err: unknown) => {
          if (controller.signal.aborted) return;
          setRef((cur) => (cur === null ? null : { ...cur, error: err instanceof Error ? err : new Error(String(err)) }));
        },
      );
    }, POLL_MS);
    return () => {
      clearTimeout(timer);
      controller.abort();
    };
  }, [jobId, tick, client, update]);

  // A job already running when the page loads (another tab started it).
  useEffect(() => {
    if (!indexed) return;
    const controller = new AbortController();
    client.refs({ signal: controller.signal }).then(
      (job) => {
        if (controller.signal.aborted || job?.state !== "running") return;
        setRef((cur) => cur ?? { base: job.base, head: null, job, error: null });
      },
      () => undefined,
    );
    return () => {
      controller.abort();
    };
  }, [indexed, client]);

  const startRef = useCallback(
    (base: string, head: string | null) => {
      const progress: RefProgress = { base, head, job: null, error: null };
      setRef(progress);
      client.startRefs(base, head, {}).then(
        (job) => {
          if (job?.state === "committed") {
            setRef(null);
            update({ base: job.base, on: true });
            setGen((g) => g + 1);
            return;
          }
          setRef({ ...progress, job });
        },
        (err: unknown) => {
          setRef({ ...progress, error: err instanceof Error ? err : new Error(String(err)) });
        },
      );
    },
    [client, update],
  );
  const dismissRef = useCallback(() => {
    setRef(null);
  }, []);

  const pin = useCallback(
    async (name: string, runId: string | null) => {
      await client.pin(name, runId, {});
      setGen((g) => g + 1);
    },
    [client],
  );
  const unpin = useCallback(
    async (name: string) => {
      await client.unpin(name, {});
      if (choice.base === `pin:${name}`) update({ base: DEFAULT_BASE, on: choice.on });
      setGen((g) => g + 1);
    },
    [client, choice, update],
  );

  const value = useMemo<CompareState>(
    () => ({ choice, setBase, setOn, summary, bases, ref, startRef, dismissRef, pin, unpin, menuOpen, setMenuOpen, client, run, comparison, reportStale }),
    [choice, setBase, setOn, summary, bases, ref, startRef, dismissRef, pin, unpin, menuOpen, client, run, comparison, reportStale],
  );
  return <CompareContext.Provider value={value}>{children}</CompareContext.Provider>;
}

/**
 * useCheckedAnswer withholds an answer (and the later pages of a paged
 * one) that does not match the summary's index run (R5-2): it reads as
 * loading, and the compare state is told so it checks the status and
 * reloads the summary. A page never mixes two comparisons.
 */
export function useCheckedAnswer<T extends ChangesHeader>(c: CompareState, state: RequestState<T>, more: readonly T[] = []): RequestState<T> {
  const summary = c.summary.state.kind === "ok" ? c.summary.state.data : null;
  let stale: T | null = null;
  if (summary !== null && state.kind === "ok") {
    stale = [state.data, ...more].find((a) => !answerMatches(summary, a)) ?? null;
  }
  const { reportStale } = c;
  useEffect(() => {
    if (stale !== null) reportStale(stale);
  }, [stale, reportStale]);
  if (stale !== null) return { kind: "loading" };
  // Without a summary answer there is no comparison to read.
  if (summary === null && state.kind === "ok") return { kind: "loading" };
  return state;
}
