# ADR 0045 - Flow graph endpoint

**Status:** Accepted (delegated review, 2026-11-03)

## Context

The developer decided (2026-11-03, the T016 task record (internal),
"Graph views", and the T016f task record (internal)) on a Flow tab in the API
detail pane: for the selected HTTP route or Tauri IPC command, who calls it
and what it runs, one or two hops each way. VISION's precision principle
governs it: every drawn edge is a stored edge with its
stored provenance and confidence, or an explicitly marked non-edge (a near
miss's recorded candidate). Nothing is inferred to fill a gap.

The existing endpoints cannot supply this graph (T016f F1):
`/api/v0/api/endpoint` (ADR 0042) gives the callers, their owners and near
misses, but not the callers of an owner, the handler's callees, any `calls`
edge, or the other targets of a conditional call. The store interface
already offers what is needed (`Edges`, `NodesByIdentity`, `Nodes`,
`FileHashes`, `Runs`), so no store, contract or extractor change is made.

Measured at task-writing time (T016f F2, F4; Langflow 68,069 nodes and
kftray 6,648 nodes): `calls` edges are all `fact` 1.0 with one evidence
span; function, method and class nodes carry no `scope` attribute; most
Langflow call sites sit in functions nested in hooks, and 100 of 148 routes
with callers get hop-2 callers only through the enclosing function; call
sites per route max 3, hop-1 callees max 20, hop-2 callees max 39, largest
two-hop neighbourhood 57 nodes. These figures were not re-measured in
T016f1's implementation run (see Known gaps).

## Options considered and decisions

### 1. Data source

- **One flow endpoint, `GET /api/v0/api/flow`, sharing `/endpoint`'s caller
  and near-miss code (chosen).** The selection moved out of
  `computeEndpoint` into `selectCalls` (`internal/server/api.go`) with no
  change of `/endpoint` output (its goldens are unchanged); `/flow` calls the
  same function, so the Flow tab's numbers equal the Callers table's by
  construction, and a test compares them for every endpoint of every test
  repository in four scope/root combinations.
- Composing existing endpoints: cannot, they lack the edges above (F1).
- A generic neighbours endpoint: every client would re-implement the count
  semantics (ADR 0042 option 1).

### 2. Hops

- **Both hops in one response, filtered on the client (chosen).** `1 hop` /
  `2 hops` and the near-miss toggle never change a count or re-request.
- A `hops` parameter: a toggle would re-request and could see another
  index state.

### 3. Roles and the enclosing step (UQ1)

Nodes appear once with every role they have (`roles`, in table order) and
their smallest `hop`:

| role | hop | rule |
|---|---|---|
| `endpoint` | 0 | the requested node |
| `handler` | 0 | the stored node named by `handler` (states as in `/endpoint`) |
| `call` | 1 | the callers of `/endpoint` (scope and root apply) |
| `owner` | 1 | source of each call's stored `contains` edge (first by identity, as `/endpoint`'s `owner`) |
| `enclosing` | 1 | source of a caller owner's stored `contains` edge, when it is a `function` or `method`; one step, never further |
| `other_target` | 1 | other targets of a call's stored call edges (a conditional call) |
| `near_miss` | 1 | the near misses of `/endpoint` |
| `near_miss_owner` | 1 | source of each near miss's stored `contains` edge (the same rule as `owner`) |
| `callee` | 1, 2 | targets of the handler's stored outgoing `calls`; then of those callees' |
| `caller` | 2 | sources of stored incoming `calls` of each caller owner and enclosing function |

**UQ1: option B, decided by the developer (2026-11-03), with these
safeguards:**

- The enclosing step is a stored containment fact only: the stored
  `contains` edge from the enclosing function or method to the owner,
  reported verbatim. Its edge kind is distinct from `calls`; the UI labels
  it "defined in". It carries no confidence tier in the UI (D3's `contains`
  row draws no confidence label).
- Hop-2 callers reached through it are reported as callers of the
  enclosing function: their `calls` edge targets the enclosing node, never
  the call or the endpoint (for example "calls useGetFlow (contains this
  call)"). They are never counted or reported as route callers:
  `counts.callers` and the `call` role come only from `selectCalls`, and
  the count-equality test covers a snapshot with an enclosing function and
  its callers.
- One step only: the enclosing function's own container and its callers
  are not included (tested). A container of another kind (`file`, `class`)
  is not an enclosing step.
- Near misses get their owner but no enclosing step and no hop-2 callers:
  they are not callers. The owner of a near miss has its own role,
  `near_miss_owner`, so the near-miss toggle can hide it by role without
  following edges; a node that owns a call and a near miss has both
  `owner` and `near_miss_owner` and stays visible while the toggle is off.

Rejected: A (owner callers only: Langflow's upstream hop 2 would be mostly
empty), C (drawing the enclosing function's callers into the owner card:
the drawn edge would target a node it does not target in the index).

### 4. Edges

