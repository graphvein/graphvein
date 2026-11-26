# ADR 0075 - Multi-repository projects

**Status:** Accepted (delegated review, 2026-11-19)

## Context

VISION.md (Foundation) promises projects that span several repositories,
linked across repositories, with each repository's identities kept stable.
The project plan moves this to launch 1 (item 2; `ROADMAP.md`) and requires the identity decision
before any rationale or disposition persists (M4, first bullet), because
those attach to node identity + content hash.

Today (inventory: the T022 task record (internal)):

- Identity is `kind:path#qualified-name`, repository-relative. The
  repository is a separate key (ADR 0005), and the store already holds
  several repositories side by side, isolated.
- Every store table keys on repository; an edge's two endpoints must be in
  the same repository.
- Cross-boundary linkers (client calls, Orval, OpenAPI documents_route,
  Tauri) run inside the indexer over one root's parse facts.
- Measured on RealWorld: react and express indexed separately link nothing;
  the composite copy links 27/27 Orval calls (exact 0.7) and 19
  documents_route. The composite changes about 97% of identities (path
  prefix). React's base URL is an environment value whose hosts and ports
  match neither backend, so the composite link rests only on co-location.
  With two backends in one composite, all 27 calls are ambiguous.

## Options considered

**A. Multi-root workspace indexed as one logical repository** with a
per-root identity prefix (`frontend/src/...`).
- Plus: reuses today's in-indexer linkers unchanged; composite tests already
  do this.
- Minus: identities depend on the project layout. Adding, removing or
  renaming a root's folder changes every identity in it, so rationale keyed
  on identity is lost (measured: 1216 of 1250 express identities change).
  All checkouts must be on disk to index any of them. One journal, one lock,
  one freshness for everything. Two teams sharing a backend in two projects
  get two different identities for the same code. Does not solve the
  two-provider ambiguity.

**B. Separate per-repository indexes, a project manifest, and a link-time
cross-repository linker over stored nodes.**
- Plus: identities never change (the repository is already a separate key).
  Each repository keeps its own index, fact cache, journal, pins, lock and
  freshness, and can be indexed alone. A repository can belong to several
  projects. The manifest is the natural place to declare the
  consumer -> provider assumption.
- Minus: a new linker pass over stored nodes (matcher must work from
  contract attributes only, so in-repository proofs the matcher needs must
  be stored as attributes: the Express equivalence proof and the
  per-alternative base); a new store table for links that cross
  repositories, with its own invalidation; project-aware query, impact,
  server and UI.

**C. Hybrid:** B for identity and storage, but the link pass parses both
repositories again in one process (A's linkers) when both checkouts are
present, falling back to stored nodes otherwise.
- Plus: keeps every in-indexer proof.
- Minus: two code paths with different results depending on what is on disk
  (the same project could link differently on two machines); doubles the
  parse cost. Rejected as a default; the missing proof can instead be stored
  as a route attribute later (contract-v1 open attribute) if measured
  necessary.

## Decision (B; open points decided by the developer on 2026-11-18)

Option B. The developer's decisions on the investigation open points are recorded
in the T022 task record (internal) ("Decisions (developer, 2026-11-18)")
and applied below.

### Identity rules

1. A node's global key is the pair **(repository ID, identity)**. The
   identity string is unchanged; no repository segment is added. Existing
   indexes stay valid; no re-index, no contract change.
2. Adding a repository to, or removing it from, a project never changes any
   identity, in that repository or another.
3. A cross-repository reference is always stored and sent as two fields
   (`repository`, `identity`), never as one concatenated string (API and
   MCP alike).
4. Repository IDs of project members are set in the project manifest
   (`repository:` per member, required). A member's own `graphvein.yaml`
   `repository:` wins: `graphvein project init` and `project add` copy it
   into the manifest, and a manifest name that differs from it is refused
   whenever the member's checkout is read. The directory name is **never**
   used for a project member (it differs between machines). A remote URL is
   never the ID (forks, SSH vs HTTPS); it may be recorded as metadata and
   shown.
5. IDs stay nonempty, without NUL and without `@ref:`. A project member ID
   must also be unique within the store (two projects sharing one store and
   one backend share that repository's index).

### Project configuration

- One format, `graphvein-project.yaml`, either in a separate project folder
  (often the parent of the checkouts) or inside one member, pointing at
  siblings by relative path. Machine-specific paths go in an uncommitted
  `graphvein-project.local.yaml` next to it, which may only override member
  paths. No flags-only mode.
- Contents: `version: 1`, a project `name` (same character rules as a
  repository ID), and `members`, each with `repository` (rule 4), `path`
  (relative to the manifest's directory; absolute paths only in the local
  file; optional, because a member's facts may come from a shared store
  without a local checkout), optional `consumes` and optional `scopes`.
- `consumes` (optional) narrows the providers a member's (or scope's) calls
  may link to: a list of member repositories or `repository` + `scope`
  pairs, each with an optional selector (`bases`: base expressions or
  environment member names; `origins`: literal `http(s)` origins;
  `path_prefixes`). Without `consumes`, every member is a candidate
  provider (see below).
- `scopes` (optional) declare link scopes: named folders inside a member,
  not nested or overlapping; the rest of the member is its default scope.
- `graphvein project init` writes the manifest from the named repositories;
  `graphvein project add <path>` adds one. Both read each member's
  `graphvein.yaml`, refuse what rule 4 refuses, and never write a partial
  file. Format and validation rules: `docs/cli/project.md`.
- The project store is one store for all members: SQLite
  `<manifest dir>/.graphvein/index.db` by default, or the configured
  Postgres. Members indexed alone keep their own store (unchanged).

### Cross-repository links

- Produced by an application-side **project link pass** (ARCHITECTURE
  pipeline step 5, "Link"), not by the indexer, so contract v1 is unchanged
  and every per-repository snapshot stays a complete, self-contained
  snapshot.
- Inputs: stored `client_call` nodes (and Orval/OpenAPI operation nodes) of
  every member, stored `route` nodes of every candidate provider (all other
  members and scopes, or only those a `consumes` declaration names), read
  in one read transaction. Matching reuses the clients matcher over contract
  attributes (method, `paths`, `candidates`, base); tiers and confidences
  stay those of ADR 0018/0027 (0.9 / 0.7 / 0.6).
- Stored in a new table owned by the **project**, not by either repository:
  `project_links(project, kind, source_repository, source_identity,
  target_repository, target_identity, payload)` with the payload carrying
  provenance `inferred`, confidence, evidence, the assumption (by default
  "matched across project repositories; base URL not proven"; with a
  declaration, the `consumes` entry and selector used), and the
  source and target repositories' run IDs and node content hashes it was
  computed from.
- A call's own snapshot keeps its in-repository status (for example
  `unmatched`); project views overlay the project status from
  `project_links` and say so.
