# ADR 0082 - Read-only MCP server

**Status:** Accepted (delegated review, 2026-11-23)

## Context

Milestone M3 asks for a read-only MCP server over the deterministic facts,
so a coding agent can ask Graphvein what a person asks the web UI: where
something is defined, which routes exist, what a route does, who calls a
function and what depends on it. The answers must carry the same honesty
as the UI: verification and precision labels, unknowns, assumptions, the
confidence with its rule, index freshness on every response and every
traversal cap that bound ("depth cap reached" is never "nothing beyond").
Depth parameters use the shared caps, project mode works where the CLI and
HTTP API already have it, and only `internal/egress` may open network
connections (ADR 0040).

The design inventory (T030 task record, internal) found the read paths to
reuse: the query package (`query.Execute`, `query.ExecuteProject`) for
callers, impact and term lookup, and the HTTP API v0 views
(`internal/server`), which carry almost all of the honesty labels but are
unexported `compute*` functions behind the HTTP handler map. It also
listed gaps an MCP answer would expose: query truncation unreported (G1),
callers and impact unbounded and without cap reports (G2), route callers
without the endpoint view's near misses (G3), most views without
freshness (G6), no paging of endpoint lists (G7) and a silent end where
the chain prints "? not modelled" (G10).

## Options considered

**Protocol implementation.**

1. The official Go SDK, `github.com/modelcontextprotocol/go-sdk` v1.8.0
   (Apache-2.0 for new code, MIT for older code; needs go 1.25 or later).
   It gives typed tools with input schemas inferred from Go structs,
   `structuredContent`, protocol-version negotiation (2024-11-05 to
   2026-07-28), cancellation and upstream conformance tests. Cost: six SDK
   and OAuth packages that import `net`, `net/http` or `os/exec` for the
   client, HTTP, SSE and command transports, about 2 MB of binary, and one
   package (`oauthex`) that captures `http.DefaultTransport` at init, which
   the runtime tripwires cannot see.
2. Hand-written JSON-RPC 2.0 over stdio: no new module and no network
   import by construction, but the team tracks the protocol (five versions
   in two years) and loses upstream conformance testing.

**Reuse of the views.**

1. An exported in-process reader in `internal/server` that runs the
   registered view handlers (with their consistency check) without a
   listener and without the browser guards.
2. Moving every `compute*` view into an exported package.
3. Calling the HTTP handler on a recorder from `internal/mcp`.

**Encoding.**

1. `structuredContent` with an `outputSchema`, plus the same object as
   minified JSON in the one text block (the specification's advice for
   clients that read only text).
2. Compact text only.
3. JSON plus a prose summary (a second wording of every claim).

## Decision

- **SDK** (protocol option 1), pinned at v1.8.0 and used only through
  `mcp.NewServer`, `mcp.AddTool` and the newline-delimited I/O transport
  over standard input and output. Three guards ship with it:
  `TestDependencySurface` lists the six network-capable packages on Linux,
  macOS and Windows with the reason they are not reached;
  `TestNetworkBoundary` allows first-party code (tests included) only the
  SDK's stdio server identifiers (`NewServer`, `Server`, `ServerOptions`,
  `ServerCapabilities`, `Implementation`, `AddTool`, `Tool`,
  `ToolAnnotations`, `CallToolRequest`, `CallToolResult`, `Content`,
  `TextContent`, `IOTransport`, `StdioTransport`, `Transport`) and rejects
  every other identifier of package `mcp` (`NewClient`,
  `CommandTransport`, `NewStreamableHTTPHandler`, `SSEHandler`, the client
  transports, the in-memory pair, ...), every other SDK package (`auth`,
  `oauthex`, `jsonrpc`, ...) and `golang.org/x/oauth2`;
  `TestMCPSessionMakesNoNetworkAttempts` runs a whole `graphvein mcp`
  session (initialize, tool list, every tool) with tripwires on egress
  (no dial, lookup or listen at all), the default resolver and the default
  HTTP transport. The new modules have reviewed licence records in
  `scripts/notices`.
- **Stdio only.** `graphvein mcp [path]` or `--project MANIFEST` reads the
  protocol from standard input and writes it to standard output; logs go
  to standard error. There is no listener and no client.
- **Reuse** (option 1): `server.Reader` builds the same route map as
  `graphvein serve` (one repository or a project, whose member views take
  the repository parameter) and answers only the GET views in
  `server.ReadPaths`; the re-index runner, pins, ref snapshots, manifest
  edits, the explorer's POST and try-it are not in it. Its JSON is the
  UI's byte for byte, so the views' goldens keep covering the facts and
  the MCP goldens cover the mapping.
