// Types, hand-written guards and requests for the change endpoints
// (docs/cli/serve.md "Change endpoints", ADR 0056):
//   GET  /api/v0/changes/bases        journal runs, pins, ref snapshots
//   GET  /api/v0/changes/summary      both sides and the counts per group
//   GET  /api/v0/changes/items        one page of items
//   GET  /api/v0/changes/impact       the traversal from one item
//   POST /api/v0/changes/pins(/delete) pin and unpin a journal run
//   POST and GET /api/v0/changes/refs  index a branch comparison
//
// The guards check the shape the views depend on. Stored fields (an item
// side's `fields`, a changed field's before and after, raw payloads) stay
// unknown and are only printed, so a value this UI does not know is shown
// as sent, never mapped to a known one. Strings the contract may extend
// (change kinds, causes, reasons, wordings, labels, caps, relations) are
// kept as any string; the views fall back to the raw value.

import { parseLinkAssumptions, type LinkAssumption } from "@/api/architecture";
import type { PrefixUnknownPath } from "@/lib/routePath";
import { ApiError, fetchJSON, requestRepository } from "@/api/client";
import {
  ApiFormatError,
  array,
  bool,
  getJSON,
  object,
  parseIde,
  parseFileLabel,
  parseIndex,
  parseScopeLabel,
  str,
  strOrNull,
  version,
  type FileLabel,
  type IdeLink,
  type IndexRun,
  type ScopeLabel,
  type JSONObject,
  type Signal,
  type Span,
} from "@/api/endpoints";
import { KINDS, type ProvenanceKind } from "@/api/routeShape";

// Readers --------------------------------------------------------------------

function strings(value: unknown, path: string): string[] {
  const list = array(value, path);
  if (!list.every((s) => typeof s === "string")) throw new ApiFormatError(`${path} is not a list of strings`);
  return list;
}

function count(obj: JSONObject, key: string, path: string): number {
  const v = obj[key];
  if (typeof v !== "number" || !Number.isInteger(v) || v < 0) throw new ApiFormatError(`${path}.${key} is not a count`);
  return v;
}

function counts(value: unknown, path: string): Record<string, number> {
  const obj = object(value, path);
  const out: Record<string, number> = {};
  for (const key of Object.keys(obj)) out[key] = count(obj, key, path);
  return out;
}

function numOrNull(obj: JSONObject, key: string, path: string): number | null {
  const v = obj[key];
  if (v === null || v === undefined) return null;
  if (typeof v !== "number" || !Number.isFinite(v)) throw new ApiFormatError(`${path}.${key} is not a number or null`);
  return v;
}

function countOrNull(obj: JSONObject, key: string, path: string): number | null {
  const v = obj[key];
  if (v === null || v === undefined) return null;
  return count(obj, key, path);
}

/** optString reads a string member the server may omit (omitempty). */
function optString(obj: JSONObject, key: string, path: string): string | null {
  const v = obj[key];
  if (v === undefined || v === null) return null;
  if (typeof v !== "string") throw new ApiFormatError(`${path}.${key} is not a string`);
  return v;
}

/** optBool reads true, false, or null/absent (not known). */
function optBool(obj: JSONObject, key: string, path: string): boolean | null {
  const v = obj[key];
  if (v === undefined || v === null) return null;
  if (typeof v !== "boolean") throw new ApiFormatError(`${path}.${key} is not a boolean`);
  return v;
}

function parseSpan(value: unknown, path: string): Span | null {
  if (value === null || value === undefined) return null;
  const obj = object(value, path);
  return {
    start_line: count(obj, "start_line", path),
    start_column: count(obj, "start_column", path),
    end_line: count(obj, "end_line", path),
    end_column: count(obj, "end_column", path),
  };
}

function indexer(value: unknown, path: string): { name: string; version: string } {
  const obj = object(value, path);
  return { name: str(obj, "name", path), version: str(obj, "version", path) };
}

// Shared header ---------------------------------------------------------------

/** Freshness as /api/v0/status computes it (the change endpoints reuse one
 * check for up to 2 s). Only the state and the changed files are read. */
export interface ChangesFreshness {
  state: string;
  changed_count: number | null;
  changed_files: string[];
}

/** ChangesHeader starts every GET answer. */
export interface ChangesHeader {
  api_version: number;
  repository: string;
  index: IndexRun | null;
  freshness: ChangesFreshness;
}

function parseHeader(obj: JSONObject, path: string): ChangesHeader {
  const f = object(obj.freshness, `${path}.freshness`);
  return {
    api_version: version(obj, path),
    repository: str(obj, "repository", path),
    index: parseIndex(obj.index, `${path}.index`),
    freshness: {
      state: str(f, "state", `${path}.freshness`),
      changed_count: countOrNull(f, "changed_count", `${path}.freshness`),
      changed_files: strings(f.changed_files, `${path}.freshness.changed_files`),
    },
  };
}

