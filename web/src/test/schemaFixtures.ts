// The Go server's schema goldens (internal/server/testdata, T018b1),
// imported so the guards and views are tested against what the server
// really sends, plus crafted variants for states no golden holds (each
// marked "crafted" with the golden it extends; T018b2 A3).
import assumptionsGoldenJSON from "../../../internal/server/testdata/schema-assumptions.golden.json";
import setsModels from "../../../internal/server/testdata/schema-sets-models.golden.json";
import setsModelDialects from "../../../internal/server/testdata/schema-sets-model-dialects.golden.json";
import summaryModels from "../../../internal/server/testdata/schema-summary-models.golden.json";
import tableModel from "../../../internal/server/testdata/schema-table-model.golden.json";
import tableModelDialects from "../../../internal/server/testdata/schema-table-model-dialects.golden.json";
import tableModelOverlap from "../../../internal/server/testdata/schema-table-model-overlap.golden.json";
import tableModelPartial from "../../../internal/server/testdata/schema-table-model-partial.golden.json";
import tableModelRoutes from "../../../internal/server/testdata/schema-table-model-routes.golden.json";
import tableModelRoutesNone from "../../../internal/server/testdata/schema-table-model-routes-none.golden.json";
import tableModelUnresolved from "../../../internal/server/testdata/schema-table-model-unresolved.golden.json";
import migrationApplied from "../../../internal/server/testdata/schema-migration-applied.golden.json";
import migrationDown from "../../../internal/server/testdata/schema-migration-down.golden.json";
import migrationSqlite from "../../../internal/server/testdata/schema-migration-sqlite-set.golden.json";
import setsFixture from "../../../internal/server/testdata/schema-sets-fixture.golden.json";
import setsStates from "../../../internal/server/testdata/schema-states-sets.golden.json";
import tableStates from "../../../internal/server/testdata/schema-states-table.golden.json";
import summaryFixture from "../../../internal/server/testdata/schema-summary-fixture.golden.json";
import summaryNotIndexed from "../../../internal/server/testdata/schema-summary-not-indexed.golden.json";
import summaryStates from "../../../internal/server/testdata/schema-summary-states.golden.json";
import tableComplete from "../../../internal/server/testdata/schema-table-complete.golden.json";
import tablePartial from "../../../internal/server/testdata/schema-table-partial.golden.json";
import tablePartialNotes from "../../../internal/server/testdata/schema-table-partial-notes.golden.json";
import tableRenamed from "../../../internal/server/testdata/schema-table-renamed-altered.golden.json";
import tableSelf from "../../../internal/server/testdata/schema-table-self-reference.golden.json";
import tableSerial from "../../../internal/server/testdata/schema-table-serial.golden.json";
import setsSupabase from "../../../internal/server/testdata/schema-sets-supabase.golden.json";
import summarySupabase from "../../../internal/server/testdata/schema-summary-supabase.golden.json";
import tableSupabase from "../../../internal/server/testdata/schema-table-supabase.golden.json";

import { ApiError } from "@/api/client";
import {
  parseAssumptions,
  parseMigrationDetail,
  parseSchemaSets,
  parseSchemaSummary,
  parseTableDetail,
  type AssumptionCatalogue,
  type MigrationDetail,
  type SchemaClient,
  type SchemaSets,
  type SchemaSummary,
  type TableDetail,
} from "@/api/schema";

export const SUMMARY_GOLDENS = { fixture: summaryFixture, states: summaryStates, not_indexed: summaryNotIndexed, models: summaryModels, supabase: summarySupabase } as const;
export const SETS_GOLDENS = { fixture: setsFixture, states: setsStates, models: setsModels, model_dialects: setsModelDialects, supabase: setsSupabase } as const;
export const TABLE_GOLDENS = {
  "schema-states-table": tableStates,
  "schema-table-complete": tableComplete,
  "schema-table-partial": tablePartial,
  "schema-table-partial-notes": tablePartialNotes,
  "schema-table-renamed-altered": tableRenamed,
  "schema-table-self-reference": tableSelf,
  "schema-table-serial": tableSerial,
  "schema-table-model": tableModel,
  "schema-table-model-dialects": tableModelDialects,
  "schema-table-model-overlap": tableModelOverlap,
  "schema-table-model-partial": tableModelPartial,
  "schema-table-model-routes": tableModelRoutes,
  "schema-table-model-routes-none": tableModelRoutesNone,
  "schema-table-model-unresolved": tableModelUnresolved,
  "schema-table-supabase": tableSupabase,
} as const;

/** assumptionsGolden is the server's catalogue golden (T018f). */
export function assumptionsGolden(): AssumptionCatalogue {
  return parseAssumptions(raw(assumptionsGoldenJSON));
}
export const MIGRATION_GOLDENS = {
  "schema-migration-applied": migrationApplied,
  "schema-migration-down": migrationDown,
  "schema-migration-sqlite-set": migrationSqlite,
} as const;

/** raw returns a mutable deep copy of a golden's JSON. */
export function raw<T>(golden: T): T {
  return structuredClone(golden);
}

export function summaryGolden(name: keyof typeof SUMMARY_GOLDENS): SchemaSummary {
  return parseSchemaSummary(raw(SUMMARY_GOLDENS[name]));
}

export function setsGolden(name: keyof typeof SETS_GOLDENS): SchemaSets {
  return parseSchemaSets(raw(SETS_GOLDENS[name]));
}

