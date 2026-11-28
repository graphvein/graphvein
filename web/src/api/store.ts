// Where this machine keeps indexes (T068 part 3; ADR 0093, ADR 0088
// revision): GET /api/v0/store needs no index, and the guarded POSTs check
// a typed folder, move one entry's index and set the folder new indexes
// go to (docs/cli/serve.md, "Storage"). Field names mirror the JSON.
// Places, origins and codes the UI does not know are kept as the server's
// word (claim row WS-69), never mapped onto a known one.

import { ApiError, fetchJSON } from "@/api/client";
import { API_VERSION } from "@/api/status";

export const STORE_PATH = "/api/v0/store";
export const STORE_CHECK_PATH = "/api/v0/store/check";
export const STORE_MOVE_PATH = "/api/v0/store/move";
export const STORE_DEFAULT_PATH = "/api/v0/store/default";

/** StoreDoc says where one folder's index is or will be kept (WS-73,
 * WS-77): the SQLite file, or Postgres named by its variable, never the
 * URL. origin is the lookup step (postgres, sqlite_path_env, in_folder,
 * stores_file, default), place inside, outside, environment or postgres. */
export interface StoreDoc {
  kind: string;
  path: string | null;
  env: string | null;
  origin: string | null;
  place: string | null;
  exists: boolean;
  /** size is the bytes of index.db and its -wal file; null when unknown. */
  size: number | null;
  stores_dir: string | null;
  dir_source: string | null;
  error_code: string | null;
  error: string | null;
}

/** StorePlaces are the places a ready folder's new index can be kept. */
export interface StorePlaces {
  choosable: boolean;
  reason: string | null;
  chosen: string | null;
  default: string | null;
  inside: string | null;
  folder: string | null;
}

/** StoreEntry is one workspace entry's index in GET /api/v0/store. */
export interface StoreEntry {
  id: string;
  kind: string;
  name: string;
  folder: string;
  folder_found: boolean;
  current: boolean;
  store: StoreDoc | null;
  movable: boolean;
  reason: string | null;
}

/** StoreSettings is GET /api/v0/store. */
export interface StoreSettings {
  team_mode: { env: string; set: boolean };
  sqlite_path: { env: string; set: boolean; entry: string | null };
  default_dir: {
    path: string | null;
    source: string | null;
    env: string;
    editable: boolean;
    reason: string | null;
    error: string | null;
  };
  stores_file: { path: string | null; error: string | null };
  registry: { path: string | null; error: string | null };
  entries: StoreEntry[];
}

export interface StoreRefusal {
  row: string;
  code: string;
  message: string;
  entry: string | null;
}

/** StoreCheck is the answer of POST /api/v0/store/check: a move plan or
 * a new default folder, with a one-time confirm value when ready. */
export interface StoreCheck {
  outcome: string;
  refusal: StoreRefusal | null;
  move: {
    id: string;
    folder: string;
    from: string;
    from_place: string;
    to: string;
    to_default: boolean;
    size: number;
  } | null;
  default_dir: {
    from: string | null;
    from_source: string | null;
    to: string;
    to_platform: boolean;
  } | null;
  confirm: string | null;
  command: string | null;
}

/** MoveResult is the answer of POST /api/v0/store/move. remaining lists
 * the original files that could not be deleted. */
export interface MoveResult {
  id: string;
  from: string;
  to: string;
  bytes: number;
  runs: number;
  deleted: string[];
  remaining: string[];
  warning: string | null;
  recorded: boolean;
}

/** StoreFormatError reports a body without the documented shape. */
export class StoreFormatError extends Error {
  constructor(message: string) {
    super(`malformed store response: ${message}`);
    this.name = "StoreFormatError";
  }
}

type Obj = Record<string, unknown>;

function object(v: unknown, path: string): Obj {
  if (typeof v !== "object" || v === null || Array.isArray(v))
    throw new StoreFormatError(`${path} is not an object`);
  return v as Obj;
}

