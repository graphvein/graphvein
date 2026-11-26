// The ELK side of the Flow tab (T016f D5): the graph handed to ELK and the
// adapter that reads ELK's answer back into positions and routes. Pure, so
// the adapter is tested on recorded ELK output and the real ELK runs only
// in one test (elkjs/lib/elk.bundled.js) and in the browser's worker.
//
// Fan-out without a pile-up of lines: every edge gets its own port, on the
// west side of its target and the east side of its source
// (elk.portConstraints FIXED_SIDE), the node grows to fit its ports
// (elk.nodeSize.constraints [PORTS, MINIMUM_SIZE] with the text size as the
// minimum), ports sit 4 px apart, and routes are orthogonal. Columns are
// ELK partitions, so a call site without a caller still sits with the other
// call sites. The graph is flat; elk.json.edgeCoords ROOT keeps every route
// in root coordinates anyway.
//
// Lines drawn forward: a stored edge whose target's column lies left of its
// source's column (the handler's exposes fact, handler -> route) would make
// ELK route a back edge around the cards. ELK gets such an edge reversed
// (out of the target's east side, into the source's west side), so it is a
// plain left-to-right segment; readLayout turns its route back, so the
// route always runs from the stored source to the stored target and the
// arrowhead stays at the stored target. Nothing else about the edge
// changes: its id, label, tier and facts are the stored ones.

import type { ElkExtendedEdge, ElkNode, ElkPoint } from "elkjs/lib/elk-api";

import { COL, COLUMNS, type FlowGraph, type GraphNode } from "@/sections/api/flow/graph";
import { CARD_MAX_W, captionWidth, cardSize, labelSize } from "@/sections/api/flow/measure";

export const ROOT_OPTIONS: Record<string, string> = {
  "elk.algorithm": "layered",
  "elk.direction": "RIGHT",
  "elk.edgeRouting": "ORTHOGONAL",
  "elk.json.edgeCoords": "ROOT",
  "elk.spacing.portPort": "4",
  "elk.spacing.nodeNode": "16",
  "elk.spacing.edgeEdge": "6",
  "elk.spacing.edgeLabel": "3",
  "elk.layered.spacing.nodeNodeBetweenLayers": "36",
  "elk.layered.spacing.edgeNodeBetweenLayers": "14",
  "elk.layered.spacing.edgeEdgeBetweenLayers": "6",
  "elk.edgeLabels.placement": "HEAD",
  "elk.layered.nodePlacement.strategy": "NETWORK_SIMPLEX",
  "elk.layered.considerModelOrder.strategy": "NODES_AND_EDGES",
  "elk.partitioning.activate": "true",
  "elk.separateConnectedComponents": "false",
};

/** drawnBackwards lists the edges whose target's column lies left of
 * their source's column; ELK lays these out reversed (see above). Computed
 * from the columns only, never from roles or names. */
export function drawnBackwards(g: FlowGraph): Set<string> {
  const column = new Map(g.nodes.map((n) => [n.id, n.column]));
  const out = new Set<string>();
  for (const e of g.edges) {
    const s = column.get(e.source);
    const t = column.get(e.target);
    if (s !== undefined && t !== undefined && t < s) out.add(e.id);
  }
  return out;
}

/** elkGraph is the ELK input for a built graph, its cards sized at the
 * card width cap `cap` (cardCap; CARD_MAX_W when the canvas allows it). */
