// Workspace documents and clients for the T038 UI tests. The raw shapes
// follow docs/cli/serve.md ("GET /api/v0/workspace", "POST .../check") and
// go through the same parsers as the server's answers.

import { parseMoveResult, parseStoreCheck, parseStoreSettings, type MoveResult, type StoreCheck, type StoreClient, type StoreSettings } from "@/api/store";
import { parseAdd, parseCheck, parseEntry, parseWorkspace, type AddResult, type CheckResult, type PlaceChoice, type Workspace, type WorkspaceClient, type WorkspaceEntry } from "@/api/workspace";

type Raw = Record<string, unknown>;

const CHECKED = "2026-11-24T10:00:00Z";

/** rawEntry builds one raw entry; patch overrides any field. */
export function rawEntry(patch: Raw & { id: string; name: string }): Raw {
  return {
    kind: "repository",
    path: `/src/${patch.name}`,
    registered: true,
    current: false,
    state: "not_open",
    freshness: null,
    members: null,
    members_error: null,
    job: null,
    conflict: null,
    ...patch,
  };
}

/** rawProject builds a raw project entry with the given members. */
export function rawProject(id: string, name: string, members: string[], patch: Raw = {}): Raw {
  return rawEntry({
    id,
    name,
    kind: "project",
    members: members.map((m) => ({ repository: m, path: `/src/${m}`, checkout: true, freshness: { state: "fresh", checked_at: CHECKED, changed_count: 0 } })),
    ...patch,
  });
}

/** rawJob is a raw running job; patch overrides fields. */
export function rawJob(patch: Raw = {}): Raw {
  return { id: "J1", state: "running", started_at: "2026-11-24T10:01:00Z", ended_at: null, phase: "parsing", progress: { done: 212, total: 412, unit: "files" }, replaces: null, run: null, error: null, ...patch };
}

/** workspaceDoc builds a parsed workspace from raw entries. */
export function workspaceDoc(entries: Raw[], current: string | null = null): Workspace {
  return parseWorkspace({ api_version: 0, registry: { path: "/cfg/workspace.yaml", error: null }, token: "TOKEN", current, suggestion: null, open_limit: 8, entries });
}

export function entry(raw: Raw): WorkspaceEntry {
  return parseEntry(raw);
}

/** SAMPLE is a project with two members and two repositories. */
export function sample(current: string | null = "express~2"): Workspace {
  return workspaceDoc(
    [
      rawProject("shop~1", "shop", ["react", "express"], { freshness: { state: "stale", checked_at: CHECKED, changed_count: 3 } }),
      rawEntry({ id: "express~2", name: "express", current: current === "express~2", state: "open", freshness: { state: "fresh", checked_at: CHECKED, changed_count: 0 } }),
      rawEntry({ id: "gotify~3", name: "gotify" }),
    ],
    current,
  );
}

/** readyCheck is a check answer that can be confirmed. */
export function readyCheck(patch: Raw = {}): CheckResult {
  return parseCheck({
    api_version: 0,
    path: "/src/miniflux",
    resolved: "/src/miniflux",
    outcome: "ready",
    refusal: null,
    kind: "repository",
    name: "miniflux",
    members: null,
    index: { state: "absent", last_run: null, error: null },
    store: { kind: "sqlite", path: "/src/miniflux/.graphvein/index.db", env: null },
    entry: null,
    confirm: "CONFIRM",
    command: "graphvein index '/src/miniflux'",
    ...patch,
  });
}

/** refusedCheck is a check answer refused with code. */
export function refusedCheck(code: string, message: string, entryId: string | null = null): CheckResult {
  return readyCheck({ outcome: "refused", resolved: null, kind: null, name: null, index: null, store: null, confirm: null, command: null, refusal: { row: "WS-x", code, message, entry: entryId } });
}

export function addResult(id: string, name: string): AddResult {
  return parseAdd({ api_version: 0, entry: rawEntry({ id, name, job: rawJob({ phase: "scanning", progress: null }) }), job: rawJob({ phase: "scanning", progress: null }), run_error: null });
}

/** addNotStarted is an add answer whose entry was registered but whose run
 * was refused (the review's input: the index lock held elsewhere). */
export function addNotStarted(id: string, name: string, code = "index_locked", message = "another process holds the index lock for this store (graphvein index or another graphvein serve); try again when it has finished"): AddResult {
  return parseAdd({ api_version: 0, added: true, entry: rawEntry({ id, name }), job: null, run_error: { code, message } });
}

/** fakeWorkspaceClient answers from mutable state the test changes. */
export function fakeWorkspaceClient(initial: Workspace, opts: { check?: (path: string, place?: PlaceChoice) => Promise<CheckResult>; add?: (path: string, confirm: string) => Promise<AddResult>; run?: (id: string) => Promise<void> } = {}): WorkspaceClient & { doc: Workspace; calls: string[] } {
  const client = {
    doc: initial,
    calls: [] as string[],
    list: () => {
      client.calls.push("list");
      return Promise.resolve(client.doc);
    },
    check: (path: string, token: string, place?: PlaceChoice) => {
      client.calls.push(`check ${path} ${token}${place !== undefined ? ` ${place.place}${place.place === "folder" ? `=${place.dir}` : ""}` : ""}`);
      return opts.check ? opts.check(path, place) : Promise.resolve(readyCheck());
    },
    add: (path: string, confirm: string, token: string) => {
      client.calls.push(`add ${path} ${confirm} ${token}`);
      return opts.add ? opts.add(path, confirm) : Promise.resolve(addResult("miniflux~9", "miniflux"));
    },
    run: (id: string) => {
      client.calls.push(`run ${id}`);
      return opts.run ? opts.run(id) : Promise.resolve();
    },
  };
  return client;
}

