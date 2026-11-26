// Graph building of the Flow tab (T016f D4): a flow response, the hop
// setting, the near-miss toggle and the tier filter in; the cards and lines
// to draw out. Pure, so every precision rule is tested without a browser:
//
// - every drawn line carries its origin: a stored edge of the response
//   (by index) or a near miss's candidate (by index), never anything else;
// - candidates are drawn only as "candidate" lines (dashed), never solid;
// - the enclosing step is the stored `contains` edge, drawn as "defined
//   in"; callers reached through it get the note "calls <fn> (contains
//   this call)" and are never counted as route callers (the counts come
//   only from the response's `counts`);
// - the filter dims, it never removes or moves anything.

import { categoryOf } from "@/api/categories";
import type { PresentedNode } from "@/api/endpoints";
import { isKnownRole, type FlowEdge, type FlowNodeEntry, type FlowResponse } from "@/api/flow";
import type { ShapeDifferences } from "@/api/shapeDifferences";
import type { LabelStyle } from "@/lib/labelChoice";
import { confidenceNumber, label } from "@/lib/labels";
import { shownRoutePath } from "@/lib/routePath";
import { candidateLabel, edgeTier, TABLE_KINDS, tierText, type FilterTier, type Tier } from "@/sections/api/flow/tiers";

export type Hops = 1 | 2;

export interface GraphOptions {
  hops: Hops;
  nearMisses: boolean;
  /** The tiers the filter shows undimmed. */
  tiers: ReadonlySet<FilterTier>;
  /** The label style of edge texts (T025b); plain when absent. */
  labels?: LabelStyle;
}

/** Columns from left to right (ELK partitions). */
export const COLUMNS = [
  "Callers · hop 2",
  "Enclosing functions",
  "Owners",
  "Call sites",
  "Endpoint",
  "Handler",
  "Callees",
  "Callees · hop 2",
  "Tables",
] as const;

export const COL = {
  callers: 0,
  enclosing: 1,
  owners: 2,
  calls: 3,
  endpoint: 4,
  handler: 5,
  callees: 6,
  callees2: 7,
  tables: 8,
} as const;

export type Tone = "linked" | "unmatched" | "dynamic" | "wrapper" | "other" | "never" | "http" | "ipc";

export interface Badge {
  text: string;
  tone: Tone;
  title?: string;
}

/** A drawn card: one node of the response, or a call with the one owner
 * folded into it. */
export interface GraphNode {
  /** The drawn id: the identity of the node (the call, for a folded card). */
  id: string;
  type: "card" | "state";
  /** Every identity this card stands for (the call and a folded owner). */
  members: string[];
  /** The first role of the primary node (raw). */
  role: string;
  roles: string[];
  hop: number;
  column: number;
  /** The identity the code peek opens: the call of a folded card. */
  peekId: string | null;
  /** The node of the IDE button (the call of a folded card). */
  node: PresentedNode | null;
  title: string;
  /** HTTP method shown before the title (endpoints). */
  method: string | null;
  sub: string;
  badges: Badge[];
  /** "via apiGet()" for a call through a wrapper function. */
  wrapper: string | null;
  /** Some member's file changed or is missing since the index. */
  changed: boolean;
  omitted: { callers: number; callees: number };
  /** Endpoint drawn muted: another target of a conditional call. */
  muted: boolean;
  /** The requested endpoint. */
  focus: boolean;
  /** Extra lines ("calls useGetFlow (contains this call)"). */
  notes: string[];
  /** Words for the peek header ("call site 1 of 3"). */
  step: string;
  dim: boolean;
}

export type EdgeOrigin = { type: "edge"; index: number } | { type: "candidate"; index: number };

export interface GraphEdge {
  id: string;
  source: string;
  target: string;
  origin: EdgeOrigin;
  tier: Tier;
  label: string | null;
  /** The label's tooltip: the other label style's text, or "" for none. */
  labelTitle: string;
  dim: boolean;
}

export interface TierCount {
  tier: Tier;
  count: number;
}

