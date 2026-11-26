# ADR 0089 - Search and couldn't-read endpoints

**Status:** Accepted (delegated review, 2026-11-25)

## Context

2026-11-24 adds three UI features that need server support (T040): a
jump-to palette (Ctrl+K) across routes, Explorer operations, tables, files
and symbols; a per-repository page "What Graphvein couldn't read"; and a
"Report a wrong fact" flag that opens the public wrong-fact issue form,
which must render nothing until the project's repository is public.

What existed: the route list (`/api/v0/api/endpoints`), the schema sets,
the generated OpenAPI document and the explorer's path values are served;
files and symbols are not. Of the couldn't-read parts, unknown mount hops,
routes without a path, dynamic and wrapper client calls and the compact
route shapes (sections not read, depth caps) are already computed by
existing endpoints; files not read, files read with syntax errors, scanner
gaps, patterns built at run time, Go router forms outside the claim
contract, files with no fact stored and unresolved in-code call totals are
not served anywhere. The store marks every unrecognised file with language
`unknown`, yet Dockerfiles, `go.mod` and compose files of that language
hold facts (5 of express's 20 such files), so a language alone does not
prove a file was not read. The unresolved call counts are written into an
`unresolved_calls` diagnostic message and, since T009, also into the
structured `file` node attributes `unresolved_calls_external` and
`unresolved_calls_unknown` (every file node has them, so a zero means
"none" only for the languages whose reader counts).

The design questions and their accepted defaults are recorded in the T040
task record (internal) and the 2026-11-24 UI decisions.

## Options considered

Search:

1. The UI loads every list (routes, schema sets, the document) and filters
   locally; files and symbols get list endpoints. Large repositories
   (dozzle 9,131 nodes) would ship every symbol to the browser.
2. One `GET /api/v0/search` over the store with a substring filter, counts
   per kind and a limit, searching every project member in one call.
   **Chosen.**

Couldn't read:

1. A generic diagnostics list the UI groups. The UI would re-implement the
   existing endpoints' rules (one rule, two implementations) and read
   counts from message text.
2. One `GET /api/v0/unread` returning every part with counts and bounded
   lists, each part computed by the code that already serves it.
   **Chosen.**

Public-repository switch:

1. A `graphvein.yaml` key with the issue URL. Any repository could point
   reports elsewhere, and the switch would be on wherever a file says so.
2. A build-time setting served by `GET /api/v0/ui`. **Chosen.**

## Decision

1. **`GET /api/v0/search?q=&kinds=&limit=&editor=`** under the claim
   contract [jump-search](../contract/claims/jump-search.md) (rows
   JS-1..JS-20, D; amended by Revision 1 and Revision 2). The query is one string matched as an ASCII
   case-folded substring of fixed keys per kind (route paths, pattern and
   name; operation path keys of the generated document; table and symbol
   names and qualified names; file base names and paths). Ranking: tier
   (prefix, word start, substring), then key length, key, identity; kind
   groups by best tier, then the order route, operation, table, symbol,
   file. Each kind holds at most `limit` items (default 10, at most 50)
   with its total. Every answer names what was searched (run, stored
   files, files not read, scanner gaps) and states its absence as
   `not_proven` only. Under `serve --project`, `repository=` searches one
   member; without it every member is searched in one call (`first=`
   first, then manifest order), each item names its member, and per-member
   and project totals are given.
2. **`GET /api/v0/unread?scope=&limit=&editor=`** under the claim contract
   [couldnt-read](../contract/claims/couldnt-read.md) (rows CR-1..CR-33,
   D; amended by Revisions 1, 2 and 3). Parts reuse the existing code: the explorer's `unresolved` and
   `mounts` lists from `generateDocument`, the calls endpoint's status
   rule, the endpoint list's compact shape, the schema views' scanner
   codes and not-read file codes (now one shared set). New parts read
   stored diagnostics (`route_dynamic_pattern` / `route_dynamic_methods`
   with the line from the documented `<expression> at L:C-L:C` message
   form through a tested parser; `go_routes_unread` verbatim, without a
   form total; amended by Revision 1, CR-38 form counts). "Files without facts" are, in a built-in run only, the
   files whose language has no built-in reader and from which no fact is
   stored; they are never called unread. Unresolved in-code call totals
   are the sums of the structured file node attributes over the files of
   counted languages without a not-read or syntax-error diagnostic, never
   a message. No count adds units. Under `serve --project` the page is per
   member, or every member plus project totals (null, with the members
   named, where a part is not available).
3. **Store.** `store.Query` gains `Contains` (ASCII case-folded substring
   of name or qualified name; the SQL prefilter lower-cases the name
   columns, which both dialects fold at least in ASCII, and Go applies the
   exact test) and the `Store` interface gains `FactFiles` (the files of
   non-file nodes and of every edge's evidence, read in one transaction).
   No schema change.
4. **Public-repository switch.** `-X main.reportURL=<form URL>`
   (`scripts/build.sh` `REPORT_URL`), empty by default; `server.ReportURL`
   accepts only an absolute https URL without user information, query or
   fragment. `GET /api/v0/ui` serves it as `report_url`, null while unset.
   The UI renders no flag while it is null and builds the prefilled form
   link (template `wrong-fact.yml`, fields `version` and `shown`) itself,
   after showing the user what will be sent.

Both endpoints are GET only behind the existing guards (Host allow-list,
loopback listener, `Sec-Fetch-Site` and `Origin` checks, CSP), read
through the `store` interface with the runs-before/after consistency
check, and follow the per-repository handler pattern, so a multi-index
server can dispatch them like every other member endpoint.

## Consequences

- The UI's palette and page read two endpoints instead of joining lists,
  and every count is at project scope in project mode.
- A new rule for an existing part (for example a new call status) is
  written once, in the endpoint that owns it.
- Search reads every `route` node and, with the prefilter, only the
  matching tables and symbols; the couldn't-read page decodes every
  edge's evidence once per request (`FactFiles`) in a built-in run.
- Measured on the pinned corpora (T040 task record, internal): the
  couldn't-read counts of dozzle and of the react + express project.

## Revision 1 (T040 server follow-ups)

- **Scanner records.** The scanner writes a diagnostic for every entry it
  saw, did not read and had no record for: `file_binary` (a NUL byte,
  info), `file_not_utf8` (warning) and `file_not_regular` (a FIFO,
  socket or device, info). They join the not-read code set (row CR-19,
  CR-34) shared with the schema views, so a binary or non-UTF-8
  `Cargo.toml` or `.py` file is now a reported gap there too (closing the
  scanner gap ADR 0048 lists). Paths left out by rule (ignore files, the
  default names, nested repositories) still get no record, so the page
  lists none of them (CR-35). Stored through the existing diagnostic
  path: no schema change, no contract version change (diagnostic codes
  are open), no extractor version change (the scanner's output is not
  cached).
- **File categories.** Items of files without facts and files not read
  carry `category` (`code_with_reader`, `code_no_reader`, `not_code`,
  `other`) from one extension table kept equal to the claim file by a
  test, with counts per category (CR-36, CR-37). `code_with_reader` was
  added beyond the three requested categories because a not-read `.py`
  file would otherwise be `other`.
- **Router form counts.** The Go route linker's per-file form counts are
  stored as the `file` node attribute `go_route_forms_outside_contract`;
  the page gives per-file and total form counts from it and names the
  files without it (CR-38). The linker is not cached: no extractor
  version bump.
- **Project search order.** Tier first, then the first member within a
  tier (JS-16, a developer-facing decision: better matches beat member
  order).
- `/api/v0/search` and `/api/v0/unread` are `server.ReadPaths` views
  (JS-21, CR-39); no MCP tool reads them.

## Revision 2 (T040 review)

**Status:** Accepted (delegated review, 2026-11-25)

The review of the T040 UI found gaps that the contract left out (CR-33)
and a search key the palette showed but never matched. Options for the
couldn't-read gaps were a softer sentence on the page, or listing the
stored diagnostics; listing them was chosen, because a sentence alone
leaves the files and mounts unnamed.

- **Records dropped (CR-40).** Stored `extract_invalid_record`
  diagnostics are listed per item with the message verbatim
  (`files.records_dropped`). The code has two writers: a fact record of
  a file that does not validate (every fact of that file is dropped) and
  a diagnostic that does not validate (it is replaced). The page says
  "dropped as invalid; when a fact record, no fact from that file is
  stored", true of both, and never parses the message to tell them
  apart. Kept apart from files not read (CR-19): that set is shared with
  the schema views, whose rule does not change here.
- **Go route recognition failed (CR-41).** Stored `go_routes_failed`
  diagnostics, verbatim, `line: null`; a run-wide one (no file) is in
  every scope.
- **Mounts that claim no path (CR-42).** Stored `route_mount_unresolved`
  diagnostics, with the expression and line read by CR-9's tested parser.
  These are mounts whose router is not resolved; the unknown mount hops of
  CR-7 are mounts whose prefix is not a constant (their routes keep a
  path with `…`).
- **TypeScript structure outside the contract (CR-43).** Stored
  `ts_structure_unread` diagnostics, one per file, verbatim
  (`files.structure_outside_contract`); no count is read from the message.
- **Full-path sentence.** The page says "Every production route has a
  full path" only when CR-6, CR-7, CR-9, CR-10, CR-40, CR-41 and CR-42
  list nothing and the document was built.
- **Document scope.** The routes without a full path and the mount hops
  stay production routes in every scope (CR-6, CR-7); the page now says
  so ("Production routes only" with tests included, "Production code"
  without), and the count is unchanged.
- **Search (JS-4).** A route without a production full path gets the key
  `prefix_unknown`: its unknown-prefix label without the leading `…`, so
  a query for any part of the label the palette shows finds it (dozzle's
  `notifications/rules`). The tiers and ranking are unchanged, so an
  exact or prefix match still ranks first.
- New counts: `route_failures`, `mounts_unresolved`,
  `files_records_dropped`, `files_structure_outside_contract`. No schema,
  store, contract-version or extractor-version change: the parts read
  stored diagnostics through `DiagnosticsWithCodes`.

UI decisions of the same review, recorded here because they bound what
the page and the flag claim:

- The wrong-fact preview says the text travels to the issue site in the
  link when "Open on GitHub" is pressed, and offers Copy text for editing
  first. Before the link is built, the scheme and host of absolute URLs
  and the host of an assumed origin become `<host>` (paths kept), and an
  absolute local path becomes `<local path>`. The link is kept under
  8,000 characters by cutting the text at a line boundary, with a line
  saying so; Copy text keeps the whole text.
- Recent jumps are stored per served index (the project name or the
  status answer's repository) and workspace entry, and an item of a
  repository not served is dropped.
- Ctrl+Enter on an Explorer operation says it has no source line instead
  of opening the Explorer silently.

## Revision 3 (T040 review, second fix round)

**Status:** Accepted (delegated review, 2026-11-25)

A route without a full path was still written by its stored pattern in
three places: the couldn't-read page's "Routes without a full path" list
(and its "Copy as text"), and the wrong-fact flag's endpoint fact
(dozzle: `GET /rules` for `…/api/notifications/rules`). The pattern is the
part below the innermost mount, so it reads as a different path.

- **CR-6 `prefix_unknown`.** Each item of `route_paths.unplaced` carries
  the route's production unknown-prefix path as the endpoint list writes
  it (the same `routePrefix` view, `…` for the unknown part), or null.
  The page shows that path with the marker "prefix unknown" and the
  stored pattern in the tooltip; without one it shows the pattern marked
  "pattern as written, no path known". Option not taken: rebuilding the
  path in the UI from the Explorer's `unknown` part and the pattern,
  which loses the known part between the mount and the registration.
- **Flag fact.** The endpoint text is the first served path, else the
  unknown-prefix path with "(prefix unknown)", else the pattern with
  "(pattern as written, not mounted)" or "shadowed".
- **Ctrl+Enter.** A route or table without an editor link says so, as an
  operation does, instead of opening its section.

No schema, store, contract-version or extractor-version change.

## Revision 4 (T040 review, third fix round)

**Status:** Accepted (delegated review, 2026-11-25)

A re-check on owncast found a route registration counted as a mount and a
route with no path labelled by its bare pattern; the wrong-fact prefill
kept hosts after a scheme built in code and cut whole lines where one
line was too long.

- **CR-44, route paths withheld.** `route_mount_unresolved` has two kinds
  of writer: the mount writers of every linker, and the Go linker's route
  registrations whose path it withholds (another pattern of the ServeMux,
  its redirect, a forwarding closure, or a route of the router it is
  mounted on may answer the path). The page tells them apart by the
  registration's message form, `METHOD "pattern" (reason)` in a `.go`
  file, which no mount expression has (`TestWithheldRouteForm` checks it
  over every message of the fixture goldens). The registrations are a new
  list `route_paths.routes_withheld` and count `routes_withheld`, worded
  "routes whose path is not known"; CR-42 keeps the mounts. Options not
  taken: one softer wording for both (it hides which unit is counted), or
  a new diagnostic code (an extractor and golden change for a page
  wording).
- **"(path not known)".** A route with neither a full path nor an
  unknown-prefix path is labelled by its pattern (or name) followed by
  "(path not known)" in search results and so in recent jumps (JS-4),
  in the couldn't-read shape lists (CR-16) and in the wrong-fact fact.
  The fact no longer says "not mounted": the endpoint detail holds no
  diagnostic that proves it, and owncast's `ANY /*` sits on the root
  router with only its path withheld. "Shadowed" stays, proven by the
  stored attributes.
- **Full-path sentence and files not read.** The sentence "Every
  production route has a full path" also needs no file in a language
  Graphvein reads (category `code_with_reader`) among the files not read
  (`file_not_utf8`, `file_too_large`, ...): a route there is not known.
  With such files the page says "Every production route in the files
  read has a full path; N files were not read", linking to that list.
- **Redaction after a built scheme.** The host after `}//` or `}://` (a
  template substitution such as `${location.protocol}`, `${proto}` or
  `http${s}`) and the operand joined by `+` to a `"//"`, `"://"` or
  `"https://"` string (`location.protocol + "//" + host`) become `<host>`.
- **Shortening.** The first line of every field is kept; an overlong
  line keeps its start plus "…" before later "and:" lines are dropped.

No schema, store, contract-version or extractor-version change.

## Revision 5 (T040 review, fourth fix round)

**Status:** Accepted (delegated review, 2026-11-25)

The API section and the change views still called a route with no
composed path "not mounted", which is false for owncast's `ANY /*` on the
root router, whose path is only withheld; and the full-path sentence did
not count files read with syntax errors.

- **One set of words, from what the server knows.** A route with neither a
  full path nor an unknown-prefix path is "shadowed" when its
  `shadowed_paths` are stored, "path withheld" when it is a CR-44
  registration, else "path not known", in the endpoint list (marker and
  tooltip), the endpoint header, the wrong-fact fact, the change views
  (items and impact paths), search labels and the couldn't-read lists.
  "Not mounted" is never said: no stored fact proves a route unreachable
  from every app.
- **Exposed per route, one classification.** The CR-44 test moves to the
  route linker's package (`routes.IsWithheldRegistration`, next to the Go
  writer), and `routes.WithheldSites` reads the registration's span from
  the message: the route node and its diagnostic share the registration
  call's span, so a route is withheld when a CR-44 diagnostic names its
  file and span. The server exposes it as the endpoint flag
  `path_withheld` (in place of `unmounted`), the detail field
  `path_withheld`, the change item side's `path_withheld` and the impact
  path's `route_withheld`, all additive. The MCP label of `unmounted`
  becomes "the path is not known", and `path_withheld` has its own.
  Options not taken: a second copy of the message test in the UI (two
  classifications drift), or a new route attribute written by the linker
  (an extractor, contract-golden and re-index change for a wording).
- **Change sides.** A side is marked from the diagnostics of its graph:
  the head always, the base only for a ref base. A journal run keeps no
  diagnostics, so a base side of a run comparison is never marked and
  reads "path not known".
- **Full-path sentence and syntax errors.** A file read with syntax
  errors (CR-20) may hide a route in the part tree-sitter recovered, so
  the sentence then reads "Every production route in the files read
  without errors has a full path; N files had syntax errors", the count
  linking to that list; with code files not read too, both counts are
  named, each linking to its list.

No schema, store, contract-version or extractor-version change.

## Known gaps

- The dynamic pattern spans exist only in message text; a pattern
  message outside the documented form has no line.
- Nested repositories are a deliberate boundary and are not listed; the
  page does not say a directory holds another checkout.
- File categories are by extension only: a script without an extension
  is `other`.
- The generated document's lists (routes without a path, mount hops) are
  production routes whatever the page's scope; the page says so, but test
  routes without a full path are not listed anywhere. Listing them needs
  a test-scope document on the server, an item for after v1.
- `extract_invalid_record` has two writers that the page does not tell
  apart (Revision 2).
- The host redaction of the wrong-fact text is by pattern (Revision 4).
  These forms keep their host: a host written without a scheme outside an
  origin assumption (a bare `api.internal:8443` in a call expression, or
  `new URL(path, "api.corp")`); a host joined after the first `+` operand
  (`"//" + sub + ".corp.internal"` keeps `".corp.internal"`); a scheme and
  host put together other than by `+` or a template (`[proto, "//",
  host].join("")`); and a scheme and host split by a substitution other
  than at `}//` or `}://` (`` `${proto}:${sep}api.corp` ``). The preview
  shows the whole text before anything is opened, so the user can see it.
- "Path withheld" rests on the Go linker's message form and on the
  registration's span in it (Revision 5): a message outside the form, or
  without a span, marks no route, which then reads "path not known". A
  journal base side is never marked. The flag replaces `unmounted` only
  for these routes; other routes keep `unmounted`, worded "path not
  known".
- Search does not tokenise: "get user" matches only that exact text.
- External indexer runs get no files-without-facts part and no unresolved
  call totals: their readers are unknown.

## Review notes (delegated review, 2026-11-25, T040)

Accepted with Revisions 1-5 after the final CI gate of 2026-11-25 (CI runs 45627357238, 45632313364 and 45638139948 (the last green on Linux, Postgres and Windows; postgres log 0 FAIL, 291 /postgres PASS)). One independent review and a focused re-check (counts equal the stored diagnostics on dozzle, owncast, miniflux and chatbot-ui at every scope), four fix rounds, converged. A regression it introduced (skip notices made freshness unknown for any repository with a binary file) was found at integration and fixed (scan.DeliberateSkip, regression tests per path). Wording for routes without a path is one classification (shadowed / path withheld / path not known). Known gaps as recorded above (test routes without a full path, pattern-based host redaction).