- **Tools** (thirteen, one entry each in one tool table, on one
  envelope): `status`, `find`, `endpoints`, `endpoint`, `source`,
  `callers`, `impact`, `chain` (the architecture chain view), `table` (the
  schema table view plus the chain's first upstream step from the table),
  `changes` and `change_impact` (the change views), `unresolved` (the
  calls view and the endpoint rows with no stored caller) and `overview`
  (the architecture, schema summary and precision views, summarized).
- **Table users** (D7, G8, G9): the architecture chain's CH6 edges carry
  an additive `access` (tool, role, operations, rows, verification,
  assumes), read by `changes.AccessEntry`, the reader change impact's I8
  step uses, so the UI's chain and the `table` tool show whether a use is
  oracle-verified or mapped and what it assumes. The sites that may also
  use a table come from the one rule change impact's I8 uses
  (`changes.ReadUnknownUses`, `UnknownUses.MayAlsoUse`): entries whose
  candidates name the table, and unbound entries (candidates null) that
  may reach it, labelled `unbound: true`; an unbound SQLModel, Prisma or
  Drizzle entry reaches only a model table of its tool, any other tool's
  entry every table (over-reported, never missed). A table whose source
  no code-to-table row covers (`changes.AccessCovered`) answers "users
  not extracted", never zero uses. Users are always a lower bound, 20 per
  page by default, and `include` (`both`, `schema`, `users`) reads one
  half.
- **Change bases** (D9): the change views are read through their GET
  handlers only (`server.ReadPaths`); a `ref:` base whose snapshot is not
  stored is refused (`would_index`) with the command that stores it, and
  a base that does not exist lists the stored bases as candidates. The
  POST pins and ref jobs are not reachable.
- **Compact detail** (token economy): `endpoints` rows without the shape
  summary, `endpoint` shapes as states and typed item names, `table`
  claims counted by verification, `changes` items with changed field
  names; `detail: "full"` returns the views' detail. Everything left out
  is an `omitted` entry `{field, how}`.
- **Envelope**: `tool`, `envelope`, `repository` or `project`, `error`,
  `freshness`, `request` (every parameter after defaults), `bounds`,
  `page`, `summary`, `results`, `unknowns`, `assumptions`, `labels` (only
  the labels used, with their meaning) and `omitted` (the fields left
  out, each with how to get it), in that order: a client that cuts a long
  text keeps the refusal, the caps and the paging. A list an unknown
  carries is paged like results (`unknowns_limit`, default 20, and
  `unknowns_cursor`) with its exact total; walk ends are one unknown per
  end line, listing the ends among the page's results. Every mapper keeps
  a list of the view keys it knows (kept, or left out with a way back);
  any other key is MC-D. The claim contract
  [mcp-server.md](../contract/claims/mcp-server.md) (rows MC-1 to MC-30
  and the default MC-D) names what each part and tool may state;
  `internal/mcp` keeps it 1:1 and the session goldens reach every row. A
  field or label value no row names is passed on as an unknown
  (`not_modelled_by_mcp`), never dropped.
- **Freshness** on every response (D4): the `/api/v0/status`
  calculation, or the project view's members and link pass. Measured on
  langflow a check costs about 1.1 s (as long as `graphvein status`), so
  one check serves a call batch: calls that start within 2 s of a check
  reuse it and say `reused: true`, keeping that check's `checked_at`, so
  the age is always visible, but only while the latest stored run of
  every repository it covers is still the one the check saw (the runs
  are read every call; a re-index inside the window checks again).
  `status` always checks. An answer whose own nodes say a file changed
  (`file_state`, which `find`, `callers` and `impact` now carry too,
  from `query.Presenter.FileState`) while the check says fresh is checked
  again, or carries the mismatch as an unknown. Every call after the
  window checks again.
  `find`, `callers` and `impact` hand that check to the query package
  (`query.Options.Fresh`), so a call scans the checkout once.
- **Shared caps** (D2 as decided): `callers` and `impact` move onto
  `internal/traverse` with `MaxDepth` 6, `MaxReached` 2,000 and `MaxFanOut`
  200, and the CLI JSON gains `caps`, `bounds` and `ends` (additive); the
  project walk keeps its depth maximum of 10 with the same reached-node
  and fan-out caps. MCP tools take each path's own maximum and default. A
  reached function with no stored edge to follow is a "? not modelled"
  end (G10). `query` reports its exact `total` (G1). A route's `callers`
  add the endpoint view's near misses as unknowns (G3).
- **Paging** (D6): every list a tool pages defaults to 50 and allows at
  most 500, with `page.more` and "N more not shown; call again with
  cursor". Caps of the reused code are bounds, never paging.
- **Target input** (D13, review): one resolver for every
  tool, on the rule `graphvein callers` and `impact` share
  (`query.Resolve`): an identity, a name or a qualified-name suffix at a
  name boundary resolves; a suffix inside a name is a candidate only. A
  "METHOD /path" target names the HTTP routes served at that path (every
  composed path, `server.ServedPaths`) before any node lookup, and only a
  route resolves it. `table` resolves tables only; `chain` a component
  directory too, refused as ambiguous when a node has the same name
  (`component:DIR` names the component). `request.match` names the tier.
  Ambiguity returns every candidate and no result.
- **Encoding** (D5, option 1), decided in one function (`Server.encode`):
  `structuredContent` and the same minified JSON as the text block by
  default; `--structured-only` (`EncodingStructured`) replaces the text
  copy with a one-line summary that names the first bound cap or the
  error first. Whether a client feeds both copies to its model is left to
  the benchmark pilot; a client that does is switched by configuration,
  with no change to any tool or claim.

## Consequences

- Agents get answers in the envelope the claim contract fixes; the views'
  honesty labels reach them, and caps, unknowns and paging are explicit.
- The CLI's `callers` and `impact` refuse depths above 6 (project: 10),
  stop at the shared reached-node and fan-out caps, and say so. Their JSON
  and the project HTTP endpoints gain `caps`, `bounds` and `ends`; results
  are unchanged where no cap binds.
- The binary links the SDK (about 2 MB) and six network-capable packages
  that are never reached; the boundary rule keeps first-party code to the
  stdio server API, and a new SDK version is a reviewed dependency change.
- One freshness scan serves a call batch, and the query package reuses
  it; `endpoint` with a "METHOD /path" target reads the HTTP endpoint list
  only, any other name the list of each transport.
- The architecture chain view's JSON gains `access` on CH6 edges
  (additive; its goldens change only by that field).
- `graphvein callers` and `impact` (single repository and project) no
  longer take a term that ends a qualified name inside a name (`Items`
  for `listItems`) as the target: the answer is "no matching target" with
  those nodes as candidates, so the CLI and the MCP tools resolve the
  same term to the same node.
- Change impact's I8 "may also be used by" lists the unbound sites that
  may reach a table as well (`unbound: true`), the same list as the MCP
  `table` tool's.

## Measured cost

See the MCP-1 and MCP-2 measurements in the T030 task record (internal):
freshness per call and answer sizes, compact and full, on the fastapi and
langflow corpora.

## Known gaps

- `callers` and `impact` do not filter test code (the request echoes
  `scope: all`); `endpoints` and `endpoint` honour the views' `scope`.
- The "? not modelled" end of callers and impact names what they do not
  follow; the chain's CH11 forms (FastAPI injection, unresolved callees,
  declared dependencies) are richer and come with the `chain` tool, which
  also follows the bridges callers and impact do not.