export interface FlowGraph {
  nodes: GraphNode[];
  edges: GraphEdge[];
  /** Drawn lines per tier (legend and chips). */
  tierCounts: Map<Tier, number>;
  /** The tiers of the calls' links to the endpoint, per tier (summary). */
  callerTiers: Map<Tier, number>;
  /** Hop-2 nodes drawn upstream (callers of owners or enclosing
   * functions); not route callers. */
  upstreamHop2: number;
  /** Hop-2 nodes that only the 2-hop setting shows. */
  hiddenHop2: number;
  /** Near-miss nodes hidden by the toggle. */
  hiddenNearMisses: number;
  /** T026 AG-C3: the drawn tables, how many of them a drawn line writes,
   * and the tables at hop 2 that only the 2-hop setting shows. */
  tables: { drawn: number; written: number; hiddenHop2: number };
}

type Attrs = Record<string, unknown>;

function s(a: Attrs, key: string): string | null {
  const v = a[key];
  return typeof v === "string" ? v : null;
}

function line(n: PresentedNode): string {
  const l = n.span?.start_line;
  return l === undefined ? n.file : `${n.file}:${String(l)}`;
}

/** shortName is the last segment of an identity's qualified part:
 * function:web/src/client.ts#apiGet -> apiGet, ...#a.b.call -> call. */
export function shortName(identity: string): string {
  const hash = identity.lastIndexOf("#");
  const q = hash === -1 ? identity : identity.slice(hash + 1);
  let decoded = q;
  try {
    decoded = decodeURIComponent(q);
  } catch {
    // A malformed escape stays as written.
  }
  const dot = decoded.lastIndexOf(".");
  return dot === -1 ? decoded : decoded.slice(dot + 1);
}

/** wrapperOf names the wrapper function a call goes through: the http
 * `wrapper` attribute, or the ipc `wrapper_identity` of a wrapper form. */
export function wrapperOf(n: PresentedNode): string | null {
  const a = n.attributes;
  const id = n.kind === "ipc_call" ? s(a, "wrapper_identity") : n.kind === "client_call" ? s(a, "wrapper") : null;
  if (id === null || id === "") return null;
  return `via ${shortName(id)}()`;
}

function isCall(n: PresentedNode): boolean {
  return n.kind === "client_call" || n.kind === "ipc_call" || n.kind === "action_call";
}

function isEndpointKind(n: PresentedNode): boolean {
  return n.kind === "route" || n.kind === "ipc_command" || n.kind === "server_action";
}

/** endpointTitle is a route's method and path, or a command's name. */
export function endpointTitle(n: PresentedNode): { method: string | null; title: string } {
  const a = n.attributes;
  if (n.kind === "route") {
    // A path whose prefix the code does not fix keeps its "…" (T033).
    const template = shownRoutePath(a);
    return { method: s(a, "method"), title: template ?? s(a, "pattern") ?? n.name };
  }
  if (n.kind === "server_action") return { method: null, title: s(a, "action") ?? n.name };
  return { method: null, title: s(a, "command") ?? n.name };
}

function nodeTitle(n: PresentedNode): string {
  if (isEndpointKind(n)) return endpointTitle(n).title;
  if (n.kind === "file") return `${n.file} (top level)`;
  if (isCall(n)) return n.name;
  return n.qualified_name !== "" ? n.qualified_name : n.name;
}

function ownerTitle(owner: PresentedNode, call: PresentedNode): string {
  if (owner.kind === "file") {
    const base = call.file.split("/").pop() ?? call.file;
    return `${base} (top level)`;
  }
  return owner.qualified_name !== "" ? owner.qualified_name : owner.name;
}

function changed(n: PresentedNode): boolean {
  return n.file_state !== null && n.file_state !== "unchanged";
}

