// The live examples of "Start here" (T045): one Fact, one Inferred link
// and one Unknown from the index being viewed, each with a link into it,
// shown as "In your index:" after the guide's fixed illustration of the
// label. Every example is picked from the API section's own answers
// (claim rows HP-1 to HP-5 of docs/contract/claims/help.md, kept 1:1 by
// rows.test.ts); when the index has no candidate, or a request fails, the
// example is absent and the page shows only the fixed illustration.
// Nothing about the repository is ever made up.

import type { ApiClient, EndpointRow, EndpointsResponse, HttpEndpointRow, TransportSummary } from "@/api/endpoints";
import { withNumber } from "@/lib/labels";
import { DEFAULT_PARAMS, apiHash } from "@/sections/api/params";
import type { ExampleKind } from "@/sections/help/markdown";

/** Example is one example line: what it shows and where its link goes. */
export interface Example {
  /** The route, call or count the example is about. */
  subject: string;
  /** The label as the Start here text words it ("Fact · 1.0"). */
  label: string;
  /** A short note after the label (where the caller is); "" for none. */
  detail: string;
  href: string;
  /** The claim row that picked it. */
  row: string;
}

/** Examples are the three live examples. inferredSearchedAll is true when
 * HP-2 read every HTTP route with a caller (at most INFERRED_CANDIDATES of
 * them, no detail request failed), so a missing Inferred example proves
 * the index has none; otherwise it proves nothing (HP-4). */
export type Examples = Record<ExampleKind, Example | null> & { inferredSearchedAll: boolean };

/** INFERRED_CANDIDATES is how many called routes HP-2 opens at most, so
 * the page never requests every route of a large index. */
export const INFERRED_CANDIDATES = 3;

function http(res: EndpointsResponse): TransportSummary | undefined {
  return res.transports.find((t) => t.transport === "http");
}

function isFullHttp(r: EndpointRow): r is HttpEndpointRow & { method: string; path: string } {
  return r.transport === "http" && r.method !== null && r.path !== null;
}

/** rowName is how the API list names an endpoint: method and path, the
 * command or the action name, else the stored name. */
function rowName(r: EndpointRow): string {
  if (isFullHttp(r)) return `${r.method} ${r.path}`;
  if (r.transport === "tauri_ipc") return r.command ?? r.name;
  if (r.transport === "server_action") return r.action ?? r.name;
  return r.name;
}

/** factExample is HP-1: the first HTTP route with a method and a full
 * path in the server's order, else the first endpoint of any transport. */
export function factExample(res: EndpointsResponse): Example | null {
  const rows = res.transports.flatMap((t) => t.endpoints);
  const row = rows.find(isFullHttp) ?? rows[0];
  if (row === undefined) return null;
  return {
    subject: rowName(row),
    label: withNumber("Fact", 1),
    detail: row.transport === "server_action" ? "from its 'use server' directive" : "from its registration",
    href: apiHash({ ...DEFAULT_PARAMS, sel: row.id, tab: "overview" }),
    row: "HP-1",
  };
}

/** unknownExample is HP-3: the HTTP calls whose stored status is
 * "dynamic" (the URL is built at runtime), when there are any. */
export function unknownExample(res: EndpointsResponse): Example | null {
  const n = http(res)?.calls.by_status.dynamic ?? 0;
  if (!Number.isInteger(n) || n <= 0) return null;
  return {
    subject: `${n.toLocaleString("en-US")} ${n === 1 ? "call" : "calls"} whose URL is built at runtime`,
    label: "Unknown",
    detail: "listed, never linked",
    href: apiHash({ ...DEFAULT_PARAMS, gap: { transport: "http", category: "dynamic" } }),
    row: "HP-3",
  };
}

/** inferredCandidates are the routes HP-2 opens: the first HTTP routes
 * with at least one caller, in the server's order. */
export function inferredCandidates(res: EndpointsResponse): HttpEndpointRow[] {
  return (http(res)?.endpoints ?? []).filter((r): r is HttpEndpointRow => r.transport === "http" && r.caller_count > 0).slice(0, INFERRED_CANDIDATES);
}

/**
 * loadExamples requests the endpoint list and, for HP-2, the detail of at
 * most INFERRED_CANDIDATES called routes, one after another, until one has
 * a caller linked by an inferred edge. A failed detail request skips that
 * route (HP-2); a failed list request rejects (HP-4).
 */
export async function loadExamples(client: ApiClient, signal: AbortSignal): Promise<Examples> {
  const res = await client.endpoints({ scope: "production", root: null }, { signal });
  let inferred: Example | null = null;
  const called = (http(res)?.endpoints ?? []).filter((r) => r.transport === "http" && r.caller_count > 0).length;
  let searchedAll = called <= INFERRED_CANDIDATES;
  for (const row of inferredCandidates(res)) {
    let detail;
    try {
      detail = await client.endpoint({ id: row.id, scope: "production", root: null }, { signal });
    } catch {
      if (signal.aborted) throw new Error("aborted");
      searchedAll = false;
      continue;
    }
    for (const caller of detail.callers) {
      const edge = caller.edges.find((e) => e.provenance === "inferred" && Number.isFinite(e.confidence));
      if (edge === undefined) continue;
      const line = caller.node.span?.start_line ?? null;
      inferred = {
        subject: rowName(row),
        label: withNumber("Inferred", edge.confidence),
        detail: caller.node.file === "" ? "a call matched by its URL" : `called from ${caller.node.file}${line !== null ? `:${String(line)}` : ""}`,
        href: apiHash({ ...DEFAULT_PARAMS, sel: row.id, peek: caller.node.identity }),
        row: "HP-2",
      };
      break;
    }
    if (inferred !== null) break;
  }
  return { fact: factExample(res), inferred, unknown: unknownExample(res), inferredSearchedAll: searchedAll };
}
