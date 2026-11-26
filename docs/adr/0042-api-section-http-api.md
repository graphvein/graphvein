# ADR 0042 - API section HTTP API

**Status:** Accepted (delegated review, 2026-11-03)

## Context

The first real UI view (T016d) shows, for the indexed repository, every
HTTP route and every Tauri IPC command, who calls each one, how sure each
link is and where the graph is incomplete. The UI decisions
(the T016 task record (internal): U3 B, U4 A, M5, developer decision D1)
put HTTP routes and Tauri IPC commands in one API section, grouped by
transport with the same coverage counts, with a gap sheet per category.
The developer decided (2026-11-03) that coverage is grouped by the calling
site's source root, that production is the default scope with an "Include
tests" toggle, and that nothing is hidden or relabelled.

VISION's precision principle governs the data: a guess is
never shown as a link. The index records callers as stored edges; dynamic
calls record no candidates (T016d F5), ambiguous and test-only calls record
capped candidate lists, and a call whose status is not `linked` may still
have stored edges through one alternative. The index stores no reason text
for any status. T016d1 adds the server side: three read-only endpoints under
HTTP API v0 (ADR 0041), with no store, contract or extractor change.

## Options considered and decisions

### 1. Endpoint granularity

- **One list endpoint with coverage, plus detail and calls endpoints
  (chosen).** `GET /api/v0/api/endpoints` draws the coverage cards and the
  list in one request; `/api/v0/api/endpoint` (detail) and
  `/api/v0/api/calls` (gap lists) load on demand.
- One endpoint per card or list: more requests, and counts and list could
  come from different index states.
- A generic graph query endpoint: every client would have to re-implement
  the count semantics, which is where precision errors would creep in.

### 2. Raw statuses on the wire

- **Raw statuses, categories in the UI (chosen).** The server reports the
  stored `status` string exactly (a missing or non-string status as `""`),
  so future statuses pass through. Clients must map any status they do not
  know to "other", never to linked; only the exact string `linked` is linked.
- Server-side categories: would hide what the index says and need a server
  change for every new status.

### 3. Count definitions and scope

