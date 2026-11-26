// Types, guards and requests for the "try it" endpoints (T029, ADR 0081;
// docs/cli/serve.md "Try it endpoints", claim rows TI-* of
// docs/contract/claims/try-it.md). The browser never sends a URL: it names
// an environment, a stored route and one of its path keys, and values; the
// server builds the request, applies every guardrail and answers with the
// upstream response as data. The UI only shows what the server decided:
// confirmations, the production acknowledgement and missing auth are all
// enforced by the server, so a UI bug can never skip one.

import { ApiError, fetchJSON, requestRepository } from "@/api/client";
import { ApiFormatError, array, getJSON, object, str, strOrNull, type JSONObject } from "@/api/endpoints";

export interface EnvAuth {
  header: string | null;
  env: string | null;
  envSet: boolean;
  prefix: string | null;
  /** A value typed for this server session is held (never shown). */
  sessionValue: boolean;
}

export interface Pin {
  origin: string | null;
  sha256: string;
}

export interface EnvLimits {
  request_bytes: number | null;
  response_bytes: number | null;
  timeout_seconds: number | null;
}

export interface EnvOptions {
  tls: { pins: Pin[]; ca_file: string | null } | null;
  use_system_proxy: boolean;
  limits: EnvLimits | null;
}

export interface Limits {
  request_bytes: number;
  response_bytes: number;
  timeout_seconds: number;
}

export interface Environment {
  id: string;
  name: string;
  label: string;
  production: boolean;
  /** "repository" or "project". */
  source: string;
  file: string;
  line: number;
  baseUrl: string;
  origin: string;
  auth: EnvAuth;
  selected: boolean;
  acknowledged: boolean;
  remembered: boolean;
  options: EnvOptions;
  effectiveLimits: Limits;
}

export interface EditFile {
  /** "repository" or "project". */
  target: string;
  file: string;
  exists: boolean;
  hash: string;
  writable: boolean;
}

export interface Environments {
  repository: string;
  token: string;
  selected: string | null;
  environments: Environment[];
  files: EditFile[];
  configError: string | null;
  limits: { request_body_bytes: number; response_body_bytes: number; timeout_seconds: number; confirmation_ttl_seconds: number };
  ceilings: Limits;
}

export interface Suggestion {
  url: string;
  sourceKind: string;
  row: string;
  file: string | null;
  line: number | null;
  reason: string;
}

export interface Suggestions {
  repository: string;
  notice: string;
  suggestions: Suggestion[];
}

export interface EditAnswer {
  applied: boolean;
  target: string;
  file: string;
  baseHash: string;
  resultHash: string;
  diff: string;
  valid: boolean;
  problems: string[];
  change: string;
  environment: string;
}

export interface SelectAnswer {
  selected: string | null;
  production: boolean;
  acknowledged: boolean;
  acknowledgementRequired: boolean;
  remembered: boolean;
}

export interface AuthAnswer {
  environment: string;
  header: string | null;
  sessionValue: boolean;
}

export interface Pair {
  name: string;
  value: string;
}

export interface Part {
  name: string;
  value?: string;
  data_base64?: string;
  filename?: string;
  content_type?: string;
}

/** TryRequest is the body of prepare and send (repository is added by the
 * client in project mode). An undefined field is left out of the JSON. */
export interface TryRequest {
  environment: string;
  route: string;
  path: string;
  method: string;
  path_values?: Record<string, string> | undefined;
  free_form?: boolean | undefined;
  query?: Pair[] | undefined;
  headers?: Pair[] | undefined;
  body?: string | null | undefined;
  content_type?: string | undefined;
  multipart?: Part[] | null | undefined;
  send_without_auth?: boolean | undefined;
  confirmation?: string | undefined;
  remember?: boolean | undefined;
}

/** The reasons prepare gives for a confirmation (TI-9, TI-26, TI-27). */
/** method, method_override, body_on_get_or_head or free_form_path; another
 * value is shown raw. */
export type ConfirmationReason = string;

export interface Prepared {
  environment: string;
  label: string;
  production: boolean;
  method: string;
  url: string;
  headers: string[];
  freeForm: boolean;
  authHeader: string | null;
  authSource: string | null;
  authMissing: { header: string; env: string } | null;
  sendWithoutAuth: boolean;
  bodyBytes: number;
  bodySha256: string;
  multipart: { name: string; filename: string | null; content_type: string | null; bytes: number }[] | null;
  limits: Limits;
  proxy: string | null;
  proxyCleartext: boolean;
  confirmationRequired: boolean;
  confirmationReasons: ConfirmationReason[];
  confirmation: string | null;
  expiresAt: string | null;
  acknowledgementRequired: boolean;
  selected: boolean;
}

