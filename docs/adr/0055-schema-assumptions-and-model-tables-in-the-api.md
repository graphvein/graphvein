# ADR 0055 - Schema assumptions and model tables in the schema API

**Status:** Accepted (delegated review, 2026-11-09)

## Context

The schema read API (ADR 0048) serves sqlx migration sets only. Two
extractors now state things the Schema view (T018b2) cannot show:

- T018a2 (ADR 0051) bases every migration fact on environment assumptions
  E1-E6 and E8 (`docs/extract/schema.md` "Environment assumptions"). The
  map `contractAssumptions` in `internal/extract/schema/contract.go` names,
  per contract row and precondition, the assumptions its claim rests on,
  "for a later task to show next to the facts". No record says which row
  produced a fact, so the API cannot say why a fact holds.
- T018c2 (ADR 0054) stores SQLModel tables (`source: "model"`, `set:
  "models"`) with `assumptions: ["V1", "V7"]`, the library `pins` and
  `reasons: [{row, line, reason}]`. Decision PD1 kept them out of the
  `/api/v0/schema/*` endpoints until a view design existed; decision PD2
  allows only a name-overlap note between a model table and a migration
  table (L5), never a merge or a verdict.

The developer chose the round 2 design R2 on 2026-11-09
(T018f design round 2 mockups, internal): one card per source group in the set strip
(a dashed card for a source with nothing read; "Alembic migrations found,
not read yet" for a tool Graphvein detects but does not read), a "Rests
on" line on the group and the table, row chips only where a row rests on
more than its table, one Assumptions tab as the catalogue, and a "why"
popover on every fact that is unknown, partial, "as far as the
migrations show" or carries a chip. The VISION precision principle
applies unchanged: an absence only where the API proves it, unknown shown
as unknown, and an assumption visible wherever a fact rests on one.

The view needs facts that are not stored or not served today: which
contract row set each fact and where; the opaque statements behind
`opaque_after`; model groups; detected but unread migration tools; the
assumption texts and the row table.

## Options considered

1. **Where per-fact provenance comes from.**
   - A. The extractors store it on the records: migration `column` and
     `db_index` records gain `claims: [{fact, row, migration, span}]`,
     table `reasons` gain `row`, migration records gain `opaque_sites`;
     model `reasons` gain `fact` and `file` (chosen). Only the pass knows
     which statement last set a fact and under which row.
   - B. The server re-derives rows from the fact kind and the history
     evidence (rejected: a column type can come from M2, M5c or M1b; the
     server would guess where the pass knows, the precision rule ADR 0048
     option 2 forbids).
   - C. Show only table-level assumptions, no per-fact rows (rejected:
     the developer asked for row chips and per-fact reasons; an index's
     E2 dependency would be invisible).
2. **Opaque sites per table or per migration.**
   - A. Each migration record stores its own opaque statements
     (`opaque_sites: [{span, row, kind}]`), and the API lists, per table,
     the set's sites after the table's CREATE (chosen: stored once; the
     table list follows from data already stored).
   - B. Each table stores its own list (rejected: every opaque statement
     is repeated on every table created before it; store size grows with
     tables x statements).
3. **Model tables in the API** (PD1).
   - A. Groups per source: `source: "models"` beside the migration
     groups in `/sets`, with model-specific fields and the same derived
     `completeness` vocabulary (chosen; ADR 0048 option 3 reserved the
     field).
   - B. A separate `/api/v0/schema/models` endpoint (rejected: the view
     would join two responses that can come from different index runs).
   - C. Keep them out (rejected by the developer's R2 decision).
4. **Overlap between a model table and a migration table** (PD2).
   - A. Derived by the server with L5's rule (exact name equality; the
     column-name difference only when both tables are `complete`),
     exposed on both tables, checked against the stored
     `schema_model_migration_overlap` diagnostics in a test (chosen: no
     message parsing, both directions from one rule).
   - B. Parse the diagnostic's message (rejected: messages are text, ADR
     0048 never parses them).
   - C. Store an attribute on the model table (rejected for now: a record
     change in T018c2's area for data the server can derive exactly; it
     would still give only one direction).
