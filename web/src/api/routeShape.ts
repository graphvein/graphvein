// Types and hand-written guards for a route's request/response shape as
// GET /api/v0/api/endpoint serves it (`shape`) and the compact form of the
// endpoint list rows (`shape`). The rules are rows SM-1..SM-29 of
// docs/contract/claims/route-shape.md; the server checks them before
// serving, so these guards only check the JSON types. Field names mirror
// the JSON.

import { ApiFormatError, array, object, str, strOrNull, type JSONObject } from "@/api/endpoints";

/** The model version this UI reads (row SM-1). */
export const ROUTE_SHAPE_VERSION = 1;

/** What a section claims as a whole (SM-2..SM-5). not_read is never
 * "none": only provably_none says there is nothing. */
export type SectionState = "not_read" | "provably_none" | "partial" | "complete";

/** How Graphvein knows an item (SM-6..SM-10). */
export type ProvenanceKind = "fact" | "read_in_handler" | "intended" | "returned" | "mapped";

export type Access = "declared" | "read_by_handler" | "read_by_dependency";

export type Presence = "required" | "optional" | "unknown";

export type SchemaType = "object" | "array" | "string" | "number" | "integer" | "boolean" | "null" | "enum" | "ref" | "unknown";

/** Why a followed call chain ended (SM-22..SM-28). depth_cap is shown as
 * "depth cap reached (N)" with the run's depth_limit, never as unknown. */
export type ChainStop =
  | "ended"
  | "depth_cap"
  | "cycle"
  | "joined"
  | "budget"
  | "resolver_error"
  | "unresolved"
  | "ambiguous"
  | "external"
  | "transformed"
  | "opaque_type";

export const SHAPE_SECTIONS = ["path", "query", "header", "cookie", "body", "responses", "security"] as const;
export type ShapeSectionName = (typeof SHAPE_SECTIONS)[number];

export interface ShapeStep {
  file: string;
  line: number;
  function: string;
}

export interface ShapeProvenance {
  kind: ProvenanceKind;
  /** The extractor's claim row (SH-F4, ...). */
  rule: string;
  file: string;
  line: number;
  /** Calls followed from the handler to the site, outermost first. */
  chain: ShapeStep[];
  confidence: number;
}

export interface ShapeSchema {
  type: SchemaType;
  fields?: ShapeField[];
  additional?: "allowed" | "forbidden" | "unknown";
  items?: ShapeSchema;
  /** Literals: string, number, boolean or null. */
  enum?: (string | number | boolean | null)[];
  ref?: string;
  nullable?: boolean;
  /** Why the schema is unknown. */
  reason?: string;
}

export interface ShapeField {
  name: string;
  presence: Presence;
  schema: ShapeSchema;
}

interface SectionBase {
  state: SectionState;
  reason: string | null;
  evidence: ShapeProvenance | null;
}

export interface ShapeParam {
  name: string;
  access: Access;
  presence: Presence;
  /** null: the type was not read. */
  schema: ShapeSchema | null;
  provenance: ShapeProvenance;
}

export interface ShapeContent {
  /** null: not read. */
  content_type: string | null;
  schema: ShapeSchema | null;
  provenance: ShapeProvenance;
}

export interface ShapeResponse {
  /** "200", "4XX" or "default". */
  status: string;
  content_type: string | null;
  schema: ShapeSchema | null;
  provenance: ShapeProvenance;
}

export interface ShapeRequirement {
  /** Applied together; an empty list is the proven anonymous alternative. */
  schemes: { name: string; scopes: string[] }[];
  provenance: ShapeProvenance;
}

export interface ShapeScheme {
  name: string;
  type: "http" | "apiKey" | "oauth2" | "openIdConnect" | "unknown";
  scheme: string | null;
  bearer_format: string | null;
  in: string | null;
  param_name: string | null;
  token_url: string | null;
  openid_connect_url: string | null;
  provenance: ShapeProvenance;
}

