// The jump-to palette's model (T040): rows from a search answer and the
// workspace list, keyboard movement, and where each row opens. Pure, so
// the order, the grouping and the keys are tested without a browser.

import type { SearchAnswer, SearchItem, SearchKind } from "@/api/search";
import { SEARCH_KINDS, parseSearchItem } from "@/api/search";
import type { WorkspaceEntry as FullEntry } from "@/api/workspace";

/** WorkspaceEntry is what a "Switch to <name>" row reads of a workspace
 * entry (the sidebar's list, T038). */
export type WorkspaceEntry = Pick<FullEntry, "id" | "kind" | "name" | "path" | "state">;
import { formatCount } from "@/lib/count";
import { apiHash, parseApiParams } from "@/sections/api/params";
import { explorerRouteHash } from "@/sections/explorer/params";
import { DEFAULT_SCHEMA_PARAMS, schemaHash } from "@/sections/schema/params";

/** KIND_LABEL names a kind's group and chip. */
export const KIND_LABEL: Record<SearchKind, string> = {
  route: "Routes",
  operation: "Explorer operations",
  table: "Tables",
  symbol: "Symbols",
  file: "Files",
};

/** KIND_SECTION says where a kind opens, for the group heading. */
export const KIND_SECTION: Record<SearchKind, string> = {
  route: "API section",
  operation: "Explorer",
  table: "Schema section",
  symbol: "opens in the editor",
  file: "opens in the editor",
};

/** Filter is the kind chip: every kind, or one. */
export type KindFilter = "all" | SearchKind;

/** chipOrder is the order of the kind chips: "All" then the fixed kind
 * order (row JS-11's tie-break), whatever the results, so no chip moves
 * between queries. Kinds the server did not answer are left out. */
export function chipOrder(answered: readonly SearchKind[]): KindFilter[] {
  return ["all", ...SEARCH_KINDS.filter((k) => answered.includes(k))];
}

/** A row of the palette: a search result or a workspace entry. */
export type JumpRow = { type: "item"; key: string; item: SearchItem } | { type: "workspace"; key: string; entry: WorkspaceEntry };

/** JumpGroup is a heading and its rows. */
export interface JumpGroup {
  key: string;
  kind: SearchKind | "workspace" | "recent";
  label: string;
  total: number;
  rows: JumpRow[];
  /** More matches exist than the rows shown. */
  truncated: boolean;
  /** Members the kind could not be searched in, with why. */
  unavailable: { repository: string; reason: string }[];
  /** Per-member totals in project mode ("react 12 · express 14"). */
  split: { repository: string; total: number }[];
}

/** itemKey identifies a result across requests (and editors): the
 * chooser's pick finds the same item in the answer for the new editor. */
export function itemKey(it: SearchItem): string {
  return `${it.kind}\u0000${it.repository}\u0000${it.id ?? it.file}\u0000${it.label}`;
}

/** matchWorkspace keeps the entries a query names: every other entry
 * than the one viewed, matched by name (ASCII case folding, as the
 * server's search), all of them for an empty query. An entry whose
 * folder is gone cannot be opened and is left out. */
export function matchWorkspace<E extends WorkspaceEntry>(entries: readonly E[], viewed: string | null, query: string): E[] {
  const q = foldASCII(query.trim());
  return entries.filter((e) => e.id !== viewed && e.state !== "folder_gone" && (q === "" || foldASCII(e.name).includes(q)));
}

/** foldASCII folds A-Z only (the search's fold, row JS-1). */
export function foldASCII(s: string): string {
  return s.replace(/[A-Z]/g, (c) => c.toLowerCase());
}

/** buildGroups turns an answer into the palette's groups: the server's
 * group order (best tier first, row JS-11), filtered by the chip, then
 * the workspace entries. Empty kind groups are not listed (their 0 is on
 * the chip). */
export function buildGroups(answer: SearchAnswer | null, filter: KindFilter, workspace: readonly WorkspaceEntry[], recent: readonly SearchItem[] = []): JumpGroup[] {
  const out: JumpGroup[] = [];
  // Recent jumps stand for an empty query only (answer null), first.
  const shownRecent = answer === null ? recent.filter((it) => filter === "all" || it.kind === filter) : [];
  if (shownRecent.length > 0) {
    out.push({
      key: "recent",
      kind: "recent",
      label: "Recent",
      total: shownRecent.length,
      rows: shownRecent.map((item) => ({ type: "item", key: `recent\u0000${itemKey(item)}`, item })),
      truncated: false,
      unavailable: [],
      split: [],
    });
  }
  if (answer !== null) {
    for (const g of answer.groups) {
      if (filter !== "all" && g.kind !== filter) continue;
      if (g.items.length === 0 && g.unavailable.length === 0) continue;
      out.push({
        key: g.kind,
        kind: g.kind,
        label: KIND_LABEL[g.kind],
        total: g.total,
        rows: g.items.map((item) => ({ type: "item", key: itemKey(item), item })),
        truncated: g.truncated,
        unavailable: g.unavailable,
        split: answer.project === null ? [] : answer.members.map((m) => ({ repository: m.repository, total: m.totals[g.kind] ?? 0 })).filter((s) => s.total > 0),
      });
    }
  }
  if (filter === "all" && workspace.length > 0) {
    out.push({
      key: "workspace",
      kind: "workspace",
      label: "Workspace",
      total: workspace.length,
      rows: workspace.map((entry) => ({ type: "workspace", key: `ws\u0000${entry.id}`, entry })),
      truncated: false,
      unavailable: [],
      split: [],
    });
  }
  return out;
}

