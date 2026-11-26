// Types, the hand-written guard and the request of the Flow tab's endpoint
// (docs/cli/serve.md "GET /api/v0/api/flow", ADR 0045):
//   GET /api/v0/api/flow?id=&scope=&root=
// The answer holds both hops; the Flow tab filters hops and near misses on
// the client, so a toggle never re-requests or changes a count. Edges are
// stored edges, reported verbatim; near misses are a separate candidates
// list and never edges. Roles and edge kinds stay raw strings: a role this
// UI does not know is kept (the node is drawn with its raw role).

import { parseLinkAssumptions, type LinkAssumption } from "@/api/architecture";
import { parseProjectEndpoint, type ProjectEndpoint } from "@/api/project";
import { parseShapeDifferences, type ShapeDifferences } from "@/api/shapeDifferences";
import {
  ApiFormatError,
  array,
  bool,
  getJSON,
  isTransport,
  object,
  parseIdeStatus,
  parseScopeLabel,
  parseIndex,
  parseNode,
  str,
  strOrNull,
  version,
  type HandlerInfo,
  type ScopeLabel,
  type IdeStatus,
  type IndexRun,
  type JSONObject,
  type PresentedNode,
  type Scope,
  type Signal,
  type Span,
  type Transport,
} from "@/api/endpoints";

/** The roles ADR 0045 documents, in its table order. */
export const FLOW_ROLES = [
  "endpoint",
  "handler",
  "call",
  "owner",
  "enclosing",
  "other_target",
  "near_miss",
  "near_miss_owner",
  "callee",
  "caller",
  "nested",
  "table",
] as const;

export type KnownRole = (typeof FLOW_ROLES)[number];

/** isKnownRole says whether a raw role is one this UI documents. */
export function isKnownRole(role: string): role is KnownRole {
  return (FLOW_ROLES as readonly string[]).includes(role);
}

export interface FlowOmitted {
  callers: number;
  callees: number;
}

/** A node of the neighbourhood with every role it has and its smallest
 * hop. */
export interface FlowNodeEntry {
  node: PresentedNode;
  roles: string[];
  hop: number;
  /** Neighbours the fan-out cap cut that no returned edge reaches. */
  omitted: FlowOmitted;
}

export interface Evidence {
  file: string;
  span: Span | null;
}

/** A stored edge as the server reports it. */
export interface FlowEdge {
  kind: string;
  source: string;
  target: string;
  provenance: string;
  confidence: number;
  /** The http match tier of a call edge; null when absent or unknown. */
  match: string | null;
  /** T026 AG-C3: a reads_table / writes_table edge's chain wording
   * ("reads", "may write", "builds a query on"); null for other kinds. */
  wording: string | null;
  evidence: Evidence[];
  /** A calls_route edge's comparison of what the call sends with what
   * the route reads (T029 W4); null when nothing was compared. */
  shape_differences: ShapeDifferences | null;
  /** The assumptions a calls_route link rests on (an origin nothing
   * proves, UP-V1, UP-V2; T033); empty when there is none. */
  assumptions: LinkAssumption[];
}

/** A candidate table of an unknown site; name null when not stored. */
export interface FlowTableRef {
  id: string;
  name: string | null;
}

/** T026 AG-C3: a site of a returned handler-side function that the
 * indexer recognised but could not bind to a table ("may also use"). */
export interface FlowMayAlsoUse {
  accessor: string;
  hop: number;
  row: string | null;
  tool: string | null;
  reason: string | null;
  partial: boolean;
  line: number | null;
  /** null: any table of the tool's sources in the package. */
  candidates: FlowTableRef[] | null;
}

/** T026 AG-C3: what lies at hop 3, counted, not returned. */
export interface FlowBeyond {
  hop: number;
  tables: number;
  functions: number;
  may_also_use: number;
}

/** A near miss's recorded candidate: not an edge. */
export interface FlowCandidate {
  call: string;
  endpoint: string;
  relation: string;
  candidate_count: number;
  candidate_count_exact: boolean;
}

