// The architecture map's model (T026 AG-C1, combination X+): which cards
// and lines the overview draws for one /api/v0/architecture answer and the
// Show toggles. Pure, so the rules are tested without a canvas.
//
// Precision (VISION, T026 "Governing principle"):
// - every line is one or more of the response's links; nothing here adds a
//   link. A deploy marker is text on its card, never a line (AG3); an SDK
//   stub is a dashed chip on its card, never a line or a protocol (AG5, O2,
//   ADR 0079 revision of T067); calls without a link are a chip on their
//   card, never a line (CE1).
// - workspace libraries (CV3 folded components) sit in the libraries band
//   below the cards (T067, design record T062 variant A): one chip each,
//   one line per card-library pair, library-library pairs drawn only for
//   the selected library and counted on the band. A library no stored
//   link touches is worded "No stored link", never "unused".
// - parallel links between one pair of cards are bundled into one line
//   (AG15); a cell whose links differ reads "mixed" with counts per value,
//   computed from the links' own cells (never a value no link has).
// - a component the toggles hide is counted in the Hidden note, so a gap on
//   the map is never presented as an absence.

import type { Across, ArchitectureResponse, Cell, ChainEdge, ChainMember, Component, Database, Ghost, Link, RemoteMember, Stub } from "@/api/architecture";
import type { HiddenParts } from "@/lib/labels";

/** ShowOptions are the Show toggles. Libraries (the band, AG4 as revised
 * by T067) and SDK chips (AG5) are on by default; tests and docs are off
 * (AG10). */
export interface ShowOptions {
  libraries: boolean;
  tests: boolean;
  docs: boolean;
  configured: boolean;
  sdk: boolean;
}

export const DEFAULT_SHOW: ShowOptions = { libraries: true, tests: false, docs: false, configured: false, sdk: true };

/** The node id of the libraries band. */
export const BAND_ID = "band";
/** The node id of the band's "+ n more libraries" chip. */
export const MORE_ID = "lib-more";

export const componentNodeId = (id: string) => `c:${id}`;
/** libraryNodeId is a library chip's node id in the band. Not a card id:
 * selecting a library lights its stored neighbours, it starts no chain. */
export const libraryNodeId = (id: string) => `lib:${id}`;
/** Database endpoints are named database:<set> by the server; the node id
 * is that name. */
export const databaseNodeId = (id: string) => id;

export type MapNode =
  // across: the component's calls the project links into another member
  // (serve --project, CE8), null on a single-repository server.
  // stub: the SDK chip on the card (AG5), null when none or toggled off;
  // fan: the card's line chip into the band; narrow: the 180 px card the
  // map uses above NARROW_CARDS cards (kind on the second line).
  | { type: "component"; id: string; component: Component; ghosts: Ghost[]; expanded: boolean; across: Across | null; stub: Stub | null; fan: Fan | null; narrow: boolean }
  | { type: "database"; id: string; database: Database }
  // The libraries band (one per map) and its chips.
  | { type: "band"; id: string; band: Band }
  | { type: "library"; id: string; component: Component; users: number; display: string; row: number }
  | { type: "more"; id: string; count: number; expanded: boolean }
  // CE8 (serve --project): another member's muted card, reached by the
  // stored project links; nothing of that repository is drawn inside it
  // but a chain's member that crosses the link (CH13).
  | { type: "repository"; id: string; member: RemoteMember; assumptions: string[] }
  // AG-C2, a chain drawn in place (chain.ts): a card opened to the chain's
  // members only, one member of it, and a step's folded count.
  | { type: "container"; id: string; card: Extract<MapNode, { type: "component" | "database" | "library" | "repository" }>; members: string[] }
  | { type: "member"; id: string; member: ChainMember; number: number; parent: string }
  | { type: "fold"; id: string; number: number; count: number; hidden: number; parent: string | null };

/** Cells are the three glyph cells of a line. */
export interface Cells {
  protocol: Cell;
  auth: Cell;
  tls: Cell;
}

