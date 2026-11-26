// Status categories of the API section (T016d D10). The server passes raw
// statuses through; this file is the only place that groups them. The
// precision rule: only the exact string "linked" is linked. Anything this
// UI does not know (a new status, a case variant, "" for a missing status,
// a status of the other transport) is "other", shown with its raw value.

import type { StatusCounts, Transport } from "@/api/endpoints";

export type Category =
  | "linked"
  | "linked_across"
  | "unmatched"
  | "dynamic"
  | "wrapper"
  | "ambiguous"
  | "ambiguous_across"
  | "test_only"
  | "unproven"
  | "plugin"
  | "global"
  | "other";

/** CATEGORY_ORDER is the order of the coverage cells after the M5 five
 * (linked, unmatched, dynamic, wrapper, no linked caller). */
export const CATEGORY_ORDER: readonly Category[] = [
  "linked",
  "linked_across",
  "unmatched",
  "dynamic",
  "wrapper",
  "ambiguous",
  "ambiguous_across",
  "test_only",
  "unproven",
  "plugin",
  "global",
  "other",
];

/** The categories every coverage card shows, also at 0 (M5). */
export const ALWAYS_SHOWN: readonly Category[] = ["linked", "unmatched", "dynamic", "wrapper"];

const SHARED: Record<string, Category> = {
  linked: "linked",
  unmatched: "unmatched",
  dynamic: "dynamic",
  wrapper: "wrapper",
  ambiguous: "ambiguous",
  test_only: "test_only",
  // Project scope (serve --project, T022 MR-UI): the server's
  // project_by_status names the calls the link pass decided across
  // repositories. A stored project link is a linked-class result of its
  // own (inferred, never "linked" inside the repository); an ambiguous
  // outcome is never a link.
  linked_across: "linked_across",
  ambiguous_across: "ambiguous_across",
};

const TAURI_ONLY: Record<string, Category> = {
  registry_unproven: "unproven",
  acl_possible: "unproven",
  shadow_possible: "unproven",
  unknown_command: "unproven",
  plugin: "plugin",
  global_unlinked: "global",
};

/** categoryOf maps a raw status of a transport to its category. */
export function categoryOf(transport: Transport, raw: string): Category {
  // Object.hasOwn: a status such as "constructor" or "__proto__" must not
  // find an inherited property.
  if (Object.hasOwn(SHARED, raw)) return SHARED[raw] ?? "other";
  if (transport === "tauri_ipc" && Object.hasOwn(TAURI_ONLY, raw)) return TAURI_ONLY[raw] ?? "other";
  return "other";
}

/** The raw statuses this UI documents for a category (the gap sheet
 * requests these when none was counted). "other" has none. */
export function knownStatuses(transport: Transport, category: Category): string[] {
  const out: string[] = [];
  for (const [raw, c] of Object.entries(SHARED)) if (c === category) out.push(raw);
  if (transport === "tauri_ipc") for (const [raw, c] of Object.entries(TAURI_ONLY)) if (c === category) out.push(raw);
  return out;
}

export const CATEGORY_LABEL: Record<Category, string> = {
  linked: "Linked",
  linked_across: "Linked across repositories",
  unmatched: "Unmatched",
  dynamic: "Dynamic",
  wrapper: "Wrapper",
  ambiguous: "Ambiguous",
  ambiguous_across: "Ambiguous across repositories",
  test_only: "Test only",
  unproven: "Unproven",
  plugin: "Plugin",
  global: "Global",
  other: "Other",
};

/** The token family of each category (--linked, --other, ...). The further
 * categories share the neutral "other" colour and are told apart by their
 * label and icon. */
export const CATEGORY_TONE: Record<Category, "linked" | "across" | "unmatched" | "dynamic" | "wrapper" | "other"> = {
  linked: "linked",
  linked_across: "across",
  unmatched: "unmatched",
  dynamic: "dynamic",
  wrapper: "wrapper",
  ambiguous: "other",
  ambiguous_across: "other",
  test_only: "other",
  unproven: "other",
  plugin: "other",
  global: "other",
  other: "other",
};

/** One raw status and its count, as listed in tooltips ("weird: 2"). */
export interface RawCount {
  status: string;
  count: number;
}

export interface CategoryCount {
  category: Category;
  count: number;
  /** The raw statuses behind the count, sorted by status. */
  raws: RawCount[];
}

/** categoryCounts groups a by_status map by category. Every category is
 * present, with 0 when nothing maps to it. */
export function categoryCounts(transport: Transport, byStatus: StatusCounts): Map<Category, CategoryCount> {
  const out = new Map<Category, CategoryCount>();
  for (const c of CATEGORY_ORDER) out.set(c, { category: c, count: 0, raws: [] });
  for (const raw of Object.keys(byStatus).sort()) {
    const n = byStatus[raw] ?? 0;
    const entry = out.get(categoryOf(transport, raw));
    if (entry === undefined) continue;
    entry.count += n;
    entry.raws.push({ status: raw, count: n });
  }
  return out;
}

/** A coverage cell: a call category, or the endpoints without a linked caller. */
export type CoverageCell = ({ kind: "category" } & CategoryCount) | { kind: "never"; count: number };

/** coverageCells returns the cells of a coverage card in D10 order: the M5
 * five always (linked, unmatched, dynamic, wrapper, no linked caller), then
 * each further category with a non-zero count. */