5. **Detected but unread tools.**
   - A. `unread_tools` on `/summary` and `/sets`, derived from stored file
     paths by a fixed marker rule (chosen: the scanner already stores
     every file it reads; no extractor change).
   - B. A detector pass that emits records (rejected: a new record kind
     for a hint that changes no fact).
6. **Assumption catalogue.**
   - A. `GET /api/v0/schema/assumptions` built from Go tables in the
     extractor packages (texts plus the row table), checked against the
     documents (chosen: one source, served once per page load).
   - B. Texts in the web bundle (rejected: a second copy that drifts from
     `contractAssumptions` and the docs).
   - C. Texts repeated on every fact (rejected: size).
7. **Where the assumptions are shown** (UI).
   - A. R2: "Rests on" lines, row chips where a row differs, popovers, the
     Assumptions tab (chosen by the developer, 2026-11-09).
   - B. Round 1 variant 3's right-hand "rests on" rail beside the detail
     (considered; kept as a documented option, not built in v1). The rail
     needs no API field beyond those the popovers use. Design, toggle,
     measurements and the sources to rebuild it are in
     the T018f rail-option mockup (internal).

## Decision

Options 1A, 2A, 3A, 4A, 5A, 6A and 7A. The exact shapes are in
the T018f task record (internal) Part 1; summarised:

- **Records** (extractor change, aggregate extractor version bumped once):
  migration `column` and `db_index` records gain `claims` (one entry per
  known fact: the fact name, the contract row that set it, the migration
  and byte span of the statement); table `reasons` entries gain `row`;
  migration records gain `opaque_sites`; model table and column `reasons`
  entries gain `fact` and `file`. These are provenance metadata, not
  claims: no fact changes, and a test checks each `claims` row against the
  fact kinds `TestContractFacts` allows that row. Contract v1 attributes
  are open maps, so `contract_version` stays 1.
- **API** (additive; `api_version` stays 0 as for every additive field
  since ADR 0048): `/sets` gains model groups (`source: "models"`),
  per-group and per-table `rests_on`, `unread_tools`, `overlaps` on
  tables of both kinds and `opaque_sites` on tables; `/summary` gains
  `model_groups`, `sources_read` and `unread_tools`, and `found` counts
  model groups; `/table` serves claims with `link` (IDE link, honouring
  `editor=` exactly as `ide` does), `opaque_sites` with links and the
  overlap in both directions; `/migration` lists its `opaque_sites`;
  new `GET /api/v0/schema/assumptions` (no nodes, no links, no `editor`).
- **Model groups (G3, 2026-11-09)**: one model group per ORM tool and
  schema root (the T018 task record (internal), Shared pieces 2), keyed
  by the model tables' stored `set` (`models/<tool>/<root>`). The server
  groups by source and stored `set`, so a model group never merges with a
  migration set. T018c2's SQLModel tables store `set: "models"` until
  T018g0 renames them; ADR 0054's record shape is not changed here, and
  the server needs no change when the key changes. Each group carries
  `tables_by_scope` (production and test counts, O3).
- **Presentation rules**: the table "Rests on" line is the intersection
  of the assumption sets of every row behind the table's facts (every
  fact rests on them); a row chip shows the assumptions a fact's row adds
  to that line; the group line is the union (the facts of the group rest
  on these) and is worded so ("Facts in <group> rest on").

## Consequences

- Facts can be explained down to the statement and the contract row; an
  E2-dependent index or an opaque statement is visible where it matters.
- Model tables appear in the Schema view, labelled by source, never merged
  with a migration table; "No migrations found" can no longer appear for a
  repository whose only migrations are Alembic.
