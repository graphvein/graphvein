// Types, guard and requests of the Settings page's endpoint (T039, ADR 0088;
// docs/cli/serve.md "GET and POST /api/v0/config"). graphvein.yaml stays the
// source of truth: the page reads every form key with its value and origin,
// and writes only through the guarded preview-then-apply edit, with the hash
// of the file it read and of the previewed result.

import { ApiError, fetchJSON } from "@/api/client";
import { ApiFormatError, array, bool, object, str, strOrNull, version, type JSONObject, type Signal } from "@/api/endpoints";
import { readIndexSettings, type IndexSettings } from "@/api/indexSettings";

export type { IndexSettings, SettingsState } from "@/api/indexSettings";

/** CONFIG_PATH is the Settings endpoint (per member in project mode: the
 * client adds repository=). */
export const CONFIG_PATH = "/api/v0/config";

/** KeyKind is the type of a form key (the settings claim contract's key
 * rows). */
export type KeyKind = "enum" | "integer" | "boolean" | "list" | "map";

/** MapEntry is one entry of a map key, in file order. */
export interface MapEntry {
  key: string;
  value: string;
}

/** SettingValue is a key's value as the API sends it: a string (enum), a
 * number (integer), a boolean, a list of strings or a map as ordered
 * entries; null when nothing applies or the value is unknown. */
export type SettingValue = string | number | boolean | string[] | MapEntry[] | null;

/** Origin is where a key's value comes from: the file (with its line), the
 * built-in default, nothing (not set), or unknown (the file does not load,
 * or the member has no checkout here). */
export type Origin = "file" | "builtin" | "not_set" | "unknown";

/** Applies says when a written change takes effect. */
export type Applies = "at_once" | "next_index_run";

export interface ConfigKey {
  key: string;
  kind: KeyKind;
  choices: string[];
  min: number | null;
  max: number | null;
  value: SettingValue;
  default: SettingValue;
  origin: Origin;
  line: number | null;
  applies: Applies;
  editable: boolean;
  reason: string | null;
  /** The key of the file that decides instead of this one
   * (ide.link_template for ide.editor), with its line; null when none
   * does (and from a server that does not send it). */
  locked_by: { key: string; line: number } | null;
}

/** OtherKey is a key the file sets that the form does not edit. */
export interface OtherKey {
  key: string;
  line: number;
  /** The guarded edit that edits it (path values, try-it environments);
   * null when only the YAML view does. */
  edited_by: string | null;
}

export interface ProjectEnvironment {
  name: string;
  label: string;
  line: number;
  for_member: boolean;
  base_urls: { repository: string; base_url: string }[];
}

export interface ConfigProject {
  file: string;
  error: string | null;
  environments: ProjectEnvironment[];
  edited_by: string;
}

export interface Config {
  repository: string;
  file: string;
  /** null for a member without a checkout here: nothing is known. */
  exists: boolean | null;
  hash: string | null;
  text: string | null;
  editable: boolean;
  reason: string | null;
  token: string;
  config_error: string | null;
  keys: ConfigKey[];
  other_keys: OtherKey[];
  reindex: { path: string; scope: string };
  index_settings: IndexSettings | null;
  ignore: { file: string; exists: boolean | null; text: string | null; editable: boolean };
  project: ConfigProject | null;
}

/** EditResult is a preview or an applied edit. */
export interface EditResult {
  applied: boolean;
  base_hash: string;
  result_hash: string;
  diff: string;
  valid: boolean;
  problems: string[];
  changes: string[];
  reformatted: boolean;
  /** lost_comments are the comments of the file on disk the result no
   * longer has (the server compares both texts); null when the answer
   * does not say, so the page claims nothing about comments. */
  lost_comments: LostComment[] | null;
  changed_keys: { key: string; applies: Applies }[];
  other_keys_changed: boolean;
  reindex_needed: boolean;
}

/** LostComment is a comment of the file on disk, with its line there. */
export interface LostComment {
  line: number;
  text: string;
}

/** Operation is one typed edit of a form key. */
export type Operation = { op: "set"; key: string; value: Exclude<SettingValue, null> } | { op: "remove"; key: string };

/** EditRequest is either typed operations or the whole text. */
export type EditRequest = { operations: Operation[] } | { text: string };

