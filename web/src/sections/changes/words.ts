// Every word the Changes section and the compare chip show for a value of
// the change endpoints, in one place so the wording and its tests cannot
// drift (T019 Revision 2 and 4, the V3 mockup README). A value this UI does
// not know is shown raw, never mapped to a known word.
//
// Principle (T019 Requirements): an absence is shown only where the API
// proves it. "unknown now" is never "removed", "known now" never "added",
// "not extracted" never zero or "no users".

import type { ChangeItem, ChangeRun, GroupCount, SideInfo } from "@/api/changes";
import type { LabelStyle } from "@/lib/labelChoice";
import { label, type Label } from "@/lib/labels";
import { NO_PATH_TITLE, NO_PATH_WORDS, isFullPathEntry, noPathKind, prefixReasonText, prefixUnknownPaths, servedPath, shadowedPaths, type PrefixUnknownPath } from "@/lib/routePath";
import { formatShortTime } from "@/lib/time";

/** PRINCIPLE is shown under the Changes section. */
export const PRINCIPLE =
  "An absence is shown only where the API proves it: unknown now is never “removed”, known now is never “added”, not extracted is never zero or “no users”.";

export function plural(n: number, one: string, many = `${one}s`): string {
  return `${n.toLocaleString("en-US")} ${n === 1 ? one : many}`;
}

/** GROUP_LABEL names the groups of the summary, in the API's order. */
export const GROUP_LABEL: Record<string, string> = {
  routes: "Routes",
  client_calls: "Client calls and links",
  api_operations: "API operations",
  ipc: "IPC commands",
  server_actions: "Server actions",
  migration_tables: "Migration tables",
  model_tables: "Model tables",
  migrations: "Migrations",
  code: "Code",
  angular: "Angular UI",
  react: "React UI",
  architecture: "Architecture",
  files: "Files",
  gaps: "Knowledge gaps",
  other: "Other facts",
  unclassified: "Unclassified",
};

export function groupLabel(group: string): string {
  return GROUP_LABEL[group] ?? group;
}

/** GROUP_ROWS names the contract rows behind each group (T019 claim
 * contract), in the group heading's tooltip (T025). */
export const GROUP_ROWS: Record<string, string> = {
  routes: "C3",
  client_calls: "C4 C5",
  api_operations: "C6",
  ipc: "C7",
  server_actions: "C18",
  migration_tables: "C8 C9",
  model_tables: "C11",
  migrations: "C10",
  code: "C2",
  angular: "C12",
  react: "C17",
  architecture: "C13",
  files: "C1",
  gaps: "C15",
  other: "C16",
  unclassified: "D",
};

/** CHANGE_LABEL is the chip text of a change kind. */
export const CHANGE_LABEL: Record<string, string> = {
  added: "added",
  removed: "no longer in the index",
  changed: "changed",
  body_changed: "body changed",
  unknown_now: "unknown now",
  known_now: "known now",
  link_gained: "link gained",
  link_lost: "link lost",
  link_unknown_now: "link unknown now",
  became_partial: "became partial",
  recovered: "recovered",
  unclassified: "unclassified",
};

export function changeLabel(change: string): string {
  return CHANGE_LABEL[change] ?? change;
}

/** itemChangeLabel is an item's chip: a removed file the head tree lacks
 * reads "deleted" (G8); every other removal "no longer in the index". */
export function itemChangeLabel(item: Pick<ChangeItem, "change" | "wording">): string {
  if (item.change === "removed" && item.wording === "deleted") return "deleted";
  return changeLabel(item.change);
}

export const CAUSE_LABEL: Record<string, string> = { code: "code", relink: "relink", extractor_or_relink: "extractor or relink" };

export const CAUSE_TITLE: Record<string, string> = {
  code: "cause: the file changed",
  relink: "cause: the file is unchanged; linking changed",
  extractor_or_relink: "cause: the file is unchanged and the extractor version differs: the extractor or linking changed",
};

/** ITEM_LABEL words an item's labels. */
export const ITEM_LABEL: Record<string, string> = {
  edited: "edited (history rewritten)",
  verdict_changed: "verdict changed",
  alters_changed: "alters changed",
  handler_changed: "handler changed",
  // T029 W2 (row MT-13): the route gained or lost a request or response
  // model.
  models_changed: "models changed",
  table_access_changed: "table access changed",
};

