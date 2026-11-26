// The Overview tab (D2, T018b2 section 7): the selected group's ER diagram
// with React Flow, laid out by ELK in the page's one worker. This module is
// a lazy chunk (with React Flow); the schema home without this tab imports
// neither. Edges come only from the group's stored foreign_keys, one per
// ordered table pair; partial and unknown tables are dashed amber; the
// legend is always visible. Zoom in, zoom out and Fit are buttons (keys +,
// -, 0 are hints); a node is a button that opens the full-fact panel, and
// Esc or the panel's close button closes it.

import "@xyflow/react/dist/style.css";
import "@/sections/api/flow/flow.css";
import "@/sections/schema/overview.css";

import { Handle, MarkerType, Position, ReactFlow, ReactFlowProvider, useReactFlow, type Edge, type Node, type NodeProps } from "@xyflow/react";
import type { ElkNode } from "elkjs/lib/elk-api";
import { Check, Maximize, Minus, Plus } from "lucide-react";
import { useEffect, useEffectEvent, useMemo, useState, type KeyboardEvent, type ReactNode } from "react";

import type { SchemaGroup, SchemaTable } from "@/api/schema";
import { cn } from "@/lib/utils";
import { pageElk } from "@/sections/api/flow/elk";
import { textWidth } from "@/sections/api/flow/measure";
import { absenceCell, completenessBadge, keyChip, typeCell } from "@/sections/schema/cells";
import { mapView, type MapEdge } from "@/sections/schema/map";
import { CompletenessBadge, HalfCircle, QuietGlyph, tableName } from "@/sections/schema/parts";
import { commonState } from "@/sections/schema/state";

export interface LayoutRunner {
  layout(graph: ElkNode): Promise<ElkNode>;
}

export interface OverviewTabProps {
  group: SchemaGroup;
  run: string;
  selected: string | null;
  onSelect: (id: string) => void;
  /** The full-fact panel of the selected table, or null when closed. */
  panel: ReactNode;
  onClosePanel: () => void;
  elk?: LayoutRunner | undefined;
}

const HEAD = 32;
const ROW = 20;
const PAD = 10;

type TableData = { table: SchemaTable; quietMark: ReactNode; selected: boolean; onSelect: (id: string) => void; fks: Set<string> };
type TableNode = Node<TableData, "table">;

function colName(c: SchemaTable["columns"][number]): string {
  const n = c.node.attributes.name;
  return typeof n === "string" ? n : c.node.name;
}

/** nodeSize sizes a table node from its text (12-13 px fonts). */
export function nodeSize(t: SchemaTable): { width: number; height: number } {
  const c = completenessBadge(t.completeness, t.node.attributes);
  const quiet = c.tone === "complete" || c.tone === "in_source";
  let w = textWidth(tableName(t.node), "title") + (quiet ? 24 : textWidth(c.short, "badge") + 30);
  for (const col of t.columns) {
    const type = typeCell(col.node.attributes).text;
    w = Math.max(w, 44 + textWidth(colName(col), "sub") + 16 + textWidth(type, "sub"));
  }
  return { width: Math.ceil(Math.max(180, w + 2 * PAD)), height: HEAD + Math.max(1, t.columns.length) * ROW + 8 };
}

