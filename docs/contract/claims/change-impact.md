# Claim contract: change impact

The claim contract of change impact (ADR 0056): cross-cutting rules (G), change rows
(C, D) and impact relation rows (I), as recorded when the feature was designed (task
T019, internal record). `internal/changes` keeps its row and relation tables 1:1 with
these rows (`TestChangeClaimContract`).

## Claim contract

Every change item and impact path names the row that produced it. The
code holds the table as data (`internal/changes/contract.go`), 1:1 with
this section; `TestChangeClaimContract` checks that every row has code,
every code path names a row, every fixture item names an existing row and
every row is exercised by at least one fixture item.

Change kinds: `added`, `removed`, `changed` (user-level field differs;
`body_changed` when only the content hash differs), `unknown_now`,
`known_now`, and the link kinds `link_gained`, `link_lost`,
`link_unknown_now`. Causes: `code`, `relink`, `extractor_or_relink`
(G4). Field values that go from a value to null or `unknown` are
`unknown_now` for that field; from null or `unknown` to a value,
`known_now`.

### Cross-cutting rules (G)

| id | rule |
|---|---|
| G1 | Keys only: file by path, node by identity, edge by kind + source + target (ADR 0005). Nothing is matched fuzzily; a rename is removed + added, with at most a C14 hint. |
| G2 | Projection: moves and L5 location nodes are counted, never listed as changes. |
| G3 | Knowledge: a side knows a record's state only when no footprint file (or containing directory) is a knowledge gap of that side and the side is not wholly partial. Absent on a side that does not know: `unknown_now` (head) or `known_now` (base) instead of `removed`/`added`. A change in a footprint that is a gap on either side is `unknown_now` per field. |
| G4 | Cause: `code` when a footprint file's content hash differs between the sides; otherwise `relink` when both sides' extractor versions are equal (another file or configuration changed the resolution); otherwise `extractor_or_relink`. The comparison carries `extractor_changed` when versions differ. Ref comparisons have equal versions (ADR 0056); a current-index head with another version than the binary is refused (`reindex_needed`). |
| G5 | Coverage: a run or pin base the journal or overlay no longer covers is refused (`base_expired`), never answered partially. |
| G6 | Consistency: a comparison is bound to its head run; a request that sees another head run than its cursor or a run committed during the request answers 409 `index_changed` (ADR 0042 pattern). |
| G7 | Test scope: facts with test scope follow the existing Include tests switch; hidden counts are always shown (T018f O3 pattern). |
| G8 | Wording: removals on run and pin bases read "no longer in the index"; "deleted" is used only on ref comparisons where the head tree proves the path absent. |

### Change rows (C)

