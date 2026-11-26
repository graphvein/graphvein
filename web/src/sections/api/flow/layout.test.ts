// The ELK adapter on recorded output, and one run of the real ELK (the
// bundled build, tests only) on the goldens: every card placed, every
// line routed, labels placed, ports on the right sides.
import ELK from "elkjs/lib/elk.bundled.js";
import type { ElkNode } from "elkjs/lib/elk-api";
import { describe, expect, it } from "vitest";

import { buildGraph, COL, COLUMNS, type FlowGraph, type GraphOptions } from "@/sections/api/flow/graph";
import {
  CAPTION_MIN_W,
  CAPTION_ROW,
  captionColumns,
  captionX,
  chainColumns,
  clearOverlays,
  clearTop,
  covered,
  drawnBackwards,
  elkGraph,
  estimatedGap,
  FIT_TOP,
  fitViewport,
  focusBox,
  LayoutError,
  measuredChain,
  minimapRect,
  NAME_INSET,
  NAME_ROOM,
  outside,
  placeCaption,
  placeView,
  readLayout,
  separateLabels,
  ROOT_OPTIONS,
  type Box,
  type Layout,
  type Route,
  type Viewport,
} from "@/sections/api/flow/layout";
import { CARD_MIN_CAP, captionWidth, cardSize, chainWidth } from "@/sections/api/flow/measure";
import { FILTER_TIERS } from "@/sections/api/flow/tiers";
import { flowGolden, FLOW_GOLDENS, type FlowGoldenName } from "@/test/flowFixtures";

const ALL: GraphOptions = { hops: 2, nearMisses: true, tiers: new Set(FILTER_TIERS) };

function tiny(): FlowGraph {
  const g = buildGraph(flowGolden("flow-route-never"), ALL);
  return g;
}

describe("ELK input", () => {
  it("gives every line its own ports, west in and east out, and sizes nodes from their text", () => {
    const f = flowGolden("flow-graph");
    const g = buildGraph(f, ALL);
    const input = elkGraph(g);
    const back = drawnBackwards(g);
    expect(input.layoutOptions).toEqual(ROOT_OPTIONS);
    expect(ROOT_OPTIONS["elk.json.edgeCoords"]).toBe("ROOT");
    expect(ROOT_OPTIONS["elk.spacing.portPort"]).toBe("4");
    expect(ROOT_OPTIONS["elk.edgeRouting"]).toBe("ORTHOGONAL");
    const ports = new Map<string, string>();
    for (const c of input.children ?? []) {
      expect(c.layoutOptions?.["elk.portConstraints"]).toBe("FIXED_SIDE");
      expect(c.layoutOptions?.["elk.nodeSize.constraints"]).toBe("[PORTS, MINIMUM_SIZE]");
      const card = g.nodes.find((n) => n.id === c.id);
      if (card === undefined) throw new Error(c.id);
      const size = cardSize(card);
      expect(c.layoutOptions?.["elk.nodeSize.minimum"]).toBe(`(${String(size.width)}, ${String(size.height)})`);
      expect(c.layoutOptions?.["elk.partitioning.partition"]).toBe(String(card.column));
      for (const p of c.ports ?? []) ports.set(p.id, `${c.id} ${p.layoutOptions?.["elk.port.side"] ?? ""}`);
    }
    expect(input.edges).toHaveLength(g.edges.length);
    for (const e of input.edges ?? []) {
      const line = g.edges.find((x) => x.id === e.id);
      // A line whose target sits left of its source goes to ELK reversed.
      const [from, to] = back.has(e.id) ? [line?.target, line?.source] : [line?.source, line?.target];
      expect(ports.get(e.sources[0] ?? "")).toBe(`${from ?? ""} EAST`);
      expect(ports.get(e.targets[0] ?? "")).toBe(`${to ?? ""} WEST`);
      expect(e.labels?.length ?? 0).toBe(line?.label === null ? 0 : 1);
    }
    // One port per line end, so a fan-in never shares a port.
    expect(ports.size).toBe(2 * g.edges.length);
  });
});