export interface LimitHint {
  /** request_bytes, response_bytes or timeout_seconds. */
  limit: string;
  value: number;
  ceiling: number;
  hint: string;
}

export interface RedirectFollow {
  url: string;
  origin: string;
  sameOrigin: boolean;
  method: string;
  keepsBody: boolean;
  environment: string | null;
  path: string;
  query: Pair[];
  suggestedEnvironment: { name: string; base_url: string } | null;
}

export interface Sent {
  environment: string;
  method: string;
  url: string;
  status: number;
  statusText: string;
  headers: Pair[];
  body: string;
  /** "text" or "hex" (the first 4 KiB of a body that is not text). */
  bodyEncoding: string;
  bodyBytes: number;
  truncated: boolean;
  truncatedReason: string | null;
  truncatedLimit: LimitHint | null;
  redirect: string | null;
  redirectFollow: RedirectFollow | null;
  durationMs: number;
  remembered: boolean;
}

/** CertificateDetails are the details of upstream_certificate_untrusted
 * and upstream_certificate_changed (TI-29). */
export interface CertificateDetails {
  environment: string;
  origin: string;
  certificate: { subject: string; issuer: string; sans: string[]; not_before: string; not_after: string; sha256: string; self_signed: boolean };
  reason: string;
  pinnedSha256: string[];
}

function num(o: JSONObject, key: string, path: string): number {
  const v = o[key];
  if (typeof v !== "number") throw new ApiFormatError(`${path}.${key} is not a number`);
  return v;
}

function numOrNull(o: JSONObject, key: string, path: string): number | null {
  const v = o[key];
  if (v === null || v === undefined) return null;
  if (typeof v !== "number") throw new ApiFormatError(`${path}.${key} is not a number or null`);
  return v;
}

function bool(o: JSONObject, key: string, path: string): boolean {
  const v = o[key];
  if (typeof v !== "boolean") throw new ApiFormatError(`${path}.${key} is not a boolean`);
  return v;
}

function optStr(o: JSONObject, key: string, path: string): string | null {
  return o[key] === undefined ? null : strOrNull(o, key, path);
}

function strings(v: unknown, path: string): string[] {
  if (v === null || v === undefined) return [];
  return array(v, path).map((s, i) => {
    if (typeof s !== "string") throw new ApiFormatError(`${path}[${String(i)}] is not a string`);
    return s;
  });
}

function pairs(v: unknown, path: string): Pair[] {
  if (v === null || v === undefined) return [];
  return array(v, path).map((x, i) => {
    const o = object(x, `${path}[${String(i)}]`);
    return { name: str(o, "name", path), value: str(o, "value", path) };
  });
}

function limits(v: unknown, path: string): Limits {
  const o = object(v, path);
  return { request_bytes: num(o, "request_bytes", path), response_bytes: num(o, "response_bytes", path), timeout_seconds: num(o, "timeout_seconds", path) };
}

function options(v: unknown, path: string): EnvOptions {
  const o = object(v, path);
  let tls: EnvOptions["tls"] = null;
  if (o.tls !== null && o.tls !== undefined) {
    const t = object(o.tls, `${path}.tls`);
    tls = {
      pins: (t.pins === null || t.pins === undefined ? [] : array(t.pins, `${path}.tls.pins`)).map((p, i) => {
        const po = object(p, `${path}.tls.pins[${String(i)}]`);
        return { origin: optStr(po, "origin", path), sha256: str(po, "sha256", path) };
      }),
      ca_file: optStr(t, "ca_file", `${path}.tls`),
    };
  }
  let l: EnvLimits | null = null;
  if (o.limits !== null && o.limits !== undefined) {
    const lo = object(o.limits, `${path}.limits`);
    l = { request_bytes: numOrNull(lo, "request_bytes", path), response_bytes: numOrNull(lo, "response_bytes", path), timeout_seconds: numOrNull(lo, "timeout_seconds", path) };
  }
  return { tls, use_system_proxy: o.use_system_proxy === true, limits: l };
}

