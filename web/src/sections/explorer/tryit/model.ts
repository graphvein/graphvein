// Pure helpers of the Explorer's "Try it out" (T029 part B): what the
// request builder pre-fills from an operation, the JSON body template, the
// "Copy as curl" text, and the words for confirmation reasons and limits.
// The server decides everything that guards a request (confirmations,
// production, auth, certificates); nothing here can let a request through.

import type { DocOperation, OpenApiDocument, OpenApiSchema } from "@/api/openapi";
import type { ShapeProvenance, ShapeSchema } from "@/api/routeShape";
import type { Environment, Pair, Part, Prepared } from "@/api/tryit";
import { indentJson } from "@/sections/explorer/tryit/jsonIndent";
import { openapiTypeText, sectionView, shapeTypeText, templateProvenance } from "@/sections/explorer/model";

/** A key-value row of the request builder (query or header). provenance
 * is set for a row pre-filled from the operation's shape: the user sees
 * where the name comes from; a row the user added has none. */
export interface KeyValue {
  id: number;
  name: string;
  value: string;
  enabled: boolean;
  provenance: ShapeProvenance | null;
  /** The type in words, "type not read" when unknown. */
  type: string | null;
  required: boolean | null;
}

/** PathParam is one {name} of the path key: always required; its type is
 * the document's, or "type not read". */
export interface PathParam {
  name: string;
  type: string;
  provenance: ShapeProvenance;
}

/** pathParams lists the path key's parameters in template order. */
export function pathParams(op: DocOperation): PathParam[] {
  const names = [...op.key.matchAll(/\{([^}]+)\}/g)].map((m) => m[1] ?? "");
  return names.map((name) => {
    const p = op.parameters.find((x) => x.in === "path" && x.name === name);
    return { name, type: p === undefined ? "type not read" : openapiTypeText(p.schema, p.typeNotRead), provenance: p?.provenance ?? templateProvenance(op) };
  });
}

let nextId = 1;

/** newRow makes an empty builder row. */
export function newRow(name = "", value = ""): KeyValue {
  return { id: nextId++, name, value, enabled: true, provenance: null, type: null, required: null };
}

/** prefillRows pre-fills the query or header rows from the operation's
 * section items (standard parameters and names kept in x-graphvein), each
 * labelled with its provenance. Values start empty; an optional or
 * unknown-presence row starts disabled so nothing is sent by surprise.
 * Names a security scheme reads are left out: the environment's auth sets
 * them. */
export function prefillRows(op: DocOperation, doc: Pick<OpenApiDocument, "securitySchemes">, key: "query" | "header"): KeyValue[] {
  const view = sectionView(op, key, doc);
  return view.items
    .filter((i) => i.readBy === undefined)
    .map((i) => ({
      id: nextId++,
      name: i.name,
      value: "",
      enabled: i.presence === "required",
      provenance: i.provenance,
      type: i.type,
      required: i.presence === "required" ? true : i.presence === "optional" ? false : null,
    }));
}

/** BodyTemplate is a JSON template for the request body and where its
 * shape comes from: a fact (the document's requestBody schema) or an
 * intended type (a static type nothing checks at runtime). It is a
 * template to edit, never an example the server is known to accept. */
export interface BodyTemplate {
  contentType: string;
  text: string;
  /** The provenance kind: fact, intended, ... */
  kind: string;
  schemaName: string;
  provenance: ShapeProvenance | null;
}

function openapiTemplate(schema: OpenApiSchema, doc: Pick<OpenApiDocument, "schemas">, depth: number): unknown {
  if (depth > 6) return null;
  if (typeof schema.$ref === "string") {
    const name = schema.$ref.slice(schema.$ref.lastIndexOf("/") + 1);
    const target = doc.schemas[name];
    return target === undefined ? {} : openapiTemplate(target, doc, depth + 1);
  }
  if (Array.isArray(schema.enum) && schema.enum.length > 0) return schema.enum[0] as unknown;
  if (Array.isArray(schema.anyOf) && schema.anyOf.length > 0) {
    const first: unknown = (schema.anyOf as unknown[]).find((s) => typeof s === "object" && s !== null && (s as OpenApiSchema).type !== "null");
    return typeof first === "object" && first !== null ? openapiTemplate(first as OpenApiSchema, doc, depth + 1) : null;
  }
  const type: unknown = Array.isArray(schema.type) ? (schema.type as unknown[]).find((t) => t !== "null") : schema.type;
  switch (type) {
    case "object": {
      const props = typeof schema.properties === "object" && schema.properties !== null ? (schema.properties as Record<string, OpenApiSchema>) : {};
      return Object.fromEntries(Object.entries(props).map(([k, v]) => [k, openapiTemplate(v, doc, depth + 1)]));
    }
    case "array":
      return typeof schema.items === "object" && schema.items !== null ? [openapiTemplate(schema.items as OpenApiSchema, doc, depth + 1)] : [];
    case "string":
      return "";
    case "integer":
    case "number":
      return 0;
    case "boolean":
      return false;
    case "null":
      return null;
    default:
      return typeof schema.properties === "object" && schema.properties !== null ? openapiTemplate({ ...schema, type: "object" }, doc, depth) : null;
  }
}