function TableCard({ data }: NodeProps<TableNode>) {
  const t = data.table;
  const c = completenessBadge(t.completeness, t.node.attributes);
  const quiet = c.tone === "complete" || c.tone === "in_source";
  return (
    <button
      type="button"
      className={cn(
        "block w-full overflow-hidden rounded-lg border bg-card text-left shadow-[0_1px_2px_rgba(0,0,0,.05)] focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none",
        !quiet && "border-dashed border-dynamic",
        data.selected && "border-2 border-brand",
      )}
      onClick={() => {
        data.onSelect(t.node.identity);
      }}
      data-testid="er-node"
      data-table={tableName(t.node)}
      aria-label={`${tableName(t.node)}, ${c.short}, open the table panel`}
    >
      <Handle type="target" position={Position.Left} isConnectable={false} />
      <Handle type="source" position={Position.Right} isConnectable={false} />
      <div className={cn("flex h-8 items-center gap-2 border-b px-2.5", !quiet && "bg-dynamic-soft")}>
        <span className="font-mono text-[13px] font-semibold">{tableName(t.node)}</span>
        <span className="flex-1" />
        {quiet ? data.quietMark : <CompletenessBadge c={{ ...c, text: c.short }} />}
      </div>
      <div className="py-1">
        {t.columns.length === 0 && (
          <div className="px-2.5 text-xs text-muted-foreground italic" data-testid="er-no-columns">
            {quiet ? absenceCell(t.completeness, "no columns").text : "no columns known"}
          </div>
        )}
        {t.columns.map((col) => {
          const pk = keyChip(col.node.attributes, "primary_key");
          const type = typeCell(col.node.attributes);
          return (
            <div key={col.node.identity} className="flex h-5 items-center gap-1.5 px-2.5 font-mono text-xs">
              <span className={cn("w-[20px] text-xs font-semibold", pk.unknown ? "text-muted-foreground" : "text-brand-text")} title={pk.title}>
                {pk.text}
              </span>
              <span className="w-[20px] text-xs font-semibold text-http">{data.fks.has(col.node.identity) ? "FK" : ""}</span>
              <span>{colName(col)}</span>
              <span className="flex-1" />
              <span className={cn(type.unknown ? "text-muted-foreground italic" : "text-http")}>{type.text}</span>
            </div>
          );
        })}
      </div>
    </button>
  );
}

const nodeTypes = { table: TableCard };

/** EDGE_LABEL: edge labels ("N pairs") at the canvas's 12 px minimum
 * (React Flow's default is 10 px; T018b2 section 7). */
export const EDGE_LABEL = { fontSize: 12 } as const;

/** FIT_INSET is where Fit (and the first view) puts the diagram's top-left
 * corner: 24 px from the left, and below the zoom toolbar (12 px from the
 * top, 32 px high, plus a 12 px gap) so the toolbar never covers the
 * first node row (Review fixes R2). */
export const FIT_INSET = { x: 24, y: 56 } as const;

const TOOL =
  "inline-flex size-8 items-center justify-center rounded-md border bg-card text-foreground outline-none hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring [&_svg]:size-4";

const cache = new Map<string, Map<string, { x: number; y: number }>>();

function isTyping(t: EventTarget | null): boolean {
  return t instanceof HTMLElement && (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.tagName === "SELECT" || t.isContentEditable);
}

/** Legend is a strip under the canvas, not over it, so it never covers a
 * node at any canvas size; it is always visible. */
function Legend({ hint }: { hint: boolean }) {
  return (
    <div className="flex flex-none flex-wrap items-center gap-x-4 gap-y-1 border-t bg-card px-3 py-2 text-xs" data-testid="er-legend">
      <b className="font-semibold">Legend</b>
      <span className="flex items-center gap-2">
        <svg aria-hidden="true" width="28" height="8">
          <path d="M0 4h22" className="stroke-edge" strokeWidth="1.5" />
          <path d="M22 0l6 4-6 4z" className="fill-edge" />
        </svg>
        foreign key (stored edge)
      </span>
      <span className="flex items-center gap-2">
        <span className="font-mono text-xs font-semibold text-brand-text">PK</span>
        <span className="font-mono text-xs font-semibold text-http">FK</span> primary key, foreign key column
      </span>
      <span className="flex items-center gap-2">
        <Check aria-hidden="true" className="size-3.5 text-linked" /> complete · <HalfCircle className="text-muted-foreground" /> complete as far as the migrations show
      </span>
      <span className="flex items-center gap-2">
        <span className="inline-block h-3 w-7 rounded-[3px] border border-dashed border-dynamic" /> partial or unknown: named on the node, facts last known
      </span>
      <span className="text-muted-foreground">Nullability, defaults, indexes and history are in the table panel.</span>
      {hint && <span>Select a table to see every fact.</span>}
    </div>
  );
}

