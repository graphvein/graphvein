// The architecture map's model (T026 AG-C1, combination X+): which cards
// and lines the overview draws for one /api/v0/architecture answer and the
// Show toggles. Pure, so the rules are tested without a canvas.
//
// Precision (VISION, T026 "Governing principle"):
// - every line is one or more of the response's links; nothing here adds a
//   link. A deploy marker is text on its card, never a line (AG3); an SDK
//   stub is a dotted line to a muted stub card, never a protocol (AG5, O2);
//   calls without a link are a chip on their card, never a line (CE1).
// - parallel links between one pair of cards are bundled into one line
//   (AG15); a cell whose links differ reads "mixed" with counts per value,
//   computed from the links' own cells (never a value no link has).
// - a component the toggles hide is counted in the Hidden note, so a gap on
//   the map is never presented as an absence.

import type { Across, ArchitectureResponse, Cell, ChainEdge, ChainMember, Component, Database, Ghost, Link, RemoteMember, Stub } from "@/api/architecture";
import type { HiddenParts } from "@/lib/labels";

/** ShowOptions are the Show toggles. Libraries, tests and docs are off by
 * default (AG4, AG10); SDK stubs are on (AG5). */
export interface ShowOptions {
  libraries: boolean;
  tests: boolean;
  docs: boolean;
  configured: boolean;
  sdk: boolean;
}

export const DEFAULT_SHOW: ShowOptions = { libraries: false, tests: false, docs: false, configured: false, sdk: true };

/** The node id of the folded libraries card. */
export const LIBRARIES_ID = "lib";

export const componentNodeId = (id: string) => `c:${id}`;
export const stubNodeId = (component: string) => `stub:${component}`;
/** Database endpoints are named database:<set> by the server; the node id
 * is that name. */
export const databaseNodeId = (id: string) => id;

export type MapNode =
  // across: the component's calls the project links into another member
  // (serve --project, CE8), null on a single-repository server.
  | { type: "component"; id: string; component: Component; ghosts: Ghost[]; expanded: boolean; across: Across | null }
  | { type: "libraries"; id: string; members: Component[]; inside: number }
  | { type: "database"; id: string; database: Database }
  | { type: "stub"; id: string; stub: Stub }
  // CE8 (serve --project): another member's muted card, reached by the
  // stored project links; nothing of that repository is drawn inside it
  // but a chain's member that crosses the link (CH13).
  | { type: "repository"; id: string; member: RemoteMember; assumptions: string[] }
  // AG-C2, a chain drawn in place (chain.ts): a card opened to the chain's
  // members only, one member of it, and a step's folded count.
  | { type: "container"; id: string; card: Extract<MapNode, { type: "component" | "database" | "libraries" | "repository" }>; members: string[] }
  | { type: "member"; id: string; member: ChainMember; number: number; parent: string }
  | { type: "fold"; id: string; number: number; count: number; hidden: number; parent: string | null };

/** Cells are the three glyph cells of a line. */
export interface Cells {
  protocol: Cell;
  auth: Cell;
  tls: Cell;
}

/** MapEdge is one drawn line: the links it bundles, or one SDK stub. */
export interface MapEdge {
  id: string;
  /** link: bundled links; stub: an SDK stub; chain: stored edges between
   * two drawn chain members; fold: a drawn member to its step's fold. */
  type: "link" | "stub" | "chain" | "fold";
  source: string;
  target: string;
  links: Link[];
  /** Sum of the links' counts (and back counts). */
  count: number;
  /** True when some bundled link runs the other way or has a back count. */
  both: boolean;
  /** The families in the bundle, in first-seen order. */
  families: string[];
  cells: Cells;
  /** chain lines: the stored edges they draw (one per kind), and whether
   * both members sit in one opened card (a compact chip). */
  chain?: ChainEdge[];
  inner?: boolean;
}

export interface MapModel {
  nodes: MapNode[];
  edges: MapEdge[];
  hidden: HiddenParts;
  /** Cards on the map (the minimap shows above MINIMAP_CARDS). */
  cards: number;
}

