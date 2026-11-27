// Types, hand-written guards and requests for the schema endpoints
// (docs/cli/serve.md "Schema endpoints", ADR 0048):
//   GET /api/v0/schema/summary    the shell's cheap count (D1)
//   GET /api/v0/schema/sets       every group with its migrations and tables
//   GET /api/v0/schema/table      one table in detail
//   GET /api/v0/schema/migration  one migration in detail
//   GET /api/v0/schema/assumptions the assumption catalogue (T018f)
//
// The guards check the shape the view depends on and nothing else. Stored
// attributes stay Record<string, unknown> and are read only through the
// readers below, so a missing attribute or an unexpected JSON type reads
// as unknown, never as false, 0 or a default (VISION, principle 6). Derived
// string fields (completeness, default_state, objects, table_list, found,
// unknown_reason) are kept as any string: a value this UI does not know is
// rendered as unknown with the raw value, never mapped to a known one.

import {
  ApiFormatError,
  array,
  getJSON,
  object,
  parseIde,
  parseIdeStatus,
  parseIndex,
  parseNode,
  parseScopeLabel,
  str,
  strOrNull,
  version,
  type IdeLink,
  type IdeStatus,
  type IndexRun,
  type JSONObject,
  type PresentedNode,
  type ScopeLabel,
  type Signal,
  type Span,
} from "@/api/endpoints";
import { parseModelRoutes, type ModelRoutes } from "@/api/modelTables";

// Attribute readers ---------------------------------------------------------

/** attrString is the attribute when it is a string, else null (missing,
 * null and other types all read as "not a known string"). */
export function attrString(attrs: Record<string, unknown>, key: string): string | null {
  const v = attrs[key];
  return typeof v === "string" ? v : null;
}

/** attrInt is the attribute when it is an integer, else null. */
export function attrInt(attrs: Record<string, unknown>, key: string): number | null {
  const v = attrs[key];
  return typeof v === "number" && Number.isInteger(v) ? v : null;
}

/** A three-valued fact: known true, known false, stored null (not known),
 * or a missing attribute or another JSON type ("invalid"). Both null and
 * "invalid" are rendered as unknown; they are kept apart for tooltips. */
export type Bool3 = true | false | null | "invalid";

/** attrBool3 reads a true/false/null attribute without defaulting. */
export function attrBool3(attrs: Record<string, unknown>, key: string): Bool3 {
  if (!Object.hasOwn(attrs, key)) return "invalid";
  const v = attrs[key];
  if (v === true || v === false || v === null) return v;
  return "invalid";
}

/** A list of strings, stored null (not known), or anything else. */
export type StringList = { kind: "list"; items: string[] } | { kind: "null" } | { kind: "invalid"; raw: unknown };

/** attrStringList reads a list of strings (`primary_key`, `elements`,
 * `previous_names`). */
export function attrStringList(attrs: Record<string, unknown>, key: string): StringList {
  if (!Object.hasOwn(attrs, key)) return { kind: "invalid", raw: undefined };
  const v = attrs[key];
  if (v === null) return { kind: "null" };
  if (Array.isArray(v) && v.every((x) => typeof x === "string")) return { kind: "list", items: v };
  return { kind: "invalid", raw: v };
}

/** attrObjects reads a list of objects (`unique_keys`, `additional_references`);
 * null when the attribute is null, "invalid" for anything else. */
export function attrObjects(attrs: Record<string, unknown>, key: string): JSONObject[] | null | "invalid" {
  if (!Object.hasOwn(attrs, key)) return "invalid";
  const v = attrs[key];
  if (v === null) return null;
  if (Array.isArray(v) && v.every((x) => typeof x === "object" && x !== null && !Array.isArray(x))) return v as JSONObject[];
  return "invalid";
}

/** rawText writes a raw value for a tooltip. */
export function rawText(v: unknown): string {
  if (v === undefined) return "missing";
  try {
    return JSON.stringify(v);
  } catch {
    return "unprintable value";
  }
}

// Types ---------------------------------------------------------------------

/** SchemaSummary is GET /api/v0/schema/summary. Counts are null when the
 * repository is not indexed. */
export interface SchemaSummary {
  api_version: number;
  repository: string;
  index: IndexRun | null;
  found: string;
  unknown_reason: string | null;
  sets: number | null;
  test_only_sets: number | null;
  failed_sets: number | null;
  sets_without_records: number | null;
  unreadable_manifests: number | null;
  scan_gaps: number | null;
  /** T018f: the number of model groups; null when not indexed or sent by
   * an older server (never 0 unproven). */
  model_groups: number | null;
  /** T018f: the sources whose pass the index proves ran; null when not
   * known (older server, not indexed, another indexer). */
  sources_read: string[] | null;
  /** T018f: schema tools found but not read; null when not known. */
  unread_tools: UnreadTool[] | null;
  /** T070: model sources a linker found and refused as a whole, with the
   * stored message naming the reason; null when not known (not indexed,
   * an older server). [] never means every source was read. */
  refused_sources: RefusedSource[] | null;
}

/** RefusedSource is one whole-source refusal of the summary (T070): the
 * model tool, the file the diagnostic is filed on, its code and its
 * message verbatim. */
