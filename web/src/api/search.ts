// Types, guard and request of the jump-to search (docs/cli/serve.md
// "GET /api/v0/search", claim contract jump-search, ADR 0089). One
// repository answers its own body; a project answers every member in one
// body. Both are read into one shape (SearchAnswer) so the palette has a
// single code path: a single repository is a project of one member
// without a project name.

import { array, bool, getJSON, intOrNull, object, parseIde, parseIdeStatus, str, strOrNull, version, type IdeLink, type IdeStatus, type JSONObject, type Signal } from "@/api/endpoints";

/** SearchKind is a kind of result the server searches (row JS-2). */
export type SearchKind = "route" | "operation" | "table" | "symbol" | "file";

/** SEARCH_KINDS is the fixed kind order (row JS-11's tie-break): the
 * palette's kind chips use it, so the chips never move with the results. */
export const SEARCH_KINDS: readonly SearchKind[] = ["route", "operation", "table", "symbol", "file"];

export function isSearchKind(value: string): value is SearchKind {
  return (SEARCH_KINDS as readonly string[]).includes(value);
}

/** SearchItem is one result. Kind fields are null where the kind has none. */
export interface SearchItem {
  kind: SearchKind;
  repository: string;
  /** The node identity (the route for an operation, the file node for a
   * file); null for a file without a stored file node. */
  id: string | null;
  label: string;
  tier: string;
  match_key: string;
  file: string;
  line: number | null;
  /** The server-built editor link; null without a checkout or for an
   * operation. */
  ide: IdeLink | null;
  method: string | null;
  path: string | null;
  prefix_unknown: string | null;
  /** A test route (row JS-4); null for other kinds. */
  test: boolean | null;
  node_kind: string | null;
  qualified_name: string | null;
  source: string | null;
  language: string | null;
}

export interface SearchGroup {
  kind: SearchKind;
  available: boolean;
  /** The repositories the kind could not be searched in, with why. */
  unavailable: { repository: string; reason: string }[];
  total: number;
  best_tier: string | null;
  truncated: boolean;
  items: SearchItem[];
}

/** SearchSearched names what one repository's search covered (row
 * JS-13): the palette words "no result" with it, never as an absence. */
export interface SearchSearched {
  indexed: boolean;
  files: number;
  files_not_read: number;
  scanner_gaps: number;
}

export interface SearchMember {
  repository: string;
  checkout: boolean;
  ide: IdeStatus;
  searched: SearchSearched;
  totals: Partial<Record<SearchKind, number>>;
}

/** SearchAnswer is either body: project is null for one repository. */
export interface SearchAnswer {
  project: string | null;
  query: string;
  kinds: SearchKind[];
  members: SearchMember[];
  totals: Partial<Record<SearchKind, number>>;
  /** Server order (row JS-11): best tier first. */
  groups: SearchGroup[];
}

function count(obj: JSONObject, key: string, path: string): number {
  const v = intOrNull(obj, key, path);
  if (v === null || v < 0) throw new Error(`${path}.${key} is not a count`);
  return v;
}

function optStr(obj: JSONObject, key: string, path: string): string | null {
  return obj[key] === undefined ? null : strOrNull(obj, key, path);
}

function totals(value: unknown, path: string): Partial<Record<SearchKind, number>> {
  const obj = object(value, path);
  const out: Partial<Record<SearchKind, number>> = {};
  for (const k of SEARCH_KINDS) {
    if (obj[k] !== undefined) out[k] = count(obj, k, path);
  }
  return out;
}

function searched(value: unknown, path: string): SearchSearched {
  const obj = object(value, path);
  return {
    indexed: bool(obj, "indexed", path),
    files: count(obj, "files", path),
    files_not_read: count(obj, "files_not_read", path),
    scanner_gaps: count(obj, "scanner_gaps", path),
  };
}

function item(value: unknown, path: string, kind: SearchKind): SearchItem {
  const obj = object(value, path);
  return {
    kind,
    repository: str(obj, "repository", path),
    id: strOrNull(obj, "id", path),
    label: str(obj, "label", path),
    tier: str(obj, "tier", path),
    match_key: str(obj, "match_key", path),
    file: str(obj, "file", path),
    line: intOrNull(obj, "line", path),
    ide: parseIde(obj.ide, `${path}.ide`),
    method: optStr(obj, "method", path),
    path: optStr(obj, "path", path),
    prefix_unknown: optStr(obj, "prefix_unknown", path),
    test: obj.test === undefined || obj.test === null ? null : bool(obj, "test", path),
    node_kind: optStr(obj, "node_kind", path),
    qualified_name: optStr(obj, "qualified_name", path),
    source: optStr(obj, "source", path),
    language: optStr(obj, "language", path),
  };
}

