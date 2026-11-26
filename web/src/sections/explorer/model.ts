// The Explorer's view model (T029): operations grouped for the list, and
// each expanded operation's sections with the state the document proves.
// Pure functions over the parsed document (api/openapi.ts), so the rules
// below are tested without rendering.
//
// The one rule every function keeps: absence is shown only where the
// document proves it. A section is "not read" unless the stored shape says
// otherwise (x-graphvein.not_read, OG-41); "provably none" comes only from
// a stored provably_none state with its evidence; a partial section says
// what may be missing. No function turns a missing field into "none".

import type { DocMedia, DocOperation, DocParameter, NotStandardItem, OpenApiDocument, OpenApiSchema } from "@/api/openapi";
import type { SectionState, ShapeParam, ShapeProvenance, ShapeSchema } from "@/api/routeShape";

/** The sections of an expanded operation, in Swagger's order. */
export const SECTION_KEYS = ["path", "query", "header", "cookie", "body", "responses", "security"] as const;
export type SectionKey = (typeof SECTION_KEYS)[number];

export const SECTION_TITLE: Record<SectionKey, string> = {
  path: "Path",
  query: "Query",
  header: "Headers",
  cookie: "Cookies",
  body: "Request body",
  responses: "Responses",
  security: "Security",
};

/** The x-graphvein.not_read name of a section (OG-41). */
const NOT_READ_NAME: Record<SectionKey, string> = {
  path: "path_parameter_types",
  query: "query",
  header: "header",
  cookie: "cookie",
  body: "requestBody",
  responses: "responses",
  security: "security",
};

/** What a section claims as a whole. Besides the stored states:
 * "template" is a path section without a stored shape whose names come
 * from the route's template (types not read, OG-26), "no_template_params"
 * one whose template has no parameter (a fact of the path key), and
 * "declared" a security section written from FastAPI security dependencies
 * (OG-30) without a stored shape section. */
export type SectionStatus =
  | { kind: "not_read"; note?: string }
  | { kind: "provably_none"; evidence: ShapeProvenance | null; note?: string }
  | { kind: "none_besides_scheme"; evidence: ShapeProvenance | null; schemes: string[] }
  | { kind: "partial"; reason: string | null }
  | { kind: "complete"; evidence: ShapeProvenance | null; note?: string }
  | { kind: "template" }
  | { kind: "no_template_params" }
  | { kind: "declared" };

/** One row of a section. provenance is the chip; inDocument says whether
 * the item is a standard OpenAPI field of the document (else row and why
 * say which claim row keeps it in x-graphvein). */
export interface ItemView {
  name: string;
  /** The type or schema in words ("string", "ItemPublic", "type not read"). */
  type: string;
  /** The schema to expand, when it has fields (an object or a model). */
  schema: { kind: "openapi"; schema: OpenApiSchema } | { kind: "shape"; schema: ShapeSchema } | null;
  presence: "required" | "optional" | "unknown" | null;
  access: string | null;
  provenance: ShapeProvenance;
  inDocument: boolean;
  row: string | null;
  why: string | null;
  /** Extra facts in words (a content type, a Starlette path convertor). */
  notes: string[];
  /** Set for a name a security scheme of the operation reads (a header,
   * query parameter or cookie): the scheme's name. Such an item is not a
   * parameter of the document; the Security section has the scheme. */
  readBy?: string;
}

export interface SectionView {
  key: SectionKey;
  status: SectionStatus;
  items: ItemView[];
}

/** templateProvenance is the provenance of a path parameter name that
 * comes from the stored route's template (OG-26): the route is a stored
 * fact at its registration (OG-32, confidence 1). */
export function templateProvenance(op: DocOperation): ShapeProvenance {
  return { kind: "fact", rule: op.rule, file: op.file, line: op.line, chain: [], confidence: 1 };
}