export interface RefusedSource {
  tool: string;
  file: string;
  code: string;
  message: string;
}

/** UnreadTool is a schema tool the index holds files of but Graphvein
 * does not read yet (T018f 1.3, the Alembic marker rule). path null: a
 * configuration whose script directory was not located; files null: not
 * counted. Unknown tools are kept as sent. */
export interface UnreadTool {
  tool: string;
  path: string | null;
  marker: string | null;
  files: number | null;
}

/** A stored claim (T018f 1.1): which contract row set a fact and where.
 * link and migration_ref come only with /table. A model claim (T018g0)
 * names its file instead of a migration and carries verification:
 * "oracle" (checked by the ORM's oracle) or "mapped" (from the ORM's own
 * per-dialect mapping, not checked against that database). */
export interface Claim {
  fact: string;
  row: string | null;
  migration: string | null;
  file: string | null;
  span: Span | null;
  link: IdeLink | null;
  migration_ref: JSONObject | null;
  verification: string | null;
  /** ADR 0065: the dialect of a per-dialect fact's claim; null for a
   * single fact (the key is omitted then). */
  dialect: string | null;
}

/** An opaque statement after a table's CREATE (T018f): the statement's
 * migration, span, contract row and kind; link only with /table and
 * /migration. */
export interface OpaqueSite {
  migration: string | null;
  span: Span | null;
  row: string | null;
  kind: string | null;
  link: IdeLink | null;
}

/** A same-name model/migration pair (T018f, L5): the other table. Column
 * lists are null when either side is not complete (never [] unproven).
 * completeness and name come only with /table. */
export interface Overlap {
  table: string;
  set: string | null;
  source: string | null;
  columns_only_here: string[] | null;
  columns_only_there: string[] | null;
  completeness: string | null;
  name: string | null;
}

/** A model group's file (T018f): its tables and the linker's unread and
 * core-table counts (null: present but not readable). */
export interface ModelFile {
  file: string;
  tables: number | null;
  unread: number | null;
  core_tables: number | null;
}

/** A stored node as /sets returns it: no IDE link and no file state
 * (file_state is null here; the view marks changed files from the polled
 * status instead). */
export type StoredNode = PresentedNode;

export interface SchemaColumn {
  node: StoredNode;
  default_state: string;
}

export interface SchemaTable {
  node: StoredNode;
  completeness: string;
  created_by: string | null;
  altered_by: string[];
  columns: SchemaColumn[];
  indexes: StoredNode[];
  foreign_keys_out: number;
  foreign_keys_in: number;
  unresolved_references: number;
  /** T018f: the assumptions every fact of the table rests on; null when
   * not recorded (pre-bump index, older server) or no fact is stated. */
  rests_on: string[] | null;
  /** T018f: same-name tables of the other source; null when not sent. */
  overlaps: Overlap[] | null;
  /** T018f: the opaque statements behind opaque_after; null when not
   * recorded or not complete (never a partial list). */
  opaque_sites: OpaqueSite[] | null;
  /** T018g0 Revision 1: a model table's column claims counted by
   * verification ("oracle", "mapped", ...); /sets serves no model claims
   * themselves (/table does). null on a migration table, an older index or
   * an older server. */
  verification_counts: Record<string, number> | null;
}

export interface SchemaMigration {
  id: string;
  file: string;
  attributes: Record<string, unknown>;
  creates: string[];
  alters: string[];
}

export interface EdgeFacts {
  kind: string;
  provenance: string;
  confidence: number;
}

export interface ForeignKeyPair {
  source_table: string;
  source_column: string;
  target_table: string | null;
  target_column: string;
  edge: EdgeFacts;
}

export interface SchemaGroup {
  set: string | null;
  source: string | null;
  tool: string | null;
  package: string | null;
  dialect: string | null;
  /** ADR 0065: a model group's run-time dialects (sorted), when every
   * table states the same list; null otherwise (then "dialects" may be
   * in conflicts). */
  dialects: string[] | null;
  conflicts: string[];
  objects: string | null;
  not_modeled_reasons: string[];
  table_list: string | null;
  table_list_reasons: string[];
  /** Verbatim from the set record: an integer, or null. */
  opaque_total: unknown;
  opaque_by_kind: unknown;
  test_only: boolean | null;
  diagnostic_counts: Record<string, number>;
  migrations: SchemaMigration[];
  tables: SchemaTable[];
  foreign_keys: ForeignKeyPair[];
  unattached: { columns: number; indexes: number } | null;
  linked_without_edge: number;
  /** T018f: the union of the assumptions behind the group's facts; null
   * when not recorded. */
  rests_on: string[] | null;
  /** T018f O3: tables by scope; null when not sent. */
  tables_by_scope: { production: number; test: number } | null;
  /** Model groups (T018f): pins verbatim (an object, or null with
   * pins_conflict), the tables' stored assumptions and the files. All
   * null on a migration group or an older server. */
  pins: unknown;
  pins_conflict: boolean | null;
  assumptions: string[] | null;
  files: ModelFile[] | null;
  /** T020: the group's scope label from the server; null when not sent. */
  precision: ScopeLabel | null;
  /** T024c1: the platform a migration set relies on without creating its
   * objects (Supabase, assumption SB-V2); null when the server sends none. */
  platform: Platform | null;
}

