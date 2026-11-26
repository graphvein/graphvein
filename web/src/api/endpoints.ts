// Types, hand-written guards and requests for the API section endpoints
// (docs/cli/serve.md "API section endpoints", ADR 0042):
//   GET /api/v0/api/endpoints  coverage counts and the endpoint list (D2)
//   GET /api/v0/api/endpoint   one endpoint with handler and callers (D3)
//   GET /api/v0/api/calls      the calls of some raw statuses (D4)
// Field names mirror the JSON. Statuses stay raw strings here; the UI maps
// them to categories in categories.ts, where only "linked" is linked.

import { ApiError, fetchJSON } from "@/api/client";
import { parseRouteModelTables, type RouteModelTable } from "@/api/modelTables";
import { parseCallProject, parseProjectEndpoint, type CallProject, type ProjectEndpoint } from "@/api/project";
import { parseRouteShape, parseRouteShapeSummary, type RouteShape, type RouteShapeSummary } from "@/api/routeShape";
import { parseShapeDifferences, type ShapeDifferences } from "@/api/shapeDifferences";
import { API_VERSION } from "@/api/status";

/** The three transports, fixed by node kind on the server: HTTP routes,
 * Tauri IPC commands and Next.js server actions (T024b). */
export type Transport = "http" | "tauri_ipc" | "server_action";

export const TRANSPORTS: readonly Transport[] = ["http", "tauri_ipc", "server_action"];

/** isTransport reports a transport name this UI knows. */
export function isTransport(value: string): value is Transport {
  return value === "http" || value === "tauri_ipc" || value === "server_action";
}

/** The scopes the server accepts. The UI offers production (default) and
 * all ("Include tests"); test exists for completeness. */
export type Scope = "production" | "test" | "all";

/** ApiFormatError reports a response that does not have the documented
 * shape. fetch functions turn it into an ApiError of kind "format". */
export class ApiFormatError extends Error {
  constructor(message: string) {
    super(`malformed API response: ${message}`);
    this.name = "ApiFormatError";
  }
}

/** PrecisionLabel is Graphvein's precision label (T020, ADR 0066). */
export type PrecisionLabel = "precise" | "best_effort";

/** ScopeLabel is the label the server attaches to the facts of one fact
 * family ({label, row, reason} in docs/cli/serve.md). The UI never
 * computes one: it shows what the server sent, or nothing. */
export interface ScopeLabel {
  label: PrecisionLabel;
  /** The rule table row that decided it ("PL-F2", or "D"). */
  row: string | null;
  /** Why the label is best_effort (default_branch, external_indexer,
   * extractor_mismatch, scope_unknown, ...); null when precise. */
  reason: string | null;
  /** The facts of a precise label's records its row does not cover (no
   * oracle compares them): shown unverified beside the chip (T023 GR10:
   * Go routes' handler, middleware, conditional and scope). */
  unverified?: readonly string[];
}

/** FileLabel is a file's label as /source and the change items serve it:
 * Graphvein's own label, or for an external run the producer's own value
 * in reported (never shown as Graphvein's). */
export interface FileLabel {
  language: string | null;
  precision: PrecisionLabel | null;
  reason: string | null;
  /** The rule row behind the label (PL-L1, D); null when the server names
   * none (a refused run, an invalid stored label, an older server). */
  row: string | null;
  reported: string | null;
}

function precisionLabel(value: unknown): PrecisionLabel | null {
  return value === "precise" || value === "best_effort" ? value : null;
}

/**
 * parseScopeLabel reads a scope label. Absent or null is no label (an
 * older server, or a view the server does not label); a label word this
 * UI does not know is also no label, so a newer server can never make the
 * UI claim more than it understands.
 */
export function parseScopeLabel(value: unknown, path: string): ScopeLabel | null {
  if (value === undefined || value === null) return null;
  const obj = object(value, path);
  const label = precisionLabel(obj.label);
  if (label === null) return null;
  const unverified = Array.isArray(obj.unverified) ? obj.unverified.filter((x): x is string => typeof x === "string") : [];
  const out: ScopeLabel = { label, row: strOrNull(obj, "row", path), reason: obj.reason === undefined ? null : strOrNull(obj, "reason", path) };
  if (unverified.length > 0) out.unverified = unverified;
  return out;
}

/** parseFileLabel reads the five file label fields of obj (absent fields
 * read as null: an older server). */
export function parseFileLabel(obj: JSONObject, path: string): FileLabel {
  const opt = (key: string) => (obj[key] === undefined ? null : strOrNull(obj, key, path));
  return {
    language: opt("language"),
    precision: precisionLabel(opt("precision")),
    reason: opt("precision_reason"),
    row: opt("precision_row"),
    reported: opt("reported_precision"),
  };
}