/** sectionStatus is what the document proves about one section. */
export function sectionStatus(op: DocOperation, key: SectionKey): SectionStatus {
  const stored = op.shape?.sections[key];
  if (stored !== undefined) {
    const s: SectionState = stored.state;
    // A stored reason on a section that is not partial is a note: a
    // committed spec that differs from the code, or that it is the only
    // source of the section's items (T029 SH-O, rows SH-O3 and SH-O18).
    const note = stored.reason !== null && stored.reason !== "" ? { note: stored.reason } : {};
    if (s === "provably_none") return { kind: "provably_none", evidence: stored.evidence, ...note };
    if (s === "partial") return { kind: "partial", reason: stored.reason };
    if (s === "complete") return { kind: "complete", evidence: stored.evidence, ...note };
    return { kind: "not_read", ...note };
  }
  if (key === "path") {
    // Names come from the path key itself (OG-11); without a stored shape
    // their types are not read.
    return op.parameters.some((p) => p.in === "path") ? { kind: "template" } : { kind: "no_template_params" };
  }
  if (key === "security" && op.security !== null) return { kind: "declared" };
  // Without a stored section the document names the field in not_read;
  // a field missing from not_read is still never claimed as "none".
  return { kind: "not_read" };
}

/** notReadNames lists the not_read names of the document for a key, for
 * tooltips ("x-graphvein.not_read: query"). */
export function notReadName(key: SectionKey): string {
  return NOT_READ_NAME[key];
}

function refName(ref: string): string {
  const i = ref.lastIndexOf("/");
  return i === -1 ? ref : ref.slice(i + 1);
}

/** openapiTypeText says a written Schema Object in words. {} with no
 * x-graphvein reason is "no constraint", never a type. */
export function openapiTypeText(schema: OpenApiSchema, typeNotRead = false): string {
  if (typeNotRead) return "type not read";
  const x = schema["x-graphvein"];
  if (typeof schema.$ref === "string") return refName(schema.$ref);
  if (Array.isArray(schema.anyOf)) return schema.anyOf.map((s) => (typeof s === "object" && s !== null ? openapiTypeText(s as OpenApiSchema) : "?")).join(" | ");
  if (Array.isArray(schema.enum)) return schema.enum.map((v) => JSON.stringify(v)).join(" | ");
  const type = schema.type;
  if (typeof type === "string" || Array.isArray(type)) {
    const types = (Array.isArray(type) ? type : [type]).map(String);
    return types
      .map((t) => {
        if (t === "array" && typeof schema.items === "object" && schema.items !== null) return `${openapiTypeText(schema.items as OpenApiSchema)}[]`;
        return t;
      })
      .join(" | ");
  }
  if (typeof x === "object" && x !== null && "unknown" in x && typeof x.unknown === "string") {
    // Some reasons already end with the words ("... unless strict(): type
    // not claimed"); they are said once.
    const reason = x.unknown.replace(/:?\s*type not claimed$/, "");
    return reason === "" ? "type not claimed" : `type not claimed: ${reason}`;
  }
  return "no constraint";
}

/** requestBodyPresence says whether the request body must be sent, for
 * the Request body heading: the document's `required`, or "unknown" when
 * x-graphvein says the presence was not read (never "optional", OpenAPI's
 * default for a missing `required`). null when no body is written. */
export function requestBodyPresence(op: DocOperation): ItemView["presence"] {
  if (op.requestBody === null) return null;
  if (op.requestBodyRequired === true) return "required";
  if (op.requestBodyRequired === false) return "optional";
  return "unknown";
}

/** emptyDocumentText says why a document has no operation, from its own
 * counts: no route stored, only test-scope routes, or the production
 * routes' entries listed under "Not in the document" by reason. */
export function emptyDocumentText(doc: Pick<OpenApiDocument, "counts" | "excludedTest">): string {
  const c = doc.counts;
  const n = (v: number, one: string, many = `${one}s`) => `${String(v)} ${v === 1 ? one : many}`;
  if (c.routes === 0) return "No route is stored for this repository (0 routes indexed), so the document has no operation.";
  // Every route that is not production is test-scope (OG-48).
  if (c.production_routes === 0) return `No operation is in the document: ${n(c.routes, "route")} indexed, ${c.routes === 1 ? "it is" : "all"} test-scope, and the document covers production routes only.`;
  const reasons = [
    c.any_method > 0 ? `${n(c.any_method, "any-method entry", "any-method entries")} (never expanded to methods)` : null,
    c.unresolved > 0 ? `${n(c.unresolved, "route")} with an unknown path part` : null,
    c.conflicts > 0 ? n(c.conflicts, "conflict") : null,
    c.unrepresentable > 0 ? `${n(c.unrepresentable, "path")} OpenAPI cannot express` : null,
  ].filter((r): r is string => r !== null);
  const of = `${n(c.production_routes, "production route")} indexed${doc.excludedTest.routes > 0 ? ` (${String(doc.excludedTest.routes)} test-scope left out)` : ""}`;
  if (reasons.length === 0) return `No operation is in the document: ${of}, and the document gives no reason for any of them.`;
  return `No operation is in the document: ${of}; listed below: ${reasons.join(", ")}.`;
}