describe("lines whose target sits left of their source", () => {
  it("are found from the columns, the handler's exposes fact among them", () => {
    for (const name of Object.keys(FLOW_GOLDENS) as FlowGoldenName[]) {
      const f = flowGolden(name);
      const g = buildGraph(f, ALL);
      const col = new Map(g.nodes.map((n) => [n.id, n.column]));
      const back = drawnBackwards(g);
      for (const e of g.edges) {
        const left = (col.get(e.target) ?? 0) < (col.get(e.source) ?? 0);
        expect(back.has(e.id), `${name} ${e.id}`).toBe(left);
      }
      const handler = f.handler.node?.identity;
      const exposes = g.edges.find((e) => e.source === handler && e.target === f.endpoint.identity);
      if (exposes !== undefined) expect(back.has(exposes.id), name).toBe(true);
    }
  });

  it("are laid out reversed and drawn as a straight segment ending at the stored target", async () => {
    const f = flowGolden("flow-route");
    const g = buildGraph(f, ALL);
    const back = [...drawnBackwards(g)];
    expect(back.length).toBeGreaterThan(0);
    const l = readLayout(g, await new ELK().layout(elkGraph(g)));
    for (const id of back) {
      const e = g.edges.find((x) => x.id === id);
      const pts = l.routes.get(id)?.points ?? [];
      const src = l.boxes.get(e?.source ?? "");
      const tgt = l.boxes.get(e?.target ?? "");
      const first = pts[0];
      const last = pts[pts.length - 1];
      if (src === undefined || tgt === undefined || first === undefined || last === undefined) throw new Error(id);
      // Starts on the stored source's west side and ends on the stored
      // target's east side, where the arrowhead is drawn (ports are 1 px).
      expect(Math.abs(first.x - src.x)).toBeLessThanOrEqual(1.5);
      expect(Math.abs(last.x - (tgt.x + tgt.width))).toBeLessThanOrEqual(1.5);
      // Never runs past either card: no loop around them.
      for (const p of pts) {
        expect(p.x).toBeGreaterThanOrEqual(tgt.x + tgt.width - 1.5);
        expect(p.x).toBeLessThanOrEqual(src.x + 1.5);
      }
    }
  });

  it("have their recorded route turned back to the stored direction", () => {
    const g = buildGraph(flowGolden("flow-route"), ALL);
    const id = [...drawnBackwards(g)][0] ?? "";
    const out: ElkNode = {
      id: "root",
      children: g.nodes.map((n) => ({ id: n.id, x: 0, y: 0, width: 10, height: 10 })),
      edges: g.edges.map((e) => ({
        id: e.id,
        sources: [],
        targets: [],
        sections: [{ id: `${e.id}_s0`, startPoint: { x: 1, y: 5 }, endPoint: { x: 9, y: 5 } }],
        labels: e.label === null ? [] : [{ text: e.label, x: 0, y: 0, width: 5, height: 5 }],
      })),
    };
    const l = readLayout(g, out);
    expect(l.routes.get(id)?.points).toEqual([
      { x: 9, y: 5 },
      { x: 1, y: 5 },
    ]);
    const forward = g.edges.find((e) => e.id !== id);
    if (forward !== undefined)
      expect(l.routes.get(forward.id)?.points).toEqual([
        { x: 1, y: 5 },
        { x: 9, y: 5 },
      ]);
  });
});

