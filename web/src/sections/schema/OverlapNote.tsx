// The model/migration overlap note (T018f R-9, 2.1, PD2): a model table
// whose name equals a table of a migration set is a link both ways, in both
// tables' details, with "not merged, not compared" and no verdict. The
// column lists (names only) are shown only when the API sends both (both
// tables complete); null lists say nothing about columns. A table hidden by
// Include tests is named with a "test, hidden" mark instead of a link.

import { ArrowLeftRight } from "lucide-react";

import type { Overlap, SchemaTable } from "@/api/schema";
import { fileLine, tableName } from "@/sections/schema/parts";

/** OverlapTarget is what the note knows of the other table from /sets. */
export interface OverlapTarget {
  table: SchemaTable | null;
  groupLabel: string;
  hiddenTest: boolean;
}

function names(list: string[]): string {
  return list.length === 0 ? "none" : list.join(", ");
}

/** OverlapNote renders one note per overlap. here is this table's source. */
export function OverlapNote({ overlaps, here, name, target, onTable }: { overlaps: readonly Overlap[]; here: "models" | "migrations"; name: string; target: (o: Overlap) => OverlapTarget; onTable: (id: string) => void }) {
  if (overlaps.length === 0) return null;
  return (
    <div className="grid gap-2" data-testid="overlap-notes">
      {overlaps.map((o) => {
        const t = target(o);
        const other = t.table;
        const otherName = o.name ?? (other !== null ? tableName(other.node) : name);
        const where = other !== null ? fileLine(other.node.file, other.node.span?.start_line) : null;
        const lists = o.columns_only_here !== null && o.columns_only_there !== null;
        const model = here === "models" ? o.columns_only_here : o.columns_only_there;
        const migration = here === "models" ? o.columns_only_there : o.columns_only_here;
        return (
          <div key={o.table} className="rounded-lg border border-ipc/40 bg-ipc-soft px-3 py-2 text-[13px]" data-testid="overlap-note" data-other={o.table}>
            <ArrowLeftRight aria-hidden="true" className="mr-1 inline size-3.5 align-[-2px]" />
            <b className="font-mono font-semibold">{t.groupLabel}</b> also defines a table named <span className="font-mono">{otherName}</span>
            {where !== null && <> (<span className="font-mono">{where}</span>)</>}.{" "}
            {t.hiddenTest ? (
              <span className="rounded-[3px] bg-dynamic-soft px-1 text-[11px] font-semibold text-dynamic" data-testid="overlap-hidden">
                test, hidden
              </span>
            ) : other !== null ? (
              <button type="button" className="text-brand-text underline underline-offset-[3px]" onClick={() => { onTable(o.table); }} data-testid="overlap-open">
                Open it
              </button>
            ) : (
              <span className="text-muted-foreground" title={o.table}>
                not in the loaded sets
              </span>
            )}
            . The two are not merged and not compared: which database this model maps to is runtime configuration.
            {lists && model !== null && migration !== null && (
              <span className="block" data-testid="overlap-columns">
                Names only: columns only in the model <span className="font-mono">{names(model)}</span>; only in the migration <span className="font-mono">{names(migration)}</span>.
              </span>
            )}
          </div>
        );
      })}
    </div>
  );
}
