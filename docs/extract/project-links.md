# Project links (cross-repository)

ADR 0075 "Cross-repository links", task T022 (link scopes). Package
`internal/projectlink`. The project link pass is application-side: it reads
the stored nodes of every member of a project (`graphvein-project.yaml`,
[docs/cli/project.md](../cli/project.md)) and writes links owned by the
project (`store.ProjectLink`). No member snapshot changes and the indexer
contract is unchanged.

## Inputs

For each member, from the store: its latest run (`Repositories`) and its
`route`, `client_call` and `api_operation` nodes (`Nodes` with a kind
filter). From the manifest: member order, `consumes` with selectors, and
each member's link scopes with their own `consumes`. A member without a stored index
has no run and no nodes.

Sources are the stored client call alternatives (`paths` entries of
`client_call` nodes) and OpenAPI operation path entries (`paths` of
`api_operation` nodes). Targets are stored `route` nodes of other members,
and, for the scope overlay (rows PL-S1 to PL-S3), of the source's own
member.
Matching reuses the client matcher (`clients.NewStoredRouteMatcher`) over
contract attributes: method, path templates with optional and trailing-slash
flags, route path scope, framework and `equivalent_registrations`; the
call's base kind, origin and origin resolution; the operation's exact
tier. Tiers and confidences are those of [client matching](clients.md) and
[OpenAPI route correspondence](openapi.md#route-correspondence): 0.9 exact
without origin, 0.7 exact with an origin or an assignment-derived origin,
0.6 suffix; operations 0.9 without server origins, 0.7 with.

Two stored attributes (extractors-47) let the pass match as the
in-repository linker does:

- ADR 0022's Express equivalence proof is stored as the route attribute
  `equivalent_registrations` ([routes](routes.md)): tied routes of one
  group collapse to the first registration in source order, exactly the
  ties the in-repository linker collapses. Routes without the attribute (or
  an index before extractors-47) stay tied (PL-R10).
- A call with more than one alternative, or a conditional one, stores each
  alternative's `base` and `base_expression` ([clients](clients.md)). Each
  alternative is then its own source, matched with its own base kind, and
  `bases` selectors compare its own base expression, never the call-level
  one (which is the last alternative's). A single unconditional
  alternative uses the call-level attributes, as before.

The link precision rules LF-1 and LF-2 of [client matching](clients.md)
are rules of that shared matcher, so they hold here too (T034). A call with
an origin never takes a provider route without a literal segment (a
catch-all, `/{id}`, `/`), except a call of another member whose literal
origin a covering `consumes` entry names in `origins` (PL-L4): that entry
is the proof LF-1 asks for. An assignment-derived origin is never proven.
A link scope of the same member is matched with the member's own matcher,
where LF-1 holds even when a `consumes` entry names the origin (a possible
miss, never a false link). An OpenAPI operation path entry with literal
origins (its spec's `servers`) never takes another member's route without
a literal segment unless a covering `consumes` entry names one of its
origins in `origins`, the routes of the spec's own member included (as in
the repository's own correspondence). A link whose origin is
assumed, not proven (a call's unresolved origin, an operation whose
origins no `consumes` entry names), appends "origin <origin> assumed to
be <provider>'s server" to its stored assumption.
A catch-all that wins only because a call parameter stands where other
routes of the endpoint have literal segments leaves the call ambiguous
(LF-2), as inside the repository.

Routes served below a prefix their code does not fix carry no `paths`
entry, only `unresolved_prefix_paths` entries (T033, ADR 0084). The
shared matcher's unknown-prefix tier (rows UP-13..UP-21 of
[client matching](clients.md)) replays on stored nodes, and the pass links
on it only through a declaration (rows UP-25 and UP-26):

- Without a `consumes` entry that covers the source and names the
  provider, another member, a result that rests on such entries is
  neither a link nor a tie nor a possible caller in any endpoint, the
  scope overlay included. When those are a source's only matches, its
  outcome is `unproven` (PL-R13); when another provider has a candidate,
  the source is decided as if the entries did not exist.
- With such an entry, the declared provider's result counts as any
  result: one route in one possible provider links (PL-L7, 0.6, `assumes`
  UP-V1, UP-V3 and, for an unresolved base, UP-V2), a tie inside the
  provider is `ambiguous` (UP-17, PL-R10), and a second declared provider
  with a candidate makes the source `ambiguous` (PL-R11). A call with a
  literal origin reaches the tier only through an entry whose `origins`
  names that origin (row UP-21; the entry is the proof LF-1 asks for, as
  in PL-L4). The routes of the source's own member never link this way
  (as for LF-1, a same-member declaration is no waiver).

Fixture: `testdata/fixtures/projectlink-unknown-prefix`, test
`TestUnknownPrefixProjectScenarios` (scenarios without a declaration,
with one, with an `origins` selector, and with two declared providers).

`TestStoredMatcherReplaysClientLinks` (`internal/index`) checks on the
client fixtures that the stored attributes reproduce every in-repository
alternative result (status, candidates and count).

## Next.js route targets

TOP-1 of 2026-11-18 (T022 x T024; NX-S had left them out). Next.js route
handlers and Pages Router API routes of another member are targets
(`clients.NewProjectRouteMatcher`), under the project rules:

- The same-origin rule (NX-L1) is the repository's own: a source is
  matched against Next.js routes only on another member's endpoints. The
  member's own endpoints (the scope overlay, PL-S1 to PL-S3, and a
  `consumes` entry naming the own repository) never take its Next.js
  routes, as inside the repository.
- A call from another repository is matched under the cross-repository
  assumption, like a call to any other framework's route. A call with a
  literal origin reaches a provider only through a `consumes` entry whose
  `origins` names it (PL-R5, PL-L4), so the literal-origin refusal of
  NX-L1 does not apply; without that declaration it stays PL-R5.
- NX-L3 (literal rewrites, `basePath`), NX-R4 (unresolved base) and HEAD
  for GET apply as inside the repository, from the stored route
  attributes (`rewrites`, `base_path`, `project`).
- A link to a Next.js route (any of PL-L1 to PL-L6) stores the row
  assumptions it rests on in `assumes` (`["NX-V1"]`, or `["NX-V1",
  "NX-V3"]` for a call an afterFiles or fallback rewrite re-routes), and
  its assumption text ends "; Next.js route: assumes NX-V1" (or "NX-V1,
  NX-V3"). `store.ProjectLink.Assumes` is additive: other links omit it.
  A link to a route whose `basePath` is not read (NX-R4) also assumes
  UP-V1 and UP-V2 at 0.6 (decision U6 of T033, as inside the repository):
  "; Next.js route: assumes NX-V1, UP-V1, UP-V2".
- A link (any row) to a route path entry that rests on a path assumption
  of the route side (GR-V5, RO-V1, RO-V2; [routes](routes.md)) stores the
  ids in `assumes` after any Next.js or unknown-prefix ones, and its
  assumption text ends "; route path: assumes RO-V2" (the
  matcher rule is "Path assumptions on links" in
  [client calls](clients.md)). The confidence is the row's.
- A Next.js application whose routes the member's link scopes divide
  (routes of one `project` in two endpoints) is no target on any endpoint:
  NX-L3 needs all of an application's routes to tell whether one serves a
  path before a rewrite applies.

Each candidate provider is matched separately (one matcher per provider
endpoint, see "Link scopes"). A provider is *possible* when its matcher
links or finds an ambiguous tie for the source. A less specific route in a
second provider still makes that provider possible: specificity orders
routes inside one server, but which server a call reaches is exactly what
is not proven.

## Link scopes

Step B5. A member's `scopes` ([manifest](../cli/project.md#the-manifest))
declare named folders inside it, typically several servers in one
repository (express `apps/app`, `apps/user`, `apps/article`; ADR 0021
note). A node belongs to the declared scope whose folder holds its file
(`path` + `/` is a prefix of the node's file); every other node belongs
to the member's default scope. Scopes never nest (manifest rule
`scope-overlap`).

- **Provider endpoints.** A member without scopes is one endpoint. A
  member with scopes is one endpoint per declared scope (its routes in that
  folder) plus its default scope (its other routes). Each endpoint has its
  own matcher, so a call matching routes of two scopes is ambiguous
  (PL-R11), as across two members.
- **Consumers.** A source in a declared scope uses that scope's
  `consumes`; a source in the default scope uses the member's `consumes`.
  A scope without `consumes` does not inherit the member's: its sources
  follow the default rules (every other member is a candidate).
- **Naming a scope.** A `consumes` entry with `scope` names that scope's
  endpoint only; an entry with `default_scope: true` names only the
  member's default scope (T022 B6b; its declaration shows
  "`repository` (default scope)"); an entry with neither names every
  endpoint of the repository (its declared scopes and its default scope). Declarations show
  the scope as `repository/scope`, for example "declared in
  graphvein-project.yaml: `frontend` consumes `express/user`". Links
  record `source_scope` and `target_scope` attributes when an end lies in
  a declared scope.
- **Scope overlay.** A call (or alternative, or operation path) its own
  index left `ambiguous` keeps that status in the member's snapshot. The
  pass may resolve it as a project overlay, and only through a
  declaration: the member declares scopes, and a `consumes` entry of the
  source's consumer that covers it names the member's own repository.
  The pass then matches the source against every covered endpoint. It
  links (PL-S1, PL-S2) only when exactly one covered endpoint has a
  candidate, that endpoint is a scope of the source's own repository, its
  matcher links one route, and that route is one of the ties the member's
  own matcher (over all its routes, replaying the in-repository result)
  leaves; a truncated tie list (more than ten) proves nothing. Anything
  else stays ambiguous (PL-S3). A source in a member without scopes, or
  whose consumer names no scope of its own repository, keeps its own
  result (PL-R1). The overlay link's assumption names the declaration and
  ends "resolves an in-repository ambiguity across link scopes".
- Calls the member's own index linked or left `test_only` are never
  changed by scopes, even when a declaration names another scope.

## Candidate providers

- Without `consumes`, every other member is a candidate provider.
- With `consumes`, only the providers of entries that *cover* the source
  are candidates. An entry covers a client call when its address lists
  allow it and its path prefixes allow it:
  - address: a call alternative with a literal origin is covered only when
    `origins` lists that origin (compared in lower case). Any other call is
    covered when the entry lists neither `bases` nor `origins`, or when its
    `base_expression` (the alternative's own, for a call with several or
    conditional alternatives) equals a listed base, or equals `import.meta.env.B`
    or `process.env.B` for a listed base `B` (exact text as the clients
    extractor records it; nothing is parsed out of a longer expression).
  - paths: `path_prefixes` empty, or the template equals a prefix or
    continues it at a `/`.
  - Selectors narrow client calls only (T022 B6c R1). An operation path
    entry has no base, so every entry covers it, whatever its `bases`,
    `origins` and `path_prefixes`: operations follow the consumer's
    providers as with entries without `select` (entries naming different
    providers leave it ambiguous, PL-R11). The one exception is literal
    evidence: when an entry's `origins` names one of the operation's server
    origins, only the entries naming one cover it. A link's assumption
    names the entry without its selector unless that origin decided it.
- The assumption on a link without a declaration is "matched across project
  repositories; base URL not proven". With a declaration it names the entry:
  "declared in graphvein-project.yaml: `frontend` consumes `express`",
  followed by the selector lists the entry has, for example
  "(bases: __API_URL__; path_prefixes: /api)".

## Claim contract

One row per source form. Every source (a client call, or one path entry of
an operation) gets exactly one row; a form no row lists goes to PL-U, which
claims nothing. Rows are checked in table order; the first that applies
decides. PL-R4 (B3: a member declaring link scopes got no link until unit
B5) was removed when B5 implemented scopes; its ID is not reused. The code table is `internal/projectlink/contract.go`, and
`TestContractTable` keeps both tables and the per-row cases 1:1.

| id | form | claim | refusal / unknown |
|---|---|---|---|
| PL-S1 | a call alternative its own index left `ambiguous`, in a member that declares link scopes, covered by a `consumes` entry of its consumer naming its own repository, where exactly one covered endpoint has a candidate, it is a scope of the own repository, its matcher links one route, and that route is one of the in-repository ties | `calls_route` to that route in the same repository, at the matcher's tier and confidence, assumption naming the entry and "resolves an in-repository ambiguity across link scopes" (project overlay; the member's snapshot keeps `ambiguous`) | - |
| PL-S2 | an operation path entry in the same position as PL-S1 (exact tier only) | `documents_route` to that route, 0.9 or 0.7, assumption as PL-S1 | - |
| PL-S3 | any other source its own index left `ambiguous`, in a member that declares link scopes, covered by a `consumes` entry of its consumer naming its own repository: two or more covered endpoints have candidates, the one endpoint is another repository or is itself tied, no declared scope holds one of the in-repository ties, the tie list is truncated, or a covered provider has no stored index | none (outcome `ambiguous`; candidates the declarations leave, or the in-repository ties) | - |
| PL-R1 | a source the member's own index decided, other than PL-S1 to PL-S3: a call whose own status is `wrapper`, `linked`, `ambiguous`, `test_only` or `generated` (committed bundler output, ADR 0085), an alternative (of a call with several or conditional alternatives) whose own status is `linked`, `ambiguous` or `test_only`, or an operation path whose own status is `linked`, `ambiguous` or `test_only` | none: the repository's own result stands (outcome `in_repository`) | - |
| PL-R2 | a source without a template: own status `dynamic` (a call with one unconditional alternative, or an alternative) or `unresolved` (operation), a null template, or a call with no paths (an Orval call bound to the wrong generated file) | none (outcome `dynamic`) | - |
| PL-R3 | an unmatched alternative of a call with several or conditional alternatives that has no stored `base` (an index before extractors-47) | none (outcome `unproven`): the alternative's base is not known | - |
| PL-R5 | a call alternative with a literal origin that no covering `consumes` entry names in `origins` | none (outcome `unmatched`, reason literal origin) | - |
| PL-R6 | a source in a member with `consumes` that no entry covers | none (outcome `unmatched`, reason not declared) | - |
| PL-R12 | an operation path entry without `consumes`, in a member that itself stores route nodes (a server's own spec documents its own server; T022 review) | none (outcome `unmatched`, reason "spec of a server repository"): only a `consumes` entry naming the provider links it across (PL-L6) | - |
| PL-R7 | a candidate provider without a stored index | none (outcome `incomplete`, naming the provider): its routes could make the source ambiguous; the indexed providers' matching routes are listed as candidates (T022 review), or the reason ends "no route of an indexed provider matches" | - |
| PL-R8 | no candidate provider has a candidate route | none (outcome `unmatched`) | - |
| PL-R13 | the only matches of a call alternative in its candidate providers are routes' `unresolved_prefix_paths` entries (routes served below a prefix their code does not fix; T033, row UP-25 of the [unknown-prefix contract](../contract/claims/unknown-prefix-paths.md)), and no `consumes` entry naming the provider covers it (no `consumes`, or an entry naming the source's own member) | none (outcome `unproven`, reason "only routes whose prefix is not read from code match ... without a consumes entry naming the provider"): neither the server nor the prefix the call reaches is shown (decision U5) | - |
| PL-R9 | only test-scope routes match a production source | none (outcome `test_only`, candidates listed) | - |
| PL-R10 | exactly one possible provider (a member, or one link scope of a member), with tied routes | none (outcome `ambiguous`, candidates listed) | - |
| PL-R11 | two or more possible providers (members, or link scopes of one or more members) | none (outcome `ambiguous`, candidates of every provider listed) | - |
| PL-L1 | a call alternative without `consumes`, one possible provider, one route | `calls_route` at the matcher's tier and confidence, assumption "matched across project repositories; base URL not proven" | - |
| PL-L7 | a call alternative covered by a `consumes` entry that names the provider, another member (for a literal origin, an entry whose `origins` names it: row UP-21), one possible provider, one route, matched only on its `unresolved_prefix_paths` entries (row UP-26 of the [unknown-prefix contract](../contract/claims/unknown-prefix-paths.md)) | `calls_route`, inferred, 0.6, `match: "suffix"`, link attribute `route_prefix: "unresolved"`, `assumes` UP-V1 (UP-V2 for an unresolved base) and UP-V3, assumption naming the entry (and its selector) and ending "route prefix not read from code: assumes ..." | - |
| PL-L2 | a call alternative covered by a `consumes` entry without a selector, one possible provider, one route | `calls_route` at the matcher's tier and confidence, assumption naming the entry | - |
| PL-L3 | a call alternative covered through `bases` or `path_prefixes` of a selector (no literal origin), one possible provider, one route | `calls_route` at the matcher's tier and confidence, assumption naming the entry and its selector | - |
| PL-L4 | a call alternative with a literal origin covered through `origins`, one possible provider, one route | `calls_route` at the matcher's tier and confidence (0.7 for a literal origin), assumption naming the entry and its selector | - |
| PL-L5 | an operation path entry without `consumes`, in a member that stores no route nodes (a frontend's or client's spec), one possible provider, one route (exact tier only) | `documents_route`, 0.9 or 0.7 as inside one repository, assumption "matched across project repositories; base URL not proven" | - |
| PL-L6 | an operation path entry covered by a `consumes` entry, one possible provider, one route (exact tier only) | `documents_route`, 0.9 or 0.7, assumption naming the entry (its selector only when an `origins` entry named the server origin) | - |
| PL-U | anything else: attributes that do not decode to the shapes above, an own status outside the listed values | none (outcome `unknown`) | - |

