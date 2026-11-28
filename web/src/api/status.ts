// Types and a hand-written guard for GET /api/v0/status (docs/cli/serve.md).
// Field names mirror the JSON so the types read like the API documentation.

import { readIndexSettings, type IndexSettings } from "@/api/indexSettings";

/** The freshness states the UI understands. The server may add states in
 * later versions; parseStatus maps anything else to "unknown", so an
 * unexpected answer is never shown as fresh. */
export type FreshnessState = "fresh" | "stale" | "not_indexed" | "unknown";

export interface Extractor {
  current: string;
  indexed: string | null;
  matches: boolean | null;
}

export interface LastRun {
  finished_at: string;
  indexer: { name: string; version: string };
  contract_version: number;
  commit?: string;
}

export interface Freshness {
  state: FreshnessState;
  /** The state exactly as the server sent it; differs from state only when
   * the server sent something this UI does not know. */
  reported_state: string;
  checked_at: string;
  changed_count: number | null;
  changed_files: string[];
  changed_files_truncated: boolean;
  error?: string;
  /** Whether graphvein.yaml's index settings changed since the last run
   * (ADR 0088); absent when the server did not send it, which proves
   * nothing either way. */
  settings?: IndexSettings;
}

export interface Status {
  api_version: number;
  repository: string;
  server_version: string;
  extractor: Extractor;
  /** parse_failed counts the files the run could not parse (T074); the
   * server omits it when zero, and so does this type. */
  index: { indexed: boolean; last_run: LastRun | null; parse_failed?: number };
  freshness: Freshness;
}

/** StatusFormatError reports a status body that does not have the documented
 * shape. The UI treats it like a failed request. */
export class StatusFormatError extends Error {
  constructor(message: string) {
    super(`malformed status response: ${message}`);
    this.name = "StatusFormatError";
  }
}

/** StatusVersionError reports a status of another API version. It is a
 * StatusFormatError (the UI cannot read it) whose message names the
 * versions instead of calling the body malformed. */
export class StatusVersionError extends StatusFormatError {
  constructor(message: string) {
    super(message);
    this.message = message;
    this.name = "StatusVersionError";
  }
}

type JSONObject = Record<string, unknown>;

