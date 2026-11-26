// The map legend: line families and the glyph's cell states. Folded to
// one line by default (X+ "Legend >"), so it never covers cards on a small
// canvas; open, it lists every family and state the map can draw.

import { ChevronRight } from "lucide-react";
import { useState } from "react";

import { useLabelStyle } from "@/hooks/useLabels";
import { label } from "@/lib/labels";
import { cn } from "@/lib/utils";

const FAMILIES: { family: string; row: string; color: string; dash?: string }[] = [
  { family: "http", row: "CE1", color: "--http-bar" },
  { family: "tauri_ipc", row: "CE2", color: "--ipc-bar" },
  { family: "sql", row: "CE3", color: "--brand" },
  { family: "in_process", row: "CE4", color: "--edge" },
  { family: "server_action", row: "CE7", color: "--http-bar" },
];

const STATES: { state: string; cls: string }[] = [
  { state: "proven", cls: "bg-linked-bar" },
  { state: "as_written", cls: "bg-wrapper-bar" },
  { state: "mixed", cls: "gv-cell-half" },
  { state: "not_applicable", cls: "gv-cell-na" },
  { state: "unknown", cls: "gv-cell-hatch" },
];

function Cell({ cls }: { cls: string }) {
  return <span aria-hidden="true" className={cn("inline-block h-[12px] w-[14px] rounded-[2px]", cls)} />;
}

/** Legend is the map's key, bottom left of the canvas. */
export function Legend() {
  const style = useLabelStyle();
  const [open, setOpen] = useState(false);
  return (
    <div className="absolute bottom-3 left-3 z-[5] max-w-[calc(100%-72px)] rounded-lg border bg-card/95 px-3 py-1.5 text-xs shadow-xs" data-testid="map-legend">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
        <span className="inline-flex items-center gap-1">
          <span className="inline-flex gap-px" aria-hidden="true">
            <Cell cls="bg-http-bar" />
            <Cell cls="gv-cell-hatch" />
            <Cell cls="gv-cell-hatch" />
          </span>
          protocol · auth · TLS
        </span>
        <button
          type="button"
          aria-expanded={open}
          className="inline-flex items-center gap-0.5 rounded-md border px-1.5 py-0.5 font-medium hover:bg-accent outline-none focus-visible:ring-2 focus-visible:ring-ring"
          onClick={() => {
            setOpen((o) => !o);
          }}
        >
          Legend <ChevronRight aria-hidden="true" className={cn("size-3.5 transition-transform", open && "rotate-90")} />
        </button>
      </div>
      {open && (
        <div className="mt-1.5 grid gap-1 border-t pt-1.5">
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
            <span className="font-medium text-muted-foreground">Lines</span>
            {FAMILIES.map((f) => {
              const l = label(style, "linkFamily", { family: f.family, row: f.row });
              return (
                <span key={f.family} className="inline-flex items-center gap-1" title={l.title}>
                  <svg aria-hidden="true" width="22" height="8">
                    <line x1="0" y1="4" x2="22" y2="4" stroke={`hsl(var(${f.color}))`} strokeWidth="2.2" />
                  </svg>
                  {l.text}
                </span>
              );
            })}
            <span className="inline-flex items-center gap-1">
              <svg aria-hidden="true" width="22" height="8">
                <line x1="0" y1="4" x2="22" y2="4" stroke="hsl(var(--muted-foreground))" strokeWidth="1.3" strokeDasharray="2 4" />
              </svg>
              SDK imports (no protocol)
            </span>
          </div>
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
            <span className="font-medium text-muted-foreground">Cells</span>
            {STATES.map((s) => {
              const l = label(style, "cellState", s.state);
              return (
                <span key={s.state} className="inline-flex items-center gap-1" title={l.title}>
                  <Cell cls={s.cls} />
                  {s.state === "as_written" ? `${l.text} / documented` : l.text}
                </span>
              );
            })}
          </div>
          <div className="text-muted-foreground">Deploy markers are text on a card, never a line. Calls with no stored link are a dashed chip. In a project, a muted dashed card is another repository: its lines are stored project links, linked across repositories.</div>
        </div>
      )}
    </div>
  );
}
