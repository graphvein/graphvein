// Minimal JSON client for the Graphvein HTTP API. It turns every way a
// request can fail into an ApiError, so callers handle one error type.

import { parseStatus, StatusFormatError, type Status } from "@/api/status";

/** How a request failed: the server could not be reached, did not answer
 * in time, answered with an error, or sent a body the UI cannot read. */
export type ApiErrorKind = "network" | "timeout" | "http" | "format";

/** ApiError is the single error type of the client. code and status are set
 * for "http" errors (code from the server's error envelope when present). */
export class ApiError extends Error {
  readonly kind: ApiErrorKind;
  readonly status: number | undefined;
  readonly code: string | undefined;
  /** details is the envelope's error.details, when the server sends any:
   * structured data the UI acts on (the try-it refusals, T029: a
   * certificate to pin, the auth variable that is not set, a limit). */
  readonly details: unknown;

  constructor(kind: ApiErrorKind, message: string, status?: number, code?: string, details?: unknown) {
    super(message);
    this.name = "ApiError";
    this.kind = kind;
    this.status = status;
    this.code = code;
    this.details = details;
  }
}

/** Requests that take longer than this count as failed (D8). */
export const REQUEST_TIMEOUT_MS = 15_000;

interface RequestOptions {
  signal?: AbortSignal;
  timeoutMs?: number;
  /** POST sends body as JSON with Content-Type: application/json; the
   * default is GET. POST changes state: re-index (ADR 0052), the change
   * endpoints' pins and refs, and the manifest edit (ADR 0075). */
  method?: "GET" | "POST";
  body?: unknown;
  /** headers adds request headers (the manifest edit's session token). */
  headers?: Record<string, string>;
}

function envelopeError(body: unknown): { code: string; message: string; details: unknown } | undefined {
  if (typeof body !== "object" || body === null || !("error" in body)) return undefined;
  const err = body.error;
  if (typeof err !== "object" || err === null) return undefined;
  const code = "code" in err && typeof err.code === "string" ? err.code : undefined;
  const message = "message" in err && typeof err.message === "string" ? err.message : undefined;
  const details = "details" in err ? err.details : undefined;
  return code !== undefined && message !== undefined ? { code, message, details } : undefined;
}

// Project mode (serve --project, T022 B9). One project per server, with a
// repository filter: every per-member request names the selected member
// with repository=. The shell sets it before it renders the sections, so
// every request of a section names the same member; null (a
// single-repository server) adds nothing, so those requests are unchanged.
let selectedRepository: string | null = null;

/** setRequestRepository names the member every later request reads (null:
 * a single-repository server). Only the project shell calls it. */
export function setRequestRepository(repository: string | null): void {
  selectedRepository = repository;
}

/** requestRepository is the member requests name, or null outside project
 * mode. */
export function requestRepository(): string | null {
  return selectedRepository;
}

/** Paths that are not per member: the project document itself and the
 * project re-index, which refuses a repository parameter; and the jump-to
 * search and the couldn't-read page (T040), which answer every member
 * without it and name a member themselves when they want one. */
const PROJECT_WIDE = new Set(["/api/v0/project", "/api/v0/index/run", "/api/v0/project/manifest", "/api/v0/project/suggestions", "/api/v0/search", "/api/v0/unread"]);

// The workspace (T038, ADR 0087): one server serves several indexes, and
// every per-index request names the viewed entry with ws=. The workspace
// root sets it before it renders the shell; null (a server without a
// workspace list, or the first-run state) adds nothing.
let selectedWorkspace: string | null = null;

/** setRequestWorkspace names the workspace entry every later request
 * reads (null: none). Only the workspace root calls it. */
export function setRequestWorkspace(id: string | null): void {
  selectedWorkspace = id;
}

/** requestWorkspace is the entry requests name, or null. */
export function requestWorkspace(): string | null {
  return selectedWorkspace;
}

/** withWorkspace adds ws= to a per-index API path: every path but the
 * workspace's own endpoints. */