export interface FlowTruncated {
  nodes_omitted: number;
  edges_omitted: number;
  reasons: string[];
}

export interface FlowCounts {
  callers: number;
  near_misses: number;
  near_misses_incomplete: number;
}

export interface FlowResponse {
  api_version: number;
  repository: string;
  index: IndexRun | null;
  scope: string;
  root: string | null;
  ide: IdeStatus;
  transport: Transport;
  endpoint: PresentedNode;
  handler: HandlerInfo;
  counts: FlowCounts;
  nodes: FlowNodeEntry[];
  edges: FlowEdge[];
  candidates: FlowCandidate[];
  skipped_edges: number;
  truncated: FlowTruncated | null;
  /** T020: /endpoint's two labels; they cover the route or command and
   * call facts only, never the structural nodes. Null when not sent. */
  precision: ScopeLabel | null;
  callers_precision: ScopeLabel | null;
  /** T020 O-S1-5: the label of the code `calls` edges of the chain, which
   * no file label covers. Null when not sent. */
  code_calls_precision: ScopeLabel | null;
  /** T026 AG-C3; [] when not sent. */
  may_also_use: FlowMayAlsoUse[];
  /** T026 AG-C3; null when not sent, when hop 2 was dropped or no handler
   * node is stored. */
  tables_beyond: FlowBeyond | null;
  /** T022 B9 (serve --project, HTTP routes): the cross-repository links
   * into the route, as /endpoint lists them. They are project records,
   * not stored edges of this repository, so they are not among nodes and
   * edges; null on a single-repository server. */
  project: ProjectEndpoint | null;
}

function num(obj: JSONObject, key: string, path: string): number {
  const value = obj[key];
  if (typeof value !== "number" || !Number.isFinite(value)) throw new ApiFormatError(`${path}.${key} is not a number`);
  return value;
}

function count(obj: JSONObject, key: string, path: string): number {
  const value = num(obj, key, path);
  if (!Number.isInteger(value) || value < 0) throw new ApiFormatError(`${path}.${key} is not a count`);
  return value;
}

function strings(value: unknown, path: string): string[] {
  const list = array(value, path);
  if (!list.every((s) => typeof s === "string")) throw new ApiFormatError(`${path} is not a list of strings`);
  return list;
}

function parseSpan(value: unknown, path: string): Span | null {
  if (value === undefined || value === null) return null;
  const obj = object(value, path);
  return {
    start_line: num(obj, "start_line", path),
    start_column: num(obj, "start_column", path),
    end_line: num(obj, "end_line", path),
    end_column: num(obj, "end_column", path),
  };
}

function parseMatch(obj: JSONObject, path: string): string | null {
  const match = obj.match;
  if (match !== undefined && match !== null && typeof match !== "string") {
    throw new ApiFormatError(`${path}.match is not a string or null`);
  }
  return match ?? null;
}

function parseEdgeRef(value: unknown, path: string): HandlerInfo["edge"] {
  if (value === null) return null;
  const obj = object(value, path);
  return {
    kind: str(obj, "kind", path),
    provenance: str(obj, "provenance", path),
    confidence: num(obj, "confidence", path),
    match: parseMatch(obj, path),
  };
}

function parseFlowEdge(value: unknown, path: string): FlowEdge {
  const obj = object(value, path);
  return {
    kind: str(obj, "kind", path),
    source: str(obj, "source", path),
    target: str(obj, "target", path),
    provenance: str(obj, "provenance", path),
    confidence: num(obj, "confidence", path),
    match: parseMatch(obj, path),
    wording: obj.wording === undefined || obj.wording === null ? null : str(obj, "wording", path),
    shape_differences: parseShapeDifferences(obj.shape_differences, `${path}.shape_differences`),
    assumptions: parseLinkAssumptions(obj.assumptions, `${path}.assumptions`),
    evidence: array(obj.evidence ?? [], `${path}.evidence`).map((e, i) => {
      const q = `${path}.evidence[${String(i)}]`;
      const eo = object(e, q);
      return { file: str(eo, "file", q), span: parseSpan(eo.span, `${q}.span`) };
    }),
  };
}