/** shapeTypeText says a stored schema (SM-20) in words. */
export function shapeTypeText(schema: ShapeSchema | null): string {
  if (schema === null) return "type not read";
  let t: string;
  switch (schema.type) {
    case "ref":
      t = schema.ref ?? "ref";
      break;
    case "array":
      t = `${schema.items !== undefined ? shapeTypeText(schema.items) : "?"}[]`;
      break;
    case "enum":
      t = (schema.enum ?? []).map((v) => JSON.stringify(v)).join(" | ");
      break;
    case "unknown":
      t = `unknown${schema.reason !== undefined && schema.reason !== "" ? `: ${schema.reason}` : ""}`;
      break;
    default:
      t = schema.type;
  }
  return schema.nullable === true ? `${t} | null` : t;
}

function hasFields(schema: OpenApiSchema): boolean {
  return typeof schema.$ref === "string" || (typeof schema.properties === "object" && schema.properties !== null) || (typeof schema.items === "object" && schema.items !== null);
}

function paramItem(p: DocParameter, op: DocOperation): ItemView {
  const notes: string[] = [];
  if (p.matches !== null) notes.push(p.matches);
  return {
    name: p.name,
    type: openapiTypeText(p.schema, p.typeNotRead),
    schema: hasFields(p.schema) ? { kind: "openapi", schema: p.schema } : null,
    presence: p.required === null ? null : p.required ? "required" : "optional",
    access: "declared",
    provenance: p.provenance ?? templateProvenance(op),
    inDocument: true,
    row: null,
    why: null,
    notes,
  };
}

function mediaItem(name: string, m: DocMedia, op: DocOperation, extra: string[]): ItemView {
  return {
    name,
    type: m.schema === null ? "schema not read" : openapiTypeText(m.schema),
    schema: m.schema !== null && hasFields(m.schema) ? { kind: "openapi", schema: m.schema } : null,
    presence: null,
    access: null,
    provenance: m.provenance ?? templateProvenance(op),
    inDocument: true,
    row: null,
    why: null,
    notes: [m.contentType, ...extra],
  };
}

function shapeSchemaRef(schema: ShapeSchema | null): ItemView["schema"] {
  if (schema === null) return null;
  return schema.type === "object" || schema.type === "ref" || schema.type === "array" ? { kind: "shape", schema } : null;
}

/** sectionItems lists a section's items: the standard fields first, then
 * the items x-graphvein keeps out of them, each with its provenance. */
