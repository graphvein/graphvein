// The Flow tab (T016f2): who calls the selected route or command and what
// it runs, one or two hops each way, drawn with React Flow from ELK's
// layout. This module is the lazy chunk: it alone imports @xyflow/react,
// and its layout runs in elkjs's worker, created on the first layout
// (elk.ts). The API section's entry chunk contains neither.
//
// Precision (VISION, principle 6): the graph comes from graph.ts, which draws
// only the response's stored edges and its near misses' candidates
// (dashed); nothing here adds a line. Counts in the summary are the
// response's `counts`, equal to the Callers tab's by construction.
//
// Every keyboard action has a visible control (D7): cards are buttons, the
// selected card's toolbar has Peek source, the IDE button and the
// connected cards; zoom and fit are buttons; hops and near misses are in
// the tab row; shortcuts appear as hints (title, aria-keyshortcuts).

import type { ProjectEndpoint } from "@/api/project";
import { RepoBadge } from "@/components/RepoBadge";
import "@xyflow/react/dist/style.css";
import "@/sections/api/flow/flow.css";

import { MiniMap, ReactFlow, ReactFlowProvider, useReactFlow, useViewport, type Node } from "@xyflow/react";
import type { ElkNode } from "elkjs/lib/elk-api";
import { ArrowLeft, ArrowRight, MapIcon, Maximize, Minus, Plus, TriangleAlert } from "lucide-react";
import { useEffect, useEffectEvent, useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent, type RefObject } from "react";

import { endpointNoun, neverCalledLabel } from "@/api/categories";
import type { FlowResponse } from "@/api/flow";
import { Skeleton } from "@/components/ui/skeleton";
import { TooltipProvider } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { FlowContext, type FlowActions, type Neighbour } from "@/sections/api/flow/context";
import { pageElk } from "@/sections/api/flow/elk";
import { FlowLine, type RouteEdge } from "@/sections/api/flow/FlowEdge";
import { FlowCaption, FlowCard, type CardNode } from "@/sections/api/flow/FlowNode";
import { COL, isDrawnSubset, truncatedText, type FlowGraph, type GraphOptions, type Hops } from "@/sections/api/flow/graph";
import {
  CAPTION_ROW,
  CAPTION_ROW_GAP,
  captionColumns,
  chainColumns,
  clearOverlays,
  covered,
  elkGraph,
  estimatedGap,
  focusBox,
  MAP_MIN_HEIGHT,
  measuredChain,
  MINIMAP,
  minimapRect,
  outside,
  placeCaption,
  placeView,
  readLayout,
  type Box,
  type CardBox,
  type Focus,
  type Layout,
} from "@/sections/api/flow/layout";
import { Legend } from "@/sections/api/flow/Legend";
import { CAP_STEP, CARD_MIN_CAP, cardCap, cardSize, CHAIN_PAD, nameEnd } from "@/sections/api/flow/measure";
import { TIER_LABEL, type FilterTier, type Tier } from "@/sections/api/flow/tiers";

/** The part of the layout client the tab uses; tests pass their own. */
export interface LayoutRunner {
  layout(graph: ElkNode): Promise<ElkNode>;
}

export interface FlowTabProps {
  flow: FlowResponse;
  /** The cards and lines to draw (the hop setting applied). */
  graph: FlowGraph;
  /** The 2-hop graph of the same flow and near-miss setting: the one that
   * is laid out (T016i D3), so switching hops never moves a card. Equal to
   * graph with 2 hops. */
  layoutGraph: FlowGraph;
  options: GraphOptions;
  selectedNode: string | null;
  selectedEdge: string | null;
  peekOpen: boolean;
  /** Selects a card or a line (or clears both); selecting opens the peek. */
  onSelect: (sel: { node: string | null; edge: string | null }) => void;
  onTogglePeek: () => void;
  onHops: (hops: Hops) => void;
  onTiers: (tiers: FilterTier[]) => void;
  onGap: (category: "dynamic" | "unmatched") => void;
  /** Opens the Callers tab (the error state's way out). */
  onCallers: () => void;
  elk?: LayoutRunner | undefined;
  /** Used when the canvas reports no size (jsdom). */
  fallbackSize?: { width: number; height: number } | undefined;
  /** T026 AG-C3: the Architecture chain hash this tab was opened from;
   * then the first view shows the downstream side with the Tables column
   * and the summary links back. */
  back?: string | null | undefined;
}

// Layouts per (endpoint, scope, root, index run, near misses, card cap):
// the 2-hop graph is laid out and 1 hop draws its subset at the same
// places (T016i D3); a tier filter never re-lays out. Small and per page.
const CACHE_LIMIT = 24;

/** A layout and the card width cap its cards were sized at. */
interface CappedLayout {
  layout: Layout;
  cap: number;
}

const cache = new Map<string, CappedLayout>();

function remember(key: string, layout: CappedLayout): void {
  cache.delete(key);
  cache.set(key, layout);
  while (cache.size > CACHE_LIMIT) {
    const first = cache.keys().next();
    if (first.done === true) break;
    cache.delete(first.value);
  }
}

/** layoutKey names a layout: the same key always has the same cards and
 * lines. Hops are not part of it: 1 hop is drawn at the 2-hop layout.
 * `alone` names a 1-hop graph laid out on its own, the fallback for a
 * graph that is not a subset of the 2-hop one (never seen on the known
 * shapes, D3.4). `cap` is the first pass's card width cap (T016i2 D2.4), a
 * function of the canvas width: a narrower or wider canvas lays out again. */
export function layoutKey(f: FlowResponse, o: Pick<GraphOptions, "nearMisses">, alone = false, cap: number | null = null): string {
  return JSON.stringify([f.endpoint.identity, f.scope, f.root, f.index?.finished_at ?? "none", o.nearMisses, ...(alone ? ["1 hop alone"] : []), ...(cap !== null ? [cap] : [])]);
}