- Invalidation: every publish of a member run, and every manifest change
  (hashed), marks the project's links stale; the link pass recomputes them
  for the whole project (it is cheap: measured inputs are tens of calls and
  routes; recompute, never patch). A link is **fresh** only when both
  members' latest runs equal the run IDs it records; otherwise it is shown
  as stale, with the member that changed.
- Kinds in the first slice: `calls_route` (fetch/axios/Orval client calls),
  `documents_route` (an OpenAPI spec in one repository, routes in another).
  Tauri and Angular cross-repository links are out of scope until a corpus
  needs them.

### Storage

Migration 0005 (both dialects; `migrations.Version` 5) adds four tables.
Text keys use `COLLATE "C"` on Postgres, as in migration 0004, so both
backends order keys in byte order.

- `projects(project PK, payload)`: the project name and the manifest hash
  of the last link pass.
- `project_members(project, repository, ordinal, payload)`, key
  (project, repository): each member in manifest order with the run the
  last link pass read (empty when the member had no stored index). No
  foreign key to `repositories`: a member may not be indexed yet.
- `project_links(project, kind, source_repository, source_identity,
  target_repository, target_identity, payload)`, that key as primary key,
  with indexes on each endpoint. Both endpoint repositories reference
  `project_members`; there is deliberately no foreign key to `nodes`, so a
  member re-index never deletes links silently: they are kept and shown
  stale until the next pass. The payload is `store.ProjectLink`: endpoints
  as (repository, identity, run, content hash), kind, provenance,
  confidence, evidence, assumption and pass-specific attributes.
- `project_link_runs(project, seq, payload)`: the link journal. One row per
  pass with its member runs, manifest hash, added/removed/changed counts
  and the link set as keys with payload hashes (not payloads); the newest
  30 passes per project are kept.
- `project_outcomes(project, ordinal, source_repository, source_identity,
  status, payload)` (added to migration 0005, before release):
  every source's outcome of the last pass (`store.ProjectOutcome`: row,
  status, reason, target, candidates), linked or not, replaced in full by
  each pass in the same transaction as the links, so views never rerun
  the pass to say why a call has no link.

Store methods: `Repositories` (every stored repository, ref namespaces
excluded, with its latest run by finish time), `ProjectLinks` (project,
members, links and pass number in one read transaction; unknown project is
`ErrProjectUnknown`), `ReplaceProjectLinks` and `ProjectLinkRuns`.
`ReplaceProjectLinks` writes project, members, the complete link set, the
outcomes and the journal row in one transaction, writing only links whose
payload changed. A link whose payload differs only in its endpoints' runs
(a member indexed again with the same content) is rewritten, so stored
runs are the members' latest, but counted as unchanged; the journal's
payload hash leaves the runs out for the same reason. A member
record may carry the error that stopped `graphvein index --project` from
indexing it before the pass (`index_error`): the pass linked its previous
index, and readers show those links as stale. It refuses, writing nothing, an invalid set (`ErrInvalid`: an
endpoint outside the members, an endpoint run other than the member run
the pass recorded, a confidence outside (0, 1], an empty assumption,
duplicate keys) and a stale one (`ErrProjectStale`: a member run that is
not that member's latest run, or an endpoint node not stored with the
recorded content hash). Freshness is computed by readers from stored data:
the last pass's member runs against `Repositories`, and its manifest hash
against the current manifest; nothing is written to mark links stale.

Project lock: `postgres.ProjectLocker` is a session advisory lock in its
own class (714839212) with key `LockKey(schema, project)`;
`sqlite.ProjectLocker` is the store's index lock.