// Bases -----------------------------------------------------------------------

/** ChangeRun is one journal run. commit and dirty are null when the run
 * recorded none (an index made outside git). */
export interface ChangeRun {
  seq: number;
  run_id: string;
  previous_run: string | null;
  initial: boolean;
  indexer: { name: string; version: string };
  commit: string | null;
  dirty: boolean | null;
  finished_at: string;
  gaps: string[];
  gaps_truncated: boolean;
  gaps_fileless: boolean;
}

export interface Pin {
  id: string;
  name: string;
  seq: number;
  created_at: string;
  run: ChangeRun;
}

export interface RefSnapshot {
  namespace: string;
  commit: string;
  extractor: string;
  created_at: string;
  last_used_at: string;
  config_source: string;
  run: ChangeRun;
}

export interface Retention {
  journal_runs: number;
  journal_budget: number;
  max_pins: number;
  max_ref_snapshots: number;
}

/** ChangesBases is GET /api/v0/changes/bases. */
export interface ChangesBases extends ChangesHeader {
  runs: ChangeRun[];
  pins: Pin[];
  refs: RefSnapshot[];
  retention: Retention;
}

function parseRun(value: unknown, path: string): ChangeRun {
  const obj = object(value, path);
  const seq = obj.seq;
  if (typeof seq !== "number" || !Number.isInteger(seq)) throw new ApiFormatError(`${path}.seq is not an integer`);
  return {
    seq,
    run_id: str(obj, "run_id", path),
    previous_run: optString(obj, "previous_run", path),
    initial: optBool(obj, "initial", path) === true,
    indexer: indexer(obj.indexer, `${path}.indexer`),
    commit: optString(obj, "commit", path),
    dirty: optBool(obj, "dirty", path),
    finished_at: str(obj, "finished_at", path),
    gaps: obj.gaps === undefined ? [] : strings(obj.gaps, `${path}.gaps`),
    gaps_truncated: optBool(obj, "gaps_truncated", path) === true,
    gaps_fileless: optBool(obj, "gaps_fileless", path) === true,
  };
}

/** parsePin reads one pin (bases and the POST pins answer). */
export function parsePin(value: unknown, path: string): Pin {
  const obj = object(value, path);
  const seq = obj.seq;
  if (typeof seq !== "number" || !Number.isInteger(seq)) throw new ApiFormatError(`${path}.seq is not an integer`);
  return { id: str(obj, "id", path), name: str(obj, "name", path), seq, created_at: str(obj, "created_at", path), run: parseRun(obj.run, `${path}.run`) };
}

export function parseBases(value: unknown): ChangesBases {
  const obj = object(value, "bases");
  const r = object(obj.retention, "bases.retention");
  const budget = r.journal_budget;
  if (typeof budget !== "number") throw new ApiFormatError("bases.retention.journal_budget is not a number");
  return {
    ...parseHeader(obj, "bases"),
    runs: array(obj.runs, "bases.runs").map((v, i) => parseRun(v, `bases.runs[${String(i)}]`)),
    pins: array(obj.pins, "bases.pins").map((v, i) => parsePin(v, `bases.pins[${String(i)}]`)),
    refs: array(obj.refs, "bases.refs").map((v, i) => {
      const p = `bases.refs[${String(i)}]`;
      const o = object(v, p);
      return {
        namespace: str(o, "namespace", p),
        commit: str(o, "commit", p),
        extractor: str(o, "extractor", p),
        created_at: str(o, "created_at", p),
        last_used_at: str(o, "last_used_at", p),
        config_source: str(o, "config_source", p),
        run: parseRun(o.run, `${p}.run`),
      };
    }),
    retention: {
      journal_runs: count(r, "journal_runs", "bases.retention"),
      journal_budget: budget,
      max_pins: count(r, "max_pins", "bases.retention"),
      max_ref_snapshots: count(r, "max_ref_snapshots", "bases.retention"),
    },
  };
}

// Summary ---------------------------------------------------------------------

/** SideInfo is one side of a comparison. kind is "run", "pin", "ref" or
 * "current"; commit null: the side recorded none; dirty null: not known. */
export interface SideInfo {
  kind: string;
  repository: string;
  pin: string | null;
  run: string;
  finished_at: string;
  commit: string | null;
  dirty: boolean | null;
  extractor: { name: string; version: string };
  partial: boolean;
  partial_reasons: string[];
  gaps: string[];
  gaps_truncated: boolean;
}

