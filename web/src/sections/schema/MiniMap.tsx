// The compact foreign-key map above the table list (D2, Q7 variant X).
// This module is a lazy chunk: it is imported on the first map render, and
// its layout runs in the page's one ELK worker (flow/elk.ts, ADR 0046), the
// same worker the API Flow tab uses. It draws plain SVG lines under HTML
// buttons (no React Flow): boxes are buttons, so click and keyboard select a
// table. The map keeps its natural size (every text 12 px or more) and
// scrolls instead of shrinking.

import type { ElkNode } from "elkjs/lib/elk-api";
import { Maximize2 } from "lucide-react";
import { useEffect, useEffectEvent, useLayoutEffect, useMemo, useRef, useState } from "react";

import type { SchemaGroup } from "@/api/schema";
import { cn } from "@/lib/utils";
import { pageElk } from "@/sections/api/flow/elk";
import { textWidth } from "@/sections/api/flow/measure";
import { boxWidth, mapView, miniElkGraph, readPlaced, type Placed } from "@/sections/schema/map";
import { OUTLINE_BUTTON, tableName } from "@/sections/schema/parts";
import { objectsState } from "@/sections/schema/state";

export interface LayoutRunner {
  layout(graph: ElkNode): Promise<ElkNode>;
}

export interface MiniMapProps {
  group: SchemaGroup;
  /** Identifies the index run, so a cached layout is never reused for
   * another index. */
  run: string;
  selected: string | null;
  onSelect: (id: string) => void;
  onOverview: () => void;
  elk?: LayoutRunner | undefined;
}

const cache = new Map<string, Placed>();

function measure(s: string): number {
  return textWidth(s, "title");
}