- **Traversal edges plus the induced `calls` subgraph (chosen).** Every
  stored edge used to reach a node (`calls_route` / `invokes_command`,
  `exposes_*`, `contains`, `calls`) plus every stored `calls` edge between
  two included nodes, so a node reached twice shows both edges and
  recursion shows a self-loop. Edges are reported verbatim (`kind`,
  `source`, `target`, `provenance`, `confidence`, `evidence`); http call
  edges add `match` (`linkedMatch` of ADR 0042, null when no linked entry
  says so). An edge whose other end is not a stored node is skipped and
  counted in `skipped_edges` (the store rejects such edges at ingest, so
  this is defensive; a test injects one).
- Traversal edges only: hides a stored relation between two drawn nodes.

### 5. Non-edges

- **A separate `candidates` list (chosen):** one entry per near miss
  (`call`, `endpoint`, `relation`, `candidate_count`,
  `candidate_count_exact`); only the requested endpoint is listed as the
  candidate target, the tie size is in the count.
- Edges with a flag: a client could draw them as links.

### 6. Caps

Fan-out 25 `calls` neighbours per node and direction; the node's
`omitted.callers` / `omitted.callees` count the neighbours the cap cut that
no returned edge reaches. They are computed after the induced subgraph and
any hop-2 drop: a cut neighbour can still be returned another way (a
handler callee cut at hop 1 that another callee calls, so it appears at hop
2 and the handler's edge to it is induced; a hop-2 caller reached from two
expanded nodes), and then it is not hidden and not counted. 200 callers and
200 near misses (`calls_cap`, `near_misses_cap`; counts stay uncut); 300
nodes and 600 edges in total, beyond which hop 2 is dropped whole
(`hop2_dropped`), including roles and cuts that only hop 2 gave. Order
inside a cap: file, span start, identity. `truncated` is null or
{`nodes_omitted`, `edges_omitted`, `reasons`}. Under `calls_cap` and
`near_misses_cap`, `nodes_omitted` counts only the cut call and near-miss
nodes (not their owners or enclosing functions, which may be shared with
returned calls, nor other targets or hop-2 callers they would have led to),
and `edges_omitted` counts, for cut calls, only their stored edges to the
endpoint. Under `hop2_dropped` both count the removed hop-2 nodes and the
edges touching them. Rejected: no caps (an unbounded response for a hub
function), paging (the graph is drawn whole).

Hop 1 is never cut, so it can exceed the totals alone; then the reason
`hop1_over_totals` is reported (with `hop2_dropped` too if hop-2 nodes were
removed first). It adds nothing to `nodes_omitted` or `edges_omitted`,
since nothing is left out; the returned `nodes` and `edges` show the
excess. Worst case of hop 1 after the calls caps: 2 (endpoint and handler)
+ 200 calls + 200 near misses + up to 200 owners + up to 200 near-miss
owners + up to 200 enclosing functions + 25 handler callees = 1,027 nodes,
plus the other targets of the 200 calls (bounded only by their stored call
edges; a conditional call has a few). Edges: 1 `exposes_*` + the 200 calls'
call edges (at least 200) + up to 600 `contains` + 25 `calls`, plus the
induced `calls` edges among the hop-1 nodes, bounded only by the number of
pairs. The measured largest two-hop neighbourhood (57 nodes, Context) is
far below the totals, so this is reached only by a hub endpoint.

### 7. Scope of hop-2 nodes

- **No scope filter for hop-2 functions (chosen):** function, method and
  class nodes store no scope (F2). Scope and root filter only call and
  near-miss nodes, as in `/endpoint`; other targets are shown with their
  raw `scope` attribute.
- Path heuristics (`test/`, `.test.ts`): guesses from names; rejected.

### 8. UI, dependencies and size (T016f2)

To be added by T016f2 (React Flow and ELK versions, licence handling per
UQ2, which is still open, the same-origin worker under the unchanged CSP,
the lazy chunk, the raised embedded-size bound with measurements).

## Consequences

- New read-only endpoint under HTTP API v0 with the API section's
  parameters, errors (400, 404, 500, 503 `index_changed`), headers and the
  runs-before/after check; it goes through the existing security chain
  (Host allow-list, `Sec-Fetch-Site`/`Origin`, GET/HEAD only, CSP).
- `internal/server/api.go` shares `endpointParams`, `selectCalls`,
  `reader.containers` and `handlerWithEdge` between `/endpoint` and
  `/flow`.
- Goldens on SQLite and Postgres (byte-identical bodies) for the
  api-section fixture and a crafted snapshot
  (`internal/server/testdata/flow-graph.ndjson`); the caps are tested on a
  snapshot generated in the test.

## Known gaps

- No expansion beyond two hops; `omitted` chips are not expandable.
- Hop 1 is not bounded by the totals (`hop1_over_totals`, §6); a hub
  endpoint with 200 calls in distinct functions can return over 1,000
  nodes.