/** STORES is the per-user stores folder of the storage fixtures. */
export const STORES = "/home/dev/.local/share/graphvein/stores";

/** rawStoreDoc is a raw store document (WS-73) of a new per-user store;
 * patch overrides fields. */
export function rawStoreDoc(patch: Raw = {}): Raw {
  return { kind: "sqlite", path: `${STORES}/miniflux-3f9a1c27/index.db`, env: null, origin: "default", place: "outside", exists: false, size: null, stores_dir: STORES, dir_source: "platform", error_code: null, error: null, ...patch };
}

/** storeCheck is a ready check answer with the store and its places. */
export function storeCheck(patch: Raw = {}): CheckResult {
  return readyCheck({
    store: rawStoreDoc(),
    places: { choosable: true, reason: null, chosen: null, default: `${STORES}/miniflux-3f9a1c27/index.db`, inside: "/src/miniflux/.graphvein/index.db", folder: null },
    ...patch,
  });
}

/** rawStoreEntry is one raw entry of GET /api/v0/store. */
export function rawStoreEntry(patch: Raw = {}): Raw {
  return { id: "miniflux~1", kind: "repository", name: "miniflux", folder: "/src/miniflux", folder_found: true, current: true, store: rawStoreDoc({ exists: true, size: 50544640 }), movable: true, reason: null, ...patch };
}

/** storeSettings is a parsed GET /api/v0/store; patch overrides top-level
 * fields. */
export function storeSettings(patch: Raw = {}): StoreSettings {
  return parseStoreSettings({
    api_version: 0,
    team_mode: { env: "GRAPHVEIN_POSTGRES_URL", set: false },
    sqlite_path: { env: "GRAPHVEIN_SQLITE_PATH", set: false, entry: null },
    default_dir: { path: STORES, source: "platform", env: "GRAPHVEIN_STORE_DIR", editable: true, reason: null, error: null },
    stores_file: { path: "/home/dev/.config/graphvein/stores.yaml", error: null },
    registry: { path: "/home/dev/.config/graphvein/workspace.yaml", error: null },
    entries: [rawStoreEntry()],
    ...patch,
  });
}

/** readyMove is a ready store/check answer for a move of miniflux~1. */
export function readyMove(to = "/data/graphvein/miniflux-3f9a1c27/index.db"): StoreCheck {
  return parseStoreCheck({
    api_version: 0,
    outcome: "ready",
    refusal: null,
    move: { id: "miniflux~1", folder: "/src/miniflux", from: `${STORES}/miniflux-3f9a1c27/index.db`, from_place: "outside", to, to_default: false, size: 50544640 },
    default_dir: null,
    confirm: "MOVE",
    confirm_expires_at: "2026-11-24T10:10:00Z",
    command: "graphvein store move '/src/miniflux' '/data/graphvein'",
  });
}

/** refusedStore is a refused store/check answer. */
export function refusedStore(code: string, message: string): StoreCheck {
  return parseStoreCheck({ api_version: 0, outcome: "refused", refusal: { row: "WS-75", code, message, entry: null }, move: null, default_dir: null, confirm: null, confirm_expires_at: null, command: null });
}

/** fakeStoreClient answers from the given functions and records calls. */
export function fakeStoreClient(
  settings: StoreSettings,
  opts: { checkMove?: (id: string, dir: string) => Promise<StoreCheck>; checkDefault?: (dir: string) => Promise<StoreCheck>; move?: () => Promise<MoveResult> } = {},
): StoreClient & { calls: string[] } {
  const client = {
    calls: [] as string[],
    get: () => {
      client.calls.push("get");
      return Promise.resolve(settings);
    },
    checkMove: (id: string, dir: string, token: string) => {
      client.calls.push(`checkMove ${id} ${dir} ${token}`);
      return opts.checkMove ? opts.checkMove(id, dir) : Promise.resolve(readyMove());
    },
    checkDefault: (dir: string, token: string) => {
      client.calls.push(`checkDefault ${dir} ${token}`);
      return opts.checkDefault
        ? opts.checkDefault(dir)
        : Promise.resolve(parseStoreCheck({ api_version: 0, outcome: "ready", refusal: null, move: null, default_dir: { from: STORES, from_source: "platform", to: dir, to_platform: dir === "" }, confirm: "DEF", confirm_expires_at: null, command: null }));
    },
    move: (id: string, dir: string, confirm: string, token: string) => {
      client.calls.push(`move ${id} ${dir} ${confirm} ${token}`);
      return opts.move
        ? opts.move()
        : Promise.resolve(parseMoveResult({ api_version: 0, id, from: `${STORES}/miniflux-3f9a1c27/index.db`, to: "/data/graphvein/miniflux-3f9a1c27/index.db", bytes: 50544640, runs: 3, deleted: ["index.db", "index.db-lock"], remaining: [], warning: null, recorded: true }));
    },
    setDefault: (dir: string, confirm: string, token: string) => {
      client.calls.push(`setDefault ${dir} ${confirm} ${token}`);
      return Promise.resolve(settings.default_dir);
    },
  };
  return client;
}
