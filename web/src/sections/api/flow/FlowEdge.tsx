// The lines of the Flow tab: the ELK route of one drawn line, styled by its
// tier (D3). Solid lines are stored edges; the only dashed line is a near
// miss's recorded candidate. "defined in" is thin, grey and has no arrow:
// structure, not a call, with no confidence. A label is a button that
// selects the line (UQ9 A); the line itself is clickable too.

import { BaseEdge, EdgeLabelRenderer, type Edge, type EdgeProps } from "@xyflow/react";
import type { ReactNode } from "react";

import { cn } from "@/lib/utils";
import { useFlowActions } from "@/sections/api/flow/context";
import type { GraphEdge } from "@/sections/api/flow/graph";
import type { Route } from "@/sections/api/flow/layout";
import type { Tier } from "@/sections/api/flow/tiers";

export type RouteEdge = Edge<{ line: GraphEdge; route: Route; related: boolean }, "route">;

/** How each tier is drawn. Colours are token names. */
export interface TierStyle {
  width: number;
  color: string;
  dash: string | null;
  arrow: "filled" | "open" | "dot";
  tone: "strong" | "normal" | "muted" | "candidate";
}

export const TIER_STYLE: Record<Tier, TierStyle> = {
  fact: { width: 2.6, color: "edge", dash: null, arrow: "filled", tone: "strong" },
  exact: { width: 1.7, color: "edge", dash: null, arrow: "filled", tone: "normal" },
  suffix: { width: 1, color: "muted-foreground", dash: null, arrow: "filled", tone: "muted" },
  call: { width: 1.3, color: "edge", dash: null, arrow: "filled", tone: "muted" },
  access: { width: 1.8, color: "brand", dash: null, arrow: "filled", tone: "normal" },
  contains: { width: 1, color: "never-bar", dash: null, arrow: "dot", tone: "muted" },
  candidate: { width: 1.5, color: "other-bar", dash: "5 4", arrow: "open", tone: "candidate" },
  other: { width: 1.3, color: "never", dash: null, arrow: "open", tone: "normal" },
};

/** roundedPath draws an orthogonal route with small rounded corners. */
export function roundedPath(pts: { x: number; y: number }[], r = 6): string {
  const first = pts[0];
  if (first === undefined) return "";
  let d = `M ${String(first.x)} ${String(first.y)}`;
  for (let i = 1; i < pts.length - 1; i++) {
    const p = pts[i - 1];
    const c = pts[i];
    const n = pts[i + 1];
    if (p === undefined || c === undefined || n === undefined) continue;
    const d1 = Math.hypot(c.x - p.x, c.y - p.y) || 1;
    const d2 = Math.hypot(n.x - c.x, n.y - c.y) || 1;
    const rr = Math.min(r, d1 / 2, d2 / 2);
    const ax = c.x + ((p.x - c.x) / d1) * rr;
    const ay = c.y + ((p.y - c.y) / d1) * rr;
    const bx = c.x + ((n.x - c.x) / d2) * rr;
    const by = c.y + ((n.y - c.y) / d2) * rr;
    d += ` L ${String(ax)} ${String(ay)} Q ${String(c.x)} ${String(c.y)} ${String(bx)} ${String(by)}`;
  }
  const last = pts[pts.length - 1];
  if (last !== undefined && pts.length > 1) d += ` L ${String(last.x)} ${String(last.y)}`;
  return d;
}

const LABEL_TONE: Record<TierStyle["tone"], string> = {
  strong: "border bg-card font-semibold text-foreground",
  normal: "border bg-card text-foreground",
  muted: "bg-canvas text-muted-foreground",
  candidate: "border border-dashed border-other-bar bg-other-soft text-other",
};

/** FlowLine renders one line. */
export function FlowLine({ id, data }: EdgeProps<RouteEdge>) {
  const a = useFlowActions();
  if (data === undefined) return null;
  const { line, route, related } = data;
  const st = TIER_STYLE[line.tier];
  const color = `hsl(var(--${st.color}))`;
  const pts = route.points.map((p) => ({ ...p }));
  const tip = pts[pts.length - 1];
  const prev = pts[pts.length - 2];
  let head: ReactNode = null;
  if (tip !== undefined && prev !== undefined) {
    const len = Math.hypot(tip.x - prev.x, tip.y - prev.y) || 1;
    const ux = (tip.x - prev.x) / len;
    const uy = (tip.y - prev.y) / len;
    const al = st.arrow === "dot" ? 4 : 7 + st.width;
    const base = { x: tip.x - ux * al, y: tip.y - uy * al };
    const hw = 3.6 + st.width * 0.6;
    const px = -uy;
    const py = ux;
    if (st.arrow === "dot") {
      head = <circle cx={tip.x - ux * 3.5} cy={tip.y - uy * 3.5} r={3} fill="hsl(var(--canvas))" stroke={color} strokeWidth={1.2} />;
    } else if (st.arrow === "open") {
      head = <path d={`M ${String(base.x + px * hw)} ${String(base.y + py * hw)} L ${String(tip.x)} ${String(tip.y)} L ${String(base.x - px * hw)} ${String(base.y - py * hw)}`} fill="none" stroke={color} strokeWidth={1.5} />;
    } else {
      head = <path d={`M ${String(base.x + px * hw)} ${String(base.y + py * hw)} L ${String(tip.x)} ${String(tip.y)} L ${String(base.x - px * hw)} ${String(base.y - py * hw)} Z`} fill={color} />;
    }
    // Stop the line where the arrow begins.
    pts[pts.length - 1] = st.arrow === "filled" ? base : st.arrow === "dot" ? { x: tip.x - ux * 6.5, y: tip.y - uy * 6.5 } : { x: tip.x - ux, y: tip.y - uy };
  }
  const selected = a.selectedEdge === id;
  const path = roundedPath(pts);
  const label = route.label;
  return (
    <>
      {(selected || related) && (
        <path d={path} fill="none" stroke={`hsl(var(--brand) / ${selected ? ".55" : ".22"})`} strokeWidth={st.width + (selected ? 5 : 7)} strokeLinecap="round" />
      )}
      <g opacity={line.dim ? 0.18 : 1} data-flow-line={id} data-tier={line.tier} data-dim={line.dim ? "true" : undefined}>
        <BaseEdge id={id} path={path} interactionWidth={14} style={{ stroke: color, strokeWidth: st.width, strokeDasharray: st.dash ?? undefined }} />
        {head}
      </g>
      {label !== null && line.label !== null && (
        <EdgeLabelRenderer>
          <button
            type="button"
            className={cn(
              "nodrag nopan absolute top-0 left-0 grid place-items-center rounded-[4px] text-xs leading-none font-medium whitespace-nowrap outline-none focus-visible:ring-2 focus-visible:ring-ring",
              LABEL_TONE[st.tone],
              selected && "ring-2 ring-brand",
              line.dim && "opacity-35",
            )}
            style={{ transform: `translate(${String(label.x)}px, ${String(label.y)}px)`, width: label.width, height: label.height, pointerEvents: "all" }}
            aria-pressed={selected}
            aria-label={`Line: ${line.label}`}
            title={`${line.labelTitle !== "" ? `${line.labelTitle}. ` : ""}Select this line: its stored facts and the source's code`}
            data-flow-label={id}
            onClick={(e) => {
              e.stopPropagation();
              a.selectEdge(id);
            }}
          >
            {line.label}
          </button>
        </EdgeLabelRenderer>
      )}
    </>
  );
}
