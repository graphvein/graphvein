// The chain drawn in place (T026, decisions AG16-AG18, rows
// CH1-CH12): one /api/v0/architecture/chain answer turned into numbered
// steps, the cards it crosses and the map model of the chain. Pure, so
// the rules are tested without a canvas.
//
// Precision:
// - every member, edge, end line (CH11) and cap note (CH12) comes from the
//   answer; nothing here adds a step or an edge. The map draws the members
//   the server's budget hint names (`steps[].map.shown`); every other
//   member is counted in a fold of its step, never dropped silently.
// - a member whose card the Show toggles hide is counted in its step's
//   fold as "in hidden components", so a gap on the map is never an
//   absence.
// - numbering runs top to bottom over both directions: the furthest
//   upstream step is 1, the start follows the up steps, then the down
//   steps. The inspector uses the same numbers (synced highlight).

import type { ChainEdge, ChainEnd, ChainMember, ChainResponse, ChainSide, ChainStep, Link } from "@/api/architecture";
import { LIBRARIES_ID, componentNodeId, type MapEdge, type MapModel, type MapNode } from "@/sections/architecture/model";

export type ChainSideName = "up" | "start" | "down";

/** ChainPosition is one numbered step of the drawn chain. */
export interface ChainPosition {
  number: number;
  side: ChainSideName;
  /** The step in its direction (0 for the start). */
  step: number;
  members: ChainMember[];
  /** Members the server's budget hint draws on the map. */
  shown: Set<string>;
  /** Members the map folds (the rest of the step). */
  folded: number;
  hiddenTest: number;
  bounds: ChainStep["bounds"];
  /** Edges into this step from the step nearer the start. */
  edges: ChainEdge[];
}

/** ChainView is a chain answer arranged for the map and the inspector. */
export interface ChainView {
  response: ChainResponse;
  /** True for a component start (CH9): members are cards, no container. */
  componentLevel: boolean;
  positions: ChainPosition[];
  /** Number of each member (its position). */
  numberOf: Map<string, number>;
  member: Map<string, ChainMember>;
  ends: ChainEnd[];
  /** Every followed edge of both directions, from the member nearer the
   * start. */
  edges: (ChainEdge & { side: "up" | "down" })[];
  members: number;
  hiddenTest: number;
  folded: number;
  notFollowed: Record<string, number>;
}

function sideSteps(s: ChainSide | null): ChainStep[] {
  return s === null ? [] : s.steps;
}

/** chainView numbers the steps of an answer. */
export function chainView(r: ChainResponse): ChainView {
  const up = sideSteps(r.up);
  const down = sideSteps(r.down);
  const upFurther = up.filter((s) => s.step > 0);
  const startStep = down.find((s) => s.step === 0) ?? up.find((s) => s.step === 0);
  const positions: ChainPosition[] = [];
  const edgesUp = r.up?.edges ?? [];
  const edgesDown = r.down?.edges ?? [];
  const into = (side: "up" | "down", st: ChainStep): ChainEdge[] => {
    const ids = new Set(st.members.map((m) => m.id));
    return (side === "up" ? edgesUp : edgesDown).filter((e) => ids.has(e.to));
  };
  const pos = (side: ChainSideName, st: ChainStep, n: number, edges: ChainEdge[]): ChainPosition => ({
    number: n,
    side,
    step: st.step,
    members: st.members,
    shown: new Set(st.map.shown),
    folded: st.map.folded,
    hiddenTest: st.hidden_test,
    bounds: st.bounds,
    edges,
  });
  let n = 1;
  for (const st of [...upFurther].sort((a, b) => b.step - a.step)) positions.push(pos("up", st, n++, into("up", st)));
  if (startStep !== undefined) positions.push(pos("start", startStep, n++, []));
  for (const st of down.filter((s) => s.step > 0).sort((a, b) => a.step - b.step)) positions.push(pos("down", st, n++, into("down", st)));
  const numberOf = new Map<string, number>();
  const member = new Map<string, ChainMember>();
  for (const p of positions) {
    for (const m of p.members) {
      if (!numberOf.has(m.id)) numberOf.set(m.id, p.number);
      if (!member.has(m.id)) member.set(m.id, m);
    }
  }
  const sides = [r.up, r.down].filter((s): s is ChainSide => s !== null);
  const notFollowed: Record<string, number> = {};
  for (const s of sides) for (const [k, v] of Object.entries(s.not_followed)) notFollowed[k] = (notFollowed[k] ?? 0) + v;
  return {
    response: r,
    componentLevel: r.start.level === "component",
    positions,
    numberOf,
    member,
    ends: sides.flatMap((s) => s.ends),
    edges: [...edgesUp.map((e) => ({ ...e, side: "up" as const })), ...edgesDown.map((e) => ({ ...e, side: "down" as const }))],
    members: sides.reduce((a, s) => a + s.members, 0),
    hiddenTest: sides.reduce((a, s) => a + s.hidden_test, 0),
    folded: positions.reduce((a, p) => a + p.folded, 0),
    notFollowed,
  };
}