Notes:

- OpenAPI server origins are documentation, not a call's address: the
  literal-origin rule (PL-R5) applies to client calls only, as inside one
  repository, where spec origins only lower the confidence to 0.7.
- Several path entries of one operation reaching the same target give one
  `documents_route` at the strongest confidence, as inside one repository.
- Each alternative of a call with several or conditional alternatives
  gets its own outcome (its template); a dynamic call with such
  alternatives is classified per alternative too, as the in-repository
  linker links its other alternatives. Alternatives reaching the same
  target give one `calls_route` at the strongest confidence.
- Every source gets an outcome (`projectlink.Outcome`): row, status,
  candidates (repository and identity, at most ten, with the full count)
  and, for a link, its target. Links carry the row, match tier, template
  and declaration in their attributes, the source's file and span as
  evidence, and both endpoints' runs and content hashes.
- Every outcome, linked or not, is stored with the links in the same
  transaction (`store.ProjectLinkSet.Outcomes`, table `project_outcomes`), so views read why a call has no project link instead of
  running the pass again.

## Known gaps

- A Next.js application divided by link scopes is no project link target
  (see "Next.js route targets"); declare its folder as one scope.
- Link scopes resolve in-repository ambiguity only as a project overlay:
  `graphvein index` of the repository alone still records the call
  `ambiguous`, and so does its snapshot inside the project.
- A call its own index linked to a route of another scope (by
  specificity, inside one repository) keeps that link even when its
  consumer declares a different scope; scopes never override an
  in-repository link.
- Members indexed before extractors-47 have neither stored attribute:
  their alternatives are PL-R3 and their equivalent Express registrations
  stay ambiguous (PL-R10) until they are re-indexed.
- An alternative whose merged values disagree on the base expression has
  none stored, so a `bases` selector does not cover it (a declaration
  without selectors still does).
- Base selectors match the recorded base expression text only; an
  expression the extractor compacts (`(process.env.APIasstring)`) is not
  matched by its member name.
- A frontend calling a third-party API whose paths match a member's routes
  links wrongly without declarations; the assumption is shown (ADR 0075
  "Known gaps").