/** Platform is a set's environment assumption about platform objects. */
export interface Platform {
  name: string;
  assumption: string;
  objects: string[];
  /** T024c1b: the set's further platform assumptions (SB-V3 a bare
   * supabase/migrations directory, SB-V4 each platform extension the set
   * creates); empty when the server sends none. */
  assumptions: PlatformAssumption[];
}

/** PlatformAssumption is one further environment assumption of a set. */
export interface PlatformAssumption {
  id: string;
  text: string;
}

function readPlatform(value: unknown): Platform | null {
  if (value === null || typeof value !== "object" || Array.isArray(value)) return null;
  const o = value as Record<string, unknown>;
  if (typeof o.name !== "string" || typeof o.assumption !== "string" || !Array.isArray(o.objects)) return null;
  const assumptions: PlatformAssumption[] = [];
  if (Array.isArray(o.assumptions)) {
    for (const a of o.assumptions as unknown[]) {
      if (a === null || typeof a !== "object" || Array.isArray(a)) continue;
      const r = a as Record<string, unknown>;
      if (typeof r.id === "string" && typeof r.text === "string") assumptions.push({ id: r.id, text: r.text });
    }
  }
  return { name: o.name, assumption: o.assumption, objects: (o.objects as unknown[]).filter((x): x is string => typeof x === "string"), assumptions };
}

export interface FailedSet {
  package: string;
  message: string;
}

export interface RecordlessSet {
  package: string;
  diagnostic_counts: Record<string, number>;
}

/** An unreadable manifest or a scan gap: a path the index did not read. */
export interface UnreadPath {
  file: string;
  code: string;
  message: string;
}

export interface SchemaSets {
  api_version: number;
  repository: string;
  index: IndexRun | null;
  sets: SchemaGroup[];
  failed_sets: FailedSet[];
  sets_without_records: RecordlessSet[];
  unreadable_manifests: UnreadPath[];
  scan_gaps: UnreadPath[];
  /** T018f: the summary's unread tools; null when not known. */
  unread_tools: UnreadTool[] | null;
  /** B1 reasons that hold for the whole repository (python_unread,
   * python_scan_gap), named on the "No model table claimed" card; null
   * when the model pass did not run or the server predates the field. */
  model_list_reasons: string[] | null;
}

export interface Evidence {
  file: string;
  span: Span | null;
}

export interface ColumnLink {
  target_column: string;
  target_table: string | null;
  edge: EdgeFacts & { evidence: Evidence[] };
}

export interface TableColumn {
  node: PresentedNode;
  default_state: string;
  foreign_keys: ColumnLink[];
  /** T018f: the stored claims with link; null on a pre-bump index. */
  claims: Claim[] | null;
}

/** A migration named by a history event; version, order and description
 * are verbatim (any JSON value). */
export interface MigrationRef {
  id: string;
  file: string;
  version: unknown;
  order: unknown;
  description: unknown;
}

export interface HistoryEvent {
  migration: MigrationRef;
  event: string;
  evidence: Evidence[];
}

export interface Neighbour {
  table: StoredNode;
  completeness: string;
  pairs: { source_column: string; target_column: string }[];
}

export interface TableDetail {
  api_version: number;
  repository: string;
  index: IndexRun | null;
  ide: IdeStatus;
  set: string | null;
  dialect: string | null;
  /** ADR 0065: the table's run-time dialects; null when not sent. */
  dialects: string[] | null;
  table: PresentedNode;
  completeness: string;
  columns: TableColumn[];
  indexes: PresentedNode[];
  /** T018f: each index's claims (with link) by index identity; null
   * entries on a pre-bump index. */
  index_claims: Record<string, Claim[] | null>;
  history: HistoryEvent[];
  /** The stored reasons verbatim, each with migration_ref; read with the
   * attribute readers. */
  reasons: JSONObject[];
  neighbours: { outgoing: Neighbour[]; incoming: Neighbour[] };
  unresolved_references: { column: string; reference: unknown }[];
  truncated: { tables_omitted: number; pairs_omitted: number; reasons: string[] } | null;
  /** T018f, as on /sets. */
  rests_on: string[] | null;
  overlaps: Overlap[] | null;
  opaque_sites: OpaqueSite[] | null;
  /** T020: the label of the table's /sets group; null when not sent. */
  precision: ScopeLabel | null;
  /** T029 (MT-20..MT-29): the routes that accept or return the table's
   * model; null for a migration table (or an older server). */
  model_routes: ModelRoutes | null;
}

export interface TableRef {
  id: string;
  name: unknown;
  completeness: string;
}

export interface MigrationDetail {
  api_version: number;
  repository: string;
  index: IndexRun | null;
  ide: IdeStatus;
  set: string | null;
  migration: PresentedNode;
  previous: string | null;
  next: string | null;
  creates: TableRef[];
  alters: (TableRef & { evidence: Evidence[] })[];
  diagnostics: { severity: string; code: string; message: string }[];
  diagnostics_total: number;
  /** T018f: the migration's opaque statements with link; null when not
   * recorded. */
  opaque_sites: OpaqueSite[] | null;
}

/** One assumption of the catalogue (T018f /assumptions). */
export interface Assumption {
  id: string;
  source: string;
  title: string;
  text: string;
  /** The assumption this one is folded into, or null when in force. */
  folded_into: string | null;
}