const KINDS: readonly string[] = ["enum", "integer", "boolean", "list", "map"];
const ORIGINS: readonly string[] = ["file", "builtin", "not_set", "unknown"];

function strings(value: unknown, path: string): string[] {
  return array(value, path).map((v, i) => {
    if (typeof v !== "string") throw new ApiFormatError(`${path}[${String(i)}] is not a string`);
    return v;
  });
}

function intOrNullAt(obj: JSONObject, key: string, path: string): number | null {
  const v = obj[key];
  if (v === undefined || v === null) return null;
  if (typeof v !== "number" || !Number.isInteger(v)) throw new ApiFormatError(`${path}.${key} is not an integer`);
  return v;
}

/** parseValue checks a value against its key's kind. */
export function parseValue(value: unknown, kind: KeyKind, path: string): SettingValue {
  if (value === null || value === undefined) return null;
  switch (kind) {
    case "enum":
      if (typeof value === "string") return value;
      break;
    case "integer":
      if (typeof value === "number" && Number.isInteger(value)) return value;
      break;
    case "boolean":
      if (typeof value === "boolean") return value;
      break;
    case "list":
      return strings(value, path);
    case "map":
      return array(value, path).map((e, i) => {
        const o = object(e, `${path}[${String(i)}]`);
        return { key: str(o, "key", `${path}[${String(i)}]`), value: str(o, "value", `${path}[${String(i)}]`) };
      });
  }
  throw new ApiFormatError(`${path} is not a ${kind} value`);
}

function parseKey(value: unknown, path: string): ConfigKey {
  const o = object(value, path);
  const kind = str(o, "kind", path);
  if (!KINDS.includes(kind)) throw new ApiFormatError(`${path}.kind ${kind} is not known`);
  const k = kind as KeyKind;
  const origin = str(o, "origin", path);
  const applies = str(o, "applies", path);
  if (applies !== "at_once" && applies !== "next_index_run") throw new ApiFormatError(`${path}.applies ${applies} is not known`);
  return {
    key: str(o, "key", path),
    kind: k,
    choices: o.choices === undefined ? [] : strings(o.choices, `${path}.choices`),
    min: intOrNullAt(o, "min", path),
    max: intOrNullAt(o, "max", path),
    value: parseValue(o.value, k, `${path}.value`),
    default: parseValue(o.default, k, `${path}.default`),
    // An origin this UI does not know proves nothing: unknown.
    origin: ORIGINS.includes(origin) ? (origin as Origin) : "unknown",
    line: intOrNullAt(o, "line", path),
    applies,
    editable: bool(o, "editable", path),
    reason: strOrNull(o, "reason", path),
    locked_by: parseLock(o.locked_by, `${path}.locked_by`),
  };
}

function parseLock(value: unknown, path: string): { key: string; line: number } | null {
  if (value === null || value === undefined) return null;
  const o = object(value, path);
  const line = intOrNullAt(o, "line", path);
  if (line === null) throw new ApiFormatError(`${path}.line: a line number expected`);
  return { key: str(o, "key", path), line };
}

function boolOrNull(o: JSONObject, key: string, path: string): boolean | null {
  return o[key] === null ? null : bool(o, key, path);
}

function parseProjectPart(value: unknown, path: string): ConfigProject | null {
  if (value === null || value === undefined) return null;
  const o = object(value, path);
  return {
    file: str(o, "file", path),
    error: strOrNull(o, "error", path),
    environments: array(o.environments ?? [], `${path}.environments`).map((e, i) => {
      const p = `${path}.environments[${String(i)}]`;
      const eo = object(e, p);
      return {
        name: str(eo, "name", p),
        label: str(eo, "label", p),
        line: intOrNullAt(eo, "line", p) ?? 0,
        for_member: bool(eo, "for_member", p),
        base_urls: array(eo.base_urls ?? [], `${p}.base_urls`).map((b, j) => {
          const bo = object(b, `${p}.base_urls[${String(j)}]`);
          return { repository: str(bo, "repository", `${p}.base_urls`), base_url: str(bo, "base_url", `${p}.base_urls`) };
        }),
      };
    }),
    edited_by: str(o, "edited_by", path),
  };
}