/** MapEdge is one drawn line: the links it bundles. */
export interface MapEdge {
  id: string;
  /** link: bundled links; chain: stored edges between two drawn chain
   * members; fold: a drawn member to its step's fold. */
  type: "link" | "chain" | "fold";
  /** Lines into the libraries band (routed by the band, not ELK): card, a
   * card-library pair (always drawn); between, a library-library pair
   * (drawn while one of its ends is selected). */
  band?: "card" | "between";
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
  /** Cards on the map, band chips not counted (the minimap shows above
   * MINIMAP_CARDS). */
  cards: number;
}

/** Fan is a card's line chip into the band: the summed count of its
 * card-library links and the libraries they reach. */
export interface Fan {
  count: number;
  libraries: number;
  families: string[];
  cells: Cells;
  both: boolean;
}

/** BandRow is one row of the band: the libraries a card uses, one level
 * of libraries used by libraries only, the linked libraries no stored
 * link ends at, or the libraries no stored link touches. */
export interface BandRow {
  kind: "card" | "level" | "nouser" | "none";
  /** 1..n for kind level (the longest chain of library links from a
   * library a card uses); 0 otherwise. */
  level: number;
  /** Library node ids in display order (the card row is reordered under
   * its users by the layout); folded chips are left out. */
  members: string[];
  /** Libraries of the row, folded ones included. */
  total: number;
}

/** Band is the libraries band's model. */
export interface Band {
  rows: BandRow[];
  /** The name prefix every library shares (at least three), dropped from
   * the chips and named in the footer; null when none. */
  prefix: string | null;
  /** Libraries in the band. */
  libraries: number;
  /** Stored links (not pairs) between two libraries, and card-library
   * pairs drawn as lines. */
  between: number;
  cardLines: number;
  /** Libraries no stored link touches. */
  unlinked: number;
  /** Chips show their directory on a second line (names repeat, or few
   * chips). */
  dirs: boolean;
  /** Libraries folded behind the "+ n more libraries" chip (0: none). */
  folded: number;
  expanded: boolean;
}

/** The minimap shows when the map has more cards than this (C1). */
export const MINIMAP_CARDS = 12;
/** Above this many cards the map draws narrow cards (T062 A). */
export const NARROW_CARDS = 6;
/** Chips per band row before the rest fold into "+ n more libraries":
 * four chip lines of six (T062 open question 4). */
export const BAND_ROW_CAP = 24;
/** Chips show their directory when the band has at most this many. */
export const FEW_CHIPS = 12;

/** placement says where a component goes: its own card, the libraries
 * band, or nowhere (hidden by a toggle). An unknown visibility is drawn,
 * as the API returns it. */
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

/** MapOptions are view states that are not Show toggles. */
export interface MapOptions {
  /** The band shows every library (the "+ n more libraries" chip open). */
  bandAll?: boolean;
}

/** buildMap computes the cards and lines for an answer, the toggles and
 * the expanded component (null: none). */
