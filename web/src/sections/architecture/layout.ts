// The ELK side of the architecture map (AG2, ADR 0046): card sizes, the
// graph handed to ELK (layered, top to bottom, orthogonal lines, each
// line's chip a centre label so ELK keeps chips off cards and off each
// other) and the reader of ELK's answer. Pure; the real ELK runs in the
// page's worker (sections/api/flow/elk.ts), shared with the Flow tab.
//
// The libraries band (T067) is not ELK's: ELK lays out the card lane, the
// band is a fixed grid below it (placeBand) and its lines are routed here
// on a bus (T072): one trunk per card from its line chip to its own track
// between the card lane and the band, right angles, one port per card on
// each chip.

import type { ElkNode } from "elkjs/lib/elk-api";

import { BAND_ID, MORE_ID, type MapEdge, type MapModel, type MapNode } from "@/sections/architecture/model";
import { LIST_ROWS } from "@/sections/architecture/rows";

export const CARD_W = 288;
/** NARROW_W is the card width above NARROW_CARDS cards (T062 A). */
export const NARROW_W = 180;
export const CARD_H = 100;
/** FAN_H is the room under a card for its line chip into the band. */
export const FAN_H = 36;
export const MARKER_H = 22;
export const EXPANDED_W = 470;
export const EXP_HEADER = 70;
export const ROW_H = 26;
export const FILTER_H = 40;
/** LIST_ROWS_H is the scrolling list's height (LIST_ROWS rows). */
export const LIST_ROWS_H = LIST_ROWS * ROW_H;
export const FOOTER_H = 26;
export const CHIP_H = 24;
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

/** cardHeight is a collapsed component card's height without its fan. */
export function cardHeight(n: Extract<MapNode, { type: "component" }>): number {
  return CARD_H + (n.component.markers.length > 0 || n.component.ingress.length > 0 ? MARKER_H : 0) + (n.across !== null ? 2 * MARKER_H : 0);
}

/** nodeSize is a card's size; expandedRows is the expanded card's row
 * count (null while loading). */
