// The state mapping of T016j (variant D): from the status answer, the run
// answer (GET /api/v0/index/run) and what this tab did, to what the
// freshness badge and the Re-index slot show. It is a pure function so each
// row of the task's state table is a unit test.
//
// Governing principle: an absence, a zero, a default or "complete" is shown
// only where the API proves it. Here:
// - "Fresh" only when the lock is proven free (lock.state "free"); while it
//   is held, unknown or not yet read, a fresh answer shows "Indexed T"
//   without a freshness word;
// - after a job committed run R, "Indexed R, checking freshness..." until a
//   status answer names R or a later run and this server's job released the
//   lock;
// - "Indexing" only while the run answer reports a held lock;
// - a failed or cancelled job stays visible until dismissed or the next run.

import type { IndexRun, Job } from "@/api/indexRun";
import type { StatusView } from "@/hooks/useStatus";
import { COUNT_SIZER_SAMPLES, formatCount } from "@/lib/count";
import { compareRuns } from "@/sections/api/ApiSection";

/** RunView is the latest answer of GET /api/v0/index/run. */
export type RunView =
  | { kind: "loading" }
  | { kind: "ok"; doc: IndexRun }
  | { kind: "failed"; error: Error };

/** PostError is a POST refusal shown to the user (index_locked is not one:
 * the next run poll shows the holder instead). */
export interface PostError {
  code: string;
  message: string;
}

export interface ReindexInputs {
  status: StatusView;
  run: RunView;
  /** True while this tab's POST is in flight. */
  pending: boolean;
  /** This tab's last POST refusal, until dismissed or the next POST. */
  postError: PostError | null;
  /** The job this tab started, to notice a restarted server. */
  startedJobId: string | null;
  /** A failed or cancelled job the user dismissed. */
  dismissedJobId: string | null;
}

/** BadgeOverride replaces the status badge's text without changing what it
 * describes: the index the views show. null leaves the badge as the status
 * answer says. */
export type BadgeOverride =
  | { kind: "indexed"; finishedAt: string; reason: "held" | "unknown" }
  | { kind: "checking"; finishedAt: string };

/** Slot is what the Re-index slot shows. */
export type Slot =
  | { kind: "idle"; primary: boolean }
  | { kind: "unavailable"; reason: string }
  | { kind: "pending" }
  | { kind: "checking" }
  | { kind: "running"; job: Job | null }
  | { kind: "elsewhere" }
  | { kind: "failed"; job: Job }
  | { kind: "cancelled"; job: Job }
  | { kind: "not_started"; error: PostError }
  | { kind: "outcome_unknown"; jobId: string };

export interface ReindexModel {
  badge: BadgeOverride | null;
  slot: Slot;
  /** Why Retry (or Re-index) in the details is unavailable now; null when
   * the lock is proven free. */
  startBlocked: string | null;
}

/** unsettled is a free lock next to a job that has not ended: an answer
 * that cannot describe a finished run (a server that releases the lock
 * before recording the outcome). The UI treats it as "outcome
 * still being read", polls fast and never shows idle or Fresh for it. */
export function unsettled(doc: IndexRun): boolean {
  const job = doc.job;
  return doc.lock.state === "free" && job !== null && job.ended_at === null && (job.state === "running" || job.state === "unknown");
}

function statusRun(status: StatusView): string | null {
  if (status.kind !== "ok") return null;
  return status.status.index.last_run?.finished_at ?? "none";
}

function badgeOverride(input: ReindexInputs): BadgeOverride | null {
  const { status, run } = input;
  if (status.kind !== "ok") return null;
  const lock = run.kind === "ok" ? run.doc.lock.state : "unknown";
  const settling = run.kind === "ok" && unsettled(run.doc);
  const held = lock === "held_here" || lock === "held_elsewhere" || settling;
  const job = run.kind === "ok" ? run.doc.job : null;
  const shown = statusRun(status) ?? "none";
  if (job?.state === "committed" && job.run !== null) {
    // Still closing its store (held_here), or the status has not named the
    // run yet. A lock held elsewhere after the switch is another run: the
    // committed job is then just the shown index.
    if (lock === "held_here" || compareRuns(shown, job.run.finished_at) < 0) {
      return { kind: "checking", finishedAt: job.run.finished_at };
    }
  }
  const last = status.status.index.last_run;
  if (status.status.freshness.state === "fresh" && (lock !== "free" || settling) && last !== null) {
    return { kind: "indexed", finishedAt: last.finished_at, reason: held ? "held" : "unknown" };
  }
  return null;
}

