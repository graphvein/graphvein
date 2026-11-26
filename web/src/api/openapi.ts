// Types, guards and requests for the generated OpenAPI document as
// GET /api/v0/openapi serves it (T029, ADR 0081; claim contract
// docs/contract/claims/openapi-generation.md, rows OG-1..OG-93). The
// Explorer reads the document and nothing else for its operations, so it
// shows exactly what the document claims: standard fields are facts (with
// their provenance in x-graphvein), everything else stays in x-graphvein
// and is shown as such, and every field the generator did not read is
// named in x-graphvein.not_read.
//
// The guards check the fields the Explorer reads; the server builds the
// document from one function (the same bytes as `graphvein openapi`), so a
// field missing here is a format error, never read as "none".

import { ApiError, fetchJSON, withRepository } from "@/api/client";
import { ApiFormatError, array, getJSON, httpClient, object, str, strOrNull, type IdeLink, type IdeStatus, type JSONObject } from "@/api/endpoints";
import {
  int,
  oneOf,
  parseChain,
  parseContent,
  parseParam,
  parseProvenance,
  parseRequirement,
  parseResponse,
  parseSchema,
  STATES,
  type SectionState,
  type ShapeChain,
  type ShapeContent,
  type ShapeParam,
  type ShapeProvenance,
  type ShapeRequirement,
  type ShapeResponse,
  type ShapeSchema,
} from "@/api/routeShape";
import { hasShapeDifferences, type LinkedCallDifferences } from "@/api/shapeDifferences";

/** The eight methods OpenAPI 3.1 names, in its order (OG-51). */
export const OPENAPI_METHODS = ["get", "put", "post", "delete", "options", "head", "patch", "trace"] as const;
export type OpenApiMethod = (typeof OPENAPI_METHODS)[number];

/** A raw OpenAPI Schema Object as the document writes it (OG-83..OG-86). */
export type OpenApiSchema = JSONObject;

/** Where an operation's path comes from (OG-43, OG-92): literal,
 * constant, settings_default or set_by_you; another value is kept raw. */
export type Resolution = string;

export interface Assumption {
  id: string;
  from: string;
  key: string | null;
  value: string | null;
  text: string | null;
}

export interface DocParameter {
  name: string;
  in: string;
  /** null: the document writes no required (never a claim of optional). */
  required: boolean | null;
  schema: OpenApiSchema;
  /** x-graphvein.type "not_read": the schema states no constraint. */
  typeNotRead: boolean;
  /** null for a path parameter that comes from the template only (OG-26). */
  provenance: ShapeProvenance | null;
  /** x-graphvein.matches (Starlette path convertor, OG-18). */
  matches: string | null;
}

export interface DocMedia {
  contentType: string;
  /** null: x-graphvein.schema "not_read". */
  schema: OpenApiSchema | null;
  provenance: ShapeProvenance | null;
}

export interface DocResponse {
  status: string;
  content: DocMedia[];
  /** x-graphvein.body "none": a proven empty body (OG-81). */
  emptyBody: boolean;
  provenance: ShapeProvenance | null;
}

/** One section's state as the document stores it (OG-71). */
export interface DocSection {
  state: SectionState;
  reason: string | null;
  evidence: ShapeProvenance | null;
  /** How many of its items are written as standard fields. */
  standard: number;
}

/** One shape item kept out of the standard fields (OG-73..OG-87), with the
 * row that keeps it out and why. */
export type NotStandardItem =
  | { section: "path" | "query" | "header" | "cookie"; row: string; why: string; item: ShapeParam }
  | { section: "body"; row: string; why: string; item: ShapeContent }
  | { section: "responses"; row: string; why: string; item: ShapeResponse }
  | { section: "security"; row: string; why: string; item: ShapeRequirement };

export interface DocShape {
  version: number;
  depthLimit: number;
  sections: Partial<Record<string, DocSection>>;
  notStandard: NotStandardItem[];
  models: { name: string; schema: ShapeSchema; provenance: ShapeProvenance }[];
  depthCap: ShapeChain[];
  stops: Record<string, number>;
}

export interface Shadow {
  route: string;
  method: string;
  key: string;
  order: string;
}

export interface DocumentedBy {
  operation: string;
  file: string;
  confidence: number;
}