export function sectionItems(op: DocOperation, key: SectionKey): ItemView[] {
  const out: ItemView[] = [];
  if (key === "path" || key === "query" || key === "header" || key === "cookie") {
    for (const p of op.parameters) if (p.in === key) out.push(paramItem(p, op));
  } else if (key === "body") {
    for (const m of op.requestBody ?? []) out.push({ ...mediaItem(m.contentType, m, op, []), notes: [] });
  } else if (key === "responses") {
    for (const r of op.responses ?? []) {
      if (r.content.length === 0) {
        out.push({
          name: r.status,
          type: r.emptyBody ? "no body (proven empty)" : "body not read",
          schema: null,
          presence: null,
          access: null,
          provenance: r.provenance ?? templateProvenance(op),
          inDocument: true,
          row: null,
          why: null,
          notes: [],
        });
      }
      for (const m of r.content) out.push(mediaItem(r.status, m, op, []));
    }
  } else {
    for (const req of op.security ?? []) {
      // OG-30: written from the route's FastAPI security dependencies.
      const deps = Array.isArray(op.auth.security_dependencies) ? op.auth.security_dependencies : [];
      out.push({
        name: req.length === 0 ? "anonymous" : req.map((s) => s.name).join(" + "),
        type: req.length === 0 ? "no credentials (a proven alternative)" : req.length === 1 ? "scheme" : "all of these schemes",
        schema: null,
        presence: null,
        access: null,
        provenance: securityProvenance(op, deps) ?? templateProvenance(op),
        inDocument: true,
        row: null,
        why: null,
        notes: [],
      });
    }
  }
  for (const n of op.shape?.notStandard ?? []) {
    if (n.section !== key) continue;
    if (isParamItem(n)) {
      out.push({
        name: n.item.name,
        type: shapeTypeText(n.item.schema),
        schema: shapeSchemaRef(n.item.schema),
        presence: n.item.presence,
        access: n.item.access,
        provenance: n.item.provenance,
        inDocument: false,
        row: n.row,
        why: n.why,
        notes: [],
      });
    } else if (n.section === "body") {
      out.push({
        name: n.item.content_type ?? "content type not read",
        type: shapeTypeText(n.item.schema),
        schema: shapeSchemaRef(n.item.schema),
        presence: null,
        access: null,
        provenance: n.item.provenance,
        inDocument: false,
        row: n.row,
        why: n.why,
        notes: [],
      });
    } else if (n.section === "responses") {
      out.push({
        name: n.item.status,
        type: n.item.schema === null ? "body not read" : shapeTypeText(n.item.schema),
        schema: shapeSchemaRef(n.item.schema),
        presence: null,
        access: null,
        provenance: n.item.provenance,
        inDocument: false,
        row: n.row,
        why: n.why,
        notes: n.item.content_type !== null ? [n.item.content_type] : [],
      });
    } else {
      // The security section (the union's last variant).
      out.push({
        name: n.item.schemes.length === 0 ? "anonymous" : n.item.schemes.map((s) => s.name).join(" + "),
        type: n.item.schemes.length === 0 ? "no credentials (an alternative)" : n.item.schemes.length === 1 ? "scheme" : "all of these schemes",
        schema: null,
        presence: null,
        access: null,
        provenance: n.item.provenance,
        inDocument: false,
        row: n.row,
        why: n.why,
        notes: [],
      });
    }
  }
  if (key === "responses") out.sort((a, b) => a.name.localeCompare(b.name));
  return out;
}

/** isParamItem says whether a kept item is a parameter. */
function isParamItem(n: NotStandardItem): n is Extract<NotStandardItem, { item: ShapeParam }> {
  return n.section === "path" || n.section === "query" || n.section === "header" || n.section === "cookie";
}

/** securityProvenance is the declaration site of the first FastAPI
 * security dependency (its chain's first step), when stored. */
function securityProvenance(op: DocOperation, deps: unknown[]): ShapeProvenance | null {
  const first = deps[0];
  if (typeof first !== "object" || first === null || !("chain" in first) || !Array.isArray(first.chain)) return null;
  const step: unknown = first.chain[0];
  if (typeof step !== "object" || step === null) return null;
  const file = "file" in step && typeof step.file === "string" ? step.file : null;
  const span = "span" in step && typeof step.span === "object" && step.span !== null ? (step.span as Record<string, unknown>) : null;
  const line = span !== null && typeof span.start_line === "number" ? span.start_line : null;
  if (file === null || line === null) return null;
  return { kind: "fact", rule: op.rule, file, line, chain: [], confidence: 1 };
}

/** SchemeRead is where one security scheme that applies to an operation
 * sends its credential: the location (null when not known) and the name
 * (null when not stored, such as an API key class whose key name the
 * index does not keep). */
export interface SchemeRead {
  scheme: string;
  location: "header" | "query" | "cookie" | null;
  name: string | null;
  provenance: ShapeProvenance | null;
}

/** classRead is where a FastAPI security class reads its credential:
 * HTTP and OAuth2 classes read Authorization; the API key classes read a
 * header, query parameter or cookie whose name is not stored. */
