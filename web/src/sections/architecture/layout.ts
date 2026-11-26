// The ELK side of the architecture map (AG2, ADR 0046): card sizes, the
// graph handed to ELK (layered, top to bottom, orthogonal lines, each
// line's chip a centre label so ELK keeps chips off cards and off each
// other) and the reader of ELK's answer. Pure; the real ELK runs in the
// page's worker (sections/api/flow/elk.ts), shared with the Flow tab.

import type { ElkNode } from "elkjs/lib/elk-api";

import type { MapEdge, MapModel, MapNode } from "@/sections/architecture/model";
import { LIST_ROWS } from "@/sections/architecture/rows";

export const CARD_W = 288;
export const CARD_H = 100;
export const MARKER_H = 22;
export const STUB_W = 196;
export const STUB_H = 64;
export const EXPANDED_W = 470;
export const EXP_HEADER = 70;
export const ROW_H = 26;
export const FILTER_H = 40;
/** LIST_ROWS_H is the scrolling list's height (LIST_ROWS rows). */
export const LIST_ROWS_H = LIST_ROWS * ROW_H;
export const FOOTER_H = 26;
export const CHIP_H = 24;
export const LIB_LINES = 4;
export const LIB_LINE_H = 18;
/** Chain drawn in place (AG-C2): a member node, a fold and the opened
 * card's header room. */
export const MEMBER_W = 250;
export const MEMBER_H = 56;
export const FOLD_W = 230;
export const FOLD_H = 34;
export const CONTAINER_HEAD = 48;

/** expandedHeight is the expanded card's height for n rows (null: the
 * rows are loading). Above LIST_ROWS rows the list scrolls at LIST_ROWS. */
export function expandedHeight(rows: number | null): number {
  if (rows === null) return EXP_HEADER + 3 * ROW_H;
  if (rows <= LIST_ROWS) return EXP_HEADER + Math.max(1, rows) * ROW_H + 10;
  return EXP_HEADER + FILTER_H + LIST_ROWS_H + FOOTER_H + 10;
}

/** nodeSize is a card's size; expandedRows is the expanded card's row
 * count (null while loading). */
export function nodeSize(n: MapNode, expandedRows: number | null): { width: number; height: number } {
  switch (n.type) {
    case "component":
      if (n.expanded) return { width: EXPANDED_W, height: expandedHeight(expandedRows) };
      return { width: CARD_W, height: CARD_H + (n.component.markers.length > 0 || n.component.ingress.length > 0 ? MARKER_H : 0) + (n.across !== null ? 2 * MARKER_H : 0) };
    case "libraries": {
      const lines = Math.min(LIB_LINES, n.members.length) + (n.members.length > LIB_LINES ? 1 : 0);
      return { width: CARD_W, height: CARD_H - 30 + lines * LIB_LINE_H + 12 };
    }
    case "database":
    case "repository":
      return { width: CARD_W, height: CARD_H };
    case "stub":
      return { width: STUB_W, height: STUB_H };
    case "member":
      return { width: MEMBER_W, height: MEMBER_H };
    case "fold":
      return { width: FOLD_W, height: FOLD_H };
    case "container":
      // ELK sizes an opened card around its members; this is its minimum.
      return { width: CARD_W, height: CARD_H };
  }
}

/** chainChipWidth estimates a chain line's chip: its wordings and tiers. */
export function chainChipWidth(text: string): number {
  return Math.ceil(16 + text.length * 6.4);
}

/** chipWidth estimates a line chip's width: family tags, count, glyph. */
export function chipWidth(e: MapEdge, familyText: (f: string) => string): number {
  const tags = e.families.reduce((w, f) => w + familyText(f).length * 7 + 14, 0);
  const count = String(e.count).length * 7.5 + (e.both ? 16 : 0);
  return Math.ceil(10 + tags + count + 8 + 56 + 10);
}