describe("ELK adapter", () => {
  it("reads positions, routes and labels from recorded output", () => {
    const g = tiny();
    const out: ElkNode = {
      id: "root",
      width: 500,
      height: 120,
      children: g.nodes.map((n, i) => ({ id: n.id, x: 10 + 200 * i, y: 20, width: 150, height: 52 })),
      edges: g.edges.map((e) => ({
        id: e.id,
        sources: [],
        targets: [],
        sections: [{ id: `${e.id}_s0`, startPoint: { x: 160, y: 40 }, bendPoints: [{ x: 180, y: 40 }, { x: 180, y: 60 }], endPoint: { x: 210, y: 60 } }],
        labels: e.label === null ? [] : [{ text: e.label, x: 170, y: 30, width: 60, height: 20 }],
      })),
    };
    const l = readLayout(g, out);
    expect(l.boxes.get(g.nodes[0]?.id ?? "")).toEqual({ x: 10, y: 20, width: 150, height: 52 });
    const back = drawnBackwards(g);
    for (const e of g.edges) {
      const elkRoute = [
        { x: 160, y: 40 },
        { x: 180, y: 40 },
        { x: 180, y: 60 },
        { x: 210, y: 60 },
      ];
      // A reversed edge's route reads from the stored source.
      expect(l.routes.get(e.id)?.points).toEqual(back.has(e.id) ? elkRoute.reverse() : elkRoute);
      if (e.label !== null) expect(l.routes.get(e.id)?.label).toEqual({ x: 170, y: 30, width: 60, height: 20, text: e.label });
    }
    expect(l.width).toBe(Math.max(500, 10 + 200 * (g.nodes.length - 1) + 150));
  });

  it("refuses an answer without a position or a route", () => {
    const g = tiny();
    expect(() => readLayout(g, { id: "root", children: [], edges: [] })).toThrow(LayoutError);
    const out: ElkNode = { id: "root", children: g.nodes.map((n) => ({ id: n.id, x: 0, y: 0, width: 1, height: 1 })), edges: [] };
    if (g.edges.length > 0) expect(() => readLayout(g, out)).toThrow(/no route/);
  });

  it("fits a bare box at zoom 1 at most: centred when it fits, from its left edge when wider", () => {
    expect(fitViewport({ x: 0, y: 0, width: 400, height: 200 }, { width: 1000, height: 600 })).toEqual({ x: 300, y: 200, zoom: 1 });
    // Taller than the canvas: the top card starts below the captions' room.
    expect(fitViewport({ x: 50, y: 10, width: 3000, height: 2000 }, { width: 1000, height: 600 })).toEqual({ x: -26, y: FIT_TOP - 10, zoom: 1 });
    expect(fitViewport({ x: 0, y: 0, width: 400, height: 500 }, { width: 1000, height: 600 }).y).toBe(FIT_TOP);
  });

  it("keeps every card clear of the overview, or says it must be hidden", () => {
    const canvas = { width: 1000, height: 600 };
    const map = minimapRect(canvas);
    // The overview's rectangle: its size, React Flow's 15 px margin and an
    // 8 px gap, in the bottom-right corner.
    expect(map).toEqual({ x: 1000 - 191, y: 600 - 127, width: 191, height: 127 });
    // The plain fit puts the second card under the overview; placeView
    // fits into the room left of it instead.
    const cards = [
      { x: 0, y: 0, width: 300, height: 60 },
      { x: 400, y: 400, width: 300, height: 60 },
    ];
    const box = { x: 0, y: 0, width: 700, height: 460 };
    const f = { box, anchor: null, cards };
    expect(covered(fitViewport(f, canvas), cards, map)).toBe(1);
    const placed = placeView(f, canvas, true);
    expect(placed).toEqual({ viewport: fitViewport(f, { width: map.x, height: 600 }), mapClear: true });
    expect(covered(placed.viewport, cards, map)).toBe(0);
    // Wider than the room left of the overview but short: placed above it.
    const flat = [{ x: 0, y: 0, width: 1400, height: 60 }];
    const above = placeView({ box: { x: 0, y: 0, width: 1400, height: 60 }, anchor: null, cards: flat }, canvas, true);
    expect(above.mapClear).toBe(true);
    expect(covered(above.viewport, flat, map)).toBe(0);
    // Neither keeps every card clear: the plain fit, and the overview must
    // not be shown.
    const big = { x: 0, y: 0, width: 3000, height: 2000 };
    const fb = { box: big, anchor: null, cards: [big] };
    expect(placeView(fb, canvas, true)).toEqual({ viewport: fitViewport(fb, canvas), mapClear: false });
    // Not allowed (a short canvas, the peek open): never clear.
    expect(placeView(f, canvas, false)).toEqual({ viewport: fitViewport(f, canvas), mapClear: false });
  });
});

// The initial viewport on hand-made columns (x, width): hop-2 callers,
// enclosing functions, call sites, endpoint, handler, callees, each card
// 60 px tall at y 0 unless given.
function col(x: number, width: number, y = 0): Box {
  return { x, y, width, height: 60 };
}
const CALLERS = col(0, 250);
const ENCLOSING = col(290, 250);
const CALLS = col(580, 250);
const ENDPOINT = col(870, 200);
const HANDLER = col(1110, 250);
const CALLEES = col(1400, 250);

function onScreen(vp: Viewport, b: Box): { left: number; right: number; top: number } {
  return { left: b.x * vp.zoom + vp.x, right: (b.x + b.width) * vp.zoom + vp.x, top: b.y * vp.zoom + vp.y };
}

function whole(vp: Viewport, b: Box, width: number): boolean {
  const s = onScreen(vp, b);
  return s.left >= 0 && s.right <= width;
}

// No card cut at the left edge; at the right edge at least NAME_ROOM shows.
function namesRead(vp: Viewport, cards: Box[], width: number): boolean {
  return cards.every((b) => {
    const s = onScreen(vp, b);
    if (s.left < 0 && s.right > 0) return false;
    return !(s.left < width && s.right > width && width - s.left < Math.min(NAME_ROOM, b.width));
  });
}

function span(cards: Box[]): Box {
  const x0 = Math.min(...cards.map((b) => b.x));
  const y0 = Math.min(...cards.map((b) => b.y));
  const x1 = Math.max(...cards.map((b) => b.x + b.width));
  const y1 = Math.max(...cards.map((b) => b.y + b.height));
  return { x: x0, y: y0, width: x1 - x0, height: y1 - y0 };
}