/** columnOf places a node by its first role (D4). */
export function columnOf(role: string, hop: number): number {
  switch (role) {
    case "endpoint":
    case "other_target":
      return COL.endpoint;
    case "handler":
      return COL.handler;
    case "call":
    case "near_miss":
      return COL.calls;
    case "owner":
    case "near_miss_owner":
      return COL.owners;
    case "enclosing":
      return COL.enclosing;
    case "callee":
      return hop >= 2 ? COL.callees2 : COL.callees;
    case "caller":
      return COL.callers;
    case "nested":
      // A nested function sits with its container's hop (T026 AG-C3).
      return hop <= 0 ? COL.handler : hop === 1 ? COL.callees : COL.callees2;
    case "table":
      return COL.tables;
    default:
      // A role this UI does not know: drawn beside the endpoint with its
      // raw role, never dropped.
      return COL.endpoint;
  }
}

const NEAR_MISS_ROLES = new Set(["near_miss", "near_miss_owner"]);

/** onlyNearMiss says whether a node is there only because of a near miss
 * (the toggle hides those; an owner of a call too stays). */
function onlyNearMiss(e: FlowNodeEntry): boolean {
  return e.roles.every((r) => NEAR_MISS_ROLES.has(r));
}

function callEdgeKinds(e: FlowEdge): boolean {
  return e.kind === "calls_route" || e.kind === "invokes_command";
}

function roleStep(role: string, hop: number): string {
  switch (role) {
    case "endpoint":
      return "endpoint";
    case "handler":
      return "handler";
    case "other_target":
      return "other target of a conditional call";
    case "owner":
      return "owner of call sites";
    case "near_miss_owner":
      return "owner of a near miss";
    case "enclosing":
      return "enclosing function (defined in)";
    case "callee":
      return `callee, hop ${String(hop)}`;
    case "caller":
      return "caller, hop 2";
    case "nested":
      return `nested function (contains, not a call), hop ${String(hop)}`;
    case "table":
      return `table, hop ${String(hop)}`;
    default:
      return `role ${role}`;
  }
}

/** visibleEntries applies the hop setting and the near-miss toggle. */
export function visibleEntries(f: FlowResponse, o: Pick<GraphOptions, "hops" | "nearMisses">): FlowNodeEntry[] {
  return f.nodes.filter((n) => (o.hops === 2 || n.hop <= 1) && (o.nearMisses || !onlyNearMiss(n)));
}