function str(o: Obj, key: string, path: string): string {
  const v = o[key];
  if (typeof v !== "string")
    throw new StoreFormatError(`${path}.${key} is not a string`);
  return v;
}

function strOrNull(o: Obj, key: string, path: string): string | null {
  const v = o[key];
  if (v === null || v === undefined) return null;
  if (typeof v !== "string")
    throw new StoreFormatError(`${path}.${key} is not a string or null`);
  return v;
}

function bool(o: Obj, key: string, path: string): boolean {
  const v = o[key];
  if (typeof v !== "boolean")
    throw new StoreFormatError(`${path}.${key} is not a boolean`);
  return v;
}

function count(o: Obj, key: string, path: string): number {
  const v = o[key];
  if (typeof v !== "number" || !Number.isInteger(v) || v < 0)
    throw new StoreFormatError(`${path}.${key} is not a count`);
  return v;
}

function strings(o: Obj, key: string, path: string): string[] {
  const v = o[key];
  if (v === null || v === undefined) return [];
  if (!Array.isArray(v) || v.some((s) => typeof s !== "string"))
    throw new StoreFormatError(`${path}.${key} is not a list of strings`);
  return v as string[];
}

function version(o: Obj): void {
  if (o.api_version !== API_VERSION)
    throw new StoreFormatError(
      `api_version is ${String(o.api_version)}; this UI reads version ${String(API_VERSION)}`,
    );
}

/** parseStoreDoc checks one store document; fields an older answer left
 * out read as not known (null, exists false). */
export function parseStoreDoc(v: unknown, path = "store"): StoreDoc | null {
  if (v === null || v === undefined) return null;
  const o = object(v, path);
  const size = o.size;
  if (
    size !== null &&
    size !== undefined &&
    (typeof size !== "number" || size < 0)
  )
    throw new StoreFormatError(`${path}.size is not a size`);
  return {
    kind: str(o, "kind", path),
    path: strOrNull(o, "path", path),
    env: strOrNull(o, "env", path),
    origin: strOrNull(o, "origin", path),
    place: strOrNull(o, "place", path),
    exists: o.exists === true,
    size: typeof size === "number" ? size : null,
    stores_dir: strOrNull(o, "stores_dir", path),
    dir_source: strOrNull(o, "dir_source", path),
    error_code: strOrNull(o, "error_code", path),
    error: strOrNull(o, "error", path),
  };
}

/** parsePlaces checks a check answer's places; null when absent. */
export function parsePlaces(v: unknown): StorePlaces | null {
  if (v === null || v === undefined) return null;
  const o = object(v, "places");
  return {
    choosable: bool(o, "choosable", "places"),
    reason: strOrNull(o, "reason", "places"),
    chosen: strOrNull(o, "chosen", "places"),
    default: strOrNull(o, "default", "places"),
    inside: strOrNull(o, "inside", "places"),
    folder: strOrNull(o, "folder", "places"),
  };
}

function file(
  v: unknown,
  path: string,
): { path: string | null; error: string | null } {
  const o = object(v, path);
  return {
    path: strOrNull(o, "path", path),
    error: strOrNull(o, "error", path),
  };
}

function refusal(v: unknown): StoreRefusal | null {
  if (v === null || v === undefined) return null;
  const r = object(v, "refusal");
  return {
    row: str(r, "row", "refusal"),
    code: str(r, "code", "refusal"),
    message: str(r, "message", "refusal"),
    entry: strOrNull(r, "entry", "refusal"),
  };
}

