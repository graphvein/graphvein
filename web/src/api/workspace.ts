// The workspace of a multi-index server (T038, ADR 0087): GET
// /api/v0/workspace lists every registered index, and the typed-path POSTs
// check, add and remove a folder (ADR 0041 revision 1). Field names mirror
// the JSON (docs/cli/serve.md, "Workspace"). States, kinds and refusal
// codes the UI does not know are kept as the server's word (claim row
// WS-69), never mapped onto a known one.

import { ApiError, fetchJSON } from "@/api/client";
import { parseJob, type Job } from "@/api/indexRun";
import { API_VERSION } from "@/api/status";

export const WORKSPACE_PATH = "/api/v0/workspace";
export const WORKSPACE_CHECK_PATH = "/api/v0/workspace/check";
export const WORKSPACE_ADD_PATH = "/api/v0/workspace/add";
export const WORKSPACE_REMOVE_PATH = "/api/v0/workspace/remove";

/** Freshness is a recorded check of an entry or a member; null in the
 * documents until one finished (WS-9). */
export interface Freshness {
  state: string;
  checked_at: string;
  changed_count: number | null;
  error: string | null;
}

export interface WorkspaceMember {
  repository: string;
  path: string | null;
  checkout: boolean;
  freshness: Freshness | null;
}

/** StoreConflict says an entry uses a repository or project name that
 * another entry, in another folder, uses in the one store every entry
 * shares (WS-57, WS-58): its index is not answered. entry, entry_name and
 * entry_path are the other entry. */
export interface StoreConflict {
  code: string;
  message: string;
  kind: string;
  name: string;
  entry: string;
  entry_name: string;
  entry_path: string;
}

/** WorkspaceEntry is one registered index. kind is "repository" or
 * "project"; state "open", "not_open" or "folder_gone" (any other word is
 * kept). */
export interface WorkspaceEntry {
  id: string;
  kind: string;
  name: string;
  path: string;
  registered: boolean;
  current: boolean;
  state: string;
  freshness: Freshness | null;
  members: WorkspaceMember[] | null;
  members_error: string | null;
  job: Job | null;
  conflict: StoreConflict | null;
}

export interface Workspace {
  registry: { path: string; error: string | null };
  token: string;
  current: string | null;
  suggestion: { path: string; kind: string } | null;
  open_limit: number;
  entries: WorkspaceEntry[];
}

/** WorkspaceFormatError reports a body without the documented shape. */
export class WorkspaceFormatError extends Error {
  constructor(message: string) {
    super(`malformed workspace response: ${message}`);
    this.name = "WorkspaceFormatError";
  }
}

type Obj = Record<string, unknown>;

function object(v: unknown, path: string): Obj {
  if (typeof v !== "object" || v === null || Array.isArray(v)) throw new WorkspaceFormatError(`${path} is not an object`);
  return v as Obj;
}

function str(o: Obj, key: string, path: string): string {
  const v = o[key];
  if (typeof v !== "string") throw new WorkspaceFormatError(`${path}.${key} is not a string`);
  return v;
}

function strOrNull(o: Obj, key: string, path: string): string | null {
  const v = o[key];
  if (v === null || v === undefined) return null;
  if (typeof v !== "string") throw new WorkspaceFormatError(`${path}.${key} is not a string or null`);
  return v;
}

function bool(o: Obj, key: string, path: string): boolean {
  const v = o[key];
  if (typeof v !== "boolean") throw new WorkspaceFormatError(`${path}.${key} is not a boolean`);
  return v;
}

function version(o: Obj): void {
  if (o.api_version !== API_VERSION) throw new WorkspaceFormatError(`api_version is ${String(o.api_version)}; this UI reads version ${String(API_VERSION)}`);
}

function freshness(v: unknown, path: string): Freshness | null {
  if (v === null || v === undefined) return null;
  const o = object(v, path);
  const c = o.changed_count;
  if (c !== null && c !== undefined && (typeof c !== "number" || !Number.isInteger(c) || c < 0)) throw new WorkspaceFormatError(`${path}.changed_count is not a count`);
  return { state: str(o, "state", path), checked_at: str(o, "checked_at", path), changed_count: typeof c === "number" ? c : null, error: strOrNull(o, "error", path) };
}

function job(v: unknown, path: string): Job | null {
  try {
    return parseJob(v ?? null);
  } catch (err) {
    throw new WorkspaceFormatError(`${path}: ${err instanceof Error ? err.message : String(err)}`);
  }
}