/** buildGraph turns a flow response into the cards and lines to draw. */
export function buildGraph(f: FlowResponse, o: GraphOptions): FlowGraph {
  const visible = visibleEntries(f, o);
  const byId = new Map(visible.map((e) => [e.node.identity, e]));
  const endpointId = f.endpoint.identity;
  const has = (id: string, role: string) => byId.get(id)?.roles.includes(role) === true;

  // Stored edges between two visible nodes, with their index.
  const edges = f.edges.map((e, index) => ({ e, index })).filter(({ e }) => byId.has(e.source) && byId.has(e.target));

  // Owner folding: an owner with exactly one visible call (or near miss)
  // becomes part of that call's card; its contains edge is not drawn.
  const foldedInto = new Map<string, string>(); // owner -> call
  const foldedOwner = new Map<string, string>(); // call -> owner
  const foldedEdge = new Set<number>();
  for (const entry of visible) {
    const id = entry.node.identity;
    if (!(entry.roles.includes("owner") || entry.roles.includes("near_miss_owner"))) continue;
    if (entry.roles.includes("endpoint") || entry.roles.includes("handler")) continue;
    const children = edges.filter(({ e }) => e.kind === "contains" && e.source === id && (has(e.target, "call") || has(e.target, "near_miss")));
    const only = children.length === 1 ? children[0] : undefined;
    if (only === undefined || foldedOwner.has(only.e.target)) continue;
    foldedInto.set(id, only.e.target);
    foldedOwner.set(only.e.target, id);
    foldedEdge.add(only.index);
  }
  const cardOf = (id: string) => foldedInto.get(id) ?? id;

  // Stored call edges per call (all of them, for "1 of K").
  const callTargets = new Map<string, number>();
  for (const e of f.edges) if (callEdgeKinds(e)) callTargets.set(e.source, (callTargets.get(e.source) ?? 0) + 1);

  const calls = visible.filter((e) => e.roles[0] === "call");
  const nearMisses = visible.filter((e) => e.roles[0] === "near_miss");

  const nodes: GraphNode[] = [];
  for (const entry of visible) {
    const n = entry.node;
    const id = n.identity;
    if (foldedInto.has(id)) continue;
    const role = entry.roles[0] ?? "";
    const ownerId = foldedOwner.get(id);
    const owner = ownerId === undefined ? undefined : byId.get(ownerId);
    const badges: Badge[] = [];
    for (const r of entry.roles) if (!isKnownRole(r)) badges.push({ text: `role ${r}`, tone: "other", title: "A role this UI does not know" });
    let title = nodeTitle(n);
    let method: string | null = null;
    const sub = line(n);
    let step = roleStep(role, entry.hop);
    let wrapper: string | null = null;
    if (isEndpointKind(n)) {
      const t = endpointTitle(n);
      method = t.method;
      title = t.title;
    }
    if (role === "other_target") {
      badges.push({ text: "other target", tone: "never", title: "Another stored target of a conditional call" });
    }
    if (isCall(n)) {
      // Title = what, sub = where (T016f D4): a folded card's title is its
      // owner, an unfolded call's title is the call itself; the sub is only
      // its place (T016i D2.4: the call's name is not repeated).
      title = owner !== undefined ? ownerTitle(owner.node, n) : nodeTitle(n);
      const status = s(n.attributes, "status") ?? "";
      if (status !== "linked") {
        const category = categoryOf(f.transport, status);
        badges.push({ text: category === "other" ? `status ${status === "" ? "(none)" : status}` : category.replace("_", " "), tone: category === "dynamic" ? "dynamic" : category === "unmatched" ? "unmatched" : category === "wrapper" ? "wrapper" : "other", title: `Status: ${status === "" ? "(no status)" : status}` });
      }
      wrapper = wrapperOf(n);
      if (role === "call") {
        const i = calls.indexOf(entry);
        step = `call site ${String(i + 1)} of ${String(calls.length)}`;
      } else if (role === "near_miss") {
        const i = nearMisses.indexOf(entry);
        step = `near miss ${String(i + 1)} of ${String(nearMisses.length)}`;
        badges.push({ text: "near miss", tone: "other", title: "Lists this endpoint as a recorded candidate; not a stored link" });
      }
    }
    if (role === "nested") badges.push({ text: "nested", tone: "never", title: "Defined inside the function before it (a stored contains edge); structure, not a call" });
    if (role === "table") {
      title = n.name;
      const access = tableAccess(id, edges.map(({ e }) => e), o.labels ?? "plain");
      if (access !== null) badges.push(access);
    }
    if (n.attributes.scope === "test") badges.push({ text: "test", tone: "other", title: "Test scope" });
    const members = owner === undefined ? [id] : [id, owner.node.identity];
    const memberNodes = owner === undefined ? [n] : [n, owner.node];
    nodes.push({
      id,
      type: "card",
      members,
      role,
      roles: owner === undefined ? entry.roles : [...entry.roles, ...owner.roles.filter((r) => !entry.roles.includes(r))],
      hop: entry.hop,
      column: columnOf(role, entry.hop),
      peekId: id,
      node: n,
      title,
      method,
      sub,
      badges,
      wrapper,
      changed: memberNodes.some(changed),
      omitted: {
        callers: entry.omitted.callers + (owner?.omitted.callers ?? 0),
        callees: entry.omitted.callees + (owner?.omitted.callees ?? 0),
      },
      muted: role === "other_target",
      focus: id === endpointId,
      notes: [],
      step,
      dim: false,
    });
  }

  // The handler box shows the state when no handler node is stored. It is
  // not a node of the index and no line touches it.
  if (f.handler.node === null) {
    const why =
      f.handler.state === "inline"
        ? "Inline handler in the registration"
        : f.handler.state === "missing"
          ? "Handler named but not in the index"
          : f.handler.state === "unresolved"
            ? "Handler not resolved"
            : `Handler state ${f.handler.state}`;
    nodes.push({
      id: "state:handler",
      type: "state",
      members: [],
      role: "handler",
      roles: [],
      hop: 0,
      column: COL.handler,
      peekId: null,
      node: null,
      title: why,
      method: null,
      sub: f.handler.expression ?? "no stored handler node",
      badges: [],
      wrapper: null,
      changed: false,
      omitted: { callers: 0, callees: 0 },
      muted: true,
      focus: false,
      notes: [],
      step: "handler",
      dim: false,
    });
  }

  nodes.push(...tableCards(f));

  const nodeById = new Map(nodes.map((n) => [n.id, n]));

  // Lines: stored edges, then candidates.
  const out: GraphEdge[] = [];
  for (const { e, index } of edges) {
    if (foldedEdge.has(index)) continue;
    const tier = edgeTier(e);
    const text = tierText(e, tier, o.labels ?? "plain");
    let label = text?.text ?? null;
    // A nested function's contains edge reads from the container to it.
    if (e.kind === "contains" && has(e.target, "nested") && !has(e.target, "owner")) label = "nested, not a call";
    const k = callTargets.get(e.source) ?? 0;
    if (callEdgeKinds(e) && k > 1 && label !== null) label = `${label} · 1 of ${String(k)}`;
    out.push({ id: `e${String(index)}`, source: cardOf(e.source), target: cardOf(e.target), origin: { type: "edge", index }, tier, label, labelTitle: text?.title ?? "", dim: false });
    // A hop-2 caller of an enclosing function calls that function, not the
    // route: say so on its card.
    if (e.kind === "calls" && has(e.target, "enclosing") && !has(e.target, "owner") && has(e.source, "caller")) {
      const card = nodeById.get(cardOf(e.source));
      const target = byId.get(e.target);
      if (card !== undefined && target !== undefined) {
        const note = `calls ${nodeTitle(target.node)} (contains this call)`;
        if (!card.notes.includes(note)) card.notes.push(note);
      }
    }
  }
  if (o.nearMisses) {
    f.candidates.forEach((c, index) => {
      if (!byId.has(c.call) || !byId.has(c.endpoint)) return;
      out.push({
        id: `c${String(index)}`,
        source: cardOf(c.call),
        target: cardOf(c.endpoint),
        origin: { type: "candidate", index },
        tier: "candidate",
        label: candidateLabel(c.relation, c.candidate_count, c.candidate_count_exact),
        labelTitle: "",
        dim: false,
      });
    });
  }

  // The filter dims lines of switched-off tiers and the cards left with
  // only dimmed lines. "defined in" lines follow their cards.
  for (const e of out) if (e.tier !== "contains") e.dim = !o.tiers.has(e.tier);
  for (const n of nodes) {
    if (n.focus || n.type === "state") continue;
    const tiered = out.filter((e) => e.tier !== "contains" && (e.source === n.id || e.target === n.id));
    if (tiered.length > 0) n.dim = tiered.every((e) => e.dim);
  }
  for (const n of nodes) {
    if (n.focus || n.type === "state" || n.dim) continue;
    const tiered = out.filter((e) => e.tier !== "contains" && (e.source === n.id || e.target === n.id));
    if (tiered.length > 0) continue;
    const structural = out.filter((e) => e.tier === "contains" && (e.source === n.id || e.target === n.id));
    if (structural.length === 0) continue;
    n.dim = structural.every((e) => nodeById.get(e.source === n.id ? e.target : e.source)?.dim === true);
  }
  for (const e of out) {
    if (e.tier === "contains") e.dim = nodeById.get(e.source)?.dim === true || nodeById.get(e.target)?.dim === true;
  }

  // Tab order and ELK model order: by column, then response order.
  const order = new Map(f.nodes.map((n, i) => [n.node.identity, i]));
  nodes.sort((a, b) => a.column - b.column || (order.get(a.id) ?? Infinity) - (order.get(b.id) ?? Infinity));

  const tierCounts = new Map<Tier, number>();
  for (const e of out) tierCounts.set(e.tier, (tierCounts.get(e.tier) ?? 0) + 1);
  const callerTiers = new Map<Tier, number>();
  for (const c of f.nodes) {
    if (!c.roles.includes("call")) continue;
    const link = f.edges.find((e) => e.source === c.node.identity && e.target === endpointId && callEdgeKinds(e));
    const t = link === undefined ? "other" : edgeTier(link);
    callerTiers.set(t, (callerTiers.get(t) ?? 0) + 1);
  }
  return {
    nodes,
    edges: out,
    tierCounts,
    callerTiers,
    upstreamHop2: visible.filter((e) => e.hop === 2 && e.roles[0] === "caller").length,
    hiddenHop2: o.hops === 1 ? f.nodes.filter((e) => e.hop >= 2).length : 0,
    hiddenNearMisses: o.nearMisses ? 0 : f.nodes.filter(onlyNearMiss).length,
    tables: {
      drawn: visible.filter((e) => e.roles.includes("table")).length,
      written: visible.filter((e) => e.roles.includes("table") && edges.some(({ e: x }) => x.kind === "writes_table" && x.target === e.node.identity)).length,
      hiddenHop2: o.hops === 1 ? f.nodes.filter((e) => e.hop >= 2 && e.roles.includes("table")).length : 0,
    },
  };
}