/** cardOf is the map card a member belongs to on a model: a component's
 * card (or the libraries card), a table's database card, a database
 * member's own card; null when the Show toggles hide its card or the
 * answer names no card for it. */
export function cardOf(model: MapModel, m: ChainMember): string | null {
  const has = (id: string) => model.nodes.some((n) => n.id === id && (n.type === "component" || n.type === "database" || n.type === "libraries" || n.type === "repository"));
  if (m.kind === "database") return has(m.id) ? m.id : null;
  // CH13: another member's node, or its card in a component chain.
  if (m.kind === "repository") return has(m.id) ? m.id : null;
  if (m.repository !== null) return has(`repository:${m.repository}`) ? `repository:${m.repository}` : null;
  if (m.database !== null && has(m.database)) return m.database;
  if (m.component === null) return null;
  const card = componentNodeId(m.component);
  if (has(card)) return card;
  const lib = model.nodes.flatMap((n) => (n.type === "libraries" ? [n] : []))[0];
  if (lib !== undefined && lib.members.some((c) => c.id === m.component)) return LIBRARIES_ID;
  return null;
}

/** memberNodeId is the map node id of a drawn member. */
export const memberNodeId = (identity: string) => `m:${identity}`;

/** ChainOverlay is what the cards and lines read while a chain is drawn. */
export interface ChainOverlay {
  view: ChainView;
  /** Step numbers per card (component-level chains) or per member node. */
  numbers: Map<string, number[]>;
  /** The selected member (synced with the inspector), or null. */
  selected: string | null;
}

/** chainNumbersOnCards numbers the cards of a component-level chain. */
export function chainNumbersOnCards(model: MapModel, v: ChainView): Map<string, number[]> {
  const out = new Map<string, number[]>();
  for (const p of v.positions) {
    for (const m of p.members) {
      const card = cardOf(model, m);
      if (card === null) continue;
      const list = out.get(card) ?? [];
      if (!list.includes(p.number)) list.push(p.number);
      out.set(card, list);
    }
  }
  return out;
}

/** componentChainFocus is what a component-level chain leaves undimmed:
 * its cards and the lines between two of them that bundle a chain edge. */
export function componentChainFocus(model: MapModel, v: ChainView): { nodes: Set<string>; edges: Set<string> } {
  const nodes = new Set<string>();
  for (const m of v.member.values()) {
    const c = cardOf(model, m);
    if (c !== null) nodes.add(c);
  }
  const pairs = new Set<string>();
  for (const e of v.edges) {
    const a = v.member.get(e.from);
    const b = v.member.get(e.to);
    if (a === undefined || b === undefined) continue;
    const ca = cardOf(model, a);
    const cb = cardOf(model, b);
    if (ca === null || cb === null || ca === cb) continue;
    pairs.add(ca < cb ? `${ca}|${cb}` : `${cb}|${ca}`);
  }
  const edges = new Set<string>();
  for (const e of model.edges) {
    if (e.type !== "link") continue;
    const key = e.source < e.target ? `${e.source}|${e.target}` : `${e.target}|${e.source}`;
    if (pairs.has(key)) edges.add(e.id);
  }
  return { nodes, edges };
}

/** ChainLineData is a drawn chain edge: the stored edges between two drawn
 * members (one per kind), in map direction (the caller above). */
export interface ChainLine {
  from: string;
  to: string;
  kinds: ChainEdge[];
}

/** buildChainMap turns the overview model into the chain's map: each card
 * the drawn members cross opens to those members only (a container with
 * numbered member nodes), each step's other members fold into counts, the
 * other cards stay (dimmed by the focus), and lines touching an opened
 * card give way to the chain's own edges. */