/** One operation of the document, flattened with its path key and method. */
export interface DocOperation {
  key: string;
  method: OpenApiMethod;
  route: string;
  file: string;
  line: number;
  framework: string;
  handler: string | null;
  handlerExpression: string | null;
  rule: string;
  template: string;
  resolution: Resolution;
  conditional: boolean;
  assumptions: Assumption[];
  equivalentRegistrations: string[];
  documentedBy: DocumentedBy[];
  /** The route's auth attributes as stored (OG-42); an empty list is
   * "none read", never "unauthenticated". */
  auth: Record<string, unknown>;
  notRead: string[];
  shape: DocShape | null;
  shapeError: string | null;
  mayBeShadowed: Shadow[];
  /** "not_checked" for an operation placed by a mount value below a Go hop
   * (OG-96): sibling patterns above the hop may answer some paths. */
  shadowing: string | null;
  parameters: DocParameter[];
  /** null: no requestBody written. */
  requestBody: DocMedia[] | null;
  /** Whether a request body must be sent: the standard `required` when
   * written, "not_read" when x-graphvein says its presence was not read
   * (OG-79), null when the document says neither (no body written). */
  requestBodyRequired: boolean | "not_read" | null;
  /** null: no responses written. */
  responses: DocResponse[] | null;
  /** null: no security written (OG-30, OG-87). An empty requirement is the
   * proven anonymous alternative. */
  security: { name: string; scopes: string[] }[][] | null;
}

export interface RouteRef {
  route: string;
  file: string;
  line: number;
  framework: string;
}

export interface Unresolved {
  route: string;
  method: string;
  pattern: string;
  file: string;
  line: number;
  reason: string;
  templatesAfterBase: string[];
  unknown: { part: string; text: string; diagnostic: string | null; expression: string | null; project: string | null };
  keys: string[];
}

export interface Unrepresentable {
  route: string;
  method: string;
  key: string;
  reason: string;
}

export interface Conflict {
  reason: string;
  keys: string[];
  method: string | null;
  routes: string[];
}

export interface UserValue {
  key: string;
  value: string;
  status: string;
  operations: number;
  why: string | null;
  registration: string | null;
  warning: string | null;
}

export interface ImportedSpec {
  spec: string;
  file: string;
  title: string | null;
  specVersion: string | null;
  scope: string;
  operations: number;
  statuses: Record<string, number>;
}

export interface DocCounts {
  routes: number;
  production_routes: number;
  operations: number;
  path_keys: number;
  any_method: number;
  unresolved: number;
  unrepresentable: number;
  conflicts: number;
  set_by_you: number;
  conditional: number;
  settings_default: number;
  with_shape: number;
  may_be_shadowed: number;
}

export interface DocServer {
  url: string;
  description: string;
  environment: string;
  label: string;
}

/** The parsed document: what the Explorer shows. */
export interface OpenApiDocument {
  openapi: string;
  title: string;
  servers: DocServer[];
  operations: DocOperation[];
  /** ANY-method routes, listed per path key (OG-17): never expanded. */
  anyMethod: (RouteRef & { key: string })[];
  securitySchemes: Record<string, JSONObject>;
  schemas: Record<string, JSONObject>;
  repository: string;
  index: { run: string; finishedAt: string; extractor: string; commit: string | null } | null;
  counts: DocCounts;
  unresolved: Unresolved[];
  unrepresentable: Unrepresentable[];
  conflicts: Conflict[];
  excludedTest: { routes: number; pathEntries: number };
  importedSpecs: ImportedSpec[];
  userValues: UserValue[];
  shapeErrors: number;
}

function optStr(o: JSONObject, key: string, path: string): string | null {
  return o[key] === undefined ? null : strOrNull(o, key, path);
}

function bool(o: JSONObject, key: string, path: string): boolean {
  const v = o[key];
  if (typeof v !== "boolean") throw new ApiFormatError(`${path}.${key} is not a boolean`);
  return v;
}

function strings(value: unknown, path: string): string[] {
  const list = value === undefined ? [] : array(value, path);
  if (!list.every((s) => typeof s === "string")) throw new ApiFormatError(`${path} is not a list of strings`);
  return list;
}

function num(o: JSONObject, key: string, path: string): number {
  const v = o[key];
  if (typeof v !== "number" || !Number.isFinite(v)) throw new ApiFormatError(`${path}.${key} is not a number`);
  return v;
}