describe("the initial viewport", () => {
  const canvas = { width: 1300, height: 700 };

  it("with hop-2 columns: fits call sites, endpoint and handler, no name cut at an edge", () => {
    const cards = [CALLERS, ENCLOSING, CALLS, ENDPOINT, HANDLER, CALLEES];
    const box = span([CALLS, ENDPOINT, HANDLER]);
    const vp = fitViewport({ box, anchor: ENDPOINT, cards }, canvas);
    expect(vp.zoom).toBe(1);
    for (const b of [CALLS, ENDPOINT, HANDLER]) expect(whole(vp, b, canvas.width)).toBe(true);
    expect(namesRead(vp, cards, canvas.width)).toBe(true);
    // Centred would cut the enclosing functions at the left edge (x -320);
    // the nearest offset that shows them whole is taken.
    expect(vp.x).toBe(-282);
    // The hop-2 callers lie to the left, the callees partly to the right:
    // both pills show.
    const out = outside(vp, cards.map((b, i) => ({ box: b, i })), canvas.width);
    expect(out.left.map((c) => c.i)).toEqual([0]);
    expect(out.right.map((c) => c.i)).toEqual([5]);
  });

  it("without the caller columns (never called): centred, nothing outside", () => {
    const cards = [ENDPOINT, HANDLER];
    const box = span(cards);
    const vp = fitViewport({ box, anchor: ENDPOINT, cards }, canvas);
    expect(vp).toEqual({ x: (canvas.width - box.width) / 2 - box.x, y: (canvas.height - 60) / 2, zoom: 1 });
    const out = outside(vp, cards.map((b) => ({ box: b })), canvas.width);
    expect(out.left).toHaveLength(0);
    expect(out.right).toHaveLength(0);
  });

  it("at 1 hop: call sites, endpoint, handler and callees, centred on the three", () => {
    // 1 hop draws no hop-2 callers or enclosing functions: the call sites
    // are the first column.
    const shift = (b: Box) => ({ ...b, x: b.x - CALLS.x });
    const cards = [CALLS, ENDPOINT, HANDLER, CALLEES].map(shift);
    const box = span(cards.slice(0, 3));
    const vp = fitViewport({ box, anchor: cards[1] ?? null, cards }, canvas);
    expect(vp.x).toBe((canvas.width - box.width) / 2 - box.x);
    expect(namesRead(vp, cards, canvas.width)).toBe(true);
    const out = outside(vp, cards.map((b) => ({ box: b })), canvas.width);
    expect(out.left).toHaveLength(0);
    expect(out.right).toHaveLength(1);
  });

  it("on a narrow canvas: the call sites from the left, the endpoint whole beside them", () => {
    const narrow = { width: 700, height: 700 };
    const cards = [CALLERS, ENCLOSING, CALLS, ENDPOINT, HANDLER, CALLEES];
    const box = span([CALLS, ENDPOINT, HANDLER]);
    const vp = fitViewport({ box, anchor: ENDPOINT, cards }, narrow);
    expect(vp.zoom).toBe(1);
    expect(whole(vp, ENDPOINT, narrow.width)).toBe(true);
    expect(namesRead(vp, cards, narrow.width)).toBe(true);
    // The call sites start at the left padding; the handler's name still
    // reads at the right edge.
    expect(vp.x).toBe(24 - CALLS.x);
    expect(onScreen(vp, HANDLER).left).toBeLessThan(narrow.width - NAME_ROOM);
    expect(onScreen(vp, CALLS).right).toBeLessThan(onScreen(vp, ENDPOINT).left);
  });

  it("on a canvas too narrow for the call sites beside the endpoint: the endpoint centred, no name cut", () => {
    const narrow = { width: 500, height: 700 };
    const cards = [CALLERS, ENCLOSING, CALLS, ENDPOINT, HANDLER, CALLEES];
    const box = span([CALLS, ENDPOINT, HANDLER]);
    const vp = fitViewport({ box, anchor: ENDPOINT, cards }, narrow);
    expect(whole(vp, ENDPOINT, narrow.width)).toBe(true);
    expect(namesRead(vp, cards, narrow.width)).toBe(true);
    // Centred (x -720) cuts the call site at the left (-140..110); the call
    // site moves fully out instead, the endpoint still whole.
    expect(onScreen(vp, CALLS).right).toBeLessThanOrEqual(0);
  });

  it("with wide call sites: every name of the call sites, the endpoint and the handler reads", () => {
    // Langflow's acceptance route at 1440x900: the three columns are wider
    // than the canvas at zoom 1. The handler's name needs 82 px; showing
    // it cuts the widest call site by less than its padding, so every
    // name still reads.
    const wide = { width: 1284, height: 700 };
    const call = { ...col(0, 572), name: 300 };
    const call2 = { ...col(48, 500), name: 170 };
    const endpoint = { ...col(752, 356), name: 230 };
    const handler = { ...col(1210, 356), name: 82 };
    const cards = [call, call2, endpoint, handler];
    const vp = fitViewport({ box: span(cards), anchor: endpoint, cards }, wide);
    expect(vp.x).toBe(wide.width - handler.x - handler.name);
    expect(onScreen(vp, call).left + NAME_INSET).toBeGreaterThanOrEqual(0);
    expect(whole(vp, endpoint, wide.width)).toBe(true);
    expect(wide.width - onScreen(vp, handler).left).toBeGreaterThanOrEqual(handler.name);
  });

  it("keeps the call sites rather than fit the room left of the overview", () => {
    // The plain fit starts at the call sites and puts a low handler-column
    // card under the overview; the room left of it is too narrow for the
    // call sites beside the endpoint, so fitting there would push them
    // out: the plain fit is kept and the overview is hidden.
    const calls = col(0, 500);
    const endpoint = col(720, 380);
    const handler = col(1140, 350);
    const low = col(1140, 350, 450);
    const cards = [calls, endpoint, handler, low];
    const f = { box: span(cards), anchor: endpoint, cards };
    const map = minimapRect(canvas);
    const plain = fitViewport(f, canvas);
    expect(covered(plain, cards, map)).toBe(1);
    expect(onScreen(plain, calls).left).toBe(24);
    expect(onScreen(fitViewport(f, { width: map.x, height: canvas.height }), calls).right).toBeLessThanOrEqual(0);
    expect(placeView(f, canvas, true)).toEqual({ viewport: plain, mapClear: false });
  });

  it("keeps the focus's cards in view below the captions' room, never moving up", () => {
    // A card above the call sites, in the focus columns: the view moves down.
    const high = col(580, 250, -400);
    const cards = [high, CALLS, ENDPOINT, HANDLER];
    const box = span([CALLS, ENDPOINT, HANDLER]);
    const vp = fitViewport({ box, anchor: ENDPOINT, cards }, canvas);
    expect(onScreen(vp, high).top).toBe(FIT_TOP);
    // A card out of view does not count.
    expect(clearTop({ x: -2000, y: 10, zoom: 1 }, [high], canvas.width)).toEqual({ x: -2000, y: 10, zoom: 1 });
    expect(clearTop({ x: 0, y: 400 + FIT_TOP + 6, zoom: 1 }, [high], canvas.width).y).toBe(400 + FIT_TOP + 6);
    expect(clearTop({ x: 0, y: 0, zoom: 1 }, [high], canvas.width, 120).y).toBe(520);
  });
});