/** The minimap shows when the map has more cards than this (C1). */
export const MINIMAP_CARDS = 12;

/** isDrawn says where a component goes: its own card, the libraries card,
 * or nowhere (hidden by a toggle). An unknown visibility is drawn, as the
 * API returns it. */
export function placement(c: Component, show: ShowOptions): "card" | "libraries" | "hidden" {
  if (c.visibility === "folded") return show.libraries ? "libraries" : "hidden";
  if (c.visibility === "hidden") {
    if (c.hidden_by === "tests") return show.tests ? "card" : "hidden";
    if (c.hidden_by === "docs") return show.docs ? "card" : "hidden";
    return show.configured ? "card" : "hidden";
  }
  return "card";
}

/** mergeCells bundles the same cell of several links: equal values keep
 * the cell; differing values read "mixed" with the count per value (each
 * link's own counts when it is mixed itself, else its member count). */
export function mergeCells(cells: readonly { cell: Cell; weight: number }[]): Cell {
  const first = cells[0];
  if (first === undefined) return { value: "?", state: "unknown", source: "unknown: no link", rows: [], counts: null };
  const same = cells.every((c) => c.cell.value === first.cell.value && c.cell.state === first.cell.state);
  if (same && first.cell.state !== "mixed") return first.cell;
  const counts: Record<string, number> = {};
  const rows: string[] = [];
  for (const { cell, weight } of cells) {
    for (const r of cell.rows) if (!rows.includes(r)) rows.push(r);
    if (cell.counts !== null) for (const [v, n] of Object.entries(cell.counts)) counts[v] = (counts[v] ?? 0) + n;
    else counts[cell.value] = (counts[cell.value] ?? 0) + weight;
  }
  return { value: "mixed", state: "mixed", source: "members differ; counts per value", rows, counts };
}

/** linkCells merges the cells of the links of one line. */
export function linkCells(links: readonly Link[]): Cells {
  const w = (l: Link) => Math.max(1, l.count + l.back_count);
  return {
    protocol: mergeCells(links.map((l) => ({ cell: l.cells.protocol, weight: w(l) }))),
    auth: mergeCells(links.map((l) => ({ cell: l.cells.auth, weight: w(l) }))),
    tls: mergeCells(links.map((l) => ({ cell: l.cells.tls, weight: w(l) }))),
  };
}

/** buildMap computes the cards and lines for an answer, the toggles and
 * the expanded component (null: none). */
