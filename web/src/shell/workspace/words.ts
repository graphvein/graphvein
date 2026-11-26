// Words and dot states of the Workspace list (T038, claim rows WS-41,
// WS-42, WS-44, WS-69). Freshness comes only from a recorded check
// (`freshness`, WS-9): null is "not checked yet" and never green; a state
// the UI does not know keeps the server's word.

import { ApiError } from "@/api/client";
import type { Job } from "@/api/indexRun";
import type { Freshness, WorkspaceEntry } from "@/api/workspace";
import { formatShortTime } from "@/lib/time";

/** DotState is how a freshness dot is drawn. */
export type DotState = "fresh" | "warn" | "unchecked" | "gone" | "none";

/** dotState maps a recorded freshness to a dot (WS-41): green only for
 * fresh, amber only for stale (something proven changed); not indexed,
 * unknown, a word the UI does not know and no check at all are the
 * neutral hollow dot, which claims nothing. */
export function dotState(f: Freshness | null): DotState {
  if (f === null) return "unchecked";
  if (f.state === "fresh") return "fresh";
  return f.state === "stale" ? "warn" : "unchecked";
}

/** checkAge words how long ago a check ran ("4 min ago"), so a tooltip
 * says how old a dot is (T038 review); "" for a time that does not
 * parse or lies in the future (another clock). */
export function checkAge(checkedAt: string, now: number = Date.now()): string {
  const t = new Date(checkedAt).getTime();
  if (Number.isNaN(t) || t > now) return "";
  const minutes = Math.floor((now - t) / 60_000);
  if (minutes < 1) return "less than a minute ago";
  if (minutes < 60) return `${String(minutes)} min ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${String(hours)} h ago`;
  return `${String(Math.floor(hours / 24))} d ago`;
}

/** freshnessText words a recorded freshness with the time of its check
 * and the check's age. */
export function freshnessText(f: Freshness | null, now: number = Date.now()): string {
  if (f === null) return "not checked yet";
  const age = checkAge(f.checked_at, now);
  const at = `checked ${formatShortTime(f.checked_at)}${age === "" ? "" : ` (${age})`}`;
  switch (f.state) {
    case "fresh":
      return `fresh, ${at}`;
    case "stale":
      return f.changed_count === null ? `stale: files changed since the index, ${at}` : `stale: ${String(f.changed_count)} ${f.changed_count === 1 ? "file" : "files"} changed since the index, ${at}`;
    case "not_indexed":
      return `not indexed yet, ${at}`;
    case "unknown":
      return f.error === null ? `freshness unknown, ${at}` : `freshness unknown: ${f.error}, ${at}`;
    default:
      return `${f.state}, ${at}`;
  }
}

/** entryDot is an entry's dot: a folder that is gone overrides freshness
 * (WS-42). */
export function entryDot(e: WorkspaceEntry): DotState {
  if (e.state === "folder_gone") return "gone";
  return e.conflict !== null ? "unchecked" : dotState(e.freshness);
}

/** conflictHint is the short line of an entry in a shared-store conflict
 * (WS-58): which entry holds the name and where to rename one. The full
 * server message is the line's tooltip and screen-reader text. */
export function conflictHint(e: WorkspaceEntry): { holder: string; rename: string } | null {
  const c = e.conflict;
  if (c === null) return null;
  return { holder: `name ${c.name} shared with ${c.entry_name}`, rename: c.kind === "project" ? "rename in graphvein-project.yaml" : "rename in graphvein.yaml" };
}

/** entryTitle is an entry row's tooltip: everything the list knows. */
export function entryTitle(e: WorkspaceEntry): string {
  const parts = [`${e.name} (${e.kind === "project" ? "project" : e.kind === "repository" ? "repository" : e.kind})`, e.path];
  if (e.state === "folder_gone") parts.push(`folder not found at ${e.path}; the entry stays until it is removed`);
  else if (e.conflict !== null) parts.push(e.conflict.message);
  else parts.push(freshnessText(e.freshness));
  if (e.state === "open") parts.push("index open in this server");
  else if (e.state !== "not_open" && e.state !== "folder_gone") parts.push(`state: ${e.state}`);
  if (e.members_error !== null) parts.push(`manifest: ${e.members_error}`);
  return parts.join("\n");
}

/** jobRunning says whether a job has not ended. */
export function jobRunning(job: Job | null): boolean {
  return job !== null && job.ended_at === null && job.state !== "committed" && job.state !== "failed" && job.state !== "cancelled";
}

const PHASE_WORDS: Record<string, string> = {
  starting: "starting",
  scanning: "scanning",
  cache: "fact cache",
  parsing: "parsing",
  linking: "linking",
  writing: "writing",
  committing: "committing",
  finishing: "finishing",
};

/** phaseWord words a run phase; an unknown phase keeps the server's word
 * (WS-44). */
export function phaseWord(phase: string | null): string {
  if (phase === null) return "starting";
  return PHASE_WORDS[phase] ?? phase;
}

/** jobProgress is the run-counted part of a job: "212/412" only when the
 * run counted both numbers, never an estimate (WS-44). */
export function jobProgress(job: Job): { text: string; fraction: number | null } {
  const p = job.progress;
  if (p === null || p.total === 0) return { text: phaseWord(job.phase), fraction: null };
  return { text: `${String(p.done)}/${String(p.total)}`, fraction: p.done / p.total };
}

/** errorWords is a failed request's code and message for display: the
 * server's code when it sent one, its message without a repeated code. */
export function errorWords(err: Error): { code: string; message: string } {
  if (err instanceof ApiError) {
    const code = err.code ?? err.kind;
    const message = err.message.startsWith(`${code}: `) ? err.message.slice(code.length + 2) : err.message;
    return { code, message };
  }
  return { code: "error", message: err.message };
}

/** needsIndex says whether an entry's row offers "Index it" (WS-55): its
 * recorded check found no index, its folder is there, and no run of this
 * server is running or has committed since. */
export function needsIndex(e: WorkspaceEntry): boolean {
  if (e.state === "folder_gone" || e.conflict !== null || e.freshness === null || e.freshness.state !== "not_indexed") return false;
  return e.job === null || (!jobRunning(e.job) && e.job.state !== "committed");
}