describe("clearing the pills and buttons", () => {
  // A pill in the top-right corner of a 1000 x 600 canvas.
  const pill = { x: 800, y: 12, width: 180, height: 28 };
  const canvas = { width: 1000, height: 600 };

  it("moves the view as little as it takes so no card lies under a pill", () => {
    // A card under the pill: moved down below it (its top 8 px under the
    // pill), the smallest move.
    const under = col(700, 200, 10);
    const vp = clearOverlays({ x: 0, y: 0, zoom: 1 }, [under], [pill], canvas);
    expect(vp).toEqual({ x: 0, y: 12 + 28 + 8 - 10, zoom: 1 });
    // Moving up is the smaller move for a card that only touches the
    // pill's top.
    const high = col(700, 200, -40);
    expect(clearOverlays({ x: 0, y: 0, zoom: 1 }, [high], [pill], canvas).y).toBe(12 - 8 - 20);
    // Nothing under a pill: unchanged.
    const free = { x: 0, y: 0, zoom: 1 };
    expect(clearOverlays(free, [col(0, 200, 100)], [pill], canvas)).toBe(free);
  });

  it("moves sideways when moving up or down would push the handler out", () => {
    // A tall callee column under the pill (the largest flow after "Handler
    // + downstream"): up or down would take the handler out of the canvas,
    // so the column is pushed right, past the pill and out of the canvas.
    const tower = Array.from({ length: 10 }, (_, i) => col(900, 300, 20 + 70 * i));
    const handler = col(24, 300, 530);
    const vp = clearOverlays({ x: 0, y: 0, zoom: 1 }, [...tower, handler], [pill], canvas, [handler]);
    expect(vp).toEqual({ x: pill.x + pill.width + 8 - 900, y: 0, zoom: 1 });
    // No move keeps the handler in: the view is kept.
    const wide = col(0, 1000, 530);
    const kept = { x: 0, y: 0, zoom: 1 };
    expect(clearOverlays(kept, [...tower, wide], [pill], canvas, [wide])).toBe(kept);
  });
});