export const ROOT_OPTIONS: Record<string, string> = {
  "elk.algorithm": "layered",
  "elk.direction": "DOWN",
  "elk.edgeRouting": "ORTHOGONAL",
  "elk.json.edgeCoords": "ROOT",
  "elk.spacing.nodeNode": "56",
  "elk.spacing.edgeNode": "40",
  "elk.spacing.edgeEdge": "14",
  "elk.spacing.edgeLabel": "6",
  "elk.layered.spacing.nodeNodeBetweenLayers": "64",
  "elk.layered.spacing.edgeNodeBetweenLayers": "24",
  "elk.edgeLabels.placement": "CENTER",
  "elk.layered.nodePlacement.strategy": "NETWORK_SIMPLEX",
  "elk.layered.considerModelOrder.strategy": "NODES_AND_EDGES",
  "elk.separateConnectedComponents": "true",
  "elk.padding": "[top=24,left=24,bottom=24,right=24]",
};

/** parentOf is the opened card a chain node sits in (null: the root). */
export function parentOf(n: MapNode): string | null {
  if (n.type === "member") return n.parent;
  if (n.type === "fold") return n.parent;
  return null;
}

const INNER_PAD = 16;
const INNER_GAP_X = 72;
const INNER_GAP_Y = 18;

/** Inner is an opened card's own layout: its members and folds in one
 * column per step, left to right in step order (X+-1), offsets relative
 * to the card, and the card's size. */
export interface Inner {
  width: number;
  height: number;
  at: Map<string, { x: number; y: number }>;
}

/** innerLayouts places every opened card's members (a chain drawn in
 * place). Folds of a step sit under that step's members. */
export function innerLayouts(m: MapModel): Map<string, Inner> {
  const out = new Map<string, Inner>();
  const byCard = new Map<string, { id: string; number: number; w: number; h: number }[]>();
  for (const n of m.nodes) {
    if (n.type !== "member" && n.type !== "fold") continue;
    const parent = parentOf(n);
    if (parent === null) continue;
    const size = nodeSize(n, null);
    const list = byCard.get(parent) ?? [];
    list.push({ id: n.id, number: n.number, w: size.width, h: size.height });
    byCard.set(parent, list);
  }
  for (const [card, list] of byCard) {
    const steps = [...new Set(list.map((x) => x.number))].sort((a, b) => a - b);
    const at = new Map<string, { x: number; y: number }>();
    let x = INNER_PAD;
    let bottom = 0;
    for (const step of steps) {
      let y = CONTAINER_HEAD + 4;
      let w = 0;
      for (const item of list.filter((i) => i.number === step)) {
        at.set(item.id, { x, y });
        y += item.h + INNER_GAP_Y;
        w = Math.max(w, item.w);
      }
      bottom = Math.max(bottom, y - INNER_GAP_Y);
      x += w + INNER_GAP_X;
    }
    out.set(card, { width: Math.max(CARD_W, x - INNER_GAP_X + INNER_PAD), height: Math.max(CARD_H, bottom + INNER_PAD), at });
  }
  return out;
}

/** elkGraph is the ELK input for a model. A chain drawn in place gives ELK
 * the opened cards at their inner size (innerLayouts) and one edge per
 * pair of opened cards a chain line joins, so ELK orders them by the
 * chain; the member lines are routed afterwards (chainRoutes). */
export function elkGraph(m: MapModel, expandedRows: number | null, familyText: (f: string) => string): ElkNode {
  const inner = innerLayouts(m);
  const top: ElkNode[] = [];
  for (const n of m.nodes) {
    if (parentOf(n) !== null) continue;
    const i = inner.get(n.id);
    top.push({ id: n.id, ...(n.type === "container" && i !== undefined ? { width: i.width, height: i.height } : nodeSize(n, expandedRows)) });
  }
  const parent = new Map(m.nodes.map((n) => [n.id, parentOf(n) ?? n.id]));
  const edges: NonNullable<ElkNode["edges"]> = [];
  const pairs = new Set<string>();
  for (const e of m.edges) {
    if (e.type === "chain" || e.type === "fold") {
      const a = parent.get(e.source) ?? e.source;
      const b = parent.get(e.target) ?? e.target;
      if (a === b || pairs.has(`${a}>${b}`) || pairs.has(`${b}>${a}`)) continue;
      pairs.add(`${a}>${b}`);
      edges.push({ id: `cc:${a}>${b}`, sources: [a], targets: [b], labels: [{ id: `cc:${a}>${b}:chip`, text: "", width: 120, height: CHIP_H }] });
      continue;
    }
    edges.push({ id: e.id, sources: [e.source], targets: [e.target], labels: e.type === "link" ? [{ id: `${e.id}:chip`, text: e.id, width: chipWidth(e, familyText), height: CHIP_H }] : [] });
  }
  return { id: "root", layoutOptions: ROOT_OPTIONS, children: top, edges };
}