/** resizedKey re-checks a cached layout against the current canvas width
 * (T016i2 review). The key holds only the first pass's cap, so a
 * narrower canvas in the same first-cap band can hit a cached layout whose
 * chain no longer fits. Then the graph is laid out again under its own
 * key, starting at the cap the cached layout's measured chain gives for this
 * width (one CAP_STEP below the cached cap at least, so it always
 * narrows); caps are in steps, so a resize drag lays out only a few times.
 * At the floor nothing narrower exists, and the cached layout stands. */
export function resizedKey(base: string, graph: FlowGraph, width: number | null, first: number | null): { key: string; start: number | null } {
  const at = cache.get(base);
  if (width === null || first === null || at === undefined || at.cap <= CARD_MIN_CAP || fitsChain(graph, at.layout, width)) return { key: base, start: first };
  const m = measuredChain(graph, at.layout, at.cap);
  const start = Math.min(cardCap(width, m.layers, m.fixed), Math.max(CARD_MIN_CAP, at.cap - CAP_STEP));
  return { key: `${base} refit ${String(start)}`, start };
}

/** fitsChain says whether a layout's chain (call sites to handler) fits a
 * canvas `width` px wide with CHAIN_PAD at each side. */
export function fitsChain(graph: FlowGraph, layout: Layout, width: number): boolean {
  const box = focusBox(graph, layout, COL.calls, COL.handler);
  return box === null || box.width + 2 * CHAIN_PAD <= width;
}

/** layoutAtCap lays a graph out for a canvas `width` px wide (T016i2 D2):
 * first at `first`, the cap from the estimated layer gaps; then, if the
 * chain measured in that layout (measuredChain: its ELK layers and the
 * room between them) gives another cap, once more at that cap.
 * The second layout is taken when it narrows the cards (the first did not
 * fit) or when its chain fits; a wider second layout that no longer fits
 * (ELK's gaps move a little with card widths) leaves the first. At most two
 * layouts. */
export async function layoutAtCap(graph: FlowGraph, runner: LayoutRunner, width: number, first: number): Promise<CappedLayout> {
  const one = readLayout(graph, await runner.layout(elkGraph(graph, first)));
  const m = measuredChain(graph, one, first);
  const second = cardCap(width, m.layers, m.fixed);
  if (second === first) return { layout: one, cap: first };
  const two = readLayout(graph, await runner.layout(elkGraph(graph, second)));
  if (second < first || fitsChain(graph, two, width)) return { layout: two, cap: second };
  return { layout: one, cap: first };
}

type LayoutState = { kind: "loading" } | { kind: "ok"; layout: Layout; cap: number } | { kind: "failed"; error: Error };

function useLayout(graph: FlowGraph, key: string, runner: LayoutRunner, width: number | null, first: number | null): { state: LayoutState; retry: () => void } {
  const [attempt, setAttempt] = useState(0);
  const [held, setHeld] = useState<{ key: string; attempt: number; state: LayoutState } | null>(null);
  const run = useEffectEvent((w: number, cap: number) => layoutAtCap(graph, runner, w, cap));
  useEffect(() => {
    // Nothing to size the cards for until the canvas has a width.
    if (cache.has(key) || width === null || first === null) return;
    let live = true;
    run(width, first).then(
      (capped) => {
        remember(key, capped);
        if (live) setHeld({ key, attempt, state: { kind: "ok", ...capped } });
      },
      (err: unknown) => {
        if (live) setHeld({ key, attempt, state: { kind: "failed", error: err instanceof Error ? err : new Error(String(err)) } });
      },
    );
    return () => {
      live = false;
    };
  }, [key, attempt, width, first]);
  const cached = cache.get(key);
  let state: LayoutState = { kind: "loading" };
  if (cached !== undefined) state = { kind: "ok", ...cached };
  else if (held !== null && held.key === key && held.attempt === attempt) state = held.state;
  return {
    state,
    retry: () => {
      setAttempt((a) => a + 1);
    },
  };
}

function tierMix(callerTiers: Map<Tier, number>): string {
  const parts: string[] = [];
  for (const t of ["fact", "exact", "suffix", "other"] as const) {
    const n = callerTiers.get(t) ?? 0;
    if (n > 0) parts.push(`${String(n)} ${TIER_LABEL[t].toLowerCase()}`);
  }
  return parts.join(", ");
}

/** Summary is the one line above the canvas, from `counts` only. */
function Summary({ flow, graph, options, back }: { flow: FlowResponse; graph: FlowGraph; options: GraphOptions; back: string | null }) {
  const c = flow.counts;
  const mix = tierMix(graph.callerTiers);
  const noun = endpointNoun(flow.transport, 1);
  return (
    <div className="flex flex-wrap items-center gap-x-3.5 gap-y-1 border-b px-5 py-[7px] text-[12.5px] text-muted-foreground" data-testid="flow-summary" data-callers={c.callers} data-near-misses={c.near_misses}>
      {back !== null && (
        <a href={back} className="inline-flex h-6 flex-none items-center gap-1 rounded-md border border-brand/60 bg-brand-soft px-2 text-[12px] font-medium text-brand-text hover:bg-brand-soft/70" data-testid="flow-back">
          <ArrowLeft aria-hidden="true" className="size-3.5" />
          Architecture chain
        </a>
      )}
      <span>
        <b className="font-medium text-foreground">
          {c.callers} {c.callers === 1 ? "caller" : "callers"}
        </b>
        {mix !== "" && ` (${mix})`}
        {" · "}
        <b className="font-medium text-foreground">
          {c.near_misses} {c.near_misses === 1 ? "near miss" : "near misses"}
        </b>
        {c.near_misses_incomplete > 0 && ` (${String(c.near_misses_incomplete)} more could not be listed)`}
        {!options.nearMisses && c.near_misses > 0 && " (hidden)"}
      </span>
      {options.hops === 2 && graph.upstreamHop2 > 0 && (
        <span data-testid="flow-upstream">
          {graph.upstreamHop2} {graph.upstreamHop2 === 1 ? "function calls" : "functions call"} an owner or enclosing function (hop 2; not
          callers of this {noun})
        </span>
      )}
      {(graph.tables.drawn > 0 || graph.tables.hiddenHop2 > 0) && (
        <span data-testid="flow-tables" data-tables={graph.tables.drawn} data-written={graph.tables.written}>
          <b className="font-medium text-foreground">
            {graph.tables.drawn} {graph.tables.drawn === 1 ? "table" : "tables"}
          </b>{" "}
          ({graph.tables.written} written)
          {graph.tables.hiddenHop2 > 0 && ` · +${String(graph.tables.hiddenHop2)} at hop 2 (2 hops shows ${graph.tables.hiddenHop2 === 1 ? "it" : "them"})`}
          {" · stored edges only, a lower bound"}
        </span>
      )}
      <span>Dashed edges are candidates, not links.</span>
      {flow.skipped_edges > 0 && (
        <span>
          {flow.skipped_edges} stored {flow.skipped_edges === 1 ? "edge points" : "edges point"} to a node that is not stored and{" "}
          {flow.skipped_edges === 1 ? "is" : "are"} not drawn.
        </span>
      )}
    </div>
  );
}