/** ext is a field's x-graphvein object (an empty object when absent). */
function ext(o: JSONObject, path: string): JSONObject {
  return o["x-graphvein"] === undefined ? {} : object(o["x-graphvein"], `${path}.x-graphvein`);
}

function provenanceOf(x: JSONObject, path: string): ShapeProvenance | null {
  return x.provenance === undefined ? null : parseProvenance(x.provenance, `${path}.x-graphvein.provenance`);
}

function parseMedia(content: unknown, path: string): DocMedia[] {
  const c = object(content, path);
  return Object.entries(c).map(([type, v]) => {
    const p = `${path}[${type}]`;
    const m = object(v, p);
    const x = ext(m, p);
    return { contentType: type, schema: m.schema === undefined ? null : object(m.schema, `${p}.schema`), provenance: provenanceOf(x, p) };
  });
}

const PARAM_SECTIONS = ["path", "query", "header", "cookie"] as const;

function parseNotStandard(value: unknown, path: string): NotStandardItem {
  const o = object(value, path);
  const section = str(o, "section", path);
  const row = str(o, "row", path);
  const why = str(o, "why", path);
  const ip = `${path}.item`;
  if ((PARAM_SECTIONS as readonly string[]).includes(section)) {
    return { section: section as (typeof PARAM_SECTIONS)[number], row, why, item: parseParam(o.item, ip) };
  }
  if (section === "body") return { section, row, why, item: parseContent(o.item, ip) };
  if (section === "responses") return { section, row, why, item: parseResponse(o.item, ip) };
  if (section === "security") return { section, row, why, item: parseRequirement(o.item, ip) };
  throw new ApiFormatError(`${path}.section is not a shape section`);
}

function parseShape(value: unknown, path: string): DocShape {
  const o = object(value, path);
  const sections: Record<string, DocSection> = {};
  for (const [name, v] of Object.entries(object(o.sections, `${path}.sections`))) {
    const p = `${path}.sections.${name}`;
    const s = object(v, p);
    sections[name] = {
      state: oneOf(s.state, STATES, `${p}.state`),
      reason: optStr(s, "reason", p),
      evidence: s.evidence === undefined || s.evidence === null ? null : parseProvenance(s.evidence, `${p}.evidence`),
      standard: int(s, "standard", p),
    };
  }
  const stops: Record<string, number> = {};
  for (const [k, v] of Object.entries(o.stops === undefined ? {} : object(o.stops, `${path}.stops`))) {
    if (typeof v !== "number") throw new ApiFormatError(`${path}.stops.${k} is not a number`);
    stops[k] = v;
  }
  return {
    version: int(o, "version", path),
    depthLimit: int(o, "depth_limit", path),
    sections,
    notStandard: (o.not_standard === undefined ? [] : array(o.not_standard, `${path}.not_standard`)).map((v, i) => parseNotStandard(v, `${path}.not_standard[${String(i)}]`)),
    models: (o.models === undefined ? [] : array(o.models, `${path}.models`)).map((v, i) => {
      const p = `${path}.models[${String(i)}]`;
      const m = object(v, p);
      return { name: str(m, "name", p), schema: parseSchema(m.schema, `${p}.schema`), provenance: parseProvenance(m.provenance, `${p}.provenance`) };
    }),
    depthCap: (o.depth_cap === undefined || o.depth_cap === null ? [] : array(o.depth_cap, `${path}.depth_cap`)).map((v, i) => parseChain(v, `${path}.depth_cap[${String(i)}]`)),
    stops,
  };
}

function parseAssumption(value: unknown, path: string): Assumption {
  const o = object(value, path);
  return { id: str(o, "id", path), from: str(o, "from", path), key: optStr(o, "key", path), value: optStr(o, "value", path), text: optStr(o, "text", path) };
}

