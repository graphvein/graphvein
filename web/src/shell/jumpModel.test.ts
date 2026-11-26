import { describe, expect, it } from "vitest";

import { parseSearch, SEARCH_KINDS, searchPath, type SearchAnswer, type SearchItem, type SearchKind } from "@/api/search";
import { addRecent, buildGroups, chipOrder, parseRecent, RECENT_MAX, recentForIndex, recentKey, servedIndex, flatRows, highlight, matchWorkspace, move, nextGroup, NO_EDITOR_LINK, NO_OPERATION_LINE, NO_ROUTE_EDITOR_LINK, NO_TABLE_EDITOR_LINK, noResultText, targetOf, type JumpRow, type WorkspaceEntry } from "@/shell/jumpModel";

function item(kind: SearchKind, label: string, extra: Partial<SearchItem> = {}): SearchItem {
  return {
    kind,
    repository: "app",
    id: `${kind}:${label}`,
    label,
    tier: "prefix",
    match_key: "name",
    file: "src/app.ts",
    line: 3,
    ide: null,
    method: null,
    path: null,
    prefix_unknown: null,
    test: null,
    node_kind: null,
    qualified_name: null,
    source: null,
    language: null,
    ...extra,
  };
}

function answer(groups: [SearchKind, SearchItem[], number?][], project: string | null = null): SearchAnswer {
  const totals: Partial<Record<SearchKind, number>> = {};
  for (const k of SEARCH_KINDS) totals[k] = 0;
  for (const [k, items, total] of groups) totals[k] = total ?? items.length;
  return {
    project,
    query: "fav",
    kinds: [...SEARCH_KINDS],
    members: [{ repository: "app", checkout: true, ide: { available: true, error: null }, searched: { indexed: true, files: 26, files_not_read: 2, scanner_gaps: 1 }, totals }],
    totals,
    groups: groups.map(([kind, items, total]) => ({ kind, available: true, unavailable: [], total: total ?? items.length, best_tier: "prefix", truncated: (total ?? items.length) > items.length, items })),
  };
}

const WS: WorkspaceEntry[] = [
  { id: "shop~1", kind: "project", name: "shop", path: "/src/shop", state: "not_open" },
  { id: "api~2", kind: "repository", name: "api", path: "/src/api", state: "open" },
  { id: "gone~3", kind: "repository", name: "gone", path: "/src/gone", state: "folder_gone" },
];