export function parseEnvironments(value: unknown): Environments {
  const o = object(value, "environments");
  const lim = object(o.limits, "environments.limits");
  return {
    repository: str(o, "repository", "environments"),
    token: str(o, "token", "environments"),
    selected: optStr(o, "selected", "environments"),
    environments: array(o.environments, "environments.environments").map((v, i) => {
      const p = `environments.environments[${String(i)}]`;
      const e = object(v, p);
      const a = e.auth === null || e.auth === undefined ? {} : object(e.auth, `${p}.auth`);
      return {
        id: str(e, "id", p),
        name: str(e, "name", p),
        label: str(e, "label", p),
        production: bool(e, "production", p),
        source: str(e, "source", p),
        file: str(e, "file", p),
        line: num(e, "line", p),
        baseUrl: str(e, "base_url", p),
        origin: str(e, "origin", p),
        auth: { header: optStr(a, "header", p), env: optStr(a, "env", p), envSet: a.env_set === true, prefix: optStr(a, "prefix", p), sessionValue: a.session_value === true },
        selected: bool(e, "selected", p),
        acknowledged: bool(e, "acknowledged", p),
        remembered: bool(e, "remembered", p),
        options: options(e.options, `${p}.options`),
        effectiveLimits: limits(e.effective_limits, `${p}.effective_limits`),
      };
    }),
    files: array(o.files, "environments.files").map((v, i) => {
      const p = `environments.files[${String(i)}]`;
      const f = object(v, p);
      return { target: str(f, "target", p), file: str(f, "file", p), exists: bool(f, "exists", p), hash: str(f, "hash", p), writable: bool(f, "writable", p) };
    }),
    configError: optStr(o, "config_error", "environments"),
    limits: {
      request_body_bytes: num(lim, "request_body_bytes", "limits"),
      response_body_bytes: num(lim, "response_body_bytes", "limits"),
      timeout_seconds: num(lim, "timeout_seconds", "limits"),
      confirmation_ttl_seconds: num(lim, "confirmation_ttl_seconds", "limits"),
    },
    ceilings: limits(o.limit_ceilings, "environments.limit_ceilings"),
  };
}

export function parseSuggestions(value: unknown): Suggestions {
  const o = object(value, "suggestions");
  return {
    repository: str(o, "repository", "suggestions"),
    notice: str(o, "notice", "suggestions"),
    suggestions: array(o.suggestions, "suggestions.suggestions").map((v, i) => {
      const p = `suggestions.suggestions[${String(i)}]`;
      const s = object(v, p);
      return { url: str(s, "url", p), sourceKind: str(s, "source_kind", p), row: str(s, "row", p), file: optStr(s, "file", p), line: numOrNull(s, "line", p), reason: str(s, "reason", p) };
    }),
  };
}

export function parseEdit(value: unknown): EditAnswer {
  const o = object(value, "edit");
  return {
    applied: bool(o, "applied", "edit"),
    target: str(o, "target", "edit"),
    file: str(o, "file", "edit"),
    baseHash: str(o, "base_hash", "edit"),
    resultHash: str(o, "result_hash", "edit"),
    diff: typeof o.diff === "string" ? o.diff : "",
    valid: o.valid !== false,
    problems: strings(o.problems, "edit.problems"),
    change: typeof o.change === "string" ? o.change : "",
    environment: typeof o.environment === "string" ? o.environment : "",
  };
}

export function parseSelect(value: unknown): SelectAnswer {
  const o = object(value, "select");
  return {
    selected: optStr(o, "selected", "select"),
    production: o.production === true,
    acknowledged: o.acknowledged === true,
    acknowledgementRequired: o.acknowledgement_required === true,
    remembered: o.remembered === true,
  };
}

export function parseAuth(value: unknown): AuthAnswer {
  const o = object(value, "auth");
  return { environment: str(o, "environment", "auth"), header: optStr(o, "header", "auth"), sessionValue: o.session_value === true };
}