export interface IndexRun {
  finished_at: string;
  indexer: { name: string; version: string };
  contract_version: number;
}

export interface StatusCounts {
  [status: string]: number;
}

export interface CallCounts {
  total: number;
  /** The statuses every view counts with: under serve --project the
   * project-scope statuses (project_by_status: calls the link pass decided
   * across repositories read linked_across or ambiguous_across; T022
   * MR-UI), else the repository's. */
  by_status: StatusCounts;
  /** The repository's own statuses (the server's by_status), the
   * secondary text in project mode; null outside it. */
  repository_by_status: StatusCounts | null;
  linked_without_edge: number;
  edges_from_unlinked: number;
  unknown_scope: number;
}

export interface RootCounts {
  root: string;
  manifest: string | null;
  total: number;
  /** As CallCounts.by_status: project scope under serve --project. */
  by_status: StatusCounts;
  repository_by_status: StatusCounts | null;
}

interface EndpointRowBase {
  id: string;
  name: string;
  framework: string | null;
  file: string;
  line: number | null;
  /** The raw scope attribute: null when absent, "test", or anything an
   * external indexer wrote. */
  scope: unknown;
  caller_count: number;
  /** T022 B9 (serve --project, HTTP routes): calls of other members
   * linked to the route; null on a single-repository server. */
  project_caller_count: number | null;
  /** T022 review (serve --project, HTTP routes): calls of other members
   * the link pass left ambiguous (or incomplete) with this route among the
   * candidates: "possibly called", never callers; null on a
   * single-repository server. */
  project_possible_caller_count: number | null;
  flags: string[];
}

/** RoutePrefixUnknown is the path a route row shows when the route has no
 * full path but a path whose prefix the code does not fix (T033 row UP-29;
 * a Next.js basePath not read, NX-R4): `path` is the known part with "…"
 * for the unknown prefix ("…/api/hosts/{host}/logs/stream"), never a full
 * path. */
export interface RoutePrefixUnknown {
  path: string;
  reason: string;
  /** "prefix unknown: <why>", as the server words the reason. */
  reason_text: string;
  /** The unresolved mount the entry comes from; null for a generated
   * server or a Next.js basePath. */
  hop: string | null;
  /** How many unknown-prefix paths the route has. */
  count: number;
}

export interface HttpEndpointRow extends EndpointRowBase {
  transport: "http";
  method: string | null;
  path: string | null;
  /** Full paths only: 0 for a route whose prefix is unknown. */
  path_count: number;
  /** Set when path is null and the route has an unknown-prefix path. */
  prefix_unknown: RoutePrefixUnknown | null;
  pattern: string | null;
  documented: boolean;
  /** T029 SM: the compact request/response shape; null when the route
   * has no stored shape (every section not read). */
  shape: RouteShapeSummary | null;
}

export interface TauriEndpointRow extends EndpointRowBase {
  transport: "tauri_ipc";
  command: string | null;
  registry: string | null;
  cfg_gated: boolean;
  test_only: boolean;
  unreachable: boolean;
  plugin: string | null;
  handler: string | null;
}

/** ActionEndpointRow is one server action (T024b, ADR 0077): action is
 * the export or binding name (null for an anonymous inline action). */
export interface ActionEndpointRow extends EndpointRowBase {
  transport: "server_action";
  action: string | null;
  action_scope: string | null;
  form: string | null;
  handler: string | null;
}

export type EndpointRow = HttpEndpointRow | TauriEndpointRow | ActionEndpointRow;

export interface TransportSummary {
  transport: Transport;
  frameworks: string[];
  endpoint_count: number;
  never_called: number;
  calls: CallCounts;
  roots: RootCounts[];
  openapi: { specs: number; documented_endpoints: number } | null;
  endpoints: EndpointRow[];
  /** T020: the labels of the transport's endpoints and of its calls;
   * null when the server sends none. */
  precision: ScopeLabel | null;
  callers_precision: ScopeLabel | null;
  /** T022 B9 (serve --project, HTTP): calls of the scope and root that a
   * project link connects to another member's route; null on a
   * single-repository server. */
  project_linked_calls: number | null;
  /** T022 B8 (serve --project, HTTP): routes with neither a caller in
   * the repository nor a project link; null on a single-repository
   * server. never_called keeps its single-repository meaning. */
  never_called_in_project: number | null;
  /** T022 review (serve --project, HTTP): routes without a caller or
   * link that ambiguous or incomplete calls of other members may reach
   * ("possibly called (ambiguous across repositories)"); excluded from
   * never_called_in_project. null on a single-repository server. */
  possibly_called_in_project: number | null;
}

