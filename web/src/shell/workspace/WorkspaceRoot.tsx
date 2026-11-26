// The workspace root (T038, variant A): one server serves every index of
// the workspace registry, and the page views one entry at a time, named
// by `?ws=<entry id>` in its URL (location.ts). The root lists the
// workspace (GET /api/v0/workspace), picks the viewed entry, names it on
// every request (api/client.ts withWorkspace) and remounts the shell
// below it when it changes, so no section shows one index's facts under
// another's name (claim row WS-48). It also owns the view memory
// (memory.ts) and the add-a-folder flow, whose dialog outlives a switch.
//
// A server without the workspace endpoint (an older binary) renders the
// shell exactly as before.

import { createContext, useCallback, useContext, useEffect, useLayoutEffect, useMemo, useState, type ReactNode } from "react";

import { ApiError, setRequestWorkspace } from "@/api/client";
import { httpWorkspaceClient, type Workspace, type WorkspaceClient, type WorkspaceEntry } from "@/api/workspace";
import { DEFAULT_HASH, useHash, useLocationKey } from "@/router";
import { readLocation, writeLocation } from "@/shell/workspace/location";
import { readScroll, readView, restoreScroll, saveView } from "@/shell/workspace/memory";

/** LIST_POLL_MS is the list's cadence while nothing runs; RUN_POLL_MS
 * while an entry's job runs or an add is waiting for its job. */
export const LIST_POLL_MS = 10_000;
export const RUN_POLL_MS = 1_000;

/** AddRequest opens the add dialog, optionally at a typed path already
 * checked (the first-run card). */
export interface AddRequest {
  path?: string;
}

/** WorkspaceState is what the sidebar, the top bar and the add flow read. */
export interface WorkspaceState {
  workspace: Workspace;
  /** The viewed entry; null in the first-run or choose state. */
  entry: WorkspaceEntry | null;
  /** An entry id from the URL that this server does not have (WS-47). */
  unknownLink: string | null;
  dismissUnknown: () => void;
  /** switchTo views another entry (and project member), remembering the
   * current view and restoring the target's. */
  switchTo: (id: string, member?: string | null) => void;
  /** refresh asks for the list now. */
  refresh: () => void;
  /** watch makes the list poll fast until the entry's job ended (an add
   * that just started). */
  watch: (id: string) => void;
  client: WorkspaceClient;
  /** The add dialog: openAdd shows it; addRequest is non-null while open. */
  addRequest: AddRequest | null;
  openAdd: (req?: AddRequest) => void;
  closeAdd: () => void;
}

const WorkspaceContext = createContext<WorkspaceState | null>(null);

/** useWorkspace is the workspace state, or null on a server without a
 * workspace list (and in component tests of the shell). */
export function useWorkspace(): WorkspaceState | null {
  return useContext(WorkspaceContext);
}

/** WorkspaceProvider provides a state directly (tests). */
export function WorkspaceProvider({ value, children }: { value: WorkspaceState; children: ReactNode }) {
  return <WorkspaceContext.Provider value={value}>{children}</WorkspaceContext.Provider>;
}

function running(ws: Workspace): boolean {
  return ws.entries.some((e) => e.job !== null && e.job.ended_at === null && (e.job.state === "running" || e.job.state === "unknown"));
}

// The scroll positions a switch restores once the target's shell mounted;
// module state, as the page has one workspace root.
let pendingScroll: Record<string, number> | null = null;

type Probe = { kind: "loading" } | { kind: "legacy" } | { kind: "ok"; workspace: Workspace };

/** pickEntry is the viewed entry for a URL id: the named entry, else the
 * server's current entry; unknown is the URL id this server does not
 * have. */
export function pickEntry(ws: Workspace, urlId: string | null): { entry: WorkspaceEntry | null; unknown: string | null } {
  const named = urlId === null ? undefined : ws.entries.find((e) => e.id === urlId);
  if (named !== undefined) return { entry: named, unknown: null };
  const current = ws.current === null ? undefined : ws.entries.find((e) => e.id === ws.current);
  return { entry: current ?? null, unknown: urlId };
}

/** WorkspaceRoot is the page. shell renders the viewed entry (null: no
 * workspace list, the shell as before); empty renders the first-run or
 * choose state; overlay (the add dialog) renders beside either and
 * outlives a switch. */
