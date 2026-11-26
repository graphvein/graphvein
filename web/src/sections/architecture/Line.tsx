// The map's lines: one per bundled pair of cards (AG15) on ELK's
// orthogonal route, with a chip that names the families, the count and
// the three-cell glyph. An SDK stub's line is thin and dotted with no chip
// (AG5: never a protocol). A line is a button through its chip.

import { BaseEdge, EdgeLabelRenderer, type Edge, type EdgeProps } from "@xyflow/react";

import { formatCount } from "@/lib/count";
import { useLabelStyle } from "@/hooks/useLabels";
import type { LabelStyle } from "@/lib/labelChoice";
import { label } from "@/lib/labels";
import { cn } from "@/lib/utils";
import { roundedPath } from "@/sections/api/flow/FlowEdge";
import { useMapActions } from "@/sections/architecture/context";
import { FAMILY_STROKE, FAMILY_TEXT, Glyph } from "@/sections/architecture/Glyph";
import type { Box } from "@/sections/architecture/layout";
import type { MapEdge } from "@/sections/architecture/model";

export type LineEdge = Edge<{ edge: MapEdge; points: { x: number; y: number }[]; chip: Box | null }, "line">;

function arrow(tip: { x: number; y: number }, prev: { x: number; y: number }, color: string, size = 7) {
  const len = Math.hypot(tip.x - prev.x, tip.y - prev.y) || 1;
  const ux = (tip.x - prev.x) / len;
  const uy = (tip.y - prev.y) / len;
  const bx = tip.x - ux * size;
  const by = tip.y - uy * size;
  const hw = 4.2;
  return {
    base: { x: bx, y: by },
    path: <path d={`M ${String(bx - uy * hw)} ${String(by + ux * hw)} L ${String(tip.x)} ${String(tip.y)} L ${String(bx + uy * hw)} ${String(by - ux * hw)} Z`} fill={color} />,
  };
}

/** chainLineText is a chain line's chip: each stored edge's wording and
 * tier in the applied style. */
export function chainLineText(style: LabelStyle, edge: MapEdge): { text: string; title: string } {
  const parts = (edge.chain ?? []).map((k) => label(style, "chainEdge", { wording: k.wording, row: k.row, tier: k.tier, dashed: k.dashed }));
  // Equal wordings of a bundle are counted once (×n).
  const counted = new Map<string, { text: string; title: string; n: number }>();
  for (const p of parts) {
    const c = counted.get(p.text) ?? { ...p, n: 0 };
    c.n++;
    counted.set(p.text, c);
  }
  const title = [...counted.values()].map((p) => `${p.text}${p.n > 1 ? ` ×${String(p.n)}` : ""} (${p.title})`).join("; ");
  // Inside one opened card the chip names the tier only (X+-1); the
  // wording is in its tooltip and the inspector.
  if (edge.inner === true) return { text: [...new Set((edge.chain ?? []).map((k) => (k.dashed ? k.wording.split(",")[0] ?? k.wording : label(style, "linkTier", k.tier).text)))].join(" + "), title };
  if (counted.size <= 1) return { text: [...counted.values()].map((p) => `${p.text}${p.n > 1 ? ` ×${String(p.n)}` : ""}`).join(""), title };
  // A bundle of several wordings: the first two, then tiers counted.
  const kinds = edge.chain ?? [];
  const words = [...new Set(kinds.map((k) => k.wording))];
  const tiers = new Map<string, number>();
  for (const k of kinds) {
    const t = label(style, "linkTier", k.tier).text;
    tiers.set(t, (tiers.get(t) ?? 0) + 1);
  }
  const tierText = [...tiers].map(([t, n]) => `${t} ×${String(n)}`).join(", ");
  return { text: `${words.slice(0, 2).join(", ")}${words.length > 2 ? ` +${String(words.length - 2)}` : ""} · ${tierText}`, title };
}

/** ChainLine draws stored edges between two chain members (dashed when
 * every one is structure, CH7/CH8), or a member's dotted line to its
 * step's fold. */
