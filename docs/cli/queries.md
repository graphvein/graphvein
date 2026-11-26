# Graph query commands

`query <term> [path]`, `callers <target> [path]` and `impact <target> [path]`
accept `--repository` like status and `--json`. Repository ID precedence is
flag, root's `graphvein.yaml`, directory basename; see [configuration](../config.md).
Storage uses GRAPHVEIN_SQLITE_PATH or explicitly configured GRAPHVEIN_POSTGRES_URL.
These commands require an existing current-schema store, open it read-only, and
never index or migrate. With SQLite, a read that meets the lock of a running
`graphvein index` waits up to 3 s for it before failing as locked
([ADR 0043](../adr/0043-sqlite-read-busy-timeout.md)).
Freshness uses status's scan and ignore rules; stale facts produce a warning and successful reads remain exit 0.
For external snapshots, this reflects Graphvein's scan rather than the producer's
own file inventory.

Query supports `--kind`, `--file` (exact relative path), and positive `--limit`
(default 50); `total` counts every match, and the text form ends with
"N more not shown; raise --limit" when the limit cut the list. A term matches a canonical identity, exact display/qualified name,
or case-sensitive qualified-name suffix. A callers or impact target is
the one match that the term names: an identity, a name, or a suffix that
starts at a name boundary (`save` or `.save` for `User.save`, `db::open`
for `crate::db::open`). A suffix that starts inside a name (`Items` for
`listItems`) never resolves: with no other match the answer is "no
matching target" and lists those nodes as candidates ("names that end
with this term inside a name"); several matches are "ambiguous target"
with every candidate. Callers walks incoming calls to positive
`--depth` (default 1); impact walks incoming calls/imports/references to positive
depth (default 3). Both run on the shared breadth-first walk of change
impact and the architecture chain (`internal/traverse`): depth at most 10
(6 before T042; a project allows 10 too; usage error above it), at most 2,000 reached nodes and at most 200 new
nodes from one node per step; every cap that binds is reported (`caps`,
`bounds`), so a cut never reads as "nothing beyond". Impact also follows outgoing exposes_route, incoming calls_route and incoming
contains from a client_call to its owner. Route callers include their client
calls and owners (two edge distances even at default depth one). File impact
seeds all recursively contained symbols. Traversal
is cycle-safe and returns each reached node once at its shortest distance.
Ties choose the first path in kind/source/target order; results sort by distance
then identity. Targets and file seed symbols are excluded from reached results.

Target lookup with no match or multiple matches exits 1. Ambiguity lists canonical
identities to retry. Usage errors exit 2; storage/scan errors exit 1. Queries
with no matching nodes return an empty list successfully.

## Stable JSON shape

One UTF-8 JSON object followed by LF, with `freshness` first. Always-present
fields are:

- `freshness`: `{ "indexed": true, "changed_files": [] }`. Changed files are
  sorted relative paths covering additions, modifications and deletions.
- `command`: `query`, `callers` or `impact`.
- `nodes`: array of wrapped contract v1 node records for query; empty for traversals.
- `results`: array of `{ "node": <wrapped v1 node>, "distance": 1, "path": [<v1 edge>] }`.
  Paths run outward from the target, following incoming ordinary/calls_route/
  contains edges and outgoing exposes_route edges. Full evidence/provenance/
  confidence remain on each edge. Results reached through an inferred edge
  additionally carry provenance: inferred and confidence equal to the minimum
  along the selected shortest path; fact-only results retain their existing shape.
- `possibly_missing_callers`: integer, zero outside impact. For impact, the
  number of unresolved unknown call sites across the entire repository whose
  callee simple name matches the target or any reached node's simple name.
  Simple names discard any `@...` suffix, then take the final dot segment.
  This deliberately replaces the previous file-level sum; the JSON interface
  is not published before v1. These are possible callers, never proven edges.
- `possibly_missing_call_sites`: sorted array of
  `{"file", "span", "caller", "name", "matches": [<identity>...]}`, empty outside
  impact. Sorting is by file then start/end span; matches sort by identity.
  Caller is an enclosing symbol identity or null. External calls are excluded.