/** MiniMap renders the map card's body. */
export default function MiniMap({ group, run, selected, onSelect, onOverview, elk }: MiniMapProps) {
  const view = useMemo(() => mapView(group, selected), [group, selected]);
  const key = JSON.stringify([run, group.set, view.tables.map((t) => t.node.identity)]);
  const runner = elk ?? pageElk();
  const [held, setHeld] = useState<{ key: string; placed: Placed | null; error: string | null } | null>(null);
  const layout = useEffectEvent(() => Promise.resolve().then(() => runner.layout(miniElkGraph(view, (t) => boxWidth(t, measure))).then((out) => readPlaced(view, out))));
  useEffect(() => {
    if (cache.has(key) || view.tables.length === 0) return;
    let live = true;
    layout().then(
      (placed) => {
        cache.set(key, placed);
        if (live) setHeld({ key, placed, error: null });
      },
      (err: unknown) => {
        if (live) setHeld({ key, placed: null, error: err instanceof Error ? err.message : String(err) });
      },
    );
    return () => {
      live = false;
    };
  }, [key, view.tables.length]);
  const placed = cache.get(key) ?? (held?.key === key ? held.placed : null);
  const error = held?.key === key ? held.error : null;
  const scrollRef = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    if (placed === null || selected === null) return;
    const b = placed.boxes.get(selected);
    const el = scrollRef.current;
    if (b === undefined || el === null) return;
    if (b.x < el.scrollLeft || b.x + b.width > el.scrollLeft + el.clientWidth) el.scrollLeft = Math.max(0, b.x + b.width / 2 - el.clientWidth / 2);
    if (b.y < el.scrollTop || b.y + b.height > el.scrollTop + el.clientHeight) el.scrollTop = Math.max(0, b.y - 8);
  }, [placed, selected]);

  const pairs = group.foreign_keys.length;
  // Only an extracted group's lists are counts; otherwise the map draws the
  // stored tables and says the total is unknown (Review fixes R3).
  const counted = objectsState(group) === "extracted";
  const tablesWord = (n: number) => (n === 1 ? "table" : "tables");
  return (
    <div className="gv-mini flex-none border-b" data-testid="mini-map" data-mode={view.mode} data-boxes={view.tables.length} data-edges={view.edges.length}>
      <div className="flex items-start gap-2 px-3 pt-2 text-xs">
        <b className="text-[13px] font-semibold">Map</b>
        <span className="text-muted-foreground" data-testid="map-header">
          {view.mode === "neighbourhood"
            ? `Neighbourhood · 1 hop · ${String(view.tables.length)} of ${String(view.total)} ${counted ? "" : "stored "}tables`
            : counted
              ? `${String(view.total)} ${tablesWord(view.total)} · ${String(pairs)} FK ${pairs === 1 ? "pair" : "pairs"}`
              : `${String(view.total)} stored ${tablesWord(view.total)} · whether all were extracted is unknown`}
        </span>
        <span className="flex-1" />
        <button type="button" className={OUTLINE_BUTTON} onClick={onOverview} title="Open the Overview diagram">
          <Maximize2 aria-hidden="true" />
          Overview
        </button>
      </div>
      <div ref={scrollRef} className="relative h-[232px] overflow-auto" aria-busy={placed === null && error === null}>
        {view.tables.length === 0 ? (
          <div className="px-3 py-6 text-xs text-muted-foreground">No tables to draw.</div>
        ) : error !== null ? (
          <div role="alert" className="px-3 py-4 text-xs">
            Could not lay out the map: {error}
          </div>
        ) : placed === null ? (
          <div className="px-3 py-6 text-xs text-muted-foreground">Laying out the map…</div>
        ) : (
          <div className="relative" style={{ width: placed.width, height: placed.height }}>
            <svg aria-hidden="true" className="absolute inset-0 overflow-visible" width={placed.width} height={placed.height}>
              <defs>
                <marker id="gv-mini-arrow" viewBox="0 0 8 8" refX="4" refY="4" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
                  <path d="M0 0 L8 4 L0 8 z" className="fill-edge" />
                </marker>
              </defs>
              {view.edges.map((e) => {
                const pts = placed.routes.get(e.id);
                if (pts === undefined) return null;
                const on = e.source === selected || e.target === selected;
                return (
                  <polyline
                    key={e.id}
                    points={pts.map((p) => `${String(p.x)},${String(p.y)}`).join(" ")}
                    fill="none"
                    markerStart="url(#gv-mini-arrow)"
                    className={cn(on ? "stroke-brand" : "stroke-edge/55")}
                    strokeWidth={on ? 1.8 : 1.2}
                  >
                    <title>{`${String(e.pairs)} foreign-key ${e.pairs === 1 ? "pair" : "pairs"}`}</title>
                  </polyline>
                );
              })}
            </svg>
            {view.tables.map((t) => {
              const b = placed.boxes.get(t.node.identity);
              if (b === undefined) return null;
              const quiet = t.completeness === "complete" || t.completeness === "complete_in_source";
              const sel = t.node.identity === selected;
              return (
                <button
                  key={t.node.identity}
                  type="button"
                  className={cn(
                    "absolute flex items-center justify-center gap-1 rounded-md border bg-card px-2 font-mono text-[13px] whitespace-nowrap hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring",
                    sel && "border-2 border-brand font-semibold text-brand-text",
                    !quiet && "border-dashed border-dynamic bg-dynamic-soft",
                  )}
                  style={{ left: b.x, top: b.y, width: b.width, height: b.height }}
                  onClick={() => {
                    onSelect(t.node.identity);
                  }}
                  title={`${tableName(t.node)} · ${t.completeness}`}
                  data-testid="map-box"
                >
                  {tableName(t.node)}
                  {!quiet && <span className="font-sans text-xs font-semibold text-dynamic">{t.completeness === "partial" ? "partial" : "unknown"}</span>}
                </button>
              );
            })}
          </div>
        )}
      </div>
      {(view.outside > 0 || view.self > 0) && (
        <div className="px-3 pb-1.5 text-xs text-muted-foreground" data-testid="map-outside">
          {view.outside > 0 && `${String(view.outside)} foreign-key ${view.outside === 1 ? "pair leads" : "pairs lead"} to tables outside this view.`}
          {view.outside > 0 && view.self > 0 && " "}
          {view.self > 0 && `${String(view.self)} self-referencing ${view.self === 1 ? "pair is" : "pairs are"} not drawn.`}
        </div>
      )}
    </div>
  );
}