function parseOperation(key: string, method: OpenApiMethod, value: unknown, path: string): DocOperation {
  const o = object(value, path);
  const x = object(o["x-graphvein"], `${path}.x-graphvein`);
  const xp = `${path}.x-graphvein`;
  const parameters = (o.parameters === undefined ? [] : array(o.parameters, `${path}.parameters`)).map((v, i) => {
    const p = `${path}.parameters[${String(i)}]`;
    const po = object(v, p);
    const px = ext(po, p);
    const required = po.required;
    if (required !== undefined && typeof required !== "boolean") throw new ApiFormatError(`${p}.required is not a boolean`);
    return {
      name: str(po, "name", p),
      in: str(po, "in", p),
      required: required ?? null,
      schema: po.schema === undefined ? {} : object(po.schema, `${p}.schema`),
      typeNotRead: px.type === "not_read",
      provenance: provenanceOf(px, p),
      matches: typeof px.matches === "string" ? px.matches : null,
    };
  });
  let requestBody: DocMedia[] | null = null;
  let requestBodyRequired: DocOperation["requestBodyRequired"] = null;
  if (o.requestBody !== undefined) {
    const rb = object(o.requestBody, `${path}.requestBody`);
    requestBody = parseMedia(rb.content, `${path}.requestBody.content`);
    if (typeof rb.required === "boolean") requestBodyRequired = rb.required;
    else if (rb.required !== undefined) throw new ApiFormatError(`${path}.requestBody.required is not a boolean`);
    else if (rb["x-graphvein"] !== undefined && ext(rb, `${path}.requestBody`).required === "not_read") requestBodyRequired = "not_read";
  }
  let responses: DocResponse[] | null = null;
  if (o.responses !== undefined) {
    responses = Object.entries(object(o.responses, `${path}.responses`)).map(([status, v]) => {
      const p = `${path}.responses[${status}]`;
      const r = object(v, p);
      const rx = ext(r, p);
      return { status, content: r.content === undefined ? [] : parseMedia(r.content, `${p}.content`), emptyBody: rx.body === "none", provenance: provenanceOf(rx, p) };
    });
  }
  let security: DocOperation["security"] = null;
  if (o.security !== undefined) {
    security = array(o.security, `${path}.security`).map((req, i) => {
      const q = `${path}.security[${String(i)}]`;
      return Object.entries(object(req, q)).map(([name, scopes]) => ({ name, scopes: strings(scopes, `${q}.${name}`) }));
    });
  }
  return {
    key,
    method,
    route: str(x, "route", xp),
    file: str(x, "file", xp),
    line: int(x, "line", xp),
    framework: str(x, "framework", xp),
    handler: optStr(x, "handler", xp),
    handlerExpression: optStr(x, "handler_expression", xp),
    rule: str(x, "rule", xp),
    template: str(x, "template", xp),
    resolution: str(x, "resolution", xp),
    conditional: bool(x, "conditional", xp),
    assumptions: (x.assumptions === undefined ? [] : array(x.assumptions, `${xp}.assumptions`)).map((v, i) => parseAssumption(v, `${xp}.assumptions[${String(i)}]`)),
    equivalentRegistrations: strings(x.equivalent_registrations, `${xp}.equivalent_registrations`),
    documentedBy: (x.documented_by === undefined ? [] : array(x.documented_by, `${xp}.documented_by`)).map((v, i) => {
      const p = `${xp}.documented_by[${String(i)}]`;
      const d = object(v, p);
      return { operation: str(d, "operation", p), file: str(d, "file", p), confidence: num(d, "confidence", p) };
    }),
    auth: x.auth === undefined ? {} : object(x.auth, `${xp}.auth`),
    notRead: strings(x.not_read, `${xp}.not_read`),
    shape: x.shape === undefined || x.shape === null ? null : parseShape(x.shape, `${xp}.shape`),
    shapeError: optStr(x, "shape_error", xp),
    shadowing: optStr(x, "shadowing", xp),
    mayBeShadowed: (x.may_be_shadowed === undefined ? [] : array(x.may_be_shadowed, `${xp}.may_be_shadowed`)).map((v, i) => {
      const p = `${xp}.may_be_shadowed[${String(i)}]`;
      const s = object(v, p);
      return { route: str(s, "route", p), method: str(s, "method", p), key: str(s, "key", p), order: str(s, "order", p) };
    }),
    parameters,
    requestBody,
    requestBodyRequired,
    responses,
    security,
  };
}

const COUNT_KEYS: readonly (keyof DocCounts)[] = [
  "routes",
  "production_routes",
  "operations",
  "path_keys",
  "any_method",
  "unresolved",
  "unrepresentable",
  "conflicts",
  "set_by_you",
  "conditional",
  "settings_default",
  "with_shape",
  "may_be_shadowed",
];