| id | fact form | may claim | compares (user-level fields) | refusal / downgrade conditions |
|---|---|---|---|---|
| C1 | `file` record | added, removed, changed | `content_hash`, `language`, `precision` | G3 for the file itself only on the side's scan gaps; G8 wording |
| C2 | code symbol nodes: `function`, `method`, `class`, `struct`, `enum`, `interface`, `trait`, `type`, `type_alias`, `union`, `macro`, `variable`, `constant` | added, removed, body_changed | `content_hash` (signatures are not modelled: D) | G3 |
| C3 | `route` node; its `exposes_route` edges (Revision 1, label `handler_changed`) and `uses_model` edges (T029 W2, row MT-13 of [route-model-types.md](route-model-types.md): label `models_changed`, field `model_edges`) | added, removed, changed | method, path pattern as stored, effective path, handler identity, framework, scope (exact attribute names fixed in A0); `unresolved_prefix_paths` for a route that stores it on either side (T033 row UP-31 of [unknown-prefix-paths.md](unknown-prefix-paths.md)) | G3; a path or method that becomes unresolved: field `unknown_now`; unknown-prefix paths that appear: `known_now`, that go: `unknown_now` |
| C4 | `client_call` node | added, removed, changed | method, URL pattern and its resolution, status, match, wrapper | G3; resolution that becomes `dynamic`: field `unknown_now` |
| C5 | link between a `client_call` and a `route` (`calls_route` edge plus the call's status) | link_gained, link_lost (reasons `target_removed`, `target_changed`, `call_changed`), link_unknown_now | edge presence, call status, edge `match` and confidence | `link_lost` only when the head call is known static (C4 fields known), its head status is `unmatched`, and the reason is proven: `target_removed` = the base target is C3-removed and the call's method and URL are unchanged; `target_changed` = the base target is C3-changed in method or path and the call is unchanged; `call_changed` = the call's method or URL changed and the base target is unchanged. Head status `dynamic`, `ambiguous`, `wrapper` or any other, or no proven reason: `link_unknown_now` |
| C6 | `api_operation` nodes, `documents_route` and `calls_operation` edges | added, removed, changed | method, path, operation id, status | G3; C5 logic for `calls_operation` |
| C7 | `ipc_command` and `ipc_call` nodes, `exposes_command` and `invokes_command` edges | added, removed, changed, and the C5 link kinds for `invokes_command` | command name, handler identity, call status | G3; C5 conditions with command name in place of method and URL |
| C8 | migration-defined `table` (source migration) | added, removed, changed | name, set, completeness | removed only when the head set's table list is complete (ADR 0048 `table_list`, no `may_be_incomplete`) and the set has no gap; otherwise `unknown_now`; added on a base set that was not complete: `known_now` |
| C9 | migration-defined `column`, `db_index`, and `foreign_key` edges | added, removed, changed | the facts in the record's `claims` (ADR 0055): written and normalised type, nullable, default, identity or generated, unique, key membership, index elements | only when the table is `complete` on both sides; otherwise added = `known_now`, removed = `unknown_now`, a field not claimed on both sides is not compared |
| C10 | `migration` node | added, removed, changed (`edited`), verdict changed | `content_hash` (an existing version whose content changed is `edited`: the history was rewritten), `applied`, `order`, may-fail state and reasons (ADR 0051) | a verdict that becomes null: `unknown_now`; never infers the cause of a verdict change beyond the stored reasons |
| C11 | model-defined `table` and `column` (source model, group `models/<tool>/<root>`), foreign keys between them | as C8 and C9 | the facts in `claims` (T018g0 kit); `tool` and `pins` as context fields | as C8 and C9 with the model group's `table_list` and the table's `state`; G3 |
| C12 | Angular `ui_route` (any `framework` but `react-router`) and Angular structure edges (`routes_to`, `loads_children`, `renders`, `injects`, `uses_pipe`, `uses_template`, `declares`, `scope_imports`, `scope_exports`) | added, removed, changed | path and target identity for `ui_route`; edge presence | G3 |
| C13 | architecture: L1 component nodes and the edges between them with protocol, auth and encryption annotations | added, removed, changed | annotations with their provenance | not extracted yet: the row answers `not_extracted` for the group; when present, an annotation that becomes `unknown`: `unknown_now` |
| C14 | move hint (inferred) | `possible_move` between one removed and one added node | same kind and same `content_hash`, or same kind and same qualified name in another file; the pairing must be unique in the comparison | labelled inferred; the two items stay removed and added; nothing (rationale, dispositions) follows the hint |
| C15 | knowledge gaps | `became_partial`, `recovered` per file; side `partial` flags | the error diagnostics' codes | none: these are facts about the runs |
| C16 | other records of kinds listed by no row above but by the generic list (`external`, `api_spec`, `imports`, `contains`, `calls` edges; `passes_function` edges whose source is not a react-router route, T026 AG-F) | added, removed, changed | edge presence; node `content_hash` | G3; listed in "Other facts" (calls edges also feed I1) |
| C17 | T026 AG-F: react-router `ui_route` (`framework: "react-router"`, `docs/lang/typescript-bridges.md` RR rows); React bridge edges: `renders` that is not the Angular linker's (a source that is not a class, or two classes neither of which carries the `angular` attribute on either side), `routes_to` and `passes_function` from a react-router route | added, removed, changed | `path`, `index`, `lazy`, `open`, `component`, `element`, `loader`, `action` for `ui_route`; edge presence | G3; group "React UI", never the Angular group |
| C18 | `server_action` and `action_call` nodes, `exposes_action` and `calls_action` edges (T024b R1) | added, removed, changed, and the C5 link kinds for `calls_action` | action name, handler identity, form; a use's target action(s), form, boundary, status | G3; C5 conditions with the target action in place of method and URL; impact not followed yet (relation ID) |
| D | any record whose kind no row lists, or whose payload does not decode | `unclassified` (raw before/after only) | none | never a seed of impact; counted per kind |

### Impact relation rows (I)

Traversal starts from the items of rows C2-C13 and C16-C18 (seeds), follows
only these relations, breadth-first, sorted edge order, depth 3 by default
(maximum 10 since T042, 6 before), 2,000 reached nodes, fan-out 200 per node; each binding cap is
reported. "Dependent" is what the traversal reaches.

| id | from (seed or reached kind) | follows | dependent | claim wording |
|---|---|---|---|---|
| I1 | function, method, class | incoming `calls` | the caller | "calls the changed code" |
| I2 | client_call, ipc_call, ui_route | incoming `contains` from function, method, class, variable | the enclosing symbol | "makes the changed call" |
| I3 | route | incoming `calls_route` (client_call); incoming `documents_route` (api_operation) | the client call; the operation | "calls this route" / "documents this route" |
| I4 | function, method | outgoing `exposes_route` (route), outgoing `exposes_command` (ipc_command) | the route or command it handles | "handled by the changed code" |
| I5 | ipc_command | incoming `invokes_command` (ipc_call) | the IPC call | "invokes this command" |
| I6 | api_operation | incoming `calls_operation` (client_call) | the client call | "calls this operation" |
| I7 | table, column | incoming `foreign_key` (column) to the table's columns; incoming `alters` (migration) | the referencing column; later migrations that alter the table | "references this column" / "alters this table" |
| I8 | table, column | incoming `reads_table`, `writes_table` | code using the table | Revision 4: "reads / writes this table" (fact), "may read / may write" (inferred), "builds a query on this table" (`role: builds`); users are a lower bound ("uses found", "may also be used by": entries naming the table and unbound entries that may reach it, `unbound: true`, the rule shared with the MCP table tool); `not_extracted` only for sources no code-to-table row covers; never "no users" |
| I9 | class, file | incoming `renders`, `injects`, `routes_to`, `loads_children`, `uses_pipe`, `uses_template` | the Angular component, service consumer or UI route | "renders / injects / routes to / loads / uses this" |
| I10 | file | incoming `imports` (depth 1 only) | the importing file | "imports the changed file" |
| I11 | any reached node | containing L1 component and the architecture edges incident to it | the component edge | not extracted yet: `not_extracted` |
| I12 | route (project scope only, T022 B7) | stored project link `calls_route` (client_call of a member) | the client call in the consumer repository; the traversal continues in that member's graph | "calls this route from another project repository"; inferred, at most the link's confidence, assumption shown; a stale link is followed with its reasons |
| I13 | route (project scope only, T022 B7) | stored project link `documents_route` (api_operation of a member) | the OpenAPI operation in the other repository | "documents this route from another project repository"; as I12 |
| I14 | function | T026 AG-F: incoming `renders` (any source); incoming `routes_to` (ui_route) | the React element owner (function, class or module code of a file) or the react-router route of a function component (class components are I9's) | "renders this" / "routes to this" |
| I15 | function, method | T026 AG-F: incoming `passes_function` (any source) | the code or react-router route that passes the function as a value: it depends on the function's signature and behaviour as a callback | "passes this as callback, call not modelled"; always inferred (the call is not modelled), whatever the edge's provenance |
| I16 | class, struct, type, interface, type_alias, enum, union, and the variable or constant of a named zod or Joi schema (T029 W2, rows MT-14..MT-17 of [route-model-types.md](route-model-types.md)) | incoming `uses_model` (route) | the route whose request or response shape uses the model | "through request body (fact) model UserIn", "through response (returned) model Pet": the role and the using items' provenance kinds; the step carries the route's uses (`model`); a path whose uses are all mapped is inferred |
| ID | any other edge kind, or a listed kind between endpoint kinds the row does not list | not followed | none | counted per kind in `untraversed` |

Every path carries `provenance` (`fact` only when every edge is a fact,
else `inferred`) and `confidence` (the minimum edge confidence).
Unknowns per traversal: blind spots (ADR 0009: unresolved call sites whose
callee simple name matches a reached function), near misses of a changed
route (ADR 0042), reached nodes in gap files, untraversed kinds. A removed
seed takes its first hop through the removed incoming edges in the change
set, then continues in the graph that still holds the dependents (ADR
0056). The wording is "depends on" for fact paths and "may depend on" for
inferred paths; never "breaks".