function conflict(v: unknown, path: string): StoreConflict | null {
  if (v === null || v === undefined) return null;
  const o = object(v, path);
  return {
    code: str(o, "code", path),
    message: str(o, "message", path),
    kind: str(o, "kind", path),
    name: str(o, "name", path),
    entry: str(o, "entry", path),
    entry_name: str(o, "entry_name", path),
    entry_path: str(o, "entry_path", path),
  };
}

function member(v: unknown, path: string): WorkspaceMember {
  const o = object(v, path);
  return { repository: str(o, "repository", path), path: strOrNull(o, "path", path), checkout: bool(o, "checkout", path), freshness: freshness(o.freshness, `${path}.freshness`) };
}

/** parseEntry checks one entry (of the list or of an add answer). */
export function parseEntry(v: unknown, path = "entry"): WorkspaceEntry {
  const o = object(v, path);
  let members: WorkspaceMember[] | null = null;
  if (o.members !== null && o.members !== undefined) {
    if (!Array.isArray(o.members)) throw new WorkspaceFormatError(`${path}.members is not a list`);
    members = o.members.map((m, i) => member(m, `${path}.members[${String(i)}]`));
  }
  return {
    id: str(o, "id", path),
    kind: str(o, "kind", path),
    name: str(o, "name", path),
    path: str(o, "path", path),
    registered: bool(o, "registered", path),
    current: bool(o, "current", path),
    state: str(o, "state", path),
    freshness: freshness(o.freshness, `${path}.freshness`),
    members,
    members_error: strOrNull(o, "members_error", path),
    job: job(o.job, `${path}.job`),
    conflict: conflict(o.conflict, `${path}.conflict`),
  };
}

/** parseWorkspace checks a decoded GET /api/v0/workspace body. */
export function parseWorkspace(v: unknown): Workspace {
  const o = object(v, "workspace");
  version(o);
  const reg = object(o.registry, "registry");
  if (!Array.isArray(o.entries)) throw new WorkspaceFormatError("entries is not a list");
  let suggestion: Workspace["suggestion"] = null;
  if (o.suggestion !== null && o.suggestion !== undefined) {
    const s = object(o.suggestion, "suggestion");
    suggestion = { path: str(s, "path", "suggestion"), kind: str(s, "kind", "suggestion") };
  }
  const limit = o.open_limit;
  return {
    registry: { path: str(reg, "path", "registry"), error: strOrNull(reg, "error", "registry") },
    token: str(o, "token", "workspace"),
    current: strOrNull(o, "current", "workspace"),
    suggestion,
    open_limit: typeof limit === "number" ? limit : 0,
    entries: o.entries.map((e, i) => parseEntry(e, `entries[${String(i)}]`)),
  };
}

/** Refusal is why a check refused a path (row, code, message and, for
 * inside_registered, the entry). */
export interface Refusal {
  row: string;
  code: string;
  message: string;
  entry: string | null;
}

export interface CheckMember {
  repository: string;
  path: string | null;
  checkout: boolean;
}

/** CheckResult is the answer of POST /api/v0/workspace/check. outcome is
 * "ready", "registered" or "refused" (any other word is kept). */
export interface CheckResult {
  path: string;
  resolved: string | null;
  outcome: string;
  refusal: Refusal | null;
  kind: string | null;
  name: string | null;
  members: CheckMember[] | null;
  index: { state: string; last_run: string | null; error: string | null } | null;
  store: { kind: string; path: string | null; env: string | null } | null;
  entry: string | null;
  confirm: string | null;
  command: string | null;
  /** contains lists the registered entries inside a ready folder (WS-54);
   * empty when none or not reported. */
  contains: ContainedEntry[];
}

/** ContainedEntry is a registered entry inside a checked folder. */
export interface ContainedEntry {
  id: string;
  kind: string;
  name: string;
  path: string;
}