export function withWorkspace(path: string): string {
  if (selectedWorkspace === null || !path.startsWith("/api/") || path.startsWith("/api/v0/workspace")) return path;
  const q = path.indexOf("?");
  if (q !== -1 && new URLSearchParams(path.slice(q + 1)).has("ws")) return path;
  return `${path}${q === -1 ? "?" : "&"}ws=${encodeURIComponent(selectedWorkspace)}`;
}

/** withRepository adds repository= to a per-member API path in project
 * mode, and ws= for the viewed workspace entry (withWorkspace). */
export function withRepository(path: string): string {
  return withWorkspace(withMember(path));
}

function withMember(path: string): string {
  if (selectedRepository === null || !path.startsWith("/api/")) return path;
  const q = path.indexOf("?");
  const bare = q === -1 ? path : path.slice(0, q);
  if (PROJECT_WIDE.has(bare) || bare.startsWith("/api/v0/workspace")) return path;
  // A request that names its member itself (the Explorer's per-member
  // counts, T029) keeps it: a second repository= would be refused.
  if (q !== -1 && new URLSearchParams(path.slice(q + 1)).has("repository")) return path;
  return `${path}${q === -1 ? "?" : "&"}repository=${encodeURIComponent(selectedRepository)}`;
}

/** fetchJSON requests path (GET unless options.method says POST) from the
 * server that served the page and returns the decoded body. Aborting signal rejects with the signal's reason; every
 * other failure rejects with an ApiError. */
export async function fetchJSON(path: string, options: RequestOptions = {}): Promise<unknown> {
  const timeoutMs = options.timeoutMs ?? REQUEST_TIMEOUT_MS;
  const controller = new AbortController();
  // The controller is aborted by the timeout or by the caller's signal; an
  // abort that did not come from the caller is a timeout.
  const timer = setTimeout(() => {
    controller.abort();
  }, timeoutMs);
  const outer = options.signal;
  const forward = () => {
    controller.abort();
  };
  outer?.addEventListener("abort", forward);
  if (outer?.aborted === true) controller.abort();
  try {
    let response: Response;
    try {
      const post = options.method === "POST";
      response = await fetch(withRepository(path), {
        method: post ? "POST" : "GET",
        headers: { Accept: "application/json", ...(post ? { "Content-Type": "application/json" } : {}), ...options.headers },
        ...(post ? { body: JSON.stringify(options.body ?? {}) } : {}),
        // Freshness is computed per request; a cached answer would be an
        // old answer presented as new.
        cache: "no-store",
        signal: controller.signal,
      });
    } catch (err) {
      throw requestError(err, controller.signal, outer, timeoutMs);
    }
    let body: unknown;
    try {
      body = await response.json();
    } catch (err) {
      if (controller.signal.aborted) throw requestError(err, controller.signal, outer, timeoutMs);
      body = undefined;
      if (response.ok) throw new ApiError("format", `${path}: response is not JSON`);
    }
    if (!response.ok) {
      const env = envelopeError(body);
      const message = env
        ? `${env.code}: ${env.message}`
        : `HTTP ${String(response.status)} ${response.statusText}`.trim();
      throw new ApiError("http", message, response.status, env?.code, env?.details);
    }
    return body;
  } finally {
    clearTimeout(timer);
    outer?.removeEventListener("abort", forward);
  }
}

function requestError(
  err: unknown,
  own: AbortSignal,
  outer: AbortSignal | undefined,
  timeoutMs: number,
): unknown {
  if (outer?.aborted === true) return outer.reason;
  if (own.aborted) return new ApiError("timeout", `the server did not answer within ${String(timeoutMs / 1000)} s`);
  const detail = err instanceof Error ? err.message : String(err);
  return new ApiError("network", detail);
}

/** STATUS_PATH is the only endpoint of HTTP API v0. */
export const STATUS_PATH = "/api/v0/status";

/** fetchStatus requests and validates the server status. A malformed body
 * becomes an ApiError of kind "format". */
export async function fetchStatus(options: RequestOptions = {}): Promise<Status> {
  const body = await fetchJSON(STATUS_PATH, options);
  try {
    return parseStatus(body);
  } catch (err) {
    if (err instanceof StatusFormatError) throw new ApiError("format", err.message);
    throw err;
  }
}