/** crafted (extends schema-summary-fixture): a summary with the given
 * fields, for the unknown reasons, none, test-only and failed-only rows. */
export function craftedSummary(patch: Partial<SchemaSummary>): SchemaSummary {
  return { ...summaryGolden("fixture"), sets: 0, test_only_sets: 0, failed_sets: 0, sets_without_records: 0, unreadable_manifests: 0, scan_gaps: 0, ...patch };
}

/** crafted (extends schema-states-sets): the states response with every
 * group test-only (Q5). */
export function craftedAllTestOnly(): SchemaSets {
  const s = setsGolden("states");
  return { ...s, sets: s.sets.filter((g) => g.set !== null).map((g) => ({ ...g, test_only: true })) };
}

/** crafted (extends schema-states-sets): no group, only the record-less,
 * failed and unreadable entries, plus scan gaps (Q6 failed-only with
 * Revision 1's lists; no golden has a scan gap). */
export function craftedFailedOnly(): SchemaSets {
  const s = setsGolden("states");
  return {
    ...s,
    sets: [],
    scan_gaps: [
      { file: "vendor-link", code: "symlink", message: "vendor-link: symlink not followed" },
      { file: "", code: "invalid_path", message: "an entry with an unsupported path was skipped" },
    ],
  };
}

/** crafted (extends schema-sets-fixture's pg-serial group): one group of
 * 26 tables in a chain t00 → t01 → … → t25 (Q7 neighbourhood mode). */
export function craftedBigSet(): SchemaSets {
  const s = setsGolden("fixture");
  const base = s.sets.find((g) => g.set === "pg-serial/migrations");
  if (base === undefined) throw new Error("fixture lost pg-serial");
  const proto = base.tables[0];
  if (proto === undefined) throw new Error("pg-serial has no table");
  const id = (i: number) => `table:big/migrations/1.sql#t${String(i).padStart(2, "0")}`;
  const col = (i: number) => `column:big/migrations/1.sql#t${String(i).padStart(2, "0")}.id`;
  const tables = Array.from({ length: 26 }, (_, i) => ({
    ...proto,
    node: { ...proto.node, identity: id(i), name: `t${String(i).padStart(2, "0")}`, qualified_name: `t${String(i).padStart(2, "0")}`, attributes: { ...proto.node.attributes, name: `t${String(i).padStart(2, "0")}` } },
    columns: proto.columns.map((c) => ({ ...c, node: { ...c.node, identity: col(i) } })),
    indexes: [],
  }));
  const foreign_keys = Array.from({ length: 25 }, (_, i) => ({
    source_table: id(i),
    source_column: col(i),
    target_table: id(i + 1),
    target_column: col(i + 1),
    edge: { kind: "foreign_key", provenance: "fact", confidence: 1 },
  }));
  return { ...s, sets: [{ ...base, set: "big/migrations", tables, foreign_keys }], failed_sets: [], sets_without_records: [], unreadable_manifests: [] };
}

/** fakeSchemaClient answers from goldens; table and migration details are
 * looked up by identity in the given maps, else 404. */
export function fakeSchemaClient(opts: {
  summary: SchemaSummary | (() => Promise<SchemaSummary>);
  sets?: SchemaSets | (() => Promise<SchemaSets>);
  tables?: Record<string, TableDetail>;
  migrations?: Record<string, MigrationDetail>;
  /** The catalogue; default: the server's golden. */
  assumptions?: AssumptionCatalogue | (() => Promise<AssumptionCatalogue>);
  calls?: string[];
  /** Details not in the maps never answer (the view keeps showing the
   * /sets facts) instead of 404. */
  pending?: boolean;
}): SchemaClient {
  const log = (s: string) => opts.calls?.push(s);
  return {
    summary: () => {
      log("summary");
      return typeof opts.summary === "function" ? opts.summary() : Promise.resolve(structuredClone(opts.summary));
    },
    sets: () => {
      log("sets");
      const s = opts.sets;
      if (s === undefined) return Promise.reject(new Error("no sets"));
      return typeof s === "function" ? s() : Promise.resolve(structuredClone(s));
    },
    table: ({ id, editor }) => {
      log(`table:${id}${editor !== undefined ? `@${editor}` : ""}`);
      const t = opts.tables?.[id];
      if (t === undefined && opts.pending === true) return new Promise<never>(() => undefined);
      return t === undefined ? Promise.reject(new ApiError("http", `not_found: no table with identity ${id}`, 404, "not_found")) : Promise.resolve(structuredClone(t));
    },
    migration: ({ id, editor }) => {
      log(`migration:${id}${editor !== undefined ? `@${editor}` : ""}`);
      const m = opts.migrations?.[id];
      if (m === undefined && opts.pending === true) return new Promise<never>(() => undefined);
      return m === undefined ? Promise.reject(new ApiError("http", `not_found: no migration with identity ${id}`, 404, "not_found")) : Promise.resolve(structuredClone(m));
    },
    assumptions: () => {
      log("assumptions");
      const a = opts.assumptions ?? assumptionsGolden();
      return typeof a === "function" ? a() : Promise.resolve(structuredClone(a));
    },
  };
}

export function tableGolden(name: keyof typeof TABLE_GOLDENS): TableDetail {
  return parseTableDetail(raw(TABLE_GOLDENS[name]));
}

export function migrationGolden(name: keyof typeof MIGRATION_GOLDENS): MigrationDetail {
  return parseMigrationDetail(raw(MIGRATION_GOLDENS[name]));
}