function Canvas({ group, positions, selected, onSelect, panel, onClosePanel }: OverviewTabProps & { positions: Map<string, { x: number; y: number }> }) {
  const rf = useReactFlow<TableNode>();
  const common = commonState(group.tables);
  const view = useMemo(() => mapView(group, selected, true), [group, selected]);
  const fks = useMemo(() => new Set(group.foreign_keys.map((f) => f.source_column)), [group]);
  const nodes = useMemo<TableNode[]>(
    () =>
      view.tables.map((t) => {
        const c = completenessBadge(t.completeness, t.node.attributes);
        const isCommon = (c.tone === "complete" && common === "complete") || (c.tone === "in_source" && common === "complete_in_source");
        const size = nodeSize(t);
        return {
          id: t.node.identity,
          type: "table",
          position: positions.get(t.node.identity) ?? { x: 0, y: 0 },
          data: { table: t, quietMark: isCommon ? <QuietGlyph c={c} /> : <CompletenessBadge c={{ ...c, text: c.short }} />, selected: t.node.identity === selected, onSelect, fks },
          width: size.width,
          height: size.height,
          draggable: false,
          selectable: false,
        };
      }),
    [view, positions, selected, onSelect, common, fks],
  );
  const edges = useMemo<Edge[]>(
    () =>
      view.edges.map((e: MapEdge) => ({
        id: e.id,
        source: e.source,
        target: e.target,
        type: "smoothstep",
        markerEnd: { type: MarkerType.ArrowClosed, width: 16, height: 16, color: e.source === selected || e.target === selected ? "hsl(var(--brand))" : "hsl(var(--edge))" },
        style: { stroke: e.source === selected || e.target === selected ? "hsl(var(--brand))" : "hsl(var(--edge))", strokeWidth: e.source === selected || e.target === selected ? 2 : 1.3 },
        label: e.pairs > 1 ? `${String(e.pairs)} pairs` : undefined,
        labelStyle: EDGE_LABEL,
        focusable: false,
        selectable: false,
      })),
    [view, selected],
  );
  const fit = () => {
    let minX = Infinity;
    let minY = Infinity;
    for (const p of positions.values()) {
      minX = Math.min(minX, p.x);
      minY = Math.min(minY, p.y);
    }
    void rf.setViewport({ x: FIT_INSET.x - (Number.isFinite(minX) ? minX : 0), y: FIT_INSET.y - (Number.isFinite(minY) ? minY : 0), zoom: 1 });
  };
  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.ctrlKey || event.metaKey || event.altKey || isTyping(event.target)) return;
    const k = event.key;
    if (k === "+" || k === "=") void rf.zoomIn({ duration: 0 });
    else if (k === "-") void rf.zoomOut({ duration: 0 });
    else if (k === "0") fit();
    else if (k === "Escape" && panel !== null) onClosePanel();
    else return;
    event.preventDefault();
  };
  return (
    <div className="absolute inset-0 flex flex-col" onKeyDown={onKeyDown} data-testid="er-canvas" data-nodes={nodes.length} data-edges={edges.length}>
      <div className="relative min-h-0 flex-1">
        <ReactFlow
          nodes={nodes}
          edges={edges}
          nodeTypes={nodeTypes}
          defaultViewport={{ ...FIT_INSET, zoom: 1 }}
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
          attributionPosition="bottom-right"
        />
        <div className="absolute top-3 left-3 z-[5] flex items-center gap-1" role="toolbar" aria-label="Zoom">
          <button type="button" className={TOOL} aria-label="Zoom out" aria-keyshortcuts="-" title="Zoom out (-)" onClick={() => void rf.zoomOut({ duration: 0 })}>
            <Minus aria-hidden="true" />
          </button>
          <button type="button" className={TOOL} aria-label="Zoom in" aria-keyshortcuts="+" title="Zoom in (+)" onClick={() => void rf.zoomIn({ duration: 0 })}>
            <Plus aria-hidden="true" />
          </button>
          <button type="button" className={cn(TOOL, "w-auto gap-1.5 px-2.5 text-xs font-medium")} aria-keyshortcuts="0" title="Fit: back to full size at the top left (0)" onClick={fit}>
            <Maximize aria-hidden="true" />
            Fit
          </button>
        </div>
        {panel !== null && (
          <div className="absolute inset-y-3 right-3 z-10 w-[min(560px,calc(100%-24px))] overflow-auto rounded-xl border bg-card shadow-[0_8px_24px_rgba(0,0,0,.18)]" data-testid="er-panel">
            {panel}
          </div>
        )}
      </div>
      <Legend hint={panel === null} />
    </div>
  );
}