describe("the real ELK", () => {
  it("places every card and routes every line of every golden, both hop settings", async () => {
    const elk = new ELK();
    for (const name of Object.keys(FLOW_GOLDENS) as FlowGoldenName[]) {
      const f = flowGolden(name);
      for (const hops of [1, 2] as const) {
        const g = buildGraph(f, { ...ALL, hops });
        const l = readLayout(g, await elk.layout(elkGraph(g)));
        for (const n of g.nodes) {
          const b = l.boxes.get(n.id);
          const size = cardSize(n);
          expect(b?.width).toBeGreaterThanOrEqual(size.width);
          expect(b?.height).toBeGreaterThanOrEqual(size.height);
        }
        // Columns are kept left to right.
        const byCol = new Map<number, number[]>();
        for (const n of g.nodes) byCol.set(n.column, [...(byCol.get(n.column) ?? []), l.boxes.get(n.id)?.x ?? NaN]);
        const cols = [...byCol.keys()].sort((a, b) => a - b);
        for (let i = 1; i < cols.length; i++) {
          const left = Math.max(...(byCol.get(cols[i - 1] ?? -1) ?? []));
          const right = Math.min(...(byCol.get(cols[i] ?? -1) ?? []));
          expect(right, `${name} column ${String(cols[i])}`).toBeGreaterThan(left);
        }
        for (const e of g.edges) {
          const r = l.routes.get(e.id);
          expect(r?.points.length).toBeGreaterThanOrEqual(2);
          // Orthogonal: every segment is horizontal or vertical (ELK leaves a
          // sub-pixel step on self-loops).
          const pts = r?.points ?? [];
          for (let i = 1; i < pts.length; i++) {
            const a = pts[i - 1];
            const b = pts[i];
            expect(a !== undefined && b !== undefined && (Math.abs(a.x - b.x) < 0.5 || Math.abs(a.y - b.y) < 0.5)).toBe(true);
          }
        }
        const focus = focusBox(g, l, 3, 5);
        expect(focus).not.toBeNull();
      }
    }
  }, 60_000);
});

describe("column captions (T016i D2.6)", () => {
  it("sits at the column's left edge while that is in view", () => {
    expect(captionX(120, 480, 90, 8)).toBe(120);
  });
  it("is clamped to the canvas edge, or past the toolbar, while the column is on screen", () => {
    expect(captionX(-200, 300, 90, 8)).toBe(8);
    expect(captionX(-200, 300, 90, 360)).toBe(210);
  });
  it("never leaves its column: at most the right edge minus its width", () => {
    expect(captionX(-300, 50, 90, 8)).toBe(-40);
    expect(captionX(-300, -10, 90, 8)).toBe(-100);
  });
});

describe("caption columns (T016i2 D1.4)", () => {
  it("gives one caption per column with a drawn card, its edges from the drawn cards only", async () => {
    const f = flowGolden("flow-graph");
    const g2 = buildGraph(f, ALL);
    const g1 = buildGraph(f, { ...ALL, hops: 1 });
    const l = readLayout(g2, await new ELK().layout(elkGraph(g2)));
    const placed = (g: FlowGraph) => g.nodes.flatMap((node) => {
      const box = l.boxes.get(node.id);
      return box === undefined ? [] : [{ node, box }];
    });
    const two = captionColumns(placed(g2));
    const one = captionColumns(placed(g1));
    expect(two.map((c) => c.col)).toEqual([...new Set(g2.nodes.map((n) => n.column))].sort((a, b) => a - b));
    expect(one.map((c) => c.col)).toEqual([...new Set(g1.nodes.map((n) => n.column))].sort((a, b) => a - b));
    expect(one.length).toBeLessThan(two.length);
    for (const c of one) {
      const boxes = g1.nodes.filter((n) => n.column === c.col).map((n) => l.boxes.get(n.id) as Box);
      expect(c.left).toBe(Math.min(...boxes.map((b) => b.x)));
      expect(c.right).toBe(Math.max(...boxes.map((b) => b.x + b.width)));
      expect(c.text).toBe(COLUMNS[c.col]);
      expect(c.width).toBe(captionWidth(c.text));
    }
  });

  it("draws no caption for a column without cards", () => {
    const n = buildGraph(flowGolden("flow-route-never"), ALL).nodes[0];
    if (n === undefined) throw new Error("no node");
    expect(captionColumns([])).toEqual([]);
    expect(captionColumns([{ node: n, box: { x: 10, y: 0, width: 200, height: 40 } }])).toEqual([
      { col: n.column, text: COLUMNS[n.column], left: 10, right: 210, width: captionWidth(COLUMNS[n.column] ?? "") },
    ]);
  });
});