export function coverageCells(transport: Transport, byStatus: StatusCounts, neverCalled: number, project = false): CoverageCell[] {
  const counts = categoryCounts(transport, byStatus);
  const cells: CoverageCell[] = [];
  // Project scope: "linked across repositories" is always shown, right
  // after "linked" (T022 MR-UI).
  const always = project ? ["linked", "linked_across", ...ALWAYS_SHOWN.slice(1)] : ALWAYS_SHOWN;
  for (const c of always) {
    const entry = counts.get(c as Category);
    if (entry !== undefined) cells.push({ kind: "category", ...entry });
  }
  cells.push({ kind: "never", count: neverCalled });
  for (const c of CATEGORY_ORDER) {
    if (always.includes(c)) continue;
    const entry = counts.get(c);
    if (entry !== undefined && entry.count > 0) cells.push({ kind: "category", ...entry });
  }
  return cells;
}

/** linkedPercent is floor(100 * linked / total), so 199 of 200 is 99 %,
 * never 100 %; null when there are no calls. */
export function linkedPercent(linked: number, total: number): number | null {
  if (total <= 0) return null;
  return Math.floor((100 * linked) / total);
}

/** linkedCount counts the linked-class calls of a status map: "linked"
 * and, in project scope, "linked_across" (a stored project link). The
 * linked percentage and the headline read it. */
export function linkedCount(byStatus: StatusCounts): number {
  return (byStatus.linked ?? 0) + (byStatus.linked_across ?? 0);
}

/** gapStatuses returns the raw statuses the gap sheet requests for a
 * category: the ones counted in by_status, else the documented ones. For
 * "other" only the counted raw values exist. */
export function gapStatuses(transport: Transport, category: Category, byStatus: StatusCounts): string[] {
  const counted = Object.keys(byStatus)
    .filter((raw) => categoryOf(transport, raw) === category)
    .sort();
  return counted.length > 0 ? counted : knownStatuses(transport, category);
}

/** neverCalledLabel names the endpoints without a stored caller, with a
 * count when given ("3 with no linked caller"). The wording says what
 * Graphvein found, not that nothing calls the endpoint: the same view counts
 * unmatched, dynamic and wrapper calls that may still reach it, and unknown
 * is never zero. In production scope test calls are not callers
 * (docs/cli/serve.md), so the label names the scope: "no linked caller in
 * production". Every place that labels such endpoints uses this, so the
 * wording stays consistent across the cards, strip, list, detail and gap
 * sheet. The API keys (never_called, ...) keep their names: they are the
 * contract, this is only the text shown. */
export function neverCalledLabel(scope: string, count?: number, project = false): string {
  // Project mode (T022 B9 R1): an endpoint called only from another member
  // has a linked caller; the headline is the project's.
  const where = project ? " in the project" : scope === "production" ? " in production" : "";
  return count === undefined ? `No linked caller${where}` : `${String(count)} with no linked caller${where}`;
}

/** NO_LINKED_CALLER_HINT is the tooltip sentence next to that label: what
 * the absence of a linked caller does and does not prove. */
export const NO_LINKED_CALLER_HINT = "Graphvein found no call it could link; unresolved calls may still reach it.";

/** NeverScoped is what the never-called headline of a transport counts:
 * the project's endpoints without a caller anywhere in the project under
 * serve --project, else the repository's. */
export interface NeverScoped {
  count: number;
  project: boolean;
  /** The repository-only count, the secondary text in project mode. */
  repositoryCount: number;
}

/** neverScoped reads a transport summary's never-called headline. */
export function neverScoped(t: { never_called: number; never_called_in_project: number | null }): NeverScoped {
  return t.never_called_in_project === null
    ? { count: t.never_called, project: false, repositoryCount: t.never_called }
    : { count: t.never_called_in_project, project: true, repositoryCount: t.never_called };
}

/** isNeverRow says whether an endpoint row has no caller in the scope that
 * applies: the repository, plus other members' calls in project mode. */
export function isNeverRow(r: { caller_count: number; project_caller_count: number | null; project_possible_caller_count?: number | null }): boolean {
  return r.caller_count === 0 && (r.project_caller_count ?? 0) === 0 && (r.project_possible_caller_count ?? 0) === 0;
}

/** isPossiblyCalledRow says whether an endpoint row has no caller and no
 * project link, but ambiguous or incomplete calls of other members may
 * reach it (T022 review): "possibly called (ambiguous across
 * repositories)", neither with a linked caller nor without one. */
export function isPossiblyCalledRow(r: { caller_count: number; project_caller_count: number | null; project_possible_caller_count?: number | null }): boolean {
  return r.caller_count === 0 && (r.project_caller_count ?? 0) === 0 && (r.project_possible_caller_count ?? 0) > 0;
}

/** POSSIBLY_CALLED is the label of such rows and of their count. */
export const POSSIBLY_CALLED = "possibly called (ambiguous across repositories)";

/** rawLabel shows a raw status in text: "" (missing) becomes "(no status)". */
export function rawLabel(raw: string): string {
  return raw === "" ? "(no status)" : raw;
}

/** callNoun is what the calls of a transport are called in the UI. */
export function callNoun(transport: Transport, n: number): string {
  if (transport === "http") return n === 1 ? "client call" : "client calls";
  if (transport === "server_action") return n === 1 ? "action use" : "action uses";
  return n === 1 ? "invoke site" : "invoke sites";
}

/** endpointNoun is what the endpoints of a transport are called. */
export function endpointNoun(transport: Transport, n: number): string {
  if (transport === "http") return n === 1 ? "route" : "routes";
  if (transport === "server_action") return n === 1 ? "action" : "actions";
  return n === 1 ? "command" : "commands";
}

export const TRANSPORT_LABEL: Record<Transport, string> = { http: "HTTP", tauri_ipc: "Tauri IPC", server_action: "Server actions" };