function isObject(value: unknown): value is JSONObject {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function object(value: unknown, path: string): JSONObject {
  if (!isObject(value)) throw new StatusFormatError(`${path} is not an object`);
  return value;
}

function string(obj: JSONObject, key: string, path: string): string {
  const value = obj[key];
  if (typeof value !== "string") throw new StatusFormatError(`${path}.${key} is not a string`);
  return value;
}

function stringOrNull(obj: JSONObject, key: string, path: string): string | null {
  const value = obj[key];
  if (value === null) return null;
  if (typeof value !== "string") throw new StatusFormatError(`${path}.${key} is not a string or null`);
  return value;
}

function number(obj: JSONObject, key: string, path: string): number {
  const value = obj[key];
  if (typeof value !== "number" || !Number.isFinite(value)) {
    throw new StatusFormatError(`${path}.${key} is not a number`);
  }
  return value;
}

function boolean(obj: JSONObject, key: string, path: string): boolean {
  const value = obj[key];
  if (typeof value !== "boolean") throw new StatusFormatError(`${path}.${key} is not a boolean`);
  return value;
}

function parseExtractor(value: unknown): Extractor {
  const obj = object(value, "extractor");
  const matches = obj.matches;
  if (matches !== null && typeof matches !== "boolean") {
    throw new StatusFormatError("extractor.matches is not a boolean or null");
  }
  return {
    current: string(obj, "current", "extractor"),
    indexed: stringOrNull(obj, "indexed", "extractor"),
    matches,
  };
}

function parseLastRun(value: unknown): LastRun | null {
  if (value === null) return null;
  const obj = object(value, "index.last_run");
  const indexer = object(obj.indexer, "index.last_run.indexer");
  const run: LastRun = {
    finished_at: string(obj, "finished_at", "index.last_run"),
    indexer: {
      name: string(indexer, "name", "index.last_run.indexer"),
      version: string(indexer, "version", "index.last_run.indexer"),
    },
    contract_version: number(obj, "contract_version", "index.last_run"),
  };
  if (obj.commit !== undefined) run.commit = string(obj, "commit", "index.last_run");
  return run;
}

const KNOWN_STATES: readonly string[] = ["fresh", "stale", "not_indexed", "unknown"];

function parseFreshness(value: unknown): Freshness {
  const obj = object(value, "freshness");
  const reported = string(obj, "state", "freshness");
  const count = obj.changed_count;
  if (count !== null && (typeof count !== "number" || !Number.isInteger(count) || count < 0)) {
    throw new StatusFormatError("freshness.changed_count is not a count or null");
  }
  const files = obj.changed_files;
  if (!Array.isArray(files) || !files.every((f) => typeof f === "string")) {
    throw new StatusFormatError("freshness.changed_files is not a list of paths");
  }
  const state: FreshnessState = KNOWN_STATES.includes(reported) ? (reported as FreshnessState) : "unknown";
  const freshness: Freshness = {
    state,
    reported_state: reported,
    checked_at: string(obj, "checked_at", "freshness"),
    changed_count: count,
    changed_files: files,
    changed_files_truncated: boolean(obj, "changed_files_truncated", "freshness"),
  };
  if (obj.error !== undefined) freshness.error = string(obj, "error", "freshness");
  if (obj.settings !== undefined && obj.settings !== null) {
    freshness.settings = readIndexSettings(obj.settings, "freshness.settings", (m) => new StatusFormatError(m));
  }
  return freshness;
}

/** API_VERSION is the HTTP API version this UI reads (docs/cli/serve.md). */
export const API_VERSION = 0;

/** parseStatus checks a decoded status body and returns it typed. Missing
 * or mistyped fields and an api_version other than API_VERSION throw
 * StatusFormatError; an unrecognised freshness state becomes "unknown". */
export function parseStatus(value: unknown): Status {
  const obj = object(value, "status");
  // Checked first: another API version may have another shape, and "the
  // server speaks version 1" is clearer than the first missing field.
  const apiVersion = number(obj, "api_version", "status");
  if (apiVersion !== API_VERSION) {
    throw new StatusVersionError(
      `the server speaks API version ${String(apiVersion)}; this UI reads version ${String(API_VERSION)}`,
    );
  }
  const index = object(obj.index, "index");
  const status: Status = {
    api_version: apiVersion,
    repository: string(obj, "repository", "status"),
    server_version: string(obj, "server_version", "status"),
    extractor: parseExtractor(obj.extractor),
    index: {
      indexed: boolean(index, "indexed", "index"),
      last_run: parseLastRun(index.last_run),
      ...(index.parse_failed !== undefined ? { parse_failed: number(index, "parse_failed", "index") } : {}),
    },
    freshness: parseFreshness(obj.freshness),
  };
  if (status.freshness.state === "fresh" && !freshIsConsistent(status)) {
    status.freshness.state = "unknown";
  }
  return status;
}

/** freshIsConsistent checks everything a "fresh" answer implies under the
 * state table of docs/cli/serve.md (and internal/server/status.go): runs
 * exist (index.indexed true, last_run present), nothing changed (count 0,
 * no paths, list not truncated) and the check did not fail (no error, which
 * appears only for "unknown"). An answer that says "fresh" but breaks any
 * of these contradicts itself, so it is not trusted. */
function freshIsConsistent(status: Status): boolean {
  const f = status.freshness;
  return (
    status.index.indexed &&
    status.index.last_run !== null &&
    f.changed_count === 0 &&
    f.changed_files.length === 0 &&
    !f.changed_files_truncated &&
    f.error === undefined
  );
}