/** parseOpenApi reads the document the Explorer shows. */
export function parseOpenApi(value: unknown): OpenApiDocument {
  const d = object(value, "document");
  const info = object(d.info, "info");
  const x = object(d["x-graphvein"], "x-graphvein");
  const counts = object(x.counts, "x-graphvein.counts");
  const outCounts = {} as DocCounts;
  for (const k of COUNT_KEYS) outCounts[k] = counts[k] === undefined ? 0 : int(counts, k, "x-graphvein.counts");
  const operations: DocOperation[] = [];
  const anyMethod: OpenApiDocument["anyMethod"] = [];
  for (const [key, v] of Object.entries(object(d.paths, "paths"))) {
    const p = `paths[${key}]`;
    const item = object(v, p);
    for (const m of OPENAPI_METHODS) {
      if (item[m] !== undefined) operations.push(parseOperation(key, m, item[m], `${p}.${m}`));
    }
    const ix = ext(item, p);
    for (const [i, r] of (ix.any_method === undefined ? [] : array(ix.any_method, `${p}.x-graphvein.any_method`)).entries()) {
      const q = `${p}.x-graphvein.any_method[${String(i)}]`;
      const ro = object(r, q);
      anyMethod.push({ key, route: str(ro, "route", q), file: str(ro, "file", q), line: int(ro, "line", q), framework: str(ro, "framework", q) });
    }
  }
  const components = d.components === undefined ? {} : object(d.components, "components");
  const records = (v: unknown, p: string): Record<string, JSONObject> => {
    const out: Record<string, JSONObject> = {};
    if (v === undefined) return out;
    for (const [k, s] of Object.entries(object(v, p))) out[k] = object(s, `${p}.${k}`);
    return out;
  };
  const index = x.index === null || x.index === undefined ? null : object(x.index, "x-graphvein.index");
  const excluded = object(x.excluded_test, "x-graphvein.excluded_test");
  return {
    openapi: str(d, "openapi", "document"),
    title: str(info, "title", "info"),
    servers: (d.servers === undefined ? [] : array(d.servers, "servers")).map((v, i) => {
      const p = `servers[${String(i)}]`;
      const s = object(v, p);
      const sx = ext(s, p);
      return { url: str(s, "url", p), description: str(s, "description", p), environment: typeof sx.environment === "string" ? sx.environment : "", label: typeof sx.label === "string" ? sx.label : "" };
    }),
    operations,
    anyMethod,
    securitySchemes: records(components.securitySchemes, "components.securitySchemes"),
    schemas: records(components.schemas, "components.schemas"),
    repository: str(x, "repository", "x-graphvein"),
    index:
      index === null
        ? null
        : { run: str(index, "run", "x-graphvein.index"), finishedAt: str(index, "finished_at", "x-graphvein.index"), extractor: str(index, "extractor", "x-graphvein.index"), commit: optStr(index, "commit", "x-graphvein.index") },
    counts: outCounts,
    unresolved: array(x.unresolved, "x-graphvein.unresolved").map((v, i) => {
      const p = `x-graphvein.unresolved[${String(i)}]`;
      const u = object(v, p);
      const un = object(u.unknown, `${p}.unknown`);
      return {
        route: str(u, "route", p),
        method: str(u, "method", p),
        pattern: str(u, "pattern", p),
        file: str(u, "file", p),
        line: int(u, "line", p),
        reason: str(u, "reason", p),
        templatesAfterBase: strings(u.templates_after_base, `${p}.templates_after_base`),
        unknown: { part: str(un, "part", `${p}.unknown`), text: str(un, "text", `${p}.unknown`), diagnostic: optStr(un, "diagnostic", p), expression: optStr(un, "expression", p), project: optStr(un, "project", p) },
        keys: strings(u.keys, `${p}.keys`),
      };
    }),
    unrepresentable: array(x.unrepresentable, "x-graphvein.unrepresentable").map((v, i) => {
      const p = `x-graphvein.unrepresentable[${String(i)}]`;
      const u = object(v, p);
      return { route: str(u, "route", p), method: str(u, "method", p), key: str(u, "key", p), reason: str(u, "reason", p) };
    }),
    conflicts: array(x.conflicts, "x-graphvein.conflicts").map((v, i) => {
      const p = `x-graphvein.conflicts[${String(i)}]`;
      const c = object(v, p);
      return { reason: str(c, "reason", p), keys: strings(c.keys, `${p}.keys`), method: optStr(c, "method", p), routes: strings(c.routes, `${p}.routes`) };
    }),
    excludedTest: { routes: int(excluded, "routes", "x-graphvein.excluded_test"), pathEntries: int(excluded, "path_entries", "x-graphvein.excluded_test") },
    importedSpecs: array(x.imported_specs, "x-graphvein.imported_specs").map((v, i) => {
      const p = `x-graphvein.imported_specs[${String(i)}]`;
      const s = object(v, p);
      const st: Record<string, number> = {};
      for (const [k, n] of Object.entries(s.statuses === undefined ? {} : object(s.statuses, `${p}.statuses`))) st[k] = typeof n === "number" ? n : 0;
      return { spec: str(s, "spec", p), file: str(s, "file", p), title: optStr(s, "title", p), specVersion: optStr(s, "spec_version", p), scope: str(s, "scope", p), operations: int(s, "operations", p), statuses: st };
    }),
    userValues: array(x.user_values, "x-graphvein.user_values").map((v, i) => parseUserValue(v, `x-graphvein.user_values[${String(i)}]`)),
    shapeErrors: int(x, "shape_errors", "x-graphvein"),
  };
}

