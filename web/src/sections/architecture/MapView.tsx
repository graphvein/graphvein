// The architecture map's canvas (T026 AG-C1): React Flow draws the cards
// and lines at ELK's positions (AG2, ADR 0046). This module is the lazy
// chunk: it alone imports @xyflow/react, so the section's entry chunk
// stays small, and its layout runs in the page's ELK worker.
//
// AG-C2: a chain drawn in place arrives as the model of chain.ts
// (buildChainMap): the crossed cards opened to their members, nested for
// ELK, which lays out across the hierarchy in the same single run (the
// other cards are laid out again too; pinning them is a follow-up).

import "@xyflow/react/dist/style.css";
import "@/sections/api/flow/flow.css";

import { MiniMap, ReactFlow, ReactFlowProvider, useReactFlow, type Node } from "@xyflow/react";
import type { ElkNode } from "elkjs/lib/elk-api";
import { Maximize, Minus, Plus, TriangleAlert } from "lucide-react";
import { useEffect, useEffectEvent, useMemo, useState } from "react";

import { Skeleton } from "@/components/ui/skeleton";
import { useLabelStyle } from "@/hooks/useLabels";
import { label } from "@/lib/labels";
import { pageElk } from "@/sections/api/flow/elk";
import { MapCard, type CardNode } from "@/sections/architecture/Cards";
import { MapContext, type MapActions } from "@/sections/architecture/context";
import { Legend } from "@/sections/architecture/Legend";
import { elkGraph, readLayout, type Placed } from "@/sections/architecture/layout";
import { MapLine, RAISED_LINE_Z, chainLineText, type LineEdge } from "@/sections/architecture/Line";
import { MINIMAP_CARDS, type MapEdge, type MapModel } from "@/sections/architecture/model";

/** The part of the layout client the map uses; tests pass their own. */
export interface LayoutRunner {
  layout(graph: ElkNode): Promise<ElkNode>;
}

export interface MapViewProps {
  model: MapModel;
  actions: MapActions;
  /** Rows of the expanded component (null while loading or none). */
  expandedRows: number | null;
  elk?: LayoutRunner | undefined;
  /** Reports each layout's time in ms (the ELK timing gate). */
  onLayout?: ((ms: number) => void) | undefined;
}

const nodeTypes = { card: MapCard };
const FIT_PADDING = { top: "52px", left: "24px", right: "24px", bottom: "56px" } as const;
const edgeTypes = { line: MapLine };

type LayoutState = { kind: "loading" } | { kind: "ok"; placed: Placed; key: string } | { kind: "failed"; error: string; key: string };

function useMapLayout(model: MapModel, expandedRows: number | null, runner: LayoutRunner, onLayout: ((ms: number) => void) | undefined) {
  const style = useLabelStyle();
  const familyText = (f: string) => label(style, "linkFamily", { family: f, row: "" }).text;
  const chainText = (e: MapEdge) => chainLineText(style, e).text;
  const graph = elkGraph(model, expandedRows, familyText);
  const key = JSON.stringify([graph, model.edges.filter((e) => e.type === "chain").map(chainText), model.nodes.filter((n) => n.type === "member" || n.type === "fold").map((n) => n.id)]);
  const [state, setState] = useState<LayoutState>({ kind: "loading" });
  const [attempt, setAttempt] = useState(0);
  const run = useEffectEvent(async () => {
    const t0 = performance.now();
    const out = await runner.layout(graph);
    const placed = readLayout(model, out, chainText);
    onLayout?.(performance.now() - t0);
    return placed;
  });
  useEffect(() => {
    let live = true;
    run().then(
      (placed) => {
        if (live) setState({ kind: "ok", placed, key });
      },
      (err: unknown) => {
        if (live) setState({ kind: "failed", error: err instanceof Error ? err.message : String(err), key });
      },
    );
    return () => {
      live = false;
    };
  }, [key, attempt]);
  // A layout of another graph stays on screen until the new one is ready,
  // so expanding never flashes an empty canvas; the stale one is never
  // drawn with the new model's cards (both come from the same key).
  return {
    state,
    stale: state.kind !== "loading" && state.key !== key,
    retry: () => {
      setAttempt((n) => n + 1);
    },
  };
}