export function buildMap(r: ArchitectureResponse, show: ShowOptions, expanded: string | null, opts: MapOptions = {}): MapModel {
  const where = new Map<string, string>();
  const nodes: MapNode[] = [];
  const cards: Component[] = [];
  const libs: Component[] = [];
  const hidden: HiddenParts = { tests: 0, docs: 0, configured: 0, libraries: 0, testEdges: 0, sdk: 0 };
  for (const c of r.components) {
    const p = placement(c, show);
    if (p === "card") cards.push(c);
    else if (p === "libraries") libs.push(c);
    else if (c.visibility === "folded") hidden.libraries++;
    else if (c.hidden_by === "tests") hidden.tests++;
    else if (c.hidden_by === "docs") hidden.docs++;
    else hidden.configured++;
  }
  if (r.scope === "production") hidden.testEdges = Object.values(r.hidden.test_edges).reduce((a, b) => a + b, 0);
  const cardCount = cards.length + r.databases.length + (r.project?.members.length ?? 0);
  const narrow = cardCount > NARROW_CARDS;
  for (const c of cards) where.set(c.id, componentNodeId(c.id));
  for (const c of libs) where.set(c.id, libraryNodeId(c.id));
  for (const d of r.databases) where.set(d.id, databaseNodeId(d.id));
  for (const m of r.project?.members ?? []) where.set(m.id, m.id);

  // SDK chips (AG5, O2 (a)): one per component card, never a protocol.
  const stubs = new Map<string, Stub>();
  for (const s of r.stubs) {
    if (!cards.some((c) => c.id === s.component)) continue;
    if (!show.sdk) hidden.sdk++;
    else stubs.set(s.component, s);
  }

  // The band: rows, the chips it shows and the ones folded behind "+ n
  // more libraries".
  const band = libs.length > 0 ? bandModel(libs, r.links, new Set(cards.map((c) => c.id)), opts.bandAll === true) : null;
  const shownLibs = new Set(band?.model.rows.flatMap((row) => row.members) ?? []);
  for (const c of libs) if (!shownLibs.has(libraryNodeId(c.id))) where.set(c.id, MORE_ID);

  // Lines: links bundled per unordered pair of drawn ends (AG15).
  const pairs = new Map<string, Link[]>();
  for (const l of r.links) {
    const s = where.get(l.source);
    const t = where.get(l.target);
    if (s === undefined || t === undefined || s === t) continue;
    const key = s < t ? `${s}|${t}` : `${t}|${s}`;
    const list = pairs.get(key) ?? [];
    list.push(l);
    pairs.set(key, list);
  }
  const isBand = (id: string) => id.startsWith("lib:") || id === MORE_ID;
  const edges: MapEdge[] = [];
  const fans = new Map<string, MapEdge[]>();
  for (const [key, links] of pairs) {
    const head = links[0];
    if (head === undefined) continue;
    let source = where.get(head.source) ?? "";
    let target = where.get(head.target) ?? "";
    const families: string[] = [];
    for (const l of links) if (!families.includes(l.family)) families.push(l.family);
    const sb = isBand(source);
    const tb = isBand(target);
    // A card-library line runs from the card into the band.
    if (sb && !tb) [source, target] = [target, source];
    const e: MapEdge = {
      id: `e:${key}`,
      type: "link",
      source,
      target,
      links,
      count: links.reduce((a, l) => a + l.count + l.back_count, 0),
      both: links.some((l) => l.back_count > 0 || where.get(l.source) !== source),
      families,
      cells: linkCells(links),
      ...(sb && tb ? { band: "between" as const } : sb || tb ? { band: "card" as const } : {}),
    };
    edges.push(e);
    if (e.band === "card") fans.set(source, [...(fans.get(source) ?? []), e]);
  }

  for (const c of cards) {
    const id = componentNodeId(c.id);
    const fanEdges = fans.get(id) ?? [];
    const fan: Fan | null =
      fanEdges.length === 0
        ? null
        : {
            count: fanEdges.reduce((a, e) => a + e.count, 0),
            libraries: fanEdges.reduce((a, e) => a + (e.target === MORE_ID ? new Set(e.links.flatMap((l) => [l.source, l.target]).filter((x) => x !== c.id)).size : 1), 0),
            families: [...new Set(fanEdges.flatMap((e) => e.families))],
            cells: linkCells(fanEdges.flatMap((e) => e.links)),
            both: fanEdges.some((e) => e.both),
          };
    nodes.push({ type: "component", id, component: c, ghosts: r.ghosts.filter((g) => g.component === c.id), expanded: expanded === c.id, across: r.project?.across.find((a) => a.component === c.id) ?? null, stub: stubs.get(c.id) ?? null, fan, narrow });
  }
  for (const d of r.databases) nodes.push({ type: "database", id: databaseNodeId(d.id), database: d });
  for (const m of r.project?.members ?? []) nodes.push({ type: "repository", id: m.id, member: m, assumptions: r.project?.assumptions ?? [] });
  if (band !== null) {
    nodes.push({ type: "band", id: BAND_ID, band: band.model });
    for (const c of libs) {
      const id = libraryNodeId(c.id);
      if (!shownLibs.has(id)) continue;
      nodes.push({ type: "library", id, component: c, users: band.users.get(c.id) ?? 0, display: band.display.get(c.id) ?? c.name, row: band.rowOf.get(id) ?? 0 });
    }
    if (band.model.folded > 0 || band.model.expanded) nodes.push({ type: "more", id: MORE_ID, count: band.model.folded, expanded: band.model.expanded });
  }
  return { nodes, edges, hidden, cards: cardCount };
}

/** sharedPrefix is the name prefix, ending in "-" or "_", that every name
 * shares, when at least three names share it and each keeps a rest;
 * otherwise null. */