function parseUserValue(v: unknown, p: string): UserValue {
  const u = object(v, p);
  return {
    key: str(u, "key", p),
    value: str(u, "value", p),
    status: str(u, "status", p),
    operations: int(u, "operations", p),
    why: optStr(u, "why", p),
    registration: optStr(u, "registration", p),
    warning: optStr(u, "warning", p),
  };
}

/** A project server without repository= lists its members (one document
 * per member, OG-61). */
export interface MemberList {
  project: string;
  members: { repository: string; document: string; checkout: boolean }[];
}

export type OpenApiAnswer = { kind: "document"; document: OpenApiDocument } | { kind: "members"; list: MemberList };

/** parseOpenApiAnswer reads either a document or a project member list. */
export function parseOpenApiAnswer(value: unknown): OpenApiAnswer {
  const o = object(value, "answer");
  if (o.openapi === undefined && Array.isArray(o.members)) {
    return {
      kind: "members",
      list: {
        project: str(o, "project", "answer"),
        members: o.members.map((m, i) => {
          const p = `members[${String(i)}]`;
          const mo = object(m, p);
          return { repository: str(mo, "repository", p), document: str(mo, "document", p), checkout: bool(mo, "checkout", p) };
        }),
      },
    };
  }
  return { kind: "document", document: parseOpenApi(value) };
}

// Explorer path values (GET and POST /api/v0/explorer/path-values,
// rows OG-90..OG-93): the user-supplied path parts and their guarded edit.

export interface PathValues {
  file: string;
  exists: boolean;
  hash: string;
  editable: boolean;
  reason: string | null;
  token: string;
  values: UserValue[];
  unresolved: { route: string; method: string; pattern: string; file: string; line: number; reason: string; keys: string[] }[];
  /** The unresolved mount hops (rows OG-94, OG-95): one value for a hop's
   * key places every route below it. */
  mounts: MountHop[];
}

/** MountHop is one hop where composition stopped, with the routes below. */
export interface MountHop {
  key: string;
  hop: string;
  file: string;
  line: number;
  reason: string;
  registration: string;
  shadowingNotChecked: boolean;
  routes: number;
  /** Some routes below the hop: method, known prefix above the hop and
   * path below it. */
  examples: MountExample[];
  ambiguous: boolean;
  /** What a value states (row OG-95): "hop" (only the part the hop adds;
   * `above` lists the prefixes Graphvein read above it, "" for the root),
   * "nothing" (the hop adds no unread part: only "/" is accepted) or
   * "root" (the whole prefix from the root: the routers above the hop are
   * not proven either). */
  covers: "hop" | "nothing" | "root";
  above: string[];
  framework: string;
}

/** MountExample is one route below a hop. */
export interface MountExample {
  method: string;
  above: string;
  path: string;
}

