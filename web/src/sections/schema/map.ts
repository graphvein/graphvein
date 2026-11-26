// What the compact map and the Overview diagram draw (D2, Q7 variant X):
// the tables of one group as boxes and one edge per ordered table pair of
// the group's stored foreign_keys (the only links, ADR 0048). Above
// NEIGHBOURHOOD_ABOVE tables the compact map shows the selected table and
// its one-hop neighbours only, and counts the pairs that leave the view.
// Unresolved references are never drawn.

import type { ElkExtendedEdge, ElkNode } from "elkjs/lib/elk-api";

import type { SchemaGroup, SchemaTable } from "@/api/schema";
import { tableName } from "@/sections/schema/parts";

/** NEIGHBOURHOOD_ABOVE is D2's threshold: more tables than this switch the
 * compact map to the neighbourhood mode. */
export const NEIGHBOURHOOD_ABOVE = 25;

export interface MapEdge {
  id: string;
  /** The referencing table. */
  source: string;
  /** The referenced table. */
  target: string;
  /** Stored column pairs behind the edge. */
  pairs: number;
}

export interface MapView {
  mode: "all" | "neighbourhood";
  tables: SchemaTable[];
  edges: MapEdge[];
  /** Tables in the group (M of "N of M"). */
  total: number;
  /** Pairs with one end in the view and the other outside it (another
   * table of the group, a table elsewhere, or a column without table). */
  outside: number;
  /** Self-reference pairs (not drawn as edges). */
  self: number;
}

/** tableEdges folds the group's pairs into one edge per ordered table
 * pair. Pairs whose target column has no table, and self-references, are
 * not edges. */
export function tableEdges(group: SchemaGroup): MapEdge[] {
  const m = new Map<string, MapEdge>();
  for (const fk of group.foreign_keys) {
    if (fk.target_table === null || fk.target_table === fk.source_table) continue;
    const id = `${fk.source_table}→${fk.target_table}`;
    const e = m.get(id);
    if (e === undefined) m.set(id, { id, source: fk.source_table, target: fk.target_table, pairs: 1 });
    else e.pairs++;
  }
  return [...m.values()];
}

/** mapView picks what the compact map shows for a selection. full forces
 * the whole group (the Overview diagram). */
export function mapView(group: SchemaGroup, selected: string | null, full = false): MapView {
  const ids = new Set(group.tables.map((t) => t.node.identity));
  const all = tableEdges(group).filter((e) => ids.has(e.source) && ids.has(e.target));
  const self = group.foreign_keys.filter((f) => f.target_table !== null && f.target_table === f.source_table).length;
  let shown = ids;
  let mode: MapView["mode"] = "all";
  if (!full && group.tables.length > NEIGHBOURHOOD_ABOVE) {
    mode = "neighbourhood";
    const center = selected !== null && ids.has(selected) ? selected : (group.tables[0]?.node.identity ?? null);
    shown = new Set(center === null ? [] : [center]);
    for (const e of all) {
      if (e.source === center) shown.add(e.target);
      if (e.target === center) shown.add(e.source);
    }
  }
  let outside = 0;
  for (const fk of group.foreign_keys) {
    const s = shown.has(fk.source_table);
    const t = fk.target_table !== null && shown.has(fk.target_table);
    if (s !== t) outside++;
  }
  return {
    mode,
    tables: group.tables.filter((t) => shown.has(t.node.identity)),
    edges: all.filter((e) => shown.has(e.source) && shown.has(e.target)),
    total: group.tables.length,
    outside,
    self,
  };
}

/** isQuiet says whether a table's box is drawn plain: only complete and
 * complete_in_source; partial and unknown boxes are dashed amber with the
 * word (never quiet). */
export function isQuiet(completeness: string): boolean {
  return completeness === "complete" || completeness === "complete_in_source";
}

export const BOX_H = 28;
const CHAR = 8;

/** boxWidth estimates a compact-map box width (mono 13 px) from its text. */
export function boxWidth(t: SchemaTable, measure: (s: string) => number = (s) => s.length * CHAR): number {
  const word = isQuiet(t.completeness) ? 0 : measure(t.completeness === "partial" ? " partial" : " unknown") + 8;
  return Math.ceil(Math.max(64, measure(tableName(t.node)) + word + 24));
}

/** miniElkGraph is the ELK graph of a view: layered, top-down, referenced
 * tables above the tables that reference them. */
export function miniElkGraph(view: MapView, width: (t: SchemaTable) => number): ElkNode {
  const edges: ElkExtendedEdge[] = view.edges.map((e) => ({ id: e.id, sources: [e.target], targets: [e.source] }));
  return {
    id: "root",
    layoutOptions: {
      "elk.algorithm": "layered",
      "elk.direction": "DOWN",
      "elk.edgeRouting": "POLYLINE",
      "elk.json.edgeCoords": "ROOT",
      "elk.spacing.nodeNode": "14",
      "elk.layered.spacing.nodeNodeBetweenLayers": "26",
      "elk.layered.considerModelOrder.strategy": "NODES_AND_EDGES",
      "elk.padding": "[top=12,left=12,bottom=12,right=12]",
    },
    children: view.tables.map((t) => ({ id: t.node.identity, width: width(t), height: BOX_H })),
    edges,
  };
}

export interface Placed {
  boxes: Map<string, { x: number; y: number; width: number; height: number }>;
  routes: Map<string, { x: number; y: number }[]>;
  width: number;
  height: number;
}

function finite(n: number | undefined): n is number {
  return typeof n === "number" && Number.isFinite(n);
}

/** readPlaced reads ELK's answer; a box without a position fails. */
export function readPlaced(view: MapView, out: ElkNode): Placed {
  const boxes = new Map<string, { x: number; y: number; width: number; height: number }>();
  for (const c of out.children ?? []) {
    if (finite(c.x) && finite(c.y) && finite(c.width) && finite(c.height)) boxes.set(c.id, { x: c.x, y: c.y, width: c.width, height: c.height });
  }
  for (const t of view.tables) if (!boxes.has(t.node.identity)) throw new Error(`no position for ${t.node.identity}`);
  const routes = new Map<string, { x: number; y: number }[]>();
  for (const e of out.edges ?? []) {
    const sec = e.sections?.[0];
    if (sec === undefined) continue;
    const pts = [sec.startPoint, ...(sec.bendPoints ?? []), sec.endPoint];
    if (pts.every((p) => finite(p.x) && finite(p.y))) routes.set(e.id, pts.map((p) => ({ x: p.x, y: p.y })));
  }
  let width = 0;
  let height = 0;
  for (const b of boxes.values()) {
    width = Math.max(width, b.x + b.width + 12);
    height = Math.max(height, b.y + b.height + 12);
  }
  return { boxes, routes, width, height };
}