/** parseStoreSettings checks a decoded GET /api/v0/store body. */
export function parseStoreSettings(v: unknown): StoreSettings {
  const o = object(v, "store");
  version(o);
  const team = object(o.team_mode, "team_mode");
  const sp = object(o.sqlite_path, "sqlite_path");
  const dd = object(o.default_dir, "default_dir");
  if (!Array.isArray(o.entries))
    throw new StoreFormatError("entries is not a list");
  return {
    team_mode: {
      env: str(team, "env", "team_mode"),
      set: bool(team, "set", "team_mode"),
    },
    sqlite_path: {
      env: str(sp, "env", "sqlite_path"),
      set: bool(sp, "set", "sqlite_path"),
      entry: strOrNull(sp, "entry", "sqlite_path"),
    },
    default_dir: {
      path: strOrNull(dd, "path", "default_dir"),
      source: strOrNull(dd, "source", "default_dir"),
      env: str(dd, "env", "default_dir"),
      editable: bool(dd, "editable", "default_dir"),
      reason: strOrNull(dd, "reason", "default_dir"),
      error: strOrNull(dd, "error", "default_dir"),
    },
    stores_file: file(o.stores_file, "stores_file"),
    registry: file(o.registry, "registry"),
    entries: o.entries.map((e, i) => {
      const p = `entries[${String(i)}]`;
      const eo = object(e, p);
      return {
        id: str(eo, "id", p),
        kind: str(eo, "kind", p),
        name: str(eo, "name", p),
        folder: str(eo, "folder", p),
        folder_found: bool(eo, "folder_found", p),
        current: bool(eo, "current", p),
        store: parseStoreDoc(eo.store, `${p}.store`),
        movable: bool(eo, "movable", p),
        reason: strOrNull(eo, "reason", p),
      };
    }),
  };
}

/** parseStoreCheck checks a decoded store/check answer. */
export function parseStoreCheck(v: unknown): StoreCheck {
  const o = object(v, "check");
  version(o);
  let move: StoreCheck["move"] = null;
  if (o.move !== null && o.move !== undefined) {
    const m = object(o.move, "move");
    move = {
      id: str(m, "id", "move"),
      folder: str(m, "folder", "move"),
      from: str(m, "from", "move"),
      from_place: str(m, "from_place", "move"),
      to: str(m, "to", "move"),
      to_default: bool(m, "to_default", "move"),
      size: count(m, "size", "move"),
    };
  }
  let dd: StoreCheck["default_dir"] = null;
  if (o.default_dir !== null && o.default_dir !== undefined) {
    const d = object(o.default_dir, "default_dir");
    dd = {
      from: strOrNull(d, "from", "default_dir"),
      from_source: strOrNull(d, "from_source", "default_dir"),
      to: str(d, "to", "default_dir"),
      to_platform: bool(d, "to_platform", "default_dir"),
    };
  }
  return {
    outcome: str(o, "outcome", "check"),
    refusal: refusal(o.refusal),
    move,
    default_dir: dd,
    confirm: strOrNull(o, "confirm", "check"),
    command: strOrNull(o, "command", "check"),
  };
}

/** parseMoveResult checks a decoded store/move answer. */
export function parseMoveResult(v: unknown): MoveResult {
  const o = object(v, "move");
  version(o);
  return {
    id: str(o, "id", "move"),
    from: str(o, "from", "move"),
    to: str(o, "to", "move"),
    bytes: count(o, "bytes", "move"),
    runs: count(o, "runs", "move"),
    deleted: strings(o, "deleted", "move"),
    remaining: strings(o, "remaining", "move"),
    warning: strOrNull(o, "warning", "move"),
    recorded: o.recorded === true,
  };
}

/** parseDefaultResult reads the new default folder of a store/default
 * answer. */
export function parseDefaultResult(v: unknown): StoreSettings["default_dir"] {
  const o = object(v, "default");
  version(o);
  const dd = object(o.default_dir, "default_dir");
  return {
    path: strOrNull(dd, "path", "default_dir"),
    source: strOrNull(dd, "source", "default_dir"),
    env: str(dd, "env", "default_dir"),
    editable: bool(dd, "editable", "default_dir"),
    reason: strOrNull(dd, "reason", "default_dir"),
    error: strOrNull(dd, "error", "default_dir"),
  };
}