function parseEntry(value: unknown, path: string): FlowNodeEntry {
  const obj = object(value, path);
  const roles = strings(obj.roles, `${path}.roles`);
  if (roles.length === 0) throw new ApiFormatError(`${path}.roles is empty`);
  const omitted = object(obj.omitted, `${path}.omitted`);
  return {
    node: parseNode(obj.node, `${path}.node`),
    roles,
    hop: count(obj, "hop", path),
    omitted: { callers: count(omitted, "callers", `${path}.omitted`), callees: count(omitted, "callees", `${path}.omitted`) },
  };
}

function parseCandidate(value: unknown, path: string): FlowCandidate {
  const obj = object(value, path);
  return {
    call: str(obj, "call", path),
    endpoint: str(obj, "endpoint", path),
    relation: str(obj, "relation", path),
    candidate_count: count(obj, "candidate_count", path),
    // As in /endpoint: an absent field counts as false ("at least N").
    candidate_count_exact: obj.candidate_count_exact === undefined ? false : bool(obj, "candidate_count_exact", path),
  };
}

function intOrNull(obj: JSONObject, key: string, path: string): number | null {
  return obj[key] === undefined || obj[key] === null ? null : count(obj, key, path);
}

function strOrAbsent(obj: JSONObject, key: string, path: string): string | null {
  return obj[key] === undefined ? null : strOrNull(obj, key, path);
}

function parseMayAlsoUse(value: unknown, path: string): FlowMayAlsoUse {
  const obj = object(value, path);
  const cands = obj.candidates;
  return {
    accessor: str(obj, "accessor", path),
    hop: count(obj, "hop", path),
    row: strOrAbsent(obj, "row", path),
    tool: strOrAbsent(obj, "tool", path),
    reason: strOrAbsent(obj, "reason", path),
    partial: obj.partial === undefined ? false : bool(obj, "partial", path),
    line: intOrNull(obj, "line", path),
    candidates:
      cands === undefined || cands === null
        ? null
        : array(cands, `${path}.candidates`).map((c, i) => {
            const q = `${path}.candidates[${String(i)}]`;
            const co = object(c, q);
            return { id: str(co, "id", q), name: strOrAbsent(co, "name", q) };
          }),
  };
}

function parseBeyond(value: unknown, path: string): FlowBeyond | null {
  if (value === undefined || value === null) return null;
  const obj = object(value, path);
  return { hop: count(obj, "hop", path), tables: count(obj, "tables", path), functions: count(obj, "functions", path), may_also_use: count(obj, "may_also_use", path) };
}

function parseTransport(obj: JSONObject, path: string): Transport {
  const value = str(obj, "transport", path);
  if (!isTransport(value)) throw new ApiFormatError(`${path}.transport is not a known transport: ${value}`);
  return value;
}

/** parseFlow checks a /api/v0/api/flow body. Every edge's ends must be
 * returned nodes: the server leaves other edges out (skipped_edges), so an
 * edge to an unknown node is a malformed answer, not something to draw. */