const nodeTypes = { card: FlowCard };
const edgeTypes = { route: FlowLine };

function findCard(box: HTMLElement | null, id: string): HTMLElement | null {
  if (box === null) return null;
  for (const el of box.querySelectorAll<HTMLElement>("[data-flow-card]")) if (el.getAttribute("data-flow-card") === id) return el;
  return null;
}

function isTyping(t: EventTarget | null): boolean {
  return t instanceof HTMLElement && (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.tagName === "SELECT" || t.isContentEditable);
}

/** downstreamFocus is the first view of a Flow tab opened from the
 * Architecture chain: the widest column range ending at the Tables column
 * that fits the canvas, anchored on the Tables column; null when no table
 * column is drawn. */
export function downstreamFocus(graph: FlowGraph, layout: Layout, cards: CardBox[], width: number): Focus | null {
  const tables = focusBox(graph, layout, COL.tables, COL.tables);
  if (tables === null) return null;
  let box: Box = tables;
  for (const start of [COL.callees2, COL.callees, COL.handler]) {
    const b = focusBox(graph, layout, start, COL.tables);
    if (b === null) continue;
    if (b.width + 48 > width) break;
    box = b;
  }
  // Vertically the Tables column: the callee columns can be far taller
  // (langflow's hop-2 callees), and the tables are what the view is for.
  // Cards above the tables (a tall callee column) would pull the view up
  // (fitViewport keeps every card in view below the toolbar), so only the
  // cards level with the Tables column count.
  const level = cards.filter((c) => c.y + c.height > tables.y && c.y < tables.y + tables.height);
  return { box: { x: box.x, width: box.width, y: tables.y, height: tables.height }, anchor: tables, cards: level };
}

/** PILL_NAME names a column on the left pan pill. */
const PILL_NAME = ["Hop-2 callers", "Enclosing functions", "Owners", "Call sites", "Endpoint", "Handler", "Callees", "Hop-2 callees", "Tables"];

const TOOL =
  "inline-flex size-8 items-center justify-center rounded-md border bg-card text-foreground outline-none hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring [&_svg]:size-4";

interface CanvasProps extends FlowTabProps {
  layout: Layout;
  /** The card width cap the layout was made at (T016i2 D2). */
  cap: number;
  size: { width: number; height: number };
  /** The card that had the focus when the previous canvas went away (a
   * near-miss change re-lays out); this canvas focuses it again. */
  restoreRef: RefObject<string | null>;
}