function parseCovers(m: JSONObject, p: string): MountHop["covers"] {
  const c = m.value_covers;
  if (c === undefined || c === "root") return "root";
  if (c === "hop" || c === "nothing") return c;
  throw new Error(`${p}.value_covers: unknown value ${JSON.stringify(c)}`);
}

function parseMountExample(v: unknown, p: string): MountExample {
  const e = object(v, p);
  return { method: str(e, "method", p), above: str(e, "above", p), path: str(e, "path", p) };
}

function parseMount(v: unknown, p: string): MountHop {
  const m = object(v, p);
  return {
    key: str(m, "key", p),
    hop: str(m, "hop", p),
    file: str(m, "file", p),
    line: int(m, "line", p),
    reason: str(m, "reason", p),
    registration: str(m, "registration", p),
    shadowingNotChecked: bool(m, "shadowing_not_checked", p),
    routes: int(m, "routes", p),
    examples: array(m.examples, `${p}.examples`).map((e, i) => parseMountExample(e, `${p}.examples[${String(i)}]`)),
    ambiguous: bool(m, "ambiguous", p),
    covers: parseCovers(m, p),
    above: m.above === undefined ? [] : strings(m.above, `${p}.above`),
    framework: m.framework === undefined ? "" : str(m, "framework", p),
  };
}

export function parsePathValues(value: unknown): PathValues {
  const o = object(value, "path-values");
  return {
    file: str(o, "file", "path-values"),
    exists: bool(o, "exists", "path-values"),
    hash: str(o, "hash", "path-values"),
    editable: bool(o, "editable", "path-values"),
    reason: optStr(o, "reason", "path-values"),
    token: typeof o.token === "string" ? o.token : "",
    values: array(o.values, "path-values.values").map((v, i) => parseUserValue(v, `path-values.values[${String(i)}]`)),
    unresolved: array(o.unresolved, "path-values.unresolved").map((v, i) => {
      const p = `path-values.unresolved[${String(i)}]`;
      const u = object(v, p);
      return { route: str(u, "route", p), method: str(u, "method", p), pattern: str(u, "pattern", p), file: str(u, "file", p), line: int(u, "line", p), reason: str(u, "reason", p), keys: strings(u.keys, `${p}.keys`) };
    }),
    mounts: (o.mounts === undefined ? [] : array(o.mounts, "path-values.mounts")).map((v, i) => parseMount(v, `path-values.mounts[${String(i)}]`)),
  };
}

export type PathValueOp = { op: "set"; key: string; value: string } | { op: "remove"; key: string };

export interface PathValueEdit {
  applied: boolean;
  baseHash: string;
  resultHash: string;
  diff: string;
  valid: boolean;
  problems: string[];
  changes: string[];
}

export function parsePathValueEdit(value: unknown): PathValueEdit {
  const o = object(value, "edit");
  return {
    applied: bool(o, "applied", "edit"),
    baseHash: str(o, "base_hash", "edit"),
    resultHash: str(o, "result_hash", "edit"),
    diff: typeof o.diff === "string" ? o.diff : "",
    valid: o.valid === undefined ? true : bool(o, "valid", "edit"),
    problems: strings(o.problems, "edit.problems"),
    changes: strings(o.changes, "edit.changes"),
  };
}

type Signal = { signal?: AbortSignal };

export const OPENAPI_PATH = "/api/v0/openapi";
export const PATH_VALUES_PATH = "/api/v0/explorer/path-values";

/** ExplorerClient is every request the Explorer makes; tests replace it. */
export interface ExplorerClient {
  /** The selected member's (or the repository's) document. */
  document: (options: Signal) => Promise<OpenApiAnswer>;
  /** One named member's document (project mode: the member select's
   * counts). */
  memberDocument: (repository: string, options: Signal) => Promise<OpenApiDocument>;
  /** The document as text, for the Spec tab and the download. */
  text: (format: "json" | "yaml", options: Signal) => Promise<string>;
  pathValues: (options: Signal) => Promise<PathValues>;
  editPathValues: (body: { base_hash: string; operations: PathValueOp[]; apply?: boolean; result_hash?: string }, token: string) => Promise<PathValueEdit>;
  /** The tags of committed spec operations (api_operation identities), as
   * the import stored them; for grouping by OpenAPI tag. */
  operationTags: (ids: readonly string[], options: Signal) => Promise<Map<string, string[]>>;
  /** The editor links of a route's shape chips (T029): the endpoint
   * detail's shape_links with its ide status, requested for the route of
   * an opened operation; the document carries no editor link. Absent:
   * the chips show no editor button. */
  routeLinks?: (route: string, editor: string | null, options: Signal) => Promise<{ status: IdeStatus; links: Record<string, IdeLink>; callers?: LinkedCallDifferences[] }>;
}