export function elkGraph(g: FlowGraph, cap: number = CARD_MAX_W): ElkNode {
  const backwards = drawnBackwards(g);
  const ports = new Map<string, { id: string; layoutOptions: Record<string, string>; width: number; height: number }[]>();
  const add = (node: string, id: string, side: "WEST" | "EAST") => {
    const list = ports.get(node) ?? [];
    list.push({ id, layoutOptions: { "elk.port.side": side }, width: 1, height: 1 });
    ports.set(node, list);
  };
  const edges: ElkExtendedEdge[] = g.edges.map((e) => {
    // ELK's source and target: the stored ones, or swapped for a line
    // whose target sits left of its source.
    const back = backwards.has(e.id);
    const from = back ? e.target : e.source;
    const to = back ? e.source : e.target;
    add(from, `${e.id}:out`, "EAST");
    add(to, `${e.id}:in`, "WEST");
    // HEAD places a label near ELK's target; for a reversed edge TAIL keeps
    // it near the stored target, as for every other line.
    const placement = back ? { layoutOptions: { "elk.edgeLabels.placement": "TAIL" } } : {};
    return {
      id: e.id,
      sources: [`${e.id}:out`],
      targets: [`${e.id}:in`],
      labels: e.label === null ? [] : [{ text: e.label, ...labelSize(e.label), ...placement }],
    };
  });
  return {
    id: "root",
    layoutOptions: ROOT_OPTIONS,
    children: g.nodes.map((n) => {
      const size = cardSize(n, cap);
      return {
        id: n.id,
        width: size.width,
        height: size.height,
        ports: ports.get(n.id) ?? [],
        layoutOptions: {
          "elk.partitioning.partition": String(n.column),
          "elk.portConstraints": "FIXED_SIDE",
          "elk.nodeSize.constraints": "[PORTS, MINIMUM_SIZE]",
          "elk.nodeSize.minimum": `(${String(size.width)}, ${String(size.height)})`,
        },
      };
    }),
    edges,
  };
}

export type Viewport = { x: number; y: number; zoom: number };

