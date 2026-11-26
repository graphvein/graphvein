// The Model files tab (T018f 2.1): the Migrations tab's place for a model
// group. One row per file of the group's `files`: the tables it holds and
// the linker's unread and core-table counts. A count the API sends as null
// reads "unknown", never 0.

import type { SchemaGroup } from "@/api/schema";
import { cn } from "@/lib/utils";
import { tableName } from "@/sections/schema/parts";

function Count({ n, warn }: { n: number | null; warn?: boolean }) {
  if (n === null) return <i className="text-muted-foreground">unknown</i>;
  return <span className={cn("tabular-nums", warn === true && n > 0 && "font-semibold text-dynamic")}>{n}</span>;
}

/** ModelFilesTab lists a model group's files. */
export function ModelFilesTab({ group, onTable, hiddenIds }: { group: SchemaGroup; onTable: (id: string) => void; hiddenIds: Set<string> }) {
  const files = group.files;
  if (files === null)
    return (
      <div className="rounded-xl border bg-card px-6 py-8 text-center text-[13px] text-muted-foreground italic" data-testid="model-files-unknown">
        The files of this group are not known (the server did not send them).
      </div>
    );
  const th = "px-3 py-1.5 text-left text-xs font-medium text-muted-foreground";
  return (
    <div className="min-h-0 flex-1 overflow-auto rounded-xl border bg-card" data-testid="model-files">
      <table className="w-full border-collapse text-[13px]">
        <thead>
          <tr className="border-b">
            <th className={th}>File</th>
            <th className={th}>Tables</th>
            <th className={th}>Classes not read</th>
            <th className={th}>Core tables not read</th>
          </tr>
        </thead>
        <tbody>
          {files.map((f) => {
            const tables = group.tables.filter((t) => t.node.file === f.file);
            const hidden = (f.tables ?? 0) - tables.length;
            return (
              <tr key={f.file} className="border-b align-top last:border-b-0" data-testid="model-file" data-file={f.file}>
                <td className="px-3 py-1.5 font-mono text-[12.5px]">{f.file}</td>
                <td className="px-3 py-1.5">
                  <span className="flex flex-wrap gap-x-2 gap-y-0.5">
                    <Count n={f.tables} />
                    {tables.map((t) => (
                      <button key={t.node.identity} type="button" className="font-mono text-[12.5px] text-brand-text underline underline-offset-[3px]" onClick={() => { onTable(t.node.identity); }}>
                        {tableName(t.node)}
                      </button>
                    ))}
                    {hidden > 0 && [...hiddenIds].length > 0 && <span className="text-xs text-muted-foreground">{hidden} test, hidden</span>}
                  </span>
                </td>
                <td className="px-3 py-1.5">
                  <Count n={f.unread} warn />
                </td>
                <td className="px-3 py-1.5">
                  <Count n={f.core_tables} warn />
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
      <p className="m-0 border-t px-3 py-2 text-xs text-muted-foreground">
        Classes not read and core tables not read are the model linker&apos;s counts (schema_model_unread, schema_model_core_tables): tables those files may define that the list
        does not show.
      </p>
    </div>
  );
}