/** One contract row of the catalogue: its form (branch), its claim
 * (summary) and the assumptions its facts rest on. branch is null for a
 * model row (T018f Revision 2, C3: the model contract names no form a user
 * reads). */
export interface ContractRow {
  source: string;
  /** The model tool whose contract holds the row; null for migration rows. */
  tool: string | null;
  branch: string | null;
  summary: string;
  assumptions: string[];
}

/** AssumptionCatalogue is GET /api/v0/schema/assumptions: the serving
 * binary's catalogue. rows is keyed by row source, then row id (the
 * contracts share ids): "migrations", and "models/<tool>" per model tool,
 * because every model contract has its own default row D. */
export interface AssumptionCatalogue {
  api_version: number;
  repository: string;
  index: IndexRun | null;
  extractor: string | null;
  assumptions: Assumption[];
  rows: Record<string, Record<string, ContractRow>>;
}

// Guards --------------------------------------------------------------------

function countOrNull(obj: JSONObject, key: string, path: string): number | null {
  const v = obj[key];
  if (v === null) return null;
  if (typeof v !== "number" || !Number.isInteger(v) || v < 0) throw new ApiFormatError(`${path}.${key} is not a count or null`);
  return v;
}

function count(obj: JSONObject, key: string, path: string): number {
  const v = countOrNull(obj, key, path);
  if (v === null) throw new ApiFormatError(`${path}.${key} is not a count`);
  return v;
}

function strings(value: unknown, path: string): string[] {
  const list = array(value, path);
  if (!list.every((s) => typeof s === "string")) throw new ApiFormatError(`${path} is not a list of strings`);
  return list;
}

function counts(value: unknown, path: string): Record<string, number> {
  const obj = object(value, path);
  for (const [k, v] of Object.entries(obj)) {
    if (typeof v !== "number" || !Number.isInteger(v) || v < 0) throw new ApiFormatError(`${path}.${k} is not a count`);
  }
  return obj as Record<string, number>;
}

function boolOrNull(obj: JSONObject, key: string, path: string): boolean | null {
  const v = obj[key];
  if (v === null || typeof v === "boolean") return v;
  throw new ApiFormatError(`${path}.${key} is not a boolean or null`);
}

/** storedNode parses a node without the presentation fields (/sets,
 * neighbours): file_state reads null, ide null. */
function storedNode(value: unknown, path: string): StoredNode {
  const obj = object(value, path);
  return parseNode({ ...obj, file_state: obj.file_state ?? null, ide: obj.ide ?? null }, path);
}

function edgeFacts(value: unknown, path: string): EdgeFacts {
  const obj = object(value, path);
  const c = obj.confidence;
  if (typeof c !== "number") throw new ApiFormatError(`${path}.confidence is not a number`);
  return { kind: str(obj, "kind", path), provenance: str(obj, "provenance", path), confidence: c };
}

function parseSpanOrNull(value: unknown, path: string): Span | null {
  if (value === null || value === undefined) return null;
  const obj = object(value, path);
  const n = (k: string) => {
    const v = obj[k];
    if (typeof v !== "number") throw new ApiFormatError(`${path}.${k} is not a number`);
    return v;
  };
  return { start_line: n("start_line"), start_column: n("start_column"), end_line: n("end_line"), end_column: n("end_column") };
}

function evidence(value: unknown, path: string): Evidence[] {
  if (value === null || value === undefined) return [];
  return array(value, path).map((e, i) => {
    const p = `${path}[${String(i)}]`;
    const o = object(e, p);
    return { file: str(o, "file", p), span: parseSpanOrNull(o.span, `${p}.span`) };
  });
}

// Lenient readers for the T018f fields. An absent field (an older server
// or a pre-bump index) or a value of another shape reads null, which the
// view shows as unknown or "not recorded"; never [] or 0 (principle: show
// an absence only where the API proves it). A list with one unreadable
// entry reads null as a whole: never a partial list.

/** stringsOrNull reads a list of strings, else null. */
export function stringsOrNull(v: unknown): string[] | null {
  return Array.isArray(v) && v.every((x) => typeof x === "string") ? v : null;
}

function countLenient(v: unknown): number | null {
  return typeof v === "number" && Number.isInteger(v) && v >= 0 ? v : null;
}

function strLenient(v: unknown): string | null {
  return typeof v === "string" ? v : null;
}

function plainObject(v: unknown): JSONObject | null {
  return v !== null && typeof v === "object" && !Array.isArray(v) ? (v as JSONObject) : null;
}

/** spanLenient reads a span, else null. */
export function spanLenient(v: unknown): Span | null {
  const o = plainObject(v);
  if (o === null) return null;
  const n = (k: string) => (typeof o[k] === "number" ? o[k] : null);
  const sl = n("start_line");
  const sc = n("start_column");
  const el = n("end_line");
  const ec = n("end_column");
  if (sl === null || sc === null || el === null || ec === null) return null;
  return { start_line: sl, start_column: sc, end_line: el, end_column: ec };
}

/** readLink reads an IDE link object, else null. */
export function readLink(v: unknown): IdeLink | null {
  return linkLenient(v);
}