type Point = { x: number; y: number };

/** chainRoute is an orthogonal route between two drawn chain boxes: side
 * to side inside one opened card, else bottom to top (or top to bottom)
 * through the gap between the two cards. */
export function chainRoute(a: Box, b: Box, sameCard: boolean, ca: Box, cb: Box): Point[] {
  const acx = a.x + a.width / 2;
  const acy = a.y + a.height / 2;
  const bcx = b.x + b.width / 2;
  const bcy = b.y + b.height / 2;
  const horizontal = sameCard ? Math.abs(acx - bcx) > 4 : cb.y < ca.y + ca.height && ca.y < cb.y + cb.height;
  if (horizontal) {
    const right = bcx > acx;
    const sx = right ? a.x + a.width : a.x;
    const ex = right ? b.x : b.x + b.width;
    const mx = sameCard ? (sx + ex) / 2 : right ? (ca.x + ca.width + cb.x) / 2 : (cb.x + cb.width + ca.x) / 2;
    return Math.abs(acy - bcy) < 1 ? [{ x: sx, y: acy }, { x: ex, y: bcy }] : [{ x: sx, y: acy }, { x: mx, y: acy }, { x: mx, y: bcy }, { x: ex, y: bcy }];
  }
  const down = bcy > acy;
  const sy = down ? a.y + a.height : a.y;
  const ey = down ? b.y : b.y + b.height;
  const my = sameCard ? (sy + ey) / 2 : down ? (ca.y + ca.height + cb.y) / 2 : (cb.y + cb.height + ca.y) / 2;
  return Math.abs(acx - bcx) < 1 ? [{ x: acx, y: sy }, { x: bcx, y: ey }] : [{ x: acx, y: sy }, { x: acx, y: my }, { x: bcx, y: my }, { x: bcx, y: ey }];
}

/** chainChip centres a chip on the route's longest segment. */
export function chainChip(points: readonly Point[], width: number, height: number): Box {
  let best = { x: points[0]?.x ?? 0, y: points[0]?.y ?? 0 };
  let len = -1;
  for (let i = 1; i < points.length; i++) {
    const p = points[i - 1];
    const q = points[i];
    if (p === undefined || q === undefined) continue;
    const l = Math.hypot(q.x - p.x, q.y - p.y);
    if (l > len) {
      len = l;
      best = { x: (p.x + q.x) / 2, y: (p.y + q.y) / 2 };
    }
  }
  return { x: best.x - width / 2, y: best.y - height / 2, width, height };
}