export interface ShapeChain {
  section: ShapeSectionName;
  start: ShapeStep;
  steps: ShapeStep[];
  depth: number;
  stop: ChainStop;
  /** Where the chain stopped; null for ended and resolver_error. */
  at: ShapeStep | null;
  detail: string | null;
}

export interface RouteShape {
  version: number;
  path: SectionBase & { items: ShapeParam[] };
  query: SectionBase & { items: ShapeParam[] };
  header: SectionBase & { items: ShapeParam[] };
  cookie: SectionBase & { items: ShapeParam[] };
  body: SectionBase & { items: ShapeContent[] };
  responses: SectionBase & { items: ShapeResponse[] };
  security: SectionBase & { items: ShapeRequirement[] };
  security_schemes: ShapeScheme[];
  models: { name: string; schema: ShapeSchema; provenance: ShapeProvenance }[];
  /** depth_limit is the run's configured call-following depth
   * (graphvein.yaml index.shape_depth), for "raise it" hints. */
  follow: { depth_limit: number; chains: ShapeChain[] };
}

/** The compact shape of an endpoint list row. */
export interface RouteShapeSummary {
  sections: Record<ShapeSectionName, { state: SectionState; items: number }>;
  depth_cap: number;
  depth_limit: number;
}

export function oneOf<T extends string>(value: unknown, allowed: readonly T[], path: string): T {
  if (typeof value !== "string" || !(allowed as readonly string[]).includes(value)) {
    throw new ApiFormatError(`${path} is not one of ${allowed.join(", ")}`);
  }
  return value as T;
}

export function int(obj: JSONObject, key: string, path: string): number {
  const value = obj[key];
  if (typeof value !== "number" || !Number.isInteger(value)) throw new ApiFormatError(`${path}.${key} is not an integer`);
  return value;
}

function optString(obj: JSONObject, key: string, path: string): string | null {
  return obj[key] === undefined ? null : strOrNull(obj, key, path);
}

export const STATES: readonly SectionState[] = ["not_read", "provably_none", "partial", "complete"];
export const KINDS: readonly ProvenanceKind[] = ["fact", "read_in_handler", "intended", "returned", "mapped"];
const ACCESS: readonly Access[] = ["declared", "read_by_handler", "read_by_dependency"];
const PRESENCE: readonly Presence[] = ["required", "optional", "unknown"];
const TYPES: readonly SchemaType[] = ["object", "array", "string", "number", "integer", "boolean", "null", "enum", "ref", "unknown"];
const STOPS: readonly ChainStop[] = ["ended", "depth_cap", "cycle", "joined", "budget", "resolver_error", "unresolved", "ambiguous", "external", "transformed", "opaque_type"];
const SCHEME_TYPES: readonly ShapeScheme["type"][] = ["http", "apiKey", "oauth2", "openIdConnect", "unknown"];
const ADDITIONAL: readonly NonNullable<ShapeSchema["additional"]>[] = ["allowed", "forbidden", "unknown"];

export function parseStep(value: unknown, path: string): ShapeStep {
  const o = object(value, path);
  return { file: str(o, "file", path), line: int(o, "line", path), function: str(o, "function", path) };
}

/** parseProvenance reads one provenance (shared with the OpenAPI
 * document's x-graphvein, which writes the stored form). */
export function parseProvenance(value: unknown, path: string): ShapeProvenance {
  const o = object(value, path);
  const confidence = o.confidence;
  if (typeof confidence !== "number") throw new ApiFormatError(`${path}.confidence is not a number`);
  return {
    kind: oneOf(o.kind, KINDS, `${path}.kind`),
    rule: str(o, "rule", path),
    file: str(o, "file", path),
    line: int(o, "line", path),
    chain: array(o.chain, `${path}.chain`).map((s, i) => parseStep(s, `${path}.chain[${String(i)}]`)),
    confidence,
  };
}