function linkLenient(v: unknown): IdeLink | null {
  try {
    return parseIde(v, "link");
  } catch {
    return null;
  }
}

function listOf<T>(v: unknown, read: (o: JSONObject) => T | null): T[] | null {
  if (!Array.isArray(v)) return null;
  const out: T[] = [];
  for (const x of v) {
    const o = plainObject(x);
    const r = o === null ? null : read(o);
    if (r === null) return null;
    out.push(r);
  }
  return out;
}

/** readClaims reads a claims list (stored attribute or /table entry). */
export function readClaims(v: unknown): Claim[] | null {
  return listOf(v, (o) => {
    const fact = strLenient(o.fact);
    if (fact === null) return null;
    return { fact, row: strLenient(o.row), migration: strLenient(o.migration), file: strLenient(o.file), span: spanLenient(o.span), link: linkLenient(o.link), migration_ref: plainObject(o.migration_ref), verification: strLenient(o.verification), dialect: strLenient(o.dialect) };
  });
}

/** readOpaqueSites reads an opaque_sites list. */
export function readOpaqueSites(v: unknown): OpaqueSite[] | null {
  return listOf(v, (o) => ({ migration: strLenient(o.migration), span: spanLenient(o.span), row: strLenient(o.row), kind: strLenient(o.kind), link: linkLenient(o.link) }));
}

function readOverlaps(v: unknown): Overlap[] | null {
  return listOf(v, (o) => {
    const table = strLenient(o.table);
    if (table === null) return null;
    return {
      table,
      set: strLenient(o.set),
      source: strLenient(o.source),
      columns_only_here: stringsOrNull(o.columns_only_here),
      columns_only_there: stringsOrNull(o.columns_only_there),
      completeness: strLenient(o.completeness),
      name: strLenient(o.name),
    };
  });
}

/** readUnreadTools reads an unread_tools list. */
export function readUnreadTools(v: unknown): UnreadTool[] | null {
  return listOf(v, (o) => {
    const tool = strLenient(o.tool);
    if (tool === null) return null;
    return { tool, path: strLenient(o.path), marker: strLenient(o.marker), files: countLenient(o.files) };
  });
}

/** readRefusedSources reads refused_sources leniently: null when absent
 * or not a list (an older server: not known), and null when an entry
 * lacks a string tool, file, code or message (never a partial list). */
export function readRefusedSources(v: unknown): RefusedSource[] | null {
  return listOf(v, (o) => {
    const tool = strLenient(o.tool);
    const file = strLenient(o.file);
    const code = strLenient(o.code);
    const message = strLenient(o.message);
    if (tool === null || file === null || code === null || message === null) return null;
    return { tool, file, code, message };
  });
}

function readModelFiles(v: unknown): ModelFile[] | null {
  return listOf(v, (o) => {
    const file = strLenient(o.file);
    if (file === null) return null;
    return { file, tables: countLenient(o.tables), unread: countLenient(o.unread), core_tables: countLenient(o.core_tables) };
  });
}

/** verificationCounts reads verification_counts: an object of counts, or
 * null when absent or malformed (never a guessed count). */
function verificationCounts(v: unknown): Record<string, number> | null {
  const o = plainObject(v);
  if (o === null) return null;
  const out: Record<string, number> = {};
  for (const [k, raw] of Object.entries(o)) {
    const n = countLenient(raw);
    if (n === null) return null;
    out[k] = n;
  }
  return out;
}

function tablesByScope(v: unknown): { production: number; test: number } | null {
  const o = plainObject(v);
  if (o === null) return null;
  const production = countLenient(o.production);
  const test = countLenient(o.test);
  return production === null || test === null ? null : { production, test };
}

/** parseSchemaSummary checks a summary body. */
export function parseSchemaSummary(value: unknown): SchemaSummary {
  const p = "summary";
  const obj = object(value, p);
  return {
    api_version: version(obj, p),
    repository: str(obj, "repository", p),
    index: parseIndex(obj.index, `${p}.index`),
    found: str(obj, "found", p),
    unknown_reason: strOrNull(obj, "unknown_reason", p),
    sets: countOrNull(obj, "sets", p),
    test_only_sets: countOrNull(obj, "test_only_sets", p),
    failed_sets: countOrNull(obj, "failed_sets", p),
    // Fields added by T018b1's review fixes: an older server omits them,
    // which reads as unknown (null), never as 0.
    sets_without_records: obj.sets_without_records === undefined ? null : countOrNull(obj, "sets_without_records", p),
    unreadable_manifests: obj.unreadable_manifests === undefined ? null : countOrNull(obj, "unreadable_manifests", p),
    scan_gaps: obj.scan_gaps === undefined ? null : countOrNull(obj, "scan_gaps", p),
    model_groups: countLenient(obj.model_groups),
    sources_read: stringsOrNull(obj.sources_read),
    unread_tools: readUnreadTools(obj.unread_tools),
    refused_sources: readRefusedSources(obj.refused_sources),
  };
}