/** GroupCount counts one group's items and what was counted but not
 * listed (moves, other changes, context, hidden tests). */
export interface GroupCount {
  group: string;
  items: number;
  changes: Record<string, number>;
  causes: Record<string, number>;
  moves: number;
  other_changes: number;
  context: number;
  hidden_tests: number;
  kinds: Record<string, number>;
}

export interface Summary {
  contract_version: string;
  base: SideInfo;
  head: SideInfo;
  extractor_changed: boolean;
  head_commit_differs: boolean;
  partial: boolean;
  groups: GroupCount[];
  locations: number;
  file_metadata_only: number;
  /** Groups the index does not extract: never zero, never "no change". */
  not_extracted: string[];
  /** O7: the ref base that separates an extractor upgrade from relinking. */
  extractor_recheck: string | null;
  total: number;
}

/** ChangesSummary is GET /api/v0/changes/summary. */
export interface ChangesSummary extends ChangesHeader {
  base: string;
  summary: Summary;
}

function parseSide(value: unknown, path: string): SideInfo {
  const obj = object(value, path);
  return {
    kind: str(obj, "kind", path),
    repository: str(obj, "repository", path),
    pin: optString(obj, "pin", path),
    run: str(obj, "run", path),
    finished_at: str(obj, "finished_at", path),
    commit: optString(obj, "commit", path),
    dirty: optBool(obj, "dirty", path),
    extractor: indexer(obj.extractor, `${path}.extractor`),
    partial: bool(obj, "partial", path),
    partial_reasons: strings(obj.partial_reasons, `${path}.partial_reasons`),
    gaps: strings(obj.gaps, `${path}.gaps`),
    gaps_truncated: bool(obj, "gaps_truncated", path),
  };
}

/** parseSummaryBody reads the `summary` member (shared with the CLI
 * goldens, whose document has the same member). */
export function parseSummaryBody(value: unknown, path = "summary"): Summary {
  const s = object(value, path);
  return {
    contract_version: str(s, "contract_version", path),
    base: parseSide(s.base, `${path}.base`),
    head: parseSide(s.head, `${path}.head`),
    extractor_changed: bool(s, "extractor_changed", path),
    head_commit_differs: bool(s, "head_commit_differs", path),
    partial: bool(s, "partial", path),
    groups: array(s.groups, `${path}.groups`).map((v, i) => {
      const p = `${path}.groups[${String(i)}]`;
      const g = object(v, p);
      return {
        group: str(g, "group", p),
        items: count(g, "items", p),
        changes: counts(g.changes, `${p}.changes`),
        causes: counts(g.causes, `${p}.causes`),
        moves: count(g, "moves", p),
        other_changes: count(g, "other_changes", p),
        context: count(g, "context", p),
        hidden_tests: count(g, "hidden_tests", p),
        kinds: counts(g.kinds, `${p}.kinds`),
      };
    }),
    locations: count(s, "locations", path),
    file_metadata_only: count(s, "file_metadata_only", path),
    not_extracted: strings(s.not_extracted, `${path}.not_extracted`),
    extractor_recheck: strOrNull(s, "extractor_recheck", path),
    total: count(s, "total", path),
  };
}

export function parseSummary(value: unknown): ChangesSummary {
  const obj = object(value, "summary response");
  return { ...parseHeader(obj, "summary response"), base: str(obj, "base", "summary response"), summary: parseSummaryBody(obj.summary) };
}

// Items -----------------------------------------------------------------------

/** ItemSide is one side of an item: file, span and the stored fields the
 * row compares. ide is the head side's editor link (server-built, ADR
 * 0050): only when the head is the current index and the file is
 * unchanged since it was indexed. */
export interface ItemSide {
  file: string | null;
  span: Span | null;
  fields: JSONObject;
  ide: IdeLink | null;
  /** A route side that is a registration whose path the Go linker
   * withholds (couldn't-read row CR-44), from that side's diagnostics:
   * "path withheld" rather than "path not known". */
  path_withheld: boolean;
}

/** ChangedField is one changed field: change is "changed", "unknown_now"
 * or "known_now"; before and after are the stored values. */
export interface ChangedField {
  name: string;
  change: string;
  before: unknown;
  after: unknown;
}

/** Hint is an inferred move hint (C14): nothing follows it. */
export interface Hint {
  row: string;
  kind: string;
  other: string;
  basis: string;
  provenance: string;
}

export interface RawPayloads {
  before: unknown;
  after: unknown;
  related: { record: string; key: string; before: unknown; after: unknown }[];
}

