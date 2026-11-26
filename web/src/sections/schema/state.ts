// Page states, visible groups and the set's common state (T018b2 sections
// 3, 4 and Revision 1). One rule each, pure, so the tests can walk every
// row. pageState reads summary before sets: `found: unknown` never shows
// "No migrations found", whatever sets says.

import type { SchemaGroup, SchemaSets, SchemaSummary, SchemaTable, UnreadTool } from "@/api/schema";

/** A summary unknown_reason for which /sets is requested anyway: the
 * schema pass ran, and /sets lists the paths behind the reason. */
export const LISTED_UNKNOWN_REASONS: readonly string[] = ["scan_incomplete", "manifest_unreadable"];

export type PageState =
  | { kind: "not_indexed" }
  | { kind: "unknown"; found: string; reason: string | null }
  | { kind: "none" }
  /** No group holds a migration or a table, and the pass found a set it
   * stored nothing for (failed_sets or sets_without_records). */
  | { kind: "failed_only" }
  /** Every group is test-only and Include tests is off. */
  | { kind: "tests_only"; hidden: SchemaGroup[] }
  /** summary said sets but /sets holds nothing (the index changed between
   * the two reads). */
  | { kind: "changed" }
  | { kind: "home"; groups: SchemaGroup[] };

/** summaryPage is the page decided by the status and the summary alone,
 * or null when it depends on /sets (found sets). */
export function summaryPage(statusIndexed: boolean | null, summary: SchemaSummary): PageState | null {
  if (statusIndexed === false || (summary.found === "unknown" && summary.unknown_reason === "not_indexed")) return { kind: "not_indexed" };
  if (summary.found === "none") return { kind: "none" };
  if (summary.found !== "sets") return { kind: "unknown", found: summary.found, reason: summary.unknown_reason };
  return null;
}

/** needsSets says whether the view requests /sets for this summary. */
export function needsSets(statusIndexed: boolean | null, summary: SchemaSummary): boolean {
  const page = summaryPage(statusIndexed, summary);
  if (page === null) return true;
  return page.kind === "unknown" && page.found === "unknown" && page.reason !== null && LISTED_UNKNOWN_REASONS.includes(page.reason);
}

/** visibleGroups hides test_only true groups unless tests are included;
 * false and null (no migrations, or tables from another producer) are
 * always shown. */
export function visibleGroups(sets: SchemaSets, tests: boolean): SchemaGroup[] {
  return sets.sets.filter((g) => tests || g.test_only !== true);
}

/** pageState is section 3's table. */
export function pageState(statusIndexed: boolean | null, summary: SchemaSummary, sets: SchemaSets, tests: boolean): PageState {
  const early = summaryPage(statusIndexed, summary);
  if (early !== null) return early;
  const holds = sets.sets.some((g) => g.migrations.length > 0 || g.tables.length > 0);
  if (!holds && (sets.failed_sets.length > 0 || sets.sets_without_records.length > 0)) return { kind: "failed_only" };
  if (sets.sets.length === 0) return { kind: "changed" };
  if (!tests && sets.sets.every((g) => g.test_only === true)) return { kind: "tests_only", hidden: sets.sets };
  return { kind: "home", groups: visibleGroups(sets, tests) };
}

/** The set's common state (D5): the most frequent completeness among
 * `complete` and `complete_in_source`, ties to `complete_in_source`;
 * partial and unknown are never common. null when no table has either. */
export type CommonState = "complete" | "complete_in_source" | null;

export function commonState(tables: readonly SchemaTable[]): CommonState {
  let complete = 0;
  let inSource = 0;
  for (const t of tables) {
    if (t.completeness === "complete") complete++;
    else if (t.completeness === "complete_in_source") inSource++;
  }
  if (complete === 0 && inSource === 0) return null;
  return complete > inSource ? "complete" : "complete_in_source";
}

/** stateCounts counts a group's tables by completeness (raw values). */
export function stateCounts(tables: readonly SchemaTable[]): Map<string, number> {
  const m = new Map<string, number>();
  for (const t of tables) m.set(t.completeness, (m.get(t.completeness) ?? 0) + 1);
  return m;
}

/** hiddenTestSets counts the test-only groups Include tests hides. */
export function hiddenTestSets(sets: SchemaSets, tests: boolean): number {
  return tests ? 0 : sets.sets.filter((g) => g.test_only === true).length;
}