/** parseSearchItem checks one stored item, its kind included (the
 * palette's recent jumps keep items in this browser). */
export function parseSearchItem(value: unknown, path: string): SearchItem {
  const obj = object(value, path);
  const kind = str(obj, "kind", path);
  if (!isSearchKind(kind)) throw new Error(`${path}.kind is not a known kind`);
  return item(obj, path, kind);
}

function groups(value: unknown, path: string): SearchGroup[] {
  const out: SearchGroup[] = [];
  array(value, path).forEach((v, i) => {
    const p = `${path}[${String(i)}]`;
    const obj = object(v, p);
    const kind = str(obj, "kind", p);
    // A kind from a newer server is not shown: the palette has no words
    // for it (the default row D of the contract claims nothing either).
    if (!isSearchKind(kind)) return;
    out.push({
      kind,
      available: bool(obj, "available", p),
      unavailable: array(obj.unavailable, `${p}.unavailable`).map((u, j) => {
        const q = `${p}.unavailable[${String(j)}]`;
        const uo = object(u, q);
        return { repository: str(uo, "repository", q), reason: str(uo, "reason", q) };
      }),
      total: count(obj, "total", p),
      best_tier: strOrNull(obj, "best_tier", p),
      truncated: bool(obj, "truncated", p),
      items: array(obj.items, `${p}.items`).map((it, j) => item(it, `${p}.items[${String(j)}]`, kind)),
    });
  });
  return out;
}

function kinds(value: unknown, path: string): SearchKind[] {
  return array(value, path).filter((k): k is SearchKind => typeof k === "string" && isSearchKind(k));
}

/** parseSearch checks either body of GET /api/v0/search. */
export function parseSearch(value: unknown): SearchAnswer {
  const p = "search";
  const obj = object(value, p);
  version(obj, p);
  if (typeof obj.project === "string") {
    return {
      project: obj.project,
      query: str(obj, "query", p),
      kinds: kinds(obj.kinds, `${p}.kinds`),
      members: array(obj.members, `${p}.members`).map((m, i) => {
        const q = `${p}.members[${String(i)}]`;
        const mo = object(m, q);
        return {
          repository: str(mo, "repository", q),
          checkout: bool(mo, "checkout", q),
          ide: parseIdeStatus(mo.ide, `${q}.ide`),
          searched: searched(mo.searched, `${q}.searched`),
          totals: totals(mo.totals, `${q}.totals`),
        };
      }),
      totals: totals(obj.totals, `${p}.totals`),
      groups: groups(obj.groups, `${p}.groups`),
    };
  }
  const t = totals(obj.totals, `${p}.totals`);
  return {
    project: null,
    query: str(obj, "query", p),
    kinds: kinds(obj.kinds, `${p}.kinds`),
    members: [{ repository: str(obj, "repository", p), checkout: true, ide: parseIdeStatus(obj.ide, `${p}.ide`), searched: searched(obj.searched, `${p}.searched`), totals: t }],
    totals: t,
    groups: groups(obj.groups, `${p}.groups`),
  };
}

export interface SearchParams {
  q: string;
  /** Absent: every kind. */
  kinds?: readonly SearchKind[];
  limit: number;
  editor?: string;
  /** Project mode: "all" searches every member with `first` first; a
   * member name searches that member only. Absent on a single repository. */
  project?: { member: "all"; first: string } | { member: string };
}

/** searchPath builds the request. In project mode the path names its
 * scope itself (repository= or first=): the search is project-wide in
 * api/client.ts, so no member is added to it. */
export function searchPath(p: SearchParams): string {
  const q = new URLSearchParams();
  q.set("q", p.q);
  if (p.kinds !== undefined) q.set("kinds", p.kinds.join(","));
  q.set("limit", String(p.limit));
  if (p.editor !== undefined) q.set("editor", p.editor);
  if (p.project !== undefined) {
    if ("first" in p.project) q.set("first", p.project.first);
    else q.set("repository", p.project.member);
  }
  return `/api/v0/search?${q.toString()}`;
}

/** SearchFetcher runs one search; tests pass their own. */
export type SearchFetcher = (p: SearchParams, options: Signal) => Promise<SearchAnswer>;

export const fetchSearch: SearchFetcher = (p, o) => getJSON(searchPath(p), parseSearch, o);