/** tierWord is a table line's tier: "fact" or its stored confidence. */
function tierWord(e: FlowEdge): string {
  return e.provenance === "fact" ? "fact" : confidenceNumber(e.confidence);
}

/** tableAccess is a table card's access badge from the drawn lines into
 * it: "R·W fact", "R fact · W 0.9", "W 0.9"; null when none is drawn. */
export function tableAccess(table: string, drawn: readonly FlowEdge[], style: LabelStyle = "plain"): Badge | null {
  const into = drawn.filter((e) => e.target === table && TABLE_KINDS.includes(e.kind));
  if (into.length === 0) return null;
  // A line the server words "builds a query on" (CH6: a query builder's
  // entry) counts as builds, not as a read or write.
  const builder = (e: FlowEdge) => e.wording === "builds a query on";
  const tiers = (kind: string) => [...new Set(into.filter((e) => e.kind === kind && !builder(e)).map(tierWord))].sort();
  const writes = tiers("writes_table");
  const builds = [...new Set(into.filter(builder).map(tierWord))].sort();
  const words = into.map((e) => `${e.wording ?? e.kind} (${e.provenance} ${confidenceNumber(e.confidence)})`);
  const l = label(style, "tableAccess", { reads: tiers("reads_table"), writes, builds, detail: [...new Set(words)].join(", ") });
  return { text: l.text, tone: writes.length > 0 ? "linked" : "never", title: l.title };
}