export function sharedPrefix(names: readonly string[]): string | null {
  const first = names[0];
  if (names.length < 3 || first === undefined) return null;
  let p = first;
  for (const n of names) while (!n.startsWith(p)) p = p.slice(0, -1);
  const cut = Math.max(p.lastIndexOf("-"), p.lastIndexOf("_"));
  if (cut <= 0) return null;
  const prefix = p.slice(0, cut + 1);
  return names.every((n) => n.length > prefix.length) ? prefix : null;
}

/** libraryUsers counts, per library, the distinct components a stored
 * link from them reaches it from (the chip's number, T062 question 5). */
export function libraryUsers(libs: readonly Component[], links: readonly Link[]): Map<string, Set<string>> {
  const users = new Map(libs.map((c) => [c.id, new Set<string>()]));
  for (const l of links) if (l.source !== l.target) users.get(l.target)?.add(l.source);
  return users;
}

/** bandLevels assigns the band rows (T062 variant A): libraries a stored
 * link from a card reaches are row "card"; every other library a library
 * uses sits one level below the deepest library using it (the longest
 * path from the card row). A cycle stops at its first repeat: a depth-first
 * walk of the uses, from the card row and then every library in id order,
 * leaves out each link back into the walk's own path. A linked library no
 * stored link ends at is "nouser"; a library no link touches is "none".
 * Returns the level per library id (0: card row, -1: nouser, -2: none). */
export function bandLevels(libs: readonly Component[], links: readonly Link[], cards: ReadonlySet<string>): Map<string, number> {
  const isLib = new Set(libs.map((c) => c.id));
  const linked = new Set<string>();
  const direct = new Set<string>();
  const usesOf = new Map<string, string[]>();
  const anyUser = new Set<string>();
  for (const l of links) {
    if (l.source === l.target) continue;
    linked.add(l.source).add(l.target);
    if (!isLib.has(l.target)) continue;
    if (cards.has(l.source)) direct.add(l.target);
    else if (isLib.has(l.source)) {
      anyUser.add(l.target);
      const list = usesOf.get(l.source) ?? [];
      if (!list.includes(l.target)) list.push(l.target);
      usesOf.set(l.source, list);
    }
  }
  for (const list of usesOf.values()) list.sort();
  // The walk: users kept per library, back links left out.
  const users = new Map<string, string[]>();
  const state = new Map<string, "open" | "done">();
  const walk = (id: string) => {
    state.set(id, "open");
    for (const t of usesOf.get(id) ?? []) {
      if (state.get(t) === "open") continue;
      users.set(t, [...(users.get(t) ?? []), id]);
      if (!state.has(t)) walk(t);
    }
    state.set(id, "done");
  };
  const ids = libs.map((c) => c.id).sort();
  for (const id of ids) if (direct.has(id) && !state.has(id)) walk(id);
  for (const id of ids) if (!state.has(id)) walk(id);
  const memo = new Map<string, number>();
  const level = (id: string): number => {
    if (direct.has(id)) return 0;
    const known = memo.get(id);
    if (known !== undefined) return known;
    const us = users.get(id) ?? [];
    const v = us.length === 0 ? (anyUser.has(id) ? 1 : -1) : 1 + Math.max(...us.map((u) => Math.max(0, level(u))));
    memo.set(id, v);
    return v;
  };
  const out = new Map<string, number>();
  for (const c of libs) out.set(c.id, !linked.has(c.id) ? -2 : level(c.id));
  return out;
}

