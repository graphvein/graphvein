// Project mode fixtures (T022 B9), in the shapes of
// internal/server/testdata/project-users.golden.json and
// project-monorepo.golden.json.

import { parseProject, type Project, type ProjectEndpoint } from "@/api/project";

const ASSUME = "matched across project repositories; base URL not proven";

function member(repository: string, over: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    repository,
    repository_source: "manifest",
    path: repository,
    path_source: "manifest",
    checkout: true,
    remote: null,
    extractor: { current: "extractors-48", indexed: "extractors-48", matches: true },
    index: { contract_version: 1, finished_at: "2026-11-03T14:32:05.123Z", indexer: { name: "graphvein", version: "extractors-48" } },
    freshness: { state: "fresh", checked_at: "2026-11-03T14:40:00Z", changed_count: 0, changed_files: [], changed_files_truncated: false },
    links: { in_pass: true, pass_run: "r", index_error: null, links: 27, stale: 0, stale_reasons: [] },
    ...over,
  };
}

/** projectBody is a react + express + axum-ddd project without
 * declarations: some links, an ambiguity group between two providers. */
export function projectBody(over: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    api_version: 0,
    project: "shop",
    server_version: "test",
    members: [member("react"), member("express"), member("axum-ddd", { checkout: false, path: null, path_source: null, freshness: { state: "unknown", checked_at: "x", changed_count: null, changed_files: [], changed_files_truncated: false, error: "no checkout here: freshness needs the member's files" } })],
    link_pass: { state: "fresh", pass: 2, links: 27, stale_links: 0, outcomes: { linked: 27, ambiguous: 2, dynamic: 4 }, stale_reasons: [] },
    checks: [],
    declarations: { consumes: [], scopes: [] },
    ambiguous: {
      total: 2,
      groups: [
        {
          row: "PL-R11",
          providers: ["axum-ddd", "express"],
          count: 2,
          sources: [
            {
              source: { repository: "react", identity: "client_call:src/api.ts#getArticles.GET", kind: "client_call", name: "GET /api/articles", file: "src/api.ts", line: 12 },
              template: "/api/articles",
              candidate_count: 2,
              candidates: [
                { repository: "axum-ddd", identity: "route:src/routes.rs#GET%20%2Fapi%2Farticles", kind: "route", name: "GET /api/articles", file: "src/routes.rs", line: 40 },
                { repository: "express", identity: "route:apps/article/routes.ts#GET%20%2Fapi%2Farticles", kind: "route", name: "GET /api/articles", file: "apps/article/routes.ts", line: 9 },
              ],
            },
          ],
        },
      ],
    },
    conflicts: [],
    ...over,
  };
}

export function project(over: Record<string, unknown> = {}): Project {
  return parseProject(projectBody(over));
}

/** conflictBody is the monorepo scenario's one declaration conflict. */
export const CONFLICT = {
  call: { repository: "monorepo", identity: "client_call:web/client.ts#userFortyTwoFromOrders.GET", kind: "client_call", name: "GET `${import.meta.env.VITE_ORDERS}/t022/users/42`", file: "web/client.ts", line: 21 },
  consumer: "monorepo/web",
  declared: ["`monorepo/web` consumes `monorepo/orders` (bases: VITE_ORDERS)"],
  target: { repository: "monorepo", identity: "route:apps/users/server.ts#GET%20%2Ft022%2Fusers%2F%3Aid", kind: "route", name: "GET /t022/users/:id", file: "apps/users/server.ts", line: 10 },
  target_scope: "users",
};

/** projectEndpoint is the `project` part of an express route's endpoint
 * answer: two react callers, one of them stale, and an ambiguous call. */
export function projectEndpoint(): ProjectEndpoint {
  const node = (name: string, line: number) => ({
    identity: `client_call:src/api.ts#${name}`,
    kind: "client_call",
    level: 3,
    name: `GET ${name}`,
    qualified_name: name,
    file: "src/shared/api/api.gen.ts",
    span: { start_line: line, start_column: 1, end_line: line, end_column: 20 },
    content_hash: "h",
    attributes: { url_expression: "`/api/articles`", method: "GET", status: "unmatched" },
    ide: null,
    file_state: "unchanged",
    source_root: ".",
  });
  return {
    project: "shop",
    link_pass: { state: "fresh", pass: 2, stale_reasons: [] },
    callers: [
      { repository: "react", identity: "client_call:a", node: node("getArticles", 40), owner: { id: "f", kind: "function", qualified_name: "getArticles" }, kind: "calls_route", provenance: "inferred", confidence: 0.7, assumption: ASSUME, stale: false, stale_reasons: [], shape_differences: null, ide_status: null },
      { repository: "react", identity: "client_call:b", node: null, owner: null, kind: "calls_route", provenance: "inferred", confidence: 0.7, assumption: ASSUME, stale: true, stale_reasons: ["react was indexed again after the link pass"], shape_differences: null, ide_status: null },
    ],
    operations: [],
    possible_callers: [
      { repository: "react", identity: "client_call:c", kind: "calls_route", row: "PL-R11", status: "ambiguous", wording: "ambiguous across repositories", reason: "", candidate_count: 3, candidate_repositories: ["axum", "express"] },
      { repository: "react", identity: "client_call:d", kind: "calls_route", row: "PL-R7", status: "incomplete", wording: "not linked: a candidate provider had no stored index", reason: "no stored index: ghost", candidate_count: 1, candidate_repositories: ["express"] },
    ],
    declaration_conflicts: [],
  };
}

export { ASSUME };