/** hopWord names a hop for the table notes. */
function hopWord(hop: number): string {
  return `hop ${String(hop)}`;
}

/** tableCards are the Tables column's state boxes (T026 AG-C3), the same
 * in both hop settings so 1 hop draws at the 2-hop layout: the sites the
 * indexer could not bind to a table ("may also use", from the response's
 * may_also_use), and what lies past the drawn hops (tables_beyond and the
 * callees the fan-out cap cut). Neither is a node of the index and no line
 * touches them; neither ever says a table is not used. */
function tableCards(f: FlowResponse): GraphNode[] {
  const hasTables = f.nodes.some((n) => n.roles.includes("table"));
  const out: GraphNode[] = [];
  const box = (id: string, role: string, title: string, sub: string, notes: string[]): GraphNode => ({
    id,
    type: "state",
    members: [],
    role,
    roles: [],
    hop: 0,
    column: COL.tables,
    peekId: null,
    node: null,
    title,
    method: null,
    sub,
    badges: [],
    wrapper: null,
    changed: false,
    omitted: { callers: 0, callees: 0 },
    muted: true,
    focus: false,
    notes,
    step: role,
    dim: false,
  });
  const names = new Map(f.nodes.map((n) => [n.node.identity, n.node]));
  if (f.may_also_use.length > 0) {
    const notes = f.may_also_use.map((m) => {
      const fn = names.get(m.accessor);
      const who = fn === undefined ? shortName(m.accessor) : fn.qualified_name !== "" ? fn.qualified_name : fn.name;
      const what = m.candidates === null ? `any ${m.tool ?? "?"} table` : m.candidates.map((c) => c.name ?? shortName(c.id)).join(", ") || "no candidate";
      const where = m.line === null ? "" : `:${String(m.line)}`;
      return `${who}${where} (${hopWord(m.hop)}): ${what}${m.reason !== null ? ` · ${m.reason}` : ""}${m.partial ? " · partial" : ""}`;
    });
    const n = f.may_also_use.length;
    out.push(box("tables:may-also-use", "may_also_use", "may also use", `${String(n)} ${n === 1 ? "site" : "sites"} not bound to a table`, notes));
  }
  const omitted = f.nodes.filter((n) => n.roles.some((r) => r === "handler" || r === "nested" || r === "callee")).reduce((sum, n) => sum + n.omitted.callees, 0);
  const b = f.tables_beyond;
  const dropped = f.truncated?.reasons.includes("hop2_dropped") === true;
  const notes: string[] = [];
  if (omitted > 0) notes.push(`tables of ${String(omitted)} ${omitted === 1 ? "callee" : "callees"} cut by the fan-out cap not read`);
  if (dropped) notes.push("hop 2 left out (caps): its tables not read");
  if (b !== null && b.may_also_use > 0) notes.push(`+ ${String(b.may_also_use)} unbound ${b.may_also_use === 1 ? "site" : "sites"} at hop ${String(b.hop)}`);
  notes.push("deeper calls not read");
  if (b !== null && b.tables > 0) {
    out.push(box("tables:beyond", "fold", `+ ${String(b.tables)} at hop ${String(b.hop)}`, `${String(b.tables)} ${b.tables === 1 ? "table" : "tables"} via ${String(b.functions)} ${b.functions === 1 ? "function" : "functions"}, not drawn`, notes));
  } else if (hasTables || out.length > 0 || omitted > 0 || dropped) {
    const title = b === null ? "Tables past hop 2 not counted" : `No other table at hop ${String(b.hop)}`;
    out.push(box("tables:beyond", "fold", title, b === null ? "no handler node, or hop 2 left out" : "stored table edges of hop-3 functions", notes));
  }
  return out;
}