/** TAG_CONCURRENCY bounds the node reads made for tag grouping. */
const TAG_CONCURRENCY = 4;

/** readOperationTags reads each api_operation node (GET /api/v0/source,
 * no context lines) and returns its stored tags. A node that cannot be
 * read is left out: its operations fall in "No tag", never under a guess. */
async function readOperationTags(ids: readonly string[], options: Signal): Promise<Map<string, string[]>> {
  const out = new Map<string, string[]>();
  let next = 0;
  const worker = async () => {
    while (next < ids.length) {
      const id = ids[next++];
      if (id === undefined) return;
      try {
        const body = await fetchJSON(`/api/v0/source?id=${encodeURIComponent(id)}&context=0`, options);
        const node = object(object(body, "source").node, "source.node");
        const attrs = object(node.attributes, "source.node.attributes");
        const tags = Array.isArray(attrs.tags) ? attrs.tags.filter((t): t is string => typeof t === "string") : [];
        out.set(id, tags);
      } catch (err) {
        if (options.signal?.aborted === true) throw err;
      }
    }
  };
  await Promise.all(Array.from({ length: Math.min(TAG_CONCURRENCY, ids.length) }, worker));
  return out;
}

/** fetchText requests the document as text with the same error handling as
 * fetchJSON (the error envelope is JSON). */
async function fetchText(path: string, options: Signal): Promise<string> {
  let response: Response;
  try {
    response = await fetch(withRepository(path), { headers: { Accept: "application/json, application/yaml" }, cache: "no-store", ...(options.signal ? { signal: options.signal } : {}) });
  } catch (err) {
    if (options.signal?.aborted === true) throw options.signal.reason;
    throw new ApiError("network", err instanceof Error ? err.message : String(err));
  }
  const text = await response.text();
  if (!response.ok) {
    let code: string | undefined;
    let message = `HTTP ${String(response.status)}`;
    try {
      const body = JSON.parse(text) as { error?: { code?: string; message?: string } };
      if (body.error?.code !== undefined) {
        code = body.error.code;
        message = `${body.error.code}: ${body.error.message ?? ""}`;
      }
    } catch {
      // Not the JSON error envelope: keep the status line.
    }
    throw new ApiError("http", message, response.status, code);
  }
  return text;
}

export const httpExplorerClient: ExplorerClient = {
  document: (options) => getJSON(OPENAPI_PATH, parseOpenApiAnswer, options),
  memberDocument: (repository, options) => getJSON(`${OPENAPI_PATH}?repository=${encodeURIComponent(repository)}`, parseOpenApi, options),
  text: (format, options) => fetchText(`${OPENAPI_PATH}?format=${format}`, options),
  pathValues: (options) => getJSON(PATH_VALUES_PATH, parsePathValues, options),
  editPathValues: async (body, token) => parsePathValueEdit(await fetchJSON(PATH_VALUES_PATH, { method: "POST", body, headers: { "X-Graphvein-Token": token } })),
  operationTags: readOperationTags,
  routeLinks: async (route, editor, options) => {
    const d = await httpClient.endpoint({ id: route, scope: "all", root: null, ...(editor !== null ? { editor } : {}) }, options);
    // The request differences of the route's links (T029 W4): callers of
    // this repository, then cross-repository callers in project mode.
    const callers: LinkedCallDifferences[] = [];
    for (const c of d.callers) {
      if (hasShapeDifferences(c.shape_differences)) callers.push({ name: c.node.qualified_name, file: c.node.file, line: c.node.span?.start_line ?? null, diffs: c.shape_differences });
    }
    for (const c of d.project?.callers ?? []) {
      if (c.node !== null && hasShapeDifferences(c.shape_differences)) callers.push({ name: `${c.repository}: ${c.node.qualified_name}`, file: c.node.file, line: c.node.span?.start_line ?? null, diffs: c.shape_differences });
    }
    return { status: d.ide, links: d.shape_links, callers };
  },
};