/** parseCheck checks a decoded check answer. */
export function parseCheck(v: unknown): CheckResult {
  const o = object(v, "check");
  version(o);
  let refusal: Refusal | null = null;
  if (o.refusal !== null && o.refusal !== undefined) {
    const r = object(o.refusal, "refusal");
    refusal = { row: str(r, "row", "refusal"), code: str(r, "code", "refusal"), message: str(r, "message", "refusal"), entry: strOrNull(r, "entry", "refusal") };
  }
  let members: CheckMember[] | null = null;
  if (o.members !== null && o.members !== undefined) {
    if (!Array.isArray(o.members)) throw new WorkspaceFormatError("members is not a list");
    members = o.members.map((m, i) => {
      const mo = object(m, `members[${String(i)}]`);
      return { repository: str(mo, "repository", "member"), path: strOrNull(mo, "path", "member"), checkout: bool(mo, "checkout", "member") };
    });
  }
  let index: CheckResult["index"] = null;
  if (o.index !== null && o.index !== undefined) {
    const x = object(o.index, "index");
    index = { state: str(x, "state", "index"), last_run: strOrNull(x, "last_run", "index"), error: strOrNull(x, "error", "index") };
  }
  let contains: ContainedEntry[] = [];
  if (o.contains !== null && o.contains !== undefined) {
    if (!Array.isArray(o.contains)) throw new WorkspaceFormatError("contains is not a list");
    contains = o.contains.map((c, i) => {
      const co = object(c, `contains[${String(i)}]`);
      return { id: str(co, "id", "contains"), kind: str(co, "kind", "contains"), name: str(co, "name", "contains"), path: str(co, "path", "contains") };
    });
  }
  let store: CheckResult["store"] = null;
  if (o.store !== null && o.store !== undefined) {
    const s = object(o.store, "store");
    store = { kind: str(s, "kind", "store"), path: strOrNull(s, "path", "store"), env: strOrNull(s, "env", "store") };
  }
  return {
    path: str(o, "path", "check"),
    resolved: strOrNull(o, "resolved", "check"),
    outcome: str(o, "outcome", "check"),
    refusal,
    kind: strOrNull(o, "kind", "check"),
    name: strOrNull(o, "name", "check"),
    members,
    index,
    store,
    entry: strOrNull(o, "entry", "check"),
    confirm: strOrNull(o, "confirm", "check"),
    command: strOrNull(o, "command", "check"),
    contains,
  };
}

/** AddResult is the answer of POST /api/v0/workspace/add. runError is
 * set when the entry was registered but its run did not start (WS-36,
 * WS-56): the folder is in the workspace, not indexed. */
export interface AddResult {
  entry: WorkspaceEntry;
  job: Job | null;
  runError: { code: string; message: string } | null;
}

export function parseAdd(v: unknown): AddResult {
  const o = object(v, "add");
  version(o);
  let runError: AddResult["runError"] = null;
  if (o.run_error !== null && o.run_error !== undefined) {
    const r = object(o.run_error, "run_error");
    runError = { code: str(r, "code", "run_error"), message: str(r, "message", "run_error") };
  }
  return { entry: parseEntry(o.entry), job: job(o.job, "job"), runError };
}

function typed<T>(parse: (v: unknown) => T, body: unknown): T {
  try {
    return parse(body);
  } catch (err) {
    if (err instanceof WorkspaceFormatError) throw new ApiError("format", err.message);
    throw err;
  }
}

/** WorkspaceClient is every workspace request; tests pass their own. */
export interface WorkspaceClient {
  list: (options: { signal?: AbortSignal }) => Promise<Workspace>;
  check: (path: string, token: string) => Promise<CheckResult>;
  add: (path: string, confirm: string, token: string) => Promise<AddResult>;
  /** run starts an entry's index run (POST /api/v0/index/run?ws=), which
   * creates its store when it has none (WS-15, WS-55). */
  run: (id: string) => Promise<void>;
}

const TOKEN_HEADER = "X-Graphvein-Token";

/** httpWorkspaceClient talks to the server that served the page. */
export const httpWorkspaceClient: WorkspaceClient = {
  list: async ({ signal }) => typed(parseWorkspace, await fetchJSON(WORKSPACE_PATH, signal ? { signal } : {})),
  check: async (path, token) => typed(parseCheck, await fetchJSON(WORKSPACE_CHECK_PATH, { method: "POST", body: { path }, headers: { [TOKEN_HEADER]: token } })),
  add: async (path, confirm, token) => typed(parseAdd, await fetchJSON(WORKSPACE_ADD_PATH, { method: "POST", body: { path, confirm }, headers: { [TOKEN_HEADER]: token }, timeoutMs: 60_000 })),
  run: async (id) => {
    await fetchJSON(`/api/v0/index/run?ws=${encodeURIComponent(id)}`, { method: "POST", body: {}, timeoutMs: 60_000 });
  },
};
