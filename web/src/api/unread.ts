// Types, guard and request of the "What Graphvein couldn't read" page
// (docs/cli/serve.md "GET /api/v0/unread", claim contract couldnt-read,
// ADR 0089). One repository answers its page; a project without
// repository= answers every member's page and the project totals.

import { array, bool, getJSON, intOrNull, object, parseIde, parseIdeStatus, parseIndex, str, strOrNull, version, type IdeLink, type IdeStatus, type IndexRun, type JSONObject, type Scope, type Signal } from "@/api/endpoints";

/** UnreadList is a bounded list (row CR-4): total is never capped. */
export interface UnreadList<T> {
  total: number;
  truncated: boolean;
  items: T[];
}

/** UnreadPart says whether a part could be computed, and why not. */
export interface UnreadPart {
  available: boolean;
  reason: string | null;
  error: string | null;
}

/** UnreadLocated is what every item carries to link it to code. */
export interface UnreadLocated {
  file: string;
  line: number | null;
  ide: IdeLink | null;
}

export interface UnplacedRoute extends UnreadLocated {
  route: string;
  method: string | null;
  pattern: string | null;
  reason: string;
  /** The unknown part's words, when the server names one. */
  unknown: string | null;
  /** The route's unknown-prefix path as the API view shows it ("…/x"),
   * with its reason in words; null when the route has none (row CR-6). */
  prefix_unknown: { path: string; reason_text: string } | null;
}

export interface UnreadMount extends UnreadLocated {
  key: string;
  hop: string;
  routes: number;
  framework: string | null;
}

export interface DynamicPattern extends UnreadLocated {
  code: string;
  expression: string | null;
  message: string;
}

export interface RouterForms extends UnreadLocated {
  message: string;
  forms: number | null;
  by_form: Record<string, number> | null;
}

export interface UnreadCall extends UnreadLocated {
  id: string;
  name: string;
  status: string;
  owner: string | null;
}

export interface UnreadRoute extends UnreadLocated {
  id: string;
  label: string;
  method: string | null;
  sections: string[];
  depth_cap: number | null;
  depth_limit: number | null;
}

/** File categories (row CR-36), in the order of row CR-37. */
export type FileCategory = "code_with_reader" | "code_no_reader" | "not_code" | "other";

export const FILE_CATEGORIES: readonly FileCategory[] = ["code_with_reader", "code_no_reader", "not_code", "other"];

function category(value: unknown): FileCategory {
  return typeof value === "string" && (FILE_CATEGORIES as readonly string[]).includes(value) ? (value as FileCategory) : "other";
}

export interface UnreadFile extends UnreadLocated {
  category: FileCategory;
  /** False for a path the scanner found and could not read (no record). */
  stored: boolean;
  code: string;
  message: string;
  test: boolean | null;
}

export interface ScanGap {
  path: string;
  code: string;
  message: string;
}

export interface FileWithoutFacts extends UnreadLocated {
  extension: string;
  category: FileCategory;
  language: string;
  test: boolean | null;
}

export interface UnresolvedFile extends UnreadLocated {
  external: number;
  unknown: number;
  test: boolean | null;
}

/** The count keys of `counts` (docs/cli/serve.md); null where the part
 * is not available. */
export type UnreadCounts = Record<string, number | null>;

/** UnreadPage is one repository's page. */
export interface UnreadPage {
  repository: string;
  scope: string;
  ide: IdeStatus;
  index: IndexRun | null;
  builtin_run: boolean;
  config: string;
  config_error: string | null;
  counts: UnreadCounts;
  route_paths: {
    document: UnreadPart;
    unplaced: UnreadList<UnplacedRoute>;
    mounts: UnreadList<UnreadMount>;
    dynamic: UnreadList<DynamicPattern>;
    router_forms: UnreadList<RouterForms>;
    /** Go route recognition failures (row CR-41), message verbatim. */
    failed: UnreadList<DynamicPattern>;
    /** Mounts whose router is not resolved: no path claimed (row CR-42). */
    mounts_unresolved: UnreadList<DynamicPattern>;
    /** Route registrations whose path is withheld: the route is found,
     * no path claimed (row CR-44). */
    routes_withheld: UnreadList<DynamicPattern>;
    forms: number | null;
    form_files_not_counted: number | null;
  };
  client_calls: { dynamic: UnreadList<UnreadCall>; wrapper: UnreadList<UnreadCall> };
  shapes: {
    routes: number;
    without_shape: UnreadList<UnreadRoute>;
    shape_invalid: UnreadList<UnreadRoute>;
    sections: Record<string, number>;
    sections_not_read: UnreadList<UnreadRoute>;
  };
  depth_caps: { routes: UnreadList<UnreadRoute> };
  files: {
    scope_applied: boolean;
    not_read: UnreadList<UnreadFile> & { by_category: { category: FileCategory; count: number }[] };
    syntax_errors: UnreadList<UnreadFile>;
    /** Files with a record dropped (row CR-40). */
    records_dropped: UnreadList<UnreadFile>;
    /** TypeScript structure outside the claim contract (row CR-43). */
    structure_outside_contract: UnreadList<UnreadFile>;
    scanner_gaps: UnreadList<ScanGap>;
    without_facts: UnreadPart & {
      by_extension: { extension: string; count: number }[];
      by_category: { category: FileCategory; count: number }[];
      files: UnreadList<FileWithoutFacts>;
    };
  };
  code_calls: UnreadPart & {
    languages: string[];
    external: number | null;
    unknown: number | null;
    files_counted: number | null;
    files_not_counted: number | null;
    files: UnreadList<UnresolvedFile>;
  };
}