/** flatRows is the keyboard order: every row of every group in order. */
export function flatRows(groups: readonly JumpGroup[]): JumpRow[] {
  return groups.flatMap((g) => g.rows);
}

/** move returns the selection after an arrow key: clamped at both ends,
 * -1 (nothing) when there are no rows. */
export function move(index: number, delta: number, length: number): number {
  if (length === 0) return -1;
  if (index < 0) return delta > 0 ? 0 : length - 1;
  return Math.min(length - 1, Math.max(0, index + delta));
}

/** nextGroup returns the index of the first row of the next group (Tab)
 * or of the previous one (Shift+Tab), wrapping around. */
export function nextGroup(groups: readonly JumpGroup[], index: number, dir: 1 | -1): number {
  const starts: number[] = [];
  let n = 0;
  for (const g of groups) {
    if (g.rows.length > 0) starts.push(n);
    n += g.rows.length;
  }
  if (starts.length === 0) return -1;
  let current = 0;
  starts.forEach((s, i) => {
    if (index >= s) current = i;
  });
  const next = (current + dir + starts.length) % starts.length;
  return starts[next] ?? -1;
}

/** Target is what opening a row does. */
export type Target =
  | { kind: "hash"; hash: string; repository: string }
  | { kind: "editor"; link: string; repository: string }
  | { kind: "workspace"; id: string }
  | { kind: "none"; reason: string };

/** NO_EDITOR_LINK is said when a file or symbol has no editor link. */
export const NO_EDITOR_LINK = "No editor link: the repository has no checkout here, or editor links are off in this browser";

/** NO_OPERATION_LINE is said on Ctrl+Enter on an Explorer operation:
 * the document's operations carry no source line (T040 review). */
export const NO_OPERATION_LINE = "No source line for an Explorer operation: Enter opens it in the Explorer";

/** NO_ROUTE_EDITOR_LINK and NO_TABLE_EDITOR_LINK are said on Ctrl+Enter
 * on a route or table without an editor link, as for an operation: the
 * row is not opened silently somewhere else (T040 review). */
export const NO_ROUTE_EDITOR_LINK = "No editor link for this route: Enter opens it in the API section";
export const NO_TABLE_EDITOR_LINK = "No editor link for this table: Enter opens it in the Schema section";

/** targetOf says where a row opens: routes in the API section,
 * operations in the Explorer, tables in the Schema section, files and
 * symbols in the editor (the server-built link). editor forces the
 * editor for any row with a link (Ctrl+Enter); a row without one says
 * why and is not opened somewhere else. */
export function targetOf(row: JumpRow, editor = false): Target {
  if (row.type === "workspace") return { kind: "workspace", id: row.entry.id };
  const it = row.item;
  if (editor || it.kind === "symbol" || it.kind === "file") {
    if (it.ide !== null) return { kind: "editor", link: it.ide.link, repository: it.repository };
    if (it.kind === "symbol" || it.kind === "file") return { kind: "none", reason: NO_EDITOR_LINK };
    if (it.kind === "operation") return { kind: "none", reason: NO_OPERATION_LINE };
    if (it.kind === "route") return { kind: "none", reason: NO_ROUTE_EDITOR_LINK };
    return { kind: "none", reason: NO_TABLE_EDITOR_LINK };
  }
  if (it.id === null) return { kind: "none", reason: "No stored node to open" };
  switch (it.kind) {
    case "route": {
      const p = { ...parseApiParams(""), sel: it.id, tests: it.test === true };
      return { kind: "hash", hash: apiHash(p), repository: it.repository };
    }
    case "operation":
      return { kind: "hash", hash: explorerRouteHash(it.id), repository: it.repository };
    case "table":
      return { kind: "hash", hash: schemaHash({ ...DEFAULT_SCHEMA_PARAMS, sel: it.id }), repository: it.repository };
    default:
      return { kind: "none", reason: NO_EDITOR_LINK };
  }
}

/** Segment is a part of a label, marked where the query matched. */
export interface Segment {
  text: string;
  match: boolean;
}

/** highlight marks the first folded occurrence of query in text (the
 * server matches the same way); none when it does not occur, for example
 * when another key of the item matched. */
export function highlight(text: string, query: string): Segment[] {
  const q = foldASCII(query.trim());
  const at = q === "" ? -1 : foldASCII(text).indexOf(q);
  if (at === -1) return [{ text, match: false }];
  const out: Segment[] = [];
  if (at > 0) out.push({ text: text.slice(0, at), match: false });
  out.push({ text: text.slice(at, at + q.length), match: true });
  if (at + q.length < text.length) out.push({ text: text.slice(at + q.length), match: false });
  return out;
}