/** isDrawnSubset says whether every card and line of `part` (the 1-hop
 * graph) is in `whole` (the 2-hop graph) unchanged: the same id, texts,
 * size and, for a line, the same ends and label (T016i D3.4). Then `part`
 * can be drawn at `whole`'s layout without a layout of its own. Dimming
 * may differ (it is not part of a card's box). */
export function isDrawnSubset(part: FlowGraph, whole: FlowGraph, size: (n: GraphNode) => { width: number; height: number }): boolean {
  const nodes = new Map(whole.nodes.map((n) => [n.id, n]));
  for (const n of part.nodes) {
    const w = nodes.get(n.id);
    if (w === undefined || w.type !== n.type || w.column !== n.column) return false;
    if (w.title !== n.title || w.sub !== n.sub || w.method !== n.method || w.wrapper !== n.wrapper || w.changed !== n.changed) return false;
    if (w.notes.join("\n") !== n.notes.join("\n") || w.badges.map((b) => b.text).join("\n") !== n.badges.map((b) => b.text).join("\n")) return false;
    if (w.omitted.callers !== n.omitted.callers || w.omitted.callees !== n.omitted.callees) return false;
    const a = size(n);
    const b = size(w);
    if (a.width !== b.width || a.height !== b.height) return false;
  }
  const edges = new Map(whole.edges.map((e) => [e.id, e]));
  for (const e of part.edges) {
    const w = edges.get(e.id);
    if (w === undefined || w.source !== e.source || w.target !== e.target || w.label !== e.label || w.tier !== e.tier) return false;
  }
  return true;
}

