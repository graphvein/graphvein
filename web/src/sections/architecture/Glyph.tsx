// The fixed three-cell glyph protocol | auth | TLS (AG13, AG15) and its
// full strip for the inspector. A cell is filled only for its own state:
// proven (the protocol in its family colour, other cells green), stated
// (as written, documented, as configured: olive, never the proven fill),
// not applicable (grey "n/a"), mixed (half, counts in the inspector) and
// unknown (hatched). A state this UI does not know is drawn hatched, as
// unknown: never a stronger fill than the answer proves.

import type { Cell } from "@/api/architecture";
import { useLabelStyle } from "@/hooks/useLabels";
import { label } from "@/lib/labels";
import { cn } from "@/lib/utils";
import type { Cells } from "@/sections/architecture/model";

// A server action (CE7) is an HTTP POST to its page: it shares HTTP's
// colour, its chip says "Server action".
export const FAMILY_BAR: Record<string, string> = { http: "bg-http-bar", server_action: "bg-http-bar", tauri_ipc: "bg-ipc-bar", sql: "bg-brand", in_process: "bg-edge" };
export const FAMILY_TEXT: Record<string, string> = { http: "text-http bg-http-soft", server_action: "text-http bg-http-soft", tauri_ipc: "text-ipc bg-ipc-soft", sql: "text-brand-text bg-brand-soft", in_process: "text-foreground bg-muted" };
export const FAMILY_STROKE: Record<string, string> = { http: "--http-bar", server_action: "--http-bar", tauri_ipc: "--ipc-bar", sql: "--brand", in_process: "--edge" };

const STATED = new Set(["as_written", "documented", "as_configured"]);

/** cellLook is how a cell is filled. */
export type CellLook = "family" | "proven" | "stated" | "na" | "mixed" | "unknown";

export function cellLook(cell: Cell, protocol: boolean): CellLook {
  if (cell.state === "proven") return protocol ? "family" : "proven";
  if (STATED.has(cell.state)) return "stated";
  if (cell.state === "not_applicable") return "na";
  if (cell.state === "mixed") return "mixed";
  return "unknown";
}

function lookClass(look: CellLook, family: string | null): string {
  switch (look) {
    case "family":
      return family !== null ? (FAMILY_BAR[family] ?? "bg-never-bar") : "bg-never-bar";
    case "proven":
      return "bg-linked-bar";
    case "stated":
      return "bg-wrapper-bar";
    case "na":
      return "gv-cell-na";
    case "mixed":
      return "gv-cell-half";
    case "unknown":
      return "gv-cell-hatch";
  }
}

const NAMES = ["protocol", "auth", "TLS"] as const;

/** cellText is a cell's value with its source, for tooltips and names. */
export function cellText(name: string, cell: Cell): string {
  const counts = cell.counts !== null ? ` (${Object.entries(cell.counts).map(([v, n]) => `${v} ×${String(n)}`).join(", ")})` : "";
  return `${name}: ${cell.value}${counts}; ${cell.source}`;
}

/** Glyph is the fixed-width three-cell chip glyph. family colours the
 * protocol cell (null for a bundle of several families). */
export function Glyph({ cells, family, className }: { cells: Cells; family: string | null; className?: string }) {
  const list = [cells.protocol, cells.auth, cells.tls];
  const title = list.map((c, i) => cellText(NAMES[i] ?? "", c)).join("\n");
  return (
    <span className={cn("inline-flex flex-none gap-px", className)} role="img" aria-label={title} title={title} data-testid="glyph">
      {list.map((c, i) => {
        const look = cellLook(c, i === 0);
        return (
          <span
            key={NAMES[i]}
            data-cell={NAMES[i]}
            data-look={look}
            className={cn("grid h-[14px] w-[18px] place-items-center rounded-[2px] text-[8px] leading-none font-semibold", lookClass(look, family))}
          >
            {look === "na" ? "n/a" : null}
          </span>
        );
      })}
    </span>
  );
}

const STRIP_LOOK: Record<CellLook, string> = {
  family: "",
  proven: "bg-linked-soft text-linked",
  stated: "bg-wrapper-soft text-wrapper",
  na: "gv-cell-na",
  mixed: "bg-wrapper-soft text-wrapper",
  unknown: "gv-cell-hatch text-muted-foreground",
};

/** Strip is the inspector's full strip: each cell's value in words. */
export function Strip({ cells, family }: { cells: Cells; family: string | null }) {
  const list = [cells.protocol, cells.auth, cells.tls];
  return (
    <div className="grid grid-cols-3 overflow-hidden rounded-md border text-[12.5px] font-medium" data-testid="strip">
      {list.map((c, i) => {
        const look = cellLook(c, i === 0);
        return (
          <span
            key={NAMES[i]}
            title={cellText(NAMES[i] ?? "", c)}
            data-cell={NAMES[i]}
            data-look={look}
            className={cn(
              "min-w-0 truncate border-l px-2 py-1 first:border-l-0",
              look === "family" ? (family !== null ? FAMILY_TEXT[family] : "bg-muted") : STRIP_LOOK[look],
            )}
          >
            {c.value}
          </span>
        );
      })}
    </div>
  );
}

/** CellLine is one cell in the inspector or the Links tab: the value, its
 * state word, the source (with rows in the technical style) and the counts
 * per value of a mixed cell. */
export function CellLine({ name, cell }: { name: string; cell: Cell }) {
  const style = useLabelStyle();
  const state = label(style, "cellState", cell.state);
  const source = label(style, "cellSource", { source: cell.source, state: cell.state, rows: cell.rows });
  return (
    <div className="grid grid-cols-[72px_1fr] gap-x-3 gap-y-0.5 text-[13px]" data-testid={`cell-${name}`}>
      <span className="text-muted-foreground">{name}</span>
      <span className="min-w-0">
        <span className="font-medium">{cell.value}</span>
        <span className="ml-1.5 text-xs text-muted-foreground" title={state.title}>
          {state.text}
        </span>
        <span className="block text-xs break-words text-muted-foreground" title={source.title}>
          {source.text}
        </span>
        {cell.counts !== null && (
          <span className="mt-0.5 block text-xs">
            {Object.entries(cell.counts).map(([v, n]) => (
              <span key={v} className="mr-2 inline-block">
                {v} <b className="font-medium">×{n}</b>
              </span>
            ))}
          </span>
        )}
      </span>
    </div>
  );
}