function parseGroup(value: unknown, path: string): SchemaGroup {
  const g = object(value, path);
  const un = g.unattached;
  let unattached: SchemaGroup["unattached"] = null;
  if (un !== null && un !== undefined) {
    const u = object(un, `${path}.unattached`);
    unattached = { columns: count(u, "columns", `${path}.unattached`), indexes: count(u, "indexes", `${path}.unattached`) };
  }
  return {
    set: strOrNull(g, "set", path),
    source: strOrNull(g, "source", path),
    tool: strOrNull(g, "tool", path),
    package: strOrNull(g, "package", path),
    dialect: strOrNull(g, "dialect", path),
    dialects: stringsOrNull(g.dialects),
    conflicts: strings(g.conflicts, `${path}.conflicts`),
    objects: strOrNull(g, "objects", path),
    not_modeled_reasons: strings(g.not_modeled_reasons, `${path}.not_modeled_reasons`),
    table_list: strOrNull(g, "table_list", path),
    table_list_reasons: strings(g.table_list_reasons, `${path}.table_list_reasons`),
    opaque_total: g.opaque_total,
    opaque_by_kind: g.opaque_by_kind,
    test_only: boolOrNull(g, "test_only", path),
    diagnostic_counts: counts(g.diagnostic_counts, `${path}.diagnostic_counts`),
    migrations: array(g.migrations, `${path}.migrations`).map((m, i) => {
      const mp = `${path}.migrations[${String(i)}]`;
      const o = object(m, mp);
      return {
        id: str(o, "id", mp),
        file: str(o, "file", mp),
        attributes: object(o.attributes, `${mp}.attributes`),
        creates: strings(o.creates, `${mp}.creates`),
        alters: strings(o.alters, `${mp}.alters`),
      };
    }),
    tables: array(g.tables, `${path}.tables`).map((t, i) => {
      const tp = `${path}.tables[${String(i)}]`;
      const o = object(t, tp);
      return {
        node: storedNode(o.node, `${tp}.node`),
        completeness: str(o, "completeness", tp),
        created_by: strOrNull(o, "created_by", tp),
        altered_by: strings(o.altered_by, `${tp}.altered_by`),
        columns: array(o.columns, `${tp}.columns`).map((c, j) => {
          const cp = `${tp}.columns[${String(j)}]`;
          const co = object(c, cp);
          return { node: storedNode(co.node, `${cp}.node`), default_state: str(co, "default_state", cp) };
        }),
        indexes: array(o.indexes, `${tp}.indexes`).map((x, j) => storedNode(x, `${tp}.indexes[${String(j)}]`)),
        foreign_keys_out: count(o, "foreign_keys_out", tp),
        foreign_keys_in: count(o, "foreign_keys_in", tp),
        unresolved_references: count(o, "unresolved_references", tp),
        rests_on: stringsOrNull(o.rests_on),
        overlaps: readOverlaps(o.overlaps),
        opaque_sites: readOpaqueSites(o.opaque_sites),
        verification_counts: verificationCounts(o.verification_counts),
      };
    }),
    foreign_keys: array(g.foreign_keys, `${path}.foreign_keys`).map((f, i) => {
      const fp = `${path}.foreign_keys[${String(i)}]`;
      const o = object(f, fp);
      return {
        source_table: str(o, "source_table", fp),
        source_column: str(o, "source_column", fp),
        target_table: strOrNull(o, "target_table", fp),
        target_column: str(o, "target_column", fp),
        edge: edgeFacts(o.edge, `${fp}.edge`),
      };
    }),
    unattached,
    linked_without_edge: count(g, "linked_without_edge", path),
    rests_on: stringsOrNull(g.rests_on),
    tables_by_scope: tablesByScope(g.tables_by_scope),
    pins: g.pins === undefined ? null : g.pins,
    pins_conflict: typeof g.pins_conflict === "boolean" ? g.pins_conflict : null,
    assumptions: stringsOrNull(g.assumptions),
    files: readModelFiles(g.files),
    precision: parseScopeLabel(g.precision, `${path}.precision`),
    platform: readPlatform(g.platform),
  };
}

function unreadPaths(value: unknown, path: string): UnreadPath[] {
  // Absent on a server before T018b1's review fixes: an empty list would
  // claim "none", so it is a format error instead.
  return array(value, path).map((e, i) => {
    const p = `${path}[${String(i)}]`;
    const o = object(e, p);
    return { file: str(o, "file", p), code: str(o, "code", p), message: str(o, "message", p) };
  });
}

/** parseSchemaSets checks a /sets body. */
export function parseSchemaSets(value: unknown): SchemaSets {
  const p = "sets";
  const obj = object(value, p);
  return {
    api_version: version(obj, p),
    repository: str(obj, "repository", p),
    index: parseIndex(obj.index, `${p}.index`),
    sets: array(obj.sets, `${p}.sets`).map((g, i) => parseGroup(g, `${p}.sets[${String(i)}]`)),
    failed_sets: array(obj.failed_sets, `${p}.failed_sets`).map((f, i) => {
      const fp = `${p}.failed_sets[${String(i)}]`;
      const o = object(f, fp);
      return { package: str(o, "package", fp), message: str(o, "message", fp) };
    }),
    sets_without_records: array(obj.sets_without_records, `${p}.sets_without_records`).map((f, i) => {
      const fp = `${p}.sets_without_records[${String(i)}]`;
      const o = object(f, fp);
      return { package: str(o, "package", fp), diagnostic_counts: counts(o.diagnostic_counts, `${fp}.diagnostic_counts`) };
    }),
    unreadable_manifests: unreadPaths(obj.unreadable_manifests, `${p}.unreadable_manifests`),
    scan_gaps: unreadPaths(obj.scan_gaps, `${p}.scan_gaps`),
    unread_tools: readUnreadTools(obj.unread_tools),
    model_list_reasons: obj.model_list_reasons === undefined || obj.model_list_reasons === null ? null : strings(obj.model_list_reasons, `${p}.model_list_reasons`),
  };
}