export function parsePrepared(value: unknown): Prepared {
  const o = object(value, "prepare");
  const p = "prepare";
  const am = o.auth_missing === null || o.auth_missing === undefined ? null : object(o.auth_missing, `${p}.auth_missing`);
  return {
    environment: str(o, "environment", p),
    label: str(o, "label", p),
    production: bool(o, "production", p),
    method: str(o, "method", p),
    url: str(o, "url", p),
    headers: strings(o.headers, `${p}.headers`),
    freeForm: o.free_form === true,
    authHeader: optStr(o, "auth_header", p),
    authSource: optStr(o, "auth_source", p),
    authMissing: am === null ? null : { header: str(am, "header", p), env: str(am, "env", p) },
    sendWithoutAuth: o.send_without_auth === true,
    bodyBytes: num(o, "body_bytes", p),
    bodySha256: typeof o.body_sha256 === "string" ? o.body_sha256 : "",
    multipart:
      o.multipart === null || o.multipart === undefined
        ? null
        : array(o.multipart, `${p}.multipart`).map((v, i) => {
            const m = object(v, `${p}.multipart[${String(i)}]`);
            return { name: str(m, "name", p), filename: optStr(m, "filename", p), content_type: optStr(m, "content_type", p), bytes: num(m, "bytes", p) };
          }),
    limits: limits(o.limits, `${p}.limits`),
    proxy: optStr(o, "proxy", p),
    proxyCleartext: o.proxy_cleartext === true,
    confirmationRequired: bool(o, "confirmation_required", p),
    confirmationReasons: strings(o.confirmation_reasons, `${p}.confirmation_reasons`),
    confirmation: optStr(o, "confirmation", p),
    expiresAt: optStr(o, "expires_at", p),
    acknowledgementRequired: o.acknowledgement_required === true,
    selected: o.selected !== false,
  };
}

function limitHint(v: unknown, path: string): LimitHint | null {
  if (v === null || v === undefined) return null;
  const o = object(v, path);
  return { limit: str(o, "limit", path), value: num(o, "value", path), ceiling: num(o, "ceiling", path), hint: typeof o.hint === "string" ? o.hint : "" };
}

export function parseSent(value: unknown): Sent {
  const o = object(value, "send");
  const p = "send";
  let follow: RedirectFollow | null = null;
  if (o.redirect_follow !== null && o.redirect_follow !== undefined) {
    const f = object(o.redirect_follow, `${p}.redirect_follow`);
    const se = f.suggested_environment === null || f.suggested_environment === undefined ? null : object(f.suggested_environment, `${p}.redirect_follow.suggested_environment`);
    follow = {
      url: str(f, "url", p),
      origin: str(f, "origin", p),
      sameOrigin: f.same_origin === true,
      method: str(f, "method", p),
      keepsBody: f.keeps_body === true,
      environment: optStr(f, "environment", p),
      path: str(f, "path", p),
      query: pairs(f.query, `${p}.redirect_follow.query`),
      suggestedEnvironment: se === null ? null : { name: str(se, "name", p), base_url: str(se, "base_url", p) },
    };
  }
  return {
    environment: str(o, "environment", p),
    method: str(o, "method", p),
    url: str(o, "url", p),
    status: num(o, "status", p),
    statusText: typeof o.status_text === "string" ? o.status_text : "",
    headers: pairs(o.headers, `${p}.headers`),
    body: typeof o.body === "string" ? o.body : "",
    bodyEncoding: typeof o.body_encoding === "string" ? o.body_encoding : "text",
    bodyBytes: num(o, "body_bytes", p),
    truncated: o.truncated === true,
    truncatedReason: optStr(o, "truncated_reason", p),
    truncatedLimit: limitHint(o.truncated_limit, `${p}.truncated_limit`),
    redirect: optStr(o, "redirect", p),
    redirectFollow: follow,
    durationMs: num(o, "duration_ms", p),
    remembered: o.remembered === true,
  };
}

/** certificateDetails reads the details of a certificate refusal, or null
 * when the error is another one. */
export function certificateDetails(err: unknown): (CertificateDetails & { changed: boolean }) | null {
  if (!(err instanceof ApiError) || (err.code !== "upstream_certificate_untrusted" && err.code !== "upstream_certificate_changed")) return null;
  try {
    const d = object(err.details, "details");
    const c = object(d.certificate, "details.certificate");
    return {
      changed: err.code === "upstream_certificate_changed",
      environment: str(d, "environment", "details"),
      origin: str(d, "origin", "details"),
      certificate: {
        subject: str(c, "subject", "certificate"),
        issuer: str(c, "issuer", "certificate"),
        sans: strings(c.sans, "certificate.sans"),
        not_before: str(c, "not_before", "certificate"),
        not_after: str(c, "not_after", "certificate"),
        sha256: str(c, "sha256", "certificate"),
        self_signed: c.self_signed === true,
      },
      reason: typeof d.reason === "string" ? d.reason : "",
      pinnedSha256: strings(d.pinned_sha256, "details.pinned_sha256"),
    };
  } catch {
    return null;
  }
}