export function buildChainMap(base: MapModel, v: ChainView): { model: MapModel; focus: { nodes: Set<string>; edges: Set<string> } } {
  const containers = new Map<string, ChainMember[]>();
  const drawn = new Set<string>();
  // Folds: per step number and container (null: the step's root fold).
  const folds = new Map<string, { number: number; card: string | null; count: number; hidden: number; anchor: string | null; side: ChainSideName }>();
  const fold = (p: ChainPosition, card: string | null, hidden: boolean, anchor: string | null) => {
    const key = `${String(p.number)}|${card ?? ""}`;
    const f = folds.get(key) ?? { number: p.number, card, count: 0, hidden: 0, anchor: null, side: p.side };
    f.count++;
    if (hidden) f.hidden++;
    if (f.anchor === null) f.anchor = anchor;
    folds.set(key, f);
  };
  // Drawn members first, so folds know which cards are open.
  for (const p of v.positions) {
    for (const m of p.members) {
      if (!p.shown.has(m.id) || drawn.has(m.id)) continue;
      const card = cardOf(base, m);
      if (card === null) continue;
      drawn.add(m.id);
      const list = containers.get(card) ?? [];
      list.push(m);
      containers.set(card, list);
    }
  }
  // A drawn card with folded members only opens too (its folds name
  // where the members are); members of hidden cards fold at the root.
  for (const p of v.positions) {
    for (const m of p.members) {
      if (drawn.has(m.id)) continue;
      const card = cardOf(base, m);
      if (card !== null && !containers.has(card)) containers.set(card, []);
      const anchor = m.parents.find((x) => drawn.has(x)) ?? null;
      fold(p, card, card === null, anchor);
    }
  }
  const nodes: MapNode[] = [];
  for (const n of base.nodes) {
    const members = containers.get(n.id);
    if (members !== undefined && (n.type === "component" || n.type === "database" || n.type === "libraries" || n.type === "repository")) {
      nodes.push({ type: "container", id: n.id, card: n, members: members.map((m) => m.id) });
      for (const m of members) nodes.push({ type: "member", id: memberNodeId(m.id), member: m, number: v.numberOf.get(m.id) ?? 0, parent: n.id });
    } else nodes.push(n);
  }
  for (const f of folds.values()) {
    nodes.push({ type: "fold", id: `fold:${String(f.number)}:${f.card ?? ""}`, number: f.number, count: f.count, hidden: f.hidden, parent: f.card });
  }
  const open = (id: string) => containers.has(id);
  const edges: MapEdge[] = base.edges.filter((e) => !open(e.source) && !open(e.target)).filter((e) => nodes.some((n) => n.id === e.source) && nodes.some((n) => n.id === e.target));
  // Chain lines between drawn members, in map direction: the side's
  // caller above its callee (an up edge runs from the start side, so it
  // is drawn reversed). Inside one opened card a line joins the two
  // members; between two cards the lines are bundled into one per card
  // pair (AG15), counted on its chip.
  const lines = new Map<string, ChainLine & { inner: boolean }>();
  for (const e of v.edges) {
    if (!drawn.has(e.from) || !drawn.has(e.to)) continue;
    const [from, to] = e.side === "up" ? [e.to, e.from] : [e.from, e.to];
    const mf = v.member.get(from);
    const mt = v.member.get(to);
    if (mf === undefined || mt === undefined) continue;
    const cf = cardOf(base, mf);
    const ct = cardOf(base, mt);
    if (cf === null || ct === null) continue;
    const inner = cf === ct;
    const [a, b] = inner ? [memberNodeId(from), memberNodeId(to)] : [cf, ct];
    const key = `${a}>${b}`;
    const l = lines.get(key) ?? { from: a, to: b, kinds: [], inner };
    l.kinds.push(e);
    lines.set(key, l);
  }
  const focusEdges = new Set<string>();
  for (const [key, l] of lines) {
    const id = `ch:${key}`;
    focusEdges.add(id);
    edges.push({ id, type: "chain", source: l.from, target: l.to, links: [], count: l.kinds.length, both: false, families: [], cells: EMPTY_CELLS, chain: l.kinds, inner: l.inner });
  }
  for (const f of folds.values()) {
    if (f.anchor === null || f.card === null) continue;
    const id = `fold:${String(f.number)}:${f.card}`;
    const anchor = memberNodeId(f.anchor);
    const e: MapEdge = f.side === "up" ? { id: `fe:${id}`, type: "fold", source: id, target: anchor, links: [], count: f.count, both: false, families: [], cells: EMPTY_CELLS } : { id: `fe:${id}`, type: "fold", source: anchor, target: id, links: [], count: f.count, both: false, families: [], cells: EMPTY_CELLS };
    focusEdges.add(e.id);
    edges.push(e);
  }
  const focusNodes = new Set<string>();
  for (const n of nodes) if (n.type === "container" || n.type === "member" || n.type === "fold") focusNodes.add(n.id);
  return { model: { nodes, edges, hidden: base.hidden, cards: base.cards }, focus: { nodes: focusNodes, edges: focusEdges } };
}

