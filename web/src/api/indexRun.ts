// Types, a hand-written guard and the requests for GET and POST
// /api/v0/index/run (docs/cli/serve.md, ADR 0052). Field names mirror the
// JSON. Unknown lock states become "unknown" (never "free"), unknown job
// states become "unknown", and an unknown phase is kept as the word the
// server sent, so the UI can show it without inventing a fraction.

import { ApiError, fetchJSON } from "@/api/client";
import { API_VERSION } from "@/api/status";

/** INDEX_RUN_PATH is the re-index endpoint: GET reads, POST starts a run. */
export const INDEX_RUN_PATH = "/api/v0/index/run";

/** The lock states the UI understands; anything else is "unknown". */
export type LockState = "free" | "held_here" | "held_elsewhere" | "unknown";

/** The job states the UI understands; anything else is "unknown". */
export type JobState = "running" | "committed" | "failed" | "cancelled" | "unknown";

/** The phases in the order a run passes them (docs/cli/serve.md). The
 * server may add phases; Job.phase keeps any string it sends. */
export const PHASES = ["starting", "scanning", "cache", "parsing", "linking", "writing", "committing", "finishing"] as const;

export interface RunRef {
  id?: string;
  finished_at: string;
}

export interface Progress {
  done: number;
  total: number;
  unit: string;
}

export interface Job {
  id: string;
  state: JobState;
  /** The state exactly as the server sent it. */
  reported_state: string;
  started_at: string;
  ended_at: string | null;
  phase: string | null;
  /** Non-null only when the server counted both numbers (cache, parsing,
   * writing); the UI never estimates a fraction. */
  progress: Progress | null;
  replaces: RunRef | null;
  run: RunRef | null;
  error: { code: string; message: string } | null;
}

export interface IndexRun {
  api_version: number;
  repository: string;
  lock: { state: LockState; reported_state: string; error: string | null };
  job: Job | null;
}

/** IndexRunFormatError reports a body that does not have the documented
 * shape; the UI treats it like a failed request. */
export class IndexRunFormatError extends Error {
  constructor(message: string) {
    super(`malformed index run response: ${message}`);
    this.name = "IndexRunFormatError";
  }
}

type JSONObject = Record<string, unknown>;

function object(value: unknown, path: string): JSONObject {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new IndexRunFormatError(`${path} is not an object`);
  }
  return value as JSONObject;
}

function str(obj: JSONObject, key: string, path: string): string {
  const v = obj[key];
  if (typeof v !== "string") throw new IndexRunFormatError(`${path}.${key} is not a string`);
  return v;
}

function strOrNull(obj: JSONObject, key: string, path: string): string | null {
  const v = obj[key];
  if (v === null || v === undefined) return null;
  if (typeof v !== "string") throw new IndexRunFormatError(`${path}.${key} is not a string or null`);
  return v;
}

function count(obj: JSONObject, key: string, path: string): number {
  const v = obj[key];
  if (typeof v !== "number" || !Number.isInteger(v) || v < 0) {
    throw new IndexRunFormatError(`${path}.${key} is not a count`);
  }
  return v;
}

function runRef(value: unknown, path: string): RunRef | null {
  if (value === null || value === undefined) return null;
  const obj = object(value, path);
  const ref: RunRef = { finished_at: str(obj, "finished_at", path) };
  if (obj.id !== undefined) ref.id = str(obj, "id", path);
  return ref;
}

function progress(value: unknown): Progress | null {
  if (value === null || value === undefined) return null;
  const obj = object(value, "job.progress");
  const p = { done: count(obj, "done", "job.progress"), total: count(obj, "total", "job.progress"), unit: str(obj, "unit", "job.progress") };
  // A count beyond its total is not a fraction the run proved.
  if (p.done > p.total) throw new IndexRunFormatError("job.progress.done exceeds total");
  return p;
}

const LOCK_STATES: readonly string[] = ["free", "held_here", "held_elsewhere", "unknown"];
const JOB_STATES: readonly string[] = ["running", "committed", "failed", "cancelled"];

/** parseJob checks one job document (also the `job` of a workspace entry
 * and of an add answer, T038). */
export function parseJob(value: unknown): Job | null {
  if (value === null) return null;
  const obj = object(value, "job");
  const reported = str(obj, "state", "job");
  let error: Job["error"] = null;
  if (obj.error !== null && obj.error !== undefined) {
    const e = object(obj.error, "job.error");
    error = { code: str(e, "code", "job.error"), message: str(e, "message", "job.error") };
  }
  return {
    id: str(obj, "id", "job"),
    state: JOB_STATES.includes(reported) ? (reported as JobState) : "unknown",
    reported_state: reported,
    started_at: str(obj, "started_at", "job"),
    ended_at: strOrNull(obj, "ended_at", "job"),
    phase: strOrNull(obj, "phase", "job"),
    progress: progress(obj.progress),
    replaces: runRef(obj.replaces, "job.replaces"),
    run: runRef(obj.run, "job.run"),
    error,
  };
}

/** parseIndexRun checks a decoded GET (or 202 POST) body and returns it
 * typed. A missing or mistyped field, or another API version, throws. */
export function parseIndexRun(value: unknown): IndexRun {
  const obj = object(value, "index run");
  const v = obj.api_version;
  if (v !== API_VERSION) {
    throw new IndexRunFormatError(`api_version is ${String(v)}; this UI reads version ${String(API_VERSION)}`);
  }
  const lock = object(obj.lock, "lock");
  const reported = str(lock, "state", "lock");
  if (!("job" in obj)) throw new IndexRunFormatError("job is missing");
  return {
    api_version: API_VERSION,
    repository: str(obj, "repository", "index run"),
    lock: {
      state: LOCK_STATES.includes(reported) ? (reported as LockState) : "unknown",
      reported_state: reported,
      error: strOrNull(lock, "error", "lock"),
    },
    job: parseJob(obj.job),
  };
}

function typed(body: unknown): IndexRun {
  try {
    return parseIndexRun(body);
  } catch (err) {
    if (err instanceof IndexRunFormatError) throw new ApiError("format", err.message);
    throw err;
  }
}

/** IndexRunClient reads the run state and starts a run; tests pass their
 * own. */
export interface IndexRunClient {
  fetch: (options: { signal: AbortSignal }) => Promise<IndexRun>;
  /** start POSTs {} and resolves with the 202 document; a refusal (409
   * index_locked, reindex_unsupported, 503, ...) rejects with an ApiError
   * carrying the server's code. */
  start: () => Promise<IndexRun>;
}

/** httpIndexRunClient talks to the server that served the page. */
export const httpIndexRunClient: IndexRunClient = {
  fetch: async ({ signal }) => typed(await fetchJSON(INDEX_RUN_PATH, { signal })),
  start: async () => typed(await fetchJSON(INDEX_RUN_PATH, { method: "POST", body: {} })),
};