- Stored records grow (claims per column and index, opaque sites per
  migration); the task measures the store size on axum, axum-ddd, fastapi
  and langflow and states it. Every index re-parses once (version bump).
- Indexes built before the bump have no `claims`: the API serves `claims:
  null` and the view says "row not recorded; re-index" instead of a row.
- `contractAssumptions` becomes read by a server path; its keys stay
  checked by `TestContractAssumptions`.

## Implementation notes (T018f Part 1)

- Claims are found by fingerprinting every held column and index before
  and after each statement (by object identity); a changed fact takes the
  statement's row that fits it, else the statement's own row
  (`internal/extract/schema/claims.go`). Table reason rows are the
  precondition id a reason cites, else the statement's row; `D` for the
  unlexed rest of a file; null only for the three set-level reasons.
- Model reason `fact`: `column_set` or `constraint_set` (table reasons),
  `type` (K1b), `primary_key` (an open primary key), `foreign_keys` (X1),
  null for a reason that makes a whole column unknown. `file` was already
  stored by T018c2.
- `/assumptions` keys `rows` by source, then id: the migration and
  SQLModel contracts both use M1-M9, D, T1, F1-F9 and others, so one map
  keyed by id would give one source's summary to the other's rows.
- Overlap column lists follow L5's condition exactly (migration `state`
  complete, model `column_set` complete), so the server and the
  `schema_model_migration_overlap` diagnostic agree.
- Model group diagnostics: the `schema_model_*` diagnostics of the
  repository's Python files belong to the group whose tool is `sqlmodel`
  (one per repository until T018g0), plus the `schema_*` diagnostics of
  the group's files.

## Revision 2 (T018f, 2026-11-09, after the independent review)

- Every reader of a column's references dispatches on the record's
  source (`columnReferences` beside `tableCompleteness`; the UI's
  `referenceEntries`): a model foreign key the linker left `unresolved`
  or `ambiguous` counts and shows as unresolved, never as "no foreign
  key".
- A model group's `table_list` is never `complete` while a `.py` file
  went unread (`python_unread`) or a scanner gap may hide one
  (`python_scan_gap`).
- `/table` neighbours use the source's completeness rule.
- A model table's `overlaps` is null while `unread_tools` is not empty.
- `/assumptions` serves plain text (a display text per row and
  assumption, derived from the unchanged documents) and `branch: null` for
  model rows.
- The view shows References and Keys empty only when the column is proven
  to have no foreign key, else "unknown" and "FK?" with a why popover.
- Model columns' `claims: null` means "not applicable" (documented in
  `docs/cli/serve.md`; no marker added).

## Store read by code (T018f, 2026-11-09)

The B1 rule and the model group counts read the `.py` files'
diagnostics. Reading them by file (`Diagnostics` over ~4,100 langflow
files, 33 chunked queries, ~3,400 decoded `unresolved_calls`) cost most
of `/schema/sets`. The `store` interface gains one read,
`DiagnosticsWithCodes(ctx, repository, codes, prefixes)`: the
diagnostics, in stored order, whose code is in `codes` or starts with a
prefix, on any file. Both drivers read the code from the stored payload
in SQL (`json_extract` on SQLite, `->>` on Postgres) and check the
decoded code again in Go, so the result is exact. The server keeps the
diagnostics of stored `.py` files among them (`schema_` codes and the
unread codes), which are all the model rules read; responses are
unchanged. Options considered: a `code` column on `diagnostics` with an
index (rejected here: a schema migration for ~5 ms; reconsider if more
views filter by code); a per-run response cache (not taken: the API is
documented as uncached; left to the developer).

## Known gaps

- Table-level keys (`primary_key`, `unique_keys`) carry no `claims` of
  their own; their "why" comes from the columns' `primary_key`/`unique`
  claims. A later change may add table claims.
- Model records carry `assumptions: ["V1", "V4", "V6", "V7"]` (O1, T018c2
  Revision 3.3); the catalogue also lists V2, which no record states.