export interface ChangeItem {
  id: string;
  group: string;
  change: string;
  row: string;
  record: string;
  identity: string;
  kind: string;
  name: string;
  before: ItemSide | null;
  after: ItemSide | null;
  fields: ChangedField[];
  labels: string[];
  reason: string | null;
  wording: string | null;
  cause: string | null;
  provenance: string | null;
  confidence: number | null;
  test: boolean;
  hints: Hint[];
  /** null until the item's impact was computed. */
  impact_count: number | null;
  raw: RawPayloads | null;
  /** T020: the item's labels as the server computed them; null when the
   * server sends none. */
  precision: ItemPrecision | null;
}

/** ItemPrecision is a change item's labels (docs/cli/serve.md
 * /changes/items "precision"): the item file's label, the record family's
 * scope label (null: the file label applies) and one label per changed
 * field. */
export interface ItemPrecision {
  file: FileLabel;
  scope: ScopeLabel | null;
  fields: Record<string, ScopeLabel | null>;
  /** The side whose run decided scope and fields: "base" when the item has
   * a base side and the base run is refused; null when not sent. */
  scope_side: "head" | "base" | null;
}

function parseItemPrecision(value: unknown, path: string): ItemPrecision | null {
  if (value === undefined || value === null) return null;
  const obj = object(value, path);
  const fields: Record<string, ScopeLabel | null> = {};
  if (obj.fields !== undefined && obj.fields !== null) {
    for (const [k, v] of Object.entries(object(obj.fields, `${path}.fields`))) fields[k] = parseScopeLabel(v, `${path}.fields.${k}`);
  }
  return {
    file: obj.file === undefined || obj.file === null ? { language: null, precision: null, reason: null, row: null, reported: null } : parseFileLabel(object(obj.file, `${path}.file`), `${path}.file`),
    scope: parseScopeLabel(obj.scope, `${path}.scope`),
    fields,
    scope_side: obj.scope_side === "head" || obj.scope_side === "base" ? obj.scope_side : null,
  };
}

/** ChangesItems is GET /api/v0/changes/items. */
export interface ChangesItems extends ChangesHeader {
  base: string;
  items: ChangeItem[];
  total: number;
  hidden_tests: number;
  next_cursor: string | null;
}

function parseItemSide(value: unknown, path: string): ItemSide | null {
  if (value === null || value === undefined) return null;
  const obj = object(value, path);
  return {
    file: optString(obj, "file", path),
    span: parseSpan(obj.span, `${path}.span`),
    fields: obj.fields === undefined || obj.fields === null ? {} : object(obj.fields, `${path}.fields`),
    ide: parseIde(obj.ide, `${path}.ide`),
    path_withheld: obj.path_withheld === true,
  };
}

/** parseItem reads one item (shared with the CLI goldens). */
export function parseItem(value: unknown, path: string): ChangeItem {
  const obj = object(value, path);
  let raw: RawPayloads | null = null;
  if (obj.raw !== undefined && obj.raw !== null) {
    const r = object(obj.raw, `${path}.raw`);
    raw = {
      before: r.before ?? null,
      after: r.after ?? null,
      related:
        r.related === undefined
          ? []
          : array(r.related, `${path}.raw.related`).map((v, i) => {
              const p = `${path}.raw.related[${String(i)}]`;
              const o = object(v, p);
              return { record: str(o, "record", p), key: str(o, "key", p), before: o.before ?? null, after: o.after ?? null };
            }),
    };
  }
  return {
    id: str(obj, "id", path),
    group: str(obj, "group", path),
    change: str(obj, "change", path),
    row: str(obj, "row", path),
    record: str(obj, "record", path),
    identity: str(obj, "identity", path),
    kind: str(obj, "kind", path),
    name: str(obj, "name", path),
    before: parseItemSide(obj.before, `${path}.before`),
    after: parseItemSide(obj.after, `${path}.after`),
    fields: array(obj.fields, `${path}.fields`).map((v, i) => {
      const p = `${path}.fields[${String(i)}]`;
      const f = object(v, p);
      return { name: str(f, "name", p), change: str(f, "change", p), before: f.before ?? null, after: f.after ?? null };
    }),
    labels: strings(obj.labels, `${path}.labels`),
    reason: strOrNull(obj, "reason", path),
    wording: strOrNull(obj, "wording", path),
    cause: strOrNull(obj, "cause", path),
    provenance: strOrNull(obj, "provenance", path),
    confidence: numOrNull(obj, "confidence", path),
    test: bool(obj, "test", path),
    hints: array(obj.hints, `${path}.hints`).map((v, i) => {
      const p = `${path}.hints[${String(i)}]`;
      const h = object(v, p);
      return { row: str(h, "row", p), kind: str(h, "kind", p), other: str(h, "other", p), basis: str(h, "basis", p), provenance: str(h, "provenance", p) };
    }),
    impact_count: countOrNull(obj, "impact_count", path),
    raw,
    precision: parseItemPrecision(obj.precision, `${path}.precision`),
  };
}