function neighbours(value: unknown, path: string): Neighbour[] {
  return array(value, path).map((n, i) => {
    const p = `${path}[${String(i)}]`;
    const o = object(n, p);
    return {
      table: storedNode(o.table, `${p}.table`),
      completeness: str(o, "completeness", p),
      pairs: array(o.pairs, `${p}.pairs`).map((x, j) => {
        const xp = `${p}.pairs[${String(j)}]`;
        const xo = object(x, xp);
        return { source_column: str(xo, "source_column", xp), target_column: str(xo, "target_column", xp) };
      }),
    };
  });
}

/** parseTableDetail checks a /table body. */
export function parseTableDetail(value: unknown): TableDetail {
  const p = "table";
  const obj = object(value, p);
  const nb = object(obj.neighbours, `${p}.neighbours`);
  let truncated: TableDetail["truncated"] = null;
  if (obj.truncated !== null) {
    const t = object(obj.truncated, `${p}.truncated`);
    truncated = {
      tables_omitted: count(t, "tables_omitted", `${p}.truncated`),
      pairs_omitted: count(t, "pairs_omitted", `${p}.truncated`),
      reasons: strings(t.reasons, `${p}.truncated.reasons`),
    };
  }
  return {
    api_version: version(obj, p),
    repository: str(obj, "repository", p),
    index: parseIndex(obj.index, `${p}.index`),
    ide: parseIdeStatus(obj.ide, `${p}.ide`),
    set: strOrNull(obj, "set", p),
    dialect: strOrNull(obj, "dialect", p),
    dialects: stringsOrNull(obj.dialects),
    table: parseNode(obj.table, `${p}.table`),
    completeness: str(obj, "completeness", p),
    columns: array(obj.columns, `${p}.columns`).map((c, i) => {
      const cp = `${p}.columns[${String(i)}]`;
      const o = object(c, cp);
      return {
        node: parseNode(o.node, `${cp}.node`),
        default_state: str(o, "default_state", cp),
        foreign_keys: array(o.foreign_keys, `${cp}.foreign_keys`).map((f, j) => {
          const fp = `${cp}.foreign_keys[${String(j)}]`;
          const fo = object(f, fp);
          const e = object(fo.edge, `${fp}.edge`);
          return {
            target_column: str(fo, "target_column", fp),
            target_table: strOrNull(fo, "target_table", fp),
            edge: { ...edgeFacts(e, `${fp}.edge`), evidence: evidence(e.evidence, `${fp}.edge.evidence`) },
          };
        }),
        claims: readClaims(o.claims),
      };
    }),
    indexes: array(obj.indexes, `${p}.indexes`).map((x, i) => parseNode(object(x, `${p}.indexes[${String(i)}]`).node, `${p}.indexes[${String(i)}].node`)),
    index_claims: Object.fromEntries(
      array(obj.indexes, `${p}.indexes`).map((x, i) => {
        const o = object(x, `${p}.indexes[${String(i)}]`);
        return [parseNode(o.node, `${p}.indexes[${String(i)}].node`).identity, readClaims(o.claims)];
      }),
    ),
    history: array(obj.history, `${p}.history`).map((h, i) => {
      const hp = `${p}.history[${String(i)}]`;
      const o = object(h, hp);
      const m = object(o.migration, `${hp}.migration`);
      return {
        migration: { id: str(m, "id", `${hp}.migration`), file: str(m, "file", `${hp}.migration`), version: m.version, order: m.order, description: m.description },
        event: str(o, "event", hp),
        evidence: evidence(o.evidence, `${hp}.evidence`),
      };
    }),
    reasons: array(obj.reasons, `${p}.reasons`).map((r, i) => object(r, `${p}.reasons[${String(i)}]`)),
    neighbours: { outgoing: neighbours(nb.outgoing, `${p}.neighbours.outgoing`), incoming: neighbours(nb.incoming, `${p}.neighbours.incoming`) },
    unresolved_references: array(obj.unresolved_references, `${p}.unresolved_references`).map((u, i) => {
      const up = `${p}.unresolved_references[${String(i)}]`;
      const o = object(u, up);
      return { column: str(o, "column", up), reference: o.reference };
    }),
    truncated,
    rests_on: stringsOrNull(obj.rests_on),
    overlaps: readOverlaps(obj.overlaps),
    opaque_sites: readOpaqueSites(obj.opaque_sites),
    precision: parseScopeLabel(obj.precision, `${p}.precision`),
    model_routes: parseModelRoutes(obj.model_routes, `${p}.model_routes`),
  };
}

function tableRef(value: unknown, path: string): TableRef {
  const o = object(value, path);
  return { id: str(o, "id", path), name: o.name, completeness: str(o, "completeness", path) };
}

