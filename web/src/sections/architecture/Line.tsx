// The map's lines: one per bundled pair of cards (AG15) on ELK's
// orthogonal route, with a chip that names the families, the count and
// the three-cell glyph. A line is a button through its chip. Lines into
// the libraries band (T067) have no chip of their own: the card's line
// chip counts them, and library-library lines show only while one of
// their ends is selected.

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

/** RAISED_LINE_Z is the z-index of chain and band lines: above the cards
 * (0), below the band's chips and the chain's members (2). */
export const RAISED_LINE_Z = 1;
/** CHAIN_CHIP_Z is a chain line chip's z-index: above every line, so a
 * chip interrupts its line instead of being struck through by it (T072).
 * The chip also takes the pointer (the label layer passes it through by
 * default), so its own line's wide hit path never answers over the chip. */
export const CHAIN_CHIP_Z = 3;

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
            style={{ transform: `translate(${String(chip.x)}px, ${String(chip.y)}px)`, height: chip.height, minWidth: chip.width, zIndex: CHAIN_CHIP_Z, pointerEvents: "all" }}
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
  if (props.data?.edge.band !== undefined) return <BandLine {...props} />;
  return <BundleLine {...props} />;
}

/** bandPath draws a band route: straight segments, then a curve into the
 * chip (the last two points). */
export function bandPath(pts: readonly { x: number; y: number }[]): string {
  const last = pts[pts.length - 1];
  const prev = pts[pts.length - 2];
  if (last === undefined || prev === undefined) return "";
  const head = pts.slice(0, -1).map((p, i) => `${i === 0 ? "M" : "L"} ${String(p.x)} ${String(p.y)}`).join(" ");
  const my = (prev.y + last.y) / 2;
  return `${head} C ${String(prev.x)} ${String(my)} ${String(last.x)} ${String(my)} ${String(last.x)} ${String(last.y)}`;
}

/** BandLine is a card-library line, routed on the bus (T072), or a
 * library-library line while it is selected itself: a selected library's
 * links are marked on the chips, not drawn (T071 V3). Selecting a line
 * shows its links' evidence. */
function BandLine({ id, data }: EdgeProps<LineEdge>) {
  const a = useMapActions();
  if (data === undefined) return null;
  const { edge } = data;
  const selected = a.selectedEdge === id;
  const between = edge.band === "between";
  if (between && !selected) return null;
  const lit = a.focus !== null && a.focus.edges.has(id);
  const hovered = !between && a.hover !== null && (a.hover === edge.source || a.hover === edge.target);
  const dim = a.focus !== null && !lit;
  // Bus lines rest in a lighter, solid grey (lines of one trunk lie on top
  // of each other, so no alpha): the trunk under the pointer or keyboard
  // focus and the selection's lines carry the contrast.
  const color = lit || between || hovered ? "hsl(var(--brand))" : "color-mix(in srgb, hsl(var(--edge)) 58%, hsl(var(--canvas)))";
  const pts = data.points.map((p) => ({ ...p }));
  const last = pts[pts.length - 1];
  const before = pts[pts.length - 2];
  const heads = [];
  if (last !== undefined) {
    // A bus line ends on a straight segment into the chip; a selected
    // library-library line curves in from straight above (or below).
    const prev = !between && before !== undefined ? before : { x: last.x, y: pts.length > 1 && (before?.y ?? 0) > last.y ? last.y + 10 : last.y - 10 };
    const h = arrow(last, prev, color, 6);
    heads.push(<g key="end">{h.path}</g>);
    pts[pts.length - 1] = h.base;
  }
  const path = between ? bandPath(pts) : roundedPath(pts, 5);
  return (
    <>
      {selected && <path d={path} fill="none" stroke="hsl(var(--brand) / .35)" strokeWidth={7} strokeLinecap="round" />}
      <g opacity={dim ? 0.1 : 1} data-arch-line={id} data-band={edge.band} data-dim={dim ? "true" : undefined} data-lit={lit || hovered ? "true" : undefined}>
        <BaseEdge id={id} path={path} interactionWidth={between ? 10 : 6} style={{ stroke: color, strokeWidth: lit || hovered ? 2 : 1.3 }} />
        {heads}
      </g>
    </>
  );
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
  const color = `hsl(var(${family !== null ? (FAMILY_STROKE[family] ?? "--edge") : "--edge"}))`;
  const heads = [];
  const last = pts[pts.length - 1];
  const prev = pts[pts.length - 2];
  if (last !== undefined && prev !== undefined) {
    const h = arrow(last, prev, color, 8);
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
        <BaseEdge id={id} path={path} interactionWidth={14} style={{ stroke: color, strokeWidth: 2.2 }} />
        {heads}
      </g>
      {chip !== null && (
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