function Canvas({ model, actions, placed }: { model: MapModel; actions: MapActions; placed: Placed }) {
  const rf = useReactFlow<CardNode, LineEdge>();
  const nodes = useMemo(
    (): CardNode[] =>
      model.nodes.flatMap((n) => {
        const b = placed.boxes.get(n.id);
        if (b === undefined) return [];
        // An opened card lies under its chain lines, its members above them;
        // the band lies under its lines, its chips above them.
        const zIndex = n.type === "member" || n.type === "fold" || n.type === "library" || n.type === "more" ? 2 : 0;
        return [{ id: n.id, type: "card" as const, position: { x: b.x, y: b.y }, width: b.width, height: b.height, data: { node: n, width: b.width, height: b.height, bandRows: n.type === "band" ? placed.bandRows : null }, draggable: false, selectable: false, zIndex }];
      }),
    [model, placed],
  );
  const edges = useMemo(
    (): LineEdge[] =>
      model.edges.flatMap((e) => {
        const r = placed.routes.get(e.id);
        if (r === undefined) return [];
        return [{ id: e.id, source: e.source, target: e.target, type: "line" as const, data: { edge: e, points: r.points, chip: r.chip }, selectable: false, zIndex: e.type === "chain" || e.type === "fold" || e.band !== undefined ? RAISED_LINE_Z : 0 }];
      }),
    [model, placed],
  );
  // The whole map fits between the Hidden note and the legend (T067: the
  // band's text sizes of the design record assume this room).
  const fit = () => {
    void rf.fitView({ padding: FIT_PADDING, maxZoom: 1, duration: 0 });
  };
  // Fit on every new layout (first view, expand, a toggle): the expanded
  // card when there is one (its rows stay readable), else the whole map.
  const expandedId = model.nodes.find((n) => n.type === "component" && n.expanded)?.id ?? null;
  // A chain fits its opened cards and folds (the rest stays dimmed around).
  const chainIds = model.nodes.filter((n) => n.type === "container" || (n.type === "fold" && n.parent === null)).map((n) => n.id).join("\n");
  useEffect(() => {
    const f = requestAnimationFrame(() => {
      if (chainIds !== "") void rf.fitView({ nodes: chainIds.split("\n").map((id) => ({ id })), padding: { top: "96px", left: "24px", right: "24px", bottom: "56px" }, maxZoom: 1, duration: 0 });
      else if (expandedId !== null) void rf.fitView({ nodes: [{ id: expandedId }], padding: 0.25, maxZoom: 1, duration: 0 });
      else void rf.fitView({ padding: FIT_PADDING, maxZoom: 1, duration: 0 });
    });
    return () => {
      cancelAnimationFrame(f);
    };
  }, [placed, rf, expandedId, chainIds]);
  const btn = "grid size-8 place-items-center rounded-md border bg-card text-muted-foreground shadow-xs hover:bg-accent outline-none focus-visible:ring-2 focus-visible:ring-ring [&_svg]:size-4";
  return (
    <>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        minZoom={0.2}
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
          actions.clear();
        }}
        onEdgeClick={(_, e) => {
          if (e.type === "line" && e.data?.edge.type === "link") actions.selectEdge(e.id);
        }}
      >
        {model.cards > MINIMAP_CARDS && (
          <MiniMap
            pannable
            zoomable
            position="bottom-right"
            ariaLabel="Overview of the whole map"
            nodeClassName={(n: Node) => (n.id === actions.selectedNode ? "gv-mm-focus" : "")}
            nodeStrokeWidth={0}
            nodeBorderRadius={3}
            style={{ width: 168, height: 112, right: 52, bottom: 8, margin: 0 }}
          />
        )}
      </ReactFlow>
      <div className="absolute right-3 bottom-3 z-[5] grid gap-1.5" data-testid="map-controls">
        <button type="button" className={btn} aria-label="Zoom in" title="Zoom in" onClick={() => void rf.zoomIn({ duration: 0 })}>
          <Plus aria-hidden="true" />
        </button>
        <button type="button" className={btn} aria-label="Zoom out" title="Zoom out" onClick={() => void rf.zoomOut({ duration: 0 })}>
          <Minus aria-hidden="true" />
        </button>
        <button type="button" className={btn} aria-label="Fit the map" title="Fit the map" onClick={fit}>
          <Maximize aria-hidden="true" />
        </button>
      </div>
    </>
  );
}

/** MapView is the lazy chunk's component: layout, then the canvas. */
export default function MapView({ model, actions, expandedRows, elk, onLayout }: MapViewProps) {
  const runner = elk ?? pageElk();
  const { state, stale, retry } = useMapLayout(model, expandedRows, runner, onLayout);
  return (
    <MapContext.Provider value={actions}>
      <div className="gv-flow gv-arch gv-flow-canvas relative h-full min-h-0 w-full" data-testid="arch-map" data-cards={model.cards} data-layout={state.kind}>
        {state.kind === "loading" && (
          <div className="grid h-full place-items-center" aria-label="Laying out the map">
            <Skeleton className="h-40 w-72" />
          </div>
        )}
        {state.kind === "failed" && !stale && (
          <div className="grid h-full place-items-center p-6 text-sm">
            <div className="max-w-md rounded-lg border bg-card p-4">
              <div className="flex items-center gap-2 font-medium text-unmatched">
                <TriangleAlert aria-hidden="true" className="size-4" /> The map could not be laid out
              </div>
              <p className="mt-1 text-muted-foreground">{state.error}</p>
              <button type="button" className="mt-2 font-medium text-brand-text underline" onClick={retry}>
                Retry
              </button>
              <p className="mt-1 text-xs text-muted-foreground">The Links tab lists every link without a layout.</p>
            </div>
          </div>
        )}
        {state.kind === "ok" && (
          <ReactFlowProvider>
            <Canvas model={model} actions={actions} placed={state.placed} />
          </ReactFlowProvider>
        )}
        <Legend />
      </div>
    </MapContext.Provider>
  );
}