export interface Box {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface Placed {
  boxes: Map<string, Box>;
  routes: Map<string, { points: { x: number; y: number }[]; chip: Box | null }>;
  width: number;
  height: number;
}

/** onLine centres a chip on the route segment nearest to ELK's label box
 * (ELK keeps the label's room beside the line; the X+ chip sits on it). */
export function onLine(points: readonly { x: number; y: number }[], label: Box): Box {
  const cx = label.x + label.width / 2;
  const cy = label.y + label.height / 2;
  let best: Box = label;
  let dist = Infinity;
  for (let i = 1; i < points.length; i++) {
    const a = points[i - 1];
    const b = points[i];
    if (a === undefined || b === undefined) continue;
    const vertical = Math.abs(a.x - b.x) < Math.abs(a.y - b.y);
    // The point of the segment nearest to the label's centre.
    const px = vertical ? a.x : Math.min(Math.max(cx, Math.min(a.x, b.x)), Math.max(a.x, b.x));
    const py = vertical ? Math.min(Math.max(cy, Math.min(a.y, b.y)), Math.max(a.y, b.y)) : a.y;
    const d = Math.hypot(px - cx, py - cy);
    if (d < dist) {
      dist = d;
      best = { x: px - label.width / 2, y: py - label.height / 2, width: label.width, height: label.height };
    }
  }
  return best;
}

/** overlaps says whether two boxes come closer than gap. */
export function overlaps(a: Box, b: Box, gap = 0): boolean {
  return a.x < b.x + b.width + gap && b.x < a.x + a.width + gap && a.y < b.y + b.height + gap && b.y < a.y + a.height + gap;
}

function finite(v: number | undefined): v is number {
  return typeof v === "number" && Number.isFinite(v);
}

/** readLayout reads ELK's answer; a card without a position or a line
 * without a route is an error (the map never draws a partial graph). */
export function readLayout(m: MapModel, out: ElkNode, chainText: (e: MapEdge) => string = () => ""): Placed {
  const boxes = new Map<string, Box>();
  for (const c of out.children ?? []) {
    if (finite(c.x) && finite(c.y) && finite(c.width) && finite(c.height)) boxes.set(c.id, { x: c.x, y: c.y, width: c.width, height: c.height });
  }
  // A chain's members and folds sit at their inner place in their card.
  const inner = innerLayouts(m);
  for (const n of m.nodes) {
    const parent = parentOf(n);
    if (parent === null) continue;
    const card = boxes.get(parent);
    const off = inner.get(parent)?.at.get(n.id);
    if (card === undefined || off === undefined) continue;
    boxes.set(n.id, { x: card.x + off.x, y: card.y + off.y, ...nodeSize(n, null) });
  }
  const allEdges = out.edges ?? [];
  const routes: Placed["routes"] = new Map();
  for (const e of allEdges) {
    const sec = e.sections?.[0];
    if (sec === undefined) continue;
    const points = [sec.startPoint, ...(sec.bendPoints ?? []), sec.endPoint].map((p) => ({ x: p.x, y: p.y }));
    if (!points.every((p) => finite(p.x) && finite(p.y))) continue;
    const l = e.labels?.[0];
    const label = l !== undefined && finite(l.x) && finite(l.y) && finite(l.width) && finite(l.height) ? { x: l.x, y: l.y, width: l.width, height: l.height } : null;
    routes.set(e.id, { points, chip: label });
  }
  // ELK's card-to-card stand-ins for chain lines are not drawn.
  for (const id of [...routes.keys()]) if (id.startsWith("cc:")) routes.delete(id);
  // Chain lines are routed here, between the placed members.
  const owner = new Map(m.nodes.map((n) => [n.id, parentOf(n) ?? n.id]));
  for (const e of m.edges) {
    if (e.type !== "chain" && e.type !== "fold") continue;
    const a = boxes.get(e.source);
    const b = boxes.get(e.target);
    const oa = owner.get(e.source) ?? e.source;
    const ob = owner.get(e.target) ?? e.target;
    const ca = boxes.get(oa);
    const cb = boxes.get(ob);
    if (a === undefined || b === undefined || ca === undefined || cb === undefined) continue;
    const points = chainRoute(a, b, oa === ob, ca, cb);
    const text = e.type === "chain" ? chainText(e) : "";
    routes.set(e.id, { points, chip: e.type === "chain" ? chainChip(points, chainChipWidth(text), CHIP_H - 4) : null });
  }
  // Chips sit on their line unless that would cover a card or another
  // chip; then they keep ELK's place beside the line.
  const placedChips: Box[] = [];
  // An opened card holds its members' lines: only real cards repel chips.
  const containers = new Set(m.nodes.filter((n) => n.type === "container").map((n) => n.id));
  const solid = [...boxes].filter(([id]) => !containers.has(id)).map(([, b]) => b);
  for (const [id, r] of routes) {
    if (r.chip === null || id.startsWith("ch:")) continue;
    const centred = onLine(r.points, r.chip);
    const clash = [...solid, ...placedChips].some((b) => overlaps(centred, b, 4));
    const chip = clash ? r.chip : centred;
    placedChips.push(chip);
    routes.set(id, { points: r.points, chip });
  }
  for (const n of m.nodes) if (!boxes.has(n.id)) throw new Error(`layout incomplete: no position for ${n.id}`);
  for (const e of m.edges) if (!routes.has(e.id)) throw new Error(`layout incomplete: no route for ${e.id}`);
  let width = 0;
  let height = 0;
  for (const b of boxes.values()) {
    width = Math.max(width, b.x + b.width);
    height = Math.max(height, b.y + b.height);
  }
  return { boxes, routes, width, height };
}