export function classRead(cls: string): { location: SchemeRead["location"]; name: string | null } {
  if (/^HTTP(Bearer|Basic|Digest)$/.test(cls) || cls.startsWith("OAuth2")) return { location: "header", name: "Authorization" };
  if (cls === "APIKeyHeader") return { location: "header", name: null };
  if (cls === "APIKeyQuery") return { location: "query", name: null };
  if (cls === "APIKeyCookie") return { location: "cookie", name: null };
  return { location: null, name: null };
}

/** componentRead reads a written Security Scheme Object (OG-31, OG-87). */
export function componentRead(scheme: Record<string, unknown>): { location: SchemeRead["location"]; name: string | null } {
  const type = scheme.type;
  if (type === "http" || type === "oauth2") return { location: "header", name: "Authorization" };
  if (type === "apiKey") {
    const where = scheme.in;
    const location = where === "header" || where === "query" || where === "cookie" ? where : null;
    return { location, name: typeof scheme.name === "string" && scheme.name !== "" ? scheme.name : null };
  }
  return { location: null, name: null };
}

/** schemeReads lists the security schemes that apply to an operation and
 * what each reads: the schemes its security requirements name (written or
 * kept in x-graphvein) and its declared FastAPI security dependencies. A
 * scheme the document does not describe reads from a location not known,
 * never "nothing". */
export function schemeReads(op: DocOperation, doc: Pick<OpenApiDocument, "securitySchemes">): SchemeRead[] {
  const out = new Map<string, SchemeRead>();
  const deps = Array.isArray(op.auth.security_dependencies) ? (op.auth.security_dependencies as unknown[]) : [];
  const depClass = (name: string): string | null => {
    for (const d of deps) {
      if (typeof d !== "object" || d === null) continue;
      const o = d as Record<string, unknown>;
      if ((o.scheme_name === name || o.class === name) && typeof o.class === "string") return o.class;
    }
    return null;
  };
  const add = (name: string, provenance: ShapeProvenance | null) => {
    if (out.has(name)) return;
    const component = doc.securitySchemes[name];
    const cls = depClass(name);
    const read = component !== undefined ? componentRead(component) : cls !== null ? classRead(cls) : { location: null, name: null };
    out.set(name, { scheme: name, ...read, provenance });
  };
  for (const req of op.security ?? []) for (const s of req) add(s.name, securityProvenance(op, deps));
  for (const n of op.shape?.notStandard ?? []) {
    if (n.section !== "security") continue;
    for (const s of n.item.schemes) add(s.name, n.item.provenance);
  }
  for (const d of deps) {
    if (typeof d !== "object" || d === null) continue;
    const o = d as Record<string, unknown>;
    const name = typeof o.scheme_name === "string" ? o.scheme_name : typeof o.class === "string" ? o.class : null;
    if (name !== null) add(name, securityProvenance(op, [d]));
  }
  return [...out.values()];
}

/** schemeItems are the rows a section shows for the names security
 * schemes read there: "read by the security scheme (see Security)". A
 * scheme whose location is not known is not listed as an item; the
 * section's state line says it may read there. */
export function schemeItems(op: DocOperation, key: SectionKey, reads: readonly SchemeRead[]): ItemView[] {
  if (key !== "header" && key !== "query" && key !== "cookie") return [];
  return reads
    .filter((r) => r.location === key)
    .map((r) => ({
      name: r.name ?? "name not stored",
      type: "read by the security scheme (see Security)",
      schema: null,
      presence: null,
      access: null,
      provenance: r.provenance ?? templateProvenance(op),
      inDocument: false,
      row: null,
      why: null,
      notes: [],
      readBy: r.scheme,
    }));
}

/** sectionView is one section's state and items. With the document, a
 * header, query or cookie section also shows what the operation's
 * security schemes read there, and a proven-empty section where a scheme
 * reads (or may read: location not known) says "none besides the security
 * scheme" instead of "none". */
