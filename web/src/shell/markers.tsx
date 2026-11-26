// The "changed since <base>" markers' data (T019 Part b item 6, Revision 2).
// The compare chip's base drives every view's markers: off when comparing
// is off, and an expired or unknown base (the shell's one summary request
// refused) drops every view's markers together, because each view reads
// the same summary state through useChangeMarkers. The items of a group
// are loaded once per base and index run (every page, test scope
// included, since a marker is matched to what the view shows) and shared
// by the views that read the group.
//
// T019 Revision 5: the loads are keyed by the summary's resolved
// comparison (R5-3), every page is checked against the summary's index run
// (R5-2: a mismatch reloads the summary and reads as loading), and the
// views show markers only when the comparison's head is the current index
// (R5-1): the views draw the current index, so a branch comparison's head
// commit would mark facts the view does not show.

import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";

import type { ChangeItem, ChangesClient, ChangesHeader, ChangesSummary } from "@/api/changes";
import { indexItems, type MarkerIndex } from "@/sections/changes/markers";
import { baseLabel } from "@/sections/changes/words";
import { answerMatches, markersApply, useOptionalCompare } from "@/shell/compare";

/** PAGE_LIMIT is the items endpoint's largest page. */
const PAGE_LIMIT = 500;
/** MAX_PAGES bounds one group's load (10,000 items); a group with more
 * is marked as cut, and its view says so. */
const MAX_PAGES = 20;

type GroupLoad = { state: "loading" } | { state: "ok"; items: ChangeItem[]; truncated: boolean } | { state: "failed"; error: Error } | { state: "stale" };

interface MarkersStore {
  key: string | null;
  loads: Map<string, GroupLoad>;
  request: (key: string, group: string, base: string, summary: ChangesSummary) => void;
}

/** StaleAnswer is a page read from another index run than the summary. */
export class StaleAnswer extends Error {
  readonly answer: ChangesHeader;
  constructor(answer: ChangesHeader) {
    super("the answer was read from another index run than the summary");
    this.name = "StaleAnswer";
    this.answer = answer;
  }
}

const MarkersContext = createContext<MarkersStore | null>(null);

/** loadGroup reads every page of one group's items; with summary, a page
 * from another index run throws StaleAnswer. */
export async function loadGroup(client: ChangesClient, base: string, group: string, signal?: AbortSignal, summary?: ChangesSummary): Promise<{ items: ChangeItem[]; truncated: boolean }> {
  const items: ChangeItem[] = [];
  let cursor: string | null = null;
  let pages = 0;
  do {
    const page = await client.items({ base, group, tests: true, cursor, limit: PAGE_LIMIT }, signal === undefined ? {} : { signal });
    if (summary !== undefined && !answerMatches(summary, page)) throw new StaleAnswer(page);
    items.push(...page.items);
    cursor = page.next_cursor;
    pages++;
  } while (cursor !== null && pages < MAX_PAGES);
  return { items, truncated: cursor !== null };
}

/** MarkersProvider holds the loaded groups; it sits inside CompareProvider. */
export function MarkersProvider({ children }: { children: ReactNode }) {
  const compare = useOptionalCompare();
  const [store, setStore] = useState<{ key: string | null; loads: Map<string, GroupLoad> }>({ key: null, loads: new Map() });
  const client = compare?.client ?? null;
  const reportStale = compare?.reportStale ?? null;
  // The loads started for the current key (a ref: two views asking for the
  // same group in one render must start one load).
  const started = useRef<{ key: string | null; groups: Set<string> }>({ key: null, groups: new Set() });
  const request = useCallback(
    (key: string, group: string, base: string, summary: ChangesSummary) => {
      if (client === null) return;
      if (started.current.key !== key) started.current = { key, groups: new Set() };
      if (started.current.groups.has(group)) return;
      started.current.groups.add(group);
      setStore((s) => {
        const next = new Map(s.key === key ? s.loads : undefined);
        next.set(group, { state: "loading" });
        return { key, loads: next };
      });
      const settle = (load: GroupLoad) => {
        setStore((s) => {
          if (s.key !== key) return s;
          const next = new Map(s.loads);
          next.set(group, load);
          return { key, loads: next };
        });
      };
      loadGroup(client, base, group, undefined, summary).then(
        (r) => {
          settle({ state: "ok", ...r });
        },
        (err: unknown) => {
          if (err instanceof StaleAnswer) {
            settle({ state: "stale" });
            reportStale?.(err.answer);
            return;
          }
          settle({ state: "failed", error: err instanceof Error ? err : new Error(String(err)) });
        },
      );
    },
    [client, reportStale],
  );
  const value = useMemo<MarkersStore>(() => ({ ...store, request }), [store, request]);
  return <MarkersContext.Provider value={value}>{children}</MarkersContext.Provider>;
}