export interface Box {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface Route {
  /** From the stored source to the stored target (the arrowhead's end). */
  points: ElkPoint[];
  label: (Box & { text: string }) | null;
}

export interface Layout {
  boxes: Map<string, Box>;
  routes: Map<string, Route>;
  width: number;
  height: number;
}

/** LayoutError says ELK's answer lacks a position or route the graph
 * needs; the tab shows an error instead of drawing a partial graph. */
export class LayoutError extends Error {
  constructor(message: string) {
    super(`layout incomplete: ${message}`);
    this.name = "LayoutError";
  }
}

function finite(v: number | undefined): v is number {
  return typeof v === "number" && Number.isFinite(v);
}

/** readLayout reads ELK's answer for graph g. Every card needs a box and
 * every line a route, else LayoutError. Routes of reversed edges are
 * turned back to the stored direction. */
export function readLayout(g: FlowGraph, out: ElkNode): Layout {
  const backwards = drawnBackwards(g);
  const boxes = new Map<string, Box>();
  for (const c of out.children ?? []) {
    if (!finite(c.x) || !finite(c.y) || !finite(c.width) || !finite(c.height)) continue;
    boxes.set(c.id, { x: c.x, y: c.y, width: c.width, height: c.height });
  }
  const routes = new Map<string, Route>();
  for (const e of out.edges ?? []) {
    const sec = e.sections?.[0];
    if (sec === undefined) continue;
    const points = [sec.startPoint, ...(sec.bendPoints ?? []), sec.endPoint];
    if (!points.every((p) => finite(p.x) && finite(p.y))) continue;
    const l = e.labels?.[0];
    const label =
      l !== undefined && finite(l.x) && finite(l.y) && finite(l.width) && finite(l.height)
        ? { x: l.x, y: l.y, width: l.width, height: l.height, text: l.text ?? "" }
        : null;
    const pts = points.map((p) => ({ x: p.x, y: p.y }));
    if (backwards.has(e.id)) pts.reverse();
    routes.set(e.id, { points: pts, label });
  }
  for (const n of g.nodes) if (!boxes.has(n.id)) throw new LayoutError(`no position for ${n.id}`);
  for (const e of g.edges) {
    const r = routes.get(e.id);
    if (r === undefined) throw new LayoutError(`no route for ${e.id}`);
    if (e.label !== null && r.label === null) throw new LayoutError(`no label position for ${e.id}`);
  }
  packTables(g, boxes, routes);
  let width = 0;
  let height = 0;
  for (const b of boxes.values()) {
    width = Math.max(width, b.x + b.width);
    height = Math.max(height, b.y + b.height);
  }
  return { boxes, routes, width: Math.max(width, out.width ?? 0), height: Math.max(height, out.height ?? 0) };
}

/** TABLE_GAP is the vertical gap between packed Tables column cards. */
export const TABLE_GAP = 12;

/** packTables stacks the Tables column (T026 AG-C3) into one compact
 * list after ELK: ELK places each table level with its accessors, which on
 * langflow spreads six tables over a column taller than the canvas. The
 * column keeps ELK's top and order (top to bottom), moves right when the
 * gap before it cannot hold the moved labels, the state
 * boxes go last, and each line into a table keeps ELK's route up to the
 * gap before the column (ELK routed it around every card), then runs
 * along that row to a gutter beside the column, down to the table's
 * middle; its label moves onto that row at the gap's left. Nothing is
 * drawn that is not a stored edge: only the geometry changes. */
export function packTables(g: FlowGraph, boxes: Map<string, Box>, routes: Map<string, Route>): void {
  const cards = g.nodes.filter((n) => n.column === COL.tables && boxes.has(n.id));
  if (cards.length === 0) return;
  const at = (id: string) => boxes.get(id) ?? { x: 0, y: 0, width: 0, height: 0 };
  const into = new Set(cards.map((c) => c.id));
  // The gap's left edge: the right edge of every other card.
  let gap = -Infinity;
  for (const [id, b] of boxes) if (!into.has(id)) gap = Math.max(gap, b.x + b.width);
  cards.sort((a, b) => Number(a.type === "state") - Number(b.type === "state") || at(a.id).y - at(b.id).y || at(a.id).x - at(b.id).x);
  // Room in the gap for the labels moved onto it, then the gutter.
  let labels = 0;
  for (const e of g.edges) if (into.has(e.target)) labels = Math.max(labels, routes.get(e.id)?.label?.width ?? 0);
  const elkX = Math.min(...cards.map((c) => at(c.id).x));
  const x = Number.isFinite(gap) ? Math.max(elkX, gap + labels + 28) : elkX;
  let y = Math.min(...cards.map((c) => at(c.id).y));
  for (const c of cards) {
    const b = at(c.id);
    boxes.set(c.id, { ...b, x, y });
    y += b.height + TABLE_GAP;
  }
  const gutter = Number.isFinite(gap) ? Math.max(gap + 4, x - 12) : x - 12;
  const moved: { id: string; points: Route["points"]; label: Route["label"] }[] = [];
  for (const e of g.edges) {
    if (!into.has(e.target)) continue;
    const r = routes.get(e.id);
    if (r === undefined || r.points.length === 0) continue;
    // ELK's points before the gap (at least the start).
    let last = 0;
    for (let i = 0; i < r.points.length; i++) if ((r.points[i]?.x ?? Infinity) <= gap) last = i;
    const kept = r.points.slice(0, last + 1);
    const row = kept[kept.length - 1] ?? { x: gutter, y: 0 };
    const b = at(e.target);
    const mid = b.y + b.height / 2;
    const points = [...kept, { x: gutter, y: row.y }, { x: gutter, y: mid }, { x, y: mid }];
    const label = r.label === null ? null : { ...r.label, x: Number.isFinite(gap) ? gap + 4 : gutter - r.label.width - 4, y: row.y - r.label.height - 2 };
    moved.push({ id: e.id, points, label });
  }
  separateLabels(moved);
  for (const m of moved) routes.set(m.id, { points: m.points, label: m.label });
}

/** separateLabels keeps the moved table-line labels apart (T026 AG-F):
 * lines from one accessor share a row, so their labels land on one spot.
 * A label with the same text as one already placed over it is dropped
 * (one "reads" per row; each line keeps its own stroke and selection),
 * and a label with another text moves up above the labels it would
 * overlap. Top to bottom, in edge order within a row. */
export function separateLabels(moved: { label: Route["label"] }[]): void {
  const placed: Box[] = [];
  const texts: string[] = [];
  const order = moved.filter((m) => m.label !== null).sort((a, b) => (a.label?.y ?? 0) - (b.label?.y ?? 0));
  const overlaps = (a: Box, b: Box) => a.x < b.x + b.width && b.x < a.x + a.width && a.y < b.y + b.height && b.y < a.y + a.height;
  for (const m of order) {
    const l = m.label;
    if (l === null) continue;
    if (placed.some((p, i) => texts[i] === l.text && overlaps(p, l))) {
      m.label = null;
      continue;
    }
    let y = l.y;
    for (let hit = placed.find((p) => overlaps(p, { ...l, y })); hit !== undefined; hit = placed.find((p) => overlaps(p, { ...l, y }))) y = hit.y - l.height - 2;
    m.label = { ...l, y };
    placed.push(m.label);
    texts.push(l.text);
  }
}

/** focusBox is the box of the cards in columns first..last. The initial
 * view is fitted to the call sites, the endpoint and the handler (the
 * "Fixes to apply": G1 starts fitted to callers-route-handler, with pan
 * hints to what lies outside). */
export function focusBox(g: FlowGraph, layout: Layout, first: number, last: number): Box | null {
  let x0 = Infinity;
  let y0 = Infinity;
  let x1 = -Infinity;
  let y1 = -Infinity;
  for (const n of g.nodes) {
    if (n.column < first || n.column > last) continue;
    const b = layout.boxes.get(n.id);
    if (b === undefined) continue;
    x0 = Math.min(x0, b.x);
    y0 = Math.min(y0, b.y);
    x1 = Math.max(x1, b.x + b.width);
    y1 = Math.max(y1, b.y + b.height);
  }
  if (!Number.isFinite(x0)) return null;
  return { x: x0, y: y0, width: x1 - x0, height: y1 - y0 };
}

/** FIT_TOP is the least room above the cards in view: below the zoom
 * buttons and pan pills (12-44 px), the pinned caption row (48-70 px) and
 * room for a selected top card's toolbar (6 px offset, about 34 px tall),
 * which the opaque caption row would otherwise hide (T016i2 D1.5). */
export const FIT_TOP = 114;

/** NAME_ROOM is how much of a card must show at the canvas's right edge
 * for its name to read when the card does not say (CardBox.name). */
export const NAME_ROOM = 120;

/** NAME_INSET is where a card's name starts: its padding and border. A
 * card cut at the left edge by less than this still shows its whole name. */
export const NAME_INSET = 13;

/** EDGE_GAP is the room left between the canvas's left edge and a card
 * the view is moved to show whole. */
const EDGE_GAP = 8;

/** CardBox is a card's box with, when known, the distance from its left
 * edge to the end of its name (method and title). */
export type CardBox = Box & { name?: number };

/** clipsName says whether a card whose left edge sits at screen x `left`
 * has its name cut by an edge of a canvas `width` wide: its name's start
 * beyond the left edge, or, at the right edge, less of it showing than its
 * name needs. */
function clipsName(left: number, b: CardBox, width: number): boolean {
  const right = left + b.width;
  if (left + NAME_INSET < -0.5 && right > 0.5) return true;
  return left < width - 0.5 && right > width + 0.5 && width - left < Math.min(b.name ?? NAME_ROOM, b.width) - 0.5;
}

/** nameReads says whether a card's name is in the canvas and not cut. */
function nameReads(left: number, b: CardBox, width: number): boolean {
  return left + NAME_INSET >= -0.5 && left <= width - Math.min(b.name ?? NAME_ROOM, b.width) + 0.5;
}

/** inside lists the cards that lie within a box's left and right edges. */
function inside(cards: CardBox[], box: Box): CardBox[] {
  return cards.filter((b) => b.x >= box.x - 0.5 && b.x + b.width <= box.x + box.width + 0.5);
}

/** clearX is the x offset in [lo, hi] at which no card has its name cut
 * by a canvas edge and the most names of the focus's cards read, nearest
 * to `want` among those; `want` clamped to the range when there is none.
 * The offsets where a card stops being cut are the ends of its clipped
 * interval, so only those are tried. */
function clearX(cards: CardBox[], box: Box, width: number, lo: number, hi: number, want: number): number {
  const focus = inside(cards, box);
  const tries = [want, lo, hi];
  for (const b of cards) {
    const name = Math.min(b.name ?? NAME_ROOM, b.width);
    tries.push(-b.x + EDGE_GAP, -b.x - NAME_INSET, -b.x - b.width, width - b.x - name, width - b.x - b.width, width - b.x);
  }
  // Most names read first, then fewest cards cut at the left edge at all
  // (a card cut by less than NAME_INSET still shows its name, but looks
  // cut), then the least move.
  let best: { x: number; read: number; cut: number } | null = null;
  for (const x of tries) {
    if (x < lo - 0.01 || x > hi + 0.01) continue;
    if (cards.some((b) => clipsName(b.x + x, b, width))) continue;
    const read = focus.filter((b) => nameReads(b.x + x, b, width)).length;
    const cut = cards.filter((b) => b.x + x < -0.5 && b.x + x + b.width > 0.5).length;
    const better = best === null || read > best.read || (read === best.read && (cut < best.cut || (cut === best.cut && Math.abs(x - want) < Math.abs(best.x - want))));
    if (better) best = { x, read, cut };
  }
  return best?.x ?? Math.min(hi, Math.max(lo, want));
}

/** clearTop moves a viewport down so that every card in view (horizontally)
 * starts at least `top` px below the canvas's top edge: the column
 * captions and the pills and zoom buttons above them never sit on a card.
 * It never moves the view up. */
export function clearTop(vp: Viewport, cards: Iterable<Box>, width: number, top = FIT_TOP): Viewport {
  let y = Infinity;
  for (const b of cards) {
    const left = b.x * vp.zoom + vp.x;
    const right = left + b.width * vp.zoom;
    if (right > 0 && left < width) y = Math.min(y, b.y);
  }
  if (!Number.isFinite(y)) return vp;
  return { ...vp, y: Math.max(vp.y, top - y * vp.zoom) };
}

/** Focus is what the initial view is fitted to: the box of the call-site,
 * endpoint and handler columns, the endpoint's card (the anchor when the
 * box is wider than the canvas) and every card (so none is cut where its
 * name would not read). */
export interface Focus {
  box: Box;
  anchor: Box | null;
  cards: CardBox[];
}

/** fitViewport places the focus in a canvas at zoom 1: the text floor
 * (12 px) is reached at zoom 1, so fitting never zooms out (UQ7 A). A box
 * that fits is centred; a wider one is centred on the endpoint (the call
 * sites to its left, from the canvas's left edge when the endpoint stays
 * whole beside them; the handler to its right). Within the room that keeps
 * the box (or the endpoint) whole, the view then moves as little as it
 * takes so no card is cut by the left or right edge where its name would
 * not read. Vertically the box is centred or starts at FIT_TOP, and none of
 * its cards in view starts above FIT_TOP (other cards are kept clear of the
 * pills and buttons by clearOverlays once those are drawn). */
export function fitViewport(f: Focus | Box, canvas: { width: number; height: number }, pad = 24): Viewport {
  const { box, anchor, cards } = "box" in f ? f : { box: f, anchor: null, cards: [] };
  const w = canvas.width;
  let lo: number;
  let hi: number;
  let want: number;
  if (box.width + 2 * pad <= w) {
    lo = pad - box.x;
    hi = w - pad - box.x - box.width;
    want = (w - box.width) / 2 - box.x;
  } else if (anchor !== null) {
    // The call sites (the box's left edge) from the left when the endpoint
    // stays whole beside them, else the endpoint centred.
    const start = pad - box.x;
    want = anchor.x + anchor.width + start <= w - pad ? start : w / 2 - (anchor.x + anchor.width / 2);
    lo = Math.min(want, pad - anchor.x);
    hi = Math.max(want, w - pad - anchor.x - anchor.width);
  } else {
    lo = hi = want = pad - box.x;
  }
  const x = clearX(cards, box, w, lo, hi, want);
  const y = Math.max(FIT_TOP, (canvas.height - box.height) / 2) - box.y;
  return clearTop({ x, y, zoom: 1 }, inside(cards, box), w);
}

/** clearOverlays moves a viewport, as little as it takes, up, down, left
 * or right, so that no card lies under a rectangle drawn over the canvas
 * (the pan pills and the zoom buttons, canvas pixels, measured once
 * drawn): a pill never covers a card it panned to. A sideways move pushes
 * the card past the rectangle (for a pill at the right edge, out of the
 * canvas). A move that would take one of the `keep` cards (the endpoint
 * and the handler) out of the canvas while it was inside is not taken;
 * when every move would, the viewport is kept. */
export function clearOverlays(vp: Viewport, cards: Box[], rects: Box[], canvas: { width: number; height: number }, keep: Box[] = [], gap = 8): Viewport {
  const z = vp.zoom;
  const hit = (b: Box, at: { x: number; y: number }, o: Box) => {
    const left = b.x * z + at.x;
    const top = b.y * z + at.y;
    return left < o.x + o.width && left + b.width * z > o.x && top < o.y + o.height && top + b.height * z > o.y;
  };
  const free = (at: { x: number; y: number }) => !cards.some((b) => rects.some((o) => hit(b, at, o)));
  if (free(vp)) return vp;
  const within = (b: Box, at: { x: number; y: number }) =>
    b.x * z + at.x >= 0 && (b.x + b.width) * z + at.x <= canvas.width && b.y * z + at.y >= 0 && (b.y + b.height) * z + at.y <= canvas.height;
  const kept = keep.filter((b) => within(b, vp));
  const tries: { x: number; y: number }[] = [];
  for (const b of cards) {
    for (const o of rects) {
      tries.push({ x: vp.x, y: o.y + o.height + gap - b.y * z }, { x: vp.x, y: o.y - gap - (b.y + b.height) * z });
      tries.push({ x: o.x + o.width + gap - b.x * z, y: vp.y }, { x: o.x - gap - (b.x + b.width) * z, y: vp.y });
    }
  }
  const move = (at: { x: number; y: number }) => Math.abs(at.x - vp.x) + Math.abs(at.y - vp.y);
  tries.sort((a, b) => move(a) - move(b));
  for (const at of tries) if (free(at) && kept.every((b) => within(b, at))) return { ...vp, x: at.x, y: at.y };
  return vp;
}

/** Outside lists the cards a viewport leaves wholly or partly beyond the
 * canvas's left and right edges: the pan pills show when either is not
 * empty. */
export function outside<T extends { box: Box }>(vp: Viewport, cards: Iterable<T>, width: number): { left: T[]; right: T[] } {
  const left: T[] = [];
  const right: T[] = [];
  for (const c of cards) {
    const l = c.box.x * vp.zoom + vp.x;
    const r = l + c.box.width * vp.zoom;
    if (l < -0.5) left.push(c);
    if (r > width + 0.5) right.push(c);
  }
  return { left, right };
}

/** MINIMAP is the overview's size on screen; React Flow keeps a panel
 * PANEL_MARGIN px from the canvas edges. */
export const MINIMAP = { width: 168, height: 104 };
export const PANEL_MARGIN = 15;

/** MAP_MIN_HEIGHT is the shortest canvas that shows the overview by
 * default. Below it the overview's band (104 px plus its margins) and the
 * room for the captions and zoom buttons (FIT_TOP) leave less than three
 * card rows (about 72 px each) clear of it, so it would sit on the cards
 * the canvas is there to show. */
export const MAP_MIN_HEIGHT = 440;

/** MAP_GAP is the least room between a card and the overview. */
const MAP_GAP = 8;

/** minimapRect is the overview's rectangle in canvas pixels, grown by
 * MAP_GAP so a card that touches it counts as covered. */
export function minimapRect(canvas: { width: number; height: number }): Box {
  const width = MINIMAP.width + PANEL_MARGIN + MAP_GAP;
  const height = MINIMAP.height + PANEL_MARGIN + MAP_GAP;
  return { x: canvas.width - width, y: canvas.height - height, width, height };
}

/** covered counts the boxes (graph coordinates) that a viewport puts
 * under a rectangle of the canvas (canvas pixels). */
export function covered(vp: Viewport, boxes: Iterable<Box>, rect: Box): number {
  let n = 0;
  for (const b of boxes) {
    const left = b.x * vp.zoom + vp.x;
    const top = b.y * vp.zoom + vp.y;
    const right = left + b.width * vp.zoom;
    const bottom = top + b.height * vp.zoom;
    if (left < rect.x + rect.width && right > rect.x && top < rect.y + rect.height && bottom > rect.y) n++;
  }
  return n;
}

/** placeView is fitViewport with the overview in the canvas: the plain
 * fit when the overview covers no card there, else the focus fitted into
 * the part of the canvas left of the overview, else above it, and it says
 * whether the overview then covers no card (`mapClear`). A smaller room is
 * taken only when as many of the focus's names read as in the plain
 * fit (a narrow room would otherwise push the call sites out to clear the
 * overview). When no placement keeps every card clear, or the overview is
 * not allowed (`map` false), it is the plain fit and mapClear is false: the
 * caller hides the overview rather than draw it over a card. The zoom
 * buttons and the pan pills are kept clear by FIT_TOP in every case. */
export function placeView(f: Focus, canvas: { width: number; height: number }, map: boolean): { viewport: Viewport; mapClear: boolean } {
  const plain = fitViewport(f, canvas);
  if (map) {
    const rect = minimapRect(canvas);
    if (covered(plain, f.cards, rect) === 0) return { viewport: plain, mapClear: true };
    const inBox = inside(f.cards, f.box);
    const shown = (vp: Viewport) => inBox.filter((b) => nameReads(b.x * vp.zoom + vp.x, b, canvas.width)).length;
    const want = shown(plain);
    for (const room of [
      { width: rect.x, height: canvas.height },
      { width: canvas.width, height: rect.y },
    ]) {
      const viewport = fitViewport(f, room);
      if (covered(viewport, f.cards, rect) === 0 && shown(viewport) >= want) return { viewport, mapClear: true };
    }
  }
  return { viewport: plain, mapClear: false };
}

/** CAPTION_INSET keeps a column caption this far from the canvas's left
 * edge. */
export const CAPTION_INSET = 8;

/** CAPTION_ROW places the pinned caption row (T016i2 D1): its top is the
 * top-left toolbar's bottom plus `gap` (`top` when the toolbar cannot be
 * measured), `height` tall. Its bottom (about 70 px) stays above FIT_TOP,
 * so no card in the first view lies under it. */
export const CAPTION_ROW = { top: 48, gap: 4, height: 22 } as const;

/** captionX is a column caption's screen x (T016i D2.6): the column's left
 * edge, clamped to `minX` (the canvas's left edge plus CAPTION_INSET)
 * while the column is on screen, and never past the column's right edge
 * minus the caption's width, so a caption stays over its own column. All
 * values are screen pixels. */
export function captionX(left: number, right: number, width: number, minX: number): number {
  return Math.min(Math.max(left, minX), right - width);
}

/** CAPTION_MIN_W is the least on-screen column width (px) that still gets
 * a caption: a narrower column's caption is hidden rather than drawn over
 * a neighbouring column (T016i2 review). */
export const CAPTION_MIN_W = 40;

/** CAPTION_ROW_GAP is the room between the caption row and an overlay
 * placed below it (the "No stored callers" note). */
export const CAPTION_ROW_GAP = 8;

/** CaptionPlace is where and how a caption is drawn in the pinned row:
 * screen x, the scale (the zoom below 1, else 1: 12 px at zoom >= 1, as
 * the cards' text), the most screen width it may take (its column's
 * on-screen width) and whether it is cut to that (ellipsis, full text in
 * its title) or hidden (column narrower than CAPTION_MIN_W). */
export interface CaptionPlace {
  x: number;
  scale: number;
  maxWidth: number;
  clipped: boolean;
  hidden: boolean;
}

/** placeCaption places one caption for a viewport (T016i2 D1.3 and review):
 * a caption never extends beyond its own column's on-screen extent.
 * Below zoom 1 it scales with the zoom; it is cut to the column's width
 * when wider; a column narrower than CAPTION_MIN_W on screen hides it.
 * Then the T016i D2.6 clamp (captionX) with the drawn width. */
export function placeCaption(c: Pick<CaptionColumn, "left" | "right" | "width">, vp: Viewport, minX: number = CAPTION_INSET): CaptionPlace {
  const scale = Math.min(vp.zoom, 1);
  const left = c.left * vp.zoom + vp.x;
  const right = c.right * vp.zoom + vp.x;
  const column = right - left;
  const natural = c.width * scale;
  const hidden = column < CAPTION_MIN_W;
  const clipped = !hidden && natural > column;
  const drawn = Math.min(natural, column);
  return { x: captionX(left, right, drawn, minX), scale, maxWidth: drawn, clipped, hidden };
}

/** CaptionColumn is one column caption: its text and width at 12 px and
 * the left and right edge of its column at zoom 1. */
export interface CaptionColumn {
  col: number;
  text: string;
  left: number;
  right: number;
  width: number;
}

/** captionColumns lists one caption per column that has a drawn card,
 * left to right, with the column's edges taken from the drawn cards only
 * (T016i2 D1.4): with 1 hop, the 1-hop cards at their 2-hop places, never
 * the whole 2-hop layout. */
export function captionColumns(placed: Iterable<{ node: GraphNode; box: Box }>): CaptionColumn[] {
  const cols = new Map<number, { left: number; right: number }>();
  for (const { node, box } of placed) {
    const c = cols.get(node.column) ?? { left: Infinity, right: -Infinity };
    cols.set(node.column, { left: Math.min(c.left, box.x), right: Math.max(c.right, box.x + box.width) });
  }
  return [...cols.entries()]
    .sort((a, b) => a[0] - b[0])
    .map(([col, c]) => {
      const text = COLUMNS[col] ?? "";
      return { col, text, ...c, width: captionWidth(text) };
    });
}

/** CHAIN_COLUMNS are the columns the first view must hold whole: the call
 * sites, the endpoint and the handler (focusBox's columns). */
const CHAIN_COLUMNS: readonly number[] = [COL.calls, COL.endpoint, COL.handler];

/** chainColumns lists the cards of each chain column that has cards, left
 * to right: what cardCap sizes the cap for. */
export function chainColumns(g: FlowGraph): GraphNode[][] {
  return CHAIN_COLUMNS.map((col) => g.nodes.filter((n) => n.column === col)).filter((list) => list.length > 0);
}

/** presentPairs lists the adjacent pairs of chain columns that have cards. */
function presentPairs(g: FlowGraph): [number, number][] {
  const present = CHAIN_COLUMNS.filter((col) => g.nodes.some((n) => n.column === col));
  const out: [number, number][] = [];
  for (let i = 1; i < present.length; i++) out.push([present[i - 1] ?? 0, present[i] ?? 0]);
  return out;
}

/** GAP_LABEL_ROOM and GAP_MIN make the estimate of a layer gap before ELK
 * has run: the widest label on the lines between two columns plus
 * GAP_LABEL_ROOM, at least GAP_MIN. On every flow golden this is at least
 * ELK's gap between the columns' extents (layout.test.ts checks it), but a
 * column can hold more than one ELK layer, so it is only the first pass's
 * guess: the chain measured in that layout decides (measuredChain, T016i2
 * D2.2). */
export const GAP_LABEL_ROOM = 74;
export const GAP_MIN = 48;

/** estimatedGap is the estimated sum of the layer gaps between the chain
 * columns, from the edge labels (ELK's gaps depend on them, not on card
 * widths). */
export function estimatedGap(g: FlowGraph): number {
  const column = new Map(g.nodes.map((n) => [n.id, n.column]));
  let sum = 0;
  for (const [a, b] of presentPairs(g)) {
    let label = 0;
    for (const e of g.edges) {
      if (e.label === null) continue;
      const s = column.get(e.source);
      const t = column.get(e.target);
      if ((s === a && t === b) || (s === b && t === a)) label = Math.max(label, labelSize(e.label).width);
    }
    sum += Math.max(GAP_MIN, label + GAP_LABEL_ROOM);
  }
  return sum;
}

/** ChainModel is how wide a layout's chain is at any cap: the chain's
 * cards grouped into ELK layers (a column can hold more than one layer,
 * e.g. a near miss or another target beside the endpoint), and `fixed`,
 * the chain's width minus each layer's widest card at the cap it was laid
 * out at (the gaps between layers plus any offset of a card in its layer).
 * chainWidth(layers, fixed, cap) then equals the chain's width (focusBox
 * over the call sites..handler) at the measured cap exactly, and follows
 * the card widths at other caps (T016i2 review). */
export interface ChainModel {
  layers: GraphNode[][];
  fixed: number;
}

/** measuredChain reads a ChainModel from a layout made at `cap`. Layers are
 * the groups of chain cards whose horizontal extents overlap. */
export function measuredChain(g: FlowGraph, layout: Layout, cap: number): ChainModel {
  const placed = g.nodes
    .filter((n) => CHAIN_COLUMNS.includes(n.column))
    .flatMap((n) => {
      const b = layout.boxes.get(n.id);
      return b === undefined ? [] : [{ n, left: b.x, right: b.x + b.width }];
    })
    .sort((a, b) => a.left - b.left);
  const layers: { nodes: GraphNode[]; left: number; right: number }[] = [];
  for (const p of placed) {
    const last = layers.at(-1);
    if (last !== undefined && p.left < last.right) {
      last.nodes.push(p.n);
      last.right = Math.max(last.right, p.right);
    } else layers.push({ nodes: [p.n], left: p.left, right: p.right });
  }
  const first = layers[0];
  const end = layers.at(-1);
  if (first === undefined || end === undefined) return { layers: [], fixed: 0 };
  const widest = layers.reduce((sum, l) => sum + l.nodes.reduce((m, n) => Math.max(m, cardSize(n, cap).width), 0), 0);
  return { layers: layers.map((l) => l.nodes), fixed: end.right - first.left - widest };
}