function shapeTemplate(schema: ShapeSchema, models: readonly { name: string; schema: ShapeSchema }[], depth: number): unknown {
  if (depth > 6) return null;
  switch (schema.type) {
    case "ref": {
      const m = models.find((x) => x.name === schema.ref);
      return m === undefined ? {} : shapeTemplate(m.schema, models, depth + 1);
    }
    case "object":
      return Object.fromEntries((schema.fields ?? []).map((f) => [f.name, shapeTemplate(f.schema, models, depth + 1)]));
    case "array":
      return schema.items !== undefined ? [shapeTemplate(schema.items, models, depth + 1)] : [];
    case "enum":
      return schema.enum?.[0] ?? null;
    case "string":
      return "";
    case "integer":
    case "number":
      return 0;
    case "boolean":
      return false;
    default:
      return null;
  }
}

/** bodyTemplate builds a JSON template from the operation's request body:
 * the document's schema when it writes one (a fact), else an intended
 * type kept in x-graphvein. null when no JSON body shape is known. */
export function bodyTemplate(op: DocOperation, doc: Pick<OpenApiDocument, "schemas">): BodyTemplate | null {
  for (const m of op.requestBody ?? []) {
    if (m.schema === null || !/json/i.test(m.contentType)) continue;
    return { contentType: m.contentType, text: JSON.stringify(openapiTemplate(m.schema, doc, 0), null, 2), kind: m.provenance?.kind ?? "fact", schemaName: openapiTypeText(m.schema), provenance: m.provenance };
  }
  for (const n of op.shape?.notStandard ?? []) {
    if (n.section !== "body" || n.item.schema === null) continue;
    const ct = n.item.content_type ?? "application/json";
    if (!/json/i.test(ct)) continue;
    return { contentType: ct, text: JSON.stringify(shapeTemplate(n.item.schema, op.shape?.models ?? [], 0), null, 2), kind: n.item.provenance.kind, schemaName: shapeTypeText(n.item.schema), provenance: n.item.provenance };
  }
  return null;
}

/** isMultipart says whether the operation's body is a multipart form:
 * the builder then offers fields and a file picker. */
export function isMultipart(op: DocOperation): boolean {
  if ((op.requestBody ?? []).some((m) => m.contentType.toLowerCase().startsWith("multipart/form-data"))) return true;
  return (op.shape?.notStandard ?? []).some((n) => n.section === "body" && (n.item.content_type ?? "").toLowerCase().startsWith("multipart/form-data"));
}

/** REASON_TEXT says why a request needs a confirmation (TI-9, TI-26,
 * TI-27), as the plain style words it. */
export const REASON_TEXT: Record<string, string> = {
  method: "the method may change data",
  method_override: "a method override header or _method parameter turns this GET or HEAD into another method",
  body_on_get_or_head: "a GET or HEAD with a body or content type is treated as changing data",
  free_form_path: "a free-form path does not match the operation",
};

/** bytesText formats a byte count ("1 MiB", "287 B"). */
export function bytesText(n: number): string {
  if (n < 1024) return `${String(n)} B`;
  const units = ["KiB", "MiB", "GiB"];
  let v = n / 1024;
  let u = 0;
  while (v >= 1024 && u < units.length - 1) {
    v /= 1024;
    u++;
  }
  return `${v >= 10 || Number.isInteger(v) ? String(Math.round(v)) : v.toFixed(1)} ${units[u] ?? "GiB"}`;
}

/** limitText says one limit's value in its unit. */
export function limitText(limit: string, value: number): string {
  return limit === "timeout_seconds" ? `${String(value)} s` : bytesText(value);
}

/** LIMIT_NAME says which limit in words. */
export const LIMIT_NAME: Record<string, string> = {
  request_bytes: "request body size limit",
  response_bytes: "response size limit",
  timeout_seconds: "time limit",
};

function shellQuote(s: string): string {
  return `'${s.replace(/'/g, `'"'"'`)}'`;
}

/** doubleQuoted escapes s for use inside a double-quoted shell word. The
 * shell still reads backslash, double quote, dollar and backtick there, so
 * each is escaped: a configured header name or prefix such as "$(cmd)"
 * stays text when the copied command is run. */