function Canvas(props: CanvasProps) {
  const { flow, graph, layout, cap, size, selectedNode, selectedEdge, onSelect, restoreRef } = props;
  const rf = useReactFlow<CardNode, RouteEdge>();
  const boxRef = useRef<HTMLDivElement>(null);
  const placed = useMemo(
    () =>
      graph.nodes.flatMap((node) => {
        const box = layout.boxes.get(node.id);
        return box === undefined ? [] : [{ node, box }];
      }),
    [graph, layout],
  );
  const cards = useMemo(() => placed.map((c): CardBox => ({ ...c.box, name: nameEnd(c.node, cap) })), [placed, cap]);
  // The initial view and Fit: the call-site, endpoint and handler columns
  // (the whole graph when none of them is drawn), anchored on the endpoint.
  const focus = useMemo((): Focus => {
    const box = focusBox(graph, layout, COL.calls, COL.handler) ?? { x: 0, y: 0, width: layout.width, height: layout.height };
    return { box, anchor: layout.boxes.get(flow.endpoint.identity) ?? null, cards };
  }, [graph, layout, cards, flow.endpoint.identity]);

  // The overview (minimap) never sits on a card the view was placed to
  // show: it is allowed only on a tall canvas with the peek closed
  // (MAP_MIN_HEIGHT), and then shown by default only when the fitted or
  // panned view keeps every card clear of it (placeView). The Map button
  // overrides the default until the peek opens or closes.
  const mapAllowed = !props.peekOpen && size.height >= MAP_MIN_HEIGHT;
  // Opened from the Architecture chain (T026 AG-C3): the first view is the
  // downstream side, the widest of handler..tables, callees..tables or
  // hop-2 callees..tables that fits, anchored on the Tables column.
  const [first] = useState(() => placeView(props.back != null ? (downstreamFocus(graph, layout, cards, size.width) ?? focus) : focus, size, mapAllowed));
  const [autoMap, setAutoMap] = useState(first.mapClear);
  const [userMap, setUserMap] = useState<boolean | null>(null);
  const showMap = userMap ?? autoMap;

  const neighbours = useMemo(() => {
    const byId = new Map(graph.nodes.map((n) => [n.id, n]));
    const inc = new Map<string, Neighbour[]>();
    const out = new Map<string, Neighbour[]>();
    for (const e of graph.edges) {
      const s = byId.get(e.source);
      const t = byId.get(e.target);
      if (s === undefined || t === undefined) continue;
      inc.set(t.id, [...(inc.get(t.id) ?? []), { node: s, edge: e }]);
      out.set(s.id, [...(out.get(s.id) ?? []), { node: t, edge: e }]);
    }
    return { inc, out };
  }, [graph]);

  const related = useMemo(() => {
    const nodes = new Set<string>();
    const edges = new Set<string>();
    for (const e of graph.edges) {
      if (selectedEdge === e.id) {
        nodes.add(e.source);
        nodes.add(e.target);
      }
      if (selectedNode !== null && (e.source === selectedNode || e.target === selectedNode)) edges.add(e.id);
    }
    return { nodes, edges };
  }, [graph, selectedNode, selectedEdge]);

  // Column captions: per column with a drawn card its left and right edge
  // (from the drawn cards only, T016i2 D1.4). Drawn by the pinned caption
  // row below the toolbar, in screen space.
  const columns = useMemo(() => captionColumns(placed), [placed]);

  const nodes = useMemo(() => {
    const out: CardNode[] = [];
    for (const n of graph.nodes) {
      const b = layout.boxes.get(n.id);
      if (b === undefined) continue;
      out.push({
        id: n.id,
        type: "card",
        position: { x: b.x, y: b.y },
        width: b.width,
        height: b.height,
        data: { card: n, related: related.nodes.has(n.id), cap },
        draggable: false,
        selectable: false,
        focusable: false,
      });
    }
    return out;
  }, [graph, layout, related, cap]);

  const edges = useMemo(() => {
    const out: RouteEdge[] = [];
    for (const e of graph.edges) {
      const route = layout.routes.get(e.id);
      if (route === undefined) continue;
      out.push({
        id: e.id,
        source: e.source,
        target: e.target,
        type: "route",
        data: { line: e, route, related: related.edges.has(e.id) },
        selectable: false,
        focusable: false,
        zIndex: selectedEdge === e.id ? 2 : 0,
        ariaLabel: e.label ?? TIER_LABEL[e.tier],
      });
    }
    return out;
  }, [graph, layout, related, selectedEdge]);

  // The pan pills (D7 pan hints): one on each side while cards lie beyond
  // that edge, read from the live viewport. Each pans sideways to a column
  // start. Once drawn, the view moves as little as it takes so
  // no card lies under the zoom buttons or a pill (clearOverlays with their
  // measured rectangles): a pill never covers a card it panned to. The
  // same runs after the first view and Fit.
  const vp = useViewport();
  const beyond = useMemo(() => outside(vp, placed, size.width), [vp, placed, size.width]);
  const topLeft = useRef<HTMLDivElement>(null);
  // The top-left toolbar group (zoom buttons, Fit, Map, the left pill) in
  // canvas pixels: the caption row sits below it.
  const [toolbar, setToolbar] = useState<{ right: number; top: number; bottom: number } | null>(null);
  useLayoutEffect(() => {
    const el = topLeft.current;
    const box = boxRef.current;
    if (el === null || box === null) return;
    const read = () => {
      const r = el.getBoundingClientRect();
      const c = box.getBoundingClientRect();
      const next = r.width > 0 ? { right: r.right - c.left, top: r.top - c.top, bottom: r.bottom - c.top } : null;
      setToolbar((t) => (t?.right === next?.right && t?.top === next?.top && t?.bottom === next?.bottom ? t : next));
    };
    read();
    const observer = new ResizeObserver(read);
    observer.observe(el);
    return () => {
      observer.disconnect();
    };
  }, []);
  const rightPill = useRef<HTMLButtonElement>(null);
  const neverNote = useRef<HTMLDivElement>(null);
  // The pinned caption row (T016i2 D1), in canvas pixels, below the
  // measured toolbar; overlays placed under it start below its bottom.
  const rowTop = toolbar !== null ? toolbar.bottom + CAPTION_ROW.gap : CAPTION_ROW.top;
  const rowBottom = rowTop + CAPTION_ROW.height;
  // The endpoint, the handler and the selected card (a card "Open in Flow
  // tab" landed on, T026 AG-C3) stay in view when overlays are cleared.
  const keep = useMemo(
    () => [flow.endpoint.identity, flow.handler.node?.identity, selectedNode ?? undefined].flatMap((id) => (id === undefined ? [] : (layout.boxes.get(id) ?? []))),
    [flow, layout, selectedNode],
  );
  const settle = useEffectEvent(() => {
    const canvas = boxRef.current?.getBoundingClientRect();
    if (canvas === undefined) return;
    const rects: Box[] = [];
    // The toolbar, the right pill and the "No stored callers" note.
    for (const el of [topLeft.current, rightPill.current, neverNote.current]) {
      const r = el?.getBoundingClientRect();
      if (r !== undefined && r.width > 0) rects.push({ x: r.left - canvas.left, y: r.top - canvas.top, width: r.width, height: r.height });
    }
    // No card under the caption row either (T016i2 D1.5).
    rects.push({ x: 0, y: rowTop, width: size.width, height: CAPTION_ROW.height });
    const now = rf.getViewport();
    const next = clearOverlays(now, cards, rects, size, keep);
    if (next.x === now.x && next.y === now.y) return;
    void rf.setViewport(next);
    setAutoMap(mapAllowed && covered(next, cards, minimapRect(size)) === 0);
  });
  // After the first view and after each Fit or pan (`moved`), once React
  // has drawn the pills for the new view.
  const [moved, setMoved] = useState(0);
  useEffect(() => {
    let inner = 0;
    const outer = requestAnimationFrame(() => {
      inner = requestAnimationFrame(() => {
        settle();
      });
    });
    return () => {
      cancelAnimationFrame(outer);
      cancelAnimationFrame(inner);
    };
  }, [moved]);
  const fit = () => {
    const p = placeView(focus, size, mapAllowed);
    void rf.setViewport(p.viewport);
    setAutoMap(p.mapClear);
    setMoved((m) => m + 1);
  };
  const panTo = (x: number) => {
    const next = { ...rf.getViewport(), x };
    void rf.setViewport(next);
    setAutoMap(mapAllowed && covered(next, cards, minimapRect(size)) === 0);
    setMoved((m) => m + 1);
  };
  const columnX = (test: (column: number) => boolean) => Math.min(...placed.filter((c) => test(c.node.column)).map((c) => c.box.x));
  const endpointOut = (list: typeof placed) => list.some((c) => c.node.id === flow.endpoint.identity);
  // Left: back to the fitted view when the endpoint is off to the left
  // (after panning downstream), else to the leftmost column.
  const leftmost = beyond.left.reduce((m, c) => Math.min(m, c.node.column), Infinity);
  const left = endpointOut(beyond.left)
    ? { text: "Endpoint + callers", title: "Back to the call sites, the endpoint and the handler (0)", go: fit }
    : {
        text: `${PILL_NAME[leftmost] ?? "More"} (${String(beyond.left.filter((c) => c.node.column === leftmost).length)})`,
        title: `Pan left: ${String(beyond.left.length)} ${beyond.left.length === 1 ? "card lies" : "cards lie"} left of the view`,
        go: () => {
          panTo(24 - columnX(() => true) * vp.zoom);
        },
      };
  // Right: back to the fitted view when the endpoint is off to the right
  // (after panning to the hop-2 callers), else the handler column at the
  // left edge, else (already there) the rightmost card at the right edge.
  const handlerX = columnX((c) => c >= COL.handler);
  // "There" includes a view moved right to clear the pill (clearOverlays).
  const atHandler = Number.isFinite(handlerX) && handlerX * vp.zoom + vp.x <= size.width / 4;
  const right = endpointOut(beyond.right)
    ? { text: "Endpoint + handler", title: "Back to the call sites, the endpoint and the handler (0)", go: fit }
    : !atHandler && Number.isFinite(handlerX)
      ? {
          text: "Handler + downstream",
          title: `Pan right: ${String(beyond.right.length)} ${beyond.right.length === 1 ? "card lies" : "cards lie"} right of the view`,
          go: () => {
            panTo(24 - handlerX * vp.zoom);
          },
        }
      : {
          text: `Downstream (${String(beyond.right.length)})`,
          title: `Pan right: ${String(beyond.right.length)} ${beyond.right.length === 1 ? "card lies" : "cards lie"} right of the view`,
          go: () => {
            const end = Math.max(...placed.map((c) => c.box.x + c.box.width));
            panTo(size.width - 24 - end * vp.zoom);
          },
        };
  const PILL =
    "inline-flex items-center gap-1.5 rounded-full bg-brand px-3 py-1 text-[12.5px] font-semibold text-brand-foreground shadow-md outline-none focus-visible:ring-2 focus-visible:ring-ring";

  const selectedNodeRef = useRef(selectedNode);
  useLayoutEffect(() => {
    selectedNodeRef.current = selectedNode;
  }, [selectedNode]);

  const reveal = (id: string) => {
    const b = layout.boxes.get(id);
    if (b !== undefined) revealBox(b);
  };
  const revealBox = (b: Box) => {
    const vp = rf.getViewport();
    const left = b.x * vp.zoom + vp.x;
    const topY = b.y * vp.zoom + vp.y;
    const right = left + b.width * vp.zoom;
    const bottom = topY + b.height * vp.zoom;
    const outside = left < 0 || topY < 0 || right > size.width || bottom > size.height;
    const underMap = showMap && covered(vp, [b], minimapRect(size)) > 0;
    if (outside || underMap) void rf.setCenter(b.x + b.width / 2, b.y + b.height / 2, { zoom: vp.zoom });
  };

  const moveFocus = (id: string) => {
    reveal(id);
    requestAnimationFrame(() => {
      findCard(boxRef.current, id)?.focus();
    });
  };

  // Opening the peek (or resizing the split) shrinks the canvas: keep the
  // selected card, or the selected line's label, in view.
  const revealSelection = useEffectEvent(() => {
    if (selectedNode !== null) reveal(selectedNode);
    else if (selectedEdge !== null) {
      const label = layout.routes.get(selectedEdge)?.label;
      if (label != null) revealBox(label);
    }
  });
  useEffect(() => {
    // After React Flow has measured the new canvas size.
    const frame = requestAnimationFrame(() => {
      revealSelection();
    });
    return () => {
      cancelAnimationFrame(frame);
    };
  }, [size.width, size.height, props.peekOpen, selectedNode, selectedEdge]);

  // A new canvas size or the peek opening or closing decides the overview
  // again for the view as it stands: shown only when allowed and clear of
  // every card. Opening or closing the peek also drops the Map button's
  // choice. The first run is the mount, already decided by placeView.
  const recheckMap = useEffectEvent(() => {
    setAutoMap(mapAllowed && covered(rf.getViewport(), cards, minimapRect(size)) === 0);
  });
  const seenView = useRef<{ width: number; height: number; peek: boolean } | null>(null);
  useEffect(() => {
    const was = seenView.current;
    seenView.current = { width: size.width, height: size.height, peek: props.peekOpen };
    if (was === null) return;
    if (was.peek !== props.peekOpen) setUserMap(null);
    recheckMap();
  }, [size.width, size.height, props.peekOpen]);

  // A hop or near-miss change replaces this canvas (it is keyed by its
  // layout). Before the old canvas goes, remember the focused card (for a
  // toolbar button, the selected card); the new canvas focuses it again,
  // or the endpoint when that card is no longer drawn. Focus outside the
  // canvas (the hop control, the checkbox) is left alone.
  const focusRestored = useEffectEvent((box: HTMLElement | null, want: string) => {
    const id = graph.nodes.some((n) => n.id === want) ? want : flow.endpoint.identity;
    requestAnimationFrame(() => {
      if (document.activeElement !== null && document.activeElement !== document.body) return;
      reveal(id);
      findCard(box, id)?.focus();
    });
  });
  useLayoutEffect(() => {
    const box = boxRef.current;
    const want = restoreRef.current;
    if (want !== null) {
      restoreRef.current = null;
      focusRestored(box, want);
    }
    return () => {
      const active = document.activeElement;
      if (box === null || !(active instanceof HTMLElement) || !box.contains(active)) return;
      const card = active.closest("[data-flow-card]")?.getAttribute("data-flow-card");
      // "" names no card: the new canvas focuses the endpoint.
      restoreRef.current = card ?? selectedNodeRef.current ?? "";
    };
  }, [restoreRef]);

  // A hop change keeps this canvas (T016i D3): the focused card keeps the
  // focus while it is drawn; when 1 hop drops it, the focus goes to the
  // endpoint. `lastFocus` is the card focused last inside the canvas; focus
  // that moved to a control outside (the hop buttons) clears it.
  const lastFocus = useRef<string | null>(null);
  const onFocusIn = (e: { target: EventTarget }) => {
    const t = e.target;
    lastFocus.current = t instanceof HTMLElement ? (t.closest("[data-flow-card]")?.getAttribute("data-flow-card") ?? null) : null;
  };
  const onFocusOut = (e: { relatedTarget: EventTarget | null }) => {
    // relatedTarget null: the focused card was removed (or the window lost
    // the focus); keep the record.
    const next = e.relatedTarget;
    if (next instanceof globalThis.Node && !(boxRef.current?.contains(next) ?? false)) lastFocus.current = null;
  };
  const refocus = useEffectEvent(() => {
    const want = lastFocus.current;
    if (want === null) return;
    const active = document.activeElement;
    if (active !== null && active !== document.body) return;
    const id = graph.nodes.some((n) => n.id === want) ? want : flow.endpoint.identity;
    findCard(boxRef.current, id)?.focus();
  });
  useEffect(() => {
    const frame = requestAnimationFrame(() => {
      refocus();
    });
    return () => {
      cancelAnimationFrame(frame);
    };
  }, [graph]);

  // Left and Right follow the columns, not the stored direction: the
  // nearest connected card in a column to that side (top first). A line
  // drawn from right to left (the handler's exposes fact) is still
  // followed the way it is drawn.
  const besideInColumns = (id: string, side: -1 | 1): string | undefined => {
    const me = graph.nodes.find((n) => n.id === id);
    if (me === undefined) return undefined;
    const near = [...(neighbours.inc.get(id) ?? []), ...(neighbours.out.get(id) ?? [])]
      .map((nb) => nb.node)
      .filter((n) => n.type === "card" && (n.column - me.column) * side > 0)
      .sort((a, b) => Math.abs(a.column - me.column) - Math.abs(b.column - me.column) || (layout.boxes.get(a.id)?.y ?? 0) - (layout.boxes.get(b.id)?.y ?? 0));
    return near[0]?.id;
  };

  const onCardKey = (event: KeyboardEvent<HTMLElement>, id: string) => {
    let target: string | undefined;
    if (event.key === "ArrowLeft") target = besideInColumns(id, -1);
    else if (event.key === "ArrowRight") target = besideInColumns(id, 1);
    else if (event.key === "ArrowUp" || event.key === "ArrowDown") {
      const me = graph.nodes.find((n) => n.id === id);
      const col = graph.nodes
        .filter((n) => n.column === me?.column && n.type === "card")
        .sort((a, b) => (layout.boxes.get(a.id)?.y ?? 0) - (layout.boxes.get(b.id)?.y ?? 0));
      const i = col.findIndex((n) => n.id === id);
      target = col[event.key === "ArrowUp" ? i - 1 : i + 1]?.id;
    } else return;
    event.preventDefault();
    event.stopPropagation();
    if (target !== undefined) moveFocus(target);
  };

  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.ctrlKey || event.metaKey || event.altKey || isTyping(event.target)) return;
    const k = event.key;
    if (k === "+" || k === "=") void rf.zoomIn({ duration: 0 });
    else if (k === "-") void rf.zoomOut({ duration: 0 });
    else if (k === "0") fit();
    else if (k === "1") props.onHops(1);
    else if (k === "2") props.onHops(2);
    else if (k === "o") {
      const link = boxRef.current?.querySelector<HTMLAnchorElement>('a[data-testid="toolbar-ide"]');
      link?.click();
    } else if (k === "Escape") {
      if (props.peekOpen) props.onTogglePeek();
      else if (selectedNode !== null || selectedEdge !== null) onSelect({ node: null, edge: null });
      else return;
    } else return;
    event.preventDefault();
  };

  const actions: FlowActions = {
    selectedNode,
    selectedEdge,
    peekOpen: props.peekOpen,
    ide: flow.ide,
    selectNode: (id) => {
      onSelect({ node: id, edge: null });
      moveFocus(id);
    },
    selectEdge: (id) => {
      onSelect({ node: null, edge: id });
    },
    togglePeek: props.onTogglePeek,
    incoming: (id) => neighbours.inc.get(id) ?? [],
    outgoing: (id) => neighbours.out.get(id) ?? [],
    onCardKey,
  };

  const never = flow.counts.callers === 0 && flow.counts.near_misses === 0;
  return (
    <FlowContext.Provider value={actions}>
      <div
        ref={boxRef}
        className="absolute inset-0"
        onKeyDown={onKeyDown}
        onFocus={onFocusIn}
        onBlur={onFocusOut}
        data-testid="flow-canvas"
        data-nodes={graph.nodes.length}
        data-edges={graph.edges.length}
      >
        <ReactFlow
          nodes={nodes}
          edges={edges}
          nodeTypes={nodeTypes}
          edgeTypes={edgeTypes}
          defaultViewport={first.viewport}
          minZoom={0.5}
          maxZoom={2}
          nodesDraggable={false}
          nodesConnectable={false}
          nodesFocusable={false}
          edgesFocusable={false}
          elementsSelectable={false}
          deleteKeyCode={null}
          selectionKeyCode={null}
          multiSelectionKeyCode={null}
          zoomActivationKeyCode={null}
          panActivationKeyCode={null}
          disableKeyboardA11y
          onlyRenderVisibleElements={false}
          attributionPosition="bottom-left"
          onPaneClick={() => {
            onSelect({ node: null, edge: null });
          }}
          onEdgeClick={(_, e) => {
            onSelect({ node: null, edge: e.id });
          }}
        >
          {showMap && (
            <MiniMap
              pannable
              zoomable
              position="bottom-right"
              ariaLabel="Overview of the whole graph"
              nodeClassName={(n: Node) => (n.id === flow.endpoint.identity ? "gv-mm-focus" : "")}
              nodeStrokeWidth={0}
              nodeBorderRadius={3}
              style={MINIMAP}
            />
          )}
        </ReactFlow>
        {/* The caption row, pinned below the toolbar (T016i2 D1): opaque, so a
            card panned under it never shows through a caption. */}
        <div
          aria-hidden="true"
          className="gv-flow-caption-row pointer-events-none absolute inset-x-0 z-[4] overflow-hidden"
          style={{ top: rowTop, height: CAPTION_ROW.height }}
          data-testid="flow-captions"
        >
          {columns.map((c) => (
            <FlowCaption key={c.col} text={c.text} place={placeCaption(c, vp)} />
          ))}
        </div>
        <div ref={topLeft} className="absolute top-3 left-3 z-[5] flex items-center gap-2">
          <div className="flex items-center gap-1" role="toolbar" aria-label="Zoom">
            <button type="button" className={TOOL} aria-label="Zoom out" aria-keyshortcuts="-" title="Zoom out (-)" onClick={() => void rf.zoomOut({ duration: 0 })}>
              <Minus aria-hidden="true" />
            </button>
            <button type="button" className={TOOL} aria-label="Zoom in" aria-keyshortcuts="+" title="Zoom in (+)" onClick={() => void rf.zoomIn({ duration: 0 })}>
              <Plus aria-hidden="true" />
            </button>
            <button type="button" className={cn(TOOL, "w-auto gap-1.5 px-2.5 text-xs font-medium")} aria-keyshortcuts="0" title="Fit call sites, endpoint and handler at full size (0)" onClick={fit}>
              <Maximize aria-hidden="true" />
              Fit
            </button>
            <button
              type="button"
              className={cn(TOOL, "w-auto gap-1.5 px-2.5 text-xs font-medium", showMap && "bg-accent")}
              aria-pressed={showMap}
              title={showMap ? "Hide the overview" : "Show the overview"}
              onClick={() => {
                setUserMap(!showMap);
              }}
              data-testid="map-toggle"
            >
              <MapIcon aria-hidden="true" />
              Map
            </button>
          </div>
          {beyond.left.length > 0 && (
            <button type="button" className={PILL} title={left.title} onClick={left.go} data-testid="pan-pill-left">
              <ArrowLeft aria-hidden="true" className="size-3.5" />
              {left.text}
            </button>
          )}
        </div>
        {beyond.right.length > 0 && (
          <button ref={rightPill} type="button" className={cn(PILL, "absolute top-3 right-3 z-[5]")} title={right.title} onClick={right.go} data-testid="pan-pill">
            {right.text}
            <ArrowRight aria-hidden="true" className="size-3.5" />
          </button>
        )}
        {never && (
          <div
            ref={neverNote}
            className="absolute left-3 z-[5] max-w-[420px] rounded-lg border border-dashed bg-card px-3 py-2 text-[13px]"
            style={{ top: rowBottom + CAPTION_ROW_GAP }}
            data-testid="flow-never"
          >
            <b className="font-semibold">No stored callers</b> · {neverCalledLabel(flow.scope, undefined, flow.project !== null && flow.project.callers.length === 0)}
            {(flow.project?.callers.length ?? 0) > 0 ? " in this repository; calls of other repositories are linked to it (above the canvas)" : ""}. The endpoint, its handler and what the handler
            calls are drawn; no line is added for calls that are not linked.
          </div>
        )}
      </div>
    </FlowContext.Provider>
  );
}