/** columnGaps sums the gaps between the chain columns' extents. */
function columnGaps(g: FlowGraph, l: Layout): number {
  const ext = new Map<number, { left: number; right: number }>();
  for (const n of g.nodes) {
    const b = l.boxes.get(n.id);
    if (b === undefined) continue;
    const e = ext.get(n.column) ?? { left: Infinity, right: -Infinity };
    ext.set(n.column, { left: Math.min(e.left, b.x), right: Math.max(e.right, b.x + b.width) });
  }
  const present = [COL.calls, COL.endpoint, COL.handler].filter((c) => ext.has(c));
  let sum = 0;
  for (let i = 1; i < present.length; i++) sum += (ext.get(present[i] ?? 0)?.left ?? 0) - (ext.get(present[i - 1] ?? 0)?.right ?? 0);
  return sum;
}

describe("chain gaps (T016i2 D2.2)", () => {
  it("models the chain exactly at the cap it was laid out at, columns of several ELK layers and mixed card widths included", async () => {
    const elk = new ELK();
    let multi = 0;
    for (const name of Object.keys(FLOW_GOLDENS) as FlowGoldenName[]) {
      const g = buildGraph(flowGolden(name), ALL);
      for (const cap of [CARD_MIN_CAP, 296, 360]) {
        const l = readLayout(g, await elk.layout(elkGraph(g, cap)));
        const m = measuredChain(g, l, cap);
        const box = focusBox(g, l, COL.calls, COL.handler);
        expect(chainWidth(m.layers, m.fixed, cap), `${name} cap ${String(cap)}`).toBeCloseTo(box?.width ?? 0, 6);
        if (m.layers.length > chainColumns(g).length) multi++;
      }
    }
    // flow-graph's call-site and endpoint columns hold two ELK layers each,
    // with cards of different widths: the case the per-column model missed.
    expect(multi).toBeGreaterThan(0);
  }, 60_000);

  it("estimates at least ELK's gaps between the chain columns on every golden, both hop settings", async () => {
    const elk = new ELK();
    for (const name of Object.keys(FLOW_GOLDENS) as FlowGoldenName[]) {
      for (const hops of [1, 2] as const) {
        const g = buildGraph(flowGolden(name), { ...ALL, hops });
        for (const cap of [360, CARD_MIN_CAP]) {
          const l = readLayout(g, await elk.layout(elkGraph(g, cap)));
          expect(estimatedGap(g), `${name} ${String(hops)} hops cap ${String(cap)}`).toBeGreaterThanOrEqual(columnGaps(g, l));
        }
      }
    }
  }, 60_000);

  it("lists the chain columns that have cards, left to right", () => {
    const g = buildGraph(flowGolden("flow-graph"), ALL);
    const cols = chainColumns(g);
    expect(cols.map((list) => list[0]?.column)).toEqual([COL.calls, COL.endpoint, COL.handler]);
    const never = buildGraph(flowGolden("flow-route-never"), ALL);
    expect(chainColumns(never).map((list) => list[0]?.column)).toEqual([COL.endpoint, COL.handler]);
  });

  it("sizes ELK's nodes at the cap it is given", () => {
    const f = flowGolden("flow-route");
    const long = "src/frontend/src/controllers/API/queries/flows/use-get-download-flows.ts";
    for (const n of f.nodes) if (n.node.kind === "client_call") n.node.file = long;
    const g = buildGraph(f, ALL);
    const calls = new Set(g.nodes.filter((n) => n.node?.kind === "client_call").map((n) => n.id));
    expect(calls.size).toBeGreaterThan(0);
    const widths = (cap?: number) => (elkGraph(g, cap).children ?? []).filter((c) => calls.has(c.id)).map((c) => c.width ?? 0);
    expect(new Set(widths())).toEqual(new Set([360]));
    expect(new Set(widths(CARD_MIN_CAP))).toEqual(new Set([CARD_MIN_CAP]));
  });
});