function Body(props: OverviewTabProps) {
  const { group, run } = props;
  const view = useMemo(() => mapView(group, null, true), [group]);
  const key = JSON.stringify([run, group.set, "overview"]);
  const runner = props.elk ?? pageElk();
  const [held, setHeld] = useState<{ key: string; pos: Map<string, { x: number; y: number }> | null; error: string | null } | null>(null);
  // Promise.resolve first: a worker that cannot start rejects instead of
  // throwing inside the effect.
  const run2 = useEffectEvent(() =>
    Promise.resolve()
      .then(() => runner.layout({
        id: "root",
        layoutOptions: {
          "elk.algorithm": "layered",
          "elk.direction": "RIGHT",
          "elk.edgeRouting": "ORTHOGONAL",
          "elk.spacing.nodeNode": "28",
          "elk.layered.spacing.nodeNodeBetweenLayers": "72",
          "elk.layered.considerModelOrder.strategy": "NODES_AND_EDGES",
        },
        children: view.tables.map((t) => ({ id: t.node.identity, ...nodeSize(t) })),
        edges: view.edges.map((e) => ({ id: e.id, sources: [e.source], targets: [e.target] })),
      }))
      .then((out) => {
        const pos = new Map<string, { x: number; y: number }>();
        for (const c of out.children ?? []) if (typeof c.x === "number" && typeof c.y === "number") pos.set(c.id, { x: c.x, y: c.y });
        for (const t of view.tables) if (!pos.has(t.node.identity)) throw new Error(`no position for ${t.node.identity}`);
        return pos;
      }),
  );
  useEffect(() => {
    if (cache.has(key)) return;
    let live = true;
    run2().then(
      (pos) => {
        cache.set(key, pos);
        if (live) setHeld({ key, pos, error: null });
      },
      (err: unknown) => {
        if (live) setHeld({ key, pos: null, error: err instanceof Error ? err.message : String(err) });
      },
    );
    return () => {
      live = false;
    };
  }, [key]);
  const pos = cache.get(key) ?? (held?.key === key ? held.pos : null);
  const error = held?.key === key ? held.error : null;
  return (
    <div className="gv-flow gv-flow-canvas relative min-h-[420px] flex-1 overflow-hidden rounded-xl border" data-testid="overview-tab" aria-busy={pos === null && error === null}>
      {error !== null ? (
        <div role="alert" className="absolute top-4 left-4 rounded-xl border bg-card px-4 py-3 text-[13px]">
          <b className="font-semibold">Could not lay out the diagram.</b> {error}
        </div>
      ) : pos === null ? (
        <div className="absolute top-4 left-4 text-[13px] text-muted-foreground">Laying out the diagram…</div>
      ) : (
        <Canvas {...props} positions={pos} />
      )}
    </div>
  );
}

/** OverviewTab is the lazy chunk's default export. */
export default function OverviewTab(props: OverviewTabProps) {
  return (
    <ReactFlowProvider>
      <Body {...props} />
    </ReactFlowProvider>
  );
}