export function parseFlow(value: unknown): FlowResponse {
  const p = "flow";
  const obj = object(value, p);
  const apiVersion = version(obj, p);
  const h = object(obj.handler, `${p}.handler`);
  const c = object(obj.counts, `${p}.counts`);
  let truncated: FlowTruncated | null = null;
  if (obj.truncated !== null) {
    const t = object(obj.truncated, `${p}.truncated`);
    truncated = {
      nodes_omitted: count(t, "nodes_omitted", `${p}.truncated`),
      edges_omitted: count(t, "edges_omitted", `${p}.truncated`),
      reasons: strings(t.reasons, `${p}.truncated.reasons`),
    };
  }
  const out: FlowResponse = {
    api_version: apiVersion,
    repository: str(obj, "repository", p),
    index: parseIndex(obj.index, `${p}.index`),
    scope: str(obj, "scope", p),
    root: strOrNull(obj, "root", p),
    ide: parseIdeStatus(obj.ide, `${p}.ide`),
    transport: parseTransport(obj, p),
    endpoint: parseNode(obj.endpoint, `${p}.endpoint`),
    handler: {
      state: str(h, "state", `${p}.handler`),
      expression: strOrNull(h, "expression", `${p}.handler`),
      node: h.node === null ? null : parseNode(h.node, `${p}.handler.node`),
      edge: parseEdgeRef(h.edge, `${p}.handler.edge`),
    },
    counts: {
      callers: count(c, "callers", `${p}.counts`),
      near_misses: count(c, "near_misses", `${p}.counts`),
      near_misses_incomplete: count(c, "near_misses_incomplete", `${p}.counts`),
    },
    nodes: array(obj.nodes, `${p}.nodes`).map((n, i) => parseEntry(n, `${p}.nodes[${String(i)}]`)),
    edges: array(obj.edges, `${p}.edges`).map((e, i) => parseFlowEdge(e, `${p}.edges[${String(i)}]`)),
    candidates: array(obj.candidates, `${p}.candidates`).map((e, i) => parseCandidate(e, `${p}.candidates[${String(i)}]`)),
    skipped_edges: count(obj, "skipped_edges", p),
    truncated,
    precision: parseScopeLabel(obj.precision, `${p}.precision`),
    callers_precision: parseScopeLabel(obj.callers_precision, `${p}.callers_precision`),
    code_calls_precision: parseScopeLabel(obj.code_calls_precision, `${p}.code_calls_precision`),
    may_also_use: array(obj.may_also_use ?? [], `${p}.may_also_use`).map((m, i) => parseMayAlsoUse(m, `${p}.may_also_use[${String(i)}]`)),
    tables_beyond: parseBeyond(obj.tables_beyond, `${p}.tables_beyond`),
    project: parseProjectEndpoint(obj.project, `${p}.project`),
  };
  const ids = new Set<string>();
  for (const [i, n] of out.nodes.entries()) {
    if (ids.has(n.node.identity)) throw new ApiFormatError(`${p}.nodes[${String(i)}] repeats ${n.node.identity}`);
    ids.add(n.node.identity);
  }
  for (const [i, e] of out.edges.entries()) {
    for (const end of [e.source, e.target]) {
      if (!ids.has(end)) throw new ApiFormatError(`${p}.edges[${String(i)}] names ${end}, which is not a returned node`);
    }
  }
  for (const [i, m] of out.may_also_use.entries()) {
    if (!ids.has(m.accessor)) throw new ApiFormatError(`${p}.may_also_use[${String(i)}] names ${m.accessor}, which is not a returned node`);
  }
  for (const [i, cand] of out.candidates.entries()) {
    for (const end of [cand.call, cand.endpoint]) {
      if (!ids.has(end)) throw new ApiFormatError(`${p}.candidates[${String(i)}] names ${end}, which is not a returned node`);
    }
  }
  return out;
}

export interface FlowParams {
  id: string;
  scope: Scope;
  root: string | null;
  /** The editor chosen in this browser (ADR 0050); absent: none. */
  editor?: string;
}

/** flowPath builds the request path. */
export function flowPath(p: FlowParams): string {
  const parts = [`id=${encodeURIComponent(p.id)}`, `scope=${encodeURIComponent(p.scope)}`];
  if (p.root !== null) parts.push(`root=${encodeURIComponent(p.root)}`);
  if (p.editor !== undefined) parts.push(`editor=${encodeURIComponent(p.editor)}`);
  return `/api/v0/api/flow?${parts.join("&")}`;
}

/** FlowClient is what the Flow tab requests; tests pass their own. Kept
 * apart from ApiClient so the views that do not draw a graph need not
 * fake it. */
export interface FlowClient {
  flow(p: FlowParams, options: Signal): Promise<FlowResponse>;
}

/** httpFlowClient requests the server that served the page. */
export const httpFlowClient: FlowClient = {
  flow: (p, o) => getJSON(flowPath(p), parseFlow, o),
};