function typed<T>(parse: (v: unknown) => T, body: unknown): T {
  try {
    return parse(body);
  } catch (err) {
    if (err instanceof StoreFormatError)
      throw new ApiError("format", err.message);
    throw err;
  }
}

/** StoreClient is every storage request; tests pass their own. token is
 * the workspace session token every POST needs. */
export interface StoreClient {
  get: (options: { signal?: AbortSignal }) => Promise<StoreSettings>;
  checkMove: (
    id: string,
    destDir: string,
    token: string,
  ) => Promise<StoreCheck>;
  checkDefault: (dir: string, token: string) => Promise<StoreCheck>;
  move: (
    id: string,
    destDir: string,
    confirm: string,
    token: string,
  ) => Promise<MoveResult>;
  setDefault: (
    dir: string,
    confirm: string,
    token: string,
  ) => Promise<StoreSettings["default_dir"]>;
}

const TOKEN_HEADER = "X-Graphvein-Token";

/** httpStoreClient talks to the server that served the page. A move
 * waits for the index lock and copies the store: it gets ten minutes. */
export const httpStoreClient: StoreClient = {
  get: async ({ signal }) =>
    typed(
      parseStoreSettings,
      await fetchJSON(STORE_PATH, signal ? { signal } : {}),
    ),
  checkMove: async (id, destDir, token) =>
    typed(
      parseStoreCheck,
      await fetchJSON(STORE_CHECK_PATH, {
        method: "POST",
        body: { id, dest_dir: destDir },
        headers: { [TOKEN_HEADER]: token },
      }),
    ),
  checkDefault: async (dir, token) =>
    typed(
      parseStoreCheck,
      await fetchJSON(STORE_CHECK_PATH, {
        method: "POST",
        body: { default_dir: dir },
        headers: { [TOKEN_HEADER]: token },
      }),
    ),
  move: async (id, destDir, confirm, token) =>
    typed(
      parseMoveResult,
      await fetchJSON(STORE_MOVE_PATH, {
        method: "POST",
        body: { id, dest_dir: destDir, confirm },
        headers: { [TOKEN_HEADER]: token },
        timeoutMs: 600_000,
      }),
    ),
  setDefault: async (dir, confirm, token) =>
    typed(
      parseDefaultResult,
      await fetchJSON(STORE_DEFAULT_PATH, {
        method: "POST",
        body: { default_dir: dir, confirm },
        headers: { [TOKEN_HEADER]: token },
      }),
    ),
};

/** storePlaceWords words a store's place as the chip shows it: where it
 * is relative to the folder, then which lookup step chose it. An unknown
 * word is kept. */
export function storePlaceWords(store: StoreDoc): string {
  if (store.kind === "postgres" || store.place === "postgres")
    return "Postgres · team mode";
  if (store.place === "environment") return "environment · this run only";
  if (store.place === "inside") return "inside the folder";
  if (store.place === "outside") {
    if (store.origin === "default")
      return store.dir_source === "env"
        ? "outside the folder · GRAPHVEIN_STORE_DIR"
        : "outside the folder · default";
    if (store.origin === "stores_file") return "outside the folder · chosen";
    return "outside the folder";
  }
  return store.place ?? "place not known";
}

/** formatBytes is a size in the units people read (1 decimal from MB). */
export function formatBytes(n: number): string {
  if (n < 1024) return `${String(n)} B`;
  if (n < 1024 * 1024) return `${String(Math.round(n / 1024))} KB`;
  if (n < 1024 * 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(1)} MB`;
  return `${(n / (1024 * 1024 * 1024)).toFixed(1)} GB`;
}

/** dirSourceWords is the chip of the folder new indexes go to. */
export function dirSourceWords(source: string | null): string {
  if (source === "platform") return "Built-in default";
  if (source === "env") return "environment";
  if (source === "stores_file") return "stores.yaml";
  return source ?? "not known";
}