describe("jump-to model", () => {
  it("orders the kind chips by the fixed kind order, whatever the results", () => {
    expect(chipOrder(["file", "route", "symbol", "table", "operation"])).toEqual(["all", "route", "operation", "table", "symbol", "file"]);
    // Kinds the server did not answer get no chip.
    expect(chipOrder(["symbol", "route"])).toEqual(["all", "route", "symbol"]);
  });

  it("keeps the server's group order and leaves empty kinds out", () => {
    const a = answer([
      ["symbol", [item("symbol", "favorite")]],
      ["route", [item("route", "/favorites")]],
      ["table", []],
    ]);
    const groups = buildGroups(a, "all", []);
    expect(groups.map((g) => g.kind)).toEqual(["symbol", "route"]);
    expect(groups[0]?.label).toBe("Symbols");
    // One kind chip filters the groups.
    expect(buildGroups(a, "route", WS).map((g) => g.kind)).toEqual(["route"]);
  });

  it("lists workspace entries after the results, never the viewed or a gone one", () => {
    const entries = matchWorkspace(WS, "api~2", "");
    expect(entries.map((e) => e.id)).toEqual(["shop~1"]);
    expect(matchWorkspace(WS, null, "SH").map((e) => e.name)).toEqual(["shop"]);
    const groups = buildGroups(answer([["route", [item("route", "/shop")]]]), "all", entries);
    expect(groups.map((g) => g.kind)).toEqual(["route", "workspace"]);
  });

  it("moves with the arrows, clamped, and Tab jumps to the next kind", () => {
    const groups = buildGroups(answer([["route", [item("route", "/a"), item("route", "/b")]], ["symbol", [item("symbol", "c")]], ["file", [item("file", "d"), item("file", "e")]]]), "all", []);
    expect(flatRows(groups)).toHaveLength(5);
    expect(move(-1, 1, 5)).toBe(0);
    expect(move(-1, -1, 5)).toBe(4);
    expect(move(4, 1, 5)).toBe(4);
    expect(move(0, -1, 5)).toBe(0);
    expect(move(0, 1, 0)).toBe(-1);
    expect(nextGroup(groups, 0, 1)).toBe(2);
    expect(nextGroup(groups, 1, 1)).toBe(2);
    expect(nextGroup(groups, 2, 1)).toBe(3);
    expect(nextGroup(groups, 4, 1)).toBe(0);
    expect(nextGroup(groups, 0, -1)).toBe(3);
  });

  it("opens routes, operations and tables in their section, files and symbols in the editor", () => {
    const ide = { link: "vscode://file/x:3:1", line: 3, column: 1, exact: true, line_only: false, caveat: null };
    const row = (it: SearchItem): JumpRow => ({ type: "item", key: "k", item: it });
    expect(targetOf(row(item("route", "/a", { id: "route:a#GET%20%2Fa", test: true })))).toEqual({ kind: "hash", hash: "#/api?tests=1&sel=route%3Aa%23GET%2520%252Fa", repository: "app" });
    expect(targetOf(row(item("operation", "/a", { id: "route:a" })))).toEqual({ kind: "hash", hash: "#/explorer?route=route%3Aa", repository: "app" });
    expect(targetOf(row(item("table", "users", { id: "table:m#users" })))).toEqual({ kind: "hash", hash: "#/schema?sel=table%3Am%23users", repository: "app" });
    expect(targetOf(row(item("symbol", "f", { ide })))).toEqual({ kind: "editor", link: ide.link, repository: "app" });
    expect(targetOf(row(item("file", "f")))).toEqual({ kind: "none", reason: NO_EDITOR_LINK });
    // Ctrl+Enter opens a route's code in the editor when it has a link.
    expect(targetOf(row(item("route", "/a", { ide })), true)).toEqual({ kind: "editor", link: ide.link, repository: "app" });
    // B4: Ctrl+Enter on an operation says why, it does not open silently.
    expect(targetOf(row(item("operation", "/a", { id: "route:a" })), true)).toEqual({ kind: "none", reason: NO_OPERATION_LINE });
    // JX-F2: a route or table without an editor link says so the same way.
    expect(targetOf(row(item("route", "/a", { id: "route:a" })), true)).toEqual({ kind: "none", reason: NO_ROUTE_EDITOR_LINK });
    expect(targetOf(row(item("table", "users", { id: "table:m#users" })), true)).toEqual({ kind: "none", reason: NO_TABLE_EDITOR_LINK });
    expect(targetOf({ type: "workspace", key: "w", entry: WS[0] as WorkspaceEntry })).toEqual({ kind: "workspace", id: "shop~1" });
  });

  it("highlights the first folded match only", () => {
    expect(highlight("ApiFavoriteArticle", "favorite")).toEqual([
      { text: "Api", match: false },
      { text: "Favorite", match: true },
      { text: "Article", match: false },
    ]);
    expect(highlight("abc", "zz")).toEqual([{ text: "abc", match: false }]);
  });

  it("words no result as not proof of absence, with what was not searched", () => {
    const text = noResultText(answer([]));
    expect(text).toMatch(/^Not proof that it does not exist/);
    expect(text).toContain("2 files Graphvein could not read");
    expect(text).toContain("1 path the scanner did not index");
    expect(text).not.toMatch(/\bunread\b/i);
  });

  it("parses both bodies into one shape and builds project requests", () => {
    const single = parseSearch({
      api_version: 0,
      repository: "app",
      query: "x",
      kinds: ["route", "nope"],
      limit: 10,
      ide: { available: true, error: null },
      index: null,
      searched: { indexed: true, files: 3, files_not_read: 0, scanner_gaps: 0 },
      absence: "not_proven",
      totals: { route: 1 },
      groups: [
        { kind: "route", available: true, unavailable: [], total: 1, best_tier: "prefix", truncated: false, items: [{ kind: "route", repository: "app", id: "r", label: "/x", tier: "prefix", match_key: "path", file: "a.ts", line: 1, ide: null, method: "GET", path: "/x", test: false }] },
        { kind: "future_kind", available: true, unavailable: [], total: 0, best_tier: null, truncated: false, items: [] },
      ],
    });
    expect(single.project).toBeNull();
    expect(single.kinds).toEqual(["route"]);
    expect(single.groups.map((g) => g.kind)).toEqual(["route"]);
    expect(single.members[0]?.repository).toBe("app");
    expect(searchPath({ q: "a b", limit: 8, project: { member: "all", first: "react" } })).toBe("/api/v0/search?q=a+b&limit=8&first=react");
    expect(searchPath({ q: "a", limit: 50, project: { member: "express" } })).toBe("/api/v0/search?q=a&limit=50&repository=express");
  });

  it("keeps recent jumps first, without duplicates, for an empty query only", () => {
    let list: SearchItem[] = [];
    for (const l of ["a", "b", "c", "d", "e", "f"]) list = addRecent(list, item("route", l));
    expect(list.map((i) => i.label)).toEqual(["f", "e", "d", "c", "b"]);
    expect(list.length).toBe(RECENT_MAX);
    list = addRecent(list, item("route", "c"));
    expect(list.map((i) => i.label)).toEqual(["c", "f", "e", "d", "b"]);
    const g = buildGroups(null, "all", WS.slice(0, 1), [item("symbol", "x"), item("route", "y")]);
    expect(g.map((x) => x.key)).toEqual(["recent", "workspace"]);
    expect(buildGroups(null, "route", [], [item("symbol", "x"), item("route", "y")])[0]?.rows.length).toBe(1);
    expect(buildGroups(answer([["route", [item("route", "y")]]]), "all", [], [item("symbol", "x")]).map((x) => x.key)).toEqual(["route"]);
  });

  it("reads stored recent jumps defensively and per index", () => {
    expect(parseRecent(null)).toEqual([]);
    expect(parseRecent("not json")).toEqual([]);
    expect(parseRecent("{}")).toEqual([]);
    const good = item("file", "src/a.ts");
    expect(parseRecent(JSON.stringify([good, { kind: "nope" }, { ...good, label: 3 }]))).toEqual([good]);
    // B3: keyed on the served index and the workspace entry; nothing is
    // stored while the index is not known.
    expect(recentKey(null, null)).toBeNull();
    expect(recentKey(null, "shop~1")).toBeNull();
    const a = recentKey(servedIndex(null, "repoA"), null);
    const b = recentKey(servedIndex(null, "repoB"), null);
    expect(a).not.toBe(b);
    expect(recentKey(servedIndex(null, "repoA"), "shop~1")).not.toBe(a);
    expect(recentKey(servedIndex("shop", "api"), null)).not.toBe(recentKey(servedIndex(null, "shop"), null));
    const other = { ...good, repository: "repoA" };
    expect(recentForIndex([good, other], ["app"])).toEqual([good]);
  });
});