/** parseSchema reads one stored schema node (SM-20). */
export function parseSchema(value: unknown, path: string): ShapeSchema {
  const o = object(value, path);
  const out: ShapeSchema = { type: oneOf(o.type, TYPES, `${path}.type`) };
  if (o.fields !== undefined) {
    out.fields = array(o.fields, `${path}.fields`).map((f, i) => {
      const q = `${path}.fields[${String(i)}]`;
      const fo = object(f, q);
      return { name: str(fo, "name", q), presence: oneOf(fo.presence, PRESENCE, `${q}.presence`), schema: parseSchema(fo.schema, `${q}.schema`) };
    });
  }
  if (o.additional !== undefined) out.additional = oneOf(o.additional, ADDITIONAL, `${path}.additional`);
  if (o.items !== undefined) out.items = parseSchema(o.items, `${path}.items`);
  if (o.enum !== undefined) {
    out.enum = array(o.enum, `${path}.enum`).map((e, i) => {
      if (e === null || typeof e === "string" || typeof e === "number" || typeof e === "boolean") return e;
      throw new ApiFormatError(`${path}.enum[${String(i)}] is not a literal`);
    });
  }
  if (o.ref !== undefined) out.ref = str(o, "ref", path);
  if (o.nullable !== undefined) {
    if (typeof o.nullable !== "boolean") throw new ApiFormatError(`${path}.nullable is not a boolean`);
    out.nullable = o.nullable;
  }
  if (o.reason !== undefined) out.reason = str(o, "reason", path);
  return out;
}

function schemaOrNull(value: unknown, path: string): ShapeSchema | null {
  return value === null ? null : parseSchema(value, path);
}

function parseSection<T>(value: unknown, path: string, item: (v: unknown, p: string) => T): SectionBase & { items: T[] } {
  const o = object(value, path);
  return {
    state: oneOf(o.state, STATES, `${path}.state`),
    reason: optString(o, "reason", path),
    evidence: o.evidence === undefined || o.evidence === null ? null : parseProvenance(o.evidence, `${path}.evidence`),
    items: array(o.items, `${path}.items`).map((v, i) => item(v, `${path}.items[${String(i)}]`)),
  };
}

/** parseParam reads one stored parameter item. */
export function parseParam(value: unknown, path: string): ShapeParam {
  const o = object(value, path);
  return {
    name: str(o, "name", path),
    access: oneOf(o.access, ACCESS, `${path}.access`),
    presence: oneOf(o.presence, PRESENCE, `${path}.presence`),
    schema: schemaOrNull(o.schema, `${path}.schema`),
    provenance: parseProvenance(o.provenance, `${path}.provenance`),
  };
}

/** parseContent reads one stored request body item. */
export function parseContent(v: unknown, p: string): ShapeContent {
  const c = object(v, p);
  return { content_type: strOrNull(c, "content_type", p), schema: schemaOrNull(c.schema, `${p}.schema`), provenance: parseProvenance(c.provenance, `${p}.provenance`) };
}

/** parseResponse reads one stored response item. */
export function parseResponse(v: unknown, p: string): ShapeResponse {
  const r = object(v, p);
  return {
    status: str(r, "status", p),
    content_type: strOrNull(r, "content_type", p),
    schema: schemaOrNull(r.schema, `${p}.schema`),
    provenance: parseProvenance(r.provenance, `${p}.provenance`),
  };
}

/** parseRequirement reads one stored security requirement. */
export function parseRequirement(v: unknown, p: string): ShapeRequirement {
  const r = object(v, p);
  return {
    schemes: array(r.schemes, `${p}.schemes`).map((s, i) => {
      const q = `${p}.schemes[${String(i)}]`;
      const so = object(s, q);
      const scopes = array(so.scopes, `${q}.scopes`);
      if (!scopes.every((x) => typeof x === "string")) throw new ApiFormatError(`${q}.scopes is not a list of strings`);
      return { name: str(so, "name", q), scopes };
    }),
    provenance: parseProvenance(r.provenance, `${p}.provenance`),
  };
}