export interface EndpointsResponse {
  api_version: number;
  repository: string;
  index: IndexRun | null;
  scope: string;
  root: string | null;
  transports: TransportSummary[];
}

export interface Span {
  start_line: number;
  start_column: number;
  end_line: number;
  end_column: number;
}

export interface IdeLink {
  link: string;
  line: number;
  column: number;
  exact: boolean;
  /** The link names the line only: the editor's column could not be
   * vouched for (IntelliJ IDEA expands a tab before the node to its own
   * tab size, or the file changed since indexing). Absent in the response
   * means false. */
  line_only: boolean;
  /** Why a link converted from unchanged source is still not exact:
   * "tab_size_unknown" (line only after a tab) or "column_unit_assumed"
   * (Cursor's unit is assumed from VS Code). Null when the response has
   * none: an inexact link then comes from a changed file. Other strings are
   * kept and shown as an unknown caveat. The server omits the field when
   * there is none; null is read the same. */
  caveat: string | null;
}

/** A stored node as the server presents it: attributes verbatim, plus an
 * IDE link, the state of its file now, and for calls the source root. */
export interface PresentedNode {
  identity: string;
  kind: string;
  level: number;
  name: string;
  qualified_name: string;
  file: string;
  span: Span | null;
  content_hash: string;
  attributes: Record<string, unknown>;
  ide: IdeLink | null;
  /** "unchanged", "changed", "missing", or null for synthetic nodes; any
   * other value is kept raw and treated as not unchanged. */
  file_state: string | null;
  source_root: string | null;
}

export interface EdgeRef {
  kind: string;
  provenance: string;
  confidence: number;
  /** The match tier (http callers only), else null. */
  match: string | null;
}

export interface Owner {
  id: string;
  kind: string;
  qualified_name: string;
}

export interface Reason {
  code: string;
  text: string;
  detail: string | null;
}

export interface Caller {
  node: PresentedNode;
  owner: Owner | null;
  edges: EdgeRef[];
  reasons: Reason[];
  /** What the call sends compared with what the route reads (T029 W4);
   * null when nothing was compared. */
  shape_differences: ShapeDifferences | null;
}

export interface NearMiss {
  node: PresentedNode;
  owner: Owner | null;
  relation: string;
  candidate_count: number;
  /** False when candidate_count is a lower bound (a capped list without a
   * stored tie size). An absent field counts as false: the UI then says
   * "at least N", which is never wrong. */
  candidate_count_exact: boolean;
  reasons: Reason[];
}

export interface IdeStatus {
  available: boolean;
  error: string | null;
  /** The configured or preset button label (ADR 0044), or null when the
   * template is the user's own; older servers omit it (null). */
  label?: string | null;
  /** The preset whose format built the links, or null for a custom
   * repository template (ADR 0050); older servers omit it (null). */
  editor?: string | null;
  /** Which precedence rule chose the editor: "request" (the editor
   * parameter), "repository" (graphvein.yaml) or "builtin" (Graphvein's
   * default); null when an older server omits it. Other values are kept
   * as sent and shown as unknown. */
  source?: string | null;
}

export interface HandlerInfo {
  state: string;
  expression: string | null;
  node: PresentedNode | null;
  edge: EdgeRef | null;
}

export interface OperationLink {
  node: PresentedNode;
  edge: EdgeRef;
}

export interface EndpointDetail {
  api_version: number;
  repository: string;
  index: IndexRun | null;
  scope: string;
  root: string | null;
  ide: IdeStatus;
  endpoint: PresentedNode;
  transport: Transport;
  handler: HandlerInfo;
  openapi: { specs: number; operations: OperationLink[] } | null;
  callers: Caller[];
  near_misses: NearMiss[];
  near_misses_incomplete: number;
  /** T020: the endpoint family's and its calls' labels; null when the
   * server sends none. */
  precision: ScopeLabel | null;
  callers_precision: ScopeLabel | null;
  /** T022 B9 (serve --project, HTTP routes): the cross-repository links
   * into the route; null on a single-repository server. */
  project: ProjectEndpoint | null;
  /** T029 SM (HTTP routes): the validated request/response shape; null
   * when none is stored (every section not read) or it was invalid, in
   * which case shape_error says why. */
  shape: RouteShape | null;
  shape_error: string | null;
  /** T029: the editor link of each "file:line" a shape chip shows, built
   * by the server like a node's ide link; empty when the server sends
   * none (no link, editor links unavailable, or an older server). */
  shape_links: Record<string, IdeLink>;
  /** T029 (MT-28): the shape's models that are also database tables;
   * null without a shape (or from an older server). */
  model_tables: RouteModelTable[] | null;
  /** HTTP routes: the route is a registration whose path the Go linker
   * withholds (couldn't-read row CR-44, the list's flag path_withheld);
   * false when absent (or from an older server). */
  path_withheld: boolean;
}