export function nodeSize(n: MapNode, expandedRows: number | null): { width: number; height: number } {
  switch (n.type) {
    case "component": {
      const fan = n.fan !== null ? FAN_H : 0;
      if (n.expanded) return { width: EXPANDED_W, height: expandedHeight(expandedRows) + fan };
      return { width: n.narrow ? NARROW_W : CARD_W, height: cardHeight(n) + fan };
    }
    case "database":
    case "repository":
      return { width: CARD_W, height: CARD_H };
    case "library":
    case "more":
    case "band":
      // Placed by placeBand; a stand-in size for callers that ask.
      return { width: LIB_CHIP_W, height: LIB_CHIP_H };
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

/** GUTTER is the gap between unlinked cards (ELK's component spacing). */
export const GUTTER = 24;

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
  // Gutters between unlinked cards: the band's lines run down them.
  "elk.spacing.componentComponent": String(GUTTER),
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

/** BAND_ASPECT is the card lane's width:height when a band sits below. */
export const BAND_ASPECT = "2.4";

/** inBand says whether the band places a node (not ELK). */
export function inBand(n: MapNode): boolean {
  return n.type === "band" || n.type === "library" || n.type === "more";
}

/** elkGraph is the ELK input for a model. A chain drawn in place gives ELK
 * the opened cards at their inner size (innerLayouts) and one edge per
 * pair of opened cards a chain line joins, so ELK orders them by the
 * chain; the member lines are routed afterwards (chainRoutes). */
export function elkGraph(m: MapModel, expandedRows: number | null, familyText: (f: string) => string): ElkNode {
  const inner = innerLayouts(m);
  const top: ElkNode[] = [];
  for (const n of m.nodes) {
    if (parentOf(n) !== null || inBand(n)) continue;
    const i = inner.get(n.id);
    top.push({ id: n.id, ...(n.type === "container" && i !== undefined ? { width: i.width, height: i.height } : nodeSize(n, expandedRows)) });
  }
  const parent = new Map(m.nodes.map((n) => [n.id, parentOf(n) ?? n.id]));
  const edges: NonNullable<ElkNode["edges"]> = [];
  const pairs = new Set<string>();
  for (const e of m.edges) {
    if (e.band !== undefined) continue;
    if (e.type === "chain" || e.type === "fold") {
      const a = parent.get(e.source) ?? e.source;
      const b = parent.get(e.target) ?? e.target;
      if (a === b || pairs.has(`${a}>${b}`) || pairs.has(`${b}>${a}`)) continue;
      pairs.add(`${a}>${b}`);
      edges.push({ id: `cc:${a}>${b}`, sources: [a], targets: [b], labels: [{ id: `cc:${a}>${b}:chip`, text: "", width: 120, height: CHIP_H }] });
      continue;
    }
    edges.push({ id: e.id, sources: [e.source], targets: [e.target], labels: [{ id: `${e.id}:chip`, text: e.id, width: chipWidth(e, familyText), height: CHIP_H }] });
  }
  // With a band below, unlinked cards pack into a wide lane (the band
  // takes the height); ELK packs components to this width:height ratio.
  const options = m.nodes.some((n) => n.type === "band") ? { ...ROOT_OPTIONS, "elk.aspectRatio": BAND_ASPECT } : ROOT_OPTIONS;
  return { id: "root", layoutOptions: options, children: top, edges };
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
  /** Each band row's top, relative to the band (null: no band). */
  bandRows: number[] | null;
}

/** The band's grid (T062 A, in map units): chips, their gaps, the row
 * label column, the gap between the card lane and the band where the
 * lines turn, and the footer. Chips with a directory line are wider. */
export const LIB_CHIP_W = 120;
export const LIB_CHIP_H = 30;
export const LIB_CHIP_DIR_W = 230;
export const LIB_CHIP_DIR_H = 44;
export const LIB_GAP = 8;
export const BAND_LABEL_W = 112;
export const BAND_PAD = 14;
export const BAND_GAP = 56;
export const BAND_ROW_GAP = 10;
export const BAND_FOOT_H = 40;
/** A row's least height: its two-line label. */
export const BAND_ROW_MIN = 32;
/** The shortest band: the label column and three chips. */
const bandMinWidth = (chipW: number) => 2 * BAND_PAD + BAND_LABEL_W + 3 * chipW + 2 * LIB_GAP;

/** BandBus is the room the bus lines need (T072), or null for a band no
 * card line reaches (the band as T067 placed it). gap: between the card
 * lane and the band; innerGap: between the card row's first and second
 * chip line (the inner bus). */
export interface BandBus {
  gap: number;
  innerGap: number;
}

/** placeBand places the band below the card lane: its rows top to bottom,
 * each row's chips left to right, the "+ n more" chip last. With bus lines
 * (T072) the chip columns are BUS_GAP_X apart (as far as the same number
 * of chips per line still fits) and a card row that wraps fills column by
 * column: its top line holds the libraries of the cards with the fewest
 * libraries, each in the free column nearest to its card (a straight drop
 * from the bus); the rest fill the lines below, fed by the inner bus.
 * lane holds the placed cards. cardLines: the y of each chip line of the
 * card row. */
export function placeBand(m: MapModel, lane: ReadonlyMap<string, Box>, bus: BandBus | null = null): { boxes: Map<string, Box>; rows: number[]; gapX: number; cardLines: number[] } | null {
  const bandNode = m.nodes.find((n) => n.type === "band");
  if (bandNode?.type !== "band") return null;
  const band = bandNode.band;
  const chipW = band.dirs ? LIB_CHIP_DIR_W : LIB_CHIP_W;
  const chipH = band.dirs ? LIB_CHIP_DIR_H : LIB_CHIP_H;
  const placed = [...lane.values()];
  const x0 = placed.length > 0 ? Math.min(...placed.map((b) => b.x)) : 24;
  const right = placed.length > 0 ? Math.max(...placed.map((b) => b.x + b.width)) : 24;
  const top = placed.length > 0 ? Math.max(...placed.map((b) => b.y + b.height)) + (bus?.gap ?? BAND_GAP) : 24;
  const width = Math.max(right - x0, bandMinWidth(chipW));
  const avail = width - 2 * BAND_PAD - BAND_LABEL_W;
  const perLine = Math.max(1, Math.floor((avail + LIB_GAP) / (chipW + LIB_GAP)));
  const gapX = bus === null ? LIB_GAP : Math.max(LIB_GAP, Math.min(BUS_GAP_X, perLine > 1 ? Math.floor((avail - perLine * chipW) / (perLine - 1)) : BUS_GAP_X));
  const left = x0 + BAND_PAD + BAND_LABEL_W;
  // Per chip, the cards using it: their centre x, top and how many
  // libraries each uses.
  interface Owner {
    x: number;
    y: number;
    fan: number;
  }
  const owners = new Map<string, Owner[]>();
  const fanOf = new Map<string, number>();
  for (const e of m.edges) if (e.band === "card") fanOf.set(e.source, (fanOf.get(e.source) ?? 0) + 1);
  for (const e of m.edges) {
    if (e.band !== "card") continue;
    const b = lane.get(e.source);
    if (b === undefined) continue;
    owners.set(e.target, [...(owners.get(e.target) ?? []), { x: b.x + b.width / 2, y: b.y, fan: fanOf.get(e.source) ?? 0 }]);
  }
  const mean = (id: string) => {
    const xs = (owners.get(id) ?? []).map((o) => o.x);
    return xs.length === 0 ? Infinity : xs.reduce((a, x) => a + x, 0) / xs.length;
  };
  const present = new Set(m.nodes.filter((n) => n.type === "library").map((n) => n.id));
  const boxes = new Map<string, Box>();
  const rows: number[] = [];
  const cardLines: number[] = [];
  let y = top + BAND_PAD;
  const more = m.nodes.some((n) => n.id === MORE_ID);
  band.rows.forEach((row, i) => {
    const members = row.members.filter((id) => present.has(id));
    if (row.kind === "card") members.sort((a, b) => mean(a) - mean(b));
    const last = i === band.rows.length - 1;
    const list = last && more ? [...members, MORE_ID] : members;
    rows.push(y - top);
    const columnMajor = bus !== null && row.kind === "card" && list.length > perLine;
    const innerExtra = columnMajor ? Math.max(0, bus.innerGap - LIB_GAP) : 0;
    const base = Math.floor(list.length / perLine);
    const extra = list.length % perLine;
    const cap = (c: number) => base + (c < extra ? 1 : 0);
    if (columnMajor) {
      const cols: string[][] = Array.from({ length: perLine }, () => []);
      const least = (id: string) => (owners.get(id) ?? []).reduce<Owner | null>((a, o) => (a === null || o.fan < a.fan || (o.fan === a.fan && o.y > a.y) ? o : a), null);
      const first = list
        .slice()
        .sort((a, b) => (least(a)?.fan ?? Infinity) - (least(b)?.fan ?? Infinity) || (least(b)?.y ?? 0) - (least(a)?.y ?? 0) || (owners.get(b)?.length ?? 0) - (owners.get(a)?.length ?? 0))
        .slice(0, perLine);
      for (const id of first) {
        const at = least(id)?.x ?? mean(id);
        const want = Number.isFinite(at) ? Math.max(0, Math.min(perLine - 1, Math.round((at - left - chipW / 2) / (chipW + gapX)))) : perLine - 1;
        const free = cols.map((c, k) => (c.length === 0 ? k : -1)).filter((k) => k >= 0);
        const col = free.reduce((a, b) => (Math.abs(b - want) < Math.abs(a - want) ? b : a), free[0] ?? 0);
        cols[col]?.push(id);
      }
      // Below, line by line: chips several cards use first, so their
      // channels spread over the columns.
      const rest = list.filter((x) => !first.includes(x)).sort((a, b) => (owners.get(b)?.length ?? 0) - (owners.get(a)?.length ?? 0));
      for (let l = 1; rest.length > 0 && l <= base; l++) for (let c = 0; c < perLine && rest.length > 0; c++) if (cap(c) > l) cols[c]?.push(rest.shift() ?? "");
      cols.forEach((ids, c) => {
        ids.forEach((id, line) => {
          boxes.set(id, { x: left + c * (chipW + gapX), y: y + line * (chipH + LIB_GAP) + (line > 0 ? innerExtra : 0), width: chipW, height: chipH });
        });
      });
    } else {
      list.forEach((id, k) => {
        boxes.set(id, { x: left + (k % perLine) * (chipW + gapX), y: y + Math.floor(k / perLine) * (chipH + LIB_GAP), width: chipW, height: chipH });
      });
    }
    const lines = Math.max(1, Math.ceil(list.length / perLine));
    if (row.kind === "card") for (let l = 0; l < lines; l++) cardLines.push(y + l * (chipH + LIB_GAP) + (l > 0 ? innerExtra : 0));
    y += Math.max(lines * (chipH + LIB_GAP) - LIB_GAP + (lines > 1 ? innerExtra : 0), BAND_ROW_MIN) + BAND_ROW_GAP;
  });
  boxes.set(BAND_ID, { x: x0, y: top, width, height: y - top + BAND_FOOT_H - BAND_ROW_GAP });
  return { boxes, rows, gapX, cardLines };
}

/** The bus between the card lane and the band (T072, T071 recommendation
 * V1). BUS_PITCH is the distance between two cards' tracks, BUS_PAD the
 * room above the first and below the last track, BUS_GAP_X the chip
 * column gap that holds the channels down to chips below the top line. */
export const BUS_PITCH = 9;
export const BUS_PAD = 12;
/** The room above and below the inner bus's tracks. */
export const INNER_BUS_PAD = 9;
/** The least gap between the card lane and the band with bus lines. */
export const BUS_MIN_GAP = 40;
export const BUS_GAP_X = 20;
/** BUS_SLOT is the distance between two trunks side by side in a gutter
 * or a column gap. */
export const BUS_SLOT = 4.5;

/** busGap is the height between the card lane and the band for n track
 * levels. */
export function busGap(n: number, pitch: number): number {
  return 2 * BUS_PAD + Math.max(0, n - 1) * pitch;
}

/** Track is one horizontal run of a bus: where it is fed from above (fx)
 * and where its lines leave it downwards (drops). */
interface Track {
  key: string;
  fx: number;
  drops: number[];
}

/** trackLevels orders the tracks of one bus so that they cross as little
 * as possible (insertion search over the pairwise crossing counts: a drop
 * of the upper track through the lower track's span, the lower track's
 * feed through the upper's span), then packs tracks whose spans do not
 * overlap onto one level, keeping the order of every overlapping pair.
 * Level 0 is nearest to the chips. */
export function trackLevels(tracks: readonly Track[]): Map<string, number> {
  const span = new Map(tracks.map((t) => [t.key, { lo: Math.min(t.fx, ...t.drops), hi: Math.max(t.fx, ...t.drops) }]));
  const lo = (t: Track) => span.get(t.key)?.lo ?? 0;
  const hi = (t: Track) => span.get(t.key)?.hi ?? 0;
  const inside = (x: number, t: Track) => x > lo(t) + 0.5 && x < hi(t) - 0.5;
  const cross = (up: Track, down: Track) => up.drops.filter((x) => inside(x, down)).length + (inside(down.fx, up) ? 1 : 0);
  const cost = (order: readonly Track[]) => {
    let n = 0;
    for (let i = 0; i < order.length; i++) for (let j = i + 1; j < order.length; j++) n += cross(order[i] as Track, order[j] as Track);
    return n;
  };
  let order = tracks.slice().sort((p, q) => hi(p) - lo(p) - (hi(q) - lo(q)) || p.fx - q.fx || (p.key < q.key ? -1 : 1));
  let best = cost(order);
  for (let improved = true; improved; ) {
    improved = false;
    for (let i = 0; i < order.length; i++) {
      for (let j = 0; j < order.length; j++) {
        if (i === j) continue;
        const next = order.slice();
        const [item] = next.splice(i, 1);
        if (item === undefined) continue;
        next.splice(j, 0, item);
        const c = cost(next);
        if (c < best) {
          best = c;
          order = next;
          improved = true;
        }
      }
    }
  }
  const apart = (p: Track, q: Track) => hi(p) + 2 * BUS_SLOT < lo(q) || hi(q) + 2 * BUS_SLOT < lo(p);
  const level = new Map<string, number>();
  for (let k = order.length - 1; k >= 0; k--) {
    const t = order[k] as Track;
    let v = 0;
    for (let j = k + 1; j < order.length; j++) {
      const u = order[j] as Track;
      if (!apart(t, u)) v = Math.max(v, (level.get(u.key) ?? 0) + 1);
    }
    level.set(t.key, v);
  }
  return level;
}

/** trunkStart is where a card's lines leave it: straight down from its
 * line chip, or, when a card stands below, along a gutter between the
 * cards that no card stands in from just under the card down to the band
 * (the gutter right of the card first, then the nearest other). taken
 * holds the gutter x of the trunks placed so far; the result's x is added. */
function trunkStart(a: Box, lane: readonly Box[], bandTop: number, taken: number[]): { pts: Point[]; fx: number } {
  const sx = a.x + a.width / 2;
  const sy = a.y + a.height;
  const hits = (b: Box, x0: number, y0: number, x1: number, y1: number) => b !== a && Math.max(x0, x1) > b.x - 1 && Math.min(x0, x1) < b.x + b.width + 1 && Math.max(y0, y1) > b.y - 1 && Math.min(y0, y1) < b.y + b.height + 1;
  if (!lane.some((b) => hits(b, sx, sy, sx, bandTop))) return { pts: [{ x: sx, y: sy }], fx: sx };
  const turn = sy + 10;
  const own = a.x + a.width + GUTTER / 2;
  const gutters = [...new Set([own, a.x - GUTTER / 2, ...lane.flatMap((b) => [b.x - GUTTER / 2, b.x + b.width + GUTTER / 2])])].sort((p, q) => Math.abs(p - own) - Math.abs(q - own) || q - p);
  const offsets = [0, BUS_SLOT, -BUS_SLOT, 2 * BUS_SLOT, -2 * BUS_SLOT];
  for (const g of gutters) {
    for (const o of offsets) {
      const gx = g + o;
      if (taken.some((t) => Math.abs(t - gx) < BUS_SLOT - 0.01)) continue;
      if (lane.some((b) => hits(b, sx, turn, gx, turn) || hits(b, gx, turn, gx, bandTop))) continue;
      taken.push(gx);
      return { pts: [{ x: sx, y: sy }, { x: sx, y: turn }, { x: gx, y: turn }], fx: gx };
    }
  }
  // No clear gutter (not seen on a packed lane): the gutter right of it.
  taken.push(own);
  return { pts: [{ x: sx, y: sy }, { x: sx, y: turn }, { x: own, y: turn }], fx: own };
}

/** busRoutes routes every card-library line orthogonally (T072, the T071
 * review's recommendation V1). Each card's lines share one trunk: down
 * from its line chip (along a gutter when a card stands below) to the
 * card's track on the outer bus, between the card lane and the band. From
 * there:
 * - a chip on the card row's top line is entered straight from above, at
 *   one port per card using it, ordered by where the trunks come from;
 * - when the card row wraps, the trunk goes down the column gap nearest
 *   to it to its own track on the inner bus, between the first and the
 *   second chip line, and enters second-line chips from above;
 * - deeper chips (the card row's third line and below, the "+ n more"
 *   chip in a lower row) are entered from the side along a channel in a
 *   column gap, fed from the inner bus (or the outer bus when the card
 *   row has one line). A card's lines in one channel share its slot.
 * cardLines is the y of each chip line of the card row. Returns each
 * line's points and the track levels each bus needs. */
export function busRoutes(m: MapModel, boxes: ReadonlyMap<string, Box>, lane: readonly Box[], bandTop: number, gapX: number, cardLines: readonly number[]): { routes: Map<string, Point[]>; levels: number; inner: number } {
  const out = { routes: new Map<string, Point[]>(), levels: 0, inner: 0 };
  const edges = m.edges.filter((e) => e.band === "card" && boxes.has(e.source) && boxes.has(e.target));
  if (edges.length === 0) return out;
  const chips = m.nodes.filter((n) => n.type === "library" || n.type === "more").flatMap((n) => boxes.get(n.id) ?? []);
  const chipW = chips[0]?.width ?? LIB_CHIP_W;
  const cols = [...new Set(chips.map((b) => Math.round(b.x)))].sort((p, q) => p - q);
  const firstCol = cols[0] ?? 0;
  const secondLine = cardLines[1];
  const hasInner = secondLine !== undefined;
  const lineOf = (b: Box) => {
    const k = cardLines.findIndex((y) => Math.abs(y - b.y) < 1);
    return k === 0 ? 0 : k === 1 ? 1 : 2;
  };
  // Where each card's lines start and where they reach the outer bus.
  const start = new Map<string, { pts: Point[]; fx: number }>();
  const taken: number[] = [];
  for (const id of [...new Set(edges.map((e) => e.source))].sort()) {
    const a = boxes.get(id);
    if (a !== undefined) start.set(id, trunkStart(a, lane, bandTop, taken));
  }
  const fx = (card: string) => start.get(card)?.fx ?? 0;
  // The inner bus's feeders: a card with chips below the top line goes
  // down the column gap nearest to it.
  const gaps = [...cols.map((x) => x - gapX / 2), (cols[cols.length - 1] ?? 0) + chipW + gapX / 2];
  const feeder = new Map<string, number>();
  if (hasInner) {
    const low = [...new Set(edges.filter((e) => lineOf(boxes.get(e.target) as Box) > 0).map((e) => e.source))].sort((p, q) => fx(p) - fx(q));
    const byGap = new Map<number, string[]>();
    for (const card of low) {
      const g = gaps.reduce((p, q) => (Math.abs(q - fx(card)) < Math.abs(p - fx(card)) ? q : p), gaps[0] ?? 0);
      byGap.set(g, [...(byGap.get(g) ?? []), card]);
    }
    // Side by side across the gap in the order the trunks arrive; more
    // than fit at BUS_SLOT draw closer, never out of the gap.
    for (const [g, list] of byGap) {
      const step = Math.min(BUS_SLOT, (gapX - 4) / Math.max(1, list.length));
      list.forEach((card, i) => feeder.set(card, g + (i - (list.length - 1) / 2) * step));
    }
  }
  // Ports from above (top and second line): per chip, its lines ordered by
  // where they come from.
  const from = (e: MapEdge, line: number) => (line === 0 ? fx(e.source) : (feeder.get(e.source) ?? 0));
  const byChip = new Map<string, MapEdge[]>();
  for (const e of edges) byChip.set(e.target, [...(byChip.get(e.target) ?? []), e]);
  const end = new Map<string, { x: number; tail: Point[]; line: number }>();
  // Deep lines: per column gap, the lines entering a chip beside it.
  interface Deep {
    edge: MapEdge;
    chip: Box;
    y: number;
    /** +1: enters the chip rightwards (the gap left of it), -1: leftwards. */
    dir: 1 | -1;
  }
  const channels = new Map<number, Deep[]>();
  for (const [chip, list] of byChip) {
    const b = boxes.get(chip);
    if (b === undefined) continue;
    const line = lineOf(b);
    if (line < 2) {
      list.sort((p, q) => from(p, line) - from(q, line));
      list.forEach((e, i) => {
        const x = b.x + (b.width * (i + 1)) / (list.length + 1);
        end.set(e.id, { x, tail: [{ x, y: b.y }], line });
      });
      continue;
    }
    // A chip in the first column is entered from the right: the gap left
    // of it is the band's row label column.
    const dir: 1 | -1 = Math.round(b.x) === firstCol && cols.length > 1 ? -1 : 1;
    const key = Math.round(dir === 1 ? b.x - gapX / 2 : b.x + b.width + gapX / 2);
    list.sort((p, q) => from(p, 2) - from(q, 2));
    list.forEach((e, i) => {
      channels.set(key, [...(channels.get(key) ?? []), { edge: e, chip: b, y: b.y + (b.height * (i + 1)) / (list.length + 1), dir }]);
    });
  }
  for (const [gx, list] of channels) {
    // One slot per card and direction. A slot turning into its chip
    // crosses the slots beyond it on that side that go on down, so the
    // deeper slots lie on the far side: leftward slots left of the
    // rightward ones, the deepest of each at the outside.
    const slots = new Map<string, { dir: 1 | -1; deepest: number; lines: Deep[] }>();
    for (const d of list) {
      const k = `${String(d.dir)}|${d.edge.source}`;
      const s = slots.get(k) ?? { dir: d.dir, deepest: -Infinity, lines: [] };
      s.deepest = Math.max(s.deepest, d.y);
      s.lines.push(d);
      slots.set(k, s);
    }
    const leftward = [...slots.values()].filter((s) => s.dir === -1).sort((p, q) => p.deepest - q.deepest);
    const rightward = [...slots.values()].filter((s) => s.dir === 1).sort((p, q) => q.deepest - p.deepest);
    const all = [...leftward, ...rightward];
    const step = Math.min(BUS_SLOT, (gapX - 4) / Math.max(1, all.length));
    all.forEach((s, i) => {
      const x = gx + (i - (all.length - 1) / 2) * step;
      for (const d of s.lines) end.set(d.edge.id, { x, tail: [{ x, y: d.y }, { x: d.dir === 1 ? d.chip.x : d.chip.x + d.chip.width, y: d.y }], line: 2 });
    });
  }
  const outer: Track[] = [];
  const inner: Track[] = [];
  for (const card of start.keys()) {
    const mine = edges.filter((e) => e.source === card);
    const f = feeder.get(card);
    // Without an inner bus a deep line drops from the outer bus itself.
    const top = mine.filter((e) => (end.get(e.id)?.line ?? 0) === 0 || f === undefined).map((e) => end.get(e.id)?.x ?? 0);
    outer.push({ key: card, fx: fx(card), drops: f === undefined ? top : [...top, f] });
    if (f !== undefined) inner.push({ key: card, fx: f, drops: mine.filter((e) => (end.get(e.id)?.line ?? 0) > 0).map((e) => end.get(e.id)?.x ?? 0) });
  }
  const outerLevel = trackLevels(outer);
  const innerLevel = trackLevels(inner);
  out.levels = Math.max(0, ...outerLevel.values()) + 1;
  out.inner = inner.length === 0 ? 0 : Math.max(0, ...innerLevel.values()) + 1;
  for (const e of edges) {
    const s = start.get(e.source);
    const t = end.get(e.id);
    if (s === undefined || t === undefined) continue;
    const oy = bandTop - BUS_PAD - (outerLevel.get(e.source) ?? 0) * BUS_PITCH;
    const f = feeder.get(e.source);
    const iy = (secondLine ?? 0) - INNER_BUS_PAD - (innerLevel.get(e.source) ?? 0) * BUS_PITCH;
    const pts = t.line === 0 || f === undefined ? [...s.pts, { x: s.fx, y: oy }, { x: t.x, y: oy }, ...t.tail] : [...s.pts, { x: s.fx, y: oy }, { x: f, y: oy }, { x: f, y: iy }, { x: t.x, y: iy }, ...t.tail];
    out.routes.set(e.id, pts.filter((p, i) => i === 0 || Math.abs(p.x - (pts[i - 1]?.x ?? NaN)) > 0.01 || Math.abs(p.y - (pts[i - 1]?.y ?? NaN)) > 0.01));
  }
  return out;
}

/** betweenRoute joins two library chips: from the user's bottom into the
 * used library's top when it sits lower, else top to bottom. */
export function betweenRoute(a: Box, b: Box): Point[] {
  const ax = a.x + a.width / 2;
  const bx = b.x + b.width / 2;
  if (b.y > a.y + 1) return [{ x: ax, y: a.y + a.height }, { x: bx, y: b.y }];
  if (b.y < a.y - 1) return [{ x: ax, y: a.y }, { x: bx, y: b.y + b.height }];
  return [{ x: ax, y: a.y + a.height }, { x: bx, y: b.y + b.height }];
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
  // The band below the cards, and its lines.
  const laneBoxes = new Map([...boxes].filter(([id]) => !id.startsWith("m:") && !id.startsWith("fold:")));
  // With card lines the band leaves one bus track level per need and the
  // lines are routed on it (busRoutes, T072); the levels depend on the
  // chips' x only, so a first placement counts them and the second leaves
  // the room. Without card lines the band is placed as T067 placed it.
  const fans = m.edges.some((e) => e.band === "card");
  let band = placeBand(m, laneBoxes, fans ? { gap: BUS_MIN_GAP, innerGap: LIB_GAP } : null);
  const lane = [...laneBoxes.values()];
  if (band !== null && fans) {
    const need = busRoutes(m, new Map([...boxes, ...band.boxes]), lane, band.boxes.get(BAND_ID)?.y ?? 0, band.gapX, band.cardLines);
    band = placeBand(m, laneBoxes, { gap: Math.max(busGap(need.levels, BUS_PITCH), BUS_MIN_GAP), innerGap: need.inner === 0 ? LIB_GAP : 2 * INNER_BUS_PAD + (need.inner - 1) * BUS_PITCH });
  }
  if (band !== null) {
    for (const [id, b] of band.boxes) boxes.set(id, b);
    const bandTop = band.boxes.get(BAND_ID)?.y ?? 0;
    const bus = busRoutes(m, boxes, lane, bandTop, band.gapX, band.cardLines).routes;
    for (const e of m.edges) {
      if (e.band === undefined) continue;
      const a = boxes.get(e.source);
      const b = boxes.get(e.target);
      if (a === undefined || b === undefined) continue;
      const points = e.band === "card" ? bus.get(e.id) : betweenRoute(a, b);
      if (points !== undefined) routes.set(e.id, { points, chip: null });
    }
  }
  for (const n of m.nodes) if (!boxes.has(n.id)) throw new Error(`layout incomplete: no position for ${n.id}`);
  for (const e of m.edges) if (!routes.has(e.id)) throw new Error(`layout incomplete: no route for ${e.id}`);
  let width = 0;
  let height = 0;
  for (const b of boxes.values()) {
    width = Math.max(width, b.x + b.width);
    height = Math.max(height, b.y + b.height);
  }
  return { boxes, routes, width, height, bandRows: band?.rows ?? null };
}