export function parseItems(value: unknown): ChangesItems {
  const obj = object(value, "items response");
  return {
    ...parseHeader(obj, "items response"),
    base: str(obj, "base", "items response"),
    items: array(obj.items, "items response.items").map((v, i) => parseItem(v, `items[${String(i)}]`)),
    total: count(obj, "total", "items response"),
    hidden_tests: count(obj, "hidden_tests", "items response"),
    next_cursor: strOrNull(obj, "next_cursor", "items response"),
  };
}

// Impact ----------------------------------------------------------------------

/** TableAccess is an I8 step's owner entry (T019 Revision 4). */
export interface TableAccess {
  tool: string;
  role: string;
  operations: string[];
  rows: string[];
  verification: string;
  verification_wording: string;
  assumes: string[];
  /** "not_extracted" when a column seed reached its table's users. */
  column_use: string | null;
}

/** ModelUse is one use of a model declaration by the route of an I16
 * step (T029 W2, rows MT-14, MT-15 of the route model types contract):
 * the shape section, response status and provenance kind of the using
 * item. */
export interface ModelUse {
  section: string;
  status: string | null;
  kind: ProvenanceKind;
  rule: string;
  file: string;
  line: number;
  confidence: number;
}

/** ModelStep is an I16 step's model: its name and the route's uses of
 * it (empty when the route stores none). */
export interface ModelStep {
  name: string;
  uses: ModelUse[];
}

export interface Step {
  kind: string;
  source: string;
  target: string;
  relation: string;
  provenance: string;
  confidence: number;
  evidence: { file: string; span: Span | null }[];
  via: string | null;
  /** The step went through a removed edge of the change set. */
  removed: boolean;
  access: TableAccess | null;
  /** The route's uses of the model on an I16 step; null on others. */
  model: ModelStep | null;
  /** T022 B9 (project change impact): the repositories at both ends of
   * the step and, for a project link, its assumption; null on a
   * single-repository answer. */
  source_repository: string | null;
  target_repository: string | null;
  assumption: string | null;
  /** A calls_route step's link assumptions (an origin nothing proves,
   * UP-V1, UP-V2; T033); empty on other steps. */
  link_assumptions: LinkAssumption[];
}

export interface ImpactPath {
  node: string;
  kind: string;
  seed: string;
  distance: number;
  relation: string;
  claim: string;
  wording: string;
  provenance: string;
  /** The weakest step's confidence; null for a path inferred only by its
   * relation (I15) or a gap while every step is a fact. */
  confidence: number | null;
  steps: Step[];
  /** A reached route's composed paths as stored ({template, scope}
   * entries; empty when no full path is known); null on other paths, or when the
   * server sends none. */
  route_paths: unknown[] | null;
  /** A reached route's paths whose prefix the code does not fix (T033
   * UP-32; never full paths, never in route_paths); empty when none. */
  prefix_unknown_paths: PrefixUnknownPath[];
  /** true when every composed path of a reached route is taken first by
   * an earlier mount (route attribute shadowed_paths): the route is
   * mounted, its paths withheld, so an empty route_paths is not "not
   * mounted". */
  route_shadowed: boolean;
  /** true when a reached route is a registration whose path the Go
   * linker withholds (couldn't-read row CR-44): found, its path not
   * claimed, so an empty route_paths reads "path withheld". */
  route_withheld: boolean;
  /** T022 B9 (project change impact): the member of the reached node and
   * the assumptions of the project links on the path; null and empty on a
   * single-repository answer. */
  repository: string | null;
  assumptions: string[];
}

export interface BlindSpot {
  file: string;
  span: Span | null;
  caller: string | null;
  name: string | null;
  matches: string[];
}

export interface Unknowns {
  blind_spots: BlindSpot[];
  near_misses: { route: string; call: string; status: string }[];
  gap_nodes: string[];
  untraversed: Record<string, number>;
  not_extracted: { relation: string; nodes: string[] }[];
}

export interface UnknownUse {
  owner: string;
  row: string;
  line: number | null;
  tool: string;
  reason: string;
  partial: boolean;
  provenance: string;
}

/** TableUse lists the users of one table the traversal read: found is a
 * lower bound ("uses found"), never "no users". */
export interface TableUse {
  table: string;
  wording: string;
  found: string[];
  may_also_wording: string;
  may_also_be_used_by: UnknownUse[];
}