/** truncatedText explains the server's caps in words, or null. */
export function truncatedText(f: FlowResponse): string | null {
  const t = f.truncated;
  if (t === null) return null;
  const parts: string[] = [];
  for (const r of t.reasons) {
    if (r === "calls_cap") parts.push("only the first 200 callers are drawn");
    else if (r === "near_misses_cap") parts.push("only the first 200 near misses are drawn");
    else if (r === "hop2_dropped") parts.push("hop 2 was left out because the graph would exceed 300 nodes or 600 edges");
    else if (r === "hop1_over_totals") parts.push("hop 1 alone exceeds 300 nodes or 600 edges and is drawn whole");
    else parts.push(`reason ${r}`);
  }
  const counts =
    t.nodes_omitted > 0 || t.edges_omitted > 0
      ? ` (${String(t.nodes_omitted)} ${t.nodes_omitted === 1 ? "node" : "nodes"} and ${String(t.edges_omitted)} ${t.edges_omitted === 1 ? "edge" : "edges"} not drawn)`
      : "";
  return `The neighbourhood is capped: ${parts.join("; ")}${counts}.`;
}

/** The code peek of a Flow selection: the node to show, the header text
 * and, for a line, the stored facts shown above the code (UQ9 A). */
export interface FlowPeek {
  id: string;
  title: string;
  step: string;
  facts: string | null;
  /** A stored calls_route line's request differences (T029 W4); null for
   * a card, a candidate or a line that was not compared. */
  differences: ShapeDifferences | null;
}

function evidenceText(e: FlowEdge): string {
  const first = e.evidence[0];
  if (first === undefined) return "no evidence span";
  const l = first.span === null ? first.file : `${first.file}:${String(first.span.start_line)}`;
  return e.evidence.length > 1 ? `evidence ${l} (+${String(e.evidence.length - 1)} more)` : `evidence ${l}`;
}

/** edgeFacts is the one-line description of a drawn line, from the stored
 * edge (or the near miss's candidate) only. */
export function edgeFacts(f: FlowResponse, e: GraphEdge): string {
  if (e.origin.type === "candidate") {
    const c = f.candidates[e.origin.index];
    if (c === undefined) return "near miss";
    const n = `${c.candidate_count_exact ? "" : "at least "}${String(c.candidate_count)} candidates`;
    return `near miss · ${c.relation} · ${n} · a recorded candidate, not a stored edge`;
  }
  const s = f.edges[e.origin.index];
  if (s === undefined) return "edge";
  const parts = [s.kind, s.provenance, confidenceNumber(s.confidence)];
  if (s.match !== null) parts.push(`match ${s.match}`);
  // A link with assumptions is never shown as a plain link (T033).
  for (const a of s.assumptions) parts.push(`assumes ${a.id === "origin" ? a.text : `${a.id}: ${a.text}`}`);
  parts.push(evidenceText(s));
  return parts.join(" · ");
}

/** flowPeek finds the selected card or line in a built graph; null when it
 * is not drawn (another hop setting, an unknown id). A line peeks its
 * stored source node. */
export function flowPeek(f: FlowResponse, g: FlowGraph, node: string | null, edge: string | null): FlowPeek | null {
  if (edge !== null) {
    const e = g.edges.find((x) => x.id === edge);
    if (e === undefined) return null;
    const source = e.origin.type === "candidate" ? f.candidates[e.origin.index]?.call : f.edges[e.origin.index]?.source;
    if (source === undefined) return null;
    const card = g.nodes.find((n) => n.members.includes(source));
    const entry = f.nodes.find((n) => n.node.identity === source);
    const title = card !== undefined && card.id === source ? card.title : entry !== undefined ? nodeTitle(entry.node) : shortName(source);
    const differences = e.origin.type === "candidate" ? null : (f.edges[e.origin.index]?.shape_differences ?? null);
    return { id: source, title, step: `source of the ${e.tier === "candidate" ? "candidate" : "edge"}`, facts: edgeFacts(f, e), differences };
  }
  if (node !== null) {
    const n = g.nodes.find((x) => x.id === node);
    if (n === undefined || n.peekId === null) return null;
    return { id: n.peekId, title: n.title, step: n.step, facts: null, differences: null };
  }
  return null;
}