export interface GapCall {
  node: PresentedNode;
  owner: Owner | null;
  targets: string[];
  reasons: Reason[];
  /** T022 B9 (serve --project, HTTP calls): the call's project links and
   * link pass outcomes; null on a single-repository server. */
  project: CallProject | null;
}

export interface CallsResponse {
  api_version: number;
  repository: string;
  index: IndexRun | null;
  scope: string;
  transport: Transport;
  statuses: string[];
  root: string | null;
  ide: IdeStatus;
  total: number;
  calls: GapCall[];
}

// Guards. Each throws ApiFormatError naming the path of the first field
// that does not have the documented type.

export type JSONObject = Record<string, unknown>;

function isObject(value: unknown): value is JSONObject {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

/** Shared with source.ts. */
export function object(value: unknown, path: string): JSONObject {
  if (!isObject(value)) throw new ApiFormatError(`${path} is not an object`);
  return value;
}

/** Shared with source.ts. */
export function array(value: unknown, path: string): unknown[] {
  if (!Array.isArray(value)) throw new ApiFormatError(`${path} is not a list`);
  return value;
}

/** Shared with source.ts. */
export function str(obj: JSONObject, key: string, path: string): string {
  const value = obj[key];
  if (typeof value !== "string") throw new ApiFormatError(`${path}.${key} is not a string`);
  return value;
}

/** Shared with source.ts. */
export function strOrNull(obj: JSONObject, key: string, path: string): string | null {
  const value = obj[key];
  if (value === null) return null;
  if (typeof value !== "string") throw new ApiFormatError(`${path}.${key} is not a string or null`);
  return value;
}

function num(obj: JSONObject, key: string, path: string): number {
  const value = obj[key];
  if (typeof value !== "number" || !Number.isFinite(value)) {
    throw new ApiFormatError(`${path}.${key} is not a number`);
  }
  return value;
}

function count(obj: JSONObject, key: string, path: string): number {
  const value = num(obj, key, path);
  if (!Number.isInteger(value) || value < 0) throw new ApiFormatError(`${path}.${key} is not a count`);
  return value;
}

/** Shared with source.ts. */
export function intOrNull(obj: JSONObject, key: string, path: string): number | null {
  const value = obj[key];
  if (value === null) return null;
  if (typeof value !== "number" || !Number.isInteger(value)) {
    throw new ApiFormatError(`${path}.${key} is not an integer or null`);
  }
  return value;
}

/** Shared with source.ts. */
export function bool(obj: JSONObject, key: string, path: string): boolean {
  const value = obj[key];
  if (typeof value !== "boolean") throw new ApiFormatError(`${path}.${key} is not a boolean`);
  return value;
}

function strings(value: unknown, path: string): string[] {
  const list = array(value, path);
  if (!list.every((s) => typeof s === "string")) throw new ApiFormatError(`${path} is not a list of strings`);
  return list;
}

function statusCounts(value: unknown, path: string): StatusCounts {
  const obj = object(value, path);
  const out: StatusCounts = {};
  for (const [key, n] of Object.entries(obj)) {
    if (typeof n !== "number" || !Number.isInteger(n) || n < 0) {
      throw new ApiFormatError(`${path}[${JSON.stringify(key)}] is not a count`);
    }
    out[key] = n;
  }
  return out;
}

/** Shared with source.ts. */
export function version(obj: JSONObject, path: string): number {
  const v = num(obj, "api_version", path);
  if (v !== API_VERSION) {
    throw new ApiFormatError(
      `the server speaks API version ${String(v)}; this UI reads version ${String(API_VERSION)}`,
    );
  }
  return v;
}

function transport(obj: JSONObject, key: string, path: string): Transport {
  const value = str(obj, key, path);
  if (!isTransport(value)) {
    throw new ApiFormatError(`${path}.${key} is not a known transport: ${value}`);
  }
  return value;
}

/** Shared with source.ts. */
export function parseIndex(value: unknown, path: string): IndexRun | null {
  if (value === null) return null;
  const obj = object(value, path);
  const indexer = object(obj.indexer, `${path}.indexer`);
  return {
    finished_at: str(obj, "finished_at", path),
    indexer: { name: str(indexer, "name", `${path}.indexer`), version: str(indexer, "version", `${path}.indexer`) },
    contract_version: num(obj, "contract_version", path),
  };
}

function parseCallCounts(value: unknown, path: string): CallCounts {
  const obj = object(value, path);
  const scoped = scopedStatus(obj, path);
  return {
    total: count(obj, "total", path),
    ...scoped,
    linked_without_edge: count(obj, "linked_without_edge", path),
    edges_from_unlinked: count(obj, "edges_from_unlinked", path),
    unknown_scope: count(obj, "unknown_scope", path),
  };
}

function parseRoot(value: unknown, path: string): RootCounts {
  const obj = object(value, path);
  return {
    root: str(obj, "root", path),
    manifest: strOrNull(obj, "manifest", path),
    total: count(obj, "total", path),
    ...scopedStatus(obj, path),
  };
}

/** scopedStatus reads by_status and, under serve --project,
 * project_by_status: the project's counts become by_status (what every
 * view reads) and the repository's are kept as repository_by_status. */
function scopedStatus(obj: JSONObject, path: string): { by_status: StatusCounts; repository_by_status: StatusCounts | null } {
  const own = statusCounts(obj.by_status, `${path}.by_status`);
  if (obj.project_by_status === undefined || obj.project_by_status === null) return { by_status: own, repository_by_status: null };
  return { by_status: statusCounts(obj.project_by_status, `${path}.project_by_status`), repository_by_status: own };
}

/** parsePrefixUnknown reads a route row's prefix_unknown (absent or null
 * when the row shows a full path or the route has none). */
export function parsePrefixUnknown(value: unknown, path: string): RoutePrefixUnknown | null {
  if (value === undefined || value === null) return null;
  const o = object(value, path);
  return {
    path: str(o, "path", path),
    reason: str(o, "reason", path),
    reason_text: str(o, "reason_text", path),
    hop: strOrNull(o, "hop", path),
    count: count(o, "count", path),
  };
}

function parseRow(value: unknown, t: Transport, path: string): EndpointRow {
  const obj = object(value, path);
  const base = {
    id: str(obj, "id", path),
    name: str(obj, "name", path),
    framework: strOrNull(obj, "framework", path),
    file: str(obj, "file", path),
    line: intOrNull(obj, "line", path),
    scope: obj.scope ?? null,
    caller_count: count(obj, "caller_count", path),
    project_caller_count: obj.project_caller_count === undefined || obj.project_caller_count === null ? null : count(obj, "project_caller_count", path),
    project_possible_caller_count:
      obj.project_possible_caller_count === undefined || obj.project_possible_caller_count === null ? null : count(obj, "project_possible_caller_count", path),
    flags: strings(obj.flags, `${path}.flags`),
  };
  if (t === "http") {
    return {
      ...base,
      transport: "http",
      method: strOrNull(obj, "method", path),
      path: strOrNull(obj, "path", path),
      path_count: count(obj, "path_count", path),
      prefix_unknown: parsePrefixUnknown(obj.prefix_unknown, `${path}.prefix_unknown`),
      pattern: strOrNull(obj, "pattern", path),
      documented: bool(obj, "documented", path),
      shape: parseRouteShapeSummary(obj.shape, `${path}.shape`),
    };
  }
  if (t === "server_action") {
    return {
      ...base,
      transport: "server_action",
      action: strOrNull(obj, "action", path),
      action_scope: strOrNull(obj, "action_scope", path),
      form: strOrNull(obj, "form", path),
      handler: strOrNull(obj, "handler", path),
    };
  }
  return {
    ...base,
    transport: "tauri_ipc",
    command: strOrNull(obj, "command", path),
    registry: strOrNull(obj, "registry", path),
    cfg_gated: bool(obj, "cfg_gated", path),
    test_only: bool(obj, "test_only", path),
    unreachable: bool(obj, "unreachable", path),
    plugin: strOrNull(obj, "plugin", path),
    handler: strOrNull(obj, "handler", path),
  };
}

function parseTransportSummary(value: unknown, path: string): TransportSummary {
  const obj = object(value, path);
  const t = transport(obj, "transport", path);
  let openapi: TransportSummary["openapi"] = null;
  if (obj.openapi !== null) {
    const o = object(obj.openapi, `${path}.openapi`);
    openapi = { specs: count(o, "specs", `${path}.openapi`), documented_endpoints: count(o, "documented_endpoints", `${path}.openapi`) };
  }
  return {
    transport: t,
    frameworks: strings(obj.frameworks, `${path}.frameworks`),
    endpoint_count: count(obj, "endpoint_count", path),
    never_called: count(obj, "never_called", path),
    calls: parseCallCounts(obj.calls, `${path}.calls`),
    roots: array(obj.roots, `${path}.roots`).map((r, i) => parseRoot(r, `${path}.roots[${String(i)}]`)),
    openapi,
    endpoints: array(obj.endpoints, `${path}.endpoints`).map((r, i) =>
      parseRow(r, t, `${path}.endpoints[${String(i)}]`),
    ),
    precision: parseScopeLabel(obj.precision, `${path}.precision`),
    callers_precision: parseScopeLabel(obj.callers_precision, `${path}.callers_precision`),
    project_linked_calls: obj.project_linked_calls === undefined || obj.project_linked_calls === null ? null : count(obj, "project_linked_calls", path),
    never_called_in_project: obj.never_called_in_project === undefined || obj.never_called_in_project === null ? null : count(obj, "never_called_in_project", path),
    possibly_called_in_project:
      obj.possibly_called_in_project === undefined || obj.possibly_called_in_project === null ? null : count(obj, "possibly_called_in_project", path),
  };
}

/** parseEndpoints checks a D2 body. */
export function parseEndpoints(value: unknown): EndpointsResponse {
  const obj = object(value, "endpoints");
  const apiVersion = version(obj, "endpoints");
  return {
    api_version: apiVersion,
    repository: str(obj, "repository", "endpoints"),
    index: parseIndex(obj.index, "endpoints.index"),
    scope: str(obj, "scope", "endpoints"),
    root: strOrNull(obj, "root", "endpoints"),
    transports: array(obj.transports, "endpoints.transports").map((t, i) =>
      parseTransportSummary(t, `endpoints.transports[${String(i)}]`),
    ),
  };
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

/** parseIde reads an IDE link object; null or absent is no link. Shared
 * with schema.ts (claim, reason and site links). */
export function parseIde(value: unknown, path: string): IdeLink | null {
  if (value === null || value === undefined) return null;
  const obj = object(value, path);
  return {
    link: str(obj, "link", path),
    line: num(obj, "line", path),
    column: num(obj, "column", path),
    exact: bool(obj, "exact", path),
    line_only: obj.line_only === undefined ? false : bool(obj, "line_only", path),
    caveat: obj.caveat === undefined || obj.caveat === null ? null : str(obj, "caveat", path),
  };
}

/** parseShapeLinks reads shape_links: an object of IDE links by
 * "file:line"; absent is none. */
export function parseShapeLinks(value: unknown, path: string): Record<string, IdeLink> {
  const out: Record<string, IdeLink> = {};
  if (value === undefined || value === null) return out;
  for (const [key, v] of Object.entries(object(value, path))) {
    const link = parseIde(v, `${path}[${JSON.stringify(key)}]`);
    if (link !== null) out[key] = link;
  }
  return out;
}

/** Shared with source.ts. */
export function parseNode(value: unknown, path: string): PresentedNode {
  const obj = object(value, path);
  const sourceRoot = obj.source_root;
  if (sourceRoot !== undefined && typeof sourceRoot !== "string") {
    throw new ApiFormatError(`${path}.source_root is not a string`);
  }
  return {
    identity: str(obj, "identity", path),
    kind: str(obj, "kind", path),
    level: num(obj, "level", path),
    name: str(obj, "name", path),
    qualified_name: str(obj, "qualified_name", path),
    file: str(obj, "file", path),
    span: parseSpan(obj.span, `${path}.span`),
    content_hash: str(obj, "content_hash", path),
    attributes: object(obj.attributes, `${path}.attributes`),
    ide: parseIde(obj.ide, `${path}.ide`),
    file_state: strOrNull(obj, "file_state", path),
    source_root: sourceRoot ?? null,
  };
}

function parseEdge(value: unknown, path: string): EdgeRef {
  const obj = object(value, path);
  const match = obj.match;
  if (match !== undefined && match !== null && typeof match !== "string") {
    throw new ApiFormatError(`${path}.match is not a string or null`);
  }
  return {
    kind: str(obj, "kind", path),
    provenance: str(obj, "provenance", path),
    confidence: num(obj, "confidence", path),
    match: match ?? null,
  };
}

function parseOwner(value: unknown, path: string): Owner | null {
  if (value === null) return null;
  const obj = object(value, path);
  return { id: str(obj, "id", path), kind: str(obj, "kind", path), qualified_name: str(obj, "qualified_name", path) };
}

function parseReasons(value: unknown, path: string): Reason[] {
  return array(value, path).map((r, i) => {
    const p = `${path}[${String(i)}]`;
    const obj = object(r, p);
    return { code: str(obj, "code", p), text: str(obj, "text", p), detail: strOrNull(obj, "detail", p) };
  });
}

function optionalString(obj: Record<string, unknown>, key: string, path: string): string | null {
  const v = obj[key];
  if (v !== undefined && v !== null && typeof v !== "string") {
    throw new ApiFormatError(`${path}.${key} is not a string or null`);
  }
  return v ?? null;
}

/** parseIdeStatus checks an `ide` status object; `label`, `editor` and
 * `source` are optional (older servers omit them). */
export function parseIdeStatus(value: unknown, path: string): IdeStatus {
  const obj = object(value, path);
  return {
    available: bool(obj, "available", path),
    error: strOrNull(obj, "error", path),
    label: optionalString(obj, "label", path),
    editor: optionalString(obj, "editor", path),
    source: optionalString(obj, "source", path),
  };
}

/** parseEndpointDetail checks a D3 body. */
export function parseEndpointDetail(value: unknown): EndpointDetail {
  const p = "endpoint";
  const obj = object(value, p);
  const apiVersion = version(obj, p);
  const h = object(obj.handler, `${p}.handler`);
  let openapi: EndpointDetail["openapi"] = null;
  if (obj.openapi !== null) {
    const o = object(obj.openapi, `${p}.openapi`);
    openapi = {
      specs: count(o, "specs", `${p}.openapi`),
      operations: array(o.operations, `${p}.openapi.operations`).map((op, i) => {
        const q = `${p}.openapi.operations[${String(i)}]`;
        const opObj = object(op, q);
        return { node: parseNode(opObj.node, `${q}.node`), edge: parseEdge(opObj.edge, `${q}.edge`) };
      }),
    };
  }
  return {
    api_version: apiVersion,
    repository: str(obj, "repository", p),
    index: parseIndex(obj.index, `${p}.index`),
    scope: str(obj, "scope", p),
    root: strOrNull(obj, "root", p),
    ide: parseIdeStatus(obj.ide, `${p}.ide`),
    endpoint: parseNode(obj.endpoint, `${p}.endpoint`),
    transport: transport(obj, "transport", p),
    handler: {
      state: str(h, "state", `${p}.handler`),
      expression: strOrNull(h, "expression", `${p}.handler`),
      node: h.node === null ? null : parseNode(h.node, `${p}.handler.node`),
      edge: h.edge === null ? null : parseEdge(h.edge, `${p}.handler.edge`),
    },
    openapi,
    callers: array(obj.callers, `${p}.callers`).map((c, i) => {
      const q = `${p}.callers[${String(i)}]`;
      const co = object(c, q);
      return {
        node: parseNode(co.node, `${q}.node`),
        owner: parseOwner(co.owner, `${q}.owner`),
        edges: array(co.edges, `${q}.edges`).map((e, j) => parseEdge(e, `${q}.edges[${String(j)}]`)),
        reasons: parseReasons(co.reasons, `${q}.reasons`),
        shape_differences: parseShapeDifferences(co.shape_differences, `${q}.shape_differences`),
      };
    }),
    near_misses: array(obj.near_misses, `${p}.near_misses`).map((c, i) => {
      const q = `${p}.near_misses[${String(i)}]`;
      const co = object(c, q);
      return {
        node: parseNode(co.node, `${q}.node`),
        owner: parseOwner(co.owner, `${q}.owner`),
        relation: str(co, "relation", q),
        candidate_count: count(co, "candidate_count", q),
        candidate_count_exact: co.candidate_count_exact === undefined ? false : bool(co, "candidate_count_exact", q),
        reasons: parseReasons(co.reasons, `${q}.reasons`),
      };
    }),
    near_misses_incomplete: count(obj, "near_misses_incomplete", p),
    precision: parseScopeLabel(obj.precision, `${p}.precision`),
    callers_precision: parseScopeLabel(obj.callers_precision, `${p}.callers_precision`),
    project: parseProjectEndpoint(obj.project, `${p}.project`),
    shape: parseRouteShape(obj.shape, `${p}.shape`),
    shape_error: obj.shape_error === undefined ? null : strOrNull(obj, "shape_error", p),
    shape_links: parseShapeLinks(obj.shape_links, `${p}.shape_links`),
    model_tables: parseRouteModelTables(obj.model_tables, `${p}.model_tables`),
    path_withheld: obj.path_withheld === true,
  };
}

/** parseCalls checks a D4 body. */
export function parseCalls(value: unknown): CallsResponse {
  const p = "calls";
  const obj = object(value, p);
  const apiVersion = version(obj, p);
  return {
    api_version: apiVersion,
    repository: str(obj, "repository", p),
    index: parseIndex(obj.index, `${p}.index`),
    scope: str(obj, "scope", p),
    transport: transport(obj, "transport", p),
    statuses: strings(obj.statuses, `${p}.statuses`),
    root: strOrNull(obj, "root", p),
    ide: parseIdeStatus(obj.ide, `${p}.ide`),
    total: count(obj, "total", p),
    calls: array(obj.calls, `${p}.calls`).map((c, i) => {
      const q = `${p}.calls[${String(i)}]`;
      const co = object(c, q);
      return {
        node: parseNode(co.node, `${q}.node`),
        owner: parseOwner(co.owner, `${q}.owner`),
        targets: strings(co.targets, `${q}.targets`),
        reasons: parseReasons(co.reasons, `${q}.reasons`),
        project: parseCallProject(co.project, `${q}.project`),
      };
    }),
  };
}

// Requests.

/** The error code the server answers when an index commit landed during
 * the read (503). */
export const INDEX_CHANGED = "index_changed";

export interface Signal {
  signal?: AbortSignal;
}

/** getJSON fetches path and checks it with parse. An index_changed answer
 * is retried once (D6); a second one is returned to the caller as the
 * error, so the UI says so instead of showing a half-read index. */
/** Shared with source.ts. */
export async function getJSON<T>(path: string, parse: (v: unknown) => T, options: Signal): Promise<T> {
  let body: unknown;
  try {
    body = await fetchJSON(path, options);
  } catch (err) {
    if (!(err instanceof ApiError && err.code === INDEX_CHANGED)) throw err;
    body = await fetchJSON(path, options);
  }
  try {
    return parse(body);
  } catch (err) {
    if (err instanceof ApiFormatError) throw new ApiError("format", err.message);
    throw err;
  }
}

function query(params: [string, string | null | undefined][]): string {
  const parts: string[] = [];
  for (const [key, value] of params) {
    if (value === null || value === undefined) continue;
    parts.push(`${key}=${encodeURIComponent(value)}`);
  }
  return parts.length === 0 ? "" : `?${parts.join("&")}`;
}

export interface EndpointsParams {
  scope: Scope;
  root: string | null;
}

export interface EndpointParams {
  id: string;
  scope: Scope;
  root: string | null;
  /** The editor chosen in this browser (ADR 0050); absent: none. */
  editor?: string;
}

export interface CallsParams {
  transport: Transport;
  /** Raw statuses; "" selects calls without a string status. */
  statuses: readonly string[];
  scope: Scope;
  root: string | null;
  /** The editor chosen in this browser (ADR 0050); absent: none. */
  editor?: string;
  /** "project" matches the statuses against the calls' project-scope
   * status (serve --project, the statuses the coverage counts use there;
   * T022 MR-UI); absent: the repository's. */
  statusScope?: "project";
}

/** endpointsPath, endpointPath and callsPath build the request paths. */
export function endpointsPath(p: EndpointsParams): string {
  return `/api/v0/api/endpoints${query([["scope", p.scope], ["transport", "all"], ["root", p.root]])}`;
}

export function endpointPath(p: EndpointParams): string {
  return `/api/v0/api/endpoint${query([["id", p.id], ["scope", p.scope], ["root", p.root], ["editor", p.editor]])}`;
}

/** requestableStatuses splits raw statuses into those /api/v0/api/calls
 * can be asked for and those it cannot: the server splits the decoded
 * status value on ",", so a status that contains a comma would be read as
 * two other statuses (docs/cli/serve.md, internal/server/api.go). The gap
 * sheet says that rows for an unlistable status cannot be listed instead
 * of requesting the wrong ones. */
export function requestableStatuses(statuses: readonly string[]): { requestable: string[]; unlistable: string[] } {
  const requestable: string[] = [];
  const unlistable: string[] = [];
  for (const s of statuses) {
    if (s.includes(",")) unlistable.push(s);
    else requestable.push(s);
  }
  return { requestable, unlistable };
}

export function callsPath(p: CallsParams): string {
  // The server decodes the value and splits it on ",": a status with a
  // comma cannot be requested, so callers leave such statuses out (see
  // requestableStatuses). An empty status stays an empty item.
  const unlistable = requestableStatuses(p.statuses).unlistable;
  if (unlistable.length > 0) {
    throw new Error(`cannot request raw status ${JSON.stringify(unlistable[0])}: it contains a comma`);
  }
  const status = p.statuses.map((s) => encodeURIComponent(s)).join(",");
  return `/api/v0/api/calls${query([["transport", p.transport], ["scope", p.scope], ["root", p.root], ["editor", p.editor], ["status_scope", p.statusScope]])}&status=${status}`;
}

/** ApiClient is what the API section requests; tests pass their own. */
export interface ApiClient {
  endpoints(p: EndpointsParams, options: Signal): Promise<EndpointsResponse>;
  endpoint(p: EndpointParams, options: Signal): Promise<EndpointDetail>;
  calls(p: CallsParams, options: Signal): Promise<CallsResponse>;
}

/** httpClient requests the server that served the page. */
export const httpClient: ApiClient = {
  endpoints: (p, o) => getJSON(endpointsPath(p), parseEndpoints, o),
  endpoint: (p, o) => getJSON(endpointPath(p), parseEndpointDetail, o),
  // A status callsPath refuses becomes a rejected promise, not a throw.
  calls: (p, o) => Promise.resolve().then(() => getJSON(callsPath(p), parseCalls, o)),
};