export function sectionView(op: DocOperation, key: SectionKey, doc?: Pick<OpenApiDocument, "securitySchemes">): SectionView {
  const status = sectionStatus(op, key);
  const items = sectionItems(op, key);
  if (doc === undefined || (key !== "header" && key !== "query" && key !== "cookie")) return { key, status, items };
  const reads = schemeReads(op, doc);
  const here = reads.filter((r) => r.location === key || r.location === null);
  const out: SectionView = { key, status, items: [...schemeItems(op, key, reads), ...items] };
  if (status.kind === "provably_none" && here.length > 0) out.status = { kind: "none_besides_scheme", evidence: status.evidence, schemes: here.map((r) => r.scheme) };
  return out;
}

/** AuthLine is one stored auth attribute of the route as written (OG-42):
 * what the code declares, never proof that the route is protected. */
export interface AuthLine {
  attribute: string;
  title: string;
  /** Entries in words; empty means "none read", never "public". */
  entries: { text: string; detail: string | null }[];
  /** True for the *_unknown attributes: entries whose effect is not read. */
  unknown: boolean;
}

const AUTH_TITLE: Record<string, string> = {
  security_dependencies: "FastAPI security dependencies",
  security_dependencies_unknown: "FastAPI dependencies not read",
  middleware: "Route middleware",
  mounted_middleware: "Middleware mounted before the route",
  mounted_middleware_unknown: "Earlier middleware not proven",
  middleware_declares: "Middleware declares",
  layers: "Layers",
  layers_unknown: "Layers not read",
};

function entryText(v: unknown): { text: string; detail: string | null } {
  if (typeof v === "string") return { text: v, detail: null };
  if (typeof v === "object" && v !== null) {
    const o = v as Record<string, unknown>;
    const text =
      typeof o.class === "string"
        ? `declares ${o.class}${typeof o.scheme_name === "string" && o.scheme_name !== o.class ? ` (${o.scheme_name})` : ""}`
        : typeof o.expression === "string"
          ? o.expression
          : typeof o.name === "string"
            ? o.name
            : JSON.stringify(v);
    const parts: string[] = [];
    if (typeof o.reason === "string") parts.push(o.reason);
    if (typeof o.auto_error === "boolean") parts.push(`auto_error ${String(o.auto_error)}`);
    if (Array.isArray(o.chain)) {
      const chain = o.chain
        .map((s: unknown) => (typeof s === "object" && s !== null && "via" in s && typeof s.via === "string" ? s.via : null))
        .filter((s): s is string => s !== null);
      if (chain.length > 0) parts.push(chain.join(" → "));
    }
    if (typeof o.file === "string") {
      const span = typeof o.span === "object" && o.span !== null ? (o.span as Record<string, unknown>) : null;
      parts.push(`${o.file}${span !== null && typeof span.start_line === "number" ? `:${String(span.start_line)}` : ""}`);
    }
    return { text, detail: parts.length > 0 ? parts.join(" · ") : null };
  }
  return { text: String(v), detail: null };
}

/** authLines lists the route's stored auth attributes in a fixed order. */
export function authLines(op: DocOperation): AuthLine[] {
  const out: AuthLine[] = [];
  for (const attribute of Object.keys(AUTH_TITLE)) {
    const v = op.auth[attribute];
    if (v === undefined) continue;
    const list = Array.isArray(v) ? v : [v];
    out.push({ attribute, title: AUTH_TITLE[attribute] ?? attribute, entries: list.map(entryText), unknown: attribute.endsWith("_unknown") });
  }
  for (const attribute of Object.keys(op.auth)) {
    if (attribute in AUTH_TITLE) continue;
    const v = op.auth[attribute];
    const list = Array.isArray(v) ? v : [v];
    out.push({ attribute, title: attribute, entries: list.map(entryText), unknown: attribute.endsWith("_unknown") });
  }
  return out;
}

// Grouping.

export type GroupBy = "file" | "tag";

export interface OperationGroup {
  id: string;
  title: string;
  /** The router file (by file) or where the tag comes from (by tag). */
  subtitle: string;
  operations: DocOperation[];
}

/** operationId names an operation in the Explorer (the hash, React keys):
 * its method and path key, unique in a document. The route identity is
 * not: a Next.js basePath value set by you gives one route several keys. */
export function operationId(op: DocOperation): string {
  return `${op.method.toUpperCase()} ${op.key}`;
}

