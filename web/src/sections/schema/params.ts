// The schema section's deep-link state (T018b2 section 8):
//   #/schema?tab=tables|migrations|assumptions|overview&set=<set key>
//           &sel=<table id>&mig=<migration id>&tests=1&a=<entry id>
// Defaults are left out (tab tables, first visible group, first table,
// first migration, tests off); unknown parameters and values are ignored.
// The section writes the hash back with replaceHash. Identities are the
// canonical contract identities the detail endpoints take.

import { parseHashQuery } from "@/router";

export type SchemaTab = "tables" | "migrations" | "assumptions" | "overview";

export const SCHEMA_TABS: readonly SchemaTab[] = ["tables", "migrations", "assumptions", "overview"];

export interface SchemaParams {
  tab: SchemaTab;
  /** The group key (groupKey): null when not given. */
  set: string | null;
  sel: string | null;
  mig: string | null;
  tests: boolean;
  /** The Assumptions tab entry to scroll to and highlight (T018f): an
   * assumption id ("E2") or a contract row as rowEntry writes it. */
  a: string | null;
}

export const DEFAULT_SCHEMA_PARAMS: SchemaParams = { tab: "tables", set: null, sel: null, mig: null, tests: false, a: null };

/** rowEntry is the `a` value of a contract row: the two contracts share
 * row ids (M1, D, ...), so the source is part of it. */
export function rowEntry(source: string, id: string): string {
  return `row:${source}:${id}`;
}

/** The hash value of the group with `set: null` (tables from another
 * producer). A set key is a directory path and never "~". */
export const NO_SET_KEY = "~";

/** groupKey is the hash value of a migration group's set. */
export function groupKey(set: string | null): string {
  return set ?? NO_SET_KEY;
}

/** MODEL_KEY_PREFIX marks a model group's key: the server groups by source
 * and set, so a model group never shares a key with a migration set whose
 * directory has the same text (T018f Revision 1). */
export const MODEL_KEY_PREFIX = "@";

/** groupKeyOf is the hash value of any group. */
export function groupKeyOf(g: { set: string | null; source: string | null }): string {
  return g.source === "models" ? `${MODEL_KEY_PREFIX}${g.set ?? ""}` : groupKey(g.set);
}

function isTab(v: string | null): v is SchemaTab {
  return v !== null && (SCHEMA_TABS as readonly string[]).includes(v);
}

/** parseSchemaParams reads the section state from a location hash. */
export function parseSchemaParams(hash: string): SchemaParams {
  const q = parseHashQuery(hash);
  const tab = q.get("tab");
  return {
    tab: isTab(tab) ? tab : "tables",
    set: q.get("set") || null,
    sel: q.get("sel") || null,
    mig: q.get("mig") || null,
    tests: q.get("tests") === "1",
    // "as" is accepted as a synonym of the task's "a" (both name an entry).
    a: legacyEntry(q.get("a") || q.get("as") || null),
  };
}

/** LEGACY_MODEL_ROW is the prefix of a model contract row's entry from
 * before the API served model rows per tool (ADR 0055 amendment, ADR
 * 0064): `rows.models` held SQLModel's rows only. */
const LEGACY_MODEL_ROW = "row:models:";

/** legacyEntry keeps an old `a=row:models:<id>` deep link working: it
 * names SQLModel's row, now served under `models/sqlmodel`. */
export function legacyEntry(entry: string | null): string | null {
  if (entry?.startsWith(LEGACY_MODEL_ROW) !== true) return entry;
  return rowEntry("models/sqlmodel", entry.slice(LEGACY_MODEL_ROW.length));
}

/** schemaHash writes the section state; defaults are left out. */
export function schemaHash(p: SchemaParams): string {
  const q = new URLSearchParams();
  if (p.tab !== "tables") q.set("tab", p.tab);
  if (p.set !== null) q.set("set", p.set);
  if (p.sel !== null) q.set("sel", p.sel);
  if (p.mig !== null) q.set("mig", p.mig);
  if (p.tests) q.set("tests", "1");
  if (p.a !== null) q.set("a", p.a);
  const s = q.toString();
  return s === "" ? "#/schema" : `#/schema?${s}`;
}

/** isSchemaGraphView says whether a hash opens the Overview tab, in which
 * the shell's sidebar collapses to its icon rail (graph-view rule). */
export function isSchemaGraphView(hash: string): boolean {
  return parseSchemaParams(hash).tab === "overview";
}
