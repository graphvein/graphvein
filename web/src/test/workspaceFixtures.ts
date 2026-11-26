// Workspace documents and clients for the T038 UI tests. The raw shapes
// follow docs/cli/serve.md ("GET /api/v0/workspace", "POST .../check") and
// go through the same parsers as the server's answers.

import { parseAdd, parseCheck, parseEntry, parseWorkspace, type AddResult, type CheckResult, type Workspace, type WorkspaceClient, type WorkspaceEntry } from "@/api/workspace";

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
export function fakeWorkspaceClient(initial: Workspace, opts: { check?: (path: string) => Promise<CheckResult>; add?: (path: string, confirm: string) => Promise<AddResult>; run?: (id: string) => Promise<void> } = {}): WorkspaceClient & { doc: Workspace; calls: string[] } {
  const client = {
    doc: initial,
    calls: [] as string[],
    list: () => {
      client.calls.push("list");
      return Promise.resolve(client.doc);
    },
    check: (path: string, token: string) => {
      client.calls.push(`check ${path} ${token}`);
      return opts.check ? opts.check(path) : Promise.resolve(readyCheck());
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