function ChainLine({ id, data }: EdgeProps<LineEdge>) {
  const a = useMapActions();
  const style = useLabelStyle();
  if (data === undefined) return null;
  const { edge, chip } = data;
  const pts = data.points.map((p) => ({ ...p }));
  const fold = edge.type === "fold";
  const sel = a.chain?.selected ?? null;
  const touches = sel !== null && (edge.chain ?? []).some((k) => k.from === sel || k.to === sel);
  const color = fold ? "hsl(var(--muted-foreground))" : "hsl(var(--brand))";
  const last = pts[pts.length - 1];
  const prev = pts[pts.length - 2];
  let head = null;
  if (last !== undefined && prev !== undefined) {
    const h = arrow(last, prev, color, fold ? 6 : 8);
    head = h.path;
    pts[pts.length - 1] = h.base;
  }
  const dashed = fold || (edge.chain ?? []).every((k) => k.dashed);
  const path = roundedPath(pts);
  const text = fold ? null : chainLineText(style, edge);
  return (
    <>
      {touches && <path d={path} fill="none" stroke="hsl(var(--brand) / .30)" strokeWidth={8} strokeLinecap="round" />}
      <g data-arch-line={id} data-type={edge.type}>
        <BaseEdge id={id} path={path} interactionWidth={10} style={{ stroke: color, strokeWidth: fold ? 1.3 : 2.4, strokeDasharray: dashed ? (fold ? "2 4" : "6 4") : undefined }} />
        {head}
      </g>
      {chip !== null && text !== null && (
        <EdgeLabelRenderer>
          <span
            className={cn("nodrag nopan absolute top-0 left-0 flex items-center justify-center rounded-full border bg-card px-1.5 text-[11px] leading-none whitespace-nowrap shadow-[0_1px_2px_hsl(240_10%_4%/.06)]", touches ? "border-brand text-foreground" : "border-brand/40 text-muted-foreground")}
            style={{ transform: `translate(${String(chip.x)}px, ${String(chip.y)}px)`, height: chip.height, minWidth: chip.width }}
            title={text.title}
            data-arch-chain-chip={id}
          >
            {text.text}
          </span>
        </EdgeLabelRenderer>
      )}
    </>
  );
}

/** MapLine renders one line; React Flow's edge type "line". */
export function MapLine(props: EdgeProps<LineEdge>) {
  if (props.data?.edge.type === "chain" || props.data?.edge.type === "fold") return <ChainLine {...props} />;
  return <BundleLine {...props} />;
}

function BundleLine({ id, data }: EdgeProps<LineEdge>) {
  const a = useMapActions();
  const style = useLabelStyle();
  if (data === undefined) return null;
  const { edge, chip } = data;
  const pts = data.points.map((p) => ({ ...p }));
  const dim = a.focus !== null && !a.focus.edges.has(id);
  const selected = a.selectedEdge === id;
  const family = edge.families.length === 1 ? (edge.families[0] ?? null) : null;
  const stub = edge.type === "stub";
  const color = stub ? "hsl(var(--muted-foreground))" : `hsl(var(${family !== null ? (FAMILY_STROKE[family] ?? "--edge") : "--edge"}))`;
  const heads = [];
  const last = pts[pts.length - 1];
  const prev = pts[pts.length - 2];
  if (last !== undefined && prev !== undefined) {
    const h = arrow(last, prev, color, stub ? 6 : 8);
    heads.push(<g key="end">{h.path}</g>);
    pts[pts.length - 1] = h.base;
  }
  if (edge.both && pts.length > 1) {
    const first = pts[0];
    const next = pts[1];
    if (first !== undefined && next !== undefined) {
      const h = arrow(first, next, color, 8);
      heads.push(<g key="start">{h.path}</g>);
      pts[0] = h.base;
    }
  }
  const path = roundedPath(pts);
  const familyLabels = edge.families.map((f) => label(style, "linkFamily", { family: f, row: edge.links.find((l) => l.family === f)?.row ?? "" }));
  const name = `${familyLabels.map((l) => l.text).join(" + ")} ${formatCount(edge.count)}${edge.both ? " both ways" : ""}`;
  return (
    <>
      {selected && <path d={path} fill="none" stroke="hsl(var(--brand) / .35)" strokeWidth={8} strokeLinecap="round" />}
      <g opacity={dim ? 0.2 : 1} data-arch-line={id} data-dim={dim ? "true" : undefined} data-type={edge.type}>
        <BaseEdge id={id} path={path} interactionWidth={14} style={{ stroke: color, strokeWidth: stub ? 1.3 : 2.2, strokeDasharray: stub ? "2 4" : undefined }} />
        {heads}
      </g>
      {chip !== null && !stub && (
        <EdgeLabelRenderer>
          <button
            type="button"
            className={cn(
              "nodrag nopan absolute top-0 left-0 flex items-center gap-1.5 rounded-full border bg-card px-1.5 text-xs leading-none font-medium whitespace-nowrap shadow-[0_1px_2px_hsl(240_10%_4%/.06)] outline-none focus-visible:ring-2 focus-visible:ring-ring",
              selected && "border-brand ring-2 ring-brand/40",
              dim && "opacity-35",
            )}
            style={{ transform: `translate(${String(chip.x)}px, ${String(chip.y)}px)`, height: chip.height, minWidth: chip.width, pointerEvents: "all" }}
            aria-pressed={selected}
            aria-label={`Line: ${name}`}
            title={`${name}. Select this line: its cells, sources and evidence`}
            data-arch-chip={id}
            onClick={(e) => {
              e.stopPropagation();
              a.selectEdge(id);
            }}
          >
            {edge.families.map((f, i) => (
              <span key={f} className={cn("rounded-[4px] px-1 py-[3px] text-[11px] font-semibold", FAMILY_TEXT[f] ?? "bg-muted")} title={familyLabels[i]?.title}>
                {familyLabels[i]?.text}
              </span>
            ))}
            <span className="tabular-nums">
              {edge.both ? "⇄ " : ""}
              {formatCount(edge.count)}
            </span>
            <Glyph cells={edge.cells} family={family} />
          </button>
        </EdgeLabelRenderer>
      )}
    </>
  );
}