/** parseMigrationDetail checks a /migration body. */
export function parseMigrationDetail(value: unknown): MigrationDetail {
  const p = "migration";
  const obj = object(value, p);
  return {
    api_version: version(obj, p),
    repository: str(obj, "repository", p),
    index: parseIndex(obj.index, `${p}.index`),
    ide: parseIdeStatus(obj.ide, `${p}.ide`),
    set: strOrNull(obj, "set", p),
    migration: parseNode(obj.migration, `${p}.migration`),
    previous: strOrNull(obj, "previous", p),
    next: strOrNull(obj, "next", p),
    creates: array(obj.creates, `${p}.creates`).map((c, i) => tableRef(c, `${p}.creates[${String(i)}]`)),
    alters: array(obj.alters, `${p}.alters`).map((c, i) => {
      const ap = `${p}.alters[${String(i)}]`;
      return { ...tableRef(c, ap), evidence: evidence(object(c, ap).evidence, `${ap}.evidence`) };
    }),
    diagnostics: array(obj.diagnostics, `${p}.diagnostics`).map((d, i) => {
      const dp = `${p}.diagnostics[${String(i)}]`;
      const o = object(d, dp);
      return { severity: str(o, "severity", dp), code: str(o, "code", dp), message: str(o, "message", dp) };
    }),
    diagnostics_total: count(obj, "diagnostics_total", p),
    opaque_sites: readOpaqueSites(obj.opaque_sites),
  };
}

/** parseAssumptions checks an /assumptions body. Entries and rows that do
 * not have the shape are a format error: the catalogue is the binary's own
 * table, so a malformed one is a server fault, not an unknown fact. */
export function parseAssumptions(value: unknown): AssumptionCatalogue {
  const p = "assumptions";
  const obj = object(value, p);
  const rows: Record<string, Record<string, ContractRow>> = {};
  for (const [source, byId] of Object.entries(object(obj.rows, `${p}.rows`))) {
    const out: Record<string, ContractRow> = {};
    for (const [id, r] of Object.entries(object(byId, `${p}.rows.${source}`))) {
      const rp = `${p}.rows.${source}.${id}`;
      const o = object(r, rp);
      out[id] = { source: str(o, "source", rp), tool: strOrNull(o, "tool", rp), branch: strOrNull(o, "branch", rp), summary: str(o, "summary", rp), assumptions: strings(o.assumptions, `${rp}.assumptions`) };
    }
    rows[source] = out;
  }
  return {
    api_version: version(obj, p),
    repository: str(obj, "repository", p),
    index: parseIndex(obj.index, `${p}.index`),
    extractor: strOrNull(obj, "extractor", p),
    assumptions: array(obj.assumptions, `${p}.assumptions`).map((a, i) => {
      const ap = `${p}.assumptions[${String(i)}]`;
      const o = object(a, ap);
      return { id: str(o, "id", ap), source: str(o, "source", ap), title: str(o, "title", ap), text: str(o, "text", ap), folded_into: strOrNull(o, "folded_into", ap) };
    }),
    rows,
  };
}

// Requests ------------------------------------------------------------------

export const SCHEMA_SUMMARY_PATH = "/api/v0/schema/summary";
export const SCHEMA_SETS_PATH = "/api/v0/schema/sets";
export const SCHEMA_ASSUMPTIONS_PATH = "/api/v0/schema/assumptions";

/** SchemaDetailParams name one table or migration. /schema/summary and
 * /schema/sets present no nodes, so only the details take an editor. */
export interface SchemaDetailParams {
  id: string;
  /** The editor chosen in this browser (ADR 0050); absent: none. */
  editor?: string;
}

function detailQuery(p: SchemaDetailParams): string {
  const editor = p.editor !== undefined ? `&editor=${encodeURIComponent(p.editor)}` : "";
  return `?id=${encodeURIComponent(p.id)}${editor}`;
}

export function tablePath(p: SchemaDetailParams): string {
  return `/api/v0/schema/table${detailQuery(p)}`;
}

export function migrationPath(p: SchemaDetailParams): string {
  return `/api/v0/schema/migration${detailQuery(p)}`;
}

/** SchemaClient is what the schema view requests; tests pass their own. */
export interface SchemaClient {
  summary(options: Signal): Promise<SchemaSummary>;
  sets(options: Signal): Promise<SchemaSets>;
  table(params: SchemaDetailParams, options: Signal): Promise<TableDetail>;
  migration(params: SchemaDetailParams, options: Signal): Promise<MigrationDetail>;
  assumptions(options: Signal): Promise<AssumptionCatalogue>;
}

/** httpSchemaClient requests the server that served the page; 503
 * index_changed is retried once (getJSON). */
export const httpSchemaClient: SchemaClient = {
  summary: (o) => getJSON(SCHEMA_SUMMARY_PATH, parseSchemaSummary, o),
  sets: (o) => getJSON(SCHEMA_SETS_PATH, parseSchemaSets, o),
  table: (p, o) => getJSON(tablePath(p), parseTableDetail, o),
  migration: (p, o) => getJSON(migrationPath(p), parseMigrationDetail, o),
  assumptions: (o) => getJSON(SCHEMA_ASSUMPTIONS_PATH, parseAssumptions, o),
};