/** parseConfig checks a GET /api/v0/config body. */
export function parseConfig(value: unknown): Config {
  const p = "config";
  const o = object(value, p);
  version(o, p);
  const ignore = object(o.ignore, `${p}.ignore`);
  const reindex = object(o.reindex, `${p}.reindex`);
  return {
    repository: str(o, "repository", p),
    file: str(o, "file", p),
    exists: boolOrNull(o, "exists", p),
    hash: strOrNull(o, "hash", p),
    text: strOrNull(o, "text", p),
    editable: bool(o, "editable", p),
    reason: strOrNull(o, "reason", p),
    token: str(o, "token", p),
    config_error: strOrNull(o, "config_error", p),
    keys: array(o.keys, `${p}.keys`).map((k, i) => parseKey(k, `${p}.keys[${String(i)}]`)),
    other_keys: array(o.other_keys, `${p}.other_keys`).map((k, i) => {
      const ko = object(k, `${p}.other_keys[${String(i)}]`);
      return { key: str(ko, "key", p), line: intOrNullAt(ko, "line", p) ?? 0, edited_by: strOrNull(ko, "edited_by", p) };
    }),
    reindex: { path: str(reindex, "path", `${p}.reindex`), scope: str(reindex, "scope", `${p}.reindex`) },
    index_settings: o.index_settings === null || o.index_settings === undefined ? null : readIndexSettings(o.index_settings, `${p}.index_settings`, (m) => new ApiFormatError(m)),
    ignore: {
      file: str(ignore, "file", `${p}.ignore`),
      exists: boolOrNull(ignore, "exists", `${p}.ignore`),
      text: strOrNull(ignore, "text", `${p}.ignore`),
      editable: bool(ignore, "editable", `${p}.ignore`),
    },
    project: parseProjectPart(o.project, `${p}.project`),
  };
}

/** parseEditResult checks a POST answer. */
export function parseEditResult(value: unknown): EditResult {
  const p = "config edit";
  const o = object(value, p);
  version(o, p);
  return {
    applied: bool(o, "applied", p),
    base_hash: str(o, "base_hash", p),
    result_hash: str(o, "result_hash", p),
    diff: str(o, "diff", p),
    valid: bool(o, "valid", p),
    problems: strings(o.problems ?? [], `${p}.problems`),
    changes: strings(o.changes ?? [], `${p}.changes`),
    reformatted: bool(o, "reformatted", p),
    lost_comments:
      o.lost_comments === undefined || o.lost_comments === null
        ? null
        : array(o.lost_comments, `${p}.lost_comments`).map((c, i) => {
            const at = `${p}.lost_comments[${String(i)}]`;
            const co = object(c, at);
            const line = intOrNullAt(co, "line", at);
            if (line === null) throw new ApiFormatError(`${at}.line is missing`);
            return { line, text: str(co, "text", at) };
          }),
    changed_keys: array(o.changed_keys ?? [], `${p}.changed_keys`).map((c, i) => {
      const co = object(c, `${p}.changed_keys[${String(i)}]`);
      const applies = str(co, "applies", p);
      return { key: str(co, "key", p), applies: applies === "at_once" ? "at_once" : "next_index_run" };
    }),
    other_keys_changed: bool(o, "other_keys_changed", p),
    reindex_needed: bool(o, "reindex_needed", p),
  };
}

function typed<T>(parse: (v: unknown) => T, body: unknown): T {
  try {
    return parse(body);
  } catch (err) {
    if (err instanceof ApiFormatError) throw new ApiError("format", err.message);
    throw err;
  }
}

/** ConfigClient reads and edits graphvein.yaml; tests pass their own. */
export interface ConfigClient {
  fetch: (options: Signal) => Promise<Config>;
  /** edit previews (resultHash undefined) or applies the previewed result.
   * A refusal rejects with the server's ApiError (409 config_changed,
   * preview_changed, ...). */
  edit: (config: Config, request: EditRequest, resultHash?: string) => Promise<EditResult>;
}

/** httpConfigClient talks to the server that served the page. */
export const httpConfigClient: ConfigClient = {
  fetch: async (options) => typed(parseConfig, await fetchJSON(CONFIG_PATH, options)),
  edit: async (config, request, resultHash) => {
    const body = { base_hash: config.hash ?? "absent", ...request, ...(resultHash === undefined ? {} : { apply: true, result_hash: resultHash }) };
    return typed(parseEditResult, await fetchJSON(CONFIG_PATH, { method: "POST", body, headers: { "X-Graphvein-Token": config.token }, timeoutMs: 60_000 }));
  },
};