export function itemLabel(label: string): string {
  return ITEM_LABEL[label] ?? label;
}

/** reasonText says why a link was lost or a claim downgraded. */
export function reasonText(reason: string): string {
  switch (reason) {
    case "target_removed":
      return "target removed: the target is no longer in the index and the call is unchanged";
    case "target_changed":
      return "target changed: the target changed and the call is unchanged";
    case "call_changed":
      return "call changed: the call itself changed";
    case "call_absent":
      return "call removed: the call is no longer in the index";
    case "gap":
      return "knowledge gap: the file could not be read on one side";
    case "table_not_complete":
      return "table not complete: a statement the extractor cannot read touches the table";
    case "table_list_not_complete":
      return "table list not complete";
    case "base_table_list_unknown":
      return "the base side's table list is not known";
    case "excluded":
      return "excluded: the head tree still has the file, but ignore rules, the size limit or its language keep it out of the index";
    default:
      return reason;
  }
}

/** wordingText words a removal's `wording`. */
export function wordingText(wording: string): string {
  switch (wording) {
    case "no_longer_in_index":
      return "no longer in the index";
    case "deleted":
      return "deleted: the head side's tree lacks the file";
    case "call_removed":
      return "the call was removed";
    default:
      return wording;
  }
}

/** sideMissingText is the location line of a side the item lacks. */
export function sideMissingText(side: "base" | "head", item: Pick<ChangeItem, "change" | "wording">): string {
  if (side === "head") {
    if (item.change === "unknown_now" || item.change === "link_unknown_now") return "state unknown in the head (knowledge gap): not removed";
    if (item.wording !== null) return wordingText(item.wording);
    return "not on the head side";
  }
  if (item.change === "known_now") return "not known in the base (knowledge gap): not added";
  return "not in the base";
}

// Identities ----------------------------------------------------------------

function decode(s: string): string {
  try {
    return decodeURIComponent(s);
  } catch {
    return s;
  }
}

/** IdentityParts splits "kind:file#name" (names percent-encoded). */
export interface IdentityParts {
  kind: string;
  file: string;
  name: string;
}

export function identityParts(identity: string): IdentityParts | null {
  const colon = identity.indexOf(":");
  const hash = identity.indexOf("#");
  if (colon <= 0 || hash < colon) return null;
  return { kind: identity.slice(0, colon), file: identity.slice(colon + 1, hash), name: decode(identity.slice(hash + 1)) };
}

/** shortName is a node identity as people read it: its decoded name
 * ("GET /items/:id", "formatRow"); a file node is its path. */
export function shortName(identity: string): string {
  const p = identityParts(identity);
  if (p === null) return identity;
  if (p.kind === "file") return p.file;
  return p.name;
}

function edgeKey(identity: string): string[] | null {
  try {
    const v: unknown = JSON.parse(identity);
    if (Array.isArray(v) && v.length === 3 && v.every((x) => typeof x === "string")) return v;
  } catch {
    // not an edge key
  }
  return null;
}

/** itemTitle is an item's readable name: for an edge "source → target"
 * with both ends shortened; a migration by its file's name
 * ("0002_tags.sql", R5-5: every migration node is named "migration");
 * for another node or gap its name. */
export function itemTitle(item: Pick<ChangeItem, "record" | "identity" | "name" | "kind">): string {
  if (item.record === "edge") {
    const key = edgeKey(item.identity);
    if (key !== null) return `${shortName(key[1] ?? "")} → ${shortName(key[2] ?? "")}`;
  }
  if (item.record === "node" && item.kind === "migration") {
    const p = identityParts(item.identity);
    if (p !== null && p.file !== "") return p.file.slice(p.file.lastIndexOf("/") + 1);
  }
  return item.name;
}

/** kindWords says a stored kind ("client_call", "exposes_route") as
 * words ("client call", "exposes route"); it lives with the label table
 * (lib/labels.ts, T025b), which also gives the raw kind for the technical
 * style. */
export { kindWords } from "@/lib/labels";

/** RouteTitle is a route as the Changes section names it: the full
 * composed path with router prefixes, as the API section shows it (ADR
 * 0013), or the path as written in its file with a marker saying the full
 * path is not known. A partial path is never shown as the full path. */