`RetireProjectMember(project, repository)` removes a member
renamed from the UI, in one transaction: the project's links naming it,
the outcomes whose source, target or candidates name it, its member
record, and its whole stored index (runs, files, nodes, edges,
diagnostics, fact cache, change journal and pins, ref snapshots). It
refuses (`ErrRepositoryShared`), deleting nothing, a repository another
stored project names, and a ref namespace. The link journal keeps its
past passes. Options considered for the renamed member's rows: (a) keep
them until removed by hand (B10; orphaned rows under a name no manifest
uses); (b) migrate them to the new name (no re-index needed, but every
run header and journal payload stores the repository name, and both
backends' foreign keys would have to be rewritten in place); (c) remove
them with the rename and re-index under the new name (chosen: the
re-index was already offered, and nothing is rewritten). Removing at the
next link pass instead would need the rename remembered between the
write and the pass (across restarts and CLI passes), which the store
does not hold.

### Project index

`graphvein index --project` indexes each member checkout into the project
store under its own index lock (released before the next member), then
runs the pass under the project lock; `graphvein status --project` reports
member freshness, the pass state and stale links
([docs/cli/project.md](../cli/project.md)). The manifest hash is computed
over the declarations the pass reads (project name, member order,
`consumes`, `scopes`), not the file bytes: paths and comments do not mark
links stale. A member that fails to index does not stop the others; the
pass runs with what is stored and records the failure. Options considered
for that case: leave the member out of the pass (its sources and every
call it could serve become `incomplete`, losing links that were right a
moment ago), or abort the pass (one broken member blocks the project).
Linking its previous index and marking those links stale keeps the
links visible with an honest freshness.

### Link pass

`internal/projectlink` implements the pass; its claim contract table is
`docs/extract/project-links.md`. Each candidate provider is matched
separately with `clients.NewStoredRouteMatcher` (stored route nodes only);
two or more providers with a candidate give `ambiguous`, even when one
provider's route is more specific. Only sources the member's own index
left `unmatched` are linked (and, since step B5, sources left `ambiguous`
that a link scope declaration resolves); a candidate provider without a
stored index links nothing. Every source
gets an outcome with its row; outcomes are returned, not yet stored (T022
"B3 notes").

Two extractor outputs make the stored matcher equal to the in-repository
one (extractors-47; additive contract-v1 attributes, contract
version unchanged):

- ADR 0022's Express equivalence proof is stored as the route attribute
  `equivalent_registrations`: the identities, in source order, of a group
  of two or more registrations sharing one proof (routing object, method,
  raw pattern, non-inline handler, middleware). The routing object key
  stays internal. The stored matcher treats two routes as equivalent
  exactly when they carry the same group, so it collapses the ties the
  in-repository linker collapses, to the same first registration.
  Options considered: a registration-group key (an opaque hash, unreadable
  in the UI), the canonical first identity only (loses the group), and
  approximating the proof from stored attributes (unsafe: two routers in
  one file can serve different servers). The identity list was chosen.
- Client calls with more than one alternative, or a conditional one,
  store `base` and `base_expression` per alternative (the call-level
  values are the last alternative's). The pass classifies and matches each
  alternative as its own source; `bases` selectors compare the
  alternative's own expression. An expression that differs between values
  merged into one alternative is not stored. Option rejected: storing the
  per-alternative base on every call (single unconditional calls are
  decided by the call-level attributes; more churn, no gain).

`TestStoredMatcherReplaysClientLinks` checks on ten client fixtures that
the stored attributes reproduce every in-repository alternative result.
Measured: react + express 27/27 calls and 19/19 operations as the
composite (21 and 17 before); langflow `src/frontend` + `src/backend` as a
two-member project reproduce all 167 composite links, 18 of them from 16
calls with several alternatives (PL-R3 before), 0 wrong.

### Link scopes

A member's declared scopes are link endpoints: each scope (the routes in
its folder) and the member's default scope (its other routes) has its own
matcher, as a member has across repositories, so a call matching routes of
two scopes is ambiguous (PL-R11) even without declarations. A source in a
declared scope uses that scope's `consumes` (no inheritance from the
member); a `consumes` entry with `scope` names one endpoint, an entry
without it every endpoint of the repository. The pass resolves a source
its own index left `ambiguous` as a project overlay only through a
declaration naming a scope of its own repository, and only by narrowing:
exactly one covered endpoint has a candidate, it is an own scope, its
matcher links one route, and that route is one of the in-repository ties
(replayed by the member's whole-member matcher). Everything else stays
ambiguous (PL-S3) or keeps its own result (PL-R1). Overlay links are
ordinary project links whose source and target are in the same member,
with an assumption ending "resolves an in-repository ambiguity across link
scopes"; the member's snapshot is unchanged. Claim rows PL-S1 to PL-S3
(docs/extract/project-links.md); B3's PL-R4 refusal is removed.

Options considered: (a) scopes only filter the candidates of declared
consumers and leave undeclared matching per member (a call matching two
scopes of one provider would link by specificity across servers, which
B3 refuses across members); (b) let a declaration override an
in-repository link as well (it would change a decided result on a
declaration alone; not asked, left as a follow-up); (c) accept any route
of the declared scope, even one outside the in-repository ties (a weaker
or less specific route would win over the ties). The chosen rule is the
narrowest: it can turn a link into an ambiguity, and resolves an
ambiguity only to one of its own candidates. A shorthand string
`repository/scope` in `consumes` was not added: repository IDs may contain
`/`, so the mapping form (`repository`, `scope`) stays the only one.

### Precision

- A cross-repository link is never more confident than its weakest
  evidence: the path match tier of ADR 0018/0027, with the same confidence
  as inside one repository. Every cross-repository link shows its
  assumption: "matched across project repositories; base URL not proven",
  or the declaration it used ("declared in graphvein-project.yaml:
  `frontend` consumes `express`").
- Several candidate providers for one call give `ambiguous` with the
  candidates listed, never a link chosen by order (measured: react +
  express + axum-ddd, 27 ambiguous, 0 wrong). A `consumes` declaration or
  a selector per provider narrows the candidates.
- Base URL values from environment files, build defines or deployment
  configuration never select a provider by themselves. A call with a
  literal origin links only through a declaration whose selector names that
  origin.
- Suggestions (environment files, compose files, dev-server proxy configs)
  propose `consumes` entries with their evidence; they change nothing until
  the user confirms them, and a confirmed suggestion is an ordinary
  declaration in the manifest.
- Link scopes apply the same rules between scopes of one repository: the
  project pass may resolve an in-repository call that is ambiguous across
  scopes only through a declaration, as a project overlay; the member's
  own snapshot is unchanged.
- Precision labels: a new fact-family row for cross-repository links
  (inferred, assumption shown); member file labels unchanged.
- **Precision rule.** The family is
  `cross_repository_links` (`precision.FamilyCrossRepositoryLinks`). It is
  not a PL-F row of the rule table, because every row there earns
  `precise`; instead it is a family that is never precise:
  `precision.ScopeLabel` answers `best_effort`, row D, `default_branch`
  for it on every run before consulting any row, and `TestPrecisionRules`
  refuses a row that names it. Options considered: (a) a PL-F row with a
  "never" label (it would change the table's meaning and digest, and
  need an extractor version bump for no label change); (b) only a
  sentence in row D (nothing would stop a later row from naming the
  family); (c) a named row D family with a guard in `ScopeLabel` and the
  table test (chosen). The fact label stays `inferred` with the path
  tier's confidence and the link's `assumption`. `/api/v0/precision`
  counts the family under `serve --project` (links with an end in the
  member), and the project parts of `/api/v0/api/endpoint`, `/flow` and
  `/calls` carry `links_precision`; docs/extract/precision.md "Row D
  families" and "Cross-repository links".

### Query, impact and change journal

- Project-scoped traversals join each member's graph through
  `project_links`: callers of a route include calls in consumer
  repositories; impact of a route change continues into the consumer's
  graph. Result rows carry the repository of each node.
- Per-repository journals, pins and ref snapshots stay as they are. The
  project keeps a small link journal (link set per link pass, before
  payloads), so "since the previous index" at project scope reports links
  added, removed or changed with the member changes. A branch comparison of
  one member computes project links for the ref snapshot in memory (not
  stored).

### Query and impact

- **Project-scoped reads.** A read names a project, a node key
  (repository, identity) and reads the members' stores; links come from
  the last stored pass through `projectlink.View`, which also computes
  freshness (shared with `status --project`, so "stale" means the same
  everywhere). `graphvein callers|impact --project`
  (`query.ExecuteProject`) follows, inside each member, exactly the edges
  of the single-repository commands, and from a route the stored
  `calls_route` links back to client calls of other members. `graphvein
  changes impact --project` (`changes.ImpactRequest.Project`) continues
  the ADR 0056 traversal from every reached route of the compared member
  through `calls_route` (relation I12) and `documents_route` (I13) links
  and on in the other member's current graph, with the remaining depth,
  the same relations and caps (one reached-node budget for the whole
  project); new rows I12 and I13 in T019's relation table, held 1:1 by
  `TestChangeClaimContract` and exercised by
  `change-impact.project.golden.json`.
- **Provenance.** A link step has the link's provenance (`inferred`),
  confidence, evidence and assumption, so every path through it is "may
  depend on" and at most as confident as the link (ADR 0056: path
  confidence is the minimum); the path lists the assumptions. A stale
  link is followed, never dropped: its step carries the reasons and the
  path is `stale`. A link whose source the consumer no longer stores is
  not followed and is listed per member (`link_sources_missing`).
  Reached nodes of another member take that member's latest journal run's
  gap files (no journal run: the whole member is a gap, every path
  inferred).
- **Results carry the repository** on every node, step and path at
  project scope; single-repository outputs are unchanged (the new fields
  are omitted there).
- **Link journal diff.** `changes.DiffProjectLinks` compares two kept
  passes' link sets (keys with payload hashes): added, removed, changed,
  and whether the declarations changed. `graphvein changes --project`
  shows it (base `previous` or `pass:N`) with each member's own
  comparison counts.
- **Options considered.** (1) Make the traversals key-aware throughout
  (visited by (repository, identity)) in one graph; (2) one traversal per
  member graph, joined at project links (chosen): the single-repository
  traversal code and its goldens stay as they are, and identities equal
  across members never collide. Following stale links unmarked, or
  dropping them, was rejected: the first claims freshness it does not
  have, the second hides dependents that were right a moment ago.
- **Known gaps.** Ref comparisons are refused at project scope (links
  name current indexes; a link pass over ref snapshots in memory is left
  for later, as "Query, impact and change journal" says). `graphvein
  callers|impact` do not follow `documents_route` links, as they do not
  follow the in-repository edge. A traversal reaching a member through a
  link that a later member index left stale may cross a node that no
  longer has the link's content hash; only the source's presence is
  checked.

### Server and UI

- `graphvein serve --project <manifest>` serves one project; `serve` on a
  repository is unchanged (a one-member view). Every API endpoint takes an
  optional `repository` parameter (default: the only member, required when
  there are several, else 400). New `/api/v0/project` lists members, their
  freshness and the link pass state.
- The UI shows one project at a time, with a repository filter; every node
  shows its repository; cross-repository links are visually distinct and
  show their assumption. The source endpoint and IDE links resolve a node
  through its member's checkout path; a member without a local checkout
  shows facts but no source.

### Server read API

- **Routing.** `graphvein serve --project` builds, for every member with a
  checkout, the handlers a single-repository server for it would have
  (root = the member's checkout, repository = its ID, the project store,
  the member's own index lock for pins and ref jobs), and dispatches each
  existing path by the `repository` parameter: the only member when there
  is one, else 400 `repository_required`; an unknown member 400. Options
  considered: (1) a default member (the first in the manifest), rejected
  because an answer about the wrong repository looks right; (2) resolve
  node-taking endpoints by looking the identity up in every member,
  rejected for now (identities are only unique per repository, so it is
  ambiguous exactly when it matters, and every endpoint would need its own
  rule); (3) dispatch per member (chosen): the single-repository handlers
  and their goldens stay as they are. A member without a checkout answered
  409 `no_checkout` on the per-member endpoints in B8; step B9 replaced
  that with store-only answers (below, "UI view").
- **Project endpoints** (GET only): `/api/v0/project` (members, ID source,
  path and its source, the checkout's origin URL without credentials as
  metadata, extractor, index and freshness per member, each member's
  links and their staleness, the pass state with outcome counts and
  `PassReasons`), `/api/v0/project/callers|impact` (`query.ExecuteProject`),
  `/api/v0/project/changes/impact` (the member's change impact with
  `changes.ProjectScope`, ref bases refused) and
  `/api/v0/project/links/changes` (`changes.DiffProjectLinks`). The API
  section's `/api/v0/api/endpoint` of an HTTP route adds the stored links
  into it: client calls (`calls_route`) and OpenAPI operations
  (`documents_route`) of other members, each with its assumption,
  `inferred` provenance, confidence and stale reasons, the source node
  presented through its own member's checkout; and the calls the pass left
  ambiguous with this route among the candidates (never a link). The
  endpoints list counts project callers per route and
  `never_called_in_project`, so a route only other members call is not
  shown as never called in a project view.
- **Remote URL check** (deferred from B4). Reading the remote needs a
  checkout and a git subprocess, so it is done for views, not stored:
  `/api/v0/project` reports `same_remote` when two members are separate
  repository roots with the same origin URL (normalised), the case of one
  repository indexed twice under two IDs; folders of one repository are
  not reported. It never changes an ID or a link.
- **Re-index.** `POST /api/v0/index/run` under `--project` runs the same
  code as `graphvein index --project` (members in manifest order, each
  under its own index lock, then the pass under the project lock; never
  two locks at once) in the server's job. Its document reports each
  member's outcome and the stored pass; the job is `committed` only when
  every member checkout committed and the pass was stored. The lock state
  is `held_here` while the job runs (its lockers are its own), otherwise
  the project lock's probe.
- **Security.** No write endpoint is added; the new paths are GET and
  HEAD only and pass the same Host, Fetch Metadata and Origin checks
  (ADR 0041, ADR 0052). Member POST endpoints (re-index, pins, refs) keep
  their rules.
- **Known gaps.** The architecture graph endpoints (2D graph) are on
  another stack, not on this branch: when they land, their node queries
  take `repository` like every per-member endpoint, and cross-repository
  edges come from `projectlink.View` as in `/api/v0/api/endpoint` (the
  integration point is `projectState`, `internal/server/project.go`).
  The manifest is read once at start; restart `serve` after editing it.
  (`/api/v0/api/flow` and `/api/v0/api/calls` gained project links and
  the web UI the `repository` parameter.)

### UI view

- **Detecting project mode.** The page asks `GET /api/v0/project` once
  at start: 404 (a single-repository server) or a failed request renders
  the shell exactly as before, with no `repository` parameter and no
  project part; the status request then reports a failure as it always
  did. Options considered: a field in `/api/v0/status` (rejected: in
  project mode it needs `repository` itself, so it cannot be the first
  question) and a value injected into `index.html` (rejected: the
  embedded page is static).
- **Repository filter.** The selected member is named on every
  per-member request by the API client (`withRepository`; the project
  document and the project re-index, which refuses the parameter, are
  left out), and the whole shell remounts when it
  changes, so no section can show one member's answer under another's
  name. Options considered for the default: none until the user chooses
  (rejected: an empty page on every first visit) and the first member
  silently (rejected: an answer about the wrong repository looks right,
  B8's reason for no server default). Chosen: the first member, shown as
  a visible default ("first member, shown by default: choose a
  repository") until a member is chosen; the choice is kept per project
  in browser storage.
- **Where the project is shown (developer decision 2026-11-19).** No second top bar: the top bar is the single-repository one,
  and the project lives in the sidebar. Its "Repository" section is the
  member list (the project name; one row per member with a freshness
  dot; the selected row highlighted; a click switches, the same
  `repository=` and remount; the default note "shown by default: choose
  a repository" under the highlighted row until a choice is made) with
  the link status line linking to the Project view; the collapsed
  sidebar (icon rail) opens the same list in a popover. Options
  considered: B9's project bar above the top bar (replaced: a second bar
  costs height on every view and splits the frame's navigation in two).
  Wording through the label table rows 28 to 30.
- **Client-call counts in project scope (step MR-UI R1).** Under
  `serve --project` the API section's call counts are the project's: a
  call the repository leaves unmatched but a stored project link
  connects is `linked_across` (a linked-class category of its own,
  inferred, counted in the linked percentage), one with an ambiguous link
  pass outcome `ambiguous_across`; the repository's counts are secondary
  text. Additive server fields: `project_by_status` on calls and roots,
  `status_scope=project` on `/api/v0/api/calls`. Options considered: a
  note beside the repository's counts (B9; rejected by review: the
  headline "0% linked" read as a wrong project fact).
- **What is shown.** The sidebar member list (above); a Project view (members,
  links summary with outcome counts, declarations or the "no
  declarations" explanation, ambiguous sources grouped by claim row and
  candidate providers, the stale-link notice, and stored in-repository
  links a link scope declaration contradicts, flagged "conflicts with
  declaration" and never overridden); repository badges wherever a view
  mixes members (cross-repository callers, impact paths, the Project
  view); cross-repository callers in the API section's Callers tab with
  their assumption, `inferred` wording from the label table (rows 22 to
  25, ADR 0078) and stale marking; the Flow tab names them without
  drawing them (they are project records, not stored edges of the
  repository); the gap sheet says when a call is linked or ambiguous
  across repositories and when a route without callers in its repository
  is called from another; the Changes impact uses
  `/api/v0/project/changes/impact` (except for ref bases) and labels
  paths that cross repositories with the link's assumption.
- **Server additions (additive).** `/api/v0/project` adds
  `declarations`, `ambiguous` (groups by row and candidate providers,
  sources capped at 50 per group with the full count) and `conflicts`
  (`projectlink.DeclarationConflicts`: a call with one unconditional
  alternative whose own index linked it to a route of a scope its
  covering own-repository `consumes` entries do not name; computed from
  the current manifest and indexes, only when a member declares scopes).
  `/api/v0/api/flow` adds `project` as `/endpoint` does; each call of
  `/api/v0/api/calls` adds `project` (its links with target briefs and
  its outcomes).
- **Members without a checkout** answer from the store instead of 409:
  nodes are presented with no file state and no editor link (the ide
  status says why), freshness is `unknown` ("no checkout here"), and
  `graphvein.yaml` reads as absent (defaults, never the server's working
  directory). Only code peek (`/api/v0/source`), a `ref:` base and a ref
  job answer 409 `no_checkout`; the UI shows "no local checkout here"
  under the header.
- **Known gaps.** The Flow canvas does not draw project links as nodes;
  the project change impact's per-member unknowns (`members`) are not
  shown; a call with several alternatives is never checked for a
  declaration conflict; the endpoints list header still counts "never
  called" inside the repository (rows say "+N" from other repositories).

### Suggestions

- `graphvein project suggest` (package `internal/projectsuggest`) reads
  member checkouts statically (env files, Compose files, Vite and webpack
  dev-server proxies, Create React App `proxy`) and proposes `consumes`
  entries with evidence (member, file, line, key, value). It parses
  JavaScript and TypeScript configs with the existing tree-sitter runtime
  (`internal/parse`, no new dependency) and never runs a file, a script or
  a dev server.
- Claim contract (docs/cli/project.md "Suggestions", rows SG-E1 to SG-M3
  and the single default SG-U): configuration written in code is read
  only in literal forms (`export default {…}`, `defineConfig({…})`,
  `module.exports = {…}`); anything computed, a pattern key, a path
  rewrite, interpolation, credentials or a query is listed as not read,
  without its value.
- A proposal needs an address that reaches exactly one other member: a
  loopback port that member declares (env port key or published Compose
  port of a service built from it), or a service name and container port
  in the same Compose file. A service is a member's only through its
  literal, existing build context; image-only services are no member.
  Everything else is unmatched evidence with the reason (no member, the
  member itself, several members, a host that is not local). A public host
  name is never mapped to a member.
- Nothing is applied until the user accepts: `--accept N` appends the entry
  through the manifest writer (`config.AddProjectConsume`, comments kept,
  full validation, atomic replace), refuses a provider the consumer
  already declares, and says when the entry is the consumer's first (it
  narrows that consumer's candidates). Links change only at the next link
  pass (test: listing then linking changes no link; accepting then
  linking does). `Suggest` returns the listing as structured data
  (`--json`) for the B10 UI, which confirms through the same writer.
- Known gaps: build-time aliases (webpack `DefinePlugin` `__API_URL__`
  from `process.env.API_URL`) are not traced, so a proposed `bases: [KEY]`
  may not cover calls through the alias; one entry per provider means a
  `bases` proposal and a `path_prefixes` proposal for the same provider
  cannot both be accepted; Compose `env_file`, profiles, multiple-file
  merges and `--project-directory` are not modelled; Kubernetes and
  reverse-proxy configs are not read. Corpus (T022 "B6 notes"): react
  + express + axum-ddd and the langflow frontend propose nothing; every
  address is listed as unmatched or not read with its reason.

Step B6b (coverage recovery, T022 "B6b notes"):

- Literal build-time defines (rows SG-D1, SG-D2): a webpack
  `new DefinePlugin({…})` with one literal object argument, read wherever
  it is created in a `webpack.*` file (the argument is literal even when
  the config is a function), and Vite `define` in the literal exported
  config. `NAME: JSON.stringify(process.env.X)` makes an env file address
  of X in the config's folder also offer base `NAME`, only when the config
  loads env files with dotenv; `NAME: JSON.stringify('URL')` (or
  `'"URL"'`) is an address through `NAME`. A key `import.meta.env.N` or
  `process.env.N` is base `N`.
- Service names outside Compose (row SG-M4): an env, define or proxy
  address whose host names a service of the Compose files in its own
  member checkout matches when every service of that name is built from
  one other member and one declares that container port; an image-only or
  unread service of that name leaves the name unproven.
- Stored-call check (row SG-M5): a proposal with a base is made only when
  the consumer's stored index (the project store) has a call, in the
  evidence's scope, that the selector covers by the pass's own rules
  (`projectlink.CoveredCalls`); `bases` and `origins` keep only what
  stored calls record. Otherwise the address is unmatched "no calls use
  this base" (a backend's CORS origin proposes nothing), or, without a
  stored index, says to index the project first. Proxy proposals are not
  checked.
- `consumes` entries may name a member's default scope only
  (`default_scope: true`, manifest rule `consumes-default-scope`), closing
  B5's gap; `scope: ""` was not used because an entry without `scope`
  already means the whole repository. The manifest hash leaves the field
  out when false, so existing manifests keep their hash.
- Known gaps: closed for declared constants (extractors-47):
  a call through a bare define constant declared only in a `.d.ts`
  (`${__API_URL__}/…`, or a URL object or Orval mutator taking its origin
  from it) records base `unresolved` with `base_expression: __API_URL__`
  (docs/extract/clients.md "Build-time constant bases"), so react's 27
  Orval calls are covered by `bases: [__API_URL__]` and react-published
  proposes it; an undeclared constant stays dynamic. Define
  constants are tied only to env files in the config's folder; Compose
  `env_file` is still not modelled.

### Manifest editing from the UI

- The YAML stays the source of truth. The UI edits declarations only:
  `consumes` and selectors, link scopes, repository names, and confirming
  suggestions. Each edit is shown as a diff of the manifest before it is
  written, and written back through the same writer as the CLI.
- Adding a member by folder path stays in the CLI (`graphvein project add`)
  or a desktop dialog; the web API accepts no filesystem path from a form.
- Security (ADR 0041, ADR 0052): the write endpoint exists only under
  `serve --project`, on the loopback bind; it requires the same session
  token and Origin/Host checks as the re-index endpoint; it writes only the
  manifest file the server was started with (never a path from the
  request, never the local override file); scope folders in an edit are
  validated as relative, inside the member, and without `..`; the request
  carries the hash of the manifest it was based on, and a mismatch (the
  file changed on disk) is refused; the new content is fully re-validated
  before an atomic replace; a refused edit leaves the file untouched; the
  size of an edit is bounded. The write marks project links stale like any
  manifest change.

### Manifest editing

- **Endpoints** (docs/cli/serve.md "Project manifest endpoints"):
  `GET /api/v0/project/manifest` (the file, its hash, the parsed
  declarations and the session token), `GET /api/v0/project/suggestions`
  (B6's listing) and `POST /api/v0/project/manifest` with typed
  operations: `add_consume`, `remove_consume`, `set_select` (in place; `{}`
  clears), `set_default_scope`, `add_scope`, `remove_scope`,
  `rename_repository` (the member and every consumes entry naming it) and
  `accept_suggestion`. Each operation takes exactly its listed fields; any
  other field (a file or member path included) is refused. The only path
  is `add_scope`'s folder: relative, slash-separated, no `.`/`..`, no
  backslash or drive, and an existing folder inside the member's checkout
  once symbolic links are resolved (a member without a checkout cannot
  get a new scope from the UI).
- **Preview, then apply.** Without `apply` the answer is the unified diff
  of the file, the validation result (`config.ParseProject` with the local
  file, as the CLI), one sentence per operation and the re-index offer;
  nothing is written. `apply` needs `base_hash` (the file the edit was
  made on; otherwise 409 `manifest_changed`) and `result_hash` (the
  preview's result; otherwise 409 `preview_changed`, so a suggestion list
  that moved between preview and confirm never writes something the user
  did not see). An invalid result is never written (422). Writes go
  through `config.WriteFileAtomic` (temporary file and rename in the
  manifest's folder), serialised by a server mutex.
- **Session token** (decided on 2026-11-18, pending the developer's acceptance of this ADR: keep it. The
  manifest write requires every ADR 0052 check plus the per-process
  token; the re-index and change endpoints are unchanged, without a
  token). ADR 0052
  chose no CSRF token for the re-index endpoint (option 4b rejected): the
  Host allow-list, Fetch Metadata, Origin, `http.CrossOriginProtection`
  and the JSON content type already refuse cross-site requests. This
  section asks for "the same session token ... as the re-index endpoint",
  which does not exist, so B10 keeps every ADR 0052 check and adds a
  token: random per server process (`crypto/rand`), returned only by
  `GET /api/v0/project/manifest` (behind the same checks, so a foreign
  page can neither send it nor read it), required in `X-Graphvein-Token`
  and compared in constant time; a custom header also forces a CORS
  preflight, which the server never grants. The edit also requires a
  loopback peer address. Options considered: (a) ADR 0052's checks only,
  as for re-index; (b) a per-process token in a header (chosen: strictly
  stronger, no cookie, no new state beyond memory); (c) a cookie with
  `SameSite=Strict` (cookies are shared by every loopback port, so another
  local server's page could read or set it).
- **Reload.** After a write the server reloads the manifest and rebuilds
  its project routes (`projectRouter`): the edited declarations, scopes and
  member names are served at once (B8's restart requirement is gone); a
  renamed member's index lock is opened on first use and closed at
  shutdown. The links are stale until the link pass, as for any manifest
  change. The offer is `link_pass` (`POST /api/v0/index/run` with
  `{"link_pass_only": true}`: the link pass alone over the stored indexes,
  members reported `linked_from_store`) when no member changed, and
  `project` (a full project re-index) after a rename.
- **Rename retirement.** An applied rename takes the project
  lock before writing (409 `index_locked` when another process holds it,
  409 `index_running` while the server's own re-index runs; nothing is
  written), writes the manifest, reloads it and calls
  `RetireProjectMember` for each old name; the answer lists them in
  `retired` with `removed` and an `error` when the rows stay (a server
  without a writer store, a repository another project names).
- **Edits on disk.** Each request to a `/api/v0/project` path
  compares the content hash of the manifest and the local file with those
  the server serves and reloads on a change (two small reads; no file
  watcher, which would need platform code and a goroutine per server). A
  changed declaration makes the links stale (the manifest hash the pass
  stored differs), shown by the existing stale-link notice; a file that
  does not validate is logged and ignored until it validates. Per-member
  requests do not check: the UI asks `/api/v0/project` first and at the
  status cadence.
- **UI.** The Project view's "Edit declarations" part: consumes entries
  with selector editing, default scope and removal; an add form; link
  scopes; rename (only where the manifest sets the name); suggestions read
  on demand with their evidence. Every edit shows the diff preview and
  needs a confirm ("Write graphvein-project.yaml"); Plain/Technical label
  rows 26-27 word the operations and selector fields.

### Unknowns from unlinked outcomes (T022 review)

- **Context.** The independent review (the T022 task record (internal)) found
  that project callers, impact and change impact read only stored links:
  a route that ambiguous calls of another member may reach (a frontend
  with two backends serving the same paths) answered "nothing depends on
  this, 0 blind spots" (A1), and "Not called in the project" counted it
  (B1).
- **Decision.** One class rule, `projectlink.View.PossibleCallers`, used
  by every reader: for a route, the stored outcomes of other members that
  may reach it without a link. An `ambiguous` outcome (PL-R10, PL-R11,
  PL-S3) qualifies when its candidates hold the route; a list cut at ten
  is ordered, so a route sorting after its last listed candidate
  qualifies too. An `incomplete` outcome (PL-R7) now lists the indexed
  providers' matching routes as candidates (the pass change of this
  unit) and qualifies by the same rule; one whose indexed providers match
  nothing says so in its reason and never qualifies; one stored before
  this change (no candidates, no such reason) qualifies for every route of
  a candidate provider, decided from the current manifest as a superset
  (every other member without consumes or with a changed manifest).
  Every other status claims nothing about the route. Readers show them as
  unknowns: `graphvein callers|impact --project` and the project
  endpoints list `possible_callers` ("may be called by ... (ambiguous
  across repositories)") counted in the blind-spot line; `changes impact
  --project` lists `possible_dependents` ("may depend on", relation I12 or
  I13 wording, the outcome's reason; never paths, never followed); the
  endpoint's callers tab lists them apart; the endpoints list counts
  `possibly_called_in_project` and leaves such routes out of
  `never_called_in_project`.
- **Options considered.** (1) Count them in the existing name-matched
  blind spots: rejected, a different evidence class with a known
  candidate set; (2) follow them as links at a lower confidence: rejected,
  ambiguity is never a link (ADR 0075 "Precision"); (3) per-route
  unknowns from stored outcomes (chosen). For PL-R7 without candidates,
  listing every call of the consumer for every route was rejected as
  noise (a `POST /login` shown as a possible caller of `GET /user`).
- **Server spec refusal (B2).** A new refusal row PL-R12 before PL-L5: an
  OpenAPI operation of a member that itself stores routes documents its
  own server, so without a `consumes` entry it never links to another
  member's route (outcome `unmatched`, reason "spec of a server
  repository"); with one it links as PL-L6. Frontend specs keep PL-L5.
- **Rename deletion (B3).** A UI rename previews `retiring` per old name
  with a stored index (node count, the projects whose pass lists it,
  `standalone` when none does) and the apply needs `confirm_delete`
  naming it (409 `delete_confirmation_required` otherwise; nothing
  written). `store.ProjectStore.RepositoryFootprint` reads it.
- **Smaller fixes.** `status --project` lists stored repositories a kept
  pass named but the manifest no longer does, with `graphvein project
  forget NAME` (C1); the endpoint and the calls list mark in-repository
  links that a scope declaration contradicts (C2); a scope folder from
  an edit must match the case on disk component by component (C4).
- **Known gaps.** PL-R3 (`unproven`, alternatives without stored bases)
  and PL-R9 (`test_only`) outcomes are not listed as possible callers.
  Possible callers are read from the last stored pass; a stale pass's
  outcomes are shown with the pass's staleness, not recomputed.
  Possible dependents are not followed further into the consumer's
  graph (only the consumer call is named). `status --project` lists
  orphans only while the link journal keeps a pass naming them.

### Locks

- Indexing a member takes that member's index lock (unchanged). The link
  pass takes a project lock (Postgres advisory keyed by schema + project;
  SQLite: the store lock) and reads all members in one read transaction, so
  it never combines a half-published member.

## Consequences

- No identity change, no contract version bump, no re-index of existing
  stores. A store migration (schema version 5) adds the project tables;
  single-repository stores migrate with empty project tables.
- Projects work with partial checkouts (stored facts suffice for linking);
  source views need the checkout.
- The composite corpus tests stay as they are (they test in-repository
  linking); new project tests assert the same 27 calls_route and 19
  documents_route across separate repositories.
- Projects link without any declaration when one provider matches; with
  several providers the result is ambiguous until the user declares
  `consumes` (stated in the UI with the candidates, not hidden).
- The manifest gains a writer (CLI and server) that must keep comments and
  never write an invalid file.

## Known gaps

- A member indexed before extractors-47 has neither stored attribute: its
  equivalent Express registrations stay ambiguous and its calls with
  several alternatives are `unproven` (PL-R3) until it is re-indexed.
- Link scopes resolve in-repository ambiguity only as a project overlay;
  `graphvein index` of a single repository (no project) still reports such
  calls ambiguous (ADR 0021 note).
- A cross-repository link without declarations rests on path matching alone:
  a frontend that also calls a third-party API with paths matching a
  member's routes can link wrongly; literal-origin calls are protected,
  base-variable calls are not until declared. The assumption is shown on
  every such link.
- Suggestions cover env files, compose files, dev-server proxy configs and
  literal build-time defines only; Kubernetes and reverse-proxy
  configurations are not read. A call through a bare define constant is
  recorded as dynamic by the clients extractor, so its base cannot be
  proposed.
- The MCP server (launch 1 item 5) must adopt the same `repository` field
  and project scope.
- Postgres and the other operating systems need CI evidence.
- Manifest editing: the session token is kept for the
  manifest write only (decided in B11, pending acceptance); the re-index
  and change endpoints rely on ADR 0052's checks without it. A rename
  from the UI removes the old name's rows (B11), but a rename made on
  disk is not recognised as one: its old name's stored index stays in
  the project store until `graphvein project forget` removes it,
  as does a member removed from the manifest; their project rows go with
  the next link pass. A rename keeps nothing of the old name's change
  journal or pins. The UI never adds or removes a member. Since the
  review's step MR-F, `status --project` points to `graphvein project
  forget NAME` for such a name while a kept link pass names it, and a UI
  rename asks for an explicit confirm naming the index it deletes.
- A Next.js application divided by a member's link scopes is no project
  link target. Since TOP-1 of 2026-11-18 another member's Next.js routes are
  targets otherwise (`docs/extract/project-links.md`, "Next.js route
  targets"), and a link stores the row assumptions it rests on (NX-V1,
  NX-V3) in the additive `assumes` list of `store.ProjectLink` (payload
  only, no schema change) beside its assumption text.

## Acceptance notes (2026-11-19)

- Results: combined gate CI run 44533180619 on 0ef4343 (`[ci windows]`): postgres raw log 0 FAIL, 226 `/postgres` PASS; Windows 0 FAIL. Full check on the stacked head: 0 FAIL, 225 `/postgres` PASS. Corpus suites (RealWorld, LinkScopes, Suggestions, ProjectIndex/Query/Serve) pass.
- Revisions: B1 manifest and CLI; B2 store migration 5; B3 project linker; B3b Express equivalence proof and per-alternative bases; B4 project index; B5 link scopes; B6/B6b/B6c suggestions, literal defines and build-time constant bases; B7 project callers and impact; B8 server API; B9 UI; B10 manifest editing; B11 precision, docs, rename retirement; MR-UI (project in the sidebar per the developer, `project forget`, project-scope counts); MR-F (independent review: possible callers from ambiguous/incomplete outcomes, possibly-called counts, PL-R12, rename confirm); the committed-binary history was avoided by rebuilding MR-F's commit on `wt/s33-mr2`; stack follow-ups (project links to Next.js routes with their assumptions; architecture overview in project scope).
- Coverage: react+express 27 `calls_route` + 19 `documents_route`; react+axum-ddd the same; react+realworld-gin 27 + 19 (after the trailing-slash fix); react+express+axum-ddd 0 links, 27 calls ambiguous (selectors link 22); express link scopes 16 + 30; langflow frontend+backend 167 links. The review checked 64 cross-repository facts by hand: 0 wrong.
- Verification labels: cross-repository links are the `cross_repository_links` family: never Precise (`best_effort`, inferred), confidence never above the path tier, assumption "matched across project repositories; base URL not proven" (plus Next.js assumptions where they apply) shown on every link.
- Security of manifest editing: `serve --project` on a loopback bind and from a loopback peer; ADR 0052 checks plus a per-process `X-Graphvein-Token`; typed operations only; the only path accepted is a relative, case-exact scope folder inside the member; preview then apply with base and result hashes; atomic writes; renames take the project lock and need `confirm_delete` naming each index deleted.
- Known gaps: members indexed before extractors-47 must be re-indexed; scope overlays only in project mode; a link from path matching alone can be wrong for a third-party API with matching paths (labelled inferred, assumption shown); no Kubernetes/reverse-proxy suggestions; a rename made on disk needs `project forget`; possible dependents are not followed into the consumer's graph; PL-R3/PL-R9 outcomes are not listed as possible callers.
- After v1: MCP adoption of repository/project scope; a manifest file watcher; the token for the re-index endpoint.