function doubleQuoted(s: string): string {
  return s.replace(/[\\"$`]/g, "\\$&");
}

/** curlCommand writes the prepared request as a curl command line. The
 * auth header is written with its variable ($API_TOKEN) or a placeholder
 * for a value typed for the session, never a value: the UI never has it.
 * The URL is the one the server built. */
export function curlCommand(p: Prepared, env: Environment | undefined, req: { headers: Pair[]; body: string | null; contentType: string; multipart: Part[] | null }): string {
  const lines = [`curl -X ${p.method} ${shellQuote(p.url)}`];
  for (const h of req.headers) lines.push(`-H ${shellQuote(`${h.name}: ${h.value}`)}`);
  if (p.authHeader !== null && p.authMissing === null) {
    const prefix = env?.auth.prefix ?? "";
    // The variable is left for the shell to expand: double quotes.
    const value = env?.auth.sessionValue === true || env?.auth.env === null || env?.auth.env === undefined ? "<value typed for this session>" : `$${doubleQuoted(env.auth.env)}`;
    lines.push(`-H "${doubleQuoted(p.authHeader)}: ${doubleQuoted(prefix)}${value}"`);
  }
  if (req.multipart !== null) {
    for (const part of req.multipart) {
      if (part.filename !== undefined) lines.push(`-F ${shellQuote(`${part.name}=@${part.filename}${part.content_type !== undefined && part.content_type !== "" ? `;type=${part.content_type}` : ""}`)}`);
      else lines.push(`--form-string ${shellQuote(`${part.name}=${part.value ?? ""}`)}`);
    }
  } else if (req.body !== null && req.body !== "") {
    if (req.contentType !== "") lines.push(`-H ${shellQuote(`Content-Type: ${req.contentType}`)}`);
    lines.push(`--data-raw ${shellQuote(req.body)}`);
  }
  return lines.join(" \\\n  ");
}

/** BodyView is how the response view shows a body: always the received
 * text, re-indented only when that changes nothing but whitespace. */
export interface BodyView {
  text: string;
  /** The body is one complete JSON value. */
  json: boolean;
  /** text was re-indented (tokens as received, whitespace changed). */
  indented: boolean;
  /** Why a body that says it is JSON is shown as received, or null. */
  note: string | null;
  /** Keys that repeat inside one object, shown as received. */
  duplicateKeys: string[];
}

/** prettyBody never re-serialises a body: parsing JSON into JavaScript
 * values and printing it again would round large integers, collapse
 * repeated keys and respell numbers. A JSON body is re-indented token for
 * token (indentJson); anything else, or JSON that is cut or malformed, is
 * shown exactly as received. */
export function prettyBody(body: string, contentType: string | null): BodyView {
  const saysJson = contentType !== null && /json/i.test(contentType);
  const looksJson = saysJson || /^\s*[[{]/.test(body);
  if (!looksJson) return { text: body, json: false, indented: false, note: null, duplicateKeys: [] };
  const r = indentJson(body);
  if (!r.ok) return { text: body, json: false, indented: false, note: saysJson ? `shown as received: ${r.reason}` : null, duplicateKeys: [] };
  return { text: r.text, json: true, indented: r.text !== body, note: null, duplicateKeys: r.duplicateKeys };
}

/** opensslFingerprint writes a SHA-256 fingerprint the way
 * `openssl x509 -noout -fingerprint -sha256` prints it: upper-case hex
 * pairs separated by colons, so the two can be compared by eye. A value
 * that is not 64 hex characters is returned unchanged. */
export function opensslFingerprint(sha256: string): string {
  const hex = sha256.replace(/^sha256:/i, "");
  if (!/^[0-9a-fA-F]{64}$/.test(hex)) return sha256;
  return (hex.toUpperCase().match(/../g) ?? []).join(":");
}

/** unavailableReason says why an operation cannot be tried (OX-23), or
 * null: no environment configured or selected. A route the server cannot
 * dial (TI-7) answers its own refusal when sent. */
export function unavailableReason(envs: readonly Environment[] | null, selected: Environment | null): string | null {
  if (envs !== null && envs.length === 0) return "No environment is configured: add one in the environment bar above (a URL you type, or a suggestion you pick).";
  if (selected === null) return "No environment is selected: choose one in the environment bar above.";
  return null;
}

/** authText says where an environment's auth comes from, without a value. */
export function authText(env: Environment): string {
  const a = env.auth;
  if (a.sessionValue) return `${a.header ?? "Authorization"}: a value typed for this session`;
  if (a.env === null) return "no auth configured";
  const prefix = (a.prefix ?? "").trim();
  return `${prefix !== "" ? `${prefix} ` : ""}from $${a.env} · ${a.envSet ? "set" : "not set"}`;
}

/** fileToBase64 reads a picked file as standard base64 (multipart parts,
 * TI-45). */
export async function fileToBase64(file: Blob): Promise<string> {
  const buf = new Uint8Array(await file.arrayBuffer());
  let bin = "";
  const chunk = 0x8000;
  for (let i = 0; i < buf.length; i += chunk) bin += String.fromCharCode(...buf.subarray(i, i + chunk));
  return btoa(bin);
}

/** suggestedName proposes an environment name for a typed URL: its host
 * and port, as the server's redirect suggestion does. */
export function suggestedName(url: string): string {
  try {
    const u = new URL(url);
    const port = u.port !== "" ? u.port : u.protocol === "https:" ? "443" : "80";
    return `${u.hostname}-${port}`.replace(/[^A-Za-z0-9._-]/g, "-").slice(0, 64);
  } catch {
    return "";
  }
}