export interface RouteTitle {
  /** "GET /api/v1/items", or the name as written when no full path is
   * known. */
  text: string;
  /** null for a full path; else what is known, in the API section's
   * words for a route without a composed path (NO_PATH_WORDS): "shadowed"
   * (every composed path an earlier mount takes first: mounted, its paths
   * withheld), "path withheld" (couldn't-read row CR-44: the server's
   * path_withheld or route_withheld), "path not known" (no composed path
   * stored, never "not mounted"); "prefix unknown"; or "path as written"
   * when the response carries no composed paths. */
  marker: string | null;
  /** "1 of 3 paths" when the route is mounted at more than one path. */
  more: string | null;
  /** Tooltip text: the name as written and every composed path. */
  title: string;
}

function templateOf(entry: unknown): { template: string | null; test: boolean } {
  if (typeof entry !== "object" || entry === null) return { template: null, test: false };
  const o = entry as Record<string, unknown>;
  return { template: servedPath(o), test: o.scope === "test" };
}

/** routeTitle words a route from its name as written ("GET /") and its
 * stored composed paths (the route node's `paths`, or an impact path's
 * `route_paths`; undefined when the response has none). Like the API
 * list, it shows the first production path, else the first. */
//
// prefixed are the route's paths whose prefix the code does not fix (T033
// row UP-29; prefixUnknownPaths): shown with "…" and the marker "prefix
// unknown" when the route has no full path, never as a full path.
//
// shadowed are the withheld paths of a route whose every composed path an
// earlier mount takes first (route attribute shadowed_paths; an impact
// path's route_shadowed gives them without the mounts): such a route is
// mounted, so it is marked "shadowed", never "not mounted".
//
// withheld is the server's mark of a registration whose path the Go
// linker withholds (row CR-44): "path withheld". Any other route without a
// composed path is "path not known": no stored fact proves it unmounted.
export function routeTitle(name: string, paths: unknown, prefixed: readonly PrefixUnknownPath[] = [], shadowed: readonly { path: string; by: string }[] | boolean = [], withheld = false): RouteTitle {
  const asWritten = `as written in its file: ${name}`;
  const space0 = name.indexOf(" ");
  const method0 = space0 > 0 ? name.slice(0, space0) : null;
  const first = prefixed[0];
  const fullEntries = Array.isArray(paths) ? paths.filter(isFullPathEntry) : null;
  if (first !== undefined && (fullEntries === null || fullEntries.map(templateOf).every((e) => e.template === null))) {
    const text = method0 !== null ? `${method0} ${first.path}` : first.path;
    const all = prefixed.map((p) => p.path + (p.scope === "test" ? " (test)" : "")).join(", ");
    return { text, marker: "prefix unknown", more: prefixed.length > 1 ? `1 of ${String(prefixed.length)} paths` : null, title: `${text}\n${asWritten}\n${prefixReasonText(first.reason)}; “…” is not read from code, never a full path\npaths below the unknown prefix: ${all}` };
  }
  if (fullEntries === null) {
    return { text: name, marker: "path as written", more: null, title: `${asWritten}; the full path with router prefixes is not known here` };
  }
  const entries = fullEntries.map(templateOf);
  const known = entries.filter((e): e is { template: string; test: boolean } => e.template !== null);
  if (entries.length === 0 || known.length === 0) {
    const taken = typeof shadowed === "boolean" ? [] : shadowed;
    if (shadowed === true || taken.length > 0) {
      const by = taken.length > 0 ? `: ${taken.map((x) => `${x.path} by ${x.by}`).join("; ")}` : "";
      return { text: name, marker: NO_PATH_WORDS.shadowed, more: null, title: `${asWritten}; every composed path is taken first by a mount added before the route${by}` };
    }
    const kind = noPathKind(false, withheld);
    return { text: name, marker: NO_PATH_WORDS[kind], more: null, title: `${asWritten}; ${NO_PATH_TITLE[kind]}` };
  }
  const chosen = known.find((e) => !e.test) ?? known[0];
  const space = name.indexOf(" ");
  const method = space > 0 ? name.slice(0, space) : null;
  const text = method !== null ? `${method} ${chosen?.template ?? ""}` : (chosen?.template ?? name);
  const more = entries.length > 1 ? `1 of ${String(entries.length)} paths` : null;
  const all = entries.map((e) => (e.template ?? "(no template)") + (e.test ? " (test)" : "")).join(", ");
  return { text, marker: null, more, title: `${text}\n${asWritten}\ncomposed paths: ${all}` };
}