/** MarkersView is what a view draws. */
export type MarkersView =
  /** No markers: comparing is off, nothing is indexed, the base was refused,
   * the comparison's head is not the current index, or the view is outside
   * the providers (component tests). */
  | { kind: "off" }
  | { kind: "loading" }
  | { kind: "failed"; message: string }
  | { kind: "ok"; index: MarkerIndex; items: ChangeItem[]; baseText: string; truncated: boolean };

const OFF: MarkersView = { kind: "off" };

/** useChangeMarkers loads and indexes the items of the given groups for
 * the compare chip's base. Groups the summary lists without items, and
 * groups it reports as not extracted, are not requested. */
export function useChangeMarkers(groups: readonly string[]): MarkersView {
  const store = useContext(MarkersContext);
  const compare = useOptionalCompare();
  const answered = compare !== null && compare.choice.on && compare.ref === null && compare.summary.state.kind === "ok" ? compare.summary.state.data : null;
  // Markers describe the current index only (R5-1).
  const summary = answered !== null && markersApply(answered) ? answered : null;
  const loading = compare !== null && compare.choice.on && compare.ref === null && compare.summary.state.kind === "loading";
  const key = summary === null || compare === null || compare.comparison === null ? null : compare.comparison;
  const base = compare?.choice.base ?? "";
  const wanted = summary === null ? [] : groups.filter((g) => !summary.summary.not_extracted.includes(g) && summary.summary.groups.some((c) => c.group === g && c.items + c.hidden_tests > 0));
  const wantedKey = wanted.join(",");
  const request = store?.request;

  useEffect(() => {
    if (key === null || summary === null || request === undefined || wantedKey === "") return;
    for (const g of wantedKey.split(",")) request(key, g, base, summary);
  }, [key, wantedKey, request, base, summary]);

  const loads = store !== null && store.key === key ? store.loads : null;
  const runs = compare?.bases.state.kind === "ok" ? compare.bases.state.data.runs : [];
  if (store === null || compare === null) return OFF;
  if (loading) return LOADING;
  if (key === null) return OFF;
  if (loads === null) return LOADING;
  return viewOf(loads, wantedKey, baseLabel(compare.choice.base, runs));
}

const LOADING: MarkersView = { kind: "loading" };

/** views caches one view per loads map and request, so the many cards of a
 * graph share one index instead of building their own. */
const views = new WeakMap<Map<string, GroupLoad>, Map<string, MarkersView>>();

function viewOf(loads: Map<string, GroupLoad>, wantedKey: string, baseText: string): MarkersView {
  let byKey = views.get(loads);
  if (byKey === undefined) {
    byKey = new Map();
    views.set(loads, byKey);
  }
  const cacheKey = `${wantedKey}\n${baseText}`;
  const cached = byKey.get(cacheKey);
  if (cached !== undefined) return cached;
  const items: ChangeItem[] = [];
  let truncated = false;
  for (const g of wantedKey === "" ? [] : wantedKey.split(",")) {
    const l = loads.get(g);
    // Not started yet (the effect runs after this render) or loading.
    if (l === undefined || l.state === "loading" || l.state === "stale") return LOADING;
    if (l.state === "failed") return { kind: "failed", message: l.error.message };
    items.push(...l.items);
    truncated = truncated || l.truncated;
  }
  const view: MarkersView = { kind: "ok", index: indexItems(items), items, baseText, truncated };
  byKey.set(cacheKey, view);
  return view;
}
