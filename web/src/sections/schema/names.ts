// Name lookups over the loaded /sets answer: the detail endpoints return
// identities (target columns, neighbour pairs, unresolved references), and
// the view names them from the same index run's tables and columns. An
// identity /sets does not hold is shown verbatim, never guessed from its
// text.

import type { SchemaSets } from "@/api/schema";
import { tableName } from "@/sections/schema/parts";

export interface Names {
  table: (id: string) => string | null;
  column: (id: string) => { table: string | null; tableId: string | null; column: string } | null;
}

/** names indexes every table and column of every group (hidden test-only
 * groups too: a reference may cross into one). */
export function names(sets: SchemaSets): Names {
  const tables = new Map<string, string>();
  const columns = new Map<string, { table: string; tableId: string; column: string }>();
  for (const g of sets.sets) {
    for (const t of g.tables) {
      const tn = tableName(t.node);
      tables.set(t.node.identity, tn);
      for (const c of t.columns) {
        const n = c.node.attributes.name;
        columns.set(c.node.identity, { table: tn, tableId: t.node.identity, column: typeof n === "string" ? n : c.node.name });
      }
    }
  }
  return {
    table: (id) => tables.get(id) ?? null,
    column: (id) => columns.get(id) ?? null,
  };
}

/** columnLabel names a column identity "table.column", else the identity. */
export function columnLabel(n: Names, id: string): string {
  const c = n.column(id);
  return c === null ? id : `${c.table ?? "?"}.${c.column}`;
}