- Per-table `opaque_sites` lists only statements counted in a migration's
  `opaque`; `opaque_after` also counts lexing failures, unreadable
  statements with an opaque kind and calls of set-defined functions in
  modelled statements, so such tables get `opaque_sites: null` (unknown,
  never a partial list).
- The Alembic marker rule finds script directories with the default
  layout; a project with another `script_location` or `version_locations`
  shows only its `alembic.ini` (with `path: null`), and a repository with
  neither shows nothing. `unread_tools` never states that a repository has
  no migrations.
- Set-level popovers (strip facts from `/sets`) show the migration's
  `file:line` as text and link to the migration in the Migrations tab
  (whose detail has "Open in IDE"), not an IDE link to the line: `/sets`
  carries no links (ADR 0048) and stays so.
- (Closed.) A migration table's `overlaps` is null unless
  every model table is listed (`sources_read` holds `models`, no B1
  reason, every model group's `table_list` complete); with no model
  group, `/sets` serves the B1 reasons as `model_list_reasons` and the
  dashed "No model table claimed" card names them.

## Review notes (delegated review, 2026-11-09)

Accepted after T018f Parts 1-2, Revisions 1 and 2 and steps f6 and f7
(records: the T018f task records (internal)).

- Results (gates, quiet machine, main = extractors-29, branch =
  extractors-30):
  - Corpora (`index --emit`, all 10): no record added or removed; header
    29 -> 30. Changed records only where expected: axum 37 (33 `claims`,
    4 `opaque_sites`), axum-ddd 47 (42 `claims`, 5 `opaque_sites`),
    langflow 37 nodes (44 model `reasons` entries gain `fact`); the other
    seven corpora 0; diagnostics identical.
  - Store size (fresh): axum +3.5 %, axum-ddd +1.9 %, fastapi 0 %,
    langflow 0 % (snapshot +605 bytes). Under the 10 % rule.
  - Timing (Langflow, interleaved, 6 runs, median): fresh 147.8 -> 146.9
    s (-0.6 %), warm 57.7 -> 57.7 s (0.0 %). Under the 10 % gate.
  - Latency (median of 20, after f7): langflow `/summary` 19.5 -> 22.8
    ms, `/sets` 20.9 -> 46.3 ms, `/table` (model) 17.7 ms,
    `/assumptions` 0.93 ms; axum `/sets` 3.08 -> 4.41 ms, `/table` 2.51
    -> 4.13 ms; fastapi `/sets` 1.09 -> 2.03 ms; `/assumptions` under
    5 ms everywhere. langflow `/sets` stays +122 % but under 50 ms; the
    developer accepted it on 2026-11-09 because main serves no model
    groups on langflow (`sets: []`), so the baseline does none of the new
    work. axum's 20 % excess is about a millisecond.
  - Measure check (`measure.cjs`, 15 states light and dark plus 390 px;
    after Revision 2, 18 states): top, head, strip, list, pane and
    detail header spans 0; every popover opens without a shift, takes
    focus, Escape refocuses the trigger; no popover descendant wider
    than its box; bottom sheet at 390 px; 0 external requests, 0 CSP,
    console or page errors. Playwright numbers (axum, axum-ddd, fastapi,
    langflow, mixed): 0 mismatches between DOM and API.
  - Screenshots: session scratchpad `T018f-built/` (light, dark,
    narrow, the Revision 2 fixes); none committed.
  - Alembic rule: fastapi `backend/app/alembic` (5), langflow
    `src/backend/base/langflow/alembic` (119), axum and axum-ddd `[]`.
  - CI: run 42660835411 on `2263675` green (Windows success; postgres
    job 0 FAIL; only expected skips: four helpers, env-gated
    `TestGenerate*`, `TestOracleRealHistories`, `TestModelCensus`,
    `TestModelOracle`, two `TestB3CasesPostgres` subcases needing
    `search_path` public).
- Revisions:
  - Revision 1 (G3): one model group per ORM tool and schema root, keyed
    by the stored `set`; `tool`, `pins`, `assumptions`, `files`,
    `table_list` and `tables_by_scope` per group.
  - Revision 2 (review fixes): A1, B1, C1-C9 below.
  - f6: a migration table's `overlaps` is null unless every model table
    is listed (mirror of C6); a model table's `overlaps` is null while
    `unread_tools` is not empty; `/sets` serves `model_list_reasons`
    (null when the model pass did not run) and the dashed "No model table
    claimed" card names them.
  - f7: `/sets` and `/summary` read file hashes once; the model rules
    read the `.py` diagnostics by code through
    `store.DiagnosticsWithCodes` (section "Store read by code");
    responses byte-identical.
- Independent review (one, of Parts 1 and 2), each finding fixed as a
  class rule with a test:
  - Class A (A1): an unresolved model foreign key was shown as no foreign
    key. Every reader of a column's references now dispatches on source.
  - Class B (B1): a model group's `table_list` was `complete` while
    `.py` files went unread; now `may_be_incomplete` with
    `python_unread` or `python_scan_gap`.
  - Class C: C1 popover overflow and focus scroll; C2 plain served text;
    C3 model rows `branch: null`; C4 `/table` neighbours by source; C5
    empty References/Keys only for a proven absence ("unknown", "FK?"
    otherwise); C6 model-side overlaps null with unread tools; C7 "test,
    hidden" mark on links to hidden tables; C8 strip overflow cue; C9
    model `claims: null` documented as not applicable.
- Developer decisions (2026-11-09):
  - R2 alone; the right-hand rail (option 7B) stays a documented option
    in the T018f rail-option mockup (internal), not built in v1.
  - Sticky (fixed) detail header; `unread_tools` in the schema summary.
  - O1: model records carry V1, V4, V6, V7 (done in T018c2). O2: the
    group line reads "Facts in <group> rest on" (union), the table line
    "Rests on" (intersection). O3: Include tests governs test-scope
    model tables per table, with hidden counts (`tables_by_scope`) and
    "test, hidden" marks. O4: `found: sets` for an index with model
    groups only.
  - The langflow `/sets` latency (46.3 ms against main's 20.9 ms) is
    accepted (f7 option a); no response cache.
- Known gaps and follow-ups (besides "Known gaps" above):
  - Assumption texts still use internal ids (E1, V4, row ids) in their
    prose.
  - SQLModel tables keep the group key `models` until T018g0 renames it
    to `models/<tool>/<root>`.
  - Tables whose `opaque_after` counts non-opaque statements (lexing
    failures, unreadable statements, set-defined function calls) get
    `opaque_sites: null`; a later record change could list them.
  - No table-level `claims` for `primary_key`/`unique_keys`.
  - A per-process `/sets` response cache (f7 option b) remains an option
    if repeat-request latency matters; it needs an ADR and ends the
    documented "uncached" property.

## Amendment (T018g6 Revision 4, 2026-11-15; status unchanged)

The model rows of `/api/v0/schema/assumptions` are now served per tool
(ADR 0064): `rows["models/<tool>"]` for drizzle, prisma,
sequelize, sqlmodel and typeorm, each with its own default row D, and
every row carries a `tool` field (`null` on migration rows). `rows.models`
is no longer served, because the tools share row ids (D, and any id two
contracts might reuse) and one key per id gave one tool's summary to
another tool's rows.

Deep links written before the change keep working: the web reads an
Assumptions tab entry `a=row:models:<id>` as `row:models/sqlmodel:<id>`
(SQLModel's rows were the only model rows `rows.models` served;
`web/src/sections/schema/params.ts`, `parseSchemaParams`), so an old
`#/schema?tab=assumptions&a=row:models:M1` opens SQLModel's row M1.
