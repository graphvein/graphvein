// Source endpoint bodies for the code peek tests. The node and index come
// from the Go server's API section golden (internal/server/testdata), so
// they have the shape the server sends; the source part follows the
// documented response (docs/cli/serve.md "GET /api/v0/source").

import { ApiError } from "@/api/client";
import { parseSource, type SourceClient, type SourceParams, type SourceResponse } from "@/api/source";
import { DETAIL_GOLDENS, raw } from "@/test/apiFixtures";

const route = DETAIL_GOLDENS["api-endpoint-route"];

/** RUN is the finished_at of the golden index run. */
export const RUN = route.index.finished_at;

/** goldenNode returns a raw presented node of the route golden: a caller
 * (by index) or, with "handler", the handler. */
export function goldenNode(which: number | "handler" = 0): Record<string, unknown> {
  const g = raw(route) as unknown as { handler: { node: Record<string, unknown> | null }; callers: { node: Record<string, unknown> }[] };
  const node = which === "handler" ? g.handler.node : g.callers[which]?.node;
  if (node === undefined || node === null) throw new Error("fixture changed");
  return node;
}

export interface SourceOver {
  state?: string;
  reason?: string | null;
  first_line?: number | null;
  lines?: string[];
  cut_lines?: number[];
  truncated?: boolean;
  highlight?: { start_line: number; start_column: number; end_line: number; end_column: number } | null;
  size?: number | null;
  line_count?: number | null;
  path?: string;
  ide?: Record<string, unknown>;
  node?: Record<string, unknown>;
  finished_at?: string;
}

const LINES = ["import { api } from './api';", "", "export async function atRoot() {", "  return fetch('/t016d-items');", "}"];

/** sourceBody builds a raw source response: by default an exact answer
 * for a five-line file with line 4 highlighted at fetch(...). */
export function sourceBody(o: SourceOver = {}): unknown {
  const node = o.node ?? goldenNode();
  const lines = o.lines ?? LINES;
  return {
    api_version: 0,
    repository: "fixture",
    index: { ...route.index, finished_at: o.finished_at ?? RUN },
    ide: o.ide ?? { available: true, error: null, label: "Open in VS Code" },
    node,
    source: {
      state: o.state ?? "exact",
      reason: o.reason ?? null,
      file: { path: o.path ?? (node.file as string), size: o.size === undefined ? 120 : o.size, line_count: o.line_count === undefined ? lines.length : o.line_count },
      first_line: o.first_line === undefined ? (lines.length > 0 ? 1 : null) : o.first_line,
      lines,
      cut_lines: o.cut_lines ?? [],
      truncated: o.truncated ?? false,
      highlight: o.highlight === undefined ? { start_line: 4, start_column: 9, end_line: 4, end_column: 30 } : o.highlight,
    },
  };
}

export function sourceResponse(o: SourceOver = {}): SourceResponse {
  return parseSource(sourceBody(o));
}

/** NO_SOURCE are the states that read no lines. */
export const NO_SOURCE = ["missing", "not_regular", "too_large", "binary", "unreadable", "no_location", "not_indexed"] as const;

/** noSource builds a no-source answer: no lines, no highlight. */
export function noSource(state: string, o: SourceOver = {}): SourceOver {
  return { state, lines: [], first_line: null, highlight: null, line_count: null, ...o };
}

/** stubSource answers every request with answer(params) (a body, or an
 * Error to reject) and records the requests. */
export function stubSource(answer: (p: SourceParams) => unknown, requests: SourceParams[] = []): SourceClient {
  return {
    source: (p) => {
      requests.push(p);
      const body = answer(p);
      if (body instanceof Error) return Promise.reject(body);
      return Promise.resolve(parseSource(structuredClone(body)));
    },
  };
}

/** notFound is the server's 404 for an unknown identity. */
export function notFound(id: string): ApiError {
  return new ApiError("http", `not_found: no node with identity ${id}`, 404, "not_found");
}