/** stem is a file's name without directory and extension. */
function stem(file: string): string {
  const i = file.lastIndexOf("/");
  return (i === -1 ? file : file.slice(i + 1)).replace(/\.[^.]+$/, "");
}

/** groupByFile groups operations by the file that registers them (the
 * router file), in file order; operations keep the document's order. */
export function groupByFile(ops: readonly DocOperation[]): OperationGroup[] {
  const map = new Map<string, DocOperation[]>();
  for (const op of ops) {
    const list = map.get(op.file);
    if (list === undefined) map.set(op.file, [op]);
    else list.push(op);
  }
  const files = [...map.keys()].sort((a, b) => (a < b ? -1 : a > b ? 1 : 0));
  // A file name several router files share ("route.ts", "index.js") gets
  // its directory, so the titles tell the groups apart.
  const names = new Map<string, number>();
  for (const f of files) names.set(stem(f), (names.get(stem(f)) ?? 0) + 1);
  return files.map((file) => {
    const segs = file.split("/");
    const shared = (names.get(stem(file)) ?? 0) > 1 && segs.length > 1;
    return { id: `file:${file}`, title: shared ? `${segs[segs.length - 2] ?? ""}/${stem(file)}` : stem(file), subtitle: file, operations: map.get(file) ?? [] };
  });
}

/** NO_TAG is the group of operations no committed spec documents. */
export const NO_TAG = "tag:";

/** groupByTag groups operations by the first tag of the committed spec
 * operations that document them (OG-44), as the spec writes them: an
 * operation documented with tags appears under each of its specs' first
 * tags. Operations without a documenting tag go to one "No tag" group,
 * last. tags maps an api_operation identity to its tags. */
export function groupByTag(ops: readonly DocOperation[], tags: ReadonlyMap<string, readonly string[]>): OperationGroup[] {
  const map = new Map<string, { files: Set<string>; ops: DocOperation[] }>();
  const none: DocOperation[] = [];
  for (const op of ops) {
    const seen = new Set<string>();
    for (const d of op.documentedBy) {
      const t = tags.get(d.operation)?.[0];
      if (t === undefined || seen.has(t)) continue;
      seen.add(t);
      const g = map.get(t) ?? { files: new Set<string>(), ops: [] };
      g.files.add(d.file);
      g.ops.push(op);
      map.set(t, g);
    }
    if (seen.size === 0) none.push(op);
  }
  const out: OperationGroup[] = [...map.entries()]
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([tag, g]) => ({ id: `tag:${tag}`, title: tag, subtitle: `tag in ${[...g.files].join(", ")}`, operations: g.ops }));
  if (none.length > 0) out.push({ id: NO_TAG, title: "No tag", subtitle: "no committed spec documents these with a tag", operations: none });
  return out;
}

/** documentingOperations lists the distinct committed spec operations
 * that document the given operations (the tags to read). */
export function documentingOperations(ops: readonly DocOperation[]): string[] {
  const set = new Set<string>();
  for (const op of ops) for (const d of op.documentedBy) set.add(d.operation);
  return [...set].sort();
}

/** matchesFilter says whether an operation matches a list filter: the
 * text is looked for in the method, path, handler and file. */
export function matchesFilter(op: DocOperation, text: string): boolean {
  const q = text.trim().toLowerCase();
  if (q === "") return true;
  return [op.method, op.key, op.handler ?? "", op.handlerExpression ?? "", op.file].some((s) => s.toLowerCase().includes(q));
}

/** handlerName is the handler's short name ("update_item"), or its
 * expression, or null when none is stored. */
export function handlerName(op: DocOperation): string | null {
  if (op.handler !== null) {
    const i = op.handler.lastIndexOf("#");
    return i === -1 ? op.handler : op.handler.slice(i + 1);
  }
  return op.handlerExpression;
}

/** OperationMarker is a marker shown on an operation's row (OG-19, OG-20,
 * OG-43, OG-47, OG-92). */
export interface OperationMarker {
  kind: "shadowed" | "shadowing_not_checked" | "equivalent" | "conditional" | "resolution" | "conflict" | "shape_error" | "depth_cap";
  text: string;
  title: string;
}