- Endpoint rows carry the route declaration's file and line, not the
  handler's; the handler is in `endpoint`.
- A reused freshness check (inside the 2 s window, same latest run) is
  re-checked only when the answer serves a node whose `file_state` says
  its file changed: a tool whose rows carry no file state (`endpoints`,
  for example) can still report the reused check as fresh right after an
  edit within the window. `checked_at` and `reused: true` show the
  check's age; the next call after the window checks again.
- Unbound table access sites (null candidates) carry no package, so they
  are "may also be used by" for every table their tool may reach in the
  repository, not only their own package's: over-reported, never missed.
- The endpoints rows show one path per route; the MCP server reads the
  other served paths from the stored route for the rows that count
  several (one store read per call that filters or resolves by path).
- The change tools in project mode read one member's change set;
  `change_impact` crosses links (no `ref:` base), `changes` does not.
- `find` in project mode needs a member checkout (the query package reads
  the checkout for presentation).
- Near-miss candidate lists are capped upstream at ten
  (`candidate_count_exact: false` is passed on as a lower bound).
- The client configuration snippets in the documentation follow each
  client's documented format and were not run against the clients.
- A client that passes both `structuredContent` and the text copy to its
  model pays for the answer twice until it is started with
  `--structured-only`.

## Review notes (2026-11-23)