/** FlowTabBody lays out the graph and draws it with its summary and legend. */
function FlowTabBody(props: FlowTabProps) {
  const { flow, graph, layoutGraph, options } = props;
  const restoreRef = useRef<string | null>(null);
  const runner = props.elk ?? pageElk();
  // 1 hop is drawn at the 2-hop layout when every drawn card has the size
  // of its 2-hop box (D3.4, proven on the goldens); otherwise it is laid
  // out on its own, as before, so no wrong size is ever drawn.
  const ref = useRef<HTMLDivElement>(null);
  const [measured, setMeasured] = useState<{ width: number; height: number } | null>(null);
  useLayoutEffect(() => {
    const el = ref.current;
    if (el === null) return;
    const read = () => {
      if (el.clientWidth > 0 && el.clientHeight > 0) setMeasured({ width: el.clientWidth, height: el.clientHeight });
    };
    read();
    const observer = new ResizeObserver(read);
    observer.observe(el);
    return () => {
      observer.disconnect();
    };
  }, []);
  const size = measured ?? props.fallbackSize ?? null;
  const width = size?.width ?? null;
  // The first pass's card width cap, from the canvas width and the layer
  // gaps estimated from the labels (T016i2 D2); the same for 1 and 2 hops,
  // whose chain columns are the same cards.
  const first = useMemo(() => (width === null ? null : cardCap(width, chainColumns(layoutGraph), estimatedGap(layoutGraph))), [width, layoutGraph]);
  const alone = useMemo(() => graph !== layoutGraph && !isDrawnSubset(graph, layoutGraph, (n) => cardSize(n, first ?? undefined)), [graph, layoutGraph, first]);
  const laid = alone ? graph : layoutGraph;
  const { key, start } = resizedKey(layoutKey(flow, options, alone, first), laid, width, first);
  const { state, retry } = useLayout(laid, key, runner, width, start);
  const truncated = truncatedText(flow);
  const toggle = (t: FilterTier) => {
    const on = new Set(options.tiers);
    if (on.has(t)) on.delete(t);
    else on.add(t);
    props.onTiers([...on]);
  };
  return (
    <div className="gv-flow flex flex-1 flex-col" data-testid="flow-tab">
      <Summary flow={flow} graph={graph} options={options} back={props.back ?? null} />
      <ProjectStrip project={flow.project} onCallers={props.onCallers} />
      {truncated !== null && (
        <div role="note" className="flex items-start gap-2 border-b border-stale/60 bg-stale/12 px-5 py-1.5 text-[12.5px]" data-testid="flow-truncated">
          <TriangleAlert aria-hidden="true" className="mt-0.5 size-4 flex-none text-dynamic" />
          {truncated}
        </div>
      )}
      <div ref={ref} className="gv-flow-canvas relative min-h-[200px] flex-1 overflow-hidden" aria-busy={state.kind === "loading"}>
        {state.kind === "ok" && size !== null ? (
          <Canvas key={key} {...props} layout={state.layout} cap={state.cap} size={size} restoreRef={restoreRef} />
        ) : state.kind === "failed" ? (
          <div role="alert" className="absolute top-4 left-4 grid max-w-[460px] justify-items-start gap-2 rounded-xl border bg-card px-4 py-3 text-[13px]" data-testid="flow-error">
            <span>
              <b className="font-semibold">Could not lay out the graph.</b> {state.error.message}
            </span>
            <span className="flex gap-2">
              <button type="button" className="h-7 rounded-md border border-input bg-background px-2.5 text-[12.5px] font-medium hover:bg-accent" onClick={retry}>
                Retry
              </button>
              <button type="button" className="h-7 rounded-md px-2.5 text-[12.5px] font-medium text-brand-text underline underline-offset-[3px]" onClick={props.onCallers}>
                Open the Callers list
              </button>
            </span>
          </div>
        ) : (
          <FlowSkeleton label="Laying out the graph" />
        )}
      </div>
      <Legend transport={props.flow.transport} precision={props.flow.precision} callersPrecision={props.flow.callers_precision} codeCallsPrecision={props.flow.code_calls_precision} counts={graph.tierCounts} tiers={options.tiers} onToggle={toggle} onGap={props.onGap} />
    </div>
  );
}