/** itemRouteTitle is routeTitle for a route item, from its head side's
 * stored paths (the base side's for a removed route); null for any other
 * item. */
export function itemRouteTitle(item: Pick<ChangeItem, "record" | "kind" | "name" | "before" | "after">): RouteTitle | null {
  if (item.record !== "node" || item.kind !== "route") return null;
  const side = item.after ?? item.before;
  if (side === null) return routeTitle(item.name, undefined);
  return routeTitle(item.name, side.fields.paths, prefixUnknownPaths({ paths: side.fields.paths, unresolved_prefix_paths: side.fields.unresolved_prefix_paths }), shadowedPaths(side.fields), side.path_withheld);
}

/** VERIFICATION_TITLE explains an I8 step's verification (the documented
 * words, T019 Revision 5 R5-4): an oracle compared the form on fixtures;
 * it never checked this repository's database. */
export const VERIFICATION_TITLE: Record<string, string> = {
  oracle: "compared by an oracle on fixtures",
  mapped: "mapped, not verified",
};

/** verificationTitle is the tooltip of a verification value; one this UI
 * does not know is shown raw and claims nothing. */
export function verificationTitle(v: string): string {
  return VERIFICATION_TITLE[v] ?? `verification ${v}: not verified`;
}

/** itemFile is the file an item names, head side first. */
export function itemFile(item: Pick<ChangeItem, "after" | "before">): string | null {
  return item.after?.file ?? item.before?.file ?? null;
}

/** fileLine is "file:line" (1-based) or the file alone without a span. */
export function fileLine(file: string, span: { start_line: number } | null): string {
  return span === null ? file : `${file}:${String(span.start_line)}`;
}

/** valueText prints a stored value compactly. */
export function valueText(v: unknown): string {
  if (v === null || v === undefined) return "null";
  if (typeof v === "string") return v;
  try {
    return JSON.stringify(v);
  } catch {
    return "unprintable value";
  }
}

// Bases ---------------------------------------------------------------------

/** refLabel names a branch comparison in one form whatever its state
 * (indexing, refused, answered): "main..feature", and "main..current
 * index" without a head ref (T019 Revision 5, R5-6). */
export function refLabel(base: string, head: string | null): string {
  return `${base}..${head === null || head === "" ? "current index" : head}`;
}

/** baseLabel names a base parameter ("previous run", "run 3", "pin
 * release-1", "main..current index", "main..feature"). runs gives run
 * numbers. */
export function baseLabel(base: string, runs: readonly ChangeRun[] = []): string {
  if (base === "previous") return "previous run";
  if (base.startsWith("run:")) {
    const id = base.slice(4);
    const r = runs.find((x) => x.run_id === id);
    return r === undefined ? `run ${id.slice(0, 8)}` : `run ${String(r.seq)}`;
  }
  if (base.startsWith("pin:")) return `pin ${base.slice(4)}`;
  if (base.startsWith("ref:")) {
    const spec = base.slice(4);
    const at = spec.indexOf("..");
    return at < 0 ? refLabel(spec, null) : refLabel(spec.slice(0, at), spec.slice(at + 2));
  }
  return base;
}

/** shortCommit is the first 7 characters of a commit. */
export function shortCommit(c: string): string {
  return c.slice(0, 7);
}

/** runText is a run in the picker: "run 3 · Nov 14, 10:33 · c4b7a95". */
export function runText(r: ChangeRun): string {
  const parts = [`run ${String(r.seq)}`, formatShortTime(r.finished_at)];
  if (r.commit !== null) parts.push(shortCommit(r.commit) + (r.dirty === true ? " (dirty)" : ""));
  return parts.join(" · ");
}

/** sideName names one side of a summary. The base side of a branch
 * comparison is always the merge base (O2), and says so (R5-5). */