/** parseChain reads one followed call chain record (SM-29). */
export function parseChain(v: unknown, p: string): ShapeChain {
  const c = object(v, p);
  return {
    section: oneOf(c.section, SHAPE_SECTIONS, `${p}.section`),
    start: parseStep(c.start, `${p}.start`),
    steps: array(c.steps, `${p}.steps`).map((s, j) => parseStep(s, `${p}.steps[${String(j)}]`)),
    depth: int(c, "depth", p),
    stop: oneOf(c.stop, STOPS, `${p}.stop`),
    at: c.at === null || c.at === undefined ? null : parseStep(c.at, `${p}.at`),
    detail: optString(c, "detail", p),
  };
}

/**
 * parseRouteShape reads the endpoint detail's shape. Absent or null is no
 * stored shape (every section not read); another model version is also
 * read as no shape, so a newer server can never make this UI claim more
 * than it understands.
 */
export function parseRouteShape(value: unknown, path: string): RouteShape | null {
  if (value === undefined || value === null) return null;
  const o = object(value, path);
  if (o.version !== ROUTE_SHAPE_VERSION) return null;
  const follow = object(o.follow, `${path}.follow`);
  return {
    version: ROUTE_SHAPE_VERSION,
    path: parseSection(o.path, `${path}.path`, parseParam),
    query: parseSection(o.query, `${path}.query`, parseParam),
    header: parseSection(o.header, `${path}.header`, parseParam),
    cookie: parseSection(o.cookie, `${path}.cookie`, parseParam),
    body: parseSection(o.body, `${path}.body`, parseContent),
    responses: parseSection(o.responses, `${path}.responses`, parseResponse),
    security: parseSection(o.security, `${path}.security`, parseRequirement),
    security_schemes: array(o.security_schemes, `${path}.security_schemes`).map((v, i) => {
      const p = `${path}.security_schemes[${String(i)}]`;
      const s = object(v, p);
      return {
        name: str(s, "name", p),
        type: oneOf(s.type, SCHEME_TYPES, `${p}.type`),
        scheme: strOrNull(s, "scheme", p),
        bearer_format: strOrNull(s, "bearer_format", p),
        in: strOrNull(s, "in", p),
        param_name: strOrNull(s, "param_name", p),
        token_url: strOrNull(s, "token_url", p),
        openid_connect_url: strOrNull(s, "openid_connect_url", p),
        provenance: parseProvenance(s.provenance, `${p}.provenance`),
      };
    }),
    models: array(o.models, `${path}.models`).map((v, i) => {
      const p = `${path}.models[${String(i)}]`;
      const m = object(v, p);
      return { name: str(m, "name", p), schema: parseSchema(m.schema, `${p}.schema`), provenance: parseProvenance(m.provenance, `${p}.provenance`) };
    }),
    follow: {
      depth_limit: int(follow, "depth_limit", `${path}.follow`),
      chains: array(follow.chains, `${path}.follow.chains`).map((v, i) => parseChain(v, `${path}.follow.chains[${String(i)}]`)),
    },
  };
}

/** parseRouteShapeSummary reads an endpoint list row's shape (absent or
 * null: no stored shape). */
export function parseRouteShapeSummary(value: unknown, path: string): RouteShapeSummary | null {
  if (value === undefined || value === null) return null;
  const o = object(value, path);
  const sections = object(o.sections, `${path}.sections`);
  const out = {} as RouteShapeSummary["sections"];
  for (const name of SHAPE_SECTIONS) {
    const q = `${path}.sections.${name}`;
    const s = object(sections[name], q);
    out[name] = { state: oneOf(s.state, STATES, `${q}.state`), items: int(s, "items", q) };
  }
  return { sections: out, depth_cap: int(o, "depth_cap", path), depth_limit: int(o, "depth_limit", path) };
}

/** depthCapText is the wording for a depth_cap stop (SM-23): the limit
 * and where to raise it, never "unknown". */
export function depthCapText(limit: number): string {
  return `depth cap reached (${String(limit)}); raise index.shape_depth in graphvein.yaml (at most 10)`;
}