const UNKNOWN = { value: "?", state: "unknown", source: "unknown: a chain edge carries no cells", rows: [], counts: null };
const EMPTY_CELLS = { protocol: UNKNOWN, auth: UNKNOWN, tls: UNKNOWN };

/** flowTarget is the endpoint the Flow tab opens for a chain: the start
 * when it is a route or IPC command, else the selected member when it is
 * one, else the route or command nearest the start; null when the chain
 * has none (the Flow tab shows endpoints only). */
export function flowTarget(v: ChainView, selected: string | null): string | null {
  const endpoint = (kind: string) => kind === "route" || kind === "ipc_command";
  if (endpoint(v.response.start.kind)) return v.response.start.id;
  const sel = selected !== null ? v.member.get(selected) : undefined;
  if (sel !== undefined && endpoint(sel.kind)) return sel.id;
  const start = v.positions.find((p) => p.side === "start")?.number ?? 0;
  let best: { id: string; d: number } | null = null;
  for (const p of v.positions) {
    for (const m of p.members) {
      if (!endpoint(m.kind)) continue;
      const d = Math.abs(p.number - start);
      if (best === null || d < best.d) best = { id: m.id, d };
    }
  }
  return best?.id ?? null;
}

/** flowHash is the Flow tab's hash for an endpoint, with the selected
 * member as its card when there is one. */
export function flowHash(endpoint: string, member: string | null, back: string | null = null): string {
  const q = new URLSearchParams();
  q.set("sel", endpoint);
  q.set("tab", "flow");
  if (member !== null && member !== endpoint) q.set("node", member);
  // T026 AG-C3: the chain this link leaves, so the Flow tab starts at the
  // Tables column and links back.
  if (back !== null) q.set("back", back);
  return `#/api?${q.toString()}`;
}

/** chainLinkIds are the links of the overview a chain crosses: an HTTP or
 * IPC link with a member pair the chain follows, or a link without
 * members (SQL, in-process) between two cards a chain edge joins. */
export function chainLinkIds(r: { links: readonly Link[] }, v: ChainView): Set<string> {
  const pairs = new Set<string>();
  const cardPairs = new Set<string>();
  for (const e of v.edges) {
    pairs.add(`${e.from}>${e.to}`).add(`${e.to}>${e.from}`);
    const a = v.member.get(e.from);
    const b = v.member.get(e.to);
    if (a === undefined || b === undefined) continue;
    const ca = endpointOf(a);
    const cb = endpointOf(b);
    if (ca !== null && cb !== null && ca !== cb) cardPairs.add(`${ca}|${cb}`).add(`${cb}|${ca}`);
  }
  const out = new Set<string>();
  for (const l of r.links) {
    const byCard = cardPairs.has(`${l.source}|${l.target}`);
    if (v.componentLevel ? byCard : l.members.length > 0 ? l.members.some((m) => pairs.has(`${m.source}>${m.target}`)) : byCard) out.add(l.id);
  }
  return out;
}

/** endpointOf is the link endpoint of a member: a database card for a
 * table or database, else its component directory. */
function endpointOf(m: ChainMember): string | null {
  if (m.kind === "database" || m.kind === "repository") return m.id;
  if (m.repository !== null) return `repository:${m.repository}`;
  if (m.kind === "table" && m.database !== null) return m.database;
  if (m.kind === "component") return m.id;
  return m.component;
}

/** memberTitle names a chain member: a route's method and path, an IPC
 * command, else its name. */
export function memberTitle(m: { kind: string; name: string; method: string | null; path: string | null; full_path?: string | null; prefix_path?: string | null; command: string | null }): string {
  // A path whose prefix the code does not fix keeps its "…" (T033 UP-33).
  const path = m.full_path ?? m.prefix_path ?? m.path;
  if (m.kind === "route" && path !== null) return `${m.method ?? ""} ${path}`.trim();
  if (m.kind === "ipc_command" && m.command !== null) return m.command;
  return m.name;
}

/** chainOrder is every member in step order (the arrow keys' walk). */
export function chainOrder(v: ChainView): string[] {
  const out: string[] = [];
  const seen = new Set<string>();
  for (const p of v.positions) {
    for (const m of p.members) {
      if (seen.has(m.id)) continue;
      seen.add(m.id);
      out.push(m.id);
    }
  }
  return out;
}

/** memberPath is a route member's served path, else its path whose
 * prefix the code does not fix ("…" for the prefix), else its pattern. */
export function memberPath(m: { path: string | null; full_path: string | null; prefix_path?: string | null }): string | null {
  return m.full_path ?? m.prefix_path ?? m.path;
}