/** noResultText words a search without a match: never as proof that the
 * thing does not exist (row JS-14), with what was not searched. */
export function noResultText(answer: SearchAnswer): string {
  const notRead = answer.members.reduce((n, m) => n + m.searched.files_not_read, 0);
  const gaps = answer.members.reduce((n, m) => n + m.searched.scanner_gaps, 0);
  const notIndexed = answer.members.filter((m) => !m.searched.indexed).map((m) => m.repository);
  const parts: string[] = ["file contents"];
  if (notRead > 0) parts.push(`${formatCount(notRead)} ${notRead === 1 ? "file" : "files"} Graphvein could not read`);
  if (gaps > 0) parts.push(`${formatCount(gaps)} ${gaps === 1 ? "path" : "paths"} the scanner did not index`);
  parts.push("languages without a reader");
  let text = `Not proof that it does not exist: only stored names and paths were searched, not ${joinWords(parts)}.`;
  if (notIndexed.length > 0) text += ` Not indexed: ${notIndexed.join(", ")}.`;
  return text;
}

function joinWords(parts: string[]): string {
  if (parts.length <= 1) return parts.join("");
  return `${parts.slice(0, -1).join(", ")} or ${parts[parts.length - 1] ?? ""}`;
}

/** searchedText is the line under the results: what was searched. */
export function searchedText(answer: SearchAnswer): string {
  const files = answer.members.reduce((n, m) => n + m.searched.files, 0);
  const who = answer.project === null ? (answer.members[0]?.repository ?? "") : `project ${answer.project}`;
  return `Searched the names and paths stored for ${who} (${formatCount(files)} files)`;
}

/** RECENT_MAX is how many recent jumps the empty palette lists. */
export const RECENT_MAX = 5;

/** RECENT_KEY prefixes the localStorage key of the recent jumps.
 * localStorage is per server address only, and one address serves one
 * index today and another tomorrow, so the key names the served index
 * (servedIndex) and the workspace entry viewed (T040 review). */
export const RECENT_KEY = "graphvein.jump.recent.v1";

/** servedIndex is the index identity the server reports: the project
 * name of /api/v0/project under serve --project, else the repository of
 * /api/v0/status; null while neither answered. */
export function servedIndex(project: string | null, repository: string | undefined): string | null {
  if (project !== null) return `project:${project}`;
  return repository !== undefined ? `repository:${repository}` : null;
}

/** recentKey is the storage key for the index viewed (servedIndex) and
 * the workspace entry (?ws=, else the server's current entry, so one
 * entry has one key); null, while the index is not known, stores none. */
export function recentKey(index: string | null, workspace: string | null): string | null {
  if (index === null) return null;
  const k = `${RECENT_KEY}:${encodeURIComponent(index)}`;
  return workspace === null ? k : `${k}:ws=${encodeURIComponent(workspace)}`;
}

/** recentForIndex keeps the stored items of the repositories served: the
 * one repository, or the project's members. An item of another
 * repository (stored by an older version, or under the same name by
 * another index) is dropped. */
export function recentForIndex(list: readonly SearchItem[], repositories: readonly string[]): SearchItem[] {
  return list.filter((it) => repositories.includes(it.repository));
}

/** addRecent puts an opened item first, without a duplicate, keeping at
 * most RECENT_MAX. */
export function addRecent(list: readonly SearchItem[], item: SearchItem): SearchItem[] {
  const k = itemKey(item);
  return [item, ...list.filter((it) => itemKey(it) !== k)].slice(0, RECENT_MAX);
}

/** parseRecent reads the stored list: an entry that does not check is
 * dropped (written by another version, or edited by hand), never shown
 * half-read. */
export function parseRecent(raw: string | null): SearchItem[] {
  if (raw === null) return [];
  let value: unknown;
  try {
    value = JSON.parse(raw);
  } catch {
    return [];
  }
  if (!Array.isArray(value)) return [];
  const out: SearchItem[] = [];
  value.forEach((v: unknown, i) => {
    try {
      out.push(parseSearchItem(v, `recent[${String(i)}]`));
    } catch {
      // Dropped: see above.
    }
  });
  return out.slice(0, RECENT_MAX);
}

/** readRecent loads the recent jumps; storage that cannot be read (a
 * private window, blocked site data) or no key lists none. */
export function readRecent(key: string | null): SearchItem[] {
  if (key === null) return [];
  try {
    return parseRecent(window.localStorage.getItem(key));
  } catch {
    return [];
  }
}

/** writeRecent stores the recent jumps; a failure only loses them. */
export function writeRecent(key: string | null, list: readonly SearchItem[]): void {
  if (key === null) return;
  try {
    window.localStorage.setItem(key, JSON.stringify(list));
  } catch {
    // Not kept: the palette works without them.
  }
}