/** ChangesImpact is GET /api/v0/changes/impact (one page of paths). */
export interface ChangesImpact extends ChangesHeader {
  base: string;
  seeds: string[];
  depth: number;
  paths: ImpactPath[];
  unknowns: Unknowns;
  caps: string[];
  table_uses: TableUse[];
  total: number;
  next_cursor: string | null;
}

function parseAccess(value: unknown, path: string): TableAccess | null {
  if (value === undefined || value === null) return null;
  const o = object(value, path);
  return {
    tool: str(o, "tool", path),
    role: str(o, "role", path),
    operations: strings(o.operations, `${path}.operations`),
    rows: strings(o.rows, `${path}.rows`),
    verification: str(o, "verification", path),
    verification_wording: str(o, "verification_wording", path),
    assumes: strings(o.assumes, `${path}.assumes`),
    column_use: optString(o, "column_use", path),
  };
}

function parseModel(value: unknown, path: string): ModelStep | null {
  if (value === undefined || value === null) return null;
  const o = object(value, path);
  return {
    name: str(o, "name", path),
    uses: array(o.uses, `${path}.uses`).map((v, i) => {
      const p = `${path}.uses[${String(i)}]`;
      const u = object(v, p);
      const line = u.line;
      const conf = u.confidence;
      if (typeof line !== "number" || !Number.isInteger(line)) throw new ApiFormatError(`${p}.line is not an integer`);
      if (typeof conf !== "number") throw new ApiFormatError(`${p}.confidence is not a number`);
      const kind = KINDS.find((k) => k === u.kind);
      if (kind === undefined) throw new ApiFormatError(`${p}.kind is not a shape provenance kind`);
      return {
        section: str(u, "section", p),
        status: strOrNull(u, "status", p),
        kind,
        rule: str(u, "rule", p),
        file: str(u, "file", p),
        line,
        confidence: conf,
      };
    }),
  };
}

function parseStep(value: unknown, path: string): Step {
  const o = object(value, path);
  const conf = o.confidence;
  if (typeof conf !== "number") throw new ApiFormatError(`${path}.confidence is not a number`);
  return {
    kind: str(o, "kind", path),
    source: str(o, "source", path),
    target: str(o, "target", path),
    relation: str(o, "relation", path),
    provenance: str(o, "provenance", path),
    confidence: conf,
    evidence: (o.evidence === null || o.evidence === undefined ? [] : array(o.evidence, `${path}.evidence`)).map((v, i) => {
      const p = `${path}.evidence[${String(i)}]`;
      const e = object(v, p);
      return { file: str(e, "file", p), span: parseSpan(e.span, `${p}.span`) };
    }),
    via: strOrNull(o, "via", path),
    removed: bool(o, "removed", path),
    access: parseAccess(o.access, `${path}.access`),
    model: parseModel(o.model, `${path}.model`),
    source_repository: optString(o, "source_repository", path),
    target_repository: optString(o, "target_repository", path),
    assumption: optString(o, "assumption", path),
    link_assumptions: parseLinkAssumptions(o.link_assumptions, `${path}.link_assumptions`),
  };
}

/** parsePrefixUnknownPaths reads an impact path's prefix_unknown_paths. */
function parsePrefixUnknownPaths(value: unknown, path: string): PrefixUnknownPath[] {
  if (value === undefined || value === null) return [];
  return array(value, path).map((v, i) => {
    const p = `${path}[${String(i)}]`;
    const o = object(v, p);
    return { path: str(o, "path", p), reason: str(o, "reason", p), hop: strOrNull(o, "hop", p), scope: o.scope === "test" ? "test" : "production" };
  });
}

/** parseImpactBody reads the traversal members (shared with the CLI
 * goldens' `impact`). */