describe("caption placement at any zoom", () => {
  // MIN_W is the 150 px card minimum: the narrowest column there is.
  const MIN_W = 150;
  const long = "Enclosing functions";

  it("keeps 12 px and T016i's clamp at zoom >= 1 when the caption fits", () => {
    const c = { left: 100, right: 400, width: captionWidth("Endpoint") };
    expect(placeCaption(c, { x: 0, y: 0, zoom: 1 })).toEqual({ x: 100, scale: 1, maxWidth: c.width, clipped: false, hidden: false });
    expect(placeCaption(c, { x: -300, y: 0, zoom: 1 }).x).toBe(8);
    const two = placeCaption(c, { x: 0, y: 0, zoom: 2 });
    expect(two.scale).toBe(1);
    expect(two.x).toBe(200);
  });

  it("scales with the zoom below 1 and cuts a long caption to a MIN_W column at zoom 0.5", () => {
    const c = { left: 1000, right: 1000 + MIN_W, width: captionWidth(long) };
    const vp = { x: -200, y: 0, zoom: 0.5 };
    const p = placeCaption(c, vp);
    const left = 1000 * 0.5 - 200;
    const right = (1000 + MIN_W) * 0.5 - 200;
    expect(p.scale).toBe(0.5);
    expect(c.width * 0.5).toBeGreaterThan(right - left);
    expect(p.clipped).toBe(true);
    expect(p.hidden).toBe(false);
    expect(p.maxWidth).toBe(right - left);
    expect(p.x).toBe(left);
    expect(p.x + p.maxWidth).toBeLessThanOrEqual(right);
  });

  it("never pushes a caption into the neighbouring column, even clamped at the left edge", () => {
    const c = { left: 0, right: MIN_W, width: captionWidth(long) };
    for (const x of [-60, -40, -20, 0, 20]) {
      const p = placeCaption(c, { x, y: 0, zoom: 0.5 });
      if (p.hidden) continue;
      expect(p.x).toBeGreaterThanOrEqual(x - 0.001);
      expect(p.x + p.maxWidth).toBeLessThanOrEqual(MIN_W * 0.5 + x + 0.001);
    }
  });

  it("hides the caption of a column narrower than CAPTION_MIN_W on screen", () => {
    const c = { left: 0, right: 2 * CAPTION_MIN_W - 2, width: captionWidth(long) };
    expect(placeCaption(c, { x: 0, y: 0, zoom: 0.5 }).hidden).toBe(true);
    expect(placeCaption(c, { x: 0, y: 0, zoom: 1 }).hidden).toBe(false);
  });
});

describe("overlays below the caption row", () => {
  it("moves the view so no card lies under the caption row or the 'No stored callers' note", () => {
    const canvas = { width: 1100, height: 600 };
    const rowBottom = CAPTION_ROW.top + CAPTION_ROW.height;
    const row = { x: 0, y: CAPTION_ROW.top, width: canvas.width, height: CAPTION_ROW.height };
    const note = { x: 12, y: rowBottom + 8, width: 420, height: 62 };
    // An endpoint card at the first view's top, under the note's corner.
    const endpoint = { x: 0, y: 0, width: 300, height: 52 };
    const handler = { x: 500, y: 0, width: 200, height: 52 };
    const vp = { x: 100, y: FIT_TOP, zoom: 1 };
    const next = clearOverlays(vp, [endpoint, handler], [row, note], canvas, [endpoint, handler]);
    const hit = (b: Box, o: Box) => {
      const l = b.x + next.x;
      const t = b.y + next.y;
      return l < o.x + o.width && l + b.width > o.x && t < o.y + o.height && t + b.height > o.y;
    };
    expect(hit(endpoint, note) || hit(handler, note) || hit(endpoint, row) || hit(handler, row)).toBe(false);
    expect(next).not.toEqual(vp);
  });
});

describe("separateLabels (T026 AG-F)", () => {
  const at = (text: string, y: number): { label: Route["label"] } => ({ label: { text, x: 100, y, width: 40, height: 16 } });
  it("keeps one label per row for the same text and moves other texts above", () => {
    const moved = [at("reads", 50), at("reads", 50), at("writes", 50), at("reads", 200)];
    separateLabels(moved);
    expect(moved[0]?.label?.y).toBe(50);
    expect(moved[1]?.label).toBeNull();
    expect(moved[2]?.label?.y).toBe(50 - 16 - 2);
    expect(moved[3]?.label?.y).toBe(200);
    const boxes = moved.flatMap((m) => (m.label === null ? [] : [m.label]));
    for (const a of boxes) for (const b of boxes) if (a !== b) expect(a.y + a.height <= b.y || b.y + b.height <= a.y).toBe(true);
  });
});