function bandModel(libs: readonly Component[], links: readonly Link[], cards: ReadonlySet<string>, expanded: boolean) {
  const users = libraryUsers(libs, links);
  const levels = bandLevels(libs, links, cards);
  const prefix = sharedPrefix(libs.map((c) => c.name));
  const display = new Map<string, string>();
  for (const c of libs) {
    const name = prefix !== null && c.name.startsWith(prefix) ? c.name.slice(prefix.length) : c.name;
    display.set(c.id, c.directory === "." ? `${name} (root ./)` : name);
  }
  const names = [...display.values()];
  const dirs = libs.length <= FEW_CHIPS || new Set(names).size < names.length;
  const n = (id: string) => users.get(id)?.size ?? 0;
  const byUsers = (a: Component, b: Component) => n(b.id) - n(a.id) || (display.get(a.id) ?? "").localeCompare(display.get(b.id) ?? "");
  const groups: { kind: BandRow["kind"]; level: number; list: Component[] }[] = [];
  const at = (v: number) => libs.filter((c) => levels.get(c.id) === v);
  // The card row keeps the answer's order; the layout puts it under its users.
  if (at(0).length > 0) groups.push({ kind: "card", level: 0, list: at(0) });
  const max = Math.max(0, ...levels.values());
  for (let k = 1; k <= max; k++) if (at(k).length > 0) groups.push({ kind: "level", level: k, list: at(k).sort(byUsers) });
  if (at(-1).length > 0) groups.push({ kind: "nouser", level: 0, list: at(-1).sort(byUsers) });
  if (at(-2).length > 0) groups.push({ kind: "none", level: 0, list: at(-2).sort(byUsers) });
  let folded = 0;
  const rowOf = new Map<string, number>();
  const rows: BandRow[] = groups.map((g, i) => {
    const shown = expanded ? g.list : g.list.slice(0, BAND_ROW_CAP);
    folded += g.list.length - shown.length;
    for (const c of shown) rowOf.set(libraryNodeId(c.id), i);
    return { kind: g.kind, level: g.level, members: shown.map((c) => libraryNodeId(c.id)), total: g.list.length };
  });
  const isLib = new Set(libs.map((c) => c.id));
  const model: Band = {
    rows,
    prefix,
    libraries: libs.length,
    between: links.filter((l) => l.source !== l.target && isLib.has(l.source) && isLib.has(l.target)).length,
    cardLines: 0,
    unlinked: at(-2).length,
    dirs,
    folded,
    expanded: expanded && groups.some((g) => g.list.length > BAND_ROW_CAP),
  };
  const pairs = new Set<string>();
  for (const l of links) {
    const [a, b] = [l.source, l.target];
    if (cards.has(a) === cards.has(b) || !(isLib.has(a) || isLib.has(b))) continue;
    pairs.add(a < b ? `${a}|${b}` : `${b}|${a}`);
  }
  model.cardLines = pairs.size;
  return { model, users: new Map([...users].map(([k, v]) => [k, v.size])), display, rowOf };
}

/** Mark is one chip's relation to the selected card or library (T072):
 * the stored counts of the links between them, by direction. */
export interface Mark {
  /** The selection uses it (↓ on the chip). */
  uses: number;
  /** It uses the selection (↑ on the chip). */
  usedBy: number;
}

/** selectionMarks gives, for a selected card or library, every node a
 * band line joins to it with the stored counts of those links by
 * direction (a bundled line sums its links; the "+ n more" chip sums the
 * libraries folded behind it). Empty for any other selection: the band
 * marks its chips instead of drawing a selected library's lines (T071
 * recommendation V3). */
export function selectionMarks(model: MapModel, selected: string | null): Map<string, Mark> {
  const out = new Map<string, Mark>();
  if (selected === null) return out;
  const raw = selected.startsWith("c:") ? selected.slice(2) : selected.startsWith("lib:") ? selected.slice(4) : null;
  if (raw === null) return out;
  for (const e of model.edges) {
    if (e.band === undefined || (e.source !== selected && e.target !== selected)) continue;
    const other = e.source === selected ? e.target : e.source;
    const mk = out.get(other) ?? { uses: 0, usedBy: 0 };
    for (const l of e.links) {
      if (l.source === raw) {
        mk.uses += l.count;
        mk.usedBy += l.back_count;
      } else {
        mk.usedBy += l.count;
        mk.uses += l.back_count;
      }
    }
    out.set(other, mk);
  }
  return out;
}

/** bandHoverNodes is the card or band chip under the pointer (or holding
 * keyboard focus) and the other ends of its card-library lines; empty
 * when it has none. */
export function bandHoverNodes(model: MapModel, hover: string | null): Set<string> {
  const out = new Set<string>();
  if (hover === null) return out;
  for (const e of model.edges) {
    if (e.band !== "card" || (e.source !== hover && e.target !== hover)) continue;
    out.add(e.source).add(e.target);
  }
  return out;
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
  if (selected.node === null || !model.nodes.some((n) => n.id === selected.node && n.type !== "band")) return null;
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