- `unknown_calls_in_result_files`: previous file-level sum, retained as context
  for impact (target file plus distinct files represented by reached nodes).
- `blind_spot_data`: true for impact when any indexed file node carries
  `unresolved_unknown_call_sites`, even an empty array. False means unavailable,
  as for external indexers that omit this attribute; the matched count is zero.
- `candidates`: sorted identity array, empty except ambiguous lookup.
- `caps`: the traversal caps that bound (`depth`, `reached_nodes`,
  `fan_out`, in that order), empty when none did and for query. `bounds`:
  one `{"cap", "step", "beyond"}` per bound cap and step: `beyond` counts
  the distinct nodes the cap kept out (for `depth`, the nodes one step past
  the last, at `step` depth + 1). The text form prints one line per bound,
  for example `depth cap reached (3): 1 beyond step 3`.
- `ends`: `{"node", "text"}` for each reached function or method (the
  target included) with no stored edge the walk follows, in result order:
  "? not modelled: no stored caller; dependency injection, framework
  callbacks, JSX renders, passed callbacks and dynamic calls are not
  followed by callers" (impact: "no stored caller, import, reference or
  route; ..."). The walk ends there because nothing is modelled, not
  because nothing calls it. Empty for query. The text form prints each.
- `total` (query only): the number of stored nodes matching the term;
  `nodes` holds at most `--limit` of them.

`target` is a wrapped v1 node and appears only on successful traversal target resolution.
Wrapped nodes retain every v1 field and add sibling `ide`:
`{"link":"vscode://file/...:1:1","line":1,"column":1,"exact":true}`, plus
`"line_only":true` (with `exact` false) when the link names the line without
a column (the `jetbrains` preset on a line with a tab before the node, or on
a file that changed since indexing), and `"caveat"` when a link converted
from unchanged source is still not exact: `tab_size_unknown` (the line-only
link after a tab) or `column_unit_assumed` (the `cursor` preset, whose
column unit is assumed from VS Code). Both are omitted otherwise.
Nodes without source spans have `ide: null`. Indexer contract records remain
unchanged. See [IDE configuration](../config.md) for units, encoding and inexact
fallback behavior.
`error` appears only for failed target resolution (`no matching target` or
`ambiguous target`). Lookup errors still include freshness. Other operational
errors go to stderr and do not emit a response. Empty arrays are always `[]`.
Human output starts with freshness, prints IDE links on node lines, includes
evidence locations and an inferred confidence label on reached result lines.
Impact ends with the matched count and up to 20 call
sites as file:line. Additional sites point to `--json`; missing data is explicitly
reported as unavailable. Field meanings and
the six fixture responses per language are pinned by CLI/backend golden tests.

## Project scope (`--project`)

`callers` and `impact` take `--project MANIFEST` (the manifest file or its
directory; T022, [ADR 0075](../adr/0075-multi-repository-projects.md)
"Query, impact and change journal"). They then read the project store and
traverse every member of [the project](project.md):

```
graphvein callers <target> --project MANIFEST [--repository MEMBER] [--depth N] [--json]
graphvein impact <target> --project MANIFEST [--repository MEMBER] [--depth N] [--json]
```

- No path: members and their checkouts come from the manifest.
  `--repository` names the member to look the target up in; without it
  every indexed member is searched, and a term found in several members is
  `ambiguous target` with the candidates as (repository, identity) pairs.
- Inside a member the traversal follows exactly the edges described above.
  From a route it also follows the project's stored `calls_route` links
  back to client calls of other members: callers of a backend route list
  the frontend's call sites and their enclosing functions; impact of a
  handler continues through its route into the frontend's graph.
  `documents_route` links are not followed here, as in-repository
  `documents_route` edges are not (`graphvein changes impact` follows
  both).