/** operationMarkers lists the markers of one operation, in a fixed order. */
export function operationMarkers(op: DocOperation, doc: OpenApiDocument): OperationMarker[] {
  const out: OperationMarker[] = [];
  if (op.mayBeShadowed.length > 0) {
    out.push({
      kind: "shadowed",
      text: `may be shadowed (${String(op.mayBeShadowed.length)})`,
      title: `the framework dispatches to the first matching route; whether these are registered earlier is not checked: ${op.mayBeShadowed.map((s) => `${s.method} ${s.key}`).join(", ")}`,
    });
  }
  if (op.shadowing === "not_checked") {
    out.push({
      kind: "shadowing_not_checked",
      text: "shadowing not checked",
      title: "placed by a prefix set by you below a hop: whether another pattern beside the hop answers some of these paths first was not checked",
    });
  }
  if (op.equivalentRegistrations.length > 0) {
    out.push({
      kind: "equivalent",
      text: `${String(op.equivalentRegistrations.length)} equivalent registrations`,
      title: `one operation for equivalent registrations; the first is written, as the matcher picks it: ${op.equivalentRegistrations.join(", ")}`,
    });
  }
  if (op.conditional) out.push({ kind: "conditional", text: "conditional", title: "registered under a condition: the route may not exist at runtime" });
  const conflicts = doc.conflicts.filter((c) => c.routes.includes(op.route));
  for (const c of conflicts) out.push({ kind: "conflict", text: c.reason.replace(/_/g, " "), title: `conflict ${c.reason}: ${c.keys.join(", ")}` });
  if (op.shapeError !== null) out.push({ kind: "shape_error", text: "shape not readable", title: op.shapeError });
  if (op.shape !== null && op.shape.depthCap.length > 0) {
    out.push({ kind: "depth_cap", text: `depth cap reached (${String(op.shape.depthLimit)})`, title: "raise it in graphvein.yaml (index.shape_depth)" });
  }
  return out;
}

/** unknownPathText shows an unresolved route's path with the unknown part
 * visible: "‹mount or prefix›/lorn" or "‹basePath of web›/api/x". */
export function unknownPathText(u: OpenApiDocument["unresolved"][number]): { unknown: string; rest: string } {
  const unknown = u.unknown.part === "base_path" ? `basePath${u.unknown.project !== null ? ` of ${u.unknown.project}` : ""}` : "mount or prefix";
  const rest = u.templatesAfterBase.length > 0 ? (u.templatesAfterBase[0] ?? u.pattern) : u.pattern;
  return { unknown, rest };
}

/** routeOperations lists the operations the document writes for one
 * route identity, in document order: one, or several when a value set by
 * you gives the route several path keys (a Next.js basePath). A route
 * collapsed into another's operation as an equivalent registration (OG-19)
 * has none of its own: equivalent is then the operation written for it. */
export function routeOperations(doc: Pick<OpenApiDocument, "operations">, route: string): { ops: DocOperation[]; equivalent: boolean } {
  const own = doc.operations.filter((o) => o.route === route);
  if (own.length > 0) return { ops: own, equivalent: false };
  return { ops: doc.operations.filter((o) => o.equivalentRegistrations.includes(route)), equivalent: true };
}

/** routeAbsentText says why the document has no operation for a route a
 * link points at, from the document's own lists; a route none of them
 * names gets no guessed reason. */
export function routeAbsentText(doc: Pick<OpenApiDocument, "unresolved" | "anyMethod" | "unrepresentable" | "conflicts" | "repository">, route: string): string {
  const head = `The document of ${doc.repository} has no operation for route ${route}`;
  if (doc.unresolved.some((u) => u.route === route)) return `${head}: its path has an unknown part (listed under “Not in the document” below).`;
  if (doc.anyMethod.some((a) => a.route === route)) return `${head}: it accepts any method, which the document lists per path key and never expands (listed below).`;
  if (doc.unrepresentable.some((u) => u.route === route)) return `${head}: OpenAPI cannot express its path (listed below).`;
  if (doc.conflicts.some((c) => c.routes.includes(route))) return `${head}: it conflicts with another route (listed below).`;
  return `${head}, and the document lists no reason (test-scope routes are left out of the document).`;
}