/** UnreadProject is the project-scope body (row CR-29). */
export interface UnreadProject {
  project: string;
  scope: string;
  totals: UnreadCounts;
  /** Per count, the members it is not available for. */
  not_available: Record<string, string[]>;
  members: UnreadPage[];
}

export type UnreadAnswer = { kind: "page"; page: UnreadPage } | { kind: "project"; project: UnreadProject };

function num(obj: JSONObject, key: string, path: string): number {
  const v = intOrNull(obj, key, path);
  if (v === null) throw new Error(`${path}.${key} is not an integer`);
  return v;
}

function optInt(obj: JSONObject, key: string, path: string): number | null {
  return obj[key] === undefined ? null : intOrNull(obj, key, path);
}

function optStr(obj: JSONObject, key: string, path: string): string | null {
  return obj[key] === undefined ? null : strOrNull(obj, key, path);
}

// An older server sends no prefix_unknown: read as null, so the page
// says the path is not known rather than showing the pattern as one.
function prefixUnknown(value: unknown, path: string): { path: string; reason_text: string } | null {
  if (value === null || value === undefined) return null;
  const o = object(value, path);
  return { path: str(o, "path", path), reason_text: str(o, "reason_text", path) };
}

function located(obj: JSONObject, path: string, fileKey = "file"): UnreadLocated {
  return { file: str(obj, fileKey, path), line: optInt(obj, "line", path), ide: parseIde(obj.ide, `${path}.ide`) };
}

function list<T>(value: unknown, path: string, each: (o: JSONObject, p: string) => T): UnreadList<T> {
  const obj = object(value, path);
  return {
    total: num(obj, "total", path),
    truncated: bool(obj, "truncated", path),
    items: array(obj.items, `${path}.items`).map((v, i) => {
      const p = `${path}.items[${String(i)}]`;
      return each(object(v, p), p);
    }),
  };
}

function part(obj: JSONObject, path: string): UnreadPart {
  return { available: bool(obj, "available", path), reason: strOrNull(obj, "reason", path), error: optStr(obj, "error", path) };
}

function counts(value: unknown, path: string): UnreadCounts {
  const obj = object(value, path);
  const out: UnreadCounts = {};
  for (const k of Object.keys(obj)) out[k] = intOrNull(obj, k, path);
  return out;
}

function categoryCounts(value: unknown, path: string): { category: FileCategory; count: number }[] {
  return array(value, path).map((v, i) => {
    const p = `${path}[${String(i)}]`;
    const o = object(v, p);
    return { category: category(o.category), count: num(o, "count", p) };
  });
}

function route(o: JSONObject, p: string): UnreadRoute {
  return {
    ...located(o, p),
    id: str(o, "id", p),
    label: str(o, "label", p),
    method: optStr(o, "method", p),
    sections: o.sections === undefined ? [] : array(o.sections, `${p}.sections`).filter((s): s is string => typeof s === "string"),
    depth_cap: optInt(o, "depth_cap", p),
    depth_limit: optInt(o, "depth_limit", p),
  };
}

function call(o: JSONObject, p: string): UnreadCall {
  const owner = o.owner === null || o.owner === undefined ? null : object(o.owner, `${p}.owner`);
  return { ...located(o, p), id: str(o, "id", p), name: str(o, "name", p), status: str(o, "status", p), owner: owner === null ? null : str(owner, "qualified_name", `${p}.owner`) };
}