- `nodes_omitted` under the calls caps does not count the owners,
  enclosing functions, other targets or hop-2 callers the cut calls would
  have added.
- Hop-2 nodes are not scope filtered (no stored scope).
- Edge kinds other than the ones named are never read, so an unknown edge
  kind does not appear (unknown provenances on read kinds pass through).
- Evidence spans are returned as data, not peeked.
- The induced subgraph may add crossings.
- `file_state` and IDE links read each presented file per request.
- Reads are separate transactions (retry once, then 503).
- T016f1's investigation step A1 recount and the Langflow/kftray gates (acceptance route
  and command, counts over the live API, latency) are run by a separate
  gates step, not in the implementation run; the caps above are the task's
  figures until then.

## Review notes (delegated review, 2026-11-03)

- Independent review of T016f1: no blocker or major findings; every returned edge is a stored edge returned as stored; near misses only in `candidates`; the enclosing step is one stored `contains` step from a caller's owner to a function or method; scope and root applied once in `selectCalls` for both endpoints; `/endpoint` goldens unchanged by the refactor.
- Revision after review: `omitted` recomputed after induction and hop-2 drop (it overstated hidden neighbours when an induced edge reached a cut node); `hop1_over_totals` reason; distinct `near_miss_owner` role; tests for the hop-2 drop paths. A focused re-check found no wrong count or implied link.
- Gates (2026-11-03): on Langflow (318 routes) and kftray (115 commands), in both scopes, flow counts, call and candidate identities equal `/endpoint` (0 mismatches); no truncation; acceptance sets for GET /api/v1/flows/{flow_id} and start_port_forward_tcp_cmd equal the task's F2; hop-2 callers on 138 of 148 Langflow routes with callers; latency medians 36-67 ms on Langflow, 9 ms on kftray. CI green; postgres job only the four helper skips and PASS for TestFlowGoldens, TestFlowChecks, TestFlowCountsEqualEndpoint and TestFlowCaps /postgres.
- Known gaps kept: hop 1 is never cut (worst case about 1,000 nodes, reported as `hop1_over_totals`); hop-2 functions carry no scope; `nodes_omitted` under the call caps counts only cut call nodes.

## Revision R1: tables and nested functions (T026 AG-C3, AG18)

**Status:** Accepted (delegated review, 2026-11-19; this revision only).

Context: the Architecture section's chain (ADR 0079) folds long chains with
"Open in Flow tab", and the developer decided the Flow tab gains a Tables
column (T026 AG18). Langflow's `PATCH /api/v1/flows/{flow_id}` reaches its
write to `flow` only through the handler's nested `update_flow.operation`
(chain row CH7), which §3's roles did not follow.

Options: (a) a separate tables endpoint the tab joins with `/flow` (two
reads that can see two index states, ADR 0042's 409 logic twice);
(b) additive fields and roles on `/flow` (chosen); (c) the chain endpoint
in the Flow tab (another graph shape and other caps).

Decision (additive; every existing node, edge, count and cap is unchanged
when a handler has no nested function and reaches no table):

- Role `nested` (hop 0, 1, 2): functions and methods the handler, a nested
  function or a callee contains by a stored `contains` edge, transitively
  up to 8 levels, at the container's hop. Callees of nested functions are
  callees of the next hop, so the hop limits stay two each way. Containers
  of another kind (a class) are not followed.
- Role `table` (hop 1, 2): targets of stored `reads_table` /
  `writes_table` edges of handler-side functions (handler, nested,
  callee), at the accessor's hop and at least 1. Upstream functions never
  contribute tables. The edges are returned verbatim with an added
  `wording` (chain row CH6).
- `may_also_use`: the `table_access_unknown` sites of the returned
  handler-side functions, as stored (a partial unknown, never an edge).
- `tables_beyond`: counts of the tables, functions and unknown sites one
  hop past hop 2 (callees of the hop-2 functions and their nested
  functions, read without a fan-out cap); null when hop 2 was dropped or no
  handler node is stored. Never presented as an absence: deeper hops are
  not read.
- Caps: nested functions and tables count towards the 300-node and
  600-edge totals and are dropped with hop 2; no own fan-out cap (a
  function has a few nested functions and tables).
- UI: a Tables column, two state boxes ("may also use", "+ n at hop 3")
  that no line touches, a "Reads / writes" tier; "Open in Flow tab" adds
  `back=` and starts at the downstream side (docs/cli/serve.md).

Consequences: goldens gain `may_also_use: []` and `tables_beyond` (no
other change on the existing fixtures); a crafted snapshot `flowtables`
(`internal/server/flow_tables_test.go`) has a golden on SQLite and
Postgres with equal bodies. Known gaps: the hop-3 count does not follow
the callees the fan-out cap cut at hop 1 or 2 (the UI names them as not
read); `contains` to lambdas or closures stored as other kinds is not
followed; test-scope functions are not filtered (§7).