- Review: one independent review (R1-R14: 4 A, 8 B, 2 C), a fix round, a confirmation pass (all confirmed; N1-N3 class C fixed), and an integration step after the link precision changes (calls in generated bundle output and origin assumptions carried into answers).
- Revisions: freshness reuse rechecks on a new run or a changed served node; tables without code-to-table coverage answer "users not extracted"; routes match every path they are served at, with scope; "METHOD /path" targets resolve to routes first; one name-resolution rule shared with the CLI (exact name or a suffix at a name boundary; other matches only as candidates); one reader for "may also be used by" shared with change impact; every view field mapped or named in `omitted`; unknowns paged with exact counts; compact answers by default.
- Results: gate run 45221478019 (Linux and Postgres; 0 FAIL, 246 `/postgres` PASS, only the opt-in oracle and census skips); local full check 0 FAIL, 245 `/postgres` PASS. Runtime test: a session calling every tool makes no egress attempt and no listen.
- Coverage: all 13 tools on fastapi and langflow; langflow answers after the fixes: `impact get_settings_service` 45 KB (was 2.77 MB), `callers` 38 KB, `table Flow` 45 KB, `unresolved` and `overview` about 50 KB.
- Known gaps: as listed above, plus: the architecture chain and change-impact steps through a `calls_route` edge do not yet show an origin assumption (planned with the unknown-prefix surfaces); project link changes are not a tool; `callers`/`impact` have no test-scope filter.
- After v1: the M5 tools (rationale, dispositions, findings, agent write-back).

## Revision 1: test scope for callers and impact, project link changes

**Status:** Accepted (delegated review, 2026-11-25)

Two additions after review, recorded in the T043 task record (internal).
They close two of the known gaps above ("`callers` and `impact` do not
filter test code"; "project link changes are not a tool"). The server
now has fourteen tools.

- **Test scope for `callers` and `impact`** (claim row MC-31). Options:
  (1) filter the walk's results in `internal/mcp` with the test-scope rule
  the chain and table tools already apply through their views; (2) give
  the query package and the project query views a scope parameter that
  keeps test nodes out of the walk itself, as the chain's CH10 does; (3)
  classify by the endpoint views' rule (the `scope` attribute only).
  Decision: option 1, with the rule shared rather than copied: the chain's
  CH10 test is now one function in `internal/server` (`testScoped`, and
  the exported `server.TestScoped` that applies the repository's test
  rules), which the chain, the architecture links' may-also-use sites and
  the MCP filter call. Option 3 would miss functions, which carry no
  scope attribute; option 2 changes the CLI and the project HTTP views and
  is left for later. `scope` takes `production` (default) or `all`, the
  values of `chain`, `table` and `overview`; the request echoes it. In
  production scope the results and "? not modelled" ends in test code are
  left out and counted in one unknown (`test_scope_hidden`, with how to
  get them: `scope: "all"`); in `all` a result in test code is marked
  `test_code`. A member without a checkout uses the built-in test names
  only, as the views read its `graphvein.yaml` as absent.
- **`link_changes`** (claim row MC-32): the project link journal diff of
  `GET /api/v0/project/links/changes`, added to `server.ReadPaths` (a GET
  view that reads the journal only). Base `previous` (default) or
  `pass:N`; results are the added, removed and changed links with their
  kind and ends, paged; the summary names the passes, the counts, whether
  the manifest changed and the members and runs of each pass. Fewer than
  two kept passes is an unknown (`no_comparison`), never "no change". A
  single-repository server registers the tool and refuses it
  (`project_only`), so the tool list is the same in both modes and the
  refusal says why. The tool never links or indexes.
- **Consequences**: the default answer of `callers` and `impact` changes
  where test code calls or depends on the target (fewer results, one
  `test_scope_hidden` unknown); `scope: "all"` gives the earlier answer
  with `test_code` marks. The tool list, and with it every session
  golden's tool list, gains `link_changes`.
- **Known gaps**: the filter applies to the results, not the walk: the
  caps, bounds, blind spots (possibly missing call sites), near misses
  and possible callers of other members count test code, and a production
  node reached only through test code is kept with `via_test_code: true`
  (it is a stored path, not a production one); a production node whose
  shortest path runs through test code is shown with that path, not a
  longer production one. A changed link's payload is not kept by the
  journal, so `link_changes` names the link but not what changed in it
  (`omitted` says where the current link is shown). The link passes kept
  are not listed as candidates of a `base_unknown` refusal (the view does
  not list them).

### Review notes for Revision 1 (delegated review, 2026-11-25, T043)

Accepted after gate 1 (CI run 45612588023: changes and postgres green; postgres log 0 FAIL, 254 `/postgres` PASS). One independent review (the T043 review notes, internal): A1 (`unknown_calls_in_result_files` counted the files of hidden test results), B1-B4 (unfiltered parts unlabelled, `test_code`/`via_test_code` unexplained, `link_changes` comparing the newest pass with itself, a member without a checkout reading another `graphvein.yaml`) and the docs points were fixed before the gate; the link changes `state` (compared / same pass / no comparison) is shared with the server endpoint and the CLI. MCP goldens changed only as explained in the task summary. 14 tools. Known gaps: the test-code filter applies to results, not to the walk (caps and blind spots still count test code; `via_test_code` marks nodes reached only through it); `link_changes` names a changed link but not what changed in it.