The counts of docs/cli/serve.md: endpoints and calls per transport (fixed by
node kind, not by the `transport` attribute), `by_status` on raw values,
`caller_count` from stored call edges only, `never_called`,
`linked_without_edge` (an inconsistency, expected 0) and
`edges_from_unlinked` (legitimate). Scope `production` is "no `scope`
attribute", `test` is `scope: "test"`; any other value is unknown and only
in `all` (unknown is not production). Callers come only from stored edges;
near misses only from recorded candidates of ambiguous and test-only
choices (http: the path entry's status, so a call whose node status is
`dynamic` can hold one; tauri: the node's); dynamic and unmatched choices are
never attached to an endpoint. A tie size that is only a lower bound (no
stored count, a list at the cap of ten) is marked `candidate_count_exact:
false` and its reason says "At least"; it is never shown as exact
(T016d Revision 1).

### 4. Reasons

- **Derived from stored attributes (chosen).** A fixed table per transport
  (docs/cli/serve.md) maps attributes to codes and short texts that claim no
  more than the attribute; an unknown status gets `status_unknown`.
- Stored reason texts: none exist; adding them is an extractor and contract
  change.
- No reasons: the gap sheet would list calls without saying why.

### 5. Identity in a query parameter

- **`?id=` (chosen).** Identities contain `#`, `%` and `/`; a query value
  is unambiguous once URL-encoded.
- A path segment: `%2F` and `#` handling differs between clients, proxies
  and `net/http`'s path cleaning.

### 6. Consistency

- **Runs before and after (chosen).** Each handler reads the runs first and
  last; when the run count or the latest run (ID and finish time) differs,
  it repeats the whole read once, then answers 503 `index_changed`.
- A new store method with one read transaction: rejected, no new store
  methods in T016d.
- No check: counts and lists could mix two index states.

### 7. No cache, no server-side search or paging

Every request reads the store after it arrives. Lists are bounded by the
repository's call count (Langflow: 422 dynamic calls) and the store has no
attribute search. Revisit when a corpus exceeds a few thousand rows in one
list or the 500 ms latency target fails.

### 8. Source roots

- **Nearest stored manifest directory, else first path segment (chosen).**
  The deepest directory from the call file up to the root holding a stored
  `Cargo.toml`, `deno.json`, `deno.jsonc`, `go.mod`, `package.json`,
  `pyproject.toml`, `setup.cfg` or `setup.py` (a key of `FileHashes`, so an
  ignored manifest does not count); else the first path segment; else `.`.
  Deterministic and computed from the index alone.
- Configured roots: no configuration exists yet; later if the rule proves
  too coarse.
- Name heuristics such as treating `docs/` specially: rejected, they guess
  from names.

### 9. UI building blocks (T016d2)

- **Native `<dialog>`, hand-written windowing and split (chosen).** The gap
  sheet is a `<dialog>` opened with `showModal()` (top layer, inert
  background, Esc, `::backdrop` and the slide-in in CSS); the endpoint list
  is a fixed-height windowed listbox (`web/src/components/VirtualList.tsx`,
  prefix offsets and binary search, rows positioned with `transform` through
  React style props); the list/detail split is a small separator component
  with pointer, keyboard and double-click reset. The CSP stays exactly as in
  ADR 0041 and no npm dependency is added. Playwright under the real CSP
  (T016d2 A4): Radix Dialog raises a `style-src-elem` violation and a
  console error; the native dialog, Radix Tooltip and ToggleGroup raise
  none.
- Radix Dialog / Sheet: blocked by the CSP (react-remove-scroll injects
  `<style>`); a per-response nonce would need a templated `index.html`, a
  global hand-off to the library and would widen what an HTML injection
  could style. Rejected.
- `@tanstack/react-virtual`, `react-resizable-panels`: work, but each is a
  dependency (T016c D6 requires an ADR and the developer) for about 100
  lines of code that fixed row heights make simple and testable in jsdom.

The UI maps raw statuses to categories in `web/src/api/categories.ts`:
only the exact string `linked` is linked; ambiguous, test only, unproven
(registry_unproven, acl_possible, shadow_possible, unknown_command), plugin
and global get a shared neutral colour with their label and icon; anything
else, including `""` and case variants, is "Other" with the raw value
shown. Guards for every response are hand-written (no schema library). The
wrapper colour became an olive family distinct from the HTTP accent, and an
`--other` family was added (values and contrast in T016d2's summary).

## Consequences

- The UI (T016d2) consumes only these endpoints and `/api/v0/status`; the
  count semantics live in one place (`internal/server/apidata.go`) and are
  golden-tested on SQLite and Postgres.
- `internal/query` exports its presenter (`query.NewPresenter`,
  `Presenter.Node`, `Presenter.FileState`), so CLI and API IDE links mean
  the same; CLI output is unchanged.
- Responses report `file_state` per presented node, so stale lines are
  marked per file without a freshness walk.
- New error codes `bad_request` (400) and `index_changed` (503).

## Known gaps

- Documentation code samples are production scope (ADR 0021). The per-root
  breakdown makes them visible (Langflow `docs`: 409 dynamic calls) but
  does not exclude them.
- The manifest list is fixed in code; a directory with calls but no
  manifest ancestor is grouped by its first path segment.
- Per-endpoint near misses can miss capped candidates;
  `near_misses_incomplete` counts the calls whose list was (or may have
  been) cut.
- A conditional tauri call whose node status is not `ambiguous` or
  `test_only` (for example `dynamic`) stores no candidates for its ambiguous
  or test-only alternatives (internal/extract/tauri), so it cannot be a near
  miss; http path entries carry their own candidates and are found.
- No OpenAPI operation gap list; Orval `calls_operation` is visible only as
  the caller's `operation` attribute.
- Reads are separate transactions; the runs check retries once and then
  fails with 503, it does not make the reads one snapshot. An ingest that
  commits and leaves the same run count and latest run (not possible with
  Graphvein's own runs, which are new IDs) would not be detected.
- `file_state` and IDE links read each presented file on every request.
- The fixture's Tauri app files carry an `.inert` suffix that the test
  removes, so self-indexing Graphvein gains no app registry and no other
  fixture's record changes (T016d Revision 1); the self-index does not exercise
  the app.
- UI (T016d2): the gap sheet renders every row (no windowing; Langflow's
  largest list is 422 rows); search and grouping run in the browser. A
  near-miss candidate count without `candidate_count_exact` is shown as a
  lower bound ("at least N"). The never-called sheet has no IDE links (the
  list endpoint presents no nodes); each row selects the endpoint instead.

## Review notes (delegated review, 2026-11-03)

- Delegated review in place of developer review (T016d1, server data).
  - The investigation: independent recount equal on every figure: Langflow production
    296 endpoints, 588 calls (160 linked / 6 unmatched / 422 dynamic),
    per root `src/frontend` 160/6/13 and `docs` 0/0/409, 149 never called;
    kftray all scopes 105 calls (101/2 wrapper/2 dynamic), 28 never
    invoked. No dynamic call has candidates. Latency on Langflow 29-63 ms.
  - Review 1 (independent): 1 major (the new fixture changed existing
    self-index records: extra suffix candidates on 8 ambiguous calls and a
    wrong 0.6 calls_route edge to `ANY /files/*`), 4 minor (reason texts
    asserting more than stored; capped candidate counts shown as exact;
    near-miss gate on node status), fixed in d46614e: unique single-segment
    fixture paths, Tauri fixture files stored as `.inert`, reason texts
    limited to stored data, `candidate_count_exact`, near misses gated on
    the path entry status for HTTP.
  - Re-check (fresh reviewer): self-index diff against fc1b48d 0 records
    outside the fixture and the changed files; missing 0.7 / 0.6 caller
    goldens added in 5f68f4f.
  - Found by the review, pre-existing (not this ADR): the client matcher
    lets an unresolved-base literal call suffix-match a route whose
    remainder has no literal segment (`ANY /files/*`, `GET /users/:id`),
    contrary to ADR 0018 ("Parameter-only suffixes never match"); fires 0
    times on the corpora and the self-index today; scheduled as its own
    precision task.
  - CI green including postgres (four helper skips; TestAPI* /postgres
    subtests pass, with byte-identical SQLite/Postgres bodies).

## Review notes, UI (delegated review, 2026-11-03)

- T016d2 (API section UI) reviewed against this API:
  - Review 1 (independent): no blocker; 2 major (detail/gap sheet could
    show a different index run than the list; never-called wording ignored
    the Production scope) and 3 minor findings, fixed in c9d05f8.
  - Re-check (fresh reviewer, Playwright on the real binary): mixed-run
    guard verified both directions with no reload loop; remaining
    "never called" labels in Production scope fixed in 85f3e17 through one
    scope-aware label.
  - UI numbers equal the live API on Langflow and kftray (258 checks); 0
    CSP violations and console errors; CI green.