export function sideName(side: SideInfo, base: string, runs: readonly ChangeRun[], which: "base" | "head" = "head"): string {
  switch (side.kind) {
    case "current":
      return "current index";
    case "pin":
      return `pin ${side.pin ?? "?"}`;
    case "ref":
      if (which === "base") return side.commit === null ? "merge base snapshot" : `merge base ${shortCommit(side.commit)}`;
      return side.commit === null ? "ref snapshot" : `commit ${shortCommit(side.commit)}`;
    case "run":
      return base === "previous" ? "previous run" : baseLabel(`run:${side.run}`, runs);
    default:
      return side.kind;
  }
}

/** runSeq is "run N" for a run id the journal still lists, else null. */
export function runSeq(id: string, runs: readonly ChangeRun[]): string | null {
  const r = runs.find((x) => x.run_id === id);
  return r === undefined ? null : `run ${String(r.seq)}`;
}

/** PARTIAL_REASON words a side's partial_reasons. */
export function partialReasonText(reason: string): string {
  switch (reason) {
    case "gaps_truncated":
      return "the gap list was cut";
    case "gaps_fileless":
      return "a knowledge gap names no file";
    case "gaps_unknown":
      return "the gaps of this run are not known";
    default:
      return reason;
  }
}

// Groups --------------------------------------------------------------------

/** GroupEntry is a group in the sidebar and the narrow group picker:
 * count null means not extracted (never a zero). */
export interface GroupEntry {
  id: string;
  label: string;
  count: number | null;
  /** Text shown in place of the count. */
  text: string;
}

export function groupEntries(groups: readonly GroupCount[], notExtracted: readonly string[]): GroupEntry[] {
  const out: GroupEntry[] = groups.map((g) => {
    const ne = notExtracted.includes(g.group);
    return { id: g.group, label: groupLabel(g.group), count: ne ? null : g.items, text: ne ? "not extracted" : g.items.toLocaleString("en-US") };
  });
  // A not-extracted group the API lists only there still appears.
  for (const id of notExtracted) {
    if (!out.some((e) => e.id === id)) out.push({ id, label: groupLabel(id), count: null, text: "not extracted" });
  }
  return out;
}

/** changeTotals adds the per-group change counts. */
export function changeTotals(groups: readonly GroupCount[]): [string, number][] {
  const totals = new Map<string, number>();
  for (const g of groups) for (const [k, n] of Object.entries(g.changes)) totals.set(k, (totals.get(k) ?? 0) + n);
  return [...totals.entries()].filter(([, n]) => n > 0);
}

// Impact ----------------------------------------------------------------------

/** SEED_ROWS are the rows whose node items start a traversal (T019 "Impact
 * relation rows": C2 to C13, C16, C17 and C18). */
const SEED_ROWS = new Set(["C2", "C3", "C4", "C5", "C6", "C7", "C8", "C9", "C10", "C11", "C12", "C13", "C16", "C17", "C18"]);

export function isSeed(item: Pick<ChangeItem, "record" | "row">): boolean {
  return item.record === "node" && SEED_ROWS.has(item.row);
}

/** notSeedText says why an item starts no traversal. */
export function notSeedText(item: Pick<ChangeItem, "record" | "row">): string {
  if (item.record === "edge") return "An edge item starts no impact traversal; its endpoints' items do.";
  if (item.row === "C1") return "A file record starts no impact traversal (row C1); the items in the file do.";
  if (item.row === "C15") return "A knowledge gap is a fact about the runs (row C15); it starts no traversal.";
  if (item.row === "D") return "An unclassified record is never a seed of impact (row D).";
  return `Row ${item.row} items start no impact traversal.`;
}

export function capText(cap: string, depth: number): string {
  switch (cap) {
    case "depth":
      return `Depth cap reached: nodes beyond depth ${String(depth)} were not traversed.`;
    case "reached_nodes":
      return "Node cap reached: the traversal stopped at 2,000 reached nodes.";
    case "fan_out":
      return "Fan-out cap reached: a node had more than 200 dependents; the rest were not followed.";
    default:
      return `Cap reached: ${cap}`;
  }
}

/** notExtractedRelationText words an impact unknown of `not_extracted`
 * in a label style (the label table's row 21, T025b): the technical style
 * names the rule id, the plain style keeps it in the tooltip. */
export function notExtractedRelationText(relation: string, nodes: readonly string[], style: LabelStyle): Label {
  return label(style, "notExtracted", { relation, names: nodes.map(shortName) });
}