- Every result carries its `repository`; every path step carries
  `source_repository` and `target_repository`. A project link step has
  `project_link: true`, the link's `assumption` and its provenance and
  confidence, so a result reached through a link is `inferred` and at most
  as confident as the link; its `assumptions` are listed on the result.
- A stale link (a member indexed again since the link pass, a member that
  failed to index, or a manifest whose declarations changed) is still
  followed: its step has `stale: true` and `stale_reasons` (for example
  `frontend indexed again since the pass`), and the result `stale: true`.
  Without a stored link pass nothing crosses repositories.
- JSON: `{"project", "command", "members": [{"repository", "indexed",
  "checkout", "changed_files"}], "link_pass": {"stored", "pass", "links",
  "stale_reasons"}, "target", "results", "possibly_missing_callers",
  "possibly_missing_call_sites", "unknown_calls_in_result_files",
  "blind_spot_data", "possible_callers", "candidates", "caps", "bounds",
  "ends", "error"}`. `caps` and `bounds` are as above, with the same
  reached-node and fan-out caps; the project depth maximum is 10 (as the
  HTTP project endpoints), and `ends` entries carry their `repository`.
  `possible_callers` (T022 review) lists, per reached route (`route`),
  the client calls of other members the last link pass left `ambiguous`
  with the route among the candidates, or `incomplete` (PL-R7) with it
  among the indexed providers' candidates: `source`, `row`, `status`,
  `wording` ("ambiguous across repositories" or "not linked: a candidate
  provider had no stored index"), `reason`, `candidate_count` and
  `candidate_repositories`. They are unknowns, never results: the text
  output counts them in the blind-spot line ("N possible callers in other
  repositories (ambiguous or incomplete, not linked)") and lists each as
  "ROUTE may be called by REPO CALL (ambiguous across repositories; 2
  candidates in axum, express)". Golden:
  `testdata/fixtures/projectthree.cli-query.golden.json`. Blind spots are matched by
  name within each member whose nodes the traversal reached and carry the
  member's `repository`. A member without a checkout here has no IDE links
  and no changed files. Golden: `testdata/fixtures/projectimpact.cli-query.golden.json`.
- Text output: one freshness line per member, the link pass state, then
  `distance repository identity` per result; a project link step prints
  `(project link)`, its assumption and, when stale, why.
- `query` has no project mode; single-repository behaviour (no
  `--project`) is unchanged.

## graphvein changes

`graphvein changes [path]` shows what a change did to the stored facts and
what depends on them (ADR 0056, claim contract in
the T019 task record (internal)). It takes
`--repository` like the query commands and opens the store read-only,
except where noted.

```
graphvein changes [path] [--base previous|run:ID|pin:NAME|ref:BASE[..HEAD]]
                  [--group G] [--change C] [--cause C] [--kind K] [--tests] [--raw] [--json]
graphvein changes impact ID [path] [--base B] [--depth N] [--json]
graphvein changes pin NAME [path] [--run ID]
graphvein changes unpin NAME [path]
graphvein changes --project MANIFEST [--base previous|pass:N] [--json]
graphvein changes impact ID --project MANIFEST [--repository MEMBER] [--base B] [--depth N] [--json]
```

- `--base` defaults to `previous`, the run before the latest; `run:ID` is a
  journal run (`GET /api/v0/changes/bases` lists them), `pin:NAME` a
  pinned baseline. `ref:BASE` compares the merge base of `BASE` and the
  commit the current index recorded (not the live `HEAD`;
  `head_commit_differs` says when they differ, and an index without a
  recorded commit must be re-indexed) with the current index;
  `ref:BASE..HEAD` the merge base with `HEAD`'s snapshot; `ref:a...b` is
  refused. Ref snapshots that are not stored yet, or were made by another
  extractor version, are indexed first, under the index lock (ref names
  are resolved with local git only; nothing is fetched).
- The text output lists both sides, the counts per group (with moves and
  other changes) and one line per item: row, change, kind, id, the reason
  of a lost link or a downgrade, and the cause. `--tests` includes
  test-scope items (otherwise counted as hidden).
- `--json` prints `{"repository", "base", "summary", "items",
  "hidden_tests"}`: `summary` and each item have exactly the shapes of
  `GET /api/v0/changes/summary` and `/items` ([serve](serve.md#change-endpoints-adr-0056));
  `--raw` adds both stored payloads to every item. The fixture goldens
  `testdata/fixtures/change-impact.<case>.golden.json` are this document
  plus an `impacts` list.
- `changes impact ID` traverses outward from one item (its `id`, as in
  the item list) and prints the paths with relation, wording, provenance
  and confidence (an I8 path, code using a table, adds its verification,
  assumptions and, from a column, "column use not extracted"), each
  table's users ("N uses found (a lower bound)", then one "may also be
  used by" line per unknown site naming the table), then the unknowns
  (blind spots, near misses, gap nodes, relations not extracted yet) and
  binding caps; `--json` prints
  the document with `items: []` and one `impacts` entry (the HTTP impact
  shape without paging).
- `changes pin NAME [--run ID]` pins a journal run (default: the newest)
  and `changes unpin NAME` removes a pin; both take the index lock.
- With `--project MANIFEST` (T022):
  - `changes --project MANIFEST [--base previous|pass:N] [--json]` reports
    the project link journal: which links were added, removed or changed
    (payload differs other than in endpoint runs) between the base pass
    (`previous`: the pass before the newest; `pass:N`) and the newest
    pass, and whether the manifest's declarations changed; then each
    member's own comparison against its previous index as counts per
    group (an error for one member, such as a single run, stops none of
    the others). List a member's items with `graphvein changes` in its
    checkout. JSON: `{"project", "base", "state", "links": {"from_pass",
    "to_pass", "added", "removed", "changed", "members",
    "manifest_changed"}, "links_note", "members": [{"repository",
    "head_run", "base_run", "groups", "error"}]}`. `state` is the API's:
    `compared` (with `links`), `no_comparison` (fewer than two passes are
    kept) or `same_pass` (the base is the newest pass: nothing compared,
    which is never "no change"); `links` is null with a `links_note` in
    the last two, and the text output prints that note instead of counts.
  - `changes impact ID --project MANIFEST --repository MEMBER` (the
    member may be left out when the project has one) compares MEMBER in
    the project store and traverses from the item as above, then through
    the project's stored links: relation I12 (a `calls_route` link, to the
    consumer's client call) and I13 (a `documents_route` link, to the
    other member's OpenAPI operation), and on in that member's graph with
    the same relations and the remaining depth. Paths carry `repository`,
    steps `source_repository`/`target_repository`; a link step carries
    its `assumption` and, when stale, `stale_reasons`; the path lists its
    `assumptions` and is `stale` when one of its links is. A link is
    inferred, so every path through one is "may depend on" with at most
    the link's confidence. `impact.project` names the project and
    `impact.members` holds the unknowns and table uses of every other
    member reached, with `link_sources_missing` (stale links whose source
    the member no longer stores: not followed).
    `impact.possible_dependents` (T022 review) lists, per reached
    route, the sources of other members the last link pass left
    `ambiguous` or `incomplete` with the route among their candidates:
    claim "may depend on", provenance `inferred`, relation I12 or I13 with
    its wording, `route`/`route_repository`, `row`, `status` and `reason`
    (why it is not a link). They are never paths and never followed; the
    text output lists each after the paths ("not a link: ...") and counts
    them on the Unknowns line. Golden:
    `testdata/fixtures/projectthree.cli-query.golden.json`. Ref bases are refused
    (exit 2): project links name the members' current indexes. Golden:
    `testdata/fixtures/change-impact.project.golden.json`.
  - `pin` and `unpin` stay per repository (exit 2 with `--project`).

Exit codes: 0 on success, 2 for usage errors (an unknown base form, a
missing ID), 1 for everything else, including the first run (no previous
run to compare with), an unknown or expired base, an unknown pin, a held
index lock and git or ref errors.