export function parseImpactBody(obj: JSONObject, path: string): Omit<ChangesImpact, keyof ChangesHeader | "base" | "total" | "next_cursor"> {
  const u = object(obj.unknowns, `${path}.unknowns`);
  const untraversed = counts(u.untraversed, `${path}.unknowns.untraversed`);
  const depth = obj.depth;
  if (typeof depth !== "number" || !Number.isInteger(depth)) throw new ApiFormatError(`${path}.depth is not an integer`);
  return {
    seeds: strings(obj.seeds, `${path}.seeds`),
    depth,
    paths: array(obj.paths, `${path}.paths`).map((v, i) => {
      const p = `${path}.paths[${String(i)}]`;
      const o = object(v, p);
      const conf = o.confidence;
      const dist = o.distance;
      // null: a path inferred only by its relation (I15) or a gap while
      // every step is a fact has no confidence (T019 Revision 7).
      if (conf !== null && typeof conf !== "number") throw new ApiFormatError(`${p}.confidence is not a number or null`);
      if (typeof dist !== "number" || !Number.isInteger(dist)) throw new ApiFormatError(`${p}.distance is not an integer`);
      return {
        node: str(o, "node", p),
        kind: str(o, "kind", p),
        seed: str(o, "seed", p),
        distance: dist,
        relation: str(o, "relation", p),
        claim: str(o, "claim", p),
        wording: str(o, "wording", p),
        provenance: str(o, "provenance", p),
        confidence: conf,
        steps: array(o.steps, `${p}.steps`).map((s, j) => parseStep(s, `${p}.steps[${String(j)}]`)),
        route_paths: o.route_paths === undefined || o.route_paths === null ? null : array(o.route_paths, `${p}.route_paths`),
        prefix_unknown_paths: parsePrefixUnknownPaths(o.prefix_unknown_paths, `${p}.prefix_unknown_paths`),
        route_shadowed: o.route_shadowed === true,
        route_withheld: o.route_withheld === true,
        repository: optString(o, "repository", p),
        assumptions: o.assumptions === undefined || o.assumptions === null ? [] : strings(o.assumptions, `${p}.assumptions`),
      };
    }),
    unknowns: {
      blind_spots: array(u.blind_spots, `${path}.unknowns.blind_spots`).map((v, i) => {
        const p = `${path}.unknowns.blind_spots[${String(i)}]`;
        const o = object(v, p);
        return { file: str(o, "file", p), span: parseSpan(o.span, `${p}.span`), caller: strOrNull(o, "caller", p), name: strOrNull(o, "name", p), matches: o.matches === null ? [] : strings(o.matches, `${p}.matches`) };
      }),
      near_misses: array(u.near_misses, `${path}.unknowns.near_misses`).map((v, i) => {
        const p = `${path}.unknowns.near_misses[${String(i)}]`;
        const o = object(v, p);
        return { route: str(o, "route", p), call: str(o, "call", p), status: str(o, "status", p) };
      }),
      gap_nodes: strings(u.gap_nodes, `${path}.unknowns.gap_nodes`),
      untraversed,
      not_extracted: array(u.not_extracted, `${path}.unknowns.not_extracted`).map((v, i) => {
        const p = `${path}.unknowns.not_extracted[${String(i)}]`;
        const o = object(v, p);
        return { relation: str(o, "relation", p), nodes: o.nodes === null ? [] : strings(o.nodes, `${p}.nodes`) };
      }),
    },
    caps: strings(obj.caps, `${path}.caps`),
    table_uses: (obj.table_uses === undefined || obj.table_uses === null ? [] : array(obj.table_uses, `${path}.table_uses`)).map((v, i) => {
      const p = `${path}.table_uses[${String(i)}]`;
      const o = object(v, p);
      return {
        table: str(o, "table", p),
        wording: str(o, "wording", p),
        found: strings(o.found, `${p}.found`),
        may_also_wording: str(o, "may_also_wording", p),
        may_also_be_used_by: array(o.may_also_be_used_by, `${p}.may_also_be_used_by`).map((w, j) => {
          const q = `${p}.may_also_be_used_by[${String(j)}]`;
          const m = object(w, q);
          return {
            owner: str(m, "owner", q),
            row: str(m, "row", q),
            line: countOrNull(m, "line", q),
            tool: str(m, "tool", q),
            reason: str(m, "reason", q),
            partial: bool(m, "partial", q),
            provenance: str(m, "provenance", q),
          };
        }),
      };
    }),
  };
}

export function parseImpact(value: unknown): ChangesImpact {
  const obj = object(value, "impact");
  return {
    ...parseHeader(obj, "impact"),
    base: str(obj, "base", "impact"),
    ...parseImpactBody(obj, "impact"),
    total: count(obj, "total", "impact"),
    next_cursor: strOrNull(obj, "next_cursor", "impact"),
  };
}

// Refs ------------------------------------------------------------------------

/** RefJob is the latest branch-indexing job. state: "running",
 * "committed", "failed" (with error.code) or "cancelled". */
export interface RefJob {
  id: string;
  state: string;
  started_at: string;
  ended_at: string | null;
  base: string;
  commits: string[];
  indexed: string[];
  error: { code: string; message: string } | null;
}

export function parseRefs(value: unknown): RefJob | null {
  const obj = object(value, "refs");
  version(obj, "refs");
  if (obj.job === null || obj.job === undefined) return null;
  const j = object(obj.job, "refs.job");
  let error: RefJob["error"] = null;
  if (j.error !== null && j.error !== undefined) {
    const e = object(j.error, "refs.job.error");
    error = { code: str(e, "code", "refs.job.error"), message: str(e, "message", "refs.job.error") };
  }
  return {
    id: str(j, "id", "refs.job"),
    state: str(j, "state", "refs.job"),
    started_at: str(j, "started_at", "refs.job"),
    ended_at: strOrNull(j, "ended_at", "refs.job"),
    base: str(j, "base", "refs.job"),
    commits: strings(j.commits, "refs.job.commits"),
    indexed: strings(j.indexed, "refs.job.indexed"),
    error,
  };
}