export function buildMap(r: ArchitectureResponse, show: ShowOptions, expanded: string | null): MapModel {
  const where = new Map<string, string>();
  const nodes: MapNode[] = [];
  const members: Component[] = [];
  const hidden: HiddenParts = { tests: 0, docs: 0, configured: 0, libraries: 0, testEdges: 0, sdk: 0 };
  for (const c of r.components) {
    const p = placement(c, show);
    if (p === "card") {
      const id = componentNodeId(c.id);
      where.set(c.id, id);
      nodes.push({ type: "component", id, component: c, ghosts: r.ghosts.filter((g) => g.component === c.id), expanded: expanded === c.id, across: r.project?.across.find((a) => a.component === c.id) ?? null });
    } else if (p === "libraries") {
      where.set(c.id, LIBRARIES_ID);
      members.push(c);
    } else if (c.visibility === "folded") hidden.libraries++;
    else if (c.hidden_by === "tests") hidden.tests++;
    else if (c.hidden_by === "docs") hidden.docs++;
    else hidden.configured++;
  }
  if (r.scope === "production") hidden.testEdges = Object.values(r.hidden.test_edges).reduce((a, b) => a + b, 0);
  for (const d of r.databases) {
    where.set(d.id, databaseNodeId(d.id));
    nodes.push({ type: "database", id: databaseNodeId(d.id), database: d });
  }
  for (const m of r.project?.members ?? []) {
    where.set(m.id, m.id);
    nodes.push({ type: "repository", id: m.id, member: m, assumptions: r.project?.assumptions ?? [] });
  }

  // Lines: links bundled per unordered pair of drawn cards (AG15).
  const pairs = new Map<string, Link[]>();
  let inside = 0;
  for (const l of r.links) {
    const s = where.get(l.source);
    const t = where.get(l.target);
    if (s === undefined || t === undefined) continue;
    if (s === t) {
      if (s === LIBRARIES_ID) inside += l.count + l.back_count;
      continue;
    }
    const key = s < t ? `${s}|${t}` : `${t}|${s}`;
    const list = pairs.get(key) ?? [];
    list.push(l);
    pairs.set(key, list);
  }
  if (members.length > 0) nodes.push({ type: "libraries", id: LIBRARIES_ID, members, inside });

  const edges: MapEdge[] = [];
  for (const [key, links] of pairs) {
    const head = links[0];
    if (head === undefined) continue;
    const source = where.get(head.source) ?? "";
    const target = where.get(head.target) ?? "";
    const families: string[] = [];
    for (const l of links) if (!families.includes(l.family)) families.push(l.family);
    edges.push({
      id: `e:${key}`,
      type: "link",
      source,
      target,
      links,
      count: links.reduce((a, l) => a + l.count + l.back_count, 0),
      both: links.some((l) => l.back_count > 0 || where.get(l.source) !== source),
      families,
      cells: linkCells(links),
    });
  }

  // SDK stubs (AG5, O2 (a)): one per component card, never a protocol.
  for (const s of r.stubs) {
    const owner = where.get(s.component);
    if (owner === undefined || owner === LIBRARIES_ID) continue;
    if (!show.sdk) {
      hidden.sdk++;
      continue;
    }
    const id = stubNodeId(s.component);
    nodes.push({ type: "stub", id, stub: s });
    edges.push({ id: `s:${s.component}`, type: "stub", source: owner, target: id, links: [], count: s.packages, both: false, families: [], cells: { protocol: s.protocol, auth: s.protocol, tls: s.protocol } });
  }
  return { nodes, edges, hidden, cards: nodes.length };
}

/** focusOf is what stays undimmed while something is selected: the
 * selected card or line, the lines touching it and the cards at their other
 * end. null: nothing selected, nothing dimmed. */
export function focusOf(model: MapModel, selected: { node: string | null; edge: string | null }): { nodes: Set<string>; edges: Set<string> } | null {
  if (selected.node === null && selected.edge === null) return null;
  const nodes = new Set<string>();
  const edges = new Set<string>();
  if (selected.edge !== null) {
    const e = model.edges.find((x) => x.id === selected.edge);
    if (e === undefined) return null;
    edges.add(e.id);
    nodes.add(e.source).add(e.target);
    return { nodes, edges };
  }
  if (selected.node === null || !model.nodes.some((n) => n.id === selected.node)) return null;
  nodes.add(selected.node);
  for (const e of model.edges) {
    if (e.source === selected.node || e.target === selected.node) {
      edges.add(e.id);
      nodes.add(e.source).add(e.target);
    }
  }
  return { nodes, edges };
}

/** endpointName names a link endpoint for text: a component's name, a
 * database card's tool and set, or the raw id when the answer lacks it. */
export function endpointName(r: ArchitectureResponse, id: string): string {
  const c = r.components.find((x) => x.id === id);
  if (c !== undefined) return c.name;
  const d = r.databases.find((x) => x.id === id);
  if (d !== undefined) return databaseTitle(d);
  const m = r.project?.members.find((x) => x.id === id);
  if (m !== undefined) return m.name;
  return id;
}

/** databaseTitle names a database card by its schema set's tool. */
export function databaseTitle(d: Database): string {
  return `${TOOL_WORD[d.tool] ?? d.tool} schema`;
}

const TOOL_WORD: Record<string, string> = { sqlmodel: "SQLModel", sqlalchemy: "SQLAlchemy", prisma: "Prisma", drizzle: "Drizzle", typeorm: "TypeORM", sqlx: "sqlx", diesel: "Diesel", alembic: "Alembic", django: "Django", gorm: "GORM" };