/** diagnosticItem reads a stored route diagnostic item (rows CR-9,
 * CR-41, CR-42, CR-44). */
function diagnosticItem(o: JSONObject, q: string): DynamicPattern {
  return { ...located(o, q), code: str(o, "code", q), expression: optStr(o, "expression", q), message: str(o, "message", q) };
}

function unreadFile(o: JSONObject, p: string): UnreadFile {
  return {
    ...located(o, p),
    category: category(o.category),
    stored: o.stored === undefined ? true : bool(o, "stored", p),
    code: str(o, "code", p),
    message: str(o, "message", p),
    test: o.test === null || o.test === undefined ? null : bool(o, "test", p),
  };
}

function byForm(value: unknown, path: string): Record<string, number> | null {
  if (value === null || value === undefined) return null;
  const obj = object(value, path);
  const out: Record<string, number> = {};
  for (const k of Object.keys(obj)) out[k] = num(obj, k, path);
  return out;
}

/** parsePage checks one repository's page. */
export function parsePage(value: unknown, p = "unread"): UnreadPage {
  const obj = object(value, p);
  const rp = object(obj.route_paths, `${p}.route_paths`);
  const cc = object(obj.client_calls, `${p}.client_calls`);
  const sh = object(obj.shapes, `${p}.shapes`);
  const fi = object(obj.files, `${p}.files`);
  const nr = object(fi.not_read, `${p}.files.not_read`);
  const wf = object(fi.without_facts, `${p}.files.without_facts`);
  const code = object(obj.code_calls, `${p}.code_calls`);
  const sections: Record<string, number> = {};
  const so = object(sh.sections, `${p}.shapes.sections`);
  for (const k of Object.keys(so)) sections[k] = num(so, k, `${p}.shapes.sections`);
  return {
    repository: str(obj, "repository", p),
    scope: str(obj, "scope", p),
    ide: parseIdeStatus(obj.ide, `${p}.ide`),
    index: parseIndex(obj.index, `${p}.index`),
    builtin_run: bool(obj, "builtin_run", p),
    config: str(obj, "config", p),
    config_error: strOrNull(obj, "config_error", p),
    counts: counts(obj.counts, `${p}.counts`),
    route_paths: {
      document: part(object(rp.document, `${p}.route_paths.document`), `${p}.route_paths.document`),
      unplaced: list(rp.unplaced, `${p}.route_paths.unplaced`, (o, q) => {
        const unknown = o.unknown === null || o.unknown === undefined ? null : object(o.unknown, `${q}.unknown`);
        return {
          ...located(o, q),
          route: str(o, "route", q),
          method: optStr(o, "method", q),
          pattern: optStr(o, "pattern", q),
          reason: str(o, "reason", q),
          unknown: unknown !== null && typeof unknown.text === "string" ? unknown.text : null,
          prefix_unknown: prefixUnknown(o.prefix_unknown, `${q}.prefix_unknown`),
        };
      }),
      mounts: list(rp.mounts, `${p}.route_paths.mounts`, (o, q) => ({ ...located(o, q), key: str(o, "key", q), hop: str(o, "hop", q), routes: num(o, "routes", q), framework: optStr(o, "framework", q) })),
      dynamic: list(rp.dynamic, `${p}.route_paths.dynamic`, diagnosticItem),
      failed: list(rp.failed, `${p}.route_paths.failed`, diagnosticItem),
      mounts_unresolved: list(rp.mounts_unresolved, `${p}.route_paths.mounts_unresolved`, diagnosticItem),
      routes_withheld: list(rp.routes_withheld, `${p}.route_paths.routes_withheld`, diagnosticItem),
      router_forms: list(rp.router_forms, `${p}.route_paths.router_forms`, (o, q) => ({ ...located(o, q), message: str(o, "message", q), forms: optInt(o, "forms", q), by_form: byForm(o.by_form, `${q}.by_form`) })),
      forms: optInt(rp, "forms", `${p}.route_paths`),
      form_files_not_counted: optInt(rp, "form_files_not_counted", `${p}.route_paths`),
    },
    client_calls: { dynamic: list(cc.dynamic, `${p}.client_calls.dynamic`, call), wrapper: list(cc.wrapper, `${p}.client_calls.wrapper`, call) },
    shapes: {
      routes: num(sh, "routes", `${p}.shapes`),
      without_shape: list(sh.without_shape, `${p}.shapes.without_shape`, route),
      shape_invalid: list(sh.shape_invalid, `${p}.shapes.shape_invalid`, route),
      sections,
      sections_not_read: list(sh.sections_not_read, `${p}.shapes.sections_not_read`, route),
    },
    depth_caps: { routes: list(object(obj.depth_caps, `${p}.depth_caps`).routes, `${p}.depth_caps.routes`, route) },
    files: {
      scope_applied: bool(fi, "scope_applied", `${p}.files`),
      not_read: { ...list(nr, `${p}.files.not_read`, unreadFile), by_category: nr.by_category === undefined ? [] : categoryCounts(nr.by_category, `${p}.files.not_read.by_category`) },
      syntax_errors: list(fi.syntax_errors, `${p}.files.syntax_errors`, unreadFile),
      records_dropped: list(fi.records_dropped, `${p}.files.records_dropped`, unreadFile),
      structure_outside_contract: list(fi.structure_outside_contract, `${p}.files.structure_outside_contract`, unreadFile),
      scanner_gaps: list(fi.scanner_gaps, `${p}.files.scanner_gaps`, (o, q) => ({ path: str(o, "path", q), code: str(o, "code", q), message: str(o, "message", q) })),
      without_facts: {
        ...part(wf, `${p}.files.without_facts`),
        by_extension: array(wf.by_extension, `${p}.files.without_facts.by_extension`).map((v, i) => {
          const q = `${p}.files.without_facts.by_extension[${String(i)}]`;
          const o = object(v, q);
          return { extension: str(o, "extension", q), count: num(o, "count", q) };
        }),
        by_category: wf.by_category === undefined ? [] : categoryCounts(wf.by_category, `${p}.files.without_facts.by_category`),
        files: list(wf.files, `${p}.files.without_facts.files`, (o, q) => ({
          ...located(o, q),
          extension: str(o, "extension", q),
          category: category(o.category),
          language: str(o, "language", q),
          test: o.test === null || o.test === undefined ? null : bool(o, "test", q),
        })),
      },
    },
    code_calls: {
      ...part(code, `${p}.code_calls`),
      languages: array(code.languages, `${p}.code_calls.languages`).filter((s): s is string => typeof s === "string"),
      external: optInt(code, "external", `${p}.code_calls`),
      unknown: optInt(code, "unknown", `${p}.code_calls`),
      files_counted: optInt(code, "files_counted", `${p}.code_calls`),
      files_not_counted: optInt(code, "files_not_counted", `${p}.code_calls`),
      files: list(code.files, `${p}.code_calls.files`, (o, q) => ({
        ...located(o, q),
        external: num(o, "external", q),
        unknown: num(o, "unknown", q),
        test: o.test === null || o.test === undefined ? null : bool(o, "test", q),
      })),
    },
  };
}