export function WorkspaceRoot({ client = httpWorkspaceClient, shell, empty, overlay }: { client?: WorkspaceClient; shell: (entry: WorkspaceEntry | null) => ReactNode; empty: () => ReactNode; overlay?: ReactNode }) {
  const [probe, setProbe] = useState<Probe>({ kind: "loading" });
  const [tick, setTick] = useState(0);
  const [watched, setWatched] = useState<string | null>(null);
  const [addRequest, setAddRequest] = useState<AddRequest | null>(null);
  // The id of an opened link this server does not have, kept after the
  // URL is rewritten to the entry shown instead (WS-47).
  const [unknownLink, setUnknownLink] = useState<string | null>(null);
  // The location key, not the hash alone: a switch between two entries
  // on the same section changes only the page query.
  useLocationKey();
  const hash = useHash();
  const loc = readLocation();

  const ws = probe.kind === "ok" ? probe.workspace : null;
  const fast = ws !== null && (running(ws) || (watched !== null && !ws.entries.some((e) => e.id === watched && e.job !== null && e.job.ended_at !== null)));

  useEffect(() => {
    const controller = new AbortController();
    client
      .list({ signal: controller.signal })
      .then((w) => {
        setProbe({ kind: "ok", workspace: w });
      })
      .catch((err: unknown) => {
        if (controller.signal.aborted) return;
        // 404: a server without the workspace (an older binary). Any other
        // failure keeps the last list; before the first one the shell runs
        // as before and reports the failure itself.
        setProbe((p) => (p.kind === "ok" ? p : err instanceof ApiError && err.status === 404 ? { kind: "legacy" } : p.kind === "loading" ? { kind: "legacy" } : p));
      });
    return () => {
      controller.abort();
    };
  }, [client, tick]);

  useEffect(() => {
    if (probe.kind !== "ok") return;
    const timer = window.setTimeout(() => {
      if (document.visibilityState === "visible") setTick((t) => t + 1);
      else setTick((t) => t);
    }, fast ? RUN_POLL_MS : LIST_POLL_MS);
    return () => {
      window.clearTimeout(timer);
    };
  }, [probe, fast]);

  useEffect(() => {
    const onVisible = () => {
      if (document.visibilityState === "visible") setTick((t) => t + 1);
    };
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, []);

  const picked = ws === null ? { entry: null, unknown: null } : pickEntry(ws, loc.ws);
  const entry = picked.entry;
  const entryId = entry?.id ?? null;
  const unknown = picked.unknown;

  // The URL names the viewed entry (a page opened without ws=, or with an
  // id this server does not have, gets the current one), so a copied link
  // and the view memory hold it. An unknown id is said first (WS-47).
  // The unknown id is kept in state (set while rendering, the React
  // pattern for state derived from a changed input), because the rewrite
  // below removes it from the URL.
  const [seenUnknown, setSeenUnknown] = useState<string | null>(null);
  if (unknown !== null && unknown !== seenUnknown) {
    setSeenUnknown(unknown);
    setUnknownLink(unknown);
  }
  // A layout effect, not a passive one: the URL is written in the same
  // commit that first shows the entry, before the browser paints it, so
  // there is no moment where the page shows an entry its URL (and a
  // copied link) does not name.
  useLayoutEffect(() => {
    if (entryId === null || loc.ws === entryId) return;
    // The section of a link to another index is kept, never its selection:
    // an item id of one index means nothing in another.
    const hash = window.location.hash || DEFAULT_HASH;
    writeLocation({ ws: entryId, member: unknown !== null ? null : loc.member }, unknown !== null ? (hash.split("?")[0] ?? DEFAULT_HASH) : hash, "replace");
  }, [entryId, loc.ws, loc.member, unknown]);

  // Remember the view as it changes (scroll positions at a switch and when
  // the page is left).
  useEffect(() => {
    if (entryId === null || loc.ws !== entryId) return;
    saveView(entryId, { hash, member: loc.member });
  }, [entryId, hash, loc.ws, loc.member]);
  useEffect(() => {
    if (entryId === null) return;
    const onHide = () => {
      saveView(entryId, { hash: window.location.hash, member: readLocation().member, scroll: readScroll() });
    };
    window.addEventListener("pagehide", onHide);
    return () => {
      window.removeEventListener("pagehide", onHide);
    };
  }, [entryId]);

  useEffect(() => {
    const want = pendingScroll;
    pendingScroll = null;
    if (want === null) return;
    return restoreScroll(want);
  }, [entryId]);

  const switchTo = useCallback(
    (id: string, member: string | null = null) => {
      const from = readLocation();
      if (from.ws === id && (member === null || member === from.member)) return;
      if (from.ws !== null) saveView(from.ws, { hash: window.location.hash, member: from.member, scroll: readScroll() });
      const memory = readView(id);
      const target = member ?? memory?.member ?? null;
      // A member chosen in the list opens that member's remembered view
      // only when the memory is of the same member.
      const sameView = memory !== null && (member === null || memory.member === member);
      pendingScroll = sameView ? memory.scroll : null;
      writeLocation({ ws: id, member: target }, sameView ? memory.hash : DEFAULT_HASH, "push");
    },
    [],
  );
  const refresh = useCallback(() => {
    setTick((t) => t + 1);
  }, []);
  const watch = useCallback((id: string) => {
    setWatched(id);
    setTick((t) => t + 1);
  }, []);
  const openAdd = useCallback((req: AddRequest = {}) => {
    setAddRequest(req);
  }, []);
  const closeAdd = useCallback(() => {
    setAddRequest(null);
  }, []);
  const dismissUnknown = useCallback(() => {
    setUnknownLink(null);
  }, []);

  const value = useMemo<WorkspaceState | null>(
    () => (ws === null ? null : { workspace: ws, entry, unknownLink, dismissUnknown, switchTo, refresh, watch, client, addRequest, openAdd, closeAdd }),
    [ws, entry, unknownLink, dismissUnknown, switchTo, refresh, watch, client, addRequest, openAdd, closeAdd],
  );

  if (probe.kind === "loading") return <div aria-busy="true" className="h-screen bg-background" />;
  if (probe.kind === "legacy" || ws === null || value === null) {
    setRequestWorkspace(null);
    return <>{shell(null)}</>;
  }
  // Every request below names the viewed entry (api/client.ts).
  setRequestWorkspace(entryId);
  return (
    <WorkspaceContext.Provider value={value}>
      {entry === null ? empty() : shell(entry)}
      {overlay}
    </WorkspaceContext.Provider>
  );
}