/** limitDetails reads a limit named in a refusal's details
 * (body_too_large: request_bytes; upstream_timeout: timeout_seconds). */
export function limitDetails(err: unknown): LimitHint | null {
  if (!(err instanceof ApiError) || (err.code !== "body_too_large" && err.code !== "upstream_timeout")) return null;
  try {
    return limitHint(err.details, "details");
  } catch {
    return null;
  }
}

/** authMissingDetails reads the details of a 400 auth_missing. */
export function authMissingDetails(err: unknown): { header: string; env: string } | null {
  if (!(err instanceof ApiError) || err.code !== "auth_missing") return null;
  try {
    const d = object(err.details, "details");
    return { header: str(d, "header", "details"), env: str(d, "env", "details") };
  } catch {
    return null;
  }
}

export const TRYIT_ENVIRONMENTS_PATH = "/api/v0/tryit/environments";
export const TRYIT_SUGGESTIONS_PATH = "/api/v0/tryit/suggestions";
const TRYIT = "/api/v0/tryit";

type Signal = { signal?: AbortSignal };

export interface AddEnvironment {
  target: string;
  base_hash: string;
  environment: { name: string; label?: string; base_url: string; auth?: { header: string; env: string; prefix: string } };
  apply: boolean;
  result_hash: string;
}

export interface EditOptions {
  target: string;
  base_hash: string;
  options: { name: string; tls: { pins: Pin[]; ca_file: string | null } | null; use_system_proxy: boolean; limits: EnvLimits | null };
  apply: boolean;
  result_hash: string;
}

/** TryItClient is every request the try-it panel makes; tests replace
 * it with a fake that plays the server's refusals. */
export interface TryItClient {
  environments: (options: Signal) => Promise<Environments>;
  /** One named member's environments (project mode: the production
   * dialog lists every member a project environment covers). */
  memberEnvironments: (repository: string, options: Signal) => Promise<Environments>;
  suggestions: (options: Signal) => Promise<Suggestions>;
  edit: (body: AddEnvironment | EditOptions, token: string) => Promise<EditAnswer>;
  select: (body: { environment: string; acknowledge_production: boolean }, token: string) => Promise<SelectAnswer>;
  auth: (body: { environment: string; value?: string; header?: string; clear?: boolean }, token: string) => Promise<AuthAnswer>;
  prepare: (body: TryRequest, token: string) => Promise<Prepared>;
  send: (body: TryRequest, token: string) => Promise<Sent>;
}

/** withMember adds the selected member to a POST body in project mode
 * (TI-22): POST bodies name it, never the query string. */
function withMember<T extends object>(body: T): T & { repository?: string } {
  const r = requestRepository();
  return r === null ? body : { ...body, repository: r };
}

async function post<T>(path: string, body: object, token: string, parse: (v: unknown) => T, timeoutMs?: number): Promise<T> {
  return parse(await fetchJSON(path, { method: "POST", body: withMember(body), headers: { "X-Graphvein-Token": token }, ...(timeoutMs !== undefined ? { timeoutMs } : {}) }));
}

/** SEND_TIMEOUT_MS covers the longest exchange an environment may allow
 * (600 s) plus the dial: the server cuts the exchange, not the browser. */
export const SEND_TIMEOUT_MS = 615_000;

export const httpTryItClient: TryItClient = {
  environments: (options) => getJSON(TRYIT_ENVIRONMENTS_PATH, parseEnvironments, options),
  memberEnvironments: (repository, options) => getJSON(`${TRYIT_ENVIRONMENTS_PATH}?repository=${encodeURIComponent(repository)}`, parseEnvironments, options),
  suggestions: (options) => getJSON(TRYIT_SUGGESTIONS_PATH, parseSuggestions, options),
  edit: (body, token) => post(TRYIT_ENVIRONMENTS_PATH, body, token, parseEdit),
  select: (body, token) => post(`${TRYIT}/select`, body, token, parseSelect),
  auth: (body, token) => post(`${TRYIT}/auth`, body, token, parseAuth),
  prepare: (body, token) => post(`${TRYIT}/prepare`, body, token, parsePrepared),
  send: (body, token) => post(`${TRYIT}/send`, body, token, parseSent, SEND_TIMEOUT_MS),
};