/** ProjectStrip (serve --project only, T022 B9) names the calls of other
 * repositories linked to the route. They are project links, not stored
 * edges of this repository, so the canvas does not draw them; the
 * Callers list shows each with its assumption. */
function ProjectStrip({ project, onCallers }: { project: ProjectEndpoint | null; onCallers: () => void }) {
  if (project === null || (project.callers.length === 0 && project.operations.length === 0)) return null;
  const byRepo = new Map<string, number>();
  for (const l of project.callers) byRepo.set(l.repository, (byRepo.get(l.repository) ?? 0) + 1);
  const stale = project.callers.filter((l) => l.stale).length;
  return (
    <div className="flex flex-wrap items-center gap-x-2 gap-y-1 border-b border-dashed border-brand/40 bg-brand-soft/40 px-5 py-[6px] text-[12.5px]" data-testid="flow-project">
      <b className="font-medium">
        {project.callers.length} {project.callers.length === 1 ? "call" : "calls"} from other repositories
      </b>
      {[...byRepo.entries()].map(([repo, n]) => (
        <span key={repo} className="inline-flex items-center gap-1">
          <RepoBadge repository={repo} />
          <span className="text-muted-foreground tabular-nums">{n}</span>
        </span>
      ))}
      {stale > 0 && <span className="text-dynamic">{stale} stale</span>}
      <span className="text-muted-foreground">Not drawn here: they are project links, not edges of this repository.</span>
      <button type="button" className="font-medium text-brand-text underline decoration-brand-text/35 underline-offset-[3px]" onClick={onCallers}>
        See them in Callers
      </button>
    </div>
  );
}

/** FlowSkeleton is the loading canvas (data, chunk or layout). */
export function FlowSkeleton({ label }: { label: string }) {
  return (
    <div className="absolute inset-0 grid grid-cols-4 items-center gap-8 p-8" aria-busy="true">
      <span className="sr-only">{label}</span>
      {[0, 1, 2, 3].map((i) => (
        <div key={i} className="grid gap-3">
          <Skeleton className="h-12" />
          {i % 2 === 0 && <Skeleton className="h-12" />}
        </div>
      ))}
    </div>
  );
}

/** FlowTab is the lazy chunk's default export. */
export default function FlowTab(props: FlowTabProps) {
  // The shell has a TooltipProvider too; this one keeps the cut cards'
  // tooltips working wherever the tab is rendered (tests render it alone).
  return (
    <TooltipProvider delayDuration={200}>
      <ReactFlowProvider>
        <FlowTabBody {...props} />
      </ReactFlowProvider>
    </TooltipProvider>
  );
}