/** parseUnread checks either body of GET /api/v0/unread. */
export function parseUnread(value: unknown): UnreadAnswer {
  const p = "unread";
  const obj = object(value, p);
  version(obj, p);
  if (typeof obj.project === "string") {
    const na = object(obj.not_available, `${p}.not_available`);
    const notAvailable: Record<string, string[]> = {};
    for (const k of Object.keys(na)) notAvailable[k] = array(na[k], `${p}.not_available.${k}`).filter((s): s is string => typeof s === "string");
    return {
      kind: "project",
      project: {
        project: obj.project,
        scope: str(obj, "scope", p),
        totals: counts(obj.totals, `${p}.totals`),
        not_available: notAvailable,
        members: array(obj.members, `${p}.members`).map((m, i) => parsePage(m, `${p}.members[${String(i)}]`)),
      },
    };
  }
  return { kind: "page", page: parsePage(obj, p) };
}

/** UNREAD_LIMIT is the list bound the page asks for: the server's
 * maximum (row CR-4), so most lists are complete; counts are never capped. */
export const UNREAD_LIMIT = 2000;

export interface UnreadParams {
  scope: Scope;
  /** Project mode: a member's page, or "all" for every member and the
   * project totals. Absent on a single repository. */
  member?: string;
  editor?: string;
}

export function unreadPath(p: UnreadParams): string {
  const q = new URLSearchParams();
  q.set("scope", p.scope);
  q.set("limit", String(UNREAD_LIMIT));
  if (p.editor !== undefined) q.set("editor", p.editor);
  if (p.member !== undefined && p.member !== "all") q.set("repository", p.member);
  return `/api/v0/unread?${q.toString()}`;
}

export type UnreadFetcher = (p: UnreadParams, options: Signal) => Promise<UnreadAnswer>;

export const fetchUnread: UnreadFetcher = (p, o) => getJSON(unreadPath(p), parseUnread, o);
