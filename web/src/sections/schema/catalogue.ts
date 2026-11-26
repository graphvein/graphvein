// The assumption catalogue as the schema view reads it (T018f Part 2):
// GET /api/v0/schema/assumptions, loaded once per section, plus the pure
// rules the "Rests on" line, the row chips and the popovers share. The
// catalogue is the serving binary's; the API decides every value, so an id
// the catalogue lacks is shown raw ("not in this build's catalogue") and a
// catalogue that failed to load leaves the ids visible without texts.

import { createContext, useContext } from "react";

import type { Assumption, AssumptionCatalogue, Claim, ContractRow, SchemaTable } from "@/api/schema";

/** CatalogueView is the catalogue request as the view needs it. */
export type CatalogueView =
  | { kind: "loading" }
  | { kind: "failed"; message: string }
  | { kind: "ok"; data: AssumptionCatalogue; byId: Map<string, Assumption>; versionNote: string | null };

/** catalogueView builds the view of a loaded catalogue. indexVersion is
 * the index's extractor version: the catalogue is the binary's, so a
 * different index version gets a note (task 1.2). */
export function catalogueView(data: AssumptionCatalogue, indexVersion: string | null): CatalogueView {
  const byId = new Map(data.assumptions.map((a) => [a.id, a]));
  const differs = indexVersion !== null && data.extractor !== null && indexVersion !== data.extractor;
  const versionNote = differs ? `Assumptions of this Graphvein build (${data.extractor ?? "unknown"}); the index was built by ${indexVersion}.` : null;
  return { kind: "ok", data, byId, versionNote };
}

/** CatalogueContext carries the catalogue and the section's navigation to
 * the components that open popovers. */
export interface SchemaWhy {
  catalogue: CatalogueView;
  /** Opens the Assumptions tab on an entry (`a`), or at the top (null). */
  openEntry: (entry: string | null) => void;
  /** Opens the Migrations tab on a migration. */
  openMigration: (id: string) => void;
}

export const SchemaWhyContext = createContext<SchemaWhy>({
  catalogue: { kind: "loading" },
  openEntry: () => undefined,
  openMigration: () => undefined,
});

/** useSchemaWhy reads the section's catalogue and navigation. */
export function useSchemaWhy(): SchemaWhy {
  return useContext(SchemaWhyContext);
}

/** tableSource is the contract a table's rows come from: "models" for a
 * model table (stored source "model"), else "migrations". */
export function tableSource(t: Pick<SchemaTable, "node">): "models" | "migrations" {
  return t.node.attributes.source === "model" ? "models" : "migrations";
}

/** rowSource is the catalogue key of the contract rows behind a table's
 * facts: "migrations", or "models/<tool>" for a model table (each model
 * tool has its own rows, D included). A model table without a tool keeps
 * "models", which the catalogue does not hold: its row ids are shown raw. */
export function rowSource(t: Pick<SchemaTable, "node">, tool: string | null): string {
  if (tableSource(t) === "migrations") return "migrations";
  return tool === null ? "models" : `models/${tool}`;
}

/** contractRow looks a row up; null when the catalogue is not loaded or
 * lacks it. */
export function contractRow(c: CatalogueView, source: string, id: string): ContractRow | null {
  if (c.kind !== "ok") return null;
  return c.data.rows[source]?.[id] ?? null;
}

/** RowChip is what a grid or index row shows beside its name (R-3). */
export type RowChip =
  /** The claims or the table's rests_on are not recorded (pre-bump). */
  | { kind: "not_recorded" }
  /** The catalogue is not loaded (loading or failed): nothing to compare. */
  | { kind: "unavailable" }
  /** ids: the assumptions the row's claims add to its table's line;
   * unknownRows: claim rows the catalogue lacks (their assumptions are not
   * known, so they are shown raw). Both empty: no chip. */
  | { kind: "ids"; ids: string[]; unknownRows: string[] };

/** rowChip is the set difference of R-3: the union of the assumptions of
 * the rows behind the record's claims, minus the table's rests_on. */
export function rowChip(claims: readonly Claim[] | null, source: string, restsOn: readonly string[] | null, c: CatalogueView): RowChip {
  if (claims === null || restsOn === null) return { kind: "not_recorded" };
  if (c.kind !== "ok") return { kind: "unavailable" };
  const base = new Set(restsOn);
  const ids = new Set<string>();
  const unknownRows = new Set<string>();
  for (const cl of claims) {
    if (cl.row === null) {
      unknownRows.add("(row not recorded)");
      continue;
    }
    const row = contractRow(c, source, cl.row);
    if (row === null) {
      unknownRows.add(cl.row);
      continue;
    }
    for (const a of row.assumptions) if (!base.has(a)) ids.add(a);
  }
  return { kind: "ids", ids: [...ids].sort(), unknownRows: [...unknownRows].sort() };
}

/** rowsOfClaims lists the distinct contract rows behind a list of claims. */
export function rowsOfClaims(claims: readonly Claim[]): string[] {
  return [...new Set(claims.map((c) => c.row).filter((r): r is string => r !== null))].sort();
}

/** rowsResting lists the rows of a source whose assumptions include id
 * (the Assumptions tab's "rows that rest on it"). */
export function rowsResting(data: AssumptionCatalogue, id: string): { source: string; id: string }[] {
  const out: { source: string; id: string }[] = [];
  for (const [source, rows] of Object.entries(data.rows)) {
    for (const [rid, r] of Object.entries(rows)) if (r.assumptions.includes(id)) out.push({ source, id: rid });
  }
  return out;
}