function lockReason(run: RunView): string | null {
  if (run.kind === "loading") return "Reading whether an index run is in progress...";
  if (run.kind === "failed") return `The index run state could not be read: ${run.error.message}`;
  if (unsettled(run.doc)) return "Reading the outcome of this server's re-index...";
  switch (run.doc.lock.state) {
    case "free":
      return null;
    case "held_here":
      return "This server's re-index is running.";
    case "held_elsewhere":
      return "Another process holds the index lock.";
    case "unknown":
      if (run.doc.lock.reported_state !== "unknown") {
        return `The server reported a lock state this UI does not know: ${run.doc.lock.reported_state}`;
      }
      return `The server could not check the index lock: ${run.doc.lock.error ?? "no reason given"}`;
  }
}

/** deriveReindex maps the inputs to the badge and slot (task section 5 as
 * changed by the developer decisions D1-D8). The order of the checks is the
 * priority of the states. */
export function deriveReindex(input: ReindexInputs): ReindexModel {
  const { run, status } = input;
  const badge = badgeOverride(input);
  const startBlocked = lockReason(run);
  const result = (slot: Slot): ReindexModel => ({ badge, slot, startBlocked });

  if (input.pending) return result({ kind: "pending" });
  if (run.kind !== "ok") return result({ kind: "unavailable", reason: startBlocked ?? "" });
  const { lock, job } = run.doc;
  if (lock.state === "held_elsewhere") return result({ kind: "elsewhere" });
  if (job !== null && job.id !== input.dismissedJobId) {
    if (job.state === "failed") return result({ kind: "failed", job });
    if (job.state === "cancelled") return result({ kind: "cancelled", job });
  }
  // A held lock is newer news than a POST refusal.
  if (lock.state === "held_here") return result({ kind: "running", job: job?.ended_at === null ? job : null });
  if (unsettled(run.doc)) return result({ kind: "checking" });
  if (input.postError !== null) return result({ kind: "not_started", error: input.postError });
  if (job === null && input.startedJobId !== null) return result({ kind: "outcome_unknown", jobId: input.startedJobId });
  if (lock.state !== "free") return result({ kind: "unavailable", reason: startBlocked ?? "" });
  const fresh = status.kind === "ok" ? status.status.freshness.state : null;
  return result({ kind: "idle", primary: fresh === "stale" || fresh === "not_indexed" });
}

export { formatCount } from "@/lib/count";

/** PHASE_LABELS names the phases in the details list. */
export const PHASE_LABELS: Record<string, string> = {
  starting: "Starting",
  scanning: "Scanning",
  cache: "Fact cache",
  parsing: "Parsing",
  linking: "Linking",
  writing: "Writing",
  committing: "Committing",
  finishing: "Finishing",
};

/** slotLabel is the visible text of the slot. */
export function slotLabel(slot: Slot): string {
  switch (slot.kind) {
    case "idle":
    case "unavailable":
      return "Re-index";
    case "pending":
      return "Starting...";
    case "checking":
      return "Checking...";
    case "running": {
      const p = slot.job?.state === "running" ? slot.job.progress : null;
      return p === null ? "Indexing" : `Indexing ${formatCount(p.done)} / ${formatCount(p.total)}`;
    }
    case "elsewhere":
      return "Indexing (CLI)";
    case "failed":
    case "not_started":
      return "Re-index failed · details";
    case "cancelled":
      return "Re-index cancelled · details";
    case "outcome_unknown":
      return "Outcome unknown · details";
  }
}

/** SLOT_SIZER_LABELS are every label the slot can show, at its widest, so
 * the slot never changes width between states (decision D1 gate). */
export const SLOT_SIZER_LABELS = [
  "Re-index",
  "Starting...",
  "Checking...",
  ...COUNT_SIZER_SAMPLES.map((c) => `Indexing ${c} / ${c}`),
  "Indexing (CLI)",
  "Re-index failed · details",
  "Re-index cancelled · details",
  "Outcome unknown · details",
];

/** announcement is the polite live-region text: the state and phase, never
 * every count (progress inside a button is not announced). */
export function announcement(slot: Slot): string {
  switch (slot.kind) {
    case "idle":
    case "unavailable":
      return "";
    case "pending":
      return "Starting a re-index";
    case "checking":
      return "Reading the re-index outcome";
    case "running": {
      const phase = slot.job?.phase;
      if (slot.job?.state === "committed") return "Re-index committed";
      return phase ? `Indexing: ${PHASE_LABELS[phase] ?? phase}` : "Indexing";
    }
    case "elsewhere":
      return "Another process is indexing";
    case "failed":
    case "not_started":
      return "Re-index failed";
    case "cancelled":
      return "Re-index cancelled";
    case "outcome_unknown":
      return "Re-index outcome unknown";
  }
}