// Requests --------------------------------------------------------------------

function query(params: [string, string | number | boolean | null | undefined][]): string {
  const parts: string[] = [];
  for (const [key, value] of params) {
    if (value === null || value === undefined || value === "" || value === false) continue;
    parts.push(`${key}=${encodeURIComponent(String(value))}`);
  }
  return parts.length === 0 ? "" : `?${parts.join("&")}`;
}

export interface ItemsParams {
  base: string;
  group?: string | null;
  change?: string | null;
  q?: string | null;
  tests?: boolean;
  raw?: boolean;
  cursor?: string | null;
  limit?: number;
}

export interface ImpactParams {
  base: string;
  id: string;
  depth?: number;
  cursor?: string | null;
  limit?: number;
}

export function summaryPath(base: string): string {
  return `/api/v0/changes/summary${query([["base", base]])}`;
}

export function itemsPath(p: ItemsParams): string {
  return `/api/v0/changes/items${query([
    ["base", p.base],
    ["group", p.group],
    ["change", p.change],
    ["q", p.q],
    ["tests", p.tests === true ? "true" : null],
    ["raw", p.raw === true ? "true" : null],
    ["cursor", p.cursor],
    ["limit", p.limit],
  ])}`;
}

/** PROJECT_IMPACT_PATH continues a member's change impact through the
 * project's links into the other members (T022 B8). */
export const PROJECT_IMPACT_PATH = "/api/v0/project/changes/impact";

/** impactPath is the member's change impact; in project mode (a request
 * repository is set) the project endpoint, which crosses repositories
 * through the stored links, except for a ref base, which links do not
 * name (the project endpoint refuses it). */
export function impactPath(p: ImpactParams): string {
  const project = requestRepository() !== null && !p.base.startsWith("ref:");
  return `${project ? PROJECT_IMPACT_PATH : "/api/v0/changes/impact"}${query([["base", p.base], ["id", p.id], ["depth", p.depth], ["cursor", p.cursor], ["limit", p.limit]])}`;
}

/** ChangesClient is what the compare chip and the Changes section
 * request; tests pass their own. */
export interface ChangesClient {
  bases(options: Signal): Promise<ChangesBases>;
  summary(base: string, options: Signal): Promise<ChangesSummary>;
  items(p: ItemsParams, options: Signal): Promise<ChangesItems>;
  impact(p: ImpactParams, options: Signal): Promise<ChangesImpact>;
  refs(options: Signal): Promise<RefJob | null>;
  startRefs(base: string, head: string | null, options: Signal): Promise<RefJob | null>;
  pin(name: string, run: string | null, options: Signal): Promise<Pin>;
  unpin(name: string, options: Signal): Promise<void>;
}

/** httpChangesClient requests the server that served the page. The
 * change endpoints answer 409 index_changed after their own retry;
 * getJSON retries once more, like every view read. */
export const httpChangesClient: ChangesClient = {
  bases: (o) => getJSON("/api/v0/changes/bases", parseBases, o),
  summary: (base, o) => getJSON(summaryPath(base), parseSummary, o),
  items: (p, o) => getJSON(itemsPath(p), parseItems, o),
  impact: (p, o) => getJSON(impactPath(p), parseImpact, o),
  refs: (o) => getJSON("/api/v0/changes/refs", parseRefs, o),
  startRefs: async (base, head, o) => {
    const body = await fetchJSON("/api/v0/changes/refs", { method: "POST", body: head === null ? { base } : { base, head }, ...o });
    return asFormat(() => parseRefs(body));
  },
  pin: async (name, run, o) => {
    const body = await fetchJSON("/api/v0/changes/pins", { method: "POST", body: run === null ? { name } : { name, run }, ...o });
    return asFormat(() => parsePin(object(object(body, "pin response").pin, "pin"), "pin"));
  },
  unpin: async (name, o) => {
    await fetchJSON("/api/v0/changes/pins/delete", { method: "POST", body: { name }, ...o });
  },
};

/** asFormat turns a malformed POST answer into the ApiError getJSON
 * gives for a malformed GET answer. */
function asFormat<T>(f: () => T): T {
  try {
    return f();
  } catch (err) {
    if (err instanceof ApiFormatError) throw new ApiError("format", err.message);
    throw err;
  }
}