/** unreadableCount is the Q6 "N unreadable" number from the summary:
 * failed sets, sets without records and unreadable manifests (Revision 1).
 * null when any count is not known. */
export function unreadableCount(s: SchemaSummary): number | null {
  if (s.failed_sets === null || s.sets_without_records === null || s.unreadable_manifests === null) return null;
  return s.failed_sets + s.sets_without_records + s.unreadable_manifests;
}

/** ObjectsState says whether a group's tables were extracted: "extracted"
 * (`objects` modeled, or the `set: null` group of another producer, whose
 * tables are what it stored), "not_extracted" (`objects` not_modeled) or
 * "unknown" (any other value). Only an extracted group's zero is a count;
 * the others show "not extracted" or "unknown", never 0 (Review fixes R1). */
export type ObjectsState = "extracted" | "not_extracted" | "unknown";

export function objectsState(g: Pick<SchemaGroup, "objects" | "set">): ObjectsState {
  if (g.objects === "modeled") return "extracted";
  if (g.objects === "not_modeled") return "not_extracted";
  if (g.objects === null && g.set === null) return "extracted";
  return "unknown";
}

/** isTestTable: a table whose stored scope is "test" (T018f O3). */
export function isTestTable(t: Pick<SchemaTable, "node">): boolean {
  return t.node.attributes.scope === "test";
}

/** ScopedGroups is the home's groups after Include tests per table. */
export interface ScopedGroups {
  groups: SchemaGroup[];
  /** Test-scope tables left out, per scoped group. */
  hidden: Map<SchemaGroup, number>;
  /** The identities of every table left out. */
  hiddenIds: Set<string>;
}

/** scopeTables applies Include tests per table to model groups (T018f
 * O3): with tests off, a model group's test-scope tables are left out of
 * the list and counted, so the group can say how many are hidden; with it
 * on, nothing changes. Migration groups keep the per-group test_only rule
 * (visibleGroups). A group with nothing hidden is returned as is. */
export function scopeTables(groups: readonly SchemaGroup[], tests: boolean): ScopedGroups {
  const hidden = new Map<SchemaGroup, number>();
  const hiddenIds = new Set<string>();
  const out = groups.map((g) => {
    if (tests || g.source !== "models") return g;
    const keep = g.tables.filter((t) => !isTestTable(t));
    if (keep.length === g.tables.length) return g;
    for (const t of g.tables) if (isTestTable(t)) hiddenIds.add(t.node.identity);
    const scoped = { ...g, tables: keep };
    hidden.set(scoped, g.tables.length - keep.length);
    return scoped;
  });
  return { groups: out, hidden, hiddenIds };
}

/** SourceCard is one card of the set strip (T018f R-1, 2.1): a group, a
 * source that was read and holds no group, or a tool found but not read. */
export type SourceCard =
  | { kind: "group"; group: SchemaGroup }
  | { kind: "empty"; source: "migrations" | "models"; reasons: readonly string[] }
  | { kind: "unread"; tool: UnreadTool };

/** sourceCards lists the strip's cards: one per visible group (migration
 * groups first, as the API orders them), then for each source in
 * sources_read with no group at all (hidden test-only groups count as
 * groups) a dashed card, then one dashed card per unread tool. A source not
 * in sources_read gets no card. "No migration set found" is never said
 * while a tool is found but not read, or while a set or manifest could
 * not be read (those are listed below the strip). The models card carries
 * model_list_reasons (python_unread, python_scan_gap), which it names. */
export function sourceCards(visible: readonly SchemaGroup[], sets: SchemaSets, sourcesRead: readonly string[] | null, unread: readonly UnreadTool[] | null): SourceCard[] {
  const out: SourceCard[] = visible.map((group) => ({ kind: "group", group }));
  const read = sourcesRead ?? [];
  const has = (src: string) => sets.sets.some((g) => g.source === src);
  const unreadPaths = sets.failed_sets.length + sets.sets_without_records.length + sets.unreadable_manifests.length + sets.scan_gaps.length;
  if (read.includes("migrations") && !has("migrations") && unread !== null && unread.length === 0 && unreadPaths === 0) out.push({ kind: "empty", source: "migrations", reasons: [] });
  if (read.includes("models") && !has("models")) out.push({ kind: "empty", source: "models", reasons: sets.model_list_reasons ?? [] });
  for (const tool of unread ?? []) out.push({ kind: "unread", tool });
  return out;
}
